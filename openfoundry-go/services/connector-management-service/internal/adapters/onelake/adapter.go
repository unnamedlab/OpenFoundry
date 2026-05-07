// Package onelake is the Go port of
// `services/connector-management-service/src/connectors/onelake.rs` —
// the Microsoft OneLake (Fabric Lakehouse) ABFS-compatible source.
//
// OneLake exposes Fabric Lakehouses at
// `https://onelake.dfs.fabric.microsoft.com/<workspace>/<lakehouse>.Lakehouse/...`,
// which Foundry models as a thin variant of the ABFS connector. This
// adapter mirrors that variant while keeping the CMA-8 contract (passthrough
// Discover + Query delegate):
//
//   - DiscoverSources: passthrough via [opentable.Discover] (returns the
//     inline `iceberg_tables[]` / `delta_tables[]` declared in
//     `connection.config`). Live ABFS listing (the `object_store::azure`
//     path in Rust) is intentionally out of scope for the Go port —
//     the connector-agent and the Iceberg REST catalog (see
//     internal/handlers/iceberg_catalog.go) surface live data instead,
//     mirroring the S3 / Azure Blob ports.
//   - QueryVirtualTable: delegates to
//     [catalogbridge.Bridge.QueryVirtualTable] (CMA-13) so configurations
//     carrying inline `tables[]` / `sample_rows` or
//     `base_url` + resource templates produce previews through the same
//     code path as the tabular bridge connectors.
//   - StreamArrow: returns [adapters.ErrNotImplemented] — `onelake.rs`
//     does not expose `stream_arrow_ipc`.
//   - BuildIngestSpec: deferred. Returns [adapters.ErrNotImplemented]
//     until the `onelake` variant of `IngestJobSpec` lands.
//
// Required config keys (mirrors Rust `validate_config`):
//
//   - `workspace` — Fabric workspace GUID or display name.
//   - `lakehouse` — lakehouse name (without the `.Lakehouse` suffix).
//   - One credential variant:
//   - `oauth_token`                                       — Entra ID bearer.
//   - `tenant_id` + `client_id` + `client_secret`         — service principal.
//
// Optional:
//   - `namespace`                       — `Files` (default) or `Tables`.
//   - `prefix`                          — additional path narrowing.
//   - `iceberg_tables[]`, `delta_tables[]` — see internal/adapters/opentable.
//   - `tables[]` / `views[]` / `base_url` + `catalog_path` — see
//     internal/adapters/catalogbridge.
package onelake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters/catalogbridge"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters/opentable"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

// ConnectorType is the `connections.connector_type` value the registry
// binds this adapter under. Mirrors the Rust module's implicit name.
const ConnectorType = "onelake"

const (
	storePrefix       = "onelake"
	defaultSourceKind = "onelake_object"
)

// Adapter is the onelake [adapters.ConnectorAdapter] implementation. It
// is safe for concurrent use; the embedded bridge holds an [http.Client]
// reused across calls.
type Adapter struct {
	bridge *catalogbridge.Bridge
}

// New returns a onelake [Adapter] backed by [http.DefaultClient].
func New() *Adapter {
	return &Adapter{bridge: catalogbridge.New(ConnectorType, defaultSourceKind, nil)}
}

// Factory returns an [adapters.Factory] that yields fresh Adapters; the
// registry asks for an instance per request so per-connection state
// (HTTP client overrides used by tests) stays scoped to the constructed
// value.
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

type onelakeConfig struct {
	Workspace    string          `json:"workspace"`
	Lakehouse    string          `json:"lakehouse"`
	OAuthToken   json.RawMessage `json:"oauth_token"`
	TenantID     string          `json:"tenant_id"`
	ClientID     string          `json:"client_id"`
	ClientSecret string          `json:"client_secret"`
}

// ValidateConfig mirrors Rust's `validate_config`: non-empty `workspace`
// and `lakehouse` plus one of `oauth_token` or the
// `tenant_id`+`client_id`+`client_secret` triplet is required.
func ValidateConfig(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("onelake source requires 'workspace'")
	}
	var cfg onelakeConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("onelake: invalid config: %w", err)
	}
	if strings.TrimSpace(cfg.Workspace) == "" {
		return errors.New("onelake source requires 'workspace'")
	}
	if strings.TrimSpace(cfg.Lakehouse) == "" {
		return errors.New("onelake source requires 'lakehouse'")
	}
	hasToken := len(cfg.OAuthToken) > 0
	hasSP := strings.TrimSpace(cfg.TenantID) != "" &&
		strings.TrimSpace(cfg.ClientID) != "" &&
		strings.TrimSpace(cfg.ClientSecret) != ""
	if !hasToken && !hasSP {
		return errors.New("onelake source requires 'oauth_token' or ('tenant_id'+'client_id'+'client_secret')")
	}
	return nil
}

// DiscoverSources turns the inline iceberg_tables[] / delta_tables[]
// entries into [adapters.Source] descriptors. Mirrors the open-table
// branch of Rust's `discover_sources`, including the "must declare at
// least one table" failure mode (the live ABFS-listing branch is out
// of scope for the Go port — see package doc).
func (a *Adapter) DiscoverSources(_ context.Context, c *models.Connection, _ string) ([]adapters.Source, error) {
	if c == nil {
		return nil, errors.New("onelake: connection is nil")
	}
	if err := ValidateConfig(c.Config); err != nil {
		return nil, err
	}
	sources, err := opentable.Discover(c.Config, storePrefix)
	if err != nil {
		return nil, fmt.Errorf("onelake: %w", err)
	}
	if len(sources) == 0 {
		return nil, errors.New("onelake source did not expose any virtual tables; declare 'iceberg_tables[]' or 'delta_tables[]'")
	}
	return sources, nil
}

// QueryVirtualTable delegates to [catalogbridge.Bridge.QueryVirtualTable].
// Configurations that only carry inline open-table pointers will surface
// the bridge's "requires 'base_url'" envelope — clients then fall back
// to the Iceberg REST `LoadTable` path. Mirrors the CMA-8 "Query:
// delegate to catalog_bridge" contract.
func (a *Adapter) QueryVirtualTable(ctx context.Context, c *models.Connection, q *adapters.Query, _ string) (*adapters.Result, error) {
	if c == nil {
		return nil, errors.New("onelake: connection is nil")
	}
	if err := ValidateConfig(c.Config); err != nil {
		return nil, err
	}
	return a.bridge.QueryVirtualTable(ctx, c, q)
}

// StreamArrow is unsupported — `onelake.rs` does not expose
// `stream_arrow_ipc`.
func (a *Adapter) StreamArrow(_ context.Context, _ *models.Connection, _ *adapters.Query, _ string) (adapters.ArrowStream, error) {
	return nil, fmt.Errorf("%w: onelake arrow streaming", adapters.ErrNotImplemented)
}

// BuildIngestSpec is deferred (CMA-8 explicitly defers the onelake
// ingest variant). The dispatcher translates this into the existing
// "ingest is not supported for connector type: onelake" envelope.
func (a *Adapter) BuildIngestSpec(_ context.Context, _ *models.Connection, _ *adapters.Source) (*adapters.IngestSpec, error) {
	return nil, fmt.Errorf("%w: onelake ingest spec", adapters.ErrNotImplemented)
}
