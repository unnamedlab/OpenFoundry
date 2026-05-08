package lineage

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/models"
)

func TestBFSPathsFollowsDownstreamRelations(t *testing.T) {
	t.Parallel()
	source := uuid.New()
	pipeline := uuid.New()
	target := uuid.New()
	relations := []models.LineageRelationRecord{
		{
			ID: uuid.New(), SourceID: source, SourceKind: "dataset",
			TargetID: pipeline, TargetKind: "pipeline",
			RelationKind: "consumes", EffectiveMarking: "confidential",
			Metadata: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(),
		},
		{
			ID: uuid.New(), SourceID: pipeline, SourceKind: "pipeline",
			TargetID: target, TargetKind: "dataset",
			RelationKind: "produces", EffectiveMarking: "confidential",
			Metadata: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(),
		},
	}
	root := models.NodeKey{ID: source, Kind: models.KindDataset}
	paths := BFSPaths(root, relations, Outgoing)
	if _, ok := paths[models.NodeKey{ID: pipeline, Kind: models.KindPipeline}]; !ok {
		t.Fatal("pipeline reachable")
	}
	if _, ok := paths[models.NodeKey{ID: target, Kind: models.KindDataset}]; !ok {
		t.Fatal("target reachable")
	}
}

func TestBuildImpactItemsPreservesMarkingAndDistance(t *testing.T) {
	t.Parallel()
	source := uuid.New()
	target := uuid.New()
	root := models.NodeKey{ID: source, Kind: models.KindDataset}
	tgtKey := models.NodeKey{ID: target, Kind: models.KindWorkflow}
	r := models.LineageRelationRecord{
		ID: uuid.New(), SourceID: source, SourceKind: "dataset",
		TargetID: target, TargetKind: "workflow",
		RelationKind: "consumes", EffectiveMarking: "pii",
		Metadata:  json.RawMessage(`{}`),
		CreatedAt: time.Now().UTC(),
	}
	overlays := map[models.NodeKey]models.LineageNodeRecord{
		tgtKey: {
			EntityID: target, EntityKind: "workflow",
			Label:    "Review workflow",
			Marking:  "pii",
			Metadata: json.RawMessage(`{"status":"active"}`),
		},
	}
	paths := map[models.NodeKey][]uuid.UUID{
		root:   {},
		tgtKey: {r.ID},
	}
	items := BuildImpactItems(root, paths, overlays, []models.LineageRelationRecord{r})
	if len(items) != 1 {
		t.Fatalf("got %d items", len(items))
	}
	if items[0].Distance != 1 {
		t.Fatalf("distance %d", items[0].Distance)
	}
	if items[0].Marking != "pii" {
		t.Fatalf("marking %s", items[0].Marking)
	}
	if items[0].EffectiveMarking != "pii" {
		t.Fatalf("effective_marking %s", items[0].EffectiveMarking)
	}
	if !items[0].RequiresAcknowledgement {
		t.Fatal("expected requires_acknowledgement")
	}
}

func TestBuildImpactItemsElevatesEffectiveMarkingFromPath(t *testing.T) {
	t.Parallel()
	source := uuid.New()
	target := uuid.New()
	root := models.NodeKey{ID: source, Kind: models.KindDataset}
	tgtKey := models.NodeKey{ID: target, Kind: models.KindPipeline}
	r := models.LineageRelationRecord{
		ID: uuid.New(), SourceID: source, SourceKind: "dataset",
		TargetID: target, TargetKind: "pipeline",
		RelationKind: "consumes", EffectiveMarking: "confidential",
		Metadata:  json.RawMessage(`{}`),
		CreatedAt: time.Now().UTC(),
	}
	overlays := map[models.NodeKey]models.LineageNodeRecord{
		tgtKey: {
			EntityID: target, EntityKind: "pipeline",
			Label:    "Public pipeline",
			Marking:  "public",
			Metadata: json.RawMessage(`{"status":"active"}`),
		},
	}
	paths := map[models.NodeKey][]uuid.UUID{
		root:   {},
		tgtKey: {r.ID},
	}
	items := BuildImpactItems(root, paths, overlays, []models.LineageRelationRecord{r})
	if items[0].Marking != "public" {
		t.Fatalf("marking %s", items[0].Marking)
	}
	if items[0].EffectiveMarking != "confidential" {
		t.Fatalf("effective_marking %s", items[0].EffectiveMarking)
	}
	if !items[0].RequiresAcknowledgement {
		t.Fatal("expected requires_acknowledgement (confidential)")
	}
}

func TestBuildCandidateAcknowledgesSensitiveEffective(t *testing.T) {
	t.Parallel()
	target := uuid.New()
	item := models.LineageImpactItem{
		ID:                      target,
		Kind:                    "pipeline",
		Label:                   "Risk scorer",
		Distance:                2,
		Marking:                 "public",
		EffectiveMarking:        "pii",
		RequiresAcknowledgement: true,
		Metadata:                json.RawMessage(`{"status":"active"}`),
	}
	overlays := map[models.NodeKey]models.LineageNodeRecord{
		{ID: target, Kind: models.KindPipeline}: {
			EntityID:   target,
			EntityKind: "pipeline",
			Label:      "Risk scorer",
			Marking:    "public",
			Metadata:   json.RawMessage(`{"status":"active"}`),
		},
	}
	c := BuildCandidate(item, overlays)
	if !c.Triggerable {
		t.Fatal("expected triggerable (status=active)")
	}
	if !c.RequiresAcknowledgement {
		t.Fatal("requires_acknowledgement preserved")
	}
	if c.EffectiveMarking != "pii" {
		t.Fatalf("effective_marking got %s", c.EffectiveMarking)
	}
	if c.BlockedReason != nil {
		t.Fatalf("blocked_reason should be nil pre-trigger, got %v", *c.BlockedReason)
	}
}
