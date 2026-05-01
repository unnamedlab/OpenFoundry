//! Tarea 8 — Versionado de datasets integrado al pipeline de ingesta.
//!
//! After a successful `sync_run`, `connector-management-service` calls
//! `dataset-versioning-service` (`POST /api/v1/datasets/:id/append`) to
//! register a new `DatasetVersion`. The submission carries:
//!
//! * `content_hash` — sha256 of the materialised payload bytes (or of a
//!   canonical signature when the bytes are not available locally).
//! * `schema` — connector-reported column descriptors (when available).
//! * `row_count` — number of rows affected by the sync.
//! * lineage — the source `Connection.id` plus the originating `sync_def_id`
//!   so the version graph can be walked backwards to the data connection.
//!
//! Idempotency: re-running the same sync with the same `content_hash` reuses
//! the previously-recorded `dataset_version_id` instead of opening a new
//! version. This mirrors the Foundry semantic where unchanged inputs do not
//! advance the dataset transaction history.

use std::future::Future;

use serde::Deserialize;
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use sqlx::PgPool;
use uuid::Uuid;

/// Inputs required to register a new dataset version.
#[derive(Debug, Clone)]
pub struct VersionContent {
    pub source_id: Uuid,
    pub output_dataset_id: Uuid,
    pub content_hash: String,
    pub row_count: i64,
    pub size_bytes: i64,
    pub schema: Value,
    pub message: String,
    pub branch_name: Option<String>,
}

/// Outcome of [`ensure_dataset_version`].
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VersionRegistration {
    pub dataset_version_id: Uuid,
    pub content_hash: String,
    pub reused: bool,
}

/// Compute the deterministic content hash used by the version registry.
/// SHA-256 is canonical so identical payload bytes (or an identical
/// canonical signature) always map to the same hash.
pub fn compute_content_hash(bytes: &[u8]) -> String {
    let mut hasher = Sha256::new();
    hasher.update(bytes);
    format!("{:x}", hasher.finalize())
}

/// Build a stable signature when the connector did not return raw bytes
/// (e.g. the gRPC bridge ran the actual ingest out-of-band). The signature
/// pins the sync definition + source identity + row/byte counters so two
/// successive runs over identical state share a hash.
pub fn signature_for_bridge_run(
    sync_def_id: Uuid,
    source_id: Uuid,
    output_dataset_id: Uuid,
    extra: &Value,
) -> String {
    let payload = json!({
        "sync_def_id": sync_def_id,
        "source_id": source_id,
        "output_dataset_id": output_dataset_id,
        "extra": extra,
    });
    let bytes = serde_json::to_vec(&payload).unwrap_or_default();
    compute_content_hash(&bytes)
}

/// Look up the `dataset_version_id` recorded by a previous successful run
/// of the same `sync_def_id` carrying the same `content_hash`. Used to
/// short-circuit the POST when nothing changed.
pub async fn previous_version_for_hash(
    db: &PgPool,
    sync_def_id: Uuid,
    content_hash: &str,
) -> Result<Option<Uuid>, sqlx::Error> {
    sqlx::query_scalar::<_, Option<Uuid>>(
        r#"SELECT dataset_version_id
             FROM sync_runs
            WHERE sync_def_id = $1
              AND content_hash = $2
              AND dataset_version_id IS NOT NULL
            ORDER BY started_at DESC
            LIMIT 1"#,
    )
    .bind(sync_def_id)
    .bind(content_hash)
    .fetch_optional(db)
    .await
    .map(|opt| opt.flatten())
}

/// Pure decision helper: reuse an existing version when one is already
/// registered for the same content hash, otherwise delegate to `poster`
/// (which is expected to call `dataset-versioning-service`).
///
/// Split out from the live HTTP path so it can be exercised in tests
/// without spinning up a Postgres or a real network listener.
#[allow(dead_code)]
pub async fn ensure_dataset_version<F, Fut>(
    poster: F,
    content: &VersionContent,
    previous: Option<Uuid>,
) -> Result<VersionRegistration, String>
where
    F: FnOnce(&VersionContent) -> Fut,
    Fut: Future<Output = Result<Uuid, String>>,
{
    if let Some(existing) = previous {
        return Ok(VersionRegistration {
            dataset_version_id: existing,
            content_hash: content.content_hash.clone(),
            reused: true,
        });
    }
    let new_id = poster(content).await?;
    Ok(VersionRegistration {
        dataset_version_id: new_id,
        content_hash: content.content_hash.clone(),
        reused: false,
    })
}

/// Response shape we care about from `dataset-versioning-service`. The
/// service returns the full `DatasetVersion` row; we only need its `id`.
#[derive(Debug, Deserialize)]
struct DatasetVersionResponse {
    id: Uuid,
}

/// Live HTTP poster: issues `POST /api/v1/datasets/:id/append` against
/// `dataset-versioning-service`.
pub async fn post_dataset_version(
    http: &reqwest::Client,
    dataset_service_url: &str,
    sync_def_id: Uuid,
    content: &VersionContent,
) -> Result<Uuid, String> {
    let url = format!(
        "{}/api/v1/datasets/{}/append",
        dataset_service_url.trim_end_matches('/'),
        content.output_dataset_id
    );
    let body = json!({
        "branch_name": content.branch_name,
        "message": content.message,
        "row_delta": content.row_count,
        "size_delta_bytes": content.size_bytes,
        "metadata": {
            "content_hash": content.content_hash,
            "schema": content.schema,
            "lineage": {
                "source_id": content.source_id,
                "sync_def_id": sync_def_id,
            },
        },
    });
    let response = http
        .post(&url)
        .json(&body)
        .send()
        .await
        .map_err(|error| format!("dataset-versioning-service POST failed: {error}"))?;
    let status = response.status();
    let text = response.text().await.unwrap_or_default();
    if !status.is_success() {
        return Err(format!(
            "dataset-versioning-service responded {status}: {text}"
        ));
    }
    let parsed: DatasetVersionResponse = serde_json::from_str(&text).map_err(|error| {
        format!("could not decode DatasetVersion response: {error} (body: {text})")
    })?;
    Ok(parsed.id)
}

/// Persist the registration outcome on the `sync_runs` row.
pub async fn record_dataset_version_on_run(
    db: &PgPool,
    sync_run_id: Uuid,
    registration: &VersionRegistration,
) -> Result<(), sqlx::Error> {
    sqlx::query(
        r#"UPDATE sync_runs
              SET dataset_version_id = $2,
                  content_hash       = $3
            WHERE id = $1"#,
    )
    .bind(sync_run_id)
    .bind(registration.dataset_version_id)
    .bind(&registration.content_hash)
    .execute(db)
    .await
    .map(|_| ())
}

/// Convenience wrapper used by the live `run_sync` handler. Looks up any
/// previous version for the same hash, calls the dataset-versioning-service
/// when needed, and updates the `sync_runs` row.
pub async fn register_for_run(
    db: &PgPool,
    http: &reqwest::Client,
    dataset_service_url: &str,
    sync_def_id: Uuid,
    sync_run_id: Uuid,
    content: VersionContent,
) -> Result<VersionRegistration, String> {
    let previous = previous_version_for_hash(db, sync_def_id, &content.content_hash)
        .await
        .map_err(|error| format!("previous-version lookup failed: {error}"))?;
    let registration = if let Some(existing) = previous {
        VersionRegistration {
            dataset_version_id: existing,
            content_hash: content.content_hash.clone(),
            reused: true,
        }
    } else {
        let new_id = post_dataset_version(http, dataset_service_url, sync_def_id, &content).await?;
        VersionRegistration {
            dataset_version_id: new_id,
            content_hash: content.content_hash.clone(),
            reused: false,
        }
    };
    record_dataset_version_on_run(db, sync_run_id, &registration)
        .await
        .map_err(|error| format!("record dataset_version_id failed: {error}"))?;
    Ok(registration)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicUsize, Ordering};

    fn sample_content(hash: &str) -> VersionContent {
        VersionContent {
            source_id: Uuid::now_v7(),
            output_dataset_id: Uuid::now_v7(),
            content_hash: hash.to_string(),
            row_count: 42,
            size_bytes: 1_024,
            schema: json!({ "columns": ["id", "value"] }),
            message: "test sync".to_string(),
            branch_name: None,
        }
    }

    #[test]
    fn content_hash_is_deterministic_and_distinguishes_payloads() {
        let a = compute_content_hash(b"alpha");
        let b = compute_content_hash(b"alpha");
        let c = compute_content_hash(b"beta");
        assert_eq!(a, b);
        assert_ne!(a, c);
        assert_eq!(a.len(), 64); // sha256 hex
    }

    #[tokio::test]
    async fn two_distinct_payloads_register_two_versions() {
        let counter = AtomicUsize::new(0);
        let returned = [Uuid::now_v7(), Uuid::now_v7()];

        let first = ensure_dataset_version(
            |_content| async {
                let idx = counter.fetch_add(1, Ordering::SeqCst);
                Ok(returned[idx])
            },
            &sample_content(&compute_content_hash(b"payload-one")),
            None,
        )
        .await
        .expect("first registration");
        let second = ensure_dataset_version(
            |_content| async {
                let idx = counter.fetch_add(1, Ordering::SeqCst);
                Ok(returned[idx])
            },
            &sample_content(&compute_content_hash(b"payload-two")),
            None,
        )
        .await
        .expect("second registration");

        assert_eq!(counter.load(Ordering::SeqCst), 2, "both POSTs fired");
        assert!(!first.reused);
        assert!(!second.reused);
        assert_ne!(first.dataset_version_id, second.dataset_version_id);
    }

    #[tokio::test]
    async fn rerun_with_same_hash_reuses_previous_version() {
        let hash = compute_content_hash(b"identical-payload");
        let original_id = Uuid::now_v7();
        let counter = AtomicUsize::new(0);

        // First run: no previous version → poster fires, version stored.
        let first = ensure_dataset_version(
            |_content| async {
                counter.fetch_add(1, Ordering::SeqCst);
                Ok(original_id)
            },
            &sample_content(&hash),
            None,
        )
        .await
        .expect("first registration");
        assert!(!first.reused);
        assert_eq!(counter.load(Ordering::SeqCst), 1);

        // Second run: previous version known for the same hash → reuse,
        // poster MUST NOT be called again.
        let rerun = ensure_dataset_version(
            |_content| async {
                counter.fetch_add(1, Ordering::SeqCst);
                Err::<Uuid, String>("poster should not be called on idempotent re-run".to_string())
            },
            &sample_content(&hash),
            Some(first.dataset_version_id),
        )
        .await
        .expect("idempotent rerun");

        assert!(rerun.reused);
        assert_eq!(rerun.dataset_version_id, original_id);
        assert_eq!(
            counter.load(Ordering::SeqCst),
            1,
            "POST not re-issued for unchanged content"
        );
    }

    #[tokio::test]
    async fn poster_error_propagates() {
        let outcome = ensure_dataset_version(
            |_content| async { Err::<Uuid, String>("HTTP 502 from versioning".to_string()) },
            &sample_content(&compute_content_hash(b"x")),
            None,
        )
        .await;
        assert!(outcome.is_err());
        assert!(outcome.unwrap_err().contains("HTTP 502"));
    }

    #[test]
    fn bridge_signature_is_stable_for_identical_inputs() {
        let sync_def = Uuid::now_v7();
        let source = Uuid::now_v7();
        let dataset = Uuid::now_v7();
        let extra = json!({ "connector": "postgresql", "table": "orders" });
        assert_eq!(
            signature_for_bridge_run(sync_def, source, dataset, &extra),
            signature_for_bridge_run(sync_def, source, dataset, &extra),
        );
        assert_ne!(
            signature_for_bridge_run(sync_def, source, dataset, &extra),
            signature_for_bridge_run(sync_def, source, dataset, &json!({"different": true})),
        );
    }
}
