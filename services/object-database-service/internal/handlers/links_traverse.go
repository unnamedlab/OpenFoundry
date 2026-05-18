package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	servicecedar "github.com/openfoundry/openfoundry-go/services/object-database-service/internal/cedarauthz"
	"github.com/openfoundry/openfoundry-go/services/object-database-service/internal/storage"
)

// TraverseLinksRequest mirrors the proto TraverseLinksRequest shape on
// the HTTP wire (snake_case JSON for parity with the rest of the
// service). Direction strings are the SPA-friendly "outgoing" /
// "incoming" / "both"; "unspecified" / "" map to "outgoing" so the
// proto enum's UNSPECIFIED is treated identically.
type TraverseLinksRequest struct {
	PrimaryKey       string   `json:"primary_key"`
	LinkTypeAPIName  string   `json:"link_type_api_name,omitempty"`
	LinkTypeAPINames []string `json:"link_type_api_names,omitempty"`
	Direction        string   `json:"direction,omitempty"`
	Depth            int      `json:"depth,omitempty"`
	Limit            int      `json:"limit,omitempty"`
}

// LinkedNode is the hydrated neighbour returned alongside each edge.
type LinkedNode struct {
	ID           string         `json:"id"`
	ObjectTypeID string         `json:"object_type_id"`
	Properties   map[string]any `json:"properties"`
}

// TraverseLinksResponse is the HTTP wire shape; matches the proto
// `TraverseLinksResponse` field-for-field on snake_case.
type TraverseLinksResponse struct {
	Edges   []linkedEdgeResponse `json:"edges"`
	Objects []LinkedNode         `json:"objects"`
}

// TraverseLinks serves POST /api/v1/ontology/types/{type_id}/links/traverse.
// Returns the BFS-expanded neighbourhood of a source object across a
// configurable set of link types. v1 reuses the same machinery
// [QueryObjectsByOntologyType] uses for `search_around` — the
// dedicated endpoint exists so clients that only need traversal don't
// have to construct an object-set query.
func (h *Handlers) TraverseLinks(w http.ResponseWriter, r *http.Request) {
	tenant := tenantFromRequest(r)
	typeID := storage.TypeId(chi.URLParam(r, "type_id"))
	if ok, err := runCedarGate(h, r, servicecedar.ActionLinkRead(), string(typeID), nil); !ok {
		writeCedarError(w, err)
		return
	}

	var body TraverseLinksRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pk := strings.TrimSpace(body.PrimaryKey)
	if pk == "" {
		http.Error(w, "primary_key is required", http.StatusBadRequest)
		return
	}
	linkTypes := compactStrings(body.LinkTypeAPINames)
	if t := strings.TrimSpace(body.LinkTypeAPIName); t != "" {
		linkTypes = append(linkTypes, t)
	}
	linkTypes = compactStrings(linkTypes)
	if len(linkTypes) == 0 {
		http.Error(w, "at least one link_type_api_name is required", http.StatusBadRequest)
		return
	}
	cfg := querySearchAround{
		SourceObjectIDs:    []string{pk},
		LinkTypeIDs:        linkTypes,
		Direction:          body.Direction,
		Depth:              body.Depth,
		TargetObjectTypeID: string(typeID),
	}

	_, edges, err := h.resolveLinkedSearchAround(r, tenant, typeID, cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	limit := body.Limit
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	if len(edges) > limit {
		edges = edges[:limit]
	}

	// Hydrate one node per unique neighbour id by fetching directly
	// from the object store. The cost is O(neighbours); for depth=1
	// that's the natural fan-out of the source.
	consistency := parseConsistency(r.URL.Query().Get("consistency"))
	seen := map[string]struct{}{}
	objects := make([]LinkedNode, 0, len(edges))
	for _, edge := range edges {
		neighbour := edge.TargetObjectID
		if edge.Direction == "incoming" {
			neighbour = edge.SourceObjectID
		}
		if _, ok := seen[neighbour]; ok {
			continue
		}
		seen[neighbour] = struct{}{}
		obj, _ := h.Objects.Get(r.Context(), tenant, storage.ObjectId(neighbour), consistency)
		if obj == nil || !callerCanReadStorageObject(r, obj) {
			continue
		}
		ont := toOntologyObject(obj)
		objects = append(objects, LinkedNode{
			ID:           ont.ID,
			ObjectTypeID: ont.ObjectTypeID,
			Properties:   ont.Properties,
		})
	}

	writeJSON(w, http.StatusOK, TraverseLinksResponse{Edges: edges, Objects: objects})
}
