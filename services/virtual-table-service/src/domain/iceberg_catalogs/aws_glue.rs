//! AWS Glue Iceberg catalog client.
//!
//! Real-world implementation needs `aws-sdk-glue` behind the
//! `provider-iceberg` cargo feature. The default-build body returns
//! deterministic stubs derived from the configured database so the
//! integration tests exercise the trait wiring without spinning up
//! AWS credentials.

use async_trait::async_trait;
use serde::Deserialize;
use serde_json::Value;

use super::{CatalogKind, IcebergCatalog, IcebergCatalogError, Namespace, TableHandle};

#[derive(Debug, Clone, Deserialize)]
struct GlueConfig {
    /// AWS region the Glue catalog lives in (e.g. `eu-west-1`). Required.
    region: String,
    /// Optional Glue database scope. When absent the client lists all
    /// databases in the catalog.
    #[serde(default)]
    database: Option<String>,
    /// Optional explicit Glue catalog id (for multi-account setups).
    #[serde(default)]
    catalog_id: Option<String>,
}

#[derive(Debug)]
pub struct AwsGlueCatalog {
    config: GlueConfig,
}

impl AwsGlueCatalog {
    pub fn from_config(value: &Value) -> Result<Self, IcebergCatalogError> {
        let config: GlueConfig = serde_json::from_value(value.clone())
            .map_err(|e| IcebergCatalogError::Upstream(format!("invalid glue config: {e}")))?;
        if config.region.trim().is_empty() {
            return Err(IcebergCatalogError::Upstream("glue.region is required".into()));
        }
        Ok(Self { config })
    }
}

#[async_trait]
impl IcebergCatalog for AwsGlueCatalog {
    fn kind(&self) -> CatalogKind {
        CatalogKind::AwsGlue
    }

    async fn list_namespaces(&self) -> Result<Vec<Namespace>, IcebergCatalogError> {
        Ok(match self.config.database.as_ref() {
            Some(db) => vec![Namespace { name: db.clone() }],
            None => vec![Namespace {
                name: format!("glue.{}", self.config.region),
            }],
        })
    }

    async fn list_tables(
        &self,
        namespace: &str,
    ) -> Result<Vec<TableHandle>, IcebergCatalogError> {
        Ok(vec![TableHandle {
            namespace: namespace.into(),
            name: "stub_glue_table".into(),
            metadata_location: Some(format!(
                "s3://glue-{}/{}/stub_glue_table/metadata/v1.metadata.json",
                self.config.region, namespace
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
                "s3://glue-{}/{}/{}/metadata/v1.metadata.json",
                self.config.region, namespace, table
            )),
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[tokio::test]
    async fn glue_lists_namespaces_and_tables() {
        let catalog = AwsGlueCatalog::from_config(&json!({
            "region": "eu-west-1",
            "database": "analytics"
        }))
        .expect("config");
        let ns = catalog.list_namespaces().await.expect("list");
        assert_eq!(ns.len(), 1);
        assert_eq!(ns[0].name, "analytics");
        let tables = catalog.list_tables("analytics").await.expect("tables");
        assert_eq!(tables[0].namespace, "analytics");
        assert!(tables[0].metadata_location.as_deref().unwrap().contains("eu-west-1"));
    }

    #[test]
    fn glue_requires_region() {
        let err = AwsGlueCatalog::from_config(&json!({})).expect_err("missing region");
        assert!(matches!(err, IcebergCatalogError::Upstream(_)));
    }
}
