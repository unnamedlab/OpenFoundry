// Package parquet is the Go port of
// `services/connector-management-service/src/connectors/parquet.rs` — the
// Parquet file connector that reads Parquet objects from URLs or local
// storage paths.
//
// Where the Rust implementation only validates the `PAR1` magic markers
// and streams raw bytes downstream (decoding is deferred to the lake
// materialiser), the Go port leans on apache/arrow-go's pqarrow reader so
// that virtual-table previews and Arrow IPC streaming can be served
// directly from the adapter — matching the functional contract pinned by
// CMA-10b ("Use apache/arrow-go/parquet for read").
//
// Capability mapping:
//
//   - DiscoverSources    → emit a single synthetic source derived from the
//     configured url/path; mirrors the Rust dispatcher behaviour where the
//     parquet connector is registered/queried by direct configuration
//     (no upstream catalog).
//   - QueryVirtualTable  → decode rows via pqarrow, return JSON rows
//     bounded by the request limit ([1, 500]).
//   - StreamArrow        → re-encode the decoded Arrow record(s) as a
//     single Arrow IPC frame.
//   - BuildIngestSpec    → emit a "parquet" descriptor carrying the
//     source identity (url/path) so ingestion-replication-service can pick
//     up the bytes once the bridge port lands.
//
// The adapter uses [http.DefaultClient] for HTTP fetches; tests inject a
// fake client through [Adapter.SetHTTPClient] to keep the suite hermetic.
package parquet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

// ConnectorType is the `connections.connector_type` value the registry
// binds this adapter under.
const ConnectorType = "parquet"

const defaultSourceKind = "parquet_file"

var parquetMagic = []byte("PAR1")

// Adapter is the parquet [adapters.ConnectorAdapter] implementation.
type Adapter struct {
	httpClient *http.Client
}

// New returns a ready-to-use [Adapter] backed by [http.DefaultClient].
func New() *Adapter {
	return &Adapter{httpClient: http.DefaultClient}
}

// Factory returns an [adapters.Factory] that yields fresh Adapters; the
// HTTP client is reused inside each instance, but per-instance state
// keeps test overrides scoped.
func Factory() adapters.Factory {
	return adapters.FactoryFunc(func() adapters.ConnectorAdapter { return New() })
}

// SetHTTPClient overrides the embedded HTTP client. Used by tests to
// point at httptest servers.
func (a *Adapter) SetHTTPClient(client *http.Client) {
	if client != nil {
		a.httpClient = client
	}
}

type parquetConfig struct {
	URL  string `json:"url"`
	Path string `json:"path"`
}

// ValidateConfig mirrors Rust's `validate_config`: require a `url` or
// `path` identity field.
func ValidateConfig(raw json.RawMessage) error {
	cfg, err := parseConfig(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.URL) == "" && strings.TrimSpace(cfg.Path) == "" {
		return errors.New("parquet connector requires either 'url' or 'path'")
	}
	return nil
}

func parseConfig(raw json.RawMessage) (*parquetConfig, error) {
	cfg := &parquetConfig{}
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parquet: invalid config: %w", err)
	}
	return cfg, nil
}

// DiscoverSources emits a synthetic single source from the configured
// url/path. The selector mirrors the file's basename so registrations
// can refer to it by a stable, human-readable name.
func (a *Adapter) DiscoverSources(_ context.Context, c *models.Connection, _ string) ([]adapters.Source, error) {
	cfg, err := a.cfg(c)
	if err != nil {
		return nil, err
	}
	selector := sourceLabel(cfg)
	display := fileBaseName(cfg, selector, ".parquet")
	meta, _ := json.Marshal(map[string]any{
		"source": selector,
		"format": "parquet",
	})
	return []adapters.Source{{
		Selector:         selector,
		DisplayName:      display,
		SourceKind:       defaultSourceKind,
		SupportsSync:     true,
		SupportsZeroCopy: false,
		Metadata:         meta,
	}}, nil
}

// QueryVirtualTable decodes rows from the source via pqarrow and returns
// them as JSON, bounded by the request limit (default 50, clamped to
// [1, 500] to match Rust's behaviour).
func (a *Adapter) QueryVirtualTable(ctx context.Context, c *models.Connection, q *adapters.Query, _ string) (*adapters.Result, error) {
	if q == nil {
		return nil, errors.New("parquet: query request is nil")
	}
	cfg, err := a.cfg(c)
	if err != nil {
		return nil, err
	}
	limit := clampLimit(q.Limit, 50)
	data, err := a.readSource(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := validateMagic(data); err != nil {
		return nil, err
	}

	cols, rows, err := decodeRows(ctx, data, limit)
	if err != nil {
		return nil, err
	}
	rawRows := make([]json.RawMessage, 0, len(rows))
	for _, row := range rows {
		buf, marshalErr := json.Marshal(row)
		if marshalErr != nil {
			return nil, fmt.Errorf("parquet: marshal preview row: %w", marshalErr)
		}
		rawRows = append(rawRows, buf)
	}
	meta, _ := json.Marshal(map[string]any{
		"source": sourceLabel(cfg),
		"format": "parquet",
		"bytes":  len(data),
	})
	return &adapters.Result{
		Selector: q.Selector,
		Mode:     "preview",
		Columns:  cols,
		RowCount: len(rawRows),
		Rows:     rawRows,
		Metadata: meta,
	}, nil
}

// StreamArrow re-encodes the parquet file as Arrow IPC and returns a
// single-frame stream.
func (a *Adapter) StreamArrow(ctx context.Context, c *models.Connection, _ *adapters.Query, _ string) (adapters.ArrowStream, error) {
	cfg, err := a.cfg(c)
	if err != nil {
		return nil, err
	}
	data, err := a.readSource(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := validateMagic(data); err != nil {
		return nil, err
	}
	frame, err := encodeArrowIPC(ctx, data)
	if err != nil {
		return nil, err
	}
	return &singleFrameStream{frame: frame}, nil
}

// BuildIngestSpec emits a parquet sync descriptor. The bridge port to
// ingestion-replication-service is pending; the shape here is the
// agreed structural placeholder from CMA-0.
func (a *Adapter) BuildIngestSpec(_ context.Context, c *models.Connection, src *adapters.Source) (*adapters.IngestSpec, error) {
	if c == nil {
		return nil, errors.New("parquet: connection is nil")
	}
	cfg, err := parseConfig(c.Config)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.URL) == "" && strings.TrimSpace(cfg.Path) == "" {
		return nil, errors.New("parquet: connection config missing 'url' or 'path'")
	}
	specCfg := map[string]any{}
	if cfg.URL != "" {
		specCfg["url"] = cfg.URL
	}
	if cfg.Path != "" {
		specCfg["path"] = cfg.Path
	}
	if src != nil && src.Selector != "" {
		specCfg["selector"] = src.Selector
	}
	raw, err := json.Marshal(specCfg)
	if err != nil {
		return nil, fmt.Errorf("parquet: marshal ingest spec: %w", err)
	}
	return &adapters.IngestSpec{
		Name:      c.Name,
		Namespace: "default",
		Source:    ConnectorType,
		Config:    raw,
	}, nil
}

func (a *Adapter) cfg(c *models.Connection) (*parquetConfig, error) {
	if c == nil {
		return nil, errors.New("parquet: connection is nil")
	}
	cfg, err := parseConfig(c.Config)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.URL) == "" && strings.TrimSpace(cfg.Path) == "" {
		return nil, errors.New("parquet connector requires either 'url' or 'path'")
	}
	return cfg, nil
}

func (a *Adapter) readSource(ctx context.Context, cfg *parquetConfig) ([]byte, error) {
	if cfg.Path != "" {
		bytes, err := os.ReadFile(cfg.Path)
		if err != nil {
			return nil, fmt.Errorf("parquet: read path %q: %w", cfg.Path, err)
		}
		return bytes, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("parquet: build request: %w", err)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("parquet: source transport error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Parquet source returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parquet: read response: %w", err)
	}
	return body, nil
}

func validateMagic(data []byte) error {
	if len(data) < 8 {
		return fmt.Errorf("parquet file too small (%d bytes) — missing magic markers", len(data))
	}
	if !bytes.Equal(data[:4], parquetMagic) {
		return errors.New("parquet header magic 'PAR1' missing at start of file")
	}
	if !bytes.Equal(data[len(data)-4:], parquetMagic) {
		return errors.New("parquet footer magic 'PAR1' missing at end of file")
	}
	return nil
}

func decodeRows(ctx context.Context, data []byte, limit int) ([]string, []map[string]any, error) {
	pf, err := file.NewParquetReader(bytes.NewReader(data))
	if err != nil {
		return nil, nil, fmt.Errorf("parquet: open: %w", err)
	}
	defer pf.Close()

	mem := memory.NewGoAllocator()
	arrReader, err := pqarrow.NewFileReader(pf, pqarrow.ArrowReadProperties{}, mem)
	if err != nil {
		return nil, nil, fmt.Errorf("parquet: arrow reader: %w", err)
	}
	rr, err := arrReader.GetRecordReader(ctx, nil, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("parquet: record reader: %w", err)
	}
	defer rr.Release()

	cols := []string{}
	rows := make([]map[string]any, 0, limit)
	for rr.Next() && len(rows) < limit {
		rec := rr.Record()
		if len(cols) == 0 {
			for i := 0; i < int(rec.NumCols()); i++ {
				cols = append(cols, rec.Schema().Field(i).Name)
			}
		}
		buf, err := rec.MarshalJSON()
		if err != nil {
			return nil, nil, fmt.Errorf("parquet: record to json: %w", err)
		}
		var batch []map[string]any
		if err := json.Unmarshal(buf, &batch); err != nil {
			return nil, nil, fmt.Errorf("parquet: decode record json: %w", err)
		}
		for _, row := range batch {
			if len(rows) >= limit {
				break
			}
			rows = append(rows, row)
		}
	}
	return cols, rows, nil
}

func encodeArrowIPC(ctx context.Context, data []byte) ([]byte, error) {
	pf, err := file.NewParquetReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parquet: open: %w", err)
	}
	defer pf.Close()

	mem := memory.NewGoAllocator()
	arrReader, err := pqarrow.NewFileReader(pf, pqarrow.ArrowReadProperties{}, mem)
	if err != nil {
		return nil, fmt.Errorf("parquet: arrow reader: %w", err)
	}
	rr, err := arrReader.GetRecordReader(ctx, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("parquet: record reader: %w", err)
	}
	defer rr.Release()

	var buf bytes.Buffer
	var writer *ipc.Writer
	for rr.Next() {
		rec := rr.Record()
		if writer == nil {
			writer = ipc.NewWriter(&buf, ipc.WithSchema(rec.Schema()), ipc.WithAllocator(mem))
		}
		if err := writer.Write(rec); err != nil {
			_ = writer.Close()
			return nil, fmt.Errorf("parquet: write arrow record: %w", err)
		}
	}
	if writer == nil {
		return nil, errors.New("parquet: no records to encode")
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("parquet: close arrow stream: %w", err)
	}
	return buf.Bytes(), nil
}

func clampLimit(requested *int, fallback int) int {
	limit := fallback
	if requested != nil {
		limit = *requested
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	return limit
}

func sourceLabel(cfg *parquetConfig) string {
	if cfg.Path != "" {
		return cfg.Path
	}
	if cfg.URL != "" {
		return cfg.URL
	}
	return "parquet"
}

func fileBaseName(cfg *parquetConfig, selector, fallbackExt string) string {
	if cfg.Path != "" {
		if base := filepath.Base(cfg.Path); base != "" && base != "." && base != "/" {
			return base
		}
	}
	if cfg.URL != "" {
		if idx := strings.LastIndex(cfg.URL, "/"); idx >= 0 && idx < len(cfg.URL)-1 {
			tail := cfg.URL[idx+1:]
			if q := strings.Index(tail, "?"); q >= 0 {
				tail = tail[:q]
			}
			if tail != "" {
				return tail
			}
		}
	}
	stem := sanitizeStem(selector)
	if stem == "" {
		stem = "parquet_sync"
	}
	return stem + fallbackExt
}

func sanitizeStem(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch >= 'a' && ch <= 'z',
			ch >= 'A' && ch <= 'Z',
			ch >= '0' && ch <= '9':
			out = append(out, ch)
		default:
			out = append(out, '_')
		}
	}
	return strings.Trim(string(out), "_")
}

type singleFrameStream struct {
	frame    []byte
	consumed bool
}

func (s *singleFrameStream) Next(_ context.Context) ([]byte, error) {
	if s.consumed {
		return nil, io.EOF
	}
	s.consumed = true
	return s.frame, nil
}

func (s *singleFrameStream) Close() error { return nil }
