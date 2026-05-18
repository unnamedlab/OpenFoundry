package governanceops

import "fmt"

type UsageAttribution struct {
	UserID           string `json:"user_id,omitempty"`
	GroupID          string `json:"group_id,omitempty"`
	ProjectRID       string `json:"project_rid,omitempty"`
	WorkloadID       string `json:"workload_id,omitempty"`
	ServiceAccountID string `json:"service_account_id,omitempty"`
	OAuthAppID       string `json:"oauth_app_id,omitempty"`
	AuditExportID    string `json:"audit_export_id,omitempty"`
	RetentionJobID   string `json:"retention_job_id,omitempty"`
	EgressWorkloadID string `json:"egress_workload_id,omitempty"`
}
type UsageSample struct {
	UsageType   string           `json:"usage_type"`
	Quantity    float64          `json:"quantity"`
	Attribution UsageAttribution `json:"attribution"`
}
type UsageBudget struct {
	ID              string  `json:"id"`
	UsageType       string  `json:"usage_type"`
	Limit           float64 `json:"limit"`
	MonitorWindow   string  `json:"monitor_window"`
	FindingSeverity string  `json:"finding_severity"`
}
type UsageAnomaly struct {
	BudgetID  string   `json:"budget_id"`
	UsageType string   `json:"usage_type"`
	Quantity  float64  `json:"quantity"`
	Limit     float64  `json:"limit"`
	Finding   *Finding `json:"finding,omitempty"`
}

func EvaluateUsageBudgets(samples []UsageSample, budgets []UsageBudget) []UsageAnomaly {
	byType := map[string]float64{}
	for _, s := range samples {
		byType[s.UsageType] += s.Quantity
	}
	out := []UsageAnomaly{}
	for _, b := range budgets {
		if b.Limit <= 0 {
			continue
		}
		q := byType[b.UsageType]
		if q > b.Limit {
			f := NewFinding(FindingInput{Source: FindingAuditMonitor, Severity: firstNonEmpty(b.FindingSeverity, "medium"), Title: "Resource usage budget exceeded", Description: fmt.Sprintf("%s usage %.2f exceeded budget %.2f", b.UsageType, q, b.Limit), PolicyDecisionIDs: []string{b.ID}})
			out = append(out, UsageAnomaly{BudgetID: b.ID, UsageType: b.UsageType, Quantity: q, Limit: b.Limit, Finding: &f})
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
