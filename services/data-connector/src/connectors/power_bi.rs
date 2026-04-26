use serde_json::Value;

use super::{ConnectionTestResult, SyncPayload, catalog_bridge};
use crate::{
    AppState,
    models::registration::{DiscoveredSource, VirtualTableQueryRequest, VirtualTableQueryResponse},
};

const CONNECTOR_NAME: &str = "power_bi";
const DEFAULT_SOURCE_KIND: &str = "power_bi_dataset";

pub fn validate_config(config: &Value) -> Result<(), String> {
    catalog_bridge::validate_tabular_connector_config(config, CONNECTOR_NAME, &["workspace_id"])
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
    fn accepts_inline_dataset_catalogs() {
        let config = json!({
            "workspace_id": "workspace-01",
            "datasets": [
                {
                    "dataset": "ExecutiveMetrics",
                    "preview_rows": [{ "metric": "margin", "value": 0.31 }]
                }
            ]
        });

        assert!(validate_config(&config).is_ok());
    }
}
