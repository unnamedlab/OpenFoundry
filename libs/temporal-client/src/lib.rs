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
//! manage schedules. We keep that surface behind this trait so the
//! rest of the workspace is insulated from SDK churn.
//!
//! Following the same pattern S1 used for storage
//! (`Arc<dyn ObjectStore>`), this crate exposes a **domain-typed
//! trait** [`WorkflowClient`] that handlers consume through
//! `Arc<dyn WorkflowClient>`. The gRPC-backed implementation is
//! [`GrpcWorkflowClient`] behind the `grpc` feature flag; services
//! obtain it with [`runtime_workflow_client`] when `TEMPORAL_HOST_PORT`
//! is set. [`NoopWorkflowClient`] remains for unit tests and
//! [`LoggingWorkflowClient`] remains only for local dry-runs with no
//! Temporal frontend configured.
//!
//! ## Runtime environment
//!
//! `runtime_workflow_client("<service>")` reads these variables:
//!
//! * `TEMPORAL_HOST_PORT`: Temporal frontend target. Accepts
//!   `host:port` or a full `http://`/`https://` URL. When unset, the
//!   logging client is used for local dry-runs.
//! * `TEMPORAL_NAMESPACE`: namespace for domain wrappers; defaults to
//!   `default`.
//! * `TEMPORAL_IDENTITY`: client identity; defaults to the service
//!   name passed to `runtime_workflow_client`.
//! * `TEMPORAL_API_KEY`: optional bearer-style API key supported by
//!   the SDK connection options.
//! * `TEMPORAL_REQUIRE_REAL_CLIENT`: when true, startup fails if
//!   `TEMPORAL_HOST_PORT` is missing. Helm sets this for deployed
//!   S2 Rust services so staging/prod cannot silently fall back to
//!   the logging client.
//! * `TEMPORAL_TASK_QUEUE`: global task queue override, useful for
//!   isolated E2E namespaces.
//! * `TEMPORAL_TASK_QUEUE_WORKFLOW_AUTOMATION`,
//!   `TEMPORAL_TASK_QUEUE_PIPELINE`, `TEMPORAL_TASK_QUEUE_APPROVALS`,
//!   `TEMPORAL_TASK_QUEUE_AUTOMATION_OPS`: domain-specific task queue
//!   overrides. These win over `TEMPORAL_TASK_QUEUE`.
//! * `TEMPORAL_WORKFLOW_EXECUTION_TIMEOUT_SECS`,
//!   `TEMPORAL_WORKFLOW_RUN_TIMEOUT_SECS`,
//!   `TEMPORAL_WORKFLOW_TASK_TIMEOUT_SECS`: optional default timeouts
//!   applied by the typed domain wrappers.
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
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use thiserror::Error;
use uuid::Uuid;

#[cfg(feature = "grpc")]
use std::collections::HashMap;
#[cfg(feature = "grpc")]
use temporalio_client::{
    Client as TemporalSdkClient, ClientOptions as TemporalSdkClientOptions,
    Connection as TemporalConnection, ConnectionOptions as TemporalConnectionOptions,
    NamespacedClient, UntypedQuery, UntypedSignal, UntypedWorkflow, WorkflowCancelOptions,
    WorkflowExecutionInfo as TemporalWorkflowExecutionInfo,
    WorkflowGetResultOptions as TemporalWorkflowGetResultOptions,
    WorkflowQueryOptions as TemporalWorkflowQueryOptions,
    WorkflowSignalOptions as TemporalWorkflowSignalOptions,
    WorkflowStartOptions as TemporalWorkflowStartOptions,
    WorkflowTerminateOptions as TemporalWorkflowTerminateOptions,
    grpc::WorkflowService as TemporalWorkflowService,
    tonic::{Code as TemporalCode, IntoRequest},
};
#[cfg(feature = "grpc")]
use temporalio_common::{
    data_converters::{
        GenericPayloadConverter, RawValue, SerializationContext, SerializationContextData,
    },
    protos::temporal::api::{
        common::v1::{Payload as TemporalPayload, Payloads as TemporalPayloads, SearchAttributes},
        enums::v1::{
            ScheduleOverlapPolicy as TemporalScheduleOverlapPolicy,
            WorkflowExecutionStatus as TemporalWorkflowExecutionStatus,
            WorkflowIdConflictPolicy as TemporalWorkflowIdConflictPolicy,
            WorkflowIdReusePolicy as TemporalWorkflowIdReusePolicy,
        },
        schedule::v1::{
            IntervalSpec as TemporalIntervalSpec, Schedule as TemporalSchedule,
            ScheduleAction as TemporalScheduleAction, SchedulePolicies as TemporalSchedulePolicies,
            ScheduleSpec as TemporalScheduleSpec, ScheduleState as TemporalScheduleState,
            schedule_action,
        },
        taskqueue::v1::TaskQueue as TemporalTaskQueue,
        workflow::v1::{
            NewWorkflowExecutionInfo, WorkflowExecutionInfo as TemporalWorkflowExecutionInfoProto,
        },
        workflowservice::v1::{
            CreateScheduleRequest, DescribeScheduleRequest, ListWorkflowExecutionsRequest,
        },
    },
};
#[cfg(feature = "grpc")]
use url::Url;

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

// ── Visibility (list/query) ──────────────────────────────────────

/// Lifecycle status of a workflow execution as reported by Temporal
/// visibility (`ListWorkflowExecutions`). Mirrors
/// `temporal.api.enums.v1.WorkflowExecutionStatus` but kept Rust-side
/// so the trait surface stays decoupled from the SDK proto.
#[derive(Debug, Clone, Copy, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum WorkflowExecutionStatus {
    Unknown,
    Running,
    Completed,
    Failed,
    Canceled,
    Terminated,
    ContinuedAsNew,
    TimedOut,
    Paused,
}

impl WorkflowExecutionStatus {
    /// Stable string projection used by REST consumers (matches the
    /// `status` field shape of the legacy `workflow_run_projections`
    /// table so callers don't have to special-case the cutover).
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Unknown => "unknown",
            Self::Running => "running",
            Self::Completed => "completed",
            Self::Failed => "failed",
            Self::Canceled => "canceled",
            Self::Terminated => "terminated",
            Self::ContinuedAsNew => "continued_as_new",
            Self::TimedOut => "timed_out",
            Self::Paused => "paused",
        }
    }

    pub fn is_closed(self) -> bool {
        !matches!(self, Self::Running | Self::Unknown | Self::Paused)
    }
}

/// Single execution row returned by [`WorkflowClient::list_workflows`].
/// Mirrors the subset of `WorkflowExecutionInfo` (Temporal proto)
/// OpenFoundry needs at the REST layer; deeper per-run state should
/// be fetched with [`WorkflowClient::query_workflow`].
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WorkflowExecutionSummary {
    pub workflow_id: WorkflowId,
    pub run_id: RunId,
    pub workflow_type: WorkflowType,
    pub task_queue: TaskQueue,
    pub status: WorkflowExecutionStatus,
    pub start_time: Option<DateTime<Utc>>,
    pub close_time: Option<DateTime<Utc>>,
    pub history_length: i64,
}

/// Paginated [`WorkflowExecutionSummary`] page. `next_page_token`
/// is opaque server state — pass it back verbatim to fetch the next
/// page; `None`/empty means no further results.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct WorkflowListPage {
    pub executions: Vec<WorkflowExecutionSummary>,
    pub next_page_token: Option<String>,
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
    /// Optional interval cadence. Used by high-volume E2E/load tests
    /// and by callers that need sub-minute schedules.
    pub interval: Option<Duration>,
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
    /// If set, Temporal stops taking scheduled actions after this many
    /// successful actions. `Some(1)` gives a one-shot schedule.
    pub max_actions: Option<i64>,
}

#[derive(Debug, Clone, Eq, PartialEq, Serialize, Deserialize)]
pub struct ScheduleDescription {
    pub schedule_id: String,
    pub action_count: i64,
    pub recent_action_count: usize,
    pub running_workflow_count: usize,
    pub remaining_actions: Option<i64>,
    pub recent_workflow_ids: Vec<String>,
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

#[derive(Debug, Error, Serialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
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

/// Runtime wiring for Rust services that need to talk to Temporal.
///
/// `TEMPORAL_HOST_PORT` is the switch:
/// - unset / empty => [`LoggingWorkflowClient`]
/// - set => real gRPC client (when compiled with the `grpc` feature)
/// - set without `grpc` => configuration error; E2E must not fall
///   back to logging
#[derive(Debug, Clone, Eq, PartialEq)]
pub struct RuntimeClientConfig {
    pub host_port: Option<String>,
    pub namespace: String,
    pub identity: String,
    pub api_key: Option<String>,
    pub require_real_client: bool,
    pub deployment_environment: Option<String>,
}

impl RuntimeClientConfig {
    pub fn from_env(identity: impl Into<String>) -> Self {
        Self {
            host_port: env_non_empty("TEMPORAL_HOST_PORT"),
            namespace: env_non_empty("TEMPORAL_NAMESPACE").unwrap_or_else(|| "default".to_string()),
            identity: env_non_empty("TEMPORAL_IDENTITY").unwrap_or_else(|| identity.into()),
            api_key: env_non_empty("TEMPORAL_API_KEY"),
            require_real_client: env_flag("TEMPORAL_REQUIRE_REAL_CLIENT"),
            deployment_environment: env_non_empty("OPENFOUNDRY_DEPLOYMENT_ENVIRONMENT"),
        }
    }

    pub fn uses_temporal(&self) -> bool {
        self.host_port.is_some()
    }

    pub fn requires_temporal(&self) -> bool {
        self.require_real_client
            || self
                .deployment_environment
                .as_deref()
                .is_some_and(environment_requires_temporal)
    }
}

/// Default timeouts applied by the typed domain wrappers before they
/// call [`WorkflowClient::start_workflow`].
#[derive(Debug, Clone, Copy, Default, Eq, PartialEq)]
pub struct RuntimeWorkflowOptions {
    pub execution_timeout: Option<Duration>,
    pub run_timeout: Option<Duration>,
    pub task_timeout: Option<Duration>,
}

impl RuntimeWorkflowOptions {
    pub fn from_env() -> Result<Self> {
        Self::from_lookup(|key| std::env::var(key).ok())
    }

    pub fn from_lookup<F>(lookup: F) -> Result<Self>
    where
        F: Fn(&str) -> Option<String>,
    {
        Ok(Self {
            execution_timeout: duration_secs_from_lookup(
                "TEMPORAL_WORKFLOW_EXECUTION_TIMEOUT_SECS",
                &lookup,
            )?,
            run_timeout: duration_secs_from_lookup("TEMPORAL_WORKFLOW_RUN_TIMEOUT_SECS", &lookup)?,
            task_timeout: duration_secs_from_lookup(
                "TEMPORAL_WORKFLOW_TASK_TIMEOUT_SECS",
                &lookup,
            )?,
        })
    }

    fn apply(self, options: &mut StartWorkflowOptions) {
        if self.execution_timeout.is_some() {
            options.execution_timeout = self.execution_timeout;
        }
        if self.run_timeout.is_some() {
            options.run_timeout = self.run_timeout;
        }
        if self.task_timeout.is_some() {
            options.task_timeout = self.task_timeout;
        }
    }
}

/// Build the runtime Temporal client for a Rust service.
pub async fn runtime_workflow_client(
    identity: impl Into<String>,
) -> Result<(Arc<dyn WorkflowClient>, Namespace)> {
    let cfg = RuntimeClientConfig::from_env(identity);
    let namespace = Namespace::new(cfg.namespace.clone());

    if let Some(host_port) = cfg.host_port.clone() {
        #[cfg(feature = "grpc")]
        {
            let client = GrpcWorkflowClient::connect(cfg).await?;
            tracing::info!(
                temporal_host_port = %host_port,
                temporal_namespace = %namespace,
                "temporal runtime client configured"
            );
            return Ok((Arc::new(client), namespace));
        }

        #[cfg(not(feature = "grpc"))]
        {
            return Err(WorkflowClientError::Invalid(format!(
                "TEMPORAL_HOST_PORT={host_port} requires the `grpc` feature"
            )));
        }
    }

    if cfg.requires_temporal() {
        return Err(WorkflowClientError::Invalid(
            "TEMPORAL_HOST_PORT is required when TEMPORAL_REQUIRE_REAL_CLIENT=true or \
             OPENFOUNDRY_DEPLOYMENT_ENVIRONMENT is staging/prod"
                .into(),
        ));
    }

    tracing::info!(
        temporal_namespace = %namespace,
        "Temporal frontend not configured; using logging workflow client for local dry-run"
    );
    Ok((Arc::new(LoggingWorkflowClient), namespace))
}

fn env_non_empty(key: &str) -> Option<String> {
    std::env::var(key)
        .ok()
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
}

fn env_flag(key: &str) -> bool {
    env_non_empty(key).is_some_and(|value| {
        matches!(
            value.to_ascii_lowercase().as_str(),
            "1" | "true" | "yes" | "on" | "required"
        )
    })
}

fn environment_requires_temporal(environment: &str) -> bool {
    matches!(
        environment.trim().to_ascii_lowercase().as_str(),
        "prod" | "production" | "staging" | "stage"
    )
}

fn task_queue_from_env(domain_key: &str, default: &str) -> TaskQueue {
    TaskQueue(
        env_non_empty(domain_key)
            .or_else(|| env_non_empty("TEMPORAL_TASK_QUEUE"))
            .unwrap_or_else(|| default.to_string()),
    )
}

fn runtime_workflow_options_from_env() -> RuntimeWorkflowOptions {
    match RuntimeWorkflowOptions::from_env() {
        Ok(options) => options,
        Err(error) => {
            tracing::warn!(
                %error,
                "invalid Temporal workflow timeout environment; using SDK defaults"
            );
            RuntimeWorkflowOptions::default()
        }
    }
}

#[cfg(test)]
fn task_queue_from_lookup<F>(domain_key: &str, default: &str, lookup: F) -> TaskQueue
where
    F: Fn(&str) -> Option<String>,
{
    TaskQueue(
        lookup(domain_key)
            .map(|value| value.trim().to_string())
            .filter(|value| !value.is_empty())
            .or_else(|| {
                lookup("TEMPORAL_TASK_QUEUE")
                    .map(|value| value.trim().to_string())
                    .filter(|value| !value.is_empty())
            })
            .unwrap_or_else(|| default.to_string()),
    )
}

fn duration_secs_from_lookup<F>(key: &str, lookup: &F) -> Result<Option<Duration>>
where
    F: Fn(&str) -> Option<String>,
{
    let Some(raw) = lookup(key) else {
        return Ok(None);
    };
    let value = raw.trim();
    if value.is_empty() {
        return Ok(None);
    }
    let seconds = value
        .parse::<u64>()
        .map_err(|error| WorkflowClientError::Invalid(format!("invalid {key}: {error}")))?;
    Ok(Some(Duration::from_secs(seconds)))
}

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

    /// Page through executions in a namespace using Temporal
    /// visibility (`ListWorkflowExecutions`). `query` is the SQL-like
    /// query string accepted by the visibility API (e.g.
    /// `WorkflowType = '...' AND WorkflowId STARTS_WITH '...'`).
    /// `page_size` of 0 yields the server default; `next_page_token`
    /// is the opaque cursor from the previous response or `None` for
    /// the first call.
    async fn list_workflows(
        &self,
        namespace: &Namespace,
        query: &str,
        page_size: i32,
        next_page_token: Option<&str>,
    ) -> Result<WorkflowListPage>;

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

    async fn list_workflows(
        &self,
        _ns: &Namespace,
        _query: &str,
        _page_size: i32,
        _next_page_token: Option<&str>,
    ) -> Result<WorkflowListPage> {
        Ok(WorkflowListPage::default())
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

    async fn list_workflows(
        &self,
        ns: &Namespace,
        query: &str,
        page_size: i32,
        next_page_token: Option<&str>,
    ) -> Result<WorkflowListPage> {
        tracing::info!(
            namespace = %ns,
            query = query,
            page_size = page_size,
            "temporal.list_workflows (logging-only client)"
        );
        NoopWorkflowClient
            .list_workflows(ns, query, page_size, next_page_token)
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

#[cfg(feature = "grpc")]
#[derive(Clone)]
pub struct GrpcWorkflowClient {
    client: TemporalSdkClient,
}

#[cfg(feature = "grpc")]
impl fmt::Debug for GrpcWorkflowClient {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("GrpcWorkflowClient").finish_non_exhaustive()
    }
}

#[cfg(feature = "grpc")]
impl GrpcWorkflowClient {
    pub async fn connect(cfg: RuntimeClientConfig) -> Result<Self> {
        let host_port = cfg
            .host_port
            .clone()
            .ok_or_else(|| WorkflowClientError::Invalid("TEMPORAL_HOST_PORT is required".into()))?;
        let target = if host_port.contains("://") {
            host_port.clone()
        } else {
            format!("http://{host_port}")
        };
        let url = Url::parse(&target).map_err(|error| {
            WorkflowClientError::Invalid(format!("invalid Temporal target: {error}"))
        })?;

        let mut connection_options = TemporalConnectionOptions::new(url)
            .identity(cfg.identity)
            .build();
        connection_options.api_key = cfg.api_key;
        let connection = TemporalConnection::connect(connection_options)
            .await
            .map_err(map_connect_error)?;

        let client = TemporalSdkClient::new(
            connection,
            TemporalSdkClientOptions::new(cfg.namespace).build(),
        )
        .map_err(|error| {
            WorkflowClientError::Internal(format!("Temporal client init failed: {error}"))
        })?;

        Ok(Self { client })
    }

    fn untyped_handle(
        &self,
        namespace: &Namespace,
        workflow_id: &WorkflowId,
        run_id: Option<&RunId>,
    ) -> temporalio_client::UntypedWorkflowHandle<TemporalSdkClient> {
        TemporalWorkflowExecutionInfo {
            namespace: namespace.0.clone(),
            workflow_id: workflow_id.0.clone(),
            run_id: run_id.map(|value| value.0.clone()),
            first_execution_run_id: run_id.map(|value| value.0.clone()),
        }
        .bind_untyped(self.client.clone())
    }

    fn payload_converter(&self) -> &temporalio_common::data_converters::PayloadConverter {
        self.client.options().data_converter.payload_converter()
    }

    fn raw_value(&self, value: &serde_json::Value) -> RawValue {
        RawValue::from_value(value, self.payload_converter())
    }

    pub async fn workflow_result_json(
        &self,
        namespace: &Namespace,
        workflow_id: &WorkflowId,
        run_id: &RunId,
    ) -> Result<serde_json::Value> {
        let raw = self
            .untyped_handle(namespace, workflow_id, Some(run_id))
            .get_result(TemporalWorkflowGetResultOptions::default())
            .await
            .map_err(|error| {
                WorkflowClientError::Internal(format!("workflow result failed: {error}"))
            })?;

        Ok(raw.to_value(self.payload_converter()))
    }

    pub async fn describe_schedule(
        &self,
        namespace: &Namespace,
        schedule_id: &str,
    ) -> Result<ScheduleDescription> {
        let response = TemporalWorkflowService::describe_schedule(
            &mut self.client.clone(),
            DescribeScheduleRequest {
                namespace: namespace.0.clone(),
                schedule_id: schedule_id.to_string(),
            }
            .into_request(),
        )
        .await
        .map_err(|status| map_status(status, None))?
        .into_inner();

        let info = response.info;
        let state = response.schedule.and_then(|schedule| schedule.state);
        let recent_actions = info
            .as_ref()
            .map(|info| info.recent_actions.as_slice())
            .unwrap_or(&[]);
        let recent_workflow_ids = recent_actions
            .iter()
            .filter_map(|action| {
                action
                    .start_workflow_result
                    .as_ref()
                    .map(|execution| execution.workflow_id.clone())
            })
            .collect();

        Ok(ScheduleDescription {
            schedule_id: schedule_id.to_string(),
            action_count: info.as_ref().map(|info| info.action_count).unwrap_or(0),
            recent_action_count: recent_actions.len(),
            running_workflow_count: info
                .as_ref()
                .map(|info| info.running_workflows.len())
                .unwrap_or(0),
            remaining_actions: state
                .and_then(|state| state.limited_actions.then_some(state.remaining_actions)),
            recent_workflow_ids,
        })
    }

    fn serialize_search_attributes(
        &self,
        attributes: BTreeMap<String, serde_json::Value>,
    ) -> Result<Option<HashMap<String, TemporalPayload>>> {
        if attributes.is_empty() {
            return Ok(None);
        }

        let converter = self.payload_converter();
        let context = SerializationContext {
            data: &SerializationContextData::None,
            converter,
        };

        let mut encoded = HashMap::with_capacity(attributes.len());
        for (key, value) in attributes {
            let payload = converter
                .to_payload(&context, &value)
                .map_err(|error| WorkflowClientError::Invalid(error.to_string()))?;
            encoded.insert(key, payload);
        }
        Ok(Some(encoded))
    }
}

#[cfg(feature = "grpc")]
#[async_trait]
impl WorkflowClient for GrpcWorkflowClient {
    async fn start_workflow(&self, options: StartWorkflowOptions) -> Result<WorkflowHandle> {
        let workflow_type = options.workflow_type.0.clone();
        let workflow_id = options.workflow_id.0.clone();
        let namespace = options.namespace.clone();
        let raw_input = self.raw_value(&options.input);
        let search_attributes = self.serialize_search_attributes(options.search_attributes)?;

        let mut start_options =
            TemporalWorkflowStartOptions::new(options.task_queue.0, workflow_id.clone())
                .id_reuse_policy(map_id_reuse_policy(options.id_reuse_policy))
                .id_conflict_policy(map_id_conflict_policy(options.id_reuse_policy))
                .build();
        start_options.execution_timeout = options.execution_timeout;
        start_options.run_timeout = options.run_timeout;
        start_options.task_timeout = options.task_timeout;
        start_options.search_attributes = search_attributes;

        let handle = self
            .client
            .start_workflow(
                UntypedWorkflow::new(workflow_type),
                raw_input,
                start_options,
            )
            .await
            .map_err(|error| {
                map_start_error(error, &namespace, &WorkflowId(workflow_id.clone()))
            })?;

        Ok(WorkflowHandle {
            workflow_id: WorkflowId(handle.info().workflow_id.clone()),
            run_id: RunId(handle.run_id().unwrap_or_default().to_string()),
        })
    }

    async fn signal_workflow(
        &self,
        namespace: &Namespace,
        workflow_id: &WorkflowId,
        run_id: Option<&RunId>,
        signal_name: &str,
        input: serde_json::Value,
    ) -> Result<()> {
        self.untyped_handle(namespace, workflow_id, run_id)
            .signal(
                UntypedSignal::new(signal_name),
                self.raw_value(&input),
                TemporalWorkflowSignalOptions::builder().build(),
            )
            .await
            .map_err(|error| map_interaction_error(error, namespace, workflow_id))
    }

    async fn query_workflow(
        &self,
        namespace: &Namespace,
        workflow_id: &WorkflowId,
        run_id: Option<&RunId>,
        query_type: &str,
        input: serde_json::Value,
    ) -> Result<serde_json::Value> {
        let raw = self
            .untyped_handle(namespace, workflow_id, run_id)
            .query(
                UntypedQuery::new(query_type),
                self.raw_value(&input),
                TemporalWorkflowQueryOptions::builder().build(),
            )
            .await
            .map_err(|error| map_query_error(error, namespace, workflow_id))?;

        Ok(raw.to_value(self.payload_converter()))
    }

    async fn list_workflows(
        &self,
        namespace: &Namespace,
        query: &str,
        page_size: i32,
        next_page_token: Option<&str>,
    ) -> Result<WorkflowListPage> {
        let request = ListWorkflowExecutionsRequest {
            namespace: namespace.0.clone(),
            page_size,
            next_page_token: next_page_token
                .map(|token| token.as_bytes().to_vec())
                .unwrap_or_default(),
            query: query.to_string(),
        };

        let response = TemporalWorkflowService::list_workflow_executions(
            &mut self.client.clone(),
            request.into_request(),
        )
        .await
        .map_err(|status| map_status(status, None))?
        .into_inner();

        let executions = response
            .executions
            .into_iter()
            .map(workflow_execution_summary_from_proto)
            .collect();
        let next_page_token = if response.next_page_token.is_empty() {
            None
        } else {
            Some(String::from_utf8_lossy(&response.next_page_token).into_owned())
        };

        Ok(WorkflowListPage {
            executions,
            next_page_token,
        })
    }

    async fn cancel_workflow(
        &self,
        namespace: &Namespace,
        workflow_id: &WorkflowId,
        run_id: Option<&RunId>,
        reason: &str,
    ) -> Result<()> {
        self.untyped_handle(namespace, workflow_id, run_id)
            .cancel(WorkflowCancelOptions::builder().reason(reason).build())
            .await
            .map_err(|error| map_interaction_error(error, namespace, workflow_id))
    }

    async fn terminate_workflow(
        &self,
        namespace: &Namespace,
        workflow_id: &WorkflowId,
        run_id: Option<&RunId>,
        reason: &str,
    ) -> Result<()> {
        self.untyped_handle(namespace, workflow_id, run_id)
            .terminate(
                TemporalWorkflowTerminateOptions::builder()
                    .reason(reason)
                    .build(),
            )
            .await
            .map_err(|error| map_interaction_error(error, namespace, workflow_id))
    }

    #[allow(deprecated)]
    async fn create_schedule(&self, namespace: &Namespace, spec: ScheduleSpec) -> Result<()> {
        let converter = self.payload_converter();
        let raw_input = RawValue::from_value(&spec.start_workflow.input, converter);
        let search_attributes =
            self.serialize_search_attributes(spec.start_workflow.search_attributes)?;

        let request = CreateScheduleRequest {
            namespace: namespace.0.clone(),
            schedule_id: spec.schedule_id,
            schedule: Some(TemporalSchedule {
                spec: Some(TemporalScheduleSpec {
                    cron_string: spec.cron_expressions,
                    timezone_name: spec.timezone.unwrap_or_default(),
                    start_time: None,
                    end_time: None,
                    jitter: None,
                    structured_calendar: Vec::new(),
                    calendar: Vec::new(),
                    interval: spec
                        .interval
                        .map(|interval| {
                            Ok::<_, WorkflowClientError>(TemporalIntervalSpec {
                                interval: Some(interval.try_into().map_err(|error| {
                                    WorkflowClientError::Invalid(format!(
                                        "invalid schedule interval: {error}"
                                    ))
                                })?),
                                phase: None,
                            })
                        })
                        .transpose()?
                        .into_iter()
                        .collect(),
                    exclude_calendar: Vec::new(),
                    exclude_structured_calendar: Vec::new(),
                    timezone_data: Vec::new(),
                }),
                action: Some(TemporalScheduleAction {
                    action: Some(schedule_action::Action::StartWorkflow(NewWorkflowExecutionInfo {
                        workflow_id: spec.start_workflow.workflow_id.0,
                        workflow_type: Some(temporalio_common::protos::temporal::api::common::v1::WorkflowType {
                            name: spec.start_workflow.workflow_type.0,
                        }),
                        task_queue: Some(TemporalTaskQueue {
                            name: spec.start_workflow.task_queue.0,
                            kind: 0,
                            normal_name: String::new(),
                        }),
                        input: Some(TemporalPayloads {
                            payloads: raw_input.payloads,
                        }),
                        workflow_execution_timeout: spec
                            .start_workflow
                            .execution_timeout
                            .and_then(|value| value.try_into().ok()),
                        workflow_run_timeout: spec
                            .start_workflow
                            .run_timeout
                            .and_then(|value| value.try_into().ok()),
                        workflow_task_timeout: spec
                            .start_workflow
                            .task_timeout
                            .and_then(|value| value.try_into().ok()),
                        // Temporal schedules reject workflow id reuse policy on
                        // the scheduled action. Idempotency belongs to the
                        // schedule id; duplicate create is handled below as Ok.
                        workflow_id_reuse_policy: TemporalWorkflowIdReusePolicy::Unspecified as i32,
                        retry_policy: None,
                        cron_schedule: String::new(),
                        memo: None,
                        search_attributes: search_attributes.map(|indexed_fields| SearchAttributes { indexed_fields }),
                        header: None,
                        user_metadata: None,
                        versioning_override: None,
                        priority: None,
                    })),
                }),
                policies: Some(TemporalSchedulePolicies {
                    overlap_policy: map_schedule_overlap_policy(spec.overlap_policy) as i32,
                    catchup_window: Some(
                        spec.catchup_window
                            .try_into()
                            .map_err(|error| WorkflowClientError::Invalid(format!("invalid catchup window: {error}")))?,
                    ),
                    pause_on_failure: false,
                    keep_original_workflow_id: true,
                }),
                state: Some(TemporalScheduleState {
                    notes: String::new(),
                    paused: false,
                    limited_actions: spec.max_actions.is_some(),
                    remaining_actions: spec.max_actions.unwrap_or(0),
                }),
            }),
            initial_patch: None,
            identity: self.client.identity(),
            request_id: Uuid::new_v4().to_string(),
            memo: None,
            search_attributes: None,
        };

        match TemporalWorkflowService::create_schedule(
            &mut self.client.clone(),
            request.into_request(),
        )
        .await
        {
            Ok(_) => Ok(()),
            Err(status) if status.code() == TemporalCode::AlreadyExists => Ok(()),
            Err(status) => Err(map_schedule_create_error(status)),
        }
    }

    async fn pause_schedule(
        &self,
        _namespace: &Namespace,
        schedule_id: &str,
        note: &str,
    ) -> Result<()> {
        self.client
            .get_schedule_handle(schedule_id.to_string())
            .pause(Some(note.to_string()))
            .await
            .map_err(map_schedule_error)
    }

    async fn delete_schedule(&self, _namespace: &Namespace, schedule_id: &str) -> Result<()> {
        match self
            .client
            .get_schedule_handle(schedule_id.to_string())
            .delete()
            .await
        {
            Ok(()) => Ok(()),
            Err(error) if is_not_found_status_schedule(&error) => Ok(()),
            Err(error) => Err(map_schedule_error(error)),
        }
    }
}

#[cfg(feature = "grpc")]
fn map_connect_error(error: temporalio_client::errors::ClientConnectError) -> WorkflowClientError {
    match error {
        temporalio_client::errors::ClientConnectError::InvalidUri(err) => {
            WorkflowClientError::Invalid(err.to_string())
        }
        temporalio_client::errors::ClientConnectError::InvalidHeaders(err) => {
            WorkflowClientError::Invalid(err.to_string())
        }
        temporalio_client::errors::ClientConnectError::TonicTransportError(err) => {
            WorkflowClientError::Unavailable(err.to_string())
        }
        temporalio_client::errors::ClientConnectError::SystemInfoCallError(status) => {
            map_status(status, None)
        }
        other => WorkflowClientError::Internal(other.to_string()),
    }
}

#[cfg(feature = "grpc")]
fn map_start_error(
    error: temporalio_client::errors::WorkflowStartError,
    namespace: &Namespace,
    workflow_id: &WorkflowId,
) -> WorkflowClientError {
    match error {
        temporalio_client::errors::WorkflowStartError::AlreadyStarted {
            run_id: _,
            source: _,
        } => WorkflowClientError::AlreadyStarted {
            namespace: namespace.clone(),
            workflow_id: workflow_id.clone(),
        },
        temporalio_client::errors::WorkflowStartError::PayloadConversion(err) => {
            WorkflowClientError::Invalid(err.to_string())
        }
        temporalio_client::errors::WorkflowStartError::Rpc(status) => {
            map_status(status, Some((namespace, workflow_id)))
        }
        other => WorkflowClientError::Internal(other.to_string()),
    }
}

#[cfg(feature = "grpc")]
fn map_interaction_error(
    error: temporalio_client::errors::WorkflowInteractionError,
    namespace: &Namespace,
    workflow_id: &WorkflowId,
) -> WorkflowClientError {
    match error {
        temporalio_client::errors::WorkflowInteractionError::NotFound(_) => {
            WorkflowClientError::NotFound {
                namespace: namespace.clone(),
                workflow_id: workflow_id.clone(),
            }
        }
        temporalio_client::errors::WorkflowInteractionError::PayloadConversion(err) => {
            WorkflowClientError::Invalid(err.to_string())
        }
        temporalio_client::errors::WorkflowInteractionError::Rpc(status) => {
            map_status(status, Some((namespace, workflow_id)))
        }
        temporalio_client::errors::WorkflowInteractionError::Other(err) => {
            WorkflowClientError::Internal(err.to_string())
        }
        other => WorkflowClientError::Internal(other.to_string()),
    }
}

#[cfg(feature = "grpc")]
fn map_query_error(
    error: temporalio_client::errors::WorkflowQueryError,
    namespace: &Namespace,
    workflow_id: &WorkflowId,
) -> WorkflowClientError {
    match error {
        temporalio_client::errors::WorkflowQueryError::NotFound(_) => {
            WorkflowClientError::NotFound {
                namespace: namespace.clone(),
                workflow_id: workflow_id.clone(),
            }
        }
        temporalio_client::errors::WorkflowQueryError::Rejected(rejected) => {
            WorkflowClientError::Invalid(format!("workflow query rejected: {:?}", rejected.status))
        }
        temporalio_client::errors::WorkflowQueryError::PayloadConversion(err) => {
            WorkflowClientError::Invalid(err.to_string())
        }
        temporalio_client::errors::WorkflowQueryError::Rpc(status) => {
            map_status(status, Some((namespace, workflow_id)))
        }
        temporalio_client::errors::WorkflowQueryError::Other(err) => {
            WorkflowClientError::Internal(err.to_string())
        }
        other => WorkflowClientError::Internal(other.to_string()),
    }
}

#[cfg(feature = "grpc")]
fn map_schedule_error(error: temporalio_client::schedules::ScheduleError) -> WorkflowClientError {
    match error {
        temporalio_client::schedules::ScheduleError::Rpc(status) => map_status(status, None),
        other => WorkflowClientError::Internal(other.to_string()),
    }
}

#[cfg(feature = "grpc")]
fn map_schedule_create_error(status: temporalio_client::tonic::Status) -> WorkflowClientError {
    if status.code() == TemporalCode::AlreadyExists {
        return WorkflowClientError::Invalid("schedule already exists".into());
    }
    map_status(status, None)
}

#[cfg(feature = "grpc")]
fn is_not_found_status_schedule(error: &temporalio_client::schedules::ScheduleError) -> bool {
    matches!(
        error,
        temporalio_client::schedules::ScheduleError::Rpc(status)
            if status.code() == TemporalCode::NotFound
    )
}

#[cfg(feature = "grpc")]
fn map_status(
    status: temporalio_client::tonic::Status,
    workflow: Option<(&Namespace, &WorkflowId)>,
) -> WorkflowClientError {
    match status.code() {
        TemporalCode::AlreadyExists => workflow
            .map(
                |(namespace, workflow_id)| WorkflowClientError::AlreadyStarted {
                    namespace: namespace.clone(),
                    workflow_id: workflow_id.clone(),
                },
            )
            .unwrap_or_else(|| WorkflowClientError::Invalid(status.message().to_string())),
        TemporalCode::NotFound => workflow
            .map(|(namespace, workflow_id)| WorkflowClientError::NotFound {
                namespace: namespace.clone(),
                workflow_id: workflow_id.clone(),
            })
            .unwrap_or_else(|| WorkflowClientError::Invalid(status.message().to_string())),
        TemporalCode::InvalidArgument => WorkflowClientError::Invalid(status.message().to_string()),
        TemporalCode::Unavailable => WorkflowClientError::Unavailable(status.message().to_string()),
        _ => WorkflowClientError::Internal(status.to_string()),
    }
}

#[cfg(feature = "grpc")]
fn workflow_execution_summary_from_proto(
    info: TemporalWorkflowExecutionInfoProto,
) -> WorkflowExecutionSummary {
    let (workflow_id, run_id) = match info.execution {
        Some(execution) => (execution.workflow_id, execution.run_id),
        None => (String::new(), String::new()),
    };
    let workflow_type = info.r#type.map(|value| value.name).unwrap_or_default();

    WorkflowExecutionSummary {
        workflow_id: WorkflowId(workflow_id),
        run_id: RunId(run_id),
        workflow_type: WorkflowType(workflow_type),
        task_queue: TaskQueue(info.task_queue),
        status: workflow_status_from_proto(info.status),
        start_time: info.start_time.and_then(|value| {
            DateTime::<Utc>::from_timestamp(value.seconds, value.nanos.try_into().ok()?)
        }),
        close_time: info.close_time.and_then(|value| {
            DateTime::<Utc>::from_timestamp(value.seconds, value.nanos.try_into().ok()?)
        }),
        history_length: info.history_length,
    }
}

#[cfg(feature = "grpc")]
fn workflow_status_from_proto(status: i32) -> WorkflowExecutionStatus {
    match status {
        value if value == TemporalWorkflowExecutionStatus::Running as i32 => {
            WorkflowExecutionStatus::Running
        }
        value if value == TemporalWorkflowExecutionStatus::Completed as i32 => {
            WorkflowExecutionStatus::Completed
        }
        value if value == TemporalWorkflowExecutionStatus::Failed as i32 => {
            WorkflowExecutionStatus::Failed
        }
        value if value == TemporalWorkflowExecutionStatus::Canceled as i32 => {
            WorkflowExecutionStatus::Canceled
        }
        value if value == TemporalWorkflowExecutionStatus::Terminated as i32 => {
            WorkflowExecutionStatus::Terminated
        }
        value if value == TemporalWorkflowExecutionStatus::ContinuedAsNew as i32 => {
            WorkflowExecutionStatus::ContinuedAsNew
        }
        value if value == TemporalWorkflowExecutionStatus::TimedOut as i32 => {
            WorkflowExecutionStatus::TimedOut
        }
        value if value == TemporalWorkflowExecutionStatus::Paused as i32 => {
            WorkflowExecutionStatus::Paused
        }
        _ => WorkflowExecutionStatus::Unknown,
    }
}

#[cfg(feature = "grpc")]
fn map_id_reuse_policy(value: IdReusePolicy) -> TemporalWorkflowIdReusePolicy {
    match value {
        IdReusePolicy::AllowDuplicate => TemporalWorkflowIdReusePolicy::AllowDuplicate,
        IdReusePolicy::AllowDuplicateFailedOnly => {
            TemporalWorkflowIdReusePolicy::AllowDuplicateFailedOnly
        }
        IdReusePolicy::RejectDuplicate => TemporalWorkflowIdReusePolicy::RejectDuplicate,
        IdReusePolicy::TerminateIfRunning => TemporalWorkflowIdReusePolicy::AllowDuplicate,
    }
}

#[cfg(feature = "grpc")]
fn map_id_conflict_policy(value: IdReusePolicy) -> TemporalWorkflowIdConflictPolicy {
    match value {
        IdReusePolicy::TerminateIfRunning => TemporalWorkflowIdConflictPolicy::TerminateExisting,
        _ => TemporalWorkflowIdConflictPolicy::Fail,
    }
}

#[cfg(feature = "grpc")]
fn map_schedule_overlap_policy(value: ScheduleOverlapPolicy) -> TemporalScheduleOverlapPolicy {
    match value {
        ScheduleOverlapPolicy::Skip => TemporalScheduleOverlapPolicy::Skip,
        ScheduleOverlapPolicy::BufferOne => TemporalScheduleOverlapPolicy::BufferOne,
        ScheduleOverlapPolicy::BufferAll => TemporalScheduleOverlapPolicy::BufferAll,
        ScheduleOverlapPolicy::CancelOther => TemporalScheduleOverlapPolicy::CancelOther,
        ScheduleOverlapPolicy::TerminateOther => TemporalScheduleOverlapPolicy::TerminateOther,
        ScheduleOverlapPolicy::AllowAll => TemporalScheduleOverlapPolicy::AllowAll,
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
    pub const REINDEX: &str = "openfoundry.reindex";
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
    task_queue: TaskQueue,
    workflow_options: RuntimeWorkflowOptions,
}

impl WorkflowAutomationClient {
    pub fn new(inner: Arc<dyn WorkflowClient>, namespace: Namespace) -> Self {
        Self::new_with_options(
            inner,
            namespace,
            task_queue_from_env(
                "TEMPORAL_TASK_QUEUE_WORKFLOW_AUTOMATION",
                task_queues::WORKFLOW_AUTOMATION,
            ),
            runtime_workflow_options_from_env(),
        )
    }

    pub fn new_with_options(
        inner: Arc<dyn WorkflowClient>,
        namespace: Namespace,
        task_queue: TaskQueue,
        workflow_options: RuntimeWorkflowOptions,
    ) -> Self {
        Self {
            inner,
            namespace,
            task_queue,
            workflow_options,
        }
    }

    pub async fn start_run(
        &self,
        run_id: Uuid,
        input: AutomationRunInput,
        audit_correlation_id: Uuid,
    ) -> Result<WorkflowHandle> {
        let workflow_id = Self::workflow_id(input.definition_id, run_id);
        let mut options = StartWorkflowOptions::new(
            self.namespace.clone(),
            workflow_id,
            WorkflowType(workflow_types::AUTOMATION_RUN.to_string()),
            self.task_queue.clone(),
            serde_json::to_value(input).map_err(|e| WorkflowClientError::Invalid(e.to_string()))?,
            audit_correlation_id,
        );
        self.workflow_options.apply(&mut options);
        self.inner.start_workflow(options).await
    }

    pub async fn cancel_run(&self, definition_id: Uuid, run_id: Uuid, reason: &str) -> Result<()> {
        let workflow_id = Self::workflow_id(definition_id, run_id);
        self.inner
            .cancel_workflow(&self.namespace, &workflow_id, None, reason)
            .await
    }

    pub async fn list_runs(
        &self,
        definition_id: Uuid,
        page_size: i32,
        next_page_token: Option<&str>,
    ) -> Result<WorkflowListPage> {
        let query = format!(
            "WorkflowType = '{}' AND WorkflowId STARTS_WITH 'workflow-automation:{definition_id}:'",
            workflow_types::AUTOMATION_RUN
        );
        self.inner
            .list_workflows(&self.namespace, &query, page_size, next_page_token)
            .await
    }

    pub async fn query_run_state(
        &self,
        definition_id: Uuid,
        run_id: Uuid,
        query_type: &str,
        input: serde_json::Value,
    ) -> Result<serde_json::Value> {
        let workflow_id = Self::workflow_id(definition_id, run_id);
        self.inner
            .query_workflow(&self.namespace, &workflow_id, None, query_type, input)
            .await
    }

    pub fn workflow_id(definition_id: Uuid, run_id: Uuid) -> WorkflowId {
        WorkflowId(format!("workflow-automation:{definition_id}:{run_id}"))
    }

    pub fn parse_workflow_id(raw: &str) -> Option<(Uuid, Uuid)> {
        let mut parts = raw.split(':');
        match (parts.next(), parts.next(), parts.next(), parts.next()) {
            (Some("workflow-automation"), Some(definition_id), Some(run_id), None) => {
                Some((definition_id.parse().ok()?, run_id.parse().ok()?))
            }
            _ => None,
        }
    }
}

/// Legacy per-run input that used to be the Go workflow contract for
/// `workflow_types::AUTOMATION_RUN`. The Go worker was retired by
/// FASE 5 / Tarea 5.4 of the Foundry-pattern migration; this struct
/// is kept around because the wider crate still compiles unused code
/// paths until the libs/temporal-client retirement (FASE 8 /
/// Tarea 8.1). New code should publish to `automate.condition.v1`
/// via `services/workflow-automation-service` instead.
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
    task_queue: TaskQueue,
    workflow_options: RuntimeWorkflowOptions,
}

impl PipelineScheduleClient {
    pub fn new(inner: Arc<dyn WorkflowClient>, namespace: Namespace) -> Self {
        Self::new_with_options(
            inner,
            namespace,
            task_queue_from_env("TEMPORAL_TASK_QUEUE_PIPELINE", task_queues::PIPELINE),
            runtime_workflow_options_from_env(),
        )
    }

    pub fn new_with_options(
        inner: Arc<dyn WorkflowClient>,
        namespace: Namespace,
        task_queue: TaskQueue,
        workflow_options: RuntimeWorkflowOptions,
    ) -> Self {
        Self {
            inner,
            namespace,
            task_queue,
            workflow_options,
        }
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
        let mut start = StartWorkflowOptions::new(
            self.namespace.clone(),
            workflow_id,
            WorkflowType(workflow_types::PIPELINE_RUN.to_string()),
            self.task_queue.clone(),
            serde_json::to_value(run_input)
                .map_err(|e| WorkflowClientError::Invalid(e.to_string()))?,
            audit_correlation_id,
        );
        self.workflow_options.apply(&mut start);
        let spec = ScheduleSpec {
            schedule_id,
            cron_expressions,
            interval: None,
            timezone,
            start_workflow: start,
            overlap_policy: ScheduleOverlapPolicy::Skip,
            catchup_window: Duration::from_secs(60 * 60),
            max_actions: None,
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
    task_queue: TaskQueue,
    workflow_options: RuntimeWorkflowOptions,
}

impl ApprovalsClient {
    pub const SIGNAL_DECIDE: &'static str = "decide";

    pub fn new(inner: Arc<dyn WorkflowClient>, namespace: Namespace) -> Self {
        Self::new_with_options(
            inner,
            namespace,
            task_queue_from_env("TEMPORAL_TASK_QUEUE_APPROVALS", task_queues::APPROVALS),
            runtime_workflow_options_from_env(),
        )
    }

    pub fn new_with_options(
        inner: Arc<dyn WorkflowClient>,
        namespace: Namespace,
        task_queue: TaskQueue,
        workflow_options: RuntimeWorkflowOptions,
    ) -> Self {
        Self {
            inner,
            namespace,
            task_queue,
            workflow_options,
        }
    }

    pub async fn open(
        &self,
        request_id: Uuid,
        input: ApprovalRequestInput,
        audit_correlation_id: Uuid,
    ) -> Result<WorkflowHandle> {
        let mut options = StartWorkflowOptions::new(
            self.namespace.clone(),
            WorkflowId(format!("approval:{request_id}")),
            WorkflowType(workflow_types::APPROVAL_REQUEST.to_string()),
            self.task_queue.clone(),
            serde_json::to_value(input).map_err(|e| WorkflowClientError::Invalid(e.to_string()))?,
            audit_correlation_id,
        );
        self.workflow_options.apply(&mut options);
        self.inner.start_workflow(options).await
    }

    pub async fn decide(&self, request_id: Uuid, decision: ApprovalDecision) -> Result<()> {
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
    task_queue: TaskQueue,
    workflow_options: RuntimeWorkflowOptions,
}

impl AutomationOpsClient {
    pub fn new(inner: Arc<dyn WorkflowClient>, namespace: Namespace) -> Self {
        Self::new_with_options(
            inner,
            namespace,
            task_queue_from_env(
                "TEMPORAL_TASK_QUEUE_AUTOMATION_OPS",
                task_queues::AUTOMATION_OPS,
            ),
            runtime_workflow_options_from_env(),
        )
    }

    pub fn new_with_options(
        inner: Arc<dyn WorkflowClient>,
        namespace: Namespace,
        task_queue: TaskQueue,
        workflow_options: RuntimeWorkflowOptions,
    ) -> Self {
        Self {
            inner,
            namespace,
            task_queue,
            workflow_options,
        }
    }

    pub async fn start_task(
        &self,
        task_id: Uuid,
        input: AutomationOpsInput,
        audit_correlation_id: Uuid,
    ) -> Result<WorkflowHandle> {
        let mut options = StartWorkflowOptions::new(
            self.namespace.clone(),
            WorkflowId(format!("automation-ops:{task_id}")),
            WorkflowType(workflow_types::AUTOMATION_OPS_TASK.to_string()),
            self.task_queue.clone(),
            serde_json::to_value(input).map_err(|e| WorkflowClientError::Invalid(e.to_string()))?,
            audit_correlation_id,
        );
        self.workflow_options.apply(&mut options);
        self.inner.start_workflow(options).await
    }
}

/// Legacy per-run input that used to be the Go workflow contract for
/// `workflow_types::AUTOMATION_OPS_TASK`. The Go worker was retired
/// by FASE 6 / Tarea 6.5 of the Foundry-pattern migration; this
/// struct is kept around because `libs/temporal-client` still ships
/// dead-code paths until the FASE 8 / Tarea 8.1 wholesale retirement.
/// New code should publish to `saga.step.requested.v1` via
/// `services/automation-operations-service` instead.
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
    use std::sync::Mutex;

    #[derive(Default)]
    struct RecordingWorkflowClient {
        starts: Mutex<Vec<StartWorkflowOptions>>,
        schedules: Mutex<Vec<ScheduleSpec>>,
        list_queries: Mutex<Vec<String>>,
        queries: Mutex<Vec<(WorkflowId, String)>>,
    }

    #[async_trait]
    impl WorkflowClient for RecordingWorkflowClient {
        async fn start_workflow(&self, options: StartWorkflowOptions) -> Result<WorkflowHandle> {
            let handle = WorkflowHandle {
                workflow_id: options.workflow_id.clone(),
                run_id: RunId("mock-run".into()),
            };
            self.starts.lock().expect("starts lock").push(options);
            Ok(handle)
        }

        async fn signal_workflow(
            &self,
            _namespace: &Namespace,
            _workflow_id: &WorkflowId,
            _run_id: Option<&RunId>,
            _signal_name: &str,
            _input: serde_json::Value,
        ) -> Result<()> {
            Ok(())
        }

        async fn query_workflow(
            &self,
            _namespace: &Namespace,
            workflow_id: &WorkflowId,
            _run_id: Option<&RunId>,
            query_type: &str,
            _input: serde_json::Value,
        ) -> Result<serde_json::Value> {
            self.queries
                .lock()
                .expect("queries lock")
                .push((workflow_id.clone(), query_type.to_string()));
            Ok(serde_json::json!({"ok": true}))
        }

        async fn list_workflows(
            &self,
            _namespace: &Namespace,
            query: &str,
            _page_size: i32,
            _next_page_token: Option<&str>,
        ) -> Result<WorkflowListPage> {
            self.list_queries
                .lock()
                .expect("list_queries lock")
                .push(query.to_string());
            Ok(WorkflowListPage::default())
        }

        async fn cancel_workflow(
            &self,
            _namespace: &Namespace,
            _workflow_id: &WorkflowId,
            _run_id: Option<&RunId>,
            _reason: &str,
        ) -> Result<()> {
            Ok(())
        }

        async fn terminate_workflow(
            &self,
            _namespace: &Namespace,
            _workflow_id: &WorkflowId,
            _run_id: Option<&RunId>,
            _reason: &str,
        ) -> Result<()> {
            Ok(())
        }

        async fn create_schedule(&self, _namespace: &Namespace, spec: ScheduleSpec) -> Result<()> {
            self.schedules.lock().expect("schedules lock").push(spec);
            Ok(())
        }

        async fn pause_schedule(
            &self,
            _namespace: &Namespace,
            _schedule_id: &str,
            _note: &str,
        ) -> Result<()> {
            Ok(())
        }

        async fn delete_schedule(&self, _namespace: &Namespace, _schedule_id: &str) -> Result<()> {
            Ok(())
        }
    }

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
        let handle = client
            .start_workflow(opts)
            .await
            .expect("noop must succeed");
        assert_eq!(handle.workflow_id.0, "wf-1");
        assert!(handle.run_id.0.starts_with("noop-"));
    }

    #[tokio::test]
    async fn workflow_automation_client_round_trip() {
        let client =
            WorkflowAutomationClient::new(Arc::new(NoopWorkflowClient), Namespace::new("default"));
        let run_id = Uuid::now_v7();
        let definition_id = Uuid::now_v7();
        let handle = client
            .start_run(
                run_id,
                AutomationRunInput {
                    run_id,
                    definition_id,
                    tenant_id: "acme".into(),
                    triggered_by: "tester".into(),
                    trigger_payload: serde_json::json!({}),
                },
                Uuid::now_v7(),
            )
            .await
            .expect("noop must succeed");
        assert_eq!(
            handle.workflow_id.0,
            format!("workflow-automation:{definition_id}:{run_id}")
        );

        let parsed = WorkflowAutomationClient::parse_workflow_id(&handle.workflow_id.0)
            .expect("parse round-trip");
        assert_eq!(parsed, (definition_id, run_id));
    }

    #[tokio::test]
    async fn workflow_automation_client_list_runs_filters_by_definition_prefix() {
        let recorder = Arc::new(RecordingWorkflowClient::default());
        let client = WorkflowAutomationClient::new(recorder.clone(), Namespace::new("default"));
        let definition_id = Uuid::now_v7();
        client
            .list_runs(definition_id, 25, None)
            .await
            .expect("list_runs");
        let queries = recorder.list_queries.lock().expect("list_queries lock");
        let query = queries.first().expect("one list");
        assert!(
            query.contains(&format!(
                "WorkflowId STARTS_WITH 'workflow-automation:{definition_id}:'"
            )),
            "query missing definition prefix: {query}"
        );
        assert!(
            query.contains(&format!(
                "WorkflowType = '{}'",
                workflow_types::AUTOMATION_RUN
            )),
            "query missing workflow type: {query}"
        );
    }

    #[tokio::test]
    async fn workflow_automation_client_query_run_state_uses_canonical_id() {
        let recorder = Arc::new(RecordingWorkflowClient::default());
        let client = WorkflowAutomationClient::new(recorder.clone(), Namespace::new("default"));
        let definition_id = Uuid::now_v7();
        let run_id = Uuid::now_v7();
        client
            .query_run_state(
                definition_id,
                run_id,
                "current_state",
                serde_json::json!({}),
            )
            .await
            .expect("query_run_state");
        let queries = recorder.queries.lock().expect("queries lock");
        let (workflow_id, query_type) = queries.first().expect("one query");
        assert_eq!(
            workflow_id.0,
            format!("workflow-automation:{definition_id}:{run_id}")
        );
        assert_eq!(query_type, "current_state");
    }

    #[test]
    fn workflow_id_parses_only_canonical_format() {
        assert!(
            WorkflowAutomationClient::parse_workflow_id("workflow-automation:not-a-uuid").is_none()
        );
        assert!(WorkflowAutomationClient::parse_workflow_id("approval:123").is_none());
        let definition_id = Uuid::now_v7();
        let run_id = Uuid::now_v7();
        let canonical = WorkflowAutomationClient::workflow_id(definition_id, run_id);
        let parsed = WorkflowAutomationClient::parse_workflow_id(&canonical.0).expect("parse");
        assert_eq!(parsed, (definition_id, run_id));
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
        assert!(
            opts.search_attributes
                .contains_key(StartWorkflowOptions::SEARCH_ATTR_AUDIT_CORRELATION)
        );
    }

    #[test]
    fn runtime_client_config_uses_logging_without_host_port() {
        let cfg = RuntimeClientConfig {
            host_port: None,
            namespace: "default".into(),
            identity: "svc".into(),
            api_key: None,
            require_real_client: false,
            deployment_environment: None,
        };
        assert!(!cfg.uses_temporal());
        assert!(!cfg.requires_temporal());
    }

    #[test]
    fn runtime_client_config_uses_temporal_when_host_port_is_present() {
        let cfg = RuntimeClientConfig {
            host_port: Some("127.0.0.1:7233".into()),
            namespace: "default".into(),
            identity: "svc".into(),
            api_key: None,
            require_real_client: false,
            deployment_environment: None,
        };
        assert!(cfg.uses_temporal());
    }

    #[test]
    fn runtime_client_config_requires_temporal_for_explicit_guardrail() {
        let cfg = RuntimeClientConfig {
            host_port: None,
            namespace: "default".into(),
            identity: "svc".into(),
            api_key: None,
            require_real_client: true,
            deployment_environment: None,
        };
        assert!(cfg.requires_temporal());
    }

    #[test]
    fn runtime_client_config_requires_temporal_for_staging_and_prod() {
        for environment in ["staging", "stage", "prod", "production"] {
            let cfg = RuntimeClientConfig {
                host_port: None,
                namespace: "default".into(),
                identity: "svc".into(),
                api_key: None,
                require_real_client: false,
                deployment_environment: Some(environment.into()),
            };
            assert!(cfg.requires_temporal(), "{environment}");
        }

        let cfg = RuntimeClientConfig {
            host_port: None,
            namespace: "default".into(),
            identity: "svc".into(),
            api_key: None,
            require_real_client: false,
            deployment_environment: Some("dev".into()),
        };
        assert!(!cfg.requires_temporal());
    }

    #[test]
    fn runtime_workflow_options_parse_timeout_env() {
        let options = RuntimeWorkflowOptions::from_lookup(|key| match key {
            "TEMPORAL_WORKFLOW_EXECUTION_TIMEOUT_SECS" => Some("3600".into()),
            "TEMPORAL_WORKFLOW_RUN_TIMEOUT_SECS" => Some("600".into()),
            "TEMPORAL_WORKFLOW_TASK_TIMEOUT_SECS" => Some("30".into()),
            _ => None,
        })
        .expect("valid timeout env");

        assert_eq!(options.execution_timeout, Some(Duration::from_secs(3600)));
        assert_eq!(options.run_timeout, Some(Duration::from_secs(600)));
        assert_eq!(options.task_timeout, Some(Duration::from_secs(30)));
    }

    #[test]
    fn invalid_timeout_env_is_rejected() {
        let err = RuntimeWorkflowOptions::from_lookup(|key| {
            (key == "TEMPORAL_WORKFLOW_RUN_TIMEOUT_SECS").then(|| "nope".into())
        })
        .expect_err("invalid timeout must fail");
        assert!(matches!(err, WorkflowClientError::Invalid(_)));
    }

    #[test]
    fn domain_task_queue_override_wins_over_global_override() {
        let queue = task_queue_from_lookup(
            "TEMPORAL_TASK_QUEUE_PIPELINE",
            task_queues::PIPELINE,
            |key| match key {
                "TEMPORAL_TASK_QUEUE" => Some("global-e2e".into()),
                "TEMPORAL_TASK_QUEUE_PIPELINE" => Some("pipeline-e2e".into()),
                _ => None,
            },
        );
        assert_eq!(queue.0, "pipeline-e2e");
    }

    #[tokio::test]
    async fn workflow_automation_client_applies_configured_queue_and_timeouts() {
        let recorder = Arc::new(RecordingWorkflowClient::default());
        let client = WorkflowAutomationClient::new_with_options(
            recorder.clone(),
            Namespace::new("e2e"),
            TaskQueue("workflow-e2e".into()),
            RuntimeWorkflowOptions {
                execution_timeout: Some(Duration::from_secs(3600)),
                run_timeout: Some(Duration::from_secs(900)),
                task_timeout: Some(Duration::from_secs(15)),
            },
        );
        let run_id = Uuid::now_v7();
        client
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
            .expect("mock start");

        let starts = recorder.starts.lock().expect("starts lock");
        let options = starts.first().expect("one workflow start");
        assert_eq!(options.namespace.0, "e2e");
        assert_eq!(options.task_queue.0, "workflow-e2e");
        assert_eq!(options.execution_timeout, Some(Duration::from_secs(3600)));
        assert_eq!(options.run_timeout, Some(Duration::from_secs(900)));
        assert_eq!(options.task_timeout, Some(Duration::from_secs(15)));
    }

    #[tokio::test]
    async fn pipeline_schedule_client_applies_configured_queue_to_start_action() {
        let recorder = Arc::new(RecordingWorkflowClient::default());
        let client = PipelineScheduleClient::new_with_options(
            recorder.clone(),
            Namespace::new("e2e"),
            TaskQueue("pipeline-e2e".into()),
            RuntimeWorkflowOptions {
                execution_timeout: None,
                run_timeout: Some(Duration::from_secs(120)),
                task_timeout: None,
            },
        );
        client
            .create(
                "sched-1".into(),
                vec!["* * * * *".into()],
                Some("UTC".into()),
                PipelineRunInput {
                    pipeline_id: Uuid::now_v7(),
                    tenant_id: "acme".into(),
                    revision: None,
                    parameters: serde_json::json!({}),
                },
                Uuid::now_v7(),
            )
            .await
            .expect("mock schedule");

        let schedules = recorder.schedules.lock().expect("schedules lock");
        let spec = schedules.first().expect("one schedule");
        assert_eq!(spec.start_workflow.task_queue.0, "pipeline-e2e");
        assert_eq!(
            spec.start_workflow.run_timeout,
            Some(Duration::from_secs(120))
        );
    }
}
