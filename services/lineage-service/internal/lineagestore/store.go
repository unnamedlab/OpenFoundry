// Package lineagestore ports `services/lineage-service/src/domain/lineage/tracker.rs`.
//
// Exposes a [Store] interface implemented by an in-memory store
// (RWMutex-backed) and a Cassandra/Scylla store (gocql-backed). The
// memory store is the unit-test fallback and is also what the binary
// uses when CASSANDRA_CONTACT_POINTS is unset — same fallback rule as
// Rust.
package lineagestore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/models"
)

// Store is the runtime lineage store interface. The Cassandra
// implementation persists every relation to multiple tables for the
// adjacency / workflow / column-lineage indexes; the memory
// implementation keeps a single map keyed by relation id.
type Store interface {
	RecordRelation(ctx context.Context, relation models.LineageRelationRecord) error
	AdjacentRelations(ctx context.Context, node models.NodeKey) ([]models.LineageRelationRecord, error)
	AllRelations(ctx context.Context) ([]models.LineageRelationRecord, error)
	DeleteWorkflowRelations(ctx context.Context, workflowID uuid.UUID) error
	DatasetColumnLineage(ctx context.Context, datasetID uuid.UUID) ([]models.ColumnLineageEdge, error)
}

// columnEdgeFromRelation extracts a [models.ColumnLineageEdge] from a
// runtime relation when both `source_column` and `target_column` are
// present in the metadata blob. Returns (zero, false) for non-column
// relations.
func columnEdgeFromRelation(r models.LineageRelationRecord) (models.ColumnLineageEdge, bool) {
	if len(r.Metadata) == 0 {
		return models.ColumnLineageEdge{}, false
	}
	var meta map[string]any
	if err := json.Unmarshal(r.Metadata, &meta); err != nil {
		return models.ColumnLineageEdge{}, false
	}
	src, ok := meta[models.MetadataSourceColumnKey].(string)
	if !ok {
		return models.ColumnLineageEdge{}, false
	}
	tgt, ok := meta[models.MetadataTargetColumnKey].(string)
	if !ok {
		return models.ColumnLineageEdge{}, false
	}
	return models.ColumnLineageEdge{
		ID:              r.ID,
		SourceDatasetID: r.SourceID,
		SourceColumn:    src,
		TargetDatasetID: r.TargetID,
		TargetColumn:    tgt,
		PipelineID:      r.PipelineID,
		NodeID:          r.NodeID,
		CreatedAt:       r.CreatedAt,
	}, true
}

// jsonForCassandra returns the canonical text encoding used by the
// Rust impl when persisting metadata to Cassandra.
func jsonForCassandra(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "{}", nil
	}
	// Round-trip to validate the JSON is well-formed; we keep the
	// canonical encoding instead of writing the input bytes verbatim
	// so trailing whitespace doesn't leak into the column.
	var holder any
	if err := json.Unmarshal(raw, &holder); err != nil {
		return "", fmt.Errorf("invalid lineage metadata JSON: %w", err)
	}
	encoded, err := json.Marshal(holder)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
