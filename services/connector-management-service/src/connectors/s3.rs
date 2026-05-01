//! Amazon S3 connector — Foundry-aligned thin wrapper.
//!
//! Foundry's S3 source accepts:
//! - `url`         (s3://bucket/prefix/, with trailing slash)
//! - `endpoint`    (e.g. s3.us-east-1.amazonaws.com)
//! - `region`      (optional, required when assuming STS roles)
//! - `access_key_id` / `secret_access_key`  (option 1 credentials)
//! - `path_style`  (optional bool, virtual-hosted vs path-style URLs)
//! - `subfolder`   (optional, narrows the listing)
//!
//! Reading parquet/csv/json objects is delegated to the connector agent
//! (or to a future native client). Discovery, sync and virtual-table
//! reads route through `catalog_bridge` so credentials and egress policies
//! are honoured uniformly with the rest of the connector fleet.
//!
//! See: docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/
//! Connector type reference/Available connectors/Amazon S3.md

use serde_json::Value;

use super::{ConnectionTestResult, SyncPayload, catalog_bridge, open_table_catalog};
use crate::{
    AppState,
    models::registration::{DiscoveredSource, VirtualTableQueryRequest, VirtualTableQueryResponse},
};

const CONNECTOR_NAME: &str = "s3";
const DEFAULT_SOURCE_KIND: &str = "s3_object";

pub fn validate_config(config: &Value) -> Result<(), String> {
    // `url` is the canonical Foundry identity field for S3 sources;
    // `bucket` is also accepted to make local development friendlier.
    let identity = if config.get("url").is_some() {
        "url"
    } else {
        "bucket"
    };
    // Foundry-style "open table" configurations expose Iceberg/Delta tables
    // sitting on top of S3 directly via metadata pointers, without requiring
    // a tabular HTTP catalog. Accept those when present and skip the
    // inline-catalog requirement so the source can be created with only
    // `iceberg_tables[]` / `delta_tables[]` entries.
    if open_table_catalog::has_open_table_catalog(config) {
        if config.get(identity).is_none() {
            return Err(format!(
                "s3 connector with iceberg_tables/delta_tables requires '{identity}'"
            ));
        }
        return Ok(());
    }
    catalog_bridge::validate_tabular_connector_config(config, CONNECTOR_NAME, &[identity])
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
    let mut sources = catalog_bridge::discover_tabular_sources(
        state,
        config,
        agent_url,
        CONNECTOR_NAME,
        DEFAULT_SOURCE_KIND,
    )
    .await
    .unwrap_or_default();

    // Foundry-aligned: when the source config declares Iceberg/Delta tables
    // inline (for example because the Data Connection agent listed the
    // bucket and surfaced `iceberg_tables[]` / `delta_tables[]`), surface
    // them as DiscoveredSource entries with the upstream metadata pointer
    // attached. Bulk-register persists the pointer under
    // `connection_registrations.metadata.upstream.metadata_location`, which
    // the Iceberg REST catalog then forwards verbatim to clients via
    // LoadTable, fulfilling the zero-copy promise documented in
    // foundry-docs/Data connectivity & integration/Core concepts/Virtual tables.md.
    sources.extend(open_table_catalog::discover(config, "s3"));
    if sources.is_empty() {
        return Err("S3 source did not expose any virtual tables".to_string());
    }
    // De-dup by selector — last entry wins (keeps the pointer-bearing one).
    let mut seen = std::collections::BTreeMap::new();
    for source in sources {
        seen.insert(source.selector.clone(), source);
    }
    Ok(seen.into_values().collect())
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
    fn accepts_inline_object_catalog() {
        let config = json!({
            "url": "s3://my-bucket/prefix/",
            "endpoint": "s3.us-east-1.amazonaws.com",
            "region": "us-east-1",
            "datasets": [
                {
                    "dataset": "raw/orders.parquet",
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
