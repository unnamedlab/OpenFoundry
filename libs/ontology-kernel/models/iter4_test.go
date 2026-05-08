package models

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// libs/ontology-kernel/src/models/function_package.rs
// `default_function_package_version()` returns `"0.1.0"` and the
// constant exposes the same value.
func TestFunctionPackageDefaultVersion(t *testing.T) {
	assert.Equal(t, "0.1.0", DefaultFunctionPackageVersion)
	assert.Equal(t, "0.1.0", FunctionPackageDefaultVersion())
}

// libs/ontology-kernel/src/models/function_package.rs
// `parse_function_package_version` trims and rejects non-semver. The
// error must start with the verbatim Rust prefix.
func TestParseFunctionPackageVersion(t *testing.T) {
	v, err := ParseFunctionPackageVersion("  1.2.3  ")
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", v)

	_, err = ParseFunctionPackageVersion("v1.2")
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "function package version must be valid semver: "), err.Error())

	_, err = ParseFunctionPackageVersion("1.2")
	require.Error(t, err)

	v, err = ParseFunctionPackageVersion("1.2.3-rc1+build7")
	require.NoError(t, err)
	assert.Equal(t, "1.2.3-rc1+build7", v)
}

// libs/ontology-kernel/src/models/function_package.rs
// `FunctionCapabilities` per-field serde defaults: `allow_ontology_read`
// defaults to `true`, the rest to bool default (`false`); timeout/size
// defaults take their helper values.
func TestFunctionCapabilitiesPerFieldDefaults(t *testing.T) {
	var c FunctionCapabilities
	require.NoError(t, json.Unmarshal([]byte(`{}`), &c))
	assert.True(t, c.AllowOntologyRead)
	assert.False(t, c.AllowOntologyWrite)
	assert.False(t, c.AllowAI)
	assert.False(t, c.AllowNetwork)
	assert.Equal(t, uint64(15), c.TimeoutSeconds)
	assert.Equal(t, uint64(64*1024), c.MaxSourceBytes)
}

// libs/ontology-kernel/src/models/function_package.rs `impl Default`
// returns all four booleans as `true` — diverges from the per-field
// serde defaults and is what `unwrap_or_default()` invokes.
func TestDefaultFunctionCapabilitiesAllTrue(t *testing.T) {
	d := DefaultFunctionCapabilities()
	assert.True(t, d.AllowOntologyRead)
	assert.True(t, d.AllowOntologyWrite)
	assert.True(t, d.AllowAI)
	assert.True(t, d.AllowNetwork)
	assert.Equal(t, uint64(15), d.TimeoutSeconds)
	assert.Equal(t, uint64(64*1024), d.MaxSourceBytes)
}

// libs/ontology-kernel/src/models/function_package.rs
// `TryFrom<FunctionPackageRow>::try_from` falls back to the
// `impl Default` capabilities (all-true) on parse failure.
func TestFunctionPackageRowFallbackCapabilities(t *testing.T) {
	row := FunctionPackageRow{Capabilities: json.RawMessage(`not json`)}
	pkg := row.IntoPackage()
	assert.True(t, pkg.Capabilities.AllowOntologyWrite)
	assert.True(t, pkg.Capabilities.AllowAI)

	// And a valid payload round-trips with per-field defaults.
	row.Capabilities = json.RawMessage(`{"allow_ai": true}`)
	pkg = row.IntoPackage()
	assert.True(t, pkg.Capabilities.AllowOntologyRead) // serde default true
	assert.True(t, pkg.Capabilities.AllowAI)
	assert.False(t, pkg.Capabilities.AllowOntologyWrite)
}

// libs/ontology-kernel/src/models/action_type.rs `ActionOperationKind`
// — every variant serialises to its snake_case spelling, including
// the TASK I additions.
func TestActionOperationKindSnakeCase(t *testing.T) {
	cases := []ActionOperationKind{
		ActionOperationKindUpdateObject,
		ActionOperationKindCreateLink,
		ActionOperationKindDeleteObject,
		ActionOperationKindInvokeFunction,
		ActionOperationKindInvokeWebhook,
		ActionOperationKindCreateInterface,
		ActionOperationKindModifyInterface,
		ActionOperationKindDeleteInterface,
		ActionOperationKindCreateInterfaceLink,
		ActionOperationKindDeleteInterfaceLink,
	}
	wants := []string{
		`"update_object"`, `"create_link"`, `"delete_object"`,
		`"invoke_function"`, `"invoke_webhook"`,
		`"create_interface"`, `"modify_interface"`,
		`"delete_interface"`, `"create_interface_link"`, `"delete_interface_link"`,
	}
	for i, k := range cases {
		out, err := json.Marshal(k)
		require.NoError(t, err)
		assert.Equal(t, wants[i], string(out))
		assert.Equal(t, strings.Trim(wants[i], `"`), k.String())
	}
}

// libs/ontology-kernel/src/models/action_type.rs `ActionFormSection`
// custom serde defaults: `columns` → 1, `visible` → true.
func TestActionFormSectionDefaults(t *testing.T) {
	var s ActionFormSection
	require.NoError(t, json.Unmarshal([]byte(`{"id":"section-1"}`), &s))
	assert.Equal(t, "section-1", s.ID)
	assert.Equal(t, uint8(1), s.Columns)
	assert.True(t, s.Visible)
	assert.False(t, s.Collapsible)

	// Explicit values win over defaults.
	require.NoError(t, json.Unmarshal([]byte(`{"id":"section-2","columns":3,"visible":false}`), &s))
	assert.Equal(t, uint8(3), s.Columns)
	assert.False(t, s.Visible)
}

// libs/ontology-kernel/src/models/action_type.rs `ActionFormCondition`
// `operator` defaults to `"is"`.
func TestActionFormConditionOperatorDefault(t *testing.T) {
	var c ActionFormCondition
	require.NoError(t, json.Unmarshal([]byte(`{"left":"status"}`), &c))
	assert.Equal(t, "is", c.Operator)

	require.NoError(t, json.Unmarshal([]byte(`{"left":"status","operator":"in"}`), &c))
	assert.Equal(t, "in", c.Operator)
}

// libs/ontology-kernel/src/models/action_type.rs
// `ActionAuthorizationPolicy` — every Vec/HashMap is `omitempty` so
// the zero value serialises as `{"deny_guest_sessions":false}`.
func TestActionAuthorizationPolicyZeroValueShape(t *testing.T) {
	out, err := json.Marshal(ActionAuthorizationPolicy{})
	require.NoError(t, err)
	s := string(out)
	for _, k := range []string{
		"required_permission_keys", "any_role", "all_roles",
		"attribute_equals", "allowed_markings", "minimum_clearance",
	} {
		assert.NotContains(t, s, k)
	}
	assert.Contains(t, s, `"deny_guest_sessions":false`)
}

// libs/ontology-kernel/src/models/action_type.rs
// `ActionFormSchema` — `sections` and `parameter_overrides` are both
// `skip_serializing_if = "Vec::is_empty"`. Empty schema → `{}`.
func TestActionFormSchemaEmptyOmits(t *testing.T) {
	out, err := json.Marshal(ActionFormSchema{})
	require.NoError(t, err)
	assert.Equal(t, `{}`, string(out))
}

// libs/ontology-kernel/src/models/object_view.rs
// `ObjectScenarioSimulationRequest` — every Vec is `serde(default)`,
// `include_baseline` defaults to `true`.
func TestObjectScenarioSimulationRequestDefaults(t *testing.T) {
	var r ObjectScenarioSimulationRequest
	require.NoError(t, json.Unmarshal([]byte(`{}`), &r))
	require.NotNil(t, r.Scenarios)
	require.NotNil(t, r.Constraints)
	require.NotNil(t, r.Goals)
	assert.True(t, r.IncludeBaseline)
	assert.Empty(t, r.Scenarios)

	// Explicit `false` overrides the default.
	require.NoError(t, json.Unmarshal([]byte(`{"include_baseline": false}`), &r))
	assert.False(t, r.IncludeBaseline)
}

// libs/ontology-kernel/src/models/object_view.rs `ScenarioGoalSpec`
// `weight` defaults to 1.0.
func TestScenarioGoalSpecWeightDefault(t *testing.T) {
	var g ScenarioGoalSpec
	body := `{"name":"g","metric":"m","comparator":"gt"}`
	require.NoError(t, json.Unmarshal([]byte(body), &g))
	assert.Equal(t, 1.0, g.Weight)

	body = `{"name":"g","metric":"m","comparator":"gt","weight":0.25}`
	require.NoError(t, json.Unmarshal([]byte(body), &g))
	assert.Equal(t, 0.25, g.Weight)
}

// libs/ontology-kernel/src/models/object_view.rs
// `ScenarioSimulationCandidate.operations` default to empty Vec.
func TestScenarioSimulationCandidateOperationsDefault(t *testing.T) {
	var c ScenarioSimulationCandidate
	require.NoError(t, json.Unmarshal([]byte(`{"name":"baseline"}`), &c))
	require.NotNil(t, c.Operations)
	assert.Empty(t, c.Operations)
}

// libs/ontology-kernel/src/models/function_authoring.rs round-trip:
// nested FunctionCapabilities default semantics are preserved when a
// template is materialised from a partial payload.
func TestFunctionAuthoringTemplateNestedCapabilities(t *testing.T) {
	body := `{
	  "id": "rust-starter",
	  "runtime": "wasm",
	  "display_name": "Rust",
	  "description": "starter",
	  "entrypoint": "main",
	  "starter_source": "fn main(){}",
	  "default_capabilities": {},
	  "recommended_use_cases": ["a","b"],
	  "cli_scaffold_template": null,
	  "sdk_packages": []
	}`
	var tpl FunctionAuthoringTemplate
	require.NoError(t, json.Unmarshal([]byte(body), &tpl))
	// Nested empty payload → per-field serde defaults.
	assert.True(t, tpl.DefaultCapabilities.AllowOntologyRead)
	assert.False(t, tpl.DefaultCapabilities.AllowOntologyWrite)
	assert.Equal(t, uint64(15), tpl.DefaultCapabilities.TimeoutSeconds)
}

// libs/ontology-kernel/src/models/function_metrics.rs — round-trip
// shape covers all fields and pointer-typed durations.
func TestFunctionPackageMetricsResponseShape(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	resp := FunctionPackageMetricsResponse{
		Package:        FunctionPackageSummary{ID: id, Name: "p", Capabilities: DefaultFunctionCapabilities()},
		TotalRuns:      10,
		SuccessfulRuns: 8,
		FailedRuns:     2,
		SuccessRate:    0.8,
	}
	out, err := json.Marshal(resp)
	require.NoError(t, err)
	s := string(out)
	for _, k := range []string{
		`"package":`, `"total_runs":10`, `"successful_runs":8`,
		`"failed_runs":2`, `"simulation_runs":0`, `"action_runs":0`,
		`"success_rate":0.8`, `"avg_duration_ms":null`, `"p95_duration_ms":null`,
		`"max_duration_ms":null`, `"last_run_at":null`,
	} {
		assert.True(t, strings.Contains(s, k), "missing %s", k)
	}
}

// libs/ontology-kernel/src/models/object_type_binding.rs
// `TryFrom<&str>` binds `other` to the result of `value.trim()`, so the
// error message embeds the trimmed token, not the untrimmed input.
func TestParseObjectTypeBindingSyncModeErrorTrims(t *testing.T) {
	_, err := ParseObjectTypeBindingSyncMode("  bogus  ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sync_mode 'bogus' is not supported")
	assert.NotContains(t, err.Error(), "  bogus  ")
}

// libs/ontology-kernel/src/models/function_package.rs
// `FunctionPackage.Summary()` mirrors `From<&FunctionPackage>`.
func TestFunctionPackageSummary(t *testing.T) {
	p := FunctionPackage{
		ID:           uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Name:         "n",
		Version:      "0.1.0",
		DisplayName:  "N",
		Runtime:      "wasm",
		Entrypoint:   "main",
		Capabilities: DefaultFunctionCapabilities(),
	}
	s := p.Summary()
	assert.Equal(t, p.ID, s.ID)
	assert.Equal(t, p.Name, s.Name)
	assert.Equal(t, p.Version, s.Version)
	assert.Equal(t, p.DisplayName, s.DisplayName)
	assert.Equal(t, p.Runtime, s.Runtime)
	assert.Equal(t, p.Entrypoint, s.Entrypoint)
	assert.Equal(t, p.Capabilities, s.Capabilities)
}
