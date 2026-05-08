package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// GuardrailFlag is one annotation produced by the guardrail evaluator.
type GuardrailFlag struct {
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	Excerpt  string `json:"excerpt"`
}

// GuardrailVerdict bundles the evaluator's status + redacted text.
type GuardrailVerdict struct {
	Status       string          `json:"status"`
	RedactedText string          `json:"redacted_text"`
	Blocked      bool            `json:"blocked"`
	Flags        []GuardrailFlag `json:"flags"`
}

// DefaultGuardrailVerdict mirrors Rust impl Default — status=passed,
// blocked=false, empty redacted text + flags.
func DefaultGuardrailVerdict() GuardrailVerdict {
	return GuardrailVerdict{
		Status: "passed",
		Flags:  []GuardrailFlag{},
	}
}

// ChatAttachment is a multimodal attachment on a chat message.
// Default kind="text".
type ChatAttachment struct {
	Kind       string  `json:"kind"`
	Name       *string `json:"name"`
	MimeType   *string `json:"mime_type"`
	URL        *string `json:"url"`
	Base64Data *string `json:"base64_data"`
	Text       *string `json:"text"`
}

// ChatMessage is one turn in a conversation.
type ChatMessage struct {
	Role             string                  `json:"role"`
	Content          string                  `json:"content"`
	ProviderID       *uuid.UUID              `json:"provider_id"`
	ToolName         *string                 `json:"tool_name"`
	Citations        []KnowledgeSearchResult `json:"citations"`
	Attachments      []ChatAttachment        `json:"attachments"`
	GuardrailVerdict *GuardrailVerdict       `json:"guardrail_verdict"`
	CreatedAt        time.Time               `json:"created_at"`
}

// LlmUsageSummary is the per-call cost + latency footer.
type LlmUsageSummary struct {
	PromptTokens     int32   `json:"prompt_tokens"`
	CompletionTokens int32   `json:"completion_tokens"`
	TotalTokens      int32   `json:"total_tokens"`
	EstimatedCostUSD float32 `json:"estimated_cost_usd"`
	LatencyMs        int32   `json:"latency_ms"`
	NetworkScope     string  `json:"network_scope"`
	CacheHit         bool    `json:"cache_hit"`
}

// ChatRoutingMetadata explains which providers got picked + why.
type ChatRoutingMetadata struct {
	RequestedPrivateNetwork bool        `json:"requested_private_network"`
	UsedPrivateNetwork      bool        `json:"used_private_network"`
	PrivacyReason           *string     `json:"privacy_reason"`
	CandidateProviderIDs    []uuid.UUID `json:"candidate_provider_ids"`
	RequiredModalities      []string    `json:"required_modalities"`
}

// SemanticCacheMetadata is the cache footer carried on every reply.
type SemanticCacheMetadata struct {
	CacheKey        string  `json:"cache_key"`
	Hit             bool    `json:"hit"`
	SimilarityScore float32 `json:"similarity_score"`
}

// Conversation is the persisted chat row.
type Conversation struct {
	ID                   uuid.UUID     `json:"id"`
	Title                string        `json:"title"`
	Messages             []ChatMessage `json:"messages"`
	ProviderID           *uuid.UUID    `json:"provider_id"`
	LastCacheHit         bool          `json:"last_cache_hit"`
	LastGuardrailBlocked bool          `json:"last_guardrail_blocked"`
	CreatedAt            time.Time     `json:"created_at"`
	LastActivityAt       time.Time     `json:"last_activity_at"`
}

// ConversationSummary is the trimmed list-view row.
type ConversationSummary struct {
	ID                 uuid.UUID  `json:"id"`
	Title              string     `json:"title"`
	LastMessagePreview string     `json:"last_message_preview"`
	ProviderID         *uuid.UUID `json:"provider_id"`
	MessageCount       int32      `json:"message_count"`
	LastCacheHit       bool       `json:"last_cache_hit"`
	LastActivityAt     time.Time  `json:"last_activity_at"`
}

type ListConversationsResponse struct {
	Data []ConversationSummary `json:"data"`
}

// ChatCompletionRequest defaults: fallback_enabled=true,
// temperature=0.2, max_tokens=1024.
type ChatCompletionRequest struct {
	ConversationID        *uuid.UUID       `json:"conversation_id"`
	SystemPrompt          *string          `json:"system_prompt"`
	UserMessage           string           `json:"user_message"`
	PurposeJustification  *string          `json:"purpose_justification"`
	PromptTemplateID      *uuid.UUID       `json:"prompt_template_id"`
	PromptVariables       json.RawMessage  `json:"prompt_variables"`
	KnowledgeBaseID       *uuid.UUID       `json:"knowledge_base_id"`
	PreferredProviderID   *uuid.UUID       `json:"preferred_provider_id"`
	Attachments           []ChatAttachment `json:"attachments"`
	FallbackEnabled       bool             `json:"fallback_enabled"`
	RequirePrivateNetwork bool             `json:"require_private_network"`
	Temperature           float32          `json:"temperature"`
	MaxTokens             int32            `json:"max_tokens"`
}

func (r *ChatCompletionRequest) UnmarshalJSON(data []byte) error {
	type alias ChatCompletionRequest
	aux := struct {
		FallbackEnabled *bool    `json:"fallback_enabled"`
		Temperature     *float32 `json:"temperature"`
		MaxTokens       *int32   `json:"max_tokens"`
		*alias
	}{
		alias: (*alias)(r),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.FallbackEnabled == nil {
		r.FallbackEnabled = DefaultFallbackEnabled
	} else {
		r.FallbackEnabled = *aux.FallbackEnabled
	}
	if aux.Temperature == nil {
		r.Temperature = DefaultTemperature
	} else {
		r.Temperature = *aux.Temperature
	}
	if aux.MaxTokens == nil {
		r.MaxTokens = DefaultMaxTokens
	} else {
		r.MaxTokens = *aux.MaxTokens
	}
	return nil
}

type ChatCompletionResponse struct {
	ConversationID   uuid.UUID               `json:"conversation_id"`
	ProviderID       uuid.UUID               `json:"provider_id"`
	ProviderName     string                  `json:"provider_name"`
	Reply            string                  `json:"reply"`
	Citations        []KnowledgeSearchResult `json:"citations"`
	Guardrail        GuardrailVerdict        `json:"guardrail"`
	Cache            SemanticCacheMetadata   `json:"cache"`
	PromptUsed       string                  `json:"prompt_used"`
	CompletionTokens int32                   `json:"completion_tokens"`
	Usage            LlmUsageSummary         `json:"usage"`
	Routing          ChatRoutingMetadata     `json:"routing"`
	CreatedAt        time.Time               `json:"created_at"`
}

// CopilotRequest defaults: include_sql=true, include_pipeline_plan=true.
type CopilotRequest struct {
	Question             string      `json:"question"`
	PurposeJustification *string     `json:"purpose_justification"`
	DatasetIDs           []uuid.UUID `json:"dataset_ids"`
	OntologyTypeIDs      []uuid.UUID `json:"ontology_type_ids"`
	KnowledgeBaseIDs     []uuid.UUID `json:"knowledge_base_ids"`
	IncludeSQL           bool        `json:"include_sql"`
	IncludePipelinePlan  bool        `json:"include_pipeline_plan"`
	PreferredProviderID  *uuid.UUID  `json:"preferred_provider_id"`
}

func (r *CopilotRequest) UnmarshalJSON(data []byte) error {
	type alias CopilotRequest
	aux := struct {
		IncludeSQL          *bool `json:"include_sql"`
		IncludePipelinePlan *bool `json:"include_pipeline_plan"`
		*alias
	}{
		alias: (*alias)(r),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.IncludeSQL == nil {
		r.IncludeSQL = DefaultIncludeSQL
	} else {
		r.IncludeSQL = *aux.IncludeSQL
	}
	if aux.IncludePipelinePlan == nil {
		r.IncludePipelinePlan = DefaultIncludePipeline
	} else {
		r.IncludePipelinePlan = *aux.IncludePipelinePlan
	}
	return nil
}

type CopilotResponse struct {
	Answer              string                  `json:"answer"`
	SuggestedSQL        *string                 `json:"suggested_sql"`
	PipelineSuggestions []string                `json:"pipeline_suggestions"`
	OntologyHints       []string                `json:"ontology_hints"`
	CitedKnowledge      []KnowledgeSearchResult `json:"cited_knowledge"`
	ProviderName        string                  `json:"provider_name"`
	Cache               SemanticCacheMetadata   `json:"cache"`
	Usage               LlmUsageSummary         `json:"usage"`
	CreatedAt           time.Time               `json:"created_at"`
}

type EvaluateGuardrailsRequest struct {
	Content string `json:"content"`
}

type EvaluateGuardrailsResponse struct {
	Verdict         GuardrailVerdict `json:"verdict"`
	RiskScore       float32          `json:"risk_score"`
	Recommendations []string         `json:"recommendations"`
}

// ProviderBenchmarkRequest defaults: use_case="chat", max_tokens=1024.
type ProviderBenchmarkRequest struct {
	Prompt                string           `json:"prompt"`
	SystemPrompt          *string          `json:"system_prompt"`
	ProviderIDs           []uuid.UUID      `json:"provider_ids"`
	Attachments           []ChatAttachment `json:"attachments"`
	RubricKeywords        []string         `json:"rubric_keywords"`
	UseCase               string           `json:"use_case"`
	MaxTokens             int32            `json:"max_tokens"`
	RequirePrivateNetwork bool             `json:"require_private_network"`
}

type ProviderBenchmarkScore struct {
	Quality float32 `json:"quality"`
	Latency float32 `json:"latency"`
	Cost    float32 `json:"cost"`
	Safety  float32 `json:"safety"`
	Overall float32 `json:"overall"`
}

type ProviderBenchmarkResult struct {
	ProviderID       uuid.UUID              `json:"provider_id"`
	ProviderName     string                 `json:"provider_name"`
	NetworkScope     string                 `json:"network_scope"`
	ReplyPreview     string                 `json:"reply_preview"`
	PromptTokens     int32                  `json:"prompt_tokens"`
	CompletionTokens int32                  `json:"completion_tokens"`
	TotalTokens      int32                  `json:"total_tokens"`
	EstimatedCostUSD float32                `json:"estimated_cost_usd"`
	LatencyMs        int32                  `json:"latency_ms"`
	CacheHit         bool                   `json:"cache_hit"`
	Guardrail        GuardrailVerdict       `json:"guardrail"`
	Score            ProviderBenchmarkScore `json:"score"`
	Error            *string                `json:"error"`
}

type ProviderBenchmarkResponse struct {
	BenchmarkGroupID        uuid.UUID                 `json:"benchmark_group_id"`
	UseCase                 string                    `json:"use_case"`
	PromptExcerpt           string                    `json:"prompt_excerpt"`
	RequiredModalities      []string                  `json:"required_modalities"`
	RequestedPrivateNetwork bool                      `json:"requested_private_network"`
	RecommendedProviderID   *uuid.UUID                `json:"recommended_provider_id"`
	Results                 []ProviderBenchmarkResult `json:"results"`
	CreatedAt               time.Time                 `json:"created_at"`
}

// Conversation defaults — exposed for the HTTP-handler slice.
const (
	DefaultAttachmentKind   = "text"
	DefaultBenchmarkUseCase = "chat"
	DefaultFallbackEnabled  = true
	DefaultIncludeSQL       = true
	DefaultIncludePipeline  = true
	DefaultTemperature      = float32(0.2)
	DefaultMaxTokens        = int32(1024)
)

func (r *ChatAttachment) UnmarshalJSON(data []byte) error {
	type alias ChatAttachment
	if err := json.Unmarshal(data, (*alias)(r)); err != nil {
		return err
	}
	if r.Kind == "" {
		r.Kind = DefaultAttachmentKind
	}
	return nil
}
