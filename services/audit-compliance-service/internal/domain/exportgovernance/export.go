package exportgovernance

import "strings"

type ExportKind string

const (
	ExportCSV       ExportKind = "csv"
	ExportDataset   ExportKind = "dataset"
	ExportMedia     ExportKind = "media"
	ExportDashboard ExportKind = "dashboard"
	ExportBI        ExportKind = "bi"
	ExportNotepad   ExportKind = "notepad"
	ExportModel     ExportKind = "model"
	ExportObjectSet ExportKind = "object_set"
	ExportAPI       ExportKind = "api"
)

type ExportPolicy struct {
	CheckpointRequiredKinds    []ExportKind
	JustificationRequiredKinds []ExportKind
	BlockedMarkings            []string
	AllowedDestinations        []string
}

type ExportRequest struct {
	Kind                     ExportKind
	ResourceRID              string
	Filters                  map[string]string
	Parameters               map[string]string
	Branch                   string
	Markings                 []string
	RestrictedViewPolicyID   string
	UserID                   string
	Destination              string
	Justification            string
	MarkingAllowed           bool
	RestrictedViewAllowed    bool
	ObjectSecurityAllowed    bool
	ApplicationPolicyAllowed bool
	EgressPolicyAllowed      bool
}

type ExportProvenance struct {
	Kind                   ExportKind        `json:"kind"`
	ResourceRID            string            `json:"resource_rid"`
	Filters                map[string]string `json:"filters"`
	Parameters             map[string]string `json:"parameters"`
	Branch                 string            `json:"branch,omitempty"`
	Markings               []string          `json:"markings"`
	RestrictedViewPolicyID string            `json:"restricted_view_policy_id,omitempty"`
	UserID                 string            `json:"user_id"`
	Destination            string            `json:"destination"`
	Justification          string            `json:"justification,omitempty"`
}

type ExportDecision struct {
	Allowed               bool             `json:"allowed"`
	CheckpointRequired    bool             `json:"checkpoint_required"`
	JustificationRequired bool             `json:"justification_required"`
	Provenance            ExportProvenance `json:"provenance"`
	BlockingReasons       []string         `json:"blocking_reasons"`
}

func EvaluateExport(policy ExportPolicy, req ExportRequest) ExportDecision {
	decision := ExportDecision{
		Allowed:               true,
		CheckpointRequired:    containsKind(policy.CheckpointRequiredKinds, req.Kind),
		JustificationRequired: containsKind(policy.JustificationRequiredKinds, req.Kind),
		Provenance: ExportProvenance{
			Kind:                   req.Kind,
			ResourceRID:            strings.TrimSpace(req.ResourceRID),
			Filters:                copyMap(req.Filters),
			Parameters:             copyMap(req.Parameters),
			Branch:                 strings.TrimSpace(req.Branch),
			Markings:               normalizeStrings(req.Markings),
			RestrictedViewPolicyID: strings.TrimSpace(req.RestrictedViewPolicyID),
			UserID:                 strings.TrimSpace(req.UserID),
			Destination:            strings.TrimSpace(req.Destination),
			Justification:          strings.TrimSpace(req.Justification),
		},
	}
	if decision.Provenance.ResourceRID == "" {
		decision.BlockingReasons = append(decision.BlockingReasons, "resource is required")
	}
	if decision.Provenance.UserID == "" {
		decision.BlockingReasons = append(decision.BlockingReasons, "user is required")
	}
	if decision.Provenance.Destination == "" {
		decision.BlockingReasons = append(decision.BlockingReasons, "destination is required")
	}
	if decision.JustificationRequired && decision.Provenance.Justification == "" {
		decision.BlockingReasons = append(decision.BlockingReasons, "export justification is required")
	}
	for _, marking := range decision.Provenance.Markings {
		if containsFold(policy.BlockedMarkings, marking) {
			decision.BlockingReasons = append(decision.BlockingReasons, "export blocked by marking "+marking)
		}
	}
	if len(policy.AllowedDestinations) > 0 && !containsFold(policy.AllowedDestinations, decision.Provenance.Destination) {
		decision.BlockingReasons = append(decision.BlockingReasons, "destination is not allowed by export policy")
	}
	checks := []struct {
		ok     bool
		reason string
	}{
		{req.MarkingAllowed, "export violates markings"},
		{req.RestrictedViewAllowed, "export violates restricted view policy"},
		{req.ObjectSecurityAllowed, "export violates object security"},
		{req.ApplicationPolicyAllowed, "export violates application policy"},
		{req.EgressPolicyAllowed, "export violates egress policy"},
	}
	for _, check := range checks {
		if !check.ok {
			decision.BlockingReasons = append(decision.BlockingReasons, check.reason)
		}
	}
	decision.BlockingReasons = normalizeStrings(decision.BlockingReasons)
	if len(decision.BlockingReasons) > 0 {
		decision.Allowed = false
	}
	return decision
}

func containsKind(values []ExportKind, kind ExportKind) bool {
	for _, value := range values {
		if strings.EqualFold(string(value), string(kind)) {
			return true
		}
	}
	return false
}

func copyMap(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func containsFold(values []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), needle) {
			return true
		}
	}
	return false
}

func normalizeStrings(values []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}
