// Package schema bridges the dataset schema model with Avro JSON.
//
// Ports services/ingestion-replication-service/src/event_streaming/models/schema_bridge.rs.
//
// Foundry's "Streams" doc lists 15 supported field types: BOOLEAN,
// BYTE, SHORT, INTEGER, LONG, FLOAT, DOUBLE, DECIMAL, STRING, MAP,
// ARRAY, STRUCT, BINARY, DATE, TIMESTAMP. The authoritative model
// already lives in libs/core-models/dataset.Schema (used by
// dataset-versioning-service). This package mirrors that model on the
// streaming side by converting dataset.Schema ↔ Avro JSON so a stream's
// schema_avro is interoperable with a dataset's schema.
//
// The bridge is intentionally lossless for the documented types and
// conservative on extensions: anything we don't recognise is rejected
// with BridgeErrUnsupported so callers see a loud failure rather than
// a silent type-narrowing.
package schema

import (
	"encoding/json"
	"fmt"

	"github.com/openfoundry/openfoundry-go/libs/core-models/dataset"
)

// BridgeErrorKind tags the BridgeError variants. Mirrors the Rust
// thiserror enum so callers can branch on the error class.
type BridgeErrorKind uint8

const (
	// BridgeErrUnsupported flags a field type that we deliberately do
	// not project (e.g. Avro `fixed`, unions other than `[null, T]`).
	BridgeErrUnsupported BridgeErrorKind = iota
	// BridgeErrMalformed flags structural problems (missing keys,
	// non-record top-level, unions without a non-null branch, …).
	BridgeErrMalformed
)

// BridgeError is the typed error returned by SchemaToAvro / AvroToSchema.
type BridgeError struct {
	Kind BridgeErrorKind
	Msg  string
}

// Error implements error.
func (e *BridgeError) Error() string {
	switch e.Kind {
	case BridgeErrUnsupported:
		return "unsupported field type: " + e.Msg
	case BridgeErrMalformed:
		return "malformed avro schema: " + e.Msg
	default:
		return "schema bridge error: " + e.Msg
	}
}

func unsupported(msg string) *BridgeError {
	return &BridgeError{Kind: BridgeErrUnsupported, Msg: msg}
}

func malformed(msg string) *BridgeError {
	return &BridgeError{Kind: BridgeErrMalformed, Msg: msg}
}

// SchemaToAvro converts a Foundry dataset.Schema into an Avro record
// schema (as a JSON value tree). Streams persist the result in
// streaming_streams.schema_avro.
func SchemaToAvro(schema *dataset.Schema) (any, error) {
	if schema == nil {
		return nil, malformed("schema is nil")
	}
	fields := make([]any, 0, len(schema.Fields))
	for i := range schema.Fields {
		entry, err := fieldToAvro(&schema.Fields[i])
		if err != nil {
			return nil, err
		}
		fields = append(fields, entry)
	}
	return map[string]any{
		"type":      "record",
		"name":      "StreamRecord",
		"namespace": "openfoundry.streams",
		"fields":    fields,
	}, nil
}

// SchemaToAvroJSON is a convenience wrapper that returns the Avro
// representation as a json.RawMessage for callers that persist or
// hash the bytes directly.
func SchemaToAvroJSON(schema *dataset.Schema) (json.RawMessage, error) {
	avro, err := SchemaToAvro(schema)
	if err != nil {
		return nil, err
	}
	return json.Marshal(avro)
}

func fieldToAvro(field *dataset.SchemaField) (map[string]any, error) {
	avroType, err := fieldTypeToAvro(field)
	if err != nil {
		return nil, err
	}
	finalType := avroType
	if field.Nullable {
		// Avro nullable encoding: ["null", T].
		finalType = []any{"null", avroType}
	}
	entry := map[string]any{
		"name": field.Name,
		"type": finalType,
	}
	if field.Description != "" {
		entry["doc"] = field.Description
	}
	return entry, nil
}

func fieldTypeToAvro(field *dataset.SchemaField) (any, error) {
	switch field.Type {
	case dataset.FTBoolean:
		return "boolean", nil
	case dataset.FTByte, dataset.FTShort, dataset.FTInteger:
		return "int", nil
	case dataset.FTLong:
		return "long", nil
	case dataset.FTFloat:
		return "float", nil
	case dataset.FTDouble:
		return "double", nil
	case dataset.FTString:
		return "string", nil
	case dataset.FTBinary:
		return "bytes", nil
	case dataset.FTDate:
		return map[string]any{"type": "int", "logicalType": "date"}, nil
	case dataset.FTTimestamp:
		return map[string]any{"type": "long", "logicalType": "timestamp-millis"}, nil
	case dataset.FTDecimal:
		precision := uint8(0)
		if field.Precision != nil {
			precision = *field.Precision
		}
		scale := uint8(0)
		if field.Scale != nil {
			scale = *field.Scale
		}
		return map[string]any{
			"type":        "bytes",
			"logicalType": "decimal",
			"precision":   precision,
			"scale":       scale,
		}, nil
	case dataset.FTArray:
		if field.ArraySubType == nil {
			return nil, malformed(fmt.Sprintf("array %q missing arraySubType", field.Name))
		}
		inner, err := fieldTypeToAvro(field.ArraySubType)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "array", "items": inner}, nil
	case dataset.FTMap:
		if field.MapKeyType == nil || field.MapValueType == nil {
			return nil, malformed(fmt.Sprintf("map %q requires mapKeyType and mapValueType", field.Name))
		}
		// Avro maps are string-keyed only. Non-string keys are encoded
		// as a STRUCT array of {key, value}, matching the dataset
		// writer so the round-trip stays lossless.
		if field.MapKeyType.Type == dataset.FTString {
			value, err := fieldTypeToAvro(field.MapValueType)
			if err != nil {
				return nil, err
			}
			return map[string]any{"type": "map", "values": value}, nil
		}
		key, err := fieldToAvro(field.MapKeyType)
		if err != nil {
			return nil, err
		}
		value, err := fieldToAvro(field.MapValueType)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "record",
				"name": fmt.Sprintf(
					"Map_%s_%s",
					fieldNameForType(field.MapKeyType.Type),
					fieldNameForType(field.MapValueType.Type),
				),
				"fields": []any{key, value},
			},
		}, nil
	case dataset.FTStruct:
		fields := make([]any, 0, len(field.SubSchemas))
		for i := range field.SubSchemas {
			entry, err := fieldToAvro(&field.SubSchemas[i])
			if err != nil {
				return nil, err
			}
			fields = append(fields, entry)
		}
		return map[string]any{
			"type":   "record",
			"name":   "Struct",
			"fields": fields,
		}, nil
	default:
		return nil, unsupported(string(field.Type))
	}
}

func fieldNameForType(ft dataset.FieldType) string {
	switch ft {
	case dataset.FTBoolean:
		return "Boolean"
	case dataset.FTByte:
		return "Byte"
	case dataset.FTShort:
		return "Short"
	case dataset.FTInteger:
		return "Integer"
	case dataset.FTLong:
		return "Long"
	case dataset.FTFloat:
		return "Float"
	case dataset.FTDouble:
		return "Double"
	case dataset.FTString:
		return "String"
	case dataset.FTBinary:
		return "Binary"
	case dataset.FTDate:
		return "Date"
	case dataset.FTTimestamp:
		return "Timestamp"
	case dataset.FTDecimal:
		return "Decimal"
	case dataset.FTArray:
		return "Array"
	case dataset.FTMap:
		return "Map"
	case dataset.FTStruct:
		return "Struct"
	}
	return string(ft)
}

// AvroToSchema parses a Foundry-style Avro JSON record schema back
// into the typed dataset model. The reverse of SchemaToAvro.
//
// `value` is the decoded JSON tree (any), so callers may pass either
// a map[string]any or whatever encoding/json produces from a
// json.RawMessage. AvroJSONToSchema is the byte-form convenience.
func AvroToSchema(value any) (*dataset.Schema, error) {
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, malformed("expected object")
	}
	if t, _ := obj["type"].(string); t != "record" {
		return nil, malformed("top-level schema must be record")
	}
	rawFields, ok := obj["fields"].([]any)
	if !ok {
		return nil, malformed("record missing fields")
	}
	fields := make([]dataset.SchemaField, 0, len(rawFields))
	for _, raw := range rawFields {
		f, err := fieldFromAvro(raw)
		if err != nil {
			return nil, err
		}
		fields = append(fields, f)
	}
	return &dataset.Schema{
		Fields:         fields,
		Format:         "avro",
		CustomMetadata: nil,
	}, nil
}

// AvroJSONToSchema decodes raw JSON bytes and delegates to AvroToSchema.
func AvroJSONToSchema(payload []byte) (*dataset.Schema, error) {
	var v any
	if err := json.Unmarshal(payload, &v); err != nil {
		return nil, malformed(err.Error())
	}
	return AvroToSchema(v)
}

func fieldFromAvro(value any) (dataset.SchemaField, error) {
	obj, ok := value.(map[string]any)
	if !ok {
		return dataset.SchemaField{}, malformed("field must be object")
	}
	name, ok := obj["name"].(string)
	if !ok {
		return dataset.SchemaField{}, malformed("field missing name")
	}
	rawType, ok := obj["type"]
	if !ok {
		return dataset.SchemaField{}, malformed("field missing type")
	}
	ft, nullable, payload, err := parseFieldType(rawType)
	if err != nil {
		return dataset.SchemaField{}, err
	}
	field := dataset.SchemaField{
		Name:     name,
		Type:     ft,
		Nullable: nullable,
	}
	if doc, ok := obj["doc"].(string); ok {
		field.Description = doc
	}
	applyFieldPayload(&field, payload)
	return field, nil
}

// fieldTypePayload carries the type-specific data (decimal precision,
// nested fields, …) returned alongside the dataset.FieldType from
// parseFieldType so fieldFromAvro can attach it to the SchemaField.
type fieldTypePayload struct {
	Precision    *uint8
	Scale        *uint8
	ArraySubType *dataset.SchemaField
	MapKeyType   *dataset.SchemaField
	MapValueType *dataset.SchemaField
	SubSchemas   []dataset.SchemaField
}

func applyFieldPayload(field *dataset.SchemaField, p fieldTypePayload) {
	field.Precision = p.Precision
	field.Scale = p.Scale
	field.ArraySubType = p.ArraySubType
	field.MapKeyType = p.MapKeyType
	field.MapValueType = p.MapValueType
	field.SubSchemas = p.SubSchemas
}

// parseFieldType walks one Avro `type` node and returns the dataset
// FieldType, whether the field was wrapped in an Avro `["null", T]`
// union, and the type-specific payload for composite kinds.
func parseFieldType(raw any) (dataset.FieldType, bool, fieldTypePayload, error) {
	if arr, ok := raw.([]any); ok {
		// `["null", T]` nullable encoding.
		hasNull := false
		var other any
		for _, v := range arr {
			if s, ok := v.(string); ok && s == "null" {
				hasNull = true
				continue
			}
			if other == nil {
				other = v
			}
		}
		if other == nil {
			return "", false, fieldTypePayload{}, malformed("union without non-null branch")
		}
		ft, _, payload, err := parseFieldType(other)
		if err != nil {
			return "", false, fieldTypePayload{}, err
		}
		return ft, hasNull, payload, nil
	}
	if s, ok := raw.(string); ok {
		ft, ok := primitiveAvroToField(s)
		if !ok {
			return "", false, fieldTypePayload{}, unsupported(s)
		}
		return ft, false, fieldTypePayload{}, nil
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return "", false, fieldTypePayload{}, malformed("expected object or string for type")
	}
	kind, ok := obj["type"].(string)
	if !ok {
		return "", false, fieldTypePayload{}, malformed("nested type missing kind")
	}
	logical, _ := obj["logicalType"].(string)
	switch {
	case kind == "int" && logical == "date":
		return dataset.FTDate, false, fieldTypePayload{}, nil
	case kind == "long" && logical == "timestamp-millis":
		return dataset.FTTimestamp, false, fieldTypePayload{}, nil
	case kind == "bytes" && logical == "decimal":
		precision, ok := numberAsUint64(obj["precision"])
		if !ok {
			return "", false, fieldTypePayload{}, malformed("decimal missing precision")
		}
		scale, _ := numberAsUint64(obj["scale"])
		p := uint8(precision)
		s := uint8(scale)
		return dataset.FTDecimal, false, fieldTypePayload{Precision: &p, Scale: &s}, nil
	case kind == "array":
		items, ok := obj["items"]
		if !ok {
			return "", false, fieldTypePayload{}, malformed("array missing items")
		}
		innerType, innerNullable, innerPayload, err := parseFieldType(items)
		if err != nil {
			return "", false, fieldTypePayload{}, err
		}
		sub := &dataset.SchemaField{
			Name:     "item",
			Type:     innerType,
			Nullable: innerNullable,
		}
		applyFieldPayload(sub, innerPayload)
		return dataset.FTArray, false, fieldTypePayload{ArraySubType: sub}, nil
	case kind == "map":
		values, ok := obj["values"]
		if !ok {
			return "", false, fieldTypePayload{}, malformed("map missing values")
		}
		valueType, valueNullable, valuePayload, err := parseFieldType(values)
		if err != nil {
			return "", false, fieldTypePayload{}, err
		}
		key := &dataset.SchemaField{
			Name:     "key",
			Type:     dataset.FTString,
			Nullable: false,
		}
		val := &dataset.SchemaField{
			Name:     "value",
			Type:     valueType,
			Nullable: valueNullable,
		}
		applyFieldPayload(val, valuePayload)
		return dataset.FTMap, false, fieldTypePayload{MapKeyType: key, MapValueType: val}, nil
	case kind == "record":
		rawFields, ok := obj["fields"].([]any)
		if !ok {
			return "", false, fieldTypePayload{}, malformed("record missing fields")
		}
		sub := make([]dataset.SchemaField, 0, len(rawFields))
		for _, rf := range rawFields {
			f, err := fieldFromAvro(rf)
			if err != nil {
				return "", false, fieldTypePayload{}, err
			}
			sub = append(sub, f)
		}
		return dataset.FTStruct, false, fieldTypePayload{SubSchemas: sub}, nil
	}
	return "", false, fieldTypePayload{}, unsupported(kind)
}

func primitiveAvroToField(name string) (dataset.FieldType, bool) {
	switch name {
	case "boolean":
		return dataset.FTBoolean, true
	case "int":
		return dataset.FTInteger, true
	case "long":
		return dataset.FTLong, true
	case "float":
		return dataset.FTFloat, true
	case "double":
		return dataset.FTDouble, true
	case "string":
		return dataset.FTString, true
	case "bytes":
		return dataset.FTBinary, true
	}
	return "", false
}

// numberAsUint64 normalises Avro numeric metadata that may arrive as
// a JSON number (float64), a json.Number (when callers decode with
// UseNumber), or a native Go int/uint when the bridge round-trips an
// in-memory tree built by SchemaToAvro itself. Returns (value, ok).
func numberAsUint64(v any) (uint64, bool) {
	switch n := v.(type) {
	case float64:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil || i < 0 {
			return 0, false
		}
		return uint64(i), true
	case int:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case int8:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case int16:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case int32:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case int64:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case uint:
		return uint64(n), true
	case uint8:
		return uint64(n), true
	case uint16:
		return uint64(n), true
	case uint32:
		return uint64(n), true
	case uint64:
		return n, true
	}
	return 0, false
}
