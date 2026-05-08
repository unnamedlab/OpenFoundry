package storage

// Iceberg-backed dataset writer.
//
// The service currently does not depend on a fully-featured Iceberg
// implementation: the real REST Catalog client and Parquet/manifest writers
// are scoped to the libs/storage-abstraction port and will be wired in later.
// To keep this package functional and testable in the meantime, the writer is
// split in two collaborators:
//
//   - IcebergCatalog: a small interface that captures the only interaction we
//     need today, registering a new snapshot for an (namespace, table) pair.
//     InMemoryCatalog is the test double; RestCatalogClient is a thin HTTP
//     shim against an Iceberg REST Catalog endpoint
//     (POST /v1/namespaces/{ns}/tables/{table}/snapshots).
//   - The writer itself, which uploads the snapshot bytes to the configured
//     StorageBackend under an Iceberg-shaped path
//     (<namespace>/<table>/data/<snapshot_id>.parquet) and then asks the
//     catalog to commit the new snapshot pointing at that data file.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// IcebergTableRef is a fully-qualified Iceberg table reference (namespace +
// table name).
type IcebergTableRef struct {
	Namespace string
	Table     string
}

// NewIcebergTableRef constructs an IcebergTableRef. Mirrors
// IcebergTableRef::new in Rust.
func NewIcebergTableRef(namespace, table string) IcebergTableRef {
	return IcebergTableRef{Namespace: namespace, Table: table}
}

// SnapshotCommit is a single committed snapshot as known to the catalog.
type SnapshotCommit struct {
	SnapshotID string
	DataFile   string
	Summary    json.RawMessage
}

// Equal reports whether two commits are byte-equal in their identifying
// fields and in their serialised summary.
func (c SnapshotCommit) Equal(other SnapshotCommit) bool {
	if c.SnapshotID != other.SnapshotID || c.DataFile != other.DataFile {
		return false
	}
	return bytes.Equal(c.Summary, other.Summary)
}

// IcebergCatalog is the minimal abstraction over an Iceberg catalog. The
// interface deliberately covers only the operations needed by the writer so
// it can be backed by either an in-memory mock (tests) or the REST Catalog
// (production).
type IcebergCatalog interface {
	// AppendSnapshot appends a new snapshot to the table. Implementations
	// are expected to create the table on first use.
	AppendSnapshot(ctx context.Context, table IcebergTableRef, commit SnapshotCommit) error
}

// InMemoryCatalog is the in-memory IcebergCatalog used by unit tests and as a
// fallback when no REST Catalog endpoint is configured but the operator still
// wants to exercise the Iceberg code path locally.
type InMemoryCatalog struct {
	mu    sync.Mutex
	inner map[IcebergTableRef][]SnapshotCommit
}

// NewInMemoryCatalog returns an empty in-memory catalog.
func NewInMemoryCatalog() *InMemoryCatalog {
	return &InMemoryCatalog{inner: make(map[IcebergTableRef][]SnapshotCommit)}
}

// AppendSnapshot implements IcebergCatalog.
func (c *InMemoryCatalog) AppendSnapshot(_ context.Context, table IcebergTableRef, commit SnapshotCommit) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inner[table] = append(c.inner[table], commit)
	return nil
}

// Snapshots returns a copy of the snapshots committed for the given table.
func (c *InMemoryCatalog) Snapshots(table IcebergTableRef) []SnapshotCommit {
	c.mu.Lock()
	defer c.mu.Unlock()
	src, ok := c.inner[table]
	if !ok {
		return nil
	}
	out := make([]SnapshotCommit, len(src))
	copy(out, src)
	return out
}

// HTTPDoer is the slice of *http.Client RestCatalogClient depends on. Lets
// tests inject a fake transport without spinning up a real server.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// RestCatalogClient is the HTTP client for an Iceberg REST Catalog endpoint.
//
// This is intentionally a thin shim: it sends the snapshot summary as JSON
// and trusts the server to perform the actual table mutation. When the full
// Iceberg client lands in libs/storage-abstraction, this type will delegate
// to it instead of issuing the HTTP call directly.
type RestCatalogClient struct {
	baseURL string
	http    HTTPDoer
}

// NewRestCatalogClient builds a client targeting baseURL. Trailing slashes are
// trimmed, matching RestCatalogClient::new in Rust.
func NewRestCatalogClient(baseURL string) *RestCatalogClient {
	return &RestCatalogClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{},
	}
}

// NewRestCatalogClientWithHTTP builds a client with a caller-supplied HTTP
// doer. Mirrors RestCatalogClient::with_client in Rust and is the test seam
// for swapping in a stub transport.
func NewRestCatalogClientWithHTTP(baseURL string, doer HTTPDoer) *RestCatalogClient {
	return &RestCatalogClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    doer,
	}
}

// BaseURL returns the trimmed base URL the client posts to.
func (c *RestCatalogClient) BaseURL() string { return c.baseURL }

// restCatalogBody mirrors the JSON shape Rust emits via serde_json::json!.
// Field declaration order is preserved by encoding/json so the body is
// byte-equivalent to the Rust client.
type restCatalogBody struct {
	SnapshotID string          `json:"snapshot-id"`
	DataFile   string          `json:"data-file"`
	Summary    json.RawMessage `json:"summary"`
	Operation  string          `json:"operation"`
}

// AppendSnapshot implements IcebergCatalog by POSTing the commit to the REST
// catalog endpoint:
//
//	POST <base>/v1/namespaces/{ns}/tables/{table}/snapshots
func (c *RestCatalogClient) AppendSnapshot(ctx context.Context, table IcebergTableRef, commit SnapshotCommit) error {
	url := fmt.Sprintf("%s/v1/namespaces/%s/tables/%s/snapshots", c.baseURL, table.Namespace, table.Table)
	summary := commit.Summary
	if len(summary) == 0 {
		summary = json.RawMessage("null")
	}
	body, err := json.Marshal(restCatalogBody{
		SnapshotID: commit.SnapshotID,
		DataFile:   commit.DataFile,
		Summary:    summary,
		Operation:  "append",
	})
	if err != nil {
		return NewCatalogError("REST catalog request failed: %s", err.Error())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return NewCatalogError("REST catalog request failed: %s", err.Error())
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return NewCatalogError("REST catalog request failed: %s", err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		text, _ := io.ReadAll(resp.Body)
		return NewCatalogError("REST catalog returned %d %s: %s", resp.StatusCode, http.StatusText(resp.StatusCode), string(text))
	}
	return nil
}

// IcebergDatasetWriter is the Iceberg-backed DatasetWriter.
type IcebergDatasetWriter struct {
	backend   StorageBackend
	catalog   IcebergCatalog
	namespace string
}

// NewIcebergDatasetWriter wires the writer with the given backend, catalog
// and namespace. Mirrors IcebergDatasetWriter::new in Rust.
func NewIcebergDatasetWriter(backend StorageBackend, catalog IcebergCatalog, namespace string) *IcebergDatasetWriter {
	return &IcebergDatasetWriter{backend: backend, catalog: catalog, namespace: namespace}
}

// Namespace returns the configured catalog namespace.
func (w *IcebergDatasetWriter) Namespace() string { return w.namespace }

func (w *IcebergDatasetWriter) dataPath(snapshot DatasetSnapshot) string {
	return fmt.Sprintf("%s/%s/data/%s.parquet", w.namespace, snapshot.Table, snapshot.SnapshotID)
}

func (w *IcebergDatasetWriter) icebergURI(snapshot DatasetSnapshot) string {
	return fmt.Sprintf("iceberg://%s/%s#%s", w.namespace, snapshot.Table, snapshot.SnapshotID)
}

// BackendName implements DatasetWriter.
func (w *IcebergDatasetWriter) BackendName() string { return "iceberg" }

// Append implements DatasetWriter. The bytes are uploaded to the configured
// StorageBackend at <namespace>/<table>/data/<snapshot_id>.parquet, then the
// catalog is asked to commit the new snapshot pointing at that data file.
func (w *IcebergDatasetWriter) Append(ctx context.Context, snapshot DatasetSnapshot) (WriteOutcome, error) {
	if snapshot.Table == "" || snapshot.SnapshotID == "" {
		return WriteOutcome{}, NewInvalidSnapshotError("table and snapshot_id are required")
	}

	dataPath := w.dataPath(snapshot)
	if err := w.backend.Put(ctx, dataPath, snapshot.Payload); err != nil {
		return WriteOutcome{}, NewStorageError(err)
	}

	tableRef := NewIcebergTableRef(w.namespace, snapshot.Table)
	commit := SnapshotCommit{
		SnapshotID: snapshot.SnapshotID,
		DataFile:   dataPath,
		Summary:    snapshot.Metadata,
	}
	if err := w.catalog.AppendSnapshot(ctx, tableRef, commit); err != nil {
		return WriteOutcome{}, err
	}

	return WriteOutcome{Backend: "iceberg", Location: w.icebergURI(snapshot)}, nil
}
