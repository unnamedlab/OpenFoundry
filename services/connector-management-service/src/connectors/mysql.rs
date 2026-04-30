//! MySQL connector — Foundry-aligned thin wrapper.
//!
//! Foundry's MySQL connector relies on JDBC/ODBC drivers running on a
//! Data Connection Agent. Mirroring that, the in-process connector accepts
//! either an inline catalog (for unit tests / fixtures) or a `base_url` plus
//! resource template that points to a remote agent endpoint.
//!
//! See: docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/
//! Connector type reference/Available connectors/MySQL*.md (covered under JDBC)

use serde_json::Value;

use super::{ConnectionTestResult, SyncPayload, catalog_bridge};
use crate::{
    AppState,
    models::registration::{DiscoveredSource, VirtualTableQueryRequest, VirtualTableQueryResponse},
};

const CONNECTOR_NAME: &str = "mysql";
const DEFAULT_SOURCE_KIND: &str = "mysql_table";

pub fn validate_config(config: &Value) -> Result<(), String> {
    // `host` is the canonical Foundry identity field for MySQL sources;
    // `jdbc_url` is also accepted for parity with the JDBC connector.
    catalog_bridge::validate_tabular_connector_config(config, CONNECTOR_NAME, &["host"])
}

pub async fn test_connection(
    state: &AppState,
    config: &Value,
    agent_url: Option<&str>,
) -> Result<ConnectionTestResult, String> {
    validate_config(config)?;
    catalog_bridge::test_tabular_connector_connection(
        state,
        config,
        agent_url,
        CONNECTOR_NAME,
        DEFAULT_SOURCE_KIND,
    )
    .await
}

pub async fn fetch_dataset(
    state: &AppState,
    config: &Value,
    selector: &str,
    agent_url: Option<&str>,
) -> Result<SyncPayload, String> {
    validate_config(config)?;
    catalog_bridge::fetch_tabular_dataset(
        state,
        config,
        selector,
        agent_url,
        CONNECTOR_NAME,
        DEFAULT_SOURCE_KIND,
    )
    .await
}

pub async fn discover_sources(
    state: &AppState,
    config: &Value,
    agent_url: Option<&str>,
) -> Result<Vec<DiscoveredSource>, String> {
    validate_config(config)?;
    catalog_bridge::discover_tabular_sources(
        state,
        config,
        agent_url,
        CONNECTOR_NAME,
        DEFAULT_SOURCE_KIND,
    )
    .await
}

pub async fn query_virtual_table(
    state: &AppState,
    config: &Value,
    request: &VirtualTableQueryRequest,
    agent_url: Option<&str>,
) -> Result<VirtualTableQueryResponse, String> {
    validate_config(config)?;
    catalog_bridge::query_tabular_virtual_table(
        state,
        config,
        request,
        agent_url,
        CONNECTOR_NAME,
        DEFAULT_SOURCE_KIND,
    )
    .await
}

#[cfg(test)]
mod tests {
    use serde_json::json;

    use super::validate_config;

    #[test]
    fn accepts_inline_table_catalog() {
        let config = json!({
            "host": "mysql.internal",
            "port": 3306,
            "database": "analytics",
            "user": "foundry_reader",
            "tables": [
                {
                    "table": "public.orders",
                    "sample_rows": [{ "order_id": "ord-1" }]
                }
            ]
        });
        assert!(validate_config(&config).is_ok());
    }

    #[test]
    fn rejects_empty_config() {
        assert!(validate_config(&json!({})).is_err());
    }
}
