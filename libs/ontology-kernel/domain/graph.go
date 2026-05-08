// Schema + object graph builder.
//
// Mirrors `libs/ontology-kernel/src/domain/graph.rs`. The schema
// branch is a straight pgx port; the object branch keeps its full
// BFS expansion + summary pipeline but accepts an injected
// [GraphObjectLoader] so callers can wire whatever read-model
// loader they own. The Rust source delegates this to
// `read_models::load_object_instance_from_read_model` which depends
// on storage_abstraction::SearchBackend (not yet ported on the Go
// side); injection sidesteps the dep cycle until iter 7c₅.

package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// ---- Node + edge id helpers ----------------------------------------------

func typeNodeID(typeID uuid.UUID) string      { return "type:" + typeID.String() }
func interfaceNodeID(id uuid.UUID) string     { return "interface:" + id.String() }
func objectNodeID(id uuid.UUID) string        { return "object:" + id.String() }
func objectRoute(typeID, id uuid.UUID) string { return "/ontology/" + typeID.String() + "#object-" + id.String() }

// objectLabel mirrors `fn object_label`. Picks the primary-key
// property when set + non-empty; falls back to the object id.
func objectLabel(objectType models.ObjectType, object *ObjectInstance) string {
	if objectType.PrimaryKeyProperty == nil {
		return object.ID.String()
	}
	var props map[string]json.RawMessage
	if err := json.Unmarshal(object.Properties, &props); err != nil {
		return object.ID.String()
	}
	raw, ok := props[*objectType.PrimaryKeyProperty]
	if !ok {
		return object.ID.String()
	}
	// JSON string values: unwrap and return iff non-empty (matches
	// Rust `Some(pk) if !pk.is_empty()`); empty string falls back
	// to id.
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			if s != "" {
				return s
			}
			return object.ID.String()
		}
	}
	// Non-string values stringify via the raw JSON bytes (mirrors
	// Rust `_ => serde_json::to_string(value)`).
	if len(raw) > 0 {
		return string(raw)
	}
	return object.ID.String()
}

func incrementCount(m map[string]int, key string) { m[key]++ }

// classifyScope mirrors `fn classify_scope`.
func classifyScope(mode string, rootNeighborCount, sensitiveObjects, boundaryCrossings int) string {
	switch {
	case mode == "schema":
		return "schema"
	case sensitiveObjects > 0:
		return "sensitive_connected"
	case boundaryCrossings > 0:
		return "cross_boundary"
	case rootNeighborCount > 0:
		return "connected"
	default:
		return "local"
	}
}

// SummarizeGraph mirrors `pub(crate) fn summarize_graph`. Pure logic
// over the assembled node + edge slices; emits the [GraphSummary]
// shape consumed by the front-end. Exported so handlers / tests can
// drive it directly.
func SummarizeGraph(mode string, nodes []models.GraphNode, edges []models.GraphEdge) models.GraphSummary {
	nodeKinds := map[string]int{}
	edgeKinds := map[string]int{}
	objectTypes := map[string]int{}
	markings := map[string]int{}
	sensitiveSet := map[string]bool{}
	maxHops := 0
	rootNeighbors := 0
	sensitiveObjects := 0

	nodeMetadata := make(map[string]json.RawMessage, len(nodes))
	for _, n := range nodes {
		nodeMetadata[n.ID] = n.Metadata
	}

	for _, n := range nodes {
		incrementCount(nodeKinds, n.Kind)
		switch n.Kind {
		case "object_type":
			incrementCount(objectTypes, n.Label)
		case "object_instance":
			if n.SecondaryLabel != nil {
				incrementCount(objectTypes, *n.SecondaryLabel)
			}
		}
		var meta map[string]json.RawMessage
		if len(n.Metadata) > 0 {
			_ = json.Unmarshal(n.Metadata, &meta)
		}
		if marking, ok := nestedString(meta, "marking"); ok {
			incrementCount(markings, marking)
			if marking != "public" {
				sensitiveObjects++
				sensitiveSet[marking] = true
			}
		}
		if distance, ok := nestedUint(meta, "distance_from_root"); ok {
			if distance > maxHops {
				maxHops = distance
			}
			if distance == 1 {
				rootNeighbors++
			}
		}
	}

	boundaryCrossings := 0
	for _, e := range edges {
		incrementCount(edgeKinds, e.Kind)
		sourceOrg := nestedStringFromRaw(nodeMetadata[e.Source], "organization_id")
		targetOrg := nestedStringFromRaw(nodeMetadata[e.Target], "organization_id")
		if sourceOrg != targetOrg && (sourceOrg != "" || targetOrg != "") {
			boundaryCrossings++
		}
	}

	sensitiveMarkings := make([]string, 0, len(sensitiveSet))
	for m := range sensitiveSet {
		sensitiveMarkings = append(sensitiveMarkings, m)
	}
	sort.Strings(sensitiveMarkings)

	return models.GraphSummary{
		Scope:             classifyScope(mode, rootNeighbors, sensitiveObjects, boundaryCrossings),
		NodeKinds:         nodeKinds,
		EdgeKinds:         edgeKinds,
		ObjectTypes:       objectTypes,
		Markings:          markings,
		RootNeighborCount: rootNeighbors,
		MaxHopsReached:    maxHops,
		BoundaryCrossings: boundaryCrossings,
		SensitiveObjects:  sensitiveObjects,
		SensitiveMarkings: sensitiveMarkings,
	}
}

// ---- Schema graph (pgx) --------------------------------------------------

// BuildSchemaGraph mirrors `async fn build_schema_graph`. Reads
// every object_type / interface / interface-binding / link_type from
// PG; if a `rootTypeID` is given, narrows the visible set to the
// types directly connected by a link from/to it.
func BuildSchemaGraph(ctx context.Context, db *pgxpool.Pool, rootTypeID *uuid.UUID) (models.GraphResponse, error) {
	objectTypes, err := loadAllObjectTypes(ctx, db)
	if err != nil {
		return models.GraphResponse{}, fmt.Errorf("failed to load object types: %s", err)
	}
	interfaces, err := loadAllInterfaces(ctx, db)
	if err != nil {
		return models.GraphResponse{}, fmt.Errorf("failed to load interfaces: %s", err)
	}
	bindings, err := loadAllInterfaceBindings(ctx, db)
	if err != nil {
		return models.GraphResponse{}, fmt.Errorf("failed to load interface bindings: %s", err)
	}
	linkTypes, err := loadAllLinkTypesForGraph(ctx, db)
	if err != nil {
		return models.GraphResponse{}, fmt.Errorf("failed to load link types: %s", err)
	}

	allowedTypes := map[uuid.UUID]bool{}
	for _, t := range objectTypes {
		allowedTypes[t.ID] = true
	}
	if rootTypeID != nil {
		focused := map[uuid.UUID]bool{*rootTypeID: true}
		for _, lt := range linkTypes {
			if lt.SourceTypeID == *rootTypeID || lt.TargetTypeID == *rootTypeID {
				focused[lt.SourceTypeID] = true
				focused[lt.TargetTypeID] = true
			}
		}
		allowedTypes = focused
	}

	allowedInterfaces := map[uuid.UUID]bool{}
	for _, b := range bindings {
		if allowedTypes[b.ObjectTypeID] {
			allowedInterfaces[b.InterfaceID] = true
		}
	}

	nodes := []models.GraphNode{}
	for _, t := range objectTypes {
		if !allowedTypes[t.ID] {
			continue
		}
		secondary := t.Name
		route := "/ontology/" + t.ID.String()
		nodes = append(nodes, models.GraphNode{
			ID:             typeNodeID(t.ID),
			Kind:           "object_type",
			Label:          t.DisplayName,
			SecondaryLabel: &secondary,
			Color:          t.Color,
			Route:          &route,
			Metadata: jsonObjectOf(map[string]any{
				"icon":                  t.Icon,
				"description":           t.Description,
				"primary_key_property":  t.PrimaryKeyProperty,
			}),
		})
	}
	for _, iface := range interfaces {
		if !allowedInterfaces[iface.ID] {
			continue
		}
		secondary := iface.Name
		teal := "#0f766e"
		route := "/ontology/graph"
		nodes = append(nodes, models.GraphNode{
			ID:             interfaceNodeID(iface.ID),
			Kind:           "interface",
			Label:          iface.DisplayName,
			SecondaryLabel: &secondary,
			Color:          &teal,
			Route:          &route,
			Metadata:       jsonObjectOf(map[string]any{"description": iface.Description}),
		})
	}

	edges := []models.GraphEdge{}
	for _, lt := range linkTypes {
		if !allowedTypes[lt.SourceTypeID] || !allowedTypes[lt.TargetTypeID] {
			continue
		}
		edges = append(edges, models.GraphEdge{
			ID:     "link_type:" + lt.ID.String(),
			Kind:   "link_type",
			Source: typeNodeID(lt.SourceTypeID),
			Target: typeNodeID(lt.TargetTypeID),
			Label:  lt.DisplayName,
			Metadata: jsonObjectOf(map[string]any{
				"name":        lt.Name,
				"cardinality": lt.Cardinality,
				"description": lt.Description,
			}),
		})
	}
	for _, b := range bindings {
		if !allowedTypes[b.ObjectTypeID] || !allowedInterfaces[b.InterfaceID] {
			continue
		}
		edges = append(edges, models.GraphEdge{
			ID:       "interface_binding:" + b.ObjectTypeID.String() + ":" + b.InterfaceID.String(),
			Kind:     "interface_binding",
			Source:   typeNodeID(b.ObjectTypeID),
			Target:   interfaceNodeID(b.InterfaceID),
			Label:    "implements",
			Metadata: jsonObjectOf(map[string]any{}),
		})
	}

	summary := SummarizeGraph("schema", nodes, edges)
	return models.GraphResponse{
		Mode:         "schema",
		RootTypeID:   rootTypeID,
		Depth:        1,
		TotalNodes:   len(nodes),
		TotalEdges:   len(edges),
		Summary:      summary,
		Nodes:        nodes,
		Edges:        edges,
	}, nil
}

// ---- Object graph (LinkStore-driven BFS) ---------------------------------

// GraphObjectLoader is the read-model loader the object graph
// builder consults to hydrate every visited object id. Mirrors
// `read_models::load_object_instance_from_read_model` — returning
// (nil, nil) when the id is unknown lets the builder skip silently
// the way the Rust source does.
type GraphObjectLoader func(ctx context.Context, claims *authmw.Claims, objectID uuid.UUID) (*ObjectInstance, error)

// BuildObjectGraph mirrors `async fn build_object_graph`. The Rust
// source delegates the per-object hydration to
// `load_object_instance_from_read_model`; the Go port takes that
// function as an argument so the iter 7c₄ lands without waiting on
// SearchBackend in storage-abstraction (iter 7c₅+).
//
// Accepts depth + limit optional pointers — nil → defaults
// (depth=2, limit=40). Both clamp: depth ∈ [1, 4], limit ∈ [1, 120].
func BuildObjectGraph(
	ctx context.Context,
	db *pgxpool.Pool,
	links storage.LinkStore,
	claims *authmw.Claims,
	loader GraphObjectLoader,
	rootObjectID uuid.UUID,
	depth, limit *int,
) (models.GraphResponse, error) {
	d := 2
	if depth != nil {
		d = *depth
	}
	if d < 1 {
		d = 1
	}
	if d > 4 {
		d = 4
	}
	l := 40
	if limit != nil {
		l = *limit
	}
	if l < 1 {
		l = 1
	}
	if l > 120 {
		l = 120
	}

	root, err := loader(ctx, claims, rootObjectID)
	if err != nil {
		return models.GraphResponse{}, fmt.Errorf("failed to load root object: %s", err)
	}
	if root == nil {
		return models.GraphResponse{}, fmt.Errorf("root object was not found")
	}
	if err := EnsureObjectAccess(claims, root); err != nil {
		return models.GraphResponse{}, err
	}
	tenant := TenantFromClaims(claims)

	objectTypes, err := loadAllObjectTypes(ctx, db)
	if err != nil {
		return models.GraphResponse{}, fmt.Errorf("failed to load object types: %s", err)
	}
	objectTypeMap := map[uuid.UUID]models.ObjectType{}
	for _, t := range objectTypes {
		objectTypeMap[t.ID] = t
	}
	linkTypes, err := loadAllLinkTypesForGraph(ctx, db)
	if err != nil {
		return models.GraphResponse{}, fmt.Errorf("failed to load link types: %s", err)
	}
	linkTypeMap := map[uuid.UUID]models.LinkType{}
	linkTypeIDs := make([]storage.LinkTypeId, 0, len(linkTypes))
	for _, lt := range linkTypes {
		linkTypeMap[lt.ID] = lt
		linkTypeIDs = append(linkTypeIDs, storage.LinkTypeId(lt.ID.String()))
	}

	visited := map[uuid.UUID]bool{rootObjectID: true}
	distance := map[uuid.UUID]int{rootObjectID: 0}
	seenEdges := map[uuid.UUID]bool{}
	type queueEntry struct {
		id    uuid.UUID
		level int
	}
	queue := []queueEntry{{rootObjectID, 0}}
	linkInstances := []LinkInstance{}

	for len(queue) > 0 {
		head := queue[0]
		queue = queue[1:]
		if head.level >= d {
			continue
		}
		objectKey := storage.ObjectId(head.id.String())
		adjacent, err := CollectLinks(ctx, links, tenant, objectKey, linkTypeIDs, l)
		if err != nil {
			return models.GraphResponse{}, fmt.Errorf("failed to load graph edges: %s", err)
		}
		for _, link := range adjacent {
			linkTypeUUID, perr := uuid.Parse(string(link.LinkType))
			if perr != nil {
				continue
			}
			sourceUUID, perr := uuid.Parse(string(link.From))
			if perr != nil {
				continue
			}
			targetUUID, perr := uuid.Parse(string(link.To))
			if perr != nil {
				continue
			}
			linkID := StableLinkID(link.LinkType, link.From, link.To)
			if seenEdges[linkID] {
				continue
			}
			seenEdges[linkID] = true

			createdAt := time.UnixMilli(link.CreatedAtMs).UTC()
			linkInstances = append(linkInstances, LinkInstance{
				ID:             linkID,
				LinkTypeID:     linkTypeUUID,
				SourceObjectID: sourceUUID,
				TargetObjectID: targetUUID,
				Properties:     link.Payload,
				CreatedAt:      createdAt,
			})

			neighbour := targetUUID
			if sourceUUID == head.id {
				neighbour = targetUUID
			} else {
				neighbour = sourceUUID
			}
			if len(visited) < l && !visited[neighbour] {
				visited[neighbour] = true
				distance[neighbour] = head.level + 1
				queue = append(queue, queueEntry{neighbour, head.level + 1})
			}
		}
	}

	// Hydrate every visited object via the injected loader; drop
	// objects the caller cannot access.
	objects := []ObjectInstance{}
	allowedObjectIDs := map[uuid.UUID]bool{}
	visitedSlice := make([]uuid.UUID, 0, len(visited))
	for id := range visited {
		visitedSlice = append(visitedSlice, id)
	}
	for _, id := range visitedSlice {
		obj, lerr := loader(ctx, claims, id)
		if lerr != nil {
			return models.GraphResponse{}, fmt.Errorf("failed to hydrate object graph node: %s", lerr)
		}
		if obj == nil {
			continue
		}
		if EnsureObjectAccess(claims, obj) != nil {
			continue
		}
		allowedObjectIDs[obj.ID] = true
		objects = append(objects, *obj)
	}
	sort.SliceStable(objects, func(i, j int) bool {
		di := distance[objects[i].ID]
		dj := distance[objects[j].ID]
		if di == 0 && dj == 0 && objects[i].ID != rootObjectID {
			di = d + 1
		}
		if di == 0 && objects[i].ID != rootObjectID {
			di = d + 1
		}
		if dj == 0 && objects[j].ID != rootObjectID {
			dj = d + 1
		}
		if di != dj {
			return di < dj
		}
		return objects[i].ID.String() < objects[j].ID.String()
	})

	nodes := []models.GraphNode{}
	for _, obj := range objects {
		t, ok := objectTypeMap[obj.ObjectTypeID]
		if !ok {
			continue
		}
		dist, _ := distance[obj.ID]
		role := "extended"
		switch dist {
		case 0:
			role = "root"
		case 1:
			role = "neighbor"
		}
		secondary := t.DisplayName
		route := objectRoute(obj.ObjectTypeID, obj.ID)
		nodes = append(nodes, models.GraphNode{
			ID:             objectNodeID(obj.ID),
			Kind:           "object_instance",
			Label:          objectLabel(t, &obj),
			SecondaryLabel: &secondary,
			Color:          t.Color,
			Route:          &route,
			Metadata: jsonObjectOf(map[string]any{
				"object_type_id":     obj.ObjectTypeID,
				"distance_from_root": dist,
				"role":               role,
				"organization_id":    obj.OrganizationID,
				"marking":            obj.Marking,
				"properties":         obj.Properties,
			}),
		})
	}

	edges := []models.GraphEdge{}
	for _, link := range linkInstances {
		if !allowedObjectIDs[link.SourceObjectID] || !allowedObjectIDs[link.TargetObjectID] {
			continue
		}
		lt, ok := linkTypeMap[link.LinkTypeID]
		if !ok {
			continue
		}
		edges = append(edges, models.GraphEdge{
			ID:     "link_instance:" + link.ID.String(),
			Kind:   "link_instance",
			Source: objectNodeID(link.SourceObjectID),
			Target: objectNodeID(link.TargetObjectID),
			Label:  lt.DisplayName,
			Metadata: jsonObjectOf(map[string]any{
				"link_type_id":                       lt.ID,
				"cardinality":                        lt.Cardinality,
				"crosses_organization_boundary":      false,
				"properties":                         link.Properties,
			}),
		})
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })

	var rootTypeID *uuid.UUID
	for _, obj := range objects {
		if obj.ID == rootObjectID {
			tid := obj.ObjectTypeID
			rootTypeID = &tid
			break
		}
	}

	summary := SummarizeGraph("object", nodes, edges)

	// Second pass: backfill `crosses_organization_boundary` on each
	// edge by looking up the source/target node metadata. Mirrors
	// the Rust mutation pass at the bottom of `build_object_graph`.
	for i := range edges {
		sourceOrg := findNodeOrgID(nodes, edges[i].Source)
		targetOrg := findNodeOrgID(nodes, edges[i].Target)
		crosses := !equalOptUUID(sourceOrg, targetOrg) && (sourceOrg != nil || targetOrg != nil)
		var meta map[string]json.RawMessage
		if err := json.Unmarshal(edges[i].Metadata, &meta); err == nil {
			meta["crosses_organization_boundary"] = boolRaw(crosses)
			out, _ := json.Marshal(meta)
			edges[i].Metadata = out
		}
	}

	return models.GraphResponse{
		Mode:         "object",
		RootObjectID: &rootObjectID,
		RootTypeID:   rootTypeID,
		Depth:        d,
		TotalNodes:   len(nodes),
		TotalEdges:   len(edges),
		Summary:      summary,
		Nodes:        nodes,
		Edges:        edges,
	}, nil
}

// BuildGraph mirrors `pub async fn build_graph`. Dispatches to the
// schema or the object branch based on whether `query.RootObjectID`
// is set; the object branch needs the caller-supplied loader.
func BuildGraph(
	ctx context.Context,
	db *pgxpool.Pool,
	links storage.LinkStore,
	claims *authmw.Claims,
	loader GraphObjectLoader,
	query models.GraphQuery,
) (models.GraphResponse, error) {
	if query.RootObjectID != nil {
		return BuildObjectGraph(ctx, db, links, claims, loader, *query.RootObjectID, query.Depth, query.Limit)
	}
	return BuildSchemaGraph(ctx, db, query.RootTypeID)
}

// ---- helpers --------------------------------------------------------------

// nestedString reads a string field from a metadata map.
func nestedString(meta map[string]json.RawMessage, key string) (string, bool) {
	if meta == nil {
		return "", false
	}
	raw, ok := meta[key]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// nestedUint reads an unsigned int field from a metadata map.
func nestedUint(meta map[string]json.RawMessage, key string) (int, bool) {
	if meta == nil {
		return 0, false
	}
	raw, ok := meta[key]
	if !ok {
		return 0, false
	}
	var n uint64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, false
	}
	return int(n), true
}

// nestedStringFromRaw flattens nestedString for callers that hold a
// raw metadata blob directly.
func nestedStringFromRaw(raw json.RawMessage, key string) string {
	if len(raw) == 0 {
		return ""
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(raw, &meta); err != nil {
		return ""
	}
	v, _ := nestedString(meta, key)
	return v
}

// jsonObjectOf marshals an arbitrary map into a json.RawMessage.
// Used to keep the GraphNode/GraphEdge metadata blobs compact.
func jsonObjectOf(m map[string]any) json.RawMessage {
	b, _ := json.Marshal(m)
	return b
}

// boolRaw returns the raw JSON encoding of a bool.
func boolRaw(b bool) json.RawMessage {
	if b {
		return json.RawMessage("true")
	}
	return json.RawMessage("false")
}

// findNodeOrgID returns the parsed organization_id UUID for the node
// matching `id`, or nil when the node has no organization_id.
func findNodeOrgID(nodes []models.GraphNode, id string) *uuid.UUID {
	for _, n := range nodes {
		if n.ID != id {
			continue
		}
		var meta map[string]json.RawMessage
		if err := json.Unmarshal(n.Metadata, &meta); err != nil {
			return nil
		}
		raw, ok := meta["organization_id"]
		if !ok {
			return nil
		}
		var s *string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil
		}
		if s == nil {
			return nil
		}
		parsed, err := uuid.Parse(*s)
		if err != nil {
			return nil
		}
		return &parsed
	}
	return nil
}

func equalOptUUID(a, b *uuid.UUID) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return *a == *b
	}
}

// ---- Schema-graph PG loaders ---------------------------------------------

func loadAllObjectTypes(ctx context.Context, db *pgxpool.Pool) ([]models.ObjectType, error) {
	rows, err := db.Query(ctx,
		`SELECT id, name, display_name, description, primary_key_property, icon, color, owner_id, created_at, updated_at
           FROM object_types ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ObjectType{}
	for rows.Next() {
		var t models.ObjectType
		if err := rows.Scan(
			&t.ID, &t.Name, &t.DisplayName, &t.Description,
			&t.PrimaryKeyProperty, &t.Icon, &t.Color,
			&t.OwnerID, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func loadAllInterfaces(ctx context.Context, db *pgxpool.Pool) ([]models.OntologyInterface, error) {
	rows, err := db.Query(ctx,
		`SELECT id, name, display_name, description, owner_id, created_at, updated_at
           FROM ontology_interfaces ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.OntologyInterface{}
	for rows.Next() {
		var i models.OntologyInterface
		if err := rows.Scan(
			&i.ID, &i.Name, &i.DisplayName, &i.Description,
			&i.OwnerID, &i.CreatedAt, &i.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func loadAllInterfaceBindings(ctx context.Context, db *pgxpool.Pool) ([]models.ObjectTypeInterfaceBinding, error) {
	rows, err := db.Query(ctx,
		`SELECT object_type_id, interface_id, created_at FROM object_type_interfaces`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ObjectTypeInterfaceBinding{}
	for rows.Next() {
		var b models.ObjectTypeInterfaceBinding
		if err := rows.Scan(&b.ObjectTypeID, &b.InterfaceID, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func loadAllLinkTypesForGraph(ctx context.Context, db *pgxpool.Pool) ([]models.LinkType, error) {
	rows, err := db.Query(ctx,
		`SELECT `+linkTypeColumns+` FROM link_types ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.LinkType{}
	for rows.Next() {
		lt, err := scanLinkType(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, lt)
	}
	return out, rows.Err()
}
