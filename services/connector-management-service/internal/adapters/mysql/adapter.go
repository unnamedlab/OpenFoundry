// Package mysql is the Go port of the Rust MySQL connector that lives in
// `services/connector-management-service/src/connectors/mysql.rs`. Like the
// Rust module, the adapter is a Foundry-aligned thin wrapper over the
// shared tabular catalog bridge: every capability that exists on the Rust
// side (`validate_config`, `discover_sources`, `query_virtual_table`)
// delegates straight to [catalogbridge.Bridge] with the connector_name
// "mysql", default source kind "mysql_table", and `host` as the identity
// field for resource-template configs. `jdbc_url` is also accepted for
// parity with the JDBC connector — Foundry treats MySQL as one of the
// JDBC-routed sources, so a config that already carries a `jdbc_url`
// passes the bridge's identity check via the `tables` inline catalog or
// via the JDBC connector itself.
//
// # Driver gap
//
// Foundry's MySQL connector relies on JDBC/ODBC drivers running on a Data
// Connection Agent — there is no in-process MySQL driver in the Rust
// source either. Mirroring that, this Go port routes every read through
// the catalog-bridge HTTP path, on the assumption that the
// connector-agent (or an inline `tables` catalog) speaks MySQL on the
// user's behalf. No native MySQL driver is wired in here.
//
// If a future task requires a native MySQL handle in-process (rather than
// agent-mediated), the chosen driver in the Go ecosystem is
// [github.com/go-sql-driver/mysql] over `database/sql`. That swap would
// replace the bridge behind this adapter with a typed driver path; it is
// intentionally not done here because the Rust side does not do it
// either, and CMA-4 is a wrapper-parity task.
//
// MySQL additionally materialises bridge query results as real Apache Arrow
// IPC stream bytes and builds a sanitized ingestion spec for selected tables.
package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters/arrowipc"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters/catalogbridge"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

const (
	// ConnectorType is the `connections.connector_type` value the registry
	// binds this adapter under. Mirrors Rust's `CONNECTOR_NAME`.
	ConnectorType = "mysql"

	defaultSourceKind = "mysql_table"
)

// identityFields are the config keys the bridge requires when the user
// configures MySQL via a `resource_path_template` instead of an inline
// `tables` catalog. Matches Rust's
// `validate_tabular_connector_config(config, "mysql", &["host"])`.
var identityFields = []string{"host"}

// Adapter is the [adapters.ConnectorAdapter] implementation for MySQL.
type Adapter struct {
	bridge *catalogbridge.Bridge
}

// New returns a MySQL [Adapter] backed by [http.DefaultClient].
func New() *Adapter {
	return &Adapter{bridge: catalogbridge.New(ConnectorType, defaultSourceKind, identityFields)}
}

// Factory returns an [adapters.Factory] that constructs fresh MySQL
// adapters; the registry stores the factory and asks for an instance per
// request so per-connection state stays scoped to the constructed value.
func Factory() adapters.Factory {
	return adapters.FactoryFunc(func() adapters.ConnectorAdapter { return New() })
}

// SetHTTPClient overrides the bridge's [http.Client]. Used by tests.
func (a *Adapter) SetHTTPClient(client *http.Client) {
	if client != nil {
		a.bridge.HTTPClient = client
	}
}

// ValidateConfig mirrors Rust's `validate_config` —
// `validate_tabular_connector_config(config, "mysql", &["host"])`.
func ValidateConfig(raw json.RawMessage) error {
	return catalogbridge.New(ConnectorType, defaultSourceKind, identityFields).ValidateConfig(raw)
}

// DiscoverSources mirrors Rust's `discover_sources`.
func (a *Adapter) DiscoverSources(ctx context.Context, c *models.Connection, _ string) ([]adapters.Source, error) {
	if err := a.bridge.ValidateConfig(c.Config); err != nil {
		return nil, err
	}
	return a.bridge.DiscoverSources(ctx, c)
}

// QueryVirtualTable mirrors Rust's `query_virtual_table`.
func (a *Adapter) QueryVirtualTable(ctx context.Context, c *models.Connection, q *adapters.Query, _ string) (*adapters.Result, error) {
	if err := a.bridge.ValidateConfig(c.Config); err != nil {
		return nil, err
	}
	return a.bridge.QueryVirtualTable(ctx, c, q)
}

// StreamArrow returns a real Apache Arrow IPC stream backed by the same bridge
// query path as JSON preview. Rows and columns come directly from the adapter
// result; no pseudo-Arrow JSON envelope is emitted.
func (a *Adapter) StreamArrow(ctx context.Context, c *models.Connection, q *adapters.Query, agentURL string) (adapters.ArrowStream, error) {
	res, err := a.QueryVirtualTable(ctx, c, q, agentURL)
	if err != nil {
		return nil, err
	}
	frame, err := arrowipc.MaterializeResult(res)
	if err != nil {
		return nil, fmt.Errorf("mysql: materialize arrow ipc: %w", err)
	}
	return &arrowipc.SingleFrameStream{Frame: frame}, nil
}

// BuildIngestSpec produces the source envelope consumed by the ingestion
// bridge after validating config and removing credential-like fields.
func (a *Adapter) BuildIngestSpec(_ context.Context, c *models.Connection, src *adapters.Source) (*adapters.IngestSpec, error) {
	if c == nil {
		return nil, errors.New("mysql: connection is nil")
	}
	if src == nil {
		return nil, errors.New("mysql: source is nil")
	}
	if err := a.bridge.ValidateConfig(c.Config); err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(c.Config, &cfg); err != nil {
		return nil, fmt.Errorf("mysql: invalid config: %w", err)
	}

	selector := strings.TrimSpace(src.Selector)
	if selector == "" {
		selector = trimmedString(cfg["table"])
	}
	if selector == "" {
		return nil, errors.New("mysql: source selector is empty")
	}

	payload := sanitizeConfig(cfg)
	payload["connector"] = ConnectorType
	payload["selector"] = selector
	payload["table"] = selector
	if query := trimmedString(cfg["query"]); query != "" {
		payload["query"] = query
	}
	payload["source_identity"] = map[string]any{
		"connection_id": c.ID.String(),
		"selector":      selector,
	}
	if cursor := firstNonEmptyString(cfg, "cursor", "incremental_cursor"); cursor != "" {
		payload["incremental"] = map[string]any{"cursor": cursor}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("mysql: marshal ingest spec: %w", err)
	}
	return &adapters.IngestSpec{Name: c.Name + "-mysql-ingest", Namespace: "default", Source: ConnectorType, Config: raw}, nil
}

var secretConfigKeys = map[string]struct{}{
	"password":    {},
	"secret":      {},
	"token":       {},
	"api_key":     {},
	"private_key": {},
	"credential":  {},
	"credentials": {},
}

func sanitizeConfig(cfg map[string]any) map[string]any {
	out := make(map[string]any, len(cfg))
	for key, value := range cfg {
		if isSecretKey(key) {
			continue
		}
		out[key] = sanitizeValue(value)
	}
	return out
}

func sanitizeValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return sanitizeConfig(v)
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, sanitizeValue(item))
		}
		return out
	default:
		return value
	}
}

func isSecretKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	if _, ok := secretConfigKeys[normalized]; ok {
		return true
	}
	return strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "api_key") ||
		strings.Contains(normalized, "private_key") ||
		strings.Contains(normalized, "credential")
}

func firstNonEmptyString(cfg map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := trimmedString(cfg[key]); value != "" {
			return value
		}
	}
	return ""
}

func trimmedString(value any) string {
	s, _ := value.(string)
	return strings.TrimSpace(s)
}
