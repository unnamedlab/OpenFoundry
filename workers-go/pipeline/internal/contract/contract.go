// Package contract pins cross-language constants. Mirrors the Rust
// side at libs/temporal-client/src/lib.rs::{task_queues,
// workflow_types}. Keep in lockstep.
package contract

const (
	TaskQueue                  = "openfoundry.pipeline"
	PipelineRun                = "PipelineRun"
	HeaderAuditCorrelation     = "x-audit-correlation-id"
	SearchAttrAuditCorrelation = "audit_correlation_id"
)

// PipelineRunInput mirrors libs/temporal-client::PipelineRunInput.
type PipelineRunInput struct {
	PipelineID string         `json:"pipeline_id"`
	TenantID   string         `json:"tenant_id"`
	Revision   string         `json:"revision,omitempty"`
	Parameters map[string]any `json:"parameters"`
}

// PipelineRunResult is what scheduled and ad-hoc runs return.
type PipelineRunResult struct {
	PipelineID string `json:"pipeline_id"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}
