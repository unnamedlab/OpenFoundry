package governanceops

import "strings"

type PolicyResourceKind string

const (
	PolicyProjectTemplate PolicyResourceKind = "project_template"
	PolicyRole            PolicyResourceKind = "role"
	PolicyMarkingGrant    PolicyResourceKind = "marking_grant"
	PolicyRestrictedView  PolicyResourceKind = "restricted_view_policy"
	PolicyAppAccess       PolicyResourceKind = "app_access"
	PolicyEgress          PolicyResourceKind = "egress"
	PolicyRetention       PolicyResourceKind = "retention"
	PolicyAuditMonitor    PolicyResourceKind = "audit_monitor"
)

type DeclarativePolicyResource struct {
	Kind               PolicyResourceKind `json:"kind"`
	Name               string             `json:"name"`
	DesiredHash        string             `json:"desired_hash"`
	LiveHash           string             `json:"live_hash,omitempty"`
	RequiresPermission string             `json:"requires_permission"`
}
type PolicyAsCodePlan struct {
	Version          string                      `json:"version"`
	Resources        []DeclarativePolicyResource `json:"resources"`
	DryRun           bool                        `json:"dry_run"`
	Approved         bool                        `json:"approved"`
	ActorPermissions []string                    `json:"actor_permissions"`
	AuditEnabled     bool                        `json:"audit_enabled"`
}
type PolicyDiff struct {
	Kind             PolicyResourceKind `json:"kind"`
	Name             string             `json:"name"`
	Change           string             `json:"change"`
	RequiresApproval bool               `json:"requires_approval"`
}
type PolicyAsCodeDecision struct {
	CanRollout      bool         `json:"can_rollout"`
	CanRollback     bool         `json:"can_rollback"`
	Diffs           []PolicyDiff `json:"diffs"`
	Drift           []PolicyDiff `json:"drift"`
	BlockingReasons []string     `json:"blocking_reasons"`
}

func EvaluatePolicyAsCode(plan PolicyAsCodePlan) PolicyAsCodeDecision {
	decision := PolicyAsCodeDecision{CanRollout: true, CanRollback: true}
	if strings.TrimSpace(plan.Version) == "" {
		decision.BlockingReasons = append(decision.BlockingReasons, "policy version is required")
	}
	if !plan.AuditEnabled {
		decision.BlockingReasons = append(decision.BlockingReasons, "policy-as-code changes require audit logging")
	}
	for _, r := range plan.Resources {
		change := "unchanged"
		if r.LiveHash == "" {
			change = "create"
		} else if r.LiveHash != r.DesiredHash {
			change = "update"
		}
		if change != "unchanged" {
			decision.Diffs = append(decision.Diffs, PolicyDiff{Kind: r.Kind, Name: r.Name, Change: change, RequiresApproval: true})
		}
		if r.LiveHash != "" && r.DesiredHash != "" && r.LiveHash != r.DesiredHash {
			decision.Drift = append(decision.Drift, PolicyDiff{Kind: r.Kind, Name: r.Name, Change: "drift", RequiresApproval: true})
		}
		if r.RequiresPermission != "" && !containsFold(plan.ActorPermissions, r.RequiresPermission) {
			decision.BlockingReasons = append(decision.BlockingReasons, "missing permission "+r.RequiresPermission+" for "+r.Name)
		}
	}
	if !plan.DryRun && len(decision.Diffs) > 0 && !plan.Approved {
		decision.BlockingReasons = append(decision.BlockingReasons, "rollout requires approval")
	}
	if len(decision.BlockingReasons) > 0 {
		decision.CanRollout = false
	}
	return decision
}
