package databricks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

func TestValidateConfigAcceptsInlineTableCatalog(t *testing.T) {
	raw := json.RawMessage(`{
		"workspace_url": "https://dbc-12345.cloud.databricks.com",
		"http_path": "/sql/1.0/warehouses/abcd",
		"tables": [{"table": "main.sales.orders", "preview_rows": [{"order_id": 1, "amount": 1024}]}]
	}`)
	require.NoError(t, ValidateConfig(raw))
}

func TestValidateConfigRejectsBareConfig(t *testing.T) {
	require.Error(t, ValidateConfig(json.RawMessage(`{"workspace_url":"https://dbc-12345.cloud.databricks.com","http_path":"/sql/1.0/warehouses/abcd"}`)))
}

func TestValidateConfigRequiresIdentityFieldsForResourceTemplate(t *testing.T) {
	raw := json.RawMessage(`{
		"base_url": "https://dbc-12345.cloud.databricks.com/",
		"resource_path_template": "/api/2.0/sql/statements/{selector}"
	}`)
	err := ValidateConfig(raw)
	require.Error(t, err)
	require.Contains(t, err.Error(), "workspace_url")
	require.Contains(t, err.Error(), "http_path")
}

func TestValidateConfigAcceptsResourceTemplateWithIdentityFields(t *testing.T) {
	raw := json.RawMessage(`{
		"base_url": "https://dbc-12345.cloud.databricks.com/",
		"workspace_url": "https://dbc-12345.cloud.databricks.com",
		"http_path": "/sql/1.0/warehouses/abcd",
		"resource_path_template": "/api/2.0/sql/statements/{selector}"
	}`)
	require.NoError(t, ValidateConfig(raw))
}

func TestDiscoverSourcesReturnsInlineTables(t *testing.T) {
	c := &models.Connection{Config: json.RawMessage(`{
		"workspace_url": "https://dbc-12345.cloud.databricks.com",
		"http_path": "/sql/1.0/warehouses/abcd",
		"tables": [
			{"table": "main.sales.orders"},
			{"table": "main.sales.customers", "display_name": "Customers"}
		]
	}`)}
	sources, err := New().DiscoverSources(context.Background(), c, "")
	require.NoError(t, err)
	require.Len(t, sources, 2)
	require.Equal(t, "main.sales.orders", sources[0].Selector)
	require.Equal(t, "databricks_table", sources[0].SourceKind)
	require.Equal(t, "Customers", sources[1].DisplayName)
}

func TestQueryVirtualTableServesInlineSampleRows(t *testing.T) {
	c := &models.Connection{Config: json.RawMessage(`{
		"workspace_url": "https://dbc-12345.cloud.databricks.com",
		"http_path": "/sql/1.0/warehouses/abcd",
		"tables": [{
			"table": "main.sales.orders",
			"preview_rows": [{"order_id": 1, "amount": 1024}]
		}]
	}`)}
	res, err := New().QueryVirtualTable(context.Background(), c, &adapters.Query{Selector: "main.sales.orders"}, "")
	require.NoError(t, err)
	require.Equal(t, 1, res.RowCount)
	require.JSONEq(t, `{"order_id":1,"amount":1024}`, string(res.Rows[0]))
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
	f := Factory()
	a := f.New()
	require.NotNil(t, a)
	_, ok := a.(*Adapter)
	require.True(t, ok)
}
