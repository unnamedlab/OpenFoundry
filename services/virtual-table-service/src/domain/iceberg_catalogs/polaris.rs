//! Polaris (Apache Iceberg REST Catalog) client.
//!
//! The Polaris REST contract follows the open-source Iceberg REST
//! Catalog spec verbatim, so this stub will eventually delegate to
//! `libs/storage-abstraction`'s iceberg-rest client (P2.next, behind
//! the `provider-iceberg` feature).

use async_trait::async_trait;
use serde::Deserialize;
use serde_json::Value;

use super::{CatalogKind, IcebergCatalog, IcebergCatalogError, Namespace, TableHandle};

#[derive(Debug, Clone, Deserialize)]
struct PolarisConfig {
    /// REST catalog endpoint (e.g. `https://polaris.example.com/api/catalog`).
    /// Required.
    endpoint: String,
    /// Polaris realm / catalog name. Required.
    catalog: String,
    /// Optional OAuth credential ref (resolved from the secret manager
    /// at runtime; opaque to the trait).
    #[serde(default)]
    credential_ref: Option<String>,
}

#[derive(Debug)]
pub struct PolarisCatalog {
    config: PolarisConfig,
}

impl PolarisCatalog {
    pub fn from_config(value: &Value) -> Result<Self, IcebergCatalogError> {
        let config: PolarisConfig = serde_json::from_value(value.clone())
            .map_err(|e| IcebergCatalogError::Upstream(format!("invalid polaris config: {e}")))?;
        if !config.endpoint.starts_with("https://") {
            return Err(IcebergCatalogError::Upstream(
                "polaris.endpoint must be an https URL".into(),
            ));
        }
        if config.catalog.trim().is_empty() {
            return Err(IcebergCatalogError::Upstream(
                "polaris.catalog is required".into(),
            ));
        }
        Ok(Self { config })
    }
}

#[async_trait]
impl IcebergCatalog for PolarisCatalog {
    fn kind(&self) -> CatalogKind {
        CatalogKind::Polaris
    }

    async fn list_namespaces(&self) -> Result<Vec<Namespace>, IcebergCatalogError> {
        Ok(vec![
            Namespace {
                name: "default".into(),
            },
            Namespace {
                name: format!("{}.public", self.config.catalog),
            },
        ])
    }

    async fn list_tables(
        &self,
        namespace: &str,
    ) -> Result<Vec<TableHandle>, IcebergCatalogError> {
        Ok(vec![TableHandle {
            namespace: namespace.into(),
            name: "stub_polaris_table".into(),
            metadata_location: Some(format!(
                "{}/v1/{}/namespaces/{}/tables/stub_polaris_table",
                self.config.endpoint.trim_end_matches('/'),
                self.config.catalog,
                namespace,
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
                "{}/v1/{}/namespaces/{}/tables/{}",
                self.config.endpoint.trim_end_matches('/'),
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
    async fn polaris_lists_default_namespace() {
        let catalog = PolarisCatalog::from_config(&json!({
            "endpoint": "https://polaris.example.com/api/catalog",
            "catalog": "main"
        }))
        .expect("config");
        let namespaces = catalog.list_namespaces().await.expect("list");
        assert!(namespaces.iter().any(|n| n.name == "default"));
        assert!(namespaces.iter().any(|n| n.name == "main.public"));
    }

    #[test]
    fn polaris_requires_https_and_catalog() {
        assert!(matches!(
            PolarisCatalog::from_config(&json!({"endpoint": "http://x", "catalog": "c"}))
                .expect_err("must reject"),
            IcebergCatalogError::Upstream(_)
        ));
        assert!(matches!(
            PolarisCatalog::from_config(&json!({"endpoint": "https://x", "catalog": ""}))
                .expect_err("must reject"),
            IcebergCatalogError::Upstream(_)
        ));
    }
}
