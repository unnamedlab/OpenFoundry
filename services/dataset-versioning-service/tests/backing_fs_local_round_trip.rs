//! P3 — LocalBackingFs end-to-end round trip.
//!
//! Foundry doc § "Backing filesystem" requires the implementation to
//! map `logical_path → physical_path` and round-trip the bytes. This
//! is a Tokio unit test (no Postgres needed) so it stays fast.

use std::sync::Arc;

use bytes::Bytes;
use storage_abstraction::backing_fs::{
    BackingFileSystem, LocalBackingFs, LocalBackingFsConfig, PutOpts,
};
use storage_abstraction::local::LocalStorage;

#[tokio::test]
async fn local_backing_fs_round_trips_bytes_and_lists_under_prefix() {
    let dir = tempfile::tempdir().expect("tempdir");
    let backend = Arc::new(LocalStorage::new(dir.path().to_str().unwrap()).unwrap());
    let fs = LocalBackingFs::new(
        backend,
        LocalBackingFsConfig {
            fs_id: "local".into(),
            base_directory: "foundry/datasets".into(),
            presign_secret: "test-secret".into(),
            public_origin: "".into(),
        },
    )
    .unwrap();

    // Write three files under the same dataset prefix.
    let p1 = fs
        .put(
            "rid-x/transactions/t1/file-a.parquet",
            Bytes::from_static(b"alpha"),
            PutOpts::default(),
        )
        .await
        .unwrap();
    let p2 = fs
        .put(
            "rid-x/transactions/t1/file-b.parquet",
            Bytes::from_static(b"beta"),
            PutOpts::default(),
        )
        .await
        .unwrap();
    let p3 = fs
        .put(
            "rid-y/transactions/t1/file-c.parquet",
            Bytes::from_static(b"gamma"),
            PutOpts::default(),
        )
        .await
        .unwrap();

    // Round-trip read.
    assert_eq!(&fs.get(&p1).await.unwrap()[..], b"alpha");
    assert_eq!(&fs.get(&p2).await.unwrap()[..], b"beta");
    assert_eq!(&fs.get(&p3).await.unwrap()[..], b"gamma");

    // List under the rid-x prefix returns only its files.
    let entries = fs.list("rid-x").await.unwrap();
    let mut paths: Vec<_> = entries.iter().map(|e| e.logical_path.clone()).collect();
    paths.sort();
    assert_eq!(
        paths,
        vec![
            "rid-x/transactions/t1/file-a.parquet".to_string(),
            "rid-x/transactions/t1/file-b.parquet".to_string(),
        ]
    );

    // Delete removes the underlying object so subsequent get() fails.
    fs.delete(&p1).await.unwrap();
    let read = fs.get(&p1).await;
    assert!(read.is_err(), "deleted file must not be readable");
}

#[tokio::test]
async fn local_backing_fs_uri_format_matches_persisted_column() {
    // Persisted `dataset_files.physical_uri` strings come from
    // `PhysicalLocation::uri()`; this asserts the round-trip is stable.
    let dir = tempfile::tempdir().unwrap();
    let backend = Arc::new(LocalStorage::new(dir.path().to_str().unwrap()).unwrap());
    let fs = LocalBackingFs::new(
        backend,
        LocalBackingFsConfig {
            fs_id: "local".into(),
            base_directory: "foundry/datasets".into(),
            presign_secret: "x".into(),
            public_origin: "".into(),
        },
    )
    .unwrap();
    let physical = fs
        .put(
            "rid/transactions/t1/file.parquet",
            Bytes::from_static(b"data"),
            PutOpts::default(),
        )
        .await
        .unwrap();
    assert_eq!(
        physical.uri(),
        "local:///foundry/datasets/rid/transactions/t1/file.parquet"
    );
    assert_eq!(
        physical.object_key(),
        "foundry/datasets/rid/transactions/t1/file.parquet"
    );
}
