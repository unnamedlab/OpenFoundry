package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
	"github.com/openfoundry/openfoundry-go/services/ontology-query-service/internal/handlers"
)

type fakeLinkStore struct {
	outRes  repos.PagedResult[repos.Link]
	outErr  error
	inRes   repos.PagedResult[repos.Link]
	inErr   error
	lastDir string
}

func (f *fakeLinkStore) Put(context.Context, repos.Link) error {
	return repos.Backend("not implemented")
}
func (f *fakeLinkStore) Delete(context.Context, repos.TenantId, repos.LinkTypeId, repos.ObjectId, repos.ObjectId) (bool, error) {
	return false, repos.Backend("not implemented")
}
func (f *fakeLinkStore) ListOutgoing(_ context.Context, _ repos.TenantId, _ repos.LinkTypeId, _ repos.ObjectId, _ repos.Page, _ repos.ReadConsistency) (repos.PagedResult[repos.Link], error) {
	f.lastDir = "outgoing"
	return f.outRes, f.outErr
}
func (f *fakeLinkStore) ListIncoming(_ context.Context, _ repos.TenantId, _ repos.LinkTypeId, _ repos.ObjectId, _ repos.Page, _ repos.ReadConsistency) (repos.PagedResult[repos.Link], error) {
	f.lastDir = "incoming"
	return f.inRes, f.inErr
}

func newLinkHandler(store *fakeLinkStore) *handlers.Handlers {
	return handlers.New(handlers.AppState{Links: store})
}

func TestListOutgoingLinksRequiresAuth(t *testing.T) {
	t.Parallel()
	h := newLinkHandler(&fakeLinkStore{})
	req := httptest.NewRequest("GET", "/x", nil)
	rec := httptest.NewRecorder()
	h.ListOutgoingLinks(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestListOutgoingLinksNilStore500(t *testing.T) {
	t.Parallel()
	h := handlers.New(handlers.AppState{}) // Links nil
	req := authedReq("GET", "/x", map[string]string{
		"tenant":    uuid.NewString(),
		"object_id": uuid.NewString(),
		"link_type": "ASSIGNED_TO",
	}, nil)
	rec := httptest.NewRecorder()
	h.ListOutgoingLinks(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "link store not configured")
}

func TestListOutgoingLinksRejectsEmptyLinkType(t *testing.T) {
	t.Parallel()
	h := newLinkHandler(&fakeLinkStore{})
	req := authedReq("GET", "/x", map[string]string{
		"tenant":    uuid.NewString(),
		"object_id": uuid.NewString(),
	}, nil)
	rec := httptest.NewRecorder()
	h.ListOutgoingLinks(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "link_type")
}

func TestListOutgoingLinksReturnsPage(t *testing.T) {
	t.Parallel()
	tenant := uuid.NewString()
	from := uuid.NewString()
	to := repos.ObjectId(uuid.NewString())
	next := "opaque"
	store := &fakeLinkStore{outRes: repos.PagedResult[repos.Link]{
		Items: []repos.Link{{
			Tenant:   repos.TenantId(tenant),
			LinkType: repos.LinkTypeId("ASSIGNED_TO"),
			From:     repos.ObjectId(from),
			To:       to,
			Payload:  json.RawMessage(`{}`),
		}},
		NextToken: &next,
	}}
	h := newLinkHandler(store)
	req := authedReq("GET", "/x", map[string]string{
		"tenant":    tenant,
		"object_id": from,
		"link_type": "ASSIGNED_TO",
	}, nil)
	rec := httptest.NewRecorder()
	h.ListOutgoingLinks(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "outgoing", store.lastDir)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Len(t, body["items"], 1)
	assert.Equal(t, next, body["next_token"])
}

func TestListIncomingLinksReturnsPage(t *testing.T) {
	t.Parallel()
	tenant := uuid.NewString()
	to := uuid.NewString()
	store := &fakeLinkStore{inRes: repos.PagedResult[repos.Link]{
		Items: []repos.Link{{
			Tenant:   repos.TenantId(tenant),
			LinkType: repos.LinkTypeId("ASSIGNED_TO"),
			From:     repos.ObjectId(uuid.NewString()),
			To:       repos.ObjectId(to),
			Payload:  json.RawMessage(`{}`),
		}},
	}}
	h := newLinkHandler(store)
	req := authedReq("GET", "/x", map[string]string{
		"tenant":    tenant,
		"object_id": to,
		"link_type": "ASSIGNED_TO",
	}, nil)
	rec := httptest.NewRecorder()
	h.ListIncomingLinks(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "incoming", store.lastDir)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Len(t, body["items"], 1)
}

func TestListOutgoingLinksTenantScopeDenied(t *testing.T) {
	t.Parallel()
	tenant := uuid.NewString()
	otherOrg := uuid.New()
	h := newLinkHandler(&fakeLinkStore{})
	claims := &authmw.Claims{
		Sub:   uuid.New(),
		OrgID: &otherOrg,
		Roles: []string{"user"},
	}
	req := authedReq("GET", "/x", map[string]string{
		"tenant":    tenant,
		"object_id": uuid.NewString(),
		"link_type": "ASSIGNED_TO",
	}, claims)
	rec := httptest.NewRecorder()
	h.ListOutgoingLinks(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "tenant access denied")
}

func TestListOutgoingLinksBackendErrorMapsTo500(t *testing.T) {
	t.Parallel()
	store := &fakeLinkStore{outErr: repos.Backend("cassandra down")}
	h := newLinkHandler(store)
	req := authedReq("GET", "/x", map[string]string{
		"tenant":    uuid.NewString(),
		"object_id": uuid.NewString(),
		"link_type": "ASSIGNED_TO",
	}, nil)
	rec := httptest.NewRecorder()
	h.ListOutgoingLinks(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "cassandra down")
}

func TestListOutgoingLinksRejectsInvalidConsistency(t *testing.T) {
	t.Parallel()
	h := newLinkHandler(&fakeLinkStore{})
	req := authedReq("GET", "/x", map[string]string{
		"tenant":    uuid.NewString(),
		"object_id": uuid.NewString(),
		"link_type": "ASSIGNED_TO",
	}, nil)
	req.Header.Set("X-Consistency", "lukewarm")
	rec := httptest.NewRecorder()
	h.ListOutgoingLinks(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "X-Consistency")
}
