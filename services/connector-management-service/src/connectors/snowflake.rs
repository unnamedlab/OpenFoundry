//! Snowflake connector — productive REST implementation against the
//! `/api/v2/statements` endpoint. Supported auth flavours:
//!
//! * **Keypair JWT (preferred)** — `private_key_pem` (PKCS#8 RSA) plus
//!   `public_key_fingerprint` (`SHOW USERS` exposes it as
//!   `RSA_PUBLIC_KEY_FP`). Built via RS256.
//! * **OAuth bearer** — `oauth_token` is forwarded as `Authorization: Bearer`
//!   with `X-Snowflake-Authorization-Token-Type: OAUTH`.
//!
//! Discovery runs `SHOW TABLES IN SCHEMA :database.:schema`; sync executes
//! `SELECT * ... LIMIT n` (or a user-supplied SQL) and paginates over the
//! `partitionInfo[]` array via `?partition=N`. Result rows are materialised as
//! Arrow IPC for the dataset-versioning-service to create a new version.

use std::time::{Instant, SystemTime, UNIX_EPOCH};

use jsonwebtoken::{Algorithm, EncodingKey, Header, encode};
use reqwest::{
    Url,
    header::{HeaderMap, HeaderName, HeaderValue},
};
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

const CONNECTOR_NAME: &str = "snowflake";
const DEFAULT_SOURCE_KIND: &str = "snowflake_table";
const DEFAULT_PAGE_SIZE: i64 = 1_000;
const MAX_PARTITIONS: usize = 50;

#[derive(Debug, Serialize)]
struct JwtClaims<'a> {
    iss: String,
    sub: &'a str,
    iat: u64,
    exp: u64,
}

pub fn validate_config(config: &Value) -> Result<(), String> {
    for field in ["account", "database", "schema"] {
        if config
            .get(field)
            .and_then(Value::as_str)
            .map(str::trim)
            .unwrap_or("")
            .is_empty()
        {
            return Err(format!("{CONNECTOR_NAME} connector requires '{field}'"));
        }
    }
    let has_jwt = config.get("private_key_pem").is_some()
        && config.get("public_key_fingerprint").is_some()
        && config.get("user").is_some();
    let has_oauth = config.get("oauth_token").is_some();
    if !has_jwt && !has_oauth {
        return Err(format!(
            "{CONNECTOR_NAME} connector requires either ('user' + 'private_key_pem' + 'public_key_fingerprint') or 'oauth_token'"
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
    let response = execute_statement(
        state,
        config,
        "SELECT CURRENT_VERSION() AS version",
        Some(1),
        agent_url,
    )
    .await?;
    let latency_ms = started.elapsed().as_millis();
    let columns = extract_columns(response.get("resultSetMetaData"));
    let rows = extract_rows(response.get("data"), &columns);
    Ok(ConnectionTestResult {
        success: true,
        message: "Snowflake account reachable".to_string(),
        latency_ms,
        details: Some(json!({
            "account": config.get("account"),
            "database": config.get("database"),
            "schema": config.get("schema"),
            "version": rows
                .first()
                .and_then(|row| row.get("VERSION"))
                .cloned()
                .unwrap_or(Value::Null),
        })),
    })
}

pub async fn discover_sources(
    state: &AppState,
    config: &Value,
    agent_url: Option<&str>,
) -> Result<Vec<DiscoveredSource>, String> {
    validate_config(config)?;
    let database = require(config, "database")?;
    let schema = require(config, "schema")?;
    let statement = format!("SHOW TABLES IN SCHEMA {database}.{schema}");
    let response = execute_statement(state, config, &statement, None, agent_url).await?;
    let columns = extract_columns(response.get("resultSetMetaData"));
    let rows = extract_rows(response.get("data"), &columns);
    Ok(rows
        .into_iter()
        .filter_map(|row| {
            let name = row
                .get("name")
                .or_else(|| row.get("NAME"))
                .and_then(Value::as_str)?
                .to_string();
            let selector = format!("{database}.{schema}.{name}");
            Some(basic_discovered_source(
                selector.clone(),
                selector,
                DEFAULT_SOURCE_KIND,
                row,
            ))
        })
        .collect())
}

pub async fn fetch_dataset(
    state: &AppState,
    config: &Value,
    selector: &str,
    agent_url: Option<&str>,
) -> Result<SyncPayload, String> {
    validate_config(config)?;
    let limit = page_size(config);
    let statement = build_query(config, selector, limit)?;

    let response = execute_statement(state, config, &statement, Some(limit), agent_url).await?;
    let columns = extract_columns(response.get("resultSetMetaData"));
    let mut rows = extract_rows(response.get("data"), &columns);

    let statement_handle = response
        .get("statementHandle")
        .and_then(Value::as_str)
        .unwrap_or_default()
        .to_string();
    let partitions = response
        .get("resultSetMetaData")
        .and_then(|meta| meta.get("partitionInfo"))
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default();
    let partition_count = partitions.len();

    if partition_count > 1 && !statement_handle.is_empty() {
        let cap = partition_count.min(MAX_PARTITIONS);
        for partition_index in 1..cap {
            let partition_response = fetch_partition(
                state,
                config,
                &statement_handle,
                partition_index,
                agent_url,
            )
            .await?;
            let next_rows = extract_rows(partition_response.get("data"), &columns);
            rows.extend(next_rows);
        }
        if partition_count > MAX_PARTITIONS {
            tracing::warn!(
                "snowflake fetch hit MAX_PARTITIONS={MAX_PARTITIONS} (of {partition_count}) for statement {statement_handle}"
            );
        }
    }

    let metadata = json!({
        "selector": selector,
        "statement": statement,
        "statement_handle": statement_handle,
        "partitions": partition_count,
        "agent_url": agent_url,
    });
    arrow_payload_from_rows(
        format!("snowflake_{}.arrow", sanitize_file_name(selector)),
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
    let limit = request.limit.unwrap_or(50).clamp(1, 500) as i64;
    let statement = build_query(config, &request.selector, limit)?;
    let response = execute_statement(state, config, &statement, Some(limit), agent_url).await?;
    let columns = extract_columns(response.get("resultSetMetaData"));
    let rows = extract_rows(response.get("data"), &columns);
    let metadata = json!({
        "statement": statement,
        "statement_handle": response.get("statementHandle"),
    });
    Ok(virtual_table_response(&request.selector, rows, metadata))
}

async fn execute_statement(
    state: &AppState,
    config: &Value,
    statement: &str,
    limit: Option<i64>,
    agent_url: Option<&str>,
) -> Result<Value, String> {
    let url = base_url(config)?
        .join("/api/v2/statements")
        .map_err(|error| error.to_string())?;
    let token = obtain_token(config)?;
    let headers = build_auth_headers(config, &token)?;
    let body = build_statement_body(config, statement, limit);

    let response = http_runtime::post_json(
        state,
        config,
        url,
        headers,
        Some(token.token.clone()),
        &body,
        agent_url,
    )
    .await?;
    if !(200..300).contains(&response.status) {
        return Err(format!(
            "Snowflake statements returned HTTP {}: {}",
            response.status,
            String::from_utf8_lossy(&response.bytes)
        ));
    }
    http_runtime::json_body(&response)
}

async fn fetch_partition(
    state: &AppState,
    config: &Value,
    statement_handle: &str,
    partition: usize,
    agent_url: Option<&str>,
) -> Result<Value, String> {
    let mut url = base_url(config)?
        .join(&format!("/api/v2/statements/{statement_handle}"))
        .map_err(|error| error.to_string())?;
    url.query_pairs_mut()
        .append_pair("partition", &partition.to_string());
    let token = obtain_token(config)?;
    let headers = build_auth_headers(config, &token)?;
    let response = http_runtime::get(
        state,
        config,
        url,
        headers,
        Some(token.token),
        agent_url,
    )
    .await?;
    if !(200..300).contains(&response.status) {
        return Err(format!(
            "Snowflake partition fetch HTTP {}",
            response.status
        ));
    }
    http_runtime::json_body(&response)
}

fn build_statement_body(config: &Value, statement: &str, limit: Option<i64>) -> Value {
    let mut object = Map::new();
    object.insert("statement".to_string(), Value::String(statement.to_string()));
    object.insert("timeout".to_string(), Value::from(60));
    if let Some(database) = config.get("database").and_then(Value::as_str) {
        object.insert("database".to_string(), Value::String(database.to_string()));
    }
    if let Some(schema) = config.get("schema").and_then(Value::as_str) {
        object.insert("schema".to_string(), Value::String(schema.to_string()));
    }
    if let Some(warehouse) = config.get("warehouse").and_then(Value::as_str) {
        object.insert(
            "warehouse".to_string(),
            Value::String(warehouse.to_string()),
        );
    }
    if let Some(role) = config.get("role").and_then(Value::as_str) {
        object.insert("role".to_string(), Value::String(role.to_string()));
    }
    if let Some(limit) = limit {
        object.insert(
            "parameters".to_string(),
            json!({ "ROWS_PER_RESULTSET": limit.to_string() }),
        );
    }
    Value::Object(object)
}

struct AuthToken {
    token: String,
    kind: &'static str,
}

fn obtain_token(config: &Value) -> Result<AuthToken, String> {
    if let Some(oauth) = config.get("oauth_token").and_then(Value::as_str) {
        return Ok(AuthToken {
            token: oauth.to_string(),
            kind: "OAUTH",
        });
    }
    let user = config
        .get("user")
        .and_then(Value::as_str)
        .ok_or_else(|| "snowflake connector requires 'user' for keypair JWT".to_string())?
        .to_ascii_uppercase();
    let account = normalize_account(require(config, "account")?);
    let fingerprint = config
        .get("public_key_fingerprint")
        .and_then(Value::as_str)
        .ok_or_else(|| "snowflake connector requires 'public_key_fingerprint' for keypair JWT".to_string())?
        .to_string();
    let private_key = config
        .get("private_key_pem")
        .and_then(Value::as_str)
        .ok_or_else(|| "snowflake connector requires 'private_key_pem' for keypair JWT".to_string())?;

    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|error| error.to_string())?
        .as_secs();
    let qualified_user = format!("{account}.{user}");
    let claims = JwtClaims {
        iss: format!("{qualified_user}.{fingerprint}"),
        sub: &qualified_user,
        iat: now,
        exp: now + 3_600,
    };
    let header = Header::new(Algorithm::RS256);
    let key = EncodingKey::from_rsa_pem(private_key.as_bytes())
        .map_err(|error| format!("invalid snowflake private key: {error}"))?;
    let token = encode(&header, &claims, &key).map_err(|error| error.to_string())?;
    Ok(AuthToken {
        token,
        kind: "KEYPAIR_JWT",
    })
}

fn build_auth_headers(config: &Value, token: &AuthToken) -> Result<HeaderMap, String> {
    let mut headers = http_runtime::header_map(config)?;
    headers.insert(
        HeaderName::from_static("x-snowflake-authorization-token-type"),
        HeaderValue::from_static(if token.kind == "OAUTH" {
            "OAUTH"
        } else {
            "KEYPAIR_JWT"
        }),
    );
    headers.insert(
        HeaderName::from_static("accept"),
        HeaderValue::from_static("application/json"),
    );
    Ok(headers)
}

fn base_url(config: &Value) -> Result<Url, String> {
    let account = normalize_account(require(config, "account")?);
    let url = format!("https://{account}.snowflakecomputing.com");
    Url::parse(&url).map_err(|error| error.to_string())
}

fn normalize_account(account: &str) -> String {
    account.trim().trim_end_matches('.').to_string()
}

fn require<'a>(config: &'a Value, field: &str) -> Result<&'a str, String> {
    config
        .get(field)
        .and_then(Value::as_str)
        .filter(|value| !value.trim().is_empty())
        .ok_or_else(|| format!("snowflake connector requires '{field}'"))
}

fn build_query(config: &Value, selector: &str, limit: i64) -> Result<String, String> {
    let trimmed = selector.trim();
    if trimmed.to_ascii_lowercase().starts_with("select ") {
        return Ok(trimmed.to_string());
    }
    if !trimmed.is_empty() {
        let qualified = qualify_table(config, trimmed)?;
        return Ok(format!("SELECT * FROM {qualified} LIMIT {limit}"));
    }
    config
        .get("query")
        .and_then(Value::as_str)
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
        .ok_or_else(|| "snowflake sync requires a SQL query or table selector".to_string())
}

fn qualify_table(config: &Value, selector: &str) -> Result<String, String> {
    if selector.contains('.') {
        return Ok(selector.to_string());
    }
    let database = require(config, "database")?;
    let schema = require(config, "schema")?;
    Ok(format!("{database}.{schema}.{selector}"))
}

fn page_size(config: &Value) -> i64 {
    config
        .get("page_size")
        .and_then(Value::as_i64)
        .unwrap_or(DEFAULT_PAGE_SIZE)
        .clamp(1, 100_000)
}

fn extract_columns(meta: Option<&Value>) -> Vec<String> {
    meta.and_then(|meta| meta.get("rowType"))
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default()
        .into_iter()
        .filter_map(|field| field.get("name").and_then(Value::as_str).map(str::to_string))
        .collect()
}

fn extract_rows(data: Option<&Value>, columns: &[String]) -> Vec<Value> {
    data.and_then(Value::as_array)
        .cloned()
        .unwrap_or_default()
        .into_iter()
        .map(|row| {
            let cells = row.as_array().cloned().unwrap_or_default();
            let mut object = Map::with_capacity(columns.len());
            for (idx, column) in columns.iter().enumerate() {
                let cell = cells.get(idx).cloned().unwrap_or(Value::Null);
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
    fn requires_account_database_schema_and_credential() {
        assert!(validate_config(&json!({})).is_err());
        assert!(
            validate_config(&json!({
                "account": "a", "database": "d", "schema": "s",
                "oauth_token": "t"
            }))
            .is_ok()
        );
        // Missing credential
        assert!(
            validate_config(&json!({
                "account": "a", "database": "d", "schema": "s"
            }))
            .is_err()
        );
    }

    #[test]
    fn build_query_qualifies_table() {
        let config = json!({
            "account": "a", "database": "DB", "schema": "PUBLIC", "oauth_token": "t"
        });
        let q = build_query(&config, "ORDERS", 100).unwrap();
        assert_eq!(q, "SELECT * FROM DB.PUBLIC.ORDERS LIMIT 100");
    }

    #[test]
    fn build_query_passes_through_qualified_or_select() {
        let config = json!({
            "account": "a", "database": "DB", "schema": "PUBLIC", "oauth_token": "t"
        });
        assert_eq!(
            build_query(&config, "DB.PUBLIC.X", 10).unwrap(),
            "SELECT * FROM DB.PUBLIC.X LIMIT 10"
        );
        assert_eq!(
            build_query(&config, "select 1", 10).unwrap(),
            "select 1"
        );
    }

    #[test]
    fn extracts_rows_with_column_names() {
        let columns = vec!["A".to_string(), "B".to_string()];
        let body = json!({ "data": [["1", "x"], ["2", null]] });
        let rows = extract_rows(body.get("data"), &columns);
        assert_eq!(rows.len(), 2);
        assert_eq!(rows[0]["A"], json!("1"));
        assert_eq!(rows[1]["B"], Value::Null);
    }

    #[test]
    fn base_url_uses_account_subdomain() {
        let config = json!({
            "account": "xy12345.eu-central-1",
            "database": "DB", "schema": "PUBLIC", "oauth_token": "t"
        });
        let url = base_url(&config).unwrap();
        assert_eq!(url.as_str(), "https://xy12345.eu-central-1.snowflakecomputing.com/");
    }
}
