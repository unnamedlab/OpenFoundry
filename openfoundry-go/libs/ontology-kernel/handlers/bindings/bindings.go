// Package bindings ports `libs/ontology-kernel/src/handlers/bindings.rs`
// 1:1: ObjectType ↔ dataset binding handlers (Foundry "Models in
// the Ontology"). 6 endpoints under
// `/ontology/types/{type_id}/bindings`:
//
//   - POST   /                       → CreateObjectTypeBinding
//   - GET    /                       → ListObjectTypeBindings
//   - GET    /{binding_id}           → GetObjectTypeBinding
//   - PATCH  /{binding_id}           → UpdateObjectTypeBinding
//   - DELETE /{binding_id}           → DeleteObjectTypeBinding
//   - POST   /{binding_id}/materialize → MaterializeObjectTypeBinding
//
// The materialise path projects dataset-preview rows into the
// Cassandra-backed ObjectStore via the helpers in
// `handlers/objects` (ApplyObjectWrite + AppendObjectRevision +
// FindObjectIDByProperty + ValueAsStoreText).

package bindings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/domain"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/handlers/objects"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// Mount registers every endpoint on the chi router.
func Mount(r chi.Router, state *ontologykernel.AppState) {
	r.Post("/ontology/types/{type_id}/bindings", CreateObjectTypeBinding(state))
	r.Get("/ontology/types/{type_id}/bindings", ListObjectTypeBindings(state))
	r.Get("/ontology/types/{type_id}/bindings/{binding_id}", GetObjectTypeBinding(state))
	r.Patch("/ontology/types/{type_id}/bindings/{binding_id}", UpdateObjectTypeBinding(state))
	r.Delete("/ontology/types/{type_id}/bindings/{binding_id}", DeleteObjectTypeBinding(state))
	r.Post("/ontology/types/{type_id}/bindings/{binding_id}/materialize", MaterializeObjectTypeBinding(state))
}

// ── HTTP plumbing ────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func errBody(msg string) map[string]string { return map[string]string{"error": msg} }

func unauthorized(w http.ResponseWriter)        { writeJSON(w, http.StatusUnauthorized, errBody("missing claims")) }
func badRequest(w http.ResponseWriter, m string) { writeJSON(w, http.StatusBadRequest, errBody(m)) }
func forbidden(w http.ResponseWriter, m string)  { writeJSON(w, http.StatusForbidden, errBody(m)) }
func notFound(w http.ResponseWriter, m string)   { writeJSON(w, http.StatusNotFound, errBody(m)) }
func internalError(w http.ResponseWriter, m string) {
	writeJSON(w, http.StatusInternalServerError, errBody(m))
}

func pathUUID(r *http.Request, key string) (uuid.UUID, error) {
	raw := chi.URLParam(r, key)
	if raw == "" {
		return uuid.Nil, errors.New("missing path parameter " + key)
	}
	return uuid.Parse(strings.TrimSpace(raw))
}

// ── Manage-access helpers ────────────────────────────────────────────────

// ensureCanManage mirrors `async fn ensure_can_manage`. Admins
// always pass; everyone else needs manage access on the object type
// the binding lives under.
func ensureCanManage(ctx context.Context, state *ontologykernel.AppState, claims *authmw.Claims, ot *models.ObjectType) error {
	if claims.HasRole("admin") {
		return nil
	}
	pid, err := domain.LoadResourceProjectID(ctx, state.DB, domain.OntologyResourceKindObjectType, ot.ID)
	if err != nil {
		return errors.New("failed to load project binding: " + err.Error())
	}
	return domain.EnsureResourceManageAccess(ctx, state.DB, claims, ot.OwnerID, pid)
}

// ensureCanManageByID mirrors `async fn ensure_can_manage_by_id`.
// Returns the loaded ObjectType so the caller can reuse the row.
// Returns a sentinel error string "object type not found" so the
// caller can map to 404.
func ensureCanManageByID(ctx context.Context, state *ontologykernel.AppState, claims *authmw.Claims, objectTypeID uuid.UUID) (*models.ObjectType, error) {
	ot, err := domain.LoadObjectType(ctx, state.DB, objectTypeID)
	if err != nil {
		return nil, errors.New("failed to load object type: " + err.Error())
	}
	if ot == nil {
		return nil, errors.New("object type not found")
	}
	if err := ensureCanManage(ctx, state, claims, ot); err != nil {
		return nil, err
	}
	return ot, nil
}

// ── Validators ───────────────────────────────────────────────────────────

// validateMarking mirrors the Rust closed-set validator. Accepts the
// 5 canonical markings; rejects with the verbatim Rust string.
func validateMarking(marking string) error {
	switch marking {
	case "public", "internal", "confidential", "pii", "restricted":
		return nil
	}
	return errors.New("marking '" + marking + "' is not supported; expected one of: public, internal, confidential, pii, restricted")
}

// validateMappingTargets mirrors `fn validate_mapping_targets`. Each
// entry must have non-empty source_field and target_property; no
// duplicate target_property is allowed.
func validateMappingTargets(mapping []models.ObjectTypeBindingPropertyMapping) error {
	seen := map[string]bool{}
	for _, e := range mapping {
		if strings.TrimSpace(e.SourceField) == "" {
			return errors.New("property_mapping.source_field cannot be empty")
		}
		if strings.TrimSpace(e.TargetProperty) == "" {
			return errors.New("property_mapping.target_property cannot be empty")
		}
		if seen[e.TargetProperty] {
			return errors.New("property_mapping.target_property '" + e.TargetProperty + "' is duplicated")
		}
		seen[e.TargetProperty] = true
	}
	return nil
}

// ── CRUD endpoints ───────────────────────────────────────────────────────

// CreateObjectTypeBinding mirrors `pub async fn create_object_type_binding`.
func CreateObjectTypeBinding(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		typeID, err := pathUUID(r, "type_id")
		if err != nil {
			badRequest(w, "invalid type_id")
			return
		}
		ot, err := ensureCanManageByID(r.Context(), state, claims, typeID)
		if err != nil {
			if err.Error() == "object type not found" {
				notFound(w, err.Error())
				return
			}
			if strings.HasPrefix(err.Error(), "failed to") {
				internalError(w, err.Error())
				return
			}
			forbidden(w, err.Error())
			return
		}
		var body models.CreateObjectTypeBindingRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		if strings.TrimSpace(body.PrimaryKeyColumn) == "" {
			badRequest(w, "primary_key_column is required")
			return
		}
		if err := validateMappingTargets(body.PropertyMapping); err != nil {
			badRequest(w, err.Error())
			return
		}
		marking := "public"
		if body.DefaultMarking != nil {
			marking = *body.DefaultMarking
		}
		if err := validateMarking(marking); err != nil {
			badRequest(w, err.Error())
			return
		}
		// If the target ObjectType declares a primary key property,
		// the mapping (when non-empty) must project to it.
		if ot.PrimaryKeyProperty != nil {
			pk := *ot.PrimaryKeyProperty
			hasPK := false
			for _, m := range body.PropertyMapping {
				if m.TargetProperty == pk {
					hasPK = true
					break
				}
			}
			if !hasPK && len(body.PropertyMapping) > 0 {
				badRequest(w, "property_mapping must project to the object type's primary key property '"+pk+"'")
				return
			}
		}
		previewLimit := int32(1000)
		if body.PreviewLimit != nil {
			previewLimit = clampInt32(*body.PreviewLimit, 1, 100_000)
		}
		mappingJSON, err := json.Marshal(body.PropertyMapping)
		if err != nil {
			internalError(w, "failed to encode property_mapping: "+err.Error())
			return
		}
		id, err := uuid.NewV7()
		if err != nil {
			internalError(w, err.Error())
			return
		}
		binding, repoErr := domain.CreateBinding(r.Context(), state.DB, domain.CreateBindingInput{
			ID:               id,
			ObjectTypeID:     typeID,
			DatasetID:        body.DatasetID,
			DatasetBranch:    body.DatasetBranch,
			DatasetVersion:   body.DatasetVersion,
			PrimaryKeyColumn: body.PrimaryKeyColumn,
			PropertyMapping:  mappingJSON,
			SyncMode:         body.SyncMode,
			DefaultMarking:   marking,
			PreviewLimit:     previewLimit,
			OwnerID:          claims.Sub,
		})
		if repoErr != nil {
			if c := repoErr.Constraint(); c != "" {
				badRequest(w, "binding violates constraint '"+c+"'")
				return
			}
			internalError(w, "failed to insert binding: "+repoErr.Error())
			return
		}
		writeJSON(w, http.StatusCreated, binding)
	}
}

// ListObjectTypeBindings mirrors `pub async fn list_object_type_bindings`.
func ListObjectTypeBindings(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		typeID, err := pathUUID(r, "type_id")
		if err != nil {
			badRequest(w, "invalid type_id")
			return
		}
		if _, err := ensureCanManageByID(r.Context(), state, claims, typeID); err != nil {
			if err.Error() == "object type not found" {
				notFound(w, err.Error())
				return
			}
			if strings.HasPrefix(err.Error(), "failed to") {
				internalError(w, err.Error())
				return
			}
			forbidden(w, err.Error())
			return
		}
		data, repoErr := domain.ListBindings(r.Context(), state.DB, typeID)
		if repoErr != nil {
			internalError(w, "failed to list bindings: "+repoErr.Error())
			return
		}
		writeJSON(w, http.StatusOK, models.ListObjectTypeBindingsResponse{Data: data})
	}
}

// GetObjectTypeBinding mirrors `pub async fn get_object_type_binding`.
func GetObjectTypeBinding(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		typeID, err := pathUUID(r, "type_id")
		if err != nil {
			badRequest(w, "invalid type_id")
			return
		}
		bindingID, err := pathUUID(r, "binding_id")
		if err != nil {
			badRequest(w, "invalid binding_id")
			return
		}
		if _, err := ensureCanManageByID(r.Context(), state, claims, typeID); err != nil {
			if err.Error() == "object type not found" {
				notFound(w, err.Error())
				return
			}
			if strings.HasPrefix(err.Error(), "failed to") {
				internalError(w, err.Error())
				return
			}
			forbidden(w, err.Error())
			return
		}
		binding, repoErr := domain.LoadBinding(r.Context(), state.DB, typeID, bindingID)
		if repoErr != nil {
			internalError(w, "failed to load binding: "+repoErr.Error())
			return
		}
		if binding == nil {
			notFound(w, "binding not found")
			return
		}
		writeJSON(w, http.StatusOK, binding)
	}
}

// UpdateObjectTypeBinding mirrors `pub async fn update_object_type_binding`.
// Body fields fall back to the existing binding's values when nil.
func UpdateObjectTypeBinding(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		typeID, err := pathUUID(r, "type_id")
		if err != nil {
			badRequest(w, "invalid type_id")
			return
		}
		bindingID, err := pathUUID(r, "binding_id")
		if err != nil {
			badRequest(w, "invalid binding_id")
			return
		}
		if _, err := ensureCanManageByID(r.Context(), state, claims, typeID); err != nil {
			if err.Error() == "object type not found" {
				notFound(w, err.Error())
				return
			}
			if strings.HasPrefix(err.Error(), "failed to") {
				internalError(w, err.Error())
				return
			}
			forbidden(w, err.Error())
			return
		}
		existing, repoErr := domain.LoadBinding(r.Context(), state.DB, typeID, bindingID)
		if repoErr != nil {
			internalError(w, "failed to load binding: "+repoErr.Error())
			return
		}
		if existing == nil {
			notFound(w, "binding not found")
			return
		}
		var body models.UpdateObjectTypeBindingRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		datasetBranch := existing.DatasetBranch
		if body.DatasetBranch != nil {
			datasetBranch = body.DatasetBranch
		}
		datasetVersion := existing.DatasetVersion
		if body.DatasetVersion != nil {
			datasetVersion = body.DatasetVersion
		}
		primaryKey := existing.PrimaryKeyColumn
		if body.PrimaryKeyColumn != nil {
			primaryKey = *body.PrimaryKeyColumn
		}
		mapping := existing.PropertyMapping
		if body.PropertyMapping != nil {
			mapping = *body.PropertyMapping
		}
		if err := validateMappingTargets(mapping); err != nil {
			badRequest(w, err.Error())
			return
		}
		syncMode := existing.SyncMode
		if body.SyncMode != nil {
			syncMode = *body.SyncMode
		}
		marking := existing.DefaultMarking
		if body.DefaultMarking != nil {
			marking = *body.DefaultMarking
		}
		if err := validateMarking(marking); err != nil {
			badRequest(w, err.Error())
			return
		}
		previewLimit := existing.PreviewLimit
		if body.PreviewLimit != nil {
			previewLimit = clampInt32(*body.PreviewLimit, 1, 100_000)
		}
		mappingJSON, err := json.Marshal(mapping)
		if err != nil {
			internalError(w, "failed to encode property_mapping: "+err.Error())
			return
		}
		updated, repoErr := domain.UpdateBinding(r.Context(), state.DB, domain.UpdateBindingInput{
			BindingID:        bindingID,
			DatasetBranch:    datasetBranch,
			DatasetVersion:   datasetVersion,
			PrimaryKeyColumn: primaryKey,
			PropertyMapping:  mappingJSON,
			SyncMode:         syncMode,
			DefaultMarking:   marking,
			PreviewLimit:     previewLimit,
		})
		if repoErr != nil {
			internalError(w, "failed to update binding: "+repoErr.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated)
	}
}

// DeleteObjectTypeBinding mirrors `pub async fn delete_object_type_binding`.
func DeleteObjectTypeBinding(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		typeID, err := pathUUID(r, "type_id")
		if err != nil {
			badRequest(w, "invalid type_id")
			return
		}
		bindingID, err := pathUUID(r, "binding_id")
		if err != nil {
			badRequest(w, "invalid binding_id")
			return
		}
		if _, err := ensureCanManageByID(r.Context(), state, claims, typeID); err != nil {
			if err.Error() == "object type not found" {
				notFound(w, err.Error())
				return
			}
			if strings.HasPrefix(err.Error(), "failed to") {
				internalError(w, err.Error())
				return
			}
			forbidden(w, err.Error())
			return
		}
		deleted, repoErr := domain.DeleteBinding(r.Context(), state.DB, typeID, bindingID)
		if repoErr != nil {
			internalError(w, "failed to delete binding: "+repoErr.Error())
			return
		}
		if !deleted {
			notFound(w, "binding not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── Materialise ──────────────────────────────────────────────────────────

// datasetPreviewPayload mirrors the Rust private struct.
type datasetPreviewPayload struct {
	Rows []json.RawMessage `json:"rows"`
}

// issueServiceToken mirrors `fn issue_service_token`. Mints an
// "ontology-service" admin-scoped JWT impersonating the caller for
// the duration of the dataset preview fetch.
func issueServiceToken(state *ontologykernel.AppState, claims *authmw.Claims) (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("failed to issue service token: %w", err)
	}
	attrs, _ := json.Marshal(map[string]any{
		"service":                  "ontology-service",
		"classification_clearance": "pii",
		"impersonated_actor_id":    claims.Sub,
	})
	in := authmw.AccessClaimsInput{
		UserID:      id,
		Email:       "ontology-service@internal.openfoundry",
		Name:        "ontology-service",
		Roles:       []string{"admin"},
		Permissions: []string{"*:*"},
		OrgID:       claims.OrgID,
		Attributes:  attrs,
		AuthMethods: []string{"service"},
	}
	serviceClaims := authmw.BuildAccessClaims(state.JWTConfig, in)
	token, err := authmw.EncodeToken(state.JWTConfig, &serviceClaims)
	if err != nil {
		return "", fmt.Errorf("failed to issue service token: %w", err)
	}
	return "Bearer " + token, nil
}

// fetchDatasetPreview mirrors `async fn fetch_dataset_preview`. Hits
// the dataset-service `/api/v1/datasets/{id}/preview` endpoint with
// the optional branch / version query parameters.
func fetchDatasetPreview(
	ctx context.Context,
	state *ontologykernel.AppState,
	claims *authmw.Claims,
	binding *models.ObjectTypeBinding,
	limit int32,
	branch *string,
	version *int32,
) (*datasetPreviewPayload, error) {
	authHeader, err := issueServiceToken(state, claims)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(fmt.Sprintf("%s/api/v1/datasets/%s/preview",
		state.DatasetServiceURL, binding.DatasetID))
	if err != nil {
		return nil, fmt.Errorf("failed to build dataset preview URL: %s", err)
	}
	q := u.Query()
	q.Set("limit", strconv.Itoa(int(limit)))
	if branch != nil {
		q.Set("branch", *branch)
	}
	if version != nil {
		q.Set("version", strconv.Itoa(int(*version)))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch dataset preview: %s", err)
	}
	req.Header.Set("Authorization", authHeader)
	client := state.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch dataset preview: %s", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read dataset preview response: %s", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("dataset preview failed with HTTP %d: %s", resp.StatusCode, string(body))
	}
	var payload datasetPreviewPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("failed to decode dataset preview payload: %s", err)
	}
	return &payload, nil
}

// projectRow mirrors `fn project_row`. Empty mapping → return the
// row as-is (Rust `Value::Object(object.clone())`); otherwise pick
// only the source_field → target_property entries that are present.
func projectRow(row json.RawMessage, mapping []models.ObjectTypeBindingPropertyMapping) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(row, &obj); err != nil || obj == nil {
		return nil, errors.New("dataset preview row is not a JSON object")
	}
	if len(mapping) == 0 {
		out, _ := json.Marshal(obj)
		return out, nil
	}
	projected := map[string]json.RawMessage{}
	for _, e := range mapping {
		if v, ok := obj[e.SourceField]; ok {
			projected[e.TargetProperty] = v
		}
	}
	out, _ := json.Marshal(projected)
	return out, nil
}

// extractPrimaryKey mirrors `fn extract_primary_key`.
func extractPrimaryKey(row json.RawMessage, primaryKeyColumn string) (string, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(row, &obj); err != nil || obj == nil {
		return "", errors.New("row is not an object")
	}
	v, ok := obj[primaryKeyColumn]
	if !ok {
		return "", errors.New("row is missing primary key column '" + primaryKeyColumn + "'")
	}
	text, err := objects.ValueAsStoreText(v)
	if err != nil {
		return "", errors.New("failed to extract primary key column '" + primaryKeyColumn + "': " + err.Error())
	}
	return text, nil
}

// upsertInstance mirrors `async fn upsert_instance`. Returns
// "insert" / "update" / "skipped" for the materialise summary.
func upsertInstance(
	ctx context.Context,
	state *ontologykernel.AppState,
	claims *authmw.Claims,
	binding *models.ObjectTypeBinding,
	objectID *uuid.UUID,
	properties json.RawMessage,
) (string, error) {
	now := time.Now().UTC()
	var (
		object          domain.ObjectInstance
		expectedVersion *uint64
		operation       string
	)
	if objectID != nil {
		tenant := domain.TenantFromClaims(claims)
		existing, err := state.Stores.Objects.Get(ctx, tenant, storage.ObjectId(objectID.String()), storage.Strong())
		if err != nil {
			return "", errors.New("failed to load existing object instance: " + err.Error())
		}
		if existing == nil {
			return "", errors.New("existing object was not found in object store")
		}
		createdBy := claims.Sub
		if existing.Owner != nil {
			if u, err := uuid.Parse(string(*existing.Owner)); err == nil {
				createdBy = u
			}
		}
		var orgID *uuid.UUID
		if existing.OrganizationID != nil {
			if u, err := uuid.Parse(*existing.OrganizationID); err == nil {
				orgID = &u
			}
		}
		if orgID == nil {
			orgID = claims.OrgID
		}
		createdAt := now
		if existing.CreatedAtMs != nil {
			createdAt = time.UnixMilli(*existing.CreatedAtMs).UTC()
		} else {
			createdAt = time.UnixMilli(existing.UpdatedAtMs).UTC()
		}
		object = domain.ObjectInstance{
			ID:             *objectID,
			ObjectTypeID:   binding.ObjectTypeID,
			Properties:     properties,
			CreatedBy:      createdBy,
			OrganizationID: orgID,
			Marking:        binding.DefaultMarking,
			CreatedAt:      createdAt,
			UpdatedAt:      now,
		}
		v := existing.Version
		expectedVersion = &v
		operation = "update"
	} else {
		newID, err := uuid.NewV7()
		if err != nil {
			return "", err
		}
		object = domain.ObjectInstance{
			ID:             newID,
			ObjectTypeID:   binding.ObjectTypeID,
			Properties:     properties,
			CreatedBy:      claims.Sub,
			OrganizationID: claims.OrgID,
			Marking:        binding.DefaultMarking,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		operation = "insert"
	}
	extra, _ := json.Marshal(map[string]any{
		"source":     "object_type_binding",
		"binding_id": binding.ID,
	})
	outcome, err := objects.ApplyObjectWrite(ctx, state, claims, &object, expectedVersion, operation, extra)
	if err != nil {
		return "", err
	}
	if err := objects.AppendObjectRevision(ctx, state, claims, &object, operation, int64(outcome.CommittedVersion), nil); err != nil {
		return "", err
	}
	return operation, nil
}

// MaterializeObjectTypeBinding mirrors `pub async fn materialize_object_type_binding`.
// Per-row pipeline: project → validate → extract pk → find existing
// id → upsert. View-mode bindings reject (read-through). dry_run
// counts what would happen without writing.
func MaterializeObjectTypeBinding(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		typeID, err := pathUUID(r, "type_id")
		if err != nil {
			badRequest(w, "invalid type_id")
			return
		}
		bindingID, err := pathUUID(r, "binding_id")
		if err != nil {
			badRequest(w, "invalid binding_id")
			return
		}
		ot, err := ensureCanManageByID(r.Context(), state, claims, typeID)
		if err != nil {
			if err.Error() == "object type not found" {
				notFound(w, err.Error())
				return
			}
			if strings.HasPrefix(err.Error(), "failed to") {
				internalError(w, err.Error())
				return
			}
			forbidden(w, err.Error())
			return
		}
		binding, repoErr := domain.LoadBinding(r.Context(), state.DB, typeID, bindingID)
		if repoErr != nil {
			internalError(w, "failed to load binding: "+repoErr.Error())
			return
		}
		if binding == nil {
			notFound(w, "binding not found")
			return
		}
		if binding.SyncMode == models.ObjectTypeBindingSyncModeView {
			badRequest(w, "view-mode bindings are read-through; materialise is not applicable")
			return
		}
		if ot.PrimaryKeyProperty == nil {
			badRequest(w, "object type must define primary_key_property to materialise a binding")
			return
		}
		primaryKeyProperty := *ot.PrimaryKeyProperty

		definitions, err := domain.LoadEffectiveProperties(r.Context(), state.DB, typeID)
		if err != nil {
			internalError(w, "failed to load object type properties: "+err.Error())
			return
		}

		var body models.MaterializeBindingRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			// dry_run defaults to false on empty body — matches Rust
			// `#[serde(default)]`.
			body = models.MaterializeBindingRequest{}
		}
		limit := binding.PreviewLimit
		if body.Limit != nil {
			limit = clampInt32(*body.Limit, 1, binding.PreviewLimit)
		}
		branch := body.DatasetBranch
		if branch == nil {
			branch = binding.DatasetBranch
		}
		version := body.DatasetVersion
		if version == nil {
			version = binding.DatasetVersion
		}
		preview, err := fetchDatasetPreview(r.Context(), state, claims, binding, limit, branch, version)
		if err != nil {
			internalError(w, err.Error())
			return
		}

		var (
			rowsRead, inserted, updated, skipped, errs int32
			errorDetails                              []json.RawMessage
		)
		for index, row := range preview.Rows {
			rowsRead++
			projected, err := projectRow(row, binding.PropertyMapping)
			if err != nil {
				errs++
				errorDetails = append(errorDetails, errorDetail(index, err))
				continue
			}
			normalized, err := domain.ValidateObjectProperties(definitions, projected)
			if err != nil {
				errs++
				errorDetails = append(errorDetails, errorDetail(index, err))
				continue
			}
			pkValue, err := extractPrimaryKey(normalized, primaryKeyProperty)
			if err != nil {
				errs++
				errorDetails = append(errorDetails, errorDetail(index, err))
				continue
			}
			existingID, err := objects.FindObjectIDByProperty(r.Context(), state, claims, typeID, primaryKeyProperty, pkValue, storage.Strong())
			if err != nil {
				errs++
				errorDetails = append(errorDetails, errorDetail(index, err))
				continue
			}
			if body.DryRun {
				if existingID != nil {
					updated++
				} else {
					inserted++
				}
				continue
			}
			op, err := upsertInstance(r.Context(), state, claims, binding, existingID, normalized)
			if err != nil {
				errs++
				errorDetails = append(errorDetails, errorDetail(index, err))
				continue
			}
			switch op {
			case "insert":
				inserted++
			case "update":
				updated++
			default:
				skipped++
			}
		}

		status := "completed"
		if errs > 0 {
			if inserted+updated > 0 {
				status = "completed_with_errors"
			} else {
				status = "failed"
			}
		}

		if !body.DryRun {
			summary, _ := json.Marshal(map[string]any{
				"rows_read": rowsRead,
				"inserted":  inserted,
				"updated":   updated,
				"skipped":   skipped,
				"errors":    errs,
				"dry_run":   body.DryRun,
			})
			_ = domain.RecordMaterializationResult(r.Context(), state.DB, bindingID, status, summary)
		}

		writeJSON(w, http.StatusOK, models.MaterializeBindingResponse{
			BindingID:    bindingID,
			Status:       status,
			RowsRead:     rowsRead,
			Inserted:     inserted,
			Updated:      updated,
			Skipped:      skipped,
			Errors:       errs,
			DryRun:       body.DryRun,
			ErrorDetails: errorDetails,
		})
	}
}

func errorDetail(rowIndex int, err error) json.RawMessage {
	out, _ := json.Marshal(map[string]any{
		"row_index": rowIndex,
		"error":     err.Error(),
	})
	return out
}

func clampInt32(value, lo, hi int32) int32 {
	if value < lo {
		return lo
	}
	if value > hi {
		return hi
	}
	return value
}
