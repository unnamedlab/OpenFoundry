package lineagestore

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/gocql/gocql"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/models"
)

// optUUID converts *uuid.UUID into a gocql-friendly nullable column.
// gocql encodes nil pointers as the CQL NULL value when used directly
// as a positional parameter.
func optUUID(p *uuid.UUID) any {
	if p == nil {
		return nil
	}
	return *p
}

func optStr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func ptrUUID(g *gocql.UUID) *uuid.UUID {
	if g == nil {
		return nil
	}
	v := uuid.UUID(*g)
	return &v
}

// scanRelationsInto reads every row from a relations-table iterator
// and inserts them keyed by relation id into `dst` (skipping
// duplicates that may arrive when the same relation comes back from
// the source AND the target index in [CassandraStore.AdjacentRelations]).
func scanRelationsInto(iter *gocql.Iter, dst map[uuid.UUID]models.LineageRelationRecord) error {
	var (
		relID, srcID, tgtID                       gocql.UUID
		srcKind, tgtKind, relKind, marking        string
		pipelineID, workflowID                    *gocql.UUID
		nodeID, stepID                            *string
		metadata                                  string
		createdAt                                 time.Time
	)
	for iter.Scan(&relID, &srcID, &srcKind, &tgtID, &tgtKind, &relKind,
		&pipelineID, &workflowID, &nodeID, &stepID, &marking, &metadata, &createdAt) {
		raw, err := metadataFromCassandra(metadata)
		if err != nil {
			return err
		}
		id := uuid.UUID(relID)
		if _, ok := dst[id]; ok {
			continue
		}
		dst[id] = models.LineageRelationRecord{
			ID:               id,
			SourceID:         uuid.UUID(srcID),
			SourceKind:       srcKind,
			TargetID:         uuid.UUID(tgtID),
			TargetKind:       tgtKind,
			RelationKind:     relKind,
			PipelineID:       ptrUUID(pipelineID),
			WorkflowID:       ptrUUID(workflowID),
			NodeID:           nodeID,
			StepID:           stepID,
			EffectiveMarking: marking,
			Metadata:         raw,
			CreatedAt:        createdAt,
		}
	}
	return iter.Close()
}

func metadataFromCassandra(raw string) (json.RawMessage, error) {
	if raw == "" {
		return json.RawMessage("{}"), nil
	}
	// Parse + re-marshal to validate. Avoids leaking malformed input
	// through to API responses.
	var holder any
	if err := json.Unmarshal([]byte(raw), &holder); err != nil {
		return nil, fmt.Errorf("invalid lineage metadata JSON: %w", err)
	}
	encoded, err := json.Marshal(holder)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

// millisToTime converts a Cassandra `timestamp` (epoch millis) to a
// time.Time. The Rust impl uses CqlTimestamp(i64); gocql normally
// hydrates `timestamp` columns as time.Time directly, but the
// DELETE-by-workflow query uses raw int64 to keep symmetry with the
// Rust scyllarust API.
func millisToTime(ms int64) time.Time {
	if ms == 0 {
		return time.Now().UTC()
	}
	return time.UnixMilli(ms).UTC()
}

func sortColumnEdgesDesc(edges []models.ColumnLineageEdge) {
	sort.SliceStable(edges, func(i, j int) bool {
		return edges[i].CreatedAt.After(edges[j].CreatedAt)
	})
}
