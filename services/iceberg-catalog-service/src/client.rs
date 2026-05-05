//! Reusable Iceberg REST Catalog HTTP client.
//!
//! `pipeline-build-service` consumes this module to plug a
//! [`FoundryIcebergTxn`] into its build executor: every job opens
//! one transaction and forwards reads/writes through the wrapper. The
//! client is exposed as a library API (rather than just internal to
//! the catalog binary) precisely so build executor code doesn't need
//! to re-implement the JSON shapes.

use std::sync::Arc;

use serde::{Deserialize, Serialize};
use serde_json::Value;

use crate::domain::foundry_transaction::{
    CatalogClient, CommitOutcome, ConflictKind, DataFileRef, FoundryIcebergTxn, PendingOp,
    SnapshotView, TableChange, TableCommitOutcome, TableRid, TxnError,
};

const DEFAULT_TIMEOUT_SECS: u64 = 10;

/// HTTP implementation of [`CatalogClient`]. Wraps a `reqwest::Client`
/// pre-configured with the iceberg catalog base URL and a bearer
/// token (typically minted via the `oauth/tokens` endpoint or as a
/// long-lived `ofty_*` token).
#[derive(Clone)]
pub struct HttpCatalogClient {
    base_url: String,
    bearer: String,
    http: reqwest::Client,
}

impl HttpCatalogClient {
    pub fn new(base_url: impl Into<String>, bearer: impl Into<String>) -> Self {
        let http = reqwest::Client::builder()
            .timeout(std::time::Duration::from_secs(DEFAULT_TIMEOUT_SECS))
            .build()
            .unwrap_or_else(|_| reqwest::Client::new());
        Self {
            base_url: base_url.into().trim_end_matches('/').to_string(),
            bearer: bearer.into(),
            http,
        }
    }

    pub fn with_client(mut self, client: reqwest::Client) -> Self {
        self.http = client;
        self
    }

    pub fn open_transaction(self, build_rid: impl Into<String>) -> FoundryIcebergTxn {
        FoundryIcebergTxn::begin(build_rid, Arc::new(self))
    }

    fn auth_header(&self) -> (&'static str, String) {
        ("authorization", format!("Bearer {}", self.bearer))
    }
}

#[derive(Debug, Deserialize)]
struct LoadTableHttpResponse {
    metadata: Value,
}

#[derive(Debug, Deserialize)]
struct ConflictBody {
    error: ConflictBodyInner,
}

#[derive(Debug, Deserialize)]
struct ConflictBodyInner {
    #[serde(default)]
    table_rid: Option<String>,
    #[serde(default)]
    conflicting_with: Option<String>,
    #[serde(default)]
    message: Option<String>,
}

#[derive(Debug, Serialize)]
struct WireTableChangeIdentifier {
    namespace: Vec<String>,
    name: String,
}

#[derive(Debug, Serialize)]
struct WireTableChange {
    identifier: WireTableChangeIdentifier,
    requirements: Vec<Value>,
    updates: Vec<Value>,
}

#[derive(Debug, Serialize)]
struct WireMultiCommitBody {
    #[serde(rename = "table-changes")]
    table_changes: Vec<WireTableChange>,
    build_rid: String,
}

#[derive(Debug, Deserialize)]
struct WireMultiCommitResponse {
    committed: Vec<WireCommittedTable>,
}

#[derive(Debug, Deserialize)]
struct WireCommittedTable {
    table_rid: String,
    new_snapshot_id: Option<i64>,
    #[serde(rename = "metadata-location")]
    metadata_location: String,
}

/// Map a [`TableRid`] of the form `ri.foundry.main.iceberg-table.<id>`
/// to its `(namespace, name)` pair. Handlers need both shapes — one
/// for the URL path, the other for the JSON identifier.
///
/// Per ADR — the catalog stores `(namespace, name)` so the resolution
/// goes through the catalog admin API. We use the bearer token to
/// look it up over the same HTTP client so this client is fully
/// decoupled from the postgres model.
async fn resolve_table_identifier(
    http: &reqwest::Client,
    base_url: &str,
    bearer: &str,
    table_rid: &str,
) -> Result<(Vec<String>, String), TxnError> {
    let id = table_rid
        .strip_prefix("ri.foundry.main.iceberg-table.")
        .ok_or_else(|| TxnError::Other(format!("malformed table rid: {table_rid}")))?;
    let url = format!("{base_url}/api/v1/iceberg-tables/{id}");
    let response = http
        .get(&url)
        .header("authorization", format!("Bearer {bearer}"))
        .send()
        .await
        .map_err(|err| TxnError::Catalog(format!("resolve {table_rid}: {err}")))?;
    if !response.status().is_success() {
        return Err(TxnError::Catalog(format!(
            "resolve {table_rid} returned {}",
            response.status()
        )));
    }
    #[derive(Deserialize)]
    struct DetailResponse {
        summary: DetailSummary,
    }
    #[derive(Deserialize)]
    struct DetailSummary {
        namespace: Vec<String>,
        name: String,
    }
    let body: DetailResponse = response
        .json()
        .await
        .map_err(|err| TxnError::Catalog(format!("decode {table_rid}: {err}")))?;
    Ok((body.summary.namespace, body.summary.name))
}

#[async_trait::async_trait]
impl CatalogClient for HttpCatalogClient {
    async fn snapshot(&self, table_rid: &TableRid) -> Result<SnapshotView, TxnError> {
        let (namespace, name) =
            resolve_table_identifier(&self.http, &self.base_url, &self.bearer, table_rid).await?;

        let url = format!(
            "{}/iceberg/v1/namespaces/{}/tables/{}",
            self.base_url,
            namespace.join("."),
            name
        );
        let (header, value) = self.auth_header();
        let response = self
            .http
            .get(&url)
            .header(header, value)
            .send()
            .await
            .map_err(|err| TxnError::Catalog(format!("load {table_rid}: {err}")))?;
        if !response.status().is_success() {
            return Err(TxnError::Catalog(format!(
                "load {table_rid} returned {}",
                response.status()
            )));
        }
        let body: LoadTableHttpResponse = response
            .json()
            .await
            .map_err(|err| TxnError::Catalog(format!("decode {table_rid}: {err}")))?;

        let snapshot_id = body
            .metadata
            .get("current-snapshot-id")
            .and_then(Value::as_i64)
            .filter(|id| *id >= 0)
            .unwrap_or(0);
        let table_was_empty = snapshot_id == 0;
        let data_file_count = body
            .metadata
            .get("snapshots")
            .and_then(Value::as_array)
            .map(|a| a.len() as u64)
            .unwrap_or(0);
        let schema_id = body
            .metadata
            .get("current-schema-id")
            .and_then(Value::as_i64)
            .unwrap_or(0) as i32;

        Ok(SnapshotView {
            table_rid: table_rid.clone(),
            snapshot_id,
            table_was_empty,
            data_file_count,
            schema_id,
        })
    }

    async fn commit_batch(
        &self,
        build_rid: &str,
        changes: Vec<TableChange>,
    ) -> Result<CommitOutcome, TxnError> {
        let mut wire_changes = Vec::with_capacity(changes.len());
        for change in changes {
            let (namespace, name) = resolve_table_identifier(
                &self.http,
                &self.base_url,
                &self.bearer,
                &change.table_rid,
            )
            .await?;
            let (requirements, updates) = pending_to_updates(
                change.captured_snapshot_id,
                &change.ops,
            );
            wire_changes.push(WireTableChange {
                identifier: WireTableChangeIdentifier { namespace, name },
                requirements,
                updates,
            });
        }
        let body = WireMultiCommitBody {
            table_changes: wire_changes,
            build_rid: build_rid.to_string(),
        };

        let url = format!("{}/iceberg/v1/transactions/commit", self.base_url);
        let (header, value) = self.auth_header();
        let response = self
            .http
            .post(&url)
            .header(header, value)
            .json(&body)
            .send()
            .await
            .map_err(|err| TxnError::Catalog(format!("commit_batch: {err}")))?;

        let status = response.status();
        if status == reqwest::StatusCode::CONFLICT {
            // 409 → Retryable. Try to surface the conflicting source
            // so the build executor's metric label matches.
            let body: ConflictBody = response.json().await.unwrap_or(ConflictBody {
                error: ConflictBodyInner {
                    table_rid: None,
                    conflicting_with: None,
                    message: None,
                },
            });
            let kind = match body.error.conflicting_with.as_deref() {
                Some("compaction") => ConflictKind::Compaction,
                Some("maintenance") => ConflictKind::Maintenance,
                Some("user_job") => ConflictKind::UserJob,
                _ => ConflictKind::Unknown,
            };
            return Err(TxnError::Retryable {
                table_rid: body.error.table_rid.unwrap_or_default(),
                reason: body.error.message.unwrap_or_else(|| "conflict".to_string()),
                conflicting_with: kind,
            });
        }
        if !status.is_success() {
            let text = response.text().await.unwrap_or_default();
            return Err(TxnError::Catalog(format!(
                "commit_batch returned {status}: {text}"
            )));
        }
        let parsed: WireMultiCommitResponse = response
            .json()
            .await
            .map_err(|err| TxnError::Catalog(format!("commit_batch decode: {err}")))?;
        Ok(CommitOutcome {
            table_changes: parsed
                .committed
                .into_iter()
                .map(|c| TableCommitOutcome {
                    table_rid: c.table_rid,
                    new_snapshot_id: c.new_snapshot_id.unwrap_or(0),
                    metadata_location: c.metadata_location,
                })
                .collect(),
        })
    }
}

/// Translate the wrapper's [`PendingOp`] vocabulary into the spec's
/// `requirements`/`updates` shape that the multi-table commit handler
/// consumes. The captured snapshot id always becomes an
/// `assert-ref-snapshot-id` requirement so optimistic concurrency
/// checks fire on the server side.
fn pending_to_updates(
    captured_snapshot_id: i64,
    ops: &[PendingOp],
) -> (Vec<Value>, Vec<Value>) {
    let mut requirements = vec![serde_json::json!({
        "type": "assert-ref-snapshot-id",
        "ref": "main",
        "snapshot-id": if captured_snapshot_id == 0 { Value::Null } else { Value::Number(captured_snapshot_id.into()) }
    })];
    let mut updates: Vec<Value> = Vec::new();

    for op in ops {
        match op {
            PendingOp::AppendFiles { data_files } => {
                let snapshot_id = next_snapshot_id();
                updates.push(serde_json::json!({
                    "action": "add-snapshot",
                    "snapshot": build_snapshot_value(
                        snapshot_id,
                        if captured_snapshot_id > 0 { Some(captured_snapshot_id) } else { None },
                        "append",
                        data_files,
                        &[],
                    ),
                }));
            }
            PendingOp::Overwrite { added, removed } => {
                let snapshot_id = next_snapshot_id();
                updates.push(serde_json::json!({
                    "action": "add-snapshot",
                    "snapshot": build_snapshot_value(
                        snapshot_id,
                        Some(captured_snapshot_id),
                        "overwrite",
                        added,
                        removed,
                    ),
                }));
            }
            PendingOp::Delete { delete_files } => {
                let snapshot_id = next_snapshot_id();
                updates.push(serde_json::json!({
                    "action": "add-snapshot",
                    "snapshot": build_snapshot_value(
                        snapshot_id,
                        Some(captured_snapshot_id),
                        "delete",
                        &[],
                        delete_files,
                    ),
                }));
            }
            PendingOp::AlterSchema { updates: schema_updates } => {
                requirements.push(serde_json::json!({
                    "type": "assert-current-schema-id",
                }));
                for u in schema_updates {
                    updates.push(u.clone());
                }
            }
        }
    }

    (requirements, updates)
}

fn next_snapshot_id() -> i64 {
    use std::sync::atomic::{AtomicI64, Ordering};
    static SEED: AtomicI64 = AtomicI64::new(1);
    let now_ms = chrono::Utc::now().timestamp_millis();
    // Mix monotonic counter so two ops scheduled in the same tx don't
    // collide on `INSERT … ON CONFLICT (table_id, snapshot_id)`.
    now_ms.saturating_mul(1000) + SEED.fetch_add(1, Ordering::Relaxed)
}

fn build_snapshot_value(
    snapshot_id: i64,
    parent: Option<i64>,
    operation: &str,
    added: &[DataFileRef],
    removed: &[DataFileRef],
) -> Value {
    let added_records: u64 = added.iter().map(|f| f.record_count).sum();
    let added_bytes: u64 = added.iter().map(|f| f.file_size_bytes).sum();
    let removed_records: u64 = removed.iter().map(|f| f.record_count).sum();
    serde_json::json!({
        "snapshot-id": snapshot_id,
        "parent-snapshot-id": parent,
        "sequence-number": snapshot_id,
        "manifest-list": format!("metadata/snap-{snapshot_id}-manifest-list.avro"),
        "summary": {
            "operation": operation,
            "added-data-files": added.len().to_string(),
            "deleted-data-files": removed.len().to_string(),
            "added-records": added_records.to_string(),
            "deleted-records": removed_records.to_string(),
            "added-files-size": added_bytes.to_string(),
        },
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn append_translates_to_add_snapshot_with_assert_ref() {
        let (requirements, updates) = pending_to_updates(
            42,
            &[PendingOp::AppendFiles {
                data_files: vec![DataFileRef {
                    path: "s3://x".to_string(),
                    record_count: 5,
                    file_size_bytes: 100,
                }],
            }],
        );
        assert_eq!(requirements.len(), 1);
        assert_eq!(requirements[0]["type"], "assert-ref-snapshot-id");
        assert_eq!(requirements[0]["snapshot-id"], 42);
        assert_eq!(updates.len(), 1);
        assert_eq!(updates[0]["action"], "add-snapshot");
        assert_eq!(updates[0]["snapshot"]["summary"]["operation"], "append");
        assert_eq!(updates[0]["snapshot"]["summary"]["added-records"], "5");
    }

    #[test]
    fn empty_table_uses_null_snapshot_id_in_assertion() {
        let (requirements, _) = pending_to_updates(
            0,
            &[PendingOp::AppendFiles { data_files: vec![] }],
        );
        assert!(requirements[0]["snapshot-id"].is_null());
    }

    #[test]
    fn overwrite_carries_removed_count_in_summary() {
        let (_, updates) = pending_to_updates(
            42,
            &[PendingOp::Overwrite {
                added: vec![DataFileRef {
                    path: "a".to_string(),
                    record_count: 1,
                    file_size_bytes: 1,
                }],
                removed: vec![DataFileRef {
                    path: "b".to_string(),
                    record_count: 3,
                    file_size_bytes: 1,
                }],
            }],
        );
        assert_eq!(updates[0]["snapshot"]["summary"]["operation"], "overwrite");
        assert_eq!(updates[0]["snapshot"]["summary"]["deleted-records"], "3");
    }
}
