package models

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// libs/ontology-kernel/src/models/funnel.rs
// `fn normalize_preview_limit(value).unwrap_or_else(default_preview_limit).clamp(1, 1_000)`.
func TestNormalizePreviewLimit(t *testing.T) {
	v := int32(0)
	assert.Equal(t, int32(500), NormalizePreviewLimit(nil))
	assert.Equal(t, int32(1), NormalizePreviewLimit(&v)) // 0 clamps to 1
	v = 5000
	assert.Equal(t, int32(1000), NormalizePreviewLimit(&v))
	v = 250
	assert.Equal(t, int32(250), NormalizePreviewLimit(&v))
}

// libs/ontology-kernel/src/models/funnel.rs
// `fn normalize_stale_after_hours(value).unwrap_or(24).clamp(1, 24*30)`.
func TestNormalizeStaleAfterHours(t *testing.T) {
	assert.Equal(t, int64(24), NormalizeStaleAfterHours(nil))
	v := int64(0)
	assert.Equal(t, int64(1), NormalizeStaleAfterHours(&v))
	v = 24 * 30
	assert.Equal(t, int64(720), NormalizeStaleAfterHours(&v))
	v = 24*30 + 1
	assert.Equal(t, int64(720), NormalizeStaleAfterHours(&v))
}

// libs/ontology-kernel/src/models/funnel.rs default_funnel_status / default_marking.
func TestFunnelStatusAndMarkingDefaults(t *testing.T) {
	assert.Equal(t, "active", NormalizeFunnelStatus(nil))
	assert.Equal(t, "public", NormalizeDefaultMarking(nil))

	s := "paused"
	assert.Equal(t, "paused", NormalizeFunnelStatus(&s))
	m := "private"
	assert.Equal(t, "private", NormalizeDefaultMarking(&m))
}

// libs/ontology-kernel/src/models/funnel.rs `UpdateOntologyFunnelSourceRequest`
// three-way semantics: `pipeline_id`, `dataset_branch`, `dataset_version`
// are `Option<Option<T>>` — absent omits, null clears, value replaces.
func TestUpdateOntologyFunnelSourceThreeWay(t *testing.T) {
	// Absent on the wire → all three optional updaters stay nil.
	var r UpdateOntologyFunnelSourceRequest
	require.NoError(t, json.Unmarshal([]byte(`{}`), &r))
	assert.Nil(t, r.PipelineID)
	assert.Nil(t, r.DatasetBranch)
	assert.Nil(t, r.DatasetVersion)

	// Null on the wire → updaters present, Value nil (clear).
	r = UpdateOntologyFunnelSourceRequest{}
	body := `{"pipeline_id": null, "dataset_branch": null, "dataset_version": null}`
	require.NoError(t, json.Unmarshal([]byte(body), &r))
	require.NotNil(t, r.PipelineID)
	assert.Nil(t, r.PipelineID.Value)
	require.NotNil(t, r.DatasetBranch)
	assert.Nil(t, r.DatasetBranch.Value)
	require.NotNil(t, r.DatasetVersion)
	assert.Nil(t, r.DatasetVersion.Value)

	// Value on the wire → updaters present, Value non-nil.
	r = UpdateOntologyFunnelSourceRequest{}
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	body = `{"pipeline_id": "11111111-1111-1111-1111-111111111111", "dataset_branch": "main", "dataset_version": 7}`
	require.NoError(t, json.Unmarshal([]byte(body), &r))
	require.NotNil(t, r.PipelineID.Value)
	assert.Equal(t, id, *r.PipelineID.Value)
	require.NotNil(t, r.DatasetBranch.Value)
	assert.Equal(t, "main", *r.DatasetBranch.Value)
	require.NotNil(t, r.DatasetVersion.Value)
	assert.Equal(t, int32(7), *r.DatasetVersion.Value)

	// Round-trip: absent omits, null clears, value replaces.
	absent, err := json.Marshal(UpdateOntologyFunnelSourceRequest{})
	require.NoError(t, err)
	for _, k := range []string{"pipeline_id", "dataset_branch", "dataset_version"} {
		assert.NotContains(t, string(absent), k)
	}

	cleared, err := json.Marshal(UpdateOntologyFunnelSourceRequest{
		PipelineID:     &UUIDUpdate{},
		DatasetBranch:  &StringUpdate{},
		DatasetVersion: &Int32Update{},
	})
	require.NoError(t, err)
	assert.Contains(t, string(cleared), `"pipeline_id":null`)
	assert.Contains(t, string(cleared), `"dataset_branch":null`)
	assert.Contains(t, string(cleared), `"dataset_version":null`)
}

// libs/ontology-kernel/src/models/object_type_binding.rs `ObjectTypeBindingSyncMode`
// has `#[serde(rename_all = "snake_case")]`.
func TestObjectTypeBindingSyncModeSnakeCase(t *testing.T) {
	for _, m := range []ObjectTypeBindingSyncMode{
		ObjectTypeBindingSyncModeSnapshot,
		ObjectTypeBindingSyncModeIncremental,
		ObjectTypeBindingSyncModeView,
	} {
		out, err := json.Marshal(m)
		require.NoError(t, err)
		// Each token is the lowercased single-word name.
		assert.Equal(t, `"`+m.AsStr()+`"`, string(out))
	}
}

// libs/ontology-kernel/src/models/object_type_binding.rs
// `TryFrom<&str>` returns a verbatim error string for unknown tokens.
func TestParseObjectTypeBindingSyncModeError(t *testing.T) {
	cases := []string{"bogus", " soft ", "INCREMENTAL"}
	for _, in := range cases {
		_, err := ParseObjectTypeBindingSyncMode(in)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is not supported; expected one of: snapshot, incremental, view")
	}

	// Whitespace is trimmed before matching, mirroring `value.trim()`.
	mode, err := ParseObjectTypeBindingSyncMode("  snapshot  ")
	require.NoError(t, err)
	assert.Equal(t, ObjectTypeBindingSyncModeSnapshot, mode)
}

// libs/ontology-kernel/src/models/object_type_binding.rs
// `CreateObjectTypeBindingRequest.property_mapping` is `#[serde(default)]`
// — missing key decodes to empty slice, never nil.
func TestCreateObjectTypeBindingPropertyMappingDefault(t *testing.T) {
	body := `{
	  "dataset_id": "22222222-2222-2222-2222-222222222222",
	  "primary_key_column": "id",
	  "sync_mode": "snapshot"
	}`
	var r CreateObjectTypeBindingRequest
	require.NoError(t, json.Unmarshal([]byte(body), &r))
	require.NotNil(t, r.PropertyMapping)
	assert.Equal(t, []ObjectTypeBindingPropertyMapping{}, r.PropertyMapping)
}

// libs/ontology-kernel/src/models/object_type_binding.rs
// `MaterializeBindingResponse.error_details` is
// `#[serde(skip_serializing_if = "Vec::is_empty")]` — empty omits.
func TestMaterializeBindingResponseOmitsEmptyErrorDetails(t *testing.T) {
	resp := MaterializeBindingResponse{
		BindingID: uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		Status:    "ok",
	}
	out, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.NotContains(t, string(out), "error_details")

	resp.ErrorDetails = []json.RawMessage{json.RawMessage(`{"row":1}`)}
	out, err = json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"error_details":[{"row":1}]`)
}

// libs/ontology-kernel/src/models/project.rs
// `OntologyProjectRole::rank` returns 1/2/3 for viewer/editor/owner.
func TestOntologyProjectRoleRank(t *testing.T) {
	assert.Equal(t, uint8(1), OntologyProjectRoleViewer.Rank())
	assert.Equal(t, uint8(2), OntologyProjectRoleEditor.Rank())
	assert.Equal(t, uint8(3), OntologyProjectRoleOwner.Rank())

	// Role tokens are snake_case (single lowercase words here).
	out, err := json.Marshal(OntologyProjectRoleEditor)
	require.NoError(t, err)
	assert.Equal(t, `"editor"`, string(out))
}

// libs/ontology-kernel/src/models/project.rs
// `UpdateOntologyProjectRequest.workspace_slug: Option<Option<String>>`.
func TestUpdateOntologyProjectWorkspaceSlugThreeWay(t *testing.T) {
	var r UpdateOntologyProjectRequest
	require.NoError(t, json.Unmarshal([]byte(`{}`), &r))
	assert.Nil(t, r.WorkspaceSlug)

	r = UpdateOntologyProjectRequest{}
	require.NoError(t, json.Unmarshal([]byte(`{"workspace_slug": null}`), &r))
	require.NotNil(t, r.WorkspaceSlug)
	assert.Nil(t, r.WorkspaceSlug.Value)

	r = UpdateOntologyProjectRequest{}
	require.NoError(t, json.Unmarshal([]byte(`{"workspace_slug": "alpha"}`), &r))
	require.NotNil(t, r.WorkspaceSlug)
	require.NotNil(t, r.WorkspaceSlug.Value)
	assert.Equal(t, "alpha", *r.WorkspaceSlug.Value)

	absent, err := json.Marshal(UpdateOntologyProjectRequest{})
	require.NoError(t, err)
	assert.NotContains(t, string(absent), "workspace_slug")

	cleared, err := json.Marshal(UpdateOntologyProjectRequest{WorkspaceSlug: &StringUpdate{}})
	require.NoError(t, err)
	assert.Contains(t, string(cleared), `"workspace_slug":null`)

	v := "beta"
	replaced, err := json.Marshal(UpdateOntologyProjectRequest{WorkspaceSlug: &StringUpdate{Value: &v}})
	require.NoError(t, err)
	assert.Contains(t, string(replaced), `"workspace_slug":"beta"`)
}

// libs/ontology-kernel/src/models/project.rs
// `UpdateOntologyProjectBranchRequest.proposal_id: Option<Option<Uuid>>`.
func TestUpdateOntologyProjectBranchProposalIDThreeWay(t *testing.T) {
	var r UpdateOntologyProjectBranchRequest
	require.NoError(t, json.Unmarshal([]byte(`{}`), &r))
	assert.Nil(t, r.ProposalID)

	r = UpdateOntologyProjectBranchRequest{}
	require.NoError(t, json.Unmarshal([]byte(`{"proposal_id": null}`), &r))
	require.NotNil(t, r.ProposalID)
	assert.Nil(t, r.ProposalID.Value)

	r = UpdateOntologyProjectBranchRequest{}
	body := `{"proposal_id": "44444444-4444-4444-4444-444444444444"}`
	require.NoError(t, json.Unmarshal([]byte(body), &r))
	require.NotNil(t, r.ProposalID.Value)
	assert.Equal(t, uuid.MustParse("44444444-4444-4444-4444-444444444444"), *r.ProposalID.Value)

	absent, err := json.Marshal(UpdateOntologyProjectBranchRequest{})
	require.NoError(t, err)
	assert.NotContains(t, string(absent), "proposal_id")

	cleared, err := json.Marshal(UpdateOntologyProjectBranchRequest{ProposalID: &UUIDUpdate{}})
	require.NoError(t, err)
	assert.Contains(t, string(cleared), `"proposal_id":null`)
}

// libs/ontology-kernel/src/models/rule.rs `RuleTriggerSpec` —
// every field is `#[serde(default)]`, so empty payload decodes
// and re-encodes as `{}` / `[]`, never `null`.
func TestRuleTriggerSpecDefaultsRoundTrip(t *testing.T) {
	var spec RuleTriggerSpec
	require.NoError(t, json.Unmarshal([]byte(`{}`), &spec))
	assert.NotNil(t, spec.Equals)
	assert.NotNil(t, spec.NumericGte)
	assert.NotNil(t, spec.NumericLte)
	assert.NotNil(t, spec.Exists)
	assert.NotNil(t, spec.ChangedProperties)
	assert.NotNil(t, spec.Markings)

	out, err := json.Marshal(spec)
	require.NoError(t, err)
	s := string(out)
	for _, want := range []string{
		`"equals":{}`, `"numeric_gte":{}`, `"numeric_lte":{}`,
		`"exists":[]`, `"changed_properties":[]`, `"markings":[]`,
	} {
		assert.True(t, strings.Contains(s, want), "want %s in %s", want, s)
	}
}

// libs/ontology-kernel/src/models/rule.rs `RuleEvaluationMode` is
// snake_case + Display matches the wire token.
func TestRuleEvaluationModeSnakeCase(t *testing.T) {
	out, err := json.Marshal(RuleEvaluationModeAdvisory)
	require.NoError(t, err)
	assert.Equal(t, `"advisory"`, string(out))
	assert.Equal(t, "advisory", RuleEvaluationModeAdvisory.String())

	out, err = json.Marshal(RuleEvaluationModeAutomatic)
	require.NoError(t, err)
	assert.Equal(t, `"automatic"`, string(out))
	assert.Equal(t, "automatic", RuleEvaluationModeAutomatic.String())
}

// libs/ontology-kernel/src/models/rule.rs `OntologyRuleRow::try_from`
// — evaluation_mode falls back to Advisory on parse failure;
// trigger_spec / effect_spec fall back to default on failure.
func TestOntologyRuleRowFallbacks(t *testing.T) {
	row := OntologyRuleRow{EvaluationMode: "garbage"}
	r, err := row.IntoRule()
	require.NoError(t, err)
	assert.Equal(t, RuleEvaluationModeAdvisory, r.EvaluationMode)
	// Trigger spec defaults applied on the fallback path.
	assert.NotNil(t, r.TriggerSpec.Equals)
	assert.NotNil(t, r.TriggerSpec.Exists)
}
