package workspace

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

// Handlers wires the B3 Workspace HTTP surface (favorites + recents).
//
// Sharing lands in a follow-up slice; spaces / projects / trash /
// resource_resolve / resource_ops are unmounted in the Rust upstream
// (see INVENTORY-tenancy-organizations-service.md "Active vs retired").
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
	fav, err := h.Repo.CreateFavorite(r.Context(), c.Sub, kind, body.ResourceID)
	if err != nil {
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
	writeJSON(w, http.StatusOK, ListFavoritesResponse{Data: favs})
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
	out, err := h.Repo.ListRecentsByUser(r.Context(), c.Sub, kind, limit)
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
