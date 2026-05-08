// Package csv is the Go port of
// `services/connector-management-service/src/connectors/csv.rs` — the
// CSV file connector that reads CSV objects from URLs or local storage
// paths.
//
// Capability mapping:
//
//   - DiscoverSources    → emit a single synthetic source derived from
//     the configured url/path; CSV connectors are configured directly,
//     they do not advertise a remote catalog.
//   - QueryVirtualTable  → parse the CSV with stdlib encoding/csv,
//     return JSON rows bounded by the request limit ([1, 500]).
//   - StreamArrow        → encode the parsed rows as a single Arrow IPC
//     frame with all columns typed as Utf8 (mirrors the JSON-row shape
//     and matches the bigquery adapter's encoding strategy).
//   - BuildIngestSpec    → emit a "csv" descriptor carrying the source
//     identity (url/path).
//
// The adapter uses [http.DefaultClient]; tests inject a fake client
// through [Adapter.SetHTTPClient].
package csv

import (
	"bytes"
	"context"
	stdcsv "encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

// ConnectorType is the `connections.connector_type` value the registry
// binds this adapter under.
const ConnectorType = "csv"

const defaultSourceKind = "csv_file"

// Adapter is the csv [adapters.ConnectorAdapter] implementation.
type Adapter struct {
	httpClient *http.Client
}

// New returns a ready-to-use [Adapter] backed by [http.DefaultClient].
func New() *Adapter {
	return &Adapter{httpClient: http.DefaultClient}
}

// Factory returns an [adapters.Factory] that yields fresh Adapters.
func Factory() adapters.Factory {
	return adapters.FactoryFunc(func() adapters.ConnectorAdapter { return New() })
}

// SetHTTPClient overrides the embedded HTTP client.
func (a *Adapter) SetHTTPClient(client *http.Client) {
	if client != nil {
		a.httpClient = client
	}
}

type csvConfig struct {
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
		return errors.New("csv connector requires either 'url' or 'path'")
	}
	return nil
}

func parseConfig(raw json.RawMessage) (*csvConfig, error) {
	cfg := &csvConfig{}
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("csv: invalid config: %w", err)
	}
	return cfg, nil
}

// DiscoverSources emits a synthetic single source from the configured
// url/path. Mirrors the dispatcher behaviour where csv connectors are
// configured by direct identity rather than catalog discovery.
func (a *Adapter) DiscoverSources(_ context.Context, c *models.Connection, _ string) ([]adapters.Source, error) {
	cfg, err := a.cfg(c)
	if err != nil {
		return nil, err
	}
	selector := sourceLabel(cfg)
	display := fileBaseName(cfg, selector, ".csv")
	meta, _ := json.Marshal(map[string]any{
		"source": selector,
		"format": "csv",
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

// QueryVirtualTable parses the CSV file via stdlib encoding/csv and
// returns the requested number of rows, with all values surfaced as
// strings (mirrors Rust's behaviour, which returns `Value::String` per
// cell).
func (a *Adapter) QueryVirtualTable(ctx context.Context, c *models.Connection, q *adapters.Query, _ string) (*adapters.Result, error) {
	if q == nil {
		return nil, errors.New("csv: query request is nil")
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
	headers, rows, err := parseCSV(data, limit)
	if err != nil {
		return nil, err
	}
	rawRows := make([]json.RawMessage, 0, len(rows))
	for _, row := range rows {
		buf, marshalErr := json.Marshal(row)
		if marshalErr != nil {
			return nil, fmt.Errorf("csv: marshal preview row: %w", marshalErr)
		}
		rawRows = append(rawRows, buf)
	}
	meta, _ := json.Marshal(map[string]any{
		"source": sourceLabel(cfg),
		"format": "csv",
		"bytes":  len(data),
	})
	return &adapters.Result{
		Selector: q.Selector,
		Mode:     "preview",
		Columns:  headers,
		RowCount: len(rawRows),
		Rows:     rawRows,
		Metadata: meta,
	}, nil
}

// StreamArrow encodes the entire CSV file as a single Arrow IPC frame,
// with every column typed as Utf8.
func (a *Adapter) StreamArrow(ctx context.Context, c *models.Connection, _ *adapters.Query, _ string) (adapters.ArrowStream, error) {
	cfg, err := a.cfg(c)
	if err != nil {
		return nil, err
	}
	data, err := a.readSource(ctx, cfg)
	if err != nil {
		return nil, err
	}
	headers, rows, err := parseCSVAll(data)
	if err != nil {
		return nil, err
	}
	frame, err := encodeArrowIPC(headers, rows)
	if err != nil {
		return nil, err
	}
	return &singleFrameStream{frame: frame}, nil
}

// BuildIngestSpec emits a csv sync descriptor.
func (a *Adapter) BuildIngestSpec(_ context.Context, c *models.Connection, src *adapters.Source) (*adapters.IngestSpec, error) {
	if c == nil {
		return nil, errors.New("csv: connection is nil")
	}
	cfg, err := parseConfig(c.Config)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.URL) == "" && strings.TrimSpace(cfg.Path) == "" {
		return nil, errors.New("csv: connection config missing 'url' or 'path'")
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
		return nil, fmt.Errorf("csv: marshal ingest spec: %w", err)
	}
	return &adapters.IngestSpec{
		Name:      c.Name,
		Namespace: "default",
		Source:    ConnectorType,
		Config:    raw,
	}, nil
}

// CountRows decodes the CSV bytes and counts data rows (excluding the
// header). Exposed for test_connection / fetch_dataset parity with
// Rust's `count_rows`.
func CountRows(data []byte) (int64, error) {
	r := stdcsv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	if _, err := r.Read(); err != nil {
		if errors.Is(err, io.EOF) {
			return 0, nil
		}
		return 0, fmt.Errorf("csv: read header: %w", err)
	}
	var total int64
	for {
		_, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return 0, fmt.Errorf("csv: read row: %w", err)
		}
		total++
	}
	return total, nil
}

func (a *Adapter) cfg(c *models.Connection) (*csvConfig, error) {
	if c == nil {
		return nil, errors.New("csv: connection is nil")
	}
	cfg, err := parseConfig(c.Config)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.URL) == "" && strings.TrimSpace(cfg.Path) == "" {
		return nil, errors.New("csv connector requires either 'url' or 'path'")
	}
	return cfg, nil
}

func (a *Adapter) readSource(ctx context.Context, cfg *csvConfig) ([]byte, error) {
	if cfg.Path != "" {
		bytes, err := os.ReadFile(cfg.Path)
		if err != nil {
			return nil, fmt.Errorf("csv: read path %q: %w", cfg.Path, err)
		}
		return bytes, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("csv: build request: %w", err)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("csv: source transport error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("CSV source returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("csv: read response: %w", err)
	}
	return body, nil
}

func parseCSV(data []byte, limit int) ([]string, []map[string]any, error) {
	r := stdcsv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	headers, err := r.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return []string{}, []map[string]any{}, nil
		}
		return nil, nil, fmt.Errorf("csv: read header: %w", err)
	}
	rows := make([]map[string]any, 0, limit)
	for len(rows) < limit {
		record, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, nil, fmt.Errorf("csv: read row: %w", err)
		}
		rows = append(rows, recordToMap(headers, record))
	}
	return headers, rows, nil
}

func parseCSVAll(data []byte) ([]string, []map[string]any, error) {
	r := stdcsv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	headers, err := r.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return []string{}, []map[string]any{}, nil
		}
		return nil, nil, fmt.Errorf("csv: read header: %w", err)
	}
	rows := []map[string]any{}
	for {
		record, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, nil, fmt.Errorf("csv: read row: %w", err)
		}
		rows = append(rows, recordToMap(headers, record))
	}
	return headers, rows, nil
}

func recordToMap(headers, record []string) map[string]any {
	row := make(map[string]any, len(headers))
	for i, h := range headers {
		if i < len(record) {
			row[h] = record[i]
		} else {
			row[h] = ""
		}
	}
	return row
}

func encodeArrowIPC(columns []string, rows []map[string]any) ([]byte, error) {
	mem := memory.NewGoAllocator()
	fields := make([]arrow.Field, 0, len(columns))
	arrays := make([]arrow.Array, 0, len(columns))
	for _, name := range columns {
		fields = append(fields, arrow.Field{Name: name, Type: arrow.BinaryTypes.String, Nullable: true})
		builder := array.NewStringBuilder(mem)
		for _, row := range rows {
			value, ok := row[name]
			if !ok || value == nil {
				builder.AppendNull()
				continue
			}
			switch v := value.(type) {
			case string:
				builder.Append(v)
			default:
				builder.Append(fmt.Sprint(v))
			}
		}
		arr := builder.NewArray()
		arrays = append(arrays, arr)
		builder.Release()
	}
	schema := arrow.NewSchema(fields, nil)
	rec := array.NewRecord(schema, arrays, int64(len(rows)))
	defer rec.Release()
	for _, arr := range arrays {
		arr.Release()
	}
	var buf bytes.Buffer
	writer := ipc.NewWriter(&buf, ipc.WithSchema(schema), ipc.WithAllocator(mem))
	if err := writer.Write(rec); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("csv: write arrow record: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("csv: close arrow stream: %w", err)
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

func sourceLabel(cfg *csvConfig) string {
	if cfg.Path != "" {
		return cfg.Path
	}
	if cfg.URL != "" {
		return cfg.URL
	}
	return "csv"
}

func fileBaseName(cfg *csvConfig, selector, fallbackExt string) string {
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
		stem = "csv_sync"
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
