// Package handler — kernel session CRUD. 1:1 port of
// `services/notebook-runtime-service/src/handlers/sessions.rs`.
//
// **Scope deferral**: the Rust `create_session` calls
// `state.kernel_manager.ensure_session(id, &kernel)` to spin up the
// Python kernel before persisting the row, and `stop_session` calls
// `kernel_manager.drop_session(id)` after the UPDATE. Both operations
// require the inline kernel runtime which is not yet wired in Go (the
// Python sidecar is for inline functions, not for long-running
// notebook kernels). This port persists the row at status `idle` and
// transitions it to `dead` on stop without touching a kernel manager
// — execute paths return HTTP 501 in the meantime so the lifecycle
// stays consistent.
package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/notebook-runtime-service/internal/models"
)

// CreateSession mirrors `pub async fn create_session`. Persists a
// row at status `idle` so the session list endpoint can spot it; the
// kernel-manager spawn lands when the kernel runtime ports.
func (s *State) CreateSession(w http.ResponseWriter, r *http.Request) {
	claims := requireClaims(w, r)
	if claims == nil {
		return
	}
	notebookID, err := pathUUID(r, "notebook_id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid notebook id"))
		return
	}
	var body models.CreateSessionRequest
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid body"))
		return
	}
	kernel := "python"
	if body.Kernel != nil && *body.Kernel != "" {
		kernel = *body.Kernel
	}
	id, _ := uuid.NewV7()
	if s.Pool == nil {
		now := time.Now().UTC()
		writeJSON(w, http.StatusCreated, models.Session{
			ID: id, NotebookID: notebookID, Kernel: kernel,
			Status: "idle", StartedBy: claims.Sub,
			CreatedAt: now, LastActivity: now,
		})
		return
	}
	row := s.Pool.QueryRow(r.Context(), `
        INSERT INTO sessions (id, notebook_id, kernel, status, started_by)
        VALUES ($1, $2, $3, 'idle', $4)
        RETURNING id, notebook_id, kernel, status, started_by, created_at, last_activity`,
		id, notebookID, kernel, claims.Sub)
	sess, err := scanSession(row)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

// ListSessions mirrors `pub async fn list_sessions`.
func (s *State) ListSessions(w http.ResponseWriter, r *http.Request) {
	notebookID, err := pathUUID(r, "notebook_id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid notebook id"))
		return
	}
	if s.Pool == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
		return
	}
	rows, err := s.Pool.Query(r.Context(), `
        SELECT id, notebook_id, kernel, status, started_by, created_at, last_activity
        FROM sessions WHERE notebook_id = $1
        ORDER BY created_at DESC`, notebookID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	defer rows.Close()
	sessions := []models.Session{}
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
			return
		}
		sessions = append(sessions, sess)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": sessions})
}

// StopSession mirrors `pub async fn stop_session`.
func (s *State) StopSession(w http.ResponseWriter, r *http.Request) {
	sessionID, err := pathUUID(r, "session_id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid session id"))
		return
	}
	if s.Pool == nil {
		writeJSON(w, http.StatusNotFound, nil)
		return
	}
	row := s.Pool.QueryRow(r.Context(), `
        UPDATE sessions SET status = 'dead', last_activity = NOW()
        WHERE id = $1
        RETURNING id, notebook_id, kernel, status, started_by, created_at, last_activity`,
		sessionID)
	sess, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, nil)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func scanSession(s rowScanner) (models.Session, error) {
	var sess models.Session
	err := s.Scan(&sess.ID, &sess.NotebookID, &sess.Kernel, &sess.Status,
		&sess.StartedBy, &sess.CreatedAt, &sess.LastActivity)
	return sess, err
}
