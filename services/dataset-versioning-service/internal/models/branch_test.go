package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func branchFixture() DatasetBranch {
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	parent := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	head := uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	return DatasetBranch{
		ID:                       id,
		RID:                      "ri.foundry.main.branch." + id.String(),
		DatasetID:                uuid.Nil,
		DatasetRID:               "ri.foundry.main.dataset.foo",
		Name:                     "feature",
		ParentBranchID:           &parent,
		HeadTransactionID:        &head,
		CreatedFromTransactionID: &head,
		LastActivityAt:           time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC),
		Labels:                   json.RawMessage(`{"persona":"data-eng"}`),
		FallbackChain:            []string{"develop", "master"},
		Version:                  1,
		BaseVersion:              1,
		Description:              "",
		IsDefault:                false,
		CreatedAt:                time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:                time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC),
	}
}

func TestDatasetBranchIsRootIsDerivedFromParentBranchID(t *testing.T) {
	b := branchFixture()
	assert.False(t, b.IsRoot())
	b.ParentBranchID = nil
	assert.True(t, b.IsRoot())
}

func TestDatasetBranchRIDHelpersSynthesiseFromUUID(t *testing.T) {
	b := branchFixture()
	assert.Equal(t, "ri.foundry.main.branch."+b.ID.String(), b.BranchRID())
	require.NotNil(t, b.ParentBranchRID())
	assert.Equal(t, "ri.foundry.main.branch."+b.ParentBranchID.String(), *b.ParentBranchRID())
	require.NotNil(t, b.HeadTransactionRID())
	assert.Equal(t, "ri.foundry.main.transaction."+b.HeadTransactionID.String(), *b.HeadTransactionRID())
	require.NotNil(t, b.CreatedFromTransactionRID())
	assert.Equal(t, "ri.foundry.main.transaction."+b.CreatedFromTransactionID.String(), *b.CreatedFromTransactionRID())
}

func TestDatasetBranchBranchRIDFallsBackWhenColumnEmpty(t *testing.T) {
	b := branchFixture()
	b.RID = ""
	assert.Equal(t, "ri.foundry.main.branch."+b.ID.String(), b.BranchRID())
}

func TestDatasetBranchSerdeRoundTripPreservesNewFields(t *testing.T) {
	b := branchFixture()
	raw, err := json.Marshal(b)
	require.NoError(t, err)
	var asMap map[string]any
	require.NoError(t, json.Unmarshal(raw, &asMap))
	assert.Equal(t, "feature", asMap["name"])
	assert.Equal(t, []any{"develop", "master"}, asMap["fallback_chain"])
	labels := asMap["labels"].(map[string]any)
	assert.Equal(t, "data-eng", labels["persona"])
	assert.Contains(t, asMap, "rid")
	assert.Contains(t, asMap, "created_from_transaction_id")
	assert.Contains(t, asMap, "last_activity_at")

	var back DatasetBranch
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, b.FallbackChain, back.FallbackChain)
	assert.JSONEq(t, string(b.Labels), string(back.Labels))
	assert.Equal(t, b.CreatedFromTransactionID, back.CreatedFromTransactionID)
}

func TestDatasetBranchLegacyPayloadAppliesDefaults(t *testing.T) {
	legacy := []byte(`{
		"id": "00000000-0000-0000-0000-000000000001",
		"dataset_id": "00000000-0000-0000-0000-000000000000",
		"name": "old",
		"version": 1,
		"base_version": 1,
		"description": "",
		"is_default": true,
		"created_at": "2026-05-01T00:00:00Z",
		"updated_at": "2026-05-01T00:00:00Z"
	}`)
	var parsed DatasetBranch
	require.NoError(t, json.Unmarshal(legacy, &parsed))
	assert.True(t, parsed.IsRoot())
	assert.Empty(t, parsed.FallbackChain)
	assert.Empty(t, parsed.Labels)
	assert.Empty(t, parsed.DatasetRID)
}

func TestBranchMarkingsViewFromRowsPartitionsExplicitAndInherited(t *testing.T) {
	mPii := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	mHipaa := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	branch := uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	view := BranchMarkingsViewFromRows([]BranchMarking{
		{BranchID: branch, MarkingID: mPii, Source: MarkingSourceParent},
		{BranchID: branch, MarkingID: mHipaa, Source: MarkingSourceExplicit},
	})
	assert.Contains(t, view.Effective, mPii)
	assert.Contains(t, view.Effective, mHipaa)
	assert.Equal(t, []uuid.UUID{mHipaa}, view.Explicit)
	assert.Equal(t, []uuid.UUID{mPii}, view.InheritedFromParent)
}

func TestBranchMarkingsViewDedupesIdAcrossSources(t *testing.T) {
	m := uuid.MustParse("00000000-0000-0000-0000-000000000007")
	branch := uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	view := BranchMarkingsViewFromRows([]BranchMarking{
		{BranchID: branch, MarkingID: m, Source: MarkingSourceParent},
		{BranchID: branch, MarkingID: m, Source: MarkingSourceExplicit},
	})
	assert.Equal(t, []uuid.UUID{m}, view.Effective)
}

func TestBranchEnvelopeSerialisesWithRequiredFields(t *testing.T) {
	parent := "ri.foundry.main.branch.parent"
	env := NewBranchEnvelope(EventBranchCreated, "ri.foundry.main.branch.x", "ri.foundry.main.dataset.y", "user:1").
		WithParentRID(&parent).
		WithExtras(JSONValue([]byte(`{"source_kind":"child_from_branch"}`)))
	payload := env.Payload()

	var asMap map[string]any
	require.NoError(t, json.Unmarshal(payload, &asMap))
	assert.Equal(t, "dataset.branch.created.v1", asMap["event_type"])
	assert.Equal(t, "ri.foundry.main.dataset.y", asMap["dataset_rid"])
	assert.Equal(t, "ri.foundry.main.branch.parent", asMap["parent_rid"])
	assert.Equal(t, false, asMap["is_root"])
	extras := asMap["extras"].(map[string]any)
	assert.Equal(t, "child_from_branch", extras["source_kind"])
}

func TestBranchEnvelopeWithoutParentMarksEventAsRoot(t *testing.T) {
	env := NewBranchEnvelope(EventBranchCreated, "br", "ds", "system")
	payload := env.Payload()
	var asMap map[string]any
	require.NoError(t, json.Unmarshal(payload, &asMap))
	assert.Equal(t, true, asMap["is_root"])
	assert.Nil(t, asMap["parent_rid"])
}

func TestMarkingSourceCanonicalLabels(t *testing.T) {
	assert.Equal(t, "PARENT", MarkingSourceParent.String())
	assert.Equal(t, "EXPLICIT", MarkingSourceExplicit.String())
}
