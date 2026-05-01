//! Generic / custom open-table connector.
//!
//! Foundry-aligned: this is the "external Iceberg/Delta" generic source
//! that lets a customer wire any object store / REST catalog without us
//! shipping a bespoke connector. It is the SDK entry point referenced by
//! [Generic / custom connectors](../../../docs_original_palantir_foundry/foundry-docs/Data%20connectivity%20%26%20integration/Connectivity/Data%20Connection/External%20connections%20from%20code/External%20transforms.md):
//!
//! ## SDK contract
//!
//! Provide `connector_type: "generic"` plus a `config` block of the shape:
//!
//! ```jsonc
//! {
//!   // Optional human label propagated to the gallery / lineage view.
//!   "label": "ACME Iceberg lake",
//!   // OPTIONAL: Iceberg REST catalog the platform will forward to clients.
//!   "catalog_url": "https://catalog.example.com/iceberg/v1",
//!   // Inline tables — same schema accepted by s3/azure_blob/gcs.
//!   "iceberg_tables": [
//!     {"selector":"sales.orders","metadata_location":"s3://acme/orders/metadata/00042.json","snapshot_id":"42"}
//!   ],
//!   "delta_tables": [
//!     {"selector":"sales.events","table_location":"s3://acme/events/"}
//!   ]
//! }
//! ```
//!
//! Either `iceberg_tables[]`, `delta_tables[]` or `catalog_url` must be set.
//! The dispatcher returns one [`DiscoveredSource`] per inline entry and
//! tags them with `source_kind = "generic_iceberg_table"` /
//! `"generic_delta_table"` so the LoadTable handler forwards the upstream
//! pointer verbatim — same zero-copy contract as the dedicated stores.

use serde_json::Value;

use super::open_table_catalog;
use crate::models::registration::DiscoveredSource;

const STORE_PREFIX: &str = "generic";

pub fn validate_config(config: &Value) -> Result<(), String> {
    if open_table_catalog::has_open_table_catalog(config) {
        return Ok(());
    }
    if config
        .get("catalog_url")
        .and_then(Value::as_str)
        .is_some_and(|u| !u.is_empty())
    {
        return Ok(());
    }
    Err(
        "generic connector requires 'iceberg_tables[]', 'delta_tables[]' or 'catalog_url'".into(),
    )
}

pub async fn discover_sources(config: &Value) -> Result<Vec<DiscoveredSource>, String> {
    validate_config(config)?;
    let sources = open_table_catalog::discover(config, STORE_PREFIX);
    if sources.is_empty() {
        // catalog_url-only path: defer discovery to the client (LoadTable
        // proxies catalog_url verbatim via the `pushdown` config block).
        return Ok(Vec::new());
    }
    Ok(sources)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn requires_inline_tables_or_catalog_url() {
        assert!(validate_config(&json!({})).is_err());
        assert!(validate_config(&json!({"catalog_url":"https://x"})).is_ok());
        assert!(
            validate_config(&json!({"iceberg_tables":[{"selector":"a","metadata_location":"s3://x"}]}))
                .is_ok()
        );
    }

    #[tokio::test]
    async fn discovery_emits_generic_kind() {
        let cfg = json!({
            "iceberg_tables":[{"selector":"a.b","metadata_location":"s3://x/m.json"}]
        });
        let out = discover_sources(&cfg).await.unwrap();
        assert_eq!(out[0].source_kind, "generic_iceberg_table");
    }
}
