// Package handlers exposes the chi-compatible HTTP handler surface
// of the OpenFoundry AI plane (libs/ai-kernel in Rust). Consuming
// services (llm-catalog, ai-evaluation, retrieval-context,
// agent-runtime, model-catalog) instantiate Handlers with their pgx
// pool and bind the methods onto their own router.
//
// Mirrors the Rust libs/ai-kernel/src/handlers/{mod,tools,prompts,
// agents,knowledge,chat}.rs file layout — one file per surface.
package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// ErrorResponse is the canonical {"error": "..."} envelope. Matches
// Rust handlers::mod::ErrorResponse.
type ErrorResponse struct {
	Error string `json:"error"`
}

// writeJSON encodes body + sends with the right content type.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError emits {"error": msg} with the given HTTP status. Mirrors
// Rust super::{bad_request, not_found, internal_error, db_error}.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}

// dbError logs + emits a generic 500. Used when sqlx-equivalent pgx
// calls fail. Matches Rust db_error.
func dbError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "database operation failed")
}
