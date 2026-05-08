package training

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/domain/interop"
	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

// TrialOutcome bundles a TrainingTrial with the per-trial metrics
// list + the derived model schema. Mirrors Rust TrialOutcome.
type TrialOutcome struct {
	Trial   models.TrainingTrial
	Metrics []models.MetricValue
	Schema  json.RawMessage
}

// trainingDataset is the parsed in-memory dataset the logistic
// regressor consumes. Private — exposed only via TrainTrial.
type trainingDataset struct {
	FeatureNames  []string
	FeatureMeans  []float64
	FeatureScales []float64
	Rows          [][]float64
	Labels        []float64
	LabelField    string
	PositiveLabel string
	NegativeLabel string
}

// HasInlineTrainingData mirrors fn has_inline_training_data — true
// when training_config.records is a non-empty array.
func HasInlineTrainingData(trainingConfig json.RawMessage) bool {
	if len(trainingConfig) == 0 {
		return false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trainingConfig, &obj); err != nil {
		return false
	}
	rawRecords, ok := obj["records"]
	if !ok {
		return false
	}
	var records []json.RawMessage
	if err := json.Unmarshal(rawRecords, &records); err != nil {
		return false
	}
	return len(records) > 0
}

// TrainTrial runs one logistic-regression training trial against the
// inline dataset in training_config. Mirrors fn train_trial verbatim.
func TrainTrial(trainingConfig, hyperparameters json.RawMessage, objectiveMetricName string, trialIndex int) (TrialOutcome, error) {
	dataset, err := parseDataset(trainingConfig)
	if err != nil {
		return TrialOutcome{}, err
	}

	hyper := map[string]any{}
	if len(hyperparameters) > 0 {
		_ = json.Unmarshal(hyperparameters, &hyper)
	}
	learningRate := ValueAsFloat64(hyper["learning_rate"], 0.08)
	epochs := int(ValueAsUint64(hyper["epochs"], 350))
	l2 := ValueAsFloat64(hyper["l2"], 0.0)

	weights, bias := fitLogisticRegression(dataset.Rows, dataset.Labels, learningRate, epochs, l2)
	metrics := evaluateMetrics(dataset, weights, bias)

	objectiveMetric := selectMetric(metrics, objectiveMetricName)
	if objectiveMetric == nil && len(metrics) > 0 {
		first := metrics[0]
		objectiveMetric = &first
	}
	if objectiveMetric == nil {
		objectiveMetric = &models.MetricValue{Name: objectiveMetricName, Value: 0}
	}

	framework := interop.EffectiveFramework(trainingConfig)
	adapter := interop.InferModelAdapter(trainingConfig, nil)

	rowCount := len(dataset.Rows)
	featureCount := 0
	if rowCount > 0 {
		featureCount = len(dataset.Rows[0])
	}

	schemaObj := map[string]any{
		"signature":     "tabular-binary",
		"engine":        framework,
		"model_adapter": adapter,
		"model_state": map[string]any{
			"feature_names":  dataset.FeatureNames,
			"feature_means":  dataset.FeatureMeans,
			"feature_scales": dataset.FeatureScales,
			"weights":        weights,
			"bias":           bias,
			"threshold":      0.5,
			"label_field":    dataset.LabelField,
			"positive_label": dataset.PositiveLabel,
			"negative_label": dataset.NegativeLabel,
		},
		"training_summary": map[string]any{
			"row_count":        rowCount,
			"feature_count":    featureCount,
			"objective_metric": objectiveMetric.Name,
			"objective_value":  objectiveMetric.Value,
			"framework":        framework,
		},
	}
	schemaJSON, _ := json.Marshal(schemaObj)

	hyperCopy := json.RawMessage(append([]byte(nil), hyperparameters...))
	if len(hyperCopy) == 0 {
		hyperCopy = json.RawMessage("{}")
	}

	return TrialOutcome{
		Trial: models.TrainingTrial{
			ID:              fmt.Sprintf("trial-%d", trialIndex+1),
			Status:          "completed",
			Hyperparameters: hyperCopy,
			ObjectiveMetric: *objectiveMetric,
		},
		Metrics: metrics,
		Schema:  schemaJSON,
	}, nil
}

func parseDataset(trainingConfig json.RawMessage) (*trainingDataset, error) {
	if len(trainingConfig) == 0 {
		return nil, errors.New("training_config.records must be a non-empty array")
	}
	var configObj map[string]json.RawMessage
	if err := json.Unmarshal(trainingConfig, &configObj); err != nil {
		return nil, errors.New("training_config.records must be a non-empty array")
	}
	rawRecords, ok := configObj["records"]
	if !ok {
		return nil, errors.New("training_config.records must be a non-empty array")
	}
	var records []map[string]any
	if err := json.Unmarshal(rawRecords, &records); err != nil {
		return nil, errors.New("each training record must be a JSON object")
	}
	if len(records) == 0 {
		return nil, errors.New("training_config.records must contain at least one row")
	}

	labelField := stringFromConfig(configObj, "label_field", "label")
	positiveLabel := stringFromConfig(configObj, "positive_label", "positive")
	negativeLabel := stringFromConfig(configObj, "negative_label", "negative")

	featureNames := stringArrayFromConfig(configObj, "features")
	if len(featureNames) == 0 {
		featureNames = deriveFeatureNames(records, labelField)
	}
	if len(featureNames) == 0 {
		return nil, errors.New("training_config.features resolved to an empty set")
	}

	rawRows := make([][]float64, 0, len(records))
	labels := make([]float64, 0, len(records))
	for _, record := range records {
		labelVal, has := record[labelField]
		if !has {
			return nil, fmt.Errorf("missing label field '%s'", labelField)
		}
		labels = append(labels, binaryLabel(labelVal, positiveLabel))
		row := make([]float64, 0, len(featureNames))
		for _, feature := range featureNames {
			row = append(row, scalarFeature(record[feature]))
		}
		rawRows = append(rawRows, row)
	}

	means, scales, rows := standardizeRows(rawRows)

	return &trainingDataset{
		FeatureNames:  featureNames,
		FeatureMeans:  means,
		FeatureScales: scales,
		Rows:          rows,
		Labels:        labels,
		LabelField:    labelField,
		PositiveLabel: positiveLabel,
		NegativeLabel: negativeLabel,
	}, nil
}

func stringFromConfig(obj map[string]json.RawMessage, key, fallback string) string {
	raw, ok := obj[key]
	if !ok || len(raw) == 0 {
		return fallback
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil || s == "" {
		return fallback
	}
	return s
}

func stringArrayFromConfig(obj map[string]json.RawMessage, key string) []string {
	raw, ok := obj[key]
	if !ok || len(raw) == 0 {
		return nil
	}
	var arr []any
	if err := json.Unmarshal(raw, &arr); err != nil {
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

func deriveFeatureNames(records []map[string]any, labelField string) []string {
	seen := map[string]struct{}{}
	names := []string{}
	for _, record := range records {
		for k := range record {
			if k == labelField {
				continue
			}
			if _, has := seen[k]; has {
				continue
			}
			seen[k] = struct{}{}
			names = append(names, k)
		}
	}
	sort.Strings(names)
	return names
}

func scalarFeature(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		if f, err := x.Float64(); err == nil {
			return f
		}
	case bool:
		if x {
			return 1.0
		}
		return 0.0
	case string:
		if f, err := strconv.ParseFloat(x, 64); err == nil {
			return f
		}
		var hash uint64
		for _, b := range []byte(x) {
			hash += uint64(b)
		}
		return float64(hash%1000) / 1000.0
	}
	return 0.0
}

func binaryLabel(v any, positiveLabel string) float64 {
	switch x := v.(type) {
	case bool:
		if x {
			return 1.0
		}
		return 0.0
	case float64:
		if x >= 0.5 {
			return 1.0
		}
		return 0.0
	case float32:
		if x >= 0.5 {
			return 1.0
		}
		return 0.0
	case int:
		if x >= 1 {
			return 1.0
		}
		return 0.0
	case int32:
		if x >= 1 {
			return 1.0
		}
		return 0.0
	case int64:
		if x >= 1 {
			return 1.0
		}
		return 0.0
	case json.Number:
		if f, err := x.Float64(); err == nil && f >= 0.5 {
			return 1.0
		}
		return 0.0
	case string:
		if x == positiveLabel || strings.EqualFold(x, "true") || x == "1" {
			return 1.0
		}
		return 0.0
	}
	return 0.0
}

func standardizeRows(rows [][]float64) (means, scales []float64, standardized [][]float64) {
	featureCount := 0
	if len(rows) > 0 {
		featureCount = len(rows[0])
	}
	means = make([]float64, featureCount)
	scales = make([]float64, featureCount)
	for i := range scales {
		scales[i] = 1.0
	}
	if len(rows) == 0 {
		return means, scales, [][]float64{}
	}

	for _, row := range rows {
		for i, v := range row {
			means[i] += v
		}
	}
	for i := range means {
		means[i] /= float64(len(rows))
	}

	// Reset scales to 0 before accumulating squared deltas (Rust
	// initialises scales=1 then *adds* delta² which is the same
	// numerical sequence as 0 + sum + ÷ N + sqrt because the +1
	// initial cancels with the same /N — actually no, the Rust code
	// uses += and starts at 1.0 so we must mirror it exactly).
	// Reset to 1.0 to track the +=.
	for i := range scales {
		scales[i] = 1.0
	}
	for _, row := range rows {
		for i, v := range row {
			delta := v - means[i]
			scales[i] += delta * delta
		}
	}
	for i := range scales {
		scales[i] = math.Sqrt(scales[i] / float64(len(rows)))
		if scales[i] == 0 {
			scales[i] = 1.0
		}
	}

	standardized = make([][]float64, 0, len(rows))
	for _, row := range rows {
		out := make([]float64, len(row))
		for i, v := range row {
			out[i] = (v - means[i]) / scales[i]
		}
		standardized = append(standardized, out)
	}
	return means, scales, standardized
}

func fitLogisticRegression(rows [][]float64, labels []float64, learningRate float64, epochs int, l2 float64) (weights []float64, bias float64) {
	featureCount := 0
	if len(rows) > 0 {
		featureCount = len(rows[0])
	}
	weights = make([]float64, featureCount)
	if len(rows) == 0 {
		return weights, 0
	}

	for epoch := 0; epoch < epochs; epoch++ {
		gradient := make([]float64, featureCount)
		biasGradient := 0.0

		for i, row := range rows {
			prediction := sigmoid(dot(weights, row) + bias)
			err := prediction - labels[i]
			for j, v := range row {
				gradient[j] += err * v
			}
			biasGradient += err
		}

		rowCount := float64(len(rows))
		for j := range weights {
			regularised := gradient[j]/rowCount + l2*weights[j]
			weights[j] -= learningRate * regularised
		}
		bias -= learningRate * (biasGradient / rowCount)
	}
	return weights, bias
}

func evaluateMetrics(dataset *trainingDataset, weights []float64, bias float64) []models.MetricValue {
	var truePositive, trueNegative, falsePositive, falseNegative, logLoss float64
	for i, row := range dataset.Rows {
		probability := sigmoid(dot(weights, row) + bias)
		if probability < 1e-6 {
			probability = 1e-6
		} else if probability > 1.0-1e-6 {
			probability = 1.0 - 1e-6
		}
		predicted := 0.0
		if probability >= 0.5 {
			predicted = 1.0
		}
		label := dataset.Labels[i]
		logLoss += -(label*math.Log(probability) + (1.0-label)*math.Log(1.0-probability))
		switch {
		case predicted == 1.0 && label == 1.0:
			truePositive++
		case predicted == 0.0 && label == 0.0:
			trueNegative++
		case predicted == 1.0 && label == 0.0:
			falsePositive++
		case predicted == 0.0 && label == 1.0:
			falseNegative++
		}
	}

	total := truePositive + trueNegative + falsePositive + falseNegative
	if total < 1.0 {
		total = 1.0
	}
	accuracy := roundMetric((truePositive + trueNegative) / total)

	pdenom := truePositive + falsePositive
	if pdenom < 1.0 {
		pdenom = 1.0
	}
	precision := roundMetric(truePositive / pdenom)

	rdenom := truePositive + falseNegative
	if rdenom < 1.0 {
		rdenom = 1.0
	}
	recall := roundMetric(truePositive / rdenom)

	var f1 float64
	if precision+recall > 0 {
		f1 = roundMetric(2.0 * precision * recall / (precision + recall))
	}

	return []models.MetricValue{
		{Name: "accuracy", Value: accuracy},
		{Name: "precision", Value: precision},
		{Name: "recall", Value: recall},
		{Name: "f1", Value: f1},
		{Name: "log_loss", Value: roundMetric(logLoss / total)},
	}
}

func selectMetric(metrics []models.MetricValue, name string) *models.MetricValue {
	for i := range metrics {
		if metrics[i].Name == name {
			m := metrics[i]
			return &m
		}
	}
	return nil
}

func dot(left, right []float64) float64 {
	var sum float64
	n := len(left)
	if len(right) < n {
		n = len(right)
	}
	for i := 0; i < n; i++ {
		sum += left[i] * right[i]
	}
	return sum
}

func sigmoid(v float64) float64 {
	return 1.0 / (1.0 + math.Exp(-v))
}

func roundMetric(v float64) float64 {
	return math.Round(v*10_000.0) / 10_000.0
}
