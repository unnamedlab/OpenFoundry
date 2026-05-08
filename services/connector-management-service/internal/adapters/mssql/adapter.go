// Package mssql provides the Go adapter for Microsoft SQL Server catalogs.
//
// Rust's `services/connector-management-service/src/connectors/mssql.rs` is
// still an empty placeholder, so there is no upstream native SQL Server driver
// implementation to port. To avoid leaving discovery and preview unusable for
// SQL Server connections, this adapter follows the same tabular catalog bridge
// shape used by the Rust JDBC/MySQL/ODBC connectors: inline `tables` catalogs
// work locally and remote catalogs/resources are fetched from the configured
// bridge endpoint.
//
// Arrow IPC streaming and IngestSpec construction remain unsupported until a
// Rust-equivalent streaming/ingestion contract exists for SQL Server; those
// capability methods return the typed [adapters.ErrNotImplemented] sentinel.
package mssql

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
	// binds this adapter under, matching Rust's `connectors::mssql` module name.
	ConnectorType = "mssql"

	defaultSourceKind = "mssql_table"
)

// identityFields are the config keys required when SQL Server is configured
// via a `resource_path_template` instead of an inline `tables` catalog.
var identityFields = []string{"host"}

// Adapter is the [adapters.ConnectorAdapter] implementation for MSSQL.
type Adapter struct {
	bridge *catalogbridge.Bridge
}

// New returns an MSSQL [Adapter] backed by [http.DefaultClient].
func New() *Adapter {
	return &Adapter{bridge: catalogbridge.New(ConnectorType, defaultSourceKind, identityFields)}
}

// Factory returns an [adapters.Factory] that constructs fresh MSSQL adapters.
func Factory() adapters.Factory {
	return adapters.FactoryFunc(func() adapters.ConnectorAdapter { return New() })
}

// SetHTTPClient overrides the bridge's [http.Client]. Used by tests.
func (a *Adapter) SetHTTPClient(client *http.Client) {
	if client != nil {
		a.bridge.HTTPClient = client
	}
}

// ValidateConfig validates the tabular catalog/bridge configuration accepted by
// the MSSQL adapter.
func ValidateConfig(raw json.RawMessage) error {
	return catalogbridge.New(ConnectorType, defaultSourceKind, identityFields).ValidateConfig(raw)
}

// DiscoverSources returns inline SQL Server tables or remote bridge catalog
// entries using the shared tabular bridge.
func (a *Adapter) DiscoverSources(ctx context.Context, c *models.Connection, _ string) ([]adapters.Source, error) {
	if err := a.bridge.ValidateConfig(c.Config); err != nil {
		return nil, err
	}
	return a.bridge.DiscoverSources(ctx, c)
}

// QueryVirtualTable returns inline sample rows or remote bridge rows for a SQL
// Server selector using the shared tabular bridge.
func (a *Adapter) QueryVirtualTable(ctx context.Context, c *models.Connection, q *adapters.Query, _ string) (*adapters.Result, error) {
	if err := a.bridge.ValidateConfig(c.Config); err != nil {
		return nil, err
	}
	return a.bridge.QueryVirtualTable(ctx, c, q)
}

// StreamArrow returns [adapters.ErrNotImplemented]: neither the Rust placeholder
// nor the shared tabular bridge exposes SQL Server Arrow IPC streaming yet.
func (*Adapter) StreamArrow(context.Context, *models.Connection, *adapters.Query, string) (adapters.ArrowStream, error) {
	return nil, adapters.ErrNotImplemented
}

// BuildIngestSpec returns [adapters.ErrNotImplemented]: SQL Server is not wired
// into the Rust ingestion bridge yet.
func (*Adapter) BuildIngestSpec(context.Context, *models.Connection, *adapters.Source) (*adapters.IngestSpec, error) {
	return nil, adapters.ErrNotImplemented
}
