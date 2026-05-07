// Object-level full-text search facade — runs a single
// SearchBackend.Search and reshapes the hits into the
// [ObjectFulltextHit] surface the handlers expose.
//
// Mirrors `libs/ontology-kernel/src/domain/search/objects_fulltext.rs`.

package search

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	domain "github.com/openfoundry/openfoundry-go/libs/ontology-kernel/domain"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// ObjectFulltextHit mirrors `struct ObjectFulltextHit`.
type ObjectFulltextHit struct {
	ID           uuid.UUID       `json:"id"`
	ObjectTypeID uuid.UUID       `json:"object_type_id"`
	Properties   json.RawMessage `json:"properties"`
	Marking      string          `json:"marking"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	Rank         float32         `json:"rank"`
}

// ObjectFulltextQuery mirrors `struct ObjectFulltextQuery`.
type ObjectFulltextQuery struct {
	Query        string
	ObjectTypeID *uuid.UUID
	Markings     *[]string
	Limit        int64
}

// SearchObjects mirrors `pub async fn search_objects`. The Rust
// source resolves the SearchBackend via `state.stores.search`; the
// Go signature accepts the backend directly so callers stay
// independent of the not-yet-wired Stores.Search field.
func SearchObjects(
	ctx context.Context,
	backend storage.SearchBackend,
	claims *authmw.Claims,
	query ObjectFulltextQuery,
) ([]ObjectFulltextHit, error) {
	trimmed := strings.TrimSpace(query.Query)
	if trimmed == "" {
		return []ObjectFulltextHit{}, nil
	}
	limit := query.Limit
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	markings := allowedMarkingsFor(claims, query.Markings)
	if len(markings) == 0 {
		return []ObjectFulltextHit{}, nil
	}

	tenant := domain.TenantFromClaims(claims)
	q := storage.SearchQuery{
		Tenant: tenant,
		Q:      &trimmed,
		Page:   storage.Page{Size: uint32(limit)},
	}
	if query.ObjectTypeID != nil {
		typeID := storage.TypeId(query.ObjectTypeID.String())
		q.TypeID = &typeID
	}

	page, err := backend.Search(ctx, q, storage.Eventual())
	if err != nil {
		return nil, fmt.Errorf("search backend full-text query failed: %s", err)
	}

	out := []ObjectFulltextHit{}
	var fallbackOrg *uuid.UUID
	if claims != nil {
		fallbackOrg = claims.OrgID
	}
	for _, hit := range page.Items {
		obj := searchHitToObjectInstance(hit, fallbackOrg)
		if obj == nil {
			continue
		}
		matched := false
		for _, m := range markings {
			if m == obj.Marking {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		out = append(out, ObjectFulltextHit{
			ID:           obj.ID,
			ObjectTypeID: obj.ObjectTypeID,
			Properties:   obj.Properties,
			Marking:      obj.Marking,
			CreatedAt:    obj.CreatedAt,
			UpdatedAt:    obj.UpdatedAt,
			Rank:         hit.Score,
		})
	}
	return out, nil
}

// allowedMarkingsFor mirrors `fn allowed_markings_for`.
//
// Admins always see {public, confidential, pii}. Everyone else sees
// the prefix of that list determined by their clearance rank. When
// the caller supplied an explicit `requested` list, it is filtered
// through the base allowlist (so a caller cannot widen the scope
// past their clearance).
func allowedMarkingsFor(claims *authmw.Claims, requested *[]string) []string {
	base := []string{"public"}
	if claims != nil && claims.HasRole("admin") {
		base = []string{"public", "confidential", "pii"}
	} else {
		granted := uint8(0)
		if claims != nil {
			granted = domain.ClearanceRank(claims)
		}
		if granted >= 1 {
			base = append(base, "confidential")
		}
		if granted >= 2 {
			base = append(base, "pii")
		}
	}
	if requested == nil || len(*requested) == 0 {
		return base
	}
	allowed := map[string]bool{}
	for _, m := range base {
		allowed[m] = true
	}
	out := []string{}
	for _, m := range *requested {
		if allowed[m] {
			out = append(out, m)
		}
	}
	return out
}

// searchHitToObjectInstance mirrors
// `read_models::search_hit_to_object_instance`. Pulls id /
// object_type_id / properties / marking / created_at / updated_at
// out of the SearchHit's snippet, falling back to the hit's own
// id+type_id and to "public" / now() for the marking and timestamp.
//
// Inlined here to keep iter 7d independent of the read_models port —
// when iter 7c₅ adds the canonical helper to read_models.go this
// can be replaced with a single call.
func searchHitToObjectInstance(hit storage.SearchHit, fallbackOrgID *uuid.UUID) *domain.ObjectInstance {
	if len(hit.Snippet) == 0 {
		return nil
	}
	var snippet map[string]json.RawMessage
	if err := json.Unmarshal(hit.Snippet, &snippet); err != nil {
		return nil
	}

	id := snippetUUID(snippet, "id")
	if id == nil {
		if v, err := uuid.Parse(string(hit.ID)); err == nil {
			id = &v
		}
	}
	if id == nil {
		return nil
	}

	objectTypeID := snippetUUID(snippet, "object_type_id")
	if objectTypeID == nil {
		objectTypeID = snippetUUID(snippet, "type_id")
	}
	if objectTypeID == nil {
		if v, err := uuid.Parse(string(hit.TypeID)); err == nil {
			objectTypeID = &v
		}
	}
	if objectTypeID == nil {
		return nil
	}

	properties := json.RawMessage(`{}`)
	if v, ok := snippet["properties"]; ok && len(v) > 0 {
		properties = v
	} else if v, ok := snippet["payload"]; ok && len(v) > 0 {
		properties = v
	}

	createdBy := uuid.Nil
	if v := snippetUUID(snippet, "created_by"); v != nil {
		createdBy = *v
	}
	orgID := snippetUUID(snippet, "organization_id")
	if orgID == nil {
		orgID = fallbackOrgID
	}

	marking := "public"
	if s, ok := snippetString(snippet, "marking"); ok {
		marking = s
	} else if arr, ok := snippet["markings"]; ok {
		var values []string
		if err := json.Unmarshal(arr, &values); err == nil && len(values) > 0 {
			marking = values[0]
		}
	}

	updatedAt := snippetRFC3339(snippet, "updated_at")
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	createdAt := snippetRFC3339(snippet, "created_at")
	if createdAt.IsZero() {
		createdAt = updatedAt
	}

	return &domain.ObjectInstance{
		ID:             *id,
		ObjectTypeID:   *objectTypeID,
		Properties:     properties,
		CreatedBy:      createdBy,
		OrganizationID: orgID,
		Marking:        marking,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}
}

// ---- snippet helpers -----------------------------------------------------

func snippetUUID(snippet map[string]json.RawMessage, field string) *uuid.UUID {
	v, ok := snippet[field]
	if !ok || string(v) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return nil
	}
	parsed, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return &parsed
}

func snippetString(snippet map[string]json.RawMessage, field string) (string, bool) {
	v, ok := snippet[field]
	if !ok || string(v) == "null" {
		return "", false
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return "", false
	}
	return s, true
}

func snippetRFC3339(snippet map[string]json.RawMessage, field string) time.Time {
	v, ok := snippet[field]
	if !ok || string(v) == "null" {
		return time.Time{}
	}
	// Try RFC3339 string first, then ms-since-epoch number.
	var s string
	if err := json.Unmarshal(v, &s); err == nil {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.UTC()
		}
		return time.Time{}
	}
	var n int64
	if err := json.Unmarshal(v, &n); err == nil {
		return time.UnixMilli(n).UTC()
	}
	return time.Time{}
}
