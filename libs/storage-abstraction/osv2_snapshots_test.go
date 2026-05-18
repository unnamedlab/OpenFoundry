package storageabstraction

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testActionLogStore struct{ rows []ActionLogEntry }

func (s *testActionLogStore) Append(_ context.Context, entry ActionLogEntry) error {
	s.rows = append(s.rows, entry)
	return nil
}
func (s *testActionLogStore) ListRecent(_ context.Context, tenant TenantId, page Page, _ ReadConsistency) (PagedResult[ActionLogEntry], error) {
	return s.filter(func(entry ActionLogEntry) bool { return entry.Tenant == tenant }, page), nil
}
func (s *testActionLogStore) ListForObject(_ context.Context, tenant TenantId, object ObjectId, page Page, _ ReadConsistency) (PagedResult[ActionLogEntry], error) {
	return s.filter(func(entry ActionLogEntry) bool {
		return entry.Tenant == tenant && entry.Object != nil && *entry.Object == object
	}, page), nil
}
func (s *testActionLogStore) ListForAction(_ context.Context, tenant TenantId, actionID string, page Page, _ ReadConsistency) (PagedResult[ActionLogEntry], error) {
	return s.filter(func(entry ActionLogEntry) bool { return entry.Tenant == tenant && entry.ActionID == actionID }, page), nil
}
func (s *testActionLogStore) filter(fn func(ActionLogEntry) bool, page Page) PagedResult[ActionLogEntry] {
	items := []ActionLogEntry{}
	for _, row := range s.rows {
		if fn(row) {
			items = append(items, row)
		}
	}
	limit := int(page.Size)
	if limit < 1 {
		limit = 1
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return PagedResult[ActionLogEntry]{Items: items}
}

func TestTypeSnapshotIncludesContentHashAndIndexRows(t *testing.T) {
	ctx := context.Background()
	objects := &testObjectStore{rows: map[ObjectId]Object{
		"obj-1": {Tenant: "acme", ID: "obj-1", TypeID: "Aircraft", Version: 1, UpdatedAtMs: 1, Payload: json.RawMessage(`{"tail":"EC-1"}`)},
	}}
	store := NewInMemoryOSV2AdvancedStore()
	require.NoError(t, store.IndexSpatial(ctx, "acme", "Aircraft", "obj-1", "location", GeoPoint{Lat: 1, Lon: 2}, 1))
	require.NoError(t, store.PutTimeSeriesSamples(ctx, []TimeSeriesSample{{Tenant: "acme", TypeID: "Aircraft", ObjectID: "obj-1", Property: "altitude", TimestampMs: 1000, Value: 30000}}))

	meta, err := store.CreateTypeSnapshot(ctx, "acme", "Aircraft", objects, "data-health-retention")
	require.NoError(t, err)

	assert.Equal(t, 1, meta.ObjectCount)
	assert.Equal(t, 2, meta.IndexRowCount)
	assert.True(t, strings.HasPrefix(meta.ContentHash, "sha256:"))
	assert.Equal(t, "data-health-retention", meta.ScheduledBy)

	meta2, err := store.CreateTypeSnapshot(ctx, "acme", "Aircraft", objects, "data-health-retention")
	require.NoError(t, err)
	assert.Equal(t, meta.ContentHash, meta2.ContentHash, "same rows should hash deterministically")
}

func TestRestoreSnapshotRequiresDependencyAcknowledgementAndAudits(t *testing.T) {
	ctx := context.Background()
	objects := &testObjectStore{rows: map[ObjectId]Object{
		"obj-1": {Tenant: "acme", ID: "obj-1", TypeID: "Aircraft", Version: 1, UpdatedAtMs: 1, Payload: json.RawMessage(`{"tail":"snapshot"}`)},
	}}
	store := NewInMemoryOSV2AdvancedStore()
	meta, err := store.CreateTypeSnapshot(ctx, "acme", "Aircraft", objects, "data-health-retention")
	require.NoError(t, err)

	objects.rows["obj-1"] = Object{Tenant: "acme", ID: "obj-1", TypeID: "Aircraft", Version: 2, UpdatedAtMs: 2, Payload: json.RawMessage(`{"tail":"mutated"}`)}
	objects.rows["obj-2"] = Object{Tenant: "acme", ID: "obj-2", TypeID: "Aircraft", Version: 1, UpdatedAtMs: 3, Payload: json.RawMessage(`{"tail":"extra"}`)}
	actions := &testActionLogStore{}

	plan, err := store.PlanRestoreSnapshot(ctx, meta.ID, nil)
	require.NoError(t, err)
	require.Len(t, plan.Warnings, 3)

	blocked, err := store.RestoreSnapshot(ctx, SnapshotRestoreRequest{SnapshotID: meta.ID, Actor: "actor-1"}, objects, actions)
	require.NoError(t, err)
	assert.False(t, blocked.Committed)
	assert.NotEmpty(t, blocked.Warnings)
	assert.Equal(t, json.RawMessage(`{"tail":"mutated"}`), objects.rows["obj-1"].Payload)

	committed, err := store.RestoreSnapshot(ctx, SnapshotRestoreRequest{SnapshotID: meta.ID, Actor: "actor-1", AcknowledgedDependencyKinds: []string{"action", "dashboard", "osdk"}}, objects, actions)
	require.NoError(t, err)
	assert.True(t, committed.Committed)
	assert.Equal(t, json.RawMessage(`{"tail":"snapshot"}`), objects.rows["obj-1"].Payload)
	_, exists := objects.rows["obj-2"]
	assert.False(t, exists, "restore should remove rows absent from the snapshot")

	audit, err := actions.ListForAction(ctx, "acme", "osv2-snapshot-restore:"+meta.ID+":main", Page{Size: 10}, Strong())
	require.NoError(t, err)
	require.Len(t, audit.Items, 1)
	assert.Equal(t, "osv2.snapshot_restored", audit.Items[0].Kind)
}

func TestBranchOverlaySnapshotRestoreDoesNotTouchMain(t *testing.T) {
	ctx := context.Background()
	main := &testObjectStore{rows: map[ObjectId]Object{
		"obj-main": {Tenant: "acme", ID: "obj-main", TypeID: "Aircraft", Version: 1, UpdatedAtMs: 1, Payload: json.RawMessage(`{"tail":"main"}`)},
	}}
	store := NewInMemoryOSV2AdvancedStore()
	_, err := store.PutBranchObject(ctx, "branch-1", Object{Tenant: "acme", ID: "obj-branch", TypeID: "Aircraft", Version: 1, UpdatedAtMs: 2, Payload: json.RawMessage(`{"tail":"snapshot"}`)}, nil)
	require.NoError(t, err)
	meta, err := store.CreateBranchOverlaySnapshot(ctx, "branch-1", "acme", "Aircraft", main, "data-health-retention")
	require.NoError(t, err)

	_, err = store.PutBranchObject(ctx, "branch-1", Object{Tenant: "acme", ID: "obj-branch", TypeID: "Aircraft", Version: 2, UpdatedAtMs: 3, Payload: json.RawMessage(`{"tail":"mutated"}`)}, nil)
	require.NoError(t, err)
	_, err = store.PutBranchObject(ctx, "branch-1", Object{Tenant: "acme", ID: "obj-extra", TypeID: "Aircraft", Version: 1, UpdatedAtMs: 4, Payload: json.RawMessage(`{"tail":"extra"}`)}, nil)
	require.NoError(t, err)

	result, err := store.RestoreSnapshot(ctx, SnapshotRestoreRequest{SnapshotID: meta.ID, Scope: SnapshotScopeBranchOverlay, Actor: "actor-1", AcknowledgedDependencyKinds: []string{"action", "dashboard", "osdk"}}, main, nil)
	require.NoError(t, err)
	assert.True(t, result.Committed)

	branchObj, err := store.GetBranchObject(ctx, "branch-1", "acme", "obj-branch", main, Strong())
	require.NoError(t, err)
	require.NotNil(t, branchObj)
	assert.JSONEq(t, `{"tail":"snapshot"}`, string(branchObj.Payload))
	missing, err := store.GetBranchObject(ctx, "branch-1", "acme", "obj-extra", main, Strong())
	require.NoError(t, err)
	assert.Nil(t, missing)
	assert.Contains(t, main.rows, ObjectId("obj-main"), "branch restore must not mutate main rows")
}
