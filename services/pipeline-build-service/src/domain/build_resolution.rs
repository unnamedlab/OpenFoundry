//! Foundry "Build resolution" implementation.
//!
//! This module is the entry point for [`resolve_build`], the formal
//! lifecycle step that runs *before* any job executes:
//!
//!   a. Look up the [`JobSpec`] producing each declared output dataset
//!      (walking the `job_spec_fallback` chain when the build branch
//!      doesn't carry the spec).
//!   b. Build the inputs ↔ outputs graph between JobSpecs and detect
//!      cycles. A cycle fails the build per the doc: "Detects cycles in
//!      the specified input datasets and fails the build if there are
//!      cycles present."
//!   c. Resolve every input dataset's branch via
//!      [`super::branch_resolution::resolve_input_dataset`] and pull
//!      the schema for the resolved view.
//!   d. Acquire build locks: open a NEW transaction on each output
//!      dataset and insert a row into `build_input_locks`. The PRIMARY
//!      KEY on `output_dataset_rid` is the actual lock primitive — two
//!      concurrent builds racing for the same output collide on insert.
//!   e. Detect concurrent in-progress builds whose outputs feed our
//!      inputs; if any are running, transition the build to
//!      `BUILD_QUEUED` so the caller can retry resolution after the
//!      upstream finishes.
//!
//! The module is deliberately split into pure functions ([`detect_cycles`])
//! and IO-bound helpers ([`acquire_locks`], [`validate_inputs`]) so the
//! pure pieces can be unit-tested without a database.

use std::collections::{HashMap, HashSet, VecDeque};

use async_trait::async_trait;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::{PgPool, Postgres, Transaction};
use uuid::Uuid;

use crate::domain::branch_resolution::{ResolveError, resolve_input_dataset};
use crate::domain::metrics;
use crate::models::build::BuildState;
use core_models::dataset::transaction::BranchName;

fn audit(action: &str, build_id: Uuid, requested_by: &str, details: serde_json::Value) {
    tracing::info!(
        target: "audit",
        actor = requested_by,
        action = action,
        build_id = %build_id,
        details = %details,
        "pipeline-build-service lifecycle event"
    );
}

// ---------------------------------------------------------------------------
// Domain types
// ---------------------------------------------------------------------------

/// Input dataset declaration on a [`JobSpec`]. Mirrors `proto.InputSpec`.
///
/// `view_filter` carries optional view-time selectors per Foundry doc:
/// `AT_TIMESTAMP`, `AT_TRANSACTION`, `RANGE`, and the relative
/// `INCREMENTAL_SINCE_LAST_BUILD`. The resolver materialises each
/// entry into a [`crate::domain::runners::ResolvedViewFilter`] that
/// is persisted in `jobs.input_view_resolutions` so the runner can
/// replay the exact same window the orchestrator saw.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct InputSpec {
    pub dataset_rid: String,
    #[serde(default)]
    pub fallback_chain: Vec<String>,
    #[serde(default)]
    pub view_filter: Vec<crate::domain::runners::ViewFilter>,
    #[serde(default)]
    pub require_fresh: bool,
}

/// Declarative recipe loaded by [`JobSpecRepo::lookup`]. Mirrors
/// `proto.JobSpec`. The structure here is the minimum the resolver
/// needs; pipeline-authoring is the source of truth.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct JobSpec {
    pub rid: String,
    pub pipeline_rid: String,
    pub branch_name: String,
    pub inputs: Vec<InputSpec>,
    pub output_dataset_rids: Vec<String>,
    pub logic_kind: String,
    #[serde(default)]
    pub logic_payload: serde_json::Value,
    pub content_hash: String,
}

/// Snapshot of a dataset branch as seen by the versioning service.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BranchSnapshot {
    pub name: BranchName,
    pub head_transaction_rid: Option<String>,
}

/// Outcome of opening a transaction on an output dataset (one
/// transaction per output, all opened during resolution).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct OpenedTransaction {
    pub dataset_rid: String,
    pub transaction_rid: String,
}

/// Schema bundle pulled for an input dataset's resolved view.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ResolvedInputView {
    pub dataset_rid: String,
    pub branch: BranchName,
    pub schema: serde_json::Value,
}

/// Complete resolution outcome handed back to the caller.
#[derive(Debug, Clone)]
pub struct ResolvedBuild {
    pub build_id: Uuid,
    pub state: BuildState,
    pub job_specs: Vec<JobSpec>,
    pub input_views: Vec<ResolvedInputView>,
    pub opened_transactions: Vec<OpenedTransaction>,
    pub queued_reason: Option<String>,
    pub resolved_at: DateTime<Utc>,
}

#[derive(Debug, thiserror::Error)]
pub enum BuildResolutionError {
    #[error("missing JobSpec for output dataset {dataset_rid} (tried branches: {tried:?})")]
    MissingJobSpec {
        dataset_rid: String,
        tried: Vec<String>,
    },
    #[error("cycle detected in JobSpec graph: {}", .cycle_path.join(" → "))]
    CycleDetected { cycle_path: Vec<String> },
    #[error("input dataset {dataset_rid} not found")]
    InputNotFound { dataset_rid: String },
    #[error("input dataset {dataset_rid} has no branch matching build='{build_branch}' (chain: {chain:?})")]
    InputBranchMissing {
        dataset_rid: String,
        build_branch: String,
        chain: Vec<String>,
    },
    #[error("input {dataset_rid} resolved to fallback branch but require_fresh=true")]
    StaleInput { dataset_rid: String },
    #[error("output dataset {dataset_rid} already locked by build {holder_build_id}")]
    LockHeld {
        dataset_rid: String,
        holder_build_id: Uuid,
    },
    #[error("dataset versioning client error: {0}")]
    Client(String),
    #[error("invalid logic_kind on JobSpec {job_spec_rid}: {reason}")]
    InvalidLogicKind {
        job_spec_rid: String,
        reason: String,
    },
    #[error("view filter resolution failed for JobSpec {job_spec_rid}: {errors:?}")]
    ViewFilterResolution {
        job_spec_rid: String,
        errors: Vec<String>,
    },
    #[error(transparent)]
    Db(#[from] sqlx::Error),
}

// ---------------------------------------------------------------------------
// Client traits — mockable for tests.
// ---------------------------------------------------------------------------

#[derive(Debug, thiserror::Error)]
#[error("dataset client error: {0}")]
pub struct ClientError(pub String);

#[async_trait]
pub trait DatasetVersioningClient: Send + Sync {
    async fn list_branches(
        &self,
        dataset_rid: &str,
    ) -> Result<Vec<BranchSnapshot>, ClientError>;

    async fn open_transaction(
        &self,
        dataset_rid: &str,
        branch: &str,
    ) -> Result<String, ClientError>;

    async fn view_schema(
        &self,
        dataset_rid: &str,
        branch: &str,
    ) -> Result<serde_json::Value, ClientError>;
}

#[async_trait]
pub trait JobSpecRepo: Send + Sync {
    /// Look up the JobSpec that produces `output_dataset_rid` on the
    /// build branch. Implementations walk the fallback chain when the
    /// spec is not authored against the build branch directly.
    async fn lookup(
        &self,
        pipeline_rid: &str,
        output_dataset_rid: &str,
        build_branch: &str,
        fallback_chain: &[String],
    ) -> Result<Option<JobSpec>, ClientError>;
}

// ---------------------------------------------------------------------------
// Pure step: cycle detection.
// ---------------------------------------------------------------------------

/// Run a Kahn-style topological scan over the inputs ↔ outputs graph
/// between JobSpecs. Returns the cycle path (using JobSpec rids) when
/// one is detected; otherwise returns Ok.
pub fn detect_cycles(specs: &[JobSpec]) -> Result<(), BuildResolutionError> {
    // Build producer index: dataset_rid → JobSpec.rid that produces it.
    let mut producer: HashMap<&str, &str> = HashMap::new();
    for spec in specs {
        for output in &spec.output_dataset_rids {
            producer.insert(output.as_str(), spec.rid.as_str());
        }
    }

    // Adjacency: spec A → spec B when one of A's inputs is produced by
    // B (so B must run first; we orient edges A → B, then a cycle in
    // this graph is the same as a cycle in the data flow).
    let mut graph: HashMap<&str, Vec<&str>> = HashMap::new();
    let mut indegree: HashMap<&str, usize> = HashMap::new();
    for spec in specs {
        graph.entry(spec.rid.as_str()).or_default();
        indegree.entry(spec.rid.as_str()).or_insert(0);
    }
    for spec in specs {
        for input in &spec.inputs {
            if let Some(&upstream) = producer.get(input.dataset_rid.as_str()) {
                if upstream == spec.rid.as_str() {
                    // self-loop on the same JobSpec is a cycle of length 1.
                    return Err(BuildResolutionError::CycleDetected {
                        cycle_path: vec![spec.rid.clone(), spec.rid.clone()],
                    });
                }
                graph
                    .entry(spec.rid.as_str())
                    .or_default()
                    .push(upstream);
                *indegree.entry(upstream).or_insert(0) += 1;
            }
        }
    }

    let mut zero: VecDeque<&str> = indegree
        .iter()
        .filter_map(|(k, v)| if *v == 0 { Some(*k) } else { None })
        .collect();
    let mut popped = 0usize;
    while let Some(node) = zero.pop_front() {
        popped += 1;
        if let Some(neighbours) = graph.get(node) {
            for &next in neighbours {
                let entry = indegree.entry(next).or_insert(0);
                if *entry > 0 {
                    *entry -= 1;
                }
                if *entry == 0 {
                    zero.push_back(next);
                }
            }
        }
    }
    if popped == specs.len() {
        return Ok(());
    }

    // Cycle present: rebuild a representative path by walking
    // remaining nodes via DFS until we revisit one.
    let cycle = find_cycle_path(&graph, &indegree)
        .unwrap_or_else(|| specs.iter().map(|s| s.rid.clone()).collect());
    Err(BuildResolutionError::CycleDetected { cycle_path: cycle })
}

fn find_cycle_path(
    graph: &HashMap<&str, Vec<&str>>,
    indegree: &HashMap<&str, usize>,
) -> Option<Vec<String>> {
    let mut visited: HashSet<&str> = HashSet::new();
    let mut stack: Vec<&str> = Vec::new();
    let start = indegree.iter().find_map(|(k, v)| if *v > 0 { Some(*k) } else { None })?;
    fn dfs<'a>(
        node: &'a str,
        graph: &'a HashMap<&'a str, Vec<&'a str>>,
        visited: &mut HashSet<&'a str>,
        stack: &mut Vec<&'a str>,
    ) -> Option<Vec<String>> {
        if let Some(pos) = stack.iter().position(|n| *n == node) {
            let mut cycle: Vec<String> = stack[pos..].iter().map(|s| s.to_string()).collect();
            cycle.push(node.to_string());
            return Some(cycle);
        }
        if !visited.insert(node) {
            return None;
        }
        stack.push(node);
        if let Some(next) = graph.get(node) {
            for &n in next {
                if let Some(found) = dfs(n, graph, visited, stack) {
                    return Some(found);
                }
            }
        }
        stack.pop();
        None
    }
    dfs(start, graph, &mut visited, &mut stack)
}

// ---------------------------------------------------------------------------
// Step a — load JobSpecs for declared outputs.
// ---------------------------------------------------------------------------

pub async fn load_job_specs(
    pipeline_rid: &str,
    build_branch: &str,
    fallback_chain: &[String],
    output_dataset_rids: &[String],
    repo: &dyn JobSpecRepo,
) -> Result<Vec<JobSpec>, BuildResolutionError> {
    let mut specs = Vec::with_capacity(output_dataset_rids.len());
    for output in output_dataset_rids {
        let spec = repo
            .lookup(pipeline_rid, output, build_branch, fallback_chain)
            .await
            .map_err(|e| BuildResolutionError::Client(e.0))?;
        match spec {
            Some(spec) => specs.push(spec),
            None => {
                let mut tried = vec![build_branch.to_string()];
                tried.extend(fallback_chain.iter().cloned());
                return Err(BuildResolutionError::MissingJobSpec {
                    dataset_rid: output.clone(),
                    tried,
                });
            }
        }
    }
    // De-duplicate: a single JobSpec may declare multiple outputs and
    // appear once per output. Dedup by rid.
    specs.sort_by(|a, b| a.rid.cmp(&b.rid));
    specs.dedup_by(|a, b| a.rid == b.rid);
    Ok(specs)
}

// ---------------------------------------------------------------------------
// Step c — validate inputs (existence, branch, schema).
// ---------------------------------------------------------------------------

pub async fn validate_inputs(
    build_branch: &BranchName,
    specs: &[JobSpec],
    client: &dyn DatasetVersioningClient,
) -> Result<Vec<ResolvedInputView>, BuildResolutionError> {
    // Inputs produced by another spec in this build are virtual: they
    // are not yet in the versioning service. Skip them to avoid
    // false-positive InputNotFound errors.
    let producer_outputs: HashSet<&str> = specs
        .iter()
        .flat_map(|s| s.output_dataset_rids.iter().map(String::as_str))
        .collect();

    let mut resolved = Vec::new();
    let mut seen: HashSet<String> = HashSet::new();
    for spec in specs {
        for input in &spec.inputs {
            if producer_outputs.contains(input.dataset_rid.as_str()) {
                continue;
            }
            if !seen.insert(input.dataset_rid.clone()) {
                continue;
            }
            let snapshots = client
                .list_branches(&input.dataset_rid)
                .await
                .map_err(|e| BuildResolutionError::Client(e.0))?;
            if snapshots.is_empty() {
                return Err(BuildResolutionError::InputNotFound {
                    dataset_rid: input.dataset_rid.clone(),
                });
            }

            let chain: Vec<BranchName> = input
                .fallback_chain
                .iter()
                .filter_map(|s| s.parse::<BranchName>().ok())
                .collect();
            let branches: Vec<BranchName> =
                snapshots.iter().map(|s| s.name.clone()).collect();

            let outcome = resolve_input_dataset(build_branch, &chain, &branches).map_err(
                |err| match err {
                    ResolveError::NoMatch { .. } => {
                        BuildResolutionError::InputBranchMissing {
                            dataset_rid: input.dataset_rid.clone(),
                            build_branch: build_branch.as_str().to_string(),
                            chain: input.fallback_chain.clone(),
                        }
                    }
                    ResolveError::IncompatibleAncestry { .. } => {
                        // P2 — surface upgrades to the existing
                        // `InputBranchMissing` shape so callers don't have
                        // to widen their error matching mid-pipeline. The
                        // resolver-domain test
                        // `build_resolution_incompatible_ancestry_fails`
                        // hits the dedicated variant directly.
                        BuildResolutionError::InputBranchMissing {
                            dataset_rid: input.dataset_rid.clone(),
                            build_branch: build_branch.as_str().to_string(),
                            chain: input.fallback_chain.clone(),
                        }
                    }
                },
            )?;

            if input.require_fresh && outcome.fallback_index > 0 {
                return Err(BuildResolutionError::StaleInput {
                    dataset_rid: input.dataset_rid.clone(),
                });
            }

            let schema = client
                .view_schema(&input.dataset_rid, outcome.branch.as_str())
                .await
                .map_err(|e| BuildResolutionError::Client(e.0))?;

            resolved.push(ResolvedInputView {
                dataset_rid: input.dataset_rid.clone(),
                branch: outcome.branch,
                schema,
            });
        }
    }
    Ok(resolved)
}

// ---------------------------------------------------------------------------
// Step d — acquire build locks.
// ---------------------------------------------------------------------------

/// Open one transaction per output dataset and persist the lock row.
/// The PRIMARY KEY on `build_input_locks.output_dataset_rid` is what
/// enforces "one build per output" — concurrent insert attempts from
/// other builds bounce off the unique constraint and surface as
/// [`BuildResolutionError::LockHeld`].
pub async fn acquire_locks<'c>(
    tx: &mut Transaction<'c, Postgres>,
    build_id: Uuid,
    specs: &[JobSpec],
    build_branch: &str,
    client: &dyn DatasetVersioningClient,
) -> Result<Vec<OpenedTransaction>, BuildResolutionError> {
    let mut opened = Vec::new();
    for spec in specs {
        for output in &spec.output_dataset_rids {
            let txn_rid = client
                .open_transaction(output, build_branch)
                .await
                .map_err(|e| BuildResolutionError::Client(e.0))?;

            let inserted = sqlx::query(
                r#"INSERT INTO build_input_locks
                       (output_dataset_rid, build_id, transaction_rid)
                   VALUES ($1, $2, $3)
                   ON CONFLICT (output_dataset_rid) DO NOTHING"#,
            )
            .bind(output)
            .bind(build_id)
            .bind(&txn_rid)
            .execute(&mut **tx)
            .await?;

            if inserted.rows_affected() == 0 {
                let holder: Option<(Uuid,)> = sqlx::query_as(
                    "SELECT build_id FROM build_input_locks WHERE output_dataset_rid = $1",
                )
                .bind(output)
                .fetch_optional(&mut **tx)
                .await?;
                return Err(BuildResolutionError::LockHeld {
                    dataset_rid: output.clone(),
                    holder_build_id: holder.map(|h| h.0).unwrap_or(Uuid::nil()),
                });
            }

            opened.push(OpenedTransaction {
                dataset_rid: output.clone(),
                transaction_rid: txn_rid,
            });
        }
    }
    Ok(opened)
}

// ---------------------------------------------------------------------------
// Step e — concurrent build detection.
// ---------------------------------------------------------------------------

/// Returns `true` when any input dataset is the output of another
/// in-progress build. The caller queues itself when this happens.
pub async fn has_upstream_in_progress(
    pool: &PgPool,
    specs: &[JobSpec],
    self_build_id: Uuid,
) -> Result<bool, BuildResolutionError> {
    let mut input_rids: HashSet<String> = HashSet::new();
    let producer_outputs: HashSet<&str> = specs
        .iter()
        .flat_map(|s| s.output_dataset_rids.iter().map(String::as_str))
        .collect();
    for spec in specs {
        for input in &spec.inputs {
            if producer_outputs.contains(input.dataset_rid.as_str()) {
                continue;
            }
            input_rids.insert(input.dataset_rid.clone());
        }
    }
    if input_rids.is_empty() {
        return Ok(false);
    }
    let rids: Vec<String> = input_rids.into_iter().collect();

    let count: (i64,) = sqlx::query_as(
        r#"SELECT COUNT(*) FROM build_input_locks l
              JOIN builds b ON b.id = l.build_id
            WHERE l.output_dataset_rid = ANY($1)
              AND b.state IN ('BUILD_RESOLUTION','BUILD_QUEUED','BUILD_RUNNING','BUILD_ABORTING')
              AND b.id <> $2"#,
    )
    .bind(&rids)
    .bind(self_build_id)
    .fetch_one(pool)
    .await?;
    Ok(count.0 > 0)
}

// ---------------------------------------------------------------------------
// Top-level entry point.
// ---------------------------------------------------------------------------

pub struct ResolveBuildArgs<'a> {
    pub pipeline_rid: &'a str,
    pub build_branch: &'a BranchName,
    pub job_spec_fallback: &'a [String],
    pub output_dataset_rids: &'a [String],
    pub force_build: bool,
    pub requested_by: &'a str,
    pub trigger_kind: &'a str,
    /// `DEPENDENT_ONLY` (Foundry default) or `ALL_NON_DEPENDENT`. The
    /// resolver only persists it; the executor reads it from the
    /// `builds` row and applies it on first failure.
    pub abort_policy: &'a str,
}

/// Drive the full resolution sequence. Persists the new `builds` row,
/// the per-output `jobs` rows, the dependency edges, and the lock
/// rows; returns a [`ResolvedBuild`] describing the outcome.
///
/// The `BuildState` returned is one of:
///   * `BUILD_RESOLUTION` → all locks acquired, jobs created in WAITING
///     (caller will flip to `BUILD_RUNNING` when the executor picks it up).
///   * `BUILD_QUEUED` → an upstream build is producing one of our inputs;
///     locks are released and the caller should retry later.
pub async fn resolve_build(
    pool: &PgPool,
    args: ResolveBuildArgs<'_>,
    job_specs: &dyn JobSpecRepo,
    versioning: &dyn DatasetVersioningClient,
) -> Result<ResolvedBuild, BuildResolutionError> {
    let resolution_started = Utc::now();
    let resolve_timer = metrics::BUILD_RESOLUTION_DURATION_SECONDS.start_timer();

    // Step a — JobSpec lookup.
    let specs = match load_job_specs(
        args.pipeline_rid,
        args.build_branch.as_str(),
        args.job_spec_fallback,
        args.output_dataset_rids,
        job_specs,
    )
    .await
    {
        Ok(specs) => specs,
        Err(err) => {
            audit(
                "build.resolution_failed",
                Uuid::nil(),
                args.requested_by,
                serde_json::json!({
                    "pipeline_rid": args.pipeline_rid,
                    "stage": "load_job_specs",
                    "error": err.to_string(),
                }),
            );
            return Err(err);
        }
    };

    // Step a.5 — per-logic_kind validation. Foundry doc enumerates
    // five kinds (Sync / Transform / HealthCheck / Analytical /
    // Export) with kind-specific arity rules; reject mismatches
    // before locks are acquired.
    for spec in &specs {
        if let Err(reason) = crate::domain::runners::validate_logic_kind(
            &spec.logic_kind,
            spec.output_dataset_rids.len(),
        ) {
            let err = BuildResolutionError::InvalidLogicKind {
                job_spec_rid: spec.rid.clone(),
                reason,
            };
            audit(
                "build.resolution_failed",
                Uuid::nil(),
                args.requested_by,
                serde_json::json!({
                    "pipeline_rid": args.pipeline_rid,
                    "stage": "validate_logic_kind",
                    "error": err.to_string(),
                }),
            );
            return Err(err);
        }
    }

    // Step b — cycle detection.
    if let Err(err) = detect_cycles(&specs) {
        audit(
            "build.resolution_failed",
            Uuid::nil(),
            args.requested_by,
            serde_json::json!({
                "pipeline_rid": args.pipeline_rid,
                "stage": "detect_cycles",
                "error": err.to_string(),
            }),
        );
        return Err(err);
    }

    // Step c — validate inputs and pull schemas.
    let input_views = match validate_inputs(args.build_branch, &specs, versioning).await {
        Ok(v) => v,
        Err(err) => {
            audit(
                "build.resolution_failed",
                Uuid::nil(),
                args.requested_by,
                serde_json::json!({
                    "pipeline_rid": args.pipeline_rid,
                    "stage": "validate_inputs",
                    "error": err.to_string(),
                }),
            );
            return Err(err);
        }
    };

    // Insert the build row in BUILD_RESOLUTION up-front so step e can
    // check against it (and so concurrent submissions see us via
    // `build_input_locks`).
    let build_id = Uuid::now_v7();
    let mut tx = pool.begin().await?;
    sqlx::query(
        r#"INSERT INTO builds (
              id, pipeline_rid, build_branch, job_spec_fallback,
              state, trigger_kind, force_build, abort_policy,
              queued_at, requested_by
           ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)"#,
    )
    .bind(build_id)
    .bind(args.pipeline_rid)
    .bind(args.build_branch.as_str())
    .bind(args.job_spec_fallback)
    .bind(BuildState::Resolution.as_str())
    .bind(args.trigger_kind)
    .bind(args.force_build)
    .bind(args.abort_policy)
    .bind(Utc::now())
    .bind(args.requested_by)
    .execute(&mut *tx)
    .await?;

    // Step e — bail out into BUILD_QUEUED before grabbing locks if an
    // upstream build is still producing one of our inputs. We commit
    // the QUEUED state so the caller can poll.
    if has_upstream_in_progress(pool, &specs, build_id).await? {
        sqlx::query(
            "UPDATE builds SET state = $1 WHERE id = $2",
        )
        .bind(BuildState::Queued.as_str())
        .bind(build_id)
        .execute(&mut *tx)
        .await?;
        crate::domain::build_events::enqueue(
            &mut tx,
            crate::domain::build_events::BuildEvent::Queued,
            build_id,
            serde_json::json!({
                "pipeline_rid": args.pipeline_rid,
                "build_branch": args.build_branch.as_str(),
                "reason": "upstream build in progress",
            }),
        )
        .await;
        tx.commit().await?;
        metrics::record_build_state(BuildState::Queued);
        audit(
            "build.queued",
            build_id,
            args.requested_by,
            serde_json::json!({
                "pipeline_rid": args.pipeline_rid,
                "build_branch": args.build_branch.as_str(),
                "reason": "upstream build in progress",
            }),
        );
        drop(resolve_timer);
        return Ok(ResolvedBuild {
            build_id,
            state: BuildState::Queued,
            job_specs: specs,
            input_views,
            opened_transactions: vec![],
            queued_reason: Some("upstream build in progress".to_string()),
            resolved_at: resolution_started,
        });
    }

    // Step d — acquire locks (open output transactions + persist).
    let lock_timer = metrics::BUILD_LOCK_ACQUISITION_DURATION_SECONDS.start_timer();
    let opened = acquire_locks(&mut tx, build_id, &specs, args.build_branch.as_str(), versioning)
        .await?;
    drop(lock_timer);

    // Persist one Job row per JobSpec in WAITING. The dependency edges
    // are computed from the inputs↔outputs graph between specs.
    let mut spec_to_job: HashMap<&str, Uuid> = HashMap::new();
    for spec in &specs {
        // Resolve `InputSpec.view_filter` selectors into concrete
        // (view_id | transaction_rid | range) tuples and persist
        // them on `jobs.input_view_resolutions`. INCREMENTAL_SINCE_LAST_BUILD
        // queries the previous COMPLETED job for the lower bound;
        // the runner reads HEAD of the resolved branch as the upper
        // bound at execution time.
        let view_outcome = crate::domain::runners::resolve_view_filters(
            pool,
            args.pipeline_rid,
            args.build_branch.as_str(),
            spec,
            &input_views,
        )
        .await;
        if !view_outcome.errors.is_empty() {
            // No locks were acquired before this point in the per-spec
            // loop (locks happen in step d above) — but jobs from
            // earlier iterations of this loop already exist in the open
            // transaction, so the rollback on `tx.commit() not called`
            // takes care of cleanup.
            return Err(BuildResolutionError::ViewFilterResolution {
                job_spec_rid: spec.rid.clone(),
                errors: view_outcome.errors,
            });
        }
        let view_resolutions_json =
            serde_json::to_value(&view_outcome.resolutions).unwrap_or_else(|_| serde_json::json!([]));

        let job_id = Uuid::now_v7();
        spec_to_job.insert(spec.rid.as_str(), job_id);
        let txn_rids: Vec<String> = opened
            .iter()
            .filter(|o| spec.output_dataset_rids.contains(&o.dataset_rid))
            .map(|o| o.transaction_rid.clone())
            .collect();
        sqlx::query(
            r#"INSERT INTO jobs (
                  id, build_id, job_spec_rid, state,
                  output_transaction_rids,
                  canonical_logic_hash,
                  input_view_resolutions
               ) VALUES ($1,$2,$3,$4,$5,$6,$7)"#,
        )
        .bind(job_id)
        .bind(build_id)
        .bind(&spec.rid)
        .bind("WAITING")
        .bind(&txn_rids)
        .bind(&spec.content_hash)
        .bind(&view_resolutions_json)
        .execute(&mut *tx)
        .await?;
        sqlx::query(
            r#"INSERT INTO job_state_transitions (job_id, from_state, to_state, reason)
               VALUES ($1, NULL, 'WAITING', 'created during build resolution')"#,
        )
        .bind(job_id)
        .execute(&mut *tx)
        .await?;

        // Multi-output atomicity: one `job_outputs` row per declared
        // output, paired with the transaction we opened for it. The
        // executor flips `committed = TRUE` only after every output
        // has been written; partial commits are detectable via the
        // PRIMARY KEY (job_id, output_dataset_rid).
        for output in &spec.output_dataset_rids {
            if let Some(opened_txn) =
                opened.iter().find(|o| &o.dataset_rid == output)
            {
                sqlx::query(
                    r#"INSERT INTO job_outputs
                          (job_id, output_dataset_rid, transaction_rid, committed)
                       VALUES ($1, $2, $3, FALSE)
                       ON CONFLICT DO NOTHING"#,
                )
                .bind(job_id)
                .bind(output)
                .bind(&opened_txn.transaction_rid)
                .execute(&mut *tx)
                .await?;
            }
        }
    }

    let producer: HashMap<&str, &str> = specs
        .iter()
        .flat_map(|s| {
            s.output_dataset_rids
                .iter()
                .map(move |o| (o.as_str(), s.rid.as_str()))
        })
        .collect();
    for spec in &specs {
        for input in &spec.inputs {
            if let Some(&upstream_rid) = producer.get(input.dataset_rid.as_str()) {
                let dependent = spec_to_job[spec.rid.as_str()];
                let depends_on = spec_to_job[upstream_rid];
                sqlx::query(
                    r#"INSERT INTO job_dependencies (job_id, depends_on_job_id)
                       VALUES ($1, $2)
                       ON CONFLICT DO NOTHING"#,
                )
                .bind(dependent)
                .bind(depends_on)
                .execute(&mut *tx)
                .await?;
            }
        }
    }

    crate::domain::build_events::enqueue(
        &mut tx,
        crate::domain::build_events::BuildEvent::Created,
        build_id,
        serde_json::json!({
            "pipeline_rid": args.pipeline_rid,
            "build_branch": args.build_branch.as_str(),
            "trigger_kind": args.trigger_kind,
            "force_build": args.force_build,
            "job_count": specs.len(),
            "output_count": opened.len(),
        }),
    )
    .await;
    tx.commit().await?;
    metrics::record_build_state(BuildState::Resolution);
    audit(
        "build.resolved",
        build_id,
        args.requested_by,
        serde_json::json!({
            "pipeline_rid": args.pipeline_rid,
            "build_branch": args.build_branch.as_str(),
            "force_build": args.force_build,
            "job_count": specs.len(),
            "output_count": opened.len(),
        }),
    );
    drop(resolve_timer);
    Ok(ResolvedBuild {
        build_id,
        state: BuildState::Resolution,
        job_specs: specs,
        input_views,
        opened_transactions: opened,
        queued_reason: None,
        resolved_at: resolution_started,
    })
}

// ---------------------------------------------------------------------------
// Pure-function tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    fn spec(rid: &str, inputs: Vec<&str>, outputs: Vec<&str>) -> JobSpec {
        JobSpec {
            rid: rid.to_string(),
            pipeline_rid: "ri.foundry.main.pipeline.test".to_string(),
            branch_name: "master".to_string(),
            inputs: inputs
                .into_iter()
                .map(|d| InputSpec {
                    dataset_rid: d.to_string(),
                    fallback_chain: vec!["master".into()],
                    view_filter: vec![],
                    require_fresh: false,
                })
                .collect(),
            output_dataset_rids: outputs.into_iter().map(String::from).collect(),
            logic_kind: "TRANSFORM".to_string(),
            logic_payload: serde_json::Value::Null,
            content_hash: format!("hash-{rid}"),
        }
    }

    #[test]
    fn detect_cycles_accepts_dag() {
        let specs = vec![
            spec("s1", vec!["raw.a"], vec!["mid.b"]),
            spec("s2", vec!["mid.b"], vec!["final.c"]),
        ];
        assert!(detect_cycles(&specs).is_ok());
    }

    #[test]
    fn detect_cycles_rejects_two_node_cycle() {
        let specs = vec![
            spec("s1", vec!["c"], vec!["a"]),
            spec("s2", vec!["a"], vec!["c"]),
        ];
        let err = detect_cycles(&specs).expect_err("must reject");
        match err {
            BuildResolutionError::CycleDetected { cycle_path } => {
                assert!(cycle_path.len() >= 2, "{cycle_path:?}");
            }
            other => panic!("unexpected error: {other:?}"),
        }
    }

    #[test]
    fn detect_cycles_rejects_self_loop() {
        let specs = vec![spec("s1", vec!["x"], vec!["x"])];
        assert!(matches!(
            detect_cycles(&specs),
            Err(BuildResolutionError::CycleDetected { .. })
        ));
    }
}
