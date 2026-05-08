package lineage

import (
	"context"
	"sort"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/queryrouter"
)

// LineageServingSnapshot mirrors the private Rust struct.
type LineageServingSnapshot struct {
	NodeOverlays map[models.NodeKey]models.LineageNodeRecord
	Relations    []models.LineageRelationRecord
}

// LoadConnectedSnapshot ports `load_connected_serving_snapshot`. Walks
// the runtime store starting at `root`, collecting every reachable
// relation (BFS), then loads the matching `lineage_nodes` overlays.
//
// The plan argument is currently asserted to select Cassandra (same
// as the Rust `debug_assert_eq!`); when Trino lands the caller can
// dispatch into a separate loader.
func LoadConnectedSnapshot(ctx context.Context, state *AppState, root models.NodeKey, plan queryrouter.QueryPlan) (LineageServingSnapshot, error) {
	if plan.SelectedSource != queryrouter.SourceCassandra {
		// The Rust uses `debug_assert_eq!`; we soften that to a logic
		// error in release builds since handlers may evolve before
		// Trino lands.
		return LineageServingSnapshot{}, ErrUnsupportedSource
	}
	visited := map[models.NodeKey]struct{}{}
	queued := map[models.NodeKey]struct{}{root: {}}
	queue := []models.NodeKey{root}
	keys := map[models.NodeKey]struct{}{root: {}}
	relations := map[uuid.UUID]models.LineageRelationRecord{}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		visited[current] = struct{}{}

		adj, err := state.Store.AdjacentRelations(ctx, current)
		if err != nil {
			return LineageServingSnapshot{}, err
		}
		for _, r := range adj {
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
			keys[sKey] = struct{}{}
			keys[tKey] = struct{}{}
			relations[r.ID] = r

			for _, neighbor := range []models.NodeKey{sKey, tKey} {
				if _, ok := visited[neighbor]; ok {
					continue
				}
				if _, ok := queued[neighbor]; ok {
					continue
				}
				queued[neighbor] = struct{}{}
				queue = append(queue, neighbor)
			}
		}
	}

	overlays, err := LoadNodeOverlays(ctx, state.DB, keys)
	if err != nil {
		return LineageServingSnapshot{}, err
	}

	relationList := make([]models.LineageRelationRecord, 0, len(relations))
	relationIDs := make([]uuid.UUID, 0, len(relations))
	for id := range relations {
		relationIDs = append(relationIDs, id)
	}
	sort.SliceStable(relationIDs, func(i, j int) bool {
		return relationIDs[i].String() < relationIDs[j].String()
	})
	for _, id := range relationIDs {
		relationList = append(relationList, relations[id])
	}

	return LineageServingSnapshot{NodeOverlays: overlays, Relations: relationList}, nil
}

// LoadCompleteSnapshot ports `load_complete_serving_snapshot`. Pulls
// every relation from the runtime store and loads the matching
// node overlays.
func LoadCompleteSnapshot(ctx context.Context, state *AppState, plan queryrouter.QueryPlan) (LineageServingSnapshot, error) {
	if plan.SelectedSource != queryrouter.SourceCassandra {
		return LineageServingSnapshot{}, ErrUnsupportedSource
	}
	relations, err := state.Store.AllRelations(ctx)
	if err != nil {
		return LineageServingSnapshot{}, err
	}
	keys := CollectRelationNodeKeys(relations)
	overlays, err := LoadNodeOverlays(ctx, state.DB, keys)
	if err != nil {
		return LineageServingSnapshot{}, err
	}
	return LineageServingSnapshot{NodeOverlays: overlays, Relations: relations}, nil
}

// HotPathQueryPlan ports `hot_path_query_plan` — the trigger logic
// always queries the hot path (Cassandra) regardless of caller
// preference.
func HotPathQueryPlan(kind queryrouter.QueryKind) queryrouter.QueryPlan {
	return queryrouter.Plan(kind, nil, false, false)
}

// ErrUnsupportedSource is returned when a snapshot loader is asked to
// serve from Trino while it's still unimplemented.
var ErrUnsupportedSource = errLineage("snapshot loader does not support Trino source yet")

type errLineage string

func (e errLineage) Error() string { return string(e) }
