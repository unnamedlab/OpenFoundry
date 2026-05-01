//! Google Cloud Storage "open table" source. Same shape as
//! [`super::azure_blob`]; see that module for design notes.
//!
//! Required config keys:
//! * `bucket` (string)
//! * one of `access_token` (OAuth2 bearer) or `service_account_json`

use serde_json::Value;

use super::open_table_catalog;
use crate::models::registration::DiscoveredSource;

const STORE_PREFIX: &str = "gcs";

pub fn validate_config(config: &Value) -> Result<(), String> {
    if config
        .get("bucket")
        .and_then(Value::as_str)
        .map(str::is_empty)
        .unwrap_or(true)
    {
        return Err("gcs source requires 'bucket'".into());
    }
    if config.get("access_token").is_none() && config.get("service_account_json").is_none() {
        return Err("gcs source requires 'access_token' or 'service_account_json'".into());
    }
    Ok(())
}

pub async fn discover_sources(config: &Value) -> Result<Vec<DiscoveredSource>, String> {
    validate_config(config)?;
    let sources = open_table_catalog::discover(config, STORE_PREFIX);
    if sources.is_empty() {
        return Err(
            "gcs source did not expose any virtual tables; declare 'iceberg_tables[]' or 'delta_tables[]'".into(),
        );
    }
    Ok(sources)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn requires_bucket_and_credential() {
        assert!(validate_config(&json!({})).is_err());
        assert!(validate_config(&json!({"bucket":"b"})).is_err());
        assert!(validate_config(&json!({"bucket":"b","access_token":"t"})).is_ok());
    }
}
