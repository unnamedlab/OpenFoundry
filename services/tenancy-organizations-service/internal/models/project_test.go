package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOntologyProjectRoleRank(t *testing.T) {
	t.Parallel()
	// SG.6: Discoverer is the new lattice floor; existing ranks
	// shift up by one.
	assert.Equal(t, uint8(1), OntologyProjectRoleDiscoverer.Rank())
	assert.Equal(t, uint8(2), OntologyProjectRoleViewer.Rank())
	assert.Equal(t, uint8(3), OntologyProjectRoleEditor.Rank())
	assert.Equal(t, uint8(4), OntologyProjectRoleOwner.Rank())
	assert.Equal(t, uint8(0), OntologyProjectRole("unknown").Rank())
}

func TestOntologyProjectRoleJSONShape(t *testing.T) {
	t.Parallel()
	cases := map[OntologyProjectRole]string{
		OntologyProjectRoleDiscoverer: `"discoverer"`,
		OntologyProjectRoleViewer:     `"viewer"`,
		OntologyProjectRoleEditor:     `"editor"`,
		OntologyProjectRoleOwner:      `"owner"`,
	}
	for role, want := range cases {
		b, err := json.Marshal(role)
		require.NoError(t, err)
		assert.Equal(t, want, string(b))
		var got OntologyProjectRole
		require.NoError(t, json.Unmarshal(b, &got))
		assert.Equal(t, role, got)
	}
}

func TestOntologyProjectJSONRoundtrip(t *testing.T) {
	t.Parallel()
	ws := "engineering"
	in := OntologyProject{
		ID:            uuid.New(),
		Slug:          "fraud-models",
		DisplayName:   "Fraud Models",
		Description:   "ML scoring",
		WorkspaceSlug: &ws,
		OwnerID:       uuid.New(),
		CreatedAt:     time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC),
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var got OntologyProject
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in, got)
}

func TestFolderRIDFromID(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("018f2f1c-aaaa-7bbb-8ccc-000000000002")
	assert.Equal(t, "ri.compass.main.folder.018f2f1c-aaaa-7bbb-8ccc-000000000002", FolderRIDFromID(id))
}

func TestOntologyProjectMembershipJSONRoundtrip(t *testing.T) {
	t.Parallel()
	in := OntologyProjectMembership{
		ProjectID: uuid.New(),
		UserID:    uuid.New(),
		Role:      OntologyProjectRoleEditor,
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var got OntologyProjectMembership
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in, got)
}

func TestOntologyProjectResourceBindingJSONRoundtrip(t *testing.T) {
	t.Parallel()
	in := OntologyProjectResourceBinding{
		ProjectID:    uuid.New(),
		ResourceKind: "dataset",
		ResourceID:   uuid.New(),
		BoundBy:      uuid.New(),
		CreatedAt:    time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var got OntologyProjectResourceBinding
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in, got)
}

func TestOntologyProjectFolderJSONRoundtrip(t *testing.T) {
	t.Parallel()
	parent := uuid.New()
	id := uuid.New()
	projectID := uuid.New()
	in := OntologyProjectFolder{
		ID:                      id,
		RID:                     FolderRIDFromID(id),
		ProjectID:               projectID,
		ProjectRID:              ProjectRIDFromID(projectID),
		ParentFolderID:          &parent,
		ParentFolderRID:         FolderRIDFromID(parent),
		SpaceRID:                DefaultProjectSpaceRID,
		Type:                    FolderResourceType,
		TrashStatus:             FolderTrashStatusNotTrashed,
		InheritsProjectPolicies: true,
		PolicyOverridesAllowed:  true,
		Name:                    "Models",
		Slug:                    "models",
		Description:             "Production models",
		CreatedBy:               uuid.New(),
		CreatedAt:               time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:               time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var got OntologyProjectFolder
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in, got)
}

func TestCreateOntologyProjectRequestJSONRoundtrip(t *testing.T) {
	t.Parallel()
	dn, desc, ws := "Fraud Models", "ML scoring", "engineering"
	in := CreateOntologyProjectRequest{
		Slug:          "fraud-models",
		DisplayName:   &dn,
		Description:   &desc,
		WorkspaceSlug: &ws,
		Folders: []CreateOntologyProjectFolderRequest{{
			Name: "Models",
		}},
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var got CreateOntologyProjectRequest
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in, got)
}

func TestUpdateOntologyProjectRequestJSONRoundtrip(t *testing.T) {
	t.Parallel()
	dn := "Renamed"
	in := UpdateOntologyProjectRequest{DisplayName: &dn}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	assert.JSONEq(t, `{"display_name":"Renamed"}`, string(b))
	var got UpdateOntologyProjectRequest
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in, got)
}

func TestListOntologyProjectsResponseJSONRoundtrip(t *testing.T) {
	t.Parallel()
	in := ListOntologyProjectsResponse{
		Data:    []OntologyProject{},
		Total:   0,
		Page:    1,
		PerPage: 50,
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var got ListOntologyProjectsResponse
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in.Total, got.Total)
	assert.Equal(t, in.Page, got.Page)
	assert.Equal(t, in.PerPage, got.PerPage)
}

func TestUpsertOntologyProjectMembershipRequestJSONRoundtrip(t *testing.T) {
	t.Parallel()
	in := UpsertOntologyProjectMembershipRequest{
		UserID: uuid.New(),
		Role:   OntologyProjectRoleOwner,
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var got UpsertOntologyProjectMembershipRequest
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in, got)
}

func TestProjectAccessRequestTaskSG9JSONRoundtrip(t *testing.T) {
	t.Parallel()
	role := OntologyProjectRoleViewer
	groupID := uuid.New()
	in := ProjectAccessRequestTask{
		ID:              uuid.New(),
		RequestID:       uuid.New(),
		ProjectID:       uuid.New(),
		TaskType:        ProjectAccessRequestTaskGroupMembership,
		TargetUserID:    uuid.New(),
		RequestedRole:   &role,
		GroupID:         &groupID,
		Reason:          "Need incident workspace access",
		Status:          ProjectAccessRequestTaskStatusReview,
		ReviewerUserIDs: []uuid.UUID{uuid.New()},
		CreatedAt:       time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var got ProjectAccessRequestTask
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in.ID, got.ID)
	assert.Equal(t, ProjectAccessRequestTaskGroupMembership, got.TaskType)
	assert.Equal(t, ProjectAccessRequestTaskStatusReview, got.Status)
	assert.Equal(t, in.ReviewerUserIDs, got.ReviewerUserIDs)
}

func TestProjectAccessRequestFormSG9JSONShape(t *testing.T) {
	t.Parallel()
	resp := ProjectAccessRequestFormResponse{
		ProjectID:           uuid.New(),
		RequesterID:         uuid.New(),
		ProjectOwnerID:      uuid.New(),
		DefaultRole:         OntologyProjectRoleDiscoverer,
		DirectRoleReviewers: []uuid.UUID{uuid.New()},
		Groups: []ProjectAccessRequestFormGroup{{
			GroupID:         uuid.New(),
			Role:            OntologyProjectRoleViewer,
			GroupKind:       ProjectAccessGroupKindExternal,
			ReviewerUserIDs: []uuid.UUID{},
			CustomForm:      map[string]any{"Team": "blue"},
		}},
		RequiredMarkings: []ProjectRequiredMarking{{
			ProjectID:       uuid.New(),
			MarkingID:       uuid.New(),
			MarkingName:     "PII",
			ReviewerUserIDs: []uuid.UUID{uuid.New()},
			CreatedAt:       time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
			UpdatedAt:       time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
		}},
	}
	b, err := json.Marshal(resp)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(b, &view))
	for _, key := range []string{
		"project_id", "requester_id", "project_owner_id", "default_role",
		"groups", "required_markings", "direct_role_reviewers",
	} {
		assert.Contains(t, view, key)
	}
}

func TestProjectTemplateSG26JSONShape(t *testing.T) {
	t.Parallel()
	role := OntologyProjectRoleEditor
	markingID := uuid.New()
	tpl := ProjectTemplate{
		ID:          uuid.New(),
		Key:         "governed-project",
		Name:        "Governed Project",
		Description: "Repeatable secure setup",
		DefaultRole: OntologyProjectRoleViewer,
		Variables: []ProjectTemplateVariable{{
			Key: "business_unit", Required: true,
		}},
		FolderStructure: []ProjectTemplateFolderSpec{{
			Key: "data", Name: "Data",
		}},
		GeneratedGroups: []ProjectTemplateGeneratedGroup{{
			Role: OntologyProjectRoleViewer, SlugSuffix: "viewers", Requestable: true,
		}},
		DefaultRoleGrants: []ProjectTemplateRoleGrant{{
			PrincipalKind:      ProjectTemplatePrincipalGeneratedGroup,
			GeneratedGroupRole: &role,
			Role:               OntologyProjectRoleEditor,
		}},
		Markings: []ProjectTemplateMarking{{
			MarkingID:   &markingID,
			DisplayName: "Confidential",
		}},
		Constraints: []ProjectTemplateConstraint{{
			Name: "No external sharing", Metadata: map[string]any{"scope": "project"},
		}},
		GovernanceTags: []string{"governed"},
		Active:         true,
		CreatedAt:      time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(tpl)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, key := range []string{
		"id", "key", "name", "default_role", "variables", "folder_structure",
		"generated_groups", "default_role_grants", "markings", "constraints",
		"governance_tags", "active", "created_at", "updated_at",
	} {
		assert.Contains(t, view, key)
	}
	assert.Equal(t, "viewer", view["default_role"])
}

func TestBindOntologyProjectResourceRequestJSONRoundtrip(t *testing.T) {
	t.Parallel()
	in := BindOntologyProjectResourceRequest{
		ResourceKind: "dataset",
		ResourceID:   uuid.New(),
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var got BindOntologyProjectResourceRequest
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in, got)
}
