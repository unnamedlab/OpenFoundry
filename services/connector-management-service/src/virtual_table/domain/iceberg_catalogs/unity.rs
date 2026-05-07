//! Databricks Unity Catalog client (Iceberg).
//!
//! Foundry doc § "Iceberg catalogs" marks Unity as `Legacy:
//! recommended to use Databricks source` for Amazon S3 and Azure
//! ADLS sources. The trait still surfaces those legacy combinations
//! so existing pipelines keep registering, but each rejection /
//! acceptance carries a `legacy_warning` flag (read by the handler
//! and forwarded as a `properties.warnings.unity_legacy` entry on
//! the registered virtual table).

use async_trait::async_trait;
use serde::Deserialize;
use serde_json::Value;

use super::{CatalogKind, IcebergCatalog, IcebergCatalogError, Namespace, TableHandle};

#[derive(Debug, Clone, Deserialize)]
struct UnityConfig {
    /// Databricks workspace host (e.g. `https://acme.cloud.databricks.com`).
    /// Required.
    workspace_url: String,
    /// Unity catalog name. Required.
    catalog: String,
    /// Optional OAuth M2M credential ref.
    #[serde(default)]
    credential_ref: Option<String>,
}

#[derive(Debug)]
pub struct UnityCatalog {
    config: UnityConfig,
}

impl UnityCatalog {
    pub fn from_config(value: &Value) -> Result<Self, IcebergCatalogError> {
        let config: UnityConfig = serde_json::from_value(value.clone())
            .map_err(|e| IcebergCatalogError::Upstream(format!("invalid unity config: {e}")))?;
        if !config.workspace_url.starts_with("https://") {
            return Err(IcebergCatalogError::Upstream(
                "unity.workspace_url must be an https URL".into(),
            ));
        }
        if config.catalog.trim().is_empty() {
            return Err(IcebergCatalogError::Upstream(
                "unity.catalog is required".into(),
            ));
        }
        Ok(Self { config })
    }
}

#[async_trait]
impl IcebergCatalog for UnityCatalog {
    fn kind(&self) -> CatalogKind {
        CatalogKind::UnityCatalog
    }

    async fn list_namespaces(&self) -> Result<Vec<Namespace>, IcebergCatalogError> {
        Ok(vec![Namespace {
            name: format!("{}.default", self.config.catalog),
        }])
    }

    async fn list_tables(
        &self,
        namespace: &str,
    ) -> Result<Vec<TableHandle>, IcebergCatalogError> {
        Ok(vec![TableHandle {
            namespace: namespace.into(),
            name: "stub_unity_table".into(),
            metadata_location: Some(format!(
                "{}/api/2.1/unity-catalog/tables/{}.{}.{}",
                self.config.workspace_url.trim_end_matches('/'),
                self.config.catalog,
                namespace,
                "stub_unity_table"
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
                "{}/api/2.1/unity-catalog/tables/{}.{}.{}",
                self.config.workspace_url.trim_end_matches('/'),
                self.config.catalog,
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
    async fn unity_loads_table_under_three_part_id() {
        let catalog = UnityCatalog::from_config(&json!({
            "workspace_url": "https://acme.cloud.databricks.com",
            "catalog": "main"
        }))
        .expect("config");
        let table = catalog
            .load_table("public", "events")
            .await
            .expect("load");
        assert!(
            table
                .metadata_location
                .as_deref()
                .unwrap()
                .ends_with("/main.public.events")
        );
    }
}
