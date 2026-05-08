package storage

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Port of `append_writes_blob_and_metadata` from
// services/ingestion-replication-service/src/event_streaming/storage/legacy.rs.
func TestLegacyAppendWritesBlobAndMetadata(t *testing.T) {
	backend := NewInMemoryStorageBackend()
	writer := NewLegacyDatasetWriter(backend, "streaming_service")

	snap := NewDatasetSnapshot("window_1", "snap_1", []byte("row1")).
		WithMetadata(json.RawMessage(`{"rows":1}`))
	outcome, err := writer.Append(context.Background(), snap)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if outcome.Backend != "legacy" {
		t.Errorf("backend: got %q", outcome.Backend)
	}
	exists, err := backend.Exists(context.Background(), "streaming_service/window_1/snapshots/snap_1.bin")
	if err != nil || !exists {
		t.Errorf("expected blob path to exist (err=%v, ok=%v)", err, exists)
	}
	exists, err = backend.Exists(context.Background(), "streaming_service/window_1/snapshots/snap_1.json")
	if err != nil || !exists {
		t.Errorf("expected metadata path to exist (err=%v, ok=%v)", err, exists)
	}

	meta, ok := backend.Get("streaming_service/window_1/snapshots/snap_1.json")
	if !ok {
		t.Fatalf("metadata absent")
	}
	if string(meta) != `{"rows":1}` {
		t.Errorf("metadata bytes: got %q", string(meta))
	}
}

// Port of `append_rejects_empty_identifiers` from legacy.rs.
func TestLegacyAppendRejectsEmptyIdentifiers(t *testing.T) {
	backend := NewInMemoryStorageBackend()
	writer := NewLegacyDatasetWriter(backend, "streaming_service")
	snap := NewDatasetSnapshot("", "snap_1", nil)
	_, err := writer.Append(context.Background(), snap)
	if !IsWriterErrorKind(err, WriterErrorInvalidSnapshot) {
		t.Errorf("kind: got %v", err)
	}
	if !strings.Contains(err.Error(), "invalid snapshot:") {
		t.Errorf("message: %v", err)
	}
}

// Additional: verify that a JSON-null metadata skips the .json sidecar, matching
// the Rust `if !snapshot.metadata.is_null()` guard.
func TestLegacyAppendSkipsMetadataWhenNull(t *testing.T) {
	backend := NewInMemoryStorageBackend()
	writer := NewLegacyDatasetWriter(backend, "streaming_service")
	snap := NewDatasetSnapshot("window_2", "snap_2", []byte("row"))
	if _, err := writer.Append(context.Background(), snap); err != nil {
		t.Fatalf("Append: %v", err)
	}
	exists, _ := backend.Exists(context.Background(), "streaming_service/window_2/snapshots/snap_2.json")
	if exists {
		t.Errorf("expected metadata sidecar to be absent for null metadata")
	}
}
