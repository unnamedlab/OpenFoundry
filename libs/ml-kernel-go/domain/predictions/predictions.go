// Package predictions hosts the deterministic record-prediction
// pure-logic mirroring libs/ml-kernel/src/domain/predictions.rs.
//
// The runtime branch reads a model_state object out of
// ModelRuntime.Schema (feature_names, feature_means, feature_scales,
// weights, bias, threshold, positive_label, negative_label) and
// computes a sigmoid score. When the schema lacks model_state or any
// shape mismatch occurs we fall back to a deterministic sin-wave
// score so the surface always returns *something* — same semantics
// as the Rust source.
package predictions

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

// jsonNumberAlias is the json.Number type when the caller decoded
// input with json.UseNumber(). We accept it everywhere a numeric
// value is expected so the prediction logic round-trips losslessly
// regardless of whether numbers arrived as float64 or json.Number.
type jsonNumberAlias = json.Number

// ModelRuntime is the in-memory snapshot of a model version that
// PredictRecord interrogates. Schema is the JSON-decoded model
// definition exactly as ingested from the registry.
type ModelRuntime struct {
	VersionNumber int32
	Schema        map[string]any
}

func roundScore(value float64) float64 {
	return math.Round(value*100.0) / 100.0
}

// scalarScore extracts a numeric signal from a JSON value. Mirrors
// the Rust match arm semantics: numbers are passed through; strings
// become min(len, 100)/100; bools map to 0.65 / 0.35; everything
// else returns (0, false) so the caller can decide.
func scalarScore(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	case string:
		l := float64(len(x))
		if l > 100 {
			l = 100
		}
		return l / 100.0, true
	case bool:
		if x {
			return 0.65, true
		}
		return 0.35, true
	default:
		return 0, false
	}
}

// RouteVariant picks a TrafficSplitEntry deterministically based on
// the record's ordinal — mirrors fn route_variant in predictions.rs.
// The bucket is `(ordinal*37) mod 100`; we accumulate allocations
// (saturated at 255) and return the first split whose cumulative
// reaches the bucket. Falls back to splits[0] for the rare case where
// all allocations sum below the bucket.
func RouteVariant(splits []models.TrafficSplitEntry, ordinal int) (models.TrafficSplitEntry, bool) {
	if len(splits) == 0 {
		return models.TrafficSplitEntry{}, false
	}
	bucket := uint8((uint64(ordinal) * 37) % 100)
	var cumulative uint16
	for _, s := range splits {
		cumulative += uint16(s.Allocation)
		if cumulative > 255 {
			cumulative = 255
		}
		if uint16(bucket) < cumulative {
			return s, true
		}
	}
	return splits[0], true
}

// PredictRecord runs the model_state path when available and falls
// back to the deterministic sin-wave heuristic otherwise.
func PredictRecord(input any, split models.TrafficSplitEntry, runtime ModelRuntime, explain bool, ordinal int) models.PredictionOutput {
	if out, ok := predictWithModelState(input, split, runtime, explain, ordinal); ok {
		return out
	}
	return fallbackPredict(input, split, runtime.VersionNumber, explain, ordinal)
}

func predictWithModelState(input any, split models.TrafficSplitEntry, runtime ModelRuntime, explain bool, ordinal int) (models.PredictionOutput, bool) {
	zero := models.PredictionOutput{}
	rawState, ok := runtime.Schema["model_state"].(map[string]any)
	if !ok {
		return zero, false
	}
	featureNames := stringArray(rawState["feature_names"])
	featureMeans := numberArray(rawState["feature_means"])
	featureScales := numberArray(rawState["feature_scales"])
	weights := numberArray(rawState["weights"])
	if len(featureNames) == 0 ||
		len(featureNames) != len(weights) ||
		len(featureNames) != len(featureMeans) ||
		len(featureNames) != len(featureScales) {
		return zero, false
	}

	bias, _ := numberOr(rawState["bias"], 0)
	threshold, _ := numberOr(rawState["threshold"], 0.5)
	positiveLabel := stringOr(rawState["positive_label"], "positive")
	negativeLabel := stringOr(rawState["negative_label"], "negative")

	object, ok := input.(map[string]any)
	if !ok {
		return zero, false
	}
	standardized := make([]float64, 0, len(featureNames))
	contributions := make([]models.FeatureContribution, 0)
	for index, fname := range featureNames {
		raw, present := object[fname]
		if !present {
			return zero, false
		}
		score, _ := scalarScore(raw)
		scale := featureScales[index]
		if scale == 0 {
			scale = 1
		}
		standardizedValue := (score - featureMeans[index]) / scale
		standardized = append(standardized, standardizedValue)
		if explain {
			contributions = append(contributions, models.FeatureContribution{
				Name:  fname,
				Value: roundScore(math.Abs(weights[index] * standardizedValue)),
			})
		}
	}

	sort.SliceStable(contributions, func(i, j int) bool {
		return contributions[i].Value > contributions[j].Value
	})
	if len(contributions) > 3 {
		contributions = contributions[:3]
	}

	rawSignal := bias
	for i, w := range weights {
		rawSignal += w * standardized[i]
	}
	score := sigmoid(rawSignal)
	if score < 0.001 {
		score = 0.001
	} else if score > 0.999 {
		score = 0.999
	}
	score = roundScore(score)

	confidence := 0.5 + math.Abs(score-threshold)
	if confidence < 0.5 {
		confidence = 0.5
	} else if confidence > 0.99 {
		confidence = 0.99
	}
	confidence = roundScore(confidence)

	predictedLabel := negativeLabel
	if score >= threshold {
		predictedLabel = positiveLabel
	}
	return models.PredictionOutput{
		RecordID:       fmt.Sprintf("record-%d", ordinal+1),
		Variant:        split.Label,
		ModelVersionID: split.ModelVersionID,
		PredictedLabel: predictedLabel,
		Score:          score,
		Confidence:     confidence,
		Contributions:  contributions,
	}, true
}

func fallbackPredict(input any, split models.TrafficSplitEntry, versionNumber int32, explain bool, ordinal int) models.PredictionOutput {
	rawSignal := float64(versionNumber) * 0.08
	contributions := make([]models.FeatureContribution, 0)

	if obj, ok := input.(map[string]any); ok {
		for k, v := range obj {
			if score, ok := scalarScore(v); ok {
				rawSignal += score
				if explain {
					contributions = append(contributions, models.FeatureContribution{
						Name:  k,
						Value: roundScore(score),
					})
				}
			}
		}
	} else if score, ok := scalarScore(input); ok {
		rawSignal += score
		if explain {
			contributions = append(contributions, models.FeatureContribution{
				Name:  "input",
				Value: roundScore(score),
			})
		}
	}

	if len(contributions) == 0 && explain {
		contributions = append(contributions, models.FeatureContribution{Name: "bias", Value: 0.42})
	}

	sort.SliceStable(contributions, func(i, j int) bool {
		return contributions[i].Value > contributions[j].Value
	})
	if len(contributions) > 3 {
		contributions = contributions[:3]
	}

	score := (math.Sin(rawSignal) + 1.0) / 2.0
	if score < 0.02 {
		score = 0.02
	} else if score > 0.98 {
		score = 0.98
	}
	score = roundScore(score)

	confidence := 0.58 + math.Abs(score-0.5)*0.8
	if confidence < 0.51 {
		confidence = 0.51
	} else if confidence > 0.99 {
		confidence = 0.99
	}
	confidence = roundScore(confidence)

	predictedLabel := "negative"
	if score >= 0.5 {
		predictedLabel = "positive"
	}

	return models.PredictionOutput{
		RecordID:       fmt.Sprintf("record-%d", ordinal+1),
		Variant:        split.Label,
		ModelVersionID: split.ModelVersionID,
		PredictedLabel: predictedLabel,
		Score:          score,
		Confidence:     confidence,
		Contributions:  contributions,
	}
}

func sigmoid(v float64) float64 {
	return 1.0 / (1.0 + math.Exp(-v))
}

func stringArray(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func numberArray(v any) []float64 {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]float64, 0, len(arr))
	for _, item := range arr {
		if f, ok := numberOr(item, math.NaN()); ok {
			out = append(out, f)
		}
	}
	return out
}

func numberOr(v any, fallback float64) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		if f, err := x.Float64(); err == nil {
			return f, true
		}
	}
	return fallback, false
}

func stringOr(v any, fallback string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fallback
}
