# Foundry-pattern orchestration — performance baseline

FASE 11 / Tarea 11.2 of the Foundry-pattern migration. Replaces the
retired Temporal workflow benchmark suite. Establishes the post-cutover
baseline that future regressions are measured against.

## Surfaces under test

| Surface  | HTTP submit                          | Terminal-state poll                                         | Substrate driver                                  |
|----------|--------------------------------------|-------------------------------------------------------------|---------------------------------------------------|
| Automate | `POST /api/v1/workflows/{id}/execute`| `GET /api/v1/workflows/{id}/runs/{run_id}`                  | `workflow-automation-service` state machine + Kafka consumer |
| Saga     | `POST /api/v1/automations`           | `GET /api/v1/automations/{saga_id}`                         | `automation-operations-service` + `libs/saga::SagaRunner`     |

Both endpoints return immediately after the outbox INSERT inside the
serving transaction (see ADR-0038). The end-to-end metric the harness
captures is *submit → terminal state visible to the public API*, which
covers Postgres → Debezium → Kafka → consumer → Postgres state-machine
update.

## SLO targets (post-migration baseline)

| Metric              | Target |
|---------------------|--------|
| `automate_e2e_ms`   | p50 < 500 ms, p95 < 2 s, p99 < 5 s |
| `saga_e2e_ms`       | p50 < 800 ms, p95 < 3 s, p99 < 8 s |
| `automate_ok`       | ≥ 99 % |
| `saga_ok`           | ≥ 98 % |
| `http_req_failed`   | < 1 % |

These are wired as k6 thresholds in
[`k6/automate-saga-mix.js`](k6/automate-saga-mix.js); the run aborts
early if any threshold is breached.

The numbers are intentionally generous on the first iteration — the
goal is to catch regressions, not to gate the migration on a tightened
SLO set that we have no historical data for. Tighten in PRs against
[`runbooks/iteration-playbook.md`](runbooks/iteration-playbook.md), not
here.

## Running

```bash
export OF_BENCH_BASE_URL=https://gateway.dev.openfoundry.local
export OF_BENCH_TOKEN=<bearer JWT with workflows:execute + automations:create>
export OF_BENCH_WORKFLOW_ID=<workflow definition with a no-op effect>
export OF_BENCH_SAGA_TYPE=cleanup_workspace
export OF_BENCH_RPS=50           # automate arrival rate (saga = RPS/5)
export OF_BENCH_DURATION=5m

k6 run --out json=smoke/results/foundry-pattern-bench.json \
  benchmarks/foundry-pattern/k6/automate-saga-mix.js
```

## What this harness does *not* do

- It does not assert audit-log latency. Those events traverse a
  separate Debezium connector + consumer; the smoke scenario at
  [`smoke/scenarios/foundry-pattern-full-flow.json`](../../smoke/scenarios/foundry-pattern-full-flow.json)
  validates correctness; a dedicated bench can be added later if
  ingestion lag becomes a concern.
- It does not exercise compensation paths. The cleanup_workspace saga
  is configured to succeed; chaos is the responsibility of the
  ChaosMesh manifests at
  [`infra/test-tools/chaos/foundry-pattern/`](../../infra/test-tools/chaos/foundry-pattern/).
- It does not replace `benchmarks/ontology/`. That harness measures
  the ontology hot path (read/write mix); this one measures the
  orchestration substrate.
