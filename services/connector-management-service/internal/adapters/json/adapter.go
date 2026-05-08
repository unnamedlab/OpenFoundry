// Package json is the Go port of
// `services/connector-management-service/src/connectors/json.rs` — the
// JSON / NDJSON file connector that reads JSON objects or arrays from
// URLs or local storage paths.
//
// Capability mapping:
//
//   - DiscoverSources    → emit a single synthetic source derived from
//     the configured url/path; JSON connectors are configured directly,
//     no upstream catalog is queried.
//   - QueryVirtualTable  → parse the JSON payload (array, single object,
//     or NDJSON one-object-per-line) and return rows bounded by the
//     request limit ([1, 500]).
//   - StreamArrow        → encode the parsed rows as a single Arrow IPC
//     frame with all columns typed as Utf8 (mirrors the bigquery / csv
//     adapters' encoding strategy).
//   - BuildIngestSpec    → emit a "json" descriptor carrying the source
//     identity (url/path).
//
// The adapter uses [http.DefaultClient]; tests inject a fake client
// through [Adapter.SetHTTPClient].
package json

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
const ConnectorType = "json"

const defaultSourceKind = "json_file"

// Adapter is the json [adapters.ConnectorAdapter] implementation.
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

type jsonConfig struct {
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
		return errors.New("json connector requires either 'url' or 'path'")
	}
	return nil
}

func parseConfig(raw json.RawMessage) (*jsonConfig, error) {
	cfg := &jsonConfig{}
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("json: invalid config: %w", err)
	}
	return cfg, nil
}

// DiscoverSources emits a synthetic single source from the configured
// url/path.
func (a *Adapter) DiscoverSources(_ context.Context, c *models.Connection, _ string) ([]adapters.Source, error) {
	cfg, err := a.cfg(c)
	if err != nil {
		return nil, err
	}
	selector := sourceLabel(cfg)
	display := fileBaseName(cfg, selector, ".json")
	meta, _ := json.Marshal(map[string]any{
		"source": selector,
		"format": "json",
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

// QueryVirtualTable parses the JSON payload (array of objects, single
// object, or NDJSON) and returns rows bounded by the request limit.
func (a *Adapter) QueryVirtualTable(ctx context.Context, c *models.Connection, q *adapters.Query, _ string) (*adapters.Result, error) {
	if q == nil {
		return nil, errors.New("json: query request is nil")
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
	rows, err := parseRows(data, limit)
	if err != nil {
		return nil, err
	}
	rawRows := make([]json.RawMessage, 0, len(rows))
	for _, row := range rows {
		buf, marshalErr := json.Marshal(row)
		if marshalErr != nil {
			return nil, fmt.Errorf("json: marshal preview row: %w", marshalErr)
		}
		rawRows = append(rawRows, buf)
	}
	cols := collectColumns(rows)
	meta, _ := json.Marshal(map[string]any{
		"source": sourceLabel(cfg),
		"format": "json",
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

// StreamArrow encodes the entire JSON payload as a single Arrow IPC
// frame with every column typed as Utf8 (numbers and booleans are
// stringified — the json connector's primary surface is preview /
// inspection rather than typed analytics).
func (a *Adapter) StreamArrow(ctx context.Context, c *models.Connection, _ *adapters.Query, _ string) (adapters.ArrowStream, error) {
	cfg, err := a.cfg(c)
	if err != nil {
		return nil, err
	}
	data, err := a.readSource(ctx, cfg)
	if err != nil {
		return nil, err
	}
	rows, err := parseRows(data, 0)
	if err != nil {
		return nil, err
	}
	cols := collectColumns(rows)
	frame, err := encodeArrowIPC(cols, rows)
	if err != nil {
		return nil, err
	}
	return &singleFrameStream{frame: frame}, nil
}

// BuildIngestSpec emits a json sync descriptor.
func (a *Adapter) BuildIngestSpec(_ context.Context, c *models.Connection, src *adapters.Source) (*adapters.IngestSpec, error) {
	if c == nil {
		return nil, errors.New("json: connection is nil")
	}
	cfg, err := parseConfig(c.Config)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.URL) == "" && strings.TrimSpace(cfg.Path) == "" {
		return nil, errors.New("json: connection config missing 'url' or 'path'")
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
		return nil, fmt.Errorf("json: marshal ingest spec: %w", err)
	}
	return &adapters.IngestSpec{
		Name:      c.Name,
		Namespace: "default",
		Source:    ConnectorType,
		Config:    raw,
	}, nil
}

// CountRows counts rows in the JSON payload. Array → length, single
// object → 1, NDJSON (one JSON value per line, possibly object or
// scalar) → non-empty line/value count, empty → 0. The Go port uses a
// streaming json.Decoder so NDJSON files are counted correctly even when
// the first byte is `{` (Rust's `count_rows` returns 1 in that case
// because it short-circuits on the first character).
func CountRows(data []byte) (int64, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return 0, nil
	}
	if trimmed[0] == '[' {
		var rows []json.RawMessage
		if err := json.Unmarshal(trimmed, &rows); err != nil {
			return 0, fmt.Errorf("json: count rows array: %w", err)
		}
		return int64(len(rows)), nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var total int64
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return countNonEmptyLines(data), nil
		}
		total++
	}
	return total, nil
}

func countNonEmptyLines(data []byte) int64 {
	var total int64
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		if line := bytes.TrimSpace(scanner.Bytes()); len(line) > 0 {
			total++
		}
	}
	return total
}

func (a *Adapter) cfg(c *models.Connection) (*jsonConfig, error) {
	if c == nil {
		return nil, errors.New("json: connection is nil")
	}
	cfg, err := parseConfig(c.Config)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.URL) == "" && strings.TrimSpace(cfg.Path) == "" {
		return nil, errors.New("json connector requires either 'url' or 'path'")
	}
	return cfg, nil
}

func (a *Adapter) readSource(ctx context.Context, cfg *jsonConfig) ([]byte, error) {
	if cfg.Path != "" {
		bytes, err := os.ReadFile(cfg.Path)
		if err != nil {
			return nil, fmt.Errorf("json: read path %q: %w", cfg.Path, err)
		}
		return bytes, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("json: build request: %w", err)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("json: source transport error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("JSON source returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("json: read response: %w", err)
	}
	return body, nil
}

// parseRows parses the JSON payload into row maps. Accepts:
//
//   - JSON array of objects (`[{...}, {...}]`)
//   - Single JSON object (`{...}`) → wrapped in a single-element slice
//   - NDJSON / JSON-stream (one JSON value per line or whitespace-
//     separated), decoded via stdlib json.Decoder; non-object values are
//     wrapped as `{"value": <scalar>}` to match Rust's `json!({"value":
//     other})` fallback.
//
// limit == 0 means "no cap" (used by StreamArrow).
func parseRows(data []byte, limit int) ([]map[string]any, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return []map[string]any{}, nil
	}
	if trimmed[0] == '[' {
		var raw []json.RawMessage
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			return nil, fmt.Errorf("json: decode array: %w", err)
		}
		out := make([]map[string]any, 0, len(raw))
		for _, item := range raw {
			if limit > 0 && len(out) >= limit {
				break
			}
			row, err := decodeRow(item)
			if err != nil {
				return nil, err
			}
			out = append(out, row)
		}
		return out, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	out := []map[string]any{}
	for {
		if limit > 0 && len(out) >= limit {
			break
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("json: decode value: %w", err)
		}
		row, err := decodeRow(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

func decodeRow(raw json.RawMessage) (map[string]any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return map[string]any{}, nil
	}
	if trimmed[0] == '{' {
		var row map[string]any
		if err := json.Unmarshal(trimmed, &row); err != nil {
			return nil, fmt.Errorf("json: decode object: %w", err)
		}
		return row, nil
	}
	var v any
	if err := json.Unmarshal(trimmed, &v); err != nil {
		return nil, fmt.Errorf("json: decode value: %w", err)
	}
	return map[string]any{"value": v}, nil
}

func collectColumns(rows []map[string]any) []string {
	seen := map[string]struct{}{}
	cols := []string{}
	for _, row := range rows {
		for k := range row {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			cols = append(cols, k)
		}
	}
	sort.Strings(cols)
	return cols
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
				buf, err := json.Marshal(v)
				if err != nil {
					builder.AppendNull()
					continue
				}
				builder.Append(string(buf))
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
		return nil, fmt.Errorf("json: write arrow record: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("json: close arrow stream: %w", err)
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

func sourceLabel(cfg *jsonConfig) string {
	if cfg.Path != "" {
		return cfg.Path
	}
	if cfg.URL != "" {
		return cfg.URL
	}
	return "json"
}

func fileBaseName(cfg *jsonConfig, selector, fallbackExt string) string {
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
		stem = "json_sync"
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
