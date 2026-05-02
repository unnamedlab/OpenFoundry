// Ontology hot-path mixed workload — S1.8 baseline
//
// Mix: 80 % read by-id, 15 % read by-type, 5 % write (action execute).
// 50 % de los reads pide `X-Consistency: strong`, el resto `eventual`.
//
// Run:
//   k6 run --out json=benchmarks/results/ontology-mix-k6.json \
//     benchmarks/ontology/k6/ontology-mix.js
//
// Variables de entorno requeridas:
//   OF_BENCH_BASE_URL      e.g. https://ontology.dev.openfoundry.local
//   OF_BENCH_TOKEN         bearer JWT con permisos de read+execute
//   OF_BENCH_TENANT        tenant id usado para todos los reads
//   OF_BENCH_TYPE_ID       object type id usado para list_by_type
//   OF_BENCH_OBJECT_IDS    fichero con un id por línea (output de seed.sh)
//   OF_BENCH_ACTION_ID     action type id que el bench ejecutará en el 5% write

import http from 'k6/http';
import { check, group, fail } from 'k6';
import { SharedArray } from 'k6/data';
import { Rate } from 'k6/metrics';
import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';

const BASE_URL = __ENV.OF_BENCH_BASE_URL || fail('OF_BENCH_BASE_URL no definida');
const TOKEN = __ENV.OF_BENCH_TOKEN || fail('OF_BENCH_TOKEN no definida');
const TENANT = __ENV.OF_BENCH_TENANT || fail('OF_BENCH_TENANT no definida');
const TYPE_ID = __ENV.OF_BENCH_TYPE_ID || fail('OF_BENCH_TYPE_ID no definida');
const ACTION_ID = __ENV.OF_BENCH_ACTION_ID || fail('OF_BENCH_ACTION_ID no definida');
const IDS_FILE = __ENV.OF_BENCH_OBJECT_IDS || './object-ids.txt';

// SharedArray => parsed once, reused across VUs (zero-copy).
const objectIds = new SharedArray('object-ids', function () {
  return open(IDS_FILE).split('\n').filter((line) => line.length > 0);
});

if (objectIds.length < 1000) {
  fail(`OF_BENCH_OBJECT_IDS contiene ${objectIds.length} entradas; mínimo 1000 para mix realista`);
}

// Custom rates por grupo para inspección post-run.
const readByIdOk = new Rate('read_by_id_ok');
const readByTypeOk = new Rate('read_by_type_ok');
const writeOk = new Rate('write_ok');

export const options = {
  // Workload shape: arrival-rate constante a 5 000 RPS sostenidos.
  // pre-allocated VUs holgados para evitar starvation; subir si la
  // suite reporta `dropped_iterations`.
  scenarios: {
    ontology_mix: {
      executor: 'constant-arrival-rate',
      rate: 5000,
      timeUnit: '1s',
      duration: '5m',
      preAllocatedVUs: 400,
      maxVUs: 1200,
    },
  },
  thresholds: {
    // SLO global (S1.8.c) — fail-fast si se viola.
    'http_req_duration': [
      'p(50)<5',
      'p(95)<20',
      'p(99)<50',
    ],
    // SLO específico del read by-id (representa el 80% de la carga).
    'http_req_duration{group:::read-by-id}': [
      'p(50)<5',
      'p(95)<15',
      'p(99)<35',
    ],
    'http_req_failed': ['rate<0.001'],
    // Sustained throughput.
    'iterations': ['rate>=4950'],
    // Cada path debe tener tasa de éxito casi total.
    'read_by_id_ok': ['rate>0.999'],
    'read_by_type_ok': ['rate>0.999'],
    'write_ok': ['rate>0.99'],
  },
  // Detectar regresiones temprano sin esperar al final.
  abortOnFail: true,
  noConnectionReuse: false,
  discardResponseBodies: false,
};

function pickId() {
  return objectIds[Math.floor(Math.random() * objectIds.length)];
}

function pickConsistency() {
  return Math.random() < 0.5 ? 'strong' : 'eventual';
}

function authHeaders(extra) {
  return Object.assign(
    {
      'Authorization': `Bearer ${TOKEN}`,
      'X-Bench-Run-Id': __ENV.OF_BENCH_RUN_ID || 'local',
    },
    extra || {},
  );
}

function readById() {
  group('read-by-id', () => {
    const id = pickId();
    const consistency = pickConsistency();
    const res = http.get(
      `${BASE_URL}/api/v1/ontology/objects/${TENANT}/${id}`,
      {
        headers: authHeaders({ 'X-Consistency': consistency }),
        tags: { group: 'read-by-id', consistency },
      },
    );
    const ok = check(res, {
      'read-by-id 200': (r) => r.status === 200,
    });
    readByIdOk.add(ok);
  });
}

function readByType() {
  group('read-by-type', () => {
    const consistency = pickConsistency();
    const res = http.get(
      `${BASE_URL}/api/v1/ontology/objects/${TENANT}/by-type/${TYPE_ID}?limit=50`,
      {
        headers: authHeaders({ 'X-Consistency': consistency }),
        tags: { group: 'read-by-type', consistency },
      },
    );
    const ok = check(res, {
      'read-by-type 200': (r) => r.status === 200,
    });
    readByTypeOk.add(ok);
  });
}

function executeAction() {
  group('write', () => {
    const targetId = pickId();
    const payload = JSON.stringify({
      tenant_id: TENANT,
      target_object_id: targetId,
      // event_id determinista por iteración VU/iter — la idempotencia del
      // helper `apply_object_with_outbox` (S1.4.c) absorbe cualquier replay.
      idempotency_key: `${__VU}-${__ITER}-${uuidv4()}`,
      payload: { source: 'k6-bench' },
    });
    const res = http.post(
      `${BASE_URL}/api/v1/ontology/actions/${ACTION_ID}/execute`,
      payload,
      {
        headers: authHeaders({ 'Content-Type': 'application/json' }),
        tags: { group: 'write' },
      },
    );
    const ok = check(res, {
      'write 2xx': (r) => r.status >= 200 && r.status < 300,
    });
    writeOk.add(ok);
  });
}

export default function () {
  const r = Math.random();
  if (r < 0.80) {
    readById();
  } else if (r < 0.95) {
    readByType();
  } else {
    executeAction();
  }
}

// Resumen plano (sin colores) para log capture en CI.
export function handleSummary(data) {
  return {
    'stdout': textSummary(data),
    'benchmarks/results/ontology-mix-k6.json': JSON.stringify(data, null, 2),
  };
}

// Re-implementación mínima del summary textual (evita dependencia
// adicional de jslib y mantiene la salida parseable).
function textSummary(data) {
  const m = data.metrics;
  const dur = m.http_req_duration && m.http_req_duration.values;
  const it = m.iterations && m.iterations.values;
  const failed = m.http_req_failed && m.http_req_failed.values;
  const lines = [
    '=== ontology-mix S1.8 baseline ===',
    `iterations:        ${it ? it.count : '?'} total / ${it ? it.rate.toFixed(0) : '?'} ips`,
    `http_req_duration: p50=${(dur && dur['p(50)']).toFixed(2)}ms ` +
      `p95=${(dur && dur['p(95)']).toFixed(2)}ms ` +
      `p99=${(dur && dur['p(99)']).toFixed(2)}ms`,
    `error rate:        ${failed ? (failed.rate * 100).toFixed(3) : '?'} %`,
    '',
  ];
  return lines.join('\n');
}
