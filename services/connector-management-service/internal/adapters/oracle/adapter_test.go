package oracle

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
		"host": "oracle.internal",
		"service_name": "analytics",
		"tables": [{"table": "ANALYTICS.ORDERS", "sample_rows": [{"ORDER_ID": "ord-1"}]}]
	}`)
	require.NoError(t, ValidateConfig(raw))
}

func TestValidateConfigRejectsBareConfig(t *testing.T) {
	require.Error(t, ValidateConfig(json.RawMessage(`{"host":"oracle.internal"}`)))
}

func TestValidateConfigRequiresHostForResourceTemplate(t *testing.T) {
	raw := json.RawMessage(`{
		"base_url": "https://oracle-bridge.example.com/",
		"resource_path_template": "/v1/oracle/{host}/tables/{selector}"
	}`)
	err := ValidateConfig(raw)
	require.Error(t, err)
	require.Contains(t, err.Error(), "host")
}

func TestDiscoverSourcesReturnsInlineTables(t *testing.T) {
	c := &models.Connection{Config: json.RawMessage(`{
		"host": "oracle.internal",
		"tables": [{"table": "ANALYTICS.ORDERS"}, {"table": "ANALYTICS.CUSTOMERS"}]
	}`)}
	sources, err := New().DiscoverSources(context.Background(), c, "")
	require.NoError(t, err)
	require.Len(t, sources, 2)
	require.Equal(t, "ANALYTICS.ORDERS", sources[0].Selector)
	require.Equal(t, "oracle_table", sources[0].SourceKind)
}

func TestQueryVirtualTableServesInlineSampleRows(t *testing.T) {
	c := &models.Connection{Config: json.RawMessage(`{
		"host": "oracle.internal",
		"tables": [{
			"table": "ANALYTICS.ORDERS",
			"sample_rows": [{"ORDER_ID": "ord-1"}, {"ORDER_ID": "ord-2"}]
		}]
	}`)}
	limit := 1
	res, err := New().QueryVirtualTable(context.Background(), c, &adapters.Query{Selector: "ANALYTICS.ORDERS", Limit: &limit}, "")
	require.NoError(t, err)
	require.Equal(t, 1, res.RowCount)
	require.JSONEq(t, `{"ORDER_ID":"ord-1"}`, string(res.Rows[0]))
}

func TestQueryVirtualTableFetchesRemoteResource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/tables/ANALYTICS.ORDERS/preview", r.URL.Path)
		_, _ = w.Write([]byte(`[{"ORDER_ID":"ord-1"},{"ORDER_ID":"ord-2"}]`))
	}))
	defer srv.Close()

	a := New()
	a.SetHTTPClient(srv.Client())
	c := &models.Connection{Config: json.RawMessage(`{
		"host": "oracle.internal",
		"base_url": ` + quoteJSON(srv.URL+"/") + `,
		"resource_path_template": "/tables/{selector}/preview"
	}`)}
	limit := 1
	res, err := a.QueryVirtualTable(context.Background(), c, &adapters.Query{Selector: "ANALYTICS.ORDERS", Limit: &limit}, "")
	require.NoError(t, err)
	require.Equal(t, 1, res.RowCount)
	require.JSONEq(t, `{"ORDER_ID":"ord-1"}`, string(res.Rows[0]))
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

func quoteJSON(s string) string {
	encoded, _ := json.Marshal(s)
	return string(encoded)
}
