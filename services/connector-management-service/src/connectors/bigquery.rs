//! BigQuery connector — productive REST implementation. Uses the BigQuery v2
//! REST API directly with reqwest plus a JWT-bearer OAuth2 exchange against
//! `oauth2.googleapis.com/token`. Auth flavours supported:
//!
//! * `service_account_json` (string OR JSON object) — preferred; builds the
//!   RS256 self-signed JWT and exchanges it for an access token.
//! * `access_token` — short-lived bearer token; sent as-is.
//!
//! Discovery enumerates datasets + tables. Sync runs `jobs.query` with
//! `useLegacySql=false`, then paginates `getQueryResults` via `pageToken`.
//! Results are materialised as Arrow IPC so the dataset-versioning-service can
//! create a new version downstream.

use std::time::{Instant, SystemTime, UNIX_EPOCH};

use jsonwebtoken::{Algorithm, EncodingKey, Header, encode};
use reqwest::{Url, header::HeaderMap};
use serde::Serialize;
use serde_json::{Map, Value, json};

use super::{
    ConnectionTestResult, SyncPayload, arrow_payload_from_rows, basic_discovered_source,
    http_runtime, virtual_table_response,
};
use crate::{
    AppState,
    models::registration::{DiscoveredSource, VirtualTableQueryRequest, VirtualTableQueryResponse},
};

const CONNECTOR_NAME: &str = "bigquery";
const DEFAULT_SOURCE_KIND: &str = "bigquery_table";
const TOKEN_URL: &str = "https://oauth2.googleapis.com/token";
const BIGQUERY_BASE: &str = "https://bigquery.googleapis.com/bigquery/v2/";
const BIGQUERY_SCOPE: &str = "https://www.googleapis.com/auth/bigquery";
const DEFAULT_PAGE_SIZE: i64 = 1_000;
const MAX_PAGES: usize = 100;

#[derive(Debug, Serialize)]
struct JwtClaims<'a> {
    iss: &'a str,
    scope: &'a str,
    aud: &'a str,
    iat: u64,
    exp: u64,
}

pub fn validate_config(config: &Value) -> Result<(), String> {
    let project = config.get("project_id").and_then(Value::as_str);
    if project.map(str::trim).unwrap_or("").is_empty() {
        return Err(format!("{CONNECTOR_NAME} connector requires 'project_id'"));
    }
    if config.get("access_token").is_none() && config.get("service_account_json").is_none() {
        return Err(format!(
            "{CONNECTOR_NAME} connector requires 'access_token' or 'service_account_json'"
        ));
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
    let token = obtain_access_token(state, config, agent_url).await?;
    let project_id = require_project(config)?;
    let url = bigquery_url(&format!("projects/{project_id}"))?;
    let response = http_runtime::get(
        state,
        config,
        url,
        HeaderMap::new(),
        Some(token),
        agent_url,
    )
    .await?;
    let latency_ms = started.elapsed().as_millis();
    if !(200..300).contains(&response.status) {
        return Err(format!("BigQuery returned HTTP {}", response.status));
    }
    let body = http_runtime::json_body(&response).unwrap_or(Value::Null);
    Ok(ConnectionTestResult {
        success: true,
        message: format!("BigQuery project '{project_id}' reachable"),
        latency_ms,
        details: Some(json!({
            "project_id": project_id,
            "friendly_name": body.get("friendlyName").cloned().unwrap_or(Value::Null),
            "location": body.get("location").cloned().unwrap_or(Value::Null),
        })),
    })
}

pub async fn discover_sources(
    state: &AppState,
    config: &Value,
    agent_url: Option<&str>,
) -> Result<Vec<DiscoveredSource>, String> {
    validate_config(config)?;
    let token = obtain_access_token(state, config, agent_url).await?;
    let project_id = require_project(config)?;
    let datasets = list_datasets(state, config, &token, project_id, agent_url).await?;

    let mut sources = Vec::new();
    for dataset_id in datasets {
        match list_tables(state, config, &token, project_id, &dataset_id, agent_url).await {
            Ok(tables) => {
                for table in tables {
                    let table_id = table
                        .get("tableReference")
                        .and_then(|reference| reference.get("tableId"))
                        .and_then(Value::as_str)
                        .unwrap_or_default()
                        .to_string();
                    if table_id.is_empty() {
                        continue;
                    }
                    let selector = format!("{dataset_id}.{table_id}");
                    sources.push(basic_discovered_source(
                        selector.clone(),
                        selector,
                        DEFAULT_SOURCE_KIND,
                        json!({
                            "project_id": project_id,
                            "dataset_id": dataset_id,
                            "table_id": table_id,
                            "type": table.get("type").cloned().unwrap_or(Value::Null),
                        }),
                    ));
                }
            }
            Err(error) => {
                tracing::warn!(
                    project = project_id,
                    dataset = dataset_id,
                    "bigquery list_tables failed: {error}"
                );
            }
        }
    }
    Ok(sources)
}

pub async fn fetch_dataset(
    state: &AppState,
    config: &Value,
    selector: &str,
    agent_url: Option<&str>,
) -> Result<SyncPayload, String> {
    validate_config(config)?;
    let token = obtain_access_token(state, config, agent_url).await?;
    let project_id = require_project(config)?;
    let query = build_query(config, selector)?;

    let initial_url = bigquery_url(&format!("projects/{project_id}/queries"))?;
    let body = json!({
        "query": query,
        "useLegacySql": false,
        "maxResults": page_size(config),
        "useQueryCache": true,
        "location": config.get("location").and_then(Value::as_str),
    });
    let response = http_runtime::post_json(
        state,
        config,
        initial_url,
        HeaderMap::new(),
        Some(token.clone()),
        &body,
        agent_url,
    )
    .await?;
    if !(200..300).contains(&response.status) {
        return Err(format!(
            "BigQuery jobs.query returned HTTP {}: {}",
            response.status,
            String::from_utf8_lossy(&response.bytes)
        ));
    }
    let mut payload = http_runtime::json_body(&response)?;

    let columns = extract_columns(payload.get("schema"));
    let mut rows = extract_rows(payload.get("rows"), &columns);
    let job_reference = payload
        .get("jobReference")
        .cloned()
        .unwrap_or(Value::Null);
    let job_id = job_reference
        .get("jobId")
        .and_then(Value::as_str)
        .unwrap_or_default()
        .to_string();

    let mut total_rows: i64 = payload
        .get("totalRows")
        .and_then(Value::as_str)
        .and_then(|value| value.parse().ok())
        .unwrap_or_else(|| rows.len() as i64);

    let mut pages = 1usize;
    while let Some(page_token) = payload
        .get("pageToken")
        .and_then(Value::as_str)
        .map(str::to_string)
    {
        if pages >= MAX_PAGES {
            tracing::warn!(
                "bigquery fetch hit MAX_PAGES={MAX_PAGES} for job {job_id}; stopping pagination"
            );
            break;
        }
        if job_id.is_empty() {
            break;
        }
        let mut next_url = bigquery_url(&format!("projects/{project_id}/queries/{job_id}"))?;
        next_url
            .query_pairs_mut()
            .append_pair("pageToken", &page_token)
            .append_pair("maxResults", &page_size(config).to_string());

        let next = http_runtime::get(
            state,
            config,
            next_url,
            HeaderMap::new(),
            Some(token.clone()),
            agent_url,
        )
        .await?;
        if !(200..300).contains(&next.status) {
            return Err(format!(
                "BigQuery getQueryResults returned HTTP {}",
                next.status
            ));
        }
        payload = http_runtime::json_body(&next)?;
        let next_rows = extract_rows(payload.get("rows"), &columns);
        if next_rows.is_empty() {
            break;
        }
        total_rows = payload
            .get("totalRows")
            .and_then(Value::as_str)
            .and_then(|value| value.parse().ok())
            .unwrap_or(total_rows);
        rows.extend(next_rows);
        pages += 1;
    }

    let metadata = json!({
        "project_id": project_id,
        "selector": selector,
        "query": query,
        "job_reference": job_reference,
        "pages": pages,
        "total_rows": total_rows,
        "agent_url": agent_url,
    });
    arrow_payload_from_rows(
        format!("bigquery_{}.arrow", sanitize_file_name(selector)),
        columns,
        rows,
        metadata,
    )
}

pub async fn query_virtual_table(
    state: &AppState,
    config: &Value,
    request: &VirtualTableQueryRequest,
    agent_url: Option<&str>,
) -> Result<VirtualTableQueryResponse, String> {
    let payload = fetch_dataset(state, config, &request.selector, agent_url).await?;
    let limit = request.limit.unwrap_or(50).clamp(1, 500);
    // Decode arrow back into JSON rows is heavy; use the metadata + a SELECT
    // LIMIT shortcut by re-running a bounded query on demand instead.
    let rows = bounded_preview(state, config, &request.selector, limit, agent_url).await?;
    Ok(virtual_table_response(
        &request.selector,
        rows,
        payload.metadata,
    ))
}

async fn bounded_preview(
    state: &AppState,
    config: &Value,
    selector: &str,
    limit: usize,
    agent_url: Option<&str>,
) -> Result<Vec<Value>, String> {
    let token = obtain_access_token(state, config, agent_url).await?;
    let project_id = require_project(config)?;
    let preview_query = if selector.trim().to_ascii_lowercase().starts_with("select ") {
        format!("SELECT * FROM ({}) LIMIT {limit}", selector.trim())
    } else {
        format!(
            "SELECT * FROM `{project_id}.{}` LIMIT {limit}",
            selector.trim()
        )
    };
    let url = bigquery_url(&format!("projects/{project_id}/queries"))?;
    let body = json!({
        "query": preview_query,
        "useLegacySql": false,
        "maxResults": limit,
        "useQueryCache": true,
    });
    let response = http_runtime::post_json(
        state,
        config,
        url,
        HeaderMap::new(),
        Some(token),
        &body,
        agent_url,
    )
    .await?;
    if !(200..300).contains(&response.status) {
        return Err(format!("BigQuery preview returned HTTP {}", response.status));
    }
    let payload = http_runtime::json_body(&response)?;
    let columns = extract_columns(payload.get("schema"));
    Ok(extract_rows(payload.get("rows"), &columns))
}

async fn list_datasets(
    state: &AppState,
    config: &Value,
    token: &str,
    project_id: &str,
    agent_url: Option<&str>,
) -> Result<Vec<String>, String> {
    let url = bigquery_url(&format!("projects/{project_id}/datasets"))?;
    let response = http_runtime::get(
        state,
        config,
        url,
        HeaderMap::new(),
        Some(token.to_string()),
        agent_url,
    )
    .await?;
    if !(200..300).contains(&response.status) {
        return Err(format!("BigQuery listDatasets HTTP {}", response.status));
    }
    let payload = http_runtime::json_body(&response)?;
    Ok(payload
        .get("datasets")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default()
        .into_iter()
        .filter_map(|item| {
            item.get("datasetReference")
                .and_then(|reference| reference.get("datasetId"))
                .and_then(Value::as_str)
                .map(str::to_string)
        })
        .collect())
}

async fn list_tables(
    state: &AppState,
    config: &Value,
    token: &str,
    project_id: &str,
    dataset_id: &str,
    agent_url: Option<&str>,
) -> Result<Vec<Value>, String> {
    let url = bigquery_url(&format!(
        "projects/{project_id}/datasets/{dataset_id}/tables"
    ))?;
    let response = http_runtime::get(
        state,
        config,
        url,
        HeaderMap::new(),
        Some(token.to_string()),
        agent_url,
    )
    .await?;
    if !(200..300).contains(&response.status) {
        return Err(format!("BigQuery listTables HTTP {}", response.status));
    }
    let payload = http_runtime::json_body(&response)?;
    Ok(payload
        .get("tables")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default())
}

async fn obtain_access_token(
    state: &AppState,
    config: &Value,
    agent_url: Option<&str>,
) -> Result<String, String> {
    if let Some(token) = config.get("access_token").and_then(Value::as_str) {
        return Ok(token.to_string());
    }
    let service_account = parse_service_account(config)?;
    let jwt = build_service_account_jwt(&service_account)?;

    let url = Url::parse(TOKEN_URL).map_err(|error| error.to_string())?;
    let form = vec![
        (
            "grant_type".to_string(),
            "urn:ietf:params:oauth:grant-type:jwt-bearer".to_string(),
        ),
        ("assertion".to_string(), jwt),
    ];
    let response = http_runtime::post_form(
        state,
        config,
        url,
        HeaderMap::new(),
        &form,
        agent_url,
    )
    .await?;
    if !(200..300).contains(&response.status) {
        return Err(format!(
            "BigQuery token exchange returned HTTP {}: {}",
            response.status,
            String::from_utf8_lossy(&response.bytes)
        ));
    }
    let payload = http_runtime::json_body(&response)?;
    payload
        .get("access_token")
        .and_then(Value::as_str)
        .map(str::to_string)
        .ok_or_else(|| "BigQuery token exchange response missing 'access_token'".to_string())
}

#[derive(Debug)]
struct ServiceAccount {
    client_email: String,
    private_key: String,
    private_key_id: Option<String>,
    token_uri: String,
}

fn parse_service_account(config: &Value) -> Result<ServiceAccount, String> {
    let raw = config
        .get("service_account_json")
        .ok_or_else(|| "missing 'service_account_json'".to_string())?;
    let object: Map<String, Value> = match raw {
        Value::String(text) => serde_json::from_str(text)
            .map_err(|error| format!("service_account_json is not valid JSON: {error}"))?,
        Value::Object(map) => map.clone(),
        _ => return Err("service_account_json must be a JSON object or a JSON string".to_string()),
    };
    let client_email = object
        .get("client_email")
        .and_then(Value::as_str)
        .ok_or_else(|| "service_account_json missing 'client_email'".to_string())?
        .to_string();
    let private_key = object
        .get("private_key")
        .and_then(Value::as_str)
        .ok_or_else(|| "service_account_json missing 'private_key'".to_string())?
        .to_string();
    let private_key_id = object
        .get("private_key_id")
        .and_then(Value::as_str)
        .map(str::to_string);
    let token_uri = object
        .get("token_uri")
        .and_then(Value::as_str)
        .unwrap_or(TOKEN_URL)
        .to_string();
    Ok(ServiceAccount {
        client_email,
        private_key,
        private_key_id,
        token_uri,
    })
}

fn build_service_account_jwt(service_account: &ServiceAccount) -> Result<String, String> {
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|error| error.to_string())?
        .as_secs();
    let claims = JwtClaims {
        iss: &service_account.client_email,
        scope: BIGQUERY_SCOPE,
        aud: &service_account.token_uri,
        iat: now,
        exp: now + 3_600,
    };
    let mut header = Header::new(Algorithm::RS256);
    header.kid = service_account.private_key_id.clone();
    let key = EncodingKey::from_rsa_pem(service_account.private_key.as_bytes())
        .map_err(|error| format!("invalid service-account private key: {error}"))?;
    encode(&header, &claims, &key).map_err(|error| error.to_string())
}

fn require_project(config: &Value) -> Result<&str, String> {
    config
        .get("project_id")
        .and_then(Value::as_str)
        .filter(|value| !value.trim().is_empty())
        .ok_or_else(|| "bigquery connector requires 'project_id'".to_string())
}

fn bigquery_url(path: &str) -> Result<Url, String> {
    Url::parse(BIGQUERY_BASE)
        .and_then(|base| base.join(path))
        .map_err(|error| error.to_string())
}

fn build_query(config: &Value, selector: &str) -> Result<String, String> {
    let trimmed = selector.trim();
    if trimmed.to_ascii_lowercase().starts_with("select ") {
        return Ok(trimmed.to_string());
    }
    let project_id = require_project(config)?;
    if !trimmed.is_empty() {
        return Ok(format!("SELECT * FROM `{project_id}.{trimmed}`"));
    }
    config
        .get("query")
        .and_then(Value::as_str)
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
        .ok_or_else(|| "bigquery sync requires a SQL query or table selector".to_string())
}

fn page_size(config: &Value) -> i64 {
    config
        .get("page_size")
        .and_then(Value::as_i64)
        .unwrap_or(DEFAULT_PAGE_SIZE)
        .clamp(1, 100_000)
}

fn extract_columns(schema: Option<&Value>) -> Vec<String> {
    schema
        .and_then(|schema| schema.get("fields"))
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default()
        .into_iter()
        .filter_map(|field| field.get("name").and_then(Value::as_str).map(str::to_string))
        .collect()
}

fn extract_rows(rows: Option<&Value>, columns: &[String]) -> Vec<Value> {
    rows.and_then(Value::as_array)
        .cloned()
        .unwrap_or_default()
        .into_iter()
        .map(|row| {
            let cells = row
                .get("f")
                .and_then(Value::as_array)
                .cloned()
                .unwrap_or_default();
            let mut object = Map::with_capacity(columns.len());
            for (idx, column) in columns.iter().enumerate() {
                let cell = cells
                    .get(idx)
                    .and_then(|cell| cell.get("v"))
                    .cloned()
                    .unwrap_or(Value::Null);
                object.insert(column.clone(), cell);
            }
            Value::Object(object)
        })
        .collect()
}

fn sanitize_file_name(selector: &str) -> String {
    selector
        .chars()
        .map(|c| if c.is_ascii_alphanumeric() { c } else { '_' })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn requires_project_and_credential() {
        assert!(validate_config(&json!({})).is_err());
        assert!(
            validate_config(&json!({ "project_id": "p", "access_token": "t" })).is_ok()
        );
        assert!(validate_config(&json!({ "project_id": "p" })).is_err());
    }

    #[test]
    fn build_query_accepts_table_selector() {
        let config = json!({ "project_id": "demo" });
        let q = build_query(&config, "ds.table").unwrap();
        assert_eq!(q, "SELECT * FROM `demo.ds.table`");
    }

    #[test]
    fn build_query_passes_through_select() {
        let config = json!({ "project_id": "demo" });
        let q = build_query(&config, "select 1 as v").unwrap();
        assert_eq!(q, "select 1 as v");
    }

    #[test]
    fn extracts_rows_from_bigquery_envelope() {
        let columns = vec!["a".to_string(), "b".to_string()];
        let body = json!({
            "rows": [
                {"f": [{"v": "1"}, {"v": "x"}]},
                {"f": [{"v": "2"}, {"v": null}]}
            ]
        });
        let rows = extract_rows(body.get("rows"), &columns);
        assert_eq!(rows.len(), 2);
        assert_eq!(rows[0]["a"], json!("1"));
        assert_eq!(rows[1]["b"], Value::Null);
    }

    #[test]
    fn extracts_columns_from_schema() {
        let schema = json!({
            "fields": [{ "name": "id" }, { "name": "name" }]
        });
        let columns = extract_columns(Some(&schema));
        assert_eq!(columns, vec!["id", "name"]);
    }

    #[test]
    fn parse_service_account_accepts_string_form() {
        let config = json!({
            "service_account_json": "{\"client_email\":\"sa@example.iam.gserviceaccount.com\",\"private_key\":\"-----BEGIN-----\\n...\\n-----END-----\"}"
        });
        let sa = parse_service_account(&config).unwrap();
        assert_eq!(sa.client_email, "sa@example.iam.gserviceaccount.com");
        assert_eq!(sa.token_uri, TOKEN_URL);
    }
}
