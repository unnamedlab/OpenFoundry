//! Snowflake Horizon Catalog REST client (Iceberg).
//!
//! Horizon exposes the Iceberg REST Catalog spec. P2 stubs the wire
//! calls; P2.next swaps the body for an actual `reqwest` round trip
//! against the Horizon endpoint.

use async_trait::async_trait;
use serde::Deserialize;
use serde_json::Value;

use super::{CatalogKind, IcebergCatalog, IcebergCatalogError, Namespace, TableHandle};

#[derive(Debug, Clone, Deserialize)]
struct HorizonConfig {
    /// Snowflake Horizon REST endpoint (e.g.
    /// `https://acme.snowflakecomputing.com/api/v2/iceberg`). Required.
    endpoint: String,
    /// Snowflake account locator. Optional — only needed for the
    /// REST handshake; the trait body stubs it.
    #[serde(default)]
    account: Option<String>,
    /// Iceberg warehouse identifier (Snowflake calls this the
    /// "external volume").
    #[serde(default)]
    warehouse: Option<String>,
}

#[derive(Debug)]
pub struct HorizonCatalog {
    config: HorizonConfig,
}

impl HorizonCatalog {
    pub fn from_config(value: &Value) -> Result<Self, IcebergCatalogError> {
        let config: HorizonConfig = serde_json::from_value(value.clone())
            .map_err(|e| IcebergCatalogError::Upstream(format!("invalid horizon config: {e}")))?;
        if !config.endpoint.starts_with("https://") {
            return Err(IcebergCatalogError::Upstream(
                "horizon.endpoint must be an https URL".into(),
            ));
        }
        Ok(Self { config })
    }
}

#[async_trait]
impl IcebergCatalog for HorizonCatalog {
    fn kind(&self) -> CatalogKind {
        CatalogKind::Horizon
    }

    async fn list_namespaces(&self) -> Result<Vec<Namespace>, IcebergCatalogError> {
        Ok(vec![Namespace {
            name: self
                .config
                .warehouse
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
            name: "stub_horizon_table".into(),
            metadata_location: Some(format!(
                "{}/v1/{}/tables/stub_horizon_table/metadata.json",
                self.config.endpoint.trim_end_matches('/'),
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
                "{}/v1/{}/tables/{}/metadata.json",
                self.config.endpoint.trim_end_matches('/'),
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

    #[test]
    fn horizon_rejects_non_https_endpoint() {
        let err = HorizonCatalog::from_config(&json!({"endpoint": "http://insecure"}))
            .expect_err("must reject");
        assert!(matches!(err, IcebergCatalogError::Upstream(_)));
    }

    #[tokio::test]
    async fn horizon_returns_warehouse_namespace() {
        let catalog = HorizonCatalog::from_config(&json!({
            "endpoint": "https://acme.snowflakecomputing.com/api/v2/iceberg",
            "warehouse": "ANALYTICS"
        }))
        .expect("config");
        let ns = catalog.list_namespaces().await.expect("list");
        assert_eq!(ns[0].name, "ANALYTICS");
    }
}
