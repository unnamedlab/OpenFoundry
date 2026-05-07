// Package gdpr ports
// `services/audit-compliance-service/src/domain/gdpr.rs` 1:1.
//
// Pure helpers: builds the export payload + the erase-response from the
// in-memory event slice. The HTTP handler wraps both with the
// security filter from `domain/security`.
package gdpr

import (
	"fmt"
	"time"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// ExportPayload mirrors `export_payload`. Returns an excerpt capped at
// 12 events (Rust default).
func ExportPayload(request *models.GdprExportRequest, events []models.AuditEvent) models.GdprExportPayload {
	matching := matchingEvents(request.SubjectID, events)
	resources := make([]string, 0, len(matching))
	for i := range matching {
		resources = append(resources, fmt.Sprintf("%s:%s", matching[i].ResourceType, matching[i].ResourceID))
	}
	excerpt := matching
	if len(excerpt) > 12 {
		excerpt = excerpt[:12]
	}
	return models.GdprExportPayload{
		SubjectID:      request.SubjectID,
		GeneratedAt:    time.Now().UTC(),
		PortableFormat: request.EffectivePortableFormat(),
		EventCount:     len(matching),
		Resources:      resources,
		AuditExcerpt:   excerpt,
	}
}

// EraseResponse mirrors `erase_response`. Returns a synthesized
// preview — the actual deletion runner lives in lineage_deletion.
func EraseResponse(request *models.GdprEraseRequest, events []models.AuditEvent) models.GdprEraseResponse {
	matching := matchingEvents(request.SubjectID, events)
	resources := make([]string, 0, len(matching))
	for i := range matching {
		resources = append(resources, fmt.Sprintf("%s:%s", matching[i].ResourceType, matching[i].ResourceID))
	}
	now := time.Now().UTC()
	completed := now.Add(2 * time.Minute)
	status := "masked"
	if request.HardDelete {
		status = "scheduled-redaction"
	}
	return models.GdprEraseResponse{
		SubjectID:         request.SubjectID,
		RequestedAt:       now,
		CompletedAt:       &completed,
		Status:            status,
		MaskedEventCount:  len(matching),
		AffectedResources: resources,
		LegalHold:         request.LegalHold,
	}
}

func matchingEvents(subjectID string, events []models.AuditEvent) []models.AuditEvent {
	out := make([]models.AuditEvent, 0)
	for i := range events {
		if events[i].SubjectID != nil && *events[i].SubjectID == subjectID {
			out = append(out, events[i])
		}
	}
	return out
}
