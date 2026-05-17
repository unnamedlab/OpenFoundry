// markings.go: SG.12 HTTP surface for markings inside SG.11
// categories, their permissions, and immutable lifecycle checks.

package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/repo"
)

func (h *Handlers) ListMarkingsForCategory(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "read")
	if !ok {
		return
	}
	categoryID, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	includeHidden := strings.EqualFold(r.URL.Query().Get("include_hidden"), "true")
	canSeeAll := claims.HasPermission("markings", "write") || claims.HasPermission("markings", "audit")
	items, err := h.Repo.ListMarkingsForCategory(r.Context(), tenantFromClaims(claims), claims.Sub, categoryID, includeHidden, canSeeAll, canSeeAll)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		writeJSONErr(w, http.StatusNotFound, "marking category not found")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.MarkingResponse]{Items: items})
}

func (h *Handlers) CreateMarking(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "write")
	if !ok {
		return
	}
	categoryID, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var body models.CreateMarkingRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.Slug = strings.TrimSpace(body.Slug)
	body.DisplayName = strings.TrimSpace(body.DisplayName)
	if body.ID != nil && *body.ID == uuid.Nil {
		writeJSONErr(w, http.StatusBadRequest, "id must be a non-empty uuid")
		return
	}
	if body.Slug == "" || body.DisplayName == "" {
		writeJSONErr(w, http.StatusBadRequest, "slug and display_name are required")
		return
	}
	if !validOptionalJSONObject(body.Metadata) {
		writeJSONErr(w, http.StatusBadRequest, "metadata must be a JSON object")
		return
	}
	if !validMarkingPrincipals(body.Administrators) ||
		!validMarkingPrincipals(body.Removers) ||
		!validMarkingPrincipals(body.Appliers) ||
		!validMarkingPrincipals(body.Members) {
		writeJSONErr(w, http.StatusBadRequest, "marking permission principals must use principal_kind user or group and a non-empty principal_id")
		return
	}
	normalizeMarkingPrincipals(body.Administrators)
	normalizeMarkingPrincipals(body.Removers)
	normalizeMarkingPrincipals(body.Appliers)
	normalizeMarkingPrincipals(body.Members)
	item, err := h.Repo.CreateMarking(r.Context(), tenantFromClaims(claims), claims.Sub, categoryID, &body)
	if err != nil {
		writeRBACMutationErr(w, err)
		return
	}
	if item == nil {
		writeJSONErr(w, http.StatusNotFound, "marking category not found")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handlers) GetMarking(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "read")
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	includeHidden := strings.EqualFold(r.URL.Query().Get("include_hidden"), "true")
	canSeeAll := claims.HasPermission("markings", "write") || claims.HasPermission("markings", "audit")
	item, err := h.Repo.GetMarking(r.Context(), tenantFromClaims(claims), claims.Sub, id, includeHidden, canSeeAll, canSeeAll)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if item == nil {
		writeJSONErr(w, http.StatusNotFound, "marking not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handlers) UpdateMarking(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "write")
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var body models.UpdateMarkingRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.DisplayName != nil {
		trimmed := strings.TrimSpace(*body.DisplayName)
		if trimmed == "" {
			writeJSONErr(w, http.StatusBadRequest, "display_name cannot be empty")
			return
		}
		body.DisplayName = &trimmed
	}
	if !validOptionalJSONObject(body.Metadata) {
		writeJSONErr(w, http.StatusBadRequest, "metadata must be a JSON object")
		return
	}
	item, err := h.Repo.UpdateMarking(r.Context(), tenantFromClaims(claims), claims.Sub, id, &body)
	if err != nil {
		writeRBACMutationErr(w, err)
		return
	}
	if item == nil {
		writeJSONErr(w, http.StatusNotFound, "marking not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handlers) DeleteMarking(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "write")
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	found, err := h.Repo.BlockDeleteMarking(r.Context(), tenantFromClaims(claims), claims.Sub, id)
	if err != nil && !errors.Is(err, repo.ErrMarkingDeletionUnsupported) {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeJSONErr(w, http.StatusNotFound, "marking not found")
		return
	}
	writeJSONErr(w, http.StatusMethodNotAllowed, repo.ErrMarkingDeletionUnsupported.Error())
}

func (h *Handlers) MoveMarkingCategory(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "write")
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var body models.MoveMarkingCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.TargetCategoryID == uuid.Nil {
		writeJSONErr(w, http.StatusBadRequest, "target_category_id is required")
		return
	}
	found, err := h.Repo.BlockMoveMarkingCategory(r.Context(), tenantFromClaims(claims), claims.Sub, id, body.TargetCategoryID)
	if err != nil && !errors.Is(err, repo.ErrMarkingCategoryMoveUnsupported) {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeJSONErr(w, http.StatusNotFound, "marking not found")
		return
	}
	writeJSONErr(w, http.StatusMethodNotAllowed, repo.ErrMarkingCategoryMoveUnsupported.Error())
}

func (h *Handlers) UpsertMarkingPermission(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "write")
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var body models.UpsertMarkingPermissionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.PrincipalKind = strings.ToLower(strings.TrimSpace(body.PrincipalKind))
	body.Permission = strings.ToLower(strings.TrimSpace(body.Permission))
	if !isAllowedMarkingCategoryPrincipalKind(body.PrincipalKind) || body.PrincipalID == uuid.Nil || !isAllowedMarkingPermission(body.Permission) {
		writeJSONErr(w, http.StatusBadRequest, "principal_kind, principal_id, and permission are required")
		return
	}
	perm, err := h.Repo.UpsertMarkingPermission(r.Context(), tenantFromClaims(claims), claims.Sub, id, &body)
	if err != nil {
		writeRBACMutationErr(w, err)
		return
	}
	if perm == nil {
		writeJSONErr(w, http.StatusNotFound, "marking not found")
		return
	}
	writeJSON(w, http.StatusCreated, perm)
}

func (h *Handlers) DeleteMarkingPermission(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "write")
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	principalKind := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "principal_kind")))
	if !isAllowedMarkingCategoryPrincipalKind(principalKind) {
		writeJSONErr(w, http.StatusBadRequest, "principal_kind must be user or group")
		return
	}
	principalID, err := uuid.Parse(chi.URLParam(r, "principal_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "principal_id must be a uuid")
		return
	}
	permission := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "permission")))
	if !isAllowedMarkingPermission(permission) {
		writeJSONErr(w, http.StatusBadRequest, "permission must be administrator, remover, applier, or member")
		return
	}
	deleted, err := h.Repo.DeleteMarkingPermission(r.Context(), tenantFromClaims(claims), claims.Sub, id, principalKind, principalID, permission)
	if err != nil {
		writeRBACMutationErr(w, err)
		return
	}
	if !deleted {
		writeJSONErr(w, http.StatusNotFound, "marking permission not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) ListMarkingAuditEvents(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "audit")
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	items, err := h.Repo.ListMarkingAuditEvents(r.Context(), tenantFromClaims(claims), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		writeJSONErr(w, http.StatusNotFound, "marking not found")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.MarkingAuditEvent]{Items: items})
}

func (h *Handlers) CheckMarkingPermission(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "read")
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var body models.MarkingPermissionCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	principalID := claims.Sub
	if body.PrincipalID != nil {
		if *body.PrincipalID == uuid.Nil {
			writeJSONErr(w, http.StatusBadRequest, "principal_id must be a non-empty uuid")
			return
		}
		principalID = *body.PrincipalID
	}
	resp, err := h.Repo.CheckMarkingPermission(
		r.Context(),
		tenantFromClaims(claims),
		id,
		principalID,
		body.GroupIDs,
		body.ResourceUpdateMarkingsAllowed,
		body.ExpandAccessAllowed,
	)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if resp == nil {
		writeJSONErr(w, http.StatusNotFound, "marking not found")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) ListResourceMarkings(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "read")
	if !ok {
		return
	}
	resourceKind, resourceID := normalizedResourceMarkingQuery(r)
	if resourceKind == "" || resourceID == "" {
		writeJSONErr(w, http.StatusBadRequest, "resource_kind and resource_id are required")
		return
	}
	items, err := h.Repo.ListResourceMarkings(r.Context(), tenantFromClaims(claims), resourceKind, resourceID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.ResourceMarking]{Items: items})
}

func (h *Handlers) ApplyResourceMarking(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "read")
	if !ok {
		return
	}
	var body models.ApplyResourceMarkingRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.ResourceKind, body.ResourceID = normalizeResourceMarkingInput(body.ResourceKind, body.ResourceID)
	if body.ResourceKind == "" || body.ResourceID == "" || body.MarkingID == uuid.Nil {
		writeJSONErr(w, http.StatusBadRequest, "resource_kind, resource_id, and marking_id are required")
		return
	}
	if !validOptionalJSONObject(body.Metadata) {
		writeJSONErr(w, http.StatusBadRequest, "metadata must be a JSON object")
		return
	}
	resp, err := h.Repo.ApplyResourceMarking(r.Context(), tenantFromClaims(claims), claims.Sub, nil, &body)
	if errors.Is(err, repo.ErrMarkingPermissionDenied) {
		writeJSON(w, http.StatusForbidden, resp)
		return
	}
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if resp == nil {
		writeJSONErr(w, http.StatusNotFound, "marking not found")
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handlers) RemoveResourceMarking(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "read")
	if !ok {
		return
	}
	var body models.RemoveResourceMarkingRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.ResourceKind, body.ResourceID = normalizeResourceMarkingInput(body.ResourceKind, body.ResourceID)
	if body.ResourceKind == "" || body.ResourceID == "" || body.MarkingID == uuid.Nil {
		writeJSONErr(w, http.StatusBadRequest, "resource_kind, resource_id, and marking_id are required")
		return
	}
	resp, err := h.Repo.RemoveResourceMarking(r.Context(), tenantFromClaims(claims), claims.Sub, nil, &body)
	if errors.Is(err, repo.ErrMarkingPermissionDenied) {
		writeJSON(w, http.StatusForbidden, resp)
		return
	}
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if resp == nil {
		writeJSONErr(w, http.StatusNotFound, "resource marking not found")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) ListResourceMarkingEdges(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "read")
	if !ok {
		return
	}
	resourceKind, resourceID := normalizedResourceMarkingQuery(r)
	direction := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("direction")))
	if direction == "" {
		direction = "all"
	}
	if direction != "all" && direction != "upstream" && direction != "downstream" {
		writeJSONErr(w, http.StatusBadRequest, "direction must be all, upstream, or downstream")
		return
	}
	items, err := h.Repo.ListResourceMarkingEdges(r.Context(), tenantFromClaims(claims), resourceKind, resourceID, direction)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.ResourceMarkingEdge]{Items: items})
}

func (h *Handlers) UpsertResourceMarkingEdge(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "write")
	if !ok {
		return
	}
	var body models.UpsertResourceMarkingEdgeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	normalizeResourceMarkingEdge(&body)
	if !validResourceMarkingEdge(body.SourceResourceKind, body.SourceResourceID, body.TargetResourceKind, body.TargetResourceID, body.RelationKind) {
		writeJSONErr(w, http.StatusBadRequest, "source resource, target resource, and relation_kind hierarchy or lineage are required")
		return
	}
	if !validOptionalJSONObject(body.Metadata) {
		writeJSONErr(w, http.StatusBadRequest, "metadata must be a JSON object")
		return
	}
	item, err := h.Repo.UpsertResourceMarkingEdge(r.Context(), tenantFromClaims(claims), claims.Sub, &body)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handlers) DeleteResourceMarkingEdge(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "write")
	if !ok {
		return
	}
	var body models.DeleteResourceMarkingEdgeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	normalizeDeleteResourceMarkingEdge(&body)
	if !validResourceMarkingEdge(body.SourceResourceKind, body.SourceResourceID, body.TargetResourceKind, body.TargetResourceID, body.RelationKind) {
		writeJSONErr(w, http.StatusBadRequest, "source resource, target resource, and relation_kind hierarchy or lineage are required")
		return
	}
	deleted, err := h.Repo.DeleteResourceMarkingEdge(r.Context(), tenantFromClaims(claims), &body)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		writeJSONErr(w, http.StatusNotFound, "resource marking edge not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) GetEffectiveResourceMarkings(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "read")
	if !ok {
		return
	}
	resourceKind, resourceID := normalizedResourceMarkingQuery(r)
	if resourceKind == "" || resourceID == "" {
		writeJSONErr(w, http.StatusBadRequest, "resource_kind and resource_id are required")
		return
	}
	maxDepth := parsePositiveIntQuery(r, "max_depth")
	resp, err := h.Repo.EffectiveResourceMarkings(r.Context(), tenantFromClaims(claims), resourceKind, resourceID, maxDepth)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) CheckResourceAccess(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "read")
	if !ok {
		return
	}
	var body models.ResourceAccessCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.ResourceKind, body.ResourceID = normalizeResourceMarkingInput(body.ResourceKind, body.ResourceID)
	if body.ResourceKind == "" || body.ResourceID == "" {
		writeJSONErr(w, http.StatusBadRequest, "resource_kind and resource_id are required")
		return
	}
	if body.PrincipalID != nil && *body.PrincipalID == uuid.Nil {
		writeJSONErr(w, http.StatusBadRequest, "principal_id must be a non-empty uuid")
		return
	}
	resp, err := h.Repo.CheckResourceAccess(
		r.Context(),
		tenantFromClaims(claims),
		claims.Sub,
		&body,
		claims.AllowedMarkings(),
		claims.HasActiveMarkingScope(),
	)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) PublishMarkingBuildOutput(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "read")
	if !ok {
		return
	}
	var body models.PublishMarkingBuildRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	normalizePublishMarkingBuildRequest(&body)
	if len(body.InputResources) == 0 || len(body.OutputResources) == 0 {
		writeJSONErr(w, http.StatusBadRequest, "input_resources and output_resources are required")
		return
	}
	if !validBuildResourceRefs(body.InputResources) || !validBuildResourceRefs(body.OutputResources) {
		writeJSONErr(w, http.StatusBadRequest, "resource_kind and resource_id are required for every build resource")
		return
	}
	if !validOptionalJSONObject(body.Metadata) {
		writeJSONErr(w, http.StatusBadRequest, "metadata must be a JSON object")
		return
	}
	resp, err := h.Repo.PublishMarkingBuild(r.Context(), tenantFromClaims(claims), claims.Sub, &body)
	if errors.Is(err, repo.ErrMarkingBuildBlocked) {
		writeJSON(w, http.StatusForbidden, resp)
		return
	}
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) ListMarkingBuildEvents(w http.ResponseWriter, r *http.Request) {
	claims, ok := requirePermission(w, r, "markings", "read")
	if !ok {
		return
	}
	resourceKind, resourceID := normalizedResourceMarkingQuery(r)
	items, err := h.Repo.ListMarkingBuildEvents(
		r.Context(),
		tenantFromClaims(claims),
		strings.TrimSpace(r.URL.Query().Get("build_id")),
		strings.TrimSpace(r.URL.Query().Get("transaction_id")),
		resourceKind,
		resourceID,
	)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.MarkingBuildEvent]{Items: items})
}

func isAllowedMarkingPermission(permission string) bool {
	switch permission {
	case models.MarkingPermissionAdministrator,
		models.MarkingPermissionRemover,
		models.MarkingPermissionApplier,
		models.MarkingPermissionMember:
		return true
	default:
		return false
	}
}

func validMarkingPrincipals(items []models.MarkingPrincipal) bool {
	for _, item := range items {
		kind := strings.ToLower(strings.TrimSpace(item.PrincipalKind))
		if !isAllowedMarkingCategoryPrincipalKind(kind) || item.PrincipalID == uuid.Nil {
			return false
		}
	}
	return true
}

func normalizeMarkingPrincipals(items []models.MarkingPrincipal) {
	for idx := range items {
		items[idx].PrincipalKind = strings.ToLower(strings.TrimSpace(items[idx].PrincipalKind))
	}
}

func normalizedResourceMarkingQuery(r *http.Request) (string, string) {
	return normalizeResourceMarkingInput(
		r.URL.Query().Get("resource_kind"),
		r.URL.Query().Get("resource_id"),
	)
}

func normalizeResourceMarkingInput(resourceKind, resourceID string) (string, string) {
	return strings.ToLower(strings.TrimSpace(resourceKind)), strings.TrimSpace(resourceID)
}

func normalizeResourceMarkingEdge(body *models.UpsertResourceMarkingEdgeRequest) {
	body.SourceResourceKind, body.SourceResourceID = normalizeResourceMarkingInput(body.SourceResourceKind, body.SourceResourceID)
	body.TargetResourceKind, body.TargetResourceID = normalizeResourceMarkingInput(body.TargetResourceKind, body.TargetResourceID)
	body.RelationKind = strings.ToLower(strings.TrimSpace(body.RelationKind))
}

func normalizeDeleteResourceMarkingEdge(body *models.DeleteResourceMarkingEdgeRequest) {
	body.SourceResourceKind, body.SourceResourceID = normalizeResourceMarkingInput(body.SourceResourceKind, body.SourceResourceID)
	body.TargetResourceKind, body.TargetResourceID = normalizeResourceMarkingInput(body.TargetResourceKind, body.TargetResourceID)
	body.RelationKind = strings.ToLower(strings.TrimSpace(body.RelationKind))
}

func validResourceMarkingEdge(sourceKind, sourceID, targetKind, targetID, relationKind string) bool {
	if sourceKind == "" || sourceID == "" || targetKind == "" || targetID == "" {
		return false
	}
	if sourceKind == targetKind && sourceID == targetID {
		return false
	}
	return relationKind == models.ResourceMarkingRelationHierarchy || relationKind == models.ResourceMarkingRelationLineage
}

func parsePositiveIntQuery(r *http.Request, key string) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return 0
	}
	var value int
	_, _ = fmt.Sscanf(raw, "%d", &value)
	if value < 0 {
		return 0
	}
	return value
}

func normalizePublishMarkingBuildRequest(body *models.PublishMarkingBuildRequest) {
	body.BuildID = strings.TrimSpace(body.BuildID)
	body.TransactionID = strings.TrimSpace(body.TransactionID)
	body.Reason = strings.TrimSpace(body.Reason)
	for idx := range body.InputResources {
		body.InputResources[idx] = normalizeBuildResourceRef(body.InputResources[idx])
	}
	for idx := range body.OutputResources {
		body.OutputResources[idx] = normalizeBuildResourceRef(body.OutputResources[idx])
	}
}

func normalizeBuildResourceRef(ref models.MarkingBuildResourceRef) models.MarkingBuildResourceRef {
	ref.ResourceKind, ref.ResourceID = normalizeResourceMarkingInput(ref.ResourceKind, ref.ResourceID)
	return ref
}

func validBuildResourceRefs(items []models.MarkingBuildResourceRef) bool {
	for _, item := range items {
		if item.ResourceKind == "" || item.ResourceID == "" {
			return false
		}
	}
	return true
}
