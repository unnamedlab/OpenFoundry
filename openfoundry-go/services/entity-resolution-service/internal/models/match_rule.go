// Package models holds the wire-format types for entity-resolution-service.
//
// All struct tags match the Rust serde encoding (snake_case fields,
// f32 → float32). The Rust source has no migrations directory; the
// schema lives in the Go-side embedded migrations now (since the Go
// port is canonical — Rust binary is `fn main(){}`).
package models

import (
	"time"

	"github.com/google/uuid"
)

// ListResponse is the {"data": [...]} envelope used by all list endpoints.
type ListResponse[T any] struct {
	Data []T `json:"data"`
}

// ErrorResponse is the {"error": "..."} envelope.
type ErrorResponse struct {
	Error string `json:"error"`
}

// FusionOverview is the canonical summary payload.
type FusionOverview struct {
	RuleCount               int64 `json:"rule_count"`
	ActiveJobCount          int64 `json:"active_job_count"`
	CompletedJobCount       int64 `json:"completed_job_count"`
	ClusterCount            int64 `json:"cluster_count"`
	PendingReviewCount      int64 `json:"pending_review_count"`
	GoldenRecordCount       int64 `json:"golden_record_count"`
	AutoMergedClusterCount  int64 `json:"auto_merged_cluster_count"`
}

// --- Match rule -----------------------------------------------------------

// BlockingStrategyConfig defaults: strategy_type="key-based",
// key_fields=["email","phone","display_name"], window_size=5, bucket_count=24.
type BlockingStrategyConfig struct {
	StrategyType string   `json:"strategy_type"`
	KeyFields    []string `json:"key_fields"`
	WindowSize   int32    `json:"window_size"`
	BucketCount  int32    `json:"bucket_count"`
}

// DefaultBlockingStrategy mirrors the Rust impl Default for BlockingStrategyConfig.
func DefaultBlockingStrategy() BlockingStrategyConfig {
	return BlockingStrategyConfig{
		StrategyType: "key-based",
		KeyFields:    []string{"email", "phone", "display_name"},
		WindowSize:   5,
		BucketCount:  24,
	}
}

type MatchCondition struct {
	Field      string  `json:"field"`
	Comparator string  `json:"comparator"`
	Weight     float32 `json:"weight"`
	Threshold  float32 `json:"threshold"`
	Required   bool    `json:"required"`
}

type MatchRule struct {
	ID                  uuid.UUID              `json:"id"`
	Name                string                 `json:"name"`
	Description         string                 `json:"description"`
	Status              string                 `json:"status"`
	EntityType          string                 `json:"entity_type"`
	BlockingStrategy    BlockingStrategyConfig `json:"blocking_strategy"`
	Conditions          []MatchCondition       `json:"conditions"`
	ReviewThreshold     float32                `json:"review_threshold"`
	AutoMergeThreshold  float32                `json:"auto_merge_threshold"`
	CreatedAt           time.Time              `json:"created_at"`
	UpdatedAt           time.Time              `json:"updated_at"`
}

type CreateMatchRuleRequest struct {
	Name               string                  `json:"name"`
	Description        *string                 `json:"description,omitempty"`
	Status             *string                 `json:"status,omitempty"`
	EntityType         *string                 `json:"entity_type,omitempty"`
	BlockingStrategy   *BlockingStrategyConfig `json:"blocking_strategy,omitempty"`
	Conditions         []MatchCondition        `json:"conditions"`
	ReviewThreshold    *float32                `json:"review_threshold,omitempty"`
	AutoMergeThreshold *float32                `json:"auto_merge_threshold,omitempty"`
}

type UpdateMatchRuleRequest struct {
	Name               *string                 `json:"name,omitempty"`
	Description        *string                 `json:"description,omitempty"`
	Status             *string                 `json:"status,omitempty"`
	EntityType         *string                 `json:"entity_type,omitempty"`
	BlockingStrategy   *BlockingStrategyConfig `json:"blocking_strategy,omitempty"`
	Conditions         *[]MatchCondition       `json:"conditions,omitempty"`
	ReviewThreshold    *float32                `json:"review_threshold,omitempty"`
	AutoMergeThreshold *float32                `json:"auto_merge_threshold,omitempty"`
}

// --- Merge strategy -------------------------------------------------------

type SurvivorshipRule struct {
	Field          string   `json:"field"`
	Strategy       string   `json:"strategy"`
	SourcePriority []string `json:"source_priority"`
	Fallback       string   `json:"fallback"`
}

type MergeStrategy struct {
	ID              uuid.UUID          `json:"id"`
	Name            string             `json:"name"`
	Description     string             `json:"description"`
	Status          string             `json:"status"`
	EntityType      string             `json:"entity_type"`
	DefaultStrategy string             `json:"default_strategy"`
	Rules           []SurvivorshipRule `json:"rules"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

type CreateMergeStrategyRequest struct {
	Name            string             `json:"name"`
	Description     *string            `json:"description,omitempty"`
	Status          *string            `json:"status,omitempty"`
	EntityType      *string            `json:"entity_type,omitempty"`
	DefaultStrategy *string            `json:"default_strategy,omitempty"`
	Rules           []SurvivorshipRule `json:"rules"`
}

type UpdateMergeStrategyRequest struct {
	Name            *string             `json:"name,omitempty"`
	Description     *string             `json:"description,omitempty"`
	Status          *string             `json:"status,omitempty"`
	EntityType      *string             `json:"entity_type,omitempty"`
	DefaultStrategy *string             `json:"default_strategy,omitempty"`
	Rules           *[]SurvivorshipRule `json:"rules,omitempty"`
}
