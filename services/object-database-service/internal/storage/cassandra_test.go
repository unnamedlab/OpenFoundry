package storage

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

func TestCassandraObjectAdapterConvertsPutOutcomeAndErrors(t *testing.T) {
	t.Parallel()
	inner := &fakeRepoObjectStore{
		putOutcome: repos.Updated(2, 3),
		putErr:     repos.Invalid("bad object"),
	}
	store := NewCassandraObjectStore(inner)

	_, err := store.Put(context.Background(), Object{Tenant: "t", ID: "o", TypeID: "x"}, nil)
	require.Error(t, err)
	repoErr, ok := AsRepoError(err)
	require.True(t, ok)
	assert.Equal(t, ErrInvalidArgument, repoErr.Kind)

	inner.putErr = nil
	out, err := store.Put(context.Background(), Object{Tenant: "t", ID: "o", TypeID: "x"}, ptr(uint64(2)))
	require.NoError(t, err)
	assert.Equal(t, PutUpdated, out.Kind)
	assert.Equal(t, uint64(2), out.PreviousVersion)
	assert.Equal(t, uint64(3), out.NewVersion)
	assert.EqualValues(t, "t", inner.lastPut.Tenant)
	assert.EqualValues(t, "o", inner.lastPut.ID)
	assert.EqualValues(t, "x", inner.lastPut.TypeID)
	assert.Empty(t, inner.lastPut.Markings)
	assert.Equal(t, uint64(2), *inner.lastExpected)
}

func TestCassandraObjectAdapterConvertsPagesAndConsistency(t *testing.T) {
	t.Parallel()
	owner := repos.OwnerId("owner-1")
	inner := &fakeRepoObjectStore{listByType: repos.PagedResult[repos.Object]{
		Items:     []repos.Object{{Tenant: "t", ID: "o", TypeID: "x", Owner: &owner, Payload: json.RawMessage(`{"a":1}`)}},
		NextToken: ptr("next"),
	}}
	store := NewCassandraObjectStore(inner)

	res, err := store.ListByType(context.Background(), "t", "x", Page{Size: 5, Token: ptr("tok")}, ReadEventual)
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	assert.EqualValues(t, "owner-1", *res.Items[0].Owner)
	assert.Equal(t, "next", *res.NextToken)
	assert.Equal(t, repos.ConsistencyEventual, inner.lastConsistency.Level)
	assert.Equal(t, uint32(5), inner.lastPage.Size)
	assert.Equal(t, "tok", *inner.lastPage.Token)
}

func TestCassandraLinkAdapterConvertsPayload(t *testing.T) {
	t.Parallel()
	inner := &fakeRepoLinkStore{incoming: repos.PagedResult[repos.Link]{Items: []repos.Link{{
		Tenant: "t", LinkType: "lt", From: "a", To: "b", Payload: json.RawMessage(`{"p":true}`), CreatedAtMs: 12,
	}}}}
	store := NewCassandraLinkStore(inner)
	payload := json.RawMessage(`{"w":1}`)

	require.NoError(t, store.Put(context.Background(), Link{Tenant: "t", LinkType: "lt", From: "a", To: "b", Payload: &payload}))
	assert.JSONEq(t, `{"w":1}`, string(inner.lastPut.Payload))

	res, err := store.ListIncoming(context.Background(), "t", "lt", "b", Page{Size: 1}, ReadStrong)
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	require.NotNil(t, res.Items[0].Payload)
	assert.JSONEq(t, `{"p":true}`, string(*res.Items[0].Payload))
}

type fakeRepoObjectStore struct {
	lastPut         repos.Object
	lastExpected    *uint64
	lastPage        repos.Page
	lastConsistency repos.ReadConsistency
	putOutcome      repos.PutOutcome
	putErr          error
	listByType      repos.PagedResult[repos.Object]
}

func (s *fakeRepoObjectStore) Get(context.Context, repos.TenantId, repos.ObjectId, repos.ReadConsistency) (*repos.Object, error) {
	return nil, nil
}
func (s *fakeRepoObjectStore) Put(_ context.Context, obj repos.Object, expected *uint64) (repos.PutOutcome, error) {
	s.lastPut = obj
	s.lastExpected = expected
	return s.putOutcome, s.putErr
}
func (s *fakeRepoObjectStore) Delete(context.Context, repos.TenantId, repos.ObjectId) (bool, error) {
	return false, nil
}
func (s *fakeRepoObjectStore) ListByType(_ context.Context, _ repos.TenantId, _ repos.TypeId, page repos.Page, c repos.ReadConsistency) (repos.PagedResult[repos.Object], error) {
	s.lastPage = page
	s.lastConsistency = c
	return s.listByType, nil
}
func (s *fakeRepoObjectStore) ListByOwner(context.Context, repos.TenantId, repos.OwnerId, repos.Page, repos.ReadConsistency) (repos.PagedResult[repos.Object], error) {
	return repos.PagedResult[repos.Object]{}, nil
}
func (s *fakeRepoObjectStore) ListByMarking(context.Context, repos.TenantId, repos.MarkingId, repos.Page, repos.ReadConsistency) (repos.PagedResult[repos.Object], error) {
	return repos.PagedResult[repos.Object]{}, nil
}

type fakeRepoLinkStore struct {
	lastPut  repos.Link
	incoming repos.PagedResult[repos.Link]
}

func (s *fakeRepoLinkStore) Put(_ context.Context, link repos.Link) error {
	s.lastPut = link
	return nil
}
func (s *fakeRepoLinkStore) Delete(context.Context, repos.TenantId, repos.LinkTypeId, repos.ObjectId, repos.ObjectId) (bool, error) {
	return false, nil
}
func (s *fakeRepoLinkStore) ListOutgoing(context.Context, repos.TenantId, repos.LinkTypeId, repos.ObjectId, repos.Page, repos.ReadConsistency) (repos.PagedResult[repos.Link], error) {
	return repos.PagedResult[repos.Link]{}, nil
}
func (s *fakeRepoLinkStore) ListIncoming(context.Context, repos.TenantId, repos.LinkTypeId, repos.ObjectId, repos.Page, repos.ReadConsistency) (repos.PagedResult[repos.Link], error) {
	return s.incoming, nil
}
