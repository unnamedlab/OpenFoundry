package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNexusSpaceJSONRoundtrip(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	in := NexusSpace{
		ID:             uuid.New(),
		Slug:           "shared-research",
		DisplayName:    "Shared Research",
		Description:    "Cross-tenant research workspace",
		SpaceKind:      "shared",
		OwnerPeerID:    &owner,
		Region:         "us-east-1",
		MemberPeerIDs:  []uuid.UUID{uuid.New(), uuid.New()},
		GovernanceTags: []string{"pii-allowed", "audit-required"},
		Status:         "active",
		CreatedAt:      time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var got NexusSpace
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in, got)
}

func TestCreateSpaceRequestJSONRoundtrip(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	in := CreateSpaceRequest{
		Slug:           "alpha",
		DisplayName:    "Alpha",
		Description:    "Alpha space",
		SpaceKind:      "shared",
		OwnerPeerID:    &owner,
		Region:         "eu-west-1",
		MemberPeerIDs:  []uuid.UUID{uuid.New()},
		GovernanceTags: []string{"public"},
		Status:         "active",
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var got CreateSpaceRequest
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in, got)
}

func TestCreateSpaceRequestSerdeDefaultsForSlices(t *testing.T) {
	t.Parallel()
	// Mirrors Rust `#[serde(default)]` on member_peer_ids and governance_tags:
	// missing fields decode as empty slices (Go: nil — semantically equivalent).
	raw := []byte(`{
		"slug": "alpha",
		"display_name": "Alpha",
		"description": "",
		"space_kind": "shared",
		"region": "us-east-1",
		"status": "active"
	}`)
	var got CreateSpaceRequest
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Empty(t, got.MemberPeerIDs)
	assert.Empty(t, got.GovernanceTags)
	assert.Nil(t, got.OwnerPeerID)
}

func TestUpdateSpaceRequestJSONRoundtrip(t *testing.T) {
	t.Parallel()
	region := "us-west-2"
	tags := []string{"audit-required"}
	in := UpdateSpaceRequest{
		Region:         &region,
		GovernanceTags: &tags,
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	assert.JSONEq(t, `{"region":"us-west-2","governance_tags":["audit-required"]}`, string(b))
	var got UpdateSpaceRequest
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in, got)
}
