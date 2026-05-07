package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

func ptr(s string) *string { return &s }

func TestParseEmbeddingPayload(t *testing.T) {
	t.Parallel()
	payload := map[string]any{
		"data": []any{
			map[string]any{"embedding": []any{0.1, 0.2, 0.3, 0.4}},
		},
	}
	emb, err := parseEmbedding(payload)
	require.NoError(t, err)
	assert.Equal(t, []float32{0.1, 0.2, 0.3, 0.4}, emb)
}

func TestParseEmbeddingMissingVector(t *testing.T) {
	t.Parallel()
	_, err := parseEmbedding(map[string]any{"data": []any{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "embedding vector")
}

func TestFlattensStructuredTextParts(t *testing.T) {
	t.Parallel()
	got := valueAsText([]any{
		map[string]any{"text": "alpha"},
		map[string]any{"text": "beta"},
	})
	assert.Equal(t, "alpha\nbeta", got)
}

func TestValueAsTextStringPassthrough(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "hello", valueAsText("hello"))
	assert.Equal(t, "", valueAsText(nil))
	assert.Equal(t, "", valueAsText(123))
}

func TestBuildsOpenAIMultimodalContent(t *testing.T) {
	t.Parallel()
	content, err := buildOpenAIUserContent("describe the image", []models.ChatAttachment{{
		Kind:     "image_url",
		MimeType: ptr("image/png"),
		URL:      ptr("https://example.com/sample.png"),
	}})
	require.NoError(t, err)
	parts, ok := content.([]map[string]any)
	require.True(t, ok)
	require.Len(t, parts, 2)
	assert.Equal(t, "text", parts[0]["type"])
	assert.Equal(t, "image_url", parts[1]["type"])
}

func TestBuildOpenAIUserContentNoAttachmentsPassthrough(t *testing.T) {
	t.Parallel()
	got, err := buildOpenAIUserContent("hi", nil)
	require.NoError(t, err)
	assert.Equal(t, "hi", got)
}

func TestBuildOpenAIRejectsImageURLWithoutURL(t *testing.T) {
	t.Parallel()
	_, err := buildOpenAIUserContent("x", []models.ChatAttachment{{Kind: "image_url"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "image_url attachment requires url")
}

func TestBuildOpenAIRejectsImageBase64WithoutData(t *testing.T) {
	t.Parallel()
	_, err := buildOpenAIUserContent("x", []models.ChatAttachment{{Kind: "image_base64"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "image_base64 attachment requires base64_data")
}

func TestBuildOpenAIRejectsUnsupportedKind(t *testing.T) {
	t.Parallel()
	_, err := buildOpenAIUserContent("x", []models.ChatAttachment{{Kind: "video"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported attachment kind 'video'")
}

func TestBuildAnthropicUserContent(t *testing.T) {
	t.Parallel()
	parts := buildAnthropicUserContent("describe", []models.ChatAttachment{
		{Kind: "image_base64", Base64Data: ptr("AAAA")},
		{Kind: "image_url", URL: ptr("https://example.com/img.png")},
		{Kind: "text", Text: ptr("extra context")},
	})
	require.Len(t, parts, 4)
	assert.Equal(t, "text", parts[0]["type"])
	assert.Equal(t, "image", parts[1]["type"])
	assert.Equal(t, "text", parts[2]["type"])
	assert.Contains(t, parts[2]["text"], "Referenced image URL")
	assert.Equal(t, "extra context", parts[3]["text"])
}

func TestBuildOllamaUserPayloadCollectsImagesAndPrompt(t *testing.T) {
	t.Parallel()
	prompt, images := buildOllamaUserPayload("base prompt", []models.ChatAttachment{
		{Kind: "text", Text: ptr("more")},
		{Kind: "image_base64", Base64Data: ptr("xyz")},
		{Kind: "image_url", URL: ptr("https://x")},
	})
	assert.Equal(t, []string{"xyz"}, images)
	assert.Contains(t, prompt, "Attachment context:\nmore")
	assert.Contains(t, prompt, "Referenced image URL: https://x")
}

func TestEndpointURLJoinSemantics(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "https://api.openai.com/v1/chat/completions",
		endpointURL("https://api.openai.com/v1", "/chat/completions"))
	assert.Equal(t, "https://api.openai.com/v1/chat/completions",
		endpointURL("https://api.openai.com/v1/", "chat/completions"))
	// Suffix already present → no double-append
	assert.Equal(t, "https://api.openai.com/v1/chat/completions",
		endpointURL("https://api.openai.com/v1/chat/completions", "/chat/completions"))
}

func TestJSONPointerMapAndArray(t *testing.T) {
	t.Parallel()
	root := map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{"content": "hello"},
			},
		},
	}
	assert.Equal(t, "hello", jsonPointer(root, "/choices/0/message/content"))
	assert.Nil(t, jsonPointer(root, "/choices/9/message/content"))
	assert.Nil(t, jsonPointer(root, "/missing"))
}

func TestUsageTokensParsesNumberShapes(t *testing.T) {
	t.Parallel()
	payload := map[string]any{
		"usage": map[string]any{
			"prompt_tokens":     float64(42),
			"completion_tokens": int64(13),
		},
	}
	assert.Equal(t, int32(42), usageTokens(payload, "prompt_tokens"))
	assert.Equal(t, int32(13), usageTokens(payload, "completion_tokens"))
	assert.Equal(t, int32(0), usageTokens(payload, "missing"))
	assert.Equal(t, int32(0), usageTokens(map[string]any{}, "prompt_tokens"))
}

func TestEmbedTextEmptyContentReturnsEmpty(t *testing.T) {
	t.Parallel()
	provider := &models.LlmProvider{APIMode: "chat_completions"}
	emb, err := EmbedText(context.Background(), provider, "   ")
	require.NoError(t, err)
	assert.Empty(t, emb)
}

func TestEmbedTextUnsupportedAPIMode(t *testing.T) {
	t.Parallel()
	provider := &models.LlmProvider{APIMode: "messages"}
	_, err := EmbedText(context.Background(), provider, "hello")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support embeddings")
}

func TestCompleteTextUnsupportedAPIMode(t *testing.T) {
	t.Parallel()
	provider := &models.LlmProvider{APIMode: "made-up"}
	_, err := CompleteText(context.Background(), nil, provider, "", "hi", nil, 0.2, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported provider api_mode")
}

func TestCompleteOpenAICompatibleHappyPath(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		body, _ := io.ReadAll(r.Body)
		var sent map[string]any
		require.NoError(t, json.Unmarshal(body, &sent))
		assert.Equal(t, "gpt-fake", sent["model"])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
            "choices":[{"message":{"content":"hello back"}}],
            "usage":{"prompt_tokens":7,"completion_tokens":4,"total_tokens":11}
        }`))
	}))
	defer server.Close()

	provider := &models.LlmProvider{
		APIMode:     "chat_completions",
		EndpointURL: server.URL,
		ModelName:   "gpt-fake",
	}
	got, err := CompleteText(context.Background(), server.Client(), provider, "", "hi", nil, 0.2, 100)
	require.NoError(t, err)
	assert.Equal(t, "hello back", got.Text)
	assert.Equal(t, int32(7), got.PromptTokens)
	assert.Equal(t, int32(4), got.CompletionTokens)
	assert.Equal(t, int32(11), got.TotalTokens)
}

func TestCompleteOpenAICompatibleNon2XXErrors(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad token"}`))
	}))
	defer server.Close()
	provider := &models.LlmProvider{APIMode: "chat_completions", EndpointURL: server.URL, ModelName: "x"}
	_, err := CompleteText(context.Background(), server.Client(), provider, "", "hi", nil, 0, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider returned 401")
}

func TestCompleteOpenAICompatibleAttachesBearer(t *testing.T) {
	t.Setenv("FAKE_OPENAI_KEY", "sk-test")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()
	cred := "FAKE_OPENAI_KEY"
	provider := &models.LlmProvider{
		APIMode:             "chat_completions",
		EndpointURL:         server.URL,
		ModelName:           "x",
		CredentialReference: &cred,
	}
	_, err := CompleteText(context.Background(), server.Client(), provider, "", "hi", nil, 0, 100)
	require.NoError(t, err)
}

func TestCompleteAnthropicHappyPath(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/messages", r.URL.Path)
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
            "content":[{"text":"claude reply"}],
            "usage":{"input_tokens":3,"output_tokens":5}
        }`))
	}))
	defer server.Close()
	provider := &models.LlmProvider{APIMode: "messages", EndpointURL: server.URL, ModelName: "claude-fake"}
	got, err := CompleteText(context.Background(), server.Client(), provider, "system", "hi", nil, 0, 200)
	require.NoError(t, err)
	assert.Equal(t, "claude reply", got.Text)
	assert.Equal(t, int32(3), got.PromptTokens)
	assert.Equal(t, int32(5), got.CompletionTokens)
	assert.Equal(t, int32(8), got.TotalTokens, "total clamps to prompt+completion when no usage.total")
}

func TestCompleteOllamaHappyPath(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), `"stream":false`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
            "message":{"content":"local reply"},
            "prompt_eval_count":12, "eval_count":34
        }`))
	}))
	defer server.Close()
	provider := &models.LlmProvider{APIMode: "chat", EndpointURL: server.URL, ModelName: "llama3"}
	got, err := CompleteText(context.Background(), server.Client(), provider, "", "hi", nil, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, "local reply", got.Text)
	assert.Equal(t, int32(12), got.PromptTokens)
	assert.Equal(t, int32(34), got.CompletionTokens)
	assert.Equal(t, int32(46), got.TotalTokens)
}

func TestEmbedOpenAICompatibleHappyPath(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/embeddings", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.5,0.25,0.0]}]}`))
	}))
	defer server.Close()
	provider := &models.LlmProvider{APIMode: "chat_completions", EndpointURL: server.URL, ModelName: "ada"}
	emb, err := EmbedTextWith(context.Background(), server.Client(), provider, "hello")
	require.NoError(t, err)
	assert.Equal(t, []float32{0.5, 0.25, 0.0}, emb)
}

func TestEmbedOllamaHappyPath(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/embeddings", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), `"prompt":"hi"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":[0.1,0.2,0.3]}`))
	}))
	defer server.Close()
	provider := &models.LlmProvider{APIMode: "chat", EndpointURL: server.URL, ModelName: "nomic"}
	emb, err := EmbedTextWith(context.Background(), server.Client(), provider, "hi")
	require.NoError(t, err)
	assert.Equal(t, []float32{0.1, 0.2, 0.3}, emb)
}

func TestProviderTokenReadsEnv(t *testing.T) {
	t.Setenv("PROVIDER_TOKEN_TEST", "tok")
	cred := "PROVIDER_TOKEN_TEST"
	provider := &models.LlmProvider{CredentialReference: &cred}
	assert.Equal(t, "tok", providerToken(provider))

	emptyCred := ""
	provider = &models.LlmProvider{CredentialReference: &emptyCred}
	assert.Equal(t, "", providerToken(provider))

	provider = &models.LlmProvider{CredentialReference: nil}
	assert.Equal(t, "", providerToken(provider))
}

func TestEmbedTextOfflineFallbackWhenNoProvider(t *testing.T) {
	t.Parallel()
	emb, err := EmbedText(context.Background(), nil, "hello")
	require.NoError(t, err)
	assert.Len(t, emb, 12)
}

func TestFakeRuntimeRecordsRequestAndReturnsResult(t *testing.T) {
	t.Parallel()
	provider := &models.LlmProvider{APIMode: "fake", ModelName: "fake-model"}
	runtime := &FakeRuntime{Result: CompletionResult{Text: "fake ok", PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5}}
	got, err := runtime.CompleteText(context.Background(), CompletionRequest{
		Provider: provider, SystemPrompt: "system", UserPrompt: "hello", Temperature: 0.2, MaxTokens: 16,
	})
	require.NoError(t, err)
	assert.Equal(t, "fake ok", got.Text)
	require.Len(t, runtime.Calls, 1)
	assert.Equal(t, provider, runtime.Calls[0].Provider)
	assert.Equal(t, "hello", runtime.Calls[0].UserPrompt)
}

func TestCompleteTextFakeAPIMode(t *testing.T) {
	t.Parallel()
	provider := &models.LlmProvider{APIMode: "fake", ModelName: "fake-model"}
	got, err := CompleteText(context.Background(), nil, provider, "system", "hello", nil, 0.2, 16)
	require.NoError(t, err)
	assert.Equal(t, "fake provider response", got.Text)
	assert.Positive(t, got.TotalTokens)
}

func TestCompleteOpenAICompatibleToolCall(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
            "choices":[{"message":{"content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup_dataset","arguments":"{\"id\":\"ds1\"}"}}]}}],
            "usage":{"prompt_tokens":9,"completion_tokens":3,"total_tokens":12}
        }`))
	}))
	defer server.Close()

	provider := &models.LlmProvider{APIMode: "chat_completions", EndpointURL: server.URL, ModelName: "gpt-tools"}
	got, err := CompleteText(context.Background(), server.Client(), provider, "", "call a tool", nil, 0.2, 100)
	require.NoError(t, err)
	require.Len(t, got.ToolCalls, 1)
	assert.Equal(t, "call_1", got.ToolCalls[0].ID)
	assert.Equal(t, "function", got.ToolCalls[0].Type)
	assert.Equal(t, "lookup_dataset", got.ToolCalls[0].Name)
	assert.JSONEq(t, `{"id":"ds1"}`, got.ToolCalls[0].Arguments)
}
