//! Parallel job orchestrator + failure cascade.
//!
//! Foundry Builds.md § Job execution:
//!
//!   * "Jobs that do not depend on each other are run in parallel."
//!   * "If a job in a build fails, all directly-dependent jobs and
//!     transactions on output datasets within that build will be
//!     terminated. Optionally, a build can be configured to abort
//!     all non-dependent jobs at the same time."
//!   * "If all jobs in the build are completed, then the build is
//!     considered completed."
//!   * "Note that if a job in a build fails, previously completed
//!     jobs may still have written data to their output datasets."
//!
//! This module owns the runtime side of the lifecycle. It is fed
//! a [`ResolvedBuild`] from `build_resolution::resolve_build` (or
//! reads the equivalent rows from Postgres for queued builds that
//! later un-queue) and drives the jobs to a terminal `BuildState` via
//! a `tokio::task::JoinSet`-based ready-queue scheduler.
//!
//! The actual transform execution is delegated to a [`JobRunner`]
//! trait so unit tests can inject deterministic outcomes without the
//! full polars/datafusion stack. The production runner lives in
//! `engine::node_runner` and is wired up at service boot.

use std::collections::{HashMap, HashSet};
use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use chrono::Utc;
use serde::{Deserialize, Serialize};
use sqlx::PgPool;
use tokio::sync::{Semaphore, watch};
use tokio::task::JoinSet;
use uuid::Uuid;

use crate::domain::build_resolution::{JobSpec, ResolvedInputView};
use crate::domain::job_lifecycle::{transition_job_in_tx, JobLifecycleError};
use crate::domain::metrics;
use crate::domain::staleness::{self, StalenessOutcome};
use crate::models::build::{AbortPolicy, BuildState};
use crate::models::job::JobState;

/// Default fan-out per build, overridden via `BUILD_PARALLELISM`.
pub const DEFAULT_PARALLELISM: usize = 4;

/// Read `BUILD_PARALLELISM` from the environment, falling back to
/// [`DEFAULT_PARALLELISM`]. Capped at 1 minimum so a misconfiguration
/// can't deadlock the executor.
pub fn parallelism_from_env() -> usize {
    std::env::var("BUILD_PARALLELISM")
        .ok()
        .and_then(|v| v.parse::<usize>().ok())
        .filter(|v| *v > 0)
        .unwrap_or(DEFAULT_PARALLELISM)
}

// ---------------------------------------------------------------------------
// JobRunner trait + outcome
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum JobOutcome {
    /// Logic ran end-to-end. The executor will commit every output
    /// transaction atomically and mark the job COMPLETED.
    Completed {
        output_content_hash: String,
    },
    /// Logic raised an error. The executor will abort every output
    /// transaction (multi-output atomicity) and mark the job FAILED.
    Failed {
        reason: String,
    },
}

#[derive(Clone)]
pub struct JobContext {
    pub build_id: Uuid,
    pub build_branch: String,
    pub job_id: Uuid,
    pub job_spec: JobSpec,
    pub resolved_inputs: Vec<ResolvedInputView>,
    pub force_build: bool,
    /// P4 — runners emit live-log entries here. `None` means the
    /// host did not configure live logs (test harnesses) and
    /// runners should fall back to `tracing::*`.
    pub log_sink: Option<std::sync::Arc<dyn crate::domain::logs::LogSink>>,
}

impl std::fmt::Debug for JobContext {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("JobContext")
            .field("build_id", &self.build_id)
            .field("build_branch", &self.build_branch)
            .field("job_id", &self.job_id)
            .field("job_spec", &self.job_spec)
            .field("resolved_inputs", &self.resolved_inputs)
            .field("force_build", &self.force_build)
            .field("log_sink", &self.log_sink.as_ref().map(|_| "<sink>"))
            .finish()
    }
}

#[async_trait]
pub trait JobRunner: Send + Sync {
    async fn run(&self, ctx: &JobContext) -> JobOutcome;
}

/// Side-channel used by P4 (live logs / SSE fan-out) to observe
/// per-job state changes without polling Postgres. The receiver side
/// is exposed through [`ExecuteBuildHandle::job_state_rx`].
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct JobStateEvent {
    pub job_id: Uuid,
    pub job_spec_rid: String,
    pub state: JobState,
    pub stale_skipped: bool,
    pub failure_reason: Option<String>,
    pub at: chrono::DateTime<Utc>,
}

// ---------------------------------------------------------------------------
// Output-transaction client (commit / abort)
// ---------------------------------------------------------------------------

#[derive(Debug, thiserror::Error)]
#[error("output transaction client error: {0}")]
pub struct OutputClientError(pub String);

#[async_trait]
pub trait OutputTransactionClient: Send + Sync {
    /// Commit the open transaction on `dataset_rid`. Foundry: this is
    /// what flips the dataset's HEAD to the new view.
    async fn commit(&self, dataset_rid: &str, transaction_rid: &str) -> Result<(), OutputClientError>;
    /// Abort an open transaction. Called during cascade aborts and
    /// multi-output rollback.
    async fn abort(&self, dataset_rid: &str, transaction_rid: &str) -> Result<(), OutputClientError>;
}

// ---------------------------------------------------------------------------
// Public entry point
// ---------------------------------------------------------------------------

#[derive(Debug, thiserror::Error)]
pub enum BuildExecutionError {
    #[error("build {0} not found")]
    BuildNotFound(Uuid),
    #[error("transition error: {0}")]
    Lifecycle(#[from] JobLifecycleError),
    #[error(transparent)]
    Db(#[from] sqlx::Error),
}

pub struct ExecuteBuildArgs {
    pub build_id: Uuid,
    pub parallelism: usize,
    pub runner: Arc<dyn JobRunner>,
    pub output_client: Arc<dyn OutputTransactionClient>,
    /// Resolved input views per `JobSpec.rid`. The executor uses these
    /// to (a) compute the staleness signature and (b) feed
    /// [`JobContext`] into the runner.
    pub job_inputs: HashMap<String, Vec<ResolvedInputView>>,
    pub job_specs: Vec<JobSpec>,
}

#[derive(Debug, Clone)]
pub struct ExecuteBuildOutcome {
    pub final_state: BuildState,
    pub completed: usize,
    pub failed: usize,
    pub aborted: usize,
    pub stale_skipped: usize,
}

/// Drive a build to a terminal state. Returns the aggregate
/// [`ExecuteBuildOutcome`] once every job is COMPLETED, FAILED, or
/// ABORTED.
pub async fn execute_build(
    pool: &PgPool,
    args: ExecuteBuildArgs,
) -> Result<ExecuteBuildOutcome, BuildExecutionError> {
    let timer = metrics::BUILD_EXECUTION_DURATION_SECONDS.start_timer();

    let build_row = load_build(pool, args.build_id).await?;
    let abort_policy = build_row.abort_policy;

    // Mark build BUILD_RUNNING.
    sqlx::query(
        "UPDATE builds SET state = $1, started_at = COALESCE(started_at, NOW()) WHERE id = $2",
    )
    .bind(BuildState::Running.as_str())
    .bind(args.build_id)
    .execute(pool)
    .await?;
    metrics::record_build_state(BuildState::Running);
    let started_outputs: Vec<String> = args
        .job_specs
        .iter()
        .flat_map(|spec| spec.output_dataset_rids.iter().cloned())
        .collect();
    {
        let mut tx = pool.begin().await?;
        crate::domain::build_events::enqueue(
            &mut tx,
            crate::domain::build_events::BuildEvent::Started,
            args.build_id,
            serde_json::json!({}),
        )
        .await;
        crate::domain::lineage_events::enqueue(
            &mut tx,
            outbox::lineage_event::LineageEventType::Start,
            args.build_id,
            &build_row.pipeline_rid,
            &started_outputs,
            Utc::now(),
        )
        .await;
        tx.commit().await?;
    }

    let (state_tx, _state_rx) = watch::channel(JobStateEvent {
        job_id: Uuid::nil(),
        job_spec_rid: String::new(),
        state: JobState::Waiting,
        stale_skipped: false,
        failure_reason: None,
        at: Utc::now(),
    });
    let state_tx = Arc::new(state_tx);

    let plan = load_plan(pool, args.build_id, &args.job_specs).await?;

    let outcome = orchestrate(
        pool,
        &plan,
        abort_policy,
        build_row.force_build,
        build_row.pipeline_rid.clone(),
        build_row.build_branch.clone(),
        args.parallelism.max(1),
        args.runner,
        args.output_client,
        args.job_inputs,
        state_tx,
    )
    .await?;

    let final_state = compute_final_state(pool, args.build_id).await?;
    sqlx::query(
        "UPDATE builds SET state = $1, finished_at = NOW() WHERE id = $2",
    )
    .bind(final_state.as_str())
    .bind(args.build_id)
    .execute(pool)
    .await?;
    metrics::record_build_state(final_state);
    {
        let mut tx = pool.begin().await?;
        let event = match final_state {
            BuildState::Completed => crate::domain::build_events::BuildEvent::Completed,
            BuildState::Failed => crate::domain::build_events::BuildEvent::Failed,
            BuildState::Aborted => crate::domain::build_events::BuildEvent::Aborted,
            _ => crate::domain::build_events::BuildEvent::Completed, // defensive
        };
        let lineage_event_type = match final_state {
            BuildState::Completed => outbox::lineage_event::LineageEventType::Complete,
            BuildState::Failed => outbox::lineage_event::LineageEventType::Fail,
            BuildState::Aborted => outbox::lineage_event::LineageEventType::Abort,
            _ => outbox::lineage_event::LineageEventType::Complete,
        };
        crate::domain::build_events::enqueue(
            &mut tx,
            event,
            args.build_id,
            serde_json::json!({
                "completed": outcome.completed,
                "failed": outcome.failed,
                "aborted": outcome.aborted,
                "stale_skipped": outcome.stale_skipped,
            }),
        )
        .await;
        crate::domain::lineage_events::enqueue(
            &mut tx,
            lineage_event_type,
            args.build_id,
            &build_row.pipeline_rid,
            &started_outputs,
            Utc::now(),
        )
        .await;
        tx.commit().await?;
    }

    // build_duration_seconds histogram — sample once per build.
    if let Ok(row) =
        sqlx::query_scalar::<_, Option<f64>>(
            "SELECT EXTRACT(EPOCH FROM (NOW() - COALESCE(started_at, queued_at, created_at))) FROM builds WHERE id = $1",
        )
        .bind(args.build_id)
        .fetch_one(pool)
        .await
    {
        if let Some(seconds) = row {
            metrics::record_build_duration(final_state, seconds);
        }
    }

    drop(timer);

    Ok(ExecuteBuildOutcome {
        final_state,
        completed: outcome.completed,
        failed: outcome.failed,
        aborted: outcome.aborted,
        stale_skipped: outcome.stale_skipped,
    })
}

// ---------------------------------------------------------------------------
// Internals — plan, orchestrate, cascade
// ---------------------------------------------------------------------------

#[derive(Debug, Clone)]
struct BuildRowMin {
    pipeline_rid: String,
    build_branch: String,
    abort_policy: AbortPolicy,
    force_build: bool,
}

async fn load_build(pool: &PgPool, build_id: Uuid) -> Result<BuildRowMin, BuildExecutionError> {
    let row: Option<(String, String, String, bool)> = sqlx::query_as(
        "SELECT pipeline_rid, build_branch, abort_policy, force_build FROM builds WHERE id = $1",
    )
    .bind(build_id)
    .fetch_optional(pool)
    .await?;
    let (pipeline_rid, build_branch, abort_policy, force_build) =
        row.ok_or(BuildExecutionError::BuildNotFound(build_id))?;
    Ok(BuildRowMin {
        pipeline_rid,
        build_branch,
        abort_policy: abort_policy
            .parse::<AbortPolicy>()
            .unwrap_or(AbortPolicy::DependentOnly),
        force_build,
    })
}

#[derive(Debug, Clone)]
struct ExecutionPlan {
    /// `job_id → JobSpec.rid` (and back via `spec_to_job`).
    job_to_spec: HashMap<Uuid, String>,
    spec_to_job: HashMap<String, Uuid>,
    /// `job_id → list of jobs it directly depends on`.
    dependencies: HashMap<Uuid, Vec<Uuid>>,
    /// `job_id → list of jobs that directly depend on it`.
    dependents: HashMap<Uuid, Vec<Uuid>>,
    specs: HashMap<String, JobSpec>,
}

async fn load_plan(
    pool: &PgPool,
    build_id: Uuid,
    specs: &[JobSpec],
) -> Result<ExecutionPlan, BuildExecutionError> {
    let job_rows: Vec<(Uuid, String)> = sqlx::query_as(
        "SELECT id, job_spec_rid FROM jobs WHERE build_id = $1",
    )
    .bind(build_id)
    .fetch_all(pool)
    .await?;

    let mut job_to_spec = HashMap::new();
    let mut spec_to_job = HashMap::new();
    for (id, spec_rid) in job_rows {
        spec_to_job.insert(spec_rid.clone(), id);
        job_to_spec.insert(id, spec_rid);
    }

    let edge_rows: Vec<(Uuid, Uuid)> = sqlx::query_as(
        r#"SELECT jd.job_id, jd.depends_on_job_id
             FROM job_dependencies jd
             JOIN jobs j ON j.id = jd.job_id
            WHERE j.build_id = $1"#,
    )
    .bind(build_id)
    .fetch_all(pool)
    .await?;

    let mut dependencies: HashMap<Uuid, Vec<Uuid>> = HashMap::new();
    let mut dependents: HashMap<Uuid, Vec<Uuid>> = HashMap::new();
    for (job_id, depends_on) in edge_rows {
        dependencies.entry(job_id).or_default().push(depends_on);
        dependents.entry(depends_on).or_default().push(job_id);
    }

    let specs: HashMap<String, JobSpec> = specs
        .iter()
        .map(|s| (s.rid.clone(), s.clone()))
        .collect();

    Ok(ExecutionPlan {
        job_to_spec,
        spec_to_job,
        dependencies,
        dependents,
        specs,
    })
}

#[derive(Default, Debug)]
struct OrchestrationOutcome {
    completed: usize,
    failed: usize,
    aborted: usize,
    stale_skipped: usize,
}

#[allow(clippy::too_many_arguments)]
async fn orchestrate(
    pool: &PgPool,
    plan: &ExecutionPlan,
    abort_policy: AbortPolicy,
    force_build: bool,
    pipeline_rid: String,
    build_branch: String,
    parallelism: usize,
    runner: Arc<dyn JobRunner>,
    output_client: Arc<dyn OutputTransactionClient>,
    job_inputs: HashMap<String, Vec<ResolvedInputView>>,
    state_tx: Arc<watch::Sender<JobStateEvent>>,
) -> Result<OrchestrationOutcome, BuildExecutionError> {
    let semaphore = Arc::new(Semaphore::new(parallelism));
    let mut outcome = OrchestrationOutcome::default();

    // Working set of remaining jobs (those not yet terminal).
    let mut remaining: HashSet<Uuid> = plan.job_to_spec.keys().copied().collect();
    // Jobs already in a terminal state (COMPLETED|FAILED|ABORTED).
    let mut completed_jobs: HashSet<Uuid> = HashSet::new();
    let mut failed_jobs: HashSet<Uuid> = HashSet::new();
    let mut aborted_jobs: HashSet<Uuid> = HashSet::new();

    let mut tasks: JoinSet<(Uuid, JobOutcome, bool)> = JoinSet::new();
    let mut in_flight: HashSet<Uuid> = HashSet::new();

    loop {
        if remaining.is_empty() && tasks.is_empty() {
            break;
        }

        // Schedule every job whose dependencies are satisfied.
        let ready_now: Vec<Uuid> = remaining
            .iter()
            .copied()
            .filter(|j| !in_flight.contains(j))
            .filter(|j| {
                plan.dependencies
                    .get(j)
                    .map(|deps| deps.iter().all(|d| completed_jobs.contains(d)))
                    .unwrap_or(true)
            })
            .collect();

        // If nothing is in flight AND nothing is ready (because every
        // remaining job has a failed/aborted ancestor), we're stuck —
        // cascade should have caught this; emit a defensive break.
        if ready_now.is_empty() && tasks.is_empty() {
            for jid in remaining.drain() {
                aborted_jobs.insert(jid);
                outcome.aborted += 1;
            }
            break;
        }

        for job_id in ready_now {
            let permit = semaphore.clone().acquire_owned().await.unwrap();
            let spec_rid = plan.job_to_spec[&job_id].clone();
            let spec = plan.specs[&spec_rid].clone();
            let inputs = job_inputs.get(&spec_rid).cloned().unwrap_or_default();
            let runner = runner.clone();
            let output_client = output_client.clone();
            let state_tx = state_tx.clone();
            let pool = pool.clone();
            let pipeline_rid = pipeline_rid.clone();
            let build_branch = build_branch.clone();
            in_flight.insert(job_id);
            remaining.remove(&job_id);

            tasks.spawn(async move {
                let _permit = permit;
                let outcome = drive_single_job(
                    &pool,
                    &pipeline_rid,
                    &build_branch,
                    job_id,
                    spec,
                    inputs,
                    force_build,
                    runner.as_ref(),
                    output_client.as_ref(),
                    state_tx.as_ref(),
                )
                .await;
                outcome
            });
        }

        let Some(joined) = tasks.join_next().await else {
            // Spurious — all tasks finished concurrently with a zero
            // "ready_now". Loop and re-check.
            continue;
        };
        let (job_id, job_outcome, was_skipped) = match joined {
            Ok(value) => value,
            Err(err) => {
                tracing::error!(error = %err, "join error in build executor");
                continue;
            }
        };
        in_flight.remove(&job_id);

        let spec_rid_for_outbox = plan.job_to_spec.get(&job_id).cloned().unwrap_or_default();
        let kind_for_outbox = plan
            .specs
            .get(&spec_rid_for_outbox)
            .map(|s| s.logic_kind.clone())
            .unwrap_or_default();
        match (&job_outcome, was_skipped) {
            (JobOutcome::Completed { .. }, true) => {
                completed_jobs.insert(job_id);
                outcome.completed += 1;
                outcome.stale_skipped += 1;
                publish_state_event(state_tx.as_ref(), &plan, job_id, JobState::Completed, true, None);
                emit_job_state_changed(
                    pool,
                    job_id,
                    &spec_rid_for_outbox,
                    JobState::Completed,
                    true,
                    None,
                    &kind_for_outbox,
                )
                .await;
            }
            (JobOutcome::Completed { .. }, false) => {
                completed_jobs.insert(job_id);
                outcome.completed += 1;
                publish_state_event(state_tx.as_ref(), &plan, job_id, JobState::Completed, false, None);
                emit_job_state_changed(
                    pool,
                    job_id,
                    &spec_rid_for_outbox,
                    JobState::Completed,
                    false,
                    None,
                    &kind_for_outbox,
                )
                .await;
            }
            (JobOutcome::Failed { reason }, _) => {
                failed_jobs.insert(job_id);
                outcome.failed += 1;
                publish_state_event(
                    state_tx.as_ref(),
                    &plan,
                    job_id,
                    JobState::Failed,
                    false,
                    Some(reason.clone()),
                );
                emit_job_state_changed(
                    pool,
                    job_id,
                    &spec_rid_for_outbox,
                    JobState::Failed,
                    false,
                    Some(reason.clone()),
                    &kind_for_outbox,
                )
                .await;

                // Failure cascade.
                let cascade = compute_cascade(plan, job_id, abort_policy, &completed_jobs);
                for cancel in cascade.dependents {
                    if remaining.contains(&cancel) || in_flight.contains(&cancel) {
                        cascade_abort_job(pool, cancel, "dependency failed", state_tx.as_ref(), plan)
                            .await?;
                        remaining.remove(&cancel);
                        aborted_jobs.insert(cancel);
                        outcome.aborted += 1;
                        metrics::record_failure_cascade(abort_policy, "dependent");
                    }
                }
                for cancel in cascade.independents {
                    if remaining.contains(&cancel) {
                        cascade_abort_job(
                            pool,
                            cancel,
                            "abort_policy=ALL_NON_DEPENDENT triggered by upstream failure",
                            state_tx.as_ref(),
                            plan,
                        )
                        .await?;
                        remaining.remove(&cancel);
                        aborted_jobs.insert(cancel);
                        outcome.aborted += 1;
                        metrics::record_failure_cascade(abort_policy, "independent");
                    }
                }
            }
        }
    }

    Ok(outcome)
}

#[allow(clippy::too_many_arguments)]
async fn drive_single_job(
    pool: &PgPool,
    pipeline_rid: &str,
    build_branch: &str,
    job_id: Uuid,
    spec: JobSpec,
    inputs: Vec<ResolvedInputView>,
    force_build: bool,
    runner: &dyn JobRunner,
    output_client: &dyn OutputTransactionClient,
    state_tx: &watch::Sender<JobStateEvent>,
) -> (Uuid, JobOutcome, bool) {
    // Persist the input signature on the job row regardless of
    // outcome — staleness compares against it on subsequent builds.
    let input_signature = staleness::input_signature(&inputs);
    let _ = sqlx::query(
        "UPDATE jobs SET input_signature = $1 WHERE id = $2",
    )
    .bind(&input_signature)
    .bind(job_id)
    .execute(pool)
    .await;

    // Step 1 — staleness short-circuit. If `force_build = true` we
    // always execute (Foundry doc § Staleness).
    if !force_build {
        match staleness::is_fresh(pool, pipeline_rid, build_branch, &spec, &inputs).await {
            Ok(StalenessOutcome::Fresh {
                previous_output_content_hash,
                ..
            }) => {
                if let Err(err) = mark_stale_skipped(
                    pool,
                    job_id,
                    previous_output_content_hash.clone(),
                    output_client,
                )
                .await
                {
                    tracing::warn!(error = %err, "failed to mark job stale_skipped, falling through");
                } else {
                    metrics::record_job_skipped();
                    publish_state_event_internal(
                        state_tx,
                        job_id,
                        spec.rid.clone(),
                        JobState::Completed,
                        true,
                        None,
                    );
                    return (
                        job_id,
                        JobOutcome::Completed {
                            output_content_hash: previous_output_content_hash
                                .unwrap_or_else(|| spec.content_hash.clone()),
                        },
                        true,
                    );
                }
            }
            Ok(_) => {}
            Err(err) => {
                tracing::warn!(error = %err, "staleness check failed, executing as fallback");
            }
        }
    }

    // Step 2 — drive the canonical lifecycle: WAITING → RUN_PENDING
    // → RUNNING. Errors at this stage abort the job before the
    // runner sees it.
    if let Err(err) = transition(pool, job_id, Some(JobState::Waiting), JobState::RunPending, "dispatching").await {
        tracing::error!(error = %err, %job_id, "WAITING → RUN_PENDING failed");
        return (
            job_id,
            JobOutcome::Failed {
                reason: err.to_string(),
            },
            false,
        );
    }
    publish_state_event_internal(state_tx, job_id, spec.rid.clone(), JobState::RunPending, false, None);

    if let Err(err) = transition(pool, job_id, Some(JobState::RunPending), JobState::Running, "running").await {
        tracing::error!(error = %err, %job_id, "RUN_PENDING → RUNNING failed");
        return (
            job_id,
            JobOutcome::Failed {
                reason: err.to_string(),
            },
            false,
        );
    }
    publish_state_event_internal(state_tx, job_id, spec.rid.clone(), JobState::Running, false, None);

    let ctx = JobContext {
        build_id: Uuid::nil(),
        build_branch: build_branch.to_string(),
        job_id,
        job_spec: spec.clone(),
        resolved_inputs: inputs,
        force_build,
        log_sink: None,
    };
    let outcome = runner.run(&ctx).await;

    match &outcome {
        JobOutcome::Completed { output_content_hash } => {
            // Multi-output atomicity: commit each output transaction;
            // if any commit fails, abort the rest.
            let outputs = sqlx::query_as::<_, (String, String)>(
                "SELECT output_dataset_rid, transaction_rid FROM job_outputs WHERE job_id = $1",
            )
            .bind(job_id)
            .fetch_all(pool)
            .await
            .unwrap_or_default();

            let mut commit_errors: Vec<String> = Vec::new();
            let mut committed: Vec<(String, String)> = Vec::new();
            for (rid, txn) in &outputs {
                match output_client.commit(rid, txn).await {
                    Ok(()) => committed.push((rid.clone(), txn.clone())),
                    Err(err) => {
                        commit_errors.push(format!("{rid}: {}", err.0));
                        break;
                    }
                }
            }

            if !commit_errors.is_empty() {
                // Roll back already-committed outputs by aborting the
                // remaining ones; the caller is responsible for any
                // compensation on the committed ones (Foundry has no
                // distributed-rollback primitive).
                for (rid, txn) in &outputs {
                    if !committed.iter().any(|(r, _)| r == rid) {
                        let _ = output_client.abort(rid, txn).await;
                    }
                }
                let _ = sqlx::query(
                    "UPDATE job_outputs SET aborted = TRUE WHERE job_id = $1 AND committed = FALSE",
                )
                .bind(job_id)
                .execute(pool)
                .await;
                let reason = format!("multi-output commit failed: {}", commit_errors.join("; "));
                let _ = transition(pool, job_id, Some(JobState::Running), JobState::Failed, &reason).await;
                let _ = sqlx::query("UPDATE jobs SET failure_reason = $1 WHERE id = $2")
                    .bind(&reason)
                    .bind(job_id)
                    .execute(pool)
                    .await;
                publish_state_event_internal(state_tx, job_id, spec.rid.clone(), JobState::Failed, false, Some(reason.clone()));
                return (job_id, JobOutcome::Failed { reason }, false);
            }

            // All outputs committed — flip rows + the job state.
            let _ = sqlx::query(
                "UPDATE job_outputs SET committed = TRUE WHERE job_id = $1",
            )
            .bind(job_id)
            .execute(pool)
            .await;
            let _ = sqlx::query(
                "UPDATE jobs SET output_content_hash = $1 WHERE id = $2",
            )
            .bind(output_content_hash)
            .bind(job_id)
            .execute(pool)
            .await;
            let _ = transition(
                pool,
                job_id,
                Some(JobState::Running),
                JobState::Completed,
                "all outputs committed",
            )
            .await;
            publish_state_event_internal(state_tx, job_id, spec.rid.clone(), JobState::Completed, false, None);
            (job_id, outcome, false)
        }
        JobOutcome::Failed { reason } => {
            // Abort every open output transaction (multi-output atomicity).
            let outputs = sqlx::query_as::<_, (String, String)>(
                "SELECT output_dataset_rid, transaction_rid FROM job_outputs WHERE job_id = $1",
            )
            .bind(job_id)
            .fetch_all(pool)
            .await
            .unwrap_or_default();
            for (rid, txn) in &outputs {
                let _ = output_client.abort(rid, txn).await;
            }
            let _ = sqlx::query(
                "UPDATE job_outputs SET aborted = TRUE WHERE job_id = $1 AND committed = FALSE",
            )
            .bind(job_id)
            .execute(pool)
            .await;
            let _ = sqlx::query("UPDATE jobs SET failure_reason = $1 WHERE id = $2")
                .bind(reason)
                .bind(job_id)
                .execute(pool)
                .await;
            let _ = transition(pool, job_id, Some(JobState::Running), JobState::Failed, reason).await;
            publish_state_event_internal(state_tx, job_id, spec.rid.clone(), JobState::Failed, false, Some(reason.clone()));
            (job_id, outcome, false)
        }
    }
}

async fn mark_stale_skipped(
    pool: &PgPool,
    job_id: Uuid,
    previous_output_content_hash: Option<String>,
    output_client: &dyn OutputTransactionClient,
) -> Result<(), BuildExecutionError> {
    // Foundry: fresh outputs are NOT recomputed and the underlying
    // dataset views do not change. We abort the (still-open) output
    // transactions because nothing was written to them.
    let outputs = sqlx::query_as::<_, (String, String)>(
        "SELECT output_dataset_rid, transaction_rid FROM job_outputs WHERE job_id = $1",
    )
    .bind(job_id)
    .fetch_all(pool)
    .await?;
    for (rid, txn) in &outputs {
        let _ = output_client.abort(rid, txn).await;
    }
    sqlx::query(
        r#"UPDATE job_outputs
              SET aborted = TRUE
            WHERE job_id = $1 AND committed = FALSE"#,
    )
    .bind(job_id)
    .execute(pool)
    .await?;

    sqlx::query(
        r#"UPDATE jobs
              SET stale_skipped = TRUE,
                  output_content_hash = COALESCE($1, output_content_hash)
            WHERE id = $2"#,
    )
    .bind(previous_output_content_hash)
    .bind(job_id)
    .execute(pool)
    .await?;

    let mut tx = pool.begin().await?;
    transition_job_in_tx(&mut tx, job_id, Some(JobState::Waiting), JobState::RunPending, Some("stale-skip dispatch"))
        .await?;
    transition_job_in_tx(&mut tx, job_id, Some(JobState::RunPending), JobState::Running, Some("stale-skip"))
        .await?;
    transition_job_in_tx(
        &mut tx,
        job_id,
        Some(JobState::Running),
        JobState::Completed,
        Some("stale_skipped: inputs + logic unchanged since last build"),
    )
    .await?;
    tx.commit().await?;
    Ok(())
}

async fn transition(
    pool: &PgPool,
    job_id: Uuid,
    expected: Option<JobState>,
    to: JobState,
    reason: &str,
) -> Result<(), BuildExecutionError> {
    let mut tx = pool.begin().await?;
    transition_job_in_tx(&mut tx, job_id, expected, to, Some(reason)).await?;
    tx.commit().await?;
    Ok(())
}

#[derive(Debug, Default)]
struct CascadePlan {
    dependents: Vec<Uuid>,
    independents: Vec<Uuid>,
}

fn compute_cascade(
    plan: &ExecutionPlan,
    failed: Uuid,
    policy: AbortPolicy,
    completed: &HashSet<Uuid>,
) -> CascadePlan {
    let mut cascade = CascadePlan::default();

    // Transitive dependents of `failed`.
    let mut stack = vec![failed];
    let mut visited: HashSet<Uuid> = HashSet::new();
    while let Some(node) = stack.pop() {
        if let Some(children) = plan.dependents.get(&node) {
            for &child in children {
                if visited.insert(child) {
                    cascade.dependents.push(child);
                    stack.push(child);
                }
            }
        }
    }

    if matches!(policy, AbortPolicy::AllNonDependent) {
        // Every remaining job not in the dependent set and not yet
        // completed becomes an independent victim.
        let dependent_set: HashSet<Uuid> = cascade.dependents.iter().copied().collect();
        for &job in plan.job_to_spec.keys() {
            if job == failed
                || dependent_set.contains(&job)
                || completed.contains(&job)
            {
                continue;
            }
            cascade.independents.push(job);
        }
    }

    cascade
}

async fn cascade_abort_job(
    pool: &PgPool,
    job_id: Uuid,
    reason: &str,
    state_tx: &watch::Sender<JobStateEvent>,
    plan: &ExecutionPlan,
) -> Result<(), BuildExecutionError> {
    // We don't know which state the job is in (could be WAITING or
    // RUN_PENDING/RUNNING because parallel scheduling). Pull the row
    // and pick the right transition. Foundry: WAITING jumps directly
    // to ABORTED; RUNNING / RUN_PENDING go through ABORT_PENDING.
    let current: Option<(String,)> =
        sqlx::query_as("SELECT state FROM jobs WHERE id = $1 FOR UPDATE")
            .bind(job_id)
            .fetch_optional(pool)
            .await?;
    let Some((current_str,)) = current else {
        return Ok(());
    };
    let from: JobState = match current_str.parse() {
        Ok(s) => s,
        Err(_) => return Ok(()),
    };
    if from.is_terminal() {
        return Ok(());
    }
    let mut tx = pool.begin().await?;
    match from {
        JobState::Waiting => {
            transition_job_in_tx(&mut tx, job_id, Some(JobState::Waiting), JobState::Aborted, Some(reason))
                .await?;
        }
        JobState::RunPending | JobState::Running => {
            transition_job_in_tx(&mut tx, job_id, Some(from), JobState::AbortPending, Some(reason))
                .await?;
            transition_job_in_tx(
                &mut tx,
                job_id,
                Some(JobState::AbortPending),
                JobState::Aborted,
                Some(reason),
            )
            .await?;
        }
        _ => {}
    }
    sqlx::query("UPDATE jobs SET failure_reason = $1 WHERE id = $2")
        .bind(reason)
        .bind(job_id)
        .execute(&mut *tx)
        .await?;
    sqlx::query(
        "UPDATE job_outputs SET aborted = TRUE WHERE job_id = $1 AND committed = FALSE",
    )
    .bind(job_id)
    .execute(&mut *tx)
    .await?;
    tx.commit().await?;
    let spec_rid = plan.job_to_spec.get(&job_id).cloned().unwrap_or_default();
    publish_state_event_internal(state_tx, job_id, spec_rid, JobState::Aborted, false, Some(reason.to_string()));
    Ok(())
}

async fn compute_final_state(pool: &PgPool, build_id: Uuid) -> Result<BuildState, BuildExecutionError> {
    let counts: (i64, i64, i64, i64) = sqlx::query_as(
        r#"SELECT
              COUNT(*) FILTER (WHERE state = 'COMPLETED') AS completed,
              COUNT(*) FILTER (WHERE state = 'FAILED') AS failed,
              COUNT(*) FILTER (WHERE state = 'ABORTED') AS aborted,
              COUNT(*) AS total
            FROM jobs WHERE build_id = $1"#,
    )
    .bind(build_id)
    .fetch_one(pool)
    .await?;
    let (completed, failed, _aborted, total) = counts;
    if completed == total {
        Ok(BuildState::Completed)
    } else if failed > 0 {
        Ok(BuildState::Failed)
    } else {
        Ok(BuildState::Aborted)
    }
}

fn publish_state_event(
    state_tx: &watch::Sender<JobStateEvent>,
    plan: &ExecutionPlan,
    job_id: Uuid,
    state: JobState,
    stale_skipped: bool,
    failure_reason: Option<String>,
) {
    let spec_rid = plan.job_to_spec.get(&job_id).cloned().unwrap_or_default();
    publish_state_event_internal(state_tx, job_id, spec_rid, state, stale_skipped, failure_reason);
}

fn publish_state_event_internal(
    state_tx: &watch::Sender<JobStateEvent>,
    job_id: Uuid,
    job_spec_rid: String,
    state: JobState,
    stale_skipped: bool,
    failure_reason: Option<String>,
) {
    let _ = state_tx.send(JobStateEvent {
        job_id,
        job_spec_rid,
        state,
        stale_skipped,
        failure_reason,
        at: Utc::now(),
    });
}

// Avoid unused-import warnings when the executor is reachable from
// callers that do not pull in the `Duration` constant.
#[allow(dead_code)]
const _BUILD_EXECUTOR_PLACEHOLDER: Duration = Duration::from_millis(0);

/// Outbox + metrics shim — emit `build.job_state_changed` for the
/// terminal state, plus tick the per-kind counters.
async fn emit_job_state_changed(
    pool: &PgPool,
    job_id: Uuid,
    spec_rid: &str,
    state: JobState,
    stale_skipped: bool,
    failure_reason: Option<String>,
    logic_kind: &str,
) {
    metrics::record_job_terminal(state, logic_kind);
    let Ok(mut tx) = pool.begin().await else {
        return;
    };
    // Pull the build_id so the outbox `aggregate_id` is the build,
    // not the job.
    let build_id: Option<(Uuid,)> =
        sqlx::query_as("SELECT build_id FROM jobs WHERE id = $1")
            .bind(job_id)
            .fetch_optional(&mut *tx)
            .await
            .ok()
            .flatten();
    let Some((build_id,)) = build_id else {
        let _ = tx.commit().await;
        return;
    };
    crate::domain::build_events::enqueue(
        &mut tx,
        crate::domain::build_events::BuildEvent::JobStateChanged,
        build_id,
        serde_json::json!({
            "job_id": job_id,
            "job_spec_rid": spec_rid,
            "state": state.as_str(),
            "stale_skipped": stale_skipped,
            "failure_reason": failure_reason,
            "logic_kind": logic_kind,
        }),
    )
    .await;
    let _ = tx.commit().await;
}

// ---------------------------------------------------------------------------
// Pure-function tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::build_resolution::{InputSpec, JobSpec};

    fn spec(rid: &str) -> JobSpec {
        JobSpec {
            rid: rid.to_string(),
            pipeline_rid: "p".into(),
            branch_name: "master".into(),
            inputs: vec![],
            output_dataset_rids: vec![format!("d-{rid}")],
            logic_kind: "TRANSFORM".into(),
            logic_payload: serde_json::Value::Null,
            content_hash: format!("h-{rid}"),
        }
    }

    fn _input_spec(d: &str) -> InputSpec {
        InputSpec {
            dataset_rid: d.into(),
            fallback_chain: vec!["master".into()],
            view_filter: vec![],
            require_fresh: false,
        }
    }

    fn linear_plan() -> ExecutionPlan {
        // a → b → c
        let a = Uuid::new_v4();
        let b = Uuid::new_v4();
        let c = Uuid::new_v4();
        let mut deps = HashMap::new();
        deps.insert(b, vec![a]);
        deps.insert(c, vec![b]);
        let mut dependents = HashMap::new();
        dependents.insert(a, vec![b]);
        dependents.insert(b, vec![c]);
        let mut job_to_spec = HashMap::new();
        job_to_spec.insert(a, "a".into());
        job_to_spec.insert(b, "b".into());
        job_to_spec.insert(c, "c".into());
        let mut spec_to_job = HashMap::new();
        spec_to_job.insert("a".into(), a);
        spec_to_job.insert("b".into(), b);
        spec_to_job.insert("c".into(), c);
        let mut specs = HashMap::new();
        specs.insert("a".into(), spec("a"));
        specs.insert("b".into(), spec("b"));
        specs.insert("c".into(), spec("c"));
        ExecutionPlan {
            job_to_spec,
            spec_to_job,
            dependencies: deps,
            dependents,
            specs,
        }
    }

    #[test]
    fn cascade_dependent_only_walks_transitive_dependents() {
        let plan = linear_plan();
        let a = plan.spec_to_job["a"];
        let b = plan.spec_to_job["b"];
        let c = plan.spec_to_job["c"];
        let cascade = compute_cascade(&plan, a, AbortPolicy::DependentOnly, &HashSet::new());
        let mut sorted = cascade.dependents.clone();
        sorted.sort();
        let mut want = vec![b, c];
        want.sort();
        assert_eq!(sorted, want);
        assert!(cascade.independents.is_empty(), "DEPENDENT_ONLY does not touch independents");
    }

    #[test]
    fn cascade_all_non_dependent_includes_unrelated_jobs() {
        // Plan: a → b ; c (independent)
        let mut plan = linear_plan();
        let c = plan.spec_to_job.remove("c").unwrap();
        plan.dependencies.remove(&c);
        plan.dependents.remove(&plan.spec_to_job["b"]);

        // Add a brand-new independent job d.
        let d = Uuid::new_v4();
        plan.job_to_spec.insert(d, "d".into());
        plan.spec_to_job.insert("d".into(), d);
        plan.specs.insert("d".into(), spec("d"));

        let a = plan.spec_to_job["a"];
        let b = plan.spec_to_job["b"];
        let cascade =
            compute_cascade(&plan, a, AbortPolicy::AllNonDependent, &HashSet::new());
        assert!(cascade.dependents.contains(&b));
        assert!(cascade.independents.contains(&d), "ALL_NON_DEPENDENT pulls in d");
    }

    #[test]
    fn cascade_all_non_dependent_skips_already_completed_jobs() {
        // Same shape as above but `d` is already COMPLETED — Foundry
        // doc: "previously completed jobs may still have written
        // data to their output datasets."
        let mut plan = linear_plan();
        let _ = plan.spec_to_job.remove("c");
        let d = Uuid::new_v4();
        plan.job_to_spec.insert(d, "d".into());
        plan.spec_to_job.insert("d".into(), d);
        let a = plan.spec_to_job["a"];

        let mut completed = HashSet::new();
        completed.insert(d);
        let cascade =
            compute_cascade(&plan, a, AbortPolicy::AllNonDependent, &completed);
        assert!(!cascade.independents.contains(&d));
    }
}
