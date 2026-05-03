//! Google Cloud Storage source — real `object_store::gcp` filesystem.
//!
//! Foundry exposes Google Cloud Storage as a real filesystem-backed source
//! (see `docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/
//! Connector type reference/Available connectors/Google Cloud Storage.md`).
//! This module mirrors that behaviour:
//!
//! * Discovery enumerates "tables" by listing object prefixes with the `/`
//!   delimiter (à la Foundry's bucket browser); inline `iceberg_tables[]` /
//!   `delta_tables[]` keep working for zero-copy registrations through
//!   [`open_table_catalog`].
//! * Fetching downloads the object pointed at by `selector`, sniffs its
//!   format from the file extension, and re-exports the bytes as a
//!   [`SyncPayload`] tagged with `parquet` / `csv` / `json`. Downstream
//!   materialisation through `storage-abstraction` already understands all
//!   three formats.
//!
//! ## Authentication
//!
//! Three modes are supported, matching the Foundry doc:
//!
//! | mode                            | config keys                                    |
//! |---------------------------------|------------------------------------------------|
//! | Static OAuth2 bearer token      | `access_token`                                 |
//! | Inline service-account JSON     | `service_account_json` (raw JSON object/string)|
//! | Application Default Credentials | none — falls back to `GOOGLE_APPLICATION_CREDENTIALS` env |
//!
//! Workload Identity Federation is honoured implicitly through ADC: when
//! the worker runs on a GKE/GCE node with a federated SA, the env-driven
//! ADC path picks it up.

use std::time::Instant;

use bytes::Bytes;
use futures::TryStreamExt;
use object_store::gcp::GoogleCloudStorageBuilder;
use object_store::{ObjectStore, path::Path as OsPath};
use serde_json::{Value, json};

use super::{ConnectionTestResult, SyncPayload, add_source_signature, open_table_catalog};
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
    // ADC (env-driven Workload Identity / GOOGLE_APPLICATION_CREDENTIALS)
    // is opt-in via `application_default: true`. Inline credentials are the
    // only path we permit by default — operators have to explicitly accept
    // env-provided credentials, mirroring the security stance of
    // `s3::validate_config` for instance metadata.
    let has_static = config.get("access_token").is_some()
        || config.get("service_account_json").is_some()
        || config
            .get("application_default")
            .and_then(Value::as_bool)
            .unwrap_or(false);
    if !has_static {
        return Err(
            "gcs source requires 'access_token', 'service_account_json' or 'application_default: true'"
                .into(),
        );
    }
    Ok(())
}

/// Build a [`GoogleCloudStorageBuilder`] from the connection config. The
/// returned store is ready to call `list_with_delimiter` / `get` against
/// the configured bucket.
fn build_store(config: &Value) -> Result<Box<dyn ObjectStore>, String> {
    let bucket = config
        .get("bucket")
        .and_then(Value::as_str)
        .ok_or_else(|| "gcs source missing 'bucket'".to_string())?;
    let mut builder = GoogleCloudStorageBuilder::new().with_bucket_name(bucket);

    if let Some(token) = config.get("access_token").and_then(Value::as_str) {
        builder = builder.with_service_account_key(token_to_sa_stub(token));
    } else if let Some(sa) = config.get("service_account_json") {
        let sa_string = match sa {
            Value::String(s) => s.clone(),
            other => other.to_string(),
        };
        builder = builder.with_service_account_key(sa_string);
    } else if config
        .get("application_default")
        .and_then(Value::as_bool)
        .unwrap_or(false)
    {
        // GoogleCloudStorageBuilder::new() already honours
        // GOOGLE_APPLICATION_CREDENTIALS and metadata-server tokens via
        // gcp_auth when no explicit key is provided. Nothing else to do.
    }

    builder
        .build()
        .map(|store| Box::new(store) as Box<dyn ObjectStore>)
        .map_err(|e| format!("gcs store build failed: {e}"))
}

/// `object_store` requires a service-account JSON for `with_service_account_key`.
/// When the operator only has a raw OAuth2 bearer token, wrap it in a
/// minimal SA-shaped JSON so the underlying `gcp_auth::CustomServiceAccount`
/// path accepts it as a static bearer credential.
fn token_to_sa_stub(token: &str) -> String {
    json!({
        "type": "authorized_user",
        "access_token": token,
        "client_id": "openfoundry-static-token",
    })
    .to_string()
}

/// Public so that handlers exercising `test_connection` can wire it in.
pub async fn test_connection(config: &Value) -> Result<ConnectionTestResult, String> {
    validate_config(config)?;
    let started = Instant::now();
    let store = build_store(config)?;
    let prefix = config
        .get("prefix")
        .or_else(|| config.get("subfolder"))
        .and_then(Value::as_str)
        .map(OsPath::from);
    // A delimited list with a tiny page proves we can both authenticate
    // and read at least one entry — the cheapest available "ping".
    let listing = store
        .list_with_delimiter(prefix.as_ref())
        .await
        .map_err(|e| format!("gcs list failed: {e}"))?;
    Ok(ConnectionTestResult {
        success: true,
        message: "Google Cloud Storage source reachable".into(),
        latency_ms: started.elapsed().as_millis(),
        details: Some(json!({
            "bucket": config.get("bucket").cloned().unwrap_or(Value::Null),
            "common_prefixes": listing.common_prefixes.len(),
            "objects": listing.objects.len(),
        })),
    })
}

pub async fn discover_sources(config: &Value) -> Result<Vec<DiscoveredSource>, String> {
    validate_config(config)?;

    // 1. Inline open-table catalog (zero-copy Iceberg/Delta).
    let mut sources = open_table_catalog::discover(config, STORE_PREFIX);

    // 2. Real bucket listing — every "directory" (`common_prefix`) becomes
    //    a virtual table, every leaf object becomes a sync source. The
    //    delimiter mode keeps us from pulling the whole bucket inventory
    //    on first-time discovery.
    let store = build_store(config)?;
    let prefix = config
        .get("prefix")
        .or_else(|| config.get("subfolder"))
        .and_then(Value::as_str)
        .map(OsPath::from);
    let listing = match store.list_with_delimiter(prefix.as_ref()).await {
        Ok(l) => l,
        Err(error) => {
            // If the operator only configured inline iceberg tables we are
            // happy to return those without the live listing.
            if !sources.is_empty() {
                tracing::warn!(%error, "gcs live listing failed; returning inline catalog only");
                return Ok(sources);
            }
            return Err(format!("gcs list_with_delimiter failed: {error}"));
        }
    };

    let bucket = config
        .get("bucket")
        .and_then(Value::as_str)
        .unwrap_or_default();
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
                "bucket": bucket,
                "uri": format!("gs://{bucket}/{selector}"),
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
                "bucket": bucket,
                "uri": format!("gs://{bucket}/{selector}"),
                "size": object.size,
                "format": format,
                "last_modified": object.last_modified.to_rfc3339(),
            }),
        });
    }
    if sources.is_empty() {
        return Err("gcs source did not expose any virtual tables, prefixes or objects".into());
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
        .map_err(|e| format!("gcs get '{selector}' failed: {e}"))?;
    let bytes: Bytes = stream
        .into_stream()
        .try_fold(Vec::<u8>::new(), |mut acc, chunk| async move {
            acc.extend_from_slice(&chunk);
            Ok(acc)
        })
        .await
        .map_err(|e| format!("gcs read '{selector}' failed: {e}"))?
        .into();

    let format = detect_format(selector);
    let rows_synced = match format.as_str() {
        // Light-weight row count for the formats whose dedicated
        // connectors already implement the same arithmetic.
        "csv" => count_csv_rows(&bytes).unwrap_or(-1),
        // Parquet/JSON row counts require schema-aware decoding which we
        // defer to materialisation; -1 = unknown.
        _ => -1,
    };

    let bucket = config
        .get("bucket")
        .and_then(Value::as_str)
        .unwrap_or_default();
    let mut payload = SyncPayload {
        bytes: bytes.to_vec(),
        format: format.clone(),
        rows_synced,
        file_name: file_name_from_selector(selector, &format),
        metadata: json!({
            "source": format!("gs://{bucket}/{selector}"),
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
        .unwrap_or_else(|| format!("gcs_object.{format}"))
}

fn count_csv_rows(bytes: &[u8]) -> Result<i64, String> {
    let mut reader = csv::Reader::from_reader(bytes);
    let mut total = 0_i64;
    for record in reader.records() {
        record.map_err(|error| error.to_string())?;
        total += 1;
    }
    Ok(total)
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
        assert!(validate_config(&json!({"bucket":"b","application_default":true})).is_ok());
        assert!(
            validate_config(
                &json!({"bucket":"b","service_account_json":{"type":"service_account"}})
            )
            .is_ok()
        );
    }

    #[test]
    fn detects_format_from_extension() {
        assert_eq!(detect_format("path/to/x.parquet"), "parquet");
        assert_eq!(detect_format("path/to/X.PARQUET"), "parquet");
        assert_eq!(detect_format("a/b.csv"), "csv");
        assert_eq!(detect_format("a/b.csv.gz"), "csv");
        assert_eq!(detect_format("a/b.ndjson"), "json");
        assert_eq!(detect_format("a/b"), "binary");
    }

    #[test]
    fn file_name_falls_back_when_selector_is_a_prefix() {
        assert_eq!(
            file_name_from_selector("a/b/c.parquet", "parquet"),
            "c.parquet"
        );
        assert_eq!(file_name_from_selector("", "csv"), "gcs_object.csv");
    }

    #[test]
    fn open_table_catalog_inline_still_works() {
        let cfg = json!({
            "bucket":"b","access_token":"t",
            "iceberg_tables":[
                {"selector":"db.t","metadata_location":"gs://b/x.json"}
            ]
        });
        // We can't call discover_sources because that hits the network;
        // verify the inline catalog helper is plumbed instead.
        let inline = open_table_catalog::discover(&cfg, STORE_PREFIX);
        assert_eq!(inline.len(), 1);
        assert_eq!(inline[0].source_kind, "gcs_iceberg_table");
    }
}
