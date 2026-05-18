package mysql

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

// TestValidateConfigAcceptsInlineTableCatalog mirrors Rust's
// `accepts_inline_table_catalog` test in `connectors/mysql.rs`.
func TestValidateConfigAcceptsInlineTableCatalog(t *testing.T) {
	raw := json.RawMessage(`{
		"host": "mysql.internal",
		"port": 3306,
		"database": "analytics",
		"user": "foundry_reader",
		"tables": [
			{
				"table": "public.orders",
				"sample_rows": [{"order_id": "ord-1"}]
			}
		]
	}`)
	require.NoError(t, ValidateConfig(raw))
}

// TestValidateConfigRejectsEmptyConfig mirrors Rust's `rejects_empty_config`
// test in `connectors/mysql.rs`.
func TestValidateConfigRejectsEmptyConfig(t *testing.T) {
	require.Error(t, ValidateConfig(json.RawMessage(`{}`)))
}

func TestValidateConfigRejectsBareHost(t *testing.T) {
	require.Error(t, ValidateConfig(json.RawMessage(`{"host":"mysql.internal"}`)))
}

func TestValidateConfigRequiresHostForResourceTemplate(t *testing.T) {
	raw := json.RawMessage(`{
		"base_url": "https://mysql-bridge.example.com/",
		"resource_path_template": "/v1/mysql/{host}/tables/{selector}"
	}`)
	err := ValidateConfig(raw)
	require.Error(t, err)
	require.Contains(t, err.Error(), "host")
}

func TestDiscoverSourcesReturnsInlineTables(t *testing.T) {
	c := &models.Connection{Config: json.RawMessage(`{
		"host": "mysql.internal",
		"tables": [{"table": "analytics.orders"}, {"table": "analytics.customers"}]
	}`)}
	sources, err := New().DiscoverSources(context.Background(), c, "")
	require.NoError(t, err)
	require.Len(t, sources, 2)
	require.Equal(t, "analytics.orders", sources[0].Selector)
	require.Equal(t, "mysql_table", sources[0].SourceKind)
}

func TestQueryVirtualTableServesInlineSampleRows(t *testing.T) {
	c := &models.Connection{Config: json.RawMessage(`{
		"host": "mysql.internal",
		"tables": [{
			"table": "analytics.orders",
			"sample_rows": [{"order_id": "ord-1"}, {"order_id": "ord-2"}]
		}]
	}`)}
	limit := 1
	res, err := New().QueryVirtualTable(context.Background(), c, &adapters.Query{Selector: "analytics.orders", Limit: &limit}, "")
	require.NoError(t, err)
	require.Equal(t, 1, res.RowCount)
	require.JSONEq(t, `{"order_id":"ord-1"}`, string(res.Rows[0]))
}

func TestDiscoverSourcesFetchesRemoteCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/catalog", r.URL.Path)
		_, _ = w.Write([]byte(`{"items":[{"name":"analytics.orders","display_name":"Orders"}]}`))
	}))
	defer srv.Close()

	a := New()
	a.SetHTTPClient(srv.Client())
	baseURL, err := json.Marshal(srv.URL + "/")
	require.NoError(t, err)
	c := &models.Connection{Config: json.RawMessage(`{
		"host": "mysql.internal",
		"base_url": ` + string(baseURL) + `,
		"catalog_path": "/catalog"
	}`)}
	sources, err := a.DiscoverSources(context.Background(), c, "")
	require.NoError(t, err)
	require.Len(t, sources, 1)
	require.Equal(t, "analytics.orders", sources[0].Selector)
	require.Equal(t, "Orders", sources[0].DisplayName)
}

func TestStreamArrowProducesParseableArrowIPC(t *testing.T) {
	c := &models.Connection{Config: json.RawMessage(`{
		"host":"mysql.internal",
		"tables":[{"table":"analytics.orders","sample_rows":[{
			"id":1,
			"amount":10.5,
			"active":true,
			"created_at":"2026-05-18T10:00:00Z",
			"notes":null,
			"payload":{"region":"us"}
		}]}]
	}`)}
	limit := 1
	stream, err := New().StreamArrow(context.Background(), c, &adapters.Query{Selector: "analytics.orders", Limit: &limit}, "")
	require.NoError(t, err)
	defer stream.Close()

	frame, err := stream.Next(context.Background())
	require.NoError(t, err)
	require.NotContains(t, string(frame), "openfoundry.arrow.ipc.json.v1")
	_, err = stream.Next(context.Background())
	require.ErrorIs(t, err, io.EOF)

	rdr, err := ipc.NewReader(bytes.NewReader(frame), ipc.WithAllocator(memory.NewGoAllocator()))
	require.NoError(t, err)
	defer rdr.Release()

	require.True(t, rdr.Next(), "expected one record batch")
	rec := rdr.RecordBatch()
	require.Equal(t, int64(1), rec.NumRows())
	fields := map[string]arrow.Type{}
	for i := 0; i < int(rec.Schema().NumFields()); i++ {
		field := rec.Schema().Field(i)
		fields[field.Name] = field.Type.ID()
	}
	require.Equal(t, arrow.INT64, fields["id"])
	require.Equal(t, arrow.FLOAT64, fields["amount"])
	require.Equal(t, arrow.BOOL, fields["active"])
	require.Equal(t, arrow.TIMESTAMP, fields["created_at"])
	require.Equal(t, arrow.NULL, fields["notes"])
	require.Equal(t, arrow.STRING, fields["payload"])
}

func TestStreamArrowPropagatesQueryErrors(t *testing.T) {
	c := &models.Connection{Config: json.RawMessage(`{"host":"mysql.internal","tables":[{"table":"analytics.orders"}]}`)}
	_, err := New().StreamArrow(context.Background(), c, &adapters.Query{Selector: "analytics.orders"}, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "base_url")
}

func TestBuildIngestSpecRedactsSecrets(t *testing.T) {
	c := &models.Connection{Name: "orders", Config: json.RawMessage(`{
		"host":"mysql.internal",
		"database":"analytics",
		"Password":"secret",
		"api_key":"api-secret",
		"bearer_token":"bearer-secret",
		"headers":{"Token":"header-secret","x-safe":"ok"},
		"credentials":{"user":"hidden"},
		"tables":[{"table":"analytics.orders"}]
	}`)}
	spec, err := New().BuildIngestSpec(context.Background(), c, &adapters.Source{Selector: "analytics.orders"})
	require.NoError(t, err)
	require.Equal(t, "mysql", spec.Source)
	require.NotContains(t, string(spec.Config), "secret")
	require.NotContains(t, string(spec.Config), "Password")
	require.NotContains(t, string(spec.Config), "api_key")
	require.NotContains(t, string(spec.Config), "bearer_token")
	require.NotContains(t, string(spec.Config), "credentials")
	require.Contains(t, string(spec.Config), "analytics.orders")
}

func TestBuildIngestSpecIncludesSelectorTableQueryAndCursor(t *testing.T) {
	c := &models.Connection{Name: "orders", Config: json.RawMessage(`{
		"host":"mysql.internal",
		"database":"analytics",
		"query":"select * from analytics.orders",
		"cursor":"updated_at",
		"tables":[{"table":"analytics.orders"}]
	}`)}
	spec, err := New().BuildIngestSpec(context.Background(), c, &adapters.Source{Selector: "analytics.orders"})
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(spec.Config, &payload))
	require.Equal(t, "analytics.orders", payload["selector"])
	require.Equal(t, "analytics.orders", payload["table"])
	require.Equal(t, "select * from analytics.orders", payload["query"])
	require.Contains(t, payload, "source_identity")
	require.Equal(t, map[string]any{"cursor": "updated_at"}, payload["incremental"])
}

func TestBuildIngestSpecValidatesInputs(t *testing.T) {
	_, err := New().BuildIngestSpec(context.Background(), nil, &adapters.Source{Selector: "analytics.orders"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "connection is nil")

	c := &models.Connection{Name: "orders", Config: json.RawMessage(`{"host":"mysql.internal","tables":[{"table":"analytics.orders"}]}`)}
	_, err = New().BuildIngestSpec(context.Background(), c, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "source is nil")
}

func TestFactoryProducesFreshAdapter(t *testing.T) {
	a := Factory().New()
	require.NotNil(t, a)
	_, ok := a.(*Adapter)
	require.True(t, ok)
}
