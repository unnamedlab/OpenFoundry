// Package jdbc is the Go port of the Rust JDBC connector that lives in
// `services/connector-management-service/src/connectors/jdbc.rs`. Like the
// Rust module, the adapter is a thin wrapper over the shared tabular
// catalog bridge: every capability that exists on the Rust side
// (`validate_config`, `discover_sources`, `query_virtual_table`)
// delegates straight to [catalogbridge.Bridge] with the connector_name
// "jdbc", default source kind "jdbc_table", and `jdbc_url` as the
// identity field for resource-template configs.
//
// # JVM gap
//
// JDBC is a Java-only contract: there is no in-process JDBC driver in
// Go's stdlib or its supported third-party drivers. The Rust side does
// not run JDBC drivers in-process either — every read flows through the
// catalog-bridge HTTP path on the assumption that the connector-agent
// runs the actual JDBC connection on the user's JVM, or that the inline
// `tables` catalog already carries the data.
//
// This Go port preserves that "JVM-out-of-process" model:
//
//   - inline catalogs (`tables` with `sample_rows`) work standalone — no
//     JVM required;
//   - resource-template configs (`base_url` + `resource_path_template`)
//     fan out through the bridge's HTTP client, which talks to a JVM
//     sidecar exactly the way the Rust connector does today via
//     `http_runtime`.
//
// If a future task needs in-process JDBC, the two paths in the Go
// ecosystem are:
//
//   - spawn a JVM sidecar (the same shape as the existing python-sidecar
//     pattern) and stream rows over a JSON/Arrow protocol — the chosen
//     approach for the broader migration plan;
//   - translate the `jdbc:` URL into the equivalent native Go driver
//     (`pgx` for PostgreSQL JDBC URLs, `go-sql-driver/mysql` for MySQL,
//     `gosnowflake` for Snowflake, etc.) on a per-prefix basis. This
//     loses driver-specific JDBC parameters, so it is not done by
//     default.
//
// CMA-11d is a wrapper-parity task; the in-process driver decision is
// out of scope here.
//
// Capabilities not exposed by the Rust connector — Arrow IPC streaming
// and IngestSpec construction — return [adapters.ErrNotImplemented].
package jdbc

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
	ConnectorType = "jdbc"

	defaultSourceKind = "jdbc_table"
)

// identityFields are the config keys the bridge requires when the user
// configures JDBC via a `resource_path_template` instead of an inline
// `tables` catalog. Matches Rust's
// `validate_tabular_connector_config(config, "jdbc", &["jdbc_url"])`.
var identityFields = []string{"jdbc_url"}

// Adapter is the [adapters.ConnectorAdapter] implementation for JDBC.
type Adapter struct {
	bridge *catalogbridge.Bridge
}

// New returns a JDBC [Adapter] backed by [http.DefaultClient].
func New() *Adapter {
	return &Adapter{bridge: catalogbridge.New(ConnectorType, defaultSourceKind, identityFields)}
}

// Factory returns an [adapters.Factory] that constructs fresh JDBC
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
// `validate_tabular_connector_config(config, "jdbc", &["jdbc_url"])`.
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

// StreamArrow returns [adapters.ErrNotImplemented]: the Rust JDBC
// connector does not expose `stream_arrow_ipc`; sync rows go through
// `fetch_dataset` (a JSON-over-HTTP path).
func (a *Adapter) StreamArrow(_ context.Context, _ *models.Connection, _ *adapters.Query, _ string) (adapters.ArrowStream, error) {
	return nil, adapters.ErrNotImplemented
}

// BuildIngestSpec returns [adapters.ErrNotImplemented]: the Rust JDBC
// connector is not wired into `ingestion_bridge::build_spec`.
func (a *Adapter) BuildIngestSpec(_ context.Context, _ *models.Connection, _ *adapters.Source) (*adapters.IngestSpec, error) {
	return nil, adapters.ErrNotImplemented
}
