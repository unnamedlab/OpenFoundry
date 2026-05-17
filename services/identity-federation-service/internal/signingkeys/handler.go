package signingkeys

import (
	"encoding/json"
	"net/http"
)

// Handler exposes the JWKS publication + admin rotation endpoints.
type Handler struct {
	Manager *Manager
}

// NewHandler wires a Handler around a Manager.
func NewHandler(m *Manager) *Handler { return &Handler{Manager: m} }

// Jwks serves GET /.well-known/jwks.json. The response includes the
// active key first and every retiring key whose grace window is
// still open. Public — no bearer required.
func (h *Handler) Jwks(w http.ResponseWriter, r *http.Request) {
	jwks, err := h.Manager.Jwks(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_ = json.NewEncoder(w).Encode(jwks)
}

// Rotate serves POST /api/v1/admin/jwks/rotate — forces an
// immediate rotation regardless of the active key's not_after.
// Bearer + admin enforcement happen at the route mount.
func (h *Handler) Rotate(w http.ResponseWriter, r *http.Request) {
	outcome, err := h.Manager.Rotate(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(outcome)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
