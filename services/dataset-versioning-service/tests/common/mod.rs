//! Shared harness for `dataset-versioning-service` integration tests.

#![allow(dead_code)]

use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use axum::Router;
use dataset_versioning_service::{
    AppState, DatasetWriter, build_router, storage::LegacyDatasetWriter,
};
use sqlx::PgPool;
use storage_abstraction::backend::StorageBackend;
use storage_abstraction::backing_fs::{BackingFileSystem, LocalBackingFs, LocalBackingFsConfig};
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
    pub mock: wiremock::MockServer,
    pub backend: Arc<dyn StorageBackend>,
    pub backing_fs: Arc<dyn BackingFileSystem>,
    pub _storage_dir: TempDir,
}

impl Harness {
    /// Backing object store used by the preview path (LocalStorage rooted
    /// at `_storage_dir`). Tests use this to drop fixture files at known
    /// physical paths before triggering a preview.
    pub fn storage_backend(&self) -> Arc<dyn StorageBackend> {
        self.backend.clone()
    }
}

pub async fn spawn() -> Harness {
    let (container, pool, _url) = boot_postgres().await;

    sqlx::migrate!("./migrations")
        .run(&pool)
        .await
        .expect("apply dataset-versioning-service migrations");

    let dir = tempfile::tempdir().expect("temp storage root");
    let local_storage =
        Arc::new(LocalStorage::new(dir.path().to_str().unwrap()).expect("local storage"));
    let backend: Arc<dyn StorageBackend> = local_storage.clone();
    let writer: Arc<dyn DatasetWriter> =
        Arc::new(LegacyDatasetWriter::new(backend.clone(), "tests"));
    let backing_fs: Arc<dyn BackingFileSystem> = Arc::new(
        LocalBackingFs::new(
            local_storage,
            LocalBackingFsConfig {
                fs_id: "local".into(),
                base_directory: "foundry/datasets".into(),
                presign_secret: "test-presign-secret".into(),
                public_origin: String::new(),
            },
        )
        .expect("local backing fs"),
    );

    let jwt_config = fixtures::jwt_config();
    let token = fixtures::dev_token(
        &jwt_config,
        vec!["dataset.read".into(), "dataset.write".into()],
    );

    let mock = wiremock::MockServer::start().await;
    let neighbour = mock.uri();

    let state = AppState {
        db: pool.clone(),
        jwt_config: Arc::new(jwt_config.clone()),
        writer,
        storage: backend.clone(),
        backing_fs: backing_fs.clone(),
        presign_ttl_seconds: 300,
        http: reqwest::Client::new(),
        retention_policy_url: Some(neighbour.clone()),
        data_asset_catalog_url: Some(neighbour),
    };
    let router = build_router(state);

    Harness {
        container,
        pool,
        router,
        jwt_config,
        token,
        mock,
        backend,
        backing_fs,
        _storage_dir: dir,
    }
}

/// Insert a SNAPSHOT transaction + view + single file row, all wired so
/// the preview path can resolve the view and read the bytes from
/// `physical_path`. Returns the freshly minted view id.
///
/// The "logical" path is set to the basename of the physical path; for
/// preview tests we don't care about logical/physical separation as
/// long as the storage `get(physical_path)` works.
pub async fn seed_committed_view_with_file(
    pool: &PgPool,
    dataset_id: uuid::Uuid,
    physical_path: &str,
    size_bytes: i64,
) -> uuid::Uuid {
    let branch_id = sqlx::query_scalar::<_, uuid::Uuid>(
        "SELECT id FROM dataset_branches WHERE dataset_id = $1 AND name = 'master' LIMIT 1",
    )
    .bind(dataset_id)
    .fetch_one(pool)
    .await
    .expect("master branch exists");

    let txn_id = uuid::Uuid::now_v7();
    sqlx::query(
        r#"INSERT INTO dataset_transactions
              (id, dataset_id, branch_id, branch_name, tx_type, status,
               summary, started_at, committed_at)
           VALUES ($1, $2, $3, 'master', 'SNAPSHOT', 'COMMITTED',
                   'preview test', NOW(), NOW())"#,
    )
    .bind(txn_id)
    .bind(dataset_id)
    .bind(branch_id)
    .execute(pool)
    .await
    .expect("seed committed transaction");

    sqlx::query(
        r#"INSERT INTO dataset_transaction_files
              (transaction_id, logical_path, physical_path, size_bytes, op)
           VALUES ($1, $2, $3, $4, 'ADD')"#,
    )
    .bind(txn_id)
    .bind(physical_path)
    .bind(physical_path)
    .bind(size_bytes)
    .execute(pool)
    .await
    .expect("seed transaction file");

    let view_id = uuid::Uuid::now_v7();
    sqlx::query(
        r#"INSERT INTO dataset_views
              (id, dataset_id, branch_id, head_transaction_id,
               computed_at, file_count, size_bytes)
           VALUES ($1, $2, $3, $4, NOW(), 1, $5)"#,
    )
    .bind(view_id)
    .bind(dataset_id)
    .bind(branch_id)
    .bind(txn_id)
    .bind(size_bytes)
    .execute(pool)
    .await
    .expect("seed view");

    sqlx::query(
        r#"INSERT INTO dataset_view_files
              (view_id, logical_path, physical_path, size_bytes, introduced_by)
           VALUES ($1, $2, $3, $4, $5)"#,
    )
    .bind(view_id)
    .bind(physical_path)
    .bind(physical_path)
    .bind(size_bytes)
    .bind(txn_id)
    .execute(pool)
    .await
    .expect("seed view file");

    // Point the branch HEAD at the seeded transaction so future
    // /views/current calls resolve to it.
    sqlx::query(
        "UPDATE dataset_branches SET head_transaction_id = $1, updated_at = NOW() WHERE id = $2",
    )
    .bind(txn_id)
    .bind(branch_id)
    .execute(pool)
    .await
    .expect("advance branch HEAD");

    view_id
}

/// Upsert the view-scoped schema for the given view. `file_format`
/// must be one of `PARQUET` | `AVRO` | `TEXT` (matches the migration's
/// CHECK constraint).
pub async fn upsert_view_schema(
    pool: &PgPool,
    view_id: uuid::Uuid,
    schema_json: &serde_json::Value,
    file_format: &str,
) {
    let custom = schema_json
        .get("custom_metadata")
        .cloned()
        .unwrap_or(serde_json::Value::Null);
    use md5::{Digest, Md5};
    let mut hasher = Md5::new();
    hasher.update(serde_json::to_string(schema_json).unwrap().as_bytes());
    let hash = format!("{:x}", hasher.finalize());
    sqlx::query(
        r#"INSERT INTO dataset_view_schemas
              (view_id, schema_json, file_format, custom_metadata, content_hash, created_at)
           VALUES ($1, $2, $3, $4, $5, NOW())
           ON CONFLICT (view_id) DO UPDATE
              SET schema_json     = EXCLUDED.schema_json,
                  file_format     = EXCLUDED.file_format,
                  custom_metadata = EXCLUDED.custom_metadata,
                  content_hash    = EXCLUDED.content_hash"#,
    )
    .bind(view_id)
    .bind(schema_json)
    .bind(file_format)
    .bind(if custom.is_null() { None } else { Some(custom) })
    .bind(hash)
    .execute(pool)
    .await
    .expect("upsert view schema");
}

/// Insert a minimal `datasets` row in the versioning DB and a default
/// `master` branch so transaction handlers find a parent. Returns the
/// dataset id.
pub async fn seed_dataset_with_master(pool: &PgPool, rid: &str) -> uuid::Uuid {
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
    id
}
