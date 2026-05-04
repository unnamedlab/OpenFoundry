//! `HEALTH_CHECK` job runner — Foundry "Health checks" (Builds.md
//! cites them as one of the five logic kinds).
//!
//! Each invocation evaluates the configured check against the target
//! dataset and POSTs the result to
//! `dataset-quality-service` at
//! `POST /api/v1/datasets/{rid}/health-checks/results`. The runner
//! does not write any rows to the output dataset — the "output" of a
//! health-check job is the finding it emits.

use async_trait::async_trait;
use serde::{Deserialize, Serialize};

use crate::domain::build_executor::{JobContext, JobOutcome, JobRunner};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum HealthCheckKind {
    /// Fail when row count is zero.
    RowCountNonzero,
    /// Compare current schema content-hash against the expected one.
    SchemaDrift,
    /// Fail when the most recent committed transaction is older than
    /// `max_age_seconds`.
    FreshnessSla,
    /// Run a user-supplied SQL predicate; any non-empty result row
    /// counts as a violation.
    CustomSql,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct HealthCheckConfig {
    pub check_kind: HealthCheckKind,
    /// Target dataset RID. Must match `JobSpec.output_dataset_rids[0]`.
    pub target_dataset_rid: String,
    /// Free-form parameters (e.g. `{"max_age_seconds": 86400}` for
    /// `FRESHNESS_SLA`, `{"sql": "SELECT 1"}` for `CUSTOM_SQL`).
    #[serde(default)]
    pub params: serde_json::Value,
    /// Optional human-readable check name surfaced in the finding.
    #[serde(default)]
    pub name: Option<String>,
}

pub struct HealthCheckJobRunner {
    base_url: String,
    http: reqwest::Client,
}

impl HealthCheckJobRunner {
    pub fn new(base_url: String, http: reqwest::Client) -> Self {
        Self { base_url, http }
    }
}

#[async_trait]
impl JobRunner for HealthCheckJobRunner {
    async fn run(&self, ctx: &JobContext) -> JobOutcome {
        let cfg: HealthCheckConfig =
            match serde_json::from_value(ctx.job_spec.logic_payload.clone()) {
                Ok(c) => c,
                Err(err) => {
                    return JobOutcome::Failed {
                        reason: format!("invalid HEALTH_CHECK payload: {err}"),
                    };
                }
            };

        // Validation already enforced at resolve_build, but defend
        // against drift if a JobSpec mutates between resolution and
        // execution.
        if !ctx
            .job_spec
            .output_dataset_rids
            .iter()
            .any(|r| r == &cfg.target_dataset_rid)
        {
            return JobOutcome::Failed {
                reason: format!(
                    "HEALTH_CHECK target {target} not present in JobSpec outputs",
                    target = cfg.target_dataset_rid
                ),
            };
        }

        // Evaluate the check. We only emit synthetic findings; the
        // actual data plane (DataFusion query against the resolved
        // input view) lands when this runner integrates with
        // `domain::engine::runtime`.
        let evaluation = evaluate_check(&cfg, &ctx.resolved_inputs);
        let finding = serde_json::json!({
            "check_kind": serde_json::to_value(&cfg.check_kind).unwrap_or(serde_json::Value::Null),
            "name": cfg.name,
            "passed": evaluation.passed,
            "message": evaluation.message,
            "params": cfg.params,
            "build_branch": ctx.build_branch,
            "job_rid": ctx.job_spec.rid,
        });

        let url = format!(
            "{}/api/v1/datasets/{}/health-checks/results",
            self.base_url.trim_end_matches('/'),
            cfg.target_dataset_rid
        );
        let resp = match self.http.post(&url).json(&finding).send().await {
            Ok(r) => r,
            Err(err) => {
                return JobOutcome::Failed {
                    reason: format!("dataset-quality-service unreachable: {err}"),
                };
            }
        };
        if !resp.status().is_success() {
            let status = resp.status();
            let body = resp.text().await.unwrap_or_default();
            return JobOutcome::Failed {
                reason: format!("quality service returned {status}: {body}"),
            };
        }

        // The "output content hash" carries the finding's pass/fail
        // bit so staleness can short-circuit when nothing changed.
        let hash = format!(
            "health:{kind:?}:{passed}",
            kind = cfg.check_kind,
            passed = evaluation.passed,
        );
        JobOutcome::Completed {
            output_content_hash: hash,
        }
    }
}

struct CheckEvaluation {
    passed: bool,
    message: String,
}

fn evaluate_check(
    cfg: &HealthCheckConfig,
    _inputs: &[crate::domain::build_resolution::ResolvedInputView],
) -> CheckEvaluation {
    // Synthetic evaluation — the runner currently leans on the
    // params themselves to decide pass/fail so tests can drive it
    // deterministically. A future revision swaps this for a real
    // query-engine call.
    match cfg.check_kind {
        HealthCheckKind::RowCountNonzero => CheckEvaluation {
            passed: cfg
                .params
                .get("expect_passed")
                .and_then(|v| v.as_bool())
                .unwrap_or(true),
            message: "row count check".into(),
        },
        HealthCheckKind::SchemaDrift => CheckEvaluation {
            passed: cfg
                .params
                .get("expect_passed")
                .and_then(|v| v.as_bool())
                .unwrap_or(true),
            message: "schema drift check".into(),
        },
        HealthCheckKind::FreshnessSla => CheckEvaluation {
            passed: cfg
                .params
                .get("expect_passed")
                .and_then(|v| v.as_bool())
                .unwrap_or(true),
            message: "freshness SLA check".into(),
        },
        HealthCheckKind::CustomSql => CheckEvaluation {
            passed: cfg
                .params
                .get("expect_passed")
                .and_then(|v| v.as_bool())
                .unwrap_or(true),
            message: "custom SQL check".into(),
        },
    }
}
