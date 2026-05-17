// Package anthropic implements the Anthropic Messages API as an
// llm.Runtime. It is enabled when ANTHROPIC_API_KEY is set and lets
// services (e.g. agent-runtime-service) hit a real provider without a
// provisioned models.LlmProvider catalog row.
//
// This slice supports non-streaming text completion only. Tool-use,
// streaming, and multi-turn agent loops are intentionally out of scope.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/domain/llm"
)

const (
	DefaultBaseURL = "https://api.anthropic.com/v1"
	DefaultModel   = "claude-sonnet-4-6"
	APIVersion     = "2023-06-01"

	EnvAPIKey  = "ANTHROPIC_API_KEY"
	EnvModel   = "ANTHROPIC_MODEL"
	EnvBaseURL = "ANTHROPIC_BASE_URL"
)

// Provider is an llm.Runtime backed by Anthropic's Messages API.
type Provider struct {
	APIKey  string
	Model   string
	BaseURL string
	Client  *http.Client
}

// New constructs a Provider, filling in defaults for empty arguments.
// The model defaults to claude-sonnet-4-6 and the base URL to
// https://api.anthropic.com/v1. A nil client is replaced with a client
// using a 60s timeout.
func New(apiKey, model, baseURL string, client *http.Client) *Provider {
	if strings.TrimSpace(model) == "" {
		model = DefaultModel
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &Provider{APIKey: apiKey, Model: model, BaseURL: baseURL, Client: client}
}

// FromEnv builds a Provider from ANTHROPIC_API_KEY (required) plus
// optional ANTHROPIC_MODEL and ANTHROPIC_BASE_URL overrides. Returns
// (nil, false) when no API key is set, so callers can fall back to a
// FakeRuntime.
func FromEnv() (*Provider, bool) {
	key := strings.TrimSpace(os.Getenv(EnvAPIKey))
	if key == "" {
		return nil, false
	}
	return New(
		key,
		strings.TrimSpace(os.Getenv(EnvModel)),
		strings.TrimSpace(os.Getenv(EnvBaseURL)),
		nil,
	), true
}

// CompleteText satisfies llm.Runtime. It posts a single user message to
// /messages and parses the first text block of the response. If the
// CompletionRequest carries a provider with a non-empty model name,
// that model wins over the Provider's default.
func (p *Provider) CompleteText(ctx context.Context, req llm.CompletionRequest) (llm.CompletionResult, error) {
	if p == nil || strings.TrimSpace(p.APIKey) == "" {
		return llm.CompletionResult{}, fmt.Errorf("anthropic provider not configured: %s missing", EnvAPIKey)
	}

	model := p.Model
	if req.Provider != nil && strings.TrimSpace(req.Provider.ModelName) != "" {
		model = req.Provider.ModelName
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	body := map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": []map[string]any{{"type": "text", "text": req.UserPrompt}},
			},
		},
	}
	if strings.TrimSpace(req.SystemPrompt) != "" {
		body["system"] = req.SystemPrompt
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return llm.CompletionResult{}, fmt.Errorf("anthropic encode request: %w", err)
	}

	url := strings.TrimRight(p.BaseURL, "/") + "/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return llm.CompletionResult{}, fmt.Errorf("anthropic build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", APIVersion)
	httpReq.Header.Set("x-api-key", p.APIKey)

	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return llm.CompletionResult{}, fmt.Errorf("anthropic request failed: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return llm.CompletionResult{}, fmt.Errorf("anthropic response read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return llm.CompletionResult{}, fmt.Errorf("anthropic returned %d: %s", resp.StatusCode, string(payload))
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int32 `json:"input_tokens"`
			OutputTokens int32 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return llm.CompletionResult{}, fmt.Errorf("anthropic response parse: %w", err)
	}

	var text strings.Builder
	for _, part := range parsed.Content {
		if part.Type == "text" {
			text.WriteString(part.Text)
		}
	}
	return llm.CompletionResult{
		Text:             text.String(),
		PromptTokens:     parsed.Usage.InputTokens,
		CompletionTokens: parsed.Usage.OutputTokens,
		TotalTokens:      parsed.Usage.InputTokens + parsed.Usage.OutputTokens,
	}, nil
}

// Ensure interface compliance at compile time.
var _ llm.Runtime = (*Provider)(nil)
