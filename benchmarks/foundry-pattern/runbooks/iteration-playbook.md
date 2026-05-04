# Foundry-pattern bench — iteration playbook

When `automate-saga-mix.js` fails a threshold, do **not** loosen the
threshold. Triage in this order.

## 1 — Where is the latency?

The end-to-end metric covers four hops. For each failing run, capture:

| Hop                                | Signal to read                                                    |
|------------------------------------|-------------------------------------------------------------------|
| HTTP submit → Postgres outbox INSERT | `outbox.events` row count growth (`outbox_inserts_total` metric)  |
| Debezium → Kafka                     | `debezium_metrics_milli_seconds_behind_source`                    |
| Kafka → consumer                     | consumer-group lag for the relevant `*.v1` topic                  |
| consumer → state-machine UPDATE      | `state_machine_transition_duration_seconds`                       |

If a single hop dominates the budget, that is where to optimise.

## 2 — Common knobs

- **Outbox poll interval** is fixed at 1 s by Debezium's `poll.interval.ms`.
  Do not lower below 500 ms — the connector starts thrashing.
- **Consumer concurrency** for `workflow-automation-service` is
  controlled by `WORKFLOW_AUTOMATION_KAFKA_CONCURRENCY` (default 4).
  Raise in steps of 2 and watch Postgres connection pool saturation.
- **Saga step retry** caps live in `libs/saga` defaults (3 attempts,
  exponential backoff). A run that compensates is *not* counted as
  `saga_ok` — investigate the dispatcher logs before changing the
  caps.

## 3 — When the SLO targets need to move

Open a PR that updates *both*
[`k6/automate-saga-mix.js`](../k6/automate-saga-mix.js) thresholds and
the table in [`README.md`](../README.md), and includes the run report
that justifies the move. Target tightening is encouraged once you have
30 days of data; loosening requires architectural sign-off.
