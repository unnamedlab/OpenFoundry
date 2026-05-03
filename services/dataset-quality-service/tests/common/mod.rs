//! Shared harness for `dataset-quality-service` integration tests.
//!
//! Boots Postgres + applies the DVS migrations first (the compute
//! path needs `datasets` / `dataset_transactions` / `dataset_files`)
//! and then the quality migrations on top. `set_ignore_missing(true)`
//! lets both sets share a single `_sqlx_migrations` table.

#![allow(dead_code)]

use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use axum::Router;
use dataset_quality_service::{AppState, build_router};
use sqlx::PgPool;
use storage_abstraction::StorageBackend;
use storage_abstraction::local::LocalStorage;
use tempfile::TempDir;
use testcontainers::{ContainerAsync, GenericImage};
use testing::{containers::boot_postgres, fixtures};

pub struct Harness {
    pub container: ContainerAsync<GenericImage>,
    pub pool: PgPool,
    pub router: Router,
    pub jwt_config: JwtConfig,
    pub token: String,
    pub _scratch: TempDir,
}

pub async fn spawn() -> Harness {
    let (container, pool, _url) = boot_postgres().await;

    let mut quality_migrator = sqlx::migrate!("./migrations");
    quality_migrator.set_ignore_missing(true);
    let mut dvs_migrator = sqlx::migrate!("../dataset-versioning-service/migrations");
    dvs_migrator.set_ignore_missing(true);
    // Apply DVS first so its earlier-numbered migrations can run; the
    // quality migration is the closing step.
    dvs_migrator
        .run(&pool)
        .await
        .expect("apply dataset-versioning migrations");
    quality_migrator
        .run(&pool)
        .await
        .expect("apply dataset-quality migrations");

    let scratch = tempfile::tempdir().expect("scratch");
    let storage: Arc<dyn StorageBackend> =
        Arc::new(LocalStorage::new(scratch.path().to_str().unwrap()).expect("local storage"));

    let jwt_config = fixtures::jwt_config();
    let token = fixtures::dev_token(
        &jwt_config,
        vec!["dataset.read".into(), "dataset.write".into()],
    );

    let state = AppState {
        db: pool.clone(),
        storage,
        jwt_config: Arc::new(jwt_config.clone()),
    };
    let router = build_router(state);

    Harness {
        container,
        pool,
        router,
        jwt_config,
        token,
        _scratch: scratch,
    }
}

/// Insert a dataset + master branch + one COMMITTED snapshot at the
/// given UTC timestamp. Used by the freshness / SLA tests so the
/// caller can pin "now - last_commit" to a known value.
pub async fn seed_dataset_with_committed_at(
    pool: &PgPool,
    rid: &str,
    committed_at: chrono::DateTime<chrono::Utc>,
) -> uuid::Uuid {
    let id = fixtures::seed_dataset(pool, rid, rid, "parquet").await;
    let branch_id = uuid::Uuid::now_v7();
    sqlx::query(
        r#"INSERT INTO dataset_branches
              (id, dataset_id, dataset_rid, name, parent_branch_id, head_transaction_id, is_default)
           VALUES ($1, $2, $3, 'master', NULL, NULL, TRUE)"#,
    )
    .bind(branch_id)
    .bind(id)
    .bind(rid)
    .execute(pool)
    .await
    .expect("seed master branch");

    let txn_id = uuid::Uuid::now_v7();
    sqlx::query(
        r#"INSERT INTO dataset_transactions
              (id, dataset_id, branch_id, branch_name, tx_type, status,
               summary, started_at, committed_at)
           VALUES ($1, $2, $3, 'master', 'SNAPSHOT', 'COMMITTED',
                   'seed', $4, $4)"#,
    )
    .bind(txn_id)
    .bind(id)
    .bind(branch_id)
    .bind(committed_at)
    .execute(pool)
    .await
    .expect("seed committed txn");

    sqlx::query(
        "UPDATE dataset_branches SET head_transaction_id = $1 WHERE id = $2",
    )
    .bind(txn_id)
    .bind(branch_id)
    .execute(pool)
    .await
    .unwrap();

    id
}

/// Insert two view-schema rows for the dataset with different content
/// hashes so `compute_schema_drift` returns true. Returns the latest
/// view's id.
pub async fn seed_schema_drift(pool: &PgPool, dataset_id: uuid::Uuid) -> uuid::Uuid {
    let branch_id = sqlx::query_scalar::<_, uuid::Uuid>(
        "SELECT id FROM dataset_branches WHERE dataset_id = $1 LIMIT 1",
    )
    .bind(dataset_id)
    .fetch_one(pool)
    .await
    .unwrap();

    for (i, hash) in ["aaa111", "bbb222"].iter().enumerate() {
        let txn_id = uuid::Uuid::now_v7();
        sqlx::query(
            r#"INSERT INTO dataset_transactions
                  (id, dataset_id, branch_id, branch_name, tx_type, status,
                   summary, started_at, committed_at)
               VALUES ($1, $2, $3, 'master', 'SNAPSHOT', 'COMMITTED',
                       'drift', NOW() + $4 * INTERVAL '1 second',
                       NOW() + $4 * INTERVAL '1 second')"#,
        )
        .bind(txn_id)
        .bind(dataset_id)
        .bind(branch_id)
        .bind(i as i32)
        .execute(pool)
        .await
        .unwrap();
        let view_id = uuid::Uuid::now_v7();
        sqlx::query(
            r#"INSERT INTO dataset_views
                  (id, dataset_id, branch_id, head_transaction_id,
                   computed_at, file_count, size_bytes)
               VALUES ($1, $2, $3, $4, NOW(), 0, 0)"#,
        )
        .bind(view_id)
        .bind(dataset_id)
        .bind(branch_id)
        .bind(txn_id)
        .execute(pool)
        .await
        .unwrap();
        let fields = if i == 0 {
            serde_json::json!({"fields":[{"name":"a","type":"LONG"}]})
        } else {
            serde_json::json!({
                "fields":[
                    {"name":"a","type":"LONG"},
                    {"name":"b","type":"STRING"}
                ]
            })
        };
        sqlx::query(
            r#"INSERT INTO dataset_view_schemas
                  (view_id, schema_json, file_format, custom_metadata, content_hash, created_at)
               VALUES ($1, $2::jsonb, 'PARQUET', NULL, $3,
                       NOW() + $4 * INTERVAL '1 second')"#,
        )
        .bind(view_id)
        .bind(fields)
        .bind(hash.to_string())
        .bind(i as i32)
        .execute(pool)
        .await
        .unwrap();
    }

    sqlx::query_scalar::<_, uuid::Uuid>(
        "SELECT id FROM dataset_views WHERE dataset_id = $1 ORDER BY computed_at DESC LIMIT 1",
    )
    .bind(dataset_id)
    .fetch_one(pool)
    .await
    .unwrap()
}
