//! "Object Storage" Iceberg catalog — files-only, metadata.json
//! discovered directly under a prefix in S3 / GCS / ABFS.
//!
//! No external service is involved: the metadata pointer is the
//! `metadata/v*.metadata.json` file at the configured prefix. P2.next
//! will read the metadata via `object_store` and parse the table
//! schema; P2 ships the trait + config validation.

use async_trait::async_trait;
use serde::Deserialize;
use serde_json::Value;

use super::{CatalogKind, IcebergCatalog, IcebergCatalogError, Namespace, TableHandle};

#[derive(Debug, Clone, Deserialize)]
struct ObjectStorageConfig {
    /// Bucket / container / filesystem name. Required.
    bucket: String,
    /// Optional namespace prefix inside the bucket; defaults to "".
    #[serde(default)]
    prefix: Option<String>,
    /// Object-store scheme: `s3` | `gs` | `abfs`. Required.
    scheme: String,
}

#[derive(Debug)]
pub struct ObjectStorageCatalog {
    config: ObjectStorageConfig,
}

impl ObjectStorageCatalog {
    pub fn from_config(value: &Value) -> Result<Self, IcebergCatalogError> {
        let config: ObjectStorageConfig = serde_json::from_value(value.clone())
            .map_err(|e| IcebergCatalogError::Upstream(format!("invalid object_storage config: {e}")))?;
        if config.bucket.trim().is_empty() {
            return Err(IcebergCatalogError::Upstream(
                "object_storage.bucket is required".into(),
            ));
        }
        if !matches!(config.scheme.as_str(), "s3" | "gs" | "abfs") {
            return Err(IcebergCatalogError::Upstream(
                "object_storage.scheme must be one of s3, gs, abfs".into(),
            ));
        }
        Ok(Self { config })
    }

    fn root_uri(&self) -> String {
        let prefix = self.config.prefix.clone().unwrap_or_default();
        let trimmed = prefix.trim_matches('/');
        if trimmed.is_empty() {
            format!("{}://{}", self.config.scheme, self.config.bucket)
        } else {
            format!("{}://{}/{}", self.config.scheme, self.config.bucket, trimmed)
        }
    }
}

#[async_trait]
impl IcebergCatalog for ObjectStorageCatalog {
    fn kind(&self) -> CatalogKind {
        CatalogKind::ObjectStorage
    }

    async fn list_namespaces(&self) -> Result<Vec<Namespace>, IcebergCatalogError> {
        // For "object storage" catalogs the namespace is the prefix
        // under which `metadata/` lives. We surface a single virtual
        // namespace named after the prefix (or "default" at the root).
        Ok(vec![Namespace {
            name: self
                .config
                .prefix
                .clone()
                .unwrap_or_else(|| "default".into()),
        }])
    }

    async fn list_tables(
        &self,
        namespace: &str,
    ) -> Result<Vec<TableHandle>, IcebergCatalogError> {
        Ok(vec![TableHandle {
            namespace: namespace.into(),
            name: "stub_object_storage_table".into(),
            metadata_location: Some(format!(
                "{}/{}/stub_object_storage_table/metadata/v1.metadata.json",
                self.root_uri(),
                namespace
            )),
        }])
    }

    async fn load_table(
        &self,
        namespace: &str,
        table: &str,
    ) -> Result<TableHandle, IcebergCatalogError> {
        Ok(TableHandle {
            namespace: namespace.into(),
            name: table.into(),
            metadata_location: Some(format!(
                "{}/{}/{}/metadata/v1.metadata.json",
                self.root_uri(),
                namespace,
                table,
            )),
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[tokio::test]
    async fn object_storage_loads_table_metadata_pointer() {
        let catalog = ObjectStorageCatalog::from_config(&json!({
            "scheme": "s3",
            "bucket": "openfoundry",
            "prefix": "warehouse/iceberg"
        }))
        .expect("config");
        let table = catalog
            .load_table("sales", "events")
            .await
            .expect("load");
        assert_eq!(
            table.metadata_location.as_deref(),
            Some("s3://openfoundry/warehouse/iceberg/sales/events/metadata/v1.metadata.json")
        );
    }

    #[test]
    fn object_storage_rejects_unknown_scheme() {
        let err = ObjectStorageCatalog::from_config(&json!({
            "scheme": "ftp",
            "bucket": "x"
        }))
        .expect_err("must reject");
        assert!(matches!(err, IcebergCatalogError::Upstream(_)));
    }
}
