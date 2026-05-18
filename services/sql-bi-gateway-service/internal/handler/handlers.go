// Package handler hosts the HTTP handlers for the saved-queries CRUD
// surface — port of services/sql-bi-gateway-service/src/http.rs.
//
// Warehousing and tabular-analysis routes live in their own packages
// (`internal/warehousing` and `internal/tabular`) so the bounded
// contexts are independently testable, mirroring the Rust module
// layout (src/warehousing/, src/tabular/).
package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/repo"
)

// SavedQueries hosts the saved-queries CRUD handlers.
//
// When Repo is nil the handlers return seed-only stub responses so the
// Flight SQL surface stays available during BI-database outages — same
// behaviour as the Rust `build_router(None)`.
type SavedQueries struct {
	Repo repo.SavedQueries
	Log  *slog.Logger
}

// New builds a SavedQueries handler set backed by repo. repo may be
// nil; in that case Mount only wires the in-memory stub responses
// (matches the Rust behaviour when the saved-queries database is
// unreachable: the Flight SQL surface stays available, the side
// router only serves /healthz).
func New(r repo.SavedQueries, log *slog.Logger) *SavedQueries {
	return &SavedQueries{Repo: r, Log: log}
}

// IsSafeRID returns true for RIDs that are safe to embed verbatim in
// SQL (alphanumeric + dot/underscore/dash). Mirrors the Rust
// `is_safe_rid` function 1:1.
func IsSafeRID(rid string) bool {
	if rid == "" {
		return false
	}
	for _, c := range rid {
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '.' || c == '_' || c == '-':
			// allowed
		default:
			return false
		}
	}
	return true
}

// Healthz is the Kubernetes liveness payload.
// GET /healthz
func Healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// CreateSavedQuery persists a new saved query.
// POST /api/v1/queries/saved
func (h *SavedQueries) CreateSavedQuery(w http.ResponseWriter, r *http.Request) {
	var body models.CreateSavedQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	bodySQL := ""
	if body.SQL != nil {
		bodySQL = strings.TrimSpace(*body.SQL)
	}
	sql := bodySQL
	if sql == "" {
		// Foundry "Open in SQL workbench" entry point: when the body's
		// `sql` is empty AND `?seed_dataset_rid=` is present, pre-fill
		// with `SELECT * FROM <dataset> LIMIT 100` so the user lands on
		// a runnable query. RID is sanitised so it can be embedded
		// without an injection vector.
		if rid := r.URL.Query().Get("seed_dataset_rid"); IsSafeRID(rid) {
			sql = `SELECT * FROM "` + rid + `" LIMIT 100`
		}
	}

	desc := ""
	if body.Description != nil {
		desc = *body.Description
	}

	ownerID := claimsOwnerID(r)

	if h.Repo == nil {
		// No DB wired — return the body the caller would have got
		// after a successful insert so dashboards keep working in
		// smoke clusters.
		now := time.Now().UTC()
		writeJSON(w, http.StatusCreated, models.SavedQuery{
			ID:          uuid.New(),
			Name:        body.Name,
			Description: desc,
			SQL:         sql,
			OwnerID:     ownerID,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		return
	}

	q, err := h.Repo.Create(r.Context(), models.SavedQuery{
		Name:        body.Name,
		Description: desc,
		SQL:         sql,
		OwnerID:     ownerID,
	})
	if err != nil {
		switch {
		case errors.Is(err, repo.ErrValidation):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		default:
			h.Log.Error("create saved query failed", slog.String("error", err.Error()))
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create failed"})
			return
		}
	}
	writeJSON(w, http.StatusCreated, q)
}

// ListSavedQueries returns a paginated, search-filtered list of saved
// queries. GET /api/v1/queries/saved?page=&per_page=&search=
func (h *SavedQueries) ListSavedQueries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := parseInt64(q.Get("page"), 1)
	if page < 1 {
		page = 1
	}
	perPage := parseInt64(q.Get("per_page"), 20)
	switch {
	case perPage < 1:
		perPage = 1
	case perPage > 100:
		perPage = 100
	}
	offset := (page - 1) * perPage

	if h.Repo == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"data": []any{}, "page": page, "per_page": perPage,
		})
		return
	}

	out, err := h.Repo.List(r.Context(), q.Get("search"), perPage, offset)
	if err != nil {
		h.Log.Error("list saved queries failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": out, "page": page, "per_page": perPage,
	})
}

// DeleteSavedQuery removes a saved query by id.
// DELETE /api/v1/queries/saved/:id
func (h *SavedQueries) DeleteSavedQuery(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if h.Repo == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.Repo.Delete(r.Context(), id); err != nil {
		switch {
		case errors.Is(err, repo.ErrNotFound):
			w.WriteHeader(http.StatusNotFound)
			return
		default:
			h.Log.Error("delete saved query failed", slog.String("error", err.Error()))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// claimsOwnerID returns the subject of the bound JWT claims, or
// uuid.Nil when the request was not authenticated. The HTTP side
// router mounts authmw.Middleware on /api/v1, so authenticated requests
// always reach the handler with claims attached; the nil-fallback only
// fires in tests and in the `allow_anonymous` dev path.
func claimsOwnerID(r *http.Request) uuid.UUID {
	if claims, ok := authmw.FromContext(r.Context()); ok && claims != nil {
		return claims.Sub
	}
	return uuid.Nil
}

func parseInt64(s string, fallback int64) int64 {
	if s == "" {
		return fallback
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fallback
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}
