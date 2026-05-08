// Package eventscheduler is the cron-driven Kafka event emitter for
// the Foundry-Schedule trigger pattern (ADR-0037, Tarea 1.3).
//
// Mirrors libs/event-scheduler from the Rust workspace verbatim —
// same operating model, same delivery semantics, same OpenLineage
// constants, same SchedulerError variants, same SQL shape.
//
// # What this package is
//
// A small library that lets a single K8s CronJob pod replace the
// ad-hoc in-process tick loops that services like
// automation-operations-service and workflow-automation-service used
// to run for time-based triggers. The pod runs the schedules-tick
// binary every minute; the binary calls [Scheduler.Tick] once and
// exits with the number of events emitted.
//
// # Operating model
//
//  1. Operators populate schedules.definitions (see
//     migrations/0001_schedules_definitions.sql) with one row per
//     scheduled trigger — cron expression, IANA time zone, Kafka
//     topic, and a verbatim JSON payload to publish.
//  2. [Scheduler.Tick] claims every enabled row whose
//     next_run_at <= now using SELECT … FOR UPDATE SKIP LOCKED,
//     publishes the payload to its topic via libs/event-bus-data, and
//     updates next_run_at / last_run_at inside the same transaction.
//     SKIP LOCKED makes overlapping ticks safe — at most one runner
//     ever fires a given row per due instant.
//  3. The runner relies on the in-house libs/scheduling-cron parser
//     (Foundry-parity Unix-5 / Quartz-6, IANA TZ, DST-correct), so it
//     matches the rest of the platform's cron semantics rather than
//     the looser semantics of an external cron crate.
//
// # Delivery semantics
//
// Each fire is one Kafka record published with libs/event-bus-data's
// at-least-once acks=all producer. The Kafka key is the schedule
// name, which gives natural per-schedule ordering on the broker;
// the OpenLineage run_id is a deterministic v5 UUID over
// (name, scheduled_for) so a re-fire (e.g. operator manually reset
// next_run_at) carries an id consumers can de-duplicate against. If
// the Kafka publish fails, the surrounding transaction rolls back
// and the row remains "due", so the next tick will retry it; we
// never silently drop fires.
package eventscheduler
