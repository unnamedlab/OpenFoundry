package lineage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/queryrouter"
)

// ErrNoLineageGraphYet is returned when trigger_lineage_builds is
// invoked against a dataset with no impact analysis available.
var ErrNoLineageGraphYet = errors.New("dataset has no lineage graph yet")

// TriggerLineageBuilds ports `trigger_lineage_builds`. Filters the
// downstream candidates by depth + workflow include flag, decorates
// each candidate context with the lineage build metadata, then
// dispatches to the pipeline executor or the workflow service.
func TriggerLineageBuilds(ctx context.Context, state *AppState, datasetID, requestedBy uuid.UUID, request models.LineageBuildRequest) (*models.LineageBuildResult, error) {
	impactPtr, err := GetLineageImpactAnalysis(ctx, state, datasetID, HotPathQueryPlan(queryrouter.KindDatasetImpact))
	if err != nil {
		return nil, err
	}
	if impactPtr == nil {
		return nil, ErrNoLineageGraphYet
	}
	impact := *impactPtr

	maxDepth := 8
	if request.MaxDepth != nil {
		maxDepth = *request.MaxDepth
	}
	if maxDepth < 1 {
		maxDepth = 1
	}

	candidates := make([]models.LineageBuildCandidate, 0, len(impact.BuildCandidates))
	for _, c := range impact.BuildCandidates {
		if c.Distance > maxDepth {
			continue
		}
		if !request.IncludeWorkflows && c.Kind == models.KindWorkflow.String() {
			continue
		}
		candidates = append(candidates, c)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Distance != candidates[j].Distance {
			return candidates[i].Distance < candidates[j].Distance
		}
		if candidates[i].Kind != candidates[j].Kind {
			return candidates[i].Kind < candidates[j].Kind
		}
		return candidates[i].Label < candidates[j].Label
	})

	triggered := []models.LineageBuildTriggerResult{}
	skipped := []models.LineageBuildTriggerResult{}

	if !request.DryRun {
		for i := range candidates {
			c := &candidates[i]
			c.BlockedReason = nil

			if c.RequiresAcknowledgement && !request.AcknowledgeSensitiveLineage {
				message := fmt.Sprintf(
					"acknowledge sensitive lineage before triggering %s build with %s marking",
					c.Kind, c.EffectiveMarking)
				c.BlockedReason = &message
				msg := message
				skipped = append(skipped, models.LineageBuildTriggerResult{
					ID:      c.ID,
					Kind:    c.Kind,
					Label:   c.Label,
					RunID:   nil,
					Status:  "blocked",
					Message: &msg,
				})
				continue
			}

			if !c.Triggerable {
				reason := "candidate is not active"
				c.BlockedReason = &reason
				msg := reason
				skipped = append(skipped, models.LineageBuildTriggerResult{
					ID: c.ID, Kind: c.Kind, Label: c.Label, RunID: nil,
					Status:  "skipped",
					Message: &msg,
				})
				continue
			}

			buildContext, err := buildLineageContext(request.Context, datasetID, impact.PropagatedMarking, c, requestedBy, request.AcknowledgeSensitiveLineage)
			if err != nil {
				return nil, err
			}

			switch c.Kind {
			case models.KindPipeline.String():
				pipeline, err := LoadPipelineByID(ctx, state.DB, c.ID)
				if err != nil {
					return nil, err
				}
				if pipeline == nil {
					return nil, fmt.Errorf("pipeline %s not found", c.ID)
				}
				rb := requestedBy
				run, err := StartPipelineRun(ctx, state, pipeline, &rb, "lineage_build", "", nil, nil, 1, max1(state.DistributedPipelineWorkers), true, buildContext)
				if err != nil {
					msg := err.Error()
					skipped = append(skipped, models.LineageBuildTriggerResult{
						ID: c.ID, Kind: c.Kind, Label: c.Label, RunID: nil,
						Status: "failed", Message: &msg,
					})
					continue
				}
				runID := run.ID
				triggered = append(triggered, models.LineageBuildTriggerResult{
					ID: c.ID, Kind: c.Kind, Label: c.Label,
					RunID:  &runID,
					Status: run.Status,
				})
			case models.KindWorkflow.String():
				endpoint := fmt.Sprintf("%s/internal/workflows/%s/runs/lineage",
					strings.TrimRight(state.WorkflowServiceURL, "/"), c.ID)
				body, err := json.Marshal(models.InternalWorkflowLineageRunRequest{Context: buildContext})
				if err != nil {
					return nil, err
				}
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
				if err != nil {
					return nil, err
				}
				req.Header.Set("Content-Type", "application/json")
				resp, err := state.HTTPClient.Do(req)
				if err != nil {
					msg := err.Error()
					skipped = append(skipped, models.LineageBuildTriggerResult{
						ID: c.ID, Kind: c.Kind, Label: c.Label, RunID: nil,
						Status: "failed", Message: &msg,
					})
					continue
				}
				if resp.StatusCode >= 400 {
					var buf bytes.Buffer
					_, _ = buf.ReadFrom(resp.Body)
					_ = resp.Body.Close()
					msg := buf.String()
					skipped = append(skipped, models.LineageBuildTriggerResult{
						ID: c.ID, Kind: c.Kind, Label: c.Label, RunID: nil,
						Status: "failed", Message: &msg,
					})
					continue
				}
				var payload map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
					payload = map[string]any{}
				}
				_ = resp.Body.Close()
				var runID *uuid.UUID
				if raw, ok := payload["id"].(string); ok {
					if parsed, err := uuid.Parse(raw); err == nil {
						runID = &parsed
					}
				}
				status := "completed"
				if raw, ok := payload["status"].(string); ok {
					status = raw
				}
				triggered = append(triggered, models.LineageBuildTriggerResult{
					ID: c.ID, Kind: c.Kind, Label: c.Label,
					RunID: runID, Status: status,
				})
			}
		}
	}

	return &models.LineageBuildResult{
		Root:                         impact.Root,
		DryRun:                       request.DryRun,
		AcknowledgedSensitiveLineage: request.AcknowledgeSensitiveLineage,
		PropagatedMarking:            impact.PropagatedMarking,
		Candidates:                   candidates,
		Triggered:                    triggered,
		Skipped:                      skipped,
	}, nil
}

func buildLineageContext(base json.RawMessage, datasetID uuid.UUID, rootMarking string, candidate *models.LineageBuildCandidate, requestedBy uuid.UUID, ackSensitive bool) (json.RawMessage, error) {
	ensured := EnsureObject(base)
	return MergeIntoObject(ensured, "lineage_build", map[string]any{
		"root_dataset_id":                  datasetID,
		"root_marking":                     rootMarking,
		"candidate_id":                     candidate.ID,
		"candidate_kind":                   candidate.Kind,
		"candidate_marking":                candidate.Marking,
		"effective_marking":                candidate.EffectiveMarking,
		"requires_acknowledgement":         candidate.RequiresAcknowledgement,
		"acknowledged_sensitive_lineage":   ackSensitive,
		"requested_by":                     requestedBy,
		"requested_at":                     nowUTC(),
	})
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
