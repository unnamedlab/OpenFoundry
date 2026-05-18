package governanceops

import (
	"fmt"
	"strings"
	"time"
)

type FindingSource string

const (
	FindingAuditMonitor     FindingSource = "audit_monitor"
	FindingPermissionDrift  FindingSource = "permission_drift"
	FindingStalePrincipal   FindingSource = "stale_principal"
	FindingRiskyEgress      FindingSource = "risky_egress"
	FindingExportAnomaly    FindingSource = "export_anomaly"
	FindingTokenLeak        FindingSource = "token_leak"
	FindingRetentionFailure FindingSource = "retention_failure"
)

type FindingInput struct {
	Source            FindingSource
	Severity          string
	Title             string
	Description       string
	AuditEventIDs     []string
	ResourceRIDs      []string
	ActorIDs          []string
	PolicyDecisionIDs []string
	DetectedAt        time.Time
}

type Finding struct {
	ID                string            `json:"id"`
	Source            FindingSource     `json:"source"`
	Severity          string            `json:"severity"`
	Status            string            `json:"status"`
	Title             string            `json:"title"`
	Description       string            `json:"description"`
	AssignedTo        *string           `json:"assigned_to,omitempty"`
	AuditEventIDs     []string          `json:"audit_event_ids"`
	ResourceRIDs      []string          `json:"resource_rids"`
	ActorIDs          []string          `json:"actor_ids"`
	PolicyDecisionIDs []string          `json:"policy_decision_ids"`
	Comments          []CaseComment     `json:"comments"`
	Tasks             []RemediationTask `json:"tasks"`
	EvidenceLinks     []EvidenceLink    `json:"evidence_links"`
	ClosureApprovals  []ClosureApproval `json:"closure_approvals"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type CaseComment struct {
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}
type RemediationTask struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Status string  `json:"status"`
	Owner  *string `json:"owner,omitempty"`
}
type EvidenceLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	Kind  string `json:"kind"`
}
type ClosureApproval struct {
	Reviewer  string    `json:"reviewer"`
	Decision  string    `json:"decision"`
	Comment   string    `json:"comment,omitempty"`
	DecidedAt time.Time `json:"decided_at"`
}

func NewFinding(in FindingInput) Finding {
	now := in.DetectedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	severity := strings.ToLower(strings.TrimSpace(in.Severity))
	if severity == "" {
		severity = "medium"
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = string(in.Source) + " finding"
	}
	return Finding{ID: fmt.Sprintf("finding-%d", now.UnixNano()), Source: in.Source, Severity: severity, Status: "open", Title: title, Description: strings.TrimSpace(in.Description), AuditEventIDs: normalize(in.AuditEventIDs), ResourceRIDs: normalize(in.ResourceRIDs), ActorIDs: normalize(in.ActorIDs), PolicyDecisionIDs: normalize(in.PolicyDecisionIDs), CreatedAt: now, UpdatedAt: now}
}

func AssignFinding(f Finding, assignee string, now time.Time) Finding {
	assignee = strings.TrimSpace(assignee)
	if assignee != "" {
		f.AssignedTo = &assignee
		f.Status = "assigned"
		f.UpdatedAt = nonzero(now)
	}
	return f
}

func AddFindingComment(f Finding, author, body string, now time.Time) Finding {
	if strings.TrimSpace(author) != "" && strings.TrimSpace(body) != "" {
		f.Comments = append(f.Comments, CaseComment{Author: strings.TrimSpace(author), Body: strings.TrimSpace(body), CreatedAt: nonzero(now)})
		f.UpdatedAt = nonzero(now)
	}
	return f
}

func AddRemediationTask(f Finding, title, owner string) Finding {
	title = strings.TrimSpace(title)
	if title == "" {
		return f
	}
	var ownerPtr *string
	if strings.TrimSpace(owner) != "" {
		o := strings.TrimSpace(owner)
		ownerPtr = &o
	}
	f.Tasks = append(f.Tasks, RemediationTask{ID: fmt.Sprintf("task-%d", len(f.Tasks)+1), Title: title, Status: "open", Owner: ownerPtr})
	f.UpdatedAt = time.Now().UTC()
	return f
}

func AddEvidenceLink(f Finding, label, url, kind string) Finding {
	if strings.TrimSpace(label) != "" && strings.TrimSpace(url) != "" {
		f.EvidenceLinks = append(f.EvidenceLinks, EvidenceLink{Label: strings.TrimSpace(label), URL: strings.TrimSpace(url), Kind: strings.TrimSpace(kind)})
		f.UpdatedAt = time.Now().UTC()
	}
	return f
}

func ApproveFindingClosure(f Finding, reviewer, decision, comment string, now time.Time) Finding {
	decision = strings.ToLower(strings.TrimSpace(decision))
	if strings.TrimSpace(reviewer) == "" || (decision != "approved" && decision != "rejected") {
		return f
	}
	f.ClosureApprovals = append(f.ClosureApprovals, ClosureApproval{Reviewer: strings.TrimSpace(reviewer), Decision: decision, Comment: strings.TrimSpace(comment), DecidedAt: nonzero(now)})
	if decision == "approved" && allTasksClosed(f.Tasks) {
		f.Status = "closed"
	}
	if decision == "rejected" {
		f.Status = "open"
	}
	f.UpdatedAt = nonzero(now)
	return f
}

func allTasksClosed(tasks []RemediationTask) bool {
	for _, t := range tasks {
		if t.Status != "done" && t.Status != "closed" {
			return false
		}
	}
	return true
}
func nonzero(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t
}
