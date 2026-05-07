package models

import (
	"time"

	"github.com/google/uuid"
)

// EntityRecord mirrors the Rust fusion_base::models::cluster::EntityRecord.
//
// Rust attributes is `serde_json::Value` but is always an object in
// practice (built via `json!({...})`); we map it to `map[string]any`
// so callers can index with `record.Attributes[field]`.
type EntityRecord struct {
	RecordID    string         `json:"record_id"`
	Source      string         `json:"source"`
	ExternalID  string         `json:"external_id"`
	DisplayName string         `json:"display_name"`
	Confidence  float32        `json:"confidence"`
	Attributes  map[string]any `json:"attributes"`
}

// MatchEvidence mirrors fusion_base::models::cluster::MatchEvidence.
type MatchEvidence struct {
	LeftRecordID   string   `json:"left_record_id"`
	RightRecordID  string   `json:"right_record_id"`
	BlockingKey    string   `json:"blocking_key"`
	RuleScore      float32  `json:"rule_score"`
	MLScore        float32  `json:"ml_score"`
	FinalScore     float32  `json:"final_score"`
	Comparators    []string `json:"comparators"`
	Explanation    string   `json:"explanation"`
	RequiresReview bool     `json:"requires_review"`
}

// ReviewQueueItem mirrors fusion_base::models::cluster::ReviewQueueItem.
type ReviewQueueItem struct {
	ID                uuid.UUID `json:"id"`
	ClusterID         uuid.UUID `json:"cluster_id"`
	Status            string    `json:"status"`
	Severity          string    `json:"severity"`
	RecommendedAction string    `json:"recommended_action"`
	Rationale         []string  `json:"rationale"`
	AssignedTo        *string   `json:"assigned_to"`
	ReviewedBy        *string   `json:"reviewed_by"`
	Notes             string    `json:"notes"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// ResolvedCluster mirrors fusion_base::models::cluster::ResolvedCluster.
type ResolvedCluster struct {
	ID                      uuid.UUID       `json:"id"`
	JobID                   uuid.UUID       `json:"job_id"`
	ClusterKey              string          `json:"cluster_key"`
	Status                  string          `json:"status"`
	Records                 []EntityRecord  `json:"records"`
	Evidence                []MatchEvidence `json:"evidence"`
	ConfidenceScore         float32         `json:"confidence_score"`
	RequiresReview          bool            `json:"requires_review"`
	SuggestedGoldenRecordID *uuid.UUID      `json:"suggested_golden_record_id"`
	CreatedAt               time.Time       `json:"created_at"`
	UpdatedAt               time.Time       `json:"updated_at"`
}

// ClusterDetail mirrors fusion_base::models::cluster::ClusterDetail.
type ClusterDetail struct {
	Cluster      ResolvedCluster  `json:"cluster"`
	ReviewItem   *ReviewQueueItem `json:"review_item"`
	GoldenRecord *GoldenRecord    `json:"golden_record"`
}

// SubmitReviewRequest mirrors fusion_base::models::cluster::SubmitReviewRequest.
type SubmitReviewRequest struct {
	Decision   string  `json:"decision"`
	Notes      *string `json:"notes,omitempty"`
	ReviewedBy *string `json:"reviewed_by,omitempty"`
}
