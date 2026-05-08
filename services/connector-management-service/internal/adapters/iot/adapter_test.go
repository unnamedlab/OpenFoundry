package iot

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

// TestRequiresBrokerAndTopic mirrors Rust's `requires_broker_and_topic`.
func TestRequiresBrokerAndTopic(t *testing.T) {
	require.Error(t, ValidateConfig(json.RawMessage(`{}`)))
	require.Error(t, ValidateConfig(json.RawMessage(`{"broker_host":"broker"}`)))
	require.NoError(t, ValidateConfig(json.RawMessage(`{"broker_host":"broker","topic":"sensors/#"}`)))
	require.NoError(t, ValidateConfig(json.RawMessage(`{"broker_host":"broker","topics":["a/#","b/+"]}`)))
}

// TestPortDefaultsConsiderTLS mirrors Rust's `port_defaults_consider_tls`.
func TestPortDefaultsConsiderTLS(t *testing.T) {
	require.Equal(t, 1883, resolvedPort(&iotConfig{BrokerHost: "h", Topic: "t"}))
	require.Equal(t, 8883, resolvedPort(&iotConfig{BrokerHost: "h", Topic: "t", TLS: true}))
	v := int64(9001)
	require.Equal(t, 9001, resolvedPort(&iotConfig{BrokerHost: "h", Topic: "t", BrokerPort: &v}))
}

// TestTopicFiltersPreferArray mirrors Rust's `topic_filters_prefer_array`.
func TestTopicFiltersPreferArray(t *testing.T) {
	cfg := &iotConfig{Topic: "ignored", Topics: []string{"a", "b"}}
	require.Equal(t, []string{"a", "b"}, topicFilters(cfg))
}

func TestTopicFiltersFallbackToScalar(t *testing.T) {
	cfg := &iotConfig{Topic: "single"}
	require.Equal(t, []string{"single"}, topicFilters(cfg))
}

// TestQoSLevelsMapCorrectly mirrors Rust's `qos_levels_map_correctly`.
func TestQoSLevelsMapCorrectly(t *testing.T) {
	require.Equal(t, QoSAtMostOnce, subscriptionQoS(&iotConfig{}))
	one := int64(1)
	require.Equal(t, QoSAtLeastOnce, subscriptionQoS(&iotConfig{QoS: &one}))
	two := int64(2)
	require.Equal(t, QoSExactlyOnce, subscriptionQoS(&iotConfig{QoS: &two}))
}

// TestFetchWindowIsClamped mirrors Rust's `fetch_window_is_clamped`.
func TestFetchWindowIsClamped(t *testing.T) {
	zero := int64(0)
	require.Equal(t, int64(100), fetchWindowMs(&iotConfig{MaxDurationMs: &zero}))
	ten := int64(10_000)
	require.Equal(t, int64(10_000), fetchWindowMs(&iotConfig{MaxDurationMs: &ten}))
	huge := int64(2_000_000)
	require.Equal(t, fetchWindowHigh, fetchWindowMs(&iotConfig{MaxDurationMs: &huge}))
}

func TestDiscoveryWindowIsClamped(t *testing.T) {
	zero := int64(0)
	require.Equal(t, discoveryWindowLow, discoveryWindowMs(&iotConfig{DiscoveryWindowMs: &zero}))
	huge := int64(1_000_000)
	require.Equal(t, discoveryWindowHigh, discoveryWindowMs(&iotConfig{DiscoveryWindowMs: &huge}))
	require.Equal(t, defaultDiscoveryWindowMs, discoveryWindowMs(&iotConfig{}))
}

func TestMaxMessagesIsClamped(t *testing.T) {
	zero := int64(0)
	require.Equal(t, maxMessagesLow, maxMessages(&iotConfig{MaxMessages: &zero}))
	huge := int64(10_000_000)
	require.Equal(t, maxMessagesHigh, maxMessages(&iotConfig{MaxMessages: &huge}))
}

func TestKeepAliveAndConnectTimeoutClamps(t *testing.T) {
	zero := int64(0)
	require.Equal(t, keepAliveClampLow, keepAliveSecs(&iotConfig{KeepAliveSecs: &zero}))
	require.Equal(t, connectTimeoutClampLow, connectTimeoutMs(&iotConfig{ConnectTimeoutMs: &zero}))
}

func TestSanitizeFileName(t *testing.T) {
	require.Equal(t, "sensors_floor_1", sanitizeFileName("sensors/floor#1"))
	require.Equal(t, "abc123", sanitizeFileName("abc123"))
	require.Equal(t, "_", sanitizeFileName("/"))
}

// TestStubRunnerSurfacesErrNotImplemented checks that the default
// runner returns the marker error so callers can translate it.
func TestStubRunnerSurfacesErrNotImplemented(t *testing.T) {
	a := New()
	conn := &models.Connection{Config: json.RawMessage(`{"broker_host":"b","topic":"sensors/#"}`)}
	_, err := a.QueryVirtualTable(context.Background(), conn, &adapters.Query{Selector: "sensors/#"}, "")
	require.ErrorIs(t, err, adapters.ErrNotImplemented)

	_, err = a.StreamArrow(context.Background(), conn, &adapters.Query{Selector: "sensors/#"}, "")
	require.ErrorIs(t, err, adapters.ErrNotImplemented)
}

// TestDiscoverSourcesFallsBackToFiltersWhenRunnerEmpty validates that
// when no live messages can be observed the configured filter list is
// returned as-is, exactly as the Rust connector does.
func TestDiscoverSourcesFallsBackToFiltersWhenRunnerEmpty(t *testing.T) {
	a := New()
	conn := &models.Connection{Config: json.RawMessage(`{"broker_host":"b","topics":["a/#","b/+"]}`)}
	sources, err := a.DiscoverSources(context.Background(), conn, "")
	require.NoError(t, err)
	require.Len(t, sources, 2)
	require.Equal(t, "a/#", sources[0].Selector)
	require.Equal(t, "mqtt_topic", sources[0].SourceKind)
	require.Equal(t, "b/+", sources[1].Selector)
	var meta map[string]any
	require.NoError(t, json.Unmarshal(sources[0].Metadata, &meta))
	require.Equal(t, false, meta["observed"])
}

// TestDiscoverSourcesUsesObservedTopicsWhenRunnerProvidesThem checks
// the live-runner path: a fake runner that produces messages drives
// the discovered selector list, deduplicated and ordered by first
// appearance.
func TestDiscoverSourcesUsesObservedTopicsWhenRunnerProvidesThem(t *testing.T) {
	a := New()
	a.SetRunner(fakeRunner{messages: []Message{
		{Topic: "sensors/floor1"},
		{Topic: "sensors/floor2"},
		{Topic: "sensors/floor1"},
	}})
	conn := &models.Connection{Config: json.RawMessage(`{"broker_host":"b","topic":"sensors/#"}`)}
	sources, err := a.DiscoverSources(context.Background(), conn, "")
	require.NoError(t, err)
	require.Len(t, sources, 2)
	require.Equal(t, "sensors/floor1", sources[0].Selector)
	require.Equal(t, "sensors/floor2", sources[1].Selector)
	var meta map[string]any
	require.NoError(t, json.Unmarshal(sources[0].Metadata, &meta))
	require.Equal(t, true, meta["observed"])
}

// TestQueryVirtualTableUsesRunnerMessages checks the live-runner path.
func TestQueryVirtualTableUsesRunnerMessages(t *testing.T) {
	a := New()
	a.SetRunner(fakeRunner{messages: []Message{
		{Topic: "sensors/a", Payload: json.RawMessage(`{"v":1}`), QoS: "AtMostOnce", Retained: false, ReceivedAt: "2026-05-08T00:00:00Z"},
	}})
	conn := &models.Connection{Config: json.RawMessage(`{"broker_host":"b","topic":"sensors/#"}`)}
	limit := 10
	res, err := a.QueryVirtualTable(context.Background(), conn, &adapters.Query{Selector: "sensors/a", Limit: &limit}, "")
	require.NoError(t, err)
	require.Equal(t, "sensors/a", res.Selector)
	require.Equal(t, 1, res.RowCount)
}

// TestQueryVirtualTablePropagatesRunnerError checks that a runner
// returning an error other than ErrNotImplemented is surfaced.
func TestQueryVirtualTablePropagatesRunnerError(t *testing.T) {
	sentinel := errors.New("broker unavailable")
	a := New()
	a.SetRunner(fakeRunner{err: sentinel})
	conn := &models.Connection{Config: json.RawMessage(`{"broker_host":"b","topic":"sensors/#"}`)}
	_, err := a.QueryVirtualTable(context.Background(), conn, &adapters.Query{Selector: "sensors/#"}, "")
	require.ErrorIs(t, err, sentinel)
}

func TestBuildIngestSpec(t *testing.T) {
	a := New()
	conn := &models.Connection{Name: "iot-prod", Config: json.RawMessage(`{"broker_host":"b","topic":"sensors/#","tls":true}`)}
	spec, err := a.BuildIngestSpec(context.Background(), conn, &adapters.Source{Selector: "sensors/floor1"})
	require.NoError(t, err)
	require.Equal(t, "iot-prod", spec.Name)
	require.Equal(t, "iot", spec.Source)
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(spec.Config, &cfg))
	require.Equal(t, "b", cfg["broker_host"])
	require.Equal(t, float64(8883), cfg["broker_port"])
	require.Equal(t, true, cfg["tls"])
	require.Equal(t, "sensors/floor1", cfg["topic"])
}

func TestSelectorFiltersPrefersExplicitSelector(t *testing.T) {
	cfg := &iotConfig{Topics: []string{"a/#", "b/+"}}
	require.Equal(t, []string{"sensors/floor1"}, selectorFilters(cfg, "sensors/floor1"))
	require.Equal(t, []string{"a/#", "b/+"}, selectorFilters(cfg, ""))
}

// fakeRunner is the test seam — feeds canned messages or a sentinel
// error into [Adapter] without needing a live MQTT broker.
type fakeRunner struct {
	messages []Message
	err      error
}

func (f fakeRunner) Drain(_ context.Context, _ runnerOpts) ([]Message, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.messages, nil
}
