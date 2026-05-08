package lineage

import (
	"encoding/json"
	"sort"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/models"
)

// Direction picks which way the BFS walks across the relation set.
type Direction int

const (
	Outgoing Direction = iota
	Incoming
)

// BFSPaths ports `bfs_paths`. Returns a map of every reachable node
// to the relation-id chain that connects it back to `root`.
func BFSPaths(root models.NodeKey, relations []models.LineageRelationRecord, dir Direction) map[models.NodeKey][]uuid.UUID {
	paths := map[models.NodeKey][]uuid.UUID{root: {}}
	queue := []models.NodeKey{root}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		currentPath := append([]uuid.UUID(nil), paths[current]...)

		for _, r := range relations {
			sk, ok := models.ParseNodeKind(r.SourceKind)
			if !ok {
				continue
			}
			tk, ok := models.ParseNodeKind(r.TargetKind)
			if !ok {
				continue
			}
			source := models.NodeKey{ID: r.SourceID, Kind: sk}
			target := models.NodeKey{ID: r.TargetID, Kind: tk}

			var next models.NodeKey
			var matched bool
			switch dir {
			case Outgoing:
				if source == current {
					next = target
					matched = true
				}
			case Incoming:
				if target == current {
					next = source
					matched = true
				}
			}
			if !matched {
				continue
			}

			if _, visited := paths[next]; visited {
				continue
			}
			nextPath := append([]uuid.UUID(nil), currentPath...)
			nextPath = append(nextPath, r.ID)
			paths[next] = nextPath
			queue = append(queue, next)
		}
	}
	return paths
}

// EffectivePathMarking ports `effective_path_marking`. Walks the
// relation chain and returns the strictest of (node marking, every
// hop's effective_marking).
func EffectivePathMarking(nodeMarking string, relationIDs []uuid.UUID, index map[uuid.UUID]models.LineageRelationRecord) string {
	values := make([]*string, 0, len(relationIDs)+1)
	nm := nodeMarking
	values = append(values, &nm)
	for _, id := range relationIDs {
		if r, ok := index[id]; ok {
			m := r.EffectiveMarking
			values = append(values, &m)
		}
	}
	return MaxMarkings(values)
}

// BuildImpactItems ports `build_impact_items`. Returns the items
// sorted by (distance, kind, label) — same as the Rust sort_by_key.
func BuildImpactItems(root models.NodeKey, paths map[models.NodeKey][]uuid.UUID, nodes map[models.NodeKey]models.LineageNodeRecord, relations []models.LineageRelationRecord) []models.LineageImpactItem {
	relationIndex := map[uuid.UUID]models.LineageRelationRecord{}
	for _, r := range relations {
		relationIndex[r.ID] = r
	}

	items := []models.LineageImpactItem{}
	for nodeKey, relationIDs := range paths {
		if nodeKey == root {
			continue
		}
		node, ok := BuildNodeView(nodeKey, nodes)
		if !ok {
			continue
		}
		effective := EffectivePathMarking(node.Marking, relationIDs, relationIndex)
		requiresAck := RequiresMarkingAcknowledgement(effective)

		mergedMeta := MergeMetadata(
			node.Metadata,
			mustMarshalMap(map[string]any{
				"node_marking":       node.Marking,
				"effective_marking":  effective,
			}),
			nil,
		)

		path := make([]models.LineagePathHop, 0, len(relationIDs))
		for _, rid := range relationIDs {
			r, ok := relationIndex[rid]
			if !ok {
				continue
			}
			path = append(path, models.LineagePathHop{
				SourceID:         r.SourceID,
				SourceKind:       r.SourceKind,
				TargetID:         r.TargetID,
				TargetKind:       r.TargetKind,
				RelationKind:     r.RelationKind,
				EffectiveMarking: r.EffectiveMarking,
			})
		}

		items = append(items, models.LineageImpactItem{
			ID:                      node.ID,
			Kind:                    node.Kind,
			Label:                   node.Label,
			Distance:                len(relationIDs),
			Marking:                 node.Marking,
			EffectiveMarking:        effective,
			RequiresAcknowledgement: requiresAck,
			Metadata:                mergedMeta,
			Path:                    path,
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Distance != items[j].Distance {
			return items[i].Distance < items[j].Distance
		}
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		return items[i].Label < items[j].Label
	})
	return items
}

// BuildCandidate ports `build_candidate`. The candidate is triggerable
// when the underlying node carries a `metadata.status == "active"`
// flag (so the caller knows whether to route to `executor::start_pipeline_run`).
func BuildCandidate(item models.LineageImpactItem, nodes map[models.NodeKey]models.LineageNodeRecord) models.LineageBuildCandidate {
	kind, ok := models.ParseNodeKind(item.Kind)
	if !ok {
		kind = models.KindDataset
	}
	key := models.NodeKey{ID: item.ID, Kind: kind}

	var status *string
	if record, ok := nodes[key]; ok {
		var meta map[string]any
		if err := json.Unmarshal(record.Metadata, &meta); err == nil {
			if s, ok := meta["status"].(string); ok {
				status = &s
			}
		}
	}
	triggerable := status != nil && *status == "active"

	return models.LineageBuildCandidate{
		ID:                      item.ID,
		Kind:                    item.Kind,
		Label:                   item.Label,
		Status:                  status,
		Distance:                item.Distance,
		Triggerable:             triggerable,
		Marking:                 item.Marking,
		EffectiveMarking:        item.EffectiveMarking,
		RequiresAcknowledgement: item.RequiresAcknowledgement,
		BlockedReason:           nil,
		Metadata:                item.Metadata,
	}
}

func mustMarshalMap(m map[string]any) json.RawMessage {
	b, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}
