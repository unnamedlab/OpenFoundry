package mssql

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

func TestValidateConfigAcceptsInlineTableCatalog(t *testing.T) {
	raw := json.RawMessage(`{
		"host": "sqlserver.internal",
		"port": 1433,
		"database": "analytics",
		"tables": [{"table": "dbo.orders", "sample_rows": [{"order_id": "ord-1"}]}]
	}`)
	require.NoError(t, ValidateConfig(raw))
}

func TestValidateConfigRejectsBareConfig(t *testing.T) {
	require.Error(t, ValidateConfig(json.RawMessage(`{"host":"sqlserver.internal"}`)))
}

func TestValidateConfigRequiresHostForResourceTemplate(t *testing.T) {
	raw := json.RawMessage(`{
		"base_url": "https://mssql-bridge.example.com/",
		"resource_path_template": "/v1/mssql/{host}/tables/{selector}"
	}`)
	err := ValidateConfig(raw)
	require.Error(t, err)
	require.Contains(t, err.Error(), "host")
}

func TestDiscoverSourcesReturnsInlineTables(t *testing.T) {
	c := &models.Connection{Config: json.RawMessage(`{
		"host": "sqlserver.internal",
		"tables": [{"table": "dbo.orders"}, {"table": "dbo.customers"}]
	}`)}
	sources, err := New().DiscoverSources(context.Background(), c, "")
	require.NoError(t, err)
	require.Len(t, sources, 2)
	require.Equal(t, "dbo.orders", sources[0].Selector)
	require.Equal(t, "mssql_table", sources[0].SourceKind)
}

func TestQueryVirtualTableServesInlineSampleRows(t *testing.T) {
	c := &models.Connection{Config: json.RawMessage(`{
		"host": "sqlserver.internal",
		"tables": [{
			"table": "dbo.orders",
			"sample_rows": [{"order_id": "ord-1"}, {"order_id": "ord-2"}]
		}]
	}`)}
	limit := 1
	res, err := New().QueryVirtualTable(context.Background(), c, &adapters.Query{Selector: "dbo.orders", Limit: &limit}, "")
	require.NoError(t, err)
	require.Equal(t, 1, res.RowCount)
	require.JSONEq(t, `{"order_id":"ord-1"}`, string(res.Rows[0]))
}

func TestDiscoverSourcesFetchesRemoteCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/catalog", r.URL.Path)
		_, _ = w.Write([]byte(`{"items":[{"name":"dbo.orders","display_name":"Orders"}]}`))
	}))
	defer srv.Close()

	a := New()
	a.SetHTTPClient(srv.Client())
	c := &models.Connection{Config: json.RawMessage(`{
		"host": "sqlserver.internal",
		"base_url": ` + strconvQuote(srv.URL+"/") + `,
		"catalog_path": "/catalog"
	}`)}
	sources, err := a.DiscoverSources(context.Background(), c, "")
	require.NoError(t, err)
	require.Len(t, sources, 1)
	require.Equal(t, "dbo.orders", sources[0].Selector)
	require.Equal(t, "Orders", sources[0].DisplayName)
}

func TestStreamArrowReturnsNotImplemented(t *testing.T) {
	_, err := New().StreamArrow(context.Background(), &models.Connection{}, &adapters.Query{}, "")
	require.True(t, errors.Is(err, adapters.ErrNotImplemented))
}

func TestBuildIngestSpecReturnsNotImplemented(t *testing.T) {
	_, err := New().BuildIngestSpec(context.Background(), &models.Connection{}, &adapters.Source{})
	require.True(t, errors.Is(err, adapters.ErrNotImplemented))
}

func TestFactoryProducesFreshAdapter(t *testing.T) {
	a := Factory().New()
	require.NotNil(t, a)
	_, ok := a.(*Adapter)
	require.True(t, ok)
}

func strconvQuote(s string) string {
	encoded, _ := json.Marshal(s)
	return string(encoded)
}
