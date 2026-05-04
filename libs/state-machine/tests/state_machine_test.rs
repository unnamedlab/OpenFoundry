//! Tarea 1.1 — integration tests for `state-machine::PgStore`.
//!
//! Boots a Postgres 16 testcontainer, applies the template migration
//! against a concrete `demo.machines` table, and exercises:
//!
//! 1. Happy path insert → load → apply transitions the machine and
//!    bumps the version monotonically.
//! 2. Optimistic concurrency: a second writer holding a stale
//!    `Loaded` token sees [`StoreError::Stale`] and the row is
//!    untouched.
//! 3. `timeout_sweep` returns rows whose `expires_at` is in the past
//!    and ignores rows with no deadline / future deadlines.
//! 4. The implementor's `transition` veto surfaces as
//!    [`StoreError::Transition`] and the row is not modified.
//!
//! Gated by the `it-postgres` feature so plain `cargo test
//! -p state-machine` stays a no-IO unit run.

#![cfg(feature = "it-postgres")]

use std::time::Duration;

use chrono::{TimeZone, Utc};
use serde::{Deserialize, Serialize};
use sqlx::{Connection, PgConnection, postgres::PgPoolOptions};
use state_machine::{Loaded, PgStore, StateMachine, StoreError, TransitionError, with_retry};
use testcontainers::{
    GenericImage, ImageExt,
    core::{IntoContainerPort, WaitFor},
    runners::AsyncRunner,
};
use uuid::Uuid;

const MIGRATION_TEMPLATE: &str = include_str!("../migrations/0001_state_machine_template.sql");
const TABLE: &str = "demo_machines";

#[derive(Copy, Clone, Eq, PartialEq, Debug, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
enum DemoState {
    Pending,
    AwaitingApproval,
    Approved,
    TimedOut,
}

#[derive(Debug)]
enum DemoEvent {
    Submit,
    Approve,
    Timeout,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
struct DemoMachine {
    id: Uuid,
    state: DemoState,
    deadline: Option<chrono::DateTime<Utc>>,
}

impl DemoMachine {
    fn new(id: Uuid) -> Self {
        Self {
            id,
            state: DemoState::Pending,
            deadline: None,
        }
    }
}

impl StateMachine for DemoMachine {
    type State = DemoState;
    type Event = DemoEvent;

    fn transition(mut self, event: Self::Event) -> Result<Self, TransitionError> {
        use DemoEvent::*;
        use DemoState::*;
        self.state = match (self.state, event) {
            (Pending, Submit) => {
                self.deadline = Some(Utc.with_ymd_and_hms(2099, 1, 1, 0, 0, 0).unwrap());
                AwaitingApproval
            }
            (AwaitingApproval, Approve) => {
                self.deadline = None;
                Approved
            }
            (AwaitingApproval, Timeout) => {
                self.deadline = None;
                TimedOut
            }
            (s, e) => {
                return Err(TransitionError::invalid(format!(
                    "no transition from {s:?} on {e:?}"
                )));
            }
        };
        Ok(self)
    }

    fn current_state(&self) -> Self::State {
        self.state
    }

    fn aggregate_id(&self) -> Uuid {
        self.id
    }

    fn expires_at(&self) -> Option<chrono::DateTime<Utc>> {
        self.deadline
    }

    fn state_str(state: Self::State) -> String {
        match state {
            DemoState::Pending => "pending",
            DemoState::AwaitingApproval => "awaiting_approval",
            DemoState::Approved => "approved",
            DemoState::TimedOut => "timed_out",
        }
        .to_string()
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn pg_store_round_trip_against_real_postgres() {
    let image = GenericImage::new("postgres", "16-alpine")
        .with_exposed_port(5432.tcp())
        .with_wait_for(WaitFor::message_on_stderr(
            "database system is ready to accept connections",
        ))
        .with_env_var("POSTGRES_USER", "of")
        .with_env_var("POSTGRES_PASSWORD", "of")
        .with_env_var("POSTGRES_DB", "state_machine_test");

    let pg = image.start().await.expect("start postgres testcontainer");
    let host_port = pg
        .get_host_port_ipv4(5432)
        .await
        .expect("expose host port for postgres");

    let url = format!("postgres://of:of@127.0.0.1:{host_port}/state_machine_test");

    // Apply the template migration to a real table name.
    let sql = MIGRATION_TEMPLATE.replace("__table__", TABLE);
    let mut admin = PgConnection::connect(&url).await.expect("connect admin");
    sqlx::raw_sql(&sql)
        .execute(&mut admin)
        .await
        .expect("apply state-machine template migration");
    drop(admin);

    let pool = PgPoolOptions::new()
        .max_connections(4)
        .connect(&url)
        .await
        .expect("connect pool");

    let store: PgStore<DemoMachine> = PgStore::new(pool.clone(), TABLE);

    // ── (1) Happy path insert → apply ───────────────────────────────
    let id = Uuid::now_v7();
    let inserted = store.insert(DemoMachine::new(id)).await.expect("insert ok");
    assert_eq!(inserted.version, 1);
    assert_eq!(inserted.machine.current_state(), DemoState::Pending);

    let after_submit = store
        .apply(inserted, DemoEvent::Submit)
        .await
        .expect("submit ok");
    assert_eq!(after_submit.version, 2);
    assert_eq!(
        after_submit.machine.current_state(),
        DemoState::AwaitingApproval
    );

    // load reads back the persisted machine and the bumped version.
    let reloaded = store.load(id).await.expect("load ok");
    assert_eq!(reloaded.version, 2);
    assert_eq!(
        reloaded.machine.current_state(),
        DemoState::AwaitingApproval
    );
    assert!(reloaded.machine.expires_at().is_some());

    // ── (2) Optimistic concurrency ──────────────────────────────────
    // Two readers race; first one wins, second one sees Stale.
    let reader_a = store.load(id).await.expect("reader A load");
    let reader_b = store.load(id).await.expect("reader B load");
    assert_eq!(reader_a.version, reader_b.version);

    let approved = store
        .apply(reader_a, DemoEvent::Approve)
        .await
        .expect("reader A apply ok");
    assert_eq!(approved.version, 3);
    assert_eq!(approved.machine.current_state(), DemoState::Approved);

    let stale_err = store
        .apply(reader_b, DemoEvent::Approve)
        .await
        .expect_err("reader B must lose");
    match stale_err {
        StoreError::Stale {
            id: bad_id,
            expected,
        } => {
            assert_eq!(bad_id, id);
            assert_eq!(expected, 2);
        }
        other => panic!("expected Stale, got {other:?}"),
    }

    // The row was not mutated by the failed write — version is still 3.
    let after_conflict = store.load(id).await.expect("reload after conflict");
    assert_eq!(after_conflict.version, 3);
    assert_eq!(after_conflict.machine.current_state(), DemoState::Approved);

    // ── (3) timeout_sweep ───────────────────────────────────────────
    // Insert one row that has already expired and one that hasn't;
    // verify the sweep returns only the expired one.
    let expired_id = Uuid::now_v7();
    let mut expired = DemoMachine::new(expired_id);
    expired.state = DemoState::AwaitingApproval;
    expired.deadline = Some(Utc.with_ymd_and_hms(2000, 1, 1, 0, 0, 0).unwrap());
    store.insert(expired).await.expect("insert expired ok");

    let future_id = Uuid::now_v7();
    let mut future = DemoMachine::new(future_id);
    future.state = DemoState::AwaitingApproval;
    future.deadline = Some(Utc.with_ymd_and_hms(2099, 12, 31, 23, 59, 0).unwrap());
    store.insert(future).await.expect("insert future ok");

    let now = Utc::now();
    let due = store.timeout_sweep(now).await.expect("sweep ok");
    let due_ids: Vec<Uuid> = due.iter().map(|l| l.machine.aggregate_id()).collect();
    assert!(
        due_ids.contains(&expired_id),
        "expired row must be returned"
    );
    assert!(
        !due_ids.contains(&future_id),
        "future row must not be returned"
    );

    // Apply the timeout event to all due rows.
    for loaded in due {
        let bumped = store
            .apply(loaded, DemoEvent::Timeout)
            .await
            .expect("timeout transition ok");
        assert_eq!(bumped.machine.current_state(), DemoState::TimedOut);
    }

    // After the sweep + apply, the expired row no longer has a
    // deadline, so a second sweep is empty.
    let empty = store
        .timeout_sweep(Utc::now())
        .await
        .expect("second sweep ok");
    let still_due: Vec<Uuid> = empty
        .into_iter()
        .map(|l| l.machine.aggregate_id())
        .collect();
    assert!(
        !still_due.contains(&expired_id),
        "expired row cleared its deadline"
    );

    // ── (4) Invalid transition surfaces as Transition error ────────
    let approved = store.load(id).await.expect("reload approved");
    let err = store
        .apply(approved, DemoEvent::Approve)
        .await
        .expect_err("Approved → Approve must be rejected");
    assert!(matches!(err, StoreError::Transition(_)), "got {err:?}");

    // The row was not mutated by the rejected event.
    let after_invalid = store.load(id).await.expect("reload after invalid");
    assert_eq!(after_invalid.version, 3);
    assert_eq!(after_invalid.machine.current_state(), DemoState::Approved);

    // ── (5) with_retry recovers from a transient Stale conflict ─────
    let id5 = Uuid::now_v7();
    store
        .insert(DemoMachine::new(id5))
        .await
        .expect("insert id5");

    // Inject one stale write by holding an old token, then reload on
    // the second attempt — the helper must succeed.
    use std::sync::atomic::{AtomicU32, Ordering};
    let attempts = AtomicU32::new(0);
    let recovered: Loaded<DemoMachine> = with_retry(3, Duration::from_millis(5), |attempt| {
        let store = store.clone();
        let attempts = &attempts;
        async move {
            attempts.fetch_add(1, Ordering::SeqCst);
            if attempt == 1 {
                // Force a conflict: load fresh, but mutate a competitor first.
                let mine = store.load(id5).await?;
                let competitor = store.load(id5).await?;
                let _winner = store.apply(competitor, DemoEvent::Submit).await?;
                store.apply(mine, DemoEvent::Submit).await
            } else {
                let fresh = store.load(id5).await?;
                // The competitor already moved us to AwaitingApproval; from
                // there a Submit is invalid, so apply the genuine next event
                // (Approve) instead of looping forever.
                store.apply(fresh, DemoEvent::Approve).await
            }
        }
    })
    .await
    .expect("with_retry must eventually succeed");
    assert!(attempts.load(Ordering::SeqCst) >= 2);
    assert_eq!(recovered.machine.current_state(), DemoState::Approved);
}
