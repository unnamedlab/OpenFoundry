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

func TestAskCopilotUsesRuntimeHappyPath(t *testing.T) {
	t.Parallel()
	runtime := &llm.FakeRuntime{Result: llm.CompletionResult{Text: "copilot says reroute", PromptTokens: 11, CompletionTokens: 5, TotalTokens: 16}}
	h := &Handlers{Runtime: runtime}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"question":"How do I reroute overloaded providers with SQL?"}`))
	w := httptest.NewRecorder()

	h.AskCopilot(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "copilot says reroute", resp["answer"])
	assert.Equal(t, "agent-runtime-fake", resp["provider_name"])
	assert.NotContains(t, resp["answer"], "agent-runtime stub")
	assert.NotEmpty(t, resp["suggested_sql"])
	assert.NotEmpty(t, resp["pipeline_suggestions"])
	usage := resp["usage"].(map[string]any)
	assert.Equal(t, float64(16), usage["total_tokens"])
	require.Len(t, runtime.Calls, 1)
	assert.Equal(t, "You are OpenFoundry Copilot. Ground answers in retrieval context and suggested platform actions.", runtime.Calls[0].SystemPrompt)
	assert.Contains(t, runtime.Calls[0].UserPrompt, "Draft answer: Copilot reviewed")
}

func TestAskCopilotRejectsMissingQuestion(t *testing.T) {
	t.Parallel()
	h := &Handlers{Runtime: &llm.FakeRuntime{}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"question":"   "}`))
	w := httptest.NewRecorder()

	h.AskCopilot(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "copilot question is required")
}

func TestAskCopilotProviderError(t *testing.T) {
	t.Parallel()
	h := &Handlers{Runtime: &llm.FakeRuntime{Err: errors.New("copilot provider down")}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"question":"How should I debug this?"}`))
	w := httptest.NewRecorder()

	h.AskCopilot(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, w.Body.String(), "copilot provider down")
}

func TestAskCopilotPassesRequestContextToRuntime(t *testing.T) {
	t.Parallel()
	runtime := &llm.FakeRuntime{Result: llm.CompletionResult{Text: "context-aware answer"}}
	h := &Handlers{Runtime: runtime}
	body := `{
		"question":"Map this to an ontology object",
		"purpose_justification":"incident response",
		"dataset_ids":["11111111-1111-1111-1111-111111111111"],
		"ontology_type_ids":["22222222-2222-2222-2222-222222222222"],
		"knowledge_base_ids":["33333333-3333-3333-3333-333333333333"]
	}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.AskCopilot(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, runtime.Calls, 1)
	prompt := runtime.Calls[0].UserPrompt
	assert.Contains(t, prompt, "Dataset IDs: [11111111-1111-1111-1111-111111111111]")
	assert.Contains(t, prompt, "Ontology type IDs: [22222222-2222-2222-2222-222222222222]")
	assert.Contains(t, prompt, "Knowledge base IDs: [33333333-3333-3333-3333-333333333333]")
	assert.Contains(t, prompt, "Purpose justification: incident response")
	assert.Contains(t, prompt, "Map the response to ontology type 22222222222222222222222222222222")
}

func TestAskCopilotKnowledgeContextPlaceholderAndOptions(t *testing.T) {
	t.Parallel()
	runtime := &llm.FakeRuntime{Result: llm.CompletionResult{Text: "no extras"}}
	h := &Handlers{Runtime: runtime}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"question":"Summarize provider status","include_sql":false,"include_pipeline_plan":false,"knowledge_base_ids":["44444444-4444-4444-4444-444444444444"]}`))
	w := httptest.NewRecorder()

	h.AskCopilot(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Nil(t, resp["suggested_sql"])
	assert.Empty(t, resp["pipeline_suggestions"])
	assert.Empty(t, resp["cited_knowledge"])
	require.Len(t, runtime.Calls, 1)
	assert.Contains(t, runtime.Calls[0].UserPrompt, "Knowledge base IDs: [44444444-4444-4444-4444-444444444444]")
	assert.Contains(t, runtime.Calls[0].UserPrompt, "Knowledge context:\n")
}

func TestCreateChatCompletionJSONContract(t *testing.T) {
	t.Parallel()
	runtime := &llm.FakeRuntime{Result: llm.CompletionResult{Text: "contract answer", PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5}}
	h := &Handlers{Runtime: runtime}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"model":"contract-model","messages":[{"role":"user","content":"hello"}],"temperature":0.1,"max_tokens":64}`))
	w := httptest.NewRecorder()

	h.CreateChatCompletion(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			Index   int `json:"index"`
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int32 `json:"prompt_tokens"`
			CompletionTokens int32 `json:"completion_tokens"`
			TotalTokens      int32 `json:"total_tokens"`
		} `json:"usage"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.NotEmpty(t, resp.ID)
	assert.Equal(t, "chat.completion", resp.Object)
	assert.Equal(t, "contract-model", resp.Model)
	require.Len(t, resp.Choices, 1)
	assert.Equal(t, 0, resp.Choices[0].Index)
	assert.Equal(t, "assistant", resp.Choices[0].Message.Role)
	assert.Equal(t, "contract answer", resp.Choices[0].Message.Content)
	assert.Equal(t, "stop", resp.Choices[0].FinishReason)
	assert.Equal(t, int32(3), resp.Usage.PromptTokens)
	assert.Equal(t, int32(2), resp.Usage.CompletionTokens)
	assert.Equal(t, int32(5), resp.Usage.TotalTokens)
	require.Len(t, runtime.Calls, 1)
	assert.Equal(t, float32(0.1), runtime.Calls[0].Temperature)
	assert.Equal(t, int32(64), runtime.Calls[0].MaxTokens)
}
