package governanceops

import "strings"

type LeastPrivilegeSignals struct {
	OwnerGrants                    []string
	EditorGrants                   []string
	UnusedGroups                   []string
	StaleTokenIDs                  []string
	UnscopedOAuthAppIDs            []string
	UnrestrictedAppAccessIDs       []string
	UnredactedEmailChannels        []string
	ProjectsMissingDiscoverer      []string
	EgressWorkloadsWithoutImporter []string
}

type RemediationProposal struct {
	ID               string   `json:"id"`
	Risk             string   `json:"risk"`
	Recommendation   string   `json:"recommendation"`
	ApprovalRequired bool     `json:"approval_required"`
	SafeAction       string   `json:"safe_action"`
	Targets          []string `json:"targets"`
}

func RecommendLeastPrivilege(s LeastPrivilegeSignals) []RemediationProposal {
	proposals := []RemediationProposal{}
	add := func(id, risk, rec, action string, targets []string) {
		if len(targets) > 0 {
			proposals = append(proposals, RemediationProposal{ID: id, Risk: risk, Recommendation: rec, ApprovalRequired: true, SafeAction: action, Targets: normalize(targets)})
		}
	}
	add("group-based-grants", "overbroad direct grants", "Replace broad owner/editor assignments with group-based least-privilege grants.", "open_approval_to_replace_direct_grants", append(s.OwnerGrants, s.EditorGrants...))
	add("default-discoverer", "missing discoverability baseline", "Use default Discoverer project roles instead of broad viewer/editor access.", "propose_discoverer_role", s.ProjectsMissingDiscoverer)
	add("specific-egress-importers", "broad egress capability", "Bind egress imports to specific importer groups and destinations.", "propose_egress_importer_group", s.EgressWorkloadsWithoutImporter)
	add("resource-oauth-scopes", "unscoped OAuth app", "Replace unscoped OAuth applications with resource-specific scopes.", "propose_oauth_scope_reduction", s.UnscopedOAuthAppIDs)
	add("unused-groups", "unused group", "Remove or archive unused groups after owner approval.", "open_group_removal_review", s.UnusedGroups)
	add("stale-tokens", "stale token", "Revoke stale tokens after owner notification.", "open_token_revocation_approval", s.StaleTokenIDs)
	add("restricted-app-access", "unrestricted app access", "Constrain application access with organization/user/group allow rules.", "open_application_access_change", s.UnrestrictedAppAccessIDs)
	add("email-redaction", "unredacted notification payload", "Enable strict email content redaction or add explicit risk acknowledgements.", "open_email_redaction_change", s.UnredactedEmailChannels)
	return proposals
}

func ProposalFinding(proposal RemediationProposal) FindingInput {
	return FindingInput{Source: FindingPermissionDrift, Severity: "medium", Title: "Least-privilege recommendation: " + proposal.ID, Description: strings.TrimSpace(proposal.Risk + " — " + proposal.Recommendation), ResourceRIDs: proposal.Targets, PolicyDecisionIDs: []string{proposal.SafeAction}}
}
