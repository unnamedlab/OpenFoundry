// Package gcs is the Go port of
// `services/connector-management-service/src/connectors/gcs.rs` — the
// Google Cloud Storage object-store source.
//
// CMA-8 contract (passthrough Discover + Query delegate):
//
//   - DiscoverSources: passthrough via [opentable.Discover] (returns the
//     inline `iceberg_tables[]` / `delta_tables[]` declared in
//     `connection.config`). Live bucket listing (the `object_store::gcp`
//     path in Rust) is intentionally out of scope here — the Go port
//     relies on the connector-agent and the Iceberg REST catalog (see
//     internal/handlers/iceberg_catalog.go) to surface live data, mirroring
//     the S3 / Azure Blob ports.
//   - QueryVirtualTable: delegates to [catalogbridge.Bridge.QueryVirtualTable]
//     (CMA-13) so configurations carrying inline `tables[]` /
//     `sample_rows` or `base_url` + resource templates produce previews
//     through the same code path as the tabular bridge connectors.
//   - StreamArrow: returns [adapters.ErrNotImplemented] — `gcs.rs` does
//     not expose `stream_arrow_ipc`.
//   - BuildIngestSpec: deferred. Returns [adapters.ErrNotImplemented]
//     until the `gcs` variant of `IngestJobSpec` lands; the dispatcher
//     translates that into the existing
//     `"<capability> is not supported for connector type: gcs"` envelope.
//
// Required config keys (mirrors Rust `validate_config`):
//
//   - `bucket` — non-empty bucket name.
//   - One credential variant:
//   - `access_token`             — static OAuth2 bearer token.
//   - `service_account_json`     — inline service-account JSON (object or string).
//   - `application_default: true` — opt-in ADC / Workload Identity Federation.
//
// Optional:
//   - `iceberg_tables[]`, `delta_tables[]` — see internal/adapters/opentable.
//   - `tables[]` / `views[]` / `base_url` + `catalog_path` — see
//     internal/adapters/catalogbridge.
package gcs

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
const ConnectorType = "gcs"

const (
	storePrefix       = "gcs"
	defaultSourceKind = "gcs_object"
)

// Adapter is the gcs [adapters.ConnectorAdapter] implementation. It is
// safe for concurrent use; the embedded bridge holds an [http.Client]
// reused across calls.
type Adapter struct {
	bridge *catalogbridge.Bridge
}

// New returns a gcs [Adapter] backed by [http.DefaultClient].
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

type gcsConfig struct {
	Bucket             string          `json:"bucket"`
	AccessToken        json.RawMessage `json:"access_token"`
	ServiceAccountJSON json.RawMessage `json:"service_account_json"`
	ApplicationDefault *bool           `json:"application_default"`
}

// ValidateConfig mirrors Rust's `validate_config`: a non-empty `bucket`
// plus one of `access_token`, `service_account_json` or
// `application_default: true` is required. ADC is opt-in (matching the
// Rust security stance) so operators have to explicitly accept
// env-provided credentials.
func ValidateConfig(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("gcs source requires 'bucket'")
	}
	var cfg gcsConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("gcs: invalid config: %w", err)
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return errors.New("gcs source requires 'bucket'")
	}
	hasADC := cfg.ApplicationDefault != nil && *cfg.ApplicationDefault
	if len(cfg.AccessToken) == 0 && len(cfg.ServiceAccountJSON) == 0 && !hasADC {
		return errors.New("gcs source requires 'access_token', 'service_account_json' or 'application_default: true'")
	}
	return nil
}

// DiscoverSources turns the inline iceberg_tables[] / delta_tables[]
// entries into [adapters.Source] descriptors. Mirrors the open-table
// branch of Rust's `discover_sources`, including the "must declare at
// least one table" failure mode (the live bucket-listing branch is out
// of scope for the Go port — see package doc).
func (a *Adapter) DiscoverSources(_ context.Context, c *models.Connection, _ string) ([]adapters.Source, error) {
	if c == nil {
		return nil, errors.New("gcs: connection is nil")
	}
	if err := ValidateConfig(c.Config); err != nil {
		return nil, err
	}
	sources, err := opentable.Discover(c.Config, storePrefix)
	if err != nil {
		return nil, fmt.Errorf("gcs: %w", err)
	}
	if len(sources) == 0 {
		return nil, errors.New("gcs source did not expose any virtual tables; declare 'iceberg_tables[]' or 'delta_tables[]'")
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
		return nil, errors.New("gcs: connection is nil")
	}
	if err := ValidateConfig(c.Config); err != nil {
		return nil, err
	}
	return a.bridge.QueryVirtualTable(ctx, c, q)
}

// StreamArrow is unsupported — `gcs.rs` does not expose
// `stream_arrow_ipc`. Sync rows go through the connector-agent path
// instead.
func (a *Adapter) StreamArrow(_ context.Context, _ *models.Connection, _ *adapters.Query, _ string) (adapters.ArrowStream, error) {
	return nil, fmt.Errorf("%w: gcs arrow streaming", adapters.ErrNotImplemented)
}

// BuildIngestSpec is deferred (CMA-8 explicitly defers the gcs ingest
// variant). The dispatcher translates this into the existing "ingest is
// not supported for connector type: gcs" envelope.
func (a *Adapter) BuildIngestSpec(_ context.Context, _ *models.Connection, _ *adapters.Source) (*adapters.IngestSpec, error) {
	return nil, fmt.Errorf("%w: gcs ingest spec", adapters.ErrNotImplemented)
}
