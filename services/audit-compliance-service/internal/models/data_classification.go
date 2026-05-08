// Data-classification + anomaly-alert types.
//
// Mirrors `services/audit-compliance-service/src/models/data_classification.rs`
// 1:1.
package models

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ClassificationLevel enumerates the three-tier data-classification
// vocabulary used everywhere in the audit ledger.
type ClassificationLevel string

const (
	ClassificationPublic       ClassificationLevel = "public"
	ClassificationConfidential ClassificationLevel = "confidential"
	ClassificationPii          ClassificationLevel = "pii"
)

// RequiresMasking returns true for confidential + pii (mirror of the
// Rust `requires_masking` helper).
func (c ClassificationLevel) RequiresMasking() bool {
	return c == ClassificationConfidential || c == ClassificationPii
}

// ParseClassificationLevel mirrors `ClassificationLevel::from_str`.
func ParseClassificationLevel(value string) (ClassificationLevel, error) {
	switch value {
	case "public":
		return ClassificationPublic, nil
	case "confidential":
		return ClassificationConfidential, nil
	case "pii":
		return ClassificationPii, nil
	default:
		return "", fmt.Errorf("unsupported classification level: %s", value)
	}
}

// AnomalyAlert mirrors the Rust struct.
type AnomalyAlert struct {
	ID                  uuid.UUID `json:"id"`
	Title               string    `json:"title"`
	Description         string    `json:"description"`
	Severity            string    `json:"severity"`
	DetectedAt          time.Time `json:"detected_at"`
	CorrelationKey      string    `json:"correlation_key"`
	LinkedEventID       uuid.UUID `json:"linked_event_id"`
	RecommendedAction   string    `json:"recommended_action"`
}

// ClassificationCatalogEntry mirrors the Rust struct of the same name.
type ClassificationCatalogEntry struct {
	Classification ClassificationLevel `json:"classification"`
	Description    string              `json:"description"`
}

// CollectorStatus is shared between the policy + events handlers; the
// Rust code keeps it under `models::policy::CollectorStatus`. We
// expose it here so both the read-model handler (`list_collectors`)
// and the overview handler can hold an instance without an extra
// alias.
type CollectorStatus struct {
	ServiceName  string     `json:"service_name"`
	Subject      string     `json:"subject"`
	Connected    bool       `json:"connected"`
	LastEventAt  *time.Time `json:"last_event_at"`
	BacklogDepth int32      `json:"backlog_depth"`
	Health       string     `json:"health"`
	NextPullAt   time.Time  `json:"next_pull_at"`
}
