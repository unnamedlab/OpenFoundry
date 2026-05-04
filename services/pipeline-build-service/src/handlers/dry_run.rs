//! `POST /pipelines/{rid}/dry-run-resolve`
//!
//! Pure simulation of `compile_build_graph` + `branch_resolution`
//! without acquiring locks or opening transactions. Used by the
//! Pipeline Builder UI's "Resolved build plan" preview to show
//! engineers exactly which JobSpec branch each output will resolve
//! against and which branch each input dataset will read from before
//! they hit "Build".
//!
//! Two backing collaborators are required:
//!   * a [`JobSpecRepo`] — looks up specs from
//!     `pipeline-authoring-service`. Falls back to
//!     `InMemoryJobSpecRepo` for callers that supply specs inline.
//!   * a [`DatasetVersioningClient`] — lists branches per input
//!     dataset. Allowed to be `None`: in that case dry-run returns the
//!     graph but skips per-input branch resolution.

use std::sync::Arc;

use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};

use crate::AppState;
use crate::domain::branch_resolution::{
    ResolveError, ResolvedInput, ResolvedOutput, resolve_input_dataset, resolve_output_dataset,
};
use crate::domain::build_resolution::{DatasetVersioningClient, InputSpec, JobSpec, JobSpecRepo};
use crate::domain::job_graph::{CompileError, InMemoryJobSpecRepo, JobGraph, compile_build_graph};
use crate::domain::metrics;

#[derive(Debug, Deserialize)]
pub struct DryRunRequest {
    pub build_branch: String,
    #[serde(default)]
    pub job_spec_fallback: Vec<String>,
    pub output_dataset_rids: Vec<String>,
    /// Optional inline JobSpecs — when provided, the handler uses
    /// them instead of going to `pipeline-authoring-service`. Lets
    /// the Pipeline Builder UI preview a graph that hasn't been
    /// published yet.
    #[serde(default)]
    pub inline_specs: Vec<JobSpec>,
    /// Per-input override `{ dataset_rid: ["develop", "master"] }`.
    /// Wins over the JobSpec's declared per-input chain.
    #[serde(default)]
    pub input_overrides: Vec<InputOverride>,
}

#[derive(Debug, Deserialize)]
pub struct InputOverride {
    pub dataset_rid: String,
    pub fallback_chain: Vec<String>,
}

#[derive(Debug, Serialize)]
pub struct DryRunResponse {
    pub jobs: Vec<DryRunJob>,
    pub errors: Vec<DryRunError>,
}

#[derive(Debug, Serialize)]
pub struct DryRunJob {
    pub job_spec_rid: String,
    pub resolved_jobspec_branch: String,
    pub output_dataset_rids: Vec<String>,
    pub resolved_outputs: Vec<DryRunOutput>,
    pub resolved_inputs: Vec<DryRunInput>,
}

#[derive(Debug, Serialize)]
pub struct DryRunInput {
    pub dataset_rid: String,
    pub resolved_input_branch: Option<String>,
    pub fallback_index: Option<usize>,
    pub fallback_chain: Vec<String>,
}

#[derive(Debug, Serialize)]
pub struct DryRunOutput {
    pub dataset_rid: String,
    pub resolved_output: String,
    pub creates_branch: bool,
    pub from_branch: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct DryRunError {
    pub dataset_rid: Option<String>,
    pub kind: String,
    pub message: String,
}

pub async fn dry_run_resolve(
    AuthUser(_claims): AuthUser,
    State(state): State<AppState>,
    Path(pipeline_rid): Path<String>,
    Json(body): Json<DryRunRequest>,
) -> impl IntoResponse {
    if body.build_branch.trim().is_empty() {
        return (StatusCode::BAD_REQUEST, "build_branch is required").into_response();
    }
    if body.output_dataset_rids.is_empty() {
        return (
            StatusCode::BAD_REQUEST,
            "output_dataset_rids must declare at least one dataset",
        )
            .into_response();
    }

    // Build the JobSpec repo: prefer inline specs, then fall back to
    // the wired-up authoring-service client when none provided.
    let inline_repo: Arc<dyn JobSpecRepo> = if !body.inline_specs.is_empty() {
        let mut repo = InMemoryJobSpecRepo::new();
        for spec in &body.inline_specs {
            repo.insert(spec.clone());
        }
        Arc::new(repo)
    } else if let Some(ports) = &state.lifecycle_ports {
        ports.job_specs.clone()
    } else {
        // No backing client and no inline specs ⇒ explicit error.
        let payload = DryRunResponse {
            jobs: vec![],
            errors: vec![DryRunError {
                dataset_rid: None,
                kind: "MISSING_BACKING_CLIENT".into(),
                message:
                    "dry-run-resolve needs either inline_specs or a configured JobSpecRepo client"
                        .into(),
            }],
        };
        return (StatusCode::BAD_REQUEST, Json(payload)).into_response();
    };

    let graph: JobGraph = match compile_build_graph(
        &pipeline_rid,
        &body.build_branch,
        &body.output_dataset_rids,
        &body.job_spec_fallback,
        inline_repo.as_ref(),
    )
    .await
    {
        Ok(g) => {
            metrics::record_build_resolution("ok");
            g
        }
        Err(CompileError::MissingJobSpec {
            dataset_rid,
            tried_branches,
        }) => {
            metrics::record_build_resolution("missing_spec");
            let payload = DryRunResponse {
                jobs: vec![],
                errors: vec![DryRunError {
                    dataset_rid: Some(dataset_rid.clone()),
                    kind: "MISSING_JOB_SPEC".into(),
                    message: format!(
                        "no JobSpec for {dataset_rid}; tried branches {tried_branches:?}"
                    ),
                }],
            };
            return (StatusCode::OK, Json(payload)).into_response();
        }
        Err(CompileError::Cycle { cycle }) => {
            metrics::record_build_resolution("cycle");
            let payload = DryRunResponse {
                jobs: vec![],
                errors: vec![DryRunError {
                    dataset_rid: None,
                    kind: "CYCLE".into(),
                    message: format!("cycle: {}", cycle.join(" → ")),
                }],
            };
            return (StatusCode::OK, Json(payload)).into_response();
        }
        Err(other) => {
            metrics::record_build_resolution("missing_spec");
            return (StatusCode::INTERNAL_SERVER_ERROR, other.to_string()).into_response();
        }
    };

    // Per-input branch resolution: if a versioning client is wired up,
    // use it; otherwise return the chain as-declared without
    // confirming branch existence.
    let versioning: Option<Arc<dyn DatasetVersioningClient>> =
        state.lifecycle_ports.as_ref().map(|p| p.versioning.clone());

    let mut jobs = Vec::new();
    let mut errors = Vec::new();

    let build_branch_parsed: core_models::dataset::transaction::BranchName =
        match body.build_branch.parse() {
            Ok(b) => b,
            Err(_) => {
                return (StatusCode::BAD_REQUEST, "invalid build_branch").into_response();
            }
        };

    for node in &graph.nodes {
        let mut resolved_inputs = Vec::new();
        for input in &node.job_spec.inputs {
            let chain = input_chain(input, &body.input_overrides);
            let resolved =
                resolve_input_branch(versioning.as_deref(), input, &chain, &build_branch_parsed)
                    .await;
            match resolved {
                Ok((branch, idx)) => {
                    resolved_inputs.push(DryRunInput {
                        dataset_rid: input.dataset_rid.clone(),
                        resolved_input_branch: Some(branch),
                        fallback_index: Some(idx),
                        fallback_chain: chain,
                    });
                }
                Err(error) => {
                    metrics::record_build_resolution("incompatible_ancestry");
                    errors.push(DryRunError {
                        dataset_rid: Some(input.dataset_rid.clone()),
                        kind: "INPUT_NOT_RESOLVABLE".into(),
                        message: error,
                    });
                    resolved_inputs.push(DryRunInput {
                        dataset_rid: input.dataset_rid.clone(),
                        resolved_input_branch: None,
                        fallback_index: None,
                        fallback_chain: chain,
                    });
                }
            }
        }

        let mut resolved_outputs = Vec::new();
        for output_rid in &node.job_spec.output_dataset_rids {
            let chain: Vec<core_models::dataset::transaction::BranchName> = body
                .job_spec_fallback
                .iter()
                .filter_map(|s| s.parse().ok())
                .collect();
            let outcome = match versioning.as_deref() {
                Some(client) => match client.list_branches(output_rid).await {
                    Ok(branches) => {
                        let branch_names: Vec<_> =
                            branches.iter().map(|b| b.name.clone()).collect();
                        match resolve_output_dataset(&build_branch_parsed, &chain, &branch_names) {
                            Ok(ResolvedOutput::Existing(name)) => DryRunOutput {
                                dataset_rid: output_rid.clone(),
                                resolved_output: name.into_string(),
                                creates_branch: false,
                                from_branch: None,
                            },
                            Ok(ResolvedOutput::CreateFrom { new_branch, from }) => DryRunOutput {
                                dataset_rid: output_rid.clone(),
                                resolved_output: new_branch.into_string(),
                                creates_branch: true,
                                from_branch: Some(from.into_string()),
                            },
                            Err(e) => {
                                errors.push(DryRunError {
                                    dataset_rid: Some(output_rid.clone()),
                                    kind: "OUTPUT_NOT_RESOLVABLE".into(),
                                    message: e.to_string(),
                                });
                                DryRunOutput {
                                    dataset_rid: output_rid.clone(),
                                    resolved_output: body.build_branch.clone(),
                                    creates_branch: false,
                                    from_branch: None,
                                }
                            }
                        }
                    }
                    Err(e) => {
                        errors.push(DryRunError {
                            dataset_rid: Some(output_rid.clone()),
                            kind: "VERSIONING_CLIENT_ERROR".into(),
                            message: e.to_string(),
                        });
                        DryRunOutput {
                            dataset_rid: output_rid.clone(),
                            resolved_output: body.build_branch.clone(),
                            creates_branch: false,
                            from_branch: None,
                        }
                    }
                },
                None => DryRunOutput {
                    dataset_rid: output_rid.clone(),
                    resolved_output: body.build_branch.clone(),
                    creates_branch: false,
                    from_branch: None,
                },
            };
            resolved_outputs.push(outcome);
        }

        jobs.push(DryRunJob {
            job_spec_rid: node.job_spec.rid.clone(),
            resolved_jobspec_branch: node.job_spec.branch_name.clone(),
            output_dataset_rids: node.job_spec.output_dataset_rids.clone(),
            resolved_outputs,
            resolved_inputs,
        });
    }

    let payload = DryRunResponse { jobs, errors };
    (StatusCode::OK, Json(payload)).into_response()
}

fn input_chain(input: &InputSpec, overrides: &[InputOverride]) -> Vec<String> {
    overrides
        .iter()
        .find(|o| o.dataset_rid == input.dataset_rid)
        .map(|o| o.fallback_chain.clone())
        .unwrap_or_else(|| input.fallback_chain.clone())
}

async fn resolve_input_branch(
    client: Option<&dyn DatasetVersioningClient>,
    input: &InputSpec,
    chain: &[String],
    build_branch: &core_models::dataset::transaction::BranchName,
) -> Result<(String, usize), String> {
    let chain_names: Vec<core_models::dataset::transaction::BranchName> =
        chain.iter().filter_map(|s| s.parse().ok()).collect();

    match client {
        None => {
            // No versioning client wired — return the build branch as
            // the optimistic resolution.
            Ok((build_branch.as_str().to_string(), 0))
        }
        Some(client) => {
            let branches = client
                .list_branches(&input.dataset_rid)
                .await
                .map_err(|e| e.to_string())?;
            let names: Vec<_> = branches.iter().map(|b| b.name.clone()).collect();
            match resolve_input_dataset(build_branch, &chain_names, &names) {
                Ok(ResolvedInput {
                    branch,
                    fallback_index,
                }) => Ok((branch.into_string(), fallback_index)),
                Err(ResolveError::NoMatch {
                    tried, available, ..
                }) => Err(format!(
                    "no branch on {ds} matches build={bb} (tried {tried:?}, available {available:?})",
                    ds = input.dataset_rid,
                    bb = build_branch.as_str(),
                )),
                Err(ResolveError::IncompatibleAncestry { .. }) => Err(format!(
                    "fallback chain on {ds} is not ancestry-compatible",
                    ds = input.dataset_rid
                )),
            }
        }
    }
}

#[allow(dead_code)]
fn _ensure_used() -> Value {
    json!({})
}
