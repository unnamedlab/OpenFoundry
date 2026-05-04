# `libs/state-machine` — Postgres-backed state machine helper

ADR-0037 — Foundry-pattern orchestration
(`docs/architecture/adr/ADR-0037-foundry-pattern-orchestration.md`).
Tarea 1.1 of
[`docs/architecture/migration-plan-foundry-pattern-orchestration.md`](../../docs/architecture/migration-plan-foundry-pattern-orchestration.md).

## What this crate gives you

A single trait, [`StateMachine`], plus a thin [`PgStore`] that
persists implementors in any service-owned Postgres table whose shape
matches `migrations/0001_state_machine_template.sql`. Atomic
transitions are implemented via optimistic concurrency
(`UPDATE … WHERE id = $1 AND version = $2 RETURNING version`); a
mismatch raises `StoreError::Stale` so the caller can reload and
retry. Timeouts are driven by a queryable `expires_at` column, swept
by `PgStore::timeout_sweep` (typically from a K8s `CronJob`).

```rust
use chrono::{DateTime, Duration, Utc};
use serde::{Deserialize, Serialize};
use state_machine::{PgStore, StateMachine, TransitionError};
use uuid::Uuid;

#[derive(Copy, Clone, Eq, PartialEq, Debug, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
enum ApprovalState { Pending, AwaitingApproval, Approved, Rejected, TimedOut }

#[derive(Debug)]
enum ApprovalEvent { Submit, Approve, Reject, Timeout }

#[derive(Clone, Debug, Serialize, Deserialize)]
struct ApprovalRequest {
    id: Uuid,
    state: ApprovalState,
    deadline: Option<DateTime<Utc>>,
}

impl StateMachine for ApprovalRequest {
    type State = ApprovalState;
    type Event = ApprovalEvent;

    fn aggregate_id(&self) -> Uuid { self.id }
    fn current_state(&self) -> Self::State { self.state }
    fn expires_at(&self) -> Option<DateTime<Utc>> { self.deadline }

    fn state_str(state: Self::State) -> String {
        match state {
            ApprovalState::Pending           => "pending",
            ApprovalState::AwaitingApproval  => "awaiting_approval",
            ApprovalState::Approved          => "approved",
            ApprovalState::Rejected          => "rejected",
            ApprovalState::TimedOut          => "timed_out",
        }.to_string()
    }

    fn transition(mut self, event: Self::Event) -> Result<Self, TransitionError> {
        use ApprovalEvent::*;
        use ApprovalState::*;
        self.state = match (self.state, event) {
            (Pending, Submit) => {
                self.deadline = Some(Utc::now() + Duration::days(7));
                AwaitingApproval
            }
            (AwaitingApproval, Approve) => Approved,
            (AwaitingApproval, Reject)  => Rejected,
            (AwaitingApproval, Timeout) => TimedOut,
            (s, e) => return Err(TransitionError::invalid(
                format!("no transition from {s:?} on {e:?}"))),
        };
        Ok(self)
    }
}

# async fn example(pool: sqlx::PgPool) -> Result<(), state_machine::StoreError> {
let store: PgStore<ApprovalRequest> = PgStore::new(pool, "approvals.requests");

let loaded = store.insert(ApprovalRequest {
    id: Uuid::now_v7(),
    state: ApprovalState::Pending,
    deadline: None,
}).await?;

let submitted = store.apply(loaded, ApprovalEvent::Submit).await?;
assert_eq!(submitted.machine.current_state(), ApprovalState::AwaitingApproval);
# Ok(()) }
```

## Schema contract

Every table backed by `PgStore` carries the column set documented in
`migrations/0001_state_machine_template.sql`:

| column       | purpose                                                       |
| ------------ | ------------------------------------------------------------- |
| `id`         | aggregate identifier (`uuid`, primary key)                    |
| `state`      | textual rendering of `current_state()` (queryable)            |
| `state_data` | full machine serialised as JSON (`jsonb`)                     |
| `version`    | optimistic concurrency token (`bigint`, bumped on every apply)|
| `expires_at` | optional timeout deadline (partial index)                     |
| `created_at` | bookkeeping                                                   |
| `updated_at` | bookkeeping                                                   |

Service migrations should `cp` the template, replace `__table__` with
their concrete name (for example `approvals.requests`) and apply it
through their own `sqlx::migrate!` runner.

## Layout

| File | Purpose |
| --- | --- |
| `migrations/0001_state_machine_template.sql` | Template DDL for service-owned tables. |
| `src/lib.rs` | `StateMachine`, `Loaded`, `PgStore`, `with_retry`, `StoreError`, `TransitionError`. |
| `tests/state_machine_test.rs` | Postgres testcontainer round-trip. Gated by `--features it-postgres`. |

## Running the tests

```sh
# Unit (no IO):
cargo test -p state-machine

# Postgres testcontainer (Docker required):
cargo test -p state-machine --features it-postgres -- --include-ignored
```

## Failure modes

- **Optimistic-lock race.** Two writers loading the same `Loaded`
  token both call `apply`; the second one returns
  `StoreError::Stale { id, expected }`. Use [`with_retry`] (or your
  own loop) to reload the row and resubmit the event.
- **`state_data` schema drift.** If the service ships an enum variant
  that the deployed code does not know how to deserialise, `load` and
  `timeout_sweep` raise `StoreError::InvalidState`. Treat this as an
  operational alert, not a runtime branch — fix the deployment.
- **`expires_at` indexing.** The template ships a partial index
  `WHERE expires_at IS NOT NULL`. Skip the index only if you know the
  table never holds rows with a deadline (then `timeout_sweep` is a
  no-op anyway).
