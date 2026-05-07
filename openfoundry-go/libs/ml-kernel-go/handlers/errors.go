// Package handlers exposes the chi-compatible HTTP handler surface
// of the OpenFoundry ML plane (libs/ml-kernel in Rust). The
// model-catalog / model-deployment / experiment-tracking / feature-
// store services bind these onto their own routers.
//
// Mirrors libs/ml-kernel/src/handlers/{mod,overview,predictions,
// training,features,deployments,models,experiments}.rs.
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

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}

func dbError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "database operation failed")
}
