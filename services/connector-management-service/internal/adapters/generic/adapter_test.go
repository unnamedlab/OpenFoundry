package generic

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

func TestValidateConfigRequiresInlineTablesOrCatalogURL(t *testing.T) {
	t.Parallel()
	if err := ValidateConfig(json.RawMessage(`{}`)); err == nil {
		t.Fatalf("expected error for empty config")
	}
	if err := ValidateConfig(json.RawMessage(`{"catalog_url":"https://x"}`)); err != nil {
		t.Fatalf("catalog_url should be accepted: %v", err)
	}
	if err := ValidateConfig(json.RawMessage(`{"iceberg_tables":[{"selector":"a","metadata_location":"s3://x"}]}`)); err != nil {
		t.Fatalf("iceberg_tables should be accepted: %v", err)
	}
	if err := ValidateConfig(json.RawMessage(`{"catalog_url":"   "}`)); err == nil {
		t.Fatalf("blank catalog_url should be rejected")
	}
}

func TestDiscoveryEmitsGenericKindForInlineTables(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{"iceberg_tables":[{"selector":"a.b","metadata_location":"s3://x/m.json"}]}`)
	conn := &models.Connection{ConnectorType: ConnectorType, Config: cfg}
	got, err := New().DiscoverSources(context.Background(), conn, "")
	if err != nil {
		t.Fatalf("DiscoverSources: %v", err)
	}
	if len(got) != 1 || got[0].SourceKind != "generic_iceberg_table" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestDiscoveryReturnsEmptySliceForCatalogURLOnly(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{"catalog_url":"https://catalog.example.com/iceberg/v1"}`)
	conn := &models.Connection{ConnectorType: ConnectorType, Config: cfg}
	got, err := New().DiscoverSources(context.Background(), conn, "")
	if err != nil {
		t.Fatalf("DiscoverSources: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0 (catalog_url-only path defers to client)", len(got))
	}
}

func TestDiscoveryRejectsConfigWithNoTablesOrCatalog(t *testing.T) {
	t.Parallel()
	conn := &models.Connection{ConnectorType: ConnectorType, Config: json.RawMessage(`{"label":"hello"}`)}
	if _, err := New().DiscoverSources(context.Background(), conn, ""); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestUnsupportedCapabilitiesReturnNotImplemented(t *testing.T) {
	t.Parallel()
	a := New()
	conn := &models.Connection{}
	if _, err := a.QueryVirtualTable(context.Background(), conn, &adapters.Query{}, ""); !errors.Is(err, adapters.ErrNotImplemented) {
		t.Fatalf("QueryVirtualTable err = %v", err)
	}
	if _, err := a.StreamArrow(context.Background(), conn, &adapters.Query{}, ""); !errors.Is(err, adapters.ErrNotImplemented) {
		t.Fatalf("StreamArrow err = %v", err)
	}
	if _, err := a.BuildIngestSpec(context.Background(), conn, &adapters.Source{}); !errors.Is(err, adapters.ErrNotImplemented) {
		t.Fatalf("BuildIngestSpec err = %v", err)
	}
}

func TestFactoryReturnsConnectorAdapter(t *testing.T) {
	t.Parallel()
	var _ adapters.ConnectorAdapter = Factory().New()
}
