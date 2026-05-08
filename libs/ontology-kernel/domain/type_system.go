package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ValidPropertyTypes mirrors the `VALID_TYPES` slice in
// `libs/ontology-kernel/src/domain/type_system.rs`. Order is kept
// verbatim because the slice is rendered into the error string
// surfaced by [ValidatePropertyType].
var ValidPropertyTypes = []string{
	"string",
	"integer",
	"float",
	"boolean",
	"date",
	"timestamp",
	"json",
	"array",
	"vector",
	"reference",
	"geo_point",
	"media_reference",
	// TASK J — struct property/parameter type. Recursive validation
	// happens in handlers/actions::materialize_parameters because it
	// needs the nested `struct_fields` schema, which
	// [ValidatePropertyValue] does not see.
	"struct",
	// TASK P — OSv2-only attachment parameter type. Stores an
	// attachment_rid returned by `POST /api/v1/ontology/actions/uploads`.
	"attachment",
}

// ValidatePropertyType mirrors `validate_property_type` — accepts the
// 14 spellings above, otherwise returns the verbatim Rust error
// string `invalid property type '<x>', valid types: ["string", ...]`.
func ValidatePropertyType(propertyType string) error {
	for _, t := range ValidPropertyTypes {
		if t == propertyType {
			return nil
		}
	}
	return fmt.Errorf(`invalid property type '%s', valid types: %s`, propertyType, rustSliceDebug(ValidPropertyTypes))
}

// rustSliceDebug formats a slice the way Rust's `Debug` does for
// `&[&str]`: `["a", "b", "c"]`. Required so the error surface in
// Go matches the byte sequence Rust handlers emit.
func rustSliceDebug(values []string) string {
	var b strings.Builder
	b.WriteString("[")
	for i, v := range values {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(`"`)
		b.WriteString(v)
		b.WriteString(`"`)
	}
	b.WriteString("]")
	return b.String()
}

// ValidatePropertyValue mirrors `validate_property_value`. Inspects
// the raw JSON `value` and returns the verbatim Rust error string on
// mismatch. The validator only looks at JSON shape — semantic checks
// (e.g. UUID parse for "reference") are intentionally absent here
// because the Rust source defers them to the writer side.
//
// Per-type contracts (1:1 with Rust):
//
//   - string / date / timestamp: value must be a JSON string.
//   - integer: must be an integer (no fractional component).
//   - float: must be a number (integers also accepted, mirroring
//     `is_f64() || is_i64()` in Rust).
//   - boolean: must be a JSON bool.
//   - json / array: anything is accepted.
//   - struct: must be a JSON object.
//   - vector: must be a non-empty array of numeric values.
//   - reference: must be a string (UUID-shape not enforced here).
//   - geo_point: must be an object with numeric `lat`/`latitude`
//     and `lon`/`longitude`, both within range.
//   - media_reference: string OR object carrying a non-empty
//     `uri`/`url`.
//   - attachment: string OR object carrying a non-empty
//     `attachment_rid`/`rid`.
func ValidatePropertyValue(propertyType string, value json.RawMessage) error {
	trimmed := bytes.TrimSpace(value)
	switch propertyType {
	case "string":
		if !isJSONString(trimmed) {
			return fmt.Errorf("expected string value")
		}
		return nil
	case "integer":
		if !isJSONInteger(trimmed) {
			return fmt.Errorf("expected integer value")
		}
		return nil
	case "float":
		if !isJSONNumber(trimmed) {
			return fmt.Errorf("expected numeric value")
		}
		return nil
	case "boolean":
		if !isJSONBool(trimmed) {
			return fmt.Errorf("expected boolean value")
		}
		return nil
	case "json", "array":
		return nil
	case "struct":
		if !isJSONObject(trimmed) {
			return fmt.Errorf("expected object value for struct")
		}
		return nil
	case "vector":
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return fmt.Errorf("expected numeric array value for vector")
		}
		if len(arr) == 0 {
			return fmt.Errorf("vector value cannot be empty")
		}
		for _, entry := range arr {
			if !isJSONNumber(bytes.TrimSpace(entry)) {
				return fmt.Errorf("vector requires an array of numeric values")
			}
		}
		return nil
	case "date", "timestamp":
		if !isJSONString(trimmed) {
			return fmt.Errorf("expected string date value")
		}
		return nil
	case "reference":
		if !isJSONString(trimmed) {
			return fmt.Errorf("expected UUID string for reference")
		}
		return nil
	case "geo_point":
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err != nil || obj == nil {
			return fmt.Errorf("expected object value with lat/lon for geo_point")
		}
		latRaw, ok := pickKey(obj, "lat", "latitude")
		if !ok {
			return fmt.Errorf("geo_point requires numeric lat/lon fields")
		}
		lonRaw, ok := pickKey(obj, "lon", "longitude")
		if !ok {
			return fmt.Errorf("geo_point requires numeric lat/lon fields")
		}
		var lat, lon float64
		if err := json.Unmarshal(latRaw, &lat); err != nil {
			return fmt.Errorf("geo_point requires numeric lat/lon fields")
		}
		if err := json.Unmarshal(lonRaw, &lon); err != nil {
			return fmt.Errorf("geo_point requires numeric lat/lon fields")
		}
		if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			return fmt.Errorf("geo_point latitude/longitude out of range")
		}
		return nil
	case "media_reference":
		if isJSONString(trimmed) {
			return nil
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err != nil || obj == nil {
			return fmt.Errorf("expected string or object for media_reference")
		}
		uriRaw, ok := pickKey(obj, "uri", "url")
		if !ok {
			return fmt.Errorf("media_reference requires a non-empty uri or url")
		}
		var uri string
		if err := json.Unmarshal(uriRaw, &uri); err != nil || strings.TrimSpace(uri) == "" {
			return fmt.Errorf("media_reference requires a non-empty uri or url")
		}
		return nil
	case "attachment":
		if isJSONString(trimmed) {
			return nil
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err != nil || obj == nil {
			return fmt.Errorf("expected string or object for attachment")
		}
		ridRaw, ok := pickKey(obj, "attachment_rid", "rid")
		if !ok {
			return fmt.Errorf("attachment requires a non-empty attachment_rid")
		}
		var rid string
		if err := json.Unmarshal(ridRaw, &rid); err != nil || strings.TrimSpace(rid) == "" {
			return fmt.Errorf("attachment requires a non-empty attachment_rid")
		}
		return nil
	default:
		return fmt.Errorf("unknown type: %s", propertyType)
	}
}

// pickKey returns the first present key from `keys` in obj, mirroring
// Rust `value.get(a).or_else(|| value.get(b))`.
func pickKey(obj map[string]json.RawMessage, keys ...string) (json.RawMessage, bool) {
	for _, k := range keys {
		if v, ok := obj[k]; ok {
			return v, true
		}
	}
	return nil, false
}

func isJSONString(b []byte) bool {
	return len(b) >= 2 && b[0] == '"' && b[len(b)-1] == '"'
}

func isJSONBool(b []byte) bool { return bytes.Equal(b, []byte("true")) || bytes.Equal(b, []byte("false")) }

func isJSONObject(b []byte) bool { return len(b) > 0 && b[0] == '{' }

// isJSONNumber accepts any JSON number (integer or float).
func isJSONNumber(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	var f float64
	return json.Unmarshal(b, &f) == nil
}

// isJSONInteger mirrors Rust `is_i64() || is_u64()` — accepts a JSON
// number with no fractional component or exponent.
func isJSONInteger(b []byte) bool {
	if !isJSONNumber(b) {
		return false
	}
	if bytes.ContainsAny(b, ".eE") {
		return false
	}
	return true
}

// ValidateCardinality mirrors `validate_cardinality` — accepts the
// four canonical edge cardinalities, otherwise returns the verbatim
// Rust error string.
func ValidateCardinality(cardinality string) error {
	switch cardinality {
	case "one_to_one", "one_to_many", "many_to_one", "many_to_many":
		return nil
	default:
		return fmt.Errorf("invalid cardinality '%s', valid: one_to_one, one_to_many, many_to_one, many_to_many", cardinality)
	}
}
