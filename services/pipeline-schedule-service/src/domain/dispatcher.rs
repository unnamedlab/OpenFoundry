//! Run dispatcher.
//!
//! Brings together the trigger engine, the build-service client, the
//! `schedule_runs` ledger, and the auto-pause supervisor. Called from
//!
//!   * the trigger evaluator's listener after `observe()` returns
//!     `Satisfied` (Event / Compound triggers);
//!   * the manual `:run-now` endpoint;
//!   * Temporal Schedule callbacks (Time triggers, in P3+).
//!
//! Maps the build-service response to the three Foundry-doc outcomes
//! (Succeeded / Ignored / Failed), persists a `schedule_runs` row,
//! resets event observations on success/ignored, and triggers the
//! auto-pause supervisor on failure.
//!
//! Concurrency note: `pending_re_run` + `active_run_id` implement the
//! doc-mandated coalesce — "If a schedule is triggered while the
//! previous run is still in action, it will remain triggered and run
//! only after the previous schedule is finished".

use std::{collections::BTreeMap, sync::Arc};

use chrono::Utc;
use sqlx::PgPool;
use uuid::Uuid;

use crate::domain::{
    build_client::{BuildAttemptOutcome, BuildServiceClient, CreateBuildPayload, RunAsPrincipal},
    notification_client::{AutoPausedAlert, NotificationClient},
    run_store::{self, InsertRun, RunOutcome, ScheduleRun},
    schedule_store,
    trigger::{AUTO_PAUSED_REASON, Schedule, ScheduleScopeKind, ScheduleTargetKind},
};

/// Tunables surfaced via `[auto_pause]` block in
/// `services/pipeline-schedule-service/config/*.toml`.
#[derive(Debug, Clone, Copy)]
pub struct AutoPauseConfig {
    pub enabled: bool,
    pub consecutive_failures_threshold: i64,
}

impl Default for AutoPauseConfig {
    fn default() -> Self {
        Self {
            enabled: true,
            consecutive_failures_threshold: 5,
        }
    }
}

/// Outbound URL the auto-pause notification's deep-link points to.
#[derive(Debug, Clone)]
pub struct DispatcherConfig {
    pub auto_pause: AutoPauseConfig,
    pub schedule_link_template: String,
}

impl Default for DispatcherConfig {
    fn default() -> Self {
        Self {
            auto_pause: AutoPauseConfig::default(),
            schedule_link_template: "/build-schedules/{rid}".to_string(),
        }
    }
}

/// Reason the dispatcher was woken up.
#[derive(Debug, Clone)]
pub enum DispatchTrigger {
    /// Cron / Time trigger fired. The optional `user_jwt` is the
    /// caller's token when a USER-mode schedule fires manually
    /// (Time triggers fired by Temporal carry no user JWT — the
    /// dispatcher then uses the run-as user's stored token).
    Cron {
        fired_at: chrono::DateTime<chrono::Utc>,
    },
    /// Compound / Event trigger satisfied — the listener tells us
    /// which event leaves matched.
    Event { events: Vec<(String, String)> },
    /// Manual `:run-now`. Carries the caller's JWT for USER mode.
    Manual {
        requested_by: Uuid,
        user_jwt: Option<String>,
    },
    /// Re-run flushed after the previous run finished (coalesce).
    PendingReRun,
}

impl DispatchTrigger {
    fn as_snapshot(&self) -> BTreeMap<String, String> {
        let mut out = BTreeMap::new();
        match self {
            DispatchTrigger::Cron { fired_at } => {
                out.insert("kind".into(), "cron".into());
                out.insert("fired_at".into(), fired_at.to_rfc3339());
            }
            DispatchTrigger::Event { events } => {
                out.insert("kind".into(), "event".into());
                let joined = events
                    .iter()
                    .map(|(p, t)| format!("{p}={t}"))
                    .collect::<Vec<_>>()
                    .join(";");
                out.insert("events".into(), joined);
            }
            DispatchTrigger::Manual { requested_by, .. } => {
                out.insert("kind".into(), "manual".into());
                out.insert("requested_by".into(), requested_by.to_string());
            }
            DispatchTrigger::PendingReRun => {
                out.insert("kind".into(), "pending_re_run".into());
            }
        }
        out
    }

    /// Caller-supplied JWT when the trigger source carries one.
    /// Cron and PendingReRun triggers don't carry a JWT — the
    /// dispatcher falls back to the schedule's own run-as identity.
    fn user_jwt(&self) -> Option<&str> {
        match self {
            DispatchTrigger::Manual { user_jwt, .. } => user_jwt.as_deref(),
            _ => None,
        }
    }
}

#[derive(Debug, thiserror::Error)]
pub enum DispatchError {
    #[error("schedule store error: {0}")]
    Store(#[from] schedule_store::StoreError),
    #[error("run store error: {0}")]
    RunStore(#[from] run_store::RunStoreError),
}

/// Result of a single `dispatch` call. Carries the outcome row plus
/// a flag indicating whether the run was coalesced (deferred to a
/// later flush) so the caller can decide whether to await a follow-up.
#[derive(Debug, Clone)]
pub struct DispatchReport {
    pub run: Option<ScheduleRun>,
    pub coalesced: bool,
    pub auto_paused: bool,
}

/// The dispatcher itself. Cheap to clone; share between handlers /
/// listener tasks.
#[derive(Clone)]
pub struct Dispatcher {
    pool: PgPool,
    build: Arc<dyn BuildServiceClient>,
    notifications: Arc<dyn NotificationClient>,
    config: DispatcherConfig,
}

impl Dispatcher {
    pub fn new(
        pool: PgPool,
        build: Arc<dyn BuildServiceClient>,
        notifications: Arc<dyn NotificationClient>,
        config: DispatcherConfig,
    ) -> Self {
        Self {
            pool,
            build,
            notifications,
            config,
        }
    }

    /// Run the schedule once. Coalesces if a run is already active.
    pub async fn dispatch(
        &self,
        schedule: &Schedule,
        trigger: DispatchTrigger,
    ) -> Result<DispatchReport, DispatchError> {
        // Coalesce: if a previous run is still in flight, just mark
        // the schedule for re-run and bail out — the post-run hook
        // will pick it up.
        if let Some(active) = schedule.active_run_id {
            tracing::debug!(
                rid = %schedule.rid,
                active_run = %active,
                "previous run still active — coalescing into pending_re_run"
            );
            schedule_store::set_pending_re_run(&self.pool, schedule.id, true).await?;
            return Ok(DispatchReport {
                run: None,
                coalesced: true,
                auto_paused: false,
            });
        }

        // Ignore dispatches against a paused schedule. The pause-
        // resume endpoint handles re-arming.
        if schedule.paused {
            return Ok(DispatchReport {
                run: None,
                coalesced: false,
                auto_paused: false,
            });
        }

        let pipeline_target = match &schedule.target.kind {
            ScheduleTargetKind::PipelineBuild(t) => t.clone(),
            // Sync / HealthCheck / DatasetBuild without a pipeline
            // RID don't fan out to pipeline-build-service. Record a
            // FAILED row so the operator notices the misconfiguration.
            other => {
                let run = run_store::insert_run(
                    &self.pool,
                    InsertRun {
                        schedule_id: schedule.id,
                        outcome: RunOutcome::Failed,
                        build_rid: None,
                        failure_reason: Some(format!(
                            "target kind not supported by dispatcher: {}",
                            describe_target_kind(other)
                        )),
                        trigger_snapshot: trigger.as_snapshot(),
                        schedule_version: schedule.version,
                        finished_now: true,
                    },
                )
                .await?;
                let auto_paused = self.maybe_auto_pause(schedule).await?;
                return Ok(DispatchReport {
                    run: Some(run),
                    coalesced: false,
                    auto_paused,
                });
            }
        };

        // Build an `output_dataset_rids` list. Schedules don't carry
        // explicit outputs; "build the pipeline" is the default
        // behaviour, mirrored by sending the pipeline RID itself.
        let outputs = vec![pipeline_target.pipeline_rid.clone()];
        let payload = CreateBuildPayload::from_target(&pipeline_target, outputs);

        // Allocate a run id up-front so we can mark the schedule
        // "active" before the network call returns.
        let run_id = Uuid::now_v7();
        schedule_store::set_active_run(&self.pool, schedule.id, Some(run_id)).await?;

        // Pick the run-as principal based on scope_kind. For USER
        // mode, prefer the caller's JWT (manual run-now); fall back
        // to the schedule-service's stored run-as JWT (Cron / Event,
        // populated separately when the user authorises the schedule).
        // PROJECT_SCOPED always uses the service principal.
        let principal = self.run_as_principal_for(schedule, &trigger);
        let outcome = match &principal {
            Some(p) => self.build.create_build_as(&payload, p).await,
            None => self.build.create_build(&payload).await,
        };
        let (run_outcome, build_rid, failure_reason) = match outcome {
            BuildAttemptOutcome::Started { build_rid } => {
                (RunOutcome::Succeeded, Some(build_rid), None)
            }
            BuildAttemptOutcome::AllOutputsFresh => (
                RunOutcome::Ignored,
                None,
                Some("all outputs fresh".to_string()),
            ),
            BuildAttemptOutcome::RejectedByService { status, reason } => (
                RunOutcome::Failed,
                None,
                Some(format!("build-service {status}: {reason}")),
            ),
        };

        // SUCCEEDED runs aren't "finished" until the build itself
        // completes downstream — the dispatcher just started one. A
        // separate finishing hook (P3) will set finished_at.
        let finished_now = !matches!(run_outcome, RunOutcome::Succeeded);
        let run = run_store::insert_run(
            &self.pool,
            InsertRun {
                schedule_id: schedule.id,
                outcome: run_outcome,
                build_rid,
                failure_reason,
                trigger_snapshot: trigger.as_snapshot(),
                schedule_version: schedule.version,
                finished_now,
            },
        )
        .await?;

        // Reset event observations after every successful or ignored
        // dispatch — the doc says event triggers stay satisfied
        // "until the entire trigger is satisfied and the schedule is
        // run".
        if matches!(run_outcome, RunOutcome::Succeeded | RunOutcome::Ignored) {
            sqlx::query("DELETE FROM schedule_event_observations WHERE schedule_id = $1")
                .bind(schedule.id)
                .execute(&self.pool)
                .await
                .map_err(|e| DispatchError::Store(schedule_store::StoreError::Db(e)))?;
        }

        // For terminal outcomes (FAILED / IGNORED), clear the active-
        // run flag immediately so any pending coalesced re-run can
        // fire. SUCCEEDED stays active until the build finishes.
        if finished_now {
            schedule_store::set_active_run(&self.pool, schedule.id, None).await?;
        }

        let auto_paused = self.maybe_auto_pause(schedule).await?;
        if matches!(run_outcome, RunOutcome::Succeeded) {
            self.maybe_auto_unpause(schedule).await?;
        }

        Ok(DispatchReport {
            run: Some(run),
            coalesced: false,
            auto_paused,
        })
    }

    /// Inspect the last `threshold` runs and auto-pause if every one
    /// was FAILED. Skipped when:
    ///   * `auto_pause.enabled = false`
    ///   * the schedule is `auto_pause_exempt`
    ///   * the schedule is already paused (idempotent)
    async fn maybe_auto_pause(&self, schedule: &Schedule) -> Result<bool, DispatchError> {
        if !self.config.auto_pause.enabled || schedule.auto_pause_exempt || schedule.paused {
            return Ok(false);
        }
        let threshold = self.config.auto_pause.consecutive_failures_threshold;
        if threshold <= 0 {
            return Ok(false);
        }
        let outcomes = run_store::last_outcomes(&self.pool, schedule.id, threshold).await?;
        if outcomes.len() < threshold as usize {
            return Ok(false);
        }
        if !outcomes.iter().all(|o| *o == RunOutcome::Failed) {
            return Ok(false);
        }

        let updated =
            schedule_store::set_paused(&self.pool, &schedule.rid, true, Some(AUTO_PAUSED_REASON))
                .await?;

        // Pull the failed run rids + last reason for the alert.
        let (last_reason, run_rids) = self.last_failed_summary(schedule.id, threshold).await?;
        let link = self
            .config
            .schedule_link_template
            .replace("{rid}", &updated.rid);
        self.notifications
            .send_auto_paused(AutoPausedAlert {
                user_id: parse_uuid_owner(&updated.created_by),
                schedule_rid: updated.rid.clone(),
                schedule_name: updated.name.clone(),
                last_failure_reason: last_reason,
                failed_run_rids: run_rids,
                link,
            })
            .await;

        tracing::warn!(
            rid = %updated.rid,
            threshold,
            "schedule auto-paused after consecutive failures"
        );
        Ok(true)
    }

    /// Re-arm a previously auto-paused schedule on first success.
    /// No-op when the schedule is paused for any other reason or
    /// not paused at all.
    async fn maybe_auto_unpause(&self, schedule: &Schedule) -> Result<bool, DispatchError> {
        if !schedule.paused {
            return Ok(false);
        }
        if schedule.paused_reason.as_deref() != Some(AUTO_PAUSED_REASON) {
            return Ok(false);
        }
        schedule_store::set_paused(&self.pool, &schedule.rid, false, None).await?;
        tracing::info!(rid = %schedule.rid, "schedule auto-unpaused after success");
        Ok(true)
    }

    /// Pick the [`RunAsPrincipal`] propagated to pipeline-build-service.
    ///
    /// USER mode: forwards the caller's JWT when the trigger carries
    /// one (manual run-now), or `None` when the trigger source is
    /// Cron / Event — the build-service then falls back to whatever
    /// service-level token the dispatcher itself was configured with,
    /// which preserves the legacy behaviour from P2.
    ///
    /// PROJECT_SCOPED mode: emits a `ServicePrincipalToken` keyed on
    /// the schedule's `service_principal_id`. The actual token mint
    /// is delegated to the caller via `service_principal_id` —
    /// production deployments wrap that id into a short-lived JWT
    /// signed with the platform's service key, mirroring the
    /// identity-federation-service contract.
    fn run_as_principal_for(
        &self,
        schedule: &Schedule,
        trigger: &DispatchTrigger,
    ) -> Option<RunAsPrincipal> {
        match schedule.scope_kind {
            ScheduleScopeKind::User => trigger
                .user_jwt()
                .map(|t| RunAsPrincipal::UserJwt(t.to_string())),
            ScheduleScopeKind::ProjectScoped => schedule
                .service_principal_id
                .map(|id| RunAsPrincipal::ServicePrincipalToken(id.to_string())),
        }
    }

    async fn last_failed_summary(
        &self,
        schedule_id: Uuid,
        n: i64,
    ) -> Result<(Option<String>, Vec<String>), DispatchError> {
        let runs = run_store::list_for_schedule(
            &self.pool,
            schedule_id,
            run_store::ListRunsFilter {
                outcome: Some(RunOutcome::Failed),
                limit: n,
                offset: 0,
            },
        )
        .await?;
        let last_reason = runs.first().and_then(|r| r.failure_reason.clone());
        let rids = runs.into_iter().map(|r| r.rid).collect();
        Ok((last_reason, rids))
    }

    /// Hook called by the build-finished listener when a SUCCEEDED
    /// run's downstream build finally completes. Marks the run row
    /// finished and flushes any pending re-run.
    pub async fn on_run_finished(
        &self,
        schedule_id: Uuid,
        run_id: Uuid,
    ) -> Result<bool, DispatchError> {
        run_store::finish_run(&self.pool, run_id, Utc::now()).await?;
        schedule_store::set_active_run(&self.pool, schedule_id, None).await?;
        let schedule = schedule_store::get_by_id(&self.pool, schedule_id).await?;
        if !schedule.pending_re_run {
            return Ok(false);
        }
        schedule_store::set_pending_re_run(&self.pool, schedule.id, false).await?;
        // Re-run with the coalesce trigger snapshot — recurse once.
        Box::pin(self.dispatch(&schedule, DispatchTrigger::PendingReRun)).await?;
        Ok(true)
    }
}

fn describe_target_kind(kind: &ScheduleTargetKind) -> &'static str {
    match kind {
        ScheduleTargetKind::PipelineBuild(_) => "pipeline_build",
        ScheduleTargetKind::DatasetBuild(_) => "dataset_build",
        ScheduleTargetKind::SyncRun(_) => "sync_run",
        ScheduleTargetKind::HealthCheck(_) => "health_check",
    }
}

fn parse_uuid_owner(created_by: &str) -> Option<Uuid> {
    Uuid::parse_str(created_by).ok()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn snapshot_event_trigger_records_kind_and_events() {
        let trig = DispatchTrigger::Event {
            events: vec![
                ("compound[0].event".into(), "ri.x".into()),
                ("compound[1].event".into(), "ri.y".into()),
            ],
        };
        let snap = trig.as_snapshot();
        assert_eq!(snap.get("kind"), Some(&"event".to_string()));
        assert!(snap.get("events").unwrap().contains("ri.x"));
        assert!(snap.get("events").unwrap().contains("ri.y"));
    }

    #[test]
    fn snapshot_manual_trigger_records_requested_by() {
        let id = Uuid::nil();
        let snap = DispatchTrigger::Manual {
            requested_by: id,
            user_jwt: None,
        }
        .as_snapshot();
        assert_eq!(snap.get("kind"), Some(&"manual".to_string()));
        assert_eq!(snap.get("requested_by"), Some(&id.to_string()));
    }

    #[test]
    fn auto_pause_config_defaults_to_threshold_5() {
        let cfg = AutoPauseConfig::default();
        assert!(cfg.enabled);
        assert_eq!(cfg.consecutive_failures_threshold, 5);
    }

    #[test]
    fn manual_trigger_user_jwt_round_trips() {
        let trig = DispatchTrigger::Manual {
            requested_by: Uuid::nil(),
            user_jwt: Some("abc.def.ghi".to_string()),
        };
        assert_eq!(trig.user_jwt(), Some("abc.def.ghi"));
        let cron = DispatchTrigger::Cron {
            fired_at: chrono::Utc::now(),
        };
        assert_eq!(cron.user_jwt(), None);
    }

    #[test]
    fn run_as_principal_header_format_matches_contract() {
        let user = RunAsPrincipal::UserJwt("abc".into());
        assert_eq!(user.as_authorization_header(), "Bearer abc");
        let sp = RunAsPrincipal::ServicePrincipalToken("xyz".into());
        assert_eq!(sp.as_authorization_header(), "Bearer sp:xyz");
    }
}
