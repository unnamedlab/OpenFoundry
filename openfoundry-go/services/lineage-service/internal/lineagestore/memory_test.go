package lineagestore

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/models"
)

func TestMemoryStoreRoundTripsAdjacent(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	dataset := uuid.New()
	pipeline := uuid.New()
	rel := models.LineageRelationRecord{
		ID: uuid.New(), SourceID: dataset, SourceKind: "dataset",
		TargetID: pipeline, TargetKind: "pipeline",
		RelationKind: "consumes", EffectiveMarking: "public",
		PipelineID: &pipeline,
		NodeID:     ptrStr("node-a"),
		Metadata:   json.RawMessage(`{}`),
		CreatedAt:  time.Now().UTC(),
	}
	if err := store.RecordRelation(context.Background(), rel); err != nil {
		t.Fatalf("record: %v", err)
	}
	got, err := store.AdjacentRelations(context.Background(), models.NodeKey{ID: dataset, Kind: models.KindDataset})
	if err != nil {
		t.Fatalf("adj: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].ID != rel.ID {
		t.Fatalf("id %s", got[0].ID)
	}
}

func TestMemoryStoreColumnLineageDeduplicates(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	source := uuid.New()
	target := uuid.New()
	now := time.Now().UTC()
	mk := func(srcCol, tgtCol string, ts time.Time) models.LineageRelationRecord {
		meta, _ := json.Marshal(map[string]string{
			models.MetadataSourceColumnKey: srcCol,
			models.MetadataTargetColumnKey: tgtCol,
		})
		return models.LineageRelationRecord{
			ID: uuid.New(), SourceID: source, SourceKind: "dataset",
			TargetID: target, TargetKind: "dataset",
			RelationKind:     "column_derives",
			EffectiveMarking: "public",
			Metadata:         meta,
			CreatedAt:        ts,
		}
	}
	if err := store.RecordRelation(context.Background(), mk("a", "b", now)); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := store.RecordRelation(context.Background(), mk("c", "d", now.Add(-time.Hour))); err != nil {
		t.Fatalf("record: %v", err)
	}
	edges, err := store.DatasetColumnLineage(context.Background(), source)
	if err != nil {
		t.Fatalf("col: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("got %d edges", len(edges))
	}
	// Sorted by created_at desc.
	if !edges[0].CreatedAt.After(edges[1].CreatedAt) {
		t.Fatalf("expected desc order, got %v then %v", edges[0].CreatedAt, edges[1].CreatedAt)
	}
}

func TestMemoryStoreDeleteWorkflowRelations(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	wf := uuid.New()
	other := uuid.New()
	for i, workflow := range []*uuid.UUID{&wf, &wf, &other} {
		rel := models.LineageRelationRecord{
			ID: uuid.New(), SourceID: uuid.New(), SourceKind: "dataset",
			TargetID: uuid.New(), TargetKind: "workflow",
			RelationKind:     fmt.Sprintf("rel-%d", i),
			EffectiveMarking: "public",
			WorkflowID:       workflow,
			Metadata:         json.RawMessage(`{}`),
			CreatedAt:        time.Now().UTC(),
		}
		if err := store.RecordRelation(context.Background(), rel); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	if err := store.DeleteWorkflowRelations(context.Background(), wf); err != nil {
		t.Fatalf("delete: %v", err)
	}
	left, err := store.AllRelations(context.Background())
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(left) != 1 {
		t.Fatalf("expected 1 surviving relation, got %d", len(left))
	}
	if left[0].WorkflowID == nil || *left[0].WorkflowID != other {
		t.Fatalf("survivor wrong workflow: %v", left[0].WorkflowID)
	}
}

func ptrStr(s string) *string { return &s }
