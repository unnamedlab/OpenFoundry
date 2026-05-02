// Stub binary entry point. The crate currently exposes the `storage` module
// for use by integration tests and by future handlers; once the HTTP layer is
// wired up, `main` will instantiate the configured `DatasetWriter` (see
// `storage::build_dataset_writer`) and inject it into request state.
//
// Reading `ICEBERG_CATALOG_URL` and `DATASET_WRITER_BACKEND` at startup keeps
// the fail-fast path exercised: `DATASET_WRITER_BACKEND=iceberg` must not boot
// unless an Iceberg REST Catalog has been configured.

#[allow(dead_code, unused_imports)]
mod storage;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let backend = std::env::var("DATASET_WRITER_BACKEND")
        .map(|v| storage::WriterBackendKind::parse(&v))
        .unwrap_or(storage::WriterBackendKind::Legacy);
    let catalog_url = std::env::var("ICEBERG_CATALOG_URL").ok();
    let settings = storage::WriterSettings {
        backend,
        iceberg: storage::IcebergSettings {
            catalog_url,
            namespace: "dataset_service".to_string(),
        },
    };
    settings.validate()?;
    // Real wiring lands once the service grows an HTTP server.
    Ok(())
}
