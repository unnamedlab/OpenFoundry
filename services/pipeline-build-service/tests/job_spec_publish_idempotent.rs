//! P2 — JobSpec publish is idempotent on content hash.
//!
//! Boots Postgres via `testing::containers::boot_postgres`, applies the
//! pipeline-authoring migrations, and exercises the upsert semantics
//! the `publish_job_spec` handler relies on:
//!
//!   1. Insert a JobSpec → version = 1.
//!   2. Republish identical content → version stays at 1, no row
//!      added (only one row in `pipeline_job_specs` for the key).
//!   3. Republish with changed `inputs` → version bumps to 2.
//!
//! Pure SQL, no HTTP — the handler's UPSERT shape is what we validate.

use sqlx::Row;
use testing::containers::boot_postgres;
use uuid::Uuid;

const PIPELINE_RID: &str = "ri.foundry.main.pipeline.publish-idem";
const OUTPUT_RID: &str = "ri.foundry.main.dataset.publish-idem-out";
const BRANCH: &str = "feature";

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn publish_is_idempotent_on_content_hash_and_bumps_on_change() {
    let (_container, pool, _url) = boot_postgres().await;
    sqlx::migrate!("./migrations")
        .run(&pool)
        .await
        .expect("apply migrations");

    let publisher = Uuid::new_v4();

    // ── 1. First publish — fresh row at version 1. ───────────────────
    let row1 = upsert_job_spec(
        &pool,
        publisher,
        OUTPUT_RID,
        serde_json::json!({"node": "transform"}),
        serde_json::json!([{ "input": "ri.foundry.main.dataset.X", "fallback_chain": ["master"] }]),
        "hash-A",
    )
    .await;
    let id1: Uuid = row1.get("id");
    let version1: i32 = row1.get("version");
    assert_eq!(version1, 1);

    // ── 2. Republish IDENTICAL content — same id, same version. ──────
    let row2 = upsert_job_spec(
        &pool,
        publisher,
        OUTPUT_RID,
        serde_json::json!({"node": "transform"}),
        serde_json::json!([{ "input": "ri.foundry.main.dataset.X", "fallback_chain": ["master"] }]),
        "hash-A",
    )
    .await;
    assert_eq!(
        row2.get::<Uuid, _>("id"),
        id1,
        "row identity must be stable"
    );
    assert_eq!(row2.get::<i32, _>("version"), 1, "version must not bump");

    // Only one row across all republishes.
    let count: i64 = sqlx::query_scalar(
        r#"SELECT COUNT(*) FROM pipeline_job_specs
            WHERE pipeline_rid = $1 AND branch_name = $2 AND output_dataset_rid = $3"#,
    )
    .bind(PIPELINE_RID)
    .bind(BRANCH)
    .bind(OUTPUT_RID)
    .fetch_one(&pool)
    .await
    .unwrap();
    assert_eq!(count, 1);

    // ── 3. Republish with NEW content — version bumps to 2. ──────────
    let row3 = upsert_job_spec(
        &pool,
        publisher,
        OUTPUT_RID,
        serde_json::json!({"node": "transform-v2"}),
        serde_json::json!([{ "input": "ri.foundry.main.dataset.X", "fallback_chain": ["develop", "master"] }]),
        "hash-B",
    )
    .await;
    assert_eq!(
        row3.get::<Uuid, _>("id"),
        id1,
        "row identity must persist across version bumps"
    );
    assert_eq!(row3.get::<i32, _>("version"), 2);
    assert_eq!(row3.get::<String, _>("content_hash"), "hash-B");
}

/// Replicates the upsert semantics from
/// `services/pipeline-authoring-service/src/handlers/job_specs.rs::publish_job_spec`.
async fn upsert_job_spec(
    pool: &sqlx::PgPool,
    publisher: Uuid,
    output_rid: &str,
    job_spec_json: serde_json::Value,
    inputs: serde_json::Value,
    content_hash: &str,
) -> sqlx::postgres::PgRow {
    // Try insert first.
    let inserted = sqlx::query(
        r#"INSERT INTO pipeline_job_specs (
                id, pipeline_rid, branch_name, output_dataset_rid,
                output_branch, job_spec_json, inputs, content_hash, version,
                published_by
            )
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, $9)
            ON CONFLICT (pipeline_rid, branch_name, output_dataset_rid)
            DO NOTHING
            RETURNING *"#,
    )
    .bind(Uuid::now_v7())
    .bind(PIPELINE_RID)
    .bind(BRANCH)
    .bind(output_rid)
    .bind(BRANCH)
    .bind(&job_spec_json)
    .bind(&inputs)
    .bind(content_hash)
    .bind(publisher)
    .fetch_optional(pool)
    .await
    .expect("insert");

    if let Some(row) = inserted {
        return row;
    }

    // Existing row: bump version when content changed; otherwise no-op.
    sqlx::query(
        r#"UPDATE pipeline_job_specs
              SET output_branch = $5,
                  job_spec_json = CASE WHEN content_hash = $8 THEN job_spec_json ELSE $6 END,
                  inputs        = CASE WHEN content_hash = $8 THEN inputs        ELSE $7 END,
                  version       = CASE WHEN content_hash = $8 THEN version       ELSE version + 1 END,
                  content_hash  = $8,
                  published_by  = CASE WHEN content_hash = $8 THEN published_by ELSE $9 END,
                  published_at  = CASE WHEN content_hash = $8 THEN published_at ELSE NOW() END
            WHERE pipeline_rid = $2 AND branch_name = $3 AND output_dataset_rid = $4
            RETURNING *"#,
    )
    .bind(Uuid::now_v7())
    .bind(PIPELINE_RID)
    .bind(BRANCH)
    .bind(output_rid)
    .bind(BRANCH)
    .bind(&job_spec_json)
    .bind(&inputs)
    .bind(content_hash)
    .bind(publisher)
    .fetch_one(pool)
    .await
    .expect("update")
}
