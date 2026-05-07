// What-if branch endpoints — list / delete are full 1:1; create
// constructs the branch envelope without the simulation cascade
// (Phase 5B will plug in `plan_action` + `simulate_target_after_preview`
// when the execute substrate lands).
package actions

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/domain"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// CreateActionWhatIfBranch mirrors `pub async fn create_action_what_if_branch`.
//
// Phase 5A scope: persists the branch with the request's `parameters`
// echoed in the preview slot. The Rust impl runs `plan_action`
// (Phase 5B substrate) so the preview carries the evaluated effect
// preview + before/after object snapshots. Until 5B lands the branch
// stays useful as a parameter checkpoint — clients can still list,
// delete and re-trigger from it.
func CreateActionWhatIfBranch(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, errBody("missing claims"))
			return
		}
		actionID, err := pathUUID(r, "id")
		if err != nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		var body models.CreateActionWhatIfBranchRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			invalid(w, "invalid request body")
			return
		}
		row, err := domain.GetActionRow(r.Context(), state.Stores.Definitions, actionID)
		if err != nil {
			dbError(w, "failed to load action type: "+err.Error())
			return
		}
		if row == nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		action := row.IntoAction()
		if err := ensureActionActorPermission(claims, action); err != nil {
			forbidden(w, err.Error())
			return
		}

		now := nowUTC()
		name := action.DisplayName + " what-if " + now.Format("2006-01-02 15:04:05")
		if body.Name != nil {
			name = *body.Name
		}
		description := ""
		if body.Description != nil {
			description = *body.Description
		}
		// Phase 5A preview: echoes `{ "kind": "what_if_branch",
		// "parameters": <body.parameters> }` so clients can read the
		// stored parameter snapshot. Phase 5B replaces this with the
		// full plan_preview + before/after object pair.
		preview, _ := json.Marshal(map[string]any{
			"kind":              "what_if_branch",
			"parameters":        json.RawMessage(orJSONNull(body.Parameters)),
			"target_object_id":  body.TargetObjectID,
			"phase_5a_preview":  true,
		})

		branchID, _ := uuid.NewV7()
		branch := models.ActionWhatIfBranch{
			ID:             branchID,
			ActionID:       action.ID,
			TargetObjectID: body.TargetObjectID,
			Name:           name,
			Description:    description,
			Parameters:     orJSONNull(body.Parameters),
			Preview:        preview,
			BeforeObject:   nil,
			AfterObject:    nil,
			Deleted:        false,
			OwnerID:        claims.Sub,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		stored, err := domain.CreateWhatIfBranch(r.Context(), state.Stores.ReadModels, domain.TenantFromClaims(claims), branch)
		if err != nil {
			dbError(w, "failed to create action what-if branch: "+err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, stored)
	}
}

// ListActionWhatIfBranches mirrors `pub async fn list_action_what_if_branches`.
func ListActionWhatIfBranches(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, errBody("missing claims"))
			return
		}
		actionID, err := pathUUID(r, "id")
		if err != nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		query := parseListWhatIfQuery(r)
		page := defaultPage(query.Page)
		perPage := defaultPerPage(query.PerPage)
		offset := (page - 1) * perPage
		showAll := claims.HasRole("admin")
		tenant := domain.TenantFromClaims(claims)

		total, err := domain.CountWhatIfBranches(r.Context(), state.Stores.ReadModels, domain.WhatIfListQuery{
			Tenant:         tenant,
			ActionID:       actionID,
			TargetObjectID: query.TargetObjectID,
			OwnerID:        claims.Sub,
			ShowAll:        showAll,
			Page:           storage.Page{Size: 10_000},
		})
		if err != nil {
			total = 0
		}

		offsetTok := strconv.FormatInt(offset, 10)
		listed, err := domain.ListWhatIfBranches(r.Context(), state.Stores.ReadModels, domain.WhatIfListQuery{
			Tenant:         tenant,
			ActionID:       actionID,
			TargetObjectID: query.TargetObjectID,
			OwnerID:        claims.Sub,
			ShowAll:        showAll,
			Page:           storage.Page{Size: uint32(perPage), Token: &offsetTok},
		})
		var data []models.ActionWhatIfBranch
		if err == nil {
			data = listed.Items
		}

		writeJSON(w, http.StatusOK, models.ListActionWhatIfBranchesResponse{
			Data: data, Total: int64(total), Page: page, PerPage: perPage,
		})
	}
}

// DeleteActionWhatIfBranch mirrors `pub async fn delete_action_what_if_branch`.
func DeleteActionWhatIfBranch(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, errBody("missing claims"))
			return
		}
		actionID, err := pathUUID(r, "id")
		if err != nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		branchID, err := pathUUID(r, "branch_id")
		if err != nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		ok2, err := domain.DeleteWhatIfBranch(r.Context(), state.Stores.ReadModels,
			domain.TenantFromClaims(claims), actionID, branchID, claims.Sub, claims.HasRole("admin"))
		if err != nil {
			dbError(w, "failed to delete what-if branch: "+err.Error())
			return
		}
		if !ok2 {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── Helpers ─────────────────────────────────────────────────────────

// ensureActionActorPermission mirrors `pub(crate) fn ensure_action_actor_permission`.
func ensureActionActorPermission(claims *authmw.Claims, action models.ActionType) error {
	if action.PermissionKey != nil {
		if !claims.HasPermissionKey(*action.PermissionKey) {
			return forbiddenErr("forbidden: missing permission '" + *action.PermissionKey + "'")
		}
	}
	for _, key := range action.AuthorizationPolicy.RequiredPermissionKeys {
		if !claims.HasPermissionKey(key) {
			return forbiddenErr("forbidden: missing permission '" + key + "'")
		}
	}
	return nil
}

func parseListWhatIfQuery(r *http.Request) models.ListActionWhatIfBranchesQuery {
	q := r.URL.Query()
	out := models.ListActionWhatIfBranchesQuery{}
	if raw := q.Get("target_object_id"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			out.TargetObjectID = &id
		}
	}
	if raw := q.Get("page"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			out.Page = &v
		}
	}
	if raw := q.Get("per_page"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			out.PerPage = &v
		}
	}
	return out
}

func orJSONNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`null`)
	}
	return raw
}

func nowUTC() time.Time { return time.Now().UTC() }
