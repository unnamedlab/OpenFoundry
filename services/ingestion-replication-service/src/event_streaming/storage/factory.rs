//! Selects the [`DatasetWriter`] implementation at startup based on runtime
//! configuration, with graceful degradation when Iceberg is requested but no
//! REST Catalog endpoint has been provided.

use std::sync::Arc;

use storage_abstraction::StorageBackend;

use super::iceberg::{IcebergDatasetWriter, InMemoryCatalog, RestCatalogClient};
use super::legacy::LegacyDatasetWriter;
use super::writer::DatasetWriter;

/// Which writer to materialize at startup.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum WriterBackendKind {
    /// Pre-Iceberg behaviour. Default for safety / rollback.
    Legacy,
    /// New behaviour: append to an Iceberg table via REST Catalog.
    Iceberg,
}

impl WriterBackendKind {
    pub fn parse(raw: &str) -> Self {
        match raw.trim().to_ascii_lowercase().as_str() {
            "iceberg" => Self::Iceberg,
            _ => Self::Legacy,
        }
    }
}

/// Iceberg-specific runtime settings.
#[derive(Debug, Clone)]
pub struct IcebergSettings {
    /// REST Catalog endpoint, e.g. `http://iceberg-catalog:8181`. When `None`
    /// the factory falls back to the legacy writer with a warning log.
    pub catalog_url: Option<String>,
    /// Catalog namespace this service writes into. For
    /// `event-streaming-service` this is `streaming_service`.
    pub namespace: String,
}

/// Aggregated writer settings.
#[derive(Debug, Clone)]
pub struct WriterSettings {
    pub backend: WriterBackendKind,
    pub iceberg: IcebergSettings,
}

/// Build the configured writer.
///
/// * If `backend == Legacy`, returns the legacy writer wrapping `storage`.
/// * If `backend == Iceberg` and `iceberg.catalog_url` is set, returns the
///   Iceberg writer talking to the REST Catalog at that URL.
/// * If `backend == Iceberg` but `iceberg.catalog_url` is `None`, logs a
///   warning and falls back to the legacy writer. This is the documented
///   "graceful degradation" path so the service still starts.
pub fn build_dataset_writer(
    storage: Arc<dyn StorageBackend>,
    settings: &WriterSettings,
) -> Arc<dyn DatasetWriter> {
    match settings.backend {
        WriterBackendKind::Legacy => {
            tracing::info!(
                namespace = %settings.iceberg.namespace,
                "dataset writer: using legacy backend"
            );
            Arc::new(LegacyDatasetWriter::new(
                storage,
                settings.iceberg.namespace.clone(),
            ))
        }
        WriterBackendKind::Iceberg => match &settings.iceberg.catalog_url {
            Some(url) if !url.trim().is_empty() => {
                tracing::info!(
                    namespace = %settings.iceberg.namespace,
                    catalog_url = %url,
                    "dataset writer: using Iceberg backend"
                );
                let catalog = Arc::new(RestCatalogClient::new(url.clone()));
                Arc::new(IcebergDatasetWriter::new(
                    storage,
                    catalog,
                    settings.iceberg.namespace.clone(),
                ))
            }
            _ => {
                tracing::warn!(
                    namespace = %settings.iceberg.namespace,
                    "ICEBERG_CATALOG_URL is not configured; falling back to legacy dataset writer"
                );
                Arc::new(LegacyDatasetWriter::new(
                    storage,
                    settings.iceberg.namespace.clone(),
                ))
            }
        },
    }
}

/// Variant of [`build_dataset_writer`] that uses an [`InMemoryCatalog`] when
/// Iceberg is requested. Intended for local development and integration tests
/// where no real REST Catalog is available.
pub fn build_dataset_writer_with_in_memory_catalog(
    storage: Arc<dyn StorageBackend>,
    settings: &WriterSettings,
) -> Arc<dyn DatasetWriter> {
    if settings.backend == WriterBackendKind::Iceberg {
        let catalog = Arc::new(InMemoryCatalog::new());
        return Arc::new(IcebergDatasetWriter::new(
            storage,
            catalog,
            settings.iceberg.namespace.clone(),
        ));
    }
    build_dataset_writer(storage, settings)
}

#[cfg(test)]
mod tests {
    use super::*;
    use storage_abstraction::local::LocalStorage;

    fn storage() -> (tempfile::TempDir, Arc<dyn StorageBackend>) {
        let dir = tempfile::tempdir().unwrap();
        let store = LocalStorage::new(dir.path().to_str().unwrap()).unwrap();
        (dir, Arc::new(store))
    }

    #[test]
    fn parse_backend_defaults_to_legacy() {
        assert_eq!(WriterBackendKind::parse(""), WriterBackendKind::Legacy);
        assert_eq!(
            WriterBackendKind::parse("legacy"),
            WriterBackendKind::Legacy
        );
        assert_eq!(
            WriterBackendKind::parse("anything"),
            WriterBackendKind::Legacy
        );
        assert_eq!(
            WriterBackendKind::parse("Iceberg"),
            WriterBackendKind::Iceberg
        );
        assert_eq!(
            WriterBackendKind::parse("ICEBERG"),
            WriterBackendKind::Iceberg
        );
    }

    #[test]
    fn iceberg_without_url_falls_back_to_legacy() {
        let (_g, s) = storage();
        let writer = build_dataset_writer(
            s,
            &WriterSettings {
                backend: WriterBackendKind::Iceberg,
                iceberg: IcebergSettings {
                    catalog_url: None,
                    namespace: "streaming_service".to_string(),
                },
            },
        );
        assert_eq!(writer.backend_name(), "legacy");
    }

    #[test]
    fn iceberg_with_url_returns_iceberg_writer() {
        let (_g, s) = storage();
        let writer = build_dataset_writer(
            s,
            &WriterSettings {
                backend: WriterBackendKind::Iceberg,
                iceberg: IcebergSettings {
                    catalog_url: Some("http://catalog:8181".to_string()),
                    namespace: "streaming_service".to_string(),
                },
            },
        );
        assert_eq!(writer.backend_name(), "iceberg");
    }

    #[test]
    fn iceberg_with_blank_url_falls_back_to_legacy() {
        let (_g, s) = storage();
        let writer = build_dataset_writer(
            s,
            &WriterSettings {
                backend: WriterBackendKind::Iceberg,
                iceberg: IcebergSettings {
                    catalog_url: Some("   ".to_string()),
                    namespace: "streaming_service".to_string(),
                },
            },
        );
        assert_eq!(writer.backend_name(), "legacy");
    }
}
