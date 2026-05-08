package csv

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

const sampleCSV = "id,name,score\n1,alice,9.5\n2,bob,7.1\n3,carol,8.4\n"

func TestValidateConfigRequiresURLOrPath(t *testing.T) {
	t.Parallel()
	if err := ValidateConfig(json.RawMessage(`{}`)); err == nil {
		t.Fatalf("expected error for empty config")
	}
	if err := ValidateConfig(json.RawMessage(`{"url":"https://example.com/a.csv"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateConfig(json.RawMessage(`{"path":"/tmp/a.csv"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCountRowsExcludesHeader(t *testing.T) {
	t.Parallel()
	got, err := CountRows([]byte(sampleCSV))
	if err != nil {
		t.Fatalf("CountRows: %v", err)
	}
	if got != 3 {
		t.Fatalf("CountRows = %d, want 3", got)
	}
}

func TestCountRowsEmpty(t *testing.T) {
	t.Parallel()
	got, err := CountRows([]byte(""))
	if err != nil {
		t.Fatalf("CountRows: %v", err)
	}
	if got != 0 {
		t.Fatalf("CountRows on empty bytes = %d", got)
	}
}

func TestDiscoverSourcesEmitsSyntheticEntry(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{"url":"https://example.com/data/orders.csv"}`)
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
	if !got[0].SupportsSync {
		t.Fatalf("expected SupportsSync=true")
	}
	if got[0].DisplayName != "orders.csv" {
		t.Fatalf("display = %q", got[0].DisplayName)
	}
}

func TestQueryVirtualTableReadsCSVFromHTTP(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		_, _ = w.Write([]byte(sampleCSV))
	}))
	defer srv.Close()
	conn := &models.Connection{
		ConnectorType: ConnectorType,
		Config:        json.RawMessage(`{"url":"` + srv.URL + `/data.csv"}`),
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
	if len(res.Columns) != 3 {
		t.Fatalf("columns = %v", res.Columns)
	}
	var first map[string]any
	if err := json.Unmarshal(res.Rows[0], &first); err != nil {
		t.Fatalf("decode row: %v", err)
	}
	if first["name"] != "alice" || first["score"] != "9.5" {
		t.Fatalf("first row = %v", first)
	}
}

func TestQueryVirtualTableReadsCSVFromPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(path, []byte(sampleCSV), 0o644); err != nil {
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

func TestStreamArrowEncodesAllRows(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(path, []byte(sampleCSV), 0o644); err != nil {
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

func TestQueryVirtualTableRejectsNilRequest(t *testing.T) {
	t.Parallel()
	conn := &models.Connection{ConnectorType: ConnectorType, Config: json.RawMessage(`{"url":"https://example.com/x.csv"}`)}
	if _, err := New().QueryVirtualTable(context.Background(), conn, nil, ""); err == nil {
		t.Fatalf("expected nil-request error")
	}
}

func TestBuildIngestSpecEmitsCSVDescriptor(t *testing.T) {
	t.Parallel()
	conn := &models.Connection{
		Name:          "raw-orders",
		ConnectorType: ConnectorType,
		Config:        json.RawMessage(`{"url":"https://example.com/data.csv"}`),
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
	if cfg["url"] != "https://example.com/data.csv" {
		t.Fatalf("url = %v", cfg["url"])
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

func TestReadSourceHTTPNon2xx(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer srv.Close()
	a := New()
	a.SetHTTPClient(srv.Client())
	_, err := a.readSource(context.Background(), &csvConfig{URL: srv.URL + "/missing"})
	if err == nil {
		t.Fatalf("expected error on 404")
	}
}

func TestUnsupportedNotImplementedSentinelNotUsed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(path, []byte(sampleCSV), 0o644); err != nil {
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
