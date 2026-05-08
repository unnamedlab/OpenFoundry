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

// AttachGroupStore is a tiny init helper: lets the consuming
// server call &Handlers{...}.AttachGroupStore(store) without
// reaching into a struct field that would otherwise change the
// public layout. Returns the receiver so calls can chain.
//
// Uses a separate field rather than a constructor so the read-
// side tests (which build &Handlers{BaseURL, Users} directly)
// stay compatible.
func (h *Handlers) AttachGroupStore(store GroupStore) *Handlers {
	h.Groups = store
	return h
}

// CreateGroup handles `POST /scim/v2/Groups`. Mirrors fn
// create_group.
//
//   - Required: displayName (trimmed-non-empty). Else 400
//     invalidValue.
//   - Idempotent on externalId: when present and resolves to an
//     existing row, fold the new payload into that row, replace
//     its members, and return 200.
//   - Insert path: bumps a fresh UUIDv7 id, persists the
//     canonical row, inserts members, returns 201.
//   - displayName uniqueness: ErrGroupNameTaken from the store
//     maps to 409 + scimType="uniqueness".
func (h *Handlers) CreateGroup(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeScimError(w, http.StatusUnauthorized, "missing claims", nil)
		return
	}
	if !RequireScimWriter(claims) {
		writeScimError(w, http.StatusForbidden, "missing scim:write permission", nil)
		return
	}
	if h.Groups == nil {
		writeScimError(w, http.StatusServiceUnavailable, "group store not configured", nil)
		return
	}

	var body ScimGroup
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t := "invalidSyntax"
		writeScimError(w, http.StatusBadRequest, "invalid SCIM group payload", &t)
		return
	}
	displayName := strings.TrimSpace(body.DisplayName)
	if displayName == "" {
		t := "invalidValue"
		writeScimError(w, http.StatusBadRequest, "displayName is required", &t)
		return
	}
	externalID := trimToOption(body.ExternalID)

	// Idempotency on externalId.
	if externalID != nil {
		existing, err := h.Groups.GetByExternalID(r.Context(), *externalID)
		if err != nil {
			slog.Error("scim: load group by externalId failed", slog.String("error", err.Error()))
			writeScimError(w, http.StatusInternalServerError, "failed to create group", nil)
			return
		}
		if existing != nil {
			existing.Name = displayName
			existing.ScimExternalID = externalID
			if err := h.Groups.Put(r.Context(), *existing); err != nil {
				if errors.Is(err, ErrGroupNameTaken) {
					t := "uniqueness"
					writeScimError(w, http.StatusConflict, "displayName already exists", &t)
					return
				}
				slog.Error("scim: idempotent group update failed", slog.String("error", err.Error()))
				writeScimError(w, http.StatusInternalServerError, "failed to create group", nil)
				return
			}
			if body.Members != nil {
				ids, scimErr := memberValuesToUUIDs(body.Members)
				if scimErr != nil {
					writeScimErrorPayload(w, parseStatus(scimErr.Status), *scimErr)
					return
				}
				if err := h.Groups.ReplaceMembers(r.Context(), existing.ID, ids); err != nil {
					if IsMemberNotFound(err) {
						t := "invalidValue"
						writeScimError(w, http.StatusBadRequest,
							"group member does not reference an existing user", &t)
						return
					}
					slog.Error("scim: idempotent member replace failed",
						slog.String("error", err.Error()))
					writeScimError(w, http.StatusInternalServerError,
						"failed to create group", nil)
					return
				}
			}
			h.writeGroupView(r.Context(), w, http.StatusOK, existing.ID)
			return
		}
	}

	// Fresh insert path.
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	rec := GroupRecord{ID: id, Name: displayName, ScimExternalID: externalID}
	if err := h.Groups.Put(r.Context(), rec); err != nil {
		if errors.Is(err, ErrGroupNameTaken) {
			t := "uniqueness"
			writeScimError(w, http.StatusConflict, "displayName already exists", &t)
			return
		}
		slog.Error("scim: create group failed", slog.String("error", err.Error()))
		writeScimError(w, http.StatusInternalServerError, "failed to create group", nil)
		return
	}
	if body.Members != nil {
		ids, scimErr := memberValuesToUUIDs(body.Members)
		if scimErr != nil {
			// Roll back the insert so the caller can retry.
			_, _ = h.Groups.Delete(r.Context(), id)
			writeScimErrorPayload(w, parseStatus(scimErr.Status), *scimErr)
			return
		}
		if err := h.Groups.AddMembers(r.Context(), id, ids); err != nil {
			_, _ = h.Groups.Delete(r.Context(), id)
			if IsMemberNotFound(err) {
				t := "invalidValue"
				writeScimError(w, http.StatusBadRequest,
					"group member does not reference an existing user", &t)
				return
			}
			slog.Error("scim: insert group members failed", slog.String("error", err.Error()))
			writeScimError(w, http.StatusInternalServerError, "failed to create group", nil)
			return
		}
	}
	h.writeGroupView(r.Context(), w, http.StatusCreated, id)
}

// GetGroup handles `GET /scim/v2/Groups/{id}`. Mirrors fn
// get_group.
func (h *Handlers) GetGroup(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeScimError(w, http.StatusUnauthorized, "missing claims", nil)
		return
	}
	if !RequireScimReader(claims) {
		writeScimError(w, http.StatusForbidden, "missing scim:read permission", nil)
		return
	}
	if h.Groups == nil {
		writeScimError(w, http.StatusServiceUnavailable, "group store not configured", nil)
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		t := "invalidValue"
		writeScimError(w, http.StatusBadRequest, "group id is not a valid UUID", &t)
		return
	}
	h.writeGroupView(r.Context(), w, http.StatusOK, id)
}

// ListGroups handles `GET /scim/v2/Groups`. Mirrors fn
// list_groups.
func (h *Handlers) ListGroups(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeScimError(w, http.StatusUnauthorized, "missing claims", nil)
		return
	}
	if !RequireScimReader(claims) {
		writeScimError(w, http.StatusForbidden, "missing scim:read permission", nil)
		return
	}
	if h.Groups == nil {
		writeScimError(w, http.StatusServiceUnavailable, "group store not configured", nil)
		return
	}
	query := parseListQuery(r)
	filter, scimErr := ParseEqFilter(query.Filter, []string{"displayName", "externalId"})
	if scimErr != nil {
		writeScimErrorPayload(w, http.StatusBadRequest, *scimErr)
		return
	}
	rows, total, err := h.Groups.List(r.Context(), filter, query.StartIndex, query.Count)
	if err != nil {
		slog.Error("scim: list groups failed", slog.String("error", err.Error()))
		writeScimError(w, http.StatusInternalServerError, "failed to list groups", nil)
		return
	}
	resources := make([]ScimGroup, 0, len(rows))
	for i := range rows {
		members, err := h.Groups.Members(r.Context(), rows[i].ID)
		if err != nil {
			slog.Error("scim: load group members failed", slog.String("error", err.Error()))
			writeScimError(w, http.StatusInternalServerError, "failed to list groups", nil)
			return
		}
		resources = append(resources, GroupToScim(rows[i], members, h.BaseURL))
	}
	writeJSON(w, http.StatusOK, NewScimList(resources, total, query.StartIndex))
}

// PatchGroup handles `PATCH /scim/v2/Groups/{id}`. Mirrors fn
// patch_group.
func (h *Handlers) PatchGroup(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeScimError(w, http.StatusUnauthorized, "missing claims", nil)
		return
	}
	if !RequireScimWriter(claims) {
		writeScimError(w, http.StatusForbidden, "missing scim:write permission", nil)
		return
	}
	if h.Groups == nil {
		writeScimError(w, http.StatusServiceUnavailable, "group store not configured", nil)
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		t := "invalidValue"
		writeScimError(w, http.StatusBadRequest, "group id is not a valid UUID", &t)
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
	rec, err := h.Groups.Get(r.Context(), id)
	if err != nil {
		slog.Error("scim: load group for patch failed", slog.String("error", err.Error()))
		writeScimError(w, http.StatusInternalServerError, "failed to patch group", nil)
		return
	}
	if rec == nil {
		writeScimError(w, http.StatusNotFound, "SCIM group not found", nil)
		return
	}
	for _, op := range body.Operations {
		if scimErr := ApplyGroupPatch(r.Context(), h.Groups, rec, op); scimErr != nil {
			writeScimErrorPayload(w, parseStatus(scimErr.Status), *scimErr)
			return
		}
	}
	if err := h.Groups.Put(r.Context(), *rec); err != nil {
		if errors.Is(err, ErrGroupNameTaken) {
			t := "uniqueness"
			writeScimError(w, http.StatusConflict, "displayName already exists", &t)
			return
		}
		slog.Error("scim: persist patched group failed", slog.String("error", err.Error()))
		writeScimError(w, http.StatusInternalServerError, "failed to patch group", nil)
		return
	}
	h.writeGroupView(r.Context(), w, http.StatusOK, rec.ID)
}

// DeleteGroup handles `DELETE /scim/v2/Groups/{id}`. Mirrors fn
// delete_group — hard delete (NOT soft-delete; matches Rust
// `DELETE FROM groups`).
func (h *Handlers) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeScimError(w, http.StatusUnauthorized, "missing claims", nil)
		return
	}
	if !RequireScimWriter(claims) {
		writeScimError(w, http.StatusForbidden, "missing scim:write permission", nil)
		return
	}
	if h.Groups == nil {
		writeScimError(w, http.StatusServiceUnavailable, "group store not configured", nil)
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		t := "invalidValue"
		writeScimError(w, http.StatusBadRequest, "group id is not a valid UUID", &t)
		return
	}
	deleted, err := h.Groups.Delete(r.Context(), id)
	if err != nil {
		slog.Error("scim: delete group failed", slog.String("error", err.Error()))
		writeScimError(w, http.StatusInternalServerError, "failed to delete group", nil)
		return
	}
	if !deleted {
		writeScimError(w, http.StatusNotFound, "SCIM group not found", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── helpers ────────────────────────────────────────────────────────

// writeGroupView reloads `id` + its members and serialises them
// as a SCIM Group response with `status`. Used by every endpoint
// that responds with the canonical wire shape.
func (h *Handlers) writeGroupView(ctx context.Context, w http.ResponseWriter, status int, id uuid.UUID) {
	record, members, err := loadGroupView(ctx, h.Groups, id)
	if err != nil {
		slog.Error("scim: reload group view failed", slog.String("error", err.Error()))
		writeScimError(w, http.StatusInternalServerError, "failed to load group", nil)
		return
	}
	if record == nil {
		writeScimError(w, http.StatusNotFound, "SCIM group not found", nil)
		return
	}
	writeJSON(w, status, GroupToScim(*record, members, h.BaseURL))
}

// trimToOption mirrors the small "trim+filter-empty" pattern used
// by both the create and patch paths.
func trimToOption(p *string) *string {
	if p == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*p)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
