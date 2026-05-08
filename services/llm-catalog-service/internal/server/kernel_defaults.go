package server

import (
	"encoding/json"
	"net/http"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

type kernelDefaultsResponse struct {
	ProviderType       string                      `json:"provider_type"`
	ModelName          string                      `json:"model_name"`
	EndpointURL        string                      `json:"endpoint_url"`
	APIMode            string                      `json:"api_mode"`
	RouteRules         models.ProviderRoutingRules `json:"route_rules"`
	ToolCategory       string                      `json:"tool_category"`
	ToolExecutionMode  string                      `json:"tool_execution_mode"`
	SupportedToolModes []string                    `json:"supported_tool_modes"`
	PromptCategory     string                      `json:"prompt_category"`
	KnowledgeStatus    string                      `json:"knowledge_status"`
	FallbackEnabled    bool                        `json:"fallback_enabled"`
	Temperature        float32                     `json:"temperature"`
	MaxTokens          int32                       `json:"max_tokens"`
}

func writeKernelDefaults(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(kernelDefaultsResponse{
		ProviderType:       models.DefaultProviderType,
		ModelName:          models.DefaultModelName,
		EndpointURL:        models.DefaultEndpointURL,
		APIMode:            models.DefaultAPIMode,
		RouteRules:         models.DefaultProviderRoutingRules(),
		ToolCategory:       models.DefaultToolCategory,
		ToolExecutionMode:  models.DefaultToolExecutionMode,
		SupportedToolModes: models.SupportedExecutionModes(),
		PromptCategory:     models.DefaultPromptCategory,
		KnowledgeStatus:    models.DefaultKnowledgeStatus,
		FallbackEnabled:    models.DefaultFallbackEnabled,
		Temperature:        models.DefaultTemperature,
		MaxTokens:          models.DefaultMaxTokens,
	})
}
