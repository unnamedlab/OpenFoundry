// Package alerting ports
// `services/audit-compliance-service/src/domain/alerting.rs` 1:1.
//
// Detects in-memory anomalies from the audit-event stream — does NOT
// touch the database. Same trim-to-8 cap as the Rust impl so the
// dashboard stays responsive.
package alerting

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// DetectAnomalies mirrors `detect_anomalies`. Critical events and
// events labelled `contains-sensitive-data` raise a same-shaped
// AnomalyAlert; the Rust impl truncates the result to 8.
func DetectAnomalies(events []models.AuditEvent) []models.AnomalyAlert {
	alerts := make([]models.AnomalyAlert, 0, len(events))
	for i := range events {
		event := &events[i]
		labels, _ := decodeLabels(event.Labels)
		hasSensitiveLabel := false
		for _, label := range labels {
			if label == "contains-sensitive-data" {
				hasSensitiveLabel = true
				break
			}
		}
		severity := models.AuditSeverity(event.Severity)
		if !severity.IsCritical() && !hasSensitiveLabel {
			continue
		}
		severityLabel := "elevated"
		if severity.IsCritical() {
			severityLabel = "critical"
		}
		alerts = append(alerts, models.AnomalyAlert{
			ID:                uuid.New(),
			Title:             fmt.Sprintf("Sensitive access pattern: %s", event.Action),
			Description:       fmt.Sprintf("%s touched %s:%s from %s", event.Actor, event.ResourceType, event.ResourceID, event.SourceService),
			Severity:          severityLabel,
			DetectedAt:        event.IngestedAt,
			CorrelationKey:    fmt.Sprintf("%s:%s", event.SourceService, event.Action),
			LinkedEventID:     event.ID,
			RecommendedAction: "Review actor session, confirm data minimization, and verify policy coverage.",
		})
	}
	if len(alerts) > 8 {
		alerts = alerts[:8]
	}
	return alerts
}

func decodeLabels(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
