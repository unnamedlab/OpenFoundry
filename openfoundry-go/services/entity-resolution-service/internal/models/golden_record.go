package models

import (
	"time"

	"github.com/google/uuid"
)

// GoldenRecordProvenance mirrors fusion_base::models::golden_record::GoldenRecordProvenance.
type GoldenRecordProvenance struct {
	Field      string `json:"field"`
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
	Strategy   string `json:"strategy"`
}

// GoldenRecord mirrors fusion_base::models::golden_record::GoldenRecord.
//
// Rust canonical_values is `serde_json::Value`. The merge layer always
// builds an object map; we expose it as `map[string]any` for the same
// ergonomics as Rust's `value.as_str()` / `value.get(field)` access.
type GoldenRecord struct {
	ID                 uuid.UUID                `json:"id"`
	ClusterID          uuid.UUID                `json:"cluster_id"`
	Title              string                   `json:"title"`
	CanonicalValues    map[string]any           `json:"canonical_values"`
	Provenance         []GoldenRecordProvenance `json:"provenance"`
	CompletenessScore  float32                  `json:"completeness_score"`
	ConfidenceScore    float32                  `json:"confidence_score"`
	Status             string                   `json:"status"`
	CreatedAt          time.Time                `json:"created_at"`
	UpdatedAt          time.Time                `json:"updated_at"`
}
