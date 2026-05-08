// Package topics holds the FASE 5 / Tarea 5.3 Foundry-pattern topic
// constants for workflow-automation-service. Pinned as constants so
// drift from the helm-provisioned values is a compile-time error.
//
// Sub-domain topics for saga and approvals live in
// internal/automationoperations and internal/approvals respectively.
package topics

// AutomateConditionV1 is the input topic. The condition consumer
// subscribes here. Payload is JSON-serialised event.AutomateConditionV1.
//
// Replaces the Temporal task queue openfoundry.workflow-automation.
const AutomateConditionV1 = "automate.condition.v1"

// AutomateOutcomeV1 is the output (control plane) topic. One record
// per terminal AutomationRun transition (completed/failed). Payload
// is JSON-serialised event.AutomateOutcomeV1. Downstream consumers:
// notification-alerting-service (UI live feed),
// audit-compliance-service (audit trail), lineage-service (E2E
// lineage stitching).
const AutomateOutcomeV1 = "automate.outcome.v1"
