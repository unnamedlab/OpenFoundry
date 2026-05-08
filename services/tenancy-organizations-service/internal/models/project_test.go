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
	assert.Equal(t, uint8(1), OntologyProjectRoleViewer.Rank())
	assert.Equal(t, uint8(2), OntologyProjectRoleEditor.Rank())
	assert.Equal(t, uint8(3), OntologyProjectRoleOwner.Rank())
	assert.Equal(t, uint8(0), OntologyProjectRole("unknown").Rank())
}

func TestOntologyProjectRoleJSONShape(t *testing.T) {
	t.Parallel()
	cases := map[OntologyProjectRole]string{
		OntologyProjectRoleViewer: `"viewer"`,
		OntologyProjectRoleEditor: `"editor"`,
		OntologyProjectRoleOwner:  `"owner"`,
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
	in := OntologyProjectFolder{
		ID:             uuid.New(),
		ProjectID:      uuid.New(),
		ParentFolderID: &parent,
		Name:           "Models",
		Slug:           "models",
		Description:    "Production models",
		CreatedBy:      uuid.New(),
		CreatedAt:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
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
