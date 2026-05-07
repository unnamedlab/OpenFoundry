package models

import (
	"time"

	"github.com/google/uuid"
)

// ResolutionJobConfig mirrors fusion_base::models::job::ResolutionJobConfig.
type ResolutionJobConfig struct {
	SourceLabels             []string                `json:"source_labels"`
	RecordCount              int32                   `json:"record_count"`
	BlockingStrategyOverride *BlockingStrategyConfig `json:"blocking_strategy_override"`
	ReviewSamplingRate       float32                 `json:"review_sampling_rate"`
}

// DefaultResolutionJobConfig mirrors `impl Default for ResolutionJobConfig`.
func DefaultResolutionJobConfig() ResolutionJobConfig {
	return ResolutionJobConfig{
		SourceLabels:             []string{"crm", "erp", "support"},
		RecordCount:              12,
		BlockingStrategyOverride: nil,
		ReviewSamplingRate:       0.25,
	}
}

// FusionJobMetrics mirrors fusion_base::models::job::FusionJobMetrics.
type FusionJobMetrics struct {
	CandidatePairs    int32   `json:"candidate_pairs"`
	MatchedPairs      int32   `json:"matched_pairs"`
	ReviewPairs       int32   `json:"review_pairs"`
	ClusterCount      int32   `json:"cluster_count"`
	GoldenRecordCount int32   `json:"golden_record_count"`
	PrecisionEstimate float32 `json:"precision_estimate"`
	RecallEstimate    float32 `json:"recall_estimate"`
}

// FusionJob mirrors fusion_base::models::job::FusionJob.
type FusionJob struct {
	ID              uuid.UUID           `json:"id"`
	Name            string              `json:"name"`
	Description     string              `json:"description"`
	Status          string              `json:"status"`
	EntityType      string              `json:"entity_type"`
	MatchRuleID     uuid.UUID           `json:"match_rule_id"`
	MergeStrategyID uuid.UUID           `json:"merge_strategy_id"`
	Config          ResolutionJobConfig `json:"config"`
	Metrics         FusionJobMetrics    `json:"metrics"`
	LastRunSummary  string              `json:"last_run_summary"`
	LastRunAt       *time.Time          `json:"last_run_at"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

// CreateFusionJobRequest mirrors fusion_base::models::job::CreateFusionJobRequest.
type CreateFusionJobRequest struct {
	Name            string               `json:"name"`
	Description     *string              `json:"description,omitempty"`
	Status          *string              `json:"status,omitempty"`
	EntityType      *string              `json:"entity_type,omitempty"`
	MatchRuleID     uuid.UUID            `json:"match_rule_id"`
	MergeStrategyID uuid.UUID            `json:"merge_strategy_id"`
	Config          *ResolutionJobConfig `json:"config,omitempty"`
}

// RunResolutionJobResponse mirrors fusion_base::models::job::RunResolutionJobResponse.
type RunResolutionJobResponse struct {
	Job                FusionJob   `json:"job"`
	ClusterIDs         []uuid.UUID `json:"cluster_ids"`
	GoldenRecordIDs    []uuid.UUID `json:"golden_record_ids"`
	ReviewQueueItemIDs []uuid.UUID `json:"review_queue_item_ids"`
	ExecutedAt         time.Time   `json:"executed_at"`
}
