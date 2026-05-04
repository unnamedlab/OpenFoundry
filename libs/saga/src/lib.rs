//! Saga choreography helper for OpenFoundry's Foundry-pattern
//! orchestration substrate (ADR-0037, Tarea 1.2).
//!
//! ## What this crate is
//!
//! A small helper that lets a service express a long-running flow as
//! a sequence of [`SagaStep`]s, each with an `execute` and a
//! `compensate` half. The runner ([`SagaRunner`]) drives the steps in
//! order, persists progress to `saga.state`, and on failure runs the
//! compensations of every previously-completed step in LIFO order. It
//! also publishes one `saga.step.*` event per state transition
//! through the canonical Debezium outbox (see the [`outbox`] crate).
//!
//! The intended consumers are `automation-operations-service` and
//! `workflow-automation-service`.
//!
//! ## Idempotency
//!
//! Re-running a saga with the same `saga_id` must not re-execute
//! steps that already completed. The runner enforces this by reading
//! `saga.state.completed_steps` on [`SagaRunner::start`] and, on every
//! [`SagaRunner::execute_step`], short-circuiting with the cached
//! output stored in `saga.state.step_outputs` when the step name is
//! already present.
//!
//! Outbox events are also idempotent: every event_id is a deterministic
//! v5 UUID derived from `(saga_id, step_name, event_kind)`, so an
//! `ON CONFLICT DO NOTHING` write inside the outbox helper makes
//! duplicate emission a no-op.
//!
//! ## Event taxonomy
//!
//! | event kind                  | when                                      |
//! | --------------------------- | ----------------------------------------- |
//! | `saga.step.completed`       | step's `execute` returned `Ok`            |
//! | `saga.step.failed`          | step's `execute` returned `Err`           |
//! | `saga.step.compensated`     | a previously-completed step was undone    |
//! | `saga.completed`            | `finish()` called after every step OK     |
//! | `saga.aborted`              | `abort()` called by the caller            |
//!
//! ## Example
//!
//! ```ignore
//! use saga::{SagaRunner, SagaStep, SagaError};
//! use serde::{Deserialize, Serialize};
//! use uuid::Uuid;
//!
//! #[derive(Serialize, Deserialize)] struct ReserveInput { sku: String, qty: u32 }
//! #[derive(Serialize, Deserialize)] struct ReserveOutput { reservation_id: Uuid }
//!
//! struct ReserveInventory;
//!
//! #[async_trait::async_trait]
//! impl SagaStep for ReserveInventory {
//!     type Input = ReserveInput;
//!     type Output = ReserveOutput;
//!     fn step_name() -> &'static str { "reserve_inventory" }
//!     async fn execute(input: Self::Input) -> Result<Self::Output, SagaError> {
//!         // call inventory service…
//!         Ok(ReserveOutput { reservation_id: Uuid::now_v7() })
//!     }
//!     async fn compensate(input: Self::Input) -> Result<(), SagaError> {
//!         // release inventory…
//!         Ok(())
//!     }
//! }
//! ```

use std::collections::HashSet;

use async_trait::async_trait;
use futures::future::BoxFuture;
use serde::Serialize;
use serde::de::DeserializeOwned;
use sqlx::{Postgres, Row, Transaction};
use thiserror::Error;
use uuid::Uuid;

pub use outbox;
use outbox::OutboxEvent;

// ─── Errors ───────────────────────────────────────────────────────────────

/// Errors raised by saga step execution, compensation, persistence or
/// outbox emission.
#[derive(Debug, Error)]
pub enum SagaError {
    /// A step's `execute` returned a domain failure.
    #[error("step `{step}` failed: {message}")]
    Step { step: String, message: String },

    /// A step's `compensate` returned a domain failure. Compensation
    /// failures are surfaced but they do not stop the runner from
    /// trying the remaining compensations — see
    /// [`SagaRunner::execute_step`] for the semantics.
    #[error("compensate `{step}` failed: {message}")]
    Compensation { step: String, message: String },

    /// Underlying Postgres error.
    #[error("database error: {0}")]
    Db(#[from] sqlx::Error),

    /// Outbox emission failed.
    #[error("outbox: {0}")]
    Outbox(#[from] outbox::OutboxError),

    /// Serializing a step input/output (or saga payload) failed.
    #[error("serialize: {0}")]
    Serialize(#[from] serde_json::Error),

    /// The saga was already in a terminal state when a write was
    /// attempted. Indicates a programming error — the caller invoked
    /// `execute_step` after `finish()` / `abort()` / a failure.
    #[error("saga is in terminal state `{0}`")]
    Terminal(String),
}

impl SagaError {
    /// Convenience constructor for use inside step bodies.
    pub fn step(step: impl Into<String>, message: impl Into<String>) -> Self {
        SagaError::Step {
            step: step.into(),
            message: message.into(),
        }
    }

    /// Convenience constructor for use inside compensation bodies.
    pub fn compensation(step: impl Into<String>, message: impl Into<String>) -> Self {
        SagaError::Compensation {
            step: step.into(),
            message: message.into(),
        }
    }
}

// ─── SagaStep trait ───────────────────────────────────────────────────────

/// A unit of work inside a saga. Implementors are expected to be
/// stateless types (often unit structs); the per-instance data lives
/// in `Input`. The `compensate` half must be safe to call after a
/// successful `execute` for the same input.
///
/// The trait uses [`macro@async_trait`] so it stays object-safe-ish
/// across services and matches the helper-trait convention used by
/// the rest of the workspace (see `libs/outbox`,
/// `libs/authz-cedar`, `libs/storage-abstraction`).
#[async_trait]
pub trait SagaStep: Send + Sync + 'static {
    /// Step input. Round-trips through `serde_json` so the runner can
    /// cache it for compensation and replay.
    type Input: Serialize + DeserializeOwned + Clone + Send + Sync + 'static;

    /// Step output. Round-trips through `serde_json` so the runner
    /// can cache it in `saga.state.step_outputs` and return it on an
    /// idempotent retry.
    type Output: Serialize + DeserializeOwned + Send + Sync + 'static;

    /// Stable name of the step. Used as the key in
    /// `saga.state.completed_steps` / `step_outputs` and in the
    /// `saga.step.*` outbox events. Must be unique inside one saga
    /// definition.
    fn step_name() -> &'static str;

    /// Forward action.
    async fn execute(input: Self::Input) -> Result<Self::Output, SagaError>;

    /// Inverse action. Run by the saga runner in LIFO order when a
    /// later step fails.
    async fn compensate(input: Self::Input) -> Result<(), SagaError>;
}

// ─── Outbox helper ────────────────────────────────────────────────────────

/// Append a single event to the transactional outbox in `tx` and
/// return the deterministic `event_id` that was generated. The id is
/// a v5 UUID over `aggregate || aggregate_id || topic || payload`, so
/// repeated calls with the same arguments inside a fresh transaction
/// are no-ops at the outbox level.
///
/// This is a thin wrapper around [`outbox::enqueue`] that fits the
/// signature spelled out in the migration plan
/// (`enqueue_outbox_event(tx, aggregate, aggregate_id, topic, payload)`).
pub async fn enqueue_outbox_event<E: Serialize>(
    tx: &mut Transaction<'_, Postgres>,
    aggregate: &str,
    aggregate_id: &str,
    topic: &str,
    payload: &E,
) -> Result<Uuid, SagaError> {
    let payload_json = serde_json::to_value(payload)?;
    let key = format!("{aggregate}:{aggregate_id}:{topic}:{payload_json}");
    let event_id = Uuid::new_v5(&Uuid::NAMESPACE_OID, key.as_bytes());
    outbox::enqueue(
        tx,
        OutboxEvent::new(event_id, aggregate, aggregate_id, topic, payload_json),
    )
    .await?;
    Ok(event_id)
}

// ─── Saga events recorded by the runner ───────────────────────────────────

/// Event kinds emitted by [`SagaRunner`] to the outbox. Surfaced in
/// the `topic` field of [`OutboxEvent`] as `saga.step.completed` etc.
#[derive(Debug, Copy, Clone, Eq, PartialEq)]
pub enum SagaEventKind {
    StepCompleted,
    StepFailed,
    StepCompensated,
    SagaCompleted,
    SagaAborted,
}

impl SagaEventKind {
    fn topic(self) -> &'static str {
        match self {
            SagaEventKind::StepCompleted => "saga.step.completed",
            SagaEventKind::StepFailed => "saga.step.failed",
            SagaEventKind::StepCompensated => "saga.step.compensated",
            SagaEventKind::SagaCompleted => "saga.completed",
            SagaEventKind::SagaAborted => "saga.aborted",
        }
    }

    fn discriminator(self) -> &'static str {
        match self {
            SagaEventKind::StepCompleted => "step.completed",
            SagaEventKind::StepFailed => "step.failed",
            SagaEventKind::StepCompensated => "step.compensated",
            SagaEventKind::SagaCompleted => "saga.completed",
            SagaEventKind::SagaAborted => "saga.aborted",
        }
    }
}

/// In-memory record of an outbox event the runner emitted. Surfaced
/// via [`SagaRunner::events`] for tests and for callers that want to
/// react before `tx.commit()`.
#[derive(Debug, Clone)]
pub struct SagaEvent {
    pub kind: SagaEventKind,
    pub step_name: Option<String>,
    pub event_id: Uuid,
}

// ─── Saga status ──────────────────────────────────────────────────────────

/// Persisted state of the saga — mirrors the `status` column of
/// `saga.state`.
#[derive(Debug, Copy, Clone, Eq, PartialEq)]
pub enum SagaStatus {
    Running,
    Completed,
    Failed,
    Compensated,
    Aborted,
}

impl SagaStatus {
    fn as_str(self) -> &'static str {
        match self {
            SagaStatus::Running => "running",
            SagaStatus::Completed => "completed",
            SagaStatus::Failed => "failed",
            SagaStatus::Compensated => "compensated",
            SagaStatus::Aborted => "aborted",
        }
    }

    fn from_str(s: &str) -> Self {
        match s {
            "completed" => SagaStatus::Completed,
            "failed" => SagaStatus::Failed,
            "compensated" => SagaStatus::Compensated,
            "aborted" => SagaStatus::Aborted,
            _ => SagaStatus::Running,
        }
    }

    fn is_terminal(self) -> bool {
        !matches!(self, SagaStatus::Running)
    }
}

// ─── Compensation chain ───────────────────────────────────────────────────

type CompensationFn =
    Box<dyn FnOnce() -> BoxFuture<'static, Result<(), SagaError>> + Send + 'static>;

struct Compensation {
    step_name: &'static str,
    invoke: CompensationFn,
}

// ─── SagaRunner ───────────────────────────────────────────────────────────

/// Driver for one saga instance. Owns the surrounding `Transaction`
/// for the lifetime of the saga so all step bookkeeping and outbox
/// emissions land atomically with the application's primary writes
/// when the caller commits.
pub struct SagaRunner<'a, 'tx> {
    tx: &'a mut Transaction<'tx, Postgres>,
    saga_id: Uuid,
    name: String,
    status: SagaStatus,
    completed_steps: Vec<String>,
    completed_set: HashSet<String>,
    step_outputs: serde_json::Map<String, serde_json::Value>,
    compensations: Vec<Compensation>,
    events: Vec<SagaEvent>,
}

impl<'a, 'tx> SagaRunner<'a, 'tx> {
    /// Start (or resume) a saga identified by `saga_id`. If the row
    /// already exists in `saga.state` its `completed_steps` and
    /// cached outputs are loaded so subsequent calls to
    /// [`execute_step`](Self::execute_step) become no-ops for the
    /// already-finished prefix. If the row is missing it is inserted
    /// with status `running`.
    ///
    /// Returns an error if the persisted status is terminal — the
    /// caller must not retry a saga that already failed/completed.
    pub async fn start(
        tx: &'a mut Transaction<'tx, Postgres>,
        saga_id: Uuid,
        name: impl Into<String>,
    ) -> Result<SagaRunner<'a, 'tx>, SagaError> {
        let name = name.into();

        // UPSERT-on-first-call. We use INSERT … ON CONFLICT DO
        // NOTHING then SELECT so we can read the persisted state
        // regardless of whether this transaction created the row.
        sqlx::query(
            "INSERT INTO saga.state (saga_id, name) \
             VALUES ($1, $2) \
             ON CONFLICT (saga_id) DO NOTHING",
        )
        .bind(saga_id)
        .bind(&name)
        .execute(&mut **tx)
        .await?;

        let row = sqlx::query(
            "SELECT status, completed_steps, step_outputs FROM saga.state WHERE saga_id = $1",
        )
        .bind(saga_id)
        .fetch_one(&mut **tx)
        .await?;

        let status_str: String = row.try_get("status")?;
        let status = SagaStatus::from_str(&status_str);
        if status.is_terminal() {
            return Err(SagaError::Terminal(status_str));
        }
        let completed_steps: Vec<String> = row.try_get("completed_steps")?;
        let step_outputs_json: serde_json::Value = row.try_get("step_outputs")?;
        let step_outputs = match step_outputs_json {
            serde_json::Value::Object(map) => map,
            _ => serde_json::Map::new(),
        };
        let completed_set: HashSet<String> = completed_steps.iter().cloned().collect();

        Ok(SagaRunner {
            tx,
            saga_id,
            name,
            status: SagaStatus::Running,
            completed_steps,
            completed_set,
            step_outputs,
            compensations: Vec::new(),
            events: Vec::new(),
        })
    }

    /// Saga aggregate id.
    pub fn saga_id(&self) -> Uuid {
        self.saga_id
    }

    /// Saga name (the one passed to [`SagaRunner::start`]).
    pub fn name(&self) -> &str {
        &self.name
    }

    /// Current status. Always `Running` until [`finish`](Self::finish)
    /// or [`abort`](Self::abort) is called, or until a step failure
    /// transitions the saga to `Failed`/`Compensated`.
    pub fn status(&self) -> SagaStatus {
        self.status
    }

    /// In-memory record of every outbox event emitted by this runner
    /// so far. Useful for tests and for callers that want to react in
    /// the same transaction before commit.
    pub fn events(&self) -> &[SagaEvent] {
        &self.events
    }

    /// Names of every step that has succeeded so far, in order.
    pub fn completed_steps(&self) -> &[String] {
        &self.completed_steps
    }

    /// Execute step `S` with the given input.
    ///
    /// * If `S::step_name()` is already present in
    ///   `completed_steps` this call is an idempotent replay: the
    ///   cached output is deserialised and returned, no event is
    ///   emitted, no compensation is recorded.
    /// * On success the runner persists the step's name + output to
    ///   `saga.state`, registers a compensation closure and emits a
    ///   `saga.step.completed` outbox event.
    /// * On failure the runner emits a `saga.step.failed` event and
    ///   then runs every previously-recorded compensation in LIFO
    ///   order, emitting a `saga.step.compensated` event per success.
    ///   The saga ends in status `failed` (no compensations ran) or
    ///   `compensated` (at least one ran). Compensation errors are
    ///   logged but do not stop the chain — they are aggregated into
    ///   the returned [`SagaError`] only if the original step's error
    ///   was already a [`SagaError::Step`].
    pub async fn execute_step<S: SagaStep>(
        &mut self,
        input: S::Input,
    ) -> Result<S::Output, SagaError> {
        if self.status.is_terminal() {
            return Err(SagaError::Terminal(self.status.as_str().to_string()));
        }
        let step_name = S::step_name();

        // Idempotent replay path.
        if self.completed_set.contains(step_name) {
            let cached =
                self.step_outputs
                    .get(step_name)
                    .cloned()
                    .ok_or_else(|| SagaError::Step {
                        step: step_name.to_string(),
                        message: "step marked complete but no cached output".to_string(),
                    })?;
            let output: S::Output = serde_json::from_value(cached)?;
            tracing::debug!(
                saga_id = %self.saga_id,
                step = step_name,
                "saga: skipping already-completed step on idempotent retry"
            );
            return Ok(output);
        }

        // Mark the step as in-flight before executing so an interrupted
        // process can later see exactly where it was. This is its own
        // sub-update inside the caller's transaction.
        sqlx::query(
            "UPDATE saga.state SET current_step = $1, updated_at = now() WHERE saga_id = $2",
        )
        .bind(step_name)
        .bind(self.saga_id)
        .execute(&mut **self.tx)
        .await?;

        // Run the user code. The step is a pure async fn (no tx
        // access) by contract — that keeps every external side-effect
        // outside the database transaction, where it belongs.
        let input_for_compensation = input.clone();
        let result = S::execute(input).await;

        match result {
            Ok(output) => {
                let output_json = serde_json::to_value(&output)?;
                let event_id = self
                    .emit(
                        SagaEventKind::StepCompleted,
                        Some(step_name),
                        &serde_json::json!({
                            "saga_id": self.saga_id,
                            "saga": self.name,
                            "step": step_name,
                            "output": output_json,
                        }),
                    )
                    .await?;

                sqlx::query(
                    "UPDATE saga.state \
                     SET completed_steps = array_append(completed_steps, $1), \
                         step_outputs    = step_outputs || jsonb_build_object($1::text, $2::jsonb), \
                         current_step    = NULL, \
                         updated_at      = now() \
                     WHERE saga_id = $3",
                )
                .bind(step_name)
                .bind(&output_json)
                .bind(self.saga_id)
                .execute(&mut **self.tx)
                .await?;

                self.completed_steps.push(step_name.to_string());
                self.completed_set.insert(step_name.to_string());
                self.step_outputs.insert(step_name.to_string(), output_json);

                // Register the compensation closure for this step.
                let compensate: CompensationFn = Box::new(move || {
                    let fut: BoxFuture<'static, Result<(), SagaError>> =
                        Box::pin(async move { S::compensate(input_for_compensation).await });
                    fut
                });
                self.compensations.push(Compensation {
                    step_name,
                    invoke: compensate,
                });

                tracing::debug!(
                    saga_id = %self.saga_id,
                    step = step_name,
                    %event_id,
                    "saga: step completed"
                );
                Ok(output)
            }
            Err(err) => {
                // Emit the failure event, then run compensations.
                let _ = self
                    .emit(
                        SagaEventKind::StepFailed,
                        Some(step_name),
                        &serde_json::json!({
                            "saga_id": self.saga_id,
                            "saga": self.name,
                            "step": step_name,
                            "error": err.to_string(),
                        }),
                    )
                    .await?;

                let ran_any = self.run_compensations().await?;

                let new_status = if ran_any {
                    SagaStatus::Compensated
                } else {
                    SagaStatus::Failed
                };
                self.set_status(new_status, Some(step_name)).await?;
                Err(err)
            }
        }
    }

    /// Mark the saga as `completed` and emit a `saga.completed`
    /// outbox event. Must be called after every step succeeded.
    pub async fn finish(&mut self) -> Result<(), SagaError> {
        if self.status.is_terminal() {
            return Err(SagaError::Terminal(self.status.as_str().to_string()));
        }
        self.emit(
            SagaEventKind::SagaCompleted,
            None,
            &serde_json::json!({
                "saga_id": self.saga_id,
                "saga": self.name,
                "completed_steps": self.completed_steps,
            }),
        )
        .await?;
        self.set_status(SagaStatus::Completed, None).await
    }

    /// Abort the saga: run every recorded compensation in LIFO order
    /// and mark the row `aborted`. Use this when the caller decides
    /// to give up before any step has failed.
    pub async fn abort(&mut self) -> Result<(), SagaError> {
        if self.status.is_terminal() {
            return Err(SagaError::Terminal(self.status.as_str().to_string()));
        }
        let _ = self.run_compensations().await?;
        self.emit(
            SagaEventKind::SagaAborted,
            None,
            &serde_json::json!({
                "saga_id": self.saga_id,
                "saga": self.name,
            }),
        )
        .await?;
        self.set_status(SagaStatus::Aborted, None).await
    }

    // ── Internals ────────────────────────────────────────────────────

    async fn run_compensations(&mut self) -> Result<bool, SagaError> {
        let mut ran_any = false;
        // LIFO drain. Take the closures out so we don't double-borrow
        // self while awaiting.
        let compensations = std::mem::take(&mut self.compensations);
        for comp in compensations.into_iter().rev() {
            let step_name = comp.step_name;
            match (comp.invoke)().await {
                Ok(()) => {
                    let _ = self
                        .emit(
                            SagaEventKind::StepCompensated,
                            Some(step_name),
                            &serde_json::json!({
                                "saga_id": self.saga_id,
                                "saga": self.name,
                                "step": step_name,
                            }),
                        )
                        .await?;
                    ran_any = true;
                }
                Err(err) => {
                    // Surface but keep going — the operator should
                    // observe partial-compensation alarms via the
                    // emitted `saga.step.failed` event from the
                    // original failure plus the absence of a
                    // `saga.step.compensated` for this step.
                    tracing::error!(
                        saga_id = %self.saga_id,
                        step = step_name,
                        error = %err,
                        "saga: compensation failed; continuing chain"
                    );
                }
            }
        }
        Ok(ran_any)
    }

    async fn emit(
        &mut self,
        kind: SagaEventKind,
        step: Option<&str>,
        payload: &serde_json::Value,
    ) -> Result<Uuid, SagaError> {
        // Deterministic event_id so duplicate emissions inside the
        // outbox helper become no-ops.
        let id_input = format!(
            "{}:{}:{}",
            self.saga_id,
            step.unwrap_or(""),
            kind.discriminator()
        );
        let event_id = Uuid::new_v5(&Uuid::NAMESPACE_OID, id_input.as_bytes());
        outbox::enqueue(
            self.tx,
            OutboxEvent::new(
                event_id,
                "saga",
                self.saga_id.to_string(),
                kind.topic(),
                payload.clone(),
            ),
        )
        .await?;
        self.events.push(SagaEvent {
            kind,
            step_name: step.map(str::to_string),
            event_id,
        });
        Ok(event_id)
    }

    async fn set_status(
        &mut self,
        status: SagaStatus,
        failed_step: Option<&str>,
    ) -> Result<(), SagaError> {
        sqlx::query(
            "UPDATE saga.state \
             SET status = $1, failed_step = COALESCE($2, failed_step), \
                 current_step = NULL, updated_at = now() \
             WHERE saga_id = $3",
        )
        .bind(status.as_str())
        .bind(failed_step)
        .bind(self.saga_id)
        .execute(&mut **self.tx)
        .await?;
        self.status = status;
        Ok(())
    }
}

// ─── Unit tests (no IO) ───────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn saga_event_kind_topics_are_stable() {
        // These strings show up on the wire — locking them in.
        assert_eq!(SagaEventKind::StepCompleted.topic(), "saga.step.completed");
        assert_eq!(SagaEventKind::StepFailed.topic(), "saga.step.failed");
        assert_eq!(
            SagaEventKind::StepCompensated.topic(),
            "saga.step.compensated"
        );
        assert_eq!(SagaEventKind::SagaCompleted.topic(), "saga.completed");
        assert_eq!(SagaEventKind::SagaAborted.topic(), "saga.aborted");
    }

    #[test]
    fn saga_status_round_trip() {
        for s in [
            SagaStatus::Running,
            SagaStatus::Completed,
            SagaStatus::Failed,
            SagaStatus::Compensated,
            SagaStatus::Aborted,
        ] {
            assert_eq!(SagaStatus::from_str(s.as_str()), s);
        }
        // Unknown strings collapse to Running so an old row left
        // behind by a buggy writer can still be resumed (the runner
        // will rewrite the column with a known value on first write).
        assert_eq!(SagaStatus::from_str("garbage"), SagaStatus::Running);
        assert!(!SagaStatus::Running.is_terminal());
        assert!(SagaStatus::Completed.is_terminal());
        assert!(SagaStatus::Aborted.is_terminal());
    }

    #[test]
    fn saga_error_constructors() {
        let e = SagaError::step("reserve", "out of stock");
        assert!(matches!(e, SagaError::Step { .. }));
        assert!(e.to_string().contains("reserve"));
        assert!(e.to_string().contains("out of stock"));

        let c = SagaError::compensation("charge", "refund failed");
        assert!(matches!(c, SagaError::Compensation { .. }));
        assert!(c.to_string().contains("charge"));
    }
}
