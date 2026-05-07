// Package domain hosts the pure-logic ML domain helpers (drift,
// predictions, training, serving, feature-store). Driver wiring
// lives in the consuming services + libs/ml-kernel-go/handlers.
package domain

import (
	"math"
	"time"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

// metricStatus classifies a drift score against its threshold.
// Mirrors Rust src/domain/drift.rs::metric_status.
func metricStatus(score, threshold float64) string {
	switch {
	case score >= threshold:
		return "alert"
	case score >= threshold*0.7:
		return "warning"
	default:
		return "healthy"
	}
}

// roundScore rounds to 2 decimal places. Mirrors Rust round_score.
func roundScore(v float64) float64 {
	return math.Round(v*100.0) / 100.0
}

// GenerateDriftReport produces a deterministic drift report given
// baseline + observed row counts and a variant count. Mirrors Rust
// src/domain/drift.rs::generate_report verbatim, including the
// 0.12/0.09 base scores, 1.5 volume-shift cap, the dataset-score
// >= 0.25 OR concept-score >= 0.18 retraining trigger, and the
// canonical recommendation strings.
func GenerateDriftReport(req models.GenerateDriftReportRequest, variantCount int) models.DriftReport {
	baselineRows := int64(10_000)
	if req.BaselineRows != nil {
		baselineRows = *req.BaselineRows
	}
	if baselineRows < 1 {
		baselineRows = 1
	}
	baseline := float64(baselineRows)

	var observed float64
	if req.ObservedRows != nil {
		o := float64(*req.ObservedRows)
		if o < 1 {
			o = 1
		}
		observed = o
	} else {
		o := baseline * 1.12
		if o < 1 {
			o = 1
		}
		observed = o
	}

	volumeShift := math.Abs(observed-baseline) / baseline
	if volumeShift > 1.5 {
		volumeShift = 1.5
	}

	datasetScore := roundScore(math.Min(0.12+volumeShift+float64(variantCount)*0.04, 1.5))
	conceptScore := roundScore(math.Min(0.09+volumeShift*0.7+float64(variantCount)*0.03, 1.5))
	recommend := datasetScore >= 0.25 || conceptScore >= 0.18

	notes := "Observed drift remains within the configured guardrails."
	if recommend {
		notes = "Observed drift exceeded the configured threshold; retraining is recommended."
	}

	return models.DriftReport{
		GeneratedAt: time.Now().UTC(),
		DatasetMetrics: []models.DriftMetric{
			{Name: "psi", Score: datasetScore, Threshold: 0.25, Status: metricStatus(datasetScore, 0.25)},
		},
		ConceptMetrics: []models.DriftMetric{
			{Name: "prediction_target_gap", Score: conceptScore, Threshold: 0.18, Status: metricStatus(conceptScore, 0.18)},
		},
		RecommendRetraining: recommend,
		Notes:               notes,
	}
}
