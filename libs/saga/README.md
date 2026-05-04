# `libs/saga` — Saga choreography helper

ADR-0037 (`docs/architecture/adr/ADR-0037-foundry-pattern-orchestration.md`),
Tarea 1.2 of
[`docs/architecture/migration-plan-foundry-pattern-orchestration.md`](../../docs/architecture/migration-plan-foundry-pattern-orchestration.md).

## What this crate gives you

Two pieces:

* The [`SagaStep`] trait — `execute(input) → Output` plus `compensate(input) → ()`,
  modelled as pure async functions so step bodies live outside the
  database transaction (where their external side-effects belong).
* [`SagaRunner`] — drives a sequence of `SagaStep`s over a caller-owned
  `sqlx::Transaction`, persists progress to `saga.state`, runs LIFO
  compensations on failure, and emits one `saga.step.*` event per
  state transition through the canonical Debezium outbox (see
  [`libs/outbox`](../outbox/README.md)).

Idempotency is built in: re-running a saga with the same `saga_id`
reads back the persisted `completed_steps` and short-circuits each
`execute_step` for already-finished prefix steps, returning the cached
output instead. Outbox event ids are deterministic v5 UUIDs derived
from `(saga_id, step_name, kind)` so duplicate emissions collapse via
the outbox's `ON CONFLICT DO NOTHING` write.

## Example — "create order" saga

`reserve_inventory` → `charge_card` → `ship_order`. Compensations
release the reservation, refund the card, and cancel the shipment in
LIFO order if any later step fails.

```rust
use saga::{SagaError, SagaRunner, SagaStep};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

#[derive(Clone, Serialize, Deserialize)] struct ReserveIn  { sku: String, qty: u32 }
#[derive(Clone, Serialize, Deserialize)] struct ReserveOut { reservation_id: Uuid }

struct ReserveInventory;

#[async_trait::async_trait]
impl SagaStep for ReserveInventory {
    type Input  = ReserveIn;
    type Output = ReserveOut;
    fn step_name() -> &'static str { "reserve_inventory" }
    async fn execute(input: Self::Input) -> Result<Self::Output, SagaError> {
        // call inventory-service…
        Ok(ReserveOut { reservation_id: Uuid::now_v7() })
    }
    async fn compensate(_input: Self::Input) -> Result<(), SagaError> {
        // release the reservation…
        Ok(())
    }
}

# // (ChargeCard / ShipOrder are analogous — see tests/saga_test.rs)
# struct ChargeCard; struct ShipOrder;
# #[derive(Clone, Serialize, Deserialize)] struct ChargeIn  { amount_cents: u32 }
# #[derive(Clone, Serialize, Deserialize)] struct ChargeOut { auth: String }
# #[derive(Clone, Serialize, Deserialize)] struct ShipIn    { address: String }
# #[derive(Clone, Serialize, Deserialize)] struct ShipOut   { tracking: String }
# #[async_trait::async_trait] impl SagaStep for ChargeCard {
#   type Input = ChargeIn; type Output = ChargeOut;
#   fn step_name() -> &'static str { "charge_card" }
#   async fn execute(_i: Self::Input) -> Result<Self::Output, SagaError> { Ok(ChargeOut { auth: "X".into() }) }
#   async fn compensate(_i: Self::Input) -> Result<(), SagaError> { Ok(()) }
# }
# #[async_trait::async_trait] impl SagaStep for ShipOrder {
#   type Input = ShipIn; type Output = ShipOut;
#   fn step_name() -> &'static str { "ship_order" }
#   async fn execute(_i: Self::Input) -> Result<Self::Output, SagaError> { Ok(ShipOut { tracking: "Y".into() }) }
#   async fn compensate(_i: Self::Input) -> Result<(), SagaError> { Ok(()) }
# }

# async fn run(pool: sqlx::PgPool, saga_id: Uuid) -> Result<(), SagaError> {
let mut tx = pool.begin().await?;
let mut saga = SagaRunner::start(&mut tx, saga_id, "create_order").await?;

saga.execute_step::<ReserveInventory>(ReserveIn { sku: "SKU-1".into(), qty: 2 }).await?;
saga.execute_step::<ChargeCard>(ChargeIn { amount_cents: 1999 }).await?;
saga.execute_step::<ShipOrder>(ShipIn { address: "1 Foundry St".into() }).await?;
saga.finish().await?;

tx.commit().await?;
# Ok(()) }
```

If `charge_card` returns `Err`, the runner emits `saga.step.failed.v1`
for `charge_card`, then runs `ReserveInventory::compensate` with the
original input and emits `saga.step.compensated.v1` for it. The saga
ends in status `compensated`. If the process crashes mid-saga and
restarts with the same `saga_id`, the runner reads back
`saga.state.completed_steps` and skips the already-finished prefix.

## Schema contract

`migrations/0001_saga_state.sql`:

| column            | purpose                                                       |
| ----------------- | ------------------------------------------------------------- |
| `saga_id`         | aggregate id (`uuid`, primary key)                            |
| `name`            | human-readable saga type (`"create_order"`, …)                |
| `status`          | `running` / `completed` / `failed` / `compensated` / `aborted`|
| `current_step`    | step currently in flight (`NULL` when idle)                   |
| `completed_steps` | step names that succeeded, in execution order                 |
| `step_outputs`    | JSON object `{ step_name → output_json }` for replay          |
| `failed_step`     | name of the step that raised, if any                          |
| `created_at` / `updated_at` | bookkeeping                                         |

The outbox table (`outbox.events`) must already be provisioned in the
same database — both crates ship their migration as plain `.sql` files
under `migrations/` so a service can apply both with `sqlx::migrate!`.

## Event taxonomy

| topic                          | when                                      |
| ------------------------------ | ----------------------------------------- |
| `saga.step.completed.v1`       | a step's `execute` returned `Ok`          |
| `saga.step.failed.v1`          | a step's `execute` returned `Err`         |
| `saga.step.compensated.v1`     | a previously-completed step was undone    |
| `saga.completed.v1`            | `finish()` ran after every step OK        |
| `saga.aborted.v1`              | `abort()` was called by the caller        |

Inbound topics (consumed by the runtime, not emitted by the runner):

| topic                          | when                                                  |
| ------------------------------ | ----------------------------------------------------- |
| `saga.step.requested.v1`       | a producer asks the runtime to start (or resume) one  |
| `saga.compensate.v1`           | upstream signal asking the runtime to roll back       |

Wire-format payloads for every topic above are pinned in
[`src/events.rs`](src/events.rs) (FASE 6 / Tarea 6.2).

Every event is emitted via `outbox::enqueue` so the application's
primary writes, the saga state update and the event publication all
land atomically with the caller's `tx.commit()`.

## Layout

| File | Purpose |
| --- | --- |
| `migrations/0001_saga_state.sql` | DDL for `saga.state`. |
| `src/lib.rs` | `SagaStep`, `SagaRunner`, `SagaError`, `SagaEventKind`, `enqueue_outbox_event`. |
| `tests/saga_test.rs` | Postgres testcontainer covering happy path / failure / idempotent retry / deterministic outbox id. Gated by `--features it-postgres`. |

## Running the tests

```sh
# Unit (no IO):
cargo test -p saga

# Postgres testcontainer (Docker required):
cargo test -p saga --features it-postgres -- --include-ignored
```

## Failure modes

- **`outbox.events` missing.** The runner calls `outbox::enqueue` on
  every state transition; if the table is absent the saga aborts on
  the very first event with `SagaError::Outbox`. Apply the outbox
  migration alongside `0001_saga_state.sql`.
- **Compensation failures.** The runner does not stop the chain on
  a compensation error — it logs and continues so a misbehaving
  third-party doesn't block the rest of the rollback. Operators
  should monitor for sagas that emitted `saga.step.failed.v1` but
  fewer `saga.step.compensated.v1` events than `completed_steps`.
- **Terminal-state replay.** Calling `SagaRunner::start` for a saga
  that already ended (`completed` / `failed` / `compensated` /
  `aborted`) returns `SagaError::Terminal`. The caller should not
  retry; it must be a new `saga_id`.
