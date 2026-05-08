// Amazon Kinesis streaming-source connector (Bloque P5).
//
// The connector talks to a Kinesis-compatible HTTP endpoint via the
// [KinesisClient] abstraction. Production wires [HttpKinesisClient]
// which posts to `kinesis.<region>.amazonaws.com` with SigV4-equivalent
// auth supplied by the caller; tests use [StaticKinesisClient] to feed
// canned shard records and assert checkpoint progression.

package connectors

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// KinesisConfig is the operator-facing config persisted in
// `streaming_streams.source_binding.config` for Kinesis sources.
type KinesisConfig struct {
	StreamName         string `json:"stream_name"`
	Region             string `json:"region"`
	ShardIteratorType  string `json:"shard_iterator_type"`
	MaxRecordsPerShard uint32 `json:"max_records_per_shard"`
}

func (c *KinesisConfig) UnmarshalJSON(b []byte) error {
	type raw struct {
		StreamName         string  `json:"stream_name"`
		Region             string  `json:"region"`
		ShardIteratorType  *string `json:"shard_iterator_type"`
		MaxRecordsPerShard *uint32 `json:"max_records_per_shard"`
	}
	var r raw
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	c.StreamName = r.StreamName
	c.Region = r.Region
	if r.ShardIteratorType != nil {
		c.ShardIteratorType = *r.ShardIteratorType
	} else {
		c.ShardIteratorType = "LATEST"
	}
	if r.MaxRecordsPerShard != nil {
		c.MaxRecordsPerShard = *r.MaxRecordsPerShard
	} else {
		c.MaxRecordsPerShard = 100
	}
	return nil
}

// KinesisGetRecordsResponse mirrors the Rust struct of the same name.
type KinesisGetRecordsResponse struct {
	Records            []KinesisRecord
	NextShardIterator  *string
	MillisBehindLatest int64
}

// KinesisRecord mirrors the Rust struct of the same name.
type KinesisRecord struct {
	SequenceNumber              string
	PartitionKey                string
	Data                        []byte
	ApproximateArrivalTimestamp time.Time
}

// KinesisClient is the pluggable HTTP client the connector calls.
// Mirrors the Rust `KinesisClient` async trait.
type KinesisClient interface {
	GetRecords(ctx context.Context, shardIterator string, limit uint32) (KinesisGetRecordsResponse, error)
	GetShardIterator(ctx context.Context, shardID, startingPosition string, startingSequenceNumber *string) (string, error)
}

// HttpDoer matches *http.Client.Do so tests can inject a fake transport.
type HttpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// HttpKinesisClient is the production HTTP-backed client.
type HttpKinesisClient struct {
	Endpoint   string
	HTTP       HttpDoer
	AuthHeader string
}

func (c *HttpKinesisClient) GetRecords(ctx context.Context, shardIterator string, limit uint32) (KinesisGetRecordsResponse, error) {
	body, err := json.Marshal(map[string]any{
		"ShardIterator": shardIterator,
		"Limit":         limit,
	})
	if err != nil {
		return KinesisGetRecordsResponse{}, newConnectorError(ConnectorErrorTransport, err.Error(), err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.Endpoint, "/")+"/?Action=GetRecords",
		bytes.NewReader(body))
	if err != nil {
		return KinesisGetRecordsResponse{}, newConnectorError(ConnectorErrorTransport, err.Error(), err)
	}
	req.Header.Set("X-Amz-Target", "Kinesis_20131202.GetRecords")
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	if c.AuthHeader != "" {
		req.Header.Set("Authorization", c.AuthHeader)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return KinesisGetRecordsResponse{}, newConnectorError(ConnectorErrorTransport, err.Error(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return KinesisGetRecordsResponse{}, newConnectorError(ConnectorErrorTransport,
			fmt.Sprintf("kinesis GetRecords status %d", resp.StatusCode), nil)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return KinesisGetRecordsResponse{}, newConnectorError(ConnectorErrorDecode, err.Error(), err)
	}
	var decoded struct {
		Records            []rawKinesisRecord `json:"Records"`
		NextShardIterator  *string            `json:"NextShardIterator"`
		MillisBehindLatest int64              `json:"MillisBehindLatest"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return KinesisGetRecordsResponse{}, newConnectorError(ConnectorErrorDecode, err.Error(), err)
	}
	records := make([]KinesisRecord, 0, len(decoded.Records))
	for _, r := range decoded.Records {
		ts := time.Unix(int64(r.ApproximateArrivalTimestamp), 0).UTC()
		if r.ApproximateArrivalTimestamp == 0 {
			ts = time.Now().UTC()
		}
		records = append(records, KinesisRecord{
			SequenceNumber:              r.SequenceNumber,
			PartitionKey:                r.PartitionKey,
			Data:                        kinesisBase64Decode(r.Data),
			ApproximateArrivalTimestamp: ts,
		})
	}
	return KinesisGetRecordsResponse{
		Records:            records,
		NextShardIterator:  decoded.NextShardIterator,
		MillisBehindLatest: decoded.MillisBehindLatest,
	}, nil
}

func (c *HttpKinesisClient) GetShardIterator(ctx context.Context, shardID, startingPosition string, startingSequenceNumber *string) (string, error) {
	payload := map[string]any{
		"ShardId":           shardID,
		"ShardIteratorType": startingPosition,
	}
	if startingSequenceNumber != nil {
		payload["StartingSequenceNumber"] = *startingSequenceNumber
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", newConnectorError(ConnectorErrorTransport, err.Error(), err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.Endpoint, "/")+"/?Action=GetShardIterator",
		bytes.NewReader(body))
	if err != nil {
		return "", newConnectorError(ConnectorErrorTransport, err.Error(), err)
	}
	req.Header.Set("X-Amz-Target", "Kinesis_20131202.GetShardIterator")
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	if c.AuthHeader != "" {
		req.Header.Set("Authorization", c.AuthHeader)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", newConnectorError(ConnectorErrorTransport, err.Error(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", newConnectorError(ConnectorErrorTransport,
			fmt.Sprintf("kinesis GetShardIterator status %d", resp.StatusCode), nil)
	}
	var decoded struct {
		ShardIterator string `json:"ShardIterator"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", newConnectorError(ConnectorErrorDecode, err.Error(), err)
	}
	return decoded.ShardIterator, nil
}

type rawKinesisRecord struct {
	SequenceNumber              string  `json:"SequenceNumber"`
	PartitionKey                string  `json:"PartitionKey"`
	Data                        string  `json:"Data"`
	ApproximateArrivalTimestamp float64 `json:"ApproximateArrivalTimestamp"`
}

func kinesisBase64Decode(s string) []byte {
	out, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	return out
}

// StaticKinesisClient is the in-memory test client.
type StaticKinesisClient struct {
	mu     sync.Mutex
	queued []KinesisRecord
}

func NewStaticKinesisClient() *StaticKinesisClient { return &StaticKinesisClient{} }

func (c *StaticKinesisClient) Enqueue(records ...KinesisRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queued = append(c.queued, records...)
}

func (c *StaticKinesisClient) GetRecords(_ context.Context, _ string, limit uint32) (KinesisGetRecordsResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	take := int(limit)
	if take > len(c.queued) {
		take = len(c.queued)
	}
	records := append([]KinesisRecord(nil), c.queued[:take]...)
	c.queued = append([]KinesisRecord(nil), c.queued[take:]...)
	next := "static-shard-iterator"
	return KinesisGetRecordsResponse{Records: records, NextShardIterator: &next}, nil
}

func (c *StaticKinesisClient) GetShardIterator(_ context.Context, _, _ string, _ *string) (string, error) {
	return "static-shard-iterator", nil
}

// KinesisConnector implements StreamingSourceConnector against a KinesisClient.
type KinesisConnector struct {
	Config  KinesisConfig
	Client  KinesisClient
	ShardID string

	mu              sync.Mutex
	iterator        *string
	lastPull        *time.Time
	checkpointStore *ConnectorCheckpoint
}

func NewKinesisConnector(config KinesisConfig, client KinesisClient, shardID string) *KinesisConnector {
	return &KinesisConnector{Config: config, Client: client, ShardID: shardID}
}

func (c *KinesisConnector) Kind() string { return "kinesis" }

func (c *KinesisConnector) Pull(ctx context.Context, opts PullOptions) ([]SourceRecord, error) {
	iterator, err := c.currentIteratorOrFetch(ctx)
	if err != nil {
		return nil, err
	}
	limit := opts.BatchSize
	if c.Config.MaxRecordsPerShard < limit {
		limit = c.Config.MaxRecordsPerShard
	}
	resp, err := c.Client.GetRecords(ctx, iterator, limit)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.iterator = resp.NextShardIterator
	now := time.Now().UTC()
	c.lastPull = &now
	c.mu.Unlock()
	if len(resp.Records) == 0 {
		return nil, &ConnectorError{Kind: ConnectorErrorEmpty}
	}
	out := make([]SourceRecord, 0, len(resp.Records))
	for _, r := range resp.Records {
		var payload json.RawMessage
		if json.Valid(r.Data) {
			payload = append(json.RawMessage(nil), r.Data...)
		} else {
			rawObj, _ := json.Marshal(map[string]string{"raw": base64.StdEncoding.EncodeToString(r.Data)})
			payload = rawObj
		}
		metadata, _ := json.Marshal(map[string]any{
			"shard_id":        c.ShardID,
			"sequence_number": r.SequenceNumber,
		})
		key := r.PartitionKey
		out = append(out, SourceRecord{
			SourceID:     r.SequenceNumber,
			PartitionKey: &key,
			Payload:      payload,
			EventTime:    r.ApproximateArrivalTimestamp,
			Metadata:     metadata,
		})
	}
	return out, nil
}

func (c *KinesisConnector) currentIteratorOrFetch(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.iterator != nil {
		it := *c.iterator
		c.mu.Unlock()
		return it, nil
	}
	c.mu.Unlock()
	it, err := c.Client.GetShardIterator(ctx, c.ShardID, c.Config.ShardIteratorType, nil)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.iterator = &it
	c.mu.Unlock()
	return it, nil
}

func (c *KinesisConnector) Checkpoint(_ context.Context, cp ConnectorCheckpoint) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cpCopy := cp
	c.checkpointStore = &cpCopy
	return nil
}

func (c *KinesisConnector) Ack(_ context.Context, _ SourceRecord) error { return nil }

func (c *KinesisConnector) Health(_ context.Context) ConnectorHealth {
	c.mu.Lock()
	defer c.mu.Unlock()
	return ConnectorHealth{Status: ConnectorStatusHealthy, LastPullAt: c.lastPull}
}

// KinesisCatalogEntry mirrors the Rust `catalog_entry` helper.
func KinesisCatalogEntry(stream *StreamDefinition) ConnectorCatalogEntry {
	details, _ := json.Marshal(map[string]any{
		"format": stream.SourceBinding.Format,
		"doc":    "Amazon Kinesis source — see Streaming.md",
	})
	return ConnectorCatalogEntry{
		ConnectorType:       "kinesis",
		Direction:           "source",
		Endpoint:            stream.SourceBinding.Endpoint,
		Status:              "healthy",
		Backlog:             0,
		ThroughputPerSecond: 0,
		Details:             details,
	}
}

var _ StreamingSourceConnector = (*KinesisConnector)(nil)
