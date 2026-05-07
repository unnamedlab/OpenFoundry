package training

import (
	"encoding/json"
	"sort"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/domain/interop"
	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

// Execution bundles the result of a TrainingJob run. Mirrors Rust
// TrainingExecution struct in libs/ml-kernel/src/domain/training/
// mod.rs.
type Execution struct {
	Trials              []models.TrainingTrial
	BestHyperparameters json.RawMessage
	BestMetrics         []models.MetricValue
	BestSchema          json.RawMessage
	BestArtifactURI     string
}

// ExecuteTraining mirrors fn execute_training. Three branches:
//
//  1. external tracking source detected on training_config →
//     synthesises a single imported-run trial and a schema that
//     captures the import-mode reproducibility metadata. No actual
//     training runs.
//  2. no inline records → returns synthetic trials (one per
//     candidate set) with deterministic 0.5 + 0.05*i objective values.
//  3. inline records present → runs train_trial for each candidate
//     set, sorts by objective_metric.value desc, picks the best.
func ExecuteTraining(trainingConfig, search json.RawMessage, objectiveMetricName string) (*Execution, error) {
	if external := interop.TrackingSourceFromTrainingConfig(trainingConfig); external != nil {
		return executeExternalImport(trainingConfig, search, objectiveMetricName, external)
	}

	if !HasInlineTrainingData(trainingConfig) {
		trials := syntheticTrials(search, objectiveMetricName)
		var bestHyperparameters json.RawMessage
		if len(trials) > 0 {
			bestHyperparameters = trials[0].Hyperparameters
		}
		return &Execution{
			Trials:              trials,
			BestHyperparameters: bestHyperparameters,
			BestMetrics:         []models.MetricValue{},
		}, nil
	}

	candidates := CandidateSets(jsonToObject(search))
	outcomes := make([]TrialOutcome, 0, len(candidates))
	for i, candidate := range candidates {
		raw, _ := json.Marshal(candidate)
		outcome, err := TrainTrial(trainingConfig, raw, objectiveMetricName, i)
		if err != nil {
			return nil, err
		}
		outcomes = append(outcomes, outcome)
	}
	sort.SliceStable(outcomes, func(i, j int) bool {
		return outcomes[i].Trial.ObjectiveMetric.Value > outcomes[j].Trial.ObjectiveMetric.Value
	})

	trials := make([]models.TrainingTrial, 0, len(outcomes))
	for _, o := range outcomes {
		trials = append(trials, o.Trial)
	}

	exec := &Execution{
		Trials:      trials,
		BestMetrics: []models.MetricValue{},
	}
	if len(outcomes) > 0 {
		exec.BestHyperparameters = outcomes[0].Trial.Hyperparameters
		exec.BestMetrics = outcomes[0].Metrics
		exec.BestSchema = outcomes[0].Schema
	}
	return exec, nil
}

func executeExternalImport(trainingConfig, search json.RawMessage, objectiveMetricName string, external *models.ExternalTrackingSource) (*Execution, error) {
	metrics := interop.MergeMetrics([]models.MetricValue{}, external.Metrics)
	bestArtifactURI := interop.PreferredArtifactURI(external, trainingConfig)

	objectiveMetric := models.MetricValue{Name: objectiveMetricName, Value: 0}
	if got := selectMetric(metrics, objectiveMetricName); got != nil {
		objectiveMetric = *got
	} else if len(metrics) > 0 {
		objectiveMetric = metrics[0]
	}

	hyperparameters := json.RawMessage("{}")
	if len(external.Params) > 0 {
		// External tracking params override only when they're a JSON
		// object; arrays / scalars fall back to training_config.
		if isJSONObject(external.Params) {
			hyperparameters = external.Params
		}
	}
	if len(hyperparameters) == 0 || string(hyperparameters) == "{}" {
		// Fall back to training_config.hyperparameters when the
		// external source didn't supply a usable params object.
		if cfgObj := jsonToObject(trainingConfig); cfgObj != nil {
			if v, ok := cfgObj["hyperparameters"]; ok {
				if raw, err := json.Marshal(v); err == nil {
					hyperparameters = raw
				}
			}
		}
	}

	signature := "external-model"
	if cfgObj := jsonToObject(trainingConfig); cfgObj != nil {
		if v, ok := cfgObj["signature"].(string); ok && v != "" {
			signature = v
		}
	}

	repro := map[string]any{
		"training_config": rawOrNull(trainingConfig),
		"hyperparameter_search": func() any {
			if len(search) == 0 {
				return map[string]any{}
			}
			return rawOrNull(search)
		}(),
		"import_mode": "external_tracking",
	}
	schemaSeed := map[string]any{
		"signature":         signature,
		"engine":            interop.EffectiveFramework(trainingConfig),
		"objective_metric":  objectiveMetricName,
		"observed_metrics":  metrics,
		"reproducibility":   repro,
	}
	schemaSeedJSON, _ := json.Marshal(schemaSeed)
	schema := interop.NormalizeModelVersionSchema(
		schemaSeedJSON,
		bestArtifactURI,
		trainingConfig,
		nil,
		nil,
		external,
	)

	trialID := "imported-run"
	if external.RunID != "" {
		trialID = "imported-" + external.RunID
	}

	return &Execution{
		Trials: []models.TrainingTrial{{
			ID:              trialID,
			Status:          "completed",
			Hyperparameters: hyperparameters,
			ObjectiveMetric: objectiveMetric,
		}},
		BestHyperparameters: hyperparameters,
		BestMetrics:         metrics,
		BestSchema:          schema,
		BestArtifactURI:     bestArtifactURI,
	}, nil
}

func syntheticTrials(search json.RawMessage, objectiveMetricName string) []models.TrainingTrial {
	candidates := CandidateSets(jsonToObject(search))
	trials := make([]models.TrainingTrial, 0, len(candidates))
	for i, c := range candidates {
		raw, _ := json.Marshal(c)
		trials = append(trials, models.TrainingTrial{
			ID:              "trial-" + itoa(i+1),
			Status:          "completed",
			Hyperparameters: raw,
			ObjectiveMetric: models.MetricValue{
				Name:  objectiveMetricName,
				Value: 0.5 + float64(i)*0.05,
			},
		})
	}
	return trials
}

func jsonToObject(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	return obj
}

func isJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	for _, b := range raw {
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
			continue
		}
		return b == '{'
	}
	return false
}

func rawOrNull(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := false
	x := n
	if x < 0 {
		negative = true
		x = -x
	}
	var b [20]byte
	i := len(b)
	for x > 0 {
		i--
		b[i] = byte('0' + x%10)
		x /= 10
	}
	if negative {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
