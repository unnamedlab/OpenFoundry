package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

// ValidPropertyTypes is the OpenFoundry canonical property-type set.
// The order is stable because the slice is rendered into the error
// string surfaced by [ValidatePropertyType].
var ValidPropertyTypes = []string{
	"string",
	"text",
	"integer",
	"long",
	"float",
	"double",
	"decimal",
	"number",
	"boolean",
	"date",
	"time",
	"timestamp",
	"json",
	"array",
	"vector",
	"reference",
	"object_reference",
	"geo_point",
	"geopoint",
	"geohash",
	"geoshape",
	"geojson",
	"geometry",
	"media_reference",
	"time_series",
	"binary",
	// TASK J — struct property/parameter type. Recursive validation
	// happens in handlers/actions::materialize_parameters because it
	// needs the nested `struct_fields` schema, which
	// [ValidatePropertyValue] does not see.
	"struct",
	// TASK P — OSv2-only attachment parameter type. Stores an
	// attachment_rid returned by `POST /api/v1/ontology/actions/uploads`.
	"attachment",
}

// ValidatePropertyType accepts the canonical spellings above plus typed
// array forms, otherwise returning `invalid property type '<x>', valid
// types: ["string", ...]`.
func ValidatePropertyType(propertyType string) error {
	for _, t := range ValidPropertyTypes {
		if t == propertyType {
			return nil
		}
	}
	metadata := models.PropertyTypeMetadataFor(propertyType)
	if metadata.IsArray && metadata.ArrayItemType != nil {
		itemMetadata := models.PropertyTypeMetadataFor(*metadata.ArrayItemType)
		if itemMetadata.TypeFamily == "unknown" {
			return fmt.Errorf(`invalid property type '%s', valid types: %s`, propertyType, rustSliceDebug(ValidPropertyTypes))
		}
		if metadata.ArrayAllowed {
			return nil
		}
		return fmt.Errorf("property type '%s' cannot be used as an array item", *metadata.ArrayItemType)
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
//   - json: anything is accepted.
//   - array: must be an array; typed arrays validate each element.
//   - struct: must be a JSON object.
//   - vector: must be a non-empty array of numeric values.
//   - reference: must be a string (UUID-shape not enforced here).
//   - geopoint: must be an object with numeric `lat`/`latitude`
//     and `lon`/`longitude`, both within range.
//   - geoshape: must be a GeoJSON-like object or non-empty string.
//   - media_reference: string OR object carrying a non-empty
//     `uri`/`url` or media `reference`.
//   - time_series: must be a string reference, object, or array.
//   - attachment: string OR object carrying a non-empty
//     `attachment_rid`/`rid`.
func ValidatePropertyValue(propertyType string, value json.RawMessage) error {
	trimmed := bytes.TrimSpace(value)
	metadata := models.PropertyTypeMetadataFor(propertyType)
	if metadata.IsArray {
		if metadata.ArrayItemType == nil {
			if !isJSONArray(trimmed) {
				return fmt.Errorf("expected array value")
			}
			return nil
		}
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return fmt.Errorf("expected array value")
		}
		for _, entry := range arr {
			if err := ValidatePropertyValue(*metadata.ArrayItemType, entry); err != nil {
				return err
			}
		}
		return nil
	}
	switch metadata.BaseType {
	case "string", "enum":
		// `enum` is a string with a constrained value set; the allowed
		// values are enforced by validation_rules.enum_values further up
		// the stack. Here we only check the JSON shape.
		if !isJSONString(trimmed) {
			return fmt.Errorf("expected string value")
		}
		return nil
	case "integer", "long":
		if !isJSONInteger(trimmed) {
			return fmt.Errorf("expected integer value")
		}
		return nil
	case "float", "decimal":
		if !isJSONNumber(trimmed) {
			return fmt.Errorf("expected numeric value")
		}
		return nil
	case "boolean":
		if !isJSONBool(trimmed) {
			return fmt.Errorf("expected boolean value")
		}
		return nil
	case "json":
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
	case "date", "time", "timestamp":
		if !isJSONString(trimmed) {
			return fmt.Errorf("expected string date value")
		}
		return nil
	case "reference", "geohash", "binary":
		if !isJSONString(trimmed) {
			return fmt.Errorf("expected string reference value")
		}
		return nil
	case "geopoint":
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err != nil || obj == nil {
			return fmt.Errorf("expected object value with lat/lon for geopoint")
		}
		latRaw, ok := pickKey(obj, "lat", "latitude")
		if !ok {
			return fmt.Errorf("geopoint requires numeric lat/lon fields")
		}
		lonRaw, ok := pickKey(obj, "lon", "longitude")
		if !ok {
			return fmt.Errorf("geopoint requires numeric lat/lon fields")
		}
		var lat, lon float64
		if err := json.Unmarshal(latRaw, &lat); err != nil {
			return fmt.Errorf("geopoint requires numeric lat/lon fields")
		}
		if err := json.Unmarshal(lonRaw, &lon); err != nil {
			return fmt.Errorf("geopoint requires numeric lat/lon fields")
		}
		if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			return fmt.Errorf("geopoint latitude/longitude out of range")
		}
		return nil
	case "geoshape":
		if isNonEmptyJSONString(trimmed) {
			return nil
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err != nil || obj == nil {
			return fmt.Errorf("expected GeoJSON object or non-empty string for geoshape")
		}
		if rawType, ok := obj["type"]; ok && isNonEmptyJSONString(bytes.TrimSpace(rawType)) {
			if _, ok := obj["coordinates"]; ok {
				return nil
			}
			if _, ok := obj["geometries"]; ok {
				return nil
			}
			if _, ok := obj["features"]; ok {
				return nil
			}
		}
		if rawGeoJSON, ok := pickKey(obj, "geojson", "geometry"); ok {
			if isJSONObject(bytes.TrimSpace(rawGeoJSON)) || isNonEmptyJSONString(bytes.TrimSpace(rawGeoJSON)) {
				return nil
			}
		}
		return fmt.Errorf("geoshape requires GeoJSON type with coordinates/geometries/features")
	case "media_reference":
		if isJSONString(trimmed) {
			return nil
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err != nil || obj == nil {
			return fmt.Errorf("expected string or object for media_reference")
		}
		if _, ok := obj["reference"]; ok {
			return nil
		}
		uriRaw, ok := pickKey(obj, "uri", "url")
		if !ok {
			return fmt.Errorf("media_reference requires a non-empty uri, url, or reference")
		}
		var uri string
		if err := json.Unmarshal(uriRaw, &uri); err != nil || strings.TrimSpace(uri) == "" {
			return fmt.Errorf("media_reference requires a non-empty uri, url, or reference")
		}
		return nil
	case "time_series":
		if isNonEmptyJSONString(trimmed) || isJSONObject(trimmed) || isJSONArray(trimmed) {
			return nil
		}
		return fmt.Errorf("expected string, object, or array value for time_series")
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

func isNonEmptyJSONString(b []byte) bool {
	if !isJSONString(b) {
		return false
	}
	var value string
	return json.Unmarshal(b, &value) == nil && strings.TrimSpace(value) != ""
}

func isJSONBool(b []byte) bool {
	return bytes.Equal(b, []byte("true")) || bytes.Equal(b, []byte("false"))
}

func isJSONObject(b []byte) bool { return len(b) > 0 && b[0] == '{' }

func isJSONArray(b []byte) bool { return len(b) > 0 && b[0] == '[' }

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
