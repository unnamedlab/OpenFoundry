// Package databricks is the Go port of the Rust Databricks (Unity Catalog
// / Delta) connector that lives in
// `services/connector-management-service/src/connectors/databricks.rs`.
// Like the Rust module, the adapter is a thin wrapper over the shared
// tabular catalog bridge: every capability that exists on the Rust side
// (`validate_config`, `discover_sources`, `query_virtual_table`) delegates
// straight to [catalogbridge.Bridge] with the connector_name "databricks",
// default source kind "databricks_table", and `workspace_url`/`http_path`
// as identity fields for resource-template configs.
//
// Capabilities not exposed by the Rust connector — Arrow IPC streaming
// and IngestSpec construction — return [adapters.ErrNotImplemented]; the
// dispatcher in `internal/domain/discovery` translates that into the
// existing "stream-arrow is not supported for connector type: databricks"
// envelope.
package databricks

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
	ConnectorType = "databricks"

	defaultSourceKind = "databricks_table"
)

// identityFields are the config keys the bridge requires when the user
// configures Databricks via a resource template instead of an inline
// `tables` catalog. Matches Rust's
// `validate_tabular_connector_config(config, "databricks", &["workspace_url", "http_path"])`.
var identityFields = []string{"workspace_url", "http_path"}

// Adapter is the [adapters.ConnectorAdapter] implementation for Databricks.
// It is safe for concurrent use; the embedded bridge holds an
// [http.Client] reused across calls.
type Adapter struct {
	bridge *catalogbridge.Bridge
}

// New returns a Databricks [Adapter] backed by [http.DefaultClient].
func New() *Adapter {
	return &Adapter{bridge: catalogbridge.New(ConnectorType, defaultSourceKind, identityFields)}
}

// Factory returns an [adapters.Factory] that constructs fresh Databricks
// adapters; the registry stores the factory and asks for an instance per
// request so per-connection state (HTTP client overrides used by tests)
// stays scoped to the constructed value.
func Factory() adapters.Factory {
	return adapters.FactoryFunc(func() adapters.ConnectorAdapter { return New() })
}

// SetHTTPClient overrides the bridge's [http.Client]. Used by tests that
// stand up an [httptest.Server]; the production path keeps
// [http.DefaultClient].
func (a *Adapter) SetHTTPClient(client *http.Client) {
	if client != nil {
		a.bridge.HTTPClient = client
	}
}

// ValidateConfig mirrors Rust's `validate_config` —
// `validate_tabular_connector_config(config, "databricks", &["workspace_url", "http_path"])`.
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

// StreamArrow returns [adapters.ErrNotImplemented]: the Rust Databricks
// connector does not expose `stream_arrow_ipc`; sync rows are produced
// via `fetch_dataset` (a JSON-over-HTTP path through the catalog bridge).
// Add real Arrow streaming once the Rust side does.
func (a *Adapter) StreamArrow(_ context.Context, _ *models.Connection, _ *adapters.Query, _ string) (adapters.ArrowStream, error) {
	return nil, adapters.ErrNotImplemented
}

// BuildIngestSpec returns [adapters.ErrNotImplemented]: the Rust Databricks
// connector is not wired into `ingestion_bridge::build_spec`. Sync flows
// continue to go through `fetch_dataset` until a typed `databricks` variant
// of `IngestJobSpec` lands.
func (a *Adapter) BuildIngestSpec(_ context.Context, _ *models.Connection, _ *adapters.Source) (*adapters.IngestSpec, error) {
	return nil, adapters.ErrNotImplemented
}
