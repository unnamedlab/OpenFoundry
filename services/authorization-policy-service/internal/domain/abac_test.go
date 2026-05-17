package domain

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

// captureSlog swaps slog.Default() to a JSON handler writing to a
// bytes.Buffer for the duration of the test, returning the buffer and
// a restore function.
func captureSlog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	prev := slog.Default()
	buf := &bytes.Buffer{}
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return buf, func() { slog.SetDefault(prev) }
}

func TestPolicyMatches_MalformedConditionDefaultDeny(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	const tenantID = "tenant-A"
	const policyID = "policy-malformed-1"

	before := testutil.ToFloat64(abacConditionUnmarshalErrors.With(prometheus.Labels{
		"tenant_id": tenantID,
		"policy_id": policyID,
	}))

	got := policyMatches(json.RawMessage("{bad json"), ctxMap{}, ctxMap{}, tenantID, policyID)
	assert.False(t, got, "malformed condition must NOT match (default-deny)")

	after := testutil.ToFloat64(abacConditionUnmarshalErrors.With(prometheus.Labels{
		"tenant_id": tenantID,
		"policy_id": policyID,
	}))
	assert.InDelta(t, before+1, after, 0.0001, "counter must increment exactly once")

	logs := buf.String()
	assert.Contains(t, logs, "malformed_abac_condition", "must log structured error event")
	assert.Contains(t, logs, policyID, "log must include policy_id")
	assert.Contains(t, logs, tenantID, "log must include tenant_id")
}

func TestPolicyMatches_ValidConditionNoMatch(t *testing.T) {
	conds := json.RawMessage(`{"subject":{"roles":["admin"]}}`)
	subject := ctxMap{"roles": []any{"viewer"}}
	resource := ctxMap{}
	assert.False(t, policyMatches(conds, subject, resource, "tenant-A", "policy-1"),
		"valid condition with mismatched subject must return false")
}

func TestPolicyMatches_ValidConditionMatches(t *testing.T) {
	conds := json.RawMessage(`{"subject":{"roles":["admin"]}}`)
	subject := ctxMap{"roles": []any{"admin", "viewer"}}
	resource := ctxMap{}
	assert.True(t, policyMatches(conds, subject, resource, "tenant-A", "policy-1"),
		"valid condition with matching subject must return true")
}

func TestPolicyMatches_EmptyConditionMatches(t *testing.T) {
	assert.True(t, policyMatches(nil, ctxMap{}, ctxMap{}, "tenant-A", "policy-empty"),
		"empty condition is the canonical 'no condition' case and must match")
}
