package controlbus

// connectors.go ports libs/event-bus-control/src/connectors.rs.
//
// Connector-config helpers shared by data-connection plane modules.
// Pure logic — no DB, no HTTP. Inputs are JSON-decoded config blocks
// from the catalog and the helpers either parse them into typed
// structs (EventStreamTopic) or validate that the operator's
// configuration is internally consistent (sample messages match
// inline schemas, bootstrap servers are present, etc.).

import (
	"encoding/json"
	"fmt"
	"strings"
)

// EventStreamTopic is one parsed entry from a connector's `topics`
// array. Mirrors the Rust EventStreamTopic struct.
type EventStreamTopic struct {
	Selector       string
	DisplayName    string
	SampleMessages []json.RawMessage
	Partitions     int64
	Metadata       json.RawMessage
}

// ValidateTopicConnectorConfig validates a Kafka-style connector
// config: requires bootstrap_servers (or brokers) plus at least one
// topic, then runs ValidateTopicSamples on each topic that ships an
// inline schema.
//
// connectorLabel is used in error messages so callers can identify
// which connector failed when many are batch-validated.
func ValidateTopicConnectorConfig(config json.RawMessage, connectorLabel string) error {
	bootstrap, _ := BootstrapServers(config)
	if bootstrap == "" {
		return fmt.Errorf("%s requires 'bootstrap_servers' or 'brokers'", connectorLabel)
	}
	topics, err := ParseTopicEntries(config, connectorLabel)
	if err != nil {
		return err
	}
	if len(topics) == 0 {
		return fmt.Errorf("%s requires at least one topic in 'topics'", connectorLabel)
	}
	for _, topic := range topics {
		if err := ValidateTopicSamples(topic.Metadata, topic.SampleMessages, connectorLabel); err != nil {
			return err
		}
	}
	return nil
}

// ParseTopicEntries decodes the connector's `topics` field into a
// slice of EventStreamTopic. Accepts both the short form (string)
// and the long form (object with selector/display_name/...).
func ParseTopicEntries(config json.RawMessage, connectorLabel string) ([]EventStreamTopic, error) {
	var raw struct {
		Topics []json.RawMessage `json:"topics"`
	}
	if err := json.Unmarshal(config, &raw); err != nil {
		return nil, fmt.Errorf("%s requires 'topics' to be an array", connectorLabel)
	}
	if raw.Topics == nil {
		return nil, fmt.Errorf("%s requires 'topics' to be an array", connectorLabel)
	}
	out := make([]EventStreamTopic, 0, len(raw.Topics))
	for index, topicBytes := range raw.Topics {
		if entry, ok := tryParseStringTopic(topicBytes); ok {
			out = append(out, entry)
			continue
		}
		entry, err := parseObjectTopic(topicBytes, index, connectorLabel)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

// FindTopicEntry returns the topic with `selector` from `config`.
func FindTopicEntry(config json.RawMessage, selector, connectorLabel string) (EventStreamTopic, error) {
	topics, err := ParseTopicEntries(config, connectorLabel)
	if err != nil {
		return EventStreamTopic{}, err
	}
	for _, t := range topics {
		if t.Selector == selector {
			return t, nil
		}
	}
	return EventStreamTopic{}, fmt.Errorf("%s topic '%s' is not configured", connectorLabel, selector)
}

// ValidateTopicSamples validates a topic's sample_messages against an
// inline schema declared in the topic config. Expected metadata shape:
//
//	{
//	  "selector": "orders",
//	  "schema":   { "type": "avro" | "protobuf" | "json", "text": "..." },
//	  "sample_messages": [ { ... } ]
//	}
//
// schema_subject (a reference to a registered subject in the
// ingestion-replication-service Schema Registry) is recognised but
// only used by the connector for traceability — actual validation
// always runs against the inline schema.text when present.
//
// Returns nil if no schema is configured (validation is opt-in).
func ValidateTopicSamples(topicMetadata json.RawMessage, samples []json.RawMessage, connectorLabel string) error {
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(topicMetadata, &meta); err != nil {
		// Metadata not an object — nothing to validate against.
		return nil
	}
	schemaRaw, ok := meta["schema"]
	if !ok || isNullJSON(schemaRaw) {
		return nil
	}
	var schema struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(schemaRaw, &schema); err != nil {
		return fmt.Errorf("%s schema: %s", connectorLabel, err.Error())
	}
	if schema.Type == "" {
		return fmt.Errorf("%s schema requires 'type'", connectorLabel)
	}
	schemaType, err := ParseSchemaType(schema.Type)
	if err != nil {
		return fmt.Errorf("%s %s", connectorLabel, err.Error())
	}
	if schema.Text == "" {
		return fmt.Errorf("%s schema requires 'text'", connectorLabel)
	}
	for index, sample := range samples {
		if err := ValidatePayload(schemaType, schema.Text, sample); err != nil {
			return fmt.Errorf("%s sample_messages[%d] does not match schema: %s",
				connectorLabel, index, err.Error())
		}
	}
	return nil
}

// BootstrapServers returns the connector's broker bootstrap string.
// Accepts both `bootstrap_servers` and `brokers` keys (Confluent vs
// standard Kafka spelling).
func BootstrapServers(config json.RawMessage) (string, bool) {
	if v, ok := stringField(config, "bootstrap_servers"); ok {
		return v, true
	}
	return stringField(config, "brokers")
}

// SanitizeFileStem normalises a topic selector into a filesystem-safe
// filename stem (alphanumerics only, leading/trailing underscores
// trimmed, max 64 chars). Falls back to `fallback` when the result
// is empty.
func SanitizeFileStem(selector, fallback string) string {
	out := make([]byte, 0, len(selector))
	for _, r := range selector {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			out = append(out, byte(r))
		} else {
			out = append(out, '_')
		}
	}
	stem := strings.Trim(string(out), "_")
	if stem == "" {
		return fallback
	}
	if len(stem) > 64 {
		stem = stem[:64]
	}
	return stem
}

// ─── internals ─────────────────────────────────────────────────────────

func tryParseStringTopic(raw json.RawMessage) (EventStreamTopic, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return EventStreamTopic{}, false
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return EventStreamTopic{}, false
	}
	metadata, _ := json.Marshal(map[string]string{"topic": trimmed})
	return EventStreamTopic{
		Selector:       trimmed,
		DisplayName:    trimmed,
		SampleMessages: []json.RawMessage{},
		Partitions:     1,
		Metadata:       metadata,
	}, true
}

func parseObjectTopic(raw json.RawMessage, index int, connectorLabel string) (EventStreamTopic, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return EventStreamTopic{}, fmt.Errorf(
			"%s topics[%d] requires 'selector', 'topic' or 'name'", connectorLabel, index)
	}

	selector := pickStringField(obj, "selector", "topic", "name")
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return EventStreamTopic{}, fmt.Errorf(
			"%s topics[%d] requires 'selector', 'topic' or 'name'", connectorLabel, index)
	}

	displayName := pickStringField(obj, "display_name", "name")
	if displayName == "" {
		displayName = selector
	}

	var samples []json.RawMessage
	if rawSamples, ok := obj["sample_messages"]; ok && !isNullJSON(rawSamples) {
		_ = json.Unmarshal(rawSamples, &samples)
	} else if rawSamples, ok := obj["preview_rows"]; ok && !isNullJSON(rawSamples) {
		_ = json.Unmarshal(rawSamples, &samples)
	}
	if samples == nil {
		samples = []json.RawMessage{}
	}

	partitions := int64(1)
	if rawParts, ok := obj["partitions"]; ok {
		var n int64
		if err := json.Unmarshal(rawParts, &n); err == nil && n > 1 {
			partitions = n
		}
	}

	// Build metadata = clone(topicObject) minus sample_messages /
	// preview_rows. Mirrors the Rust impl exactly.
	metaCopy := make(map[string]json.RawMessage, len(obj))
	for k, v := range obj {
		if k == "sample_messages" || k == "preview_rows" {
			continue
		}
		metaCopy[k] = v
	}
	metadata, err := json.Marshal(metaCopy)
	if err != nil {
		metadata = json.RawMessage(`{}`)
	}

	return EventStreamTopic{
		Selector:       selector,
		DisplayName:    displayName,
		SampleMessages: samples,
		Partitions:     partitions,
		Metadata:       metadata,
	}, nil
}

// pickStringField returns the first non-empty string field amongst
// `keys`. Empty string when none matched.
func pickStringField(obj map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		raw, ok := obj[k]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err == nil && s != "" {
			return s
		}
	}
	return ""
}

func stringField(config json.RawMessage, field string) (string, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(config, &obj); err != nil {
		return "", false
	}
	raw, ok := obj[field]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	if strings.TrimSpace(s) == "" {
		return "", false
	}
	return s, true
}

func isNullJSON(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "null"
}
