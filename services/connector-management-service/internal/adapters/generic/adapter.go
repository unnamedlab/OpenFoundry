// Package generic is the Go port of
// `services/connector-management-service/src/connectors/generic.rs` —
// the generic / custom open-table connector.
//
// Foundry-aligned: this is the "external Iceberg/Delta" generic source that
// lets a customer wire any object store / REST catalog without us shipping a
// bespoke connector. It is the SDK entry point referenced by
// `docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/Connectivity/Data Connection/External connections from code/External transforms.md`.
//
// SDK contract — supply `connector_type: "generic"` plus a `config` block:
//
//	{
//	  "label": "ACME Iceberg lake",                    // optional human label
//	  "catalog_url": "https://catalog.example.com/iceberg/v1", // optional REST catalog
//	  "iceberg_tables": [ { "selector":"…", "metadata_location":"…" } ],
//	  "delta_tables":   [ { "selector":"…", "table_location":"…"   } ]
//	}
//
// Either `iceberg_tables[]`, `delta_tables[]` or `catalog_url` must be set.
// The dispatcher returns one [adapters.Source] per inline entry tagged with
// SourceKind = "generic_iceberg_table" / "generic_delta_table" so the
// LoadTable handler forwards the upstream pointer verbatim — same zero-copy
// contract as the dedicated stores.
package generic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters/opentable"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

// ConnectorType is the `connections.connector_type` value the registry
// binds this adapter under. Mirrors the Rust module's implicit name.
const ConnectorType = "generic"

const storePrefix = "generic"

// Adapter is the generic [adapters.ConnectorAdapter] implementation. It is
// stateless and safe for concurrent use.
type Adapter struct{}

// New returns a ready-to-use [Adapter].
func New() *Adapter { return &Adapter{} }

// Factory returns an [adapters.Factory] that yields the singleton Adapter.
// The generic adapter holds no per-connection state, so a single instance is
// shared across requests.
func Factory() adapters.Factory { return adapters.SingletonFactory(New()) }

type genericConfig struct {
	CatalogURL string `json:"catalog_url"`
}

// ValidateConfig mirrors Rust's `validate_config`: at least one of
// inline tables or a catalog_url must be set.
func ValidateConfig(raw json.RawMessage) error {
	if opentable.HasCatalog(raw) {
		return nil
	}
	if len(raw) > 0 {
		var cfg genericConfig
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("generic: invalid config: %w", err)
		}
		if strings.TrimSpace(cfg.CatalogURL) != "" {
			return nil
		}
	}
	return errors.New("generic connector requires 'iceberg_tables[]', 'delta_tables[]' or 'catalog_url'")
}

// DiscoverSources returns one [adapters.Source] per inline entry. When the
// connection is configured with `catalog_url` only, discovery returns an
// empty slice — the LoadTable handler proxies catalog_url verbatim, so the
// platform defers source enumeration to the client. Mirrors Rust's
// `discover_sources` exactly.
func (a *Adapter) DiscoverSources(_ context.Context, c *models.Connection, _ string) ([]adapters.Source, error) {
	if c == nil {
		return nil, errors.New("generic: connection is nil")
	}
	if err := ValidateConfig(c.Config); err != nil {
		return nil, err
	}
	sources, err := opentable.Discover(c.Config, storePrefix)
	if err != nil {
		return nil, fmt.Errorf("generic: %w", err)
	}
	if sources == nil {
		return []adapters.Source{}, nil
	}
	return sources, nil
}

// QueryVirtualTable is unsupported for inline-catalog sources — clients
// resolve the upstream metadata pointer through the Iceberg REST `LoadTable`
// path instead.
func (a *Adapter) QueryVirtualTable(_ context.Context, _ *models.Connection, _ *adapters.Query, _ string) (*adapters.Result, error) {
	return nil, fmt.Errorf("%w: generic virtual-table preview", adapters.ErrNotImplemented)
}

// StreamArrow is unsupported for the same reason as QueryVirtualTable.
func (a *Adapter) StreamArrow(_ context.Context, _ *models.Connection, _ *adapters.Query, _ string) (adapters.ArrowStream, error) {
	return nil, fmt.Errorf("%w: generic arrow streaming", adapters.ErrNotImplemented)
}

// BuildIngestSpec is unsupported — generic is a zero-copy source, so
// ingestion-replication-service is not in the path.
func (a *Adapter) BuildIngestSpec(_ context.Context, _ *models.Connection, _ *adapters.Source) (*adapters.IngestSpec, error) {
	return nil, fmt.Errorf("%w: generic ingest spec", adapters.ErrNotImplemented)
}
