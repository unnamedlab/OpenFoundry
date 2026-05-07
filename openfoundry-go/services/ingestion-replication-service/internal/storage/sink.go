// Package storage ports the event-streaming dataset writer stack from
// services/ingestion-replication-service/src/event_streaming/storage in Rust.
//
// Two writer backends are provided here, mirroring the Rust crate:
//
//   - LegacyDatasetWriter persists each snapshot as a single blob through the
//     configured StorageBackend. It is preserved so operators can roll back at
//     runtime if the new path misbehaves in production.
//   - IcebergDatasetWriter appends the snapshot to an Iceberg table managed by
//     a REST Catalog, addressed by a configured namespace.
//
// BuildDatasetWriter picks the backend at startup based on the runtime
// configuration and degrades gracefully (legacy writer + warning log) when
// Iceberg is requested but no catalog URL is provided.
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// DatasetSnapshot is a unit of materialization (a window flush, a checkpoint
// or a replay snapshot) that needs to be persisted by a DatasetWriter.
type DatasetSnapshot struct {
	// Table is the logical table this snapshot belongs to (e.g.
	// `window_<topology>` or `checkpoint_<stream>`).
	Table string
	// SnapshotID is the stable identifier for this snapshot inside the
	// table. Used as the Iceberg snapshot id and as the file stem for the
	// legacy writer.
	SnapshotID string
	// Payload is the encoded payload (serialized rows / state). Opaque to
	// the writer.
	Payload []byte
	// Metadata is recorded next to the snapshot (window bounds, checkpoint
	// epoch, ...). The Iceberg writer forwards it to the catalog as
	// snapshot summary properties; the legacy writer persists it as a
	// sibling JSON file.
	//
	// Use a JSON `null` (or nil) to indicate "no metadata", matching the
	// Rust serde_json::Value::Null sentinel.
	Metadata json.RawMessage
}

// NewDatasetSnapshot builds a snapshot with a JSON-null metadata, matching
// DatasetSnapshot::new in Rust.
func NewDatasetSnapshot(table, snapshotID string, payload []byte) DatasetSnapshot {
	return DatasetSnapshot{
		Table:      table,
		SnapshotID: snapshotID,
		Payload:    payload,
		Metadata:   json.RawMessage("null"),
	}
}

// WithMetadata returns a copy of s with the given metadata attached.
// Mirrors DatasetSnapshot::with_metadata in Rust.
func (s DatasetSnapshot) WithMetadata(metadata json.RawMessage) DatasetSnapshot {
	s.Metadata = metadata
	return s
}

// MetadataIsNull reports whether the snapshot carries no metadata, i.e. the
// metadata bytes are absent or encode the JSON `null` literal. Matches
// Value::is_null() in Rust.
func (s DatasetSnapshot) MetadataIsNull() bool {
	if len(s.Metadata) == 0 {
		return true
	}
	return string(s.Metadata) == "null"
}

// WriteOutcome is the outcome of a successful append.
type WriteOutcome struct {
	// Backend identifies which writer produced the artefact (`"legacy"`,
	// `"iceberg"`).
	Backend string
	// Location is the logical location of the materialised data. For the
	// legacy writer this is the object-store path of the blob; for the
	// Iceberg writer this is `iceberg://<namespace>/<table>#<snapshot_id>`.
	Location string
}

// WriterErrorKind enumerates the WriterError variants from Rust's thiserror
// enum so callers can match on the failure category without parsing strings.
type WriterErrorKind int

const (
	// WriterErrorStorage maps to WriterError::Storage in Rust.
	WriterErrorStorage WriterErrorKind = iota
	// WriterErrorCatalog maps to WriterError::Catalog.
	WriterErrorCatalog
	// WriterErrorInvalidSnapshot maps to WriterError::InvalidSnapshot.
	WriterErrorInvalidSnapshot
)

// WriterError is the error type returned by every DatasetWriter
// implementation. It mirrors the Rust enum (Storage, Catalog, InvalidSnapshot)
// and preserves the exact error message format ("storage backend error: ...",
// "iceberg catalog error: ...", "invalid snapshot: ...").
type WriterError struct {
	Kind    WriterErrorKind
	Message string
	Cause   error
}

func (e *WriterError) Error() string {
	switch e.Kind {
	case WriterErrorStorage:
		return fmt.Sprintf("storage backend error: %s", e.Message)
	case WriterErrorCatalog:
		return fmt.Sprintf("iceberg catalog error: %s", e.Message)
	case WriterErrorInvalidSnapshot:
		return fmt.Sprintf("invalid snapshot: %s", e.Message)
	default:
		return e.Message
	}
}

func (e *WriterError) Unwrap() error { return e.Cause }

// IsWriterErrorKind reports whether err is a *WriterError of the given kind.
// Useful for tests asserting on the discriminant.
func IsWriterErrorKind(err error, kind WriterErrorKind) bool {
	var we *WriterError
	if !errors.As(err, &we) {
		return false
	}
	return we.Kind == kind
}

// NewStorageError wraps a backend error as WriterError::Storage.
func NewStorageError(err error) *WriterError {
	if err == nil {
		return nil
	}
	return &WriterError{Kind: WriterErrorStorage, Message: err.Error(), Cause: err}
}

// NewCatalogError builds a WriterError::Catalog with the given message.
func NewCatalogError(format string, args ...any) *WriterError {
	return &WriterError{Kind: WriterErrorCatalog, Message: fmt.Sprintf(format, args...)}
}

// NewInvalidSnapshotError builds a WriterError::InvalidSnapshot.
func NewInvalidSnapshotError(format string, args ...any) *WriterError {
	return &WriterError{Kind: WriterErrorInvalidSnapshot, Message: fmt.Sprintf(format, args...)}
}

// DatasetWriter is the sink that materialises a DatasetSnapshot into long-term
// storage. Implementations must be cheap to copy / share so the writer can be
// reused across stream operators.
type DatasetWriter interface {
	// BackendName is a stable identifier for the backend, used in logs and
	// metrics.
	BackendName() string

	// Append persists snapshot to the underlying table/blob store.
	Append(ctx context.Context, snapshot DatasetSnapshot) (WriteOutcome, error)
}

// StorageBackend is the minimal slice of libs/storage-abstraction that the
// Iceberg / Legacy writers actually use. It mirrors the Rust
// `storage_abstraction::StorageBackend` surface needed by these writers
// without forcing the rest of the abstraction crate to be ported.
type StorageBackend interface {
	// Put writes data at path, overwriting any prior contents.
	Put(ctx context.Context, path string, data []byte) error
	// Exists reports whether path is currently materialised in the
	// backend.
	Exists(ctx context.Context, path string) (bool, error)
}

// InMemoryStorageBackend is an in-memory StorageBackend used by tests and as
// a process-local fallback. Concurrency-safe.
type InMemoryStorageBackend struct {
	mu    sync.Mutex
	files map[string][]byte
}

// NewInMemoryStorageBackend returns an empty in-memory backend.
func NewInMemoryStorageBackend() *InMemoryStorageBackend {
	return &InMemoryStorageBackend{files: make(map[string][]byte)}
}

// Put implements StorageBackend.
func (b *InMemoryStorageBackend) Put(_ context.Context, path string, data []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	b.files[path] = cp
	return nil
}

// Exists implements StorageBackend.
func (b *InMemoryStorageBackend) Exists(_ context.Context, path string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.files[path]
	return ok, nil
}

// Get returns the bytes previously stored at path, or false if absent. Not
// part of the StorageBackend contract but useful for tests.
func (b *InMemoryStorageBackend) Get(path string) ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.files[path]
	if !ok {
		return nil, false
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, true
}
