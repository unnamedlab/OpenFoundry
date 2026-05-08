package models

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// libs/ontology-kernel/src/models/property.rs
// `Option<Option<PropertyInlineEditConfig>>` three-way semantics.
func TestPropertyInlineEditConfigUpdateThreeWay(t *testing.T) {
	// Field absent.
	var r UpdatePropertyRequest
	require.NoError(t, json.Unmarshal([]byte(`{}`), &r))
	assert.Nil(t, r.InlineEditConfig)

	// Field present, null → clear (set with no value).
	r = UpdatePropertyRequest{}
	require.NoError(t, json.Unmarshal([]byte(`{"inline_edit_config": null}`), &r))
	require.NotNil(t, r.InlineEditConfig)
	assert.Nil(t, r.InlineEditConfig.Value)

	// Field present, value → replace.
	r = UpdatePropertyRequest{}
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	body := `{"inline_edit_config": {"action_type_id": "11111111-1111-1111-1111-111111111111", "input_name": "amount"}}`
	require.NoError(t, json.Unmarshal([]byte(body), &r))
	require.NotNil(t, r.InlineEditConfig)
	require.NotNil(t, r.InlineEditConfig.Value)
	assert.Equal(t, id, r.InlineEditConfig.Value.ActionTypeID)
	require.NotNil(t, r.InlineEditConfig.Value.InputName)
	assert.Equal(t, "amount", *r.InlineEditConfig.Value.InputName)

	// Round-trip emits the right shape: absent → omitted, null → null,
	// value → object.
	absent, err := json.Marshal(UpdatePropertyRequest{})
	require.NoError(t, err)
	assert.NotContains(t, string(absent), "inline_edit_config")

	cleared, err := json.Marshal(UpdatePropertyRequest{InlineEditConfig: &PropertyInlineEditConfigUpdate{}})
	require.NoError(t, err)
	assert.Contains(t, string(cleared), `"inline_edit_config":null`)

	replaced, err := json.Marshal(UpdatePropertyRequest{
		InlineEditConfig: &PropertyInlineEditConfigUpdate{Value: &PropertyInlineEditConfig{ActionTypeID: id}},
	})
	require.NoError(t, err)
	assert.Contains(t, string(replaced), `"inline_edit_config":{"action_type_id":`)
}

// libs/ontology-kernel/src/models/property.rs `PropertyInlineEditConfig`
// honours `skip_serializing_if = "Option::is_none"` on `input_name`.
func TestPropertyInlineEditConfigOmitsInputName(t *testing.T) {
	cfg := PropertyInlineEditConfig{
		ActionTypeID: uuid.MustParse("22222222-2222-2222-2222-222222222222"),
	}
	b, err := json.Marshal(cfg)
	require.NoError(t, err)
	assert.Equal(t, `{"action_type_id":"22222222-2222-2222-2222-222222222222"}`, string(b))
}

// libs/ontology-kernel/src/models/quiver.rs default_chart_kind() == "line".
func TestQuiverDefaultChartKind(t *testing.T) {
	assert.Equal(t, "line", DefaultChartKind())
}

// libs/ontology-kernel/src/models/quiver.rs into_draft() applies all
// the unwrap_or_default / unwrap_or_else fallbacks.
func TestCreateQuiverIntoDraftDefaults(t *testing.T) {
	r := CreateQuiverVisualFunctionRequest{
		Name:          "weekly",
		PrimaryTypeID: uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		JoinField:     "id",
		DateField:     "ts",
		MetricField:   "amount",
		GroupField:    "country",
	}
	d := r.IntoDraft()
	assert.Equal(t, "", d.Description)
	assert.Equal(t, "", d.SecondaryJoinField)
	assert.Equal(t, "line", d.ChartKind)
	assert.Equal(t, false, d.Shared)
}

// libs/ontology-kernel/src/models/quiver.rs draft serde defaults: empty
// payload still yields chart_kind="line" via #[serde(default)].
func TestQuiverDraftDefaultChartKindOnDecode(t *testing.T) {
	body := `{
	  "name": "x",
	  "primary_type_id": "33333333-3333-3333-3333-333333333333",
	  "join_field": "id",
	  "date_field": "ts",
	  "metric_field": "amount",
	  "group_field": "country"
	}`
	var d QuiverVisualFunctionDraft
	require.NoError(t, json.Unmarshal([]byte(body), &d))
	assert.Equal(t, "line", d.ChartKind)
}

// libs/ontology-kernel/src/models/quiver.rs UpdateQuiverVisualFunctionRequest
// `selected_group: Option<Option<String>>` three-way semantics.
func TestUpdateQuiverSelectedGroupThreeWay(t *testing.T) {
	// Absent.
	var r UpdateQuiverVisualFunctionRequest
	require.NoError(t, json.Unmarshal([]byte(`{}`), &r))
	assert.Nil(t, r.SelectedGroup)

	// Null → clear.
	r = UpdateQuiverVisualFunctionRequest{}
	require.NoError(t, json.Unmarshal([]byte(`{"selected_group": null}`), &r))
	require.NotNil(t, r.SelectedGroup)
	assert.Nil(t, r.SelectedGroup.Value)

	// Value → replace.
	r = UpdateQuiverVisualFunctionRequest{}
	require.NoError(t, json.Unmarshal([]byte(`{"selected_group": "EU"}`), &r))
	require.NotNil(t, r.SelectedGroup)
	require.NotNil(t, r.SelectedGroup.Value)
	assert.Equal(t, "EU", *r.SelectedGroup.Value)

	// Round-trip: absent omits, null emits null, value emits string.
	absent, err := json.Marshal(UpdateQuiverVisualFunctionRequest{})
	require.NoError(t, err)
	assert.NotContains(t, string(absent), "selected_group")

	cleared, err := json.Marshal(UpdateQuiverVisualFunctionRequest{SelectedGroup: &StringUpdate{}})
	require.NoError(t, err)
	assert.Contains(t, string(cleared), `"selected_group":null`)

	v := "APAC"
	replaced, err := json.Marshal(UpdateQuiverVisualFunctionRequest{SelectedGroup: &StringUpdate{Value: &v}})
	require.NoError(t, err)
	assert.Contains(t, string(replaced), `"selected_group":"APAC"`)
}

// libs/ontology-kernel/src/models/object_set.rs ObjectSetPolicy
// allowed_markings always serialises as `[]` even when zero-valued.
func TestObjectSetPolicyEmptyMarkingsRoundTrip(t *testing.T) {
	var p ObjectSetPolicy
	require.NoError(t, json.Unmarshal([]byte(`{}`), &p))
	assert.Equal(t, []string{}, p.AllowedMarkings)
	assert.False(t, p.DenyGuestSessions)

	b, err := json.Marshal(p)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"allowed_markings":[]`)
}

// libs/ontology-kernel/src/models/object_set.rs ListObjectSetsResponse
// next_token is gated on serde skip_serializing_if = Option::is_none.
func TestListObjectSetsResponseOmitsNextToken(t *testing.T) {
	resp := ListObjectSetsResponse{Data: []ObjectSetDefinition{}}
	b, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "next_token")
}

// libs/ontology-kernel/src/models/submission_criteria.rs round-trip
// the same shape as the Rust round_trip_serde_preserves_shape test.
func TestSubmissionNodeRoundTrip(t *testing.T) {
	must := func(s string) *string { return &s }

	editor := json.RawMessage(`"ontology.editor"`)
	thousand := json.RawMessage(`1000`)

	node := SubmissionNode{
		Type:           SubmissionNodeTypeAll,
		FailureMessage: must("policy"),
		Children: []SubmissionNode{
			{
				Type: SubmissionNodeTypeLeaf,
				Left: &Operand{
					Kind: OperandKindCurrentUser,
					User: &OperandCurrentUser{Attribute: UserAttrRoles},
				},
				Op: OperatorIncludes,
				Right: &Operand{
					Kind:   OperandKindStatic,
					Static: &OperandStatic{Value: editor},
				},
				FailureMessage: must("must be editor"),
			},
			{
				Type: SubmissionNodeTypeNot,
				Child: &SubmissionNode{
					Type: SubmissionNodeTypeLeaf,
					Left: &Operand{
						Kind:  OperandKindParam,
						Param: &OperandParam{Name: "amount"},
					},
					Op: OperatorGt,
					Right: &Operand{
						Kind:   OperandKindStatic,
						Static: &OperandStatic{Value: thousand},
					},
				},
			},
		},
	}

	raw, err := json.Marshal(node)
	require.NoError(t, err)
	// Spot-check tag discriminants and snake_case enum values.
	s := string(raw)
	assert.Contains(t, s, `"type":"all"`)
	assert.Contains(t, s, `"type":"leaf"`)
	assert.Contains(t, s, `"type":"not"`)
	assert.Contains(t, s, `"kind":"current_user"`)
	assert.Contains(t, s, `"attribute":"roles"`)
	assert.Contains(t, s, `"op":"includes"`)
	assert.Contains(t, s, `"op":"gt"`)
	assert.Contains(t, s, `"failure_message":"policy"`)
	assert.Contains(t, s, `"failure_message":"must be editor"`)

	var back SubmissionNode
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, SubmissionNodeTypeAll, back.Type)
	require.Len(t, back.Children, 2)
	assert.Equal(t, OperatorIncludes, back.Children[0].Op)
	assert.Equal(t, OperandKindParam, back.Children[1].Child.Left.Kind)
	require.NotNil(t, back.FailureMessage)
	assert.Equal(t, "policy", *back.FailureMessage)
}

// Submission criteria operand discriminant snake_case is verbatim Rust:
// param / param_property / current_user / static, never camelCase.
func TestOperandSnakeCase(t *testing.T) {
	cases := []struct {
		name    string
		op      Operand
		want    string
		notWant string
	}{
		{
			"param",
			Operand{Kind: OperandKindParam, Param: &OperandParam{Name: "x"}},
			`"kind":"param"`, `"kind":"Param"`,
		},
		{
			"param_property",
			Operand{Kind: OperandKindParamProperty, ParamProp: &OperandParamProperty{Param: "p", Property: "q"}},
			`"kind":"param_property"`, `"kind":"paramProperty"`,
		},
		{
			"current_user",
			Operand{Kind: OperandKindCurrentUser, User: &OperandCurrentUser{Attribute: UserAttrEmail}},
			`"kind":"current_user"`, `"kind":"currentUser"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.op)
			require.NoError(t, err)
			s := string(b)
			assert.True(t, strings.Contains(s, tc.want), "want %s got %s", tc.want, s)
			assert.False(t, strings.Contains(s, tc.notWant))
		})
	}
}
