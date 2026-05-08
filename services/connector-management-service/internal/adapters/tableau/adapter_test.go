package tableau

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

func TestValidateConfigAcceptsInlineViewCatalog(t *testing.T) {
	raw := json.RawMessage(`{
		"site_id": "openfoundry",
		"views": [{"view": "Revenue Scorecard", "preview_rows": [{"metric": "revenue", "value": 1024}]}]
	}`)
	require.NoError(t, ValidateConfig(raw))
}

func TestValidateConfigRejectsBareConfig(t *testing.T) {
	require.Error(t, ValidateConfig(json.RawMessage(`{"site_id":"openfoundry"}`)))
}

func TestValidateConfigRequiresSiteIDForResourceTemplate(t *testing.T) {
	raw := json.RawMessage(`{
		"base_url": "https://tableau.example.com/",
		"view_path_template": "/api/3.10/sites/{site_id}/views/{selector}"
	}`)
	err := ValidateConfig(raw)
	require.Error(t, err)
	require.Contains(t, err.Error(), "site_id")
}

func TestDiscoverSourcesReturnsInlineViews(t *testing.T) {
	c := &models.Connection{Config: json.RawMessage(`{
		"site_id": "openfoundry",
		"views": [
			{"view": "Revenue Scorecard"},
			{"view": "Pipeline Health", "display_name": "Pipeline"}
		]
	}`)}
	sources, err := New().DiscoverSources(context.Background(), c, "")
	require.NoError(t, err)
	require.Len(t, sources, 2)
	require.Equal(t, "Revenue Scorecard", sources[0].Selector)
	require.Equal(t, "tableau_view", sources[0].SourceKind)
	require.Equal(t, "Pipeline", sources[1].DisplayName)
}

func TestQueryVirtualTableServesInlineSampleRows(t *testing.T) {
	c := &models.Connection{Config: json.RawMessage(`{
		"site_id": "openfoundry",
		"views": [{
			"view": "Revenue Scorecard",
			"preview_rows": [{"metric": "revenue", "value": 1024}]
		}]
	}`)}
	res, err := New().QueryVirtualTable(context.Background(), c, &adapters.Query{Selector: "Revenue Scorecard"}, "")
	require.NoError(t, err)
	require.Equal(t, 1, res.RowCount)
	require.JSONEq(t, `{"metric":"revenue","value":1024}`, string(res.Rows[0]))
}

func TestDiscoverSourcesAgainstCatalogPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/views", r.URL.Path)
		require.Equal(t, "Bearer tableau-token", r.Header.Get("Authorization"))
		require.Equal(t, "trace-123", r.Header.Get("X-Trace"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"view":"Revenue Scorecard","display_name":"Revenue"},{"selector":"Pipeline Health","view_path":"/api/views/pipeline"}]}`))
	}))
	defer srv.Close()

	a := New()
	a.SetHTTPClient(srv.Client())
	conn := &models.Connection{Config: json.RawMessage(`{
		"base_url":"` + srv.URL + `",
		"catalog_path":"/api/views",
		"bearer_token":"tableau-token",
		"headers":{"X-Trace":"trace-123"}
	}`)}
	sources, err := a.DiscoverSources(context.Background(), conn, "")
	require.NoError(t, err)
	require.Len(t, sources, 2)
	require.Equal(t, "Revenue Scorecard", sources[0].Selector)
	require.Equal(t, "Revenue", sources[0].DisplayName)
	require.Equal(t, "tableau_view", sources[0].SourceKind)
	require.Equal(t, "Pipeline Health", sources[1].Selector)
}

func TestQueryVirtualTableAgainstViewTemplate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/sites/openfoundry/views/Revenue%20Scorecard/data", r.URL.EscapedPath())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"metric":"revenue","value":1024},{"metric":"margin","value":0.31}]}`))
	}))
	defer srv.Close()

	a := New()
	a.SetHTTPClient(srv.Client())
	conn := &models.Connection{Config: json.RawMessage(`{
		"base_url":"` + srv.URL + `",
		"site_id":"openfoundry",
		"view_path_template":"/api/sites/{site_id}/views/{selector}/data"
	}`)}
	limit := 1
	res, err := a.QueryVirtualTable(context.Background(), conn, &adapters.Query{Selector: "Revenue Scorecard", Limit: &limit}, "")
	require.NoError(t, err)
	require.Equal(t, "Revenue Scorecard", res.Selector)
	require.Equal(t, "zero_copy", res.Mode)
	require.Equal(t, 1, res.RowCount)
	require.Equal(t, []string{"metric", "value"}, res.Columns)
	require.JSONEq(t, `{"metric":"revenue","value":1024}`, string(res.Rows[0]))
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
