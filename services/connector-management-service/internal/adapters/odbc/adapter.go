// Package odbc is the Go port of the Rust ODBC connector that lives in
// `services/connector-management-service/src/connectors/odbc.rs`. Like the
// Rust module, the adapter is a thin wrapper over the shared tabular
// catalog bridge: every capability that exists on the Rust side
// (`validate_config`, `discover_sources`, `query_virtual_table`)
// delegates straight to [catalogbridge.Bridge] with the connector_name
// "odbc", default source kind "odbc_table", and `dsn` as the identity
// field for resource-template configs.
//
// # Driver gap
//
// The Rust ODBC connector does not actually open an ODBC handle either —
// it routes every read through the same catalog-bridge HTTP path used by
// Tableau / Power BI / JDBC, on the assumption that the connector-agent
// (or an inline `tables` catalog) speaks ODBC on the user's behalf. The
// Go port preserves that behaviour, so no native ODBC driver is wired in
// here.
//
// If a future task requires native ODBC handles in-process (rather than
// agent-mediated), the choice in the Go ecosystem narrows to
// [github.com/alexbrainman/odbc], which is unmaintained-but-functional
// and only tested on Windows / macOS unixODBC. That swap would replace
// the bridge behind this adapter with a typed driver path; it is
// intentionally not done here because the Rust side does not do it
// either, and CMA-11c is a wrapper-parity task.
//
// Capabilities not exposed by the Rust connector — Arrow IPC streaming
// and IngestSpec construction — return [adapters.ErrNotImplemented].
package odbc

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
	ConnectorType = "odbc"

	defaultSourceKind = "odbc_table"
)

// identityFields are the config keys the bridge requires when the user
// configures ODBC via a `resource_path_template` instead of an inline
// `tables` catalog. Matches Rust's
// `validate_tabular_connector_config(config, "odbc", &["dsn"])`.
var identityFields = []string{"dsn"}

// Adapter is the [adapters.ConnectorAdapter] implementation for ODBC.
type Adapter struct {
	bridge *catalogbridge.Bridge
}

// New returns an ODBC [Adapter] backed by [http.DefaultClient].
func New() *Adapter {
	return &Adapter{bridge: catalogbridge.New(ConnectorType, defaultSourceKind, identityFields)}
}

// Factory returns an [adapters.Factory] that constructs fresh ODBC
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
// `validate_tabular_connector_config(config, "odbc", &["dsn"])`.
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

// StreamArrow returns [adapters.ErrNotImplemented]: the Rust ODBC
// connector does not expose `stream_arrow_ipc`; sync rows go through
// `fetch_dataset` (a JSON-over-HTTP path).
func (a *Adapter) StreamArrow(_ context.Context, _ *models.Connection, _ *adapters.Query, _ string) (adapters.ArrowStream, error) {
	return nil, adapters.ErrNotImplemented
}

// BuildIngestSpec returns [adapters.ErrNotImplemented]: the Rust ODBC
// connector is not wired into `ingestion_bridge::build_spec`.
func (a *Adapter) BuildIngestSpec(_ context.Context, _ *models.Connection, _ *adapters.Source) (*adapters.IngestSpec, error) {
	return nil, adapters.ErrNotImplemented
}
