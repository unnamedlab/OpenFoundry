// Package lineage ports
// `services/workflow-automation-service/src/domain/lineage.rs` 1:1.
//
// Builds a workflow lineage snapshot from a WorkflowDefinition + its
// parsed steps and POSTs / DELETEs it against pipeline-service's
// `/internal/lineage/workflows` endpoint. Same payload + endpoint
// shape as the Rust impl.
package lineage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/models"
)

// WorkflowLineageSyncRequest mirrors the Rust struct. Same JSON shape.
type WorkflowLineageSyncRequest struct {
	Workflow  WorkflowLineageNode       `json:"workflow"`
	Relations []WorkflowLineageRelation `json:"relations"`
}

// WorkflowLineageNode mirrors the Rust struct. Marking is *string so
// `null` can survive the round-trip when the workflow has no marking.
type WorkflowLineageNode struct {
	ID          uuid.UUID       `json:"id"`
	Label       string          `json:"label"`
	Status      string          `json:"status"`
	TriggerType string          `json:"trigger_type"`
	Marking     *string         `json:"marking"`
	Metadata    json.RawMessage `json:"metadata"`
}

// WorkflowLineageRelation mirrors the Rust struct.
type WorkflowLineageRelation struct {
	SourceID     uuid.UUID       `json:"source_id"`
	SourceKind   string          `json:"source_kind"`
	TargetID     uuid.UUID       `json:"target_id"`
	TargetKind   string          `json:"target_kind"`
	RelationKind string          `json:"relation_kind"`
	StepID       *string         `json:"step_id"`
	Metadata     json.RawMessage `json:"metadata"`
	Marking      *string         `json:"marking"`
}

var (
	incomingDatasetKeys = []string{
		"dataset_id", "dataset_ids",
		"input_dataset_id", "input_dataset_ids",
		"source_dataset_id", "source_dataset_ids",
	}
	outgoingDatasetKeys = []string{
		"output_dataset_id", "output_dataset_ids",
		"target_dataset_id", "target_dataset_ids",
	}
	pipelineKeys = []string{
		"pipeline_id", "pipeline_ids",
		"target_pipeline_id", "target_pipeline_ids",
	}
	workflowKeys = []string{
		"workflow_id", "workflow_ids",
		"target_workflow_id", "target_workflow_ids",
	}
)

// BuildSnapshot ports `build_workflow_lineage_snapshot`. Errors when
// the workflow's `steps` JSON does not parse (same surface as Rust).
func BuildSnapshot(workflow *models.WorkflowDefinition) (*WorkflowLineageSyncRequest, error) {
	steps, err := workflow.ParsedSteps()
	if err != nil {
		return nil, err
	}
	workflowMarking := extractMarking(workflow.TriggerConfig)

	relations := []WorkflowLineageRelation{}
	relations = append(relations, extractRelations(workflow.ID, nil, workflow.TriggerConfig, "triggers")...)
	for i := range steps {
		stepID := steps[i].ID
		relations = append(relations,
			extractRelations(workflow.ID, &stepID, steps[i].Config, steps[i].StepType)...)
	}

	metadata, err := json.Marshal(map[string]any{
		"description":       workflow.Description,
		"status":            workflow.Status,
		"trigger_type":      workflow.TriggerType,
		"next_run_at":       workflow.NextRunAt,
		"last_triggered_at": workflow.LastTriggeredAt,
	})
	if err != nil {
		return nil, err
	}

	return &WorkflowLineageSyncRequest{
		Workflow: WorkflowLineageNode{
			ID:          workflow.ID,
			Label:       workflow.Name,
			Status:      workflow.Status,
			TriggerType: workflow.TriggerType,
			Marking:     workflowMarking,
			Metadata:    metadata,
		},
		Relations: relations,
	}, nil
}

// SyncWorkflow ports `sync_workflow_lineage`. POST to pipeline-service.
func SyncWorkflow(ctx context.Context, client *http.Client, pipelineServiceURL string, workflow *models.WorkflowDefinition) error {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	payload, err := BuildSnapshot(workflow)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/internal/lineage/workflows/%s/sync",
		strings.TrimRight(pipelineServiceURL, "/"), workflow.ID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to reach pipeline-service for workflow lineage sync: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach pipeline-service for workflow lineage sync: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("pipeline-service rejected workflow lineage sync: status=%d", resp.StatusCode)
	}
	return nil
}

// DeleteWorkflow ports `delete_workflow_lineage`. DELETE to pipeline-service.
func DeleteWorkflow(ctx context.Context, client *http.Client, pipelineServiceURL string, workflowID uuid.UUID) error {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	endpoint := fmt.Sprintf("%s/internal/lineage/workflows/%s",
		strings.TrimRight(pipelineServiceURL, "/"), workflowID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to reach pipeline-service for workflow lineage delete: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach pipeline-service for workflow lineage delete: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("pipeline-service rejected workflow lineage delete: status=%d", resp.StatusCode)
	}
	return nil
}

func extractRelations(workflowID uuid.UUID, stepID *string, config json.RawMessage, relationHint string) []WorkflowLineageRelation {
	relations := []WorkflowLineageRelation{}
	explicit := extractMarking(config)
	lineageRoot := getJSONField(config, "lineage")

	for _, datasetID := range extractUUIDValues(getJSONField(getJSONField(lineageRoot, "inputs"), "datasets"), incomingDatasetKeys, config) {
		relations = append(relations, WorkflowLineageRelation{
			SourceID:     datasetID,
			SourceKind:   "dataset",
			TargetID:     workflowID,
			TargetKind:   "workflow",
			RelationKind: normalizeRelationKind(relationHint, "consumes"),
			StepID:       stepID,
			Metadata:     scopeMeta(stepID),
			Marking:      cloneStr(explicit),
		})
	}
	for _, datasetID := range extractUUIDValues(getJSONField(getJSONField(lineageRoot, "outputs"), "datasets"), outgoingDatasetKeys, config) {
		relations = append(relations, WorkflowLineageRelation{
			SourceID:     workflowID,
			SourceKind:   "workflow",
			TargetID:     datasetID,
			TargetKind:   "dataset",
			RelationKind: "produces",
			StepID:       stepID,
			Metadata:     scopeMeta(stepID),
			Marking:      cloneStr(explicit),
		})
	}
	for _, pipelineID := range extractUUIDValues(getJSONField(getJSONField(lineageRoot, "outputs"), "pipelines"), pipelineKeys, config) {
		relations = append(relations, WorkflowLineageRelation{
			SourceID:     workflowID,
			SourceKind:   "workflow",
			TargetID:     pipelineID,
			TargetKind:   "pipeline",
			RelationKind: "triggers",
			StepID:       stepID,
			Metadata:     scopeMeta(stepID),
			Marking:      cloneStr(explicit),
		})
	}
	for _, targetWorkflowID := range extractUUIDValues(getJSONField(getJSONField(lineageRoot, "outputs"), "workflows"), workflowKeys, config) {
		if targetWorkflowID == workflowID {
			continue
		}
		relations = append(relations, WorkflowLineageRelation{
			SourceID:     workflowID,
			SourceKind:   "workflow",
			TargetID:     targetWorkflowID,
			TargetKind:   "workflow",
			RelationKind: "triggers",
			StepID:       stepID,
			Metadata:     scopeMeta(stepID),
			Marking:      cloneStr(explicit),
		})
	}
	for _, pipelineID := range extractUUIDValues(getJSONField(getJSONField(lineageRoot, "inputs"), "pipelines"), pipelineKeys, config) {
		relations = append(relations, WorkflowLineageRelation{
			SourceID:     pipelineID,
			SourceKind:   "pipeline",
			TargetID:     workflowID,
			TargetKind:   "workflow",
			RelationKind: "triggers",
			StepID:       stepID,
			Metadata:     scopeMeta(stepID),
			Marking:      cloneStr(explicit),
		})
	}

	return relations
}

func extractUUIDValues(explicit json.RawMessage, fallbackKeys []string, config json.RawMessage) []uuid.UUID {
	seen := map[uuid.UUID]struct{}{}
	if len(explicit) > 0 {
		collectUUIDs(explicit, seen)
	}
	for _, key := range fallbackKeys {
		if raw := getJSONField(config, key); len(raw) > 0 {
			collectUUIDs(raw, seen)
		}
	}
	out := make([]uuid.UUID, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	// Sort for deterministic ordering — same effect as Rust BTreeSet.
	sort.Slice(out, func(i, j int) bool {
		return bytes.Compare(out[i][:], out[j][:]) < 0
	})
	return out
}

func collectUUIDs(value json.RawMessage, target map[uuid.UUID]struct{}) {
	var s string
	if err := json.Unmarshal(value, &s); err == nil {
		if id, err := uuid.Parse(s); err == nil {
			target[id] = struct{}{}
		}
		return
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(value, &arr); err == nil {
		for _, item := range arr {
			collectUUIDs(item, target)
		}
	}
}

func extractMarking(config json.RawMessage) *string {
	lineageRoot := getJSONField(config, "lineage")
	if marking := readJSONString(getJSONField(lineageRoot, "marking")); marking != nil {
		return marking
	}
	if marking := readJSONString(getJSONField(config, "marking")); marking != nil {
		return marking
	}
	return nil
}

func normalizeRelationKind(hint, fallback string) string {
	switch hint {
	case "action", "manual", "event", "webhook", "cron":
		return fallback
	case "approval":
		return "gates"
	case "notification":
		return "notifies"
	default:
		return fallback
	}
}

func stepScope(stepID *string) string {
	if stepID != nil {
		return "step"
	}
	return "trigger"
}

func scopeMeta(stepID *string) json.RawMessage {
	out, _ := json.Marshal(map[string]string{"scope": stepScope(stepID)})
	return out
}

func getJSONField(raw json.RawMessage, key string) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	return obj[key]
}

func readJSONString(raw json.RawMessage) *string {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil
	}
	out := s
	return &out
}

func cloneStr(s *string) *string {
	if s == nil {
		return nil
	}
	c := *s
	return &c
}
