//! Tarea 1.2 — integration tests for the saga choreography helper.
//!
//! Boots a Postgres 16 testcontainer, applies both the outbox
//! migration (so `outbox::enqueue` can write through the saga's
//! event sink) and the saga state migration, then exercises:
//!
//! 1. Happy path: 3 steps execute in order, `saga.state` ends in
//!    `completed` and the runner emitted one `saga.step.completed`
//!    per step plus a `saga.completed`.
//! 2. Failure path: step 2 fails, runner emits `saga.step.failed`
//!    and runs step 1's compensation in LIFO order, ending the saga
//!    in `compensated`.
//! 3. Idempotent retry: re-driving the same `saga_id` skips the
//!    already-completed step and returns its cached output without
//!    invoking `S::execute` a second time.
//! 4. Outbox event_id is deterministic: replaying the same emit
//!    yields the same UUID, so duplicates collapse via the
//!    outbox's `ON CONFLICT DO NOTHING`.
//!
//! Gated by the `it-postgres` feature so plain `cargo test -p saga`
//! stays a no-IO unit run.

#![cfg(feature = "it-postgres")]

use std::sync::{Arc, Mutex};

use saga::{SagaError, SagaEventKind, SagaRunner, SagaStatus, SagaStep, enqueue_outbox_event};
use serde::{Deserialize, Serialize};
use sqlx::{Connection, PgConnection, Row, postgres::PgPoolOptions};
use testcontainers::{
    GenericImage, ImageExt,
    core::{IntoContainerPort, WaitFor},
    runners::AsyncRunner,
};
use uuid::Uuid;

const SAGA_MIGRATION: &str = include_str!("../migrations/0001_saga_state.sql");
const OUTBOX_MIGRATION: &str = include_str!("../../outbox/migrations/0001_outbox_events.sql");

// ─── Test step instrumentation ──────────────────────────────────────────

/// Per-step counters so tests can assert how many times each side of
/// the contract was invoked. Wrapped in `Arc<Mutex<…>>` and stashed in
/// thread-local-style `Lazy` because `SagaStep` is a static trait —
/// implementors are stateless types.
#[derive(Default, Debug, Clone)]
struct CallLog {
    executes: Vec<String>,
    compensates: Vec<String>,
}

fn shared_log() -> &'static Mutex<Arc<Mutex<CallLog>>> {
    use std::sync::OnceLock;
    static CELL: OnceLock<Mutex<Arc<Mutex<CallLog>>>> = OnceLock::new();
    CELL.get_or_init(|| Mutex::new(Arc::new(Mutex::new(CallLog::default()))))
}

fn reset_log() -> Arc<Mutex<CallLog>> {
    let mut slot = shared_log().lock().unwrap();
    *slot = Arc::new(Mutex::new(CallLog::default()));
    slot.clone()
}

fn log() -> Arc<Mutex<CallLog>> {
    shared_log().lock().unwrap().clone()
}

/// Switch that controls whether `ChargeCard::execute` should fail.
fn shared_fail_charge() -> &'static Mutex<bool> {
    use std::sync::OnceLock;
    static CELL: OnceLock<Mutex<bool>> = OnceLock::new();
    CELL.get_or_init(|| Mutex::new(false))
}

fn set_fail_charge(v: bool) {
    *shared_fail_charge().lock().unwrap() = v;
}

fn fail_charge() -> bool {
    *shared_fail_charge().lock().unwrap()
}

// ─── Three demo steps modelling the README "create order" saga ──────────

#[derive(Debug, Clone, Serialize, Deserialize)]
struct ReserveInput {
    sku: String,
    qty: u32,
}
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
struct ReserveOutput {
    reservation_id: Uuid,
}

struct ReserveInventory;

#[async_trait::async_trait]
impl SagaStep for ReserveInventory {
    type Input = ReserveInput;
    type Output = ReserveOutput;
    fn step_name() -> &'static str {
        "reserve_inventory"
    }
    async fn execute(input: Self::Input) -> Result<Self::Output, SagaError> {
        log()
            .lock()
            .unwrap()
            .executes
            .push(format!("reserve:{}:{}", input.sku, input.qty));
        Ok(ReserveOutput {
            reservation_id: Uuid::nil(), // deterministic so cached output equality is testable
        })
    }
    async fn compensate(input: Self::Input) -> Result<(), SagaError> {
        log()
            .lock()
            .unwrap()
            .compensates
            .push(format!("reserve:{}", input.sku));
        Ok(())
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct ChargeInput {
    amount_cents: u32,
}
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
struct ChargeOutput {
    auth_code: String,
}

struct ChargeCard;

#[async_trait::async_trait]
impl SagaStep for ChargeCard {
    type Input = ChargeInput;
    type Output = ChargeOutput;
    fn step_name() -> &'static str {
        "charge_card"
    }
    async fn execute(input: Self::Input) -> Result<Self::Output, SagaError> {
        log()
            .lock()
            .unwrap()
            .executes
            .push(format!("charge:{}", input.amount_cents));
        if fail_charge() {
            return Err(SagaError::step("charge_card", "card declined"));
        }
        Ok(ChargeOutput {
            auth_code: "AUTH-1".to_string(),
        })
    }
    async fn compensate(input: Self::Input) -> Result<(), SagaError> {
        log()
            .lock()
            .unwrap()
            .compensates
            .push(format!("charge:{}", input.amount_cents));
        Ok(())
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct ShipInput {
    address: String,
}
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
struct ShipOutput {
    tracking: String,
}

struct ShipOrder;

#[async_trait::async_trait]
impl SagaStep for ShipOrder {
    type Input = ShipInput;
    type Output = ShipOutput;
    fn step_name() -> &'static str {
        "ship_order"
    }
    async fn execute(input: Self::Input) -> Result<Self::Output, SagaError> {
        log()
            .lock()
            .unwrap()
            .executes
            .push(format!("ship:{}", input.address));
        Ok(ShipOutput {
            tracking: "TRACK-1".to_string(),
        })
    }
    async fn compensate(input: Self::Input) -> Result<(), SagaError> {
        log()
            .lock()
            .unwrap()
            .compensates
            .push(format!("ship:{}", input.address));
        Ok(())
    }
}

// ─── Test harness ────────────────────────────────────────────────────────

async fn boot_pg() -> (testcontainers::ContainerAsync<GenericImage>, sqlx::PgPool) {
    let image = GenericImage::new("postgres", "16-alpine")
        .with_exposed_port(5432.tcp())
        .with_wait_for(WaitFor::message_on_stderr(
            "database system is ready to accept connections",
        ))
        .with_env_var("POSTGRES_USER", "of")
        .with_env_var("POSTGRES_PASSWORD", "of")
        .with_env_var("POSTGRES_DB", "saga_test");

    let pg = image.start().await.expect("start postgres testcontainer");
    let host_port = pg
        .get_host_port_ipv4(5432)
        .await
        .expect("expose host port for postgres");
    let url = format!("postgres://of:of@127.0.0.1:{host_port}/saga_test");

    let mut admin = PgConnection::connect(&url).await.expect("connect admin");
    sqlx::raw_sql(OUTBOX_MIGRATION)
        .execute(&mut admin)
        .await
        .expect("apply outbox migration");
    sqlx::raw_sql(SAGA_MIGRATION)
        .execute(&mut admin)
        .await
        .expect("apply saga migration");
    drop(admin);

    let pool = PgPoolOptions::new()
        .max_connections(4)
        .connect(&url)
        .await
        .expect("connect pool");
    (pg, pool)
}

async fn read_status(pool: &sqlx::PgPool, saga_id: Uuid) -> (String, Vec<String>, Option<String>) {
    let row = sqlx::query(
        "SELECT status, completed_steps, failed_step FROM saga.state WHERE saga_id = $1",
    )
    .bind(saga_id)
    .fetch_one(pool)
    .await
    .expect("read saga.state");
    (
        row.try_get("status").unwrap(),
        row.try_get("completed_steps").unwrap(),
        row.try_get("failed_step").unwrap(),
    )
}

// ─── Tests ───────────────────────────────────────────────────────────────

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn saga_happy_path_three_steps() {
    let (_pg, pool) = boot_pg().await;
    let log_handle = reset_log();
    set_fail_charge(false);

    let saga_id = Uuid::now_v7();
    let mut tx = pool.begin().await.unwrap();
    let mut runner = SagaRunner::start(&mut tx, saga_id, "create_order")
        .await
        .expect("start ok");

    let r = runner
        .execute_step::<ReserveInventory>(ReserveInput {
            sku: "SKU-1".to_string(),
            qty: 2,
        })
        .await
        .expect("reserve ok");
    assert_eq!(r.reservation_id, Uuid::nil());

    let c = runner
        .execute_step::<ChargeCard>(ChargeInput { amount_cents: 1999 })
        .await
        .expect("charge ok");
    assert_eq!(c.auth_code, "AUTH-1");

    let s = runner
        .execute_step::<ShipOrder>(ShipInput {
            address: "1 Foundry St".to_string(),
        })
        .await
        .expect("ship ok");
    assert_eq!(s.tracking, "TRACK-1");

    runner.finish().await.expect("finish ok");

    // Sanity-check the runner's view before commit.
    assert_eq!(runner.status(), SagaStatus::Completed);
    let kinds: Vec<SagaEventKind> = runner.events().iter().map(|e| e.kind).collect();
    assert_eq!(
        kinds,
        vec![
            SagaEventKind::StepCompleted,
            SagaEventKind::StepCompleted,
            SagaEventKind::StepCompleted,
            SagaEventKind::SagaCompleted,
        ]
    );

    tx.commit().await.unwrap();

    // Persisted state.
    let (status, steps, failed) = read_status(&pool, saga_id).await;
    assert_eq!(status, "completed");
    assert_eq!(
        steps,
        vec![
            "reserve_inventory".to_string(),
            "charge_card".to_string(),
            "ship_order".to_string(),
        ]
    );
    assert!(failed.is_none());

    // Each `execute` ran exactly once; no compensations on happy path.
    let log = log_handle.lock().unwrap();
    assert_eq!(log.executes.len(), 3, "{:?}", log.executes);
    assert!(log.compensates.is_empty(), "{:?}", log.compensates);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn saga_failure_runs_compensations_in_lifo_order() {
    let (_pg, pool) = boot_pg().await;
    let log_handle = reset_log();
    set_fail_charge(true);

    let saga_id = Uuid::now_v7();
    let mut tx = pool.begin().await.unwrap();
    let mut runner = SagaRunner::start(&mut tx, saga_id, "create_order")
        .await
        .expect("start ok");

    runner
        .execute_step::<ReserveInventory>(ReserveInput {
            sku: "SKU-1".to_string(),
            qty: 2,
        })
        .await
        .expect("reserve ok");

    let err = runner
        .execute_step::<ChargeCard>(ChargeInput { amount_cents: 1999 })
        .await
        .expect_err("charge must fail");
    match err {
        SagaError::Step { step, .. } => assert_eq!(step, "charge_card"),
        other => panic!("expected SagaError::Step, got {other:?}"),
    }

    // After the failure the runner is in `compensated` (one
    // compensation ran) and emitted: step_completed (reserve),
    // step_failed (charge), step_compensated (reserve).
    assert_eq!(runner.status(), SagaStatus::Compensated);
    let kinds: Vec<SagaEventKind> = runner.events().iter().map(|e| e.kind).collect();
    assert_eq!(
        kinds,
        vec![
            SagaEventKind::StepCompleted,
            SagaEventKind::StepFailed,
            SagaEventKind::StepCompensated,
        ]
    );

    tx.commit().await.unwrap();

    let (status, steps, failed) = read_status(&pool, saga_id).await;
    assert_eq!(status, "compensated");
    assert_eq!(steps, vec!["reserve_inventory".to_string()]);
    assert_eq!(failed.as_deref(), Some("charge_card"));

    let log = log_handle.lock().unwrap();
    // execute: reserve, charge (failed). compensate: reserve only.
    assert_eq!(
        log.executes,
        vec!["reserve:SKU-1:2".to_string(), "charge:1999".to_string()]
    );
    assert_eq!(log.compensates, vec!["reserve:SKU-1".to_string()]);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn saga_idempotent_retry_skips_completed_steps() {
    let (_pg, pool) = boot_pg().await;
    let _log_handle = reset_log();
    set_fail_charge(false);

    let saga_id = Uuid::now_v7();

    // First attempt — completes the first two steps then we
    // deliberately drop the runner before `ship_order` to simulate a
    // crashed process. Commit the partial progress so the next attempt
    // can read it back.
    {
        let mut tx = pool.begin().await.unwrap();
        let mut runner = SagaRunner::start(&mut tx, saga_id, "create_order")
            .await
            .expect("start ok");
        runner
            .execute_step::<ReserveInventory>(ReserveInput {
                sku: "SKU-1".to_string(),
                qty: 2,
            })
            .await
            .expect("reserve ok");
        runner
            .execute_step::<ChargeCard>(ChargeInput { amount_cents: 1999 })
            .await
            .expect("charge ok");
        tx.commit().await.unwrap();
    }

    // Second attempt — both completed steps must be skipped, the ship
    // step runs, then finish().
    let executes_before = log().lock().unwrap().executes.len();
    {
        let mut tx = pool.begin().await.unwrap();
        let mut runner = SagaRunner::start(&mut tx, saga_id, "create_order")
            .await
            .expect("resume ok");
        // The runner's view must reflect the persisted progress.
        assert_eq!(runner.completed_steps().len(), 2);

        // Idempotent replays: the cached outputs come back.
        let r = runner
            .execute_step::<ReserveInventory>(ReserveInput {
                sku: "SKU-1".to_string(),
                qty: 2,
            })
            .await
            .expect("reserve replay ok");
        assert_eq!(r.reservation_id, Uuid::nil());

        let c = runner
            .execute_step::<ChargeCard>(ChargeInput { amount_cents: 1999 })
            .await
            .expect("charge replay ok");
        assert_eq!(c.auth_code, "AUTH-1");

        // Replays must NOT have invoked `execute` a second time.
        assert_eq!(
            log().lock().unwrap().executes.len(),
            executes_before,
            "idempotent replay must not re-invoke execute()",
        );

        runner
            .execute_step::<ShipOrder>(ShipInput {
                address: "1 Foundry St".to_string(),
            })
            .await
            .expect("ship ok");
        runner.finish().await.expect("finish ok");
        tx.commit().await.unwrap();
    }

    let (status, steps, _failed) = read_status(&pool, saga_id).await;
    assert_eq!(status, "completed");
    assert_eq!(steps.len(), 3);

    // After completion, starting again must refuse — terminal state.
    let mut tx = pool.begin().await.unwrap();
    let outcome = SagaRunner::start(&mut tx, saga_id, "create_order").await;
    let err = match outcome {
        Ok(_) => panic!("must refuse terminal saga"),
        Err(e) => e,
    };
    assert!(matches!(err, SagaError::Terminal(_)), "{err:?}");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn enqueue_outbox_event_helper_is_deterministic() {
    let (_pg, pool) = boot_pg().await;

    let mut tx = pool.begin().await.unwrap();
    let id_1 = enqueue_outbox_event(
        &mut tx,
        "saga",
        "agg-1",
        "saga.completed",
        &serde_json::json!({"foo": 1}),
    )
    .await
    .expect("enqueue 1");
    let id_2 = enqueue_outbox_event(
        &mut tx,
        "saga",
        "agg-1",
        "saga.completed",
        &serde_json::json!({"foo": 1}),
    )
    .await
    .expect("enqueue 2 (duplicate)");
    assert_eq!(
        id_1, id_2,
        "deterministic v5 event_id makes duplicate enqueue a no-op"
    );

    let id_3 = enqueue_outbox_event(
        &mut tx,
        "saga",
        "agg-1",
        "saga.completed",
        &serde_json::json!({"foo": 2}),
    )
    .await
    .expect("enqueue 3 different payload");
    assert_ne!(
        id_1, id_3,
        "different payload must yield different event_id"
    );

    tx.commit().await.unwrap();
}
