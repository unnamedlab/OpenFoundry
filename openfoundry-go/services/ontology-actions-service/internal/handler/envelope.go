// Package handler hosts the HTTP handlers for ontology-actions-service.
//
// The handlers below are *substrate-only*: they mount the same URL
// surface as `services/ontology-actions-service/src/lib.rs::build_router`,
// matching status codes and response envelopes (`{"data": [...], "total": N}`)
// so the smoke tests that hit the router (path-only assertions on
// 401 vs 200 with a `data` array) continue to pass byte-for-byte.
//
// Real handler bodies belong in `libs/ontology-kernel-go/handlers/`
// (the Rust equivalent lives in `libs/ontology-kernel/handlers/`).
// That kernel slice is being ported separately; until it lands these
// handlers respond with empty envelopes and HTTP 501 on writes.
package handler

import (
	"encoding/json"
	"net/http"
)

// listEnvelope is the shape every list endpoint returns
// (`{ data: [...], total: N }`). Matches the kernel handlers exactly.
type listEnvelope struct {
	Data  []any `json:"data"`
	Total int64 `json:"total"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeEmptyList(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, listEnvelope{Data: []any{}, Total: 0})
}

// notImplemented is the shared response for write paths that depend
// on libs/ontology-kernel-go/handlers (still being ported).
func notImplemented(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"error":  "not_implemented",
		"detail": "kernel handler not yet ported (see libs/ontology-kernel-go)",
	})
}
