// Bridge handlers that adapt the gateway-fronted ontology paths
// (`/api/v1/ontology/types/{type_id}/objects[/...]`) onto the canonical
// ObjectStore shapes the service already implements. The gateway routes
// these prefixes here without rewriting the URL, so they need a dedicated
// adapter — apps/web's lib/api/ontology.ts is the canonical wire shape.
//
// Wire mapping
//   - frontend tenant ← `default` (single-tenant PoC; org_id from header
//     `x-of-tenant` overrides when present, so the gateway can inject it).
//   - object_type_id  ← storage.TypeId
//   - properties      ← payload (json-opaque)
//   - created_at      ← created_at_ms (RFC3339)
//   - updated_at      ← updated_at_ms
//   - created_by      ← `system` (PoC; real impl pulls from owner)
package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gocql/gocql"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/restrictedview"
	servicecedar "github.com/openfoundry/openfoundry-go/services/object-database-service/internal/cedarauthz"
	"github.com/openfoundry/openfoundry-go/services/object-database-service/internal/storage"
)

const defaultTenant = "default"

// ontologyObject is the wire shape the SPA's ObjectInstance type expects.
type ontologyObject struct {
	ID             string         `json:"id"`
	ObjectTypeID   string         `json:"object_type_id"`
	Properties     map[string]any `json:"properties"`
	CreatedBy      string         `json:"created_by"`
	OrganizationID *string        `json:"organization_id,omitempty"`
	Marking        *string        `json:"marking,omitempty"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
}

func tenantFromRequest(r *http.Request) storage.TenantId {
	if t := strings.TrimSpace(r.Header.Get("x-of-tenant")); t != "" {
		return storage.TenantId(t)
	}
	return storage.TenantId(defaultTenant)
}

func toOntologyObject(obj *storage.Object) ontologyObject {
	props := map[string]any{}
	if len(obj.Payload) > 0 {
		_ = json.Unmarshal(obj.Payload, &props)
	}
	createdAt := time.UnixMilli(obj.UpdatedAtMs).UTC().Format(time.RFC3339Nano)
	if obj.CreatedAtMs != nil {
		createdAt = time.UnixMilli(*obj.CreatedAtMs).UTC().Format(time.RFC3339Nano)
	}
	out := ontologyObject{
		ID:             string(obj.ID),
		ObjectTypeID:   string(obj.TypeID),
		Properties:     props,
		CreatedBy:      "system",
		OrganizationID: obj.OrganizationID,
		CreatedAt:      createdAt,
		UpdatedAt:      time.UnixMilli(obj.UpdatedAtMs).UTC().Format(time.RFC3339Nano),
	}
	if len(obj.Markings) > 0 {
		m := string(obj.Markings[0])
		out.Marking = &m
	}
	return out
}

func restrictedObjectPolicyFromRequest(r *http.Request, body *queryRequest) (restrictedview.Policy, bool) {
	policy, ok := restrictedview.PolicyFromHeaders(r.Header.Get)
	if raw := strings.TrimSpace(r.URL.Query().Get("restricted_view_policy")); raw != "" {
		policy.Policy = json.RawMessage(raw)
		ok = true
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("restricted_view_id")); raw != "" {
		policy.ID = raw
		ok = true
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("hidden_columns")); raw != "" {
		policy.HiddenColumns = splitQueryCSV(raw)
		ok = true
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("marking_columns")); raw != "" {
		policy.MarkingColumns = splitQueryCSV(raw)
		ok = true
	}
	if body != nil {
		if body.RestrictedViewID != "" {
			policy.ID = body.RestrictedViewID
			ok = true
		}
		if len(body.RestrictedViewPolicy) > 0 {
			policy.Policy = body.RestrictedViewPolicy
			ok = true
		}
		if len(body.HiddenColumns) > 0 {
			policy.HiddenColumns = body.HiddenColumns
			ok = true
		}
		if len(body.MarkingColumns) > 0 {
			policy.MarkingColumns = body.MarkingColumns
			ok = true
		}
	}
	return policy, ok
}

func filterOntologyObjectsForRestrictedView(r *http.Request, policy restrictedview.Policy, items []ontologyObject) ([]ontologyObject, restrictedview.Decision) {
	claims, _ := authmw.FromContext(r.Context())
	out := make([]ontologyObject, 0, len(items))
	aggregate := restrictedview.Decision{
		Allowed:                          true,
		HiddenColumns:                    policy.HiddenColumns,
		MatchedRestrictedViewIDs:         []string{},
		HistoricalIdentitySnapshotCaveat: restrictedview.HistoricalIdentitySnapshotCaveat,
	}
	if policy.ID != "" {
		aggregate.MatchedRestrictedViewIDs = []string{policy.ID}
	}
	for _, item := range items {
		decision := restrictedview.EvaluateRow(claims, policy, ontologyObjectPolicyRow(item))
		aggregate.RequiresRuntimeEvaluation = aggregate.RequiresRuntimeEvaluation || decision.RequiresRuntimeEvaluation
		aggregate.MatchedRules = append(aggregate.MatchedRules, decision.MatchedRules...)
		if !decision.Allowed {
			aggregate.DenyReasons = append(aggregate.DenyReasons, decision.DenyReasons...)
			continue
		}
		out = append(out, redactOntologyObject(item, policy.HiddenColumns))
	}
	aggregate.MatchedRules = compactUniqueStrings(aggregate.MatchedRules)
	aggregate.DenyReasons = compactUniqueStrings(aggregate.DenyReasons)
	return out, aggregate
}

func ontologyObjectPolicyRow(item ontologyObject) map[string]any {
	row := make(map[string]any, len(item.Properties)+5)
	for key, value := range item.Properties {
		row[key] = value
	}
	row["id"] = item.ID
	row["object_id"] = item.ID
	row["object_type_id"] = item.ObjectTypeID
	if item.OrganizationID != nil {
		row["organization_id"] = *item.OrganizationID
	}
	if item.Marking != nil {
		row["marking"] = *item.Marking
	}
	return row
}

func redactOntologyObject(item ontologyObject, hiddenColumns []string) ontologyObject {
	hidden := map[string]bool{}
	for _, col := range hiddenColumns {
		hidden[strings.ToLower(strings.TrimSpace(col))] = true
	}
	if len(hidden) == 0 {
		return item
	}
	props := make(map[string]any, len(item.Properties))
	for key, value := range item.Properties {
		if hidden[strings.ToLower(key)] {
			props[key] = nil
			continue
		}
		props[key] = value
	}
	item.Properties = props
	if hidden["organization_id"] {
		item.OrganizationID = nil
	}
	if hidden["marking"] || hidden["markings"] {
		item.Marking = nil
	}
	return item
}

func splitQueryCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func compactUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// ListObjectsByOntologyType serves GET /api/v1/ontology/types/{type_id}/objects.
// Pagination on the SPA side is page+per_page; we map per_page → storage.Page.Size.
//
// `total` is the underlying cardinality, computed via a separate unbounded list
// against the same tenant+type. This is O(N) — fine for PoC scale (10⁴) and
// the in-memory store. For Cassandra at 10⁶+ rows, swap to a denormalised
// counter (see NEXT-STEPS.md §4.1).
func (h *Handlers) ListObjectsByOntologyType(w http.ResponseWriter, r *http.Request) {
	tenant := tenantFromRequest(r)
	typeID := storage.TypeId(chi.URLParam(r, "type_id"))
	if ok, err := runCedarGate(h, r, servicecedar.ActionRead(), string(typeID), nil); !ok {
		writeCedarError(w, err)
		return
	}

	q := r.URL.Query()
	perPage := uint32(25)
	if v := q.Get("per_page"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil && n > 0 {
			if n > 5000 {
				n = 5000
			}
			perPage = uint32(n)
		}
	}
	page := 1
	if v := q.Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	consistency := parseConsistency(q.Get("consistency"))

	if policy, ok, err := restrictedObjectPolicyForType(h, r, typeID, nil); err != nil {
		writeError(w, err)
		return
	} else if ok {
		full, err := h.Objects.ListByType(r.Context(), tenant, typeID, storage.Page{Size: 1_000_000}, consistency)
		if err != nil {
			writeError(w, err)
			return
		}
		allItems := make([]ontologyObject, 0, len(full.Items))
		for i := range full.Items {
			allItems = append(allItems, toOntologyObject(&full.Items[i]))
		}
		filtered, decision := filterOntologyObjectsForRestrictedView(r, policy, allItems)
		filtered, omittedMarkingCount := filterOntologyObjectsForMarkings(r, filtered)
		total := len(filtered)
		start := (page - 1) * int(perPage)
		end := start + int(perPage)
		if start > total {
			start = total
		}
		if end > total {
			end = total
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"data":                       filtered[start:end],
			"total":                      total,
			"page":                       page,
			"per_page":                   perPage,
			"restricted_view_evaluation": decision,
			"omitted_marking_count":      omittedMarkingCount,
		})
		return
	}

	res, err := h.Objects.ListByType(r.Context(), tenant, typeID, storage.Page{Size: perPage}, consistency)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]ontologyObject, 0, len(res.Items))
	for i := range res.Items {
		items = append(items, toOntologyObject(&res.Items[i]))
	}
	items, omittedMarkingCount := filterOntologyObjectsForMarkings(r, items)

	total := len(items)
	if perPage < 5000 || res.NextToken != nil {
		// Page is potentially a slice of a larger set; ask for the full list
		// to materialise the real total. Skip when caller already pulled the
		// whole set in one shot (per_page>=5000 and no continuation).
		full, err := h.Objects.ListByType(r.Context(), tenant, typeID, storage.Page{Size: 1_000_000}, consistency)
		if err == nil {
			fullItems := make([]ontologyObject, 0, len(full.Items))
			for i := range full.Items {
				fullItems = append(fullItems, toOntologyObject(&full.Items[i]))
			}
			visible, omitted := filterOntologyObjectsForMarkings(r, fullItems)
			total = len(visible)
			omittedMarkingCount = omitted
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":                  items,
		"total":                 total,
		"page":                  page,
		"per_page":              perPage,
		"omitted_marking_count": omittedMarkingCount,
	})
}

// GetObjectByOntologyType serves GET /api/v1/ontology/types/{type_id}/objects/{object_id}.
func (h *Handlers) GetObjectByOntologyType(w http.ResponseWriter, r *http.Request) {
	tenant := tenantFromRequest(r)
	typeID := chi.URLParam(r, "type_id")
	objID := storage.ObjectId(chi.URLParam(r, "object_id"))
	if ok, err := runCedarGate(h, r, servicecedar.ActionRead(), typeID, nil); !ok {
		writeCedarError(w, err)
		return
	}
	obj, err := h.getObjectByTypePrimaryKey(r, tenant, storage.TypeId(typeID), string(objID))
	if err != nil {
		writeError(w, err)
		return
	}
	if obj == nil || !callerCanReadStorageObject(r, obj) {
		http.NotFound(w, r)
		return
	}
	out := toOntologyObject(obj)
	if policy, ok, err := restrictedObjectPolicyForType(h, r, storage.TypeId(typeID), nil); err != nil {
		writeError(w, err)
		return
	} else if ok {
		filtered, decision := filterOntologyObjectsForRestrictedView(r, policy, []ontologyObject{out})
		if len(filtered) == 0 {
			http.Error(w, strings.Join(decision.DenyReasons, "; "), http.StatusNotFound)
			return
		}
		out = filtered[0]
	}
	if !callerCanReadOntologyObject(r, out) {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// UpdateObjectByOntologyType serves PATCH /api/v1/ontology/types/{type_id}/objects/{object_id}.
// Body shape: `{ properties: {...}, replace?: bool }`. The default behavior
// merges the provided properties into the existing payload, matching the SPA's
// inline-action update flow.
func (h *Handlers) UpdateObjectByOntologyType(w http.ResponseWriter, r *http.Request) {
	tenant := tenantFromRequest(r)
	typeID := storage.TypeId(chi.URLParam(r, "type_id"))
	objID := storage.ObjectId(chi.URLParam(r, "object_id"))
	if ok, err := runCedarGate(h, r, servicecedar.ActionWrite(), string(typeID), nil); !ok {
		writeCedarError(w, err)
		return
	}

	var body struct {
		Properties map[string]any `json:"properties"`
		Replace    bool           `json:"replace"`
		Marking    *string        `json:"marking,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	existing, err := h.getObjectByTypePrimaryKey(r, tenant, typeID, string(objID))
	if err != nil {
		writeError(w, err)
		return
	}
	if existing == nil || existing.TypeID != typeID {
		http.NotFound(w, r)
		return
	}

	props := map[string]any{}
	if !body.Replace && len(existing.Payload) > 0 {
		_ = json.Unmarshal(existing.Payload, &props)
	}
	for k, v := range body.Properties {
		props[k] = v
	}
	payload, err := validateProperties(r.Context(), h.Schemas, string(typeID), props)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	next := *existing
	next.Payload = payload
	next.UpdatedAtMs = time.Now().UnixMilli()
	if body.Marking != nil && strings.TrimSpace(*body.Marking) != "" {
		next.Markings = []storage.MarkingId{storage.MarkingId(strings.TrimSpace(*body.Marking))}
	}
	expected := existing.Version
	outcome, err := h.Objects.Put(r.Context(), next, &expected)
	if err != nil {
		writeError(w, err)
		return
	}
	if outcome.Kind == storage.PutVersionConflict {
		writeOutcomeResponse(w, outcome)
		return
	}
	h.bustObjectCache(tenant, typeID, string(objID))
	if outcome.NewVersion > 0 {
		next.Version = outcome.NewVersion
	}
	writeJSON(w, http.StatusOK, toOntologyObject(&next))
}

// DeleteObjectByOntologyType serves DELETE /api/v1/ontology/types/{type_id}/objects/{object_id}.
// Matches the SPA's `deleteObject(typeId, objectId)` contract.
//
// Cascade: after the object row is removed we sweep every incident
// link via the optional [storage.IncidentLinkDeleter] hook. The
// in-memory store implements it synchronously; the Cassandra adapter
// no-ops and delegates to the indexer (ADR-0020 §S1.7). Failures on
// the cascade are logged via the response header rather than
// short-circuiting the request — the object is already gone and the
// link sweep is asynchronous in production.
func (h *Handlers) DeleteObjectByOntologyType(w http.ResponseWriter, r *http.Request) {
	tenant := tenantFromRequest(r)
	typeID := chi.URLParam(r, "type_id")
	objID := storage.ObjectId(chi.URLParam(r, "object_id"))
	if ok, err := runCedarGate(h, r, servicecedar.ActionDelete(), typeID, nil); !ok {
		writeCedarError(w, err)
		return
	}
	deleted, err := h.Objects.Delete(r.Context(), tenant, objID)
	if err != nil {
		writeError(w, err)
		return
	}
	if !deleted {
		http.NotFound(w, r)
		return
	}
	h.bustObjectCache(tenant, storage.TypeId(typeID), string(objID))
	if cascader, ok := h.Links.(storage.IncidentLinkDeleter); ok {
		if n, cerr := cascader.DeleteIncident(r.Context(), tenant, objID); cerr == nil {
			w.Header().Set("x-of-cascaded-links", strconv.Itoa(n))
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// queryFilter mirrors the WorkshopVariable.static_filter shape from
// apps/web/src/routes/apps/WorkshopEditorPage.tsx — same operators, same
// JSON keys, so the SPA can forward filters verbatim.
type queryFilter struct {
	PropertyName string `json:"property_name"`
	Operator     string `json:"operator,omitempty"` // equals | contains | gte | lte | gt | lt | in
	Value        any    `json:"value"`
}

type queryPredicate struct {
	Op           string           `json:"op,omitempty"`
	Operator     string           `json:"operator,omitempty"`
	PropertyName string           `json:"property_name,omitempty"`
	Value        any              `json:"value,omitempty"`
	Children     []queryPredicate `json:"children,omitempty"`
	And          []queryPredicate `json:"and,omitempty"`
	Or           []queryPredicate `json:"or,omitempty"`
	Not          *queryPredicate  `json:"not,omitempty"`
}

type querySort struct {
	PropertyName string `json:"property_name"`
	Direction    string `json:"direction,omitempty"`
}

type queryAggregation struct {
	ID           string `json:"id,omitempty"`
	Alias        string `json:"alias,omitempty"`
	Function     string `json:"function"`
	PropertyName string `json:"property_name,omitempty"`
}

type queryAggregationResult struct {
	ID           string `json:"id"`
	Alias        string `json:"alias,omitempty"`
	Function     string `json:"function"`
	PropertyName string `json:"property_name,omitempty"`
	Value        any    `json:"value"`
	Count        int    `json:"count"`
}

type querySearchAround struct {
	SourceObjectIDs    []string `json:"source_object_ids"`
	LinkTypeID         string   `json:"link_type_id,omitempty"`
	LinkTypeIDs        []string `json:"link_type_ids"`
	Direction          string   `json:"direction,omitempty"`
	Depth              int      `json:"depth,omitempty"`
	TargetObjectTypeID string   `json:"target_object_type_id,omitempty"`
}

type linkedEdgeResponse struct {
	LinkID         string         `json:"link_id"`
	LinkTypeID     string         `json:"link_type_id"`
	SourceObjectID string         `json:"source_object_id"`
	TargetObjectID string         `json:"target_object_id"`
	Direction      string         `json:"direction"`
	Depth          int            `json:"depth"`
	Properties     map[string]any `json:"properties,omitempty"`
}

type queryKNN struct {
	PropertyName      string    `json:"property_name,omitempty"`
	Property          string    `json:"property,omitempty"`
	VectorProperty    string    `json:"vector_property,omitempty"`
	EmbeddingProperty string    `json:"embedding_property,omitempty"`
	Vector            []float64 `json:"vector,omitempty"`
	QueryVector       []float64 `json:"query_vector,omitempty"`
	K                 int       `json:"k,omitempty"`
	KValue            int       `json:"k_value,omitempty"`
	KValueCamel       int       `json:"kValue,omitempty"`
	Metric            string    `json:"metric,omitempty"`
}

type queryKNNResult struct {
	ObjectID     string  `json:"object_id"`
	Rank         int     `json:"rank"`
	Score        float64 `json:"score"`
	Distance     float64 `json:"distance"`
	Metric       string  `json:"metric"`
	PropertyName string  `json:"property_name"`
}

type queryRequest struct {
	// Filters is the WorkshopVariable.static_filter[s] shape — the richer
	// form with `operator` per filter.
	Filters   []queryFilter   `json:"filters"`
	Predicate *queryPredicate `json:"predicate,omitempty"`
	// Equals is the existing SPA shape used by lib/api/ontology.ts:queryObjects
	// — a flat map { property: expected }. Treated as "equals" filters.
	Equals  map[string]any `json:"equals"`
	Page    int            `json:"page"`
	PerPage uint32         `json:"per_page"`
	// Limit mirrors the SPA's `queryObjects` body: cap on items when no
	// per_page is set. We unify with per_page below.
	Limit                int                `json:"limit"`
	Sort                 []querySort        `json:"sort"`
	IncludeCount         bool               `json:"include_count"`
	Aggregations         []queryAggregation `json:"aggregations"`
	SelectedObjectIDs    []string           `json:"selected_object_ids"`
	SearchAround         *querySearchAround `json:"search_around,omitempty"`
	KNN                  *queryKNN          `json:"knn,omitempty"`
	RestrictedViewID     string             `json:"restricted_view_id,omitempty"`
	RestrictedViewPolicy json.RawMessage    `json:"restricted_view_policy,omitempty"`
	HiddenColumns        []string           `json:"hidden_columns,omitempty"`
	MarkingColumns       []string           `json:"marking_columns,omitempty"`
	Explain              string             `json:"explain,omitempty"`
	Analyze              bool               `json:"analyze,omitempty"`
	MaxStalenessMs       int64              `json:"max_staleness_ms,omitempty"`
}

type objectQueryExecution struct {
	plan             storage.QueryPlan
	actuals          storage.QueryActuals
	budget           *storage.QueryBudget
	materialized     []storage.MaterializedAggregateResult
	indexable        bool
	indexName        string
	restrictedFilter []string
}

func matchesFilter(props map[string]any, f queryFilter) bool {
	actual, ok := props[f.PropertyName]
	if !ok {
		// "not present" matches "" target so the SPA's `equals ""` checks work.
		actualStr := ""
		expectedStr := strings.ToLower(strings.TrimSpace(toStringValue(f.Value)))
		switch strings.ToLower(strings.TrimSpace(f.Operator)) {
		case "is_empty":
			return true
		case "is_not_empty":
			return false
		case "not_equals", "neq", "!=":
			return !strings.EqualFold(actualStr, expectedStr)
		default:
			return strings.EqualFold(actualStr, expectedStr)
		}
	}
	actualStr := strings.ToLower(strings.TrimSpace(toStringValue(actual)))
	expectedStr := strings.ToLower(strings.TrimSpace(toStringValue(f.Value)))
	switch strings.ToLower(strings.TrimSpace(f.Operator)) {
	case "contains":
		return strings.Contains(actualStr, expectedStr)
	case "starts_with", "prefix":
		return strings.HasPrefix(actualStr, expectedStr)
	case "not_equals", "neq", "!=":
		return actualStr != expectedStr
	case "gte", ">=":
		return compareFilterValues(actual, f.Value) >= 0
	case "lte", "<=":
		return compareFilterValues(actual, f.Value) <= 0
	case "gt", ">":
		return compareFilterValues(actual, f.Value) > 0
	case "lt", "<":
		return compareFilterValues(actual, f.Value) < 0
	case "in":
		return filterValueContains(f.Value, actual)
	case "is_empty":
		return strings.TrimSpace(toStringValue(actual)) == ""
	case "is_not_empty":
		return strings.TrimSpace(toStringValue(actual)) != ""
	default: // "equals" + unknown
		return actualStr == expectedStr
	}
}

func matchesPredicate(props map[string]any, predicate queryPredicate) bool {
	op := strings.ToLower(strings.TrimSpace(predicate.Op))
	if op == "" {
		op = strings.ToLower(strings.TrimSpace(predicate.Operator))
	}
	if len(predicate.And) > 0 {
		op = "and"
		predicate.Children = append(predicate.Children, predicate.And...)
	}
	if len(predicate.Or) > 0 {
		op = "or"
		predicate.Children = append(predicate.Children, predicate.Or...)
	}
	if predicate.Not != nil {
		op = "not"
	}
	switch op {
	case "and", "":
		if len(predicate.Children) == 0 && predicate.PropertyName != "" {
			return matchesFilter(props, queryFilter{PropertyName: predicate.PropertyName, Operator: predicate.Operator, Value: predicate.Value})
		}
		for _, child := range predicate.Children {
			if !matchesPredicate(props, child) {
				return false
			}
		}
		return true
	case "or":
		for _, child := range predicate.Children {
			if matchesPredicate(props, child) {
				return true
			}
		}
		return false
	case "not":
		if predicate.Not == nil {
			return true
		}
		return !matchesPredicate(props, *predicate.Not)
	case "eq", "equals", "=", "neq", "not_equals", "!=", "gt", ">", "gte", ">=", "lt", "<", "lte", "<=", "in", "contains", "starts_with", "prefix", "is_empty", "is_not_empty":
		return matchesFilter(props, queryFilter{PropertyName: predicate.PropertyName, Operator: op, Value: predicate.Value})
	default:
		if predicate.PropertyName != "" {
			return matchesFilter(props, queryFilter{PropertyName: predicate.PropertyName, Operator: op, Value: predicate.Value})
		}
		return true
	}
}

func compareFilterValues(actual any, expected any) int {
	actualNumber, actualOK := numericFilterValue(actual)
	expectedNumber, expectedOK := numericFilterValue(expected)
	if actualOK && expectedOK {
		switch {
		case actualNumber < expectedNumber:
			return -1
		case actualNumber > expectedNumber:
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(
		strings.ToLower(strings.TrimSpace(toStringValue(actual))),
		strings.ToLower(strings.TrimSpace(toStringValue(expected))),
	)
}

func numericFilterValue(v any) (float64, bool) {
	switch t := v.(type) {
	case json.Number:
		n, err := t.Float64()
		return n, err == nil
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	case uint:
		return float64(t), true
	case uint64:
		return float64(t), true
	case uint32:
		return float64(t), true
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func filterValueContains(container any, actual any) bool {
	actualStr := strings.ToLower(strings.TrimSpace(toStringValue(actual)))
	switch values := container.(type) {
	case []any:
		for _, value := range values {
			if strings.ToLower(strings.TrimSpace(toStringValue(value))) == actualStr {
				return true
			}
		}
	case []string:
		for _, value := range values {
			if strings.ToLower(strings.TrimSpace(value)) == actualStr {
				return true
			}
		}
	default:
		return strings.ToLower(strings.TrimSpace(toStringValue(container))) == actualStr
	}
	return false
}

func toStringValue(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case json.Number:
		return t.String()
	case float64:
		// avoid scientific notation for integral floats
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	default:
		// fall back to JSON encoding — covers nested objects and arrays
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func sortOntologyObjects(items []ontologyObject, sorts []querySort) {
	sorts = compactQuerySorts(sorts)
	if len(sorts) == 0 {
		return
	}
	sort.SliceStable(items, func(i, j int) bool {
		for _, item := range sorts {
			cmp := compareFilterValues(items[i].Properties[item.PropertyName], items[j].Properties[item.PropertyName])
			if cmp == 0 {
				continue
			}
			if isQuerySortDescending(item.Direction) {
				return cmp > 0
			}
			return cmp < 0
		}
		return strings.Compare(items[i].ID, items[j].ID) < 0
	})
}

func compactQuerySorts(sorts []querySort) []querySort {
	out := make([]querySort, 0, len(sorts))
	for _, item := range sorts {
		propertyName := strings.TrimSpace(item.PropertyName)
		if propertyName == "" {
			continue
		}
		direction := "asc"
		if isQuerySortDescending(item.Direction) {
			direction = "desc"
		}
		out = append(out, querySort{PropertyName: propertyName, Direction: direction})
	}
	return out
}

func isQuerySortDescending(direction string) bool {
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "desc", "descending", "-1":
		return true
	default:
		return false
	}
}

func computeObjectQueryAggregations(items []ontologyObject, specs []queryAggregation) []queryAggregationResult {
	out := make([]queryAggregationResult, 0, len(specs))
	for _, spec := range specs {
		fn := normalizeQueryAggregationFunction(spec.Function)
		if fn == "" {
			continue
		}
		propertyName := strings.TrimSpace(spec.PropertyName)
		id := strings.TrimSpace(spec.ID)
		if id == "" {
			id = strings.TrimSpace(spec.Alias)
		}
		if id == "" && propertyName != "" {
			id = fn + ":" + propertyName
		}
		if id == "" {
			id = fn
		}

		result := queryAggregationResult{
			ID:           id,
			Alias:        strings.TrimSpace(spec.Alias),
			Function:     fn,
			PropertyName: propertyName,
		}
		switch fn {
		case "count":
			count := len(items)
			if propertyName != "" {
				count = 0
				for _, item := range items {
					if !isQueryEmptyValue(item.Properties[propertyName]) {
						count++
					}
				}
			}
			result.Value = count
			result.Count = count
		case "distinct_count", "approx_distinct":
			seen := map[string]struct{}{}
			for _, item := range items {
				value := item.ID
				if propertyName != "" {
					value = toStringValue(item.Properties[propertyName])
				}
				value = strings.ToLower(strings.TrimSpace(value))
				if value == "" {
					continue
				}
				seen[value] = struct{}{}
			}
			result.Value = len(seen)
			result.Count = len(seen)
		default:
			numbers := make([]float64, 0, len(items))
			for _, item := range items {
				if n, ok := numericFilterValue(item.Properties[propertyName]); ok {
					numbers = append(numbers, n)
				}
			}
			result.Count = len(numbers)
			if len(numbers) == 0 {
				result.Value = nil
				break
			}
			total := 0.0
			min := numbers[0]
			max := numbers[0]
			for _, n := range numbers {
				total += n
				if n < min {
					min = n
				}
				if n > max {
					max = n
				}
			}
			switch fn {
			case "sum":
				result.Value = total
			case "avg":
				result.Value = total / float64(len(numbers))
			case "min":
				result.Value = min
			case "max":
				result.Value = max
			default:
				continue
			}
		}
		out = append(out, result)
	}
	return out
}

func normalizeQueryAggregationFunction(fn string) string {
	switch strings.ToLower(strings.TrimSpace(fn)) {
	case "count":
		return "count"
	case "sum":
		return "sum"
	case "avg", "average", "mean":
		return "avg"
	case "min":
		return "min"
	case "max":
		return "max"
	case "distinct", "unique", "count_distinct", "distinct_count":
		return "distinct_count"
	case "approx_distinct":
		return "approx_distinct"
	default:
		return strings.ToLower(strings.TrimSpace(fn))
	}
}

func isQueryEmptyValue(value any) bool {
	return strings.TrimSpace(toStringValue(value)) == ""
}

func applyObjectKNN(items []ontologyObject, config queryKNN, fallbackK int) ([]ontologyObject, []queryKNNResult, map[string]any, error) {
	propertyName := normalizedKNNPropertyName(config)
	if propertyName == "" {
		return nil, nil, nil, &storage.RepoError{Kind: storage.ErrInvalidArgument, Msg: "knn requires a vector property_name"}
	}
	queryVector := normalizedKNNVector(config)
	if len(queryVector) == 0 {
		return nil, nil, nil, &storage.RepoError{Kind: storage.ErrInvalidArgument, Msg: "knn requires a non-empty vector"}
	}
	if len(queryVector) > 2048 {
		return nil, nil, nil, &storage.RepoError{Kind: storage.ErrInvalidArgument, Msg: "knn vector dimension cannot exceed 2048"}
	}
	k, err := normalizedKNNK(config, fallbackK)
	if err != nil {
		return nil, nil, nil, err
	}
	metric := normalizeKNNMetric(config.Metric)
	type candidate struct {
		item     ontologyObject
		score    float64
		distance float64
	}
	candidates := []candidate{}
	for _, item := range items {
		vector, ok := numericVectorValue(item.Properties[propertyName])
		if !ok || len(vector) != len(queryVector) {
			continue
		}
		score, distance, ok := scoreKNNVectors(queryVector, vector, metric)
		if !ok {
			continue
		}
		candidates = append(candidates, candidate{item: item, score: score, distance: distance})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return strings.Compare(candidates[i].item.ID, candidates[j].item.ID) < 0
	})
	if len(candidates) > k {
		candidates = candidates[:k]
	}
	out := make([]ontologyObject, 0, len(candidates))
	results := make([]queryKNNResult, 0, len(candidates))
	for idx, candidate := range candidates {
		item := candidate.item
		if item.Properties == nil {
			item.Properties = map[string]any{}
		}
		rank := idx + 1
		item.Properties["__of_knn_rank"] = rank
		item.Properties["__of_knn_score"] = candidate.score
		item.Properties["__of_knn_distance"] = candidate.distance
		item.Properties["__of_knn_metric"] = metric
		out = append(out, item)
		results = append(results, queryKNNResult{
			ObjectID:     item.ID,
			Rank:         rank,
			Score:        candidate.score,
			Distance:     candidate.distance,
			Metric:       metric,
			PropertyName: propertyName,
		})
	}
	return out, results, map[string]any{
		"property_name":           propertyName,
		"k":                       k,
		"metric":                  metric,
		"query_vector_dimension":  len(queryVector),
		"matched_vector_count":    len(candidates),
		"max_supported_dimension": 2048,
	}, nil
}

func normalizedKNNPropertyName(config queryKNN) string {
	for _, value := range []string{config.PropertyName, config.Property, config.VectorProperty, config.EmbeddingProperty} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizedKNNVector(config queryKNN) []float64 {
	if len(config.Vector) > 0 {
		return config.Vector
	}
	return config.QueryVector
}

func normalizedKNNK(config queryKNN, fallback int) (int, error) {
	k := config.K
	if k == 0 {
		k = config.KValue
	}
	if k == 0 {
		k = config.KValueCamel
	}
	explicit := k != 0
	if k < 0 {
		return 0, &storage.RepoError{Kind: storage.ErrInvalidArgument, Msg: "knn k must be positive"}
	}
	if k == 0 {
		k = fallback
	}
	if k <= 0 {
		k = 10
	}
	if k > 100 {
		if !explicit {
			return 100, nil
		}
		return 0, &storage.RepoError{Kind: storage.ErrInvalidArgument, Msg: "knn k cannot exceed 100"}
	}
	return k, nil
}

func normalizeKNNMetric(metric string) string {
	switch strings.ToLower(strings.TrimSpace(metric)) {
	case "euclidean", "l2", "distance":
		return "euclidean"
	case "dot", "dot_product", "inner_product":
		return "dot"
	default:
		return "cosine"
	}
}

func numericVectorValue(value any) ([]float64, bool) {
	switch typed := value.(type) {
	case []float64:
		return typed, len(typed) > 0
	case []int:
		out := make([]float64, 0, len(typed))
		for _, entry := range typed {
			out = append(out, float64(entry))
		}
		return out, len(out) > 0
	case []any:
		out := make([]float64, 0, len(typed))
		for _, entry := range typed {
			number, ok := numericFilterValue(entry)
			if !ok {
				return nil, false
			}
			out = append(out, number)
		}
		return out, len(out) > 0
	case string:
		var out []float64
		if err := json.Unmarshal([]byte(strings.TrimSpace(typed)), &out); err != nil {
			return nil, false
		}
		return out, len(out) > 0
	default:
		return nil, false
	}
}

func scoreKNNVectors(query []float64, candidate []float64, metric string) (float64, float64, bool) {
	if len(query) == 0 || len(query) != len(candidate) {
		return 0, 0, false
	}
	switch metric {
	case "euclidean":
		total := 0.0
		for i := range query {
			delta := query[i] - candidate[i]
			total += delta * delta
		}
		distance := math.Sqrt(total)
		return 1 / (1 + distance), distance, true
	case "dot":
		score := 0.0
		for i := range query {
			score += query[i] * candidate[i]
		}
		return score, -score, true
	default:
		dot := 0.0
		queryNorm := 0.0
		candidateNorm := 0.0
		for i := range query {
			dot += query[i] * candidate[i]
			queryNorm += query[i] * query[i]
			candidateNorm += candidate[i] * candidate[i]
		}
		if queryNorm == 0 || candidateNorm == 0 {
			return 0, 0, false
		}
		score := dot / (math.Sqrt(queryNorm) * math.Sqrt(candidateNorm))
		return score, 1 - score, true
	}
}

// QueryObjectsByOntologyType serves POST /api/v1/ontology/types/{type_id}/objects/query.
// Server-side filter pushdown for the SPA's WorkshopVariable.static_filter / static_filters.
// Today the InMemory store can't filter natively (no native CQL); we materialise
// the full per-tenant+type list and filter in Go. For Cassandra this should be
// replaced with a secondary-index lookup (`SELECT … WHERE type_id=? AND property_name=value`)
// once the schema supports it.
func (h *Handlers) QueryObjectsByOntologyType(w http.ResponseWriter, r *http.Request) {
	tenant := tenantFromRequest(r)
	typeID := storage.TypeId(chi.URLParam(r, "type_id"))
	if ok, err := runCedarGate(h, r, servicecedar.ActionRead(), string(typeID), nil); !ok {
		writeCedarError(w, err)
		return
	}

	var body queryRequest
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	if err := dec.Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Merge the two filter shapes: `equals` map gets normalised into the
	// richer Filters list as plain equals.
	for k, v := range body.Equals {
		body.Filters = append(body.Filters, queryFilter{PropertyName: k, Operator: "equals", Value: v})
	}
	perPage := body.PerPage
	if perPage == 0 && body.Limit > 0 {
		perPage = uint32(body.Limit)
	}
	if perPage == 0 {
		perPage = 25
	}
	if perPage > 5000 {
		perPage = 5000
	}
	page := body.Page
	if page < 1 {
		page = 1
	}

	consistency := parseConsistency(r.URL.Query().Get("consistency"))
	if body.MaxStalenessMs > 0 {
		// OSV2.24: clients opt in to local replica reads by supplying a bounded
		// staleness hint. The in-process service surface maps that to eventual
		// consistency; production Cassandra routing pins writes to the primary
		// region and only serves reads from eligible replicas.
		consistency = storage.ReadEventual
	}
	policy, hasRestrictedPolicy, err := restrictedObjectPolicyForType(h, r, typeID, &body)
	if err != nil {
		writeError(w, err)
		return
	}
	restrictedIndexFilters := []string{}
	if hasRestrictedPolicy {
		restrictedIndexFilters = restrictedIndexFiltersForQuery(policy.ID, body)
	}
	exec := buildObjectQueryExecution(r, h.Objects, tenant, typeID, body, restrictedIndexFilters)
	callerID, projectID := callerAndProjectFromRequest(r)
	if limiter, ok := h.Objects.(storage.QueryBudgetEnforcer); ok {
		budget, err := limiter.ReserveQueryBudget(r.Context(), tenant, projectID, callerID, exec.plan.EstimatedRows)
		if err != nil {
			w.Header().Set("Retry-After", strconv.FormatInt(budget.RetryAfterMs/1000, 10))
			writeError(w, err)
			return
		}
		exec.budget = &budget
		if budget.SoftWarning {
			w.Header().Set("x-openfoundry-query-budget-warning", "80-percent")
		}
	}
	if mode := explainMode(r, body); mode == "explain" {
		writeJSON(w, http.StatusOK, map[string]any{"explain": exec.plan, "budget": exec.budget})
		return
	}
	startedAt := time.Now()
	full, err := listObjectsForQuery(r, h.Objects, tenant, typeID, body, consistency)
	if err != nil {
		writeError(w, err)
		return
	}

	// `objects_by_type` is append-only: every Put fans out a new row
	// keyed on (type_id, updated_at, object_id). After an in-place
	// property update the table holds both the old and the new
	// summary for the same object_id. Dedupe by keeping the most
	// recently updated row per object_id before applying the filter
	// — otherwise stale `properties_summary` values pollute counts.
	latestByID := map[storage.ObjectId]int{}
	for i := range full.Items {
		obj := &full.Items[i]
		if prev, ok := latestByID[obj.ID]; ok {
			if full.Items[prev].UpdatedAtMs >= obj.UpdatedAtMs {
				continue
			}
		}
		latestByID[obj.ID] = i
	}
	deduped := make([]int, 0, len(latestByID))
	for _, idx := range latestByID {
		deduped = append(deduped, idx)
	}
	selectedIDs := map[string]bool{}
	for _, id := range body.SelectedObjectIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			selectedIDs[trimmed] = true
		}
	}
	linkedEdges := []linkedEdgeResponse{}
	searchAroundContract := map[string]any(nil)
	if body.SearchAround != nil {
		searchIDs, edges, err := h.resolveLinkedSearchAround(r, tenant, typeID, *body.SearchAround)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(selectedIDs) > 0 {
			for id := range selectedIDs {
				if !searchIDs[id] {
					delete(selectedIDs, id)
				}
			}
		} else {
			selectedIDs = searchIDs
		}
		linkedEdges = edges
		searchAroundContract = map[string]any{
			"source_object_ids":     compactStrings(body.SearchAround.SourceObjectIDs),
			"link_type_ids":         normalizedSearchAroundLinkTypeIDs(*body.SearchAround),
			"direction":             normalizeSearchAroundDirection(body.SearchAround.Direction),
			"depth":                 clampSearchAroundDepth(body.SearchAround.Depth),
			"target_object_type_id": string(typeID),
		}
	}

	matched := make([]ontologyObject, 0)
	for _, idx := range deduped {
		obj := &full.Items[idx]
		if len(selectedIDs) > 0 && !selectedIDs[string(obj.ID)] {
			continue
		}
		props := map[string]any{}
		if len(obj.Payload) > 0 {
			_ = json.Unmarshal(obj.Payload, &props)
		}
		ok := true
		for _, f := range body.Filters {
			if !matchesFilter(props, f) {
				ok = false
				break
			}
		}
		if ok && body.Predicate != nil {
			ok = matchesPredicate(props, *body.Predicate)
		}
		if ok {
			matched = append(matched, toOntologyObject(obj))
		}
	}

	var restrictedDecision *restrictedview.Decision
	if hasRestrictedPolicy {
		filtered, decision := filterOntologyObjectsForRestrictedView(r, policy, matched)
		matched = filtered
		restrictedDecision = &decision
	}
	var omittedMarkingCount int
	matched, omittedMarkingCount = filterOntologyObjectsForMarkings(r, matched)

	knnResults := []queryKNNResult(nil)
	knnContract := map[string]any(nil)
	if body.KNN != nil {
		var err error
		matched, knnResults, knnContract, err = applyObjectKNN(matched, *body.KNN, int(perPage))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		sortOntologyObjects(matched, body.Sort)
	}
	aggregations := computeObjectQueryAggregations(matched, body.Aggregations)
	if materialized, ok := h.Objects.(storage.MaterializedAggregateStore); ok {
		for i, spec := range body.Aggregations {
			groupBy := strings.TrimSpace(spec.Alias)
			if strings.HasPrefix(groupBy, "group_by:") {
				groupBy = strings.TrimPrefix(groupBy, "group_by:")
			} else {
				groupBy = ""
			}
			if result, found, err := materialized.ReadMaterializedAggregate(r.Context(), tenant, typeID, normalizeQueryAggregationFunction(spec.Function), spec.PropertyName, groupBy); err == nil && found {
				exec.materialized = append(exec.materialized, result)
				if i < len(aggregations) {
					aggregations[i].Value = result.Value
					aggregations[i].Count = int(result.Count)
				}
			}
		}
	}
	total := len(matched)
	start := (page - 1) * int(perPage)
	end := start + int(perPage)
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	pageItems := matched[start:end]
	pageIDs := map[string]bool{}
	for _, item := range pageItems {
		pageIDs[item.ID] = true
	}
	pageLinkedEdges := linkedEdges[:0]
	for _, edge := range linkedEdges {
		if pageIDs[edge.SourceObjectID] || pageIDs[edge.TargetObjectID] {
			pageLinkedEdges = append(pageLinkedEdges, edge)
		}
	}
	pageKNNResults := knnResults[:0]
	for _, result := range knnResults {
		if pageIDs[result.ObjectID] {
			pageKNNResults = append(pageKNNResults, result)
		}
	}
	wall := time.Since(startedAt)
	indicesHit := []string{}
	if exec.indexName != "" {
		indicesHit = append(indicesHit, exec.indexName)
	}
	exec.actuals = storage.QueryActuals{RowsScanned: uint64(len(full.Items)), IndicesHit: indicesHit, RowsReturned: uint64(len(pageItems)), WallTime: wall, WallTimeMs: float64(wall.Microseconds()) / 1000}
	if recorder, ok := h.Objects.(storage.QueryCostRecorder); ok {
		_ = recorder.RecordQueryCost(r.Context(), storage.QueryCostRecord{Tenant: tenant, ProjectID: projectID, CallerID: callerID, TypeID: typeID, RowsScanned: exec.actuals.RowsScanned, IndicesHit: indicesHit, RowsReturned: exec.actuals.RowsReturned, WallTime: wall})
	}
	if explainMode(r, body) == "analyze" {
		exec.plan.Mode = "analyze"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":                       pageItems,
		"total":                      total,
		"count":                      total,
		"page":                       page,
		"per_page":                   perPage,
		"aggregations":               aggregations,
		"linked_edges":               pageLinkedEdges,
		"knn_results":                pageKNNResults,
		"restricted_view_evaluation": restrictedDecision,
		"omitted_marking_count":      omittedMarkingCount,
		"explain":                    explainPayloadForMode(r, body, exec.plan),
		"actuals":                    actualsPayloadForMode(r, body, exec.actuals),
		"query_cost":                 exec.actuals,
		"query_budget":               exec.budget,
		"materialized_aggregates":    exec.materialized,
		"object_set": map[string]any{
			"object_type_id":      string(typeID),
			"filters":             body.Filters,
			"sort":                compactQuerySorts(body.Sort),
			"limit":               perPage,
			"include_count":       body.IncludeCount,
			"aggregations":        body.Aggregations,
			"selected_object_ids": body.SelectedObjectIDs,
			"search_around":       searchAroundContract,
			"knn":                 knnContract,
		},
	})
}

func listObjectsForQuery(r *http.Request, objects storage.ObjectStore, tenant storage.TenantId, typeID storage.TypeId, body queryRequest, consistency storage.ReadConsistency) (storage.PagedResult[storage.Object], error) {
	if indexed, ok := objects.(storage.PropertyQueryStore); ok {
		if predicate, ok := indexableQueryPredicate(body); ok {
			return indexed.QueryByProperty(r.Context(), tenant, typeID, predicate, storage.Page{Size: 1_000_000}, consistency)
		}
	}
	return objects.ListByType(r.Context(), tenant, typeID, storage.Page{Size: 1_000_000}, consistency)
}

func indexableQueryPredicate(body queryRequest) (storage.PropertyPredicate, bool) {
	if len(body.SelectedObjectIDs) > 0 || body.SearchAround != nil || body.KNN != nil {
		return storage.PropertyPredicate{}, false
	}
	if len(body.Filters) == 1 && body.Predicate == nil {
		f := body.Filters[0]
		if isIndexableOperator(f.Operator) && strings.TrimSpace(f.PropertyName) != "" {
			return storage.PropertyPredicate{PropertyName: f.PropertyName, Operator: f.Operator, Value: f.Value}, true
		}
	}
	if len(body.Filters) == 0 && body.Predicate != nil {
		return indexablePredicateNode(*body.Predicate)
	}
	return storage.PropertyPredicate{}, false
}

func indexablePredicateNode(predicate queryPredicate) (storage.PropertyPredicate, bool) {
	op := strings.ToLower(strings.TrimSpace(predicate.Op))
	if op == "" {
		op = strings.ToLower(strings.TrimSpace(predicate.Operator))
	}
	if predicate.PropertyName != "" && isIndexableOperator(op) {
		return storage.PropertyPredicate{PropertyName: predicate.PropertyName, Operator: op, Value: predicate.Value}, true
	}
	return storage.PropertyPredicate{}, false
}

func isIndexableOperator(operator string) bool {
	switch strings.ToLower(strings.TrimSpace(operator)) {
	case "", "equals", "eq", "=", "gte", ">=", "lte", "<=", "gt", ">", "lt", "<", "in", "starts_with", "prefix":
		return true
	default:
		return false
	}
}

func (h *Handlers) resolveLinkedSearchAround(
	r *http.Request,
	tenant storage.TenantId,
	targetTypeID storage.TypeId,
	config querySearchAround,
) (map[string]bool, []linkedEdgeResponse, error) {
	sourceIDs := compactStrings(config.SourceObjectIDs)
	if len(sourceIDs) == 0 {
		return map[string]bool{}, []linkedEdgeResponse{}, nil
	}
	linkTypeIDs := normalizedSearchAroundLinkTypeIDs(config)
	if len(linkTypeIDs) == 0 {
		return nil, nil, &storage.RepoError{Kind: storage.ErrInvalidArgument, Msg: "search_around requires at least one link_type_id"}
	}
	direction := normalizeSearchAroundDirection(config.Direction)
	depth := clampSearchAroundDepth(config.Depth)
	limit := 5000
	targetIDs := map[string]bool{}
	edges := []linkedEdgeResponse{}
	seenEdges := map[string]bool{}
	seenNodes := map[string]bool{}
	frontier := sourceIDs
	for _, id := range sourceIDs {
		seenNodes[id] = true
	}

	for currentDepth := 1; currentDepth <= depth && len(frontier) > 0 && len(targetIDs) < limit; currentDepth++ {
		nextFrontier := []string{}
		for _, nodeID := range frontier {
			for _, linkTypeID := range linkTypeIDs {
				if direction == "outgoing" || direction == "both" {
					items, err := collectOutgoingSearchAroundLinks(r, h.Links, tenant, storage.LinkTypeId(linkTypeID), storage.ObjectId(nodeID), limit-len(edges))
					if err != nil {
						return nil, nil, err
					}
					for _, link := range items {
						edge, neighbor := linkedSearchAroundEdge(link, "outgoing", currentDepth)
						if !seenEdges[edge.LinkID] {
							seenEdges[edge.LinkID] = true
							edges = append(edges, edge)
						}
						if !seenNodes[neighbor] {
							seenNodes[neighbor] = true
							nextFrontier = append(nextFrontier, neighbor)
						}
						targetIDs[neighbor] = true
					}
				}
				if direction == "incoming" || direction == "both" {
					items, err := collectIncomingSearchAroundLinks(r, h.Links, tenant, storage.LinkTypeId(linkTypeID), storage.ObjectId(nodeID), limit-len(edges))
					if err != nil {
						return nil, nil, err
					}
					for _, link := range items {
						edge, neighbor := linkedSearchAroundEdge(link, "incoming", currentDepth)
						if !seenEdges[edge.LinkID] {
							seenEdges[edge.LinkID] = true
							edges = append(edges, edge)
						}
						if !seenNodes[neighbor] {
							seenNodes[neighbor] = true
							nextFrontier = append(nextFrontier, neighbor)
						}
						targetIDs[neighbor] = true
					}
				}
			}
		}
		frontier = nextFrontier
	}

	if config.TargetObjectTypeID != "" && storage.TypeId(strings.TrimSpace(config.TargetObjectTypeID)) != targetTypeID {
		targetIDs = map[string]bool{}
	}
	return targetIDs, edges, nil
}

func collectOutgoingSearchAroundLinks(r *http.Request, links storage.LinkStore, tenant storage.TenantId, linkType storage.LinkTypeId, from storage.ObjectId, budget int) ([]storage.Link, error) {
	if budget <= 0 {
		return nil, nil
	}
	res, err := links.ListOutgoing(r.Context(), tenant, linkType, from, storage.Page{Size: uint32(clampInt(budget, 1, 5000))}, parseConsistency(r.URL.Query().Get("consistency")))
	if err != nil {
		return nil, err
	}
	return res.Items, nil
}

func collectIncomingSearchAroundLinks(r *http.Request, links storage.LinkStore, tenant storage.TenantId, linkType storage.LinkTypeId, to storage.ObjectId, budget int) ([]storage.Link, error) {
	if budget <= 0 {
		return nil, nil
	}
	res, err := links.ListIncoming(r.Context(), tenant, linkType, to, storage.Page{Size: uint32(clampInt(budget, 1, 5000))}, parseConsistency(r.URL.Query().Get("consistency")))
	if err != nil {
		return nil, err
	}
	return res.Items, nil
}

func linkedSearchAroundEdge(link storage.Link, direction string, depth int) (linkedEdgeResponse, string) {
	props := map[string]any{}
	if link.Payload != nil && len(*link.Payload) > 0 {
		_ = json.Unmarshal(*link.Payload, &props)
	}
	edge := linkedEdgeResponse{
		LinkID:         stableSearchAroundLinkID(link),
		LinkTypeID:     string(link.LinkType),
		SourceObjectID: string(link.From),
		TargetObjectID: string(link.To),
		Direction:      direction,
		Depth:          depth,
		Properties:     props,
	}
	if direction == "incoming" {
		return edge, string(link.From)
	}
	return edge, string(link.To)
}

func stableSearchAroundLinkID(link storage.Link) string {
	return strings.Join([]string{string(link.LinkType), string(link.From), string(link.To)}, ":")
}

func normalizedSearchAroundLinkTypeIDs(config querySearchAround) []string {
	ids := compactStrings(config.LinkTypeIDs)
	if strings.TrimSpace(config.LinkTypeID) != "" {
		ids = append(ids, strings.TrimSpace(config.LinkTypeID))
	}
	return compactStrings(ids)
}

func normalizeSearchAroundDirection(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "incoming", "inbound":
		return "incoming"
	case "both", "any", "all":
		return "both"
	default:
		return "outgoing"
	}
}

func clampSearchAroundDepth(depth int) int {
	return clampInt(depth, 1, 5)
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func compactStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	return out
}

// CreateObjectByOntologyType serves POST /api/v1/ontology/types/{type_id}/objects.
// Body shape: `{ properties: {...} }`. Used by the SPA to seed manual rows; the
// real bulk path is the indexer (see docs/poc-online-retail/RUNTIME-INDEXER.md).
func (h *Handlers) CreateObjectByOntologyType(w http.ResponseWriter, r *http.Request) {
	tenant := tenantFromRequest(r)
	typeID := storage.TypeId(chi.URLParam(r, "type_id"))
	if ok, err := runCedarGate(h, r, servicecedar.ActionWrite(), string(typeID), nil); !ok {
		writeCedarError(w, err)
		return
	}

	var body struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	payload, err := validateProperties(r.Context(), h.Schemas, string(typeID), body.Properties)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	now := time.Now().UnixMilli()
	// Mint a TimeUUID (UUIDv1) so the value is accepted by the Cassandra
	// `object_id timeuuid` column. UUIDv4 from google/uuid would be rejected by
	// gocql with "Invalid version for TimeUUID type".
	id := storage.ObjectId(gocql.TimeUUID().String())
	owner := storage.OwnerId(strings.TrimSpace(string(tenant)))
	if claims, ok := authmw.FromContext(r.Context()); ok && claims != nil {
		owner = storage.OwnerId(claims.Sub.String())
	} else if hdr := r.Header.Get("Authorization"); strings.HasPrefix(hdr, "Bearer ") {
		// The gateway already validated the JWT; here we only decode the
		// subject (UUID) to use as the object owner. Best-effort — if it
		// fails we fall through to the deterministic PoC fallback below.
		if sub := jwtSubjectUnverified(strings.TrimPrefix(hdr, "Bearer ")); sub != "" {
			owner = storage.OwnerId(sub)
		}
	}
	if _, err := uuid.Parse(string(owner)); err != nil {
		// Deterministic PoC owner UUID — only used when the request is
		// unauthenticated (e.g. seed scripts or /healthz introspection).
		owner = storage.OwnerId("00000000-0000-1000-8000-000000000001")
	}
	obj := storage.Object{
		Tenant:      tenant,
		ID:          id,
		TypeID:      typeID,
		Version:     1,
		Payload:     payload,
		Owner:       &owner,
		CreatedAtMs: &now,
		UpdatedAtMs: now,
	}
	if _, err := h.Objects.Put(r.Context(), obj, nil); err != nil {
		writeError(w, err)
		return
	}
	h.bustObjectCache(tenant, typeID, string(id))
	writeJSON(w, http.StatusCreated, toOntologyObject(&obj))
}

// jwtSubjectUnverified extracts the `sub` claim from a JWT without
// validating its signature. The gateway already authenticated the
// request; here we only need the user id to populate Object.Owner.
func jwtSubjectUnverified(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var payload struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Sub)
}

func explainMode(r *http.Request, body queryRequest) string {
	mode := strings.ToLower(strings.TrimSpace(body.Explain))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("explain")))
	}
	if body.Analyze || mode == "analyze" || mode == "explain_analyze" || strings.EqualFold(r.URL.Query().Get("explain_analyze"), "true") {
		return "analyze"
	}
	if mode == "true" || mode == "plan" || mode == "explain" {
		return "explain"
	}
	return ""
}

func callerAndProjectFromRequest(r *http.Request) (string, string) {
	caller := strings.TrimSpace(r.Header.Get("x-openfoundry-user-id"))
	if caller == "" {
		caller = strings.TrimSpace(r.Header.Get("x-of-caller"))
	}
	if caller == "" {
		caller = "anonymous"
	}
	project := strings.TrimSpace(r.Header.Get("x-openfoundry-project-id"))
	if project == "" {
		project = strings.TrimSpace(r.Header.Get("x-of-project"))
	}
	return caller, project
}

func buildObjectQueryExecution(r *http.Request, objects storage.ObjectStore, tenant storage.TenantId, typeID storage.TypeId, body queryRequest, restrictedFilters []string) objectQueryExecution {
	mode := explainMode(r, body)
	predicate, indexable := indexableQueryPredicate(body)
	indexName := ""
	estimatedRows := uint64(1_000_000)
	accessPath := "objects_by_type_scan"
	predicateText := ""
	if indexable {
		indexName = "ontology_indexes.object_property_index"
		accessPath = "property_index_lookup"
		predicateText = fmt.Sprintf("%s %s %v", predicate.PropertyName, predicate.Operator, predicate.Value)
		if stats, ok := objects.(storage.StatisticsProvider); ok {
			if hist, found, err := stats.PropertyHistogram(r.Context(), tenant, typeID, predicate.PropertyName); err == nil && found {
				estimatedRows = estimateRowsFromHistogram(hist, predicate)
			}
		} else {
			estimatedRows = 1000
		}
	}
	if len(body.SelectedObjectIDs) > 0 && uint64(len(body.SelectedObjectIDs)) < estimatedRows {
		estimatedRows = uint64(len(body.SelectedObjectIDs))
		accessPath = "selected_object_ids"
	}
	if estimatedRows == 0 {
		estimatedRows = 1
	}
	estimatedTime := 2 + float64(estimatedRows)/2500
	step := storage.QueryPlanStep{Name: "read_objects", AccessPath: accessPath, IndexName: indexName, Predicate: predicateText, EstimatedRows: estimatedRows, EstimatedTimeMs: estimatedTime, RestrictedFilters: restrictedFilters}
	plan := storage.QueryPlan{Mode: mode, IndexChoice: accessPath, EstimatedRows: estimatedRows, EstimatedTimeMs: estimatedTime, Steps: []storage.QueryPlanStep{step}}
	return objectQueryExecution{plan: plan, indexable: indexable, indexName: indexName, restrictedFilter: restrictedFilters}
}

func estimateRowsFromHistogram(hist storage.PropertyHistogram, predicate storage.PropertyPredicate) uint64 {
	if hist.TotalRows == 0 {
		return 1
	}
	op := strings.ToLower(strings.TrimSpace(predicate.Operator))
	if op == "" || op == "equals" || op == "eq" || op == "=" {
		value := strings.ToLower(strings.TrimSpace(toStringValue(predicate.Value)))
		for _, bucket := range hist.Buckets {
			if bucket.Value == value {
				return maxUint64(bucket.Count, 1)
			}
		}
		if hist.Distinct > 0 {
			return maxUint64(hist.TotalRows/hist.Distinct, 1)
		}
		return 1
	}
	if op == "in" {
		size := uint64(1)
		if arr, ok := predicate.Value.([]any); ok && len(arr) > 0 {
			size = uint64(len(arr))
		}
		if hist.Distinct > 0 {
			return maxUint64((hist.TotalRows/hist.Distinct)*size, 1)
		}
	}
	return maxUint64(hist.TotalRows/3, 1)
}

func maxUint64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

func restrictedIndexFiltersForQuery(policyID string, body queryRequest) []string {
	filters := []string{}
	if strings.TrimSpace(policyID) != "" {
		filters = append(filters, "restricted_view_id="+strings.TrimSpace(policyID))
	}
	for _, column := range body.MarkingColumns {
		if trimmed := strings.TrimSpace(column); trimmed != "" {
			filters = append(filters, "marking_column:"+trimmed)
		}
	}
	return filters
}

func explainPayloadForMode(r *http.Request, body queryRequest, plan storage.QueryPlan) any {
	if explainMode(r, body) == "" {
		return nil
	}
	return plan
}

func actualsPayloadForMode(r *http.Request, body queryRequest, actuals storage.QueryActuals) any {
	if explainMode(r, body) != "analyze" {
		return nil
	}
	return actuals
}
