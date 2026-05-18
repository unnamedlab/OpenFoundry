// Package handlers wires the HTTP endpoints for ontology-query-service.
//
// The Go service mirrors the Rust read slice: a point object read and a
// page-by-type read backed by the storage-abstraction ObjectStore. The concrete
// production store is cassandra-kernel's Cassandra implementation, but tests and
// callers can inject any implementation of the repository interfaces.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

const consistencyHeader = "X-Consistency"

type AppState struct {
	Objects repos.ObjectStore
	Links   repos.LinkStore
	Schemas repos.SchemaStore
}

type Handlers struct {
	state AppState
}

func New(state AppState) *Handlers {
	return &Handlers{state: state}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

type ListResponse[T any] struct {
	Items     []T     `json:"items"`
	NextToken *string `json:"next_token"`
}

func (h *Handlers) GetObject(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if h.state.Objects == nil {
		writeJSONErr(w, http.StatusInternalServerError, "object store not configured")
		return
	}
	tenant, ok := tenantParam(w, r)
	if !ok {
		return
	}
	objectID, ok := objectIDParam(w, r, "object_id")
	if !ok {
		return
	}
	if !canReadTenant(claims, tenant) {
		writeJSONErr(w, http.StatusForbidden, "tenant access denied")
		return
	}
	consistency, ok := consistencyHint(w, r)
	if !ok {
		return
	}

	obj, err := h.state.Objects.Get(r.Context(), repos.TenantId(tenant), repos.ObjectId(objectID), consistency)
	if err != nil {
		repoErrorToResponse(w, err)
		return
	}
	if obj == nil {
		writeJSONErr(w, http.StatusNotFound, "object not found")
		return
	}
	if !canReadMarkings(claims, obj.Markings) {
		writeJSONErr(w, http.StatusForbidden, "marking access denied")
		return
	}
	masked := ApplyPropertyMask(*obj, h.propertyMarkingsFor(r.Context(), obj.TypeID, consistency), claims)
	writeJSON(w, http.StatusOK, masked)
}

// ListOutgoingLinks pages outgoing links of the given type from `object_id`.
// Mirrors `LinkStore.ListOutgoing` and is the read-side complement to the
// link writes owned by object-database-service. The link surface enforces
// the same tenant-scope + consistency-header contract as object reads.
func (h *Handlers) ListOutgoingLinks(w http.ResponseWriter, r *http.Request) {
	h.listLinks(w, r, true)
}

// ListIncomingLinks pages incoming links of the given type into `object_id`.
func (h *Handlers) ListIncomingLinks(w http.ResponseWriter, r *http.Request) {
	h.listLinks(w, r, false)
}

func (h *Handlers) listLinks(w http.ResponseWriter, r *http.Request, outgoing bool) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if h.state.Links == nil {
		writeJSONErr(w, http.StatusInternalServerError, "link store not configured")
		return
	}
	tenant, ok := tenantParam(w, r)
	if !ok {
		return
	}
	objectID, ok := objectIDParam(w, r, "object_id")
	if !ok {
		return
	}
	linkType := strings.TrimSpace(chi.URLParam(r, "link_type"))
	if linkType == "" {
		writeJSONErr(w, http.StatusBadRequest, "link_type required")
		return
	}
	if !canReadTenant(claims, tenant) {
		writeJSONErr(w, http.StatusForbidden, "tenant access denied")
		return
	}
	consistency, ok := consistencyHint(w, r)
	if !ok {
		return
	}
	page, ok := pageParams(w, r)
	if !ok {
		return
	}

	var (
		res repos.PagedResult[repos.Link]
		err error
	)
	if outgoing {
		res, err = h.state.Links.ListOutgoing(r.Context(), repos.TenantId(tenant), repos.LinkTypeId(linkType), repos.ObjectId(objectID), page, consistency)
	} else {
		res, err = h.state.Links.ListIncoming(r.Context(), repos.TenantId(tenant), repos.LinkTypeId(linkType), repos.ObjectId(objectID), page, consistency)
	}
	if err != nil {
		repoErrorToResponse(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ListResponse[repos.Link]{Items: res.Items, NextToken: res.NextToken})
}

func (h *Handlers) ListObjectsByType(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if h.state.Objects == nil {
		writeJSONErr(w, http.StatusInternalServerError, "object store not configured")
		return
	}
	tenant, ok := tenantParam(w, r)
	if !ok {
		return
	}
	typeID := strings.TrimSpace(chi.URLParam(r, "type_id"))
	if typeID == "" {
		writeJSONErr(w, http.StatusBadRequest, "tenant and type_id required")
		return
	}
	if !canReadTenant(claims, tenant) {
		writeJSONErr(w, http.StatusForbidden, "tenant access denied")
		return
	}
	consistency, ok := consistencyHint(w, r)
	if !ok {
		return
	}
	page, ok := pageParams(w, r)
	if !ok {
		return
	}

	res, err := h.state.Objects.ListByType(r.Context(), repos.TenantId(tenant), repos.TypeId(typeID), page, consistency)
	if err != nil {
		repoErrorToResponse(w, err)
		return
	}
	schema := h.propertyMarkingsFor(r.Context(), repos.TypeId(typeID), consistency)
	items := make([]repos.Object, 0, len(res.Items))
	for _, obj := range res.Items {
		if canReadMarkings(claims, obj.Markings) {
			items = append(items, ApplyPropertyMask(obj, schema, claims))
		}
	}
	writeJSON(w, http.StatusOK, ListResponse[repos.Object]{Items: items, NextToken: res.NextToken})
}

func tenantParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	tenant := strings.TrimSpace(chi.URLParam(r, "tenant"))
	if tenant == "" {
		writeJSONErr(w, http.StatusBadRequest, "tenant and object_id required")
		return "", false
	}
	if _, err := uuid.Parse(tenant); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "tenant is not a valid UUID")
		return "", false
	}
	return tenant, true
}

func objectIDParam(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	id := strings.TrimSpace(chi.URLParam(r, name))
	if id == "" {
		writeJSONErr(w, http.StatusBadRequest, "tenant and object_id required")
		return "", false
	}
	if _, err := uuid.Parse(id); err != nil {
		writeJSONErr(w, http.StatusBadRequest, name+" is not a valid UUID")
		return "", false
	}
	return id, true
}

func consistencyHint(w http.ResponseWriter, r *http.Request) (repos.ReadConsistency, bool) {
	raw := r.Header.Get(consistencyHeader)
	if raw == "" {
		return repos.Strong(), true
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "strong":
		return repos.Strong(), true
	case "eventual":
		return repos.Eventual(), true
	default:
		writeJSONErr(w, http.StatusBadRequest, consistencyHeader+" must be `strong` or `eventual`, got `"+strings.ToLower(strings.TrimSpace(raw))+"`")
		return repos.ReadConsistency{}, false
	}
}

func pageParams(w http.ResponseWriter, r *http.Request) (repos.Page, bool) {
	page := repos.Page{Size: 100}
	q := r.URL.Query()
	if raw := strings.TrimSpace(q.Get("size")); raw != "" {
		n, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			writeJSONErr(w, http.StatusBadRequest, "size must be an unsigned integer")
			return repos.Page{}, false
		}
		page.Size = uint32(n)
	}
	if token := q.Get("token"); token != "" {
		page.Token = &token
	}
	return page, true
}

func repoErrorToResponse(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if repos.IsNotFound(err) {
		status = http.StatusNotFound
	} else if repos.IsInvalidArgument(err) || repos.IsTenantScope(err) {
		status = http.StatusBadRequest
	}
	writeJSONErr(w, status, err.Error())
}

func canReadTenant(claims *authmw.Claims, tenant string) bool {
	if claims.HasRole("admin") || claims.HasPermissionKey("rows:all") || claims.HasPermissionKey("ontology:read_all") {
		return true
	}
	if claims.OrgID == nil {
		return true
	}
	return claims.OrgID.String() == tenant
}

// propertyMarkingsFor returns the per-property marking requirements
// for `typeID`'s latest schema, or nil when the SchemaStore is not
// wired or the lookup fails. Schema lookup errors are deliberately
// swallowed: callers still get the object, just without per-property
// redaction, which preserves the previous behaviour for backends that
// have not yet populated marking metadata.
func (h *Handlers) propertyMarkingsFor(ctx context.Context, typeID repos.TypeId, consistency repos.ReadConsistency) []PropertyMarkings {
	if h.state.Schemas == nil {
		return nil
	}
	schema, err := h.state.Schemas.GetLatest(ctx, typeID, consistency)
	if err != nil || schema == nil {
		return nil
	}
	return PropertyMarkingsFromSchema(schema.JsonSchema)
}

func canReadMarkings(claims *authmw.Claims, markings []repos.MarkingId) bool {
	if len(markings) == 0 {
		return true
	}
	if claims == nil {
		return false
	}
	if !claims.HasActiveMarkingScope() &&
		(claims.HasRole("admin") || claims.HasPermissionKey("rows:all") || claims.HasPermissionKey("ontology:read_all")) {
		return true
	}
	required := make([]string, 0, len(markings))
	for _, marking := range markings {
		required = append(required, string(marking))
	}
	return claims.AllowsAllMarkings(required)
}
