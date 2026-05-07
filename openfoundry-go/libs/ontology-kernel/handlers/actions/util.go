// Shared HTTP plumbing for the actions handler package.
package actions

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func errBody(msg string) map[string]string { return map[string]string{"error": msg} }

func invalid(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, errBody(msg))
}

func dbError(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusInternalServerError, errBody(msg))
}

func forbidden(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusForbidden, errBody(msg))
}

func forbiddenErr(msg string) error { return errors.New(msg) }

func pathUUID(r *http.Request, key string) (uuid.UUID, error) {
	raw := chi.URLParam(r, key)
	if raw == "" {
		return uuid.Nil, errors.New("missing path parameter " + key)
	}
	return uuid.Parse(strings.TrimSpace(raw))
}

func parseListActionTypesQuery(r *http.Request) models.ListActionTypesQuery {
	q := r.URL.Query()
	out := models.ListActionTypesQuery{}
	if raw := q.Get("object_type_id"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			out.ObjectTypeID = &id
		}
	}
	if raw := q.Get("page"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			out.Page = &v
		}
	}
	if raw := q.Get("per_page"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			out.PerPage = &v
		}
	}
	if raw := q.Get("search"); raw != "" {
		out.Search = &raw
	}
	return out
}

func defaultPage(p *int64) int64 {
	if p == nil || *p < 1 {
		return 1
	}
	return *p
}

func defaultPerPage(p *int64) int64 {
	if p == nil {
		return 20
	}
	if *p < 1 {
		return 1
	}
	if *p > 100 {
		return 100
	}
	return *p
}

func coalesceString(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}

func coalesceBool(p *bool, fallback bool) bool {
	if p == nil {
		return fallback
	}
	return *p
}

// coalescePermissionKey mirrors Rust's `body.permission_key.or(existing.permission_key)`.
func coalescePermissionKey(body, existing *string) *string {
	if body != nil {
		return body
	}
	return existing
}

// trimSpace exposes strings.TrimSpace under the package-local name we
// already use in upload.go; cheap indirection that lets tooling rename
// once if the trim logic ever needs to differ from stdlib behaviour.
func trimSpace(s string) string { return strings.TrimSpace(s) }
