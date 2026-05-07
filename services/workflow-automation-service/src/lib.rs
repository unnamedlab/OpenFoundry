//! `workflow-automation-service` library crate.
//!
//! Post-S8 ownership boundary for the workflow / automation /
//! approval domains (ADR-0030 + `service-consolidation-map.md`):
//!
//! * `domain` + `handlers` + `models` (top-level) — workflow
//!   definitions, runs and the FASE 5 condition consumer (legacy
//!   `workflow-automation-service`).
//! * [`automation_operations`] — Postgres-backed saga substrate +
//!   `saga.step.requested.v1` consumer (legacy
//!   `automation-operations-service`, S8 merge).
//! * [`approvals`] — `audit_compliance.approval_requests` state
//!   machine + `approval.*.v1` outbox + companion timeout-sweep
//!   CronJob (legacy `approvals-service`, S8 merge).
//!
//! The companion `approvals-timeout-sweep` binary (`src/bin/`)
//! reuses [`approvals::domain::approval_request`],
//! [`approvals::event`] and [`approvals::topics`] through this lib.

pub mod approvals;
pub mod automation_operations;
pub mod config;
pub mod domain;
pub mod event;
pub mod handlers;
pub mod models;
pub mod topics;

use auth_middleware::jwt::JwtConfig;
use sqlx::PgPool;

/// Shared state injected into the axum router and the
/// background consumer tasks.
#[derive(Clone)]
pub struct AppState {
    /// Primary Postgres pool. Backs `workflow_automation.automation_runs`,
    /// `automation_operations.processed_events`, `saga.state`,
    /// `audit_compliance.approval_requests`, and the per-bounded-
    /// context `outbox.events` table the Debezium connector watches.
    /// In compose dev all three legacy services pointed at the same
    /// DB (`openfoundry_workflow_service`); in helm production the
    /// DSN is injected from `database-credentials` and the schemas
    /// are pre-created by the cluster bootstrap.
    pub db: PgPool,
    pub http_client: reqwest::Client,
    /// JWT signing config — kept on `AppState` so future helpers can
    /// issue service / actor tokens for outbound calls (mirror of
    /// the legacy `approvals-service` `runtime` helpers).
    #[allow(dead_code)]
    pub jwt_config: JwtConfig,
    /// NATS bootstrap URL for the legacy
    /// `of.workflows.run.requested` subject. Kept until
    /// `pipeline-schedule-service` migrates fully to Kafka.
    pub nats_url: String,
    pub pipeline_service_url: String,
    /// `ontology-actions-service` base URL — used by the approvals
    /// auto-apply path.
    #[allow(dead_code)]
    pub ontology_service_url: String,
    /// `audit-compliance-service` base URL. The state-machine
    /// handlers POST a synchronous audit row on every terminal
    /// transition (mirror of the legacy
    /// `workers-go/approvals/activities::EmitAuditEvent`). A
    /// follow-up FASE 9 task collapses this into a Kafka consumer
    /// of `approval.completed.v1` inside `audit-compliance-service`;
    /// the HTTP path is the safe interim.
    pub audit_compliance_service_url: String,
    /// Service bearer token forwarded to
    /// `audit-compliance-service`. Optional in dev (the audit
    /// service accepts unauthenticated writes from in-cluster
    /// peers when this is empty).
    pub audit_compliance_bearer_token: Option<String>,
    /// Default approval deadline applied at insert time when the
    /// caller does not supply one. Replaces the legacy worker's
    /// hard-coded `24*time.Hour` constant. Hours.
    pub approval_ttl_hours: u32,
}
