package controlbus_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	controlbus "github.com/openfoundry/openfoundry-go/libs/event-bus-control"
)

const avroV1 = `{
	"type": "record",
	"name": "Order",
	"fields": [
		{ "name": "order_id", "type": "string" },
		{ "name": "amount", "type": "long" }
	]
}`

// AVRO_V2_COMPATIBLE — adds a new optional field with a default.
const avroV2Compatible = `{
	"type": "record",
	"name": "Order",
	"fields": [
		{ "name": "order_id", "type": "string" },
		{ "name": "amount", "type": "long" },
		{ "name": "currency", "type": "string", "default": "USD" }
	]
}`

// AVRO_V2_BREAKING — adds a new required field with no default.
const avroV2Breaking = `{
	"type": "record",
	"name": "Order",
	"fields": [
		{ "name": "order_id", "type": "string" },
		{ "name": "amount", "type": "long" },
		{ "name": "currency", "type": "string" }
	]
}`

func TestFingerprintIsCanonicalAndStable(t *testing.T) {
	t.Parallel()
	f1, err := controlbus.Fingerprint(controlbus.SchemaTypeAvro, avroV1)
	require.NoError(t, err)
	// Same schema with reformatted whitespace must produce same fingerprint.
	var v any
	require.NoError(t, json.Unmarshal([]byte(avroV1), &v))
	pretty, err := json.MarshalIndent(v, "", "  ")
	require.NoError(t, err)
	f2, err := controlbus.Fingerprint(controlbus.SchemaTypeAvro, string(pretty))
	require.NoError(t, err)
	assert.Equal(t, f1, f2)
	assert.True(t, strings.HasPrefix(f1, "sha256:"))
}

func TestAvroPayloadValidatesAgainstSchema(t *testing.T) {
	t.Parallel()
	payload := json.RawMessage(`{"order_id": "ord-1", "amount": 4200}`)
	require.NoError(t, controlbus.ValidatePayload(controlbus.SchemaTypeAvro, avroV1, payload))
}

func TestAvroV2IsBackwardCompatibleWithV1(t *testing.T) {
	t.Parallel()
	require.NoError(t, controlbus.CheckCompatibility(
		controlbus.SchemaTypeAvro, avroV1, avroV2Compatible, controlbus.CompatibilityBackward,
	), "BACKWARD compatible: optional field with default is non-breaking")
}

func TestAvroBreakingIsRejectedUnderBackward(t *testing.T) {
	t.Parallel()
	err := controlbus.CheckCompatibility(
		controlbus.SchemaTypeAvro, avroV1, avroV2Breaking, controlbus.CompatibilityBackward,
	)
	require.Error(t, err, "removing-default required field is BREAKING")
	var se *controlbus.SchemaError
	require.True(t, errors.As(err, &se))
	assert.Equal(t, controlbus.SchemaErrCompatibility, se.Kind)
}

func TestJSONSchemaValidatesPayload(t *testing.T) {
	t.Parallel()
	schema := `{
		"type": "object",
		"required": ["order_id"],
		"properties": {
			"order_id": { "type": "string" },
			"amount": { "type": "number" }
		}
	}`
	require.NoError(t, controlbus.ValidatePayload(
		controlbus.SchemaTypeJSON, schema, json.RawMessage(`{"order_id":"ord-1"}`),
	))
	err := controlbus.ValidatePayload(
		controlbus.SchemaTypeJSON, schema, json.RawMessage(`{"amount":1}`),
	)
	require.Error(t, err)
	var se *controlbus.SchemaError
	require.True(t, errors.As(err, &se))
	assert.Equal(t, controlbus.SchemaErrValidation, se.Kind)
}

func TestJSONSchemaCompatibilityDetectsNewRequiredField(t *testing.T) {
	t.Parallel()
	v1 := `{
		"type": "object",
		"required": ["a"],
		"properties": { "a": { "type": "string" } }
	}`
	v2Breaking := `{
		"type": "object",
		"required": ["a", "b"],
		"properties": { "a": { "type": "string" } }
	}`
	err := controlbus.CheckCompatibility(
		controlbus.SchemaTypeJSON, v1, v2Breaking, controlbus.CompatibilityBackward,
	)
	require.Error(t, err, "BREAKING: new required field 'b' was not in previous schema")
	assert.Contains(t, err.Error(), "'b'")
}

func TestParseSchemaTypeAccepts(t *testing.T) {
	t.Parallel()
	cases := map[string]controlbus.SchemaType{
		"avro":        controlbus.SchemaTypeAvro,
		"AVRO":        controlbus.SchemaTypeAvro,
		"protobuf":    controlbus.SchemaTypeProtobuf,
		"PROTO":       controlbus.SchemaTypeProtobuf,
		"json":        controlbus.SchemaTypeJSON,
		"JSON_SCHEMA": controlbus.SchemaTypeJSON,
		"jsonschema":  controlbus.SchemaTypeJSON,
	}
	for input, want := range cases {
		got, err := controlbus.ParseSchemaType(input)
		require.NoError(t, err, "input=%q", input)
		assert.Equal(t, want, got, "input=%q", input)
	}
	_, err := controlbus.ParseSchemaType("toml")
	require.Error(t, err)
	var se *controlbus.SchemaError
	require.True(t, errors.As(err, &se))
	assert.Equal(t, controlbus.SchemaErrUnsupportedSchemaType, se.Kind)
}

func TestParseCompatibilityModeAccepts(t *testing.T) {
	t.Parallel()
	cases := map[string]controlbus.CompatibilityMode{
		"none":                controlbus.CompatibilityNone,
		"backward":            controlbus.CompatibilityBackward,
		"backward_transitive": controlbus.CompatibilityBackwardTransitive,
		"forward":             controlbus.CompatibilityForward,
		"forward_transitive":  controlbus.CompatibilityForwardTransitive,
		"full":                controlbus.CompatibilityFull,
		"full_transitive":     controlbus.CompatibilityFullTransitive,
	}
	for input, want := range cases {
		got, err := controlbus.ParseCompatibilityMode(input)
		require.NoError(t, err, "input=%q", input)
		assert.Equal(t, want, got, "input=%q", input)
	}
}

func TestCompatibilityNoneIsAlwaysOk(t *testing.T) {
	t.Parallel()
	require.NoError(t, controlbus.CheckCompatibility(
		controlbus.SchemaTypeAvro, avroV1, avroV2Breaking, controlbus.CompatibilityNone,
	))
}
