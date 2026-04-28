pub mod hyperparameter;
pub mod runner;

use serde_json::Value;

use crate::{
    domain::interop,
    models::{run::MetricValue, training_job::TrainingTrial},
};

#[derive(Debug, Clone)]
pub struct TrainingExecution {
    pub trials: Vec<TrainingTrial>,
    pub best_hyperparameters: Option<Value>,
    pub best_metrics: Vec<MetricValue>,
    pub best_schema: Option<Value>,
    pub best_artifact_uri: Option<String>,
}

pub fn execute_training(
    training_config: &Value,
    search: Option<&Value>,
    objective_metric_name: &str,
) -> Result<TrainingExecution, String> {
    if let Some(external_training) = interop::tracking_source_from_training_config(training_config)
    {
        let metrics = interop::merge_metrics(&[], &external_training.metrics);
        let best_artifact_uri =
            interop::preferred_artifact_uri(Some(&external_training), Some(training_config));
        let objective_metric = metrics
            .iter()
            .find(|metric| metric.name == objective_metric_name)
            .cloned()
            .or_else(|| metrics.first().cloned())
            .unwrap_or(MetricValue {
                name: objective_metric_name.to_string(),
                value: 0.0,
            });
        let hyperparameters = match external_training.params.clone() {
            Value::Object(_) => external_training.params.clone(),
            _ => training_config
                .get("hyperparameters")
                .cloned()
                .unwrap_or_else(|| Value::Object(Default::default())),
        };
        let schema = interop::normalize_model_version_schema(
            Some(serde_json::json!({
                "signature": training_config
                    .get("signature")
                    .and_then(Value::as_str)
                    .unwrap_or("external-model"),
                "engine": interop::effective_framework(training_config),
                "objective_metric": objective_metric_name,
                "observed_metrics": metrics.clone(),
                "reproducibility": {
                    "training_config": training_config,
                    "hyperparameter_search": search.cloned().unwrap_or_else(|| serde_json::json!({})),
                    "import_mode": "external_tracking"
                }
            })),
            best_artifact_uri.as_deref(),
            Some(training_config),
            None,
            None,
            Some(&external_training),
        );

        return Ok(TrainingExecution {
            trials: vec![TrainingTrial {
                id: if external_training.run_id.is_empty() {
                    "imported-run".to_string()
                } else {
                    format!("imported-{}", external_training.run_id)
                },
                status: "completed".to_string(),
                hyperparameters: hyperparameters.clone(),
                objective_metric,
            }],
            best_hyperparameters: Some(hyperparameters),
            best_metrics: metrics,
            best_schema: Some(schema),
            best_artifact_uri,
        });
    }

    if !runner::has_inline_training_data(training_config) {
        let trials = synthetic_trials(search, objective_metric_name);
        let best_hyperparameters = trials.first().map(|trial| trial.hyperparameters.clone());
        return Ok(TrainingExecution {
            trials,
            best_hyperparameters,
            best_metrics: Vec::new(),
            best_schema: None,
            best_artifact_uri: None,
        });
    }

    let mut outcomes = hyperparameter::candidate_sets(search)
        .into_iter()
        .enumerate()
        .map(|(index, candidate)| {
            runner::train_trial(training_config, &candidate, objective_metric_name, index)
        })
        .collect::<Result<Vec<_>, _>>()?;
    outcomes.sort_by(|left, right| {
        right
            .trial
            .objective_metric
            .value
            .total_cmp(&left.trial.objective_metric.value)
    });

    let trials = outcomes
        .iter()
        .map(|outcome| outcome.trial.clone())
        .collect();
    let best = outcomes.first();

    Ok(TrainingExecution {
        trials,
        best_hyperparameters: best.map(|outcome| outcome.trial.hyperparameters.clone()),
        best_metrics: best
            .map(|outcome| outcome.metrics.clone())
            .unwrap_or_default(),
        best_schema: best.map(|outcome| outcome.schema.clone()),
        best_artifact_uri: None,
    })
}

fn synthetic_trials(search: Option<&Value>, objective_metric_name: &str) -> Vec<TrainingTrial> {
    hyperparameter::candidate_sets(search)
        .into_iter()
        .enumerate()
        .map(|(index, hyperparameters)| TrainingTrial {
            id: format!("trial-{}", index + 1),
            status: "completed".to_string(),
            hyperparameters,
            objective_metric: MetricValue {
                name: objective_metric_name.to_string(),
                value: 0.5 + index as f64 * 0.05,
            },
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use serde_json::json;

    use super::execute_training;

    #[test]
    fn imports_external_tracking_runs_into_training_execution() {
        let execution = execute_training(
            &json!({
                "external_training": {
                    "system": "mlflow",
                    "run_id": "run-42",
                    "framework": "xgboost",
                    "model_uri": "models:/fraud-detector/12",
                    "params": {
                        "max_depth": 8,
                        "eta": 0.12
                    },
                    "metrics": [
                        { "name": "roc_auc", "value": 0.94 },
                        { "name": "log_loss", "value": 0.18 }
                    ]
                }
            }),
            Some(&json!({ "strategy": "external-import" })),
            "roc_auc",
        )
        .expect("external training should execute");

        assert_eq!(execution.trials.len(), 1);
        assert_eq!(execution.trials[0].objective_metric.name, "roc_auc");
        assert_eq!(
            execution.best_artifact_uri.as_deref(),
            Some("models:/fraud-detector/12")
        );
        assert_eq!(
            execution
                .best_schema
                .as_ref()
                .and_then(|schema| schema.pointer("/model_adapter/framework"))
                .and_then(|value| value.as_str()),
            Some("xgboost")
        );
    }
}
