// Package bigquery is the Go port of the Rust BigQuery connector that lives
// in `services/connector-management-service/src/connectors/bigquery.rs`.
//
// Capabilities mirrored from the Rust module:
//
//   - DiscoverSources    — enumerate BigQuery datasets + tables for a project.
//   - QueryVirtualTable  — bounded SELECT … LIMIT preview returning JSON rows.
//   - StreamArrow        — full jobs.query + getQueryResults pagination,
//     materialised as a single Arrow IPC stream.
//   - BuildIngestSpec    — produce the per-source IngestSpec the bridge sends
//     to ingestion-replication-service for downstream sync orchestration.
//
// Auth flavours match Rust:
//
//   - service_account_json (string OR JSON object) — preferred. Self-signs
//     an RS256 JWT with `aud = oauth2.googleapis.com/token` and exchanges it
//     for a short-lived bearer token.
//   - access_token — short-lived bearer; sent as-is.
//
// The HTTP base URLs (`apiBase`, `tokenURL`) and the wallclock used to mint
// JWTs are exposed as fields so adapter_test.go can stand up an httptest
// server and a deterministic clock without monkey-patching globals.
package bigquery

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/golang-jwt/jwt/v5"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

const (
	// ConnectorType is the `connections.connector_type` value the registry
	// binds this adapter under. Mirrors the Rust `CONNECTOR_NAME` constant.
	ConnectorType = "bigquery"

	defaultSourceKind = "bigquery_table"
	defaultTokenURL   = "https://oauth2.googleapis.com/token"
	defaultAPIBase    = "https://bigquery.googleapis.com/bigquery/v2/"
	bigqueryScope     = "https://www.googleapis.com/auth/bigquery"
	defaultPageSize   = int64(1_000)
	maxPageSize       = int64(100_000)
	maxPages          = 100
	jwtTTLSeconds     = 3_600
)

// Adapter is the BigQuery [adapters.ConnectorAdapter] implementation. It is
// safe for concurrent use; the embedded HTTP client is reused across calls.
type Adapter struct {
	httpClient *http.Client
	apiBase    string
	tokenURL   string
	now        func() time.Time
}

// New returns a ready-to-use [Adapter] backed by [http.DefaultClient] and
// real Google REST endpoints.
func New() *Adapter {
	return &Adapter{
		httpClient: http.DefaultClient,
		apiBase:    defaultAPIBase,
		tokenURL:   defaultTokenURL,
		now:        time.Now,
	}
}

// Factory returns an [adapters.Factory] that constructs fresh BigQuery
// adapters. Per-connection state (HTTP pool, base-URL overrides used by
// tests) is deliberately scoped to the constructed value rather than to a
// package-level singleton.
func Factory() adapters.Factory {
	return adapters.FactoryFunc(func() adapters.ConnectorAdapter { return New() })
}

// SetHTTPClient overrides the embedded [http.Client]. Intended for tests
// that need to point the adapter at an httptest server with custom
// timeouts; the production path uses [http.DefaultClient].
func (a *Adapter) SetHTTPClient(client *http.Client) {
	if client != nil {
		a.httpClient = client
	}
}

// SetAPIBase overrides the BigQuery v2 REST base URL. Used by tests; the
// production path keeps the Google-hosted base.
func (a *Adapter) SetAPIBase(base string) {
	if base != "" {
		a.apiBase = base
	}
}

// SetTokenURL overrides the OAuth2 token-exchange endpoint. Used by tests.
func (a *Adapter) SetTokenURL(tokenURL string) {
	if tokenURL != "" {
		a.tokenURL = tokenURL
	}
}

// SetNowFunc overrides the wallclock used for JWT iat/exp claims. Tests
// supply a fixed clock so the assertion produced by RS256 signing is
// deterministic; the production path uses [time.Now].
func (a *Adapter) SetNowFunc(fn func() time.Time) {
	if fn != nil {
		a.now = fn
	}
}

type bqConfig struct {
	ProjectID          string          `json:"project_id"`
	AccessToken        string          `json:"access_token"`
	ServiceAccountJSON json.RawMessage `json:"service_account_json"`
	Location           string          `json:"location"`
	PageSize           *int64          `json:"page_size"`
	Query              string          `json:"query"`
}

func parseConfig(raw json.RawMessage) (*bqConfig, error) {
	cfg := &bqConfig{}
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("bigquery: invalid config: %w", err)
	}
	return cfg, nil
}

// ValidateConfig mirrors Rust's `validate_config`: the connection must
// carry a non-empty project_id and at least one credential variant.
func ValidateConfig(raw json.RawMessage) error {
	cfg, err := parseConfig(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.ProjectID) == "" {
		return errors.New("bigquery connector requires 'project_id'")
	}
	if cfg.AccessToken == "" && len(cfg.ServiceAccountJSON) == 0 {
		return errors.New("bigquery connector requires 'access_token' or 'service_account_json'")
	}
	return nil
}

// DiscoverSources lists every dataset + table reachable for the configured
// project. Mirrors Rust's `discover_sources`.
func (a *Adapter) DiscoverSources(ctx context.Context, c *models.Connection, _ string) ([]adapters.Source, error) {
	cfg, err := a.cfg(c)
	if err != nil {
		return nil, err
	}
	token, err := a.obtainAccessToken(ctx, cfg)
	if err != nil {
		return nil, err
	}
	datasets, err := a.listDatasets(ctx, cfg.ProjectID, token)
	if err != nil {
		return nil, err
	}
	out := make([]adapters.Source, 0, len(datasets))
	for _, ds := range datasets {
		tables, err := a.listTables(ctx, cfg.ProjectID, ds, token)
		if err != nil {
			// Rust logs and continues; we mirror that to keep partial
			// dataset listings useful even when one dataset is gated.
			continue
		}
		for _, t := range tables {
			tableID, _ := t["tableReference"].(map[string]any)["tableId"].(string)
			if tableID == "" {
				continue
			}
			selector := ds + "." + tableID
			meta, _ := json.Marshal(map[string]any{
				"project_id": cfg.ProjectID,
				"dataset_id": ds,
				"table_id":   tableID,
				"type":       t["type"],
			})
			out = append(out, adapters.Source{
				Selector:         selector,
				DisplayName:      selector,
				SourceKind:       defaultSourceKind,
				SupportsSync:     true,
				SupportsZeroCopy: true,
				Metadata:         meta,
			})
		}
	}
	return out, nil
}

// QueryVirtualTable runs a bounded preview of the selector. Mirrors Rust's
// `query_virtual_table` → `bounded_preview` shortcut: the full Arrow path
// is skipped in favour of a SELECT … LIMIT round-trip that returns JSON
// rows.
func (a *Adapter) QueryVirtualTable(ctx context.Context, c *models.Connection, q *adapters.Query, _ string) (*adapters.Result, error) {
	if q == nil {
		return nil, errors.New("bigquery: query request is nil")
	}
	cfg, err := a.cfg(c)
	if err != nil {
		return nil, err
	}
	limit := 50
	if q.Limit != nil {
		limit = *q.Limit
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	token, err := a.obtainAccessToken(ctx, cfg)
	if err != nil {
		return nil, err
	}
	rows, columns, err := a.runPreview(ctx, cfg, q.Selector, limit, token)
	if err != nil {
		return nil, err
	}
	meta, _ := json.Marshal(map[string]any{
		"project_id": cfg.ProjectID,
		"selector":   q.Selector,
		"limit":      limit,
	})
	rawRows := make([]json.RawMessage, 0, len(rows))
	for _, row := range rows {
		buf, err := json.Marshal(row)
		if err != nil {
			return nil, fmt.Errorf("bigquery: marshal preview row: %w", err)
		}
		rawRows = append(rawRows, buf)
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

// StreamArrow runs jobs.query → getQueryResults paginated up to [maxPages]
// and returns a single-frame [adapters.ArrowStream] with the materialised
// IPC bytes. The stream collapses the entire result set into one chunk so
// downstream consumers (dataset-versioning-service) can ingest it as a new
// dataset version exactly as the Rust connector does today.
func (a *Adapter) StreamArrow(ctx context.Context, c *models.Connection, q *adapters.Query, _ string) (adapters.ArrowStream, error) {
	if q == nil {
		return nil, errors.New("bigquery: query request is nil")
	}
	cfg, err := a.cfg(c)
	if err != nil {
		return nil, err
	}
	token, err := a.obtainAccessToken(ctx, cfg)
	if err != nil {
		return nil, err
	}
	query, err := buildQuery(cfg, q.Selector)
	if err != nil {
		return nil, err
	}
	rows, columns, err := a.runFullQuery(ctx, cfg, query, token)
	if err != nil {
		return nil, err
	}
	bytes, err := materializeArrowStream(columns, rows)
	if err != nil {
		return nil, err
	}
	return &singleFrameStream{frame: bytes}, nil
}

// BuildIngestSpec emits an [adapters.IngestSpec] descriptor that the
// ingestion bridge uses to schedule a downstream sync. The Rust pipeline
// does not yet wire BigQuery into ingestion-replication-service, so the
// shape we emit is the structural placeholder agreed in CMA-0: source
// discriminator "bigquery" + a JSON config carrying project/dataset/table
// (and optional location/query). Once the bridge gains a typed BigQuery
// variant, only the marshalling here needs to update.
func (a *Adapter) BuildIngestSpec(_ context.Context, c *models.Connection, src *adapters.Source) (*adapters.IngestSpec, error) {
	if c == nil {
		return nil, errors.New("bigquery: connection is nil")
	}
	if src == nil {
		return nil, errors.New("bigquery: source is nil")
	}
	cfg, err := parseConfig(c.Config)
	if err != nil {
		return nil, err
	}
	project := strings.TrimSpace(cfg.ProjectID)
	if project == "" {
		return nil, errors.New("bigquery: connection config missing 'project_id'")
	}
	dataset, table := splitSelector(src.Selector)
	if dataset == "" || table == "" {
		return nil, fmt.Errorf("bigquery: selector %q must be 'dataset.table'", src.Selector)
	}
	specCfg := map[string]any{
		"project_id": project,
		"dataset_id": dataset,
		"table_id":   table,
	}
	if cfg.Location != "" {
		specCfg["location"] = cfg.Location
	}
	if cfg.Query != "" {
		specCfg["query"] = cfg.Query
	}
	raw, err := json.Marshal(specCfg)
	if err != nil {
		return nil, fmt.Errorf("bigquery: marshal ingest spec: %w", err)
	}
	return &adapters.IngestSpec{
		Name:      c.Name,
		Namespace: "default",
		Source:    ConnectorType,
		Config:    raw,
	}, nil
}

func (a *Adapter) cfg(c *models.Connection) (*bqConfig, error) {
	if c == nil {
		return nil, errors.New("bigquery: connection is nil")
	}
	cfg, err := parseConfig(c.Config)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.ProjectID) == "" {
		return nil, errors.New("bigquery connector requires 'project_id'")
	}
	if cfg.AccessToken == "" && len(cfg.ServiceAccountJSON) == 0 {
		return nil, errors.New("bigquery connector requires 'access_token' or 'service_account_json'")
	}
	return cfg, nil
}

func (a *Adapter) obtainAccessToken(ctx context.Context, cfg *bqConfig) (string, error) {
	if cfg.AccessToken != "" {
		return cfg.AccessToken, nil
	}
	sa, err := parseServiceAccount(cfg.ServiceAccountJSON)
	if err != nil {
		return "", err
	}
	tokenURI := sa.TokenURI
	if tokenURI == "" {
		tokenURI = a.tokenURL
	}
	jwtStr, err := a.buildServiceAccountJWT(sa, tokenURI)
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", jwtStr)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("bigquery: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("bigquery: token exchange transport error: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("BigQuery token exchange returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("bigquery: token response decode: %w", err)
	}
	if payload.AccessToken == "" {
		return "", errors.New("BigQuery token exchange response missing 'access_token'")
	}
	return payload.AccessToken, nil
}

type serviceAccount struct {
	ClientEmail  string `json:"client_email"`
	PrivateKey   string `json:"private_key"`
	PrivateKeyID string `json:"private_key_id"`
	TokenURI     string `json:"token_uri"`
}

func parseServiceAccount(raw json.RawMessage) (*serviceAccount, error) {
	if len(raw) == 0 {
		return nil, errors.New("missing 'service_account_json'")
	}
	// Accept either a JSON object or a JSON-encoded string holding the
	// object — Rust's `Value::String` path.
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var inner string
		if err := json.Unmarshal(trimmed, &inner); err != nil {
			return nil, fmt.Errorf("service_account_json is not valid JSON: %w", err)
		}
		raw = json.RawMessage(inner)
	}
	sa := &serviceAccount{}
	if err := json.Unmarshal(raw, sa); err != nil {
		return nil, fmt.Errorf("service_account_json is not valid JSON: %w", err)
	}
	if sa.ClientEmail == "" {
		return nil, errors.New("service_account_json missing 'client_email'")
	}
	if sa.PrivateKey == "" {
		return nil, errors.New("service_account_json missing 'private_key'")
	}
	return sa, nil
}

func (a *Adapter) buildServiceAccountJWT(sa *serviceAccount, audience string) (string, error) {
	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return "", errors.New("invalid service-account private key: not PEM-encoded")
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("invalid service-account private key: %w", err)
		}
		key = k
	case "PRIVATE KEY":
		anyKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("invalid service-account private key: %w", err)
		}
		rsaKey, ok := anyKey.(*rsa.PrivateKey)
		if !ok {
			return "", errors.New("invalid service-account private key: not an RSA key")
		}
		key = rsaKey
	default:
		return "", fmt.Errorf("invalid service-account private key: unsupported PEM type %q", block.Type)
	}
	now := a.now().UTC().Unix()
	claims := jwt.MapClaims{
		"iss":   sa.ClientEmail,
		"scope": bigqueryScope,
		"aud":   audience,
		"iat":   now,
		"exp":   now + jwtTTLSeconds,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	if sa.PrivateKeyID != "" {
		token.Header["kid"] = sa.PrivateKeyID
	}
	signed, err := token.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("bigquery: sign jwt: %w", err)
	}
	return signed, nil
}

func (a *Adapter) listDatasets(ctx context.Context, projectID, token string) ([]string, error) {
	u, err := a.bqURL(fmt.Sprintf("projects/%s/datasets", url.PathEscape(projectID)))
	if err != nil {
		return nil, err
	}
	body, err := a.doJSONGet(ctx, u, token, "listDatasets")
	if err != nil {
		return nil, err
	}
	var payload struct {
		Datasets []struct {
			DatasetReference struct {
				DatasetID string `json:"datasetId"`
			} `json:"datasetReference"`
		} `json:"datasets"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("bigquery: decode listDatasets: %w", err)
	}
	out := make([]string, 0, len(payload.Datasets))
	for _, ds := range payload.Datasets {
		if ds.DatasetReference.DatasetID != "" {
			out = append(out, ds.DatasetReference.DatasetID)
		}
	}
	return out, nil
}

func (a *Adapter) listTables(ctx context.Context, projectID, datasetID, token string) ([]map[string]any, error) {
	u, err := a.bqURL(fmt.Sprintf("projects/%s/datasets/%s/tables",
		url.PathEscape(projectID), url.PathEscape(datasetID)))
	if err != nil {
		return nil, err
	}
	body, err := a.doJSONGet(ctx, u, token, "listTables")
	if err != nil {
		return nil, err
	}
	var payload struct {
		Tables []map[string]any `json:"tables"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("bigquery: decode listTables: %w", err)
	}
	return payload.Tables, nil
}

func (a *Adapter) runPreview(ctx context.Context, cfg *bqConfig, selector string, limit int, token string) ([]map[string]any, []string, error) {
	preview := buildPreviewQuery(cfg.ProjectID, selector, limit)
	body := map[string]any{
		"query":         preview,
		"useLegacySql":  false,
		"maxResults":    limit,
		"useQueryCache": true,
	}
	payload, err := a.postQuery(ctx, cfg.ProjectID, body, token)
	if err != nil {
		return nil, nil, err
	}
	cols := extractColumns(payload["schema"])
	rows := extractRows(payload["rows"], cols)
	return rows, cols, nil
}

func (a *Adapter) runFullQuery(ctx context.Context, cfg *bqConfig, query, token string) ([]map[string]any, []string, error) {
	pageSz := pageSize(cfg)
	body := map[string]any{
		"query":         query,
		"useLegacySql":  false,
		"maxResults":    pageSz,
		"useQueryCache": true,
	}
	if cfg.Location != "" {
		body["location"] = cfg.Location
	}
	payload, err := a.postQuery(ctx, cfg.ProjectID, body, token)
	if err != nil {
		return nil, nil, err
	}
	cols := extractColumns(payload["schema"])
	rows := extractRows(payload["rows"], cols)

	jobID := ""
	if ref, ok := payload["jobReference"].(map[string]any); ok {
		if id, ok := ref["jobId"].(string); ok {
			jobID = id
		}
	}

	pages := 1
	current := payload
	for pages < maxPages {
		token2, _ := current["pageToken"].(string)
		if token2 == "" || jobID == "" {
			break
		}
		next, err := a.fetchNextPage(ctx, cfg.ProjectID, jobID, token2, pageSz, token)
		if err != nil {
			return nil, nil, err
		}
		nextRows := extractRows(next["rows"], cols)
		if len(nextRows) == 0 {
			break
		}
		rows = append(rows, nextRows...)
		current = next
		pages++
	}
	return rows, cols, nil
}

func (a *Adapter) postQuery(ctx context.Context, projectID string, body map[string]any, token string) (map[string]any, error) {
	u, err := a.bqURL(fmt.Sprintf("projects/%s/queries", url.PathEscape(projectID)))
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("bigquery: marshal jobs.query: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("bigquery: build jobs.query request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bigquery: jobs.query transport error: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("BigQuery jobs.query returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	var out map[string]any
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("bigquery: decode jobs.query: %w", err)
	}
	return out, nil
}

func (a *Adapter) fetchNextPage(ctx context.Context, projectID, jobID, pageToken string, pageSize int64, bearer string) (map[string]any, error) {
	u, err := a.bqURL(fmt.Sprintf("projects/%s/queries/%s",
		url.PathEscape(projectID), url.PathEscape(jobID)))
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("pageToken", pageToken)
	q.Set("maxResults", strconv.FormatInt(pageSize, 10))
	u.RawQuery = q.Encode()
	body, err := a.doJSONGet(ctx, u, bearer, "getQueryResults")
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("bigquery: decode getQueryResults: %w", err)
	}
	return out, nil
}

func (a *Adapter) doJSONGet(ctx context.Context, u *url.URL, token, op string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("bigquery: build %s request: %w", op, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bigquery: %s transport error: %w", op, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("BigQuery %s HTTP %d", op, resp.StatusCode)
	}
	return body, nil
}

func (a *Adapter) bqURL(path string) (*url.URL, error) {
	base, err := url.Parse(a.apiBase)
	if err != nil {
		return nil, fmt.Errorf("bigquery: parse api base %q: %w", a.apiBase, err)
	}
	rel, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("bigquery: parse path %q: %w", path, err)
	}
	return base.ResolveReference(rel), nil
}

func extractColumns(schema any) []string {
	obj, ok := schema.(map[string]any)
	if !ok {
		return nil
	}
	fields, ok := obj["fields"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		field, ok := f.(map[string]any)
		if !ok {
			continue
		}
		name, _ := field["name"].(string)
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func extractRows(rowsRaw any, columns []string) []map[string]any {
	rowList, ok := rowsRaw.([]any)
	if !ok {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(rowList))
	for _, item := range rowList {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		cells, _ := row["f"].([]any)
		obj := make(map[string]any, len(columns))
		for idx, col := range columns {
			if idx < len(cells) {
				cellMap, _ := cells[idx].(map[string]any)
				obj[col] = cellMap["v"]
			} else {
				obj[col] = nil
			}
		}
		out = append(out, obj)
	}
	return out
}

func buildQuery(cfg *bqConfig, selector string) (string, error) {
	trimmed := strings.TrimSpace(selector)
	if strings.HasPrefix(strings.ToLower(trimmed), "select ") {
		return trimmed, nil
	}
	if trimmed != "" {
		return fmt.Sprintf("SELECT * FROM `%s.%s`", cfg.ProjectID, trimmed), nil
	}
	if q := strings.TrimSpace(cfg.Query); q != "" {
		return q, nil
	}
	return "", errors.New("bigquery sync requires a SQL query or table selector")
}

func buildPreviewQuery(projectID, selector string, limit int) string {
	trimmed := strings.TrimSpace(selector)
	if strings.HasPrefix(strings.ToLower(trimmed), "select ") {
		return fmt.Sprintf("SELECT * FROM (%s) LIMIT %d", trimmed, limit)
	}
	return fmt.Sprintf("SELECT * FROM `%s.%s` LIMIT %d", projectID, trimmed, limit)
}

func pageSize(cfg *bqConfig) int64 {
	if cfg.PageSize == nil {
		return defaultPageSize
	}
	v := *cfg.PageSize
	if v < 1 {
		return 1
	}
	if v > maxPageSize {
		return maxPageSize
	}
	return v
}

func splitSelector(selector string) (string, string) {
	trimmed := strings.TrimSpace(selector)
	idx := strings.Index(trimmed, ".")
	if idx <= 0 || idx == len(trimmed)-1 {
		return "", ""
	}
	return trimmed[:idx], trimmed[idx+1:]
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
		return nil, fmt.Errorf("bigquery: write arrow record: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("bigquery: close arrow stream: %w", err)
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
