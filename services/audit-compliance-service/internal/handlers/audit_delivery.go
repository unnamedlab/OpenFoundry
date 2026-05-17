package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/domain/security"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/repo"
)

func (h *Handlers) ListAuditDeliveryDestinations(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireAuditLogAccess(w, r)
	if !ok {
		return
	}
	items, err := h.Repo.ListAuditDeliveryDestinations(r.Context())
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	filtered := make([]models.AuditDeliveryDestination, 0, len(items))
	for _, item := range items {
		if claims.AllowsOrgID(item.OrganizationID) {
			filtered = append(filtered, item)
		}
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.AuditDeliveryDestination]{Items: filtered})
}

func (h *Handlers) CreateAuditDeliveryDestination(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireAuditDeliveryManage(w, r)
	if !ok {
		return
	}
	var body models.CreateAuditDeliveryDestinationRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if !claims.AllowsOrgID(body.OrganizationID) {
		writeJSONErr(w, http.StatusForbidden, "organization is outside session scope")
		return
	}
	if !validOptionalJSONObject(body.Metadata) {
		writeJSONErr(w, http.StatusBadRequest, "metadata must be a JSON object")
		return
	}
	item, err := h.Repo.CreateAuditDeliveryDestination(r.Context(), &body, claims.Sub.String())
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handlers) ValidateAuditDeliveryDestination(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireAuditDeliveryManage(w, r)
	if !ok {
		return
	}
	id, ok := parseAuditDeliveryID(w, r)
	if !ok {
		return
	}
	dest, err := h.Repo.GetAuditDeliveryDestination(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if dest == nil {
		writeJSONErr(w, http.StatusNotFound, "destination not found")
		return
	}
	if !claims.AllowsOrgID(dest.OrganizationID) {
		writeJSONErr(w, http.StatusForbidden, "organization is outside session scope")
		return
	}
	resp, err := h.Repo.ValidateAuditDeliveryDestination(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) BackfillAuditDeliveryDestination(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireAuditDeliveryManage(w, r)
	if !ok {
		return
	}
	id, ok := parseAuditDeliveryID(w, r)
	if !ok {
		return
	}
	dest, err := h.Repo.GetAuditDeliveryDestination(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if dest == nil {
		writeJSONErr(w, http.StatusNotFound, "destination not found")
		return
	}
	if !claims.AllowsOrgID(dest.OrganizationID) {
		writeJSONErr(w, http.StatusForbidden, "organization is outside session scope")
		return
	}
	var body models.AuditDeliveryBackfillRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	file, err := h.Repo.BackfillAuditDeliveryDestination(r.Context(), id, &body)
	if errors.Is(err, repo.ErrAuditDeliveryDestinationInvalid) {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, file)
}

func (h *Handlers) ListAuditDeliveryFiles(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireAuditLogAccess(w, r)
	if !ok {
		return
	}
	orgID, ok := optionalUUIDQuery(w, r, "organization_id")
	if !ok {
		return
	}
	if !claims.AllowsOrgID(orgID) {
		writeJSONErr(w, http.StatusForbidden, "organization is outside session scope")
		return
	}
	start, ok := optionalTimeQuery(w, r, "start_time")
	if !ok {
		return
	}
	end, ok := optionalTimeQuery(w, r, "end_time")
	if !ok {
		return
	}
	items, err := h.Repo.ListAuditDeliveryFiles(r.Context(), orgID, start, end, strings.TrimSpace(r.URL.Query().Get("schema_version")))
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	filtered := make([]models.AuditDeliveryFile, 0, len(items))
	for _, item := range items {
		if claims.AllowsOrgID(item.OrganizationID) {
			filtered = append(filtered, item)
		}
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.AuditDeliveryFile]{Items: filtered})
}

func (h *Handlers) GetAuditDeliveryFileContent(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireAuditLogAccess(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	content, err := h.Repo.GetAuditDeliveryFileContent(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if content == nil || !claims.AllowsOrgID(content.File.OrganizationID) {
		writeJSONErr(w, http.StatusNotFound, "audit delivery file not found")
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		writeJSON(w, http.StatusOK, content)
		return
	}
	w.Header().Set("Content-Type", content.File.ContentFormat+"; charset=utf-8")
	w.Header().Set("X-Audit-Schema-Version", content.File.SchemaVersion)
	w.Header().Set("X-Audit-Duplicate-Count", int64String(content.File.DuplicateCount))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(content.Content))
}

func requireAuditDeliveryManage(w http.ResponseWriter, r *http.Request) (*authmw.Claims, bool) {
	claims, ok := requireAuditLogAccess(w, r)
	if !ok {
		return nil, false
	}
	if !security.CanManageAuditDelivery(claims) {
		writeJSONErr(w, http.StatusForbidden, "audit delivery management requires audit-delivery:manage")
		return nil, false
	}
	return claims, true
}

func parseAuditDeliveryID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return uuid.Nil, false
	}
	return id, true
}

func optionalUUIDQuery(w http.ResponseWriter, r *http.Request, name string) (*uuid.UUID, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid "+name)
		return nil, false
	}
	return &id, true
}

func optionalTimeQuery(w http.ResponseWriter, r *http.Request, name string) (*time.Time, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return nil, true
	}
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		value, err = time.Parse(time.RFC3339, raw)
	}
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid "+name)
		return nil, false
	}
	value = value.UTC()
	return &value, true
}

func int64String(v int64) string {
	return strconv.FormatInt(v, 10)
}
