package effectdispatcher

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestExtractRootActionRequest(t *testing.T) {
	t.Parallel()
	payload := json.RawMessage(`{
		"action_id": "promote",
		"target_object_id": "obj-1",
		"parameters": {"priority": "high"},
		"justification": "policy"
	}`)
	req, err := ExtractActionRequest(payload, uuid.Nil)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if req.ActionID != "promote" {
		t.Fatalf("action_id %s", req.ActionID)
	}
	if req.TargetObjectID == nil || *req.TargetObjectID != "obj-1" {
		t.Fatalf("target %v", req.TargetObjectID)
	}
	if req.Justification == nil || *req.Justification != "policy" {
		t.Fatalf("justification %v", req.Justification)
	}
}

func TestExtractNestedActionRequestFallsBack(t *testing.T) {
	t.Parallel()
	payload := json.RawMessage(`{
		"ontology_action": {"action_id": "promote", "parameters": {"x": 1}},
		"extra": "noise"
	}`)
	req, err := ExtractActionRequest(payload, uuid.Nil)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if req.ActionID != "promote" {
		t.Fatalf("action_id %s", req.ActionID)
	}
	if req.TargetObjectID != nil {
		t.Fatal("target should be nil")
	}
}

func TestExtractRejectsMissingActionID(t *testing.T) {
	t.Parallel()
	payload := json.RawMessage(`{"parameters": {"x": 1}}`)
	_, err := ExtractActionRequest(payload, uuid.Nil)
	if err == nil {
		t.Fatal("expected error")
	}
	de := AsDispatchError(err)
	if de == nil || de.Kind != KindInvalidPayload {
		t.Fatalf("expected InvalidPayload, got %v", err)
	}
}

func TestExtractRejectsEmptyActionID(t *testing.T) {
	t.Parallel()
	payload := json.RawMessage(`{"action_id": "   "}`)
	_, err := ExtractActionRequest(payload, uuid.Nil)
	de := AsDispatchError(err)
	if de == nil || de.Kind != KindInvalidPayload {
		t.Fatalf("expected InvalidPayload, got %v", err)
	}
}

func TestDispatchErrorTerminalClassification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err      *DispatchError
		terminal bool
	}{
		{&DispatchError{Kind: KindInvalidPayload}, true},
		{&DispatchError{Kind: KindUnconfigured}, true},
		{&DispatchError{Kind: KindNonRetryable, Status: 400}, true},
		{&DispatchError{Kind: KindExhausted, Attempts: 5}, true},
		{&DispatchError{Kind: KindRetryable}, false},
	}
	for _, tc := range cases {
		if got := tc.err.IsTerminal(); got != tc.terminal {
			t.Fatalf("kind %s: got terminal=%v want %v", tc.err.Kind, got, tc.terminal)
		}
	}
}

func TestRetryPolicyDefaultsMatchLegacyGoActivity(t *testing.T) {
	t.Parallel()
	p := DefaultRetryPolicy()
	if p.MaxAttempts != 5 {
		t.Fatal("max_attempts != 5")
	}
	if p.InitialBackoff != 30*time.Second {
		t.Fatalf("initial backoff %v", p.InitialBackoff)
	}
	if p.MaxBackoff != 600*time.Second {
		t.Fatalf("max backoff %v", p.MaxBackoff)
	}
	if p.BackoffMultiplier != 2.0 {
		t.Fatalf("multiplier %v", p.BackoffMultiplier)
	}
}

func TestRetryBackoffGrowsThenCaps(t *testing.T) {
	t.Parallel()
	p := DefaultRetryPolicy()
	cases := []struct {
		attempt uint32
		want    time.Duration
	}{
		{1, 0},
		{2, 30 * time.Second},
		{3, 60 * time.Second},
		{4, 120 * time.Second},
		{5, 240 * time.Second},
		{6, 480 * time.Second},
		{7, 600 * time.Second},
		{20, 600 * time.Second},
	}
	for _, tc := range cases {
		got := p.NextBackoff(tc.attempt)
		if got != tc.want {
			t.Fatalf("attempt %d: got %v want %v", tc.attempt, got, tc.want)
		}
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	t.Parallel()
	if got := normalizeBaseURL("ontology-actions:50106"); got != "http://ontology-actions:50106" {
		t.Fatalf("got %s", got)
	}
	if got := normalizeBaseURL("https://ontology-actions:50106/"); got != "https://ontology-actions:50106/" {
		t.Fatalf("got %s", got)
	}
	if got := normalizeBaseURL(""); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeBearerToken(t *testing.T) {
	t.Parallel()
	if got := normalizeBearerToken("xyz"); got != "Bearer xyz" {
		t.Fatalf("got %s", got)
	}
	if got := normalizeBearerToken("Bearer xyz"); got != "Bearer xyz" {
		t.Fatalf("got %s", got)
	}
	if got := normalizeBearerToken("BEARER abc"); got != "BEARER abc" {
		t.Fatalf("got %s", got)
	}
}

func TestURLEncoder(t *testing.T) {
	t.Parallel()
	if got := urlEncode("promote-customer.v2"); got != "promote-customer.v2" {
		t.Fatalf("got %s", got)
	}
	if got := urlEncode("a/b c"); got != "a%2Fb%20c" {
		t.Fatalf("got %s", got)
	}
}

func TestBodyIncludesOnlyPresentOptionalFields(t *testing.T) {
	t.Parallel()
	req := &OntologyActionRequest{
		ActionID:           "promote",
		Parameters:         json.RawMessage(`{"k":"v"}`),
		AuditCorrelationID: uuid.Nil,
	}
	body, err := req.Body()
	if err != nil {
		t.Fatal(err)
	}
	var holder map[string]any
	if err := json.Unmarshal(body, &holder); err != nil {
		t.Fatal(err)
	}
	if _, ok := holder["target_object_id"]; ok {
		t.Fatal("target_object_id should be absent")
	}
	if _, ok := holder["justification"]; ok {
		t.Fatal("justification should be absent")
	}
}
