// Package contract pins the cross-language constants (task queue
// names, workflow type names, signal names) shared with the Rust
// client at libs/temporal-client/src/lib.rs. A mismatch between this
// file and the Rust side silently wedges workflow execution — keep
// the two in lockstep and reflect every change in both sides of the
// same PR.
package contract

const (
	// TaskQueue mirrors task_queues::WORKFLOW_AUTOMATION.
	TaskQueue = "openfoundry.workflow-automation"

	// WorkflowAutomationRun mirrors workflow_types::AUTOMATION_RUN.
	WorkflowAutomationRun = "WorkflowAutomationRun"

	// SearchAttrAuditCorrelation mirrors
	// StartWorkflowOptions::SEARCH_ATTR_AUDIT_CORRELATION.
	SearchAttrAuditCorrelation = "audit_correlation_id"

	// HeaderAuditCorrelation is the HTTP request header used when
	// activities call back into the Rust services. See ADR-0021
	// §Wire format for why activities use HTTP REST + JSON instead
	// of generated gRPC bindings from `proto/`.
	HeaderAuditCorrelation = "x-audit-correlation-id"
)

// AutomationRunInput mirrors libs/temporal-client::AutomationRunInput.
// JSON tags must match the Rust serde rename (snake_case keys).
type AutomationRunInput struct {
	RunID          string         `json:"run_id"`
	DefinitionID   string         `json:"definition_id"`
	TenantID       string         `json:"tenant_id"`
	TriggeredBy    string         `json:"triggered_by"`
	TriggerPayload map[string]any `json:"trigger_payload"`
}

// AutomationRunResult is what the workflow returns to the client when
// the run completes. Kept tiny on purpose — the detailed run record
// lives in the Rust service that owns the data.
type AutomationRunResult struct {
	RunID  string         `json:"run_id"`
	Status string         `json:"status"` // "completed" | "failed" | "cancelled"
	Error  string         `json:"error,omitempty"`
	Result map[string]any `json:"result,omitempty"`
}
