# `libs/idempotency` — consumer-side deduplication helper

ADR-0038 (`docs/architecture/adr/ADR-0038-event-contract-and-idempotency.md`),
Tarea 1.4 of
[`docs/architecture/migration-plan-foundry-pattern-orchestration.md`](../../docs/architecture/migration-plan-foundry-pattern-orchestration.md).

## Why this crate exists

Every OpenFoundry data-plane consumer reads a Kafka topic delivered
**at-least-once**: any message can show up more than once after a
broker rebalance, a consumer-group restart, or a retry. ADR-0038
mandates that consumers dedupe by `event_id` before applying side
effects. This crate gives them one atomic primitive — `IdempotencyStore`
— so each team doesn't roll its own.

## Trait

```rust,ignore
#[async_trait]
pub trait IdempotencyStore: Send + Sync {
    async fn check_and_record(&self, event_id: Uuid) -> Result<Outcome, IdempotencyError>;
}

pub enum Outcome {
    FirstSeen,        // caller is the unique owner of "first delivery"
    AlreadyProcessed, // caller MUST skip side effects
}
```

`check_and_record` is **atomic**: across all replicas / pods of a
consumer, exactly one call ever returns `FirstSeen` for a given
`event_id`.

## Implementations

| Backend | Type | Pattern |
| --- | --- | --- |
| Postgres | `postgres::PgIdempotencyStore` | `INSERT … ON CONFLICT DO NOTHING RETURNING event_id` (one round-trip, no race window) |
| Cassandra | `cassandra::CassandraIdempotencyStore` | `INSERT … IF NOT EXISTS` LWT @ `LOCAL_SERIAL` (4× a regular write — only LWT gives true check-and-record on Cassandra) |
| In-memory | `MemoryIdempotencyStore` | `HashSet<Uuid>` — unit tests / dev only |

Pick the backend that matches the consumer's storage. If the
consumer is already on Postgres, use the Postgres backend (cheaper
and simpler). If the consumer is Cassandra-native (object-database,
audit-trail, action-log), use the Cassandra backend so the dedup
state lives in the same cluster as the side effects.

Cargo features make each backend opt-in so you only pay for the
driver you use:

```toml
[dependencies]
idempotency = { workspace = true, features = ["postgres"] }
# or
idempotency = { workspace = true, features = ["cassandra"] }
```

## Wrapper — `idempotent(...)`

```rust,ignore
let id = msg.event_id();
let result = idempotent(&store, id, || async {
    apply_side_effects(msg).await
}).await?;

match result {
    Some(value) => { /* first delivery: closure ran, value is its return */ }
    None        => { /* duplicate delivery: closure was skipped */ }
}
```

## Schemas

`migrations/0001_processed_events.sql` (Postgres):

```sql
CREATE SCHEMA IF NOT EXISTS idem;
CREATE TABLE IF NOT EXISTS idem.processed_events (
    event_id     uuid        PRIMARY KEY,
    processed_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS processed_events_processed_at_idx
    ON idem.processed_events (processed_at);
```

`migrations/0001_processed_events.cql` (Cassandra):

```cql
CREATE KEYSPACE IF NOT EXISTS idem
    WITH replication = {'class': 'NetworkTopologyStrategy', 'datacenter1': 1};
CREATE TABLE IF NOT EXISTS idem.processed_events (
    event_id     uuid PRIMARY KEY,
    processed_at timestamp
) WITH default_time_to_live = 2592000;  -- 30 days
```

The schema/table name is a `&'static str` chosen at construction
time, never user-controlled, because neither Postgres nor Cassandra
will bind table names as parameters. Operators can use a
per-consumer table by passing a different `&'static str` to
`PgIdempotencyStore::new` / `CassandraIdempotencyStore::new`.

## Retention

Idempotency rows are not free; they need pruning.

* **Cassandra** — built-in 30-day TTL via `default_time_to_live`.
  Bump it via `ALTER TABLE` if your Kafka topic retention is longer.
* **Postgres** — no TTL, run a nightly
  `DELETE FROM idem.processed_events WHERE processed_at < now() - interval '30 days'`.
  The `processed_at` index supports this sweep.

> **Failure mode (ADR-0038).** The retention window MUST be `≥` the
> source Kafka topic's retention, otherwise an event that is
> redelivered after its idempotency row was pruned will be
> reprocessed.

## Semantics — record-before-process

`check_and_record` records the row **before** the consumer's
business logic runs. This is the only ordering that's safe under
concurrent racers (otherwise two consumers can both observe "not
seen" and both apply the side effects).

Trade-off: a closure that fails after `FirstSeen` does NOT
un-record the row, so the next redelivery will see `AlreadyProcessed`
and skip. Combine with a transactional outbox / saga so a partial
side effect is recoverable. See
`tests::idempotent_wrapper_record_before_process_keeps_id_on_failure`
for the property under test.

## Layout

| File | Purpose |
| --- | --- |
| `migrations/0001_processed_events.sql` | Postgres DDL. |
| `migrations/0001_processed_events.cql` | Cassandra DDL. |
| `src/lib.rs` | `Outcome`, `IdempotencyError`, `IdempotencyStore` trait, `idempotent` wrapper, `MemoryIdempotencyStore`. |
| `src/postgres.rs` | `PgIdempotencyStore` (feature `postgres`). |
| `src/cassandra.rs` | `CassandraIdempotencyStore` (feature `cassandra`). |
| `tests/postgres_it.rs` | Postgres testcontainer (`--features it-postgres`): first/duplicate, concurrent racers, wrapper composition, table independence. |
| `tests/cassandra_it.rs` | Cassandra testcontainer (`--features it-cassandra`, `#[ignore]`): same invariants. |

## Running the tests

```sh
# Unit tests (no IO):
cargo test -p idempotency

# Postgres integration tests (Docker required):
cargo test -p idempotency --features it-postgres -- --test-threads=1

# Cassandra integration tests (Docker + Cassandra image, ~200 MB):
cargo test -p idempotency --features it-cassandra -- --ignored --test-threads=1
```
