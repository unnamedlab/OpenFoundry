//! Pure decoders for the request payload + the
//! `ontology.reindex.v1` record body, plus (under the `runtime`
//! feature) the Cassandra paginated scan that hydrates the records.
//!
//! The Cassandra side intentionally mirrors the Go worker
//! (`workers-go/reindex/activities/activities.go::scan`) so the
//! cut-over leaves the data plane bit-for-bit identical:
//!
//! * Same two queries: `objects_by_type` (per-type or `ALLOW
//!   FILTERING` for all-types) followed by per-id `objects_by_id`
//!   hydration.
//! * Same opaque base64 encoding of the Cassandra `PageState`.
//! * Same JSON record shape published to `ontology.reindex.v1`,
//!   so `services/ontology-indexer` keeps decoding both
//!   `object.changed.v1` and `reindex.v1` with the same code path.

use serde::{Deserialize, Serialize};
use serde_json::Value;
use thiserror::Error;

use crate::event::ReindexRequestedV1;

/// Bounds on the per-page size. The lower bound matches the
/// minimum useful page; the upper bound is the same hard cap as
/// the SQL `CHECK (page_size > 0 AND page_size <= 10000)` on
/// `reindex_jobs` so the two layers cannot disagree.
pub const MIN_PAGE_SIZE: i32 = 1;
pub const MAX_PAGE_SIZE: i32 = 10_000;
pub const DEFAULT_PAGE_SIZE: i32 = 1000;

#[derive(Debug, Error)]
pub enum DecodeError {
    #[error("invalid JSON payload: {0}")]
    Json(#[from] serde_json::Error),
    #[error("missing required field: {0}")]
    MissingField(&'static str),
    #[error("invalid field {field}: {reason}")]
    InvalidField {
        field: &'static str,
        reason: &'static str,
    },
}

/// Parsed + validated `ontology.reindex.requested.v1` payload.
///
/// `page_size` is clamped into `[MIN_PAGE_SIZE, MAX_PAGE_SIZE]`
/// and defaults to [`DEFAULT_PAGE_SIZE`] (matches the legacy Go
/// worker default of 1000).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DecodedRequest {
    pub tenant_id: String,
    pub type_id: Option<String>,
    pub page_size: i32,
    pub request_id: Option<String>,
}

/// Decode + validate a `requested.v1` payload from raw Kafka bytes.
pub fn decode_request(bytes: &[u8]) -> Result<DecodedRequest, DecodeError> {
    let raw: ReindexRequestedV1 = serde_json::from_slice(bytes)?;
    if raw.tenant_id.trim().is_empty() {
        return Err(DecodeError::MissingField("tenant_id"));
    }
    let type_id = raw.type_id.and_then(|t| {
        let trimmed = t.trim().to_string();
        if trimmed.is_empty() {
            None
        } else {
            Some(trimmed)
        }
    });
    let page_size = match raw.page_size {
        None | Some(0) => DEFAULT_PAGE_SIZE,
        Some(n) if n < 0 => {
            return Err(DecodeError::InvalidField {
                field: "page_size",
                reason: "must be non-negative",
            });
        }
        Some(n) => n.clamp(MIN_PAGE_SIZE, MAX_PAGE_SIZE),
    };
    Ok(DecodedRequest {
        tenant_id: raw.tenant_id,
        type_id,
        page_size,
        request_id: raw.request_id,
    })
}

/// One JSON record published to `ontology.reindex.v1`. Matches
/// the shape produced by the Go worker
/// (`workers-go/reindex/activities/activities.go::fetchObject`)
/// and consumed by `services/ontology-indexer::ObjectChangedV1`.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct ReindexRecord {
    pub tenant: String,
    pub id: String,
    pub type_id: String,
    pub version: i64,
    pub payload: Value,
    /// Optional dense vector. Pass-through only — the coordinator
    /// does not compute it; whatever was in
    /// `objects_by_id.properties` is forwarded verbatim.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub embedding: Option<Vec<f64>>,
    /// Always `false`: deleted rows are filtered out before
    /// publish, mirroring the Go worker's behaviour.
    #[serde(default)]
    pub deleted: bool,
}

impl ReindexRecord {
    /// Partition key used when producing to Kafka. Same `tenant/id`
    /// composition as the Go worker so re-indexed records hash to
    /// the same partition as live `object.changed.v1` records for
    /// the same object — required for the indexer's per-object
    /// version check.
    pub fn partition_key(&self) -> String {
        let mut k = String::with_capacity(self.tenant.len() + 1 + self.id.len());
        k.push_str(&self.tenant);
        k.push('/');
        k.push_str(&self.id);
        k
    }
}

/// Build a [`ReindexRecord`] from the raw fields fetched from
/// Cassandra. `properties` is the JSON column from
/// `objects_by_id`; we attempt to extract `embedding` from it
/// (same heuristic as the Go worker).
pub fn encode_batch_record(
    tenant: String,
    id: String,
    type_id: String,
    version: i64,
    properties: Value,
) -> ReindexRecord {
    let embedding = extract_embedding(&properties);
    ReindexRecord {
        tenant,
        id,
        type_id,
        version,
        payload: properties,
        embedding,
        deleted: false,
    }
}

fn extract_embedding(properties: &Value) -> Option<Vec<f64>> {
    let arr = properties.get("embedding")?.as_array()?;
    let mut out = Vec::with_capacity(arr.len());
    for v in arr {
        if let Some(f) = v.as_f64() {
            out.push(f);
        }
    }
    if out.is_empty() { None } else { Some(out) }
}

// ────────────────────────── Runtime scan ──────────────────────────

#[cfg(feature = "runtime")]
pub use cassandra::{CassandraScanner, PageOutcome, ScanError};

#[cfg(feature = "runtime")]
mod cassandra {
    //! Live Cassandra paginated scan, gated behind `runtime` so
    //! the pure decoders above stay buildable in CI without a
    //! Scylla driver dependency. Kept intentionally close to the
    //! Go worker — see the module-level doc, and uses the same
    //! `execute_paged` + `rows_typed_or_empty` API as
    //! `libs/cassandra-kernel/src/repos.rs::list_by_type`.

    use std::sync::Arc;

    use base64::Engine;
    use base64::engine::general_purpose::STANDARD as B64;
    use bytes::Bytes;
    use cassandra_kernel::scylla::Session;
    use cassandra_kernel::scylla::prepared_statement::PreparedStatement;
    use serde_json::Value;
    use thiserror::Error;
    use tokio::sync::OnceCell;
    use uuid::Uuid;

    use super::{ReindexRecord, encode_batch_record};

    /// Page returned by [`CassandraScanner::scan_page`].
    #[derive(Debug)]
    pub struct PageOutcome {
        /// Hydrated records ready for publish. Deleted rows are
        /// already filtered out.
        pub records: Vec<ReindexRecord>,
        /// Total ids fetched from the index table for this page,
        /// regardless of whether they survived the deleted-row
        /// filter. Used for the `scanned` counter on the job row.
        pub scanned: usize,
        /// Opaque next-page token, base64 of the gocql `PageState`.
        /// `None` ⇒ end of stream.
        pub next_token: Option<String>,
    }

    #[derive(Debug, Error)]
    pub enum ScanError {
        #[error("cassandra query failed: {0}")]
        Driver(String),
        #[error("invalid resume token: {0}")]
        InvalidResumeToken(String),
        #[error("invalid object payload for id {object_id}: {reason}")]
        InvalidObjectPayload { object_id: String, reason: String },
    }

    /// Cassandra scan helper. One instance per process, shared
    /// behind `Arc`. The keyspace is fixed at construction time so
    /// the table name slot in the prepared CQL is never user-
    /// controlled (CQL does not parameterise table names). Prepared
    /// statements are lazily cached per scanner instance — three
    /// shapes total (per-type index, all-types index, hydrate by
    /// id) so the cache hit rate is effectively 100%.
    pub struct CassandraScanner {
        session: Arc<Session>,
        keyspace: String,
        stmt_index_by_type: OnceCell<PreparedStatement>,
        stmt_index_all_types: OnceCell<PreparedStatement>,
        stmt_get_object: OnceCell<PreparedStatement>,
    }

    impl CassandraScanner {
        pub fn new(session: Arc<Session>, keyspace: impl Into<String>) -> Self {
            Self {
                session,
                keyspace: keyspace.into(),
                stmt_index_by_type: OnceCell::new(),
                stmt_index_all_types: OnceCell::new(),
                stmt_get_object: OnceCell::new(),
            }
        }

        async fn prep_index_by_type(&self) -> Result<&PreparedStatement, ScanError> {
            self.stmt_index_by_type
                .get_or_try_init(|| async {
                    let cql = format!(
                        "SELECT object_id FROM {ks}.objects_by_type \
                         WHERE tenant = ? AND type_id = ?",
                        ks = self.keyspace,
                    );
                    self.session
                        .prepare(cql)
                        .await
                        .map_err(|e| ScanError::Driver(e.to_string()))
                })
                .await
        }

        async fn prep_index_all_types(&self) -> Result<&PreparedStatement, ScanError> {
            self.stmt_index_all_types
                .get_or_try_init(|| async {
                    let cql = format!(
                        "SELECT type_id, object_id FROM {ks}.objects_by_type \
                         WHERE tenant = ? ALLOW FILTERING",
                        ks = self.keyspace,
                    );
                    self.session
                        .prepare(cql)
                        .await
                        .map_err(|e| ScanError::Driver(e.to_string()))
                })
                .await
        }

        async fn prep_get_object(&self) -> Result<&PreparedStatement, ScanError> {
            self.stmt_get_object
                .get_or_try_init(|| async {
                    let cql = format!(
                        "SELECT type_id, properties, revision_number, deleted \
                         FROM {ks}.objects_by_id WHERE tenant = ? AND object_id = ?",
                        ks = self.keyspace,
                    );
                    self.session
                        .prepare(cql)
                        .await
                        .map_err(|e| ScanError::Driver(e.to_string()))
                })
                .await
        }

        /// Fetch and hydrate one page. The `resume_token` is the
        /// previously-returned [`PageOutcome::next_token`], or
        /// `None` for the first page.
        pub async fn scan_page(
            &self,
            tenant_id: &str,
            type_id: Option<&str>,
            page_size: i32,
            resume_token: Option<&str>,
        ) -> Result<PageOutcome, ScanError> {
            let paging = decode_paging_state(resume_token)?;

            // Index lookup → (object_id, effective_type_id) pairs.
            let (next_state, ids) = if let Some(t) = type_id {
                let mut prep = self.prep_index_by_type().await?.clone();
                prep.set_page_size(page_size);
                let result = self
                    .session
                    .execute_paged(&prep, (tenant_id, t), paging)
                    .await
                    .map_err(|e| ScanError::Driver(e.to_string()))?;
                let next = result.paging_state.clone();
                let mut ids: Vec<(Uuid, String)> = Vec::new();
                for row in result.rows_typed_or_empty::<(Uuid,)>() {
                    let (object_id,) = row.map_err(|e| ScanError::Driver(e.to_string()))?;
                    ids.push((object_id, t.to_string()));
                }
                (next, ids)
            } else {
                let mut prep = self.prep_index_all_types().await?.clone();
                prep.set_page_size(page_size);
                let result = self
                    .session
                    .execute_paged(&prep, (tenant_id,), paging)
                    .await
                    .map_err(|e| ScanError::Driver(e.to_string()))?;
                let next = result.paging_state.clone();
                let mut ids: Vec<(Uuid, String)> = Vec::new();
                for row in result.rows_typed_or_empty::<(String, Uuid)>() {
                    let (row_type, object_id) =
                        row.map_err(|e| ScanError::Driver(e.to_string()))?;
                    ids.push((object_id, row_type));
                }
                (next, ids)
            };

            let scanned = ids.len();
            let next_token = encode_paging_state(next_state.as_ref());

            // Hydrate each id via `objects_by_id`. Deleted rows
            // are filtered here, matching the Go worker.
            let mut records = Vec::with_capacity(ids.len());
            for (object_id, row_type) in ids {
                if let Some(record) = self.fetch_object(tenant_id, object_id, &row_type).await? {
                    records.push(record);
                }
            }

            Ok(PageOutcome {
                records,
                scanned,
                next_token,
            })
        }

        async fn fetch_object(
            &self,
            tenant_id: &str,
            object_id: Uuid,
            type_id_hint: &str,
        ) -> Result<Option<ReindexRecord>, ScanError> {
            let prep = self.prep_get_object().await?.clone();
            let result = self
                .session
                .execute(&prep, (tenant_id, object_id))
                .await
                .map_err(|e| ScanError::Driver(e.to_string()))?;
            let mut rows = result
                .rows_typed_or_empty::<(Option<String>, Option<String>, Option<i64>, Option<bool>)>(
                );
            let Some(row) = rows.next() else {
                return Ok(None);
            };
            let (type_id_opt, properties_opt, revision_opt, deleted_opt) =
                row.map_err(|e| ScanError::Driver(e.to_string()))?;
            if deleted_opt.unwrap_or(false) {
                return Ok(None);
            }
            let type_id = type_id_opt.unwrap_or_else(|| type_id_hint.to_string());
            let revision = revision_opt.unwrap_or(0);
            let properties_text = properties_opt.unwrap_or_else(|| "{}".to_string());
            let properties: Value = serde_json::from_str(&properties_text).map_err(|e| {
                ScanError::InvalidObjectPayload {
                    object_id: object_id.to_string(),
                    reason: e.to_string(),
                }
            })?;
            Ok(Some(encode_batch_record(
                tenant_id.to_string(),
                object_id.to_string(),
                type_id,
                revision,
                properties,
            )))
        }
    }

    // ─── helpers ──────────────────────────────────────────────────

    fn decode_paging_state(token: Option<&str>) -> Result<Option<Bytes>, ScanError> {
        let Some(t) = token else { return Ok(None) };
        if t.is_empty() {
            return Ok(None);
        }
        let raw = B64
            .decode(t.as_bytes())
            .map_err(|e| ScanError::InvalidResumeToken(e.to_string()))?;
        Ok(Some(Bytes::from(raw)))
    }

    fn encode_paging_state(state: Option<&Bytes>) -> Option<String> {
        state.map(|s| B64.encode(s))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn decode_request_validates_tenant() {
        let err = decode_request(br#"{"tenant_id":""}"#).unwrap_err();
        assert!(matches!(err, DecodeError::MissingField("tenant_id")));
    }

    #[test]
    fn decode_request_clamps_page_size() {
        let r = decode_request(br#"{"tenant_id":"t","page_size":99999}"#).unwrap();
        assert_eq!(r.page_size, MAX_PAGE_SIZE);
    }

    #[test]
    fn decode_request_defaults_page_size() {
        let r = decode_request(br#"{"tenant_id":"t"}"#).unwrap();
        assert_eq!(r.page_size, DEFAULT_PAGE_SIZE);
        assert!(r.type_id.is_none());
    }

    #[test]
    fn decode_request_normalises_empty_type_to_none() {
        let r = decode_request(br#"{"tenant_id":"t","type_id":"  "}"#).unwrap();
        assert!(r.type_id.is_none());
    }

    #[test]
    fn decode_request_rejects_negative_page_size() {
        let err = decode_request(br#"{"tenant_id":"t","page_size":-3}"#).unwrap_err();
        assert!(matches!(
            err,
            DecodeError::InvalidField {
                field: "page_size",
                ..
            }
        ));
    }

    #[test]
    fn record_partition_key_matches_legacy_format() {
        let r = encode_batch_record(
            "tenant-a".into(),
            "00000000-0000-0000-0000-000000000001".into(),
            "users".into(),
            7,
            json!({}),
        );
        assert_eq!(
            r.partition_key(),
            "tenant-a/00000000-0000-0000-0000-000000000001"
        );
    }

    #[test]
    fn record_extracts_embedding_when_present() {
        let r = encode_batch_record(
            "tenant-a".into(),
            "id".into(),
            "users".into(),
            1,
            json!({"embedding": [0.1, 0.2, 0.3]}),
        );
        assert_eq!(r.embedding, Some(vec![0.1, 0.2, 0.3]));
    }

    #[test]
    fn record_omits_embedding_when_absent_or_empty() {
        let r1 = encode_batch_record("t".into(), "id".into(), "u".into(), 1, json!({}));
        assert!(r1.embedding.is_none());
        let r2 = encode_batch_record(
            "t".into(),
            "id".into(),
            "u".into(),
            1,
            json!({"embedding": []}),
        );
        assert!(r2.embedding.is_none());
    }

    #[test]
    fn record_round_trip_json_shape_matches_object_changed_v1() {
        // The shape MUST stay aligned with `services/ontology-indexer`
        // `ObjectChangedV1` so the indexer decodes both topics
        // with the same code path.
        let r = encode_batch_record(
            "tenant-a".into(),
            "00000000-0000-0000-0000-000000000001".into(),
            "users".into(),
            7,
            json!({"name":"alice"}),
        );
        let json = serde_json::to_value(&r).unwrap();
        assert_eq!(json["tenant"], "tenant-a");
        assert_eq!(json["id"], "00000000-0000-0000-0000-000000000001");
        assert_eq!(json["type_id"], "users");
        assert_eq!(json["version"], 7);
        assert_eq!(json["deleted"], false);
        assert_eq!(json["payload"]["name"], "alice");
    }
}
