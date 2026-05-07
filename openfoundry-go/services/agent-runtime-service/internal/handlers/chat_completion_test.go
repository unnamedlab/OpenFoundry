package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/domain/llm"
)

func TestCreateChatCompletionUsesRuntimeSuccess(t *testing.T) {
	t.Parallel()
	runtime := &llm.FakeRuntime{Result: llm.CompletionResult{Text: "agent answer", PromptTokens: 8, CompletionTokens: 4, TotalTokens: 12}}
	h := &Handlers{Runtime: runtime}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"model":"fake-model","messages":[{"role":"system","content":"be helpful"},{"role":"user","content":"hello"}]}`))
	w := httptest.NewRecorder()

	h.CreateChatCompletion(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	choices := resp["choices"].([]any)
	message := choices[0].(map[string]any)["message"].(map[string]any)
	assert.Equal(t, "agent answer", message["content"])
	usage := resp["usage"].(map[string]any)
	assert.Equal(t, float64(12), usage["total_tokens"])
	require.Len(t, runtime.Calls, 1)
	assert.Equal(t, "be helpful", runtime.Calls[0].SystemPrompt)
	assert.Equal(t, "hello", runtime.Calls[0].UserPrompt)
}

func TestCreateChatCompletionProviderError(t *testing.T) {
	t.Parallel()
	h := &Handlers{Runtime: &llm.FakeRuntime{Err: errors.New("provider down")}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"messages":[{"role":"user","content":"hello"}]}`))
	w := httptest.NewRecorder()

	h.CreateChatCompletion(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, w.Body.String(), "provider down")
}

func TestCreateChatCompletionRejectsEmptyUserMessage(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"messages":[{"role":"system","content":"only system"}]}`))
	w := httptest.NewRecorder()

	h.CreateChatCompletion(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "chat completion requires a user message")
}
