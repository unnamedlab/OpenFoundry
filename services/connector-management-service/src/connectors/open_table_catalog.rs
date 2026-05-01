//! Shared helper for "open-table" object-store sources (S3, ADLS, GCS).
//!
//! Foundry-aligned: any object-store backed source can carry inline
//! `iceberg_tables[]` and/or `delta_tables[]` arrays in its config to
//! advertise tables that already live in the lake. Discovery surfaces one
//! [`DiscoveredSource`] per entry with:
//!
//! * `source_kind`     = `<store>_<format>_table`  (e.g. `azure_iceberg_table`)
//! * `supports_sync`   = false (zero-copy only)
//! * `supports_zero_copy` = true
//! * `source_signature`= the entry's `snapshot_id` if present
//! * `metadata.upstream.metadata_location` = upstream pointer the Iceberg
//!   REST `LoadTable` handler forwards verbatim — fulfilling the zero-copy
//!   promise from
//!   `docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/Core concepts/Virtual tables.md`.
//!
//! Each source-specific connector wraps this helper with its own validator
//! and discovery dispatcher entry.

use std::collections::BTreeMap;

use serde_json::Value;

use crate::models::registration::DiscoveredSource;

/// Returns true when `config` declares at least one inline open-table
/// entry. Used by per-store `validate_config` helpers to skip the inline
/// HTTP-catalog requirement when the source operates exclusively as a
/// metadata pointer for Iceberg / Delta tables.
pub fn has_open_table_catalog(config: &Value) -> bool {
    matches!(config.get("iceberg_tables"), Some(Value::Array(a)) if !a.is_empty())
        || matches!(config.get("delta_tables"), Some(Value::Array(a)) if !a.is_empty())
}

/// Builds [`DiscoveredSource`] entries for every `iceberg_tables[]` and
/// `delta_tables[]` entry declared in `config`. `store_prefix` is prepended
/// to the `source_kind` so callers can distinguish e.g. `s3_iceberg_table`
/// from `azure_iceberg_table` downstream.
pub fn discover(config: &Value, store_prefix: &str) -> Vec<DiscoveredSource> {
    let mut sources = Vec::new();
    sources.extend(open_table_sources(
        config,
        "iceberg_tables",
        store_prefix,
        "iceberg",
    ));
    sources.extend(open_table_sources(
        config,
        "delta_tables",
        store_prefix,
        "delta",
    ));
    // De-dup by selector (last entry wins — keeps the most recently
    // declared metadata pointer).
    let mut seen: BTreeMap<String, DiscoveredSource> = BTreeMap::new();
    for source in sources {
        seen.insert(source.selector.clone(), source);
    }
    seen.into_values().collect()
}

fn open_table_sources(
    config: &Value,
    key: &str,
    store_prefix: &str,
    format: &str,
) -> Vec<DiscoveredSource> {
    let Some(items) = config.get(key).and_then(Value::as_array) else {
        return Vec::new();
    };
    items
        .iter()
        .filter_map(|item| {
            let selector = item.get("selector").and_then(Value::as_str)?.to_string();
            let metadata_location = item.get("metadata_location").and_then(Value::as_str);
            let table_location = item.get("table_location").and_then(Value::as_str);
            let display_name = item
                .get("display_name")
                .and_then(Value::as_str)
                .unwrap_or(&selector)
                .to_string();
            Some(DiscoveredSource {
                selector,
                display_name,
                source_kind: format!("{store_prefix}_{format}_table"),
                supports_sync: false,
                supports_zero_copy: true,
                source_signature: item.get("snapshot_id").and_then(|v| {
                    v.as_str()
                        .map(str::to_string)
                        .or(v.as_i64().map(|n| n.to_string()))
                }),
                metadata: serde_json::json!({
                    "format": format,
                    "upstream": {
                        "metadata_location": metadata_location,
                        "table_location": table_location,
                    },
                }),
            })
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn discover_emits_iceberg_and_delta_with_store_prefix() {
        let cfg = json!({
            "iceberg_tables": [
                {"selector":"db.t","metadata_location":"abfss://w@a.dfs/x.json","snapshot_id":"42"}
            ],
            "delta_tables": [
                {"selector":"db.d","metadata_location":"abfss://w@a.dfs/_delta_log/"}
            ]
        });
        let mut found = discover(&cfg, "azure");
        found.sort_by(|a, b| a.selector.cmp(&b.selector));
        assert_eq!(found.len(), 2);
        assert_eq!(found[0].selector, "db.d");
        assert_eq!(found[0].source_kind, "azure_delta_table");
        assert_eq!(found[1].selector, "db.t");
        assert_eq!(found[1].source_kind, "azure_iceberg_table");
        assert_eq!(found[1].source_signature.as_deref(), Some("42"));
        assert_eq!(
            found[1].metadata.pointer("/upstream/metadata_location"),
            Some(&json!("abfss://w@a.dfs/x.json"))
        );
    }

    #[test]
    fn no_open_tables_yields_empty_vec() {
        let cfg = json!({"bucket":"b"});
        assert!(discover(&cfg, "s3").is_empty());
        assert!(!has_open_table_catalog(&cfg));
    }
}
