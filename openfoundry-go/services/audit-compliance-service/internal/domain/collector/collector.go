// Package collector ports
// `services/audit-compliance-service/src/domain/collector.rs` 1:1.
//
// Synthesises a five-row "collector catalogue" view over the in-memory
// event slice — same heuristics as the Rust impl (presence of any
// event from a given source service flips the row to `connected`,
// counts mod 5 surface as backlog depth, etc.).
package collector

import (
	"time"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// catalogEntry mirrors the Rust `(service, subject, connected)` triple.
type catalogEntry struct {
	service   string
	subject   string
	connected bool
}

var catalog = []catalogEntry{
	{"gateway", "of.audit.gateway", true},
	{"auth-service", "of.audit.auth", false},
	{"dataset-service", "of.audit.datasets", false},
	{"workflow-service", "of.audit.workflows", false},
	{"notification-alerting-service", "of.audit.notifications", false},
}

// CollectorCatalog mirrors `collector_catalog` 1:1.
func CollectorCatalog(events []models.AuditEvent) []models.CollectorStatus {
	now := time.Now().UTC()
	out := make([]models.CollectorStatus, 0, len(catalog))
	for _, entry := range catalog {
		var lastEventAt *time.Time
		eventCount := 0
		for i := range events {
			if events[i].SourceService != entry.service {
				continue
			}
			eventCount++
			t := events[i].OccurredAt
			if lastEventAt == nil || t.After(*lastEventAt) {
				ts := t
				lastEventAt = &ts
			}
		}
		backlog := int32(2)
		if eventCount > 0 {
			backlog = int32(eventCount % 5)
		}
		health := "warming"
		if eventCount > 0 {
			health = "healthy"
		}
		out = append(out, models.CollectorStatus{
			ServiceName:  entry.service,
			Subject:      entry.subject,
			Connected:    entry.connected || eventCount > 0,
			LastEventAt:  lastEventAt,
			BacklogDepth: backlog,
			Health:       health,
			NextPullAt:   now.Add(30 * time.Second),
		})
	}
	return out
}
