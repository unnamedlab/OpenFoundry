//! Salesforce connector — validates an org token and runs SOQL queries for sync.

use std::time::Instant;

use reqwest::Url;
use serde_json::{Value, json};

use super::{
    ConnectionTestResult, SyncPayload, arrow_payload_from_rows, basic_discovered_source,
    http_runtime, virtual_table_response,
};
use crate::{
    AppState,
    models::registration::{DiscoveredSource, VirtualTableQueryRequest, VirtualTableQueryResponse},
};

pub fn validate_config(config: &Value) -> Result<(), String> {
    let required = ["instance_url", "access_token"];
    for field in required {
        if config.get(field).is_none() {
            return Err(format!("salesforce connector requires '{field}'"));
        }
    }
    Ok(())
}

pub async fn test_connection(
    state: &AppState,
    config: &Value,
    agent_url: Option<&str>,
) -> Result<ConnectionTestResult, String> {
    validate_config(config)?;

    let started = Instant::now();
    let url = api_base_url(config)?
        .join("limits")
        .map_err(|error| error.to_string())?;
    let response = http_runtime::get(
        state,
        config,
        url.clone(),
        http_runtime::header_map(config)?,
        Some(access_token(config)?.to_string()),
        agent_url,
    )
    .await?;
    let latency_ms = started.elapsed().as_millis();

    if !(200..300).contains(&response.status) {
        return Err(format!("Salesforce returned HTTP {}", response.status));
    }

    let payload = http_runtime::json_body(&response)?;
    Ok(ConnectionTestResult {
        success: true,
        message: "Salesforce org reachable".to_string(),
        latency_ms,
        details: Some(json!({
            "instance_url": config.get("instance_url").and_then(Value::as_str).unwrap_or_default(),
            "api_version": api_version(config),
            "limits_keys": payload.as_object().map(|object| object.len()).unwrap_or(0),
            "agent_url": agent_url,
        })),
    })
}

pub async fn fetch_dataset(
    state: &AppState,
    config: &Value,
    selector: &str,
    agent_url: Option<&str>,
) -> Result<SyncPayload, String> {
    validate_config(config)?;

    let soql = soql_query(config, selector)?;
    let endpoint = if include_deleted(config) {
        "queryAll"
    } else {
        "query"
    };
    let mut url = api_base_url(config)?
        .join(endpoint)
        .map_err(|error| error.to_string())?;
    url.query_pairs_mut().append_pair("q", &soql);

    let mut all_records: Vec<Value> = Vec::new();
    let mut total_size: i64 = 0;
    let mut pages = 0usize;
    let mut next_url = Some(url.clone());

    while let Some(current_url) = next_url.take() {
        if pages >= max_pages(config) {
            tracing::warn!(
                "salesforce fetch hit max pages ({}); stopping pagination",
                max_pages(config)
            );
            break;
        }
        let response = http_runtime::get(
            state,
            config,
            current_url.clone(),
            http_runtime::header_map(config)?,
            Some(access_token(config)?.to_string()),
            agent_url,
        )
        .await?;
        if !(200..300).contains(&response.status) {
            return Err(format!("Salesforce returned HTTP {}", response.status));
        }
        let payload = http_runtime::json_body(&response)?;
        if let Some(size) = payload.get("totalSize").and_then(Value::as_i64) {
            total_size = size;
        }
        let mut page_records = payload
            .get("records")
            .and_then(Value::as_array)
            .cloned()
            .unwrap_or_default()
            .into_iter()
            .map(|mut record| {
                if let Value::Object(object) = &mut record {
                    object.remove("attributes");
                }
                record
            })
            .collect::<Vec<_>>();
        all_records.append(&mut page_records);
        pages += 1;

        if payload.get("done").and_then(Value::as_bool).unwrap_or(true) {
            break;
        }
        if let Some(path) = payload.get("nextRecordsUrl").and_then(Value::as_str) {
            let instance = config
                .get("instance_url")
                .and_then(Value::as_str)
                .ok_or_else(|| "salesforce missing 'instance_url'".to_string())?;
            let base = Url::parse(instance).map_err(|error| error.to_string())?;
            next_url = Some(base.join(path).map_err(|error| error.to_string())?);
        }
    }

    let columns = infer_columns(&all_records);
    let metadata = json!({
        "query": soql,
        "endpoint": endpoint,
        "total_size": total_size,
        "fetched": all_records.len(),
        "pages": pages,
        "url": url.as_str(),
        "agent_url": agent_url,
    });
    arrow_payload_from_rows(
        format!("salesforce_{}.arrow", sanitize_file_name(selector)),
        columns,
        all_records,
        metadata,
    )
}

pub async fn discover_sources(
    state: &AppState,
    config: &Value,
    agent_url: Option<&str>,
) -> Result<Vec<DiscoveredSource>, String> {
    validate_config(config)?;
    let url = api_base_url(config)?
        .join("sobjects")
        .map_err(|error| error.to_string())?;
    let response = http_runtime::get(
        state,
        config,
        url,
        http_runtime::header_map(config)?,
        Some(access_token(config)?.to_string()),
        agent_url,
    )
    .await?;
    if !(200..300).contains(&response.status) {
        return Err(format!(
            "Salesforce catalog returned HTTP {}",
            response.status
        ));
    }

    let payload = http_runtime::json_body(&response)?;
    Ok(payload
        .get("sobjects")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default()
        .into_iter()
        .filter_map(|item| {
            let name = item.get("name").and_then(Value::as_str)?.to_string();
            let label = item
                .get("label")
                .and_then(Value::as_str)
                .unwrap_or(&name)
                .to_string();
            Some(basic_discovered_source(
                name.clone(),
                label,
                "salesforce_object",
                item,
            ))
        })
        .collect())
}

pub async fn query_virtual_table(
    state: &AppState,
    config: &Value,
    request: &VirtualTableQueryRequest,
    agent_url: Option<&str>,
) -> Result<VirtualTableQueryResponse, String> {
    validate_config(config)?;
    // Run a bounded SOQL query directly to avoid materialising a full sync.
    let limit = request.limit.unwrap_or(50).clamp(1, 500) as i64;
    let soql = bounded_soql(config, &request.selector, limit)?;
    let mut url = api_base_url(config)?
        .join("query")
        .map_err(|error| error.to_string())?;
    url.query_pairs_mut().append_pair("q", &soql);

    let response = http_runtime::get(
        state,
        config,
        url.clone(),
        http_runtime::header_map(config)?,
        Some(access_token(config)?.to_string()),
        agent_url,
    )
    .await?;
    if !(200..300).contains(&response.status) {
        return Err(format!("Salesforce returned HTTP {}", response.status));
    }
    let payload = http_runtime::json_body(&response)?;
    let rows = payload
        .get("records")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default()
        .into_iter()
        .map(|mut record| {
            if let Value::Object(object) = &mut record {
                object.remove("attributes");
            }
            record
        })
        .collect::<Vec<_>>();
    Ok(virtual_table_response(
        &request.selector,
        rows,
        json!({ "query": soql, "url": url.as_str() }),
    ))
}

fn access_token(config: &Value) -> Result<&str, String> {
    config
        .get("access_token")
        .and_then(Value::as_str)
        .ok_or_else(|| "salesforce connector requires 'access_token'".to_string())
}

fn api_base_url(config: &Value) -> Result<Url, String> {
    let instance_url = config
        .get("instance_url")
        .and_then(Value::as_str)
        .ok_or_else(|| "salesforce connector requires 'instance_url'".to_string())?;
    let base = Url::parse(instance_url).map_err(|error| error.to_string())?;
    base.join(&format!("/services/data/{}/", api_version(config)))
        .map_err(|error| error.to_string())
}

fn api_version(config: &Value) -> &str {
    config
        .get("api_version")
        .and_then(Value::as_str)
        .filter(|value| !value.trim().is_empty())
        .unwrap_or("v60.0")
}

fn row_limit(config: &Value) -> i64 {
    config
        .get("row_limit")
        .and_then(Value::as_i64)
        .unwrap_or(200)
        .clamp(1, 2_000)
}

fn soql_query(config: &Value, selector: &str) -> Result<String, String> {
    if selector.trim().to_ascii_lowercase().starts_with("select ") {
        return Ok(selector.trim().to_string());
    }

    if !selector.trim().is_empty() {
        return Ok(format!(
            "SELECT Id, Name FROM {} LIMIT {}",
            selector.trim(),
            row_limit(config)
        ));
    }

    config
        .get("query")
        .and_then(Value::as_str)
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
        .ok_or_else(|| "salesforce sync requires a SOQL query or object selector".to_string())
}

fn bounded_soql(config: &Value, selector: &str, limit: i64) -> Result<String, String> {
    let trimmed = selector.trim();
    if trimmed.to_ascii_lowercase().starts_with("select ") {
        return Ok(trimmed.to_string());
    }
    if !trimmed.is_empty() {
        return Ok(format!("SELECT Id, Name FROM {trimmed} LIMIT {limit}"));
    }
    let _ = config;
    Err("salesforce virtual_table requires a selector".to_string())
}

fn include_deleted(config: &Value) -> bool {
    config
        .get("include_deleted")
        .and_then(Value::as_bool)
        .unwrap_or(false)
}

fn max_pages(config: &Value) -> usize {
    config
        .get("max_pages")
        .and_then(Value::as_u64)
        .unwrap_or(50)
        .clamp(1, 1_000) as usize
}

fn infer_columns(records: &[Value]) -> Vec<String> {
    let mut columns: Vec<String> = Vec::new();
    let mut seen = std::collections::HashSet::new();
    for record in records {
        if let Some(object) = record.as_object() {
            for key in object.keys() {
                if seen.insert(key.clone()) {
                    columns.push(key.clone());
                }
            }
        }
    }
    columns
}

fn sanitize_file_name(selector: &str) -> String {
    selector
        .chars()
        .map(|c| if c.is_ascii_alphanumeric() { c } else { '_' })
        .collect()
}
