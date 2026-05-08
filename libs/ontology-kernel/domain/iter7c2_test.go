package domain

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

// ---- submission_eval.go --------------------------------------------------

func paramRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(value)
	require.NoError(t, err)
	return b
}

func sampleClaims(roles ...string) *authmw.Claims {
	return &authmw.Claims{
		Sub:   uuid.Nil,
		Email: "alice@example.com",
		Roles: append([]string(nil), roles...),
	}
}

func evalCtx(params map[string]json.RawMessage, claims *authmw.Claims) *EvaluationContext {
	if params == nil {
		params = map[string]json.RawMessage{}
	}
	return &EvaluationContext{Parameters: params, Claims: claims}
}

// libs/ontology-kernel/src/domain/submission_eval.rs
// `leaf_is_passes_when_param_matches_literal`.
func TestEvaluateSubmissionLeafIsPassesWhenParamMatches(t *testing.T) {
	params := map[string]json.RawMessage{"status": paramRaw(t, "approved")}
	node := models.NewLeaf(
		models.Operand{Kind: models.OperandKindParam, Param: &models.OperandParam{Name: "status"}},
		models.OperatorIs,
		models.Operand{Kind: models.OperandKindStatic, Static: &models.OperandStatic{Value: paramRaw(t, "approved")}},
	)
	assert.Nil(t, EvaluateSubmission(node, evalCtx(params, sampleClaims())))
}

// libs/ontology-kernel/src/domain/submission_eval.rs
// `leaf_failure_uses_authored_message`.
func TestEvaluateSubmissionLeafFailureUsesAuthoredMessage(t *testing.T) {
	msg := "editor role required"
	node := models.SubmissionNode{
		Type: models.SubmissionNodeTypeLeaf,
		Left: &models.Operand{
			Kind: models.OperandKindCurrentUser,
			User: &models.OperandCurrentUser{Attribute: models.UserAttrRoles},
		},
		Op: models.OperatorIncludes,
		Right: &models.Operand{
			Kind:   models.OperandKindStatic,
			Static: &models.OperandStatic{Value: paramRaw(t, "ontology.editor")},
		},
		FailureMessage: &msg,
	}
	errs := EvaluateSubmission(node, evalCtx(nil, sampleClaims("viewer")))
	assert.Equal(t, []string{"editor role required"}, errs)
}

// libs/ontology-kernel/src/domain/submission_eval.rs
// `all_uses_parent_message_and_suppresses_children`.
func TestEvaluateSubmissionAllParentMessageOverridesChildren(t *testing.T) {
	policy := "policy violated"
	childMsg := "admin needed"
	node := models.SubmissionNode{
		Type:           models.SubmissionNodeTypeAll,
		FailureMessage: &policy,
		Children: []models.SubmissionNode{
			{
				Type: models.SubmissionNodeTypeLeaf,
				Left: &models.Operand{
					Kind: models.OperandKindCurrentUser,
					User: &models.OperandCurrentUser{Attribute: models.UserAttrRoles},
				},
				Op: models.OperatorIncludes,
				Right: &models.Operand{
					Kind:   models.OperandKindStatic,
					Static: &models.OperandStatic{Value: paramRaw(t, "admin")},
				},
				FailureMessage: &childMsg,
			},
		},
	}
	errs := EvaluateSubmission(node, evalCtx(nil, sampleClaims()))
	assert.Equal(t, []string{"policy violated"}, errs)
}

// libs/ontology-kernel/src/domain/submission_eval.rs
// `all_without_message_surfaces_child_messages`.
func TestEvaluateSubmissionAllSurfacesChildMessages(t *testing.T) {
	a := "need admin"
	b := "not alice"
	node := models.SubmissionNode{
		Type: models.SubmissionNodeTypeAll,
		Children: []models.SubmissionNode{
			{
				Type: models.SubmissionNodeTypeLeaf,
				Left: &models.Operand{Kind: models.OperandKindCurrentUser, User: &models.OperandCurrentUser{Attribute: models.UserAttrRoles}},
				Op:   models.OperatorIncludes,
				Right: &models.Operand{Kind: models.OperandKindStatic, Static: &models.OperandStatic{Value: paramRaw(t, "admin")}},
				FailureMessage: &a,
			},
			{
				Type: models.SubmissionNodeTypeLeaf,
				Left: &models.Operand{Kind: models.OperandKindCurrentUser, User: &models.OperandCurrentUser{Attribute: models.UserAttrEmail}},
				Op:   models.OperatorIsNot,
				Right: &models.Operand{Kind: models.OperandKindStatic, Static: &models.OperandStatic{Value: paramRaw(t, "alice@example.com")}},
				FailureMessage: &b,
			},
		},
	}
	errs := EvaluateSubmission(node, evalCtx(nil, sampleClaims()))
	assert.Equal(t, []string{"need admin", "not alice"}, errs)
}

// libs/ontology-kernel/src/domain/submission_eval.rs
// `any_short_circuits_on_first_success`.
func TestEvaluateSubmissionAnyShortCircuits(t *testing.T) {
	node := models.SubmissionNode{
		Type: models.SubmissionNodeTypeAny,
		Children: []models.SubmissionNode{
			{
				Type: models.SubmissionNodeTypeLeaf,
				Left: &models.Operand{Kind: models.OperandKindCurrentUser, User: &models.OperandCurrentUser{Attribute: models.UserAttrRoles}},
				Op:   models.OperatorIncludes,
				Right: &models.Operand{Kind: models.OperandKindStatic, Static: &models.OperandStatic{Value: paramRaw(t, "admin")}},
			},
			{
				Type: models.SubmissionNodeTypeLeaf,
				Left: &models.Operand{Kind: models.OperandKindCurrentUser, User: &models.OperandCurrentUser{Attribute: models.UserAttrRoles}},
				Op:   models.OperatorIncludes,
				Right: &models.Operand{Kind: models.OperandKindStatic, Static: &models.OperandStatic{Value: paramRaw(t, "viewer")}},
			},
		},
	}
	assert.Nil(t, EvaluateSubmission(node, evalCtx(nil, sampleClaims("viewer"))))
}

// libs/ontology-kernel/src/domain/submission_eval.rs
// `not_inverts_inner_truthiness`.
func TestEvaluateSubmissionNotInverts(t *testing.T) {
	msg := "amount must be <= 100"
	mkNode := func() models.SubmissionNode {
		inner := models.SubmissionNode{
			Type: models.SubmissionNodeTypeLeaf,
			Left: &models.Operand{Kind: models.OperandKindParam, Param: &models.OperandParam{Name: "amount"}},
			Op:   models.OperatorGt,
			Right: &models.Operand{Kind: models.OperandKindStatic, Static: &models.OperandStatic{Value: paramRaw(t, 100)}},
		}
		return models.SubmissionNode{
			Type:           models.SubmissionNodeTypeNot,
			FailureMessage: &msg,
			Child:          &inner,
		}
	}
	// 50 ≤ 100 → inner is false → NOT passes.
	pass := evalCtx(map[string]json.RawMessage{"amount": paramRaw(t, 50)}, sampleClaims())
	assert.Nil(t, EvaluateSubmission(mkNode(), pass))
	// 150 > 100 → inner is true → NOT fails with the authored message.
	fail := evalCtx(map[string]json.RawMessage{"amount": paramRaw(t, 150)}, sampleClaims())
	assert.Equal(t, []string{"amount must be <= 100"}, EvaluateSubmission(mkNode(), fail))
}

// libs/ontology-kernel/src/domain/submission_eval.rs
// `matches_evaluates_regex_against_string`.
func TestEvaluateSubmissionMatchesRegex(t *testing.T) {
	node := models.NewLeaf(
		models.Operand{Kind: models.OperandKindParam, Param: &models.OperandParam{Name: "email"}},
		models.OperatorMatches,
		models.Operand{Kind: models.OperandKindStatic, Static: &models.OperandStatic{Value: paramRaw(t, `^[a-z]+@openfoundry\.test$`)}},
	)
	params := map[string]json.RawMessage{"email": paramRaw(t, "alice@openfoundry.test")}
	assert.Nil(t, EvaluateSubmission(node, evalCtx(params, sampleClaims())))
}

// libs/ontology-kernel/src/domain/submission_eval.rs
// `each_is_not_passes_when_no_array_element_matches`.
func TestEvaluateSubmissionEachIsNot(t *testing.T) {
	node := models.NewLeaf(
		models.Operand{Kind: models.OperandKindParam, Param: &models.OperandParam{Name: "tags"}},
		models.OperatorEachIsNot,
		models.Operand{Kind: models.OperandKindStatic, Static: &models.OperandStatic{Value: paramRaw(t, "red")}},
	)
	params := map[string]json.RawMessage{"tags": paramRaw(t, []string{"green", "yellow"})}
	assert.Nil(t, EvaluateSubmission(node, evalCtx(params, sampleClaims())))
}

// libs/ontology-kernel/src/domain/submission_eval.rs
// `dedupes_repeated_messages`.
func TestEvaluateSubmissionDedupes(t *testing.T) {
	msg := "admin needed"
	leaf := models.SubmissionNode{
		Type: models.SubmissionNodeTypeLeaf,
		Left: &models.Operand{Kind: models.OperandKindCurrentUser, User: &models.OperandCurrentUser{Attribute: models.UserAttrRoles}},
		Op:   models.OperatorIncludes,
		Right: &models.Operand{Kind: models.OperandKindStatic, Static: &models.OperandStatic{Value: paramRaw(t, "admin")}},
		FailureMessage: &msg,
	}
	node := models.SubmissionNode{
		Type:     models.SubmissionNodeTypeAll,
		Children: []models.SubmissionNode{leaf, leaf},
	}
	errs := EvaluateSubmission(node, evalCtx(nil, sampleClaims()))
	assert.Equal(t, []string{"admin needed"}, errs)
}

// ---- media_action_template.go --------------------------------------------

// libs/ontology-kernel/src/domain/media_action_template.rs
// `build_upload_media_action_input_pins_canonical_shape`.
func TestBuildUploadMediaActionInputCanonicalShape(t *testing.T) {
	f := BuildUploadMediaActionInput("photo", "Photo", "ri.foundry.main.media_set.aircraft")
	assert.Equal(t, "media_reference", f.PropertyType)
	assert.True(t, f.Required)
	var defaults map[string]string
	require.NoError(t, json.Unmarshal(f.DefaultValue, &defaults))
	assert.Equal(t, "ri.foundry.main.media_set.aircraft", defaults["media_set_rid"])
}

// libs/ontology-kernel/src/domain/media_action_template.rs
// `single_backing_set_emits_no_warning`.
func TestSingleBackingSetEmitsNoWarning(t *testing.T) {
	schema := []models.ActionInputField{
		BuildUploadMediaActionInput("photo", "Photo", "ri.foundry.main.media_set.aircraft"),
	}
	assert.Empty(t, BackingWarnings(schema))
}

// libs/ontology-kernel/src/domain/media_action_template.rs
// `two_distinct_backing_sets_emit_strong_discouragement`.
func TestTwoDistinctBackingSetsEmitWarning(t *testing.T) {
	schema := []models.ActionInputField{
		BuildUploadMediaActionInput("photo", "Photo", "ri.foundry.main.media_set.a"),
		BuildUploadMediaActionInput("scan", "Scan", "ri.foundry.main.media_set.b"),
	}
	warnings := BackingWarnings(schema)
	require.Len(t, warnings, 1)
	assert.Equal(t, MediaBackingMultipleSets, warnings[0].Code)
	assert.Len(t, warnings[0].DistinctSets, 2)
}

// libs/ontology-kernel/src/domain/media_action_template.rs
// `detects_pending_upload_placeholder` + camelCase parsing.
func TestDetectPendingUploadPlaceholder(t *testing.T) {
	body := json.RawMessage(`{
        "pendingUpload": true,
        "mediaSetRid": "ri.foundry.main.media_set.x",
        "fileName": "skyline.png",
        "mimeType": "image/png",
        "blobToken": "abc123"
    }`)
	parsed := TryParseMediaUploadPlaceholder(body)
	require.NotNil(t, parsed)
	assert.Equal(t, "ri.foundry.main.media_set.x", parsed.MediaSetRID)
	assert.Equal(t, "skyline.png", parsed.FileName)
	assert.Equal(t, "abc123", parsed.BlobToken)
}

// libs/ontology-kernel/src/domain/media_action_template.rs
// `detect_pending_uploads_finds_placeholder_inputs`.
func TestDetectPendingUploadsFindsPlaceholders(t *testing.T) {
	inputs := map[string]json.RawMessage{
		"photo": json.RawMessage(`{
            "pendingUpload": true,
            "mediaSetRid": "rid",
            "fileName": "x.png",
            "blobToken": "t"
        }`),
		"name": json.RawMessage(`"not a placeholder"`),
	}
	detected := DetectPendingUploads(inputs)
	require.Len(t, detected, 1)
	assert.Equal(t, "photo", detected[0].FieldName)
}

// libs/ontology-kernel/src/domain/media_action_template.rs
// `array_of_media_reference_is_warned_off`.
func TestArrayOfMediaReferenceIsWarnedOff(t *testing.T) {
	subFields := []models.ActionInputField{{Name: "ref", PropertyType: "media_reference"}}
	schema := []models.ActionInputField{{
		Name:         "photos",
		PropertyType: "array",
		StructFields: &subFields,
	}}
	warnings := BackingWarnings(schema)
	require.Len(t, warnings, 1)
	assert.Equal(t, MediaBackingListNotSupported, warnings[0].Code)
	assert.Equal(t, "photos", warnings[0].FieldName)
}

// ---- project_access.go ---------------------------------------------------

// libs/ontology-kernel/src/domain/project_access.rs
// `resource_kind_parser_accepts_supported_values`.
func TestParseOntologyResourceKind(t *testing.T) {
	kind, err := ParseOntologyResourceKind("action_type")
	require.NoError(t, err)
	assert.Equal(t, OntologyResourceKindActionType, kind)

	for _, valid := range []string{
		"object_type", "link_type", "interface", "shared_property_type",
		"action_type", "function_package", "rule", "object_set",
	} {
		_, err := ParseOntologyResourceKind(valid)
		assert.NoError(t, err, "kind %q should parse", valid)
	}

	_, err = ParseOntologyResourceKind("unknown")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resource_kind 'unknown' is not supported")
	assert.Contains(t, err.Error(), "object_type, link_type, interface, shared_property_type, action_type, function_package, rule, object_set")
}

// libs/ontology-kernel/src/domain/project_access.rs
// `workspace_slug_prefers_session_scope_over_attributes`.
func TestClaimsWorkspaceSlugPrefersSessionScope(t *testing.T) {
	wsScope := "project-alpha"
	claims := &authmw.Claims{
		Sub:        uuid.Nil,
		Email:      "user@example.com",
		Roles:      []string{"viewer"},
		Attributes: json.RawMessage(`{"workspace": "finance-lab"}`),
		SessionScope: &authmw.SessionScope{
			Workspace: &wsScope,
		},
	}
	assert.Equal(t, "project-alpha", ClaimsWorkspaceSlug(claims))
}

// libs/ontology-kernel/src/domain/project_access.rs
// `claims_workspace_slug` falls back to `workspace` attribute when
// session_scope.workspace is absent, then to `default_workspace`.
func TestClaimsWorkspaceSlugFallbackCascade(t *testing.T) {
	// No scope.workspace → workspace attribute wins.
	c := &authmw.Claims{Attributes: json.RawMessage(`{"workspace": "finance-lab"}`)}
	assert.Equal(t, "finance-lab", ClaimsWorkspaceSlug(c))

	// Neither workspace nor scope → default_workspace fallback.
	c = &authmw.Claims{Attributes: json.RawMessage(`{"default_workspace": "ops"}`)}
	assert.Equal(t, "ops", ClaimsWorkspaceSlug(c))

	// Nothing → empty string (Rust None).
	c = &authmw.Claims{}
	assert.Equal(t, "", ClaimsWorkspaceSlug(c))

	// Whitespace-only is treated as absent.
	c = &authmw.Claims{Attributes: json.RawMessage(`{"workspace": "   "}`)}
	assert.Equal(t, "", ClaimsWorkspaceSlug(c))
}

// libs/ontology-kernel/src/domain/project_access.rs
// `pub fn resource_is_visible` — admin sees everything; unbound
// resource is visible to everyone; bound resource needs an entry in
// the accessible-projects map.
func TestResourceIsVisible(t *testing.T) {
	admin := &authmw.Claims{Roles: []string{"admin"}}
	viewer := &authmw.Claims{Roles: []string{"viewer"}}
	pid := uuid.New()
	other := uuid.New()
	access := map[uuid.UUID]models.OntologyProjectRole{
		pid: models.OntologyProjectRoleViewer,
	}

	assert.True(t, ResourceIsVisible(admin, &pid, nil), "admin sees everything")
	assert.True(t, ResourceIsVisible(admin, nil, nil))
	assert.True(t, ResourceIsVisible(viewer, nil, access), "unbound resource is visible to all")
	assert.True(t, ResourceIsVisible(viewer, &pid, access))
	assert.False(t, ResourceIsVisible(viewer, &other, access), "bound to other project")
}

// libs/ontology-kernel/src/domain/project_access.rs
// `OntologyResourceKind::table_name` — pinned per kind so the
// LoadResourceOwnerID interpolation never reaches an unsafe table.
func TestOntologyResourceKindTableName(t *testing.T) {
	cases := map[OntologyResourceKind]string{
		OntologyResourceKindObjectType:         "object_types",
		OntologyResourceKindLinkType:           "link_types",
		OntologyResourceKindInterface:          "ontology_interfaces",
		OntologyResourceKindSharedPropertyType: "shared_property_types",
		OntologyResourceKindActionType:         "action_types",
		OntologyResourceKindFunctionPackage:    "ontology_function_packages",
		OntologyResourceKindRule:               "ontology_rules",
		OntologyResourceKindObjectSet:          "ontology_object_sets",
	}
	for kind, want := range cases {
		assert.Equal(t, want, kind.TableName())
	}
}

// ---- object_sets.go ------------------------------------------------------

// libs/ontology-kernel/src/domain/object_sets.rs
// `object_set_filters_support_properties_and_marking`.
func TestObjectSetFiltersSupportPropertiesAndMarking(t *testing.T) {
	object := json.RawMessage(`{
        "id": "00000000-0000-0000-0000-000000000001",
        "marking": "confidential",
        "properties": {
            "status": "active",
            "score": 99
        }
    }`)
	filters := []models.ObjectSetFilter{
		{Field: "status", Operator: "equals", Value: paramRaw(t, "active")},
		{Field: "marking", Operator: "equals", Value: paramRaw(t, "confidential")},
		{Field: "score", Operator: "gte", Value: paramRaw(t, 70)},
	}
	assert.True(t, MatchesObjectSetFilters(object, filters))
}

// libs/ontology-kernel/src/domain/object_sets.rs
// `projections_prefer_wrapper_and_base_paths`.
func TestObjectSetProjectionsPreferWrapperAndBasePaths(t *testing.T) {
	row := json.RawMessage(`{
        "base": {
            "id": "1",
            "properties": {
                "name": "Case-7"
            }
        },
        "joined": {
            "properties": {
                "owner": "Alice"
            }
        }
    }`)
	projected := ProjectObjectSetRow(row, []string{
		"id",
		"base.properties.name",
		"joined.properties.owner",
	})
	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(projected, &got))
	assert.JSONEq(t, `"1"`, string(got["id"]))
	assert.JSONEq(t, `"Case-7"`, string(got["base.properties.name"]))
	assert.JSONEq(t, `"Alice"`, string(got["joined.properties.owner"]))
}

// libs/ontology-kernel/src/domain/object_sets.rs `validate_filter` /
// `validate_traversal` / `validate_join` reject the documented
// out-of-range / unknown values verbatim.
func TestValidateObjectSetFilterRejectsBadOperator(t *testing.T) {
	err := ValidateObjectSetFilter(models.ObjectSetFilter{Field: "x", Operator: "weird"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported filter operator 'weird'")

	err = ValidateObjectSetFilter(models.ObjectSetFilter{Field: "  ", Operator: "equals"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "filters require a field")
}

func TestValidateObjectSetTraversalBoundsHops(t *testing.T) {
	err := ValidateObjectSetTraversal(models.ObjectSetTraversal{Direction: "outbound", MaxHops: 0})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "between 1 and 4")

	err = ValidateObjectSetTraversal(models.ObjectSetTraversal{Direction: "outbound", MaxHops: 5})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "between 1 and 4")

	err = ValidateObjectSetTraversal(models.ObjectSetTraversal{Direction: "lateral", MaxHops: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported traversal direction 'lateral'")

	require.NoError(t, ValidateObjectSetTraversal(models.ObjectSetTraversal{Direction: "both", MaxHops: 2}))
}

// libs/ontology-kernel/src/domain/object_sets.rs
// `enforce_object_set_policy` — the three guards (deny_guest_sessions,
// minimum_clearance, required_restricted_view_id) each reject with
// the verbatim Rust message.
func TestEnforceObjectSetPolicyGuards(t *testing.T) {
	guestEmail := "guest@example.com"
	guestClaims := &authmw.Claims{
		Sub: uuid.Nil,
		SessionScope: &authmw.SessionScope{
			GuestEmail: &guestEmail,
		},
	}
	err := EnforceObjectSetPolicy(guestClaims, models.ObjectSetPolicy{DenyGuestSessions: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "object set blocks guest sessions")

	// Insufficient clearance.
	piiClearance := "pii"
	publicClaims := &authmw.Claims{Sub: uuid.Nil}
	err = EnforceObjectSetPolicy(publicClaims, models.ObjectSetPolicy{MinimumClearance: &piiClearance})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient classification clearance")
}
