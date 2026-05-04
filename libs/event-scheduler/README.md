# `libs/event-scheduler` — Cron-driven Kafka event emitter

ADR-0037 (`docs/architecture/adr/ADR-0037-foundry-pattern-orchestration.md`),
Tarea 1.3 of
[`docs/architecture/migration-plan-foundry-pattern-orchestration.md`](../../docs/architecture/migration-plan-foundry-pattern-orchestration.md).

## What this crate gives you

A `Scheduler` and a `schedules-tick` binary that, together, replace
the in-process tick loops formerly carried by
`automation-operations-service` and `workflow-automation-service` for
time-based triggers. A single Kubernetes `CronJob` runs the binary
once a minute; each invocation publishes one Kafka event per schedule
whose `next_run_at` has elapsed.

## Operating model

1. Operators populate `schedules.definitions` (see
   [`migrations/0001_schedules_definitions.sql`](migrations/0001_schedules_definitions.sql))
   with one row per scheduled trigger — cron expression, IANA time
   zone, Kafka topic, and a verbatim JSON payload to publish.
2. `Scheduler::tick(now)` claims every `enabled` row whose
   `next_run_at <= now` using `SELECT … FOR UPDATE SKIP LOCKED`,
   publishes the payload to its Kafka topic via
   [`event-bus-data`](../event-bus-data/README.md), and updates
   `next_run_at` / `last_run_at` inside the same transaction.
3. The `SKIP LOCKED` clause makes overlapping ticks safe — at most one
   pod ever fires a given row per due instant, so a slow tick (`> 60s`)
   that overlaps the next CronJob run cannot double-publish.

## Cron semantics

Powered by the in-house [`scheduling-cron`](../scheduling-cron/) crate
(Foundry-parity Unix-5 / Quartz-6, IANA TZ, DST-correct). The external
`cron` crate is **not** used so wall-clock and DST behaviour stay
consistent with the rest of the platform.

Each row stores:

* `cron_expr` — e.g. `*/5 * * * *` (Unix-5) or `0 */5 * * * *`
  (Quartz-6).
* `cron_flavor` — `unix5` (default) or `quartz6`.
* `time_zone` — IANA name, e.g. `UTC`, `America/New_York`.

After a fire, `next_run_at` is recomputed as the smallest cron
instant **strictly greater than `now`**. This gives the standard
"skip missed fires" semantic that K8s CronJob users expect: a tick
that runs late doesn't replay every missed slot, it collapses them
into one fire and resumes from the next future slot.

## Schema contract

`migrations/0001_schedules_definitions.sql`:

| column            | purpose                                                      |
| ----------------- | ------------------------------------------------------------ |
| `id`              | PK (`uuid`)                                                  |
| `name`            | unique, used as the Kafka record key + OpenLineage `job_name`|
| `cron_expr`       | raw cron expression                                          |
| `cron_flavor`     | `unix5` or `quartz6`                                         |
| `time_zone`       | IANA zone (default `UTC`)                                    |
| `enabled`         | toggle without deleting (default `true`)                     |
| `topic`           | Kafka topic to publish to                                    |
| `payload_template`| verbatim JSON payload (no templating yet)                    |
| `next_run_at`     | UTC instant of the next fire (bootstrap with `now()`)        |
| `last_run_at`     | UTC instant of the last successful fire                      |
| `created_at`/`updated_at` | bookkeeping                                          |

A partial index `(next_run_at) WHERE enabled` covers the hot tick query.

## Delivery semantics

Each fire is one Kafka record published with `event-bus-data`'s
at-least-once `acks=all` producer. The Kafka **key** is the schedule
`name`, which gives natural per-schedule ordering on the broker. The
OpenLineage `run_id` is a deterministic v5 UUID over
`(name, scheduled_for)` so a re-fire (e.g. an operator manually
reset `next_run_at`) carries an id consumers can de-duplicate
against. If the Kafka publish fails, the surrounding transaction
rolls back and the row remains "due", so the next tick will retry it
— **no fires are silently dropped**.

OpenLineage headers attached to every record:

| header           | value                                              |
| ---------------- | -------------------------------------------------- |
| `ol-namespace`   | `of://schedules`                                   |
| `ol-job-name`    | the schedule `name`                                |
| `ol-run-id`      | `v5(NAMESPACE_OID, "<name>|<scheduled_for_rfc3339>")` |
| `ol-event-time`  | `scheduled_for` (the `next_run_at` we claimed)     |
| `ol-producer`    | `https://github.com/unnamedlab/OpenFoundry/libs/event-scheduler` |

## Example — Rust

```rust,ignore
use std::sync::Arc;
use chrono::Utc;
use event_scheduler::Scheduler;
use event_scheduler::event_bus_data::KafkaPublisher;
use sqlx::postgres::PgPoolOptions;

let pg = PgPoolOptions::new().connect(&std::env::var("DATABASE_URL")?).await?;
let publisher = KafkaPublisher::from_env("schedules-tick")?;
let scheduler = Scheduler::new(pg, Arc::new(publisher));

let fired = scheduler.tick(Utc::now()).await?;
println!("fired {fired} schedules");
```

## Example — K8s CronJob

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: schedules-tick
spec:
  schedule: "* * * * *"          # every minute
  concurrencyPolicy: Forbid       # belt-and-braces; SKIP LOCKED is the real safety net
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 3
  jobTemplate:
    spec:
      activeDeadlineSeconds: 120  # well under the 60 s overlap guard
      template:
        spec:
          restartPolicy: OnFailure
          containers:
            - name: schedules-tick
              image: ghcr.io/unnamedlab/openfoundry/schedules-tick:latest
              env:
                - name: DATABASE_URL
                  valueFrom: { secretKeyRef: { name: scheduler-pg, key: url } }
                - name: KAFKA_BOOTSTRAP_SERVERS
                  value: "kafka.kafka.svc:9092"
                - name: RUST_LOG
                  value: "info"
```

## Layout

| File | Purpose |
| --- | --- |
| `migrations/0001_schedules_definitions.sql` | DDL for `schedules.definitions`. |
| `src/lib.rs` | `Scheduler`, `ScheduleDefinition`, `SchedulerError`, `compute_next_fire`, `build_lineage`. |
| `src/bin/schedules-tick.rs` | Single-shot CLI invoked by the K8s CronJob. |
| `tests/scheduler_test.rs` | Postgres + Kafka testcontainer covering due/disabled/future filtering, multi-fire ticks, deterministic OpenLineage `run_id`, and the `SKIP LOCKED` concurrency invariant. Gated by `--features it`. |

## Running the tests

```sh
# Unit (no IO):
cargo test -p event-scheduler

# Postgres + Kafka testcontainers (Docker required):
cargo test -p event-scheduler --features it -- --test-threads=1
```

`--test-threads=1` is recommended for the integration tests because
each test boots its own Postgres + Kafka pair; running them serially
keeps the host RAM footprint predictable.

## Failure modes

- **Two CronJob pods overlap.** Safe by construction —
  `SELECT … FOR UPDATE SKIP LOCKED` ensures each due row is claimed
  by exactly one tick. The integration test
  `concurrent_ticks_never_double_fire_a_row` enforces this.
- **Tick missed an entire period.** The schedule fires **once**, not
  N times — `next_run_at` is set to the next future slot strictly
  greater than `now`. Use a downstream replay job if you need
  back-fill semantics; this crate intentionally does not catch up.
- **Invalid cron expression / time zone / flavor in the row.** The
  tick aborts before publishing with `SchedulerError::InvalidCron`
  / `InvalidTimeZone` / `UnknownFlavor`, so a bad row never sees
  Kafka traffic. Operators should `enabled = false` the row, fix it,
  and re-enable.
- **Kafka unavailable.** The publish error rolls the per-row
  transaction back; `next_run_at` is unchanged so the next tick
  retries. The pod exits non-zero so the K8s CronJob alerts.
- **Payload mistakenly contains secrets.** This crate publishes
  `payload_template` verbatim and does **not** template/sanitize.
  Don't put secrets in `payload_template`; reference them by id and
  resolve them in the consuming service.
