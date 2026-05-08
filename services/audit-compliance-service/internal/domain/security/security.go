// Package security ports
// `services/audit-compliance-service/src/domain/security.rs` 1:1.
//
// Per-claims access checks for audit events:
//
//   - admins always pass
//   - clearance must be ≥ classification rank
//   - the subject_id must be allow-listed by the session scope
//   - org-id allow-list is honoured via metadata.organization_id /
//     metadata.org_id; events with neither field default to "public-only"
//     for non-admin, non-guest sessions.
package security

import (
	"encoding/json"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// FilterEventsForClaims mirrors `filter_events_for_claims`.
func FilterEventsForClaims(events []models.AuditEvent, claims *authmw.Claims) []models.AuditEvent {
	out := make([]models.AuditEvent, 0, len(events))
	for i := range events {
		if CanAccessEvent(&events[i], claims) {
			out = append(out, events[i])
		}
	}
	return out
}

// CanAccessEvent mirrors `can_access_event` 1:1.
func CanAccessEvent(event *models.AuditEvent, claims *authmw.Claims) bool {
	if claims == nil {
		return false
	}
	if claims.HasRole("admin") {
		return true
	}
	if classificationRank(event.Classification) > clearanceRank(claims) {
		return false
	}
	if !allowsSubjectID(claims, event.SubjectID) {
		return false
	}
	allowed := claims.AllowedOrgIDs()
	if len(allowed) == 0 {
		return true
	}
	if orgID := metadataOrgID(event.Metadata); orgID != nil {
		for _, a := range allowed {
			if a == *orgID {
				return true
			}
		}
		return false
	}
	// No org_id in metadata — the Rust rule is "non-guest sessions can
	// see public events; guests see nothing".
	if claims.IsGuestSession() {
		return false
	}
	return event.Classification == string(models.ClassificationPublic)
}

// CanAccessSubject mirrors `can_access_subject`.
func CanAccessSubject(claims *authmw.Claims, subjectID string) bool {
	if claims == nil {
		return false
	}
	if claims.HasRole("admin") {
		return true
	}
	return allowsSubjectID(claims, &subjectID)
}

// allowsSubjectID approximates the Rust `claims.allows_subject_id`.
//
// The Go libs/auth-middleware `Claims` exposes
// `SessionScope.AllowedSubjectIDs`. When the scope is unset or empty
// the gate is open (matches the Rust impl for non-guest sessions);
// otherwise the subject must be in the allow-list. nil + empty
// subjects default to allowed (only the explicit allow-list filters).
func allowsSubjectID(claims *authmw.Claims, subjectID *string) bool {
	if claims == nil {
		return false
	}
	if claims.SessionScope == nil || len(claims.SessionScope.AllowedSubjectIDs) == 0 {
		return true
	}
	if subjectID == nil || *subjectID == "" {
		return true
	}
	for _, allowed := range claims.SessionScope.AllowedSubjectIDs {
		if allowed == *subjectID {
			return true
		}
	}
	return false
}

func clearanceRank(claims *authmw.Claims) uint8 {
	if claims == nil {
		return 0
	}
	value, ok := claims.ClassificationClearance()
	if !ok {
		return 0
	}
	return markingRank(value)
}

func classificationRank(value string) uint8 { return markingRank(value) }

func markingRank(value string) uint8 {
	switch value {
	case "public":
		return 0
	case "confidential":
		return 1
	case "pii":
		return 2
	default:
		return 0
	}
}

func metadataOrgID(metadata json.RawMessage) *uuid.UUID {
	if len(metadata) == 0 || string(metadata) == "null" {
		return nil
	}
	var holder map[string]json.RawMessage
	if err := json.Unmarshal(metadata, &holder); err != nil {
		return nil
	}
	for _, key := range []string{"organization_id", "org_id"} {
		raw, ok := holder[key]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		if id, err := uuid.Parse(s); err == nil {
			return &id
		}
	}
	return nil
}
