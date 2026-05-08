package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

// Proof point: this binary depends on libs/ai-kernel-go/models for
// every wire-format DTO — the test below exercises a round-trip on
// LlmProvider so a regression in the kernel models surfaces here
// immediately. The follow-up slice that wires `/api/v1/providers`
// against this same type does not have to re-pin the wire shape.
func TestLlmProviderRoundTripsViaAiKernelGo(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	p := models.LlmProvider{
		ID:                uuid.New(),
		Name:              "openai-prod",
		ProviderType:      models.DefaultProviderType,
		ModelName:         models.DefaultModelName,
		EndpointURL:       models.DefaultEndpointURL,
		APIMode:           models.DefaultAPIMode,
		Enabled:           true,
		LoadBalanceWeight: models.DefaultLoadBalanceWeight,
		MaxOutputTokens:   models.DefaultMaxOutputTokens,
		CostTier:          models.DefaultCostTier,
		Tags:              []string{"prod"},
		RouteRules:        models.DefaultProviderRoutingRules(),
		HealthState:       models.ProviderHealthState{Status: "healthy", AvgLatencyMs: 620, ErrorRate: 0.01, LastCheckedAt: now},
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	b, err := json.Marshal(p)
	require.NoError(t, err)

	var got models.LlmProvider
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, p.Name, got.Name)
	assert.Equal(t, p.ProviderType, got.ProviderType)
	assert.Equal(t, p.RouteRules.NetworkScope, got.RouteRules.NetworkScope)
	assert.Equal(t, p.RouteRules.MaxContextTokens, got.RouteRules.MaxContextTokens)
}
