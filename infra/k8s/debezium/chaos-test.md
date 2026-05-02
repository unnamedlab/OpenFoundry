# S4.1.g — Debezium Connect chaos test

> Gate: must complete with **zero duplicates** and **zero data loss**
> before the `outbox-pg-policy` connector is unpaused in production.

## Objective

Validate that killing a Debezium Connect pod mid-stream does not
produce duplicates or losses on the four
`<domain>.<entity>.<event>.v1` topics. Specifically:

1. Connect's offset commit semantics are at-least-once. Combined
   with deterministic `event_id` (v5 UUID derived from
   `aggregate || aggregate_id || version`), retries must converge
   on the same Kafka record.
2. The producer is configured with `acks=all` +
   `enable.idempotence=true` (see
   [`kafka-connect.yaml`](kafka-connect.yaml)).
3. After restart, Connect resumes from the last committed slot
   offset (replication slot is `failover`-able).

## Setup

* Connect cluster with **2 replicas** running in `kafka` namespace.
* Connector `outbox-pg-policy` **unpaused** in a non-prod environment.
* Test fixture: a load harness that emits *N* = 100 000 events
  through `libs/outbox::enqueue` against `pg-policy` over 5 minutes
  (~330 events/s).
* Each event uses a deterministic `event_id` so the harness can
  validate set-equality on the Kafka side.

## Procedure

1. **T-1 min:** start the load harness. Confirm baseline P50 publish
   latency ≤ 200 ms via `kafka-console-consumer`.
2. **T0:** at the 50 % mark (50 000 events committed in Postgres),
   `kubectl delete pod -n kafka <debezium-pod-0> --grace-period=0`.
   This forces an ungraceful kill — the partner pod must take over
   the running task.
3. **T0+30s:** validate Connect status — surviving pod owns task `0`
   and reports `state=RUNNING`. The dead pod's replacement schedules
   and joins the cluster.
4. **T+5 min:** load harness completes. Wait 60 s for the connector
   to drain the WAL.
5. **Validate (Kafka side):**
   * `kafka-consumer-groups --describe --group connect-debezium`
     shows committed offsets equal to log-end on every partition.
   * Drain each of the 4 topics into a file via `kafkacat -C -e -o
     beginning`. Count total records.
   * **Pass:** total records consumed equals *N* exactly.
   * `event_id` set equality: the Kafka set equals the Postgres tx
     log set (compare via `sort -u`).
6. **Validate (DLQ):**
   * `__dlq.outbox-pg-policy.v1` is empty.
7. **Validate (Postgres):**
   * `outbox.events` row count is **0** (steady state).
   * Replication slot
     `pg_replication_slots WHERE slot_name = 'debezium_outbox_pg_policy'`
     reports `active = true` and `confirmed_flush_lsn` advanced.

## Pass criteria

- Total Kafka records = *N*. No duplicates, no losses.
- DLQ empty.
- Connector recovers `state=RUNNING` within 60 s of the kill.
- `outbox.events` returns to row count 0 within 60 s of the load
  finishing.
- `event_id` set equality between Postgres tx log (preserved by the
  harness) and Kafka consumed payloads.

## Reporting

Save kafkacat output, `connect-rest` status snapshots, and CNPG
`pg_replication_slots` deltas to
`docs/architecture/security/drills/<date>/`. Sign off below.

## Sign-off

- [ ] Platform engineer: ___
- [ ] Data-plane SRE: ___
- [ ] Date: ___
- [ ] Outcome: PASS / FAIL — ___

> A failure (any duplicate, any loss, DLQ non-empty) blocks unpausing
> the production connector and re-runs after remediation.
