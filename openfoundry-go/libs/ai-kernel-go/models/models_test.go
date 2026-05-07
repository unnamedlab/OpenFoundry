package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderRoutingRulesDefaultsMatchRust(t *testing.T) {
	t.Parallel()
	got := DefaultProviderRoutingRules()
	assert.Equal(t, int32(100), got.Weight)
	assert.Equal(t, int32(32_000), got.MaxContextTokens)
	assert.Equal(t, "public", got.NetworkScope)
	assert.Equal(t, []string{"text"}, got.SupportedModalities)
}

func TestProviderConstantsMatchRust(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "openai", DefaultProviderType)
	assert.Equal(t, "gpt-4.1-mini", DefaultModelName)
	assert.Equal(t, "https://api.openai.com/v1", DefaultEndpointURL)
	assert.Equal(t, "chat_completions", DefaultAPIMode)
	assert.Equal(t, "standard", DefaultCostTier)
	assert.Equal(t, int32(100), DefaultLoadBalanceWeight)
	assert.Equal(t, int32(2048), DefaultMaxOutputTokens)
}

func TestAgentDefaultsMatchRust(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "active", DefaultAgentStatus)
	assert.Equal(t, "plan-act-observe", DefaultAgentPlanningStrategy)
	assert.Equal(t, int32(3), DefaultAgentMaxIterations)
}

func TestToolDefaultsMatchRust(t *testing.T) {
	t.Parallel()
	// Rust default_tool_category() = "analysis", default_execution_mode() = "simulated".
	assert.Equal(t, "analysis", DefaultToolCategory)
	assert.Equal(t, "simulated", DefaultToolExecutionMode)
	assert.Equal(t, "active", DefaultToolStatus)
}

func TestPromptDefaultsMatchRust(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "copilot", DefaultPromptCategory)
}

func TestKnowledgeBaseDefaultsMatchRust(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "active", DefaultKnowledgeStatus)
	assert.Equal(t, "deterministic-hash", DefaultEmbeddingProvider)
	assert.Equal(t, "balanced", DefaultChunkingStrategy)
	assert.Equal(t, uint32(5), DefaultSearchTopK)
	assert.InDelta(t, float32(0.55), DefaultSearchMinScore, 1e-6)
}

func TestAiPlatformOverviewSnakeCaseShape(t *testing.T) {
	t.Parallel()
	o := AiPlatformOverview{
		ProviderCount:           3,
		PrivateProviderCount:    1,
		MultimodalProviderCount: 2,
		PromptCount:             5,
		KnowledgeBaseCount:      4,
		IndexedDocumentCount:    100,
		IndexedChunkCount:       1000,
		AgentCount:              7,
		ConversationCount:       42,
		CacheEntryCount:         12,
		CacheHitRate:            0.78,
		BlockedGuardrailEvents:  3,
		LlmPromptTokens:         5000,
		LlmCompletionTokens:     2500,
		EstimatedLlmCostUSD:     1.23,
		BenchmarkRunCount:       9,
	}
	b, err := json.Marshal(o)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	for _, k := range []string{
		"provider_count", "private_provider_count", "multimodal_provider_count",
		"prompt_count", "knowledge_base_count", "indexed_document_count",
		"indexed_chunk_count", "agent_count", "conversation_count",
		"cache_entry_count", "cache_hit_rate", "blocked_guardrail_events",
		"llm_prompt_tokens", "llm_completion_tokens", "estimated_llm_cost_usd",
		"benchmark_run_count",
	} {
		assert.Contains(t, got, k, "snake_case key %s missing", k)
	}
}

func TestLlmProviderJSONFields(t *testing.T) {
	t.Parallel()
	p := LlmProvider{
		Name:                 "openai-prod",
		ProviderType:         "openai",
		ModelName:            "gpt-4.1-mini",
		EndpointURL:          "https://api.openai.com/v1",
		APIMode:              "chat_completions",
		Enabled:              true,
		LoadBalanceWeight:    100,
		MaxOutputTokens:      2048,
		CostTier:             "standard",
		Tags:                 []string{"prod"},
		RouteRules:           DefaultProviderRoutingRules(),
		HealthState:          ProviderHealthState{Status: "healthy", AvgLatencyMs: 620, ErrorRate: 0.01},
		CredentialConfigured: false,
	}
	b, err := json.Marshal(p)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "openai", got["provider_type"])
	assert.Equal(t, "gpt-4.1-mini", got["model_name"])
	assert.Equal(t, "https://api.openai.com/v1", got["endpoint_url"])
	assert.Equal(t, "chat_completions", got["api_mode"])
	assert.Equal(t, false, got["credential_configured"])
	assert.Equal(t, "standard", got["cost_tier"])

	rr := got["route_rules"].(map[string]any)
	assert.Equal(t, "public", rr["network_scope"])
	assert.Equal(t, float64(32000), rr["max_context_tokens"])
}

func TestSearchKnowledgeBaseRequestRoundTrip(t *testing.T) {
	t.Parallel()
	req := SearchKnowledgeBaseRequest{Query: "what is foundry", TopK: 5, MinScore: 0.55}
	b, err := json.Marshal(req)
	require.NoError(t, err)
	var back SearchKnowledgeBaseRequest
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, req, back)
}

func TestPromptVersionInputVariablesOmitempty(t *testing.T) {
	t.Parallel()
	v := PromptVersion{VersionNumber: 1, Content: "hi", Notes: "first"}
	b, err := json.Marshal(v)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "input_variables",
		"input_variables omitempty when empty (matches Rust serde default skip_serializing_if-equivalent)")
}

func TestKnowledgeChunkEmbeddingOmitempty(t *testing.T) {
	t.Parallel()
	c := KnowledgeChunk{ID: "c1", Position: 0, Text: "hello", TokenCount: 1}
	b, err := json.Marshal(c)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "embedding")
	assert.NotContains(t, string(b), "metadata")
}

func TestChatCompletionRequestDefaultsOnUnmarshal(t *testing.T) {
	t.Parallel()
	var req ChatCompletionRequest
	require.NoError(t, json.Unmarshal([]byte(`{"user_message":"hello"}`), &req))
	assert.True(t, req.FallbackEnabled)
	assert.Equal(t, DefaultTemperature, req.Temperature)
	assert.Equal(t, DefaultMaxTokens, req.MaxTokens)
}
