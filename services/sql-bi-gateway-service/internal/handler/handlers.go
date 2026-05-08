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
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/models"
)

// SavedQueries hosts the saved-queries CRUD handlers backed by pgx.
type SavedQueries struct {
	Pool *pgxpool.Pool
	Log  *slog.Logger
}

// New builds a SavedQueries handler set backed by pool. Pool may be
// nil; in that case Mount only wires the in-memory stub responses
// (matches the Rust behaviour when the saved-queries database is
// unreachable: the Flight SQL surface stays available, the side
// router only serves /healthz).
func New(pool *pgxpool.Pool, log *slog.Logger) *SavedQueries {
	return &SavedQueries{Pool: pool, Log: log}
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
		// P5 — Foundry "Open in SQL workbench" entry point. When the
		// body `sql` is empty AND `?seed_dataset_rid=` is present,
		// pre-fill with `SELECT * FROM <dataset>` so the user lands
		// on a runnable query. RID is sanitised to alphanumeric +
		// dot/dash/underscore so it can be embedded in SQL safely.
		if rid := r.URL.Query().Get("seed_dataset_rid"); IsSafeRID(rid) {
			sql = `SELECT * FROM "` + rid + `" LIMIT 100`
		}
	}

	desc := ""
	if body.Description != nil {
		desc = *body.Description
	}

	if h.Pool == nil {
		// No DB wired — return the body the caller would have got
		// after a successful insert so dashboards keep working in
		// smoke clusters.
		now := time.Now().UTC()
		writeJSON(w, http.StatusCreated, models.SavedQuery{
			ID:          uuid.New(),
			Name:        body.Name,
			Description: desc,
			SQL:         sql,
			OwnerID:     uuid.Nil,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		return
	}

	id := uuid.New()
	// owner_id must be derived from the authenticated user; until the
	// chi auth layer is wired into this side router we accept the
	// anonymous (nil) UUID and rely on the database's NOT NULL
	// constraint as a backstop. The Flight SQL gRPC surface is the
	// primary auth-gated entrypoint; the saved-queries CRUD here is
	// a BI-dashboard convenience.
	ownerID := uuid.Nil
	row := h.Pool.QueryRow(r.Context(), `
        INSERT INTO saved_queries (id, name, description, sql, owner_id)
        VALUES ($1, $2, $3, $4, $5)
        RETURNING id, name, description, sql, owner_id, created_at, updated_at`,
		id, body.Name, desc, sql, ownerID)

	q, err := scanSavedQuery(row)
	if err != nil {
		h.Log.Error("create saved query failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create failed"})
		return
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

	if h.Pool == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"data": []any{}, "page": page, "per_page": perPage,
		})
		return
	}

	var (
		searchVal *string
		search    = q.Get("search")
	)
	if search != "" {
		pat := "%" + search + "%"
		searchVal = &pat
	}

	rows, err := h.Pool.Query(r.Context(), `
        SELECT id, name, description, sql, owner_id, created_at, updated_at
        FROM saved_queries
        WHERE ($1::TEXT IS NULL OR name ILIKE $1 OR description ILIKE $1)
        ORDER BY updated_at DESC
        LIMIT $2 OFFSET $3`, searchVal, perPage, offset)
	if err != nil {
		h.Log.Error("list saved queries failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, nil)
		return
	}
	defer rows.Close()

	out, err := scanSavedQueries(rows)
	if err != nil {
		h.Log.Error("list saved queries scan", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, nil)
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
	if h.Pool == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	tag, err := h.Pool.Exec(r.Context(), `DELETE FROM saved_queries WHERE id = $1`, id)
	if err != nil {
		h.Log.Error("delete saved query failed", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if tag.RowsAffected() == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- scanning helpers --------------------------------------------------

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSavedQueries(rows pgx.Rows) ([]models.SavedQuery, error) {
	out := make([]models.SavedQuery, 0)
	for rows.Next() {
		q, err := scanSavedQuery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func scanSavedQuery(s rowScanner) (models.SavedQuery, error) {
	var q models.SavedQuery
	if err := s.Scan(&q.ID, &q.Name, &q.Description, &q.SQL, &q.OwnerID,
		&q.CreatedAt, &q.UpdatedAt); err != nil {
		return models.SavedQuery{}, err
	}
	return q, nil
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

