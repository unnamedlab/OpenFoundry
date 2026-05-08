// Package kinesis is the Go port of the Rust Kinesis connector that
// lives in `services/connector-management-service/src/connectors/kinesis.rs`.
//
// Capabilities mirrored from the Rust module:
//
//   - DiscoverSources    — enumerate stream + shards via DescribeStreamSummary
//     and ListShards. The aggregate `kinesis_stream` selector is emitted
//     first, followed by one `kinesis_shard` selector per shard.
//   - QueryVirtualTable  — bounded GetRecords preview (TRIM_HORIZON by
//     default) returning JSON rows clamped to [1, 500].
//   - StreamArrow        — paginated GetRecords loop materialised as a
//     single Arrow IPC frame so dataset-versioning-service can ingest it.
//   - BuildIngestSpec    — placeholder spec the bridge forwards to
//     ingestion-replication-service; the selector is the stream/shard.
//
// HTTP transport. Unlike the Rust implementation, which uses
// `aws-sdk-kinesis`, this port talks to the Kinesis Data API directly
// over POST /  with the JSON1.1 RPC envelope the AWS service exposes
// (`X-Amz-Target: Kinesis_20131202.<Action>`). The adapter signs each
// request with AWS Signature V4 from sigv4.go. The repo deliberately
// avoids `aws-sdk-go-v2` (see libs/search-abstraction/opensearch's
// rationale) so we keep the same lightweight pattern here.
//
// Auth surface mirrors Rust: optional `region`, optional static
// `access_key_id` / `secret_access_key` (with optional `session_token`),
// optional `endpoint` override for local-stack / FIPS endpoints.
package kinesis

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

const (
	// ConnectorType is the `connections.connector_type` value the registry
	// binds this adapter under. Mirrors the Rust `CONNECTOR_NAME` constant.
	ConnectorType = "kinesis"

	sourceKindStream = "kinesis_stream"
	sourceKindShard  = "kinesis_shard"

	defaultMaxRecords    = int64(1_000)
	defaultMaxIterations = int64(25)

	maxRecordsClampLow      = int64(1)
	maxRecordsClampHigh     = int64(50_000)
	maxIterationsClampLow   = int64(1)
	maxIterationsClampHigh  = int64(1_000)
	getRecordsLimitLow      = int64(1)
	getRecordsLimitHigh     = int64(10_000)
	previewIterationsPerRun = 3
	defaultPreviewLimit     = 50

	awsService              = "kinesis"
	awsAPIVersion           = "Kinesis_20131202"
	defaultRegion           = "us-east-1"
	defaultIteratorTypeName = "TRIM_HORIZON"
)

// Adapter implements [adapters.ConnectorAdapter] for AWS Kinesis Data
// Streams. It is safe for concurrent use; the embedded HTTP client is
// reused across requests.
type Adapter struct {
	httpClient *http.Client
	endpoint   string
	now        func() time.Time
}

// New returns a ready-to-use [Adapter] backed by [http.DefaultClient]
// and the canonical AWS endpoint format.
func New() *Adapter {
	return &Adapter{httpClient: http.DefaultClient, now: time.Now}
}

// Factory returns an [adapters.Factory] that constructs fresh Kinesis
// adapters; the registry stores the factory and asks for an instance
// per request so per-connection state stays scoped to the constructed
// value.
func Factory() adapters.Factory {
	return adapters.FactoryFunc(func() adapters.ConnectorAdapter { return New() })
}

// SetHTTPClient overrides the embedded [http.Client]. Intended for
// tests pointing at an httptest fake-AWS server.
func (a *Adapter) SetHTTPClient(client *http.Client) {
	if client != nil {
		a.httpClient = client
	}
}

// SetEndpoint overrides the resolved AWS endpoint URL. Intended for
// tests; production callers leave this empty so the adapter resolves
// `https://kinesis.<region>.amazonaws.com`.
func (a *Adapter) SetEndpoint(endpoint string) {
	a.endpoint = strings.TrimRight(endpoint, "/")
}

// SetNowFunc overrides the wallclock used by the sigv4 signer. Tests
// supply a fixed clock so the signature is deterministic.
func (a *Adapter) SetNowFunc(fn func() time.Time) {
	if fn != nil {
		a.now = fn
	}
}

type kinesisConfig struct {
	StreamName             string `json:"stream_name"`
	Region                 string `json:"region"`
	AccessKeyID            string `json:"access_key_id"`
	SecretAccessKey        string `json:"secret_access_key"`
	SessionToken           string `json:"session_token"`
	Endpoint               string `json:"endpoint"`
	MaxRecords             *int64 `json:"max_records"`
	MaxIterations          *int64 `json:"max_iterations"`
	IteratorType           string `json:"iterator_type"`
	StartingSequenceNumber string `json:"starting_sequence_number"`
}

func parseConfig(raw json.RawMessage) (*kinesisConfig, error) {
	cfg := &kinesisConfig{}
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("kinesis: invalid config: %w", err)
	}
	return cfg, nil
}

// ValidateConfig mirrors Rust's `validate_config`: the connection must
// carry a non-empty `stream_name`.
func ValidateConfig(raw json.RawMessage) error {
	cfg, err := parseConfig(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.StreamName) == "" {
		return errors.New("kinesis connector requires 'stream_name'")
	}
	return nil
}

// DiscoverSources mirrors Rust's `discover_sources`: the aggregate
// `kinesis_stream` selector is emitted first, then one `kinesis_shard`
// selector per shard returned by ListShards (paginated).
func (a *Adapter) DiscoverSources(ctx context.Context, c *models.Connection, _ string) ([]adapters.Source, error) {
	cfg, err := a.cfg(c)
	if err != nil {
		return nil, err
	}
	streamMeta, _ := json.Marshal(map[string]any{
		"stream_name": cfg.StreamName,
		"scope":       "stream",
	})
	out := []adapters.Source{
		{
			Selector:         cfg.StreamName,
			DisplayName:      "kinesis://" + cfg.StreamName,
			SourceKind:       sourceKindStream,
			SupportsSync:     true,
			SupportsZeroCopy: true,
			Metadata:         streamMeta,
		},
	}
	shards, err := a.listShardsDetailed(ctx, cfg)
	if err != nil {
		return nil, err
	}
	for _, shard := range shards {
		meta := map[string]any{
			"stream_name": cfg.StreamName,
			"shard_id":    shard.ShardID,
		}
		if shard.SequenceNumberRange.StartingSequenceNumber != "" {
			meta["starting_sequence"] = shard.SequenceNumberRange.StartingSequenceNumber
		} else {
			meta["starting_sequence"] = nil
		}
		if shard.SequenceNumberRange.EndingSequenceNumber != "" {
			meta["ending_sequence"] = shard.SequenceNumberRange.EndingSequenceNumber
		} else {
			meta["ending_sequence"] = nil
		}
		buf, _ := json.Marshal(meta)
		out = append(out, adapters.Source{
			Selector:         cfg.StreamName + "#" + shard.ShardID,
			DisplayName:      "kinesis://" + cfg.StreamName + "/shard/" + shard.ShardID,
			SourceKind:       sourceKindShard,
			SupportsSync:     true,
			SupportsZeroCopy: true,
			Metadata:         buf,
		})
	}
	return out, nil
}

// QueryVirtualTable mirrors Rust's `query_virtual_table` →
// `preview_rows` round-trip: bounded `GetRecords` runs over the
// matching shards with the limit clamped to [1, 500] and only a small
// number of iterations per shard so the preview returns quickly.
func (a *Adapter) QueryVirtualTable(ctx context.Context, c *models.Connection, q *adapters.Query, _ string) (*adapters.Result, error) {
	if q == nil {
		return nil, errors.New("kinesis: query request is nil")
	}
	cfg, err := a.cfg(c)
	if err != nil {
		return nil, err
	}
	limit := defaultPreviewLimit
	if q.Limit != nil {
		limit = *q.Limit
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}

	rows, scanned, iterations, err := a.fetchRows(ctx, cfg, q.Selector, int64(limit), previewIterationsPerRun, true)
	if err != nil {
		return nil, err
	}
	rawRows := make([]json.RawMessage, 0, len(rows))
	for _, row := range rows {
		buf, err := json.Marshal(row)
		if err != nil {
			return nil, fmt.Errorf("kinesis: marshal preview row: %w", err)
		}
		rawRows = append(rawRows, buf)
	}
	targetStream, shardFilter := parseSelector(q.Selector, cfg.StreamName)
	stream := cfg.StreamName
	if targetStream != "" {
		stream = targetStream
	}
	meta, _ := json.Marshal(map[string]any{
		"selector":       q.Selector,
		"stream_name":    stream,
		"shard_filter":   shardFilter,
		"shards_scanned": scanned,
		"iterations":     iterations,
		"iterator_type":  iteratorTypeName(cfg),
		"max_records":    int64(limit),
	})
	return &adapters.Result{
		Selector: q.Selector,
		Mode:     "zero_copy",
		Columns: []string{
			"shard_id",
			"sequence_number",
			"partition_key",
			"data_base64",
		},
		RowCount: len(rawRows),
		Rows:     rawRows,
		Metadata: meta,
	}, nil
}

// StreamArrow runs the full `fetch_dataset` loop bounded by
// `max_records` / `max_iterations` and packages the result as a single
// Arrow IPC frame for dataset-versioning-service to ingest as a new
// version. Mirrors Rust's `fetch_dataset` → `arrow_payload_from_rows`.
func (a *Adapter) StreamArrow(ctx context.Context, c *models.Connection, q *adapters.Query, _ string) (adapters.ArrowStream, error) {
	if q == nil {
		return nil, errors.New("kinesis: query request is nil")
	}
	cfg, err := a.cfg(c)
	if err != nil {
		return nil, err
	}
	maxRec := maxRecords(cfg)
	maxIter := maxIterations(cfg)
	rows, _, _, err := a.fetchRows(ctx, cfg, q.Selector, maxRec, maxIter, false)
	if err != nil {
		return nil, err
	}
	columns := []string{
		"stream",
		"shard_id",
		"sequence_number",
		"partition_key",
		"approximate_arrival_timestamp",
		"data_base64",
	}
	frame, err := materializeArrowStream(columns, rows)
	if err != nil {
		return nil, err
	}
	return &singleFrameStream{frame: frame}, nil
}

// BuildIngestSpec emits the `kinesis` source variant the bridge
// forwards to ingestion-replication-service. The selector becomes the
// `stream_name` (or `stream#shard_id`); credentials are passed through
// verbatim so the bridge can re-use them when invoking the source.
func (a *Adapter) BuildIngestSpec(_ context.Context, c *models.Connection, src *adapters.Source) (*adapters.IngestSpec, error) {
	if c == nil {
		return nil, errors.New("kinesis: connection is nil")
	}
	if src == nil {
		return nil, errors.New("kinesis: source is nil")
	}
	cfg, err := parseConfig(c.Config)
	if err != nil {
		return nil, err
	}
	stream, shard := parseSelector(src.Selector, cfg.StreamName)
	if stream == "" {
		stream = cfg.StreamName
	}
	if stream == "" {
		return nil, errors.New("kinesis: connection config missing 'stream_name'")
	}
	specCfg := map[string]any{
		"stream_name": stream,
	}
	if shard != "" {
		specCfg["shard_id"] = shard
	}
	if cfg.Region != "" {
		specCfg["region"] = cfg.Region
	}
	if name := iteratorTypeName(cfg); name != "" {
		specCfg["iterator_type"] = name
	}
	raw, err := json.Marshal(specCfg)
	if err != nil {
		return nil, fmt.Errorf("kinesis: marshal ingest spec: %w", err)
	}
	return &adapters.IngestSpec{
		Name:      c.Name,
		Namespace: "default",
		Source:    ConnectorType,
		Config:    raw,
	}, nil
}

func (a *Adapter) cfg(c *models.Connection) (*kinesisConfig, error) {
	if c == nil {
		return nil, errors.New("kinesis: connection is nil")
	}
	cfg, err := parseConfig(c.Config)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.StreamName) == "" {
		return nil, errors.New("kinesis connector requires 'stream_name'")
	}
	return cfg, nil
}

// fetchRows runs the GetRecords loop over the matching shards and
// returns rows matching either the full sync schema (preview=false) or
// the trimmed preview schema (preview=true), plus the number of shards
// drained and the total iterations used. Mirrors the dual-purpose Rust
// `fetch_dataset` / `preview_rows` helpers.
func (a *Adapter) fetchRows(ctx context.Context, cfg *kinesisConfig, selector string, limit, maxIter int64, preview bool) ([]map[string]any, int, int, error) {
	targetStream, shardFilter := parseSelector(selector, cfg.StreamName)
	stream := cfg.StreamName
	if targetStream != "" {
		stream = targetStream
	}
	shards, err := a.listShardIDs(ctx, cfg, stream)
	if err != nil {
		return nil, 0, 0, err
	}
	if len(shards) == 0 {
		return nil, 0, 0, fmt.Errorf("kinesis stream '%s' has no shards", stream)
	}

	itType := iteratorTypeName(cfg)
	rows := make([]map[string]any, 0, limit)
	shardsDrained := 0
	iterations := 0
	for _, shardID := range shards {
		if shardFilter != "" && shardFilter != shardID {
			continue
		}
		if int64(len(rows)) >= limit {
			break
		}
		shardsDrained++
		iterator, err := a.getShardIterator(ctx, cfg, stream, shardID, itType)
		if err != nil {
			return nil, 0, 0, err
		}
		if iterator == "" {
			continue
		}
		iterForShard := int64(0)
		for iterForShard < maxIter && int64(len(rows)) < limit {
			batchLimit := clamp64(limit-int64(len(rows)), getRecordsLimitLow, getRecordsLimitHigh)
			resp, err := a.getRecords(ctx, cfg, iterator, batchLimit, preview)
			if err != nil {
				return nil, 0, 0, err
			}
			for _, rec := range resp.Records {
				row := map[string]any{
					"shard_id":        shardID,
					"sequence_number": rec.SequenceNumber,
					"partition_key":   rec.PartitionKey,
					"data_base64":     rec.Data,
				}
				if !preview {
					row["stream"] = stream
					row["approximate_arrival_timestamp"] = formatArrivalTimestamp(rec.ApproximateArrivalTimestamp)
				}
				rows = append(rows, row)
				if int64(len(rows)) >= limit {
					break
				}
			}
			iterForShard++
			iterations++
			if resp.NextShardIterator == "" {
				break
			}
			iterator = resp.NextShardIterator
			if len(resp.Records) == 0 {
				break
			}
		}
	}
	return rows, shardsDrained, iterations, nil
}

type detailedShard struct {
	ShardID             string                 `json:"ShardId"`
	SequenceNumberRange shardSequenceNumberRng `json:"SequenceNumberRange"`
}

type shardSequenceNumberRng struct {
	StartingSequenceNumber string `json:"StartingSequenceNumber"`
	EndingSequenceNumber   string `json:"EndingSequenceNumber"`
}

type listShardsResponse struct {
	Shards    []detailedShard `json:"Shards"`
	NextToken string          `json:"NextToken"`
}

type getShardIteratorResponse struct {
	ShardIterator string `json:"ShardIterator"`
}

type kinesisRecord struct {
	SequenceNumber              string  `json:"SequenceNumber"`
	PartitionKey                string  `json:"PartitionKey"`
	Data                        string  `json:"Data"`
	ApproximateArrivalTimestamp float64 `json:"ApproximateArrivalTimestamp"`
}

type getRecordsResponse struct {
	Records           []kinesisRecord `json:"Records"`
	NextShardIterator string          `json:"NextShardIterator"`
}

func (a *Adapter) listShardsDetailed(ctx context.Context, cfg *kinesisConfig) ([]detailedShard, error) {
	out := make([]detailedShard, 0)
	var nextToken string
	for {
		body := map[string]any{}
		if nextToken == "" {
			body["StreamName"] = cfg.StreamName
		} else {
			body["NextToken"] = nextToken
		}
		var resp listShardsResponse
		if err := a.invoke(ctx, cfg, "ListShards", body, &resp); err != nil {
			return nil, fmt.Errorf("kinesis ListShards failed: %w", err)
		}
		out = append(out, resp.Shards...)
		if resp.NextToken == "" {
			break
		}
		nextToken = resp.NextToken
	}
	return out, nil
}

func (a *Adapter) listShardIDs(ctx context.Context, cfg *kinesisConfig, stream string) ([]string, error) {
	out := make([]string, 0)
	var nextToken string
	for {
		body := map[string]any{}
		if nextToken == "" {
			body["StreamName"] = stream
		} else {
			body["NextToken"] = nextToken
		}
		var resp listShardsResponse
		if err := a.invoke(ctx, cfg, "ListShards", body, &resp); err != nil {
			return nil, fmt.Errorf("kinesis ListShards failed: %w", err)
		}
		for _, s := range resp.Shards {
			out = append(out, s.ShardID)
		}
		if resp.NextToken == "" {
			break
		}
		nextToken = resp.NextToken
	}
	return out, nil
}

func (a *Adapter) getShardIterator(ctx context.Context, cfg *kinesisConfig, stream, shardID, iteratorType string) (string, error) {
	body := map[string]any{
		"StreamName":        stream,
		"ShardId":           shardID,
		"ShardIteratorType": iteratorType,
	}
	switch iteratorType {
	case "AT_SEQUENCE_NUMBER", "AFTER_SEQUENCE_NUMBER":
		if cfg.StartingSequenceNumber != "" {
			body["StartingSequenceNumber"] = cfg.StartingSequenceNumber
		}
	}
	var resp getShardIteratorResponse
	if err := a.invoke(ctx, cfg, "GetShardIterator", body, &resp); err != nil {
		return "", fmt.Errorf("kinesis GetShardIterator failed: %w", err)
	}
	return resp.ShardIterator, nil
}

func (a *Adapter) getRecords(ctx context.Context, cfg *kinesisConfig, iterator string, limit int64, preview bool) (*getRecordsResponse, error) {
	body := map[string]any{
		"ShardIterator": iterator,
		"Limit":         limit,
	}
	var resp getRecordsResponse
	if err := a.invoke(ctx, cfg, "GetRecords", body, &resp); err != nil {
		label := "GetRecords"
		if preview {
			label = "GetRecords (preview)"
		}
		return nil, fmt.Errorf("kinesis %s failed: %w", label, err)
	}
	return &resp, nil
}

func (a *Adapter) invoke(ctx context.Context, cfg *kinesisConfig, action string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", action, err)
	}
	endpoint := a.endpointFor(cfg)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build %s request: %w", action, err)
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", awsAPIVersion+"."+action)

	region := cfg.Region
	if region == "" {
		region = defaultRegion
	}
	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		signRequest(req, payload, cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken, region, awsService, a.now())
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("transport error: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode %s response: %w", action, err)
	}
	return nil
}

func (a *Adapter) endpointFor(cfg *kinesisConfig) string {
	if a.endpoint != "" {
		return a.endpoint + "/"
	}
	if strings.TrimSpace(cfg.Endpoint) != "" {
		return strings.TrimRight(cfg.Endpoint, "/") + "/"
	}
	region := cfg.Region
	if region == "" {
		region = defaultRegion
	}
	return "https://kinesis." + region + ".amazonaws.com/"
}

// EncodeRecordData base64-encodes the record's binary payload exactly
// the way the Rust adapter materialises it. Exposed for tests.
func EncodeRecordData(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// parseSelector mirrors Rust's `parse_selector` — returns (Some(stream),
// Some(shard)) for "stream#shard", (Some(stream), None) for a plain
// stream name that differs from the configured default, and (None, None)
// for the default stream.
func parseSelector(selector, defaultStream string) (string, string) {
	trimmed := strings.TrimSpace(selector)
	if trimmed == "" || trimmed == defaultStream {
		return "", ""
	}
	if i := strings.IndexByte(trimmed, '#'); i >= 0 {
		stream := trimmed[:i]
		shard := trimmed[i+1:]
		return stream, shard
	}
	return trimmed, ""
}

func maxRecords(cfg *kinesisConfig) int64 {
	v := defaultMaxRecords
	if cfg.MaxRecords != nil {
		v = *cfg.MaxRecords
	}
	return clamp64(v, maxRecordsClampLow, maxRecordsClampHigh)
}

func maxIterations(cfg *kinesisConfig) int64 {
	v := defaultMaxIterations
	if cfg.MaxIterations != nil {
		v = *cfg.MaxIterations
	}
	return clamp64(v, maxIterationsClampLow, maxIterationsClampHigh)
}

func iteratorTypeName(cfg *kinesisConfig) string {
	switch strings.ToUpper(strings.TrimSpace(cfg.IteratorType)) {
	case "LATEST":
		return "LATEST"
	case "AT_SEQUENCE_NUMBER":
		return "AT_SEQUENCE_NUMBER"
	case "AFTER_SEQUENCE_NUMBER":
		return "AFTER_SEQUENCE_NUMBER"
	case "AT_TIMESTAMP":
		return "AT_TIMESTAMP"
	default:
		return defaultIteratorTypeName
	}
}

func clamp64(v, low, high int64) int64 {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

func formatArrivalTimestamp(seconds float64) string {
	if seconds == 0 {
		return ""
	}
	return fmt.Sprintf("%v", seconds)
}

func materializeArrowStream(columns []string, rows []map[string]any) ([]byte, error) {
	mem := memory.NewGoAllocator()
	fields := make([]arrow.Field, 0, len(columns))
	arrays := make([]arrow.Array, 0, len(columns))
	for _, name := range columns {
		fields = append(fields, arrow.Field{Name: name, Type: arrow.BinaryTypes.String, Nullable: true})
		builder := array.NewStringBuilder(mem)
		for _, row := range rows {
			value, ok := row[name]
			if !ok || value == nil {
				builder.AppendNull()
				continue
			}
			switch v := value.(type) {
			case string:
				builder.Append(v)
			default:
				builder.Append(fmt.Sprint(v))
			}
		}
		arr := builder.NewArray()
		arrays = append(arrays, arr)
		builder.Release()
	}
	schema := arrow.NewSchema(fields, nil)
	rec := array.NewRecord(schema, arrays, int64(len(rows)))
	defer rec.Release()
	for _, arr := range arrays {
		arr.Release()
	}
	var buf bytes.Buffer
	writer := ipc.NewWriter(&buf, ipc.WithSchema(schema), ipc.WithAllocator(mem))
	if err := writer.Write(rec); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("kinesis: write arrow record: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("kinesis: close arrow stream: %w", err)
	}
	return buf.Bytes(), nil
}

type singleFrameStream struct {
	frame    []byte
	consumed bool
}

func (s *singleFrameStream) Next(_ context.Context) ([]byte, error) {
	if s.consumed {
		return nil, io.EOF
	}
	s.consumed = true
	return s.frame, nil
}

func (s *singleFrameStream) Close() error { return nil }
