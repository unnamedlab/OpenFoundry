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

// writeRouter mounts the read + write surfaces so the tests
// exercise the same chi setup as production.
func writeRouter(h *scim.Handlers) http.Handler {
	r := chi.NewRouter()
	r.Get(scim.RouteUsers+"/{id}", h.GetUser)
	r.Get(scim.RouteUsers, h.ListUsers)
	r.Post(scim.RouteUsers, h.CreateUser)
	r.Patch(scim.RouteUsers+"/{id}", h.PatchUser)
	r.Delete(scim.RouteUsers+"/{id}", h.DeleteUser)
	return r
}

func adminCtx(t *testing.T, req *http.Request) *http.Request {
	t.Helper()
	claims := &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}}
	return req.WithContext(authmw.ContextWithClaims(req.Context(), claims))
}

// --- helpers (pure-logic) ----------------------------------------------

func TestPrimaryEmailPicksPrimaryFlag(t *testing.T) {
	t.Parallel()
	primaryT, primaryF := true, false
	user := &scim.ScimUser{
		Emails: []scim.ScimEmail{
			{Value: "secondary@x", Primary: &primaryF},
			{Value: "primary@x", Primary: &primaryT},
		},
	}
	got := scim.PrimaryEmail(user)
	require.NotNil(t, got)
	assert.Equal(t, "primary@x", *got)
}

func TestPrimaryEmailFallsBackToFirst(t *testing.T) {
	t.Parallel()
	user := &scim.ScimUser{
		Emails: []scim.ScimEmail{
			{Value: "first@x"},
			{Value: "second@x"},
		},
	}
	got := scim.PrimaryEmail(user)
	require.NotNil(t, got)
	assert.Equal(t, "first@x", *got)
}

func TestPrimaryEmailNilWhenEmpty(t *testing.T) {
	t.Parallel()
	assert.Nil(t, scim.PrimaryEmail(&scim.ScimUser{}))
}

func TestDisplayNameFromScimFormattedWins(t *testing.T) {
	t.Parallel()
	formatted := "Alice Doe"
	given := "AliceWrong"
	name := &scim.ScimName{GivenName: &given, Formatted: &formatted}
	assert.Equal(t, "Alice Doe", scim.DisplayNameFromScim(name, "fallback@x"))
}

func TestDisplayNameFromScimJoinsGivenAndFamily(t *testing.T) {
	t.Parallel()
	given := "Alice"
	family := "Doe"
	name := &scim.ScimName{GivenName: &given, FamilyName: &family}
	assert.Equal(t, "Alice Doe", scim.DisplayNameFromScim(name, "fallback@x"))
}

func TestDisplayNameFromScimFallsBack(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "user@example.com",
		scim.DisplayNameFromScim(nil, "user@example.com"))
	assert.Equal(t, "user@example.com",
		scim.DisplayNameFromScim(&scim.ScimName{}, "user@example.com"))
}

func TestUserAttributesFromScimRoundTripsName(t *testing.T) {
	t.Parallel()
	given := "Alice"
	family := "Doe"
	user := &scim.ScimUser{
		UserName: "alice@x",
		Name:     &scim.ScimName{GivenName: &given, FamilyName: &family},
	}
	attrs := scim.UserAttributesFromScim(user)
	// Confirm the name is nested under attributes.scim.name.
	var obj struct {
		Scim struct {
			Name scim.ScimName `json:"name"`
		} `json:"scim"`
	}
	require.NoError(t, json.Unmarshal(attrs, &obj))
	require.NotNil(t, obj.Scim.Name.GivenName)
	assert.Equal(t, "Alice", *obj.Scim.Name.GivenName)
}

func TestUserAttributesFromScimCarriesExtension(t *testing.T) {
	t.Parallel()
	ext := json.RawMessage(`{"organizationId":"org-7","department":"Eng"}`)
	user := &scim.ScimUser{
		UserName:   "alice@x",
		Extensions: map[string]json.RawMessage{scim.SchemaOpenfoundryUserExtension: ext},
	}
	attrs := scim.UserAttributesFromScim(user)
	var obj struct {
		Scim struct {
			Openfoundry json.RawMessage `json:"openfoundry"`
		} `json:"scim"`
	}
	require.NoError(t, json.Unmarshal(attrs, &obj))
	assert.JSONEq(t, string(ext), string(obj.Scim.Openfoundry))
}

func TestScimExternalIDFromUserTrimAndFilter(t *testing.T) {
	t.Parallel()
	notSet := scim.ScimExternalIDFromUser(&scim.ScimUser{})
	assert.Nil(t, notSet)

	id := "  ext-7  "
	got := scim.ScimExternalIDFromUser(&scim.ScimUser{ExternalID: &id})
	require.NotNil(t, got)
	assert.Equal(t, "ext-7", *got)

	empty := "   "
	assert.Nil(t, scim.ScimExternalIDFromUser(&scim.ScimUser{ExternalID: &empty}))
}

// --- Organization resolution -------------------------------------------

func TestResolveUserOrganizationFromExtensionUUID(t *testing.T) {
	t.Parallel()
	org := uuid.New()
	user := &scim.ScimUser{
		Extensions: map[string]json.RawMessage{
			scim.SchemaOpenfoundryUserExtension: json.RawMessage(
				`{"organizationId":"` + org.String() + `"}`),
		},
	}
	got, scimErr := scim.ResolveUserOrganizationID(context.Background(), nil, user, json.RawMessage(`{}`))
	require.Nil(t, scimErr)
	require.NotNil(t, got)
	assert.Equal(t, org, *got)
}

func TestResolveUserOrganizationRejectsBadUUID(t *testing.T) {
	t.Parallel()
	user := &scim.ScimUser{
		Extensions: map[string]json.RawMessage{
			scim.SchemaOpenfoundryUserExtension: json.RawMessage(
				`{"organizationId":"not-a-uuid"}`),
		},
	}
	_, scimErr := scim.ResolveUserOrganizationID(context.Background(), nil, user, json.RawMessage(`{}`))
	require.NotNil(t, scimErr)
	assert.Equal(t, "400", scimErr.Status)
	require.NotNil(t, scimErr.ScimType)
	assert.Equal(t, "invalidValue", *scimErr.ScimType)
}

func TestResolveUserOrganizationFromAttributesSlug(t *testing.T) {
	t.Parallel()
	resolver := scim.NewInMemoryOrganizationResolver()
	id := uuid.New()
	resolver.Insert("acme", id)
	attrs := json.RawMessage(`{"scim":{"openfoundry":{"organizationSlug":"acme"}}}`)
	got, scimErr := scim.ResolveUserOrganizationID(context.Background(), resolver, &scim.ScimUser{}, attrs)
	require.Nil(t, scimErr)
	require.NotNil(t, got)
	assert.Equal(t, id, *got)
}

func TestResolveUserOrganizationUnknownSlug(t *testing.T) {
	t.Parallel()
	resolver := scim.NewInMemoryOrganizationResolver()
	attrs := json.RawMessage(`{"scim":{"openfoundry":{"organizationSlug":"missing"}}}`)
	_, scimErr := scim.ResolveUserOrganizationID(context.Background(), resolver, &scim.ScimUser{}, attrs)
	require.NotNil(t, scimErr)
	assert.Equal(t, "400", scimErr.Status)
	assert.Contains(t, scimErr.Detail, "missing")
}

// --- CreateUser ---------------------------------------------------------

func TestCreateUserHappyPath(t *testing.T) {
	t.Parallel()
	store := scim.NewInMemoryUserStore()
	h := &scim.Handlers{BaseURL: baseURL, Users: store}

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaUser + `"],
        "userName":"alice@acme.com",
        "name":{"givenName":"Alice","familyName":"Doe"},
        "emails":[{"value":"alice@acme.com","primary":true,"type":"work"}],
        "active":true
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPost, scim.RouteUsers, body))
	req.Header.Set("Content-Type", "application/scim+json")
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "got body=%s", rec.Body.String())

	var got scim.ScimUser
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	require.NotNil(t, got.ID)
	assert.Equal(t, "alice@acme.com", got.UserName)
	require.NotNil(t, got.Active)
	assert.True(t, *got.Active)
	require.NotNil(t, got.Name)
	require.NotNil(t, got.Name.GivenName)
	assert.Equal(t, "Alice", *got.Name.GivenName)
}

func TestCreateUserRejectsMissingUserName(t *testing.T) {
	t.Parallel()
	h := &scim.Handlers{BaseURL: baseURL, Users: scim.NewInMemoryUserStore()}
	body := strings.NewReader(`{"schemas":["` + scim.SchemaUser + `"]}`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPost, scim.RouteUsers, body))
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	var err scim.ScimError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&err))
	require.NotNil(t, err.ScimType)
	assert.Equal(t, "invalidValue", *err.ScimType)
}

func TestCreateUserConflictOnDuplicateUserName(t *testing.T) {
	t.Parallel()
	store := scim.NewInMemoryUserStore()
	store.Insert(scim.UserRecord{ID: uuid.New(), Email: "alice@acme.com", IsActive: true})
	h := &scim.Handlers{BaseURL: baseURL, Users: store}

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaUser + `"],
        "userName":"alice@acme.com",
        "active":true
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPost, scim.RouteUsers, body))
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code)
	var err scim.ScimError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&err))
	require.NotNil(t, err.ScimType)
	assert.Equal(t, "uniqueness", *err.ScimType)
}

func TestCreateUserIdempotentOnExternalID(t *testing.T) {
	t.Parallel()
	store := scim.NewInMemoryUserStore()
	ext := "scim-42"
	existingID := uuid.New()
	store.Insert(scim.UserRecord{
		ID: existingID, Email: "alice@old", Name: "Old",
		ScimExternalID: &ext, IsActive: true,
	})
	h := &scim.Handlers{BaseURL: baseURL, Users: store}

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaUser + `"],
        "userName":"alice@new",
        "externalId":"scim-42",
        "name":{"givenName":"Alice","familyName":"Doe"},
        "active":true
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPost, scim.RouteUsers, body))
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	// Idempotent merge → 200 OK (NOT 201).
	require.Equal(t, http.StatusOK, rec.Code, "got body=%s", rec.Body.String())

	var got scim.ScimUser
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	require.NotNil(t, got.ID)
	assert.Equal(t, existingID.String(), *got.ID)
	assert.Equal(t, "alice@new", got.UserName)
}

func TestCreateUserResolvesOrganizationFromExtension(t *testing.T) {
	t.Parallel()
	org := uuid.New()
	resolver := scim.NewInMemoryOrganizationResolver()
	resolver.Insert("acme", org)
	store := scim.NewInMemoryUserStore()
	h := &scim.Handlers{BaseURL: baseURL, Users: store, Organizations: resolver}

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaUser + `"],
        "userName":"alice@x",
        "active":true,
        "` + scim.SchemaOpenfoundryUserExtension + `":{"organizationId":"` + org.String() + `"}
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPost, scim.RouteUsers, body))
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "got body=%s", rec.Body.String())

	rows, _, err := store.List(context.Background(), nil, 1, 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].OrganizationID)
	assert.Equal(t, org, *rows[0].OrganizationID)
}

func TestCreateUserMissingClaimsIs401(t *testing.T) {
	t.Parallel()
	h := &scim.Handlers{BaseURL: baseURL, Users: scim.NewInMemoryUserStore()}
	body := strings.NewReader(`{"schemas":["` + scim.SchemaUser + `"],"userName":"a@b"}`)
	req := httptest.NewRequest(http.MethodPost, scim.RouteUsers, body)
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCreateUserNoPermissionsIs403(t *testing.T) {
	t.Parallel()
	h := &scim.Handlers{BaseURL: baseURL, Users: scim.NewInMemoryUserStore()}
	body := strings.NewReader(`{"schemas":["` + scim.SchemaUser + `"],"userName":"a@b"}`)
	req := httptest.NewRequest(http.MethodPost, scim.RouteUsers, body)
	claims := &authmw.Claims{Sub: uuid.New(), Roles: []string{"viewer"}}
	req = req.WithContext(authmw.ContextWithClaims(req.Context(), claims))
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// --- PatchUser ----------------------------------------------------------

func TestPatchUserReplaceUserName(t *testing.T) {
	t.Parallel()
	store := scim.NewInMemoryUserStore()
	id := uuid.New()
	store.Insert(scim.UserRecord{ID: id, Email: "old@x", Name: "Old", IsActive: true})
	h := &scim.Handlers{BaseURL: baseURL, Users: store}

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaPatchOp + `"],
        "Operations":[{"op":"replace","path":"userName","value":"new@x"}]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPatch,
		scim.RouteUsers+"/"+id.String(), body))
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "got body=%s", rec.Body.String())

	persisted, _ := store.Get(context.Background(), id)
	require.NotNil(t, persisted)
	assert.Equal(t, "new@x", persisted.Email)
}

func TestPatchUserPathlessReplaceObject(t *testing.T) {
	t.Parallel()
	store := scim.NewInMemoryUserStore()
	id := uuid.New()
	store.Insert(scim.UserRecord{ID: id, Email: "old@x", Name: "Old", IsActive: true})
	h := &scim.Handlers{BaseURL: baseURL, Users: store}

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaPatchOp + `"],
        "Operations":[{
            "op":"replace",
            "value":{"userName":"new@x","active":false}
        }]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPatch,
		scim.RouteUsers+"/"+id.String(), body))
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "got body=%s", rec.Body.String())

	persisted, _ := store.Get(context.Background(), id)
	require.NotNil(t, persisted)
	assert.Equal(t, "new@x", persisted.Email)
	assert.False(t, persisted.IsActive)
}

func TestPatchUserNameUpdatesAttributes(t *testing.T) {
	t.Parallel()
	store := scim.NewInMemoryUserStore()
	id := uuid.New()
	store.Insert(scim.UserRecord{ID: id, Email: "alice@x", Name: "Alice"})
	h := &scim.Handlers{BaseURL: baseURL, Users: store}

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaPatchOp + `"],
        "Operations":[{
            "op":"replace",
            "path":"name",
            "value":{"givenName":"Alicia","familyName":"Doe"}
        }]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPatch,
		scim.RouteUsers+"/"+id.String(), body))
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "got body=%s", rec.Body.String())

	persisted, _ := store.Get(context.Background(), id)
	require.NotNil(t, persisted)
	assert.Equal(t, "Alicia Doe", persisted.Name)
	// Name must also land in attributes.scim.name.
	var attrs struct {
		Scim struct {
			Name scim.ScimName `json:"name"`
		} `json:"scim"`
	}
	require.NoError(t, json.Unmarshal(persisted.Attributes, &attrs))
	require.NotNil(t, attrs.Scim.Name.GivenName)
	assert.Equal(t, "Alicia", *attrs.Scim.Name.GivenName)
}

func TestPatchUserEmailsPicksPrimary(t *testing.T) {
	t.Parallel()
	store := scim.NewInMemoryUserStore()
	id := uuid.New()
	store.Insert(scim.UserRecord{ID: id, Email: "alice@x"})
	h := &scim.Handlers{BaseURL: baseURL, Users: store}

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaPatchOp + `"],
        "Operations":[{
            "op":"replace",
            "path":"emails",
            "value":[
                {"value":"home@x","primary":false},
                {"value":"work@x","primary":true}
            ]
        }]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPatch,
		scim.RouteUsers+"/"+id.String(), body))
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "got body=%s", rec.Body.String())

	persisted, _ := store.Get(context.Background(), id)
	require.NotNil(t, persisted)
	assert.Equal(t, "work@x", persisted.Email)
}

func TestPatchUserExternalIDLandsUnderAttributes(t *testing.T) {
	t.Parallel()
	store := scim.NewInMemoryUserStore()
	id := uuid.New()
	store.Insert(scim.UserRecord{ID: id, Email: "alice@x"})
	h := &scim.Handlers{BaseURL: baseURL, Users: store}

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaPatchOp + `"],
        "Operations":[{
            "op":"replace",
            "path":"externalId",
            "value":"ext-100"
        }]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPatch,
		scim.RouteUsers+"/"+id.String(), body))
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "got body=%s", rec.Body.String())

	persisted, _ := store.Get(context.Background(), id)
	require.NotNil(t, persisted)
	require.NotNil(t, persisted.ScimExternalID)
	assert.Equal(t, "ext-100", *persisted.ScimExternalID)
	// External id should also reach the wire shape.
	scimUser := scim.UserToScim(*persisted, baseURL)
	require.NotNil(t, scimUser.ExternalID)
	assert.Equal(t, "ext-100", *scimUser.ExternalID)
}

func TestPatchUserOpenfoundryExtensionWholeObject(t *testing.T) {
	t.Parallel()
	store := scim.NewInMemoryUserStore()
	id := uuid.New()
	store.Insert(scim.UserRecord{ID: id, Email: "alice@x"})
	h := &scim.Handlers{BaseURL: baseURL, Users: store}

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaPatchOp + `"],
        "Operations":[{
            "op":"replace",
            "path":"` + scim.SchemaOpenfoundryUserExtension + `",
            "value":{"department":"Eng","jobTitle":"SRE"}
        }]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPatch,
		scim.RouteUsers+"/"+id.String(), body))
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "got body=%s", rec.Body.String())
}

func TestPatchUserOpenfoundryExtensionField(t *testing.T) {
	t.Parallel()
	store := scim.NewInMemoryUserStore()
	id := uuid.New()
	store.Insert(scim.UserRecord{
		ID: id, Email: "alice@x",
		Attributes: json.RawMessage(`{"scim":{"openfoundry":{"jobTitle":"old"}}}`),
	})
	h := &scim.Handlers{BaseURL: baseURL, Users: store}

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaPatchOp + `"],
        "Operations":[{
            "op":"replace",
            "path":"` + scim.SchemaOpenfoundryUserExtension + `.jobTitle",
            "value":"SRE Lead"
        }]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPatch,
		scim.RouteUsers+"/"+id.String(), body))
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "got body=%s", rec.Body.String())

	persisted, _ := store.Get(context.Background(), id)
	require.NotNil(t, persisted)
	var obj struct {
		Scim struct {
			Openfoundry map[string]string `json:"openfoundry"`
		} `json:"scim"`
	}
	require.NoError(t, json.Unmarshal(persisted.Attributes, &obj))
	assert.Equal(t, "SRE Lead", obj.Scim.Openfoundry["jobTitle"])
}

func TestPatchUserUnsupportedOpRejected(t *testing.T) {
	t.Parallel()
	store := scim.NewInMemoryUserStore()
	id := uuid.New()
	store.Insert(scim.UserRecord{ID: id, Email: "alice@x"})
	h := &scim.Handlers{BaseURL: baseURL, Users: store}

	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaPatchOp + `"],
        "Operations":[{"op":"remove","path":"userName"}]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPatch,
		scim.RouteUsers+"/"+id.String(), body))
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	var err scim.ScimError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&err))
	require.NotNil(t, err.ScimType)
	assert.Equal(t, "mutability", *err.ScimType)
}

func TestPatchUserMissingPatchOpSchemaRejected(t *testing.T) {
	t.Parallel()
	store := scim.NewInMemoryUserStore()
	id := uuid.New()
	store.Insert(scim.UserRecord{ID: id, Email: "alice@x"})
	h := &scim.Handlers{BaseURL: baseURL, Users: store}

	body := strings.NewReader(`{
        "schemas":["urn:wrong"],
        "Operations":[{"op":"replace","path":"userName","value":"new@x"}]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPatch,
		scim.RouteUsers+"/"+id.String(), body))
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	var err scim.ScimError
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&err))
	assert.Contains(t, err.Detail, "PatchOp schema")
}

func TestPatchUserNotFoundIs404(t *testing.T) {
	t.Parallel()
	h := &scim.Handlers{BaseURL: baseURL, Users: scim.NewInMemoryUserStore()}
	body := strings.NewReader(`{
        "schemas":["` + scim.SchemaPatchOp + `"],
        "Operations":[{"op":"replace","path":"userName","value":"a@b"}]
    }`)
	req := adminCtx(t, httptest.NewRequest(http.MethodPatch,
		scim.RouteUsers+"/"+uuid.New().String(), body))
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- DeleteUser ---------------------------------------------------------

func TestDeleteUserDeactivates(t *testing.T) {
	t.Parallel()
	store := scim.NewInMemoryUserStore()
	id := uuid.New()
	store.Insert(scim.UserRecord{ID: id, Email: "alice@x", IsActive: true})
	h := &scim.Handlers{BaseURL: baseURL, Users: store}

	req := adminCtx(t, httptest.NewRequest(http.MethodDelete,
		scim.RouteUsers+"/"+id.String(), nil))
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	persisted, _ := store.Get(context.Background(), id)
	require.NotNil(t, persisted, "soft-delete must keep the row")
	assert.False(t, persisted.IsActive)
}

func TestDeleteUserNotFoundIs404(t *testing.T) {
	t.Parallel()
	h := &scim.Handlers{BaseURL: baseURL, Users: scim.NewInMemoryUserStore()}
	req := adminCtx(t, httptest.NewRequest(http.MethodDelete,
		scim.RouteUsers+"/"+uuid.New().String(), nil))
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDeleteUserBadUUIDIs400(t *testing.T) {
	t.Parallel()
	h := &scim.Handlers{BaseURL: baseURL, Users: scim.NewInMemoryUserStore()}
	req := adminCtx(t, httptest.NewRequest(http.MethodDelete,
		scim.RouteUsers+"/not-a-uuid", nil))
	rec := httptest.NewRecorder()
	writeRouter(h).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestIsUniqueViolationClassifies(t *testing.T) {
	t.Parallel()
	assert.True(t, scim.IsUniqueViolation(scim.ErrUserNameTaken))
	assert.False(t, scim.IsUniqueViolation(nil))
}
