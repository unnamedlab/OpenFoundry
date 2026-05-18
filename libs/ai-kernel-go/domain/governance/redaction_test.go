package governance

import (
	"strings"
	"testing"
	"time"
)

func TestEvaluatePromptPayloadBlocksExternalMarkedDataAndRedactsSurfaces(t *testing.T) {
	created := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	decision := EvaluatePromptPayload(PromptPayloadPolicy{
		AllowedMarkings:         []string{"public", "pii"},
		RedactMarkings:          []string{"pii"},
		BlockedExternalMarkings: []string{"pii"},
		ExternalProvider:        true,
		RetentionDays:           7,
	}, PromptPayloadRecord{
		Prompt:       "email:alice@example.com summarize account",
		ModelInput:   "token=secret-value",
		ModelOutput:  "ok",
		DebugTrace:   "password=hunter2",
		AuditPayload: "api_key=abc123",
		Notification: "ssn=123-45-6789",
		Markings:     []string{"pii"},
		CreatedAt:    created,
	})
	if decision.Allowed {
		t.Fatalf("expected external provider block: %#v", decision)
	}
	for _, surface := range []string{decision.RedactedPrompt, decision.RedactedModelInput, decision.RedactedDebugTrace, decision.RedactedAuditPayload, decision.RedactedNotification} {
		if !strings.Contains(surface, "[REDACTED]") {
			t.Fatalf("surface was not redacted: %q", surface)
		}
	}
	if decision.DeletionDueAt == nil || !decision.DeletionDueAt.Equal(created.Add(7*24*time.Hour)) {
		t.Fatalf("unexpected deletion due at: %#v", decision.DeletionDueAt)
	}
}

func TestEvaluatePromptPayloadSummarizesAllowedSensitiveData(t *testing.T) {
	decision := EvaluatePromptPayload(PromptPayloadPolicy{
		AllowedMarkings:   []string{"confidential"},
		SummarizeMarkings: []string{"confidential"},
	}, PromptPayloadRecord{Prompt: strings.Repeat("a", 100), Markings: []string{"confidential"}})
	if !decision.Allowed {
		t.Fatalf("expected allowed: %#v", decision)
	}
	if !strings.HasPrefix(decision.RedactedPrompt, "[SUMMARY]") {
		t.Fatalf("expected summary, got %q", decision.RedactedPrompt)
	}
}
