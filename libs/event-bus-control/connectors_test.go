package controlbus_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	controlbus "github.com/openfoundry/openfoundry-go/libs/event-bus-control"
)

func TestParsesStringAndObjectTopics(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
		"topics": [
			"orders",
			{
				"selector": "payments",
				"display_name": "Payments",
				"partitions": 6,
				"sample_messages": [{"payment_id":"pay-1"}]
			}
		]
	}`)
	topics, err := controlbus.ParseTopicEntries(raw, "kafka connector")
	require.NoError(t, err)
	require.Len(t, topics, 2)
	assert.Equal(t, "orders", topics[0].Selector)
	assert.Equal(t, "Payments", topics[1].DisplayName)
	assert.Equal(t, int64(6), topics[1].Partitions)
}

func TestValidatesRequiredBootstrapServers(t *testing.T) {
	t.Parallel()
	err := controlbus.ValidateTopicConnectorConfig(
		json.RawMessage(`{"topics":["orders"]}`),
		"kafka connector",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bootstrap_servers")
}

func TestFindsConfiguredTopic(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{
		"bootstrap_servers": "broker-a:9092",
		"topics": [
			{"selector":"orders","sample_messages":[{"order_id":"ord-1"}]}
		]
	}`)
	topic, err := controlbus.FindTopicEntry(cfg, "orders", "kafka connector")
	require.NoError(t, err)
	assert.Equal(t, "orders", topic.Selector)
	require.Len(t, topic.SampleMessages, 1)
	assert.JSONEq(t, `{"order_id":"ord-1"}`, string(topic.SampleMessages[0]))

	bs, ok := controlbus.BootstrapServers(json.RawMessage(`{"brokers":"broker-a:9092"}`))
	require.True(t, ok)
	assert.Equal(t, "broker-a:9092", bs)
}

func TestSanitizesFileStems(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "orders_v1", controlbus.SanitizeFileStem("orders.v1", "fallback"))
	assert.Equal(t, "fallback", controlbus.SanitizeFileStem("///", "fallback"))
}

func TestTopicWithInlineAvroSchemaValidatesSamples(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{
		"bootstrap_servers": "broker-a:9092",
		"topics": [{
			"selector": "orders",
			"schema": {
				"type": "avro",
				"text": "{\"type\":\"record\",\"name\":\"Order\",\"fields\":[{\"name\":\"order_id\",\"type\":\"string\"}]}"
			},
			"sample_messages": [{"order_id":"ord-1"}]
		}]
	}`)
	require.NoError(t, controlbus.ValidateTopicConnectorConfig(cfg, "kafka connector"))
}

func TestTopicWithInlineSchemaRejectsInvalidSample(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{
		"bootstrap_servers": "broker-a:9092",
		"topics": [{
			"selector": "orders",
			"schema": {
				"type": "json",
				"text": "{\"type\":\"object\",\"required\":[\"order_id\"],\"properties\":{\"order_id\":{\"type\":\"string\"}}}"
			},
			"sample_messages": [{"order_id":"ord-1"}, {"wrong_field":1}]
		}]
	}`)
	err := controlbus.ValidateTopicConnectorConfig(cfg, "kafka connector")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sample_messages[1]")
}
