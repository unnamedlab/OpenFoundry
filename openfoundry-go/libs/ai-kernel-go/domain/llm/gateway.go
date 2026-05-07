package llm

import (
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

// RouteProviders filters + ranks providers for a given request.
// Mirrors Rust src/domain/llm/gateway.rs::route_providers verbatim:
//
//   - Filter: enabled=true; route_rules.use_cases empty OR contains
//     use_case OR contains "general"; supports every required
//     modality (case-insensitive); when require_private_network is
//     set, network_scope ∈ {private, hybrid, local}.
//   - Sort: provider_rank desc (with health, weight, private bonus,
//     multimodal bonus, error-rate penalty), tiebreaker on
//     load_balance_weight desc.
//   - Preferred provider override: when the preferred id is in the
//     candidate list, move it to position 0 and insert its
//     declared fallback chain at positions 1..N.
func RouteProviders(
	providers []models.LlmProvider,
	preferredProviderID *uuid.UUID,
	useCase string,
	requiredModalities []string,
	requirePrivateNetwork bool,
	preferPrivateNetwork bool,
) []models.LlmProvider {
	candidates := []models.LlmProvider{}
	for _, p := range providers {
		if !p.Enabled {
			continue
		}
		if len(p.RouteRules.UseCases) > 0 {
			ok := false
			for _, uc := range p.RouteRules.UseCases {
				if uc == useCase || uc == "general" {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}
		if !supportsRequiredModalities(p, requiredModalities) {
			continue
		}
		if requirePrivateNetwork && !ProviderUsesPrivateNetwork(p) {
			continue
		}
		candidates = append(candidates, p)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		ri := providerRank(candidates[i], preferPrivateNetwork)
		rj := providerRank(candidates[j], preferPrivateNetwork)
		if ri != rj {
			return ri > rj
		}
		return candidates[i].LoadBalanceWeight > candidates[j].LoadBalanceWeight
	})

	if preferredProviderID == nil {
		return candidates
	}
	for idx, p := range candidates {
		if p.ID != *preferredProviderID {
			continue
		}
		// Move to front.
		preferred := candidates[idx]
		candidates = append(candidates[:idx], candidates[idx+1:]...)
		candidates = append([]models.LlmProvider{preferred}, candidates...)

		// Move declared fallbacks to positions 1..N preserving order.
		var ordered []models.LlmProvider
		for _, fb := range preferred.RouteRules.FallbackProviderIDs {
			for i, c := range candidates {
				if c.ID == fb {
					ordered = append(ordered, c)
					candidates = append(candidates[:i], candidates[i+1:]...)
					break
				}
			}
		}
		// Insert in reverse so the first declared fallback ends up at index 1.
		for i := len(ordered) - 1; i >= 0; i-- {
			candidates = append(candidates[:1], append([]models.LlmProvider{ordered[i]}, candidates[1:]...)...)
		}
		break
	}
	return candidates
}

// SelectProvider picks the head of the candidate list. When
// fallback_enabled is true, prefers a non-offline provider.
func SelectProvider(candidates []models.LlmProvider, fallbackEnabled bool) *models.LlmProvider {
	if len(candidates) == 0 {
		return nil
	}
	if fallbackEnabled {
		for _, p := range candidates {
			if p.HealthState.Status != "offline" {
				cp := p
				return &cp
			}
		}
	}
	cp := candidates[0]
	return &cp
}

// EstimateTokens is the canonical token-count heuristic: word-count * 1.35,
// ceil. Used by the gateway to size context budgets.
func EstimateTokens(content string) int32 {
	wc := float32(len(strings.Fields(content)))
	approx := wc * 1.35
	if approx > float32(int32(approx)) {
		return int32(approx) + 1
	}
	return int32(approx)
}

// ProviderUsesPrivateNetwork returns true when the provider's
// network_scope is private/hybrid/local (case-insensitive).
func ProviderUsesPrivateNetwork(p models.LlmProvider) bool {
	scope := strings.ToLower(p.RouteRules.NetworkScope)
	return scope == "private" || scope == "hybrid" || scope == "local"
}

func supportsRequiredModalities(p models.LlmProvider, required []string) bool {
	for _, r := range required {
		ok := false
		for _, supported := range p.RouteRules.SupportedModalities {
			if strings.EqualFold(r, supported) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func providerRank(p models.LlmProvider, preferPrivateNetwork bool) float32 {
	healthBonus := float32(0.0)
	switch p.HealthState.Status {
	case "healthy":
		healthBonus = 100.0
	case "degraded":
		healthBonus = 50.0
	}
	privateBonus := float32(0.0)
	if preferPrivateNetwork && ProviderUsesPrivateNetwork(p) {
		privateBonus = 35.0
	}
	multimodalBonus := float32(0.0)
	for _, m := range p.RouteRules.SupportedModalities {
		if strings.EqualFold(m, "image") {
			multimodalBonus = 5.0
			break
		}
	}
	return healthBonus + float32(p.LoadBalanceWeight) + privateBonus + multimodalBonus -
		(p.HealthState.ErrorRate * 100.0)
}
