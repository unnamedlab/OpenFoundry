// Package automationoperations holds the saga substrate constants
// (legacy automation-operations-service, S8 merge per ADR-0030).
//
// Kafka topic constants are pinned here — the Rust source re-exports
// from libs/saga::events; the Go port inlines them since libs/saga-go
// is not yet ported. Drift from helm-provisioned values is locked
// by the topic_constants_match_helm_provisioning test.
package automationoperations

// Saga topics (FASE 6 / Tarea 6.2 helm-provisioned).
const (
	SagaStepRequestedV1   = "saga.step.requested.v1"
	SagaStepCompletedV1   = "saga.step.completed.v1"
	SagaStepFailedV1      = "saga.step.failed.v1"
	SagaStepCompensatedV1 = "saga.step.compensated.v1"
	SagaCompensateV1      = "saga.compensate.v1"
	SagaCompletedV1       = "saga.completed.v1"
	SagaAbortedV1         = "saga.aborted.v1"
)

// SagaConsumerGroup is the group id the saga step consumer uses.
// Verbatim from Rust src/automation_operations/domain/saga_consumer.rs.
const SagaConsumerGroup = "automation-operations-service"

// ProcessedEventsTable is the Postgres dedup table for the saga
// consumer's record-before-process rule.
const ProcessedEventsTable = "automation_operations.processed_events"
