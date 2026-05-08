package lineage

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/models"
)

// SyntheticLabel ports `synthetic_label`. Used when a node is named
// in a relation but has no row in `lineage_nodes` yet.
func SyntheticLabel(kind models.NodeKind, id uuid.UUID) string {
	idStr := id.String()
	if len(idStr) > 8 {
		idStr = idStr[:8]
	}
	return fmt.Sprintf("%s %s", kind.String(), idStr)
}

// NodeFromRecord ports `node_from_record`.
func NodeFromRecord(record models.LineageNodeRecord) models.LineageNode {
	return models.LineageNode{
		ID:       record.EntityID,
		Kind:     record.EntityKind,
		Label:    record.Label,
		Marking:  record.Marking,
		Metadata: record.Metadata,
	}
}

// BuildNodeView ports `build_node_view`. When the lineage_nodes
// overlay has a record, we use it; otherwise we synthesise a
// `public`-marked, `synthetic: true` placeholder.
func BuildNodeView(key models.NodeKey, nodes map[models.NodeKey]models.LineageNodeRecord) (models.LineageNode, bool) {
	if record, ok := nodes[key]; ok {
		return NodeFromRecord(record), true
	}
	return models.LineageNode{
		ID:       key.ID,
		Kind:     key.Kind.String(),
		Label:    SyntheticLabel(key.Kind, key.ID),
		Marking:  "public",
		Metadata: json.RawMessage(`{"synthetic":true}`),
	}, true
}

// CollectRelationNodeKeys ports `collect_relation_node_keys`.
func CollectRelationNodeKeys(relations []models.LineageRelationRecord) map[models.NodeKey]struct{} {
	keys := map[models.NodeKey]struct{}{}
	for _, r := range relations {
		sk, ok := models.ParseNodeKind(r.SourceKind)
		if !ok {
			continue
		}
		tk, ok := models.ParseNodeKind(r.TargetKind)
		if !ok {
			continue
		}
		keys[models.NodeKey{ID: r.SourceID, Kind: sk}] = struct{}{}
		keys[models.NodeKey{ID: r.TargetID, Kind: tk}] = struct{}{}
	}
	return keys
}

// BuildGraph ports `build_graph`. Iterates over every relation,
// builds the node views for both endpoints (deduping into a sorted
// map keyed by (kind, id)), and collects the edges.
func BuildGraph(nodes map[models.NodeKey]models.LineageNodeRecord, relations []models.LineageRelationRecord, allowed map[models.NodeKey]struct{}) models.LineageGraph {
	type nodeKey struct {
		kind string
		id   uuid.UUID
	}
	graphNodes := map[nodeKey]models.LineageNode{}
	graphEdges := []models.LineageGraphEdge{}

	for _, r := range relations {
		sk, ok := models.ParseNodeKind(r.SourceKind)
		if !ok {
			continue
		}
		tk, ok := models.ParseNodeKind(r.TargetKind)
		if !ok {
			continue
		}
		sKey := models.NodeKey{ID: r.SourceID, Kind: sk}
		tKey := models.NodeKey{ID: r.TargetID, Kind: tk}

		if allowed != nil {
			if _, ok := allowed[sKey]; !ok {
				continue
			}
			if _, ok := allowed[tKey]; !ok {
				continue
			}
		}

		if node, ok := BuildNodeView(sKey, nodes); ok {
			k := nodeKey{kind: node.Kind, id: node.ID}
			if _, exists := graphNodes[k]; !exists {
				graphNodes[k] = node
			}
		}
		if node, ok := BuildNodeView(tKey, nodes); ok {
			k := nodeKey{kind: node.Kind, id: node.ID}
			if _, exists := graphNodes[k]; !exists {
				graphNodes[k] = node
			}
		}

		graphEdges = append(graphEdges, models.LineageGraphEdge{
			ID:               r.ID,
			Source:           r.SourceID,
			SourceKind:       r.SourceKind,
			Target:           r.TargetID,
			TargetKind:       r.TargetKind,
			RelationKind:     r.RelationKind,
			PipelineID:       r.PipelineID,
			WorkflowID:       r.WorkflowID,
			NodeID:           r.NodeID,
			StepID:           r.StepID,
			EffectiveMarking: r.EffectiveMarking,
			Metadata:         r.Metadata,
		})
	}

	keys := make([]nodeKey, 0, len(graphNodes))
	for k := range graphNodes {
		keys = append(keys, k)
	}
	// Sort by (kind, id) — same canonical order as Rust's BTreeMap.
	sort.SliceStable(keys, func(i, j int) bool {
		if keys[i].kind != keys[j].kind {
			return keys[i].kind < keys[j].kind
		}
		return keys[i].id.String() < keys[j].id.String()
	})
	out := models.LineageGraph{
		Nodes: make([]models.LineageNode, 0, len(keys)),
		Edges: graphEdges,
	}
	for _, k := range keys {
		out.Nodes = append(out.Nodes, graphNodes[k])
	}
	return out
}
