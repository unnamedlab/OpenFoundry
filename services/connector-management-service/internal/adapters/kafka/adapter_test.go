package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	controlbus "github.com/openfoundry/openfoundry-go/libs/event-bus-control"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

// TestParsesStringAndObjectTopics is the Go port of Rust's
// `parses_string_and_object_topics` (services/connector-management-service/src/connectors/kafka.rs).
func TestParsesStringAndObjectTopics(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
		"topics": [
			"orders",
			{
				"selector": "payments",
				"display_name": "Payments",
				"partitions": 6,
				"sample_messages": [{ "payment_id": "pay-1" }]
			}
		]
	}`)
	topics, err := controlbus.ParseTopicEntries(raw, connectorLabel)
	if err != nil {
		t.Fatalf("ParseTopicEntries: %v", err)
	}
	if len(topics) != 2 {
		t.Fatalf("len = %d, want 2", len(topics))
	}
	if topics[0].Selector != "orders" {
		t.Fatalf("topics[0].Selector = %q, want %q", topics[0].Selector, "orders")
	}
	if topics[1].DisplayName != "Payments" {
		t.Fatalf("topics[1].DisplayName = %q, want %q", topics[1].DisplayName, "Payments")
	}
	if topics[1].Partitions != 6 {
		t.Fatalf("topics[1].Partitions = %d, want 6", topics[1].Partitions)
	}
}

// TestValidatesRequiredBootstrapServers is the Go port of Rust's
// `validates_required_bootstrap_servers`.
func TestValidatesRequiredBootstrapServers(t *testing.T) {
	t.Parallel()
	err := ValidateConfig(json.RawMessage(`{ "topics": ["orders"] }`))
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "bootstrap_servers") {
		t.Fatalf("error %q does not mention bootstrap_servers", err.Error())
	}
}

// TestFindsConfiguredTopic is the Go port of Rust's
// `finds_configured_topic`.
func TestFindsConfiguredTopic(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
		"bootstrap_servers": "broker-a:9092",
		"topics": [
			{
				"selector": "orders",
				"sample_messages": [{ "order_id": "ord-1" }]
			}
		]
	}`)
	topic, err := controlbus.FindTopicEntry(raw, "orders", connectorLabel)
	if err != nil {
		t.Fatalf("FindTopicEntry: %v", err)
	}
	if topic.Selector != "orders" {
		t.Fatalf("Selector = %q, want %q", topic.Selector, "orders")
	}
	if len(topic.SampleMessages) != 1 {
		t.Fatalf("SampleMessages len = %d, want 1", len(topic.SampleMessages))
	}
	got := strings.Join([]string{string(topic.SampleMessages[0])}, "")
	want := `{"order_id":"ord-1"}`
	if normalizeJSON(t, got) != normalizeJSON(t, want) {
		t.Fatalf("SampleMessages[0] = %q, want %q", got, want)
	}
}

// TestValidateConfigRejectsEmptyTopics covers the second arm of
// `validate_topic_connector_config` — bootstrap present but topics
// empty.
func TestValidateConfigRejectsEmptyTopics(t *testing.T) {
	t.Parallel()
	err := ValidateConfig(json.RawMessage(`{ "bootstrap_servers": "b:9092", "topics": [] }`))
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "at least one topic") {
		t.Fatalf("error %q does not mention 'at least one topic'", err.Error())
	}
}

// TestParseTopicsRoundTripsConfig double-checks the catalog-side
// walk DiscoverSources/QueryVirtualTable rely on. The live branch
// can't run under unit tests (no broker), but the parser drives both
// paths' fall-back format and is exercised here directly.
func TestParseTopicsRoundTripsConfig(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
		"bootstrap_servers": "broker-a:9092",
		"topics": [
			{ "selector": "orders", "display_name": "Orders", "partitions": 4 },
			"payments"
		]
	}`)
	topics, err := controlbus.ParseTopicEntries(raw, connectorLabel)
	if err != nil {
		t.Fatalf("ParseTopicEntries: %v", err)
	}
	if len(topics) != 2 {
		t.Fatalf("len = %d, want 2", len(topics))
	}
	if topics[0].DisplayName != "Orders" {
		t.Fatalf("topics[0].DisplayName = %q, want %q", topics[0].DisplayName, "Orders")
	}
	if topics[1].Selector != "payments" {
		t.Fatalf("topics[1].Selector = %q, want %q", topics[1].Selector, "payments")
	}
}

// TestQueryVirtualTableCatalogModeReturnsSampleRows verifies the
// catalog-backed query path returns the topic's sample_messages with
// `mode: "catalog_backed"` in the metadata. This bypasses the live
// path because Rust's live path is gated behind `kafka-rdkafka` and
// the broker isn't dialled in tests.
func TestQueryVirtualTableCatalogModeReturnsSampleRows(t *testing.T) {
	t.Parallel()
	// We can't easily reach the catalog path without skipping the live
	// branch, but we can validate the catalog formatter directly via
	// the public helper. Build the expected response by hand.
	rows := []json.RawMessage{
		json.RawMessage(`{"order_id":"ord-1","amount":10}`),
		json.RawMessage(`{"order_id":"ord-2","amount":20}`),
	}
	meta := json.RawMessage(`{"mode":"catalog_backed"}`)
	resp := virtualTableResponse("orders", rows, meta)
	if resp.Selector != "orders" {
		t.Fatalf("Selector = %q", resp.Selector)
	}
	if resp.Mode != "zero_copy" {
		t.Fatalf("Mode = %q, want zero_copy", resp.Mode)
	}
	if resp.RowCount != 2 {
		t.Fatalf("RowCount = %d, want 2", resp.RowCount)
	}
	if len(resp.Columns) != 2 || resp.Columns[0] != "order_id" || resp.Columns[1] != "amount" {
		t.Fatalf("Columns = %v, want [order_id amount]", resp.Columns)
	}
	if resp.SourceSignature == nil || !strings.HasPrefix(*resp.SourceSignature, "sha256:") {
		t.Fatalf("SourceSignature = %v", resp.SourceSignature)
	}
}

// TestQueryVirtualTableLimitClamped checks the [1, 500] clamp + the
// 50-default Rust applies in `query_virtual_table`.
func TestQueryVirtualTableLimitClamped(t *testing.T) {
	t.Parallel()
	if got := clampLimit(nil); got != 50 {
		t.Fatalf("nil limit = %d, want 50", got)
	}
	zero := 0
	if got := clampLimit(&zero); got != 1 {
		t.Fatalf("limit=0 = %d, want 1", got)
	}
	overshoot := 1000
	if got := clampLimit(&overshoot); got != 500 {
		t.Fatalf("limit=1000 = %d, want 500", got)
	}
	negative := -5
	if got := clampLimit(&negative); got != 1 {
		t.Fatalf("limit=-5 = %d, want 1", got)
	}
	mid := 75
	if got := clampLimit(&mid); got != 75 {
		t.Fatalf("limit=75 = %d, want 75", got)
	}
}

// TestFetchDatasetReturnsJSONPayload exercises the JSON fetch path:
// rows pass through, file_name uses the sanitised stem, and the
// metadata gains a `source_signature` from the JSON bytes.
func TestFetchDatasetReturnsJSONPayload(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
		"bootstrap_servers": "broker-a:9092",
		"topics": [{
			"selector": "orders.v1",
			"sample_messages": [
				{ "order_id": "ord-1" },
				{ "order_id": "ord-2" }
			],
			"partitions": 3
		}]
	}`)
	payload, err := New().FetchDataset(context.Background(), raw, "orders.v1")
	if err != nil {
		t.Fatalf("FetchDataset: %v", err)
	}
	if payload.Format != "json" {
		t.Fatalf("Format = %q, want json", payload.Format)
	}
	if payload.RowsSynced != 2 {
		t.Fatalf("RowsSynced = %d, want 2", payload.RowsSynced)
	}
	if payload.FileName != "orders_v1.json" {
		t.Fatalf("FileName = %q, want orders_v1.json", payload.FileName)
	}
	var meta map[string]any
	if err := json.Unmarshal(payload.Metadata, &meta); err != nil {
		t.Fatalf("metadata unmarshal: %v", err)
	}
	sig, _ := meta["source_signature"].(string)
	if !strings.HasPrefix(sig, "sha256:") {
		t.Fatalf("source_signature = %q, want sha256: prefix", sig)
	}
	if got, _ := meta["topic"].(string); got != "orders.v1" {
		t.Fatalf("metadata.topic = %q, want orders.v1", got)
	}
	if got, _ := meta["partitions"].(float64); got != 3 {
		t.Fatalf("metadata.partitions = %v, want 3", got)
	}
	// Bytes must round-trip the sample messages array verbatim.
	var rows []map[string]any
	if err := json.Unmarshal(payload.Bytes, &rows); err != nil {
		t.Fatalf("bytes unmarshal: %v", err)
	}
	if len(rows) != 2 || rows[0]["order_id"] != "ord-1" || rows[1]["order_id"] != "ord-2" {
		t.Fatalf("Bytes payload = %v", rows)
	}
}

// TestFetchDatasetUnknownTopicErrors verifies the FindTopicEntry
// failure path bubbles up as the same error message Rust uses.
func TestFetchDatasetUnknownTopicErrors(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
		"bootstrap_servers": "broker-a:9092",
		"topics": ["orders"]
	}`)
	_, err := New().FetchDataset(context.Background(), raw, "missing")
	if err == nil {
		t.Fatalf("expected error for missing topic")
	}
	if !strings.Contains(err.Error(), "kafka connector topic 'missing' is not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestFetchDatasetSanitizesFileName exercises the
// SanitizeFileStem/`kafka_sync` fallback when the selector contains
// no alphanumerics.
func TestFetchDatasetSanitizesFileName(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
		"bootstrap_servers": "b:9092",
		"topics": [{ "selector": "///" , "sample_messages": []}]
	}`)
	payload, err := New().FetchDataset(context.Background(), raw, "///")
	if err != nil {
		t.Fatalf("FetchDataset: %v", err)
	}
	if payload.FileName != "kafka_sync.json" {
		t.Fatalf("FileName = %q, want kafka_sync.json", payload.FileName)
	}
}

// TestStreamArrowAndBuildIngestSpecAreNotImplemented documents the
// 4-capability surface: Kafka has no Arrow streaming or ingest-spec
// build path on the Rust side, so both methods must surface
// adapters.ErrNotImplemented.
func TestStreamArrowAndBuildIngestSpecAreNotImplemented(t *testing.T) {
	t.Parallel()
	a := New()
	conn := &models.Connection{}
	if _, err := a.StreamArrow(context.Background(), conn, &adapters.Query{}, ""); !errors.Is(err, adapters.ErrNotImplemented) {
		t.Fatalf("StreamArrow err = %v, want ErrNotImplemented", err)
	}
	if _, err := a.BuildIngestSpec(context.Background(), conn, &adapters.Source{}); !errors.Is(err, adapters.ErrNotImplemented) {
		t.Fatalf("BuildIngestSpec err = %v, want ErrNotImplemented", err)
	}
}

// TestFactoryReturnsConnectorAdapter is the standard adapter-package
// shape check: Factory().New() must satisfy adapters.ConnectorAdapter.
func TestFactoryReturnsConnectorAdapter(t *testing.T) {
	t.Parallel()
	var _ adapters.ConnectorAdapter = Factory().New()
}

// TestBasicDiscoveredSourceShape verifies the helper renders the
// supports_sync / supports_zero_copy / source_kind fields the same
// way Rust's `basic_discovered_source` does.
func TestBasicDiscoveredSourceShape(t *testing.T) {
	t.Parallel()
	meta := json.RawMessage(`{"topic":"orders"}`)
	got := basicDiscoveredSource("orders", "Orders", "kafka_topic", meta)
	if got.Selector != "orders" || got.DisplayName != "Orders" || got.SourceKind != "kafka_topic" {
		t.Fatalf("got = %+v", got)
	}
	if !got.SupportsSync || !got.SupportsZeroCopy {
		t.Fatalf("supports flags wrong: %+v", got)
	}
	if got.SourceSignature != nil {
		t.Fatalf("SourceSignature should be nil, got %v", got.SourceSignature)
	}
}

// TestVirtualTableResponseEmptyRows checks the empty-input path: row
// count zero, columns empty, but we still get a `sha256:` signature
// (Rust serialises [] → "[]").
func TestVirtualTableResponseEmptyRows(t *testing.T) {
	t.Parallel()
	resp := virtualTableResponse("topic-x", nil, nil)
	if resp.RowCount != 0 {
		t.Fatalf("RowCount = %d, want 0", resp.RowCount)
	}
	if len(resp.Columns) != 0 {
		t.Fatalf("Columns = %v, want empty", resp.Columns)
	}
	if resp.SourceSignature == nil {
		t.Fatalf("SourceSignature must be set even for empty rows")
	}
}

// TestAddSourceSignatureAppendsToMetadata mirrors Rust's
// `add_source_signature` — the function splices a `source_signature`
// field into the payload's metadata object.
func TestAddSourceSignatureAppendsToMetadata(t *testing.T) {
	t.Parallel()
	p := SyncPayload{
		Bytes:    []byte("hello"),
		Format:   "json",
		Metadata: json.RawMessage(`{"topic":"orders"}`),
	}
	addSourceSignature(&p)
	var got map[string]string
	if err := json.Unmarshal(p.Metadata, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got["topic"] != "orders" {
		t.Fatalf("topic preserved? got %q", got["topic"])
	}
	if !strings.HasPrefix(got["source_signature"], "sha256:") {
		t.Fatalf("source_signature = %q", got["source_signature"])
	}
}

// normalizeJSON parses and re-encodes a JSON string so test
// comparisons aren't sensitive to whitespace differences. Failing the
// parse is treated as a fatal test error.
func normalizeJSON(t *testing.T, s string) string {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("normalizeJSON parse %q: %v", s, err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("normalizeJSON marshal: %v", err)
	}
	return string(out)
}
