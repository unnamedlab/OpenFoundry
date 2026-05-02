// Package contract pins the constants shared with libs/temporal-client.
package contract

const (
	TaskQueue              = "openfoundry.approvals"
	ApprovalRequest        = "ApprovalRequestWorkflow"
	SignalDecide           = "decide"
	HeaderAuditCorrelation = "x-audit-correlation-id"
)

// ApprovalRequestInput mirrors libs/temporal-client::ApprovalRequestInput.
type ApprovalRequestInput struct {
	RequestID     string         `json:"request_id"`
	TenantID      string         `json:"tenant_id"`
	Subject       string         `json:"subject"`
	ApproverSet   []string       `json:"approver_set"`
	ActionPayload map[string]any `json:"action_payload"`
}

// ApprovalDecision mirrors libs/temporal-client::ApprovalDecision.
// JSON shape is `{"outcome":"approve","approver":"…","comment":"…"}`.
type ApprovalDecision struct {
	Outcome  string `json:"outcome"` // "approve" | "reject"
	Approver string `json:"approver"`
	Comment  string `json:"comment,omitempty"`
}

type ApprovalResult struct {
	RequestID string `json:"request_id"`
	Decision  string `json:"decision"` // "approved" | "rejected" | "expired"
	Approver  string `json:"approver,omitempty"`
}
