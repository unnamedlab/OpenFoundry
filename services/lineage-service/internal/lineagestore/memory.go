package lineagestore

import (
	"context"
	"sort"
	"sync"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/models"
)

// MemoryStore is the unit-test / no-Cassandra fallback for [Store].
//
// Mirrors `MemoryLineageRuntimeStore` from tracker.rs: relations live
// in a single map keyed by relation id, guarded by a RWMutex.
type MemoryStore struct {
	mu        sync.RWMutex
	relations map[uuid.UUID]models.LineageRelationRecord
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{relations: map[uuid.UUID]models.LineageRelationRecord{}}
}

func (s *MemoryStore) RecordRelation(_ context.Context, relation models.LineageRelationRecord) error {
	s.mu.Lock()
	s.relations[relation.ID] = relation
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) AdjacentRelations(_ context.Context, node models.NodeKey) ([]models.LineageRelationRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.LineageRelationRecord, 0)
	for _, r := range s.relations {
		matches := (r.SourceID == node.ID && r.SourceKind == node.Kind.String()) ||
			(r.TargetID == node.ID && r.TargetKind == node.Kind.String())
		if matches {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *MemoryStore) AllRelations(_ context.Context) ([]models.LineageRelationRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.LineageRelationRecord, 0, len(s.relations))
	for _, r := range s.relations {
		out = append(out, r)
	}
	return out, nil
}

func (s *MemoryStore) DeleteWorkflowRelations(_ context.Context, workflowID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, r := range s.relations {
		if r.WorkflowID != nil && *r.WorkflowID == workflowID {
			delete(s.relations, id)
		}
	}
	return nil
}

func (s *MemoryStore) DatasetColumnLineage(_ context.Context, datasetID uuid.UUID) ([]models.ColumnLineageEdge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := []models.ColumnLineageEdge{}
	for _, r := range s.relations {
		if r.SourceID != datasetID && r.TargetID != datasetID {
			continue
		}
		edge, ok := columnEdgeFromRelation(r)
		if !ok {
			continue
		}
		out = append(out, edge)
	}
	// Sort by created_at desc — same as the Rust impl.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}
