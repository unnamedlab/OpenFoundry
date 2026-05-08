package scim_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/scim"
)

// groupsRouter mounts every SCIM Group endpoint so the tests
// drive the same chi setup as production.
func groupsRouter(h *scim.Handlers) http.Handler {
	r := chi.NewRouter()
	r.Get(scim.RouteGroups+"/{id}", h.GetGroup)
	r.Get(scim.RouteGroups, h.ListGroups)
	r.Post(scim.RouteGroups, h.CreateGroup)
	r.Patch(scim.RouteGroups+"/{id}", h.PatchGroup)
	r.Delete(scim.RouteGroups+"/{id}", h.DeleteGroup)
	return r
}

func newGroupsHandlers(t *testing.T) (*scim.Handlers, *scim.InMemoryUserStore, *scim.InMemoryGroupStore) {
	t.Helper()
	users := scim.NewInMemoryUserStore()
	groups := scim.NewInMemoryGroupStore(users)
	h := (&scim.Handlers{BaseURL: baseURL, Users: users}).AttachGroupStore(groups)
	return h, users, groups
}

// --- Pure helpers -------------------------------------------------------

func TestParseMemberFilterPathHappyPath(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	got, ok := scim.ParseMemberFilterPath(`members[value eq "` + id.String() + `"]`)
	require.True(t, ok)
	assert.Equal(t, id, got)
}

func TestParseMemberFilterPathRejectsMalformed(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"members[value eq \"not-a-uuid\"]",
		"members[displayName eq \"x\"]",
		`members[value eq "no-suffix"`,
		``,
		`members`,
	} {
		_, ok := scim.ParseMemberFilterPath(path)
		assert.False(t, ok, "path=%s", path)
	}
}

func TestMembersFromValueArray(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`[{"value":"a","display":"Alice"},{"value":"b"}]`)
	got, scimErr := scim.MembersFromValue(raw)
	require.Nil(t, scimErr)
	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].Value)
	require.NotNil(t, got[0].Display)
	assert.Equal(t, "Alice", *got[0].Display)
}

func TestMembersFromValueSingleObject(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"value":"abc"}`)
	got, scimErr := scim.MembersFromValue(raw)
	require.Nil(t, scimErr)
	require.Len(t, got, 1)
	assert.Equal(t, "abc", got[0].Value)
}

func TestMembersFromValueRejectsEmpty(t *testing.T) {
	t.Parallel()
	for _, raw := range []json.RawMessage{nil, json.RawMessage(""), json.RawMessage("null"), json.RawMessage("   ")} {
		_, scimErr := scim.MembersFromValue(raw)
		require.NotNil(t, scimErr, "raw=%q", string(raw))
		require.NotNil(t, scimErr.ScimType)
		assert.Equal(t, "invalidValue", *scimErr.ScimType)
	}
}

func TestMembersFromValueRejectsBadShape(t *testing.T) {
	t.Parallel()
	_, scimErr := scim.MembersFromValue(json.RawMessage(`{"unknown":1`))
	require.NotNil(t, scimErr)
	assert.Contains(t, scimErr.Detail, "must be SCIM member objects")
}

// --- GroupToScim --------------------------------------------------------

func TestGroupToScimEmptyMembers(t *testing.T) {
	t.Parallel()
	rec := scim.GroupRecord{ID: uuid.New(), Name: "Engineering"}
	got := scim.GroupToScim(rec, nil, baseURL)
	require.NotNil(t, got.ID)
	assert.Equal(t, rec.ID.String(), *got.ID)
	assert.Equal(t, "Engineering", got.DisplayName)
	assert.Empty(t, got.Members)
	require.NotNil(t, got.Meta)
	assert.Equal(t, "Group", got.Meta.ResourceType)
	assert.Equal(t, baseURL+scim.RouteGroups+"/"+rec.ID.String(), got.Meta.Location)
}

func TestGroupToScimMembersUseDisplayFallback(t *testing.T) {
	t.Parallel()
	id1, id2 := uuid.New(), uuid.New()
	members := []scim.MemberView{
		{UserID: id1, Email: "alice@x", Name: "Alice"},
		{UserID: id2, Email: "bob@x", Name: ""}, // empty name → email fallback
	}
	got := scim.GroupToScim(scim.GroupRecord{ID: uuid.New(), Name: "G"}, members, baseURL)
	require.Len(t, got.Members, 2)
	require.NotNil(t, got.Members[0].Display)
	assert.Equal(t, "Alice", *got.Members[0].Display)
	require.NotNil(t, got.Members[0].Type)
	assert.Equal(t, "User", *got.Members[0].Type)
	require.NotNil(t, got.Members[0].Ref)
	assert.Equal(t, baseURL+scim.RouteUsers+"/"+id1.String(), *got.Members[0].Ref)
	require.NotNil(t, got.Members[1].Display)
	assert.Equal(t, "bob@x", *got.Members[1].Display)
}

// --- GroupStore: in-memory ---------------------------------------------

func TestGroupStoreReplaceMembersValidatesUserExists(t *testing.T) {
	t.Parallel()
	users := scim.NewInMemoryUserStore()
	groups := scim.NewInMemoryGroupStore(users)
	gid := uuid.New()
	require.NoError(t, groups.Put(context.Background(), scim.GroupRecord{ID: gid, Name: "G"}))

	missing := uuid.New()
	err := groups.ReplaceMembers(context.Background(), gid, []uuid.UUID{missing})
	assert.True(t, scim.IsMemberNotFound(err), "missing user → ErrMemberNotFound")
}

func TestGroupStorePutEnforcesUniqueDisplayName(t *testing.T) {
	t.Parallel()
	users := scim.NewInMemoryUserStore()
	groups := scim.NewInMemoryGroupStore(users)
	id1, id2 := uuid.New(), uuid.New()
	require.NoError(t, groups.Put(context.Background(), scim.GroupRecord{ID: id1, Name: "Engineering"}))
	err := groups.Put(context.Background(), scim.GroupRecord{ID: id2, Name: "Engineering"})
	assert.True(t, scim.IsGroupUniqueViolation(err))
}

func TestGroupStoreMembersOrderedByEmail(t *testing.T) {
	t.Parallel()
	users := scim.NewInMemoryUserStore()
	uA, uB, uC := uuid.New(), uuid.New(), uuid.New()
	users.Insert(scim.UserRecord{ID: uB, Email: "b@x"})
	users.Insert(scim.UserRecord{ID: uC, Email: "c@x"})
	users.Insert(scim.UserRecord{ID: uA, Email: "a@x"})
	groups := scim.NewInMemoryGroupStore(users)
	gid := uuid.New()
	require.NoError(t, groups.Put(context.Background(), scim.GroupRecord{ID: gid, Name: "G"}))
	require.NoError(t, groups.AddMembers(context.Background(), gid, []uuid.UUID{uC, uA, uB}))

	members, err := groups.Members(context.Background(), gid)
	require.NoError(t, err)
	require.Len(t, members, 3)
	assert.Equal(t, "a@x", members[0].Email)
	assert.Equal(t, "b@x", members[1].Email)
	assert.Equal(t, "c@x", members[2].Email)
}

func TestGroupStoreReplaceMembersAtomic(t *testing.T) {
	t.Parallel()
	users := scim.NewInMemoryUserStore()
	uA, uB := uuid.New(), uuid.New()
	users.Insert(scim.UserRecord{ID: uA, Email: "a@x"})
	users.Insert(scim.UserRecord{ID: uB, Email: "b@x"})
	groups := scim.NewInMemoryGroupStore(users)
	gid := uuid.New()
	require.NoError(t, groups.Put(context.Background(), scim.GroupRecord{ID: gid, Name: "G"}))
	require.NoError(t, groups.AddMembers(context.Background(), gid, []uuid.UUID{uA}))
	require.NoError(t, groups.ReplaceMembers(context.Background(), gid, []uuid.UUID{uB}))

	members, err := groups.Members(context.Background(), gid)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, uB, members[0].UserID)
}

func TestGroupStoreRemoveMember(t *testing.T) {
	t.Parallel()
	users := scim.NewInMemoryUserStore()
	uA, uB := uuid.New(), uuid.New()
	users.Insert(scim.UserRecord{ID: uA, Email: "a@x"})
	users.Insert(scim.UserRecord{ID: uB, Email: "b@x"})
	groups := scim.NewInMemoryGroupStore(users)
	gid := uuid.New()
	require.NoError(t, groups.Put(context.Background(), scim.GroupRecord{ID: gid, Name: "G"}))
	require.NoError(t, groups.AddMembers(context.Background(), gid, []uuid.UUID{uA, uB}))
	require.NoError(t, groups.RemoveMember(context.Background(), gid, uA))

	members, err := groups.Members(context.Background(), gid)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, uB, members[0].UserID)
}

// --- CreateGroup --------------------------------------------------------

func TestCreateGroupHappyPath(t *testing.T) {
	t.Parallel()
	h, _, _ := newGroupsHandlers(t)
	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaGroup + `"],
        "displayName":"Engineering",
        "externalId":"sso-eng"
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPost, scim.RouteGroups, body))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "got body=%s", rec.Body.String())

	var got scim.ScimGroup
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	require.NotNil(t, got.ID)
	assert.Equal(t, "Engineering", got.DisplayName)
	require.NotNil(t, got.ExternalID)
	assert.Equal(t, "sso-eng", *got.ExternalID)
}

func TestCreateGroupRejectsEmptyDisplayName(t *testing.T) {
	t.Parallel()
	h, _, _ := newGroupsHandlers(t)
	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaGroup + `"],
        "displayName":"   "
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPost, scim.RouteGroups, body))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	var err scim.ScimError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&err))
	require.NotNil(t, err.ScimType)
	assert.Equal(t, "invalidValue", *err.ScimType)
	assert.Contains(t, err.Detail, "displayName is required")
}

func TestCreateGroupConflictOnDuplicateDisplayName(t *testing.T) {
	t.Parallel()
	h, _, groups := newGroupsHandlers(t)
	groups.Insert(scim.GroupRecord{ID: uuid.New(), Name: "Engineering"})
	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaGroup + `"],
        "displayName":"Engineering"
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPost, scim.RouteGroups, body))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code)
	var err scim.ScimError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&err))
	require.NotNil(t, err.ScimType)
	assert.Equal(t, "uniqueness", *err.ScimType)
}

func TestCreateGroupIdempotentOnExternalID(t *testing.T) {
	t.Parallel()
	h, _, groups := newGroupsHandlers(t)
	ext := "sso-eng"
	existingID := uuid.New()
	groups.Insert(scim.GroupRecord{ID: existingID, Name: "Old", ScimExternalID: &ext})

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaGroup + `"],
        "displayName":"Engineering",
        "externalId":"sso-eng"
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPost, scim.RouteGroups, body))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "got body=%s", rec.Body.String())

	var got scim.ScimGroup
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	require.NotNil(t, got.ID)
	assert.Equal(t, existingID.String(), *got.ID)
	assert.Equal(t, "Engineering", got.DisplayName, "Rust impl trims displayName but keeps the existing id")
}

func TestCreateGroupWithMembers(t *testing.T) {
	t.Parallel()
	h, users, _ := newGroupsHandlers(t)
	uA := uuid.New()
	users.Insert(scim.UserRecord{ID: uA, Email: "alice@x", Name: "Alice"})

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaGroup + `"],
        "displayName":"Engineering",
        "members":[{"value":"` + uA.String() + `"}]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPost, scim.RouteGroups, body))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "got body=%s", rec.Body.String())

	var got scim.ScimGroup
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	require.Len(t, got.Members, 1)
	assert.Equal(t, uA.String(), got.Members[0].Value)
	require.NotNil(t, got.Members[0].Display)
	assert.Equal(t, "Alice", *got.Members[0].Display)
}

func TestCreateGroupRejectsUnknownMember(t *testing.T) {
	t.Parallel()
	h, _, _ := newGroupsHandlers(t)
	missing := uuid.New()
	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaGroup + `"],
        "displayName":"Engineering",
        "members":[{"value":"` + missing.String() + `"}]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPost, scim.RouteGroups, body))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	var err scim.ScimError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&err))
	require.NotNil(t, err.ScimType)
	assert.Equal(t, "invalidValue", *err.ScimType)
	assert.Contains(t, err.Detail, "does not reference an existing user")
}

func TestCreateGroupRejectsNonUUIDMember(t *testing.T) {
	t.Parallel()
	h, _, _ := newGroupsHandlers(t)
	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaGroup + `"],
        "displayName":"Engineering",
        "members":[{"value":"not-a-uuid"}]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPost, scim.RouteGroups, body))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	var err scim.ScimError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&err))
	assert.Contains(t, err.Detail, "user UUID")
}

// --- GetGroup -----------------------------------------------------------

func TestGetGroupHappyPath(t *testing.T) {
	t.Parallel()
	h, _, groups := newGroupsHandlers(t)
	gid := uuid.New()
	groups.Insert(scim.GroupRecord{ID: gid, Name: "Engineering"})

	req := adminCtx(t, httptest.NewRequest(http.MethodGet, scim.RouteGroups+"/"+gid.String(), nil))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var got scim.ScimGroup
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	require.NotNil(t, got.ID)
	assert.Equal(t, gid.String(), *got.ID)
}

func TestGetGroup404(t *testing.T) {
	t.Parallel()
	h, _, _ := newGroupsHandlers(t)
	req := adminCtx(t, httptest.NewRequest(http.MethodGet,
		scim.RouteGroups+"/"+uuid.New().String(), nil))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- ListGroups ---------------------------------------------------------

func TestListGroupsHappyPath(t *testing.T) {
	t.Parallel()
	h, _, groups := newGroupsHandlers(t)
	groups.Insert(scim.GroupRecord{ID: uuid.New(), Name: "Engineering"})
	groups.Insert(scim.GroupRecord{ID: uuid.New(), Name: "Sales"})

	req := adminCtx(t, httptest.NewRequest(http.MethodGet, scim.RouteGroups, nil))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp scim.ScimListResponse[scim.ScimGroup]
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Equal(t, 2, resp.TotalResults)
	require.Len(t, resp.Resources, 2)
	// Order by name ASC.
	assert.Equal(t, "Engineering", resp.Resources[0].DisplayName)
	assert.Equal(t, "Sales", resp.Resources[1].DisplayName)
}

func TestListGroupsFilterByDisplayName(t *testing.T) {
	t.Parallel()
	h, _, groups := newGroupsHandlers(t)
	groups.Insert(scim.GroupRecord{ID: uuid.New(), Name: "Engineering"})
	groups.Insert(scim.GroupRecord{ID: uuid.New(), Name: "Sales"})

	req := adminCtx(t, httptest.NewRequest(http.MethodGet,
		scim.RouteGroups+`?filter=displayName+eq+%22Engineering%22`, nil))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp scim.ScimListResponse[scim.ScimGroup]
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Equal(t, 1, resp.TotalResults)
	assert.Equal(t, "Engineering", resp.Resources[0].DisplayName)
}

func TestListGroupsRejectsUserNameFilter(t *testing.T) {
	t.Parallel()
	h, _, _ := newGroupsHandlers(t)
	req := adminCtx(t, httptest.NewRequest(http.MethodGet,
		scim.RouteGroups+`?filter=userName+eq+%22alice%22`, nil))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	var err scim.ScimError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&err))
	require.NotNil(t, err.ScimType)
	assert.Equal(t, "invalidFilter", *err.ScimType)
}

// --- PatchGroup ---------------------------------------------------------

func TestPatchGroupReplaceDisplayName(t *testing.T) {
	t.Parallel()
	h, _, groups := newGroupsHandlers(t)
	gid := uuid.New()
	groups.Insert(scim.GroupRecord{ID: gid, Name: "Old"})

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaPatchOp + `"],
        "Operations":[{"op":"replace","path":"displayName","value":"NewName"}]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPatch,
		scim.RouteGroups+"/"+gid.String(), body))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "got body=%s", rec.Body.String())

	rec2, _ := groups.Get(context.Background(), gid)
	require.NotNil(t, rec2)
	assert.Equal(t, "NewName", rec2.Name)
}

func TestPatchGroupAddMembers(t *testing.T) {
	t.Parallel()
	h, users, groups := newGroupsHandlers(t)
	uA := uuid.New()
	users.Insert(scim.UserRecord{ID: uA, Email: "alice@x"})
	gid := uuid.New()
	require.NoError(t, groups.Put(context.Background(), scim.GroupRecord{ID: gid, Name: "G"}))

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaPatchOp + `"],
        "Operations":[{
            "op":"add",
            "path":"members",
            "value":[{"value":"` + uA.String() + `"}]
        }]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPatch,
		scim.RouteGroups+"/"+gid.String(), body))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "got body=%s", rec.Body.String())

	members, _ := groups.Members(context.Background(), gid)
	require.Len(t, members, 1)
	assert.Equal(t, uA, members[0].UserID)
}

func TestPatchGroupReplaceMembersAtomic(t *testing.T) {
	t.Parallel()
	h, users, groups := newGroupsHandlers(t)
	uA, uB := uuid.New(), uuid.New()
	users.Insert(scim.UserRecord{ID: uA, Email: "alice@x"})
	users.Insert(scim.UserRecord{ID: uB, Email: "bob@x"})
	gid := uuid.New()
	require.NoError(t, groups.Put(context.Background(), scim.GroupRecord{ID: gid, Name: "G"}))
	require.NoError(t, groups.AddMembers(context.Background(), gid, []uuid.UUID{uA}))

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaPatchOp + `"],
        "Operations":[{
            "op":"replace",
            "path":"members",
            "value":[{"value":"` + uB.String() + `"}]
        }]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPatch,
		scim.RouteGroups+"/"+gid.String(), body))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "got body=%s", rec.Body.String())

	members, _ := groups.Members(context.Background(), gid)
	require.Len(t, members, 1)
	assert.Equal(t, uB, members[0].UserID)
}

func TestPatchGroupRemoveAllMembers(t *testing.T) {
	t.Parallel()
	h, users, groups := newGroupsHandlers(t)
	uA := uuid.New()
	users.Insert(scim.UserRecord{ID: uA, Email: "alice@x"})
	gid := uuid.New()
	require.NoError(t, groups.Put(context.Background(), scim.GroupRecord{ID: gid, Name: "G"}))
	require.NoError(t, groups.AddMembers(context.Background(), gid, []uuid.UUID{uA}))

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaPatchOp + `"],
        "Operations":[{"op":"remove","path":"members"}]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPatch,
		scim.RouteGroups+"/"+gid.String(), body))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "got body=%s", rec.Body.String())

	members, _ := groups.Members(context.Background(), gid)
	assert.Empty(t, members)
}

func TestPatchGroupRemoveSpecificMember(t *testing.T) {
	t.Parallel()
	h, users, groups := newGroupsHandlers(t)
	uA, uB := uuid.New(), uuid.New()
	users.Insert(scim.UserRecord{ID: uA, Email: "alice@x"})
	users.Insert(scim.UserRecord{ID: uB, Email: "bob@x"})
	gid := uuid.New()
	require.NoError(t, groups.Put(context.Background(), scim.GroupRecord{ID: gid, Name: "G"}))
	require.NoError(t, groups.AddMembers(context.Background(), gid, []uuid.UUID{uA, uB}))

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaPatchOp + `"],
        "Operations":[{
            "op":"remove",
            "path":"members[value eq \"` + uA.String() + `\"]"
        }]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPatch,
		scim.RouteGroups+"/"+gid.String(), body))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "got body=%s", rec.Body.String())

	members, _ := groups.Members(context.Background(), gid)
	require.Len(t, members, 1)
	assert.Equal(t, uB, members[0].UserID)
}

func TestPatchGroupRemoveUnsupportedPath(t *testing.T) {
	t.Parallel()
	h, _, groups := newGroupsHandlers(t)
	gid := uuid.New()
	groups.Insert(scim.GroupRecord{ID: gid, Name: "G"})
	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaPatchOp + `"],
        "Operations":[{"op":"remove","path":"displayName"}]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPatch,
		scim.RouteGroups+"/"+gid.String(), body))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	var err scim.ScimError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&err))
	require.NotNil(t, err.ScimType)
	assert.Equal(t, "invalidPath", *err.ScimType)
}

func TestPatchGroupAddRejectsNonMembersPath(t *testing.T) {
	t.Parallel()
	h, _, groups := newGroupsHandlers(t)
	gid := uuid.New()
	groups.Insert(scim.GroupRecord{ID: gid, Name: "G"})
	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaPatchOp + `"],
        "Operations":[{"op":"add","path":"externalId","value":"x"}]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPatch,
		scim.RouteGroups+"/"+gid.String(), body))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	var err scim.ScimError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&err))
	require.NotNil(t, err.ScimType)
	assert.Equal(t, "mutability", *err.ScimType)
}

func TestPatchGroupPathlessReplaceObject(t *testing.T) {
	t.Parallel()
	h, users, groups := newGroupsHandlers(t)
	uA := uuid.New()
	users.Insert(scim.UserRecord{ID: uA, Email: "alice@x"})
	gid := uuid.New()
	require.NoError(t, groups.Put(context.Background(), scim.GroupRecord{ID: gid, Name: "Old"}))

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaPatchOp + `"],
        "Operations":[{
            "op":"replace",
            "value":{
                "displayName":"New",
                "externalId":"sso-1",
                "members":[{"value":"` + uA.String() + `"}]
            }
        }]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPatch,
		scim.RouteGroups+"/"+gid.String(), body))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "got body=%s", rec.Body.String())

	persisted, _ := groups.Get(context.Background(), gid)
	require.NotNil(t, persisted)
	assert.Equal(t, "New", persisted.Name)
	require.NotNil(t, persisted.ScimExternalID)
	assert.Equal(t, "sso-1", *persisted.ScimExternalID)
	members, _ := groups.Members(context.Background(), gid)
	require.Len(t, members, 1)
}

// --- DeleteGroup --------------------------------------------------------

func TestDeleteGroupHardDelete(t *testing.T) {
	t.Parallel()
	h, _, groups := newGroupsHandlers(t)
	gid := uuid.New()
	groups.Insert(scim.GroupRecord{ID: gid, Name: "G"})

	req := adminCtx(t, httptest.NewRequest(http.MethodDelete,
		scim.RouteGroups+"/"+gid.String(), nil))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	got, _ := groups.Get(context.Background(), gid)
	assert.Nil(t, got, "DELETE should hard-remove the row (matches Rust DELETE FROM groups)")
}

func TestDeleteGroupNotFound(t *testing.T) {
	t.Parallel()
	h, _, _ := newGroupsHandlers(t)
	req := adminCtx(t, httptest.NewRequest(http.MethodDelete,
		scim.RouteGroups+"/"+uuid.New().String(), nil))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDeleteGroupBadUUID(t *testing.T) {
	t.Parallel()
	h, _, _ := newGroupsHandlers(t)
	req := adminCtx(t, httptest.NewRequest(http.MethodDelete,
		scim.RouteGroups+"/not-a-uuid", nil))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// --- claim gates --------------------------------------------------------

func TestGroupEndpointsRejectMissingClaims(t *testing.T) {
	t.Parallel()
	h, _, groups := newGroupsHandlers(t)
	gid := uuid.New()
	groups.Insert(scim.GroupRecord{ID: gid, Name: "G"})

	cases := []struct {
		name string
		req  *http.Request
	}{
		{"GET", httptest.NewRequest(http.MethodGet, scim.RouteGroups+"/"+gid.String(), nil)},
		{"LIST", httptest.NewRequest(http.MethodGet, scim.RouteGroups, nil)},
		{"POST", httptest.NewRequest(http.MethodPost, scim.RouteGroups,
			strings.NewReader(`{"schemas":["`+scim.SchemaGroup+`"],"displayName":"x"}`))},
		{"DELETE", httptest.NewRequest(http.MethodDelete, scim.RouteGroups+"/"+gid.String(), nil)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			groupsRouter(h).ServeHTTP(rec, c.req)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}
}

func TestGroupEndpointsServiceUnavailableWhenStoreNil(t *testing.T) {
	t.Parallel()
	h := &scim.Handlers{BaseURL: baseURL, Users: scim.NewInMemoryUserStore()} // no Groups
	req := adminCtx(t, httptest.NewRequest(http.MethodGet, scim.RouteGroups, nil))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestNonAdminViewerCannotWriteGroups(t *testing.T) {
	t.Parallel()
	h, _, _ := newGroupsHandlers(t)
	body := strings.NewReader(`{"schemas":["`+scim.SchemaGroup+`"],"displayName":"x"}`)
	req := httptest.NewRequest(http.MethodPost, scim.RouteGroups, body)
	claims := &authmw.Claims{Sub: uuid.New(), Roles: []string{"viewer"}}
	req = req.WithContext(authmw.ContextWithClaims(req.Context(), claims))
	rec := httptest.NewRecorder()
	groupsRouter(h).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}
