// Package saga is the saga choreography helper for OpenFoundry's
// Foundry-pattern orchestration substrate (ADR-0037, Tarea 1.2).
// Mirrors libs/saga/src/{lib.rs,events.rs} 1:1 against pgx + the
// Go outbox helper.
//
// This file (`events.go`) ports the wire-format payloads + topic
// constants from libs/saga/src/events.rs. The runner emits these
// payloads through the transactional outbox (one row per state
// transition, surfaced on Kafka by Debezium's EventRouter SMT).
package saga

import (
	"encoding/json"

	"github.com/google/uuid"
)

// ─── Topic name constants ────────────────────────────────────────────

// SagaStepRequestedV1 is the topic the saga runtime subscribes to.
// Producers (HTTP handler in the owning service, k8s CronJob,
// upstream domain event) publish here to invite the runtime to
// start (or resume) a saga.
const SagaStepRequestedV1 = "saga.step.requested.v1"

// SagaStepCompletedV1Topic is the topic the runner emits per
// successful step.
const SagaStepCompletedV1Topic = "saga.step.completed.v1"

// SagaStepFailedV1Topic is the topic the runner emits per failed
// step.
const SagaStepFailedV1Topic = "saga.step.failed.v1"

// SagaStepCompensatedV1Topic is the topic the runner emits per
// successful LIFO compensation.
const SagaStepCompensatedV1Topic = "saga.step.compensated.v1"

// SagaCompensateV1 is the control-plane signal that asks an
// upstream saga to roll back. Out of scope for the FASE 6 single-
// saga path; declared here so the topic exists in helm and the
// wire-type is locked in.
const SagaCompensateV1 = "saga.compensate.v1"

// SagaCompletedV1Topic is the terminal topic the runner emits when
// every step succeeded and SagaRunner.Finish was called.
const SagaCompletedV1Topic = "saga.completed.v1"

// SagaAbortedV1Topic is the terminal topic the runner emits when
// the caller invoked SagaRunner.Abort. LIFO compensations have
// already run (and emitted their own SagaStepCompensatedV1Topic
// events) before this final terminal event.
const SagaAbortedV1Topic = "saga.aborted.v1"

// ─── Outbound payloads (emitted by the runner) ───────────────────────

// SagaStepCompletedV1 is the payload of saga.step.completed.v1.
type SagaStepCompletedV1 struct {
	SagaID uuid.UUID       `json:"saga_id"`
	Saga   string          `json:"saga"`
	Step   string          `json:"step"`
	Output json.RawMessage `json:"output"`
}

// SagaStepFailedV1 is the payload of saga.step.failed.v1.
type SagaStepFailedV1 struct {
	SagaID uuid.UUID `json:"saga_id"`
	Saga   string    `json:"saga"`
	Step   string    `json:"step"`
	Error  string    `json:"error"`
}

// SagaStepCompensatedV1 is the payload of saga.step.compensated.v1.
// Emitted once per previously-completed step that was successfully
// reversed.
type SagaStepCompensatedV1 struct {
	SagaID uuid.UUID `json:"saga_id"`
	Saga   string    `json:"saga"`
	Step   string    `json:"step"`
}

// SagaCompletedV1 is the payload of saga.completed.v1. Emitted
// once when every step succeeded and the caller invoked
// SagaRunner.Finish.
type SagaCompletedV1 struct {
	SagaID         uuid.UUID `json:"saga_id"`
	Saga           string    `json:"saga"`
	CompletedSteps []string  `json:"completed_steps"`
}

// SagaAbortedV1 is the payload of saga.aborted.v1. Emitted once
// when the caller invoked SagaRunner.Abort. LIFO compensations
// have already emitted their own saga.step.compensated.v1 events
// before this terminal event.
type SagaAbortedV1 struct {
	SagaID uuid.UUID `json:"saga_id"`
	Saga   string    `json:"saga"`
}

// ─── Inbound payloads (consumed by the runner) ───────────────────────

// SagaStepRequestedV1Payload is the payload of
// saga.step.requested.v1. The saga runtime subscribes to this
// topic; one record ⇒ one saga start (or resume). saga + input
// together are enough to drive the entire saga; the runtime's
// per-`saga` registry decides which step graph to dispatch.
type SagaStepRequestedV1Payload struct {
	// SagaID is the caller-chosen aggregate id. Producer retries
	// that re-publish the same SagaID are idempotent at three
	// layers: the processed_events dedup row, INSERT … ON CONFLICT
	// DO NOTHING on saga.state, and the runner's completed-steps
	// short-circuit. Recommended: deterministic UUIDv5 derived
	// from (saga, correlation_id) so the same producer trigger
	// always resolves to the same saga.
	SagaID uuid.UUID `json:"saga_id"`
	// Saga is the saga type, used by the runtime as the dispatch
	// key in its step-graph registry.
	Saga string `json:"saga"`
	// TenantID is the owning tenant — string to match the rest of
	// the platform (some producers pass UUIDs, others slugs); the
	// consumer normalises.
	TenantID string `json:"tenant_id"`
	// CorrelationID is the end-to-end correlation id propagated to
	// every effect call as `x-audit-correlation-id`. Producer
	// SHOULD set this to the id already attached to the inbound
	// HTTP request span; if absent the consumer generates a fresh
	// UUIDv7.
	CorrelationID uuid.UUID `json:"correlation_id"`
	// TriggeredBy is a free-form actor identifier (user UUID,
	// service principal, "system").
	TriggeredBy string `json:"triggered_by"`
	// Input is the free-form input payload — the saga's first step
	// is invoked with this value, the runtime is responsible for
	// parsing it into the step's typed Input. Defaults to JSON
	// null when omitted on the wire (matching Rust's
	// #[serde(default)]).
	Input json.RawMessage `json:"input,omitempty"`
}

// SagaCompensateRequestedV1 is the payload of saga.compensate.v1.
// Out-of-band compensation request (cross-saga rollback signal).
// Out of scope for FASE 6 single-saga path; the wire-type is
// locked in here so the topic + payload shape are stable for the
// FASE 6.4+ chaos / multi-saga work.
type SagaCompensateRequestedV1 struct {
	SagaID        uuid.UUID `json:"saga_id"`
	Saga          string    `json:"saga"`
	Reason        string    `json:"reason"`
	CorrelationID uuid.UUID `json:"correlation_id"`
}
