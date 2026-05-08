package storage

import "testing"

// Port of `parse_backend_defaults_to_legacy` from
// services/ingestion-replication-service/src/event_streaming/storage/factory.rs.
func TestParseBackendDefaultsToLegacy(t *testing.T) {
	cases := []struct {
		raw  string
		want WriterBackendKind
	}{
		{"", WriterBackendLegacy},
		{"legacy", WriterBackendLegacy},
		{"anything", WriterBackendLegacy},
		{"Iceberg", WriterBackendIceberg},
		{"ICEBERG", WriterBackendIceberg},
	}
	for _, c := range cases {
		if got := ParseWriterBackendKind(c.raw); got != c.want {
			t.Errorf("ParseWriterBackendKind(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}

// Port of `iceberg_without_url_falls_back_to_legacy`.
func TestIcebergWithoutURLFallsBackToLegacy(t *testing.T) {
	w := BuildDatasetWriter(NewInMemoryStorageBackend(), WriterSettings{
		Backend: WriterBackendIceberg,
		Iceberg: IcebergSettings{Namespace: "streaming_service"},
	}, nil)
	if w.BackendName() != "legacy" {
		t.Errorf("backend: got %q, want legacy", w.BackendName())
	}
}

// Port of `iceberg_with_url_returns_iceberg_writer`.
func TestIcebergWithURLReturnsIcebergWriter(t *testing.T) {
	w := BuildDatasetWriter(NewInMemoryStorageBackend(), WriterSettings{
		Backend: WriterBackendIceberg,
		Iceberg: IcebergSettings{
			CatalogURL: "http://catalog:8181",
			Namespace:  "streaming_service",
		},
	}, nil)
	if w.BackendName() != "iceberg" {
		t.Errorf("backend: got %q, want iceberg", w.BackendName())
	}
}

// Port of `iceberg_with_blank_url_falls_back_to_legacy`.
func TestIcebergWithBlankURLFallsBackToLegacy(t *testing.T) {
	w := BuildDatasetWriter(NewInMemoryStorageBackend(), WriterSettings{
		Backend: WriterBackendIceberg,
		Iceberg: IcebergSettings{
			CatalogURL: "   ",
			Namespace:  "streaming_service",
		},
	}, nil)
	if w.BackendName() != "legacy" {
		t.Errorf("backend: got %q, want legacy", w.BackendName())
	}
}

// Additional: in-memory catalog variant returns the iceberg writer even
// without a URL, matching build_dataset_writer_with_in_memory_catalog.
func TestBuildWithInMemoryCatalogReturnsIcebergWriter(t *testing.T) {
	w := BuildDatasetWriterWithInMemoryCatalog(NewInMemoryStorageBackend(), WriterSettings{
		Backend: WriterBackendIceberg,
		Iceberg: IcebergSettings{Namespace: "streaming_service"},
	}, nil)
	if w.BackendName() != "iceberg" {
		t.Errorf("backend: got %q, want iceberg", w.BackendName())
	}
}
