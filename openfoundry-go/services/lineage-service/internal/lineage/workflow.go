package lineage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/models"
)

// SyncWorkflowLineage ports `sync_workflow_lineage`.
//
// Wipes existing relations attached to `workflow_id`, ensures all
// referenced datasets/pipelines/workflows exist, then upserts the
// workflow node + writes one relation per (workflow → target / source
// → workflow) link. Returns a string error to mirror the Rust
// `Result<(), String>` shape consumed by the handler.
func SyncWorkflowLineage(ctx context.Context, state *AppState, workflowID uuid.UUID, body models.WorkflowLineageSyncRequest) error {
	if body.Workflow.ID != workflowID {
		return errors.New("workflow lineage payload does not match route workflow_id")
	}
	if err := DeleteWorkflowLineage(ctx, state, workflowID); err != nil {
		return err
	}

	explicit := NormalizeMarking(body.Workflow.Marking)
	existing, err := GetNodeRecord(ctx, state.DB, workflowID, models.KindWorkflow)
	if err != nil {
		return err
	}

	for i := range body.Relations {
		if err := ensureExternalNodes(ctx, state, &body.Relations[i]); err != nil {
			return err
		}
	}

	values := []*string{}
	if explicit != nil {
		values = append(values, explicit)
	}
	if existing != nil {
		m := existing.Marking
		values = append(values, &m)
	}
	workflowMarking := MaxMarkings(values)

	for i := range body.Relations {
		r := &body.Relations[i]
		if r.TargetID == workflowID && r.TargetKind == "workflow" {
			if kind, ok := models.ParseNodeKind(r.SourceKind); ok {
				source, err := GetNodeRecord(ctx, state.DB, r.SourceID, kind)
				if err != nil {
					return err
				}
				values := []*string{strPtr(workflowMarking)}
				if source != nil {
					values = append(values, strPtr(source.Marking))
				}
				if normalized := NormalizeMarking(r.Marking); normalized != nil {
					values = append(values, normalized)
				}
				workflowMarking = MaxMarkings(values)
			}
		}
	}

	overlay, err := json.Marshal(map[string]any{
		"status":              body.Workflow.Status,
		"trigger_type":        body.Workflow.TriggerType,
		"source":              "workflow_service",
		"lineage_synced_at":   nowUTC(),
	})
	if err != nil {
		return err
	}
	var existingMeta json.RawMessage
	if existing != nil {
		existingMeta = existing.Metadata
	}
	merged := MergeMetadata(existingMeta, overlay, body.Workflow.Metadata)
	if _, err := UpsertNode(ctx, state.DB, workflowID, models.KindWorkflow, body.Workflow.Label, workflowMarking, merged); err != nil {
		return err
	}

	for i := range body.Relations {
		r := &body.Relations[i]
		sourceKind, ok := models.ParseNodeKind(r.SourceKind)
		if !ok {
			return fmt.Errorf("unsupported source kind '%s'", r.SourceKind)
		}
		targetKind, ok := models.ParseNodeKind(r.TargetKind)
		if !ok {
			return fmt.Errorf("unsupported target kind '%s'", r.TargetKind)
		}

		if r.SourceID == workflowID && sourceKind == models.KindWorkflow {
			target, err := GetNodeRecord(ctx, state.DB, r.TargetID, targetKind)
			if err != nil {
				return err
			}
			label := SyntheticLabel(targetKind, r.TargetID)
			if target != nil {
				label = target.Label
			}
			values := []*string{strPtr(workflowMarking)}
			if target != nil {
				values = append(values, strPtr(target.Marking))
			}
			if normalized := NormalizeMarking(r.Marking); normalized != nil {
				values = append(values, normalized)
			}
			targetMarking := MaxMarkings(values)
			overlay, err := json.Marshal(map[string]any{
				"propagated_from_workflow_id": workflowID,
				"propagated_at":               nowUTC(),
			})
			if err != nil {
				return err
			}
			var targetMeta json.RawMessage
			if target != nil {
				targetMeta = target.Metadata
			}
			if _, err := UpsertNode(ctx, state.DB, r.TargetID, targetKind, label, targetMarking, MergeMetadata(targetMeta, overlay, nil)); err != nil {
				return err
			}
		}

		stepValue := "trigger"
		if r.StepID != nil {
			stepValue = *r.StepID
		}
		producerKey := fmt.Sprintf("workflow:%s:%s:%s", workflowID, stepValue, r.RelationKind)

		wfID := workflowID
		if err := PersistRelation(ctx, state, RelationWriteInput{
			SourceID:        r.SourceID,
			SourceKind:      sourceKind,
			TargetID:        r.TargetID,
			TargetKind:      targetKind,
			RelationKind:    r.RelationKind,
			ProducerKey:     producerKey,
			WorkflowID:      &wfID,
			StepID:          r.StepID,
			ExplicitMarking: r.Marking,
			Metadata:        r.Metadata,
		}); err != nil {
			return err
		}
	}
	return nil
}

// DeleteWorkflowLineage ports `delete_workflow_lineage`. Wipes
// runtime-store entries first (so the canonical relation set goes
// away), then drops the lineage_nodes overlay row.
func DeleteWorkflowLineage(ctx context.Context, state *AppState, workflowID uuid.UUID) error {
	if err := state.Store.DeleteWorkflowRelations(ctx, workflowID); err != nil {
		return err
	}
	_, err := state.DB.Exec(ctx,
		`DELETE FROM lineage_nodes WHERE entity_id = $1 AND entity_kind = 'workflow'`,
		workflowID,
	)
	return err
}

func ensureExternalNodes(ctx context.Context, state *AppState, r *models.WorkflowLineageRelationInput) error {
	for _, ref := range []struct {
		id   uuid.UUID
		kind string
	}{
		{r.SourceID, r.SourceKind},
		{r.TargetID, r.TargetKind},
	} {
		nk, ok := models.ParseNodeKind(ref.kind)
		if !ok {
			continue
		}
		switch nk {
		case models.KindDataset:
			if _, err := EnsureDatasetSnapshot(ctx, state, ref.id); err != nil {
				return err
			}
		case models.KindPipeline:
			if _, err := EnsurePipelineSnapshot(ctx, state, ref.id); err != nil {
				return err
			}
		case models.KindWorkflow:
			// The Rust impl only inserts a placeholder for workflows
			// other than the route workflow. We approximate by always
			// writing a placeholder when missing — UpsertNode is a
			// no-op when the row exists (ON CONFLICT DO UPDATE).
			if err := EnsurePlaceholderWorkflow(ctx, state.DB, ref.id); err != nil {
				return err
			}
		}
	}
	return nil
}
