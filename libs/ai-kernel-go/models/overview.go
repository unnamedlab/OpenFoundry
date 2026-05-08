// Package models is the wire-format DTO surface of the OpenFoundry
// AI plane (libs/ai-kernel in Rust).
//
// This is the foundation slice of the libs/ai-kernel-go port — only
// the plain-DTO sub-modules land here: provider, agent, prompt
// template, tool, knowledge base, plus the AiPlatformOverview
// summary. Conversation models defer to a follow-up slice because
// they cross-reference domain types (GuardrailVerdict,
// SemanticCacheMetadata, LlmUsageSummary, ChatRoutingMetadata)
// that haven't been ported yet.
//
// Wire-compat: all JSON tags are snake_case matching Rust serde
// defaults; defaults match the Rust `default = "fn"` annotations
// verbatim and are pinned by tests.
package models

// AiPlatformOverview is the canonical summary payload the AI
// platform UI renders on the landing card.
type AiPlatformOverview struct {
	ProviderCount           int64   `json:"provider_count"`
	PrivateProviderCount    int64   `json:"private_provider_count"`
	MultimodalProviderCount int64   `json:"multimodal_provider_count"`
	PromptCount             int64   `json:"prompt_count"`
	KnowledgeBaseCount      int64   `json:"knowledge_base_count"`
	IndexedDocumentCount    int64   `json:"indexed_document_count"`
	IndexedChunkCount       int64   `json:"indexed_chunk_count"`
	AgentCount              int64   `json:"agent_count"`
	ConversationCount       int64   `json:"conversation_count"`
	CacheEntryCount         int64   `json:"cache_entry_count"`
	CacheHitRate            float32 `json:"cache_hit_rate"`
	BlockedGuardrailEvents  int64   `json:"blocked_guardrail_events"`
	LlmPromptTokens         int64   `json:"llm_prompt_tokens"`
	LlmCompletionTokens     int64   `json:"llm_completion_tokens"`
	EstimatedLlmCostUSD     float64 `json:"estimated_llm_cost_usd"`
	BenchmarkRunCount       int64   `json:"benchmark_run_count"`
}
