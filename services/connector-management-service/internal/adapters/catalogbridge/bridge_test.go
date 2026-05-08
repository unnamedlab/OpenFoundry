package catalogbridge

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

func newConnection(t *testing.T, cfg map[string]any) *models.Connection {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	return &models.Connection{Config: raw}
}

func TestValidateConfigAcceptsInlineCatalog(t *testing.T) {
	b := New("tableau", "tableau_view", []string{"site_id"})
	cfg := map[string]any{
		"site_id": "openfoundry",
		"views": []any{
			map[string]any{
				"view":         "Revenue Scorecard",
				"preview_rows": []any{map[string]any{"metric": "revenue", "value": 1024}},
			},
		},
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, b.ValidateConfig(raw))
}

func TestValidateConfigAcceptsBaseURLAndCatalogPath(t *testing.T) {
	b := New("power_bi", "power_bi_dataset", []string{"workspace_id"})
	cfg := map[string]any{
		"base_url":     "https://api.powerbi.com/",
		"catalog_path": "/v1.0/myorg/datasets",
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, b.ValidateConfig(raw))
}

func TestValidateConfigRequiresIdentityFieldsForResourceTemplate(t *testing.T) {
	b := New("tableau", "tableau_view", []string{"site_id"})
	cfg := map[string]any{
		"base_url":           "https://api.example.com/",
		"view_path_template": "/views/{selector}",
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	err = b.ValidateConfig(raw)
	require.Error(t, err)
	require.Contains(t, err.Error(), "site_id")
}

func TestValidateConfigRejectsBareConfig(t *testing.T) {
	b := New("odbc", "odbc_table", []string{"dsn"})
	raw, err := json.Marshal(map[string]any{"dsn": "WarehouseDSN"})
	require.NoError(t, err)
	err = b.ValidateConfig(raw)
	require.Error(t, err)
	require.Contains(t, err.Error(), "inline catalog")
}

func TestDiscoverSourcesReturnsInlineCatalog(t *testing.T) {
	b := New("jdbc", "jdbc_table", []string{"jdbc_url"})
	c := newConnection(t, map[string]any{
		"jdbc_url": "jdbc:postgresql://warehouse.internal:5432/analytics",
		"tables": []any{
			map[string]any{
				"table":        "analytics.orders",
				"display_name": "Orders",
				"sample_rows":  []any{map[string]any{"order_id": "ord-1"}},
			},
			map[string]any{"table": "analytics.customers"},
		},
	})
	sources, err := b.DiscoverSources(context.Background(), c)
	require.NoError(t, err)
	require.Len(t, sources, 2)
	require.Equal(t, "analytics.orders", sources[0].Selector)
	require.Equal(t, "Orders", sources[0].DisplayName)
	require.Equal(t, "jdbc_table", sources[0].SourceKind)
	require.True(t, sources[0].SupportsSync)
	require.True(t, sources[0].SupportsZeroCopy)
}

func TestQueryVirtualTableReturnsInlineSampleRows(t *testing.T) {
	b := New("tableau", "tableau_view", []string{"site_id"})
	c := newConnection(t, map[string]any{
		"site_id": "openfoundry",
		"views": []any{
			map[string]any{
				"view": "Revenue Scorecard",
				"preview_rows": []any{
					map[string]any{"metric": "revenue", "value": 1024},
					map[string]any{"metric": "margin", "value": 0.31},
				},
			},
		},
	})
	limit := 1
	res, err := b.QueryVirtualTable(context.Background(), c, &adapters.Query{
		Selector: "Revenue Scorecard",
		Limit:    &limit,
	})
	require.NoError(t, err)
	require.Equal(t, "Revenue Scorecard", res.Selector)
	require.Equal(t, "zero_copy", res.Mode)
	require.Equal(t, 1, res.RowCount)
	require.Len(t, res.Rows, 1)
	require.JSONEq(t, `{"metric":"revenue","value":1024}`, string(res.Rows[0]))
	require.ElementsMatch(t, []string{"metric", "value"}, res.Columns)

	var meta map[string]any
	require.NoError(t, json.Unmarshal(res.Metadata, &meta))
	require.Equal(t, "tableau", meta["connector"])
	require.Equal(t, "inline_catalog", meta["mode"])
}

func TestQueryVirtualTableClampsLimit(t *testing.T) {
	b := New("power_bi", "power_bi_dataset", []string{"workspace_id"})
	c := newConnection(t, map[string]any{
		"workspace_id": "ws-1",
		"datasets": []any{
			map[string]any{
				"dataset":     "Sales",
				"sample_rows": makeRows(700),
			},
		},
	})
	res, err := b.QueryVirtualTable(context.Background(), c, &adapters.Query{Selector: "Sales"})
	require.NoError(t, err)
	require.Equal(t, 50, res.RowCount, "default limit should be 50")

	zero := 0
	res, err = b.QueryVirtualTable(context.Background(), c, &adapters.Query{Selector: "Sales", Limit: &zero})
	require.NoError(t, err)
	require.Equal(t, 1, res.RowCount, "limit < 1 clamped to 1")

	huge := 9999
	res, err = b.QueryVirtualTable(context.Background(), c, &adapters.Query{Selector: "Sales", Limit: &huge})
	require.NoError(t, err)
	require.Equal(t, 500, res.RowCount, "limit > 500 clamped to 500")
}

func TestDiscoverSourcesFetchesRemoteCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1.0/myorg/datasets", r.URL.Path)
		require.Equal(t, "Bearer abc123", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"items":[{"name":"executive","display_name":"Executive"},{"name":"finance"}]}`))
	}))
	defer srv.Close()

	b := New("power_bi", "power_bi_dataset", []string{"workspace_id"})
	b.HTTPClient = srv.Client()
	c := newConnection(t, map[string]any{
		"base_url":     srv.URL + "/",
		"catalog_path": "/v1.0/myorg/datasets",
		"bearer_token": "abc123",
	})
	sources, err := b.DiscoverSources(context.Background(), c)
	require.NoError(t, err)
	require.Len(t, sources, 2)
	require.Equal(t, "executive", sources[0].Selector)
	require.Equal(t, "Executive", sources[0].DisplayName)
	require.Equal(t, "finance", sources[1].Selector)
}

func TestDiscoverSourcesRemoteHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	b := New("tableau", "tableau_view", []string{"site_id"})
	b.HTTPClient = srv.Client()
	c := newConnection(t, map[string]any{
		"site_id":      "openfoundry",
		"base_url":     srv.URL + "/",
		"catalog_path": "/api/3.10/sites/openfoundry/views",
	})
	_, err := b.DiscoverSources(context.Background(), c)
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 503")
}

func TestQueryVirtualTableFetchesRemoteResource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/datasets/Sales/preview", r.URL.Path)
		_, _ = w.Write([]byte(`[{"id":1,"v":"x"},{"id":2,"v":"y"}]`))
	}))
	defer srv.Close()

	b := New("power_bi", "power_bi_dataset", []string{"workspace_id"})
	b.HTTPClient = srv.Client()
	c := newConnection(t, map[string]any{
		"workspace_id":          "ws-1",
		"base_url":              srv.URL + "/",
		"dataset_path_template": "/datasets/{selector}/preview",
	})
	limit := 1
	res, err := b.QueryVirtualTable(context.Background(), c, &adapters.Query{Selector: "Sales", Limit: &limit})
	require.NoError(t, err)
	require.Equal(t, 1, res.RowCount)
	require.JSONEq(t, `{"id":1,"v":"x"}`, string(res.Rows[0]))
}

func TestNormalizeRecordsUnwrapsEnvelopes(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  int
	}{
		{"data envelope", `{"data":[{"id":1},{"id":2}]}`, 2},
		{"items envelope", `{"items":[{"id":1}]}`, 1},
		{"records envelope", `{"records":[]}`, 0},
		{"odata value envelope", `{"value":[{"id":1},{"id":2},{"id":3}]}`, 3},
		{"top-level array", `[{"id":1},{"id":2}]`, 2},
		{"raw object becomes single row", `{"id":1}`, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var payload any
			require.NoError(t, json.Unmarshal([]byte(tc.input), &payload))
			rows := normalizeRecords(payload)
			require.Len(t, rows, tc.want)
		})
	}
}

func TestExtractColumnsPreservesFirstRowOrder(t *testing.T) {
	rows := []json.RawMessage{
		json.RawMessage(`{"a":1,"b":2,"c":3}`),
		json.RawMessage(`{"x":9}`),
	}
	cols := extractColumns(rows)
	require.Equal(t, []string{"a", "b", "c"}, cols)
}

func makeRows(n int) []any {
	out := make([]any, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, map[string]any{"id": i})
	}
	return out
}
