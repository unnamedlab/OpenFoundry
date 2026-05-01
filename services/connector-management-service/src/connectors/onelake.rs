//! Microsoft OneLake source — ABFS-compatible wrapper around
//! [`object_store::azure`].
//!
//! OneLake exposes Fabric Lakehouses as ADLS Gen2 endpoints under
//! `https://onelake.dfs.fabric.microsoft.com/<workspace>/<lakehouse>.Lakehouse/...`,
//! which means the same `MicrosoftAzureBuilder` we already use for plain
//! Azure Blob can target it as long as we override the endpoint and use
//! the workspace name as the "account" component.
//!
//! Foundry models OneLake as a thin variant of the ABFS connector (see
//! `docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/
//! Connector type reference/Available connectors/OneLake and Azure Blob
//! Filesystem (ABFS).md`). The default container layout in a Fabric
//! lakehouse is `Files/` (raw files) and `Tables/` (Delta tables); we
//! default the container to `Files` and let operators override it.
//!
//! ## Required config
//!
//! * `workspace`  — Fabric workspace GUID or display name.
//! * `lakehouse`  — lakehouse name (without the `.Lakehouse` suffix).
//! * One auth method:
//!     - `oauth_token` (Entra ID bearer)
//!     - `tenant_id` + `client_id` + `client_secret` (service principal)
//!
//! ## Optional config
//!
//! * `namespace`  — `Files` (default) or `Tables`.
//! * `prefix`     — additional path narrowing inside the namespace.
//! * `iceberg_tables[]` / `delta_tables[]` — inline open-table catalog
//!   passthrough, identical contract to [`super::azure_blob`].

use std::time::Instant;

use bytes::Bytes;
use futures::TryStreamExt;
use object_store::{ObjectStore, path::Path as OsPath};
use object_store::azure::MicrosoftAzureBuilder;
use serde_json::{Value, json};

use super::{ConnectionTestResult, SyncPayload, add_source_signature, open_table_catalog};
use crate::models::registration::DiscoveredSource;

const STORE_PREFIX: &str = "onelake";
const ENDPOINT: &str = "https://onelake.dfs.fabric.microsoft.com";
const DEFAULT_NAMESPACE: &str = "Files";

pub fn validate_config(config: &Value) -> Result<(), String> {
    if config
        .get("workspace")
        .and_then(Value::as_str)
        .map(str::is_empty)
        .unwrap_or(true)
    {
        return Err("onelake source requires 'workspace'".into());
    }
    if config
        .get("lakehouse")
        .and_then(Value::as_str)
        .map(str::is_empty)
        .unwrap_or(true)
    {
        return Err("onelake source requires 'lakehouse'".into());
    }
    let has_token = config.get("oauth_token").is_some();
    let has_sp = config.get("tenant_id").is_some()
        && config.get("client_id").is_some()
        && config.get("client_secret").is_some();
    if !has_token && !has_sp {
        return Err(
            "onelake source requires 'oauth_token' or ('tenant_id'+'client_id'+'client_secret')"
                .into(),
        );
    }
    Ok(())
}

fn namespace(config: &Value) -> String {
    config
        .get("namespace")
        .and_then(Value::as_str)
        .filter(|s| !s.is_empty())
        .unwrap_or(DEFAULT_NAMESPACE)
        .to_string()
}

fn lakehouse_container(config: &Value) -> Result<String, String> {
    let lakehouse = config
        .get("lakehouse")
        .and_then(Value::as_str)
        .ok_or_else(|| "onelake source missing 'lakehouse'".to_string())?;
    // Fabric exposes the lakehouse as the "container" in ABFS terms, with
    // the `.Lakehouse` suffix appended.
    Ok(format!("{lakehouse}.Lakehouse"))
}

fn build_store(config: &Value) -> Result<Box<dyn ObjectStore>, String> {
    let workspace = config
        .get("workspace")
        .and_then(Value::as_str)
        .ok_or_else(|| "onelake source missing 'workspace'".to_string())?;
    let container = lakehouse_container(config)?;
    let mut builder = MicrosoftAzureBuilder::new()
        .with_endpoint(ENDPOINT.to_string())
        .with_account(workspace)
        .with_container_name(container);

    if let Some(token) = config.get("oauth_token").and_then(Value::as_str) {
        builder = builder.with_bearer_token_authorization(token);
    } else if let (Some(tenant), Some(client_id), Some(client_secret)) = (
        config.get("tenant_id").and_then(Value::as_str),
        config.get("client_id").and_then(Value::as_str),
        config.get("client_secret").and_then(Value::as_str),
    ) {
        builder = builder
            .with_tenant_id(tenant)
            .with_client_id(client_id)
            .with_client_secret(client_secret);
    }

    builder
        .build()
        .map(|store| Box::new(store) as Box<dyn ObjectStore>)
        .map_err(|e| format!("onelake store build failed: {e}"))
}

fn list_prefix(config: &Value) -> OsPath {
    let ns = namespace(config);
    let extra = config
        .get("prefix")
        .and_then(Value::as_str)
        .filter(|s| !s.is_empty());
    match extra {
        Some(p) => OsPath::from(format!("{ns}/{}", p.trim_start_matches('/'))),
        None => OsPath::from(ns),
    }
}

pub async fn test_connection(config: &Value) -> Result<ConnectionTestResult, String> {
    validate_config(config)?;
    let started = Instant::now();
    let store = build_store(config)?;
    let prefix = list_prefix(config);
    let listing = store
        .list_with_delimiter(Some(&prefix))
        .await
        .map_err(|e| format!("onelake list failed: {e}"))?;
    Ok(ConnectionTestResult {
        success: true,
        message: "OneLake source reachable".into(),
        latency_ms: started.elapsed().as_millis(),
        details: Some(json!({
            "workspace": config.get("workspace").cloned().unwrap_or(Value::Null),
            "lakehouse": config.get("lakehouse").cloned().unwrap_or(Value::Null),
            "namespace": namespace(config),
            "common_prefixes": listing.common_prefixes.len(),
            "objects": listing.objects.len(),
        })),
    })
}

pub async fn discover_sources(config: &Value) -> Result<Vec<DiscoveredSource>, String> {
    validate_config(config)?;
    let mut sources = open_table_catalog::discover(config, STORE_PREFIX);

    let store = build_store(config)?;
    let prefix = list_prefix(config);
    let listing = match store.list_with_delimiter(Some(&prefix)).await {
        Ok(l) => l,
        Err(error) => {
            if !sources.is_empty() {
                tracing::warn!(%error, "onelake live listing failed; returning inline catalog only");
                return Ok(sources);
            }
            return Err(format!("onelake list_with_delimiter failed: {error}"));
        }
    };

    let workspace = config.get("workspace").and_then(Value::as_str).unwrap_or_default();
    let lakehouse = config.get("lakehouse").and_then(Value::as_str).unwrap_or_default();
    let abfss_root = format!(
        "abfss://{workspace}@onelake.dfs.fabric.microsoft.com/{lakehouse}.Lakehouse"
    );
    for prefix in listing.common_prefixes {
        let selector = prefix.to_string();
        sources.push(DiscoveredSource {
            display_name: selector.clone(),
            selector: selector.clone(),
            source_kind: format!("{STORE_PREFIX}_prefix"),
            supports_sync: true,
            supports_zero_copy: false,
            source_signature: None,
            metadata: json!({
                "uri": format!("{abfss_root}/{selector}"),
                "kind": "prefix",
            }),
        });
    }
    for object in listing.objects {
        let selector = object.location.to_string();
        let format = detect_format(&selector);
        sources.push(DiscoveredSource {
            display_name: selector.clone(),
            selector: selector.clone(),
            source_kind: format!("{STORE_PREFIX}_object"),
            supports_sync: true,
            supports_zero_copy: matches!(format.as_str(), "csv" | "json"),
            source_signature: object.e_tag.clone(),
            metadata: json!({
                "uri": format!("{abfss_root}/{selector}"),
                "size": object.size,
                "format": format,
                "last_modified": object.last_modified.to_rfc3339(),
            }),
        });
    }
    if sources.is_empty() {
        return Err("onelake source did not expose any virtual tables, prefixes or objects".into());
    }
    Ok(sources)
}

pub async fn fetch_dataset(config: &Value, selector: &str) -> Result<SyncPayload, String> {
    validate_config(config)?;
    let store = build_store(config)?;
    let path = OsPath::from(selector);
    let stream = store
        .get(&path)
        .await
        .map_err(|e| format!("onelake get '{selector}' failed: {e}"))?;
    let bytes: Bytes = stream
        .into_stream()
        .try_fold(Vec::<u8>::new(), |mut acc, chunk| async move {
            acc.extend_from_slice(&chunk);
            Ok(acc)
        })
        .await
        .map_err(|e| format!("onelake read '{selector}' failed: {e}"))?
        .into();

    let format = detect_format(selector);
    let workspace = config.get("workspace").and_then(Value::as_str).unwrap_or_default();
    let lakehouse = config.get("lakehouse").and_then(Value::as_str).unwrap_or_default();
    let mut payload = SyncPayload {
        bytes: bytes.to_vec(),
        format: format.clone(),
        rows_synced: -1,
        file_name: file_name_from_selector(selector, &format),
        metadata: json!({
            "source": format!(
                "abfss://{workspace}@onelake.dfs.fabric.microsoft.com/{lakehouse}.Lakehouse/{selector}"
            ),
            "format": format,
            "bytes": bytes.len(),
        }),
    };
    add_source_signature(&mut payload);
    Ok(payload)
}

fn detect_format(selector: &str) -> String {
    let lowered = selector.to_ascii_lowercase();
    if lowered.ends_with(".parquet") {
        "parquet".into()
    } else if lowered.ends_with(".csv") || lowered.ends_with(".csv.gz") {
        "csv".into()
    } else if lowered.ends_with(".json")
        || lowered.ends_with(".ndjson")
        || lowered.ends_with(".jsonl")
    {
        "json".into()
    } else {
        "binary".into()
    }
}

fn file_name_from_selector(selector: &str, format: &str) -> String {
    selector
        .rsplit('/')
        .next()
        .filter(|s| !s.is_empty())
        .map(|s| s.to_string())
        .unwrap_or_else(|| format!("onelake_object.{format}"))
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn requires_workspace_lakehouse_and_credential() {
        assert!(validate_config(&json!({})).is_err());
        assert!(validate_config(&json!({"workspace":"w"})).is_err());
        assert!(validate_config(&json!({"workspace":"w","lakehouse":"l"})).is_err());
        assert!(
            validate_config(&json!({"workspace":"w","lakehouse":"l","oauth_token":"t"})).is_ok()
        );
        assert!(
            validate_config(&json!({
                "workspace":"w","lakehouse":"l",
                "tenant_id":"t","client_id":"c","client_secret":"s"
            }))
            .is_ok()
        );
    }

    #[test]
    fn namespace_defaults_to_files() {
        assert_eq!(namespace(&json!({})), "Files");
        assert_eq!(namespace(&json!({"namespace":"Tables"})), "Tables");
    }

    #[test]
    fn lakehouse_container_appends_suffix() {
        let cfg = json!({"workspace":"w","lakehouse":"my_lh"});
        assert_eq!(lakehouse_container(&cfg).unwrap(), "my_lh.Lakehouse");
    }

    #[test]
    fn list_prefix_concatenates_namespace_and_prefix() {
        let cfg = json!({"namespace":"Tables","prefix":"sales"});
        assert_eq!(list_prefix(&cfg).to_string(), "Tables/sales");
        let cfg = json!({});
        assert_eq!(list_prefix(&cfg).to_string(), "Files");
    }

    #[test]
    fn detects_format_from_extension() {
        assert_eq!(detect_format("Tables/x.parquet"), "parquet");
        assert_eq!(detect_format("Files/y.csv.gz"), "csv");
        assert_eq!(detect_format("Files/z.jsonl"), "json");
        assert_eq!(detect_format("Files/blob"), "binary");
    }
}
