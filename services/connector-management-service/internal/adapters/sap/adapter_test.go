package sap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

func TestValidateConfigRequiresBaseURL(t *testing.T) {
	require.Error(t, ValidateConfig(json.RawMessage(`{}`)))
	require.NoError(t, ValidateConfig(json.RawMessage(`{"base_url":"https://sap.example.com"}`)))
}

func TestNormalizeEntityRowsHandlesWrappers(t *testing.T) {
	t.Run("d.results", func(t *testing.T) {
		var v any
		require.NoError(t, json.Unmarshal([]byte(`{"d":{"results":[{"a":1},{"a":2}]}}`), &v))
		rows := normalizeEntityRows(v)
		require.Len(t, rows, 2)
	})
	t.Run("value array", func(t *testing.T) {
		var v any
		require.NoError(t, json.Unmarshal([]byte(`{"value":[{"a":1}]}`), &v))
		rows := normalizeEntityRows(v)
		require.Len(t, rows, 1)
	})
	t.Run("top-level array", func(t *testing.T) {
		var v any
		require.NoError(t, json.Unmarshal([]byte(`[{"a":1},{"a":2},{"a":3}]`), &v))
		rows := normalizeEntityRows(v)
		require.Len(t, rows, 3)
	})
	t.Run("scalar wraps to single row", func(t *testing.T) {
		var v any
		require.NoError(t, json.Unmarshal([]byte(`{"status":"ok"}`), &v))
		rows := normalizeEntityRows(v)
		require.Len(t, rows, 1)
	})
}

func TestODataEntitySetNamesPicksUpDEnvelope(t *testing.T) {
	var v any
	require.NoError(t, json.Unmarshal([]byte(`{"d":{"EntitySets":["Customers","Orders","Items"]}}`), &v))
	names := odataEntitySetNames(v)
	require.Equal(t, []string{"Customers", "Orders", "Items"}, names)
}

func TestDiscoverSourcesUsesInlineEntities(t *testing.T) {
	a := New()
	conn := &models.Connection{Config: json.RawMessage(`{
		"base_url": "https://sap.example.com",
		"entities": [
			{"selector": "Customers", "display_name": "Customer master", "extra": "x"},
			{"name": "Orders"}
		]
	}`)}
	sources, err := a.DiscoverSources(context.Background(), conn, "")
	require.NoError(t, err)
	require.Len(t, sources, 2)
	require.Equal(t, "Customers", sources[0].Selector)
	require.Equal(t, "Customer master", sources[0].DisplayName)
	require.Equal(t, "Orders", sources[1].Selector)
	require.Equal(t, "Orders", sources[1].DisplayName)
}

func TestDiscoverSourcesAgainstFakeOData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"d":{"EntitySets":["Customers","Orders"]}}`))
	}))
	defer srv.Close()

	a := New()
	a.SetHTTPClient(srv.Client())
	conn := &models.Connection{Config: json.RawMessage(`{"base_url":"` + srv.URL + `","service_path":"/odata/"}`)}
	sources, err := a.DiscoverSources(context.Background(), conn, "")
	require.NoError(t, err)
	require.Len(t, sources, 2)
	require.Equal(t, "Customers", sources[0].Selector)
	require.Equal(t, "sap_entity_set", sources[0].SourceKind)
}

func TestQueryVirtualTableAgainstFakeOData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		require.Equal(t, "v=2", r.Header.Get("X-Custom"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"d":{"results":[{"id":1,"name":"a"},{"id":2,"name":"b"}]}}`))
	}))
	defer srv.Close()

	a := New()
	a.SetHTTPClient(srv.Client())
	conn := &models.Connection{Config: json.RawMessage(`{
		"base_url":"` + srv.URL + `",
		"service_path":"/odata/",
		"bearer_token":"secret",
		"headers":{"X-Custom":"v=2"}
	}`)}
	limit := 1
	res, err := a.QueryVirtualTable(context.Background(), conn, &adapters.Query{Selector: "Customers", Limit: &limit}, "")
	require.NoError(t, err)
	require.Equal(t, "Customers", res.Selector)
	require.Equal(t, 1, res.RowCount)
	require.Len(t, res.Rows, 1)
	require.Equal(t, "zero_copy", res.Mode)
}

func TestStreamArrowReturnsErrNotImplemented(t *testing.T) {
	a := New()
	_, err := a.StreamArrow(context.Background(), &models.Connection{}, &adapters.Query{}, "")
	require.ErrorIs(t, err, adapters.ErrNotImplemented)
}

func TestBuildIngestSpec(t *testing.T) {
	a := New()
	conn := &models.Connection{Name: "sap-prod", Config: json.RawMessage(`{"base_url":"https://sap.example.com","service_path":"/odata/"}`)}
	spec, err := a.BuildIngestSpec(context.Background(), conn, &adapters.Source{Selector: "Customers"})
	require.NoError(t, err)
	require.Equal(t, "sap-prod", spec.Name)
	require.Equal(t, "sap", spec.Source)
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(spec.Config, &cfg))
	require.Equal(t, "https://sap.example.com", cfg["base_url"])
	require.Equal(t, "Customers", cfg["entity"])
	require.Equal(t, "/odata/", cfg["service_path"])
}

func TestUpstream4xxIsSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusUnauthorized)
	}))
	defer srv.Close()

	a := New()
	a.SetHTTPClient(srv.Client())
	conn := &models.Connection{Config: json.RawMessage(`{"base_url":"` + srv.URL + `"}`)}
	_, err := a.DiscoverSources(context.Background(), conn, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 401")
}
