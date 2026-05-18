package governance

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

type PromptPayloadPolicy struct {
	AllowedMarkings         []string
	RedactMarkings          []string
	SummarizeMarkings       []string
	BlockedExternalMarkings []string
	ExternalProvider        bool
	RetentionDays           int
	DeleteAfter             time.Time
}

type PromptPayloadRecord struct {
	Prompt       string
	ModelInput   string
	ModelOutput  string
	DebugTrace   string
	AuditPayload string
	Notification string
	Markings     []string
	CreatedAt    time.Time
}

type PromptPayloadDecision struct {
	Allowed              bool       `json:"allowed"`
	RedactedPrompt       string     `json:"redacted_prompt"`
	RedactedModelInput   string     `json:"redacted_model_input"`
	RedactedModelOutput  string     `json:"redacted_model_output"`
	RedactedDebugTrace   string     `json:"redacted_debug_trace"`
	RedactedAuditPayload string     `json:"redacted_audit_payload"`
	RedactedNotification string     `json:"redacted_notification"`
	DeletionDueAt        *time.Time `json:"deletion_due_at,omitempty"`
	BlockedReasons       []string   `json:"blocked_reasons"`
	AppliedControls      []string   `json:"applied_controls"`
}

var sensitiveTokenRE = regexp.MustCompile(`(?i)(email|ssn|token|secret|api[_-]?key|password)\s*[:=]\s*[^\s,;]+`)

func EvaluatePromptPayload(policy PromptPayloadPolicy, record PromptPayloadRecord) PromptPayloadDecision {
	decision := PromptPayloadDecision{Allowed: true}
	markings := normalizeSet(record.Markings)
	allowed := normalizeSet(policy.AllowedMarkings)
	for _, marking := range markings {
		if len(allowed) > 0 && !contains(allowed, marking) {
			decision.BlockedReasons = append(decision.BlockedReasons, "marking "+marking+" is not allowed for this AI session")
		}
		if policy.ExternalProvider && containsFold(policy.BlockedExternalMarkings, marking) {
			decision.BlockedReasons = append(decision.BlockedReasons, "marking "+marking+" cannot be sent to external model providers")
		}
	}
	mode := "pass"
	if intersects(markings, policy.RedactMarkings) {
		mode = "redact"
		decision.AppliedControls = append(decision.AppliedControls, "redacted sensitive marked payload")
	} else if intersects(markings, policy.SummarizeMarkings) {
		mode = "summarize"
		decision.AppliedControls = append(decision.AppliedControls, "summarized sensitive marked payload")
	}
	decision.RedactedPrompt = transformPayload(record.Prompt, mode)
	decision.RedactedModelInput = transformPayload(record.ModelInput, mode)
	decision.RedactedModelOutput = transformPayload(record.ModelOutput, mode)
	decision.RedactedDebugTrace = transformPayload(record.DebugTrace, mode)
	decision.RedactedAuditPayload = transformPayload(record.AuditPayload, mode)
	decision.RedactedNotification = transformPayload(record.Notification, mode)
	if policy.RetentionDays > 0 {
		due := record.CreatedAt.Add(time.Duration(policy.RetentionDays) * 24 * time.Hour)
		decision.DeletionDueAt = &due
		decision.AppliedControls = append(decision.AppliedControls, "retention_days")
	}
	if !policy.DeleteAfter.IsZero() {
		due := policy.DeleteAfter
		decision.DeletionDueAt = &due
		decision.AppliedControls = append(decision.AppliedControls, "delete_after")
	}
	decision.AppliedControls = normalizeSet(decision.AppliedControls)
	decision.BlockedReasons = normalizeSet(decision.BlockedReasons)
	if len(decision.BlockedReasons) > 0 {
		decision.Allowed = false
	}
	return decision
}

func transformPayload(value, mode string) string {
	switch mode {
	case "redact":
		out := sensitiveTokenRE.ReplaceAllString(value, "$1=[REDACTED]")
		if out != value {
			return out
		}
		if strings.TrimSpace(value) == "" {
			return value
		}
		return "[REDACTED]"
	case "summarize":
		trimmed := strings.TrimSpace(value)
		if len(trimmed) <= 64 {
			return "[SUMMARY] " + trimmed
		}
		return "[SUMMARY] " + trimmed[:64] + "..."
	default:
		return value
	}
}

func intersects(values []string, candidates []string) bool {
	for _, value := range values {
		if containsFold(candidates, value) {
			return true
		}
	}
	return false
}

func containsFold(values []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), needle) {
			return true
		}
	}
	return false
}

func contains(values []string, needle string) bool { return containsFold(values, needle) }

func normalizeSet(values []string) []string {
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
