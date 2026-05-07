package kinesis

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

// TestValidateConfigRequiresStream mirrors Rust's `requires_stream_name`.
func TestValidateConfigRequiresStream(t *testing.T) {
	require.Error(t, ValidateConfig(json.RawMessage(`{}`)))
	require.NoError(t, ValidateConfig(json.RawMessage(`{"stream_name":"orders"}`)))
}

// TestParsesSelectorWithShard mirrors Rust's `parses_selector_with_shard`.
func TestParsesSelectorWithShard(t *testing.T) {
	stream, shard := parseSelector("orders#shardId-000000000001", "orders")
	require.Equal(t, "orders", stream)
	require.Equal(t, "shardId-000000000001", shard)
}

// TestParsesDefaultSelector mirrors Rust's `parses_default_selector`.
func TestParsesDefaultSelector(t *testing.T) {
	stream, shard := parseSelector("orders", "orders")
	require.Equal(t, "", stream)
	require.Equal(t, "", shard)
}

// TestIteratorTypeDefaultsToTrimHorizon mirrors the Rust test of the
// same name.
func TestIteratorTypeDefaultsToTrimHorizon(t *testing.T) {
	require.Equal(t, "TRIM_HORIZON", iteratorTypeName(&kinesisConfig{}))
	require.Equal(t, "LATEST", iteratorTypeName(&kinesisConfig{IteratorType: "LATEST"}))
	require.Equal(t, "AT_SEQUENCE_NUMBER", iteratorTypeName(&kinesisConfig{IteratorType: "at_sequence_number"}))
	require.Equal(t, "AFTER_SEQUENCE_NUMBER", iteratorTypeName(&kinesisConfig{IteratorType: "AFTER_SEQUENCE_NUMBER"}))
	require.Equal(t, "AT_TIMESTAMP", iteratorTypeName(&kinesisConfig{IteratorType: "at_timestamp"}))
}

// TestMaxRecordsClampsToRange mirrors Rust's `max_records_clamps_to_range`.
func TestMaxRecordsClampsToRange(t *testing.T) {
	zero := int64(0)
	require.Equal(t, int64(1), maxRecords(&kinesisConfig{MaxRecords: &zero}))
	huge := int64(1_000_000)
	require.Equal(t, int64(50_000), maxRecords(&kinesisConfig{MaxRecords: &huge}))
	mid := int64(250)
	require.Equal(t, int64(250), maxRecords(&kinesisConfig{MaxRecords: &mid}))
}

func TestMaxIterationsClamps(t *testing.T) {
	zero := int64(0)
	require.Equal(t, int64(1), maxIterations(&kinesisConfig{MaxIterations: &zero}))
	huge := int64(1_000_000)
	require.Equal(t, int64(1_000), maxIterations(&kinesisConfig{MaxIterations: &huge}))
	require.Equal(t, defaultMaxIterations, maxIterations(&kinesisConfig{}))
}

func TestSelectorParseWithDifferentDefault(t *testing.T) {
	stream, shard := parseSelector("other#sh1", "orders")
	require.Equal(t, "other", stream)
	require.Equal(t, "sh1", shard)

	stream, shard = parseSelector("other", "orders")
	require.Equal(t, "other", stream)
	require.Equal(t, "", shard)
}

// TestDiscoverSourcesAgainstFakeKinesis points the adapter at an
// httptest server that mimics the Kinesis Data API JSON-RPC envelope
// and asserts that the aggregate stream selector + per-shard
// selectors round-trip with the metadata Rust produces.
func TestDiscoverSourcesAgainstFakeKinesis(t *testing.T) {
	srv := newFakeKinesis(t, fakeStream{
		shards: []detailedShard{
			{ShardID: "shard-1", SequenceNumberRange: shardSequenceNumberRng{StartingSequenceNumber: "100", EndingSequenceNumber: "200"}},
			{ShardID: "shard-2", SequenceNumberRange: shardSequenceNumberRng{StartingSequenceNumber: "201"}},
		},
	})
	defer srv.Close()

	adapter := New()
	adapter.SetHTTPClient(srv.Client())
	adapter.SetEndpoint(srv.URL)
	adapter.SetNowFunc(func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) })

	conn := &models.Connection{Name: "kinesis-test", Config: json.RawMessage(`{
		"stream_name": "orders",
		"region": "us-east-1",
		"access_key_id": "AKIA",
		"secret_access_key": "SECRET"
	}`)}

	sources, err := adapter.DiscoverSources(context.Background(), conn, "")
	require.NoError(t, err)
	require.Len(t, sources, 3)

	require.Equal(t, "orders", sources[0].Selector)
	require.Equal(t, "kinesis_stream", sources[0].SourceKind)
	require.Equal(t, "kinesis://orders", sources[0].DisplayName)

	require.Equal(t, "orders#shard-1", sources[1].Selector)
	require.Equal(t, "kinesis_shard", sources[1].SourceKind)
	require.Equal(t, "kinesis://orders/shard/shard-1", sources[1].DisplayName)

	require.Equal(t, "orders#shard-2", sources[2].Selector)
}

func TestQueryVirtualTableAgainstFakeKinesis(t *testing.T) {
	srv := newFakeKinesis(t, fakeStream{
		shards: []detailedShard{{ShardID: "shard-1"}},
		records: map[string][]kinesisRecord{
			"shard-1": {
				{SequenceNumber: "1001", PartitionKey: "pk", Data: "aGVsbG8=", ApproximateArrivalTimestamp: 1_700_000_000.0},
			},
		},
	})
	defer srv.Close()

	adapter := New()
	adapter.SetHTTPClient(srv.Client())
	adapter.SetEndpoint(srv.URL)

	conn := &models.Connection{Config: json.RawMessage(`{"stream_name":"orders"}`)}
	limit := 5
	q := &adapters.Query{Selector: "orders", Limit: &limit}
	res, err := adapter.QueryVirtualTable(context.Background(), conn, q, "")
	require.NoError(t, err)
	require.Equal(t, "orders", res.Selector)
	require.Equal(t, "zero_copy", res.Mode)
	require.Equal(t, 1, res.RowCount)
	require.Len(t, res.Rows, 1)
}

func TestStreamArrowReturnsSingleFrame(t *testing.T) {
	srv := newFakeKinesis(t, fakeStream{
		shards: []detailedShard{{ShardID: "shard-a"}},
		records: map[string][]kinesisRecord{
			"shard-a": {
				{SequenceNumber: "1", PartitionKey: "pk", Data: "AAA="},
			},
		},
	})
	defer srv.Close()

	adapter := New()
	adapter.SetHTTPClient(srv.Client())
	adapter.SetEndpoint(srv.URL)

	conn := &models.Connection{Config: json.RawMessage(`{"stream_name":"orders"}`)}
	stream, err := adapter.StreamArrow(context.Background(), conn, &adapters.Query{Selector: "orders"}, "")
	require.NoError(t, err)
	defer stream.Close()

	chunk, err := stream.Next(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, chunk)

	_, err = stream.Next(context.Background())
	require.ErrorIs(t, err, io.EOF)
}

func TestBuildIngestSpec(t *testing.T) {
	a := New()
	conn := &models.Connection{Name: "k1", Config: json.RawMessage(`{"stream_name":"orders","region":"us-east-1"}`)}
	src := &adapters.Source{Selector: "orders#shard-1"}
	spec, err := a.BuildIngestSpec(context.Background(), conn, src)
	require.NoError(t, err)
	require.Equal(t, "k1", spec.Name)
	require.Equal(t, "kinesis", spec.Source)
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(spec.Config, &cfg))
	require.Equal(t, "orders", cfg["stream_name"])
	require.Equal(t, "shard-1", cfg["shard_id"])
	require.Equal(t, "us-east-1", cfg["region"])
}

func TestSigV4SignsAuthorizationHeader(t *testing.T) {
	var captured *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Clone(context.Background())
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_, _ = w.Write([]byte(`{"Shards":[],"NextToken":""}`))
	}))
	defer srv.Close()

	adapter := New()
	adapter.SetHTTPClient(srv.Client())
	adapter.SetEndpoint(srv.URL)
	adapter.SetNowFunc(func() time.Time { return time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC) })

	conn := &models.Connection{Config: json.RawMessage(`{
		"stream_name":"orders",
		"region":"us-east-1",
		"access_key_id":"AKIA",
		"secret_access_key":"SECRET"
	}`)}
	_, err := adapter.DiscoverSources(context.Background(), conn, "")
	require.NoError(t, err)
	require.NotNil(t, captured)
	require.Contains(t, captured.Header.Get("Authorization"), "AWS4-HMAC-SHA256")
	require.Contains(t, captured.Header.Get("Authorization"), "Credential=AKIA/")
	require.Equal(t, "20260508T120000Z", captured.Header.Get("X-Amz-Date"))
	require.Equal(t, "Kinesis_20131202.ListShards", captured.Header.Get("X-Amz-Target"))
}

// fakeStream is the in-memory state of the fake Kinesis server used
// by the table-driven tests above.
type fakeStream struct {
	shards  []detailedShard
	records map[string][]kinesisRecord
}

// newFakeKinesis returns an httptest server that responds to the
// Kinesis Data API JSON-RPC envelope with the provided fixture state.
func newFakeKinesis(t *testing.T, state fakeStream) *httptest.Server {
	t.Helper()
	iterators := map[string]string{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		target := r.Header.Get("X-Amz-Target")
		body, _ := io.ReadAll(r.Body)
		switch target {
		case "Kinesis_20131202.ListShards":
			resp := listShardsResponse{Shards: state.shards}
			w.Header().Set("Content-Type", "application/x-amz-json-1.1")
			_ = json.NewEncoder(w).Encode(resp)
		case "Kinesis_20131202.GetShardIterator":
			var req struct {
				ShardID string `json:"ShardId"`
			}
			require.NoError(t, json.Unmarshal(body, &req))
			iter := "iter-" + req.ShardID
			iterators[iter] = req.ShardID
			_ = json.NewEncoder(w).Encode(getShardIteratorResponse{ShardIterator: iter})
		case "Kinesis_20131202.GetRecords":
			var req struct {
				ShardIterator string `json:"ShardIterator"`
			}
			require.NoError(t, json.Unmarshal(body, &req))
			shardID := iterators[req.ShardIterator]
			records := state.records[shardID]
			_ = json.NewEncoder(w).Encode(getRecordsResponse{Records: records})
		default:
			http.Error(w, "unexpected target "+target, http.StatusBadRequest)
		}
	}))
}
