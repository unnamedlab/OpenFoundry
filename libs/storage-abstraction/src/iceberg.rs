//! Apache Iceberg integration on top of any `object_store` backend.
//!
//! This module is enabled by the `iceberg` Cargo feature and is purely
//! additive: when the feature is disabled the rest of `storage-abstraction`
//! behaves exactly as before.
//!
//! It thinly wraps the official `iceberg` crate (`iceberg-rust`,
//! Apache-2.0). Iceberg's [`FileIO`] abstraction is what actually performs
//! I/O against the backing object store, so by reusing the same `FileIO`
//! that the catalog produces (or by building one from a URL pointing at
//! the same bucket / filesystem prefix used by [`crate::backend`]) we
//! transparently share the configured object store with Iceberg.
//!
//! # Example
//!
//! ```no_run
//! # #[cfg(feature = "iceberg")]
//! # async fn run() -> Result<(), Box<dyn std::error::Error>> {
//! use storage_abstraction::iceberg::IcebergTable;
//!
//! // Connect to the Lakekeeper Iceberg REST catalog (ADR-0008).
//! let table = IcebergTable::load_table(
//!     "http://localhost:8181",
//!     &["analytics"],
//!     "events",
//! )
//! .await?;
//!
//! // Read all rows as Arrow `RecordBatch`es.
//! let _batches = table.scan_to_record_batches(None, None).await?;
//! # Ok(())
//! # }
//! ```

use std::collections::HashMap;
use std::sync::Arc;

use arrow_array::RecordBatch;
use futures::TryStreamExt;
use iceberg::spec::DataFileFormat;
use iceberg::transaction::{ApplyTransactionAction, Transaction};
use iceberg::writer::base_writer::data_file_writer::DataFileWriterBuilder;
use iceberg::writer::file_writer::ParquetWriterBuilder;
use iceberg::writer::file_writer::location_generator::{
    DefaultFileNameGenerator, DefaultLocationGenerator,
};
use iceberg::writer::file_writer::rolling_writer::RollingFileWriterBuilder;
use iceberg::writer::{IcebergWriter, IcebergWriterBuilder};
use iceberg::{Catalog, NamespaceIdent, TableIdent};
use iceberg_catalog_rest::{
    REST_CATALOG_PROP_URI, REST_CATALOG_PROP_WAREHOUSE, RestCatalogBuilder,
};
use iceberg::CatalogBuilder;
use iceberg::expr::Predicate;
use iceberg::table::Table;
use parquet::file::properties::WriterProperties;

/// Errors returned by the Iceberg integration layer.
///
/// They wrap the underlying `iceberg::Error` so callers don't need to
/// depend on the iceberg crate directly to inspect them.
#[derive(Debug, thiserror::Error)]
pub enum IcebergError {
    /// An error reported by the underlying `iceberg` crate.
    #[error("iceberg error: {0}")]
    Iceberg(#[from] iceberg::Error),
}

/// Convenience result alias.
pub type IcebergResult<T> = Result<T, IcebergError>;

/// A handle to an Iceberg table loaded from a catalog.
///
/// `IcebergTable` owns the [`Catalog`] handle it was loaded from, the
/// fully-qualified [`TableIdent`] and a snapshot of the [`Table`] metadata
/// at load time. Operations that mutate the table refresh the snapshot
/// after committing.
pub struct IcebergTable {
    /// Catalog the table was loaded from. Kept around so that mutating
    /// operations (such as `append_record_batches`) can commit
    /// transactions back to it.
    pub catalog: Arc<dyn Catalog>,
    /// Fully-qualified table identifier (`namespace.name`).
    pub identifier: TableIdent,
    /// Cached `iceberg::table::Table` snapshot.
    pub table: Table,
}

impl IcebergTable {
    /// Build an [`IcebergTable`] from an already-constructed catalog and
    /// table identifier. Prefer [`IcebergTable::load_table`] for the
    /// common REST catalog case.
    pub async fn from_catalog(
        catalog: Arc<dyn Catalog>,
        identifier: TableIdent,
    ) -> IcebergResult<Self> {
        let table = catalog.load_table(&identifier).await?;
        Ok(Self {
            catalog,
            identifier,
            table,
        })
    }

    /// Load a table from an Iceberg REST Catalog.
    ///
    /// `catalog_url` is the base URL of the REST catalog, e.g.
    /// `http://localhost:8181`. `namespace` is the namespace path (each
    /// element becomes one level), and `name` is the table name within
    /// that namespace.
    ///
    /// The catalog determines the warehouse location and propagates its
    /// configuration to a [`FileIO`] that ultimately drives the same
    /// underlying object store used by the rest of `storage-abstraction`.
    pub async fn load_table(
        catalog_url: &str,
        namespace: &[&str],
        name: &str,
    ) -> IcebergResult<Self> {
        let props = HashMap::from([(
            REST_CATALOG_PROP_URI.to_string(),
            catalog_url.to_string(),
        )]);
        let catalog = RestCatalogBuilder::default().load("rest", props).await?;
        let catalog: Arc<dyn Catalog> = Arc::new(catalog);

        let namespace_ident = NamespaceIdent::from_strs(namespace.iter().copied())?;
        let identifier = TableIdent::new(namespace_ident, name.to_string());
        Self::from_catalog(catalog, identifier).await
    }

    /// Load a table from an Iceberg REST Catalog, additionally pinning a
    /// warehouse identifier (mirrors `REST_CATALOG_PROP_WAREHOUSE`).
    pub async fn load_table_with_warehouse(
        catalog_url: &str,
        warehouse: &str,
        namespace: &[&str],
        name: &str,
    ) -> IcebergResult<Self> {
        let props = HashMap::from([
            (REST_CATALOG_PROP_URI.to_string(), catalog_url.to_string()),
            (
                REST_CATALOG_PROP_WAREHOUSE.to_string(),
                warehouse.to_string(),
            ),
        ]);
        let catalog = RestCatalogBuilder::default().load("rest", props).await?;
        let catalog: Arc<dyn Catalog> = Arc::new(catalog);

        let namespace_ident = NamespaceIdent::from_strs(namespace.iter().copied())?;
        let identifier = TableIdent::new(namespace_ident, name.to_string());
        Self::from_catalog(catalog, identifier).await
    }

    /// Scan the table and collect the matching rows as Arrow
    /// [`RecordBatch`]es.
    ///
    /// * `predicate` — optional row filter, applied at scan time.
    /// * `projection` — optional list of column names to project. When
    ///   `None`, every column is selected.
    pub async fn scan_to_record_batches(
        &self,
        predicate: Option<Predicate>,
        projection: Option<Vec<String>>,
    ) -> IcebergResult<Vec<RecordBatch>> {
        let mut builder = self.table.scan();
        if let Some(p) = predicate {
            builder = builder.with_filter(p);
        }
        builder = match projection {
            Some(cols) if !cols.is_empty() => builder.select(cols),
            _ => builder.select_all(),
        };
        let scan = builder.build()?;
        let stream = scan.to_arrow().await?;
        let batches: Vec<RecordBatch> = stream.try_collect().await?;
        Ok(batches)
    }

    /// Append a list of Arrow [`RecordBatch`]es to the table as a new
    /// snapshot.
    ///
    /// The batches are written as Parquet data files under the table
    /// location (using the table's [`FileIO`], i.e. its underlying
    /// object store) and committed via a `FastAppend` transaction.
    /// On success, the cached [`Table`] snapshot is refreshed.
    pub async fn append_record_batches(
        &mut self,
        batches: Vec<RecordBatch>,
    ) -> IcebergResult<()> {
        if batches.is_empty() {
            return Ok(());
        }

        // Build a Parquet -> rolling -> data-file writer using the
        // table's own FileIO (which is configured by the catalog from
        // the same object_store backend).
        let location_generator = DefaultLocationGenerator::new(self.table.metadata().clone())?;
        let file_name_generator = DefaultFileNameGenerator::new(
            "data".to_string(),
            None,
            DataFileFormat::Parquet,
        );
        let parquet_builder = ParquetWriterBuilder::new(
            WriterProperties::default(),
            self.table.metadata().current_schema().clone(),
        );
        let rolling_builder = RollingFileWriterBuilder::new_with_default_file_size(
            parquet_builder,
            self.table.file_io().clone(),
            location_generator,
            file_name_generator,
        );
        let data_file_builder = DataFileWriterBuilder::new(rolling_builder);
        let mut writer = data_file_builder.build(None).await?;

        for batch in batches {
            writer.write(batch).await?;
        }
        let data_files = writer.close().await?;

        // Commit a fast-append transaction adding all the new data files.
        let tx = Transaction::new(&self.table);
        let action = tx.fast_append().add_data_files(data_files);
        let tx = action.apply(tx)?;
        let new_table = tx.commit(self.catalog.as_ref()).await?;
        self.table = new_table;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;
    use std::sync::Arc;

    use arrow_array::{Int32Array, RecordBatch, StringArray};
    use arrow_schema::{
        DataType as ArrowDataType, Field as ArrowField, Schema as ArrowSchema,
    };
    use iceberg::memory::{MEMORY_CATALOG_WAREHOUSE, MemoryCatalogBuilder};
    use iceberg::spec::{NestedField, PrimitiveType, Schema, Type};
    use iceberg::{Catalog, CatalogBuilder, NamespaceIdent, TableCreation, TableIdent};
    use tempfile::TempDir;

    use super::*;

    fn iceberg_schema() -> Schema {
        Schema::builder()
            .with_schema_id(0)
            .with_fields(vec![
                NestedField::required(1, "id", Type::Primitive(PrimitiveType::Int)).into(),
                NestedField::required(2, "name", Type::Primitive(PrimitiveType::String)).into(),
            ])
            .build()
            .unwrap()
    }

    fn arrow_schema_for_table() -> ArrowSchema {
        // Iceberg writes Arrow files with the field-id metadata key set on
        // each field. Build an Arrow schema that matches what the
        // `ParquetWriterBuilder` expects.
        let id_meta =
            HashMap::from([("PARQUET:field_id".to_string(), "1".to_string())]);
        let name_meta =
            HashMap::from([("PARQUET:field_id".to_string(), "2".to_string())]);
        ArrowSchema::new(vec![
            ArrowField::new("id", ArrowDataType::Int32, false).with_metadata(id_meta),
            ArrowField::new("name", ArrowDataType::Utf8, false).with_metadata(name_meta),
        ])
    }

    #[tokio::test]
    async fn create_append_and_scan_roundtrip() {
        // Spin up an in-memory Iceberg catalog backed by a temporary
        // local-filesystem warehouse. This validates the same code paths
        // that a REST catalog would exercise without requiring network.
        let warehouse = TempDir::new().unwrap();
        let warehouse_url = format!("file://{}", warehouse.path().to_string_lossy());
        let catalog = MemoryCatalogBuilder::default()
            .load(
                "memory",
                HashMap::from([(MEMORY_CATALOG_WAREHOUSE.to_string(), warehouse_url)]),
            )
            .await
            .unwrap();
        let catalog: Arc<dyn Catalog> = Arc::new(catalog);

        // Create namespace + table.
        let namespace_ident = NamespaceIdent::new("ns".to_string());
        catalog
            .create_namespace(&namespace_ident, HashMap::new())
            .await
            .unwrap();
        let table_ident = TableIdent::new(namespace_ident.clone(), "events".to_string());
        catalog
            .create_table(
                &namespace_ident,
                TableCreation::builder()
                    .name(table_ident.name().to_string())
                    .schema(iceberg_schema())
                    .build(),
            )
            .await
            .unwrap();

        // Wrap in IcebergTable and round-trip a RecordBatch through it.
        let mut table = IcebergTable::from_catalog(catalog, table_ident)
            .await
            .unwrap();

        let arrow_schema = Arc::new(arrow_schema_for_table());
        let batch = RecordBatch::try_new(
            arrow_schema,
            vec![
                Arc::new(Int32Array::from(vec![1, 2, 3])),
                Arc::new(StringArray::from(vec!["a", "b", "c"])),
            ],
        )
        .unwrap();

        table
            .append_record_batches(vec![batch.clone()])
            .await
            .expect("append should succeed");

        let read_back = table
            .scan_to_record_batches(None, None)
            .await
            .expect("scan should succeed");

        let total_rows: usize = read_back.iter().map(|b| b.num_rows()).sum();
        assert_eq!(total_rows, 3, "expected 3 rows, got {total_rows}");
    }
}
