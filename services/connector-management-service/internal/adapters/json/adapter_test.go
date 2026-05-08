package json

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

const sampleArrayJSON = `[
  {"id":1,"name":"alice"},
  {"id":2,"name":"bob"},
  {"id":3,"name":"carol"}
]`

const sampleObjectJSON = `{"id":1,"name":"alice"}`

const sampleNDJSON = "{\"id\":1,\"name\":\"alice\"}\n{\"id\":2,\"name\":\"bob\"}\n\n{\"id\":3,\"name\":\"carol\"}\n"

func TestValidateConfigRequiresURLOrPath(t *testing.T) {
	t.Parallel()
	if err := ValidateConfig(json.RawMessage(`{}`)); err == nil {
		t.Fatalf("expected error for empty config")
	}
	if err := ValidateConfig(json.RawMessage(`{"url":"https://example.com/a.json"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateConfig(json.RawMessage(`{"path":"/tmp/a.json"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCountRowsArray(t *testing.T) {
	t.Parallel()
	got, err := CountRows([]byte(sampleArrayJSON))
	if err != nil {
		t.Fatalf("CountRows: %v", err)
	}
	if got != 3 {
		t.Fatalf("CountRows = %d, want 3", got)
	}
}

func TestCountRowsObjectIsOne(t *testing.T) {
	t.Parallel()
	got, err := CountRows([]byte(sampleObjectJSON))
	if err != nil {
		t.Fatalf("CountRows: %v", err)
	}
	if got != 1 {
		t.Fatalf("CountRows = %d, want 1", got)
	}
}

func TestCountRowsNDJSONSkipsBlankLines(t *testing.T) {
	t.Parallel()
	got, err := CountRows([]byte(sampleNDJSON))
	if err != nil {
		t.Fatalf("CountRows: %v", err)
	}
	if got != 3 {
		t.Fatalf("CountRows = %d, want 3", got)
	}
}

func TestCountRowsEmpty(t *testing.T) {
	t.Parallel()
	got, err := CountRows([]byte("   \n  "))
	if err != nil {
		t.Fatalf("CountRows: %v", err)
	}
	if got != 0 {
		t.Fatalf("CountRows = %d, want 0", got)
	}
}

func TestQueryVirtualTableArray(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleArrayJSON))
	}))
	defer srv.Close()
	conn := &models.Connection{
		ConnectorType: ConnectorType,
		Config:        json.RawMessage(`{"url":"` + srv.URL + `/data.json"}`),
	}
	a := New()
	a.SetHTTPClient(srv.Client())
	limit := 2
	res, err := a.QueryVirtualTable(context.Background(), conn, &adapters.Query{Selector: "data", Limit: &limit}, "")
	if err != nil {
		t.Fatalf("QueryVirtualTable: %v", err)
	}
	if res.RowCount != 2 {
		t.Fatalf("row_count = %d, want 2", res.RowCount)
	}
	if len(res.Columns) != 2 {
		t.Fatalf("columns = %v", res.Columns)
	}
}

func TestQueryVirtualTableNDJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.ndjson")
	if err := os.WriteFile(path, []byte(sampleNDJSON), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	conn := &models.Connection{
		ConnectorType: ConnectorType,
		Config:        json.RawMessage(`{"path":"` + path + `"}`),
	}
	res, err := New().QueryVirtualTable(context.Background(), conn, &adapters.Query{Selector: "data"}, "")
	if err != nil {
		t.Fatalf("QueryVirtualTable: %v", err)
	}
	if res.RowCount != 3 {
		t.Fatalf("row_count = %d, want 3", res.RowCount)
	}
}

func TestQueryVirtualTableSingleObject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	if err := os.WriteFile(path, []byte(sampleObjectJSON), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	conn := &models.Connection{
		ConnectorType: ConnectorType,
		Config:        json.RawMessage(`{"path":"` + path + `"}`),
	}
	res, err := New().QueryVirtualTable(context.Background(), conn, &adapters.Query{Selector: "data"}, "")
	if err != nil {
		t.Fatalf("QueryVirtualTable: %v", err)
	}
	if res.RowCount != 1 {
		t.Fatalf("row_count = %d, want 1", res.RowCount)
	}
	var row map[string]any
	if err := json.Unmarshal(res.Rows[0], &row); err != nil {
		t.Fatalf("decode row: %v", err)
	}
	if row["name"] != "alice" {
		t.Fatalf("first row = %v", row)
	}
}

func TestStreamArrowEncodesAllRows(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	if err := os.WriteFile(path, []byte(sampleArrayJSON), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	conn := &models.Connection{
		ConnectorType: ConnectorType,
		Config:        json.RawMessage(`{"path":"` + path + `"}`),
	}
	stream, err := New().StreamArrow(context.Background(), conn, &adapters.Query{Selector: "data"}, "")
	if err != nil {
		t.Fatalf("StreamArrow: %v", err)
	}
	frame, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if len(frame) == 0 {
		t.Fatalf("empty arrow frame")
	}
	_ = stream.Close()
}

func TestDiscoverSourcesEmitsSyntheticEntry(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{"url":"https://example.com/data/orders.json"}`)
	conn := &models.Connection{ConnectorType: ConnectorType, Config: cfg}
	got, err := New().DiscoverSources(context.Background(), conn, "")
	if err != nil {
		t.Fatalf("DiscoverSources: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].SourceKind != defaultSourceKind {
		t.Fatalf("source_kind = %q", got[0].SourceKind)
	}
	if got[0].DisplayName != "orders.json" {
		t.Fatalf("display = %q", got[0].DisplayName)
	}
}

func TestQueryVirtualTableWrapsScalarsAsValueRows(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "scalar.txt")
	if err := os.WriteFile(path, []byte("1\n2\n3\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	conn := &models.Connection{
		ConnectorType: ConnectorType,
		Config:        json.RawMessage(`{"path":"` + path + `"}`),
	}
	res, err := New().QueryVirtualTable(context.Background(), conn, &adapters.Query{Selector: "data"}, "")
	if err != nil {
		t.Fatalf("QueryVirtualTable: %v", err)
	}
	if res.RowCount != 3 {
		t.Fatalf("row_count = %d, want 3", res.RowCount)
	}
	var row map[string]any
	if err := json.Unmarshal(res.Rows[0], &row); err != nil {
		t.Fatalf("decode row: %v", err)
	}
	if row["value"] == nil {
		t.Fatalf("expected scalar wrapped as 'value', got %v", row)
	}
}

func TestBuildIngestSpecEmitsJSONDescriptor(t *testing.T) {
	t.Parallel()
	conn := &models.Connection{
		Name:          "raw-orders",
		ConnectorType: ConnectorType,
		Config:        json.RawMessage(`{"path":"/tmp/data.json"}`),
	}
	src := &adapters.Source{Selector: "orders"}
	spec, err := New().BuildIngestSpec(context.Background(), conn, src)
	if err != nil {
		t.Fatalf("BuildIngestSpec: %v", err)
	}
	if spec.Source != ConnectorType {
		t.Fatalf("source = %q", spec.Source)
	}
	var cfg map[string]any
	if err := json.Unmarshal(spec.Config, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg["path"] != "/tmp/data.json" {
		t.Fatalf("path = %v", cfg["path"])
	}
	if cfg["selector"] != "orders" {
		t.Fatalf("selector = %v", cfg["selector"])
	}
}

func TestBuildIngestSpecRejectsMissingIdentity(t *testing.T) {
	t.Parallel()
	conn := &models.Connection{Name: "x", ConnectorType: ConnectorType, Config: json.RawMessage(`{}`)}
	if _, err := New().BuildIngestSpec(context.Background(), conn, &adapters.Source{}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestQueryVirtualTableRejectsNilRequest(t *testing.T) {
	t.Parallel()
	conn := &models.Connection{ConnectorType: ConnectorType, Config: json.RawMessage(`{"path":"/tmp/x.json"}`)}
	if _, err := New().QueryVirtualTable(context.Background(), conn, nil, ""); err == nil {
		t.Fatalf("expected nil-request error")
	}
}

func TestReadSourceHTTPNon2xx(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer srv.Close()
	a := New()
	a.SetHTTPClient(srv.Client())
	_, err := a.readSource(context.Background(), &jsonConfig{URL: srv.URL + "/missing"})
	if err == nil {
		t.Fatalf("expected error on 404")
	}
}

func TestUnsupportedNotImplementedSentinelNotUsed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	if err := os.WriteFile(path, []byte(sampleArrayJSON), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	conn := &models.Connection{Name: "x", ConnectorType: ConnectorType, Config: json.RawMessage(`{"path":"` + path + `"}`)}
	a := New()
	if _, err := a.DiscoverSources(context.Background(), conn, ""); errors.Is(err, adapters.ErrNotImplemented) {
		t.Fatalf("DiscoverSources returned ErrNotImplemented")
	}
	if _, err := a.QueryVirtualTable(context.Background(), conn, &adapters.Query{Selector: "x"}, ""); errors.Is(err, adapters.ErrNotImplemented) {
		t.Fatalf("QueryVirtualTable returned ErrNotImplemented")
	}
	if _, err := a.StreamArrow(context.Background(), conn, &adapters.Query{Selector: "x"}, ""); errors.Is(err, adapters.ErrNotImplemented) {
		t.Fatalf("StreamArrow returned ErrNotImplemented")
	}
	if _, err := a.BuildIngestSpec(context.Background(), conn, &adapters.Source{Selector: "x"}); errors.Is(err, adapters.ErrNotImplemented) {
		t.Fatalf("BuildIngestSpec returned ErrNotImplemented")
	}
}

func TestFactoryReturnsConnectorAdapter(t *testing.T) {
	t.Parallel()
	var _ adapters.ConnectorAdapter = Factory().New()
}
