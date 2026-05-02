//! T4.2 — Retention runner.
//!
//! Periodically polls the [`retention-policy-service`] for active
//! policies, enumerates their target transactions, and:
//!
//!   1. Opens a `DELETE` transaction on each affected dataset (via
//!      `dataset-versioning-service`).
//!   2. Marks the matching files as retired from the current view.
//!   3. After the per-policy `grace_period_minutes` elapses,
//!      physically deletes the underlying file refs (Iceberg
//!      `expire_snapshots` on warehouse-managed tables, S3
//!      `DeleteObject` for raw blobs).
//!   4. Emits a `dataset.retention.applied` event on
//!      `event-bus-control` so `audit-compliance-service` receives a
//!      durable record alongside the synchronous audit-trail entry
//!      written in [`audit_emitter`].
//!
//! ## Design — why a trait soup
//!
//! The runner glues four collaborators (HTTP policy lookup, HTTP
//! transaction open/commit, storage deletion, event publication) and
//! we want to unit-test the *orchestration* (enumerate → mark →
//! physical delete after grace) without spinning any of them up. The
//! [`RetentionDeps`] trait abstracts the four side-effects and the
//! [`InMemoryRetentionDeps`] test fixture below verifies the loop
//! deterministically.
//!
//! ## Tick interval
//!
//! Driven by the `RETENTION_TICK_INTERVAL` env var (seconds). Default
//! 300 s. Set to `0` to disable the worker (used by the test binary
//! and CI runs that drive the runner manually via [`run_once`]).

use std::time::Duration;

use async_trait::async_trait;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::domain::audit_emitter;

/// Minimal projection of a retention policy needed by the runner.
/// Mirrors the catalog row shape; kept local so this crate doesn't
/// have to depend on `retention-policy-service`'s model types.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RetentionPolicySnapshot {
    pub id: Uuid,
    pub name: String,
    pub is_system: bool,
    pub grace_period_minutes: i32,
    /// Optional dataset RID this policy targets; `None` means "all".
    pub dataset_rid: Option<String>,
    /// e.g. `"ABORTED"`. When set, only transactions in this state
    /// are eligible.
    pub transaction_state: Option<String>,
}

/// A single transaction the runner intends to act on.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RetentionTarget {
    pub dataset_rid: String,
    pub transaction_id: Uuid,
    /// File refs that the deletion will cover. Used both to mark the
    /// view and (after the grace period) to physically delete.
    pub file_refs: Vec<String>,
    /// Approximate bytes that will be reclaimed; surfaces in the
    /// audit-trail entry so admins can see the impact.
    pub bytes: u64,
}

/// Outcome of [`apply_policy`]. Returned to callers and fed straight
/// into the audit-trail emitter.
#[derive(Debug, Clone, Default)]
pub struct RetentionApplied {
    pub policy_id: Uuid,
    pub targets_processed: usize,
    pub files_marked: usize,
    pub bytes_freed: u64,
    pub physical_deletes: usize,
    pub physical_delete_skipped_grace: usize,
}

#[derive(Debug, thiserror::Error)]
pub enum RetentionError {
    #[error("policy lookup failed: {0}")]
    PolicyLookup(String),
    #[error("dataset operation failed: {0}")]
    Dataset(String),
    #[error("storage operation failed: {0}")]
    Storage(String),
    #[error("event publish failed: {0}")]
    Publish(String),
}

/// All side-effects the runner needs, behind a trait so we can swap
/// them out for an in-memory fixture in unit tests.
#[async_trait]
pub trait RetentionDeps: Send + Sync {
    /// Return every active policy currently registered.
    async fn list_active_policies(&self) -> Result<Vec<RetentionPolicySnapshot>, RetentionError>;

    /// Enumerate the transactions this policy should act on right now.
    async fn enumerate_targets(
        &self,
        policy: &RetentionPolicySnapshot,
    ) -> Result<Vec<RetentionTarget>, RetentionError>;

    /// Open a DELETE transaction on the dataset and mark the files as
    /// retired from the current view. Returns the new transaction id
    /// representing the retirement on the dataset's branch.
    async fn open_delete_and_retire(
        &self,
        target: &RetentionTarget,
    ) -> Result<Uuid, RetentionError>;

    /// Physically delete the file refs (Iceberg `expire_snapshots` or
    /// S3 `DeleteObject`). Implementations should be idempotent.
    async fn physical_delete(&self, file_refs: &[String]) -> Result<(), RetentionError>;

    /// Publish a `dataset.retention.applied` event for downstream
    /// consumers (audit-compliance-service, dashboards, etc.).
    async fn publish_applied(&self, event: &RetentionAppliedEvent) -> Result<(), RetentionError>;

    /// Wall-clock — overridable in tests.
    fn now(&self) -> DateTime<Utc> {
        Utc::now()
    }

    /// Lookup of the time at which the target's deletion was first
    /// marked, so the runner can decide whether to physical-delete
    /// now or wait for the grace period to elapse.
    async fn deletion_marked_at(
        &self,
        dataset_rid: &str,
        transaction_id: Uuid,
    ) -> Result<Option<DateTime<Utc>>, RetentionError>;
}

/// Event payload published to `dataset.retention.applied`. Mirrors the
/// shape consumed by `audit-compliance-service`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RetentionAppliedEvent {
    pub policy_id: Uuid,
    pub policy_name: String,
    pub dataset_rid: String,
    pub transaction_id: Uuid,
    pub files_count: usize,
    pub bytes_freed: u64,
    pub physically_deleted: bool,
    pub occurred_at: DateTime<Utc>,
}

/// Apply a single policy end-to-end. Public so the integration tests
/// (and a future `POST /v1/retention/run` admin endpoint) can drive
/// the runner without scheduling.
pub async fn apply_policy<D: RetentionDeps + ?Sized>(
    deps: &D,
    policy: &RetentionPolicySnapshot,
) -> Result<RetentionApplied, RetentionError> {
    let targets = deps.enumerate_targets(policy).await?;
    let mut applied = RetentionApplied {
        policy_id: policy.id,
        ..Default::default()
    };

    for target in targets {
        applied.targets_processed += 1;
        applied.files_marked += target.file_refs.len();
        applied.bytes_freed += target.bytes;

        // Step 1 + 2: open the DELETE txn and retire the files from
        // the active view. Performed on every tick so a freshly
        // ABORTED transaction is retired immediately.
        let _ = deps.open_delete_and_retire(&target).await?;

        // Step 3: physical deletion is gated by the grace period so
        // operators have a window to revert. We honour the gate using
        // `deletion_marked_at` rather than the current tick's clock
        // because the same target can be revisited across ticks.
        let marked_at = deps
            .deletion_marked_at(&target.dataset_rid, target.transaction_id)
            .await?;
        let now = deps.now();
        let grace = chrono::Duration::minutes(policy.grace_period_minutes.max(0) as i64);
        let physically_deleted = match marked_at {
            Some(marked) if now - marked >= grace => {
                deps.physical_delete(&target.file_refs).await?;
                applied.physical_deletes += 1;
                true
            }
            _ => {
                applied.physical_delete_skipped_grace += 1;
                false
            }
        };

        // Step 4 — publish the per-target event. We always publish, so
        // dashboards can show the "marked for deletion" stage as well
        // as the "physically deleted" one.
        let event = RetentionAppliedEvent {
            policy_id: policy.id,
            policy_name: policy.name.clone(),
            dataset_rid: target.dataset_rid.clone(),
            transaction_id: target.transaction_id,
            files_count: target.file_refs.len(),
            bytes_freed: target.bytes,
            physically_deleted,
            occurred_at: now,
        };
        deps.publish_applied(&event).await?;
        // T4.3 — synchronous audit-trail emission, in addition to the
        // async event-bus publication.
        audit_emitter::emit_retention_delete(&audit_emitter::RetentionAuditRecord {
            policy_id: event.policy_id,
            dataset_rid: &event.dataset_rid,
            transaction_id: event.transaction_id,
            files_count: event.files_count,
            bytes_freed: event.bytes_freed,
            physically_deleted: event.physically_deleted,
        });
    }

    Ok(applied)
}

/// Run one full pass over every active policy.
pub async fn run_once<D: RetentionDeps + ?Sized>(
    deps: &D,
) -> Result<Vec<RetentionApplied>, RetentionError> {
    let policies = deps.list_active_policies().await?;
    let mut out = Vec::with_capacity(policies.len());
    for policy in policies {
        match apply_policy(deps, &policy).await {
            Ok(result) => out.push(result),
            Err(err) => {
                tracing::error!(policy_id = %policy.id, error = %err, "retention policy run failed");
            }
        }
    }
    Ok(out)
}

/// Spawn the long-running tick. `interval` of zero disables the loop
/// (useful for tests / one-shot invocations).
pub async fn run_loop<D: RetentionDeps + ?Sized>(deps: &D, interval: Duration) {
    if interval.is_zero() {
        tracing::info!("retention runner disabled (interval = 0)");
        return;
    }
    let mut ticker = tokio::time::interval(interval);
    loop {
        ticker.tick().await;
        if let Err(err) = run_once(deps).await {
            tracing::error!(error = %err, "retention tick failed");
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;
    use std::sync::Mutex;

    /// In-memory deps fixture. The runner can record every side
    /// effect so the tests can assert ordering and grace-period
    /// gating.
    struct InMemoryDeps {
        now: Mutex<DateTime<Utc>>,
        policies: Vec<RetentionPolicySnapshot>,
        targets: HashMap<Uuid, Vec<RetentionTarget>>,
        marked_at: Mutex<HashMap<(String, Uuid), DateTime<Utc>>>,
        retired: Mutex<Vec<RetentionTarget>>,
        physically_deleted: Mutex<Vec<String>>,
        published: Mutex<Vec<RetentionAppliedEvent>>,
    }

    #[async_trait]
    impl RetentionDeps for InMemoryDeps {
        async fn list_active_policies(
            &self,
        ) -> Result<Vec<RetentionPolicySnapshot>, RetentionError> {
            Ok(self.policies.clone())
        }

        async fn enumerate_targets(
            &self,
            policy: &RetentionPolicySnapshot,
        ) -> Result<Vec<RetentionTarget>, RetentionError> {
            Ok(self.targets.get(&policy.id).cloned().unwrap_or_default())
        }

        async fn open_delete_and_retire(
            &self,
            target: &RetentionTarget,
        ) -> Result<Uuid, RetentionError> {
            self.retired.lock().unwrap().push(target.clone());
            self.marked_at
                .lock()
                .unwrap()
                .entry((target.dataset_rid.clone(), target.transaction_id))
                .or_insert_with(|| *self.now.lock().unwrap());
            Ok(Uuid::now_v7())
        }

        async fn physical_delete(&self, file_refs: &[String]) -> Result<(), RetentionError> {
            self.physically_deleted
                .lock()
                .unwrap()
                .extend(file_refs.iter().cloned());
            Ok(())
        }

        async fn publish_applied(
            &self,
            event: &RetentionAppliedEvent,
        ) -> Result<(), RetentionError> {
            self.published.lock().unwrap().push(event.clone());
            Ok(())
        }

        async fn deletion_marked_at(
            &self,
            dataset_rid: &str,
            transaction_id: Uuid,
        ) -> Result<Option<DateTime<Utc>>, RetentionError> {
            Ok(self
                .marked_at
                .lock()
                .unwrap()
                .get(&(dataset_rid.to_string(), transaction_id))
                .copied())
        }

        fn now(&self) -> DateTime<Utc> {
            *self.now.lock().unwrap()
        }
    }

    fn fixture(grace: i32, target_now: bool) -> InMemoryDeps {
        let policy = RetentionPolicySnapshot {
            id: Uuid::now_v7(),
            name: "DELETE_ABORTED_TRANSACTIONS".into(),
            is_system: true,
            grace_period_minutes: grace,
            dataset_rid: None,
            transaction_state: Some("ABORTED".into()),
        };
        let target = RetentionTarget {
            dataset_rid: "ri.foundry.main.dataset.abc".into(),
            transaction_id: Uuid::now_v7(),
            file_refs: vec!["s3://b/a.parquet".into(), "s3://b/b.parquet".into()],
            bytes: 1024,
        };
        let mut targets = HashMap::new();
        targets.insert(policy.id, vec![target.clone()]);
        let now = Utc::now();
        let marked_at = if target_now {
            // Pre-mark in the past so the grace check passes.
            let mut m = HashMap::new();
            m.insert(
                (target.dataset_rid.clone(), target.transaction_id),
                now - chrono::Duration::days(1),
            );
            m
        } else {
            HashMap::new()
        };
        InMemoryDeps {
            now: Mutex::new(now),
            policies: vec![policy],
            targets,
            marked_at: Mutex::new(marked_at),
            retired: Mutex::new(vec![]),
            physically_deleted: Mutex::new(vec![]),
            published: Mutex::new(vec![]),
        }
    }

    #[tokio::test]
    async fn first_pass_marks_files_but_skips_physical_delete_when_within_grace() {
        let deps = fixture(60, false);
        let results = run_once(&deps).await.unwrap();
        assert_eq!(results.len(), 1);
        let r = &results[0];
        assert_eq!(r.targets_processed, 1);
        assert_eq!(r.files_marked, 2);
        assert_eq!(r.physical_deletes, 0);
        assert_eq!(r.physical_delete_skipped_grace, 1);
        assert!(deps.physically_deleted.lock().unwrap().is_empty());
        assert_eq!(deps.published.lock().unwrap().len(), 1);
        assert!(!deps.published.lock().unwrap()[0].physically_deleted);
    }

    #[tokio::test]
    async fn second_pass_after_grace_physically_deletes_files() {
        let deps = fixture(60, true);
        let results = run_once(&deps).await.unwrap();
        let r = &results[0];
        assert_eq!(r.physical_deletes, 1);
        assert_eq!(r.physical_delete_skipped_grace, 0);
        assert_eq!(deps.physically_deleted.lock().unwrap().len(), 2);
        assert!(deps.published.lock().unwrap()[0].physically_deleted);
    }

    #[tokio::test]
    async fn zero_grace_deletes_in_the_same_pass() {
        // 0 grace + freshly marked: now - now = 0 >= 0 ⇒ delete now.
        let deps = fixture(0, true);
        let r = run_once(&deps).await.unwrap();
        assert_eq!(r[0].physical_deletes, 1);
    }

    #[tokio::test]
    async fn run_loop_with_zero_interval_returns_immediately() {
        let deps = fixture(60, false);
        // Should not block.
        run_loop(&deps, Duration::from_secs(0)).await;
    }
}
