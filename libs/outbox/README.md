# `libs/outbox` — Transactional outbox helper

ADR-0022 (`docs/architecture/adr/ADR-0022-transactional-outbox-postgres-debezium.md`).

## What this crate gives you

```rust
use outbox::{enqueue, OutboxEvent};
use serde_json::json;
use uuid::Uuid;

let mut tx = pool.begin().await?;

// 1. Your primary write inside the same transaction:
sqlx::query!("UPDATE entities SET version = $1 WHERE id = $2", v, id)
    .execute(&mut *tx).await?;

// 2. Append the outbound event:
enqueue(
    &mut tx,
    OutboxEvent::new(
        Uuid::new_v5(&Uuid::NAMESPACE_OID, format!("{id}:{v}").as_bytes()),
        "entity",
        id.to_string(),
        "entity.changed.v1",
        json!({ "id": id, "version": v }),
    )
    .with_header("ol-run-id", lineage_run_id),
).await?;

// 3. Single commit publishes both:
tx.commit().await?;
```

The helper does an `INSERT ... ON CONFLICT DO NOTHING` followed by a
`DELETE` of the same row inside the caller's transaction. Postgres
WAL captures both records on commit; Debezium emits the INSERT through
the `EventRouter` SMT and drops the DELETE
(`tombstones.on.delete=false`). Net effect: the table stays empty in
steady state and producers never have to think about cleanup.

## Why INSERT+DELETE rather than `outbox.event.deletion.policy=delete`

The migration plan describes the cleanup contract as
`outbox.event.deletion.policy=delete`. That option is **not** part of
the upstream Debezium connector schema. The pattern above is what
Debezium itself documents and what every reference deployment uses;
its semantics are identical (row gone before commit, payload preserved
in the WAL, downstream Kafka topic clean).

## Layout

| File | Purpose |
| --- | --- |
| `migrations/0001_outbox_events.sql` | sqlx-style migration applied by callers. Mirror of `infra/local/postgres-init/02-pg-policy-outbox.sh`. |
| `src/lib.rs` | `OutboxEvent`, `OutboxError`, `enqueue`. |
| `tests/integration.rs` | Postgres testcontainer round-trip. Gated by `--features it-postgres`. |
| `tests/e2e_debezium.sh` | Full handler→Debezium→Kafka E2E against the running compose stack. Validates `ol-*` headers and the EventRouter routing. |

## Running the tests

```sh
# Unit (no IO):
cargo test -p outbox

# Postgres testcontainer:
cargo test -p outbox --features it-postgres -- --include-ignored --test integration

# Full E2E (requires `just dev-up` + kcat/jq/psql/curl on PATH):
./libs/outbox/tests/e2e_debezium.sh
```
