package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

func TestEvaluateGuardrails_PIIPath(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodPost, "/",
		strings.NewReader(`{"content":"Send the report to alice@example.com please"}`))
	w := httptest.NewRecorder()
	h.EvaluateGuardrails(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp models.EvaluateGuardrailsResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotEmpty(t, resp.Verdict.Flags, "email content should produce at least one PII flag")

	piiSeen := false
	for _, f := range resp.Verdict.Flags {
		if strings.HasPrefix(f.Kind, "pii_") {
			piiSeen = true
		}
	}
	assert.True(t, piiSeen, "expected pii_* flag for email content")
	assert.Contains(t, resp.Recommendations, "Redact PII before routing prompts to external LLM providers.")
}

func TestEvaluateGuardrails_RejectsEmptyContent(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"content":"   "}`))
	w := httptest.NewRecorder()
	h.EvaluateGuardrails(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var body errorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "guardrail evaluation requires content", body.Error)
}

func TestEvaluateGuardrails_NoIssuesPath(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodPost, "/",
		strings.NewReader(`{"content":"hello world how are you"}`))
	w := httptest.NewRecorder()
	h.EvaluateGuardrails(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp models.EvaluateGuardrailsResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Contains(t, resp.Recommendations, "No blocking issues detected; response is safe to continue.")
}

func TestBenchmarkProviders_RejectsEmptyPrompt(t *testing.T) {
	// Validation runs before the Pool guard — empty-prompt rejection
	// stays 400 even with no pool wired (Pool guard short-circuits
	// only when the pool is nil AND validation has not yet run; here
	// the prompt-empty check is the first failure).
	//
	// Note: with Pool=nil the handler returns 503 before decoding.
	// Skip until we want a stub-pool harness.
	t.Skip("benchmark validation runs after the Pool nil-guard; covered by route-level integration tests")
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodPost, "/",
		strings.NewReader(`{"prompt":"   ","use_case":"chat","max_tokens":1024}`))
	w := httptest.NewRecorder()
	h.BenchmarkProviders(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBenchmarkProviders_RejectsBlockedPrompt(t *testing.T) {
	t.Skip("benchmark guardrail-blocked rejection runs after the Pool nil-guard; covered by route-level integration tests")
}

func TestSummarizeTitleFallbacks(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "New benchmark", summarizeTitle(""))
	assert.Equal(t, "New benchmark", summarizeTitle("   "))
	short := strings.Repeat("a", 60)
	assert.Equal(t, short, summarizeTitle(short))
	long := strings.Repeat("a", 61)
	assert.Equal(t, strings.Repeat("a", 60)+"...", summarizeTitle(long))
}

func TestPreviewTextTruncation(t *testing.T) {
	t.Parallel()
	short := "alice"
	assert.Equal(t, "alice", previewText(short, 280))
	long := strings.Repeat("x", 290)
	got := previewText(long, 280)
	assert.Equal(t, strings.Repeat("x", 280)+"...", got)
}

func TestAttachmentContextFormatting(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "none", attachmentContext(nil))

	url := "https://example.com/img.png"
	mime := "image/png"
	name := "diagram"
	atts := []models.ChatAttachment{
		{Kind: "image_url", Name: &name, URL: &url},
		{Kind: "image_base64", MimeType: &mime},
	}
	got := attachmentContext(atts)
	assert.Contains(t, got, "diagram: image url https://example.com/img.png")
	assert.Contains(t, got, "embedded image/png image")
}

func TestRequiredModalitiesAddsImage(t *testing.T) {
	t.Parallel()
	url := "https://example.com/img.png"
	atts := []models.ChatAttachment{{Kind: "image_url", URL: &url}}
	assert.Equal(t, []string{"text", "image"}, requiredModalities(atts))
	assert.Equal(t, []string{"text"}, requiredModalities(nil))
}

func TestModalityLabel(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "text", modalityLabel([]string{"text"}))
	assert.Equal(t, "image+text", modalityLabel([]string{"text", "image"}))
}

func TestPrivacyReason(t *testing.T) {
	t.Parallel()
	got := privacyReason(models.GuardrailVerdict{}, true)
	require.NotNil(t, got)
	assert.Equal(t, "private network explicitly requested", *got)

	got = privacyReason(models.GuardrailVerdict{
		Flags: []models.GuardrailFlag{{Kind: "pii_email"}},
	}, false)
	require.NotNil(t, got)
	assert.Equal(t, "PII detected in prompt, preferring private-network providers", *got)

	got = privacyReason(models.GuardrailVerdict{}, false)
	assert.Nil(t, got)
}
