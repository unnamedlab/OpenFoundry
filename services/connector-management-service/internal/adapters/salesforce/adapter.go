// Package salesforce is the Go port of the Rust Salesforce connector that
// lives in `services/connector-management-service/src/connectors/salesforce.rs`.
//
// Capabilities mirrored from the Rust module:
//
//   - DiscoverSources    — GET /sobjects to enumerate the org's SObjects.
//   - QueryVirtualTable  — bounded SOQL preview returning JSON rows.
//   - StreamArrow        — paginated SOQL fetch (query / queryAll) materialised
//     as a single Arrow IPC frame so dataset-versioning-service can ingest it.
//   - BuildIngestSpec    — placeholder spec the bridge forwards to
//     ingestion-replication-service; the selector is the SObject name.
//
// Auth surface matches Rust: `instance_url` + `access_token` are mandatory.
// Higher-layer OAuth flows (refresh-token / username-password) live in the
// connector-agent and supply the access token before this adapter is invoked.
package salesforce

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

const (
	// ConnectorType is the `connections.connector_type` value the registry
	// binds this adapter under. Matches the Rust dispatch arm.
	ConnectorType = "salesforce"

	defaultSourceKind = "salesforce_object"
	defaultAPIVersion = "v60.0"

	defaultRowLimit = int64(200)
	maxRowLimit     = int64(2_000)
	minRowLimit     = int64(1)

	defaultMaxPages = 50
	maxMaxPages     = 1_000
	minMaxPages     = 1

	defaultPreviewLimit = 50
	minPreviewLimit     = 1
	maxPreviewLimit     = 500
)

// Adapter implements [adapters.ConnectorAdapter] for Salesforce. It is safe
// for concurrent use; the embedded HTTP client is reused across requests.
type Adapter struct {
	httpClient *http.Client
}

// New returns a ready-to-use [Adapter] backed by [http.DefaultClient].
func New() *Adapter {
	return &Adapter{httpClient: http.DefaultClient}
}

// Factory returns an [adapters.Factory] that constructs fresh Salesforce
// adapters. Stateless, so the singleton pattern would also work; the factory
// shape is kept symmetric with the BigQuery adapter for consistent wiring.
func Factory() adapters.Factory {
	return adapters.FactoryFunc(func() adapters.ConnectorAdapter { return New() })
}

// SetHTTPClient overrides the embedded [http.Client]. Intended for tests
// that point the adapter at an httptest server with custom timeouts.
func (a *Adapter) SetHTTPClient(client *http.Client) {
	if client != nil {
		a.httpClient = client
	}
}

type sfConfig struct {
	InstanceURL    string `json:"instance_url"`
	AccessToken    string `json:"access_token"`
	APIVersion     string `json:"api_version"`
	RowLimit       *int64 `json:"row_limit"`
	IncludeDeleted bool   `json:"include_deleted"`
	MaxPages       *int64 `json:"max_pages"`
	Query          string `json:"query"`
}

func parseConfig(raw json.RawMessage) (*sfConfig, error) {
	cfg := &sfConfig{}
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("salesforce: invalid config: %w", err)
	}
	return cfg, nil
}

// ValidateConfig mirrors Rust's `validate_config`: both `instance_url` and
// `access_token` must be present. Higher layers strip credentials before
// the config reaches this adapter.
func ValidateConfig(raw json.RawMessage) error {
	cfg, err := parseConfig(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.InstanceURL) == "" {
		return errors.New("salesforce connector requires 'instance_url'")
	}
	if cfg.AccessToken == "" {
		return errors.New("salesforce connector requires 'access_token'")
	}
	return nil
}

// DiscoverSources lists every SObject the org exposes. Mirrors Rust's
// `discover_sources`.
func (a *Adapter) DiscoverSources(ctx context.Context, c *models.Connection, _ string) ([]adapters.Source, error) {
	cfg, err := a.cfg(c)
	if err != nil {
		return nil, err
	}
	base, err := apiBaseURL(cfg)
	if err != nil {
		return nil, err
	}
	u, err := base.Parse("sobjects")
	if err != nil {
		return nil, fmt.Errorf("salesforce: build sobjects url: %w", err)
	}
	body, err := a.doGet(ctx, u, cfg.AccessToken, "Salesforce catalog")
	if err != nil {
		return nil, err
	}
	var payload struct {
		SObjects []map[string]any `json:"sobjects"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("salesforce: decode sobjects: %w", err)
	}
	out := make([]adapters.Source, 0, len(payload.SObjects))
	for _, item := range payload.SObjects {
		name, _ := item["name"].(string)
		if name == "" {
			continue
		}
		label, _ := item["label"].(string)
		if label == "" {
			label = name
		}
		meta, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("salesforce: marshal sobject metadata: %w", err)
		}
		out = append(out, adapters.Source{
			Selector:         name,
			DisplayName:      label,
			SourceKind:       defaultSourceKind,
			SupportsSync:     true,
			SupportsZeroCopy: true,
			Metadata:         meta,
		})
	}
	return out, nil
}

// QueryVirtualTable runs a bounded SOQL preview. Mirrors Rust's
// `query_virtual_table` → `bounded_soql` shortcut: a single `LIMIT n` request
// against `query` (never `queryAll`).
func (a *Adapter) QueryVirtualTable(ctx context.Context, c *models.Connection, q *adapters.Query, _ string) (*adapters.Result, error) {
	if q == nil {
		return nil, errors.New("salesforce: query request is nil")
	}
	cfg, err := a.cfg(c)
	if err != nil {
		return nil, err
	}
	limit := previewLimit(q.Limit)
	soql, err := boundedSOQL(q.Selector, limit)
	if err != nil {
		return nil, err
	}
	base, err := apiBaseURL(cfg)
	if err != nil {
		return nil, err
	}
	u, err := base.Parse("query")
	if err != nil {
		return nil, fmt.Errorf("salesforce: build query url: %w", err)
	}
	qv := u.Query()
	qv.Set("q", soql)
	u.RawQuery = qv.Encode()

	body, err := a.doGet(ctx, u, cfg.AccessToken, "Salesforce")
	if err != nil {
		return nil, err
	}
	rows, err := decodeRecords(body)
	if err != nil {
		return nil, err
	}
	columns := inferColumns(rows)
	rawRows := make([]json.RawMessage, 0, len(rows))
	for _, row := range rows {
		buf, err := json.Marshal(row)
		if err != nil {
			return nil, fmt.Errorf("salesforce: marshal preview row: %w", err)
		}
		rawRows = append(rawRows, buf)
	}
	meta, err := json.Marshal(map[string]any{
		"query": soql,
		"url":   u.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("salesforce: marshal preview metadata: %w", err)
	}
	return &adapters.Result{
		Selector: q.Selector,
		Mode:     "zero_copy",
		Columns:  columns,
		RowCount: len(rawRows),
		Rows:     rawRows,
		Metadata: meta,
	}, nil
}

// StreamArrow runs a paginated SOQL fetch and materialises the entire result
// set as a single Arrow IPC frame. Mirrors Rust's `fetch_dataset` paginated
// loop (query / queryAll, `nextRecordsUrl` follow, `done` short-circuit,
// max-pages bound). All columns are encoded as nullable Utf8 to match the
// shared `materialize_arrow_stream` helper used by the Rust connectors.
func (a *Adapter) StreamArrow(ctx context.Context, c *models.Connection, q *adapters.Query, _ string) (adapters.ArrowStream, error) {
	if q == nil {
		return nil, errors.New("salesforce: query request is nil")
	}
	cfg, err := a.cfg(c)
	if err != nil {
		return nil, err
	}
	soql, err := soqlQuery(cfg, q.Selector)
	if err != nil {
		return nil, err
	}
	base, err := apiBaseURL(cfg)
	if err != nil {
		return nil, err
	}
	endpoint := "query"
	if cfg.IncludeDeleted {
		endpoint = "queryAll"
	}
	first, err := base.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("salesforce: build %s url: %w", endpoint, err)
	}
	qv := first.Query()
	qv.Set("q", soql)
	first.RawQuery = qv.Encode()

	rows, err := a.fetchAllRecords(ctx, cfg, first)
	if err != nil {
		return nil, err
	}
	columns := inferColumns(rows)
	frame, err := materializeArrowStream(columns, rows)
	if err != nil {
		return nil, err
	}
	return &singleFrameStream{frame: frame}, nil
}

// BuildIngestSpec emits a placeholder [adapters.IngestSpec] descriptor the
// bridge forwards to ingestion-replication-service. The Rust pipeline does
// not yet wire Salesforce into the typed ingestion spec, so we follow the
// CMA-0 / BigQuery convention: source discriminator "salesforce" + a JSON
// payload carrying instance, object, and an optional override SOQL query.
func (a *Adapter) BuildIngestSpec(_ context.Context, c *models.Connection, src *adapters.Source) (*adapters.IngestSpec, error) {
	if c == nil {
		return nil, errors.New("salesforce: connection is nil")
	}
	if src == nil {
		return nil, errors.New("salesforce: source is nil")
	}
	cfg, err := parseConfig(c.Config)
	if err != nil {
		return nil, err
	}
	instance := strings.TrimSpace(cfg.InstanceURL)
	if instance == "" {
		return nil, errors.New("salesforce: connection config missing 'instance_url'")
	}
	object := strings.TrimSpace(src.Selector)
	if object == "" {
		return nil, errors.New("salesforce: source selector is empty")
	}
	specCfg := map[string]any{
		"instance_url": instance,
		"object":       object,
		"api_version":  apiVersionOrDefault(cfg),
	}
	if q := strings.TrimSpace(cfg.Query); q != "" {
		specCfg["query"] = q
	}
	if cfg.IncludeDeleted {
		specCfg["include_deleted"] = true
	}
	raw, err := json.Marshal(specCfg)
	if err != nil {
		return nil, fmt.Errorf("salesforce: marshal ingest spec: %w", err)
	}
	return &adapters.IngestSpec{
		Name:      c.Name,
		Namespace: "default",
		Source:    ConnectorType,
		Config:    raw,
	}, nil
}

func (a *Adapter) cfg(c *models.Connection) (*sfConfig, error) {
	if c == nil {
		return nil, errors.New("salesforce: connection is nil")
	}
	cfg, err := parseConfig(c.Config)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.InstanceURL) == "" {
		return nil, errors.New("salesforce connector requires 'instance_url'")
	}
	if cfg.AccessToken == "" {
		return nil, errors.New("salesforce connector requires 'access_token'")
	}
	return cfg, nil
}

func (a *Adapter) fetchAllRecords(ctx context.Context, cfg *sfConfig, first *url.URL) ([]map[string]any, error) {
	limitPages := maxPagesOrDefault(cfg)
	all := make([]map[string]any, 0)
	pages := 0
	next := first
	for next != nil {
		if pages >= limitPages {
			break
		}
		body, err := a.doGet(ctx, next, cfg.AccessToken, "Salesforce")
		if err != nil {
			return nil, err
		}
		var page struct {
			Records        []map[string]any `json:"records"`
			Done           *bool            `json:"done"`
			NextRecordsURL *string          `json:"nextRecordsUrl"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("salesforce: decode query response: %w", err)
		}
		for _, rec := range page.Records {
			delete(rec, "attributes")
			all = append(all, rec)
		}
		pages++

		done := true
		if page.Done != nil {
			done = *page.Done
		}
		if done {
			break
		}
		if page.NextRecordsURL == nil || *page.NextRecordsURL == "" {
			break
		}
		base, err := url.Parse(cfg.InstanceURL)
		if err != nil {
			return nil, fmt.Errorf("salesforce: parse instance_url: %w", err)
		}
		rel, err := url.Parse(*page.NextRecordsURL)
		if err != nil {
			return nil, fmt.Errorf("salesforce: parse nextRecordsUrl: %w", err)
		}
		next = base.ResolveReference(rel)
	}
	return all, nil
}

func (a *Adapter) doGet(ctx context.Context, u *url.URL, token, op string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("salesforce: build %s request: %w", op, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("salesforce: %s transport error: %w", op, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned HTTP %d", op, resp.StatusCode)
	}
	return body, nil
}

func decodeRecords(body []byte) ([]map[string]any, error) {
	var payload struct {
		Records []map[string]any `json:"records"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("salesforce: decode records: %w", err)
	}
	out := make([]map[string]any, 0, len(payload.Records))
	for _, rec := range payload.Records {
		delete(rec, "attributes")
		out = append(out, rec)
	}
	return out, nil
}

func apiBaseURL(cfg *sfConfig) (*url.URL, error) {
	base, err := url.Parse(cfg.InstanceURL)
	if err != nil {
		return nil, fmt.Errorf("salesforce: parse instance_url: %w", err)
	}
	rel, err := url.Parse(fmt.Sprintf("/services/data/%s/", apiVersionOrDefault(cfg)))
	if err != nil {
		return nil, fmt.Errorf("salesforce: parse api path: %w", err)
	}
	return base.ResolveReference(rel), nil
}

func apiVersionOrDefault(cfg *sfConfig) string {
	v := strings.TrimSpace(cfg.APIVersion)
	if v == "" {
		return defaultAPIVersion
	}
	return v
}

func rowLimit(cfg *sfConfig) int64 {
	if cfg.RowLimit == nil {
		return defaultRowLimit
	}
	v := *cfg.RowLimit
	if v < minRowLimit {
		return minRowLimit
	}
	if v > maxRowLimit {
		return maxRowLimit
	}
	return v
}

func maxPagesOrDefault(cfg *sfConfig) int {
	if cfg.MaxPages == nil {
		return defaultMaxPages
	}
	v := *cfg.MaxPages
	if v < minMaxPages {
		return minMaxPages
	}
	if v > maxMaxPages {
		return maxMaxPages
	}
	return int(v)
}

func previewLimit(requested *int) int64 {
	if requested == nil {
		return defaultPreviewLimit
	}
	v := int64(*requested)
	if v < minPreviewLimit {
		return minPreviewLimit
	}
	if v > maxPreviewLimit {
		return maxPreviewLimit
	}
	return v
}

func soqlQuery(cfg *sfConfig, selector string) (string, error) {
	trimmed := strings.TrimSpace(selector)
	if strings.HasPrefix(strings.ToLower(trimmed), "select ") {
		return trimmed, nil
	}
	if trimmed != "" {
		return fmt.Sprintf("SELECT Id, Name FROM %s LIMIT %d", trimmed, rowLimit(cfg)), nil
	}
	if q := strings.TrimSpace(cfg.Query); q != "" {
		return q, nil
	}
	return "", errors.New("salesforce sync requires a SOQL query or object selector")
}

func boundedSOQL(selector string, limit int64) (string, error) {
	trimmed := strings.TrimSpace(selector)
	if strings.HasPrefix(strings.ToLower(trimmed), "select ") {
		return trimmed, nil
	}
	if trimmed != "" {
		return fmt.Sprintf("SELECT Id, Name FROM %s LIMIT %d", trimmed, limit), nil
	}
	return "", errors.New("salesforce virtual_table requires a selector")
}

func inferColumns(rows []map[string]any) []string {
	out := make([]string, 0)
	seen := make(map[string]struct{})
	for _, row := range rows {
		for k := range row {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	return out
}

func materializeArrowStream(columns []string, rows []map[string]any) ([]byte, error) {
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
		return nil, fmt.Errorf("salesforce: write arrow record: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("salesforce: close arrow stream: %w", err)
	}
	return buf.Bytes(), nil
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
