// Package handlers exposes the HTTP surface of ai-evaluation-service:
// /api/v1/evaluations/benchmark + /api/v1/guardrails/evaluate. Mirrors
// services/ai-evaluation-service/src/handlers/evaluations.rs verbatim.
//
// The benchmark path validates the prompt (guardrail-blocked prompts
// 400 before any provider runs), filters providers by the optional
// provider_ids list, routes the candidates via the gateway with
// modality + privacy filters, calls llm.CompleteText for each routed
// provider, records ai_llm_usage_events per success, scores
// quality/safety/latency/cost/overall, sorts overall desc, and picks
// the head as recommended_provider_id.
//
// The guardrails path is pure logic: runs llm.EvaluateText, derives
// the recommendations list (sorted, mirroring the Rust BTreeSet), and
// returns the verdict + risk score.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/domain/evaluation"
	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/domain/llm"
	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

// Handlers wires the evaluation HTTP surface. Pool is the pgx pool
// against the ai-evaluation-service database (which owns the
// ai_providers + ai_llm_usage_events tables). Runtime is the LLM
// dispatch — production wiring uses HTTPRuntime, tests inject
// FakeRuntime.
type Handlers struct {
	Pool    *pgxpool.Pool
	Runtime llm.Runtime
}

// completionRuntime returns the injected runtime, falling back to the
// real HTTP runtime when none is configured.
func (h *Handlers) completionRuntime() llm.Runtime {
	if h.Runtime != nil {
		return h.Runtime
	}
	return llm.HTTPRuntime{}
}

// errorResponse mirrors Rust handlers::mod::ErrorResponse — the
// canonical {"error": "..."} envelope.
type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func dbError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "database operation failed")
}

// providerColumns matches libs/ai-kernel-go/handlers — same column
// order so scanProvider stays in sync with the kernel scanner.
const providerColumns = `id, name, provider_type, model_name, endpoint_url,
                         api_mode, credential_reference, enabled,
                         load_balance_weight, max_output_tokens,
                         cost_tier, tags, route_rules, health_state,
                         created_at, updated_at`

// rowScanner is the minimal pgx scanner interface accepted by
// scanProvider — both pgx.Row and pgx.Rows satisfy it.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanProvider mirrors libs/ai-kernel-go/handlers.scanProvider — same
// JSON-decoded route_rules + health_state shape, same defaults.
func scanProvider(s rowScanner) (models.LlmProvider, error) {
	var p models.LlmProvider
	var credRef *string
	var tagsRaw, routeRulesRaw, healthRaw []byte
	if err := s.Scan(
		&p.ID, &p.Name, &p.ProviderType, &p.ModelName, &p.EndpointURL,
		&p.APIMode, &credRef, &p.Enabled,
		&p.LoadBalanceWeight, &p.MaxOutputTokens,
		&p.CostTier, &tagsRaw, &routeRulesRaw, &healthRaw,
		&p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return p, err
	}
	p.CredentialReference = credRef
	p.CredentialConfigured = credRef != nil && strings.TrimSpace(*credRef) != ""
	if len(tagsRaw) > 0 {
		_ = json.Unmarshal(tagsRaw, &p.Tags)
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	if len(routeRulesRaw) > 0 {
		_ = json.Unmarshal(routeRulesRaw, &p.RouteRules)
	}
	if len(healthRaw) > 0 {
		_ = json.Unmarshal(healthRaw, &p.HealthState)
	}
	return p, nil
}

// loadProviderRows mirrors fn load_provider_rows — every configured
// provider ordered by updated_at desc, created_at desc.
func loadProviderRows(ctx context.Context, pool *pgxpool.Pool) ([]models.LlmProvider, error) {
	rows, err := pool.Query(ctx,
		`SELECT `+providerColumns+` FROM ai_providers
          ORDER BY updated_at DESC, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.LlmProvider, 0)
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// summarizeTitle mirrors fn summarize_title — first 60 runes of the
// trimmed content, "..." suffix if longer, "New benchmark" when empty.
func summarizeTitle(content string) string {
	trimmed := strings.TrimSpace(content)
	runes := []rune(trimmed)
	if len(runes) > 60 {
		return string(runes[:60]) + "..."
	}
	if len(runes) == 0 {
		return "New benchmark"
	}
	return string(runes)
}

// previewText mirrors fn preview_text — first `limit` runes of the
// trimmed content; appends "..." if truncated.
func previewText(content string, limit int) string {
	trimmed := strings.TrimSpace(content)
	runes := []rune(trimmed)
	if len(runes) > limit {
		return string(runes[:limit]) + "..."
	}
	return string(runes)
}

// attachmentContext mirrors fn attachment_context — formats
// attachments as "- <label>: …" lines for inclusion in the prompt
// echo. Returns "none" when the slice is empty.
func attachmentContext(attachments []models.ChatAttachment) string {
	if len(attachments) == 0 {
		return "none"
	}
	lines := make([]string, 0, len(attachments))
	for _, a := range attachments {
		label := "attachment"
		if a.Name != nil && strings.TrimSpace(*a.Name) != "" {
			label = *a.Name
		}
		switch a.Kind {
		case "image_url":
			url := "missing-url"
			if a.URL != nil {
				url = *a.URL
			}
			lines = append(lines, fmt.Sprintf("- %s: image url %s", label, url))
		case "image_base64":
			mime := "unknown"
			if a.MimeType != nil {
				mime = *a.MimeType
			}
			lines = append(lines, fmt.Sprintf("- %s: embedded %s image", label, mime))
		default:
			text := "text attachment"
			if a.Text != nil {
				text = *a.Text
			}
			lines = append(lines, fmt.Sprintf("- %s: %s", label, text))
		}
	}
	return strings.Join(lines, "\n")
}

// requiredModalities mirrors fn required_modalities — always includes
// "text"; appends "image" when any attachment kind starts with "image".
func requiredModalities(attachments []models.ChatAttachment) []string {
	out := []string{"text"}
	for _, a := range attachments {
		if strings.HasPrefix(a.Kind, "image") {
			out = append(out, "image")
			break
		}
	}
	return out
}

// modalityLabel mirrors fn modality_label.
func modalityLabel(required []string) string {
	for _, m := range required {
		if strings.EqualFold(m, "image") {
			return "image+text"
		}
	}
	return "text"
}

// privacyReason mirrors fn privacy_reason — "private network
// explicitly requested" when the body flag is set; the PII fallback
// when guardrails flagged a pii_* kind; nil otherwise.
func privacyReason(verdict models.GuardrailVerdict, requirePrivateNetwork bool) *string {
	if requirePrivateNetwork {
		s := "private network explicitly requested"
		return &s
	}
	for _, f := range verdict.Flags {
		if strings.HasPrefix(f.Kind, "pii_") {
			s := "PII detected in prompt, preferring private-network providers"
			return &s
		}
	}
	return nil
}

// usageSummary mirrors fn usage_summary — clamps negative tokens to
// zero before summing, computes EstimatedCostUSD via the kernel
// evaluation helper.
func usageSummary(provider models.LlmProvider, promptTokens, completionTokens, latencyMs int32, cacheHit bool) models.LlmUsageSummary {
	pt := promptTokens
	if pt < 0 {
		pt = 0
	}
	ct := completionTokens
	if ct < 0 {
		ct = 0
	}
	return models.LlmUsageSummary{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      pt + ct,
		EstimatedCostUSD: evaluation.EstimatedCostUSD(&provider, promptTokens, completionTokens, cacheHit),
		LatencyMs:        latencyMs,
		NetworkScope:     provider.RouteRules.NetworkScope,
		CacheHit:         cacheHit,
	}
}

// recordUsageEvent mirrors fn record_usage_event — best-effort insert
// into ai_llm_usage_events. The benchmark loop swallows the error so
// a metric-write hiccup doesn't kill the whole batch.
func recordUsageEvent(
	ctx context.Context,
	pool *pgxpool.Pool,
	providerID uuid.UUID,
	requestKind, useCase, modality string,
	usage models.LlmUsageSummary,
	benchmarkGroupID *uuid.UUID,
	metadata any,
) error {
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	metadataJSON, _ := json.Marshal(metadata)
	_, err = pool.Exec(ctx,
		`INSERT INTO ai_llm_usage_events (
            id, provider_id, conversation_id, request_kind, use_case,
            network_scope, modality, cache_hit, prompt_tokens,
            completion_tokens, total_tokens, estimated_cost_usd,
            latency_ms, benchmark_group_id, metadata
         ) VALUES ($1, $2, NULL, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		id, providerID, requestKind, useCase,
		usage.NetworkScope, modality, usage.CacheHit, usage.PromptTokens,
		usage.CompletionTokens, usage.TotalTokens, usage.EstimatedCostUSD,
		usage.LatencyMs, benchmarkGroupID, metadataJSON,
	)
	return err
}

// BenchmarkProviders handles `POST /api/v1/evaluations/benchmark`.
// Mirrors fn benchmark_providers verbatim:
//
//  1. validate prompt + guardrail (block sanitises 400 if blocked)
//  2. load providers, optionally filtered by body.provider_ids
//  3. route via gateway with privacy + modality filters
//  4. call llm.CompleteText for each routed provider — capture
//     latency, tokens, error
//  5. record ai_llm_usage_events per success (best-effort)
//  6. score quality/safety/latency/cost/overall, sort overall desc,
//     pick the head as recommended_provider_id.
func (h *Handlers) BenchmarkProviders(w http.ResponseWriter, r *http.Request) {
	if h.Pool == nil {
		writeError(w, http.StatusServiceUnavailable, "ai-evaluation-service is not configured with a database")
		return
	}

	var body models.ProviderBenchmarkRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		writeError(w, http.StatusBadRequest, "benchmark prompt is required")
		return
	}
	if strings.TrimSpace(body.UseCase) == "" {
		body.UseCase = models.DefaultBenchmarkUseCase
	}
	if body.MaxTokens <= 0 {
		body.MaxTokens = models.DefaultMaxTokens
	}

	promptVerdict := llm.EvaluateText(body.Prompt)
	if promptVerdict.Blocked {
		writeError(w, http.StatusBadRequest,
			"benchmark prompt is blocked by guardrails; sanitize it before benchmarking")
		return
	}

	ctx := r.Context()
	providers, err := loadProviderRows(ctx, h.Pool)
	if err != nil {
		dbError(w, err)
		return
	}
	if len(providers) == 0 {
		writeError(w, http.StatusNotFound, "no AI providers configured")
		return
	}

	candidates := providers
	if len(body.ProviderIDs) > 0 {
		filterSet := map[uuid.UUID]struct{}{}
		for _, id := range body.ProviderIDs {
			filterSet[id] = struct{}{}
		}
		filtered := make([]models.LlmProvider, 0, len(filterSet))
		for _, p := range providers {
			if _, ok := filterSet[p.ID]; ok {
				filtered = append(filtered, p)
			}
		}
		candidates = filtered
	}
	if len(candidates) == 0 {
		writeError(w, http.StatusNotFound, "no benchmark providers matched the requested ids")
		return
	}

	required := requiredModalities(body.Attachments)
	privacy := privacyReason(promptVerdict, body.RequirePrivateNetwork)
	routed := llm.RouteProviders(candidates, nil, body.UseCase, required,
		body.RequirePrivateNetwork, privacy != nil)
	if body.RequirePrivateNetwork && len(routed) == 0 {
		writeError(w, http.StatusBadRequest,
			"no private-network AI provider is configured for this benchmark")
		return
	}
	if len(routed) == 0 {
		writeError(w, http.StatusNotFound, "no eligible providers support this benchmark")
		return
	}

	benchmarkGroupID, err := uuid.NewV7()
	if err != nil {
		benchmarkGroupID = uuid.New()
	}

	systemPrompt := "You are an enterprise AI benchmark harness. Answer the user prompt clearly and concretely."
	if body.SystemPrompt != nil && strings.TrimSpace(*body.SystemPrompt) != "" {
		systemPrompt = *body.SystemPrompt
	}
	promptUsed := fmt.Sprintf("%s\n\nUser request: %s\n\nAttachments:\n%s",
		systemPrompt, promptVerdict.RedactedText, attachmentContext(body.Attachments))

	results := make([]models.ProviderBenchmarkResult, 0, len(routed))
	for _, provider := range routed {
		startedAt := time.Now()
		completion, completionErr := h.completionRuntime().CompleteText(ctx, llm.CompletionRequest{
			Provider:     &provider,
			SystemPrompt: systemPrompt,
			UserPrompt:   body.Prompt,
			Attachments:  body.Attachments,
			Temperature:  models.DefaultTemperature,
			MaxTokens:    body.MaxTokens,
		})
		latencyMs := int32(time.Since(startedAt).Milliseconds())
		if latencyMs < 0 {
			latencyMs = 0
		}

		if completionErr != nil {
			errStr := completionErr.Error()
			results = append(results, models.ProviderBenchmarkResult{
				ProviderID:       provider.ID,
				ProviderName:     provider.Name,
				NetworkScope:     provider.RouteRules.NetworkScope,
				ReplyPreview:     "",
				PromptTokens:     0,
				CompletionTokens: 0,
				TotalTokens:      0,
				EstimatedCostUSD: 0,
				LatencyMs:        latencyMs,
				CacheHit:         false,
				Guardrail:        models.DefaultGuardrailVerdict(),
				Score:            models.ProviderBenchmarkScore{},
				Error:            &errStr,
			})
			continue
		}

		promptTokens := completion.PromptTokens
		if est := llm.EstimateTokens(promptUsed); est > promptTokens {
			promptTokens = est
		}
		completionTokens := completion.CompletionTokens
		if est := llm.EstimateTokens(completion.Text); est > completionTokens {
			completionTokens = est
		}
		usage := usageSummary(provider, promptTokens, completionTokens, latencyMs, false)
		if completion.TotalTokens > usage.TotalTokens {
			usage.TotalTokens = completion.TotalTokens
		}

		replyVerdict := llm.EvaluateText(completion.Text)

		// Best-effort metric emission; the benchmark response stays
		// consistent even when the insert is rejected.
		_ = recordUsageEvent(ctx, h.Pool, provider.ID, "benchmark",
			body.UseCase, modalityLabel(required), usage, &benchmarkGroupID,
			map[string]any{
				"rubric_keywords": body.RubricKeywords,
				"provider_name":   provider.Name,
			})

		results = append(results, models.ProviderBenchmarkResult{
			ProviderID:       provider.ID,
			ProviderName:     provider.Name,
			NetworkScope:     usage.NetworkScope,
			ReplyPreview:     previewText(completion.Text, 280),
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.TotalTokens,
			EstimatedCostUSD: usage.EstimatedCostUSD,
			LatencyMs:        usage.LatencyMs,
			CacheHit:         false,
			Guardrail:        replyVerdict,
			Score:            models.ProviderBenchmarkScore{},
		})
	}

	successful := make([]int, 0, len(results))
	for i, r := range results {
		if r.Error == nil {
			successful = append(successful, i)
		}
	}

	minLatency, maxLatency := float32(0), float32(0)
	minCost, maxCost := float32(0), float32(0)
	if len(successful) > 0 {
		first := successful[0]
		minLatency = float32(results[first].LatencyMs)
		maxLatency = minLatency
		minCost = results[first].EstimatedCostUSD
		maxCost = minCost
		for _, idx := range successful[1:] {
			lat := float32(results[idx].LatencyMs)
			if lat < minLatency {
				minLatency = lat
			}
			if lat > maxLatency {
				maxLatency = lat
			}
			cost := results[idx].EstimatedCostUSD
			if cost < minCost {
				minCost = cost
			}
			if cost > maxCost {
				maxCost = cost
			}
		}
	}

	for _, idx := range successful {
		r := &results[idx]
		quality := evaluation.QualityScore(r.ReplyPreview, body.RubricKeywords)
		safety := evaluation.SafetyScore(&r.Guardrail)
		latency := evaluation.NormalizedScore(float32(r.LatencyMs), minLatency, maxLatency, true)
		cost := evaluation.NormalizedScore(r.EstimatedCostUSD, minCost, maxCost, true)
		r.Score = models.ProviderBenchmarkScore{
			Quality: quality,
			Latency: latency,
			Cost:    cost,
			Safety:  safety,
			Overall: evaluation.OverallBenchmarkScore(quality, safety, latency, cost),
		}
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score.Overall > results[j].Score.Overall
	})

	var recommended *uuid.UUID
	for _, r := range results {
		if r.Error == nil {
			id := r.ProviderID
			recommended = &id
			break
		}
	}

	writeJSON(w, http.StatusOK, models.ProviderBenchmarkResponse{
		BenchmarkGroupID:        benchmarkGroupID,
		UseCase:                 body.UseCase,
		PromptExcerpt:           summarizeTitle(body.Prompt),
		RequiredModalities:      required,
		RequestedPrivateNetwork: body.RequirePrivateNetwork,
		RecommendedProviderID:   recommended,
		Results:                 results,
		CreatedAt:               time.Now().UTC(),
	})
}

// EvaluateGuardrails handles `POST /api/v1/guardrails/evaluate`. Pure
// logic — runs llm.EvaluateText then derives the recommendations
// list mirroring the Rust BTreeSet (sorted set) ordering.
func (h *Handlers) EvaluateGuardrails(w http.ResponseWriter, r *http.Request) {
	var body models.EvaluateGuardrailsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		writeError(w, http.StatusBadRequest, "guardrail evaluation requires content")
		return
	}

	verdict := llm.EvaluateText(body.Content)

	recSet := map[string]struct{}{}
	if verdict.Blocked {
		recSet["Remove prompt-injection or toxic content before retrying."] = struct{}{}
	}
	for _, flag := range verdict.Flags {
		if strings.HasPrefix(flag.Kind, "pii_") {
			recSet["Redact PII before routing prompts to external LLM providers."] = struct{}{}
		}
	}
	if len(recSet) == 0 {
		recSet["No blocking issues detected; response is safe to continue."] = struct{}{}
	}
	recommendations := make([]string, 0, len(recSet))
	for k := range recSet {
		recommendations = append(recommendations, k)
	}
	sort.Strings(recommendations)

	writeJSON(w, http.StatusOK, models.EvaluateGuardrailsResponse{
		Verdict:         verdict,
		RiskScore:       evaluation.RiskScore(&verdict),
		Recommendations: recommendations,
	})
}
