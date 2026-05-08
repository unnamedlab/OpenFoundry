// Package event holds the wire format of the FASE 5 / Tarea 5.3
// runtime's Kafka events plus the deterministic UUIDv5 helpers.
//
// derive_run_id and derive_condition_event_id make producer
// redeliveries idempotent: a retry of the same condition collapses
// onto the same automation_runs row AND the same processed_events
// dedup key.
package event

import (
	"encoding/json"

	"github.com/google/uuid"
)

// WorkflowAutomationNamespace is the UUIDv5 namespace for everything
// emitted by workflow-automation-service. Pinned forever — generated
// once with uuidgen and never rotated. DO NOT change without a
// fleet-wide schema dance.
var WorkflowAutomationNamespace = uuid.UUID{
	0x4e, 0x21, 0x9b, 0x1a, 0x57, 0x9c, 0x4b, 0x37,
	0xb6, 0x29, 0x6c, 0xfe, 0x6e, 0x47, 0xd1, 0x40,
}

// AutomateConditionV1 is the payload of automate.condition.v1.
// Shape preserves the legacy AutomationRunInput one-to-one so
// existing producers (manual UI button, webhook, lineage S2S call,
// pipeline-schedule cron / event fan-out) continue to work.
type AutomateConditionV1 struct {
	DefinitionID    uuid.UUID       `json:"definition_id"`
	TenantID        string          `json:"tenant_id"`
	CorrelationID   uuid.UUID       `json:"correlation_id"`
	TriggeredBy     string          `json:"triggered_by"`
	TriggerType     string          `json:"trigger_type"`
	TriggerPayload  json.RawMessage `json:"trigger_payload,omitempty"`
}

// AutomateOutcomeV1 is the payload of automate.outcome.v1. Mirrors
// AutomationRun terminal-state projection. Status is "completed"
// or "failed" ("cancelled" reserved for future cancel routes).
type AutomateOutcomeV1 struct {
	RunID          uuid.UUID       `json:"run_id"`
	DefinitionID   uuid.UUID       `json:"definition_id"`
	TenantID       string          `json:"tenant_id"`
	CorrelationID  uuid.UUID       `json:"correlation_id"`
	Status         string          `json:"status"`
	EffectResponse json.RawMessage `json:"effect_response,omitempty"`
	Error          *string         `json:"error,omitempty"`
	Attempts       uint32          `json:"attempts"`
}

// DeriveRunID returns the canonical automation_runs.id for a
// (definition_id, correlation_id) pair. Producer retries that
// re-publish the same condition collapse onto the same row.
func DeriveRunID(definitionID, correlationID uuid.UUID) uuid.UUID {
	var buf [32]byte
	copy(buf[:16], definitionID[:])
	copy(buf[16:], correlationID[:])
	return uuid.NewSHA1(WorkflowAutomationNamespace, buf[:])
}

// DeriveConditionEventID returns the per-condition event_id for the
// processed_events idempotency table. Defined separately from
// DeriveRunID so the two namespaces don't collide if the run id
// ever needs to be addressable as a Kafka event id by another consumer.
func DeriveConditionEventID(definitionID, correlationID uuid.UUID) uuid.UUID {
	var buf [33]byte
	copy(buf[:16], definitionID[:])
	copy(buf[16:32], correlationID[:])
	buf[32] = 'C' // distinguish from the run-id namespace
	return uuid.NewSHA1(WorkflowAutomationNamespace, buf[:])
}

// TenantUUIDFromStr is the best-effort coercion from the wire-format
// tenant_id string to the column-format tenant_id UUID. Producers
// today pass either a UUID (workspace id) or an opaque slug. UUID
// inputs round-trip; non-UUID inputs are mapped via UUIDv5 to a
// stable derivative so the consumer never crashes on malformed
// inputs and the row is always insertable.
func TenantUUIDFromStr(tenantID string) uuid.UUID {
	if parsed, err := uuid.Parse(tenantID); err == nil {
		return parsed
	}
	return uuid.NewSHA1(WorkflowAutomationNamespace, []byte(tenantID))
}
