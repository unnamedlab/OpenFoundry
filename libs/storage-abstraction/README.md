# storage-abstraction

Object storage abstraction layer used across OpenFoundry. The crate wraps
[`object_store`](https://docs.rs/object_store) (Apache-2.0) for local and
S3-compatible backends and, optionally, the Apache
[`iceberg`](https://docs.rs/iceberg) crate (Apache-2.0, "iceberg-rust")
for table-format access on top of any of those backends.

## Cargo features

| Feature   | Default | What it pulls in |
|-----------|---------|------------------|
| _none_    | ✓       | Just the `object_store`-based storage backend modules |
| `iceberg` | ✗       | The [`iceberg`] module: `iceberg = "0.9"` and `iceberg-catalog-rest = "0.9"` (both Apache-2.0) |

The `iceberg` feature is **off by default** so that services that only
need raw object I/O are not forced to pull in (and compile) the ~300
transitive crates required by `iceberg-rust`. Enable it explicitly per
service, e.g.:

```toml
[dependencies.storage-abstraction]
workspace = true
features = ["iceberg"]
```

## Iceberg integration

The [`iceberg`] module exposes a thin wrapper around `iceberg-rust` that
reuses the same underlying `object_store` configuration through Iceberg's
`FileIO` abstraction. The catalog (REST, in-memory, …) is responsible for
producing a `FileIO` that ultimately reads from / writes to the same
bucket or filesystem prefix used elsewhere in the service.

### Public API

```rust,ignore
pub struct IcebergTable {
    pub catalog: std::sync::Arc<dyn iceberg::Catalog>,
    pub identifier: iceberg::TableIdent,
    pub table: iceberg::table::Table,
}

impl IcebergTable {
    pub async fn load_table(
        catalog_url: &str,
        namespace: &[&str],
        name: &str,
    ) -> IcebergResult<Self>;

    pub async fn load_table_with_warehouse(
        catalog_url: &str,
        warehouse: &str,
        namespace: &[&str],
        name: &str,
    ) -> IcebergResult<Self>;

    pub async fn from_catalog(
        catalog: std::sync::Arc<dyn iceberg::Catalog>,
        identifier: iceberg::TableIdent,
    ) -> IcebergResult<Self>;

    pub async fn scan_to_record_batches(
        &self,
        predicate: Option<iceberg::expr::Predicate>,
        projection: Option<Vec<String>>,
    ) -> IcebergResult<Vec<arrow_array::RecordBatch>>;

    pub async fn append_record_batches(
        &mut self,
        batches: Vec<arrow_array::RecordBatch>,
    ) -> IcebergResult<()>;
}
```

### Example: load a table from a REST Catalog and read it

```rust,ignore
use storage_abstraction::iceberg::IcebergTable;

let table = IcebergTable::load_table(
    "http://localhost:8181", // the OpenFoundry-supported Iceberg REST Catalog (Lakekeeper — see ADR-0008)
    &["analytics"],
    "events",
)
.await?;

let batches = table.scan_to_record_batches(None, None).await?;
for batch in &batches {
    println!("got {} rows", batch.num_rows());
}
```

### Example: append Arrow `RecordBatch`es

```rust,ignore
use storage_abstraction::iceberg::IcebergTable;
use arrow_array::RecordBatch;

let mut table = IcebergTable::load_table("http://localhost:8181", &["ns"], "events").await?;
let batch: RecordBatch = /* build from arrow arrays */ unimplemented!();
table.append_record_batches(vec![batch]).await?;
```

`append_record_batches` writes the batches as Parquet data files via the
table's own `FileIO` and commits them with a `FastAppend` transaction,
producing a new snapshot.

### Backends

Anything that the chosen Iceberg catalog can produce a `FileIO` for is
supported, which in practice covers the same backends as the rest of
this crate:

* local filesystem (`file://`),
* S3 / MinIO (`s3://`),
* and the other `opendal`-backed schemes shipped by `iceberg-rust`.

## Versions and licensing

* `iceberg = "0.9"` &mdash; Apache-2.0
* `iceberg-catalog-rest = "0.9"` &mdash; Apache-2.0

Both versions were verified via `cargo search iceberg` at the time the
`iceberg` module was introduced and checked against the GitHub Advisory
Database (no advisories at the time of writing).
