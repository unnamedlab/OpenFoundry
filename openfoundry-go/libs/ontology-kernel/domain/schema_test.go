package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func definition(name, source string, required bool) EffectivePropertyDefinition {
	return EffectivePropertyDefinition{
		Name:         name,
		DisplayName:  name,
		PropertyType: "string",
		Required:     required,
		Source:       source,
	}
}

// libs/ontology-kernel/src/domain/schema.rs `prefers_more_specific_property_source`.
func TestMergePrefersMoreSpecificSource(t *testing.T) {
	merged := MergeEffectiveDefinitions([]effectivePropertyEntry{
		{SharedPropertyPrecedence, definition("status", "shared_property_type", false)},
		{InterfacePropertyPrecedence, definition("status", "interface", false)},
		{DirectPropertyPrecedence, definition("status", "object_type", true)},
	})
	require.Len(t, merged, 1)
	assert.Equal(t, "object_type", merged[0].Source)
	assert.True(t, merged[0].Required)
}

// libs/ontology-kernel/src/domain/schema.rs `keeps_first_definition_for_same_precedence`.
// At equal precedence the existing entry wins (Rust `*existing >= precedence`).
func TestMergeKeepsFirstAtSamePrecedence(t *testing.T) {
	merged := MergeEffectiveDefinitions([]effectivePropertyEntry{
		{SharedPropertyPrecedence, definition("priority", "shared_property_type", false)},
		{SharedPropertyPrecedence, definition("priority", "shared_property_type", true)},
	})
	require.Len(t, merged, 1)
	assert.False(t, merged[0].Required, "first definition wins at equal precedence")
}

// libs/ontology-kernel/src/domain/schema.rs — output ordering is
// lexicographic by name (Rust uses BTreeMap; Go reproduces by
// sorting collected keys).
func TestMergeOutputIsLexicographicByName(t *testing.T) {
	merged := MergeEffectiveDefinitions([]effectivePropertyEntry{
		{DirectPropertyPrecedence, definition("zeta", "object_type", false)},
		{DirectPropertyPrecedence, definition("alpha", "object_type", false)},
		{DirectPropertyPrecedence, definition("mu", "object_type", false)},
	})
	require.Len(t, merged, 3)
	assert.Equal(t, "alpha", merged[0].Name)
	assert.Equal(t, "mu", merged[1].Name)
	assert.Equal(t, "zeta", merged[2].Name)
}

// libs/ontology-kernel/src/domain/schema.rs `validate_object_properties`
// — non-object input rejects with the verbatim message.
func TestValidateObjectPropertiesRejectsNonObject(t *testing.T) {
	_, err := ValidateObjectProperties(nil, json.RawMessage(`[1,2,3]`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "object properties must be a JSON object")
}

// libs/ontology-kernel/src/domain/schema.rs — unknown keys reject
// with `unknown property '<key>'`.
func TestValidateObjectPropertiesRejectsUnknownKeys(t *testing.T) {
	defs := []EffectivePropertyDefinition{
		{Name: "a", PropertyType: "string", Source: "object_type"},
	}
	_, err := ValidateObjectProperties(defs, json.RawMessage(`{"a":"x","b":"y"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown property 'b'`)
}

// libs/ontology-kernel/src/domain/schema.rs — required missing
// rejects with `<name> is required`; non-required absent omits.
func TestValidateObjectPropertiesRequiredAndNormalize(t *testing.T) {
	defs := []EffectivePropertyDefinition{
		{Name: "title", PropertyType: "string", Required: true, Source: "object_type"},
		{Name: "score", PropertyType: "integer", Source: "object_type"},
	}
	_, err := ValidateObjectProperties(defs, json.RawMessage(`{"score": 1}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "title is required")

	out, err := ValidateObjectProperties(defs, json.RawMessage(`{"title": "ok"}`))
	require.NoError(t, err)
	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(out, &got))
	require.Contains(t, got, "title")
	assert.NotContains(t, got, "score", "absent non-required key must not be normalized in")
}

// libs/ontology-kernel/src/domain/schema.rs — default_value injects
// for absent keys; mismatched value type rejects with the
// `<name>: <type-error>` prefix.
func TestValidateObjectPropertiesDefaultsAndTypeMismatch(t *testing.T) {
	defs := []EffectivePropertyDefinition{
		{
			Name:         "lang",
			PropertyType: "string",
			DefaultValue: json.RawMessage(`"en"`),
			Source:       "object_type",
		},
	}
	out, err := ValidateObjectProperties(defs, json.RawMessage(`{}`))
	require.NoError(t, err)
	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(out, &got))
	require.Contains(t, got, "lang")
	assert.JSONEq(t, `"en"`, string(got["lang"]))

	_, err = ValidateObjectProperties(defs, json.RawMessage(`{"lang": 42}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lang:")
	assert.Contains(t, err.Error(), "expected string value")
}
