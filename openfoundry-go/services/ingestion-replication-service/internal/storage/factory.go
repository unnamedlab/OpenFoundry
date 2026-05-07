package storage

// Selects the DatasetWriter implementation at startup based on runtime
// configuration, with graceful degradation when Iceberg is requested but no
// REST Catalog endpoint has been provided.

import (
	"log/slog"
	"strings"
)

// WriterBackendKind enumerates the writer flavours that can be materialized
// at startup.
type WriterBackendKind int

const (
	// WriterBackendLegacy is the pre-Iceberg behaviour. Default for
	// safety / rollback.
	WriterBackendLegacy WriterBackendKind = iota
	// WriterBackendIceberg appends to an Iceberg table via REST Catalog.
	WriterBackendIceberg
)

// ParseWriterBackendKind interprets a string flag into a WriterBackendKind.
// Unknown / empty values fall back to WriterBackendLegacy. The match is
// case-insensitive and trims surrounding whitespace, mirroring
// WriterBackendKind::parse in Rust.
func ParseWriterBackendKind(raw string) WriterBackendKind {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "iceberg":
		return WriterBackendIceberg
	default:
		return WriterBackendLegacy
	}
}

// IcebergSettings groups Iceberg-specific runtime settings.
type IcebergSettings struct {
	// CatalogURL is the REST Catalog endpoint, e.g.
	// `http://iceberg-catalog:8181`. When empty, the factory falls back
	// to the legacy writer with a warning log.
	CatalogURL string
	// Namespace is the catalog namespace this service writes into. For
	// `event-streaming-service` this is `streaming_service`.
	Namespace string
}

// WriterSettings is the aggregated writer configuration consumed by the
// factory.
type WriterSettings struct {
	Backend WriterBackendKind
	Iceberg IcebergSettings
}

// BuildDatasetWriter materialises the configured writer.
//
//   - If Backend == WriterBackendLegacy, returns the legacy writer wrapping
//     storage.
//   - If Backend == WriterBackendIceberg and Iceberg.CatalogURL is set,
//     returns the Iceberg writer talking to the REST Catalog at that URL.
//   - If Backend == WriterBackendIceberg but Iceberg.CatalogURL is empty,
//     logs a warning and falls back to the legacy writer. This is the
//     documented "graceful degradation" path so the service still starts.
//
// The optional logger is used for the same info / warn lines emitted by the
// Rust factory; pass nil to suppress logging (default slog logger is used).
func BuildDatasetWriter(storage StorageBackend, settings WriterSettings, logger *slog.Logger) DatasetWriter {
	if logger == nil {
		logger = slog.Default()
	}
	switch settings.Backend {
	case WriterBackendLegacy:
		logger.Info("dataset writer: using legacy backend",
			slog.String("namespace", settings.Iceberg.Namespace),
		)
		return NewLegacyDatasetWriter(storage, settings.Iceberg.Namespace)

	case WriterBackendIceberg:
		url := strings.TrimSpace(settings.Iceberg.CatalogURL)
		if url == "" {
			logger.Warn("ICEBERG_CATALOG_URL is not configured; falling back to legacy dataset writer",
				slog.String("namespace", settings.Iceberg.Namespace),
			)
			return NewLegacyDatasetWriter(storage, settings.Iceberg.Namespace)
		}
		logger.Info("dataset writer: using Iceberg backend",
			slog.String("namespace", settings.Iceberg.Namespace),
			slog.String("catalog_url", settings.Iceberg.CatalogURL),
		)
		return NewIcebergDatasetWriter(
			storage,
			NewRestCatalogClient(settings.Iceberg.CatalogURL),
			settings.Iceberg.Namespace,
		)

	default:
		logger.Info("dataset writer: using legacy backend (unknown backend kind)",
			slog.String("namespace", settings.Iceberg.Namespace),
		)
		return NewLegacyDatasetWriter(storage, settings.Iceberg.Namespace)
	}
}

// BuildDatasetWriterWithInMemoryCatalog is the variant of BuildDatasetWriter
// that uses an InMemoryCatalog when Iceberg is requested. Intended for local
// development and integration tests where no real REST Catalog is available.
func BuildDatasetWriterWithInMemoryCatalog(storage StorageBackend, settings WriterSettings, logger *slog.Logger) DatasetWriter {
	if settings.Backend == WriterBackendIceberg {
		return NewIcebergDatasetWriter(storage, NewInMemoryCatalog(), settings.Iceberg.Namespace)
	}
	return BuildDatasetWriter(storage, settings, logger)
}
