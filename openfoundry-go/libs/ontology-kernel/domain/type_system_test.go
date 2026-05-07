package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// libs/ontology-kernel/src/domain/type_system.rs `VALID_TYPES` —
// every spelling accepted by Rust must also be accepted by Go,
// in the same order so error messages render identically.
func TestValidatePropertyTypeAcceptsCanonicalSet(t *testing.T) {
	wanted := []string{
		"string", "integer", "float", "boolean", "date", "timestamp",
		"json", "array", "vector", "reference", "geo_point",
		"media_reference", "struct", "attachment",
	}
	for _, ty := range wanted {
		require.NoError(t, ValidatePropertyType(ty), "want %q to be a valid type", ty)
	}
	assert.Equal(t, wanted, ValidPropertyTypes)
}

// libs/ontology-kernel/src/domain/type_system.rs — unknown types
// surface the Rust-formatted error string with the same valid-types
// debug spelling.
func TestValidatePropertyTypeRejectsUnknown(t *testing.T) {
	err := ValidatePropertyType("nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid property type 'nope'`)
	assert.Contains(t, err.Error(), `["string", "integer"`)
	assert.Contains(t, err.Error(), `"attachment"]`)
}

// libs/ontology-kernel/src/domain/type_system.rs `accepts_geo_point_type_and_value`.
func TestGeoPointAcceptsCanonicalShape(t *testing.T) {
	require.NoError(t, ValidatePropertyType("geo_point"))
	require.NoError(t, ValidatePropertyValue("geo_point", json.RawMessage(`{"lat": 40.4, "lon": -3.7}`)))
	// `latitude` / `longitude` aliases also accepted.
	require.NoError(t, ValidatePropertyValue("geo_point", json.RawMessage(`{"latitude": 0, "longitude": 0}`)))
}

// libs/ontology-kernel/src/domain/type_system.rs — out-of-range
// lat/lon and non-numeric inputs reject with the verbatim Rust
// strings.
func TestGeoPointRangeAndShapeErrors(t *testing.T) {
	err := ValidatePropertyValue("geo_point", json.RawMessage(`{"lat": 91, "lon": 0}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")

	err = ValidatePropertyValue("geo_point", json.RawMessage(`{"lat": "x", "lon": 0}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "numeric lat/lon")

	err = ValidatePropertyValue("geo_point", json.RawMessage(`"oops"`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected object value with lat/lon for geo_point")
}

// libs/ontology-kernel/src/domain/type_system.rs `accepts_media_reference_type_and_value`.
func TestMediaReferenceShape(t *testing.T) {
	require.NoError(t, ValidatePropertyValue("media_reference", json.RawMessage(`{"uri": "s3://bucket/file.png"}`)))
	require.NoError(t, ValidatePropertyValue("media_reference", json.RawMessage(`{"url": "https://x"}`)))
	require.NoError(t, ValidatePropertyValue("media_reference", json.RawMessage(`"raw-rid"`)))

	err := ValidatePropertyValue("media_reference", json.RawMessage(`{"uri": "  "}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-empty uri or url")
}

// libs/ontology-kernel/src/domain/type_system.rs — TASK P attachment
// accepts string OR object with attachment_rid / rid.
func TestAttachmentShape(t *testing.T) {
	require.NoError(t, ValidatePropertyValue("attachment", json.RawMessage(`"rid-xyz"`)))
	require.NoError(t, ValidatePropertyValue("attachment", json.RawMessage(`{"attachment_rid": "rid-xyz"}`)))
	require.NoError(t, ValidatePropertyValue("attachment", json.RawMessage(`{"rid": "rid-xyz"}`)))

	err := ValidatePropertyValue("attachment", json.RawMessage(`{"attachment_rid": ""}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-empty attachment_rid")

	err = ValidatePropertyValue("attachment", json.RawMessage(`42`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected string or object for attachment")
}

// libs/ontology-kernel/src/domain/type_system.rs `accepts_vector_type_and_numeric_array_value`.
func TestVectorAcceptsNumericArray(t *testing.T) {
	require.NoError(t, ValidatePropertyType("vector"))
	require.NoError(t, ValidatePropertyValue("vector", json.RawMessage(`[0.1, 0.2, 0.3]`)))
	require.NoError(t, ValidatePropertyValue("vector", json.RawMessage(`[1, 2, 3]`)))

	err := ValidatePropertyValue("vector", json.RawMessage(`[]`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vector value cannot be empty")

	err = ValidatePropertyValue("vector", json.RawMessage(`["a"]`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vector requires an array of numeric values")

	err = ValidatePropertyValue("vector", json.RawMessage(`"not-array"`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected numeric array value for vector")
}

// libs/ontology-kernel/src/domain/type_system.rs — primitive type
// guards return their verbatim Rust messages on shape mismatch.
func TestPrimitiveTypeMismatches(t *testing.T) {
	require.Error(t, ValidatePropertyValue("string", json.RawMessage(`42`)))
	require.Error(t, ValidatePropertyValue("integer", json.RawMessage(`1.5`)))
	require.NoError(t, ValidatePropertyValue("integer", json.RawMessage(`42`)))
	require.NoError(t, ValidatePropertyValue("float", json.RawMessage(`42`)))
	require.NoError(t, ValidatePropertyValue("float", json.RawMessage(`3.14`)))
	require.Error(t, ValidatePropertyValue("boolean", json.RawMessage(`"true"`)))
	require.NoError(t, ValidatePropertyValue("boolean", json.RawMessage(`true`)))
	require.NoError(t, ValidatePropertyValue("json", json.RawMessage(`{"any": 1}`)))
	require.NoError(t, ValidatePropertyValue("array", json.RawMessage(`[1, "two", null]`)))
	require.Error(t, ValidatePropertyValue("struct", json.RawMessage(`[]`)))
	require.NoError(t, ValidatePropertyValue("struct", json.RawMessage(`{"k": 1}`)))
	require.Error(t, ValidatePropertyValue("date", json.RawMessage(`123`)))
	require.NoError(t, ValidatePropertyValue("date", json.RawMessage(`"2026-05-06"`)))
	require.Error(t, ValidatePropertyValue("reference", json.RawMessage(`123`)))
	require.NoError(t, ValidatePropertyValue("reference", json.RawMessage(`"a-uuid"`)))

	err := ValidatePropertyValue("nope", json.RawMessage(`null`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown type: nope")
}

// libs/ontology-kernel/src/domain/type_system.rs `validate_cardinality`.
func TestValidateCardinality(t *testing.T) {
	for _, c := range []string{"one_to_one", "one_to_many", "many_to_one", "many_to_many"} {
		require.NoError(t, ValidateCardinality(c))
	}
	err := ValidateCardinality("infinite")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid cardinality 'infinite'`)
	assert.Contains(t, err.Error(), "valid: one_to_one, one_to_many, many_to_one, many_to_many")
}
