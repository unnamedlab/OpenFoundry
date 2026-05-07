package evaluation

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

func sampleProvider() *models.LlmProvider {
	return &models.LlmProvider{
		ID:                uuid.Nil,
		Name:              "Local",
		ProviderType:      "ollama",
		ModelName:         "llama3",
		EndpointURL:       "http://localhost:11434/api",
		APIMode:           "chat",
		Enabled:           true,
		LoadBalanceWeight: 10,
		MaxOutputTokens:   2048,
		CostTier:          "local",
		Tags:              []string{},
		RouteRules: models.ProviderRoutingRules{
			InputCostPer1KTokensUSD:  0.002,
			OutputCostPer1KTokensUSD: 0.004,
			Weight:                   100,
			MaxContextTokens:         32_000,
			NetworkScope:             "public",
			SupportedModalities:      []string{"text"},
		},
		HealthState: models.ProviderHealthState{Status: "healthy", LastCheckedAt: time.Now()},
	}
}

func TestEstimatesCostFromProviderRates(t *testing.T) {
	t.Parallel()
	got := EstimatedCostUSD(sampleProvider(), 500, 250, false)
	assert.InDelta(t, 0.002, got, 0.0001)
}

func TestEstimatedCostUSDIsZeroOnCacheHit(t *testing.T) {
	t.Parallel()
	assert.Equal(t, float32(0), EstimatedCostUSD(sampleProvider(), 1000, 1000, true))
}

func TestNormalizesInverseScores(t *testing.T) {
	t.Parallel()
	got := NormalizedScore(900.0, 300.0, 900.0, true)
	assert.Less(t, got, float32(0.05))
}

func TestNormalizedScoreFlatRangeReturnsOne(t *testing.T) {
	t.Parallel()
	assert.Equal(t, float32(1.0), NormalizedScore(5, 5, 5, false))
}

func TestScoresQualityFromRubricKeywords(t *testing.T) {
	t.Parallel()
	got := QualityScore(
		"This answer covers latency, cost, and fallback policy.",
		[]string{"latency", "fallback", "private"},
	)
	assert.InDelta(t, 0.666, got, 0.02)
}

func TestQualityScoreNoRubricUsesWordCount(t *testing.T) {
	t.Parallel()
	got := QualityScore("hello", nil)
	assert.Equal(t, float32(0.35), got, "single word clamps to 0.35")
}

func TestComputesWeightedBenchmarkScore(t *testing.T) {
	t.Parallel()
	got := OverallBenchmarkScore(0.9, 1.0, 0.8, 0.7)
	assert.Greater(t, got, float32(0.85))
}

func TestRiskScoreBlockedIsOne(t *testing.T) {
	t.Parallel()
	v := models.GuardrailVerdict{Blocked: true}
	assert.Equal(t, float32(1.0), RiskScore(&v))
}

func TestRiskScoreNoFlagsIsZero(t *testing.T) {
	t.Parallel()
	v := models.GuardrailVerdict{}
	assert.Equal(t, float32(0), RiskScore(&v))
}

func TestRiskScoreCapsAt0_95(t *testing.T) {
	t.Parallel()
	v := models.GuardrailVerdict{Flags: make([]models.GuardrailFlag, 100)}
	assert.Equal(t, float32(0.95), RiskScore(&v))
}

func TestSafetyScoreInverse(t *testing.T) {
	t.Parallel()
	v := models.GuardrailVerdict{}
	assert.Equal(t, float32(1.0), SafetyScore(&v))
}

func TestCacheHitRateZeroEntries(t *testing.T) {
	t.Parallel()
	assert.Equal(t, float32(0), CacheHitRate(0, 100))
	assert.Equal(t, float32(0), CacheHitRate(-1, 100))
}

func TestCacheHitRateCapsAt100(t *testing.T) {
	t.Parallel()
	assert.Equal(t, float32(100.0), CacheHitRate(1, 1_000_000))
}

func TestNormalizedScoreClampsOutOfRange(t *testing.T) {
	t.Parallel()
	assert.Equal(t, float32(1.0), NormalizedScore(1000, 0, 100, false))
	assert.Equal(t, float32(0), NormalizedScore(-50, 0, 100, false))
	// sanity-check that clamp+inverse do not produce NaN
	got := NormalizedScore(0, 0, 1, true)
	assert.False(t, math.IsNaN(float64(got)))
}
