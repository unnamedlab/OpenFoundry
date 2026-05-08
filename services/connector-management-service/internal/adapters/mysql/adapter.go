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
// Capabilities not exposed by the Rust connector — Arrow IPC streaming
// and IngestSpec construction — return [adapters.ErrNotImplemented].
package mysql

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
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

// StreamArrow returns [adapters.ErrNotImplemented]: the Rust MySQL
// connector does not expose `stream_arrow_ipc`; sync rows go through
// `fetch_dataset` (a JSON-over-HTTP path).
func (a *Adapter) StreamArrow(_ context.Context, _ *models.Connection, _ *adapters.Query, _ string) (adapters.ArrowStream, error) {
	return nil, adapters.ErrNotImplemented
}

// BuildIngestSpec returns [adapters.ErrNotImplemented]: the Rust MySQL
// connector is not wired into `ingestion_bridge::build_spec`.
func (a *Adapter) BuildIngestSpec(_ context.Context, _ *models.Connection, _ *adapters.Source) (*adapters.IngestSpec, error) {
	return nil, adapters.ErrNotImplemented
}
