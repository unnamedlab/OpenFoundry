// Properties + LinkTypes HTTP handlers (P1 of the post-PoC plan). The web
// frontend already expects these routes; before this slice the only mount
// in this service was object_types, so the apps could not display property
// metadata or navigate link relationships.
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/ontology-definition-service/internal/models"
)

func (h *Handlers) ListProperties(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	typeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid type id")
		return
	}
	items, err := h.Repo.ListProperties(r.Context(), typeID)
	if err != nil {
		slog.Error("list properties", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list properties")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *Handlers) CreateProperty(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	typeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid type id")
		return
	}
	var body models.CreatePropertyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if body.Name == "" || body.PropertyType == "" {
		writeJSONErr(w, http.StatusBadRequest, "name + property_type are required")
		return
	}
	if err := models.ValidatePropertyType(body.PropertyType); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := h.Repo.CreateProperty(r.Context(), typeID, &body)
	if err != nil {
		slog.Error("create property", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to create property")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handlers) ListLinkTypes(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var objectTypeID *uuid.UUID
	if raw := r.URL.Query().Get("object_type_id"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			writeJSONErr(w, http.StatusBadRequest, "invalid object_type_id")
			return
		}
		objectTypeID = &parsed
	}
	items, err := h.Repo.ListLinkTypes(r.Context(), objectTypeID)
	if err != nil {
		slog.Error("list link types", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list link types")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "total": len(items)})
}

func (h *Handlers) GetLinkType(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	link, err := h.Repo.GetLinkType(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if link == nil {
		writeJSONErr(w, http.StatusNotFound, "link type not found")
		return
	}
	writeJSON(w, http.StatusOK, link)
}

func (h *Handlers) CreateLinkType(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body models.CreateLinkTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if body.Name == "" || body.SourceTypeID == uuid.Nil || body.TargetTypeID == uuid.Nil {
		writeJSONErr(w, http.StatusBadRequest, "name + source_type_id + target_type_id are required")
		return
	}
	if body.Cardinality != "" && !validLinkCardinality(body.Cardinality) {
		writeJSONErr(w, http.StatusBadRequest, "invalid cardinality")
		return
	}
	if body.Visibility != "" && !validLinkVisibility(body.Visibility) {
		writeJSONErr(w, http.StatusBadRequest, "invalid visibility")
		return
	}
	created, err := h.Repo.CreateLinkType(r.Context(), &body, claims.Sub)
	if err != nil {
		slog.Error("create link type", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to create link type")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handlers) UpdateLinkType(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body models.UpdateLinkTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if body.Cardinality != nil && *body.Cardinality != "" && !validLinkCardinality(*body.Cardinality) {
		writeJSONErr(w, http.StatusBadRequest, "invalid cardinality")
		return
	}
	if body.Visibility != nil && *body.Visibility != "" && !validLinkVisibility(*body.Visibility) {
		writeJSONErr(w, http.StatusBadRequest, "invalid visibility")
		return
	}
	updated, err := h.Repo.UpdateLinkType(r.Context(), id, &body)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if updated == nil {
		writeJSONErr(w, http.StatusNotFound, "link type not found")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handlers) DeleteLinkType(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	deleted, err := h.Repo.DeleteLinkType(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		writeJSONErr(w, http.StatusNotFound, "link type not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) ListObjectTypeGroups(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	page, perPage := pagination(r)
	items, total, err := h.Repo.ListObjectTypeGroups(r.Context(), r.URL.Query().Get("search"), perPage, (page-1)*perPage)
	if err != nil {
		slog.Error("list object type groups", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list object type groups")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "total": total, "page": page, "per_page": perPage})
}

func (h *Handlers) GetObjectTypeGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	group, err := h.Repo.GetObjectTypeGroup(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if group == nil {
		writeJSONErr(w, http.StatusNotFound, "object type group not found")
		return
	}
	writeJSON(w, http.StatusOK, group)
}

func (h *Handlers) CreateObjectTypeGroup(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body models.CreateObjectTypeGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if body.Name == "" {
		writeJSONErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if body.Visibility != "" && !validLinkVisibility(body.Visibility) {
		writeJSONErr(w, http.StatusBadRequest, "invalid visibility")
		return
	}
	if body.Status != "" && !validObjectTypeGroupStatus(body.Status) {
		writeJSONErr(w, http.StatusBadRequest, "invalid status")
		return
	}
	created, err := h.Repo.CreateObjectTypeGroup(r.Context(), &body, claims.Sub)
	if err != nil {
		slog.Error("create object type group", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to create object type group")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handlers) UpdateObjectTypeGroup(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body models.UpdateObjectTypeGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if body.Visibility != nil && *body.Visibility != "" && !validLinkVisibility(*body.Visibility) {
		writeJSONErr(w, http.StatusBadRequest, "invalid visibility")
		return
	}
	if body.Status != nil && *body.Status != "" && !validObjectTypeGroupStatus(*body.Status) {
		writeJSONErr(w, http.StatusBadRequest, "invalid status")
		return
	}
	updated, err := h.Repo.UpdateObjectTypeGroup(r.Context(), id, &body, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if updated == nil {
		writeJSONErr(w, http.StatusNotFound, "object type group not found")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handlers) DeleteObjectTypeGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	deleted, err := h.Repo.DeleteObjectTypeGroup(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		writeJSONErr(w, http.StatusNotFound, "object type group not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) AddObjectTypeToGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	groupID, objectTypeID, ok := groupAndObjectIDs(w, r)
	if !ok {
		return
	}
	group, err := h.Repo.AddObjectTypeToGroup(r.Context(), groupID, objectTypeID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if group == nil {
		writeJSONErr(w, http.StatusNotFound, "object type group not found")
		return
	}
	writeJSON(w, http.StatusOK, group)
}

func (h *Handlers) RemoveObjectTypeFromGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	groupID, objectTypeID, ok := groupAndObjectIDs(w, r)
	if !ok {
		return
	}
	group, err := h.Repo.RemoveObjectTypeFromGroup(r.Context(), groupID, objectTypeID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if group == nil {
		writeJSONErr(w, http.StatusNotFound, "object type group not found")
		return
	}
	writeJSON(w, http.StatusOK, group)
}

func groupAndObjectIDs(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	groupID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid group id")
		return uuid.Nil, uuid.Nil, false
	}
	objectTypeID, err := uuid.Parse(chi.URLParam(r, "objectTypeId"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid object type id")
		return uuid.Nil, uuid.Nil, false
	}
	return groupID, objectTypeID, true
}

func validObjectTypeGroupStatus(status string) bool {
	switch status {
	case "active", "experimental", "deprecated":
		return true
	default:
		return false
	}
}

func pagination(r *http.Request) (int64, int64) {
	page := int64(1)
	perPage := int64(20)
	if raw := r.URL.Query().Get("page"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 1 {
			page = parsed
		}
	}
	if raw := r.URL.Query().Get("per_page"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
			perPage = parsed
		}
	}
	if perPage < 1 {
		perPage = 1
	}
	if perPage > 200 {
		perPage = 200
	}
	return page, perPage
}

func validLinkCardinality(cardinality string) bool {
	switch cardinality {
	case "one_to_one", "one_to_many", "many_to_one", "many_to_many":
		return true
	default:
		return false
	}
}

func validLinkVisibility(visibility string) bool {
	switch visibility {
	case "normal", "hidden", "experimental":
		return true
	default:
		return false
	}
}
