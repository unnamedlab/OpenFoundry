package workspace

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/domain"
)

// Handlers wires the B3 Workspace HTTP surface (favorites + recents).
//
// Sharing lands in a follow-up slice; spaces / projects / trash /
// resource_resolve / resource_ops are unmounted in the Rust upstream
// (see docs/archive/INVENTORY-tenancy-organizations-service.md "Active vs retired").
type Handlers struct{ Repo *Repo }

// ─── Favorites ──────────────────────────────────────────────────────

// CreateFavorite handles POST /api/v1/workspace/favorites.
func (h *Handlers) CreateFavorite(w http.ResponseWriter, r *http.Request) {
	c, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body CreateFavoriteRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	kind, err := ParseResourceKind(body.ResourceKind)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.ResourceID == uuid.Nil {
		writeJSONErr(w, http.StatusBadRequest, "resource_id required")
		return
	}
	fav, err := h.Repo.CreateFavorite(r.Context(), c.Sub, kind, body.ResourceID, body.GroupID, body.DisplayOrder)
	if err != nil {
		if errors.Is(err, ErrFavoriteGroupNotFound) {
			writeJSONErr(w, http.StatusNotFound, err.Error())
			return
		}
		slog.Error("create favorite", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to create favorite")
		return
	}
	writeJSON(w, http.StatusCreated, fav)
}

// ListFavorites handles GET /api/v1/workspace/favorites?kind=…&limit=N.
func (h *Handlers) ListFavorites(w http.ResponseWriter, r *http.Request) {
	c, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit := parseLimit(r, 200, 1, 1000)
	var kind ResourceKind
	if raw := r.URL.Query().Get("kind"); raw != "" {
		k, err := ParseResourceKind(raw)
		if err != nil {
			writeJSONErr(w, http.StatusBadRequest, err.Error())
			return
		}
		kind = k
	}
	favs, err := h.Repo.ListFavoritesByUser(r.Context(), c.Sub, kind, limit)
	if err != nil {
		slog.Error("list favorites", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list favorites")
		return
	}
	groups, err := h.Repo.ListFavoriteGroupsByUser(r.Context(), c.Sub)
	if err != nil {
		slog.Error("list favorite groups", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list favorite groups")
		return
	}
	writeJSON(w, http.StatusOK, ListFavoritesResponse{Data: favs, Groups: groups})
}

// CreateFavoriteGroup handles POST /api/v1/workspace/favorites/groups.
func (h *Handlers) CreateFavoriteGroup(w http.ResponseWriter, r *http.Request) {
	c, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body CreateFavoriteGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		writeJSONErr(w, http.StatusBadRequest, "name required")
		return
	}
	if len(body.Name) > 120 {
		writeJSONErr(w, http.StatusBadRequest, "name must be at most 120 characters")
		return
	}
	group, err := h.Repo.CreateFavoriteGroup(r.Context(), c.Sub, body.Name, body.DisplayOrder)
	if err != nil {
		slog.Error("create favorite group", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to create favorite group")
		return
	}
	writeJSON(w, http.StatusCreated, group)
}

// ListFavoriteGroups handles GET /api/v1/workspace/favorites/groups.
func (h *Handlers) ListFavoriteGroups(w http.ResponseWriter, r *http.Request) {
	c, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	groups, err := h.Repo.ListFavoriteGroupsByUser(r.Context(), c.Sub)
	if err != nil {
		slog.Error("list favorite groups", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list favorite groups")
		return
	}
	writeJSON(w, http.StatusOK, ListFavoriteGroupsResponse{Data: groups})
}

// UpdateFavoriteOrder handles PUT /api/v1/workspace/favorites/order.
func (h *Handlers) UpdateFavoriteOrder(w http.ResponseWriter, r *http.Request) {
	c, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body UpdateFavoriteOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if len(body.Items) > 1000 {
		writeJSONErr(w, http.StatusBadRequest, "at most 1000 favorites can be reordered at once")
		return
	}
	for _, item := range body.Items {
		if item.ResourceID == uuid.Nil {
			writeJSONErr(w, http.StatusBadRequest, "resource_id required")
			return
		}
		if _, err := ParseResourceKind(item.ResourceKind); err != nil {
			writeJSONErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := h.Repo.UpdateFavoriteOrder(r.Context(), c.Sub, body.Items); err != nil {
		if errors.Is(err, ErrFavoriteGroupNotFound) {
			writeJSONErr(w, http.StatusNotFound, err.Error())
			return
		}
		slog.Error("update favorite order", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to update favorite order")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// UpdateFavoriteGroupsOrder handles PUT /api/v1/workspace/favorites/groups/order.
func (h *Handlers) UpdateFavoriteGroupsOrder(w http.ResponseWriter, r *http.Request) {
	c, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body UpdateFavoriteGroupsOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if len(body.Groups) > 200 {
		writeJSONErr(w, http.StatusBadRequest, "at most 200 favorite groups can be reordered at once")
		return
	}
	for _, group := range body.Groups {
		if group.ID == uuid.Nil {
			writeJSONErr(w, http.StatusBadRequest, "group id required")
			return
		}
	}
	if err := h.Repo.UpdateFavoriteGroupsOrder(r.Context(), c.Sub, body.Groups); err != nil {
		slog.Error("update favorite groups order", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to update favorite groups order")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteFavorite handles DELETE /api/v1/workspace/favorites/{kind}/{id}.
func (h *Handlers) DeleteFavorite(w http.ResponseWriter, r *http.Request) {
	c, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	kind, err := ParseResourceKind(chi.URLParam(r, "kind"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	resourceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid resource id")
		return
	}
	deleted, err := h.Repo.DeleteFavorite(r.Context(), c.Sub, kind, resourceID)
	if err != nil {
		slog.Error("delete favorite", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to delete favorite")
		return
	}
	if !deleted {
		writeJSONErr(w, http.StatusNotFound, "favorite not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Recents ────────────────────────────────────────────────────────

// RecordAccess handles POST /api/v1/workspace/recents.
//
// Best-effort: always returns 202 Accepted, even when the insert fails,
// because tracking must not block the calling page (matches Rust impl).
func (h *Handlers) RecordAccess(w http.ResponseWriter, r *http.Request) {
	c, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body RecordAccessRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	kind, err := ParseResourceKind(body.ResourceKind)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.ResourceID == uuid.Nil {
		writeJSONErr(w, http.StatusBadRequest, "resource_id required")
		return
	}
	if err := h.Repo.RecordAccess(r.Context(), c.Sub, kind, body.ResourceID); err != nil {
		slog.Warn("record access (best effort)", slog.String("error", err.Error()))
	}
	w.WriteHeader(http.StatusAccepted)
}

// ListRecents handles GET /api/v1/workspace/recents?kind=…&limit=N.
func (h *Handlers) ListRecents(w http.ResponseWriter, r *http.Request) {
	c, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit := parseLimit(r, 50, 1, 500)
	var kind ResourceKind
	if raw := r.URL.Query().Get("kind"); raw != "" {
		k, err := ParseResourceKind(raw)
		if err != nil {
			writeJSONErr(w, http.StatusBadRequest, err.Error())
			return
		}
		kind = k
	}
	accessible, err := domain.ListAccessibleProjects(r.Context(), h.Repo.Pool, c)
	if err != nil {
		slog.Error("list recents access", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to evaluate recents access")
		return
	}
	projectIDs := make([]uuid.UUID, 0, len(accessible))
	for id := range accessible {
		projectIDs = append(projectIDs, id)
	}
	out, err := h.Repo.ListRecentsByUser(r.Context(), c.Sub, kind, limit, projectIDs)
	if err != nil {
		slog.Error("list recents", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list recents")
		return
	}
	writeJSON(w, http.StatusOK, ListRecentsResponse{Data: out})
}

// ─── helpers ────────────────────────────────────────────────────────

func parseLimit(r *http.Request, fallback, min, max int) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
