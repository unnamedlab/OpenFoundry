package predictions

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

func TestPredictsFromRealModelStateWhenAvailable(t *testing.T) {
	t.Parallel()
	split := models.TrafficSplitEntry{
		ModelVersionID: uuid.New(),
		Label:          "champion",
		Allocation:     100,
	}
	runtime := ModelRuntime{
		VersionNumber: 3,
		Schema: map[string]any{
			"model_state": map[string]any{
				"feature_names":  []any{"tickets_open", "usage_delta"},
				"feature_means":  []any{4.0, -0.2},
				"feature_scales": []any{2.0, 0.2},
				"weights":        []any{1.8, -1.4},
				"bias":           0.2,
				"threshold":      0.5,
				"positive_label": "positive",
				"negative_label": "negative",
			},
		},
	}
	out := PredictRecord(
		map[string]any{"tickets_open": 9.0, "usage_delta": -0.9},
		split, runtime, true, 0,
	)
	assert.Equal(t, "positive", out.PredictedLabel)
	assert.GreaterOrEqual(t, out.Score, 0.5)
	assert.NotEmpty(t, out.Contributions)
	assert.Equal(t, "record-1", out.RecordID)
	assert.Equal(t, "champion", out.Variant)
}

func TestPredictRecordFallsBackWithoutModelState(t *testing.T) {
	t.Parallel()
	split := models.TrafficSplitEntry{
		ModelVersionID: uuid.New(),
		Label:          "control",
		Allocation:     50,
	}
	runtime := ModelRuntime{
		VersionNumber: 1,
		Schema:        map[string]any{},
	}
	out := PredictRecord(
		map[string]any{"feature_a": 0.5, "feature_b": "tag"},
		split, runtime, true, 4,
	)
	assert.Equal(t, "record-5", out.RecordID)
	assert.Equal(t, "control", out.Variant)
	assert.Contains(t, []string{"positive", "negative"}, out.PredictedLabel)
	assert.Greater(t, out.Score, 0.0)
	assert.LessOrEqual(t, out.Score, 0.98)
}

func TestPredictRecordFallbackBiasContributionWhenExplainAndNoFeatures(t *testing.T) {
	t.Parallel()
	split := models.TrafficSplitEntry{ModelVersionID: uuid.New(), Label: "a", Allocation: 100}
	out := PredictRecord(nil, split, ModelRuntime{VersionNumber: 1}, true, 0)
	require.NotEmpty(t, out.Contributions)
	assert.Equal(t, "bias", out.Contributions[0].Name)
	assert.Equal(t, 0.42, out.Contributions[0].Value)
}

func TestRouteVariantDeterministicByOrdinal(t *testing.T) {
	t.Parallel()
	splits := []models.TrafficSplitEntry{
		{Label: "a", Allocation: 50},
		{Label: "b", Allocation: 50},
	}
	got, ok := RouteVariant(splits, 0)
	require.True(t, ok)
	assert.Equal(t, "a", got.Label, "ordinal 0 falls in cumulative<50")
	got, ok = RouteVariant(splits, 2)
	require.True(t, ok)
	assert.Equal(t, "b", got.Label, "ordinal 2 → bucket=74 → falls in second cumulative")
}

func TestRouteVariantEmptyReturnsFalse(t *testing.T) {
	t.Parallel()
	_, ok := RouteVariant(nil, 0)
	assert.False(t, ok)
}

func TestScalarScoreCoversAllValueShapes(t *testing.T) {
	t.Parallel()
	v, ok := scalarScore(7.0)
	assert.True(t, ok)
	assert.Equal(t, 7.0, v)

	v, ok = scalarScore("hello")
	assert.True(t, ok)
	assert.Equal(t, 0.05, v)

	v, ok = scalarScore(true)
	assert.True(t, ok)
	assert.Equal(t, 0.65, v)

	v, ok = scalarScore(false)
	assert.True(t, ok)
	assert.Equal(t, 0.35, v)

	_, ok = scalarScore([]string{"x"})
	assert.False(t, ok)
}

func TestRoundScoreCutsToTwoDecimals(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 0.12, roundScore(0.1234))
	assert.Equal(t, 0.99, roundScore(0.989))
}
