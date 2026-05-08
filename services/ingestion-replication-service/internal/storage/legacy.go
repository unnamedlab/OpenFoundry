package storage

// Pre-Iceberg writer: dumps each snapshot as a single blob through the
// configured StorageBackend.
//
// The on-disk layout is `<namespace>/<table>/snapshots/<snapshot_id>.bin`
// plus an adjacent `.json` file with the snapshot metadata. The output is
// intentionally simple so it can be replayed by a backfill job if the new
// Iceberg path is rolled back.

import (
	"context"
	"encoding/json"
	"fmt"
)

// LegacyDatasetWriter is the pre-Iceberg DatasetWriter.
type LegacyDatasetWriter struct {
	backend   StorageBackend
	namespace string
}

// NewLegacyDatasetWriter wires the legacy writer with the given backend and
// namespace. Mirrors LegacyDatasetWriter::new in Rust.
func NewLegacyDatasetWriter(backend StorageBackend, namespace string) *LegacyDatasetWriter {
	return &LegacyDatasetWriter{backend: backend, namespace: namespace}
}

// Namespace returns the configured object-store namespace.
func (w *LegacyDatasetWriter) Namespace() string { return w.namespace }

func (w *LegacyDatasetWriter) dataPath(snapshot DatasetSnapshot) string {
	return fmt.Sprintf("%s/%s/snapshots/%s.bin", w.namespace, snapshot.Table, snapshot.SnapshotID)
}

func (w *LegacyDatasetWriter) metadataPath(snapshot DatasetSnapshot) string {
	return fmt.Sprintf("%s/%s/snapshots/%s.json", w.namespace, snapshot.Table, snapshot.SnapshotID)
}

// BackendName implements DatasetWriter.
func (w *LegacyDatasetWriter) BackendName() string { return "legacy" }

// Append implements DatasetWriter. Writes the payload at
// <namespace>/<table>/snapshots/<snapshot_id>.bin and, when metadata is
// non-null, writes an adjacent .json file with the encoded metadata.
func (w *LegacyDatasetWriter) Append(ctx context.Context, snapshot DatasetSnapshot) (WriteOutcome, error) {
	if snapshot.Table == "" || snapshot.SnapshotID == "" {
		return WriteOutcome{}, NewInvalidSnapshotError("table and snapshot_id are required")
	}

	dataPath := w.dataPath(snapshot)
	if err := w.backend.Put(ctx, dataPath, snapshot.Payload); err != nil {
		return WriteOutcome{}, NewStorageError(err)
	}

	if !snapshot.MetadataIsNull() {
		var v any
		if err := json.Unmarshal(snapshot.Metadata, &v); err != nil {
			return WriteOutcome{}, NewInvalidSnapshotError("failed to encode metadata: %s", err.Error())
		}
		metaBytes, err := json.Marshal(v)
		if err != nil {
			return WriteOutcome{}, NewInvalidSnapshotError("failed to encode metadata: %s", err.Error())
		}
		if err := w.backend.Put(ctx, w.metadataPath(snapshot), metaBytes); err != nil {
			return WriteOutcome{}, NewStorageError(err)
		}
	}

	return WriteOutcome{Backend: "legacy", Location: dataPath}, nil
}
