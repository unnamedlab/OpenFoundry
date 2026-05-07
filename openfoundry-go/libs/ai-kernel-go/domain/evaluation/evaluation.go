// Package evaluation hosts the pure-logic LLM evaluation helpers
// (cache hit-rate, risk / safety score, cost estimate, benchmark
// scoring) used by the chat + benchmark + overview handlers.
//
// Mirrors libs/ai-kernel/src/domain/evaluation.rs verbatim. All
// floating-point math is done in float32 to match the Rust source so
// snapshot tests stay byte-identical.
package evaluation

import (
	"math"
	"strings"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

func CacheHitRate(entryCount, totalHits int64) float32 {
	if entryCount <= 0 {
		return 0
	}
	rate := float32(totalHits) / float32(entryCount)
	if rate > 100.0 {
		return 100.0
	}
	return rate
}

func RiskScore(verdict *models.GuardrailVerdict) float32 {
	if verdict == nil {
		return 0
	}
	if verdict.Blocked {
		return 1.0
	}
	if len(verdict.Flags) == 0 {
		return 0
	}
	v := float32(len(verdict.Flags)) / 5.0
	if v > 0.95 {
		return 0.95
	}
	return v
}

func SafetyScore(verdict *models.GuardrailVerdict) float32 {
	v := 1.0 - RiskScore(verdict)
	if v < 0 {
		return 0
	}
	if v > 1.0 {
		return 1.0
	}
	return v
}

func EstimatedCostUSD(provider *models.LlmProvider, promptTokens, completionTokens int32, cacheHit bool) float32 {
	if cacheHit || provider == nil {
		return 0
	}
	pt := promptTokens
	if pt < 0 {
		pt = 0
	}
	ct := completionTokens
	if ct < 0 {
		ct = 0
	}
	inRate := provider.RouteRules.InputCostPer1KTokensUSD
	if inRate < 0 {
		inRate = 0
	}
	outRate := provider.RouteRules.OutputCostPer1KTokensUSD
	if outRate < 0 {
		outRate = 0
	}
	cost := (float32(pt)/1000.0)*inRate + (float32(ct)/1000.0)*outRate
	if cost < 0 {
		return 0
	}
	return cost
}

func QualityScore(reply string, rubricKeywords []string) float32 {
	if len(rubricKeywords) == 0 {
		score := float32(len(strings.Fields(reply))) / 120.0
		if score < 0.35 {
			return 0.35
		}
		if score > 0.9 {
			return 0.9
		}
		return score
	}
	normalized := strings.ToLower(reply)
	hits := 0
	for _, kw := range rubricKeywords {
		if strings.Contains(normalized, strings.ToLower(kw)) {
			hits++
		}
	}
	v := float32(hits) / float32(len(rubricKeywords))
	if v < 0 {
		return 0
	}
	if v > 1.0 {
		return 1.0
	}
	return v
}

func NormalizedScore(value, min, max float32, lowerIsBetter bool) float32 {
	if math.Abs(float64(max-min)) < float64(math.SmallestNonzeroFloat32) {
		return 1.0
	}
	v := (value - min) / (max - min)
	if v < 0 {
		v = 0
	}
	if v > 1.0 {
		v = 1.0
	}
	if lowerIsBetter {
		return 1.0 - v
	}
	return v
}

func OverallBenchmarkScore(quality, safety, latency, cost float32) float32 {
	v := quality*0.45 + safety*0.25 + latency*0.15 + cost*0.15
	if v < 0 {
		return 0
	}
	if v > 1.0 {
		return 1.0
	}
	return v
}
