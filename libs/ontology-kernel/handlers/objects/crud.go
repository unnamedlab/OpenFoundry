package objects

// crud.go ports the CRUD slice of
// libs/ontology-kernel/src/handlers/objects.rs:
//
//   - POST   /ontology/types/{type_id}/objects        → CreateObject
//   - GET    /ontology/types/{type_id}/objects        → ListObjects
//   - GET    /ontology/types/{type_id}/objects/{id}   → GetObject
//   - PATCH  /ontology/types/{type_id}/objects/{id}   → UpdateObject
//   - DELETE /ontology/types/{type_id}/objects/{id}   → DeleteObject
//
// The remaining ~58 handlers in objects.rs (query/knn/timeline/
// simulate/scenarios/revisions/views) port in subsequent slices —
// the helpers in objects.go (LoadObjectInstance, ApplyObjectWrite,
// AppendObjectRevision, …) already cover their write/read paths.

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
	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/domain"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// nowUTC is a thin wrapper so tests can replace the clock with a
// fixed-point if/when the upstream `time.Now()` mocking helper lands.
// Today it just delegates to `time.Now().UTC()` — same as Rust's
// `Utc::now()` call site.
var nowUTC = func() time.Time { return time.Now().UTC() }

// ─── Request / response payloads ───────────────────────────────────────

// CreateObjectRequest mirrors `pub struct CreateObjectRequest`.
// Marking is optional — defaults to "public" when absent.
type CreateObjectRequest struct {
	Properties json.RawMessage `json:"properties"`
	Marking    *string         `json:"marking,omitempty"`
}

// UpdateObjectRequest mirrors `pub struct UpdateObjectRequest`.
// Replace is optional — when absent or false, Properties is treated as
// a patch (merge into existing). When true, Properties replaces the
// stored properties wholesale.
type UpdateObjectRequest struct {
	Properties json.RawMessage `json:"properties"`
	Replace    *bool           `json:"replace,omitempty"`
	Marking    *string         `json:"marking,omitempty"`
}

// ListObjectsResponse is the envelope for ListObjects. Mirrors the
// Rust `Json(json!({"data": …, "total": …, "page": …, "per_page": …}))`.
type ListObjectsResponse struct {
	Data    []*domain.ObjectInstance `json:"data"`
	Total   int64                    `json:"total"`
	Page    int64                    `json:"page"`
	PerPage int64                    `json:"per_page"`
}

// ─── Mount + HTTP plumbing ─────────────────────────────────────────────

// Mount registers every CRUD endpoint on the chi router under the
// same path / verb shape as `lib.rs::build_router` for objects.
func Mount(r chi.Router, state *ontologykernel.AppState) {
	r.Post("/ontology/types/{type_id}/objects", CreateObject(state))
	r.Get("/ontology/types/{type_id}/objects", ListObjects(state))
	r.Get("/ontology/types/{type_id}/objects/{obj_id}", GetObject(state))
	r.Patch("/ontology/types/{type_id}/objects/{obj_id}", UpdateObject(state))
	r.Delete("/ontology/types/{type_id}/objects/{obj_id}", DeleteObject(state))
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func errBody(msg string) map[string]string { return map[string]string{"error": msg} }

func unauthorized(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, errBody("missing claims"))
}

func badRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, errBody(msg))
}

func forbidden(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusForbidden, errBody(msg))
}

// repoErrorResponse mirrors `pub(crate) fn repo_error_response`.
// Logs the error for ops + returns 500 with no body, matching the
// Rust `StatusCode::INTERNAL_SERVER_ERROR.into_response()` shape.
func repoErrorResponse(w http.ResponseWriter, ctx string, err error) {
	slog.Error("ontology objects handler", slog.String("context", ctx), slog.Any("error", err))
	w.WriteHeader(http.StatusInternalServerError)
}

func pathUUID(r *http.Request, key string) (uuid.UUID, error) {
	raw := chi.URLParam(r, key)
	if raw == "" {
		return uuid.Nil, errors.New("missing path parameter " + key)
	}
	return uuid.Parse(strings.TrimSpace(raw))
}

// ─── Handlers ─────────────────────────────────────────────────────────

// CreateObject mirrors `pub async fn create_object`.
func CreateObject(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		typeID, err := pathUUID(r, "type_id")
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		var body CreateObjectRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		marking := "public"
		if body.Marking != nil {
			marking = *body.Marking
		}
		if err := domain.ValidateMarking(marking); err != nil {
			badRequest(w, err.Error())
			return
		}

		definitions, err := domain.LoadEffectiveProperties(r.Context(), state.DB, typeID)
		if err != nil {
			slog.Error("load effective properties failed", slog.Any("error", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		normalized, err := domain.ValidateObjectProperties(definitions, body.Properties)
		if err != nil {
			badRequest(w, err.Error())
			return
		}

		id, err := uuid.NewV7()
		if err != nil {
			repoErrorResponse(w, "uuid v7", err)
			return
		}
		now := nowUTC()
		object := &domain.ObjectInstance{
			ID:             id,
			ObjectTypeID:   typeID,
			Properties:     normalized,
			CreatedBy:      claims.Sub,
			OrganizationID: claims.OrgID,
			Marking:        marking,
			CreatedAt:      now,
			UpdatedAt:      now,
		}

		outcome, err := ApplyObjectWrite(r.Context(), state, claims, object, nil, "create", json.RawMessage("{}"))
		if err != nil {
			repoErrorResponse(w, "create object failed", err)
			return
		}
		if err := AppendObjectRevision(r.Context(), state, claims, object, "create",
			int64(outcome.CommittedVersion), nil); err != nil {
			repoErrorResponse(w, "create object revision append failed", err)
			return
		}
		writeJSON(w, http.StatusCreated, object)
	}
}

// ListObjects mirrors `pub async fn list_objects`. Walks every page of
// `ListByType`, applies tenant access filter, paginates the result
// in-memory using offset/per_page (same as the Rust impl).
func ListObjects(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		typeID, err := pathUUID(r, "type_id")
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		page := parseInt64Default(r.URL.Query().Get("page"), 1)
		if page < 1 {
			page = 1
		}
		perPage := parseInt64Default(r.URL.Query().Get("per_page"), 20)
		switch {
		case perPage < 1:
			perPage = 1
		case perPage > 100:
			perPage = 100
		}

		tenant := domain.TenantFromClaims(claims)
		offset := (page - 1) * perPage
		end := offset + perPage
		var total int64
		var token *string
		data := make([]*domain.ObjectInstance, 0, perPage)

		for {
			pageResult, perr := state.Stores.Objects.ListByType(
				r.Context(), tenant, storage.TypeId(typeID.String()),
				storage.Page{Size: 200, Token: token}, storage.Strong())
			if perr != nil {
				repoErrorResponse(w, "list objects failed", perr)
				return
			}
			for _, summary := range pageResult.Items {
				instanceSummary := domain.ObjectStoreToObjectInstance(summary, claims.OrgID)
				if err := domain.EnsureObjectAccess(claims, instanceSummary); err != nil {
					continue
				}
				if total >= offset && total < end {
					full, gerr := state.Stores.Objects.Get(
						r.Context(), tenant, summary.ID, storage.Strong())
					if gerr != nil {
						repoErrorResponse(w, "list objects hydration failed", gerr)
						return
					}
					if full != nil {
						data = append(data, domain.ObjectStoreToObjectInstance(*full, claims.OrgID))
					}
				}
				total++
			}
			if pageResult.NextToken == nil {
				break
			}
			token = pageResult.NextToken
		}

		writeJSON(w, http.StatusOK, ListObjectsResponse{
			Data:    data,
			Total:   total,
			Page:    page,
			PerPage: perPage,
		})
	}
}

// GetObject mirrors `pub async fn get_object`.
func GetObject(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		objID, err := pathUUID(r, "obj_id")
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		object, err := LoadObjectInstance(r.Context(), state, claims, objID, storage.Strong())
		if err != nil {
			repoErrorResponse(w, "get object failed", err)
			return
		}
		if object == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if accessErr := domain.EnsureObjectAccess(claims, object); accessErr != nil {
			forbidden(w, accessErr.Error())
			return
		}
		writeJSON(w, http.StatusOK, object)
	}
}

// UpdateObject mirrors `pub async fn update_object`. When `replace` is
// true the request `properties` overwrite stored properties wholesale;
// otherwise `properties` is treated as a patch and shallow-merged into
// the existing object.
func UpdateObject(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		objID, err := pathUUID(r, "obj_id")
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		var body UpdateObjectRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}

		repoObject, err := LoadRepoObjectFromStore(r.Context(), state, claims, objID, storage.Strong())
		if err != nil {
			repoErrorResponse(w, "update object lookup failed", err)
			return
		}
		if repoObject == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		object := domain.ObjectStoreToObjectInstance(*repoObject, claims.OrgID)

		if accessErr := domain.EnsureObjectAccess(claims, object); accessErr != nil {
			forbidden(w, accessErr.Error())
			return
		}
		if body.Marking != nil {
			if err := domain.ValidateMarking(*body.Marking); err != nil {
				badRequest(w, err.Error())
				return
			}
		}

		definitions, err := domain.LoadEffectiveProperties(r.Context(), state.DB, object.ObjectTypeID)
		if err != nil {
			slog.Error("load effective properties failed", slog.Any("error", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		var nextProperties json.RawMessage
		if body.Replace != nil && *body.Replace {
			nextProperties = body.Properties
		} else {
			merged, mergeErr := mergePatchProperties(object.Properties, body.Properties)
			if mergeErr != nil {
				badRequest(w, mergeErr.Error())
				return
			}
			nextProperties = merged
		}

		normalized, err := domain.ValidateObjectProperties(definitions, nextProperties)
		if err != nil {
			badRequest(w, err.Error())
			return
		}

		nextMarking := object.Marking
		if body.Marking != nil {
			nextMarking = *body.Marking
		}
		updated := &domain.ObjectInstance{
			ID:             object.ID,
			ObjectTypeID:   object.ObjectTypeID,
			Properties:     normalized,
			CreatedBy:      object.CreatedBy,
			OrganizationID: object.OrganizationID,
			Marking:        nextMarking,
			CreatedAt:      object.CreatedAt,
			UpdatedAt:      nowUTC(),
		}

		expected := repoObject.Version
		outcome, err := ApplyObjectWrite(r.Context(), state, claims, updated, &expected,
			"update", json.RawMessage("{}"))
		if err != nil {
			repoErrorResponse(w, "update object failed", err)
			return
		}
		if err := AppendObjectRevision(r.Context(), state, claims, updated, "update",
			int64(outcome.CommittedVersion), nil); err != nil {
			repoErrorResponse(w, "update object revision append failed", err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	}
}

// DeleteObject mirrors `pub async fn delete_object`. Loads the object
// first so the access check + 404-vs-403 path matches Rust, then
// hands off to `ObjectStore.Delete` and appends a "delete" revision
// to the action log.
func DeleteObject(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		objID, err := pathUUID(r, "obj_id")
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		repoObject, err := LoadRepoObjectFromStore(r.Context(), state, claims, objID, storage.Strong())
		if err != nil {
			repoErrorResponse(w, "delete object lookup failed", err)
			return
		}
		if repoObject == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		object := domain.ObjectStoreToObjectInstance(*repoObject, claims.OrgID)
		if accessErr := domain.EnsureObjectAccess(claims, object); accessErr != nil {
			forbidden(w, accessErr.Error())
			return
		}

		tenant := domain.TenantFromClaims(claims)
		deleted, err := state.Stores.Objects.Delete(r.Context(), tenant, storage.ObjectId(objID.String()))
		if err != nil {
			repoErrorResponse(w, "delete object failed", err)
			return
		}
		if !deleted {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// `repoObject.version + 1` matches the Rust source's
		// revision-number convention on delete (the "next" version).
		nextVersion := int64(repoObject.Version) + 1
		if err := AppendObjectRevision(r.Context(), state, claims, object, "delete", nextVersion, nil); err != nil {
			repoErrorResponse(w, "delete object revision append failed", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ─── helpers ───────────────────────────────────────────────────────────

func parseInt64Default(raw string, def int64) int64 {
	if raw == "" {
		return def
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return def
	}
	return n
}

// mergePatchProperties shallow-merges `patch` into `current`. Both
// must decode as JSON objects; otherwise a typed error is returned
// matching the Rust BAD_REQUEST body verbatim.
func mergePatchProperties(current, patch json.RawMessage) (json.RawMessage, error) {
	merged := map[string]json.RawMessage{}
	if len(current) > 0 {
		_ = json.Unmarshal(current, &merged)
	}
	var patchMap map[string]json.RawMessage
	if err := json.Unmarshal(patch, &patchMap); err != nil || patchMap == nil {
		return nil, errors.New("properties must be a JSON object when replace=false")
	}
	for k, v := range patchMap {
		merged[k] = v
	}
	return json.Marshal(merged)
}
