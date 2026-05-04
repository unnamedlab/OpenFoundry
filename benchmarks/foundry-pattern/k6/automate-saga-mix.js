// Foundry-pattern end-to-end latency benchmark — FASE 11 / Tarea 11.2.
//
// Measures the wall-clock latency of the post-migration substrate from
// HTTP submit to terminal-state visibility, across the two hot paths:
//
//   * Automate: POST /workflows/{id}/execute → state-machine row
//     reaches `Completed` (workflow-automation-service consumes the
//     saga.step.requested.v1 event from its outbox via Kafka).
//   * Saga:     POST /automations              → state-machine row
//     reaches `Completed` (automation-operations-service runs the
//     cleanup_workspace 3-step saga via libs/saga::SagaRunner).
//
// We do NOT compare against the retired Temporal baseline directly —
// ADR-0021 has been superseded and the workers-go/ tree is gone. The
// numbers below are the post-migration baseline that future regressions
// are measured against; see `benchmarks/foundry-pattern/runbooks/`.
//
// Run:
//   k6 run --out json=smoke/results/foundry-pattern-bench.json \
//     benchmarks/foundry-pattern/k6/automate-saga-mix.js
//
// Required environment:
//   OF_BENCH_BASE_URL    https://gateway.dev.openfoundry.local
//   OF_BENCH_TOKEN       bearer JWT with workflows:execute + automations:create
//   OF_BENCH_WORKFLOW_ID workflow definition that resolves to a no-op effect
//   OF_BENCH_SAGA_TYPE   default cleanup_workspace
//   OF_BENCH_RPS         sustained arrival rate (default 50)
//   OF_BENCH_DURATION    default 5m

import http from 'k6/http';
import { check, group, fail, sleep } from 'k6';
import { Trend, Rate } from 'k6/metrics';

const BASE_URL = __ENV.OF_BENCH_BASE_URL || fail('OF_BENCH_BASE_URL no definida');
const TOKEN = __ENV.OF_BENCH_TOKEN || fail('OF_BENCH_TOKEN no definida');
const WORKFLOW_ID = __ENV.OF_BENCH_WORKFLOW_ID || fail('OF_BENCH_WORKFLOW_ID no definida');
const SAGA_TYPE = __ENV.OF_BENCH_SAGA_TYPE || 'cleanup_workspace';
const RPS = parseInt(__ENV.OF_BENCH_RPS || '50', 10);
const DURATION = __ENV.OF_BENCH_DURATION || '5m';

const automateE2E = new Trend('automate_e2e_ms', true);
const sagaE2E = new Trend('saga_e2e_ms', true);
const automateOk = new Rate('automate_ok');
const sagaOk = new Rate('saga_ok');

const POLL_TIMEOUT_MS = 30_000;
const POLL_INTERVAL_MS = 200;

function authHeaders() {
  return {
    headers: {
      Authorization: `Bearer ${TOKEN}`,
      'content-type': 'application/json',
    },
  };
}

function pollUntilCompleted(url) {
  const deadline = Date.now() + POLL_TIMEOUT_MS;
  while (Date.now() < deadline) {
    const r = http.get(url, authHeaders());
    if (r.status === 200) {
      let body;
      try { body = r.json(); } catch (_) { body = null; }
      if (body && body.state === 'Completed') return { ok: true, body };
      if (body && (body.state === 'Failed' || body.state === 'Compensated')) {
        return { ok: false, body };
      }
    }
    sleep(POLL_INTERVAL_MS / 1000);
  }
  return { ok: false, body: null, timeout: true };
}

export const options = {
  // Two arrival-rate scenarios in parallel; each shares the cluster
  // capacity but reports its own e2e trend.
  scenarios: {
    automate: {
      executor: 'constant-arrival-rate',
      rate: RPS,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: 50,
      maxVUs: 300,
      exec: 'automateFlow',
    },
    saga: {
      executor: 'constant-arrival-rate',
      rate: Math.max(1, Math.floor(RPS / 5)),
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: 20,
      maxVUs: 120,
      exec: 'sagaFlow',
    },
  },
  thresholds: {
    // Provisional SLOs for the post-migration substrate. These mirror
    // the design intent recorded in
    // `docs/architecture/foundry-pattern-orchestration.md` §"latency
    // budgets". Tune in PRs against the runbook, not here.
    automate_e2e_ms: ['p(50)<500', 'p(95)<2000', 'p(99)<5000'],
    saga_e2e_ms: ['p(50)<800', 'p(95)<3000', 'p(99)<8000'],
    automate_ok: ['rate>0.99'],
    saga_ok: ['rate>0.98'],
    http_req_failed: ['rate<0.01'],
  },
};

export function automateFlow() {
  group('automate', () => {
    const t0 = Date.now();
    const submit = http.post(
      `${BASE_URL}/api/v1/workflows/${WORKFLOW_ID}/execute`,
      JSON.stringify({ context: { bench: true } }),
      authHeaders(),
    );
    const submitted = check(submit, {
      'execute accepted': (r) => r.status === 202,
      'has run_id': (r) => !!(r.json() && r.json().run_id),
    });
    if (!submitted) {
      automateOk.add(false);
      return;
    }
    const runId = submit.json().run_id;
    const result = pollUntilCompleted(
      `${BASE_URL}/api/v1/workflows/${WORKFLOW_ID}/runs/${runId}`,
    );
    automateE2E.add(Date.now() - t0);
    automateOk.add(result.ok);
  });
}

export function sagaFlow() {
  group('saga', () => {
    const t0 = Date.now();
    const submit = http.post(
      `${BASE_URL}/api/v1/automations`,
      JSON.stringify({
        saga_type: SAGA_TYPE,
        input: { workspace_id: `bench-${__VU}-${__ITER}` },
      }),
      authHeaders(),
    );
    const submitted = check(submit, {
      'automation accepted': (r) => r.status === 202,
      'has saga_id': (r) => !!(r.json() && r.json().saga_id),
    });
    if (!submitted) {
      sagaOk.add(false);
      return;
    }
    const sagaId = submit.json().saga_id;
    const result = pollUntilCompleted(`${BASE_URL}/api/v1/automations/${sagaId}`);
    sagaE2E.add(Date.now() - t0);
    sagaOk.add(result.ok);
  });
}
