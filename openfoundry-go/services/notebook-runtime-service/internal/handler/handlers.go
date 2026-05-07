// Package handler hosts the HTTP handlers for notebook-runtime-service.
//
// Status:
//
//   - Notebook + Cell + Session CRUD: 1:1 ported against pgx (matches
//     Rust sqlx). When the DB pool is nil (smoke clusters / unit
//     tests), endpoints return the empty-envelope shape so dashboards
//     keep round-tripping.
//   - Workspace file CRUD: filesystem-backed via `domain/environment`.
//   - Notepad export: HTML rendering via `domain/notepad`.
//   - Cell execute (`ExecuteCell` / `ExecuteAllCells`): still substrate-
//     only because the kernel runtime (Python/SQL/R/LLM) is PyO3-bound
//     in Rust and requires a separate Go-side runtime that is not part
//     of this port. Returns HTTP 501.
//
// Notepad documents + presence still return the empty envelope; their
// repository slice lands when the notepad UI ships its own backend.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/notebook-runtime-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/notebook-runtime-service/internal/domain/environment"
	"github.com/openfoundry/openfoundry-go/services/notebook-runtime-service/internal/domain/notepad"
	"github.com/openfoundry/openfoundry-go/services/notebook-runtime-service/internal/models"
)

// State carries the deps every handler needs.
type State struct {
	Cfg  *config.Config
	Pool *pgxpool.Pool
}

// ── Workspace files (1:1 ported domain/environment) ──────────────────

func (s *State) ListWorkspaceFiles(w http.ResponseWriter, r *http.Request) {
	nb, err := pathUUID(r, "notebook_id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid notebook id"))
		return
	}
	files, err := environment.ListWorkspaceFiles(s.Cfg.DataDir, nb)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": files})
}

func (s *State) UpsertWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	nb, err := pathUUID(r, "notebook_id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid notebook id"))
		return
	}
	var body models.UpsertNotebookWorkspaceFileRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid body"))
		return
	}
	file, err := environment.UpsertWorkspaceFile(s.Cfg.DataDir, nb, body.Path, body.Content)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, file)
}

func (s *State) DeleteWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	nb, err := pathUUID(r, "notebook_id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid notebook id"))
		return
	}
	path := r.URL.Query().Get("path")
	ok, err := environment.DeleteWorkspaceFile(s.Cfg.DataDir, nb, path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
		return
	}
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Execute (kernel dispatch) ─────────────────────────────────────────

func (s *State) ExecuteCell(w http.ResponseWriter, r *http.Request) {
	notImplemented(w, r) // kernel runtime not ported
}

func (s *State) ExecuteAllCells(w http.ResponseWriter, r *http.Request) {
	notImplemented(w, r)
}

// ── Notepad (export wired 1:1; CRUD stubbed) ──────────────────────────

func (s *State) ListDocuments(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": []any{}, "total": 0, "page": 1, "per_page": 20})
}

func (s *State) CreateDocument(w http.ResponseWriter, r *http.Request) {
	notImplemented(w, r)
}

func (s *State) GetDocument(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, nil)
}

func (s *State) UpdateDocument(w http.ResponseWriter, r *http.Request) {
	notImplemented(w, r)
}

func (s *State) DeleteDocument(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *State) ListPresence(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
}

func (s *State) UpsertPresence(w http.ResponseWriter, r *http.Request) {
	notImplemented(w, r)
}

// ExportDocument is fully wired — it consumes the `notepad` package
// 1:1 ported from Rust. When the repository layer is ready the input
// will come from Postgres; for now the handler accepts the document
// JSON as the request body so the rendering code path is reachable
// for end-to-end browser tests.
func (s *State) ExportDocument(w http.ResponseWriter, r *http.Request) {
	var doc models.NotepadDocument
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		// Fall back to the not-found shape when the caller did not
		// pass an inline document — the rendering route is still the
		// only piece that doesn't need a DB.
		writeJSON(w, http.StatusNotFound, nil)
		return
	}
	writeJSON(w, http.StatusOK, notepad.RenderExportPayload(&doc))
}

// ── Auth helper ──────────────────────────────────────────────────────

// requireClaims pulls the JWT claims attached by authmw. Returns nil
// + writes 401 when the upstream middleware has not been wired (or
// the JWT was absent / invalid).
func requireClaims(w http.ResponseWriter, r *http.Request) *authmw.Claims {
	c, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, errBody("missing claims"))
		return nil
	}
	return c
}

// ── Shared utilities ─────────────────────────────────────────────────

func pathUUID(r *http.Request, key string) (uuid.UUID, error) {
	raw := chi.URLParam(r, key)
	if raw == "" {
		return uuid.Nil, errInvalid("missing path parameter " + key)
	}
	return uuid.Parse(strings.TrimSpace(raw))
}

func errBody(msg string) map[string]string { return map[string]string{"error": msg} }

func errInvalid(msg string) error { return errors.New(msg) }

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"error":  "not_implemented",
		"detail": "kernel runtime (Python/SQL/R/LLM) not yet ported (see service README)",
	})
}
