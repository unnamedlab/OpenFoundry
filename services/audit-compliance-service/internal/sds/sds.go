// Package sds ports the sensitive-data-scanning subsystem 1:1 from
// `services/audit-compliance-service/src/sds/`.
//
// Surface:
//
//   - Scan(req)                    — pure regex-based scan + redact
//   - CreateScanJob(ctx, db, req)  — persists a scan_jobs row + per-finding issues
//   - IssueFromRow / JobFromRow    — typed re-projections
//   - ApplyMarkings(issue, req)    — merges + dedups markings/remediations
//   - DefaultMarkings              — exposed for tests
//
// Detectors (regex catalogue) are pinned identically to the Rust impl.
package sds

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// detectors lists the (kind, regex) pairs in the same order the Rust
// impl runs them. Compile-once via package init.
var detectors = []struct {
	kind  string
	regex *regexp.Regexp
}{
	{"email", regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`)},
	{"ssn", regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)},
	{"credit_card", regexp.MustCompile(`\b(?:\d[ \-]*?){13,16}\b`)},
	{"api_key", regexp.MustCompile(`\bofk_[A-Za-z0-9_\-]{8,}\b`)},
	{"bearer_token", regexp.MustCompile(`Bearer\s+[A-Za-z0-9._=\-]{16,}`)},
}

// Scan ports `sds::scan` 1:1.
func Scan(request *models.SensitiveDataScanRequest) models.SensitiveDataScanResponse {
	redacted := request.Content
	findings := make([]models.SensitiveDataFinding, 0)
	for _, d := range detectors {
		matches := d.regex.FindAllString(request.Content, -1)
		if len(matches) == 0 {
			continue
		}
		first := matches[0]
		matchCount := len(matches)
		if request.EffectiveRedact() {
			for _, m := range matches {
				redacted = strings.ReplaceAll(redacted, m, redactValue(d.kind, m))
			}
		}
		findings = append(findings, models.SensitiveDataFinding{
			Kind:       d.kind,
			Value:      first,
			Redacted:   redactValue(d.kind, first),
			MatchCount: matchCount,
			Severity:   severityForKind(d.kind),
		})
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].Kind < findings[j].Kind })
	return models.SensitiveDataScanResponse{
		Findings:        findings,
		RedactedContent: redacted,
		RiskScore:       riskScore(request.EffectiveScope(), findings),
	}
}

// CreateScanJob ports `sds::create_scan_job`.
func CreateScanJob(ctx context.Context, db *pgxpool.Pool, request *models.RunSensitiveDataScanRequest) (*models.SDSScanJob, error) {
	scanReq := models.SensitiveDataScanRequest{
		Content: request.Content,
		Redact:  request.Redact,
		Scope:   request.EffectiveScope(),
	}
	response := Scan(&scanReq)
	remediations := recommendedRemediations(response.Findings)
	jobID := uuid.New()
	now := time.Now().UTC()

	findingsJSON, err := json.Marshal(response.Findings)
	if err != nil {
		return nil, err
	}
	remediationsJSON, err := json.Marshal(remediations)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec(ctx,
		`INSERT INTO sds_scan_jobs
		      (id, target_name, scope, status, risk_score, findings, issue_count,
		       redacted_content, remediations, requested_by, created_at, updated_at)
		    VALUES ($1, $2, $3, 'completed', $4, $5::jsonb, $6, $7, $8::jsonb, $9, $10, $11)`,
		jobID, request.TargetName, string(request.EffectiveScope()),
		int32(response.RiskScore), findingsJSON, int32(len(response.Findings)),
		response.RedactedContent, remediationsJSON, request.RequestedBy, now, now,
	); err != nil {
		return nil, fmt.Errorf("insert scan job: %w", err)
	}

	for _, finding := range response.Findings {
		issueID := uuid.New()
		markings := defaultMarkings(finding)
		issueRemediations := findingRemediations(finding)
		markingsJSON, err := json.Marshal(markings)
		if err != nil {
			return nil, err
		}
		remJSON, err := json.Marshal(issueRemediations)
		if err != nil {
			return nil, err
		}
		if _, err := db.Exec(ctx,
			`INSERT INTO sds_issues
			      (id, job_id, kind, severity, status, matched_value, redacted_value,
			       match_count, markings, remediation_actions, created_at, updated_at)
			    VALUES ($1, $2, $3, $4, 'open', $5, $6, $7, $8::jsonb, $9::jsonb, $10, $11)`,
			issueID, jobID, finding.Kind, finding.Severity, finding.Value,
			finding.Redacted, int32(finding.MatchCount), markingsJSON, remJSON, now, now,
		); err != nil {
			return nil, fmt.Errorf("insert issue: %w", err)
		}
	}

	return &models.SDSScanJob{
		ID:              jobID,
		TargetName:      request.TargetName,
		Scope:           string(request.EffectiveScope()),
		Status:          "completed",
		RiskScore:       int32(response.RiskScore),
		Findings:        findingsJSON,
		IssueCount:      int32(len(response.Findings)),
		RedactedContent: response.RedactedContent,
		Remediations:    remediationsJSON,
		RequestedBy:     request.RequestedBy,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
}

// ApplyMarkings ports `sds::apply_markings`. Merges the new markings +
// remediation actions into the issue (deduplicated, append-only) and
// returns the resulting markings/remediations + the new status.
func ApplyMarkings(issue *models.SDSIssue, request *models.MarkSensitiveIssueRequest) (json.RawMessage, json.RawMessage, string, error) {
	current := issueMarkings(issue)
	for _, m := range request.Markings {
		if !contains(current, m) {
			current = append(current, m)
		}
	}
	currentRem := issueRemediationActions(issue)
	for _, a := range request.RemediationActions {
		if !contains(currentRem, a) {
			currentRem = append(currentRem, a)
		}
	}
	status := issue.Status
	if request.Resolve {
		status = string(models.SDSIssueResolved)
	}
	markingsJSON, err := json.Marshal(current)
	if err != nil {
		return nil, nil, "", err
	}
	remJSON, err := json.Marshal(currentRem)
	if err != nil {
		return nil, nil, "", err
	}
	return markingsJSON, remJSON, status, nil
}

// RulePayload mirrors `sds::rule_payload` — encodes match_conditions
// + remediation_actions as JSON-ready payloads.
func RulePayload(request *models.CreateRemediationRuleRequest) (json.RawMessage, json.RawMessage, error) {
	mc := request.MatchConditions
	if mc == nil {
		mc = []models.SDSMatchCondition{}
	}
	ra := request.RemediationActions
	if ra == nil {
		ra = []string{}
	}
	matchJSON, err := json.Marshal(mc)
	if err != nil {
		return nil, nil, err
	}
	remJSON, err := json.Marshal(ra)
	if err != nil {
		return nil, nil, err
	}
	return matchJSON, remJSON, nil
}

func issueMarkings(issue *models.SDSIssue) []string {
	if len(issue.Markings) == 0 {
		return nil
	}
	var out []string
	_ = json.Unmarshal(issue.Markings, &out)
	return out
}

func issueRemediationActions(issue *models.SDSIssue) []string {
	if len(issue.RemediationActions) == 0 {
		return nil
	}
	var out []string
	_ = json.Unmarshal(issue.RemediationActions, &out)
	return out
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// redactValue mirrors `sds::redact_value`.
func redactValue(kind, value string) string {
	switch kind {
	case "email":
		parts := strings.Split(value, "@")
		if len(parts) != 2 {
			return "[redacted-email]"
		}
		prefix := parts[0]
		if len(prefix) > 2 {
			prefix = prefix[:2]
		}
		return fmt.Sprintf("%s***@%s", prefix, parts[1])
	case "ssn":
		return "***-**-****"
	case "credit_card":
		// Last 4 chars (post-redact preserves any spacing/dashes too —
		// matches Rust's reverse-take-4).
		runes := []rune(value)
		last4 := string(runes)
		if len(runes) > 4 {
			last4 = string(runes[len(runes)-4:])
		}
		return "**** **** **** " + last4
	case "api_key":
		return "ofk_[redacted]"
	case "bearer_token":
		return "Bearer [redacted]"
	default:
		return "[redacted]"
	}
}

func severityForKind(kind string) string {
	switch kind {
	case "ssn", "credit_card":
		return "critical"
	case "api_key", "bearer_token":
		return "high"
	case "email":
		return "medium"
	default:
		return "low"
	}
}

func scoreForKind(kind string) uint32 {
	switch kind {
	case "ssn":
		return 40
	case "credit_card":
		return 40
	case "api_key":
		return 35
	case "bearer_token":
		return 35
	case "email":
		return 10
	default:
		return 5
	}
}

func riskScore(scope models.SensitiveDataScope, findings []models.SensitiveDataFinding) uint32 {
	base := uint32(0)
	for _, f := range findings {
		base += scoreForKind(f.Kind)
	}
	multiplier := uint32(1)
	switch scope {
	case models.SDSScopePrompt, models.SDSScopeMessage:
		multiplier = 1
	case models.SDSScopeDataset, models.SDSScopeRecord, models.SDSScopeFile:
		multiplier = 2
	}
	return base * multiplier
}

func defaultMarkings(finding models.SensitiveDataFinding) []string {
	out := []string{"sensitive", finding.Kind}
	if finding.Severity == "critical" {
		out = append(out, "restricted")
	}
	return out
}

func findingRemediations(finding models.SensitiveDataFinding) []string {
	switch finding.Kind {
	case "email":
		return []string{"mask_pii"}
	case "ssn", "credit_card":
		return []string{"quarantine_record", "mask_pii"}
	case "api_key", "bearer_token":
		return []string{"revoke_credential", "rotate_secret"}
	default:
		return []string{"manual_review"}
	}
}

func recommendedRemediations(findings []models.SensitiveDataFinding) []string {
	out := make([]string, 0)
	for _, f := range findings {
		for _, action := range findingRemediations(f) {
			if !contains(out, action) {
				out = append(out, action)
			}
		}
	}
	return out
}
