package scim

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

// Organizations is an optional field on Handlers (slice 3.7b.3.2).
// When non-nil, CreateUser / PatchUser delegate to it for the
// `attributes.scim.openfoundry.organizationSlug` resolution path.
// When nil, slug resolution surfaces 400 invalidValue.
//
// We add this by extension rather than a constructor argument so
// the previous slice's tests (which build &Handlers{...} directly)
// still compile.

// CreateUser handles `POST /scim/v2/Users`. Mirrors fn create_user.
//
// Behaviour summary:
//   - 401/403 gates via RequireScimWriter (CedarAuthZ AdminGuard
//     mounts in the router).
//   - Required: userName OR a primary email. Else 400 invalidValue.
//   - Idempotency: when `externalId` is present and resolves to an
//     existing user, fold the new payload into that row instead of
//     inserting a new one. Returns 200 on idempotent merge.
//   - Insert path: bumps a fresh UUIDv7 id, persists the canonical
//     SCIM row, returns 201 Created with the SCIM-shape body.
//   - userName uniqueness: ErrUserNameTaken from the store maps to
//     409 + scimType="uniqueness".
func (h *Handlers) CreateUser(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeScimError(w, http.StatusUnauthorized, "missing claims", nil)
		return
	}
	if !RequireScimWriter(claims) {
		writeScimError(w, http.StatusForbidden, "missing scim:write permission", nil)
		return
	}
	var body ScimUser
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		invalid := "invalidSyntax"
		writeScimError(w, http.StatusBadRequest, "invalid SCIM user payload", &invalid)
		return
	}

	email := pickCreateEmail(&body)
	if email == "" {
		invalid := "invalidValue"
		writeScimError(w, http.StatusBadRequest, "userName or primary email is required", &invalid)
		return
	}
	displayName := DisplayNameFromScim(body.Name, body.UserName)
	attributes := UserAttributesFromScim(&body)
	externalID := ScimExternalIDFromUser(&body)
	orgID, scimErr := ResolveUserOrganizationID(r.Context(), h.Organizations, &body, attributes)
	if scimErr != nil {
		writeScimErrorPayload(w, parseStatus(scimErr.Status), *scimErr)
		return
	}

	// Idempotency: when externalId matches an existing row, merge
	// in place and return 200.
	if externalID != nil {
		existing, err := h.Users.GetByExternalID(r.Context(), *externalID)
		if err != nil {
			slog.Error("scim: load by externalId failed", slog.String("error", err.Error()))
			writeScimError(w, http.StatusInternalServerError, "failed to create user", nil)
			return
		}
		if existing != nil {
			active := true
			if body.Active != nil {
				active = *body.Active
			}
			MergeScimUserRecord(existing, email, displayName, active, orgID, attributes)
			existing.ScimExternalID = externalID
			if err := h.Users.Put(r.Context(), *existing); err != nil {
				if errors.Is(err, ErrUserNameTaken) {
					t := "uniqueness"
					writeScimError(w, http.StatusConflict, "userName already exists", &t)
					return
				}
				slog.Error("scim: idempotent update failed", slog.String("error", err.Error()))
				writeScimError(w, http.StatusInternalServerError, "failed to create user", nil)
				return
			}
			writeJSON(w, http.StatusOK, UserToScim(*existing, h.BaseURL))
			return
		}
	}

	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	active := true
	if body.Active != nil {
		active = *body.Active
	}
	rec := UserRecord{
		ID:             id,
		Email:          email,
		Name:           displayName,
		IsActive:       active,
		OrganizationID: orgID,
		Attributes:     attributes,
		ScimExternalID: externalID,
	}
	if err := h.Users.Put(r.Context(), rec); err != nil {
		if errors.Is(err, ErrUserNameTaken) {
			t := "uniqueness"
			writeScimError(w, http.StatusConflict, "userName already exists", &t)
			return
		}
		slog.Error("scim: create failed", slog.String("error", err.Error()))
		writeScimError(w, http.StatusInternalServerError, "failed to create user", nil)
		return
	}
	persisted, err := h.Users.Get(r.Context(), id)
	if err != nil || persisted == nil {
		slog.Error("scim: read-back of created user failed",
			slog.String("error", errString(err)))
		writeScimError(w, http.StatusInternalServerError, "failed to create user", nil)
		return
	}
	writeJSON(w, http.StatusCreated, UserToScim(*persisted, h.BaseURL))
}

// PatchUser handles `PATCH /scim/v2/Users/{id}`. Mirrors fn
// patch_user.
//
// The body MUST carry the PatchOp schema URN. Each operation is
// applied via ApplyUserPatch in document order. After all ops
// land we re-resolve organization_id (in case the openfoundry
// extension slug changed) and re-extract externalId from
// attributes.
func (h *Handlers) PatchUser(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeScimError(w, http.StatusUnauthorized, "missing claims", nil)
		return
	}
	if !RequireScimWriter(claims) {
		writeScimError(w, http.StatusForbidden, "missing scim:write permission", nil)
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		t := "invalidValue"
		writeScimError(w, http.StatusBadRequest, "user id is not a valid UUID", &t)
		return
	}

	var body ScimPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t := "invalidSyntax"
		writeScimError(w, http.StatusBadRequest, "invalid SCIM patch payload", &t)
		return
	}
	if !containsSchema(body.Schemas, SchemaPatchOp) {
		t := "invalidSyntax"
		writeScimError(w, http.StatusBadRequest, "PATCH request is missing PatchOp schema", &t)
		return
	}

	rec, err := h.Users.Get(r.Context(), id)
	if err != nil {
		slog.Error("scim: load user for patch failed", slog.String("error", err.Error()))
		writeScimError(w, http.StatusInternalServerError, "failed to patch user", nil)
		return
	}
	if rec == nil {
		writeScimError(w, http.StatusNotFound, "SCIM user not found", nil)
		return
	}

	for _, op := range body.Operations {
		if scimErr := ApplyUserPatch(rec, op); scimErr != nil {
			writeScimErrorPayload(w, parseStatus(scimErr.Status), *scimErr)
			return
		}
	}

	// Re-resolve org from the (possibly mutated) attributes.
	if newOrg, scimErr := resolveOrganizationFromAttributes(r.Context(), h.Organizations, rec.Attributes); scimErr != nil {
		writeScimErrorPayload(w, parseStatus(scimErr.Status), *scimErr)
		return
	} else if newOrg != nil {
		rec.OrganizationID = newOrg
	}
	rec.ScimExternalID = ExternalIDFromAttributes(rec.Attributes)

	if err := h.Users.Put(r.Context(), *rec); err != nil {
		if errors.Is(err, ErrUserNameTaken) {
			t := "uniqueness"
			writeScimError(w, http.StatusConflict, "userName already exists", &t)
			return
		}
		slog.Error("scim: patch persist failed", slog.String("error", err.Error()))
		writeScimError(w, http.StatusInternalServerError, "failed to patch user", nil)
		return
	}
	writeJSON(w, http.StatusOK, UserToScim(*rec, h.BaseURL))
}

// DeleteUser handles `DELETE /scim/v2/Users/{id}`. Mirrors fn
// delete_user — soft-deletes via store.SoftDelete (sets
// is_active=false). 204 No Content on success, 404 when no row
// matches the id.
func (h *Handlers) DeleteUser(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeScimError(w, http.StatusUnauthorized, "missing claims", nil)
		return
	}
	if !RequireScimWriter(claims) {
		writeScimError(w, http.StatusForbidden, "missing scim:write permission", nil)
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		t := "invalidValue"
		writeScimError(w, http.StatusBadRequest, "user id is not a valid UUID", &t)
		return
	}
	deleted, err := h.Users.SoftDelete(r.Context(), id)
	if err != nil {
		slog.Error("scim: soft-delete failed", slog.String("error", err.Error()))
		writeScimError(w, http.StatusInternalServerError, "failed to delete user", nil)
		return
	}
	if !deleted {
		writeScimError(w, http.StatusNotFound, "SCIM user not found", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── helpers ────────────────────────────────────────────────────────

// pickCreateEmail mirrors the Rust let-binding at the top of
// create_user — primary_email OR userName, trimmed-non-empty, or
// empty string when neither is usable.
func pickCreateEmail(user *ScimUser) string {
	if v := PrimaryEmail(user); v != nil {
		trimmed := strings.TrimSpace(*v)
		if trimmed != "" {
			return trimmed
		}
	}
	return strings.TrimSpace(user.UserName)
}

// containsSchema returns true when `target` appears in `schemas`.
// Used by PatchUser to enforce the PatchOp URN.
func containsSchema(schemas []string, target string) bool {
	for _, s := range schemas {
		if s == target {
			return true
		}
	}
	return false
}

// parseStatus turns a "400" / "404" / etc. ScimError.Status field
// back into an int for the http.ResponseWriter.WriteHeader call.
// Falls back to 400 on parse failure (every constructor on this
// package emits a numeric string).
func parseStatus(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return http.StatusBadRequest
		}
		n = n*10 + int(r-'0')
	}
	if n == 0 {
		return http.StatusBadRequest
	}
	return n
}

// errString safely renders a possibly-nil error.
func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// silence unused linter when context import was only in the
// receiver chains.
var _ = context.Background
