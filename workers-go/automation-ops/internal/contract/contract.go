package contract

const (
	TaskQueue              = "openfoundry.automation-ops"
	AutomationOpsTask      = "AutomationOpsTask"
	HeaderAuditCorrelation = "x-audit-correlation-id"
)

type AutomationOpsInput struct {
	TaskID   string         `json:"task_id"`
	TenantID string         `json:"tenant_id"`
	TaskType string         `json:"task_type"`
	Payload  map[string]any `json:"payload"`
}

type AutomationOpsResult struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	RunID  string `json:"run_id,omitempty"`
	Error  string `json:"error,omitempty"`
}
