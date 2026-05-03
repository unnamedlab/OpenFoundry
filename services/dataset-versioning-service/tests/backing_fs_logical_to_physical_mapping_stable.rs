//! P3 — Foundry-mapping stability.
//!
//! Doc § "Backing filesystem": "a mapping is maintained between a file's
//! logical path in Foundry and its physical path in a backing file
//! system". Two observable contracts here:
//!
//!   1. The same logical_path written from two different transactions
//!      yields *different* physical URIs (the txn id is in the path so
//!      a re-upload doesn't clobber history).
//!   2. The same logical_path written once + re-read once yields the
//!      *same* physical URI (deterministic / stable).
//!
//! This is a Tokio unit test — no Postgres needed.

use std::sync::Arc;

use bytes::Bytes;
use storage_abstraction::backing_fs::{
    BackingFileSystem, LocalBackingFs, LocalBackingFsConfig, PutOpts,
};
use storage_abstraction::local::LocalStorage;

fn build_fs() -> LocalBackingFs {
    let dir = tempfile::tempdir().unwrap();
    let backend = Arc::new(LocalStorage::new(dir.path().to_str().unwrap()).unwrap());
    LocalBackingFs::new(
        backend,
        LocalBackingFsConfig {
            fs_id: "local".into(),
            base_directory: "foundry/datasets".into(),
            presign_secret: "x".into(),
            public_origin: "".into(),
        },
    )
    .unwrap()
}

#[tokio::test]
async fn same_logical_path_in_two_commits_lands_on_distinct_physical_paths() {
    // The handler embeds the transaction id in the logical path so a
    // SNAPSHOT followed by an UPDATE on the same dataset-relative file
    // produces two distinct physical URIs that can be served
    // simultaneously (point-in-time reads against the older view stay
    // valid). We simulate the handler's naming convention here.
    let fs = build_fs();
    let txn_a = "rid/transactions/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/data.parquet";
    let txn_b = "rid/transactions/bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb/data.parquet";
    let p1 = fs
        .put(txn_a, Bytes::from_static(b"v1"), PutOpts::default())
        .await
        .unwrap();
    let p2 = fs
        .put(txn_b, Bytes::from_static(b"v2"), PutOpts::default())
        .await
        .unwrap();

    assert_ne!(p1.uri(), p2.uri(), "two commits must land at distinct URIs");
    // Both reads return the bytes that were written — no clobber.
    assert_eq!(&fs.get(&p1).await.unwrap()[..], b"v1");
    assert_eq!(&fs.get(&p2).await.unwrap()[..], b"v2");
}

#[tokio::test]
async fn same_logical_path_round_trip_is_deterministic() {
    let fs = build_fs();
    let logical = "rid/transactions/t1/data.parquet";
    let p1 = fs
        .put(logical, Bytes::from_static(b"v1"), PutOpts::default())
        .await
        .unwrap();
    // Overwriting the same logical path yields the same physical URI
    // (same fs_id / base_dir / relative_path) — only the version_token
    // moves forward.
    let p2 = fs
        .put(logical, Bytes::from_static(b"v2"), PutOpts::default())
        .await
        .unwrap();
    assert_eq!(p1.fs_id, p2.fs_id);
    assert_eq!(p1.base_dir, p2.base_dir);
    assert_eq!(p1.relative_path, p2.relative_path);
    // The version_token may differ when the underlying mtime moves; we
    // just assert determinism of the physical URI text.
    assert_eq!(p1.uri(), p2.uri());
}
