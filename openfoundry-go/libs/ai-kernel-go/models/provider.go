package models

import (
	"time"

	"github.com/google/uuid"
)

// ProviderRoutingRules controls how the LLM gateway picks a provider.
type ProviderRoutingRules struct {
	UseCases               []string    `json:"use_cases,omitempty"`
	PreferredRegions       []string    `json:"preferred_regions,omitempty"`
	FallbackProviderIDs    []uuid.UUID `json:"fallback_provider_ids,omitempty"`
	Weight                 int32       `json:"weight"`
	MaxContextTokens       int32       `json:"max_context_tokens"`
	NetworkScope           string      `json:"network_scope"`
	SupportedModalities    []string    `json:"supported_modalities"`
	InputCostPer1KTokensUSD  float32   `json:"input_cost_per_1k_tokens_usd"`
	OutputCostPer1KTokensUSD float32   `json:"output_cost_per_1k_tokens_usd"`
}

// DefaultProviderRoutingRules mirrors the Rust impl Default for
// ProviderRoutingRules + serde defaults: weight=100,
// max_context_tokens=32_000, network_scope="public",
// supported_modalities=["text"].
func DefaultProviderRoutingRules() ProviderRoutingRules {
	return ProviderRoutingRules{
		Weight:              100,
		MaxContextTokens:    32_000,
		NetworkScope:        "public",
		SupportedModalities: []string{"text"},
	}
}

// ProviderHealthState is the rolling health probe summary.
type ProviderHealthState struct {
	Status        string    `json:"status"`
	AvgLatencyMs  int32     `json:"avg_latency_ms"`
	ErrorRate     float32   `json:"error_rate"`
	LastCheckedAt time.Time `json:"last_checked_at"`
}

// LlmProvider is the catalog row for one LLM endpoint.
type LlmProvider struct {
	ID                  uuid.UUID            `json:"id"`
	Name                string               `json:"name"`
	ProviderType        string               `json:"provider_type"`
	ModelName           string               `json:"model_name"`
	EndpointURL         string               `json:"endpoint_url"`
	APIMode             string               `json:"api_mode"`
	CredentialReference *string              `json:"credential_reference"`
	CredentialConfigured bool                `json:"credential_configured"`
	Enabled             bool                 `json:"enabled"`
	LoadBalanceWeight   int32                `json:"load_balance_weight"`
	MaxOutputTokens     int32                `json:"max_output_tokens"`
	CostTier            string               `json:"cost_tier"`
	Tags                []string             `json:"tags"`
	RouteRules          ProviderRoutingRules `json:"route_rules"`
	HealthState         ProviderHealthState  `json:"health_state"`
	CreatedAt           time.Time            `json:"created_at"`
	UpdatedAt           time.Time            `json:"updated_at"`
}

// ListProvidersResponse is the {"data": [...]} list envelope.
type ListProvidersResponse struct {
	Data []LlmProvider `json:"data"`
}

// CreateProviderRequest defaults match Rust serde:
// provider_type="openai", model_name="gpt-4.1-mini",
// endpoint_url="https://api.openai.com/v1", api_mode="chat_completions",
// enabled=true, load_balance_weight=100, max_output_tokens=2048,
// cost_tier="standard".
type CreateProviderRequest struct {
	Name                string                 `json:"name"`
	ProviderType        *string                `json:"provider_type,omitempty"`
	ModelName           *string                `json:"model_name,omitempty"`
	EndpointURL         *string                `json:"endpoint_url,omitempty"`
	APIMode             *string                `json:"api_mode,omitempty"`
	CredentialReference *string                `json:"credential_reference,omitempty"`
	Enabled             *bool                  `json:"enabled,omitempty"`
	LoadBalanceWeight   *int32                 `json:"load_balance_weight,omitempty"`
	MaxOutputTokens     *int32                 `json:"max_output_tokens,omitempty"`
	CostTier            *string                `json:"cost_tier,omitempty"`
	Tags                []string               `json:"tags,omitempty"`
	RouteRules          *ProviderRoutingRules  `json:"route_rules,omitempty"`
}

type UpdateProviderRequest struct {
	Name                *string                `json:"name,omitempty"`
	ProviderType        *string                `json:"provider_type,omitempty"`
	ModelName           *string                `json:"model_name,omitempty"`
	EndpointURL         *string                `json:"endpoint_url,omitempty"`
	APIMode             *string                `json:"api_mode,omitempty"`
	CredentialReference *string                `json:"credential_reference,omitempty"`
	Enabled             *bool                  `json:"enabled,omitempty"`
	LoadBalanceWeight   *int32                 `json:"load_balance_weight,omitempty"`
	MaxOutputTokens     *int32                 `json:"max_output_tokens,omitempty"`
	CostTier            *string                `json:"cost_tier,omitempty"`
	Tags                *[]string              `json:"tags,omitempty"`
	RouteRules          *ProviderRoutingRules  `json:"route_rules,omitempty"`
	HealthState         *ProviderHealthState   `json:"health_state,omitempty"`
}

// Provider creation defaults — exposed for the HTTP-handler slice.
const (
	DefaultProviderType    = "openai"
	DefaultModelName       = "gpt-4.1-mini"
	DefaultEndpointURL     = "https://api.openai.com/v1"
	DefaultAPIMode         = "chat_completions"
	DefaultCostTier        = "standard"
	DefaultLoadBalanceWeight int32 = 100
	DefaultMaxOutputTokens   int32 = 2048
)
