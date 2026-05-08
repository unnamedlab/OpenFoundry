package power_bi

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

func TestValidateConfigAcceptsInlineDatasetCatalog(t *testing.T) {
	raw := json.RawMessage(`{
		"workspace_id": "workspace-01",
		"datasets": [{"dataset": "ExecutiveMetrics", "preview_rows": [{"metric": "margin", "value": 0.31}]}]
	}`)
	require.NoError(t, ValidateConfig(raw))
}

func TestValidateConfigRejectsBareConfig(t *testing.T) {
	require.Error(t, ValidateConfig(json.RawMessage(`{"workspace_id":"workspace-01"}`)))
}

func TestValidateConfigRequiresWorkspaceIDForResourceTemplate(t *testing.T) {
	raw := json.RawMessage(`{
		"base_url": "https://api.powerbi.com/",
		"dataset_path_template": "/v1.0/myorg/groups/{workspace_id}/datasets/{selector}"
	}`)
	err := ValidateConfig(raw)
	require.Error(t, err)
	require.Contains(t, err.Error(), "workspace_id")
}

func TestDiscoverSourcesReturnsInlineDatasets(t *testing.T) {
	c := &models.Connection{Config: json.RawMessage(`{
		"workspace_id": "workspace-01",
		"datasets": [{"dataset": "ExecutiveMetrics"}]
	}`)}
	sources, err := New().DiscoverSources(context.Background(), c, "")
	require.NoError(t, err)
	require.Len(t, sources, 1)
	require.Equal(t, "ExecutiveMetrics", sources[0].Selector)
	require.Equal(t, "power_bi_dataset", sources[0].SourceKind)
}

func TestQueryVirtualTableServesInlineSampleRows(t *testing.T) {
	c := &models.Connection{Config: json.RawMessage(`{
		"workspace_id": "workspace-01",
		"datasets": [{
			"dataset": "ExecutiveMetrics",
			"preview_rows": [{"metric": "margin", "value": 0.31}]
		}]
	}`)}
	res, err := New().QueryVirtualTable(context.Background(), c, &adapters.Query{Selector: "ExecutiveMetrics"}, "")
	require.NoError(t, err)
	require.Equal(t, 1, res.RowCount)
	require.JSONEq(t, `{"metric":"margin","value":0.31}`, string(res.Rows[0]))
}

func TestDiscoverSourcesAgainstCatalogPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1.0/myorg/groups/workspace-01/datasets", r.URL.Path)
		require.Equal(t, "Bearer power-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":[{"dataset":"ExecutiveMetrics","display_name":"Executive Metrics"},{"selector":"SalesDataset"}]}`))
	}))
	defer srv.Close()

	a := New()
	a.SetHTTPClient(srv.Client())
	conn := &models.Connection{Config: json.RawMessage(`{
		"base_url":"` + srv.URL + `",
		"catalog_path":"/v1.0/myorg/groups/workspace-01/datasets",
		"bearer_token":"power-token"
	}`)}
	sources, err := a.DiscoverSources(context.Background(), conn, "")
	require.NoError(t, err)
	require.Len(t, sources, 2)
	require.Equal(t, "ExecutiveMetrics", sources[0].Selector)
	require.Equal(t, "Executive Metrics", sources[0].DisplayName)
	require.Equal(t, "power_bi_dataset", sources[0].SourceKind)
	require.Equal(t, "SalesDataset", sources[1].Selector)
}

func TestQueryVirtualTableAgainstDatasetTemplate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1.0/myorg/groups/workspace-01/datasets/ExecutiveMetrics/rows", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"records":[{"metric":"margin","value":0.31},{"metric":"revenue","value":1024}]}`))
	}))
	defer srv.Close()

	a := New()
	a.SetHTTPClient(srv.Client())
	conn := &models.Connection{Config: json.RawMessage(`{
		"base_url":"` + srv.URL + `",
		"workspace_id":"workspace-01",
		"dataset_path_template":"/v1.0/myorg/groups/{workspace_id}/datasets/{selector}/rows"
	}`)}
	limit := 1
	res, err := a.QueryVirtualTable(context.Background(), conn, &adapters.Query{Selector: "ExecutiveMetrics", Limit: &limit}, "")
	require.NoError(t, err)
	require.Equal(t, "ExecutiveMetrics", res.Selector)
	require.Equal(t, "zero_copy", res.Mode)
	require.Equal(t, 1, res.RowCount)
	require.Equal(t, []string{"metric", "value"}, res.Columns)
	require.JSONEq(t, `{"metric":"margin","value":0.31}`, string(res.Rows[0]))
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
