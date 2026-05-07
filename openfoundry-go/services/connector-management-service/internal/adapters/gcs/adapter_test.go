package gcs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

func TestValidateConfigRejectsMissingBucket(t *testing.T) {
	t.Parallel()
	if err := ValidateConfig(json.RawMessage(`{}`)); err == nil {
		t.Fatalf("expected error for empty config")
	}
	if err := ValidateConfig(json.RawMessage(`{"access_token":"t"}`)); err == nil {
		t.Fatalf("expected error when 'bucket' is missing")
	}
}

func TestValidateConfigRejectsMissingCredential(t *testing.T) {
	t.Parallel()
	if err := ValidateConfig(json.RawMessage(`{"bucket":"b"}`)); err == nil {
		t.Fatalf("expected error for missing credential")
	}
	if err := ValidateConfig(json.RawMessage(`{"bucket":"b","application_default":false}`)); err == nil {
		t.Fatalf("expected error when application_default is explicitly false")
	}
}

func TestValidateConfigAcceptsAccessToken(t *testing.T) {
	t.Parallel()
	if err := ValidateConfig(json.RawMessage(`{"bucket":"b","access_token":"t"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateConfigAcceptsServiceAccountJSON(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{"bucket":"b","service_account_json":{"type":"service_account"}}`)
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateConfigAcceptsApplicationDefault(t *testing.T) {
	t.Parallel()
	if err := ValidateConfig(json.RawMessage(`{"bucket":"b","application_default":true}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiscoveryEmitsGCSIcebergSources(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{
		"bucket":"b","access_token":"t",
		"iceberg_tables":[{"selector":"db.t","metadata_location":"gs://b/x.json","snapshot_id":"42"}]
	}`)
	conn := &models.Connection{ConnectorType: ConnectorType, Config: cfg}
	got, err := New().DiscoverSources(context.Background(), conn, "")
	if err != nil {
		t.Fatalf("DiscoverSources: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].SourceKind != "gcs_iceberg_table" {
		t.Fatalf("source_kind = %q", got[0].SourceKind)
	}
	if !got[0].SupportsZeroCopy {
		t.Fatalf("expected SupportsZeroCopy=true")
	}
	if got[0].SourceSignature == nil || *got[0].SourceSignature != "42" {
		t.Fatalf("expected snapshot id signature, got %v", got[0].SourceSignature)
	}
}

func TestDiscoveryFailsWhenNoTablesDeclared(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{"bucket":"b","access_token":"t"}`)
	conn := &models.Connection{ConnectorType: ConnectorType, Config: cfg}
	_, err := New().DiscoverSources(context.Background(), conn, "")
	if err == nil {
		t.Fatalf("expected error when no inline tables declared")
	}
	if !strings.Contains(err.Error(), "did not expose any virtual tables") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestQueryVirtualTableServesInlineSampleRows(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{
		"bucket":"b","access_token":"t",
		"tables":[
			{"selector":"orders","sample_rows":[{"id":1,"name":"a"},{"id":2,"name":"b"}]}
		]
	}`)
	conn := &models.Connection{ConnectorType: ConnectorType, Config: cfg}
	limit := 10
	q := &adapters.Query{Selector: "orders", Limit: &limit}
	res, err := New().QueryVirtualTable(context.Background(), conn, q, "")
	if err != nil {
		t.Fatalf("QueryVirtualTable: %v", err)
	}
	if res.RowCount != 2 {
		t.Fatalf("RowCount = %d, want 2", res.RowCount)
	}
	if res.Selector != "orders" {
		t.Fatalf("Selector = %q", res.Selector)
	}
}

func TestQueryVirtualTableFetchesRemoteCatalogResource(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/datasets/orders" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":1},{"id":2},{"id":3}]}`))
	}))
	defer server.Close()

	cfgJSON, err := json.Marshal(map[string]any{
		"bucket":                  "b",
		"access_token":            "t",
		"base_url":                server.URL,
		"dataset_path_template":   "/datasets/{selector}",
		"tables":                  []map[string]any{{"selector": "orders"}},
	})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	conn := &models.Connection{ConnectorType: ConnectorType, Config: cfgJSON}
	q := &adapters.Query{Selector: "orders"}

	a := New()
	a.SetHTTPClient(server.Client())
	res, err := a.QueryVirtualTable(context.Background(), conn, q, "")
	if err != nil {
		t.Fatalf("QueryVirtualTable: %v", err)
	}
	if res.RowCount != 3 {
		t.Fatalf("RowCount = %d, want 3", res.RowCount)
	}
}

func TestQueryVirtualTableValidatesGCSConfig(t *testing.T) {
	t.Parallel()
	conn := &models.Connection{ConnectorType: ConnectorType, Config: json.RawMessage(`{}`)}
	_, err := New().QueryVirtualTable(context.Background(), conn, &adapters.Query{Selector: "x"}, "")
	if err == nil {
		t.Fatalf("expected validation error for missing bucket")
	}
	if !strings.Contains(err.Error(), "bucket") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnsupportedCapabilitiesReturnNotImplemented(t *testing.T) {
	t.Parallel()
	a := New()
	conn := &models.Connection{}
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
