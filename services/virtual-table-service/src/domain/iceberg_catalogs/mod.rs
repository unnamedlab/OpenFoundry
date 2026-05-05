//! Apache Iceberg catalog integrations for virtual tables.
//!
//! Source of truth for the compatibility table:
//!   docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/
//!   Core concepts/Virtual tables.md § "Iceberg catalogs"
//!
//! Five catalog kinds appear in the doc — AWS Glue, Horizon, Object
//! Storage, Polaris, Unity. Each provider × catalog combination is
//! either GA, Legacy ("recommended to use Databricks source" for
//! S3+Unity and ABFS+Unity) or Not available. [`compatibility`] gives
//! the matrix as a closed function so the integration tests can
//! enforce parity with the doc.
//!
//! Each kind has its own submodule that implements the
//! [`IcebergCatalog`] trait. P2 stubs keep the network call shape
//! testable without live infra; P2.next plugs in real SDK clients
//! behind the `provider-iceberg` cargo feature.

pub mod aws_glue;
pub mod horizon;
pub mod object_storage;
pub mod polaris;
pub mod unity;

use async_trait::async_trait;
use serde::{Deserialize, Serialize};

use crate::domain::capability_matrix::SourceProvider;

/// Catalog kind, aligned 1:1 with the `iceberg_catalog_kind` CHECK
/// constraint on `virtual_table_sources_link`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum CatalogKind {
    AwsGlue,
    Horizon,
    ObjectStorage,
    Polaris,
    UnityCatalog,
}

impl CatalogKind {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::AwsGlue => "AWS_GLUE",
            Self::Horizon => "HORIZON",
            Self::ObjectStorage => "OBJECT_STORAGE",
            Self::Polaris => "POLARIS",
            Self::UnityCatalog => "UNITY_CATALOG",
        }
    }

    pub fn parse(value: &str) -> Option<Self> {
        match value {
            "AWS_GLUE" => Some(Self::AwsGlue),
            "HORIZON" => Some(Self::Horizon),
            "OBJECT_STORAGE" => Some(Self::ObjectStorage),
            "POLARIS" => Some(Self::Polaris),
            "UNITY_CATALOG" => Some(Self::UnityCatalog),
            _ => None,
        }
    }
}

/// Compatibility status for a (provider, catalog) cell. Mirrors the
/// rotting-fruit emoji vocabulary in the Foundry doc.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum CompatibilityStatus {
    GenerallyAvailable,
    Legacy,
    NotAvailable,
}

impl CompatibilityStatus {
    pub fn is_supported(self) -> bool {
        matches!(self, Self::GenerallyAvailable | Self::Legacy)
    }
}

/// Look up the compatibility status for a (provider, catalog kind)
/// pair. Cells absent from the doc default to [`NotAvailable`] —
/// failing closed is safer than auto-blessing a combination Foundry
/// did not vouch for.
pub fn compatibility(provider: SourceProvider, catalog: CatalogKind) -> CompatibilityStatus {
    use CatalogKind::*;
    use CompatibilityStatus::*;
    use SourceProvider::*;
    match (provider, catalog) {
        (AmazonS3, AwsGlue) => GenerallyAvailable,
        (AmazonS3, Horizon) => NotAvailable,
        (AmazonS3, ObjectStorage) => GenerallyAvailable,
        (AmazonS3, Polaris) => GenerallyAvailable,
        (AmazonS3, UnityCatalog) => Legacy,
        (Databricks, UnityCatalog) => GenerallyAvailable,
        // Databricks is documented for Unity only; everything else is N/A.
        (Databricks, _) => NotAvailable,
        (Gcs, ObjectStorage) => GenerallyAvailable,
        (Gcs, _) => NotAvailable,
        (AzureAbfs, ObjectStorage) => GenerallyAvailable,
        (AzureAbfs, Polaris) => GenerallyAvailable,
        (AzureAbfs, UnityCatalog) => Legacy,
        (AzureAbfs, _) => NotAvailable,
        (Snowflake, Horizon) => GenerallyAvailable,
        (Snowflake, _) => NotAvailable,
        // Foundry-as-source can only host its own managed Iceberg, so
        // it does not need a third-party catalog. Treat as N/A.
        (FoundryIceberg, _) => NotAvailable,
        // BigQuery does not support virtual tables backed by Iceberg
        // in the doc (only Tables/Views/Materialized Views).
        (BigQuery, _) => NotAvailable,
    }
}

/// Iterate every (provider, catalog) cell. Used by the doc-conformance
/// integration test.
pub fn iter_cells() -> impl Iterator<Item = (SourceProvider, CatalogKind, CompatibilityStatus)> {
    const PROVIDERS: [SourceProvider; 7] = [
        SourceProvider::AmazonS3,
        SourceProvider::AzureAbfs,
        SourceProvider::BigQuery,
        SourceProvider::Databricks,
        SourceProvider::FoundryIceberg,
        SourceProvider::Gcs,
        SourceProvider::Snowflake,
    ];
    const CATALOGS: [CatalogKind; 5] = [
        CatalogKind::AwsGlue,
        CatalogKind::Horizon,
        CatalogKind::ObjectStorage,
        CatalogKind::Polaris,
        CatalogKind::UnityCatalog,
    ];
    PROVIDERS.iter().copied().flat_map(|p| {
        CATALOGS
            .iter()
            .copied()
            .map(move |c| (p, c, compatibility(p, c)))
    })
}

/// Catalog-level resource handles. Kept narrow on purpose — only what
/// the registration / discover paths actually need.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct Namespace {
    pub name: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct TableHandle {
    pub namespace: String,
    pub name: String,
    pub metadata_location: Option<String>,
}

#[derive(Debug, thiserror::Error)]
pub enum IcebergCatalogError {
    #[error("catalog not configured for source")]
    NotConfigured,
    #[error("catalog kind is not compatible with this source: {provider:?} × {catalog:?}")]
    Incompatible {
        provider: SourceProvider,
        catalog: CatalogKind,
    },
    #[error("upstream error: {0}")]
    Upstream(String),
}

#[async_trait]
pub trait IcebergCatalog: Send + Sync {
    fn kind(&self) -> CatalogKind;

    async fn list_namespaces(&self) -> Result<Vec<Namespace>, IcebergCatalogError>;

    async fn list_tables(
        &self,
        namespace: &str,
    ) -> Result<Vec<TableHandle>, IcebergCatalogError>;

    async fn load_table(
        &self,
        namespace: &str,
        table: &str,
    ) -> Result<TableHandle, IcebergCatalogError>;
}

/// Build the catalog implementation for a configured source. The
/// `config` blob shape is catalog-specific — see each submodule for
/// the keys it understands.
pub fn build_catalog(
    provider: SourceProvider,
    catalog: CatalogKind,
    config: &serde_json::Value,
) -> Result<Box<dyn IcebergCatalog>, IcebergCatalogError> {
    if !compatibility(provider, catalog).is_supported() {
        return Err(IcebergCatalogError::Incompatible { provider, catalog });
    }
    Ok(match catalog {
        CatalogKind::AwsGlue => Box::new(aws_glue::AwsGlueCatalog::from_config(config)?),
        CatalogKind::Horizon => Box::new(horizon::HorizonCatalog::from_config(config)?),
        CatalogKind::ObjectStorage => Box::new(object_storage::ObjectStorageCatalog::from_config(config)?),
        CatalogKind::Polaris => Box::new(polaris::PolarisCatalog::from_config(config)?),
        CatalogKind::UnityCatalog => Box::new(unity::UnityCatalog::from_config(config)?),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn supported_status_helpers() {
        assert!(CompatibilityStatus::GenerallyAvailable.is_supported());
        assert!(CompatibilityStatus::Legacy.is_supported());
        assert!(!CompatibilityStatus::NotAvailable.is_supported());
    }

    #[test]
    fn round_trip_kind_str() {
        for kind in [
            CatalogKind::AwsGlue,
            CatalogKind::Horizon,
            CatalogKind::ObjectStorage,
            CatalogKind::Polaris,
            CatalogKind::UnityCatalog,
        ] {
            assert_eq!(CatalogKind::parse(kind.as_str()), Some(kind));
        }
    }
}
