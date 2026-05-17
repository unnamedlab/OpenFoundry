package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/domain/llm/anthropic"
	aimodels "github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

// TestCreateChatCompletionRoutesThroughAnthropicProvider wires the
// real anthropic.Provider as the runtime, points it at an httptest
// server simulating the Messages API, and asserts that the request
// carries Anthropic's required headers and that the response is parsed
// into the chat-completion envelope.
func TestCreateChatCompletionRoutesThroughAnthropicProvider(t *testing.T) {
	t.Parallel()

	var (
		gotAPIKey  string
		gotVersion string
		gotPath    string
		gotBody    map[string]any
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &gotBody))
		_, _ = w.Write([]byte(`{
			"content":[{"type":"text","text":"hi there"}],
			"usage":{"input_tokens":11,"output_tokens":2}
		}`))
	}))
	defer upstream.Close()

	provider := anthropic.New("sk-svc-test", "claude-sonnet-4-6", upstream.URL, upstream.Client())
	h := &Handlers{
		Runtime: provider,
		Provider: &aimodels.LlmProvider{
			ID:              uuid.New(),
			Name:            "anthropic-env",
			ProviderType:    "anthropic",
			ModelName:       "claude-sonnet-4-6",
			EndpointURL:     upstream.URL,
			APIMode:         "messages",
			Enabled:         true,
			MaxOutputTokens: 128,
			RouteRules:      aimodels.DefaultProviderRoutingRules(),
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/",
		strings.NewReader(`{"messages":[{"role":"system","content":"be brief"},{"role":"user","content":"hello"}]}`))
	w := httptest.NewRecorder()

	h.CreateChatCompletion(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	assert.Equal(t, "sk-svc-test", gotAPIKey, "must forward x-api-key from provider config")
	assert.Equal(t, anthropic.APIVersion, gotVersion, "must pin anthropic-version header")
	assert.Equal(t, "/messages", gotPath, "must POST to /messages")
	assert.Equal(t, "claude-sonnet-4-6", gotBody["model"])
	assert.Equal(t, "be brief", gotBody["system"])

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	choices := resp["choices"].([]any)
	require.Len(t, choices, 1)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	assert.Equal(t, "hi there", msg["content"])
	usage := resp["usage"].(map[string]any)
	assert.EqualValues(t, 11, usage["prompt_tokens"])
	assert.EqualValues(t, 2, usage["completion_tokens"])
	assert.EqualValues(t, 13, usage["total_tokens"])
}
