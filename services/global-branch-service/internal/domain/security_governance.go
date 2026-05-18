package domain

import (
	"fmt"
	"sort"
	"strings"
)

const BranchRestrictedViewMarkingWarning = "Branch restricted-view builds may expose a different row/object set when upstream branch markings differ from main; review marking diffs before approving or merging."

// BranchRestrictedViewPolicyChange describes a restricted-view policy authored on
// a global branch before it is merged to mainline runtime users.
type BranchRestrictedViewPolicyChange struct {
	BranchID              string
	RestrictedViewID      string
	ObjectTypeID          string
	BackingDatasetRID     string
	PolicyJSON            string
	MainPolicyJSON        string
	BranchMarkingIDs      []string
	MainMarkingIDs        []string
	ChangedBy             string
	ViewPermission        bool
	EditPermission        bool
	ApprovePermission     bool
	MergePermission       bool
	BuildRequested        bool
	PropagateToObjectType bool
}

type BranchRestrictedViewPlan struct {
	RestrictedViewID     string   `json:"restricted_view_id"`
	ObjectTypeID         string   `json:"object_type_id,omitempty"`
	RequiresBuild        bool     `json:"requires_build"`
	RequiresPropagation  bool     `json:"requires_propagation"`
	CanView              bool     `json:"can_view"`
	CanEdit              bool     `json:"can_edit"`
	CanApprove           bool     `json:"can_approve"`
	CanMerge             bool     `json:"can_merge"`
	PolicyChanged        bool     `json:"policy_changed"`
	UpstreamMarkingsDiff bool     `json:"upstream_markings_diff"`
	Warnings             []string `json:"warnings"`
	BlockingReasons      []string `json:"blocking_reasons"`
}

func PlanBranchRestrictedView(change BranchRestrictedViewPolicyChange) BranchRestrictedViewPlan {
	plan := BranchRestrictedViewPlan{
		RestrictedViewID:     strings.TrimSpace(change.RestrictedViewID),
		ObjectTypeID:         strings.TrimSpace(change.ObjectTypeID),
		RequiresBuild:        change.BuildRequested,
		RequiresPropagation:  change.PropagateToObjectType,
		CanView:              change.ViewPermission,
		CanEdit:              change.EditPermission,
		CanApprove:           change.ApprovePermission,
		CanMerge:             change.MergePermission,
		PolicyChanged:        strings.TrimSpace(change.PolicyJSON) != strings.TrimSpace(change.MainPolicyJSON),
		UpstreamMarkingsDiff: !sameStringSet(change.BranchMarkingIDs, change.MainMarkingIDs),
	}
	if plan.RestrictedViewID == "" {
		plan.BlockingReasons = append(plan.BlockingReasons, "restricted_view_id is required")
	}
	if !plan.CanView {
		plan.BlockingReasons = append(plan.BlockingReasons, "view restricted view permission required")
	}
	if plan.PolicyChanged && !plan.CanEdit {
		plan.BlockingReasons = append(plan.BlockingReasons, "edit restricted view policy permission required")
	}
	if plan.PolicyChanged && !plan.CanApprove {
		plan.BlockingReasons = append(plan.BlockingReasons, "approve branch restricted view policy permission required")
	}
	if plan.PolicyChanged && !plan.CanMerge {
		plan.BlockingReasons = append(plan.BlockingReasons, "merge branch security-resource permission required")
	}
	if plan.RequiresPropagation && plan.ObjectTypeID == "" {
		plan.BlockingReasons = append(plan.BlockingReasons, "object_type_id is required when propagating to indexed object types")
	}
	if plan.UpstreamMarkingsDiff {
		plan.Warnings = append(plan.Warnings, BranchRestrictedViewMarkingWarning)
	}
	return plan
}

type SecurityDiffKind string

const (
	SecurityDiffRole                 SecurityDiffKind = "role"
	SecurityDiffMarking              SecurityDiffKind = "marking"
	SecurityDiffRestrictedViewPolicy SecurityDiffKind = "restricted_view_policy"
	SecurityDiffProjectReference     SecurityDiffKind = "project_reference"
	SecurityDiffObjectSecurity       SecurityDiffKind = "object_security"
	SecurityDiffAction               SecurityDiffKind = "action"
	SecurityDiffEgressImport         SecurityDiffKind = "egress_import"
)

type SecurityDiffChange struct {
	Kind           SecurityDiffKind `json:"kind"`
	ResourceID     string           `json:"resource_id"`
	Before         string           `json:"before,omitempty"`
	After          string           `json:"after,omitempty"`
	ExpandsAccess  bool             `json:"expands_access"`
	ReducesControl bool             `json:"reduces_control"`
	BranchOnly     bool             `json:"branch_only"`
	Description    string           `json:"description,omitempty"`
}

type SecurityDiffReport struct {
	Changes                    []SecurityDiffChange `json:"changes"`
	RequiresApproval           bool                 `json:"requires_approval"`
	PreventMainlineRuntimeLeak bool                 `json:"prevent_mainline_runtime_leak"`
	ApprovalReasons            []string             `json:"approval_reasons"`
	Warnings                   []string             `json:"warnings"`
}

func BuildSecurityDiffReport(changes []SecurityDiffChange) SecurityDiffReport {
	report := SecurityDiffReport{Changes: append([]SecurityDiffChange(nil), changes...)}
	for _, change := range changes {
		label := strings.TrimSpace(string(change.Kind))
		if change.ResourceID != "" {
			label += ":" + change.ResourceID
		}
		if change.ExpandsAccess || change.ReducesControl {
			report.RequiresApproval = true
			if change.ExpandsAccess {
				report.ApprovalReasons = append(report.ApprovalReasons, fmt.Sprintf("%s expands access", label))
			}
			if change.ReducesControl {
				report.ApprovalReasons = append(report.ApprovalReasons, fmt.Sprintf("%s reduces security controls", label))
			}
		}
		if change.BranchOnly && change.ExpandsAccess {
			report.PreventMainlineRuntimeLeak = true
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s is a branch-only access expansion and must not affect mainline runtime users before merge", label))
		}
	}
	report.ApprovalReasons = normalizeStrings(report.ApprovalReasons)
	report.Warnings = normalizeStrings(report.Warnings)
	return report
}

func sameStringSet(a, b []string) bool {
	aa := normalizeStrings(a)
	bb := normalizeStrings(b)
	if len(aa) != len(bb) {
		return false
	}
	for i := range aa {
		if !strings.EqualFold(aa[i], bb[i]) {
			return false
		}
	}
	return true
}

func normalizeStrings(values []string) []string {
	seen := map[string]string{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; !ok {
			seen[key] = trimmed
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, seen[key])
	}
	return out
}
