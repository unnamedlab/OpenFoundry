package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

// CompletionResult bundles a model's text reply with token usage.
// Mirrors Rust struct CompletionResult.
type CompletionResult struct {
	Text             string
	PromptTokens     int32
	CompletionTokens int32
	TotalTokens      int32
	ToolCalls        []ToolCall
}

// ToolCall captures OpenAI-compatible tool-call metadata when a chat
// model elects to call a tool instead of returning plain text. Rust's
// chat response contract does not expose tool calls yet, but retaining
// the data at runtime level lets agent-runtime-service and tests assert
// provider capability without changing the handler JSON contract.
type ToolCall struct {
	ID        string
	Type      string
	Name      string
	Arguments string
}

// CompletionRequest is the provider-runtime invocation shape shared by
// HTTPRuntime and FakeRuntime.
type CompletionRequest struct {
	Provider     *models.LlmProvider
	SystemPrompt string
	UserPrompt   string
	Attachments  []models.ChatAttachment
	Temperature  float32
	MaxTokens    int32
}

// Runtime is the provider dispatch interface used by chat handlers.
type Runtime interface {
	CompleteText(ctx context.Context, req CompletionRequest) (CompletionResult, error)
}

// HTTPRuntime dispatches requests to the configured provider over its
// Rust-compatible API mode.
type HTTPRuntime struct {
	Client *http.Client
}

func (r HTTPRuntime) CompleteText(ctx context.Context, req CompletionRequest) (CompletionResult, error) {
	return CompleteText(ctx, r.Client, req.Provider, req.SystemPrompt, req.UserPrompt, req.Attachments, req.Temperature, req.MaxTokens)
}

// FakeRuntime is a deterministic in-process provider for tests and
// local service wiring. It records requests and returns Result unless
// Err is set. When Result is zero-valued it echoes a stable response.
type FakeRuntime struct {
	Result CompletionResult
	Err    error
	Calls  []CompletionRequest
}

func (f *FakeRuntime) CompleteText(_ context.Context, req CompletionRequest) (CompletionResult, error) {
	f.Calls = append(f.Calls, req)
	if f.Err != nil {
		return CompletionResult{}, f.Err
	}
	if f.Result.Text != "" || f.Result.PromptTokens != 0 || f.Result.CompletionTokens != 0 || f.Result.TotalTokens != 0 || len(f.Result.ToolCalls) > 0 {
		return f.Result, nil
	}
	completionTokens := EstimateTokens("fake provider response")
	promptTokens := EstimateTokens(req.SystemPrompt + " " + req.UserPrompt)
	return CompletionResult{
		Text:             "fake provider response",
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}, nil
}

// CompleteText routes per-provider api_mode to the matching protocol
// implementation. Mirrors libs/ai-kernel/src/domain/llm/runtime.rs's
// fn complete_text. The chat handler receives the runtime's full
// reply + token counts; surrounding code synthesises the
// LlmUsageSummary + ChatRoutingMetadata.
//
// The HTTP client is injected so callers can wire timeouts, retries
// or test fakes. nil falls back to the package-level default
// http.DefaultClient.
func CompleteText(
	ctx context.Context,
	client *http.Client,
	provider *models.LlmProvider,
	systemPrompt, userPrompt string,
	attachments []models.ChatAttachment,
	temperature float32,
	maxTokens int32,
) (CompletionResult, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if provider == nil {
		return CompletionResult{}, fmt.Errorf("provider required")
	}
	switch provider.APIMode {
	case "chat_completions":
		return completeOpenAICompatible(ctx, client, provider, systemPrompt, userPrompt, attachments, temperature, maxTokens)
	case "messages":
		return completeAnthropic(ctx, client, provider, systemPrompt, userPrompt, attachments, maxTokens)
	case "chat":
		return completeOllama(ctx, client, provider, systemPrompt, userPrompt, attachments)
	case "fake":
		rt := &FakeRuntime{}
		return rt.CompleteText(ctx, CompletionRequest{Provider: provider, SystemPrompt: systemPrompt, UserPrompt: userPrompt, Attachments: attachments, Temperature: temperature, MaxTokens: maxTokens})
	default:
		return CompletionResult{}, fmt.Errorf("unsupported provider api_mode '%s'", provider.APIMode)
	}
}

// EmbedText routes per-api_mode for embedding requests. Returns the
// raw vector. Mirrors fn embed_text.
func EmbedText(ctx context.Context, provider *models.LlmProvider, content string) ([]float32, error) {
	return EmbedTextWith(ctx, http.DefaultClient, provider, content)
}

// EmbedTextWith is the explicit-client variant.
func EmbedTextWith(ctx context.Context, client *http.Client, provider *models.LlmProvider, content string) ([]float32, error) {
	if strings.TrimSpace(content) == "" {
		return []float32{}, nil
	}
	if provider == nil {
		// keep the placeholder offline-fallback semantics for callers
		// who don't have a provider — handlers that want a hard
		// error should validate before calling.
		return offlineEmbedding(content), nil
	}
	if client == nil {
		client = http.DefaultClient
	}
	switch provider.APIMode {
	case "chat_completions":
		return embedOpenAICompatible(ctx, client, provider, content)
	case "chat":
		return embedOllama(ctx, client, provider, content)
	default:
		return nil, fmt.Errorf("provider api_mode '%s' does not support embeddings in ai-service", provider.APIMode)
	}
}

func providerToken(provider *models.LlmProvider) string {
	if provider.CredentialReference == nil {
		return ""
	}
	ref := strings.TrimSpace(*provider.CredentialReference)
	if ref == "" {
		return ""
	}
	v := strings.TrimSpace(os.Getenv(ref))
	return v
}

// endpoint joins base + suffix the same way Rust does: if base
// already ends with the suffix, base wins; otherwise we trim the
// trailing slash from base and the leading slash from suffix and
// glue them with a single "/".
func endpointURL(base, suffix string) string {
	if strings.HasSuffix(base, suffix) {
		return base
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(suffix, "/")
}

func completeOpenAICompatible(
	ctx context.Context,
	client *http.Client,
	provider *models.LlmProvider,
	systemPrompt, userPrompt string,
	attachments []models.ChatAttachment,
	temperature float32,
	maxTokens int32,
) (CompletionResult, error) {
	messages := []map[string]any{}
	if strings.TrimSpace(systemPrompt) != "" {
		messages = append(messages, map[string]any{"role": "system", "content": systemPrompt})
	}
	userContent, err := buildOpenAIUserContent(userPrompt, attachments)
	if err != nil {
		return CompletionResult{}, err
	}
	messages = append(messages, map[string]any{"role": "user", "content": userContent})

	body := map[string]any{
		"model":       provider.ModelName,
		"messages":    messages,
		"temperature": temperature,
		"max_tokens":  maxTokens,
	}
	bodyJSON, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		endpointURL(provider.EndpointURL, "/chat/completions"),
		bytes.NewReader(bodyJSON))
	if err != nil {
		return CompletionResult{}, fmt.Errorf("provider request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token := providerToken(provider); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return CompletionResult{}, fmt.Errorf("provider request failed: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return CompletionResult{}, fmt.Errorf("provider response parse failed: %w", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return CompletionResult{}, fmt.Errorf("provider response parse failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CompletionResult{}, fmt.Errorf("provider returned %d: %s", resp.StatusCode, string(payload))
	}

	text := valueAsText(jsonPointer(parsed, "/choices/0/message/content"))
	if text == "" {
		text = valueAsText(jsonPointer(parsed, "/choices/0/text"))
	}
	promptTokens := usageTokens(parsed, "prompt_tokens")
	completionTokens := usageTokens(parsed, "completion_tokens")
	total := usageTokens(parsed, "total_tokens")
	if total < promptTokens+completionTokens {
		total = promptTokens + completionTokens
	}
	return CompletionResult{
		Text:             text,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      total,
		ToolCalls:        parseOpenAIToolCalls(parsed),
	}, nil
}

func completeAnthropic(
	ctx context.Context,
	client *http.Client,
	provider *models.LlmProvider,
	systemPrompt, userPrompt string,
	attachments []models.ChatAttachment,
	maxTokens int32,
) (CompletionResult, error) {
	body := map[string]any{
		"model":      provider.ModelName,
		"system":     systemPrompt,
		"max_tokens": maxTokens,
		"messages": []map[string]any{
			{"role": "user", "content": buildAnthropicUserContent(userPrompt, attachments)},
		},
	}
	bodyJSON, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		endpointURL(provider.EndpointURL, "/messages"),
		bytes.NewReader(bodyJSON))
	if err != nil {
		return CompletionResult{}, fmt.Errorf("provider request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if token := providerToken(provider); token != "" {
		req.Header.Set("x-api-key", token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return CompletionResult{}, fmt.Errorf("provider request failed: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return CompletionResult{}, fmt.Errorf("provider response parse failed: %w", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return CompletionResult{}, fmt.Errorf("provider response parse failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CompletionResult{}, fmt.Errorf("provider returned %d: %s", resp.StatusCode, string(payload))
	}

	text := stringFromPointer(parsed, "/content/0/text")
	promptTokens := usageTokens(parsed, "input_tokens")
	completionTokens := usageTokens(parsed, "output_tokens")
	total := usageTokens(parsed, "total_tokens")
	if total < promptTokens+completionTokens {
		total = promptTokens + completionTokens
	}
	return CompletionResult{
		Text:             text,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      total,
	}, nil
}

func completeOllama(
	ctx context.Context,
	client *http.Client,
	provider *models.LlmProvider,
	systemPrompt, userPrompt string,
	attachments []models.ChatAttachment,
) (CompletionResult, error) {
	messages := []map[string]any{}
	if strings.TrimSpace(systemPrompt) != "" {
		messages = append(messages, map[string]any{"role": "system", "content": systemPrompt})
	}
	prompt, images := buildOllamaUserPayload(userPrompt, attachments)
	user := map[string]any{"role": "user", "content": prompt}
	if len(images) > 0 {
		user["images"] = images
	}
	messages = append(messages, user)

	body := map[string]any{
		"model":    provider.ModelName,
		"messages": messages,
		"stream":   false,
	}
	bodyJSON, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		endpointURL(provider.EndpointURL, "/chat"),
		bytes.NewReader(bodyJSON))
	if err != nil {
		return CompletionResult{}, fmt.Errorf("provider request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return CompletionResult{}, fmt.Errorf("provider request failed: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return CompletionResult{}, fmt.Errorf("provider response parse failed: %w", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return CompletionResult{}, fmt.Errorf("provider response parse failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CompletionResult{}, fmt.Errorf("provider returned %d: %s", resp.StatusCode, string(payload))
	}

	text := stringFromPointer(parsed, "/message/content")
	promptTokens := int32(0)
	if v, ok := parsed["prompt_eval_count"].(float64); ok {
		promptTokens = int32(v)
	}
	completionTokens := int32(0)
	if v, ok := parsed["eval_count"].(float64); ok {
		completionTokens = int32(v)
	}
	return CompletionResult{
		Text:             text,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}, nil
}

func embedOpenAICompatible(ctx context.Context, client *http.Client, provider *models.LlmProvider, content string) ([]float32, error) {
	body := map[string]any{
		"model": provider.ModelName,
		"input": content,
	}
	bodyJSON, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		endpointURL(provider.EndpointURL, "/embeddings"),
		bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token := providerToken(provider); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embedding response parse failed: %w", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return nil, fmt.Errorf("embedding response parse failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding provider returned %d: %s", resp.StatusCode, string(payload))
	}
	return parseEmbedding(parsed)
}

func embedOllama(ctx context.Context, client *http.Client, provider *models.LlmProvider, content string) ([]float32, error) {
	body := map[string]any{
		"model":  provider.ModelName,
		"prompt": content,
	}
	bodyJSON, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		endpointURL(provider.EndpointURL, "/embeddings"),
		bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embedding response parse failed: %w", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return nil, fmt.Errorf("embedding response parse failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding provider returned %d: %s", resp.StatusCode, string(payload))
	}
	arr, ok := parsed["embedding"].([]any)
	if !ok {
		return nil, fmt.Errorf("embedding payload did not include an embedding vector")
	}
	out := valueArrayToFloat32(arr)
	if len(out) == 0 {
		return nil, fmt.Errorf("embedding payload did not include an embedding vector")
	}
	return out, nil
}

func buildOpenAIUserContent(userPrompt string, attachments []models.ChatAttachment) (any, error) {
	if len(attachments) == 0 {
		return userPrompt, nil
	}
	parts := []map[string]any{
		{"type": "text", "text": userPrompt},
	}
	for _, a := range attachments {
		switch a.Kind {
		case "text":
			if a.Text != nil && strings.TrimSpace(*a.Text) != "" {
				parts = append(parts, map[string]any{"type": "text", "text": *a.Text})
			}
		case "image_url":
			if a.URL == nil || strings.TrimSpace(*a.URL) == "" {
				return nil, fmt.Errorf("image_url attachment requires url")
			}
			parts = append(parts, map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": *a.URL},
			})
		case "image_base64":
			mimeType := "image/png"
			if a.MimeType != nil && strings.TrimSpace(*a.MimeType) != "" {
				mimeType = *a.MimeType
			}
			if a.Base64Data == nil || strings.TrimSpace(*a.Base64Data) == "" {
				return nil, fmt.Errorf("image_base64 attachment requires base64_data")
			}
			parts = append(parts, map[string]any{
				"type": "image_url",
				"image_url": map[string]any{
					"url": fmt.Sprintf("data:%s;base64,%s", mimeType, *a.Base64Data),
				},
			})
		default:
			return nil, fmt.Errorf("unsupported attachment kind '%s' for openai-compatible chat", a.Kind)
		}
	}
	return parts, nil
}

func buildAnthropicUserContent(userPrompt string, attachments []models.ChatAttachment) []map[string]any {
	parts := []map[string]any{{"type": "text", "text": userPrompt}}
	for _, a := range attachments {
		switch a.Kind {
		case "text":
			if a.Text != nil && strings.TrimSpace(*a.Text) != "" {
				parts = append(parts, map[string]any{"type": "text", "text": *a.Text})
			}
		case "image_base64":
			if a.Base64Data != nil && strings.TrimSpace(*a.Base64Data) != "" {
				mimeType := "image/png"
				if a.MimeType != nil && *a.MimeType != "" {
					mimeType = *a.MimeType
				}
				parts = append(parts, map[string]any{
					"type": "image",
					"source": map[string]any{
						"type":       "base64",
						"media_type": mimeType,
						"data":       *a.Base64Data,
					},
				})
			}
		case "image_url":
			if a.URL != nil && strings.TrimSpace(*a.URL) != "" {
				parts = append(parts, map[string]any{
					"type": "text",
					"text": "Referenced image URL: " + *a.URL,
				})
			}
		}
	}
	return parts
}

func buildOllamaUserPayload(userPrompt string, attachments []models.ChatAttachment) (string, []string) {
	prompt := userPrompt
	images := []string{}
	for _, a := range attachments {
		switch a.Kind {
		case "text":
			if a.Text != nil && strings.TrimSpace(*a.Text) != "" {
				prompt += "\n\nAttachment context:\n" + *a.Text
			}
		case "image_base64":
			if a.Base64Data != nil && strings.TrimSpace(*a.Base64Data) != "" {
				images = append(images, *a.Base64Data)
			}
		case "image_url":
			if a.URL != nil && strings.TrimSpace(*a.URL) != "" {
				prompt += "\n\nReferenced image URL: " + *a.URL
			}
		}
	}
	return prompt, images
}

func parseOpenAIToolCalls(payload map[string]any) []ToolCall {
	value := jsonPointer(payload, "/choices/0/message/tool_calls")
	arr, ok := value.([]any)
	if !ok || len(arr) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(arr))
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		call := ToolCall{
			ID:   valueAsText(obj["id"]),
			Type: valueAsText(obj["type"]),
		}
		if fn, ok := obj["function"].(map[string]any); ok {
			call.Name = valueAsText(fn["name"])
			call.Arguments = valueAsText(fn["arguments"])
		}
		out = append(out, call)
	}
	return out
}

func parseEmbedding(payload map[string]any) ([]float32, error) {
	value := jsonPointer(payload, "/data/0/embedding")
	arr, ok := value.([]any)
	if !ok || len(arr) == 0 {
		return nil, fmt.Errorf("embedding payload did not include an embedding vector")
	}
	out := valueArrayToFloat32(arr)
	if len(out) == 0 {
		return nil, fmt.Errorf("embedding payload did not include an embedding vector")
	}
	return out, nil
}

func valueArrayToFloat32(arr []any) []float32 {
	out := make([]float32, 0, len(arr))
	for _, item := range arr {
		switch x := item.(type) {
		case float64:
			out = append(out, float32(x))
		case float32:
			out = append(out, x)
		case int:
			out = append(out, float32(x))
		case int64:
			out = append(out, float32(x))
		case json.Number:
			if f, err := x.Float64(); err == nil {
				out = append(out, float32(f))
			}
		}
	}
	return out
}

// valueAsText mirrors fn value_as_text — accepts a string or an
// array of {text|content} parts joined by \n. Returns "" when the
// shape doesn't match.
func valueAsText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		collected := make([]string, 0, len(x))
		for _, part := range x {
			obj, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if s, ok := obj["text"].(string); ok && s != "" {
				collected = append(collected, s)
				continue
			}
			if s, ok := obj["content"].(string); ok && s != "" {
				collected = append(collected, s)
			}
		}
		return strings.Join(collected, "\n")
	}
	return ""
}

func usageTokens(payload map[string]any, key string) int32 {
	usage, ok := payload["usage"].(map[string]any)
	if !ok {
		return 0
	}
	switch v := usage[key].(type) {
	case float64:
		return int32(v)
	case int64:
		return int32(v)
	case int:
		return int32(v)
	}
	return 0
}

// jsonPointer is a tiny RFC-6901-ish lookup — supports only the
// "/segment" path style used by Rust's payload.pointer() calls.
// Numeric segments index into arrays.
func jsonPointer(root any, path string) any {
	if path == "" || path == "/" {
		return root
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	cur := root
	for _, seg := range parts {
		switch x := cur.(type) {
		case map[string]any:
			cur = x[seg]
		case []any:
			idx := 0
			for i, c := range seg {
				if i == 0 && (c == '+' || c == '-') {
					continue
				}
				if c < '0' || c > '9' {
					return nil
				}
			}
			for _, c := range seg {
				idx = idx*10 + int(c-'0')
			}
			if idx < 0 || idx >= len(x) {
				return nil
			}
			cur = x[idx]
		default:
			return nil
		}
		if cur == nil {
			return nil
		}
	}
	return cur
}

func stringFromPointer(payload map[string]any, path string) string {
	v := jsonPointer(payload, path)
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// offlineEmbedding stays as a deterministic 12-dim fallback for
// tests / dev when no provider is configured. Same algorithm as
// rag.EmbedText, kept here to break the rag → llm cosine cycle.
func offlineEmbedding(content string) []float32 {
	vector := make([]float32, 12)
	idx := 0
	for _, token := range strings.Fields(strings.ToLower(content)) {
		if token == "" {
			continue
		}
		var tokenValue uint32
		for _, b := range []byte(token) {
			tokenValue += uint32(b)
		}
		vector[idx%len(vector)] += float32(tokenValue%997) / 997.0
		idx++
	}
	var sumSq float32
	for _, v := range vector {
		sumSq += v * v
	}
	magnitude := float32(math.Sqrt(float64(sumSq)))
	if magnitude > 0 {
		for i := range vector {
			vector[i] /= magnitude
		}
	}
	return vector
}
