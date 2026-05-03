//! P4 — DELETE transactions are view-level only.
//!
//! Foundry doc § "Retention":
//!
//!   > Note that committing a DELETE transaction does not delete the
//!   > underlying file from the backing file system — it simply removes
//!   > the file reference from the dataset view.
//!
//! This test stages a file via SNAPSHOT, commits a DELETE transaction
//! against it, and asserts that:
//!   * `dataset_files.deleted_at` is populated (P3 trigger).
//!   * The bytes at `physical_uri` are still readable from the
//!     backing-fs storage backend (no physical purge happened).
//! Docker-gated.

mod common;

use bytes::Bytes;
use sqlx::PgPool;
use storage_abstraction::StorageBackend;
use uuid::Uuid;

async fn seed_committed(
    pool: &PgPool,
    backend: &dyn StorageBackend,
    dataset_id: Uuid,
    branch_id: Uuid,
    tx_type: &str,
    files: &[(&str, &str, &[u8])], // (logical_path, op, contents)
) -> Uuid {
    let txn_id = Uuid::now_v7();
    sqlx::query(
        r#"INSERT INTO dataset_transactions
              (id, dataset_id, branch_id, branch_name, tx_type, status,
               summary, started_at)
           VALUES ($1, $2, $3, 'master', $4, 'OPEN', '', NOW())"#,
    )
    .bind(txn_id)
    .bind(dataset_id)
    .bind(branch_id)
    .bind(tx_type)
    .execute(pool)
    .await
    .expect("seed open txn");

    for (logical, op, contents) in files {
        let physical = format!("foundry/datasets/{txn_id}/{logical}");
        if !contents.is_empty() {
            backend
                .put(&physical, Bytes::copy_from_slice(contents))
                .await
                .expect("put physical bytes");
        }
        sqlx::query(
            r#"INSERT INTO dataset_transaction_files
                  (transaction_id, logical_path, physical_path, size_bytes, op)
               VALUES ($1, $2, $3, $4, $5)"#,
        )
        .bind(txn_id)
        .bind(logical)
        .bind(&physical)
        .bind(contents.len() as i64)
        .bind(op)
        .execute(pool)
        .await
        .expect("stage file");
    }

    sqlx::query(
        "UPDATE dataset_transactions SET status='COMMITTED', committed_at=NOW() WHERE id=$1",
    )
    .bind(txn_id)
    .execute(pool)
    .await
    .expect("commit");
    sqlx::query(
        "UPDATE dataset_branches SET head_transaction_id=$1, updated_at=NOW() WHERE id=$2",
    )
    .bind(txn_id)
    .bind(branch_id)
    .execute(pool)
    .await
    .expect("advance HEAD");
    txn_id
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn delete_transaction_marks_file_removed_but_keeps_bytes_in_backing_fs() {
    let h = common::spawn().await;
    let dataset_id = common::seed_dataset_with_master(
        &h.pool,
        "ri.foundry.main.dataset.delete-no-purge",
    )
    .await;
    let branch_id = sqlx::query_scalar::<_, Uuid>(
        "SELECT id FROM dataset_branches WHERE dataset_id = $1 AND name = 'master'",
    )
    .bind(dataset_id)
    .fetch_one(&h.pool)
    .await
    .unwrap();

    // 1) SNAPSHOT a single file with real bytes in the backing store.
    let payload = b"persistent-bytes-do-not-purge";
    let snapshot_txn = seed_committed(
        &h.pool,
        h.backend.as_ref(),
        dataset_id,
        branch_id,
        "SNAPSHOT",
        &[("data/persistent.parquet", "ADD", payload)],
    )
    .await;

    // The seeded SNAPSHOT physical path is the one the trigger
    // recorded in dataset_files.physical_uri. Read it back so we know
    // the exact key we'll re-check after the DELETE commit.
    #[derive(sqlx::FromRow)]
    struct FileRow {
        id: Uuid,
        physical_uri: String,
    }
    let snapshot_file: FileRow = sqlx::query_as::<_, FileRow>(
        r#"SELECT id, physical_uri FROM dataset_files
            WHERE transaction_id = $1 AND logical_path = $2"#,
    )
    .bind(snapshot_txn)
    .bind("data/persistent.parquet")
    .fetch_one(&h.pool)
    .await
    .unwrap();

    // The physical_uri in dataset_files uses the local:/// scheme
    // (P3 trigger). The backing key for the backend is the path with
    // the scheme stripped.
    let backend_key = snapshot_file
        .physical_uri
        .strip_prefix("local:///")
        .unwrap()
        .to_string();
    let bytes_before = h
        .backend
        .get(&backend_key)
        .await
        .expect("file is readable before DELETE");
    assert_eq!(&bytes_before[..], payload);

    // 2) DELETE the same logical path. Foundry: REMOVE op only — no
    //    physical bytes touched.
    let delete_txn = seed_committed(
        &h.pool,
        h.backend.as_ref(),
        dataset_id,
        branch_id,
        "DELETE",
        &[("data/persistent.parquet", "REMOVE", &[])],
    )
    .await;
    let _ = delete_txn;

    // 3) The dataset_files row is soft-deleted (deleted_at populated).
    let deleted_at: Option<chrono::DateTime<chrono::Utc>> = sqlx::query_scalar(
        "SELECT deleted_at FROM dataset_files WHERE transaction_id=$1 AND logical_path=$2",
    )
    .bind(delete_txn)
    .bind("data/persistent.parquet")
    .fetch_one(&h.pool)
    .await
    .unwrap();
    assert!(
        deleted_at.is_some(),
        "DELETE txn should soft-delete the dataset_files row via the P3 trigger"
    );

    // 4) The bytes are STILL readable from the backing store: physical
    //    purge is the retention runner's job, not DVS's.
    let bytes_after = h
        .backend
        .get(&backend_key)
        .await
        .expect("DELETE must not touch the physical bytes");
    assert_eq!(
        &bytes_after[..],
        payload,
        "Foundry contract: DELETE transactions are view-level only"
    );
}
