//! Azure Blob / ADLS Gen2 / OneLake "open table" source.
//!
//! Foundry-aligned thin wrapper. The connector itself does not read object
//! payloads — that is delegated to the connector agent or to clients
//! consuming the Iceberg REST catalog (see
//! [`handlers::iceberg_catalog`](crate::handlers::iceberg_catalog)).
//!
//! Discovery turns inline `iceberg_tables[]` / `delta_tables[]` declared in
//! `connection.config` into [`DiscoveredSource`] entries with the upstream
//! `abfss://…/metadata.json` pointer attached. The catalog then forwards
//! that pointer verbatim to clients via `LoadTable`, fulfilling the
//! zero-copy promise.
//!
//! Credential vending (account SAS / service SAS) lives in
//! [`handlers::credentials_vending`](crate::handlers::credentials_vending).
//!
//! Required config keys:
//! * `account_name` (string)  — storage account
//! * one of `account_key` (base64), `sas_token` or `oauth_token`
//! Optional:
//! * `container_name` — narrows service-SAS scope
//! * `iceberg_tables[]`, `delta_tables[]` — see [`open_table_catalog`].

use serde_json::Value;

use super::open_table_catalog;
use crate::models::registration::DiscoveredSource;

const STORE_PREFIX: &str = "azure";

pub fn validate_config(config: &Value) -> Result<(), String> {
    if config
        .get("account_name")
        .and_then(Value::as_str)
        .map(str::is_empty)
        .unwrap_or(true)
    {
        return Err("azure_blob source requires 'account_name'".into());
    }
    let has_credential = config.get("account_key").is_some()
        || config.get("sas_token").is_some()
        || config.get("oauth_token").is_some();
    if !has_credential {
        return Err(
            "azure_blob source requires one of 'account_key', 'sas_token' or 'oauth_token'".into(),
        );
    }
    Ok(())
}

pub async fn discover_sources(config: &Value) -> Result<Vec<DiscoveredSource>, String> {
    validate_config(config)?;
    let sources = open_table_catalog::discover(config, STORE_PREFIX);
    if sources.is_empty() {
        return Err(
            "azure_blob source did not expose any virtual tables; declare 'iceberg_tables[]' or 'delta_tables[]'".into(),
        );
    }
    Ok(sources)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn rejects_missing_account_name() {
        assert!(validate_config(&json!({"account_key":"k"})).is_err());
    }

    #[test]
    fn rejects_missing_credential() {
        assert!(validate_config(&json!({"account_name":"a"})).is_err());
    }

    #[test]
    fn accepts_account_key() {
        assert!(validate_config(&json!({"account_name":"a","account_key":"k"})).is_ok());
    }

    #[tokio::test]
    async fn discovery_emits_azure_iceberg_sources() {
        let cfg = json!({
            "account_name":"a","account_key":"k",
            "iceberg_tables":[{"selector":"db.t","metadata_location":"abfss://w@a.dfs/x.json"}]
        });
        let out = discover_sources(&cfg).await.unwrap();
        assert_eq!(out.len(), 1);
        assert_eq!(out[0].source_kind, "azure_iceberg_table");
    }
}
