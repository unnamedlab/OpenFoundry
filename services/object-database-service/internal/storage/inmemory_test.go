package storage

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObjectStorePutInsertsThenUpdates(t *testing.T) {
	t.Parallel()
	s := NewInMemoryObjectStore()
	ctx := context.Background()

	obj := Object{
		Tenant: "t-a", ID: "obj-1", TypeID: "aircraft",
		Version: 0, Payload: json.RawMessage(`{"x":1}`),
		UpdatedAtMs: 10,
	}
	out, err := s.Put(ctx, obj, nil)
	require.NoError(t, err)
	assert.Equal(t, PutInserted, out.Kind)

	out2, err := s.Put(ctx, obj, ptr(uint64(1)))
	require.NoError(t, err)
	assert.Equal(t, PutUpdated, out2.Kind)
	assert.Equal(t, uint64(1), out2.PreviousVersion)
	assert.Equal(t, uint64(2), out2.NewVersion)

	out3, err := s.Put(ctx, obj, ptr(uint64(99)))
	require.NoError(t, err)
	assert.Equal(t, PutVersionConflict, out3.Kind)
	assert.Equal(t, uint64(99), out3.ExpectedVersion)
	assert.Equal(t, uint64(2), out3.ActualVersion)
}

func TestObjectStoreGetMiss(t *testing.T) {
	t.Parallel()
	s := NewInMemoryObjectStore()
	got, err := s.Get(context.Background(), "t", "missing", ReadStrong)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestObjectStoreListByTypeOwnerMarking(t *testing.T) {
	t.Parallel()
	s := NewInMemoryObjectStore()
	ctx := context.Background()
	owner1 := OwnerId("owner-1")
	owner2 := OwnerId("owner-2")

	must := func(o Object) {
		_, err := s.Put(ctx, o, nil)
		require.NoError(t, err)
	}
	must(Object{Tenant: "t-a", ID: "1", TypeID: "aircraft", UpdatedAtMs: 10, Owner: &owner1, Markings: []MarkingId{"public"}})
	must(Object{Tenant: "t-a", ID: "2", TypeID: "aircraft", UpdatedAtMs: 20, Owner: &owner2, Markings: []MarkingId{"secret"}})
	must(Object{Tenant: "t-b", ID: "3", TypeID: "aircraft", UpdatedAtMs: 30}) // different tenant

	page := Page{Size: 10}

	byType, err := s.ListByType(ctx, "t-a", "aircraft", page, ReadStrong)
	require.NoError(t, err)
	assert.Len(t, byType.Items, 2)
	assert.EqualValues(t, "2", byType.Items[0].ID, "newest first")

	byOwner, err := s.ListByOwner(ctx, "t-a", "owner-1", page, ReadStrong)
	require.NoError(t, err)
	assert.Len(t, byOwner.Items, 1)
	assert.EqualValues(t, "1", byOwner.Items[0].ID)

	byMarking, err := s.ListByMarking(ctx, "t-a", "secret", page, ReadStrong)
	require.NoError(t, err)
	assert.Len(t, byMarking.Items, 1)
	assert.EqualValues(t, "2", byMarking.Items[0].ID)
}

func TestObjectStoreDelete(t *testing.T) {
	t.Parallel()
	s := NewInMemoryObjectStore()
	ctx := context.Background()
	_, err := s.Put(ctx, Object{Tenant: "t", ID: "1", TypeID: "x"}, nil)
	require.NoError(t, err)

	deleted, err := s.Delete(ctx, "t", "1")
	require.NoError(t, err)
	assert.True(t, deleted)

	deleted2, err := s.Delete(ctx, "t", "1")
	require.NoError(t, err)
	assert.False(t, deleted2)
}

func TestObjectStorePointReadByTypeAndPrimaryKey(t *testing.T) {
	t.Parallel()
	s := NewInMemoryObjectStore()
	ctx := context.Background()
	obj := Object{Tenant: "t", ID: "obj-1", TypeID: "aircraft", Payload: json.RawMessage(`{"tail":"N123"}`), UpdatedAtMs: 1}
	_, err := s.Put(ctx, obj, nil)
	require.NoError(t, err)

	got, err := s.GetByTypeAndPrimaryKey(ctx, "t", "aircraft", "obj-1", ReadStrong)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.EqualValues(t, "obj-1", got.ID)

	miss, err := s.GetByTypeAndPrimaryKey(ctx, "t", "vehicle", "obj-1", ReadStrong)
	require.NoError(t, err)
	assert.Nil(t, miss)
}

func TestObjectStoreQueryByPropertyStartsWith(t *testing.T) {
	t.Parallel()
	s := NewInMemoryObjectStore()
	ctx := context.Background()
	for _, obj := range []Object{
		{Tenant: "t", ID: "a", TypeID: "aircraft", Payload: json.RawMessage(`{"tail":"N123"}`), UpdatedAtMs: 1},
		{Tenant: "t", ID: "b", TypeID: "aircraft", Payload: json.RawMessage(`{"tail":"N999"}`), UpdatedAtMs: 2},
		{Tenant: "t", ID: "c", TypeID: "aircraft", Payload: json.RawMessage(`{"tail":"X123"}`), UpdatedAtMs: 3},
	} {
		_, err := s.Put(ctx, obj, nil)
		require.NoError(t, err)
	}

	res, err := s.QueryByProperty(ctx, "t", "aircraft", PropertyPredicate{PropertyName: "tail", Operator: "starts_with", Value: "N"}, Page{Size: 10}, ReadStrong)
	require.NoError(t, err)
	assert.Len(t, res.Items, 2)
}

func TestLinkStoreOutgoingIncoming(t *testing.T) {
	t.Parallel()
	s := NewInMemoryLinkStore()
	ctx := context.Background()
	require.NoError(t, s.Put(ctx, Link{Tenant: "t", LinkType: "owns", From: "a", To: "b", CreatedAtMs: 1}))
	require.NoError(t, s.Put(ctx, Link{Tenant: "t", LinkType: "owns", From: "a", To: "b", CreatedAtMs: 2}), "duplicate ignored")
	require.NoError(t, s.Put(ctx, Link{Tenant: "t", LinkType: "owns", From: "a", To: "c", CreatedAtMs: 3}))

	out, err := s.ListOutgoing(ctx, "t", "owns", "a", Page{Size: 10}, ReadStrong)
	require.NoError(t, err)
	assert.Len(t, out.Items, 2)

	in, err := s.ListIncoming(ctx, "t", "owns", "b", Page{Size: 10}, ReadStrong)
	require.NoError(t, err)
	assert.Len(t, in.Items, 1)

	deleted, err := s.Delete(ctx, "t", "owns", "a", "b")
	require.NoError(t, err)
	assert.True(t, deleted)
}

func TestLinkStorePaginationUsesStableNeighborCursor(t *testing.T) {
	t.Parallel()
	s := NewInMemoryLinkStore()
	ctx := context.Background()
	for _, to := range []ObjectId{"c", "a", "d", "b"} {
		require.NoError(t, s.Put(ctx, Link{Tenant: "t", LinkType: "owns", From: "root", To: to, CreatedAtMs: int64(len(to))}))
	}

	page1, err := s.ListOutgoing(ctx, "t", "owns", "root", Page{Size: 2}, ReadStrong)
	require.NoError(t, err)
	require.NotNil(t, page1.NextToken)
	assert.EqualValues(t, []ObjectId{"a", "b"}, []ObjectId{page1.Items[0].To, page1.Items[1].To})

	page2, err := s.ListOutgoing(ctx, "t", "owns", "root", Page{Size: 2, Token: page1.NextToken}, ReadStrong)
	require.NoError(t, err)
	assert.Nil(t, page2.NextToken)
	assert.EqualValues(t, []ObjectId{"c", "d"}, []ObjectId{page2.Items[0].To, page2.Items[1].To})
}

func TestObjectJSONShape(t *testing.T) {
	t.Parallel()
	o := Object{
		Tenant:      "t-a",
		ID:          "obj-1",
		TypeID:      "aircraft",
		Version:     2,
		Payload:     json.RawMessage(`{"tail":"N123"}`),
		UpdatedAtMs: 99,
	}
	b, err := json.Marshal(o)
	require.NoError(t, err)
	assert.JSONEq(t,
		`{"tenant":"t-a","id":"obj-1","type_id":"aircraft","version":2,"payload":{"tail":"N123"},"updated_at_ms":99}`,
		string(b))
}

func ptr[T any](v T) *T { return &v }
