package parquet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

func TestValidateConfigRequiresURLOrPath(t *testing.T) {
	t.Parallel()
	if err := ValidateConfig(json.RawMessage(`{}`)); err == nil {
		t.Fatalf("expected error for empty config")
	}
	if err := ValidateConfig(json.RawMessage(`{"url":"https://example.com/a.parquet"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateConfig(json.RawMessage(`{"path":"/tmp/a.parquet"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateMagicAcceptsMinimalEnvelope(t *testing.T) {
	t.Parallel()
	bytes := append([]byte("PAR1"), 0, 0, 0, 0, 0, 0, 0, 0)
	bytes = append(bytes, "PAR1"...)
	if err := validateMagic(bytes); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateMagicRejectsBadHeader(t *testing.T) {
	t.Parallel()
	bytes := []byte{'X', 'X', 'X', 'X', 0, 0, 0, 0, 'P', 'A', 'R', '1'}
	if err := validateMagic(bytes); err == nil {
		t.Fatalf("expected header magic error")
	}
}

func TestValidateMagicRejectsBadFooter(t *testing.T) {
	t.Parallel()
	bytes := []byte{'P', 'A', 'R', '1', 0, 0, 0, 0, 'X', 'X', 'X', 'X'}
	if err := validateMagic(bytes); err == nil {
		t.Fatalf("expected footer magic error")
	}
}

func TestValidateMagicRejectsTruncated(t *testing.T) {
	t.Parallel()
	if err := validateMagic([]byte("PAR1")); err == nil {
		t.Fatalf("expected truncated-file error")
	}
}

func TestDiscoverSourcesEmitsSyntheticEntry(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{"url":"https://example.com/data/orders.parquet"}`)
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
}

func TestQueryVirtualTableReadsParquetFromHTTP(t *testing.T) {
	t.Parallel()
	data := buildSampleParquet(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	conn := &models.Connection{
		ConnectorType: ConnectorType,
		Config:        json.RawMessage(`{"url":"` + srv.URL + `/orders.parquet"}`),
	}
	a := New()
	a.SetHTTPClient(srv.Client())
	limit := 1
	res, err := a.QueryVirtualTable(context.Background(), conn, &adapters.Query{Selector: "orders", Limit: &limit}, "")
	if err != nil {
		t.Fatalf("QueryVirtualTable: %v", err)
	}
	if res.RowCount != 1 {
		t.Fatalf("row_count = %d, want 1", res.RowCount)
	}
	if len(res.Columns) != 2 || res.Columns[0] != "id" || res.Columns[1] != "name" {
		t.Fatalf("columns = %v", res.Columns)
	}
	var row map[string]any
	if err := json.Unmarshal(res.Rows[0], &row); err != nil {
		t.Fatalf("decode row: %v", err)
	}
	if row["name"] != "alice" {
		t.Fatalf("first row = %v", row)
	}
}

func TestQueryVirtualTableReadsParquetFromPath(t *testing.T) {
	t.Parallel()
	data := buildSampleParquet(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "orders.parquet")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	conn := &models.Connection{
		ConnectorType: ConnectorType,
		Config:        json.RawMessage(`{"path":"` + path + `"}`),
	}
	res, err := New().QueryVirtualTable(context.Background(), conn, &adapters.Query{Selector: "orders"}, "")
	if err != nil {
		t.Fatalf("QueryVirtualTable: %v", err)
	}
	if res.RowCount != 3 {
		t.Fatalf("row_count = %d, want 3", res.RowCount)
	}
}

func TestStreamArrowReturnsIPCFrame(t *testing.T) {
	t.Parallel()
	data := buildSampleParquet(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "orders.parquet")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	conn := &models.Connection{
		ConnectorType: ConnectorType,
		Config:        json.RawMessage(`{"path":"` + path + `"}`),
	}
	stream, err := New().StreamArrow(context.Background(), conn, &adapters.Query{Selector: "orders"}, "")
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

func TestQueryVirtualTableRejectsNonParquetBytes(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not parquet bytes"))
	}))
	defer srv.Close()
	conn := &models.Connection{
		ConnectorType: ConnectorType,
		Config:        json.RawMessage(`{"url":"` + srv.URL + `/data"}`),
	}
	a := New()
	a.SetHTTPClient(srv.Client())
	_, err := a.QueryVirtualTable(context.Background(), conn, &adapters.Query{Selector: "data"}, "")
	if err == nil {
		t.Fatalf("expected error for non-parquet bytes")
	}
}

func TestBuildIngestSpecEmitsParquetDescriptor(t *testing.T) {
	t.Parallel()
	conn := &models.Connection{
		Name:          "raw-orders",
		ConnectorType: ConnectorType,
		Config:        json.RawMessage(`{"url":"https://example.com/data.parquet"}`),
	}
	src := &adapters.Source{Selector: "orders"}
	spec, err := New().BuildIngestSpec(context.Background(), conn, src)
	if err != nil {
		t.Fatalf("BuildIngestSpec: %v", err)
	}
	if spec.Name != "raw-orders" {
		t.Fatalf("name = %q", spec.Name)
	}
	if spec.Source != ConnectorType {
		t.Fatalf("source = %q", spec.Source)
	}
	var cfg map[string]any
	if err := json.Unmarshal(spec.Config, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if cfg["url"] != "https://example.com/data.parquet" {
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
		t.Fatalf("expected error for missing identity")
	}
}

func TestQueryVirtualTableRejectsNilRequest(t *testing.T) {
	t.Parallel()
	conn := &models.Connection{ConnectorType: ConnectorType, Config: json.RawMessage(`{"path":"/tmp/x.parquet"}`)}
	if _, err := New().QueryVirtualTable(context.Background(), conn, nil, ""); err == nil {
		t.Fatalf("expected nil-request error")
	}
}

func TestFactoryReturnsConnectorAdapter(t *testing.T) {
	t.Parallel()
	var _ adapters.ConnectorAdapter = Factory().New()
}

func TestReadSourceHTTPNon2xx(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer srv.Close()
	a := New()
	a.SetHTTPClient(srv.Client())
	_, err := a.readSource(context.Background(), &parquetConfig{URL: srv.URL + "/missing"})
	if err == nil {
		t.Fatalf("expected error on 404")
	}
}

func TestUnsupportedNotImplementedSentinelNotUsed(t *testing.T) {
	// The parquet adapter implements all four capabilities; ensure none of
	// them returns the "not implemented" sentinel.
	t.Parallel()
	data := buildSampleParquet(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "orders.parquet")
	if err := os.WriteFile(path, data, 0o644); err != nil {
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

// buildSampleParquet builds a tiny 3-row Parquet file (id:int64, name:utf8)
// and returns the encoded bytes. Used as a hermetic source for the
// reader-side tests.
func buildSampleParquet(t *testing.T) []byte {
	t.Helper()
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: false},
	}, nil)
	idBuilder := array.NewInt64Builder(mem)
	defer idBuilder.Release()
	nameBuilder := array.NewStringBuilder(mem)
	defer nameBuilder.Release()
	idBuilder.AppendValues([]int64{1, 2, 3}, nil)
	nameBuilder.AppendValues([]string{"alice", "bob", "carol"}, nil)
	idArr := idBuilder.NewArray()
	defer idArr.Release()
	nameArr := nameBuilder.NewArray()
	defer nameArr.Release()
	rec := array.NewRecord(schema, []arrow.Array{idArr, nameArr}, 3)
	defer rec.Release()

	var buf bytes.Buffer
	writer, err := pqarrow.NewFileWriter(schema, &buf, nil, pqarrow.NewArrowWriterProperties())
	if err != nil {
		t.Fatalf("NewFileWriter: %v", err)
	}
	if err := writer.Write(rec); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf.Bytes()
}
