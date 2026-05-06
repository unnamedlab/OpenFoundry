// Package handlers wires the HTTP endpoints for ontology-query-service.
//
// All read endpoints are skeleton stubs that return 501 Not Implemented
// until libs/cassandra-kernel-go + libs/storage-abstraction-go ports
// land. The Rust read service is Cassandra-backed; per S1.5.e there is
// no SQL surface to migrate.
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

type Handlers struct {
	// ObjectStore: future field — wire in when libs/cassandra-kernel-go
	// is connected. Today this remains nil and handlers respond 501.
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// notImplemented advertises the missing Cassandra wiring without
// pretending to serve a result. 501 lets the client distinguish
// "endpoint exists but isn't backed yet" from a 404.
func notImplemented(w http.ResponseWriter) {
	writeJSONErr(w, http.StatusNotImplemented,
		"ontology-query-service: Cassandra read backend not wired in this build; "+
			"libs/cassandra-kernel-go + libs/storage-abstraction-go ports pending")
}

func (h *Handlers) GetObject(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tenant := chi.URLParam(r, "tenant")
	objectID := chi.URLParam(r, "object_id")
	if tenant == "" || objectID == "" {
		writeJSONErr(w, http.StatusBadRequest, "tenant and object_id required")
		return
	}
	notImplemented(w)
}

func (h *Handlers) ListObjectsByType(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tenant := chi.URLParam(r, "tenant")
	typeID := chi.URLParam(r, "type_id")
	if tenant == "" || typeID == "" {
		writeJSONErr(w, http.StatusBadRequest, "tenant and type_id required")
		return
	}
	notImplemented(w)
}
