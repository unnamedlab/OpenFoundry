//! # Temporal workflow client facade
//!
//! Substrate for **Stream S2.2.a** of the Cassandra/Foundry-parity
//! migration plan
//! (`docs/architecture/migration-plan-cassandra-foundry-parity.md`).
//!
//! ## Why this crate is a trait, not a re-export
//!
//! The official Rust Temporal SDK
//! (`temporalio/sdk-core` → `temporal-client`/`temporal-sdk-core`)
//! is still in pre-release as of 2026-05. ADR-0021 declares **Go SDK
//! for workers** — this means Rust services act exclusively as
//! **clients** that start workflows, send signals, query state, and
//! manage schedules. The pre-release client is good enough for that
//! narrow surface, but pinning the workspace to a moving target is
//! reckless.
//!
//! Following the same pattern S1 used for storage
//! (`Arc<dyn ObjectStore>`), this crate exposes a **domain-typed
//! trait** [`WorkflowClient`] that handlers consume through
//! `Arc<dyn WorkflowClient>`. The gRPC-backed implementation lands
//! behind the `grpc` feature flag (S2.2.a follow-up PR) once
//! upstream cuts a stable `0.4`. Until then, services can wire
//! [`NoopWorkflowClient`] (tests) or [`LoggingWorkflowClient`]
//! (local dev) without their handler code changing.
//!
//! ## Domain wrappers
//!
//! Generic `start_workflow(workflow_type, input)` is too loose for
//! real services. [`WorkflowAutomationClient`],
//! [`PipelineScheduleClient`], [`ApprovalsClient`] and
//! [`AutomationOpsClient`] sit on top of [`WorkflowClient`] and
//! expose method signatures keyed to each business domain. Each one
//! is a thin newtype over `Arc<dyn WorkflowClient>` with strongly
//! typed `Input` / `Result` JSON payloads — the trait stays small.

use std::{collections::BTreeMap, fmt, sync::Arc, time::Duration};

use async_trait::async_trait;
use serde::{Deserialize, Serialize};
use thiserror::Error;
use uuid::Uuid;

// ── Identifiers ──────────────────────────────────────────────────

/// Temporal namespace. By convention OpenFoundry uses one namespace
/// per tenant tier (`openfoundry-default`, `openfoundry-shared`,
/// per-customer namespaces in dedicated deployments).
#[derive(Debug, Clone, Eq, PartialEq, Hash, Serialize, Deserialize)]
pub struct Namespace(pub String);

impl Namespace {
    pub fn new(s: impl Into<String>) -> Self {
        Self(s.into())
    }
}

impl fmt::Display for Namespace {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

/// Stable identifier for a workflow execution. Maps to Temporal's
/// `workflow_id` (must be unique within a namespace + open
/// executions).
#[derive(Debug, Clone, Eq, PartialEq, Hash, Serialize, Deserialize)]
pub struct WorkflowId(pub String);

impl fmt::Display for WorkflowId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

/// `run_id` returned by Temporal on `StartWorkflowExecution`.
#[derive(Debug, Clone, Eq, PartialEq, Hash, Serialize, Deserialize)]
pub struct RunId(pub String);

impl fmt::Display for RunId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

/// Task queue name. Pinned per worker deployment.
#[derive(Debug, Clone, Eq, PartialEq, Hash, Serialize, Deserialize)]
pub struct TaskQueue(pub String);

impl fmt::Display for TaskQueue {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

/// Workflow type name as registered by the Go worker.
#[derive(Debug, Clone, Eq, PartialEq, Hash, Serialize, Deserialize)]
pub struct WorkflowType(pub String);

impl fmt::Display for WorkflowType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

// ── Inputs / outputs ─────────────────────────────────────────────

/// Knobs accepted by [`WorkflowClient::start_workflow`]. We expose
/// only the fields OpenFoundry actually uses — Temporal's full
/// `StartWorkflowExecutionRequest` has many more options that we
/// do not need at this layer.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StartWorkflowOptions {
    pub namespace: Namespace,
    pub workflow_id: WorkflowId,
    pub workflow_type: WorkflowType,
    pub task_queue: TaskQueue,
    /// JSON payload. Encoded as a single argument — Go workers
    /// deserialise into a struct via the standard JSON data
    /// converter.
    pub input: serde_json::Value,
    /// Hard ceiling for the entire workflow (including retries).
    /// Maps to `WorkflowExecutionTimeout`.
    pub execution_timeout: Option<Duration>,
    /// Single attempt timeout. Maps to `WorkflowRunTimeout`.
    pub run_timeout: Option<Duration>,
    /// Per-task ceiling (decision task). Maps to
    /// `WorkflowTaskTimeout`. Default is fine for almost everything.
    pub task_timeout: Option<Duration>,
    /// Conflict policy on duplicate `workflow_id`. Defaults to
    /// `RejectDuplicate` server-side; we always pass an explicit
    /// value so the policy is greppable in code.
    pub id_reuse_policy: IdReusePolicy,
    /// Headers / search attributes propagated to the workflow.
    /// `audit_correlation_id` is mandatory in OpenFoundry.
    pub search_attributes: BTreeMap<String, serde_json::Value>,
}

impl StartWorkflowOptions {
    /// Audit correlation key required by ADR-0019 (audit trail).
    pub const SEARCH_ATTR_AUDIT_CORRELATION: &'static str = "audit_correlation_id";

    /// Build a new options struct with the audit correlation header
    /// pre-populated. Callers must pass a UUID v7 obtained from the
    /// inbound request span; if you don't have one, generate it now
    /// — never let the workflow run without it.
    pub fn new(
        namespace: Namespace,
        workflow_id: WorkflowId,
        workflow_type: WorkflowType,
        task_queue: TaskQueue,
        input: serde_json::Value,
        audit_correlation_id: Uuid,
    ) -> Self {
        let mut search_attributes = BTreeMap::new();
        search_attributes.insert(
            Self::SEARCH_ATTR_AUDIT_CORRELATION.to_string(),
            serde_json::Value::String(audit_correlation_id.to_string()),
        );
        Self {
            namespace,
            workflow_id,
            workflow_type,
            task_queue,
            input,
            execution_timeout: None,
            run_timeout: None,
            task_timeout: None,
            id_reuse_policy: IdReusePolicy::AllowDuplicateFailedOnly,
            search_attributes,
        }
    }
}

#[derive(Debug, Clone, Copy, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum IdReusePolicy {
    AllowDuplicate,
    AllowDuplicateFailedOnly,
    RejectDuplicate,
    TerminateIfRunning,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WorkflowHandle {
    pub workflow_id: WorkflowId,
    pub run_id: RunId,
}

// ── Schedules ────────────────────────────────────────────────────

/// Replacement for the in-process tick loop killed by
/// [`crate::PipelineScheduleClient`] (S2.4.a). Mirrors the subset of
/// Temporal's `ScheduleSpec` that we actually use.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScheduleSpec {
    pub schedule_id: String,
    /// Cron expressions evaluated in `timezone`. Multiple expressions
    /// are unioned (any match triggers).
    pub cron_expressions: Vec<String>,
    /// IANA timezone, e.g. `Europe/Madrid`. Defaults to `UTC` when
    /// `None`.
    pub timezone: Option<String>,
    pub start_workflow: StartWorkflowOptions,
    /// Whether overlapping runs are allowed when a tick fires while a
    /// previous run is still alive. Default `Skip` matches the old
    /// pipeline-schedule-service semantics.
    pub overlap_policy: ScheduleOverlapPolicy,
    /// Catchup window for missed ticks (e.g. after a Temporal cluster
    /// outage). `Duration::ZERO` disables catchup.
    pub catchup_window: Duration,
}

#[derive(Debug, Clone, Copy, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ScheduleOverlapPolicy {
    /// Skip the new run if previous still alive.
    Skip,
    /// Buffer one new run; drop subsequent.
    BufferOne,
    /// Buffer all new runs.
    BufferAll,
    /// Cancel the previous run and start the new one.
    CancelOther,
    /// Terminate the previous run and start the new one.
    TerminateOther,
    /// Allow concurrent runs.
    AllowAll,
}

// ── Errors ───────────────────────────────────────────────────────

#[derive(Debug, Error)]
pub enum WorkflowClientError {
    #[error("workflow {workflow_id} already running in namespace {namespace}")]
    AlreadyStarted {
        namespace: Namespace,
        workflow_id: WorkflowId,
    },
    #[error("workflow {workflow_id} not found in namespace {namespace}")]
    NotFound {
        namespace: Namespace,
        workflow_id: WorkflowId,
    },
    #[error("schedule {schedule_id} not found")]
    ScheduleNotFound { schedule_id: String },
    #[error("invalid argument: {0}")]
    Invalid(String),
    #[error("unavailable: {0}")]
    Unavailable(String),
    #[error("internal: {0}")]
    Internal(String),
}

pub type Result<T> = std::result::Result<T, WorkflowClientError>;

// ── Trait ────────────────────────────────────────────────────────

/// The minimal surface OpenFoundry Rust services need from Temporal.
///
/// **Not on this trait by design**: long-running workflow execution
/// from Rust, polling, replay, or any worker-side primitive. Those
/// belong in the Go workers (ADR-0021).
#[async_trait]
pub trait WorkflowClient: Send + Sync + 'static {
    /// Start a new workflow execution. Idempotent against the
    /// `workflow_id` according to [`IdReusePolicy`].
    async fn start_workflow(&self, options: StartWorkflowOptions) -> Result<WorkflowHandle>;

    /// Send a signal to a running workflow. Used by approvals
    /// (S2.5).
    async fn signal_workflow(
        &self,
        namespace: &Namespace,
        workflow_id: &WorkflowId,
        run_id: Option<&RunId>,
        signal_name: &str,
        input: serde_json::Value,
    ) -> Result<()>;

    /// Synchronous query against a running workflow.
    async fn query_workflow(
        &self,
        namespace: &Namespace,
        workflow_id: &WorkflowId,
        run_id: Option<&RunId>,
        query_type: &str,
        input: serde_json::Value,
    ) -> Result<serde_json::Value>;

    /// Cancel a running workflow (graceful — the workflow can run
    /// cleanup activities).
    async fn cancel_workflow(
        &self,
        namespace: &Namespace,
        workflow_id: &WorkflowId,
        run_id: Option<&RunId>,
        reason: &str,
    ) -> Result<()>;

    /// Terminate a running workflow (hard — no cleanup).
    async fn terminate_workflow(
        &self,
        namespace: &Namespace,
        workflow_id: &WorkflowId,
        run_id: Option<&RunId>,
        reason: &str,
    ) -> Result<()>;

    /// Create a Temporal Schedule (S2.4).
    async fn create_schedule(&self, namespace: &Namespace, spec: ScheduleSpec) -> Result<()>;

    /// Pause a schedule without deleting it.
    async fn pause_schedule(
        &self,
        namespace: &Namespace,
        schedule_id: &str,
        note: &str,
    ) -> Result<()>;

    /// Delete a schedule. Idempotent: deleting a non-existent
    /// schedule returns Ok(()).
    async fn delete_schedule(&self, namespace: &Namespace, schedule_id: &str) -> Result<()>;
}

// ── In-process implementations ───────────────────────────────────

/// Dummy implementation that returns a deterministic
/// [`WorkflowHandle`] without contacting Temporal. Useful for unit
/// tests of handlers that wire `Arc<dyn WorkflowClient>`.
#[derive(Debug, Default, Clone)]
pub struct NoopWorkflowClient;

#[async_trait]
impl WorkflowClient for NoopWorkflowClient {
    async fn start_workflow(&self, options: StartWorkflowOptions) -> Result<WorkflowHandle> {
        Ok(WorkflowHandle {
            workflow_id: options.workflow_id,
            run_id: RunId(format!("noop-{}", Uuid::now_v7())),
        })
    }

    async fn signal_workflow(
        &self,
        _ns: &Namespace,
        _id: &WorkflowId,
        _run: Option<&RunId>,
        _signal: &str,
        _input: serde_json::Value,
    ) -> Result<()> {
        Ok(())
    }

    async fn query_workflow(
        &self,
        _ns: &Namespace,
        _id: &WorkflowId,
        _run: Option<&RunId>,
        _query: &str,
        _input: serde_json::Value,
    ) -> Result<serde_json::Value> {
        Ok(serde_json::Value::Null)
    }

    async fn cancel_workflow(
        &self,
        _ns: &Namespace,
        _id: &WorkflowId,
        _run: Option<&RunId>,
        _reason: &str,
    ) -> Result<()> {
        Ok(())
    }

    async fn terminate_workflow(
        &self,
        _ns: &Namespace,
        _id: &WorkflowId,
        _run: Option<&RunId>,
        _reason: &str,
    ) -> Result<()> {
        Ok(())
    }

    async fn create_schedule(&self, _ns: &Namespace, _spec: ScheduleSpec) -> Result<()> {
        Ok(())
    }

    async fn pause_schedule(&self, _ns: &Namespace, _id: &str, _note: &str) -> Result<()> {
        Ok(())
    }

    async fn delete_schedule(&self, _ns: &Namespace, _id: &str) -> Result<()> {
        Ok(())
    }
}

/// Tracing-only implementation — every method emits a structured
/// `tracing` event at INFO and otherwise behaves like
/// [`NoopWorkflowClient`]. Wired in `values-dev.yaml` overlays before
/// the gRPC backend lands.
#[derive(Debug, Default, Clone)]
pub struct LoggingWorkflowClient;

#[async_trait]
impl WorkflowClient for LoggingWorkflowClient {
    async fn start_workflow(&self, options: StartWorkflowOptions) -> Result<WorkflowHandle> {
        tracing::info!(
            namespace = %options.namespace,
            workflow_id = %options.workflow_id.0,
            workflow_type = %options.workflow_type.0,
            task_queue = %options.task_queue.0,
            "temporal.start_workflow (logging-only client)"
        );
        NoopWorkflowClient.start_workflow(options).await
    }

    async fn signal_workflow(
        &self,
        ns: &Namespace,
        id: &WorkflowId,
        run: Option<&RunId>,
        signal: &str,
        input: serde_json::Value,
    ) -> Result<()> {
        tracing::info!(
            namespace = %ns,
            workflow_id = %id.0,
            run_id = run.map(|r| r.0.as_str()).unwrap_or(""),
            signal = signal,
            "temporal.signal_workflow (logging-only client)"
        );
        NoopWorkflowClient
            .signal_workflow(ns, id, run, signal, input)
            .await
    }

    async fn query_workflow(
        &self,
        ns: &Namespace,
        id: &WorkflowId,
        run: Option<&RunId>,
        query: &str,
        input: serde_json::Value,
    ) -> Result<serde_json::Value> {
        tracing::info!(
            namespace = %ns,
            workflow_id = %id.0,
            run_id = run.map(|r| r.0.as_str()).unwrap_or(""),
            query = query,
            "temporal.query_workflow (logging-only client)"
        );
        NoopWorkflowClient
            .query_workflow(ns, id, run, query, input)
            .await
    }

    async fn cancel_workflow(
        &self,
        ns: &Namespace,
        id: &WorkflowId,
        run: Option<&RunId>,
        reason: &str,
    ) -> Result<()> {
        tracing::info!(
            namespace = %ns,
            workflow_id = %id.0,
            reason = reason,
            "temporal.cancel_workflow (logging-only client)"
        );
        NoopWorkflowClient
            .cancel_workflow(ns, id, run, reason)
            .await
    }

    async fn terminate_workflow(
        &self,
        ns: &Namespace,
        id: &WorkflowId,
        run: Option<&RunId>,
        reason: &str,
    ) -> Result<()> {
        tracing::info!(
            namespace = %ns,
            workflow_id = %id.0,
            reason = reason,
            "temporal.terminate_workflow (logging-only client)"
        );
        NoopWorkflowClient
            .terminate_workflow(ns, id, run, reason)
            .await
    }

    async fn create_schedule(&self, ns: &Namespace, spec: ScheduleSpec) -> Result<()> {
        tracing::info!(
            namespace = %ns,
            schedule_id = %spec.schedule_id,
            "temporal.create_schedule (logging-only client)"
        );
        NoopWorkflowClient.create_schedule(ns, spec).await
    }

    async fn pause_schedule(&self, ns: &Namespace, id: &str, note: &str) -> Result<()> {
        tracing::info!(
            namespace = %ns,
            schedule_id = id,
            note = note,
            "temporal.pause_schedule (logging-only client)"
        );
        NoopWorkflowClient.pause_schedule(ns, id, note).await
    }

    async fn delete_schedule(&self, ns: &Namespace, id: &str) -> Result<()> {
        tracing::info!(
            namespace = %ns,
            schedule_id = id,
            "temporal.delete_schedule (logging-only client)"
        );
        NoopWorkflowClient.delete_schedule(ns, id).await
    }
}

// ── Domain wrappers ──────────────────────────────────────────────

/// Default Temporal task queues. Pinned in code because workers and
/// clients must agree byte-for-byte; a typo at either side silently
/// wedges the workflow.
pub mod task_queues {
    pub const WORKFLOW_AUTOMATION: &str = "openfoundry.workflow-automation";
    pub const PIPELINE: &str = "openfoundry.pipeline";
    pub const APPROVALS: &str = "openfoundry.approvals";
    pub const AUTOMATION_OPS: &str = "openfoundry.automation-ops";
}

/// Workflow type names registered by the Go workers. Same agreement
/// rule as task queues: typo here = silent wedge.
pub mod workflow_types {
    pub const AUTOMATION_RUN: &str = "WorkflowAutomationRun";
    pub const PIPELINE_RUN: &str = "PipelineRun";
    pub const APPROVAL_REQUEST: &str = "ApprovalRequestWorkflow";
    pub const AUTOMATION_OPS_TASK: &str = "AutomationOpsTask";
}

/// Domain wrapper used by `services/workflow-automation-service`.
/// Hides the generic [`WorkflowClient`] surface behind a method set
/// keyed to the automation domain. Every method enforces the
/// workflow type + task queue pinning, so callers only choose the
/// `workflow_id` and the typed input.
#[derive(Clone)]
pub struct WorkflowAutomationClient {
    inner: Arc<dyn WorkflowClient>,
    namespace: Namespace,
}

impl WorkflowAutomationClient {
    pub fn new(inner: Arc<dyn WorkflowClient>, namespace: Namespace) -> Self {
        Self { inner, namespace }
    }

    pub async fn start_run(
        &self,
        run_id: Uuid,
        input: AutomationRunInput,
        audit_correlation_id: Uuid,
    ) -> Result<WorkflowHandle> {
        let options = StartWorkflowOptions::new(
            self.namespace.clone(),
            WorkflowId(format!("workflow-automation:{run_id}")),
            WorkflowType(workflow_types::AUTOMATION_RUN.to_string()),
            TaskQueue(task_queues::WORKFLOW_AUTOMATION.to_string()),
            serde_json::to_value(input).map_err(|e| WorkflowClientError::Invalid(e.to_string()))?,
            audit_correlation_id,
        );
        self.inner.start_workflow(options).await
    }

    pub async fn cancel_run(&self, run_id: Uuid, reason: &str) -> Result<()> {
        let workflow_id = WorkflowId(format!("workflow-automation:{run_id}"));
        self.inner
            .cancel_workflow(&self.namespace, &workflow_id, None, reason)
            .await
    }
}

/// Per-run input expected by the Go workflow registered under
/// [`workflow_types::AUTOMATION_RUN`]. Kept narrow on purpose — the
/// shape is part of the cross-language contract and changes here
/// **must** be mirrored in `workers-go/workflow-automation/`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AutomationRunInput {
    pub run_id: Uuid,
    pub definition_id: Uuid,
    pub tenant_id: String,
    pub triggered_by: String,
    pub trigger_payload: serde_json::Value,
}

/// Domain wrapper used by `services/pipeline-schedule-service`
/// (S2.4).
#[derive(Clone)]
pub struct PipelineScheduleClient {
    inner: Arc<dyn WorkflowClient>,
    namespace: Namespace,
}

impl PipelineScheduleClient {
    pub fn new(inner: Arc<dyn WorkflowClient>, namespace: Namespace) -> Self {
        Self { inner, namespace }
    }

    pub async fn create(
        &self,
        schedule_id: String,
        cron_expressions: Vec<String>,
        timezone: Option<String>,
        run_input: PipelineRunInput,
        audit_correlation_id: Uuid,
    ) -> Result<()> {
        let workflow_id = WorkflowId(format!("pipeline-run:scheduled:{schedule_id}"));
        let start = StartWorkflowOptions::new(
            self.namespace.clone(),
            workflow_id,
            WorkflowType(workflow_types::PIPELINE_RUN.to_string()),
            TaskQueue(task_queues::PIPELINE.to_string()),
            serde_json::to_value(run_input)
                .map_err(|e| WorkflowClientError::Invalid(e.to_string()))?,
            audit_correlation_id,
        );
        let spec = ScheduleSpec {
            schedule_id,
            cron_expressions,
            timezone,
            start_workflow: start,
            overlap_policy: ScheduleOverlapPolicy::Skip,
            catchup_window: Duration::from_secs(60 * 60),
        };
        self.inner.create_schedule(&self.namespace, spec).await
    }

    pub async fn delete(&self, schedule_id: &str) -> Result<()> {
        self.inner
            .delete_schedule(&self.namespace, schedule_id)
            .await
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PipelineRunInput {
    pub pipeline_id: Uuid,
    pub tenant_id: String,
    pub revision: Option<String>,
    pub parameters: serde_json::Value,
}

/// Domain wrapper used by `services/approvals-service` (S2.5).
#[derive(Clone)]
pub struct ApprovalsClient {
    inner: Arc<dyn WorkflowClient>,
    namespace: Namespace,
}

impl ApprovalsClient {
    pub const SIGNAL_DECIDE: &'static str = "decide";

    pub fn new(inner: Arc<dyn WorkflowClient>, namespace: Namespace) -> Self {
        Self { inner, namespace }
    }

    pub async fn open(
        &self,
        request_id: Uuid,
        input: ApprovalRequestInput,
        audit_correlation_id: Uuid,
    ) -> Result<WorkflowHandle> {
        let options = StartWorkflowOptions::new(
            self.namespace.clone(),
            WorkflowId(format!("approval:{request_id}")),
            WorkflowType(workflow_types::APPROVAL_REQUEST.to_string()),
            TaskQueue(task_queues::APPROVALS.to_string()),
            serde_json::to_value(input).map_err(|e| WorkflowClientError::Invalid(e.to_string()))?,
            audit_correlation_id,
        );
        self.inner.start_workflow(options).await
    }

    pub async fn decide(
        &self,
        request_id: Uuid,
        decision: ApprovalDecision,
    ) -> Result<()> {
        let workflow_id = WorkflowId(format!("approval:{request_id}"));
        self.inner
            .signal_workflow(
                &self.namespace,
                &workflow_id,
                None,
                Self::SIGNAL_DECIDE,
                serde_json::to_value(decision)
                    .map_err(|e| WorkflowClientError::Invalid(e.to_string()))?,
            )
            .await
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ApprovalRequestInput {
    pub request_id: Uuid,
    pub tenant_id: String,
    pub subject: String,
    pub approver_set: Vec<String>,
    pub action_payload: serde_json::Value,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "snake_case", tag = "outcome")]
pub enum ApprovalDecision {
    Approve {
        approver: String,
        comment: Option<String>,
    },
    Reject {
        approver: String,
        comment: Option<String>,
    },
}

/// Domain wrapper used by `services/automation-operations-service`
/// (S2.7).
#[derive(Clone)]
pub struct AutomationOpsClient {
    inner: Arc<dyn WorkflowClient>,
    namespace: Namespace,
}

impl AutomationOpsClient {
    pub fn new(inner: Arc<dyn WorkflowClient>, namespace: Namespace) -> Self {
        Self { inner, namespace }
    }

    pub async fn start_task(
        &self,
        task_id: Uuid,
        input: AutomationOpsInput,
        audit_correlation_id: Uuid,
    ) -> Result<WorkflowHandle> {
        let options = StartWorkflowOptions::new(
            self.namespace.clone(),
            WorkflowId(format!("automation-ops:{task_id}")),
            WorkflowType(workflow_types::AUTOMATION_OPS_TASK.to_string()),
            TaskQueue(task_queues::AUTOMATION_OPS.to_string()),
            serde_json::to_value(input).map_err(|e| WorkflowClientError::Invalid(e.to_string()))?,
            audit_correlation_id,
        );
        self.inner.start_workflow(options).await
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AutomationOpsInput {
    pub task_id: Uuid,
    pub tenant_id: String,
    pub task_type: String,
    pub payload: serde_json::Value,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn noop_start_returns_handle_with_workflow_id() {
        let client = NoopWorkflowClient;
        let opts = StartWorkflowOptions::new(
            Namespace::new("default"),
            WorkflowId("wf-1".into()),
            WorkflowType(workflow_types::AUTOMATION_RUN.into()),
            TaskQueue(task_queues::WORKFLOW_AUTOMATION.into()),
            serde_json::json!({}),
            Uuid::now_v7(),
        );
        let handle = client.start_workflow(opts).await.expect("noop must succeed");
        assert_eq!(handle.workflow_id.0, "wf-1");
        assert!(handle.run_id.0.starts_with("noop-"));
    }

    #[tokio::test]
    async fn workflow_automation_client_round_trip() {
        let client = WorkflowAutomationClient::new(
            Arc::new(NoopWorkflowClient),
            Namespace::new("default"),
        );
        let run_id = Uuid::now_v7();
        let handle = client
            .start_run(
                run_id,
                AutomationRunInput {
                    run_id,
                    definition_id: Uuid::now_v7(),
                    tenant_id: "acme".into(),
                    triggered_by: "tester".into(),
                    trigger_payload: serde_json::json!({}),
                },
                Uuid::now_v7(),
            )
            .await
            .expect("noop must succeed");
        assert_eq!(handle.workflow_id.0, format!("workflow-automation:{run_id}"));
    }

    #[test]
    fn search_attributes_include_audit_correlation() {
        let opts = StartWorkflowOptions::new(
            Namespace::new("default"),
            WorkflowId("wf-1".into()),
            WorkflowType("X".into()),
            TaskQueue("tq".into()),
            serde_json::json!({}),
            Uuid::now_v7(),
        );
        assert!(opts
            .search_attributes
            .contains_key(StartWorkflowOptions::SEARCH_ATTR_AUDIT_CORRELATION));
    }
}
