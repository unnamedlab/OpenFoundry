package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

func TestMetricStatusBoundaries(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "alert", metricStatus(0.30, 0.25))
	assert.Equal(t, "warning", metricStatus(0.20, 0.25))
	assert.Equal(t, "healthy", metricStatus(0.10, 0.25))
}

func TestRoundScoreTwoDecimals(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 0.12, roundScore(0.1234))
	assert.Equal(t, 0.13, roundScore(0.1267))
	assert.Equal(t, 1.50, roundScore(1.4999))
}

func TestGenerateDriftReportDefaults(t *testing.T) {
	t.Parallel()
	req := models.GenerateDriftReportRequest{}
	r := GenerateDriftReport(req, 0)
	require.Len(t, r.DatasetMetrics, 1)
	require.Len(t, r.ConceptMetrics, 1)
	assert.Equal(t, "psi", r.DatasetMetrics[0].Name)
	assert.Equal(t, "prediction_target_gap", r.ConceptMetrics[0].Name)
	// Defaults: baseline=10000, observed=11200, volume_shift=0.12.
	// dataset_score = round(0.12 + 0.12 + 0) = 0.24 → status warning,
	// concept_score = round(0.09 + 0.084 + 0) = 0.17 → status warning.
	assert.InDelta(t, 0.24, r.DatasetMetrics[0].Score, 1e-6)
	assert.InDelta(t, 0.17, r.ConceptMetrics[0].Score, 1e-6)
	// dataset_score 0.24 < 0.25 AND concept_score 0.17 < 0.18 → no retrain.
	assert.False(t, r.RecommendRetraining)
	assert.Contains(t, r.Notes, "within the configured guardrails")
}

func TestGenerateDriftReportTriggersRetraining(t *testing.T) {
	t.Parallel()
	// Heavy volume shift: observed = 2× baseline → volume_shift = 1.0
	// → dataset_score = 0.12 + 1.0 + 0 = 1.12 → far above 0.25.
	baseline := int64(10_000)
	observed := int64(20_000)
	req := models.GenerateDriftReportRequest{
		BaselineRows: &baseline,
		ObservedRows: &observed,
	}
	r := GenerateDriftReport(req, 0)
	assert.True(t, r.RecommendRetraining)
	assert.Equal(t, "alert", r.DatasetMetrics[0].Status)
	assert.Contains(t, r.Notes, "retraining is recommended")
}

func TestGenerateDriftReportClampsScore(t *testing.T) {
	t.Parallel()
	// Volume_shift caps at 1.5; dataset_score caps at 1.5. With high
	// variant_count (10) we exceed 1.5 unclamped → must clamp.
	baseline := int64(10_000)
	observed := int64(50_000)
	req := models.GenerateDriftReportRequest{
		BaselineRows: &baseline,
		ObservedRows: &observed,
	}
	r := GenerateDriftReport(req, 10)
	assert.LessOrEqual(t, r.DatasetMetrics[0].Score, 1.5)
	assert.LessOrEqual(t, r.ConceptMetrics[0].Score, 1.5)
}
