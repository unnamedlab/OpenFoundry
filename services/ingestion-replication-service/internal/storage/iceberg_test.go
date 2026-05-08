package storage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func newIcebergFixture(t *testing.T) (*InMemoryStorageBackend, *InMemoryCatalog) {
	t.Helper()
	return NewInMemoryStorageBackend(), NewInMemoryCatalog()
}

// Port of `append_writes_data_file_and_commits_snapshot` from
// services/ingestion-replication-service/src/event_streaming/storage/iceberg.rs.
func TestIcebergAppendWritesDataFileAndCommitsSnapshot(t *testing.T) {
	backend, catalog := newIcebergFixture(t)
	writer := NewIcebergDatasetWriter(backend, catalog, "streaming_service")

	snap := NewDatasetSnapshot("window_42", "snap_001", []byte("row")).
		WithMetadata(json.RawMessage(`{"rows":1}`))
	outcome, err := writer.Append(context.Background(), snap)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	if outcome.Backend != "iceberg" {
		t.Errorf("backend: got %q, want iceberg", outcome.Backend)
	}
	if outcome.Location != "iceberg://streaming_service/window_42#snap_001" {
		t.Errorf("location: got %q", outcome.Location)
	}
	exists, err := backend.Exists(context.Background(), "streaming_service/window_42/data/snap_001.parquet")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Errorf("expected data file to be written")
	}
	commits := catalog.Snapshots(NewIcebergTableRef("streaming_service", "window_42"))
	if len(commits) != 1 {
		t.Fatalf("commits: got %d, want 1", len(commits))
	}
	if commits[0].SnapshotID != "snap_001" {
		t.Errorf("snapshot id: got %q", commits[0].SnapshotID)
	}
	if commits[0].DataFile != "streaming_service/window_42/data/snap_001.parquet" {
		t.Errorf("data file: got %q", commits[0].DataFile)
	}
}

// Port of `append_is_idempotent_per_snapshot_id_at_storage_layer`.
//
// The catalog records every commit; the storage layer overwrites the data
// file. Both calls must succeed: replay safety lives at the catalog level
// (not asserted here), but we want no spurious errors.
func TestIcebergAppendIsIdempotentPerSnapshotIDAtStorageLayer(t *testing.T) {
	backend, catalog := newIcebergFixture(t)
	writer := NewIcebergDatasetWriter(backend, catalog, "streaming_service")

	snap := NewDatasetSnapshot("window_x", "snap_1", []byte("v1"))
	if _, err := writer.Append(context.Background(), snap); err != nil {
		t.Fatalf("first Append: %v", err)
	}
	if _, err := writer.Append(context.Background(), snap); err != nil {
		t.Fatalf("second Append: %v", err)
	}

	commits := catalog.Snapshots(NewIcebergTableRef("streaming_service", "window_x"))
	if len(commits) != 2 {
		t.Errorf("commits: got %d, want 2", len(commits))
	}
}

// Port of `append_rejects_empty_identifiers`.
func TestIcebergAppendRejectsEmptyIdentifiers(t *testing.T) {
	backend, catalog := newIcebergFixture(t)
	writer := NewIcebergDatasetWriter(backend, catalog, "streaming_service")
	snap := NewDatasetSnapshot("window_x", "", nil)

	_, err := writer.Append(context.Background(), snap)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !IsWriterErrorKind(err, WriterErrorInvalidSnapshot) {
		t.Errorf("kind: got %v", err)
	}
	if !strings.Contains(err.Error(), "invalid snapshot:") {
		t.Errorf("message: %v", err)
	}
}

// Port of `rest_catalog_client_trims_trailing_slash`.
func TestRestCatalogClientTrimsTrailingSlash(t *testing.T) {
	c := NewRestCatalogClient("http://catalog.local:8181/")
	if got, want := c.BaseURL(), "http://catalog.local:8181"; got != want {
		t.Errorf("base url: got %q, want %q", got, want)
	}
}

// recordingDoer captures the last request seen and returns a canned response.
type recordingDoer struct {
	lastReq    *http.Request
	lastBody   []byte
	respStatus int
	respBody   string
	err        error
}

func (d *recordingDoer) Do(req *http.Request) (*http.Response, error) {
	if d.err != nil {
		return nil, d.err
	}
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		d.lastBody = body
	}
	d.lastReq = req
	status := d.respStatus
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(d.respBody)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestRestCatalogClientPostsJSONBody(t *testing.T) {
	d := &recordingDoer{respStatus: http.StatusOK}
	c := NewRestCatalogClientWithHTTP("http://catalog.local:8181", d)
	commit := SnapshotCommit{
		SnapshotID: "snap_1",
		DataFile:   "ns/tbl/data/snap_1.parquet",
		Summary:    json.RawMessage(`{"rows":1}`),
	}
	if err := c.AppendSnapshot(context.Background(), NewIcebergTableRef("ns", "tbl"), commit); err != nil {
		t.Fatalf("AppendSnapshot: %v", err)
	}
	wantURL := "http://catalog.local:8181/v1/namespaces/ns/tables/tbl/snapshots"
	if d.lastReq.URL.String() != wantURL {
		t.Errorf("url: got %q, want %q", d.lastReq.URL.String(), wantURL)
	}
	if d.lastReq.Method != http.MethodPost {
		t.Errorf("method: got %q", d.lastReq.Method)
	}
	wantBody := `{"snapshot-id":"snap_1","data-file":"ns/tbl/data/snap_1.parquet","summary":{"rows":1},"operation":"append"}`
	if string(d.lastBody) != wantBody {
		t.Errorf("body: got %s\nwant %s", string(d.lastBody), wantBody)
	}
}

func TestRestCatalogClientReportsNonSuccess(t *testing.T) {
	d := &recordingDoer{respStatus: http.StatusInternalServerError, respBody: "boom"}
	c := NewRestCatalogClientWithHTTP("http://catalog.local:8181", d)
	err := c.AppendSnapshot(
		context.Background(),
		NewIcebergTableRef("ns", "tbl"),
		SnapshotCommit{SnapshotID: "x", DataFile: "p", Summary: json.RawMessage("null")},
	)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !IsWriterErrorKind(err, WriterErrorCatalog) {
		t.Errorf("kind: got %v", err)
	}
	if !strings.Contains(err.Error(), "REST catalog returned 500") {
		t.Errorf("message: %v", err)
	}
}

func TestRestCatalogClientReportsTransportError(t *testing.T) {
	d := &recordingDoer{err: errors.New("dial tcp: refused")}
	c := NewRestCatalogClientWithHTTP("http://catalog.local:8181", d)
	err := c.AppendSnapshot(
		context.Background(),
		NewIcebergTableRef("ns", "tbl"),
		SnapshotCommit{SnapshotID: "x", DataFile: "p"},
	)
	if !IsWriterErrorKind(err, WriterErrorCatalog) {
		t.Errorf("kind: got %v", err)
	}
	if !strings.Contains(err.Error(), "REST catalog request failed:") {
		t.Errorf("message: %v", err)
	}
}
