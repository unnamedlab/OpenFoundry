package anthropic_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/domain/llm"
	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/domain/llm/anthropic"
)

func TestProviderSendsAnthropicHeadersAndParsesResponse(t *testing.T) {
	t.Parallel()

	var (
		gotAPIKey    string
		gotVersion   string
		gotPath      string
		gotMethod    string
		gotBody      map[string]any
		gotMimeType  string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		gotMimeType = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		gotMethod = r.Method
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &gotBody))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_01",
			"type":"message",
			"role":"assistant",
			"content":[{"type":"text","text":"hello from claude"}],
			"usage":{"input_tokens":17,"output_tokens":4}
		}`))
	}))
	defer server.Close()

	provider := anthropic.New("sk-test-123", "claude-sonnet-4-6", server.URL, server.Client())

	res, err := provider.CompleteText(context.Background(), llm.CompletionRequest{
		SystemPrompt: "be terse",
		UserPrompt:   "ping",
		MaxTokens:    32,
		Temperature:  0.5,
	})
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/messages", gotPath)
	assert.Equal(t, "sk-test-123", gotAPIKey)
	assert.Equal(t, anthropic.APIVersion, gotVersion)
	assert.Equal(t, "application/json", gotMimeType)
	assert.Equal(t, "claude-sonnet-4-6", gotBody["model"])
	assert.Equal(t, "be terse", gotBody["system"])
	assert.EqualValues(t, 32, gotBody["max_tokens"])
	messages := gotBody["messages"].([]any)
	require.Len(t, messages, 1)
	first := messages[0].(map[string]any)
	assert.Equal(t, "user", first["role"])
	parts := first["content"].([]any)
	require.Len(t, parts, 1)
	assert.Equal(t, "text", parts[0].(map[string]any)["type"])
	assert.Equal(t, "ping", parts[0].(map[string]any)["text"])

	assert.Equal(t, "hello from claude", res.Text)
	assert.EqualValues(t, 17, res.PromptTokens)
	assert.EqualValues(t, 4, res.CompletionTokens)
	assert.EqualValues(t, 21, res.TotalTokens)
}

func TestProviderConcatenatesMultipleTextBlocks(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"content":[
				{"type":"text","text":"part one. "},
				{"type":"text","text":"part two."}
			],
			"usage":{"input_tokens":2,"output_tokens":3}
		}`))
	}))
	defer server.Close()

	provider := anthropic.New("sk-test-123", "", server.URL, server.Client())
	res, err := provider.CompleteText(context.Background(), llm.CompletionRequest{UserPrompt: "go"})
	require.NoError(t, err)
	assert.Equal(t, "part one. part two.", res.Text)
}

func TestProviderReturnsErrorOnNon2xx(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid key"}}`))
	}))
	defer server.Close()

	provider := anthropic.New("sk-bad", "", server.URL, server.Client())
	_, err := provider.CompleteText(context.Background(), llm.CompletionRequest{UserPrompt: "go"})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "401"), "want status surfaced, got %q", err.Error())
	assert.Contains(t, err.Error(), "invalid key")
}

func TestProviderRequiresAPIKey(t *testing.T) {
	t.Parallel()
	provider := &anthropic.Provider{}
	_, err := provider.CompleteText(context.Background(), llm.CompletionRequest{UserPrompt: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), anthropic.EnvAPIKey)
}

func TestNewAppliesDefaults(t *testing.T) {
	t.Parallel()
	p := anthropic.New("sk-x", "", "", nil)
	assert.Equal(t, anthropic.DefaultModel, p.Model)
	assert.Equal(t, anthropic.DefaultBaseURL, p.BaseURL)
	assert.NotNil(t, p.Client)
}

func TestFromEnv(t *testing.T) {
	t.Setenv(anthropic.EnvAPIKey, "")
	if _, ok := anthropic.FromEnv(); ok {
		t.Fatalf("expected FromEnv to return false when api key missing")
	}
	t.Setenv(anthropic.EnvAPIKey, "sk-from-env")
	t.Setenv(anthropic.EnvModel, "claude-test")
	t.Setenv(anthropic.EnvBaseURL, "https://example.test")
	p, ok := anthropic.FromEnv()
	require.True(t, ok)
	assert.Equal(t, "sk-from-env", p.APIKey)
	assert.Equal(t, "claude-test", p.Model)
	assert.Equal(t, "https://example.test", p.BaseURL)
}
