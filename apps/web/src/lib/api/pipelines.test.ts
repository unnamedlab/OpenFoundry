// Pipeline Builder API client tests — happy path of the canonical Foundry
// authoring → build flow: Validate → Compile → Trigger → Retry. Mocks
// `fetch` to assert the request paths/payloads land on the intended
// services (authoring vs build queue) and that response shapes round-trip
// through the typed wrappers.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import api from './client';
import {
  validatePipeline,
  compilePipeline,
  triggerRun,
  retryPipelineRun,
  abortBuild,
  listBuilds,
  type PipelineNode,
} from './pipelines';

interface Recorded {
  url: string;
  method: string;
  body: unknown;
}

let calls: Recorded[];

function mockFetch(responder: (rec: Recorded) => unknown) {
  return vi.fn(async (input: RequestInfo, init?: RequestInit) => {
    const rec: Recorded = {
      url: typeof input === 'string' ? input : input.toString(),
      method: init?.method ?? 'GET',
      body: init?.body ? JSON.parse(init.body as string) : null,
    };
    calls.push(rec);
    const payload = responder(rec);
    return new Response(JSON.stringify(payload), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });
  });
}

const sampleNodes: PipelineNode[] = [
  {
    id: 'src',
    label: 'source',
    transform_type: 'passthrough',
    config: {},
    depends_on: [],
    input_dataset_ids: ['11111111-1111-1111-1111-111111111111'],
    output_dataset_id: null,
  },
  {
    id: 'agg',
    label: 'aggregate',
    transform_type: 'sql',
    config: { sql: 'SELECT 1' },
    depends_on: ['src'],
    input_dataset_ids: [],
    output_dataset_id: '22222222-2222-2222-2222-222222222222',
  },
];

beforeEach(() => {
  calls = [];
  api.setToken('test-token');
});

afterEach(() => {
  vi.restoreAllMocks();
  api.setToken(null);
});

describe('Pipeline Builder happy path (Validate → Compile → Trigger → Retry)', () => {
  it('Validate posts the in-flight DAG to authoring service', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch(() => ({
        valid: true,
        errors: [],
        warnings: [],
        next_run_at: null,
        summary: { node_count: 2, edge_count: 1, root_node_ids: ['src'], leaf_node_ids: ['agg'] },
      })),
    );

    const res = await validatePipeline({
      nodes: sampleNodes,
      status: 'draft',
      schedule_config: { enabled: false, cron: null },
    });

    expect(res.valid).toBe(true);
    expect(res.summary.node_count).toBe(2);
    expect(calls).toHaveLength(1);
    expect(calls[0].url).toBe('/api/v1/pipelines/_validate');
    expect(calls[0].method).toBe('POST');
  });

  it('Compile returns the executable plan (authoring service)', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch(() => ({
        validation: {
          valid: true,
          errors: [],
          warnings: [],
          next_run_at: null,
          summary: { node_count: 2, edge_count: 1, root_node_ids: ['src'], leaf_node_ids: ['agg'] },
        },
        plan: {
          topological_order: ['src', 'agg'],
          stages: [['src'], ['agg']],
          summary: { node_count: 2, edge_count: 1, root_node_ids: ['src'], leaf_node_ids: ['agg'] },
        },
      })),
    );

    const res = await compilePipeline({
      nodes: sampleNodes,
      status: 'draft',
      schedule_config: { enabled: false, cron: null },
    });

    expect(res.plan.topological_order).toEqual(['src', 'agg']);
    expect(res.plan.stages).toEqual([['src'], ['agg']]);
    expect(calls[0].url).toBe('/api/v1/pipelines/_compile');
  });

  it('Trigger posts to /pipelines/{id}/runs (build service surface)', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch(() => ({
        id: 'run-1',
        pipeline_id: 'pl-1',
        status: 'running',
        trigger_type: 'manual',
        attempt_number: 1,
        execution_context: {},
        node_results: null,
        error_message: null,
        started_at: new Date().toISOString(),
        finished_at: null,
        started_by: 'user',
        started_from_node_id: null,
        retry_of_run_id: null,
      })),
    );

    const run = await triggerRun('pl-1');

    expect(run.id).toBe('run-1');
    expect(run.status).toBe('running');
    expect(calls[0].url).toBe('/api/v1/pipelines/pl-1/runs');
    expect(calls[0].method).toBe('POST');
  });

  it('Retry hits /pipelines/{id}/runs/{run_id}/retry', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch(() => ({
        id: 'run-2',
        pipeline_id: 'pl-1',
        status: 'running',
        trigger_type: 'retry',
        attempt_number: 2,
        execution_context: {},
        node_results: null,
        error_message: null,
        started_at: new Date().toISOString(),
        finished_at: null,
        started_by: 'user',
        started_from_node_id: 'agg',
        retry_of_run_id: 'run-1',
      })),
    );

    const run = await retryPipelineRun('pl-1', 'run-1', { from_node_id: 'agg' });

    expect(run.attempt_number).toBe(2);
    expect(run.retry_of_run_id).toBe('run-1');
    expect(calls[0].url).toBe('/api/v1/pipelines/pl-1/runs/run-1/retry');
    expect(calls[0].body).toEqual({ from_node_id: 'agg' });
  });

  it('Abort and list-builds target the global Builds queue surface', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch((rec) => {
        if (rec.url.includes('abort')) {
          return {
            id: 'run-1',
            pipeline_id: 'pl-1',
            status: 'aborted',
            trigger_type: 'manual',
            attempt_number: 1,
            execution_context: {},
            node_results: null,
            error_message: 'aborted by user',
            started_at: new Date().toISOString(),
            finished_at: new Date().toISOString(),
            started_by: 'user',
            started_from_node_id: null,
            retry_of_run_id: null,
          };
        }
        return { data: [], page: 1, per_page: 50 };
      }),
    );

    const aborted = await abortBuild('run-1');
    expect(aborted.status).toBe('aborted');
    expect(calls[0].url).toBe('/api/v1/builds/run-1/abort');

    const list = await listBuilds({ status: 'failed', per_page: 25 });
    expect(list.data).toEqual([]);
    expect(calls[1].url).toContain('/api/v1/builds');
    expect(calls[1].url).toContain('status=failed');
    expect(calls[1].url).toContain('per_page=25');
  });

  it('passes the bearer token on every authoring/build call', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_url: RequestInfo, init?: RequestInit) => {
        const headers = new Headers(init?.headers);
        expect(headers.get('Authorization')).toBe('Bearer test-token');
        return new Response(JSON.stringify({ data: [], page: 1, per_page: 50 }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }),
    );

    await listBuilds();
  });
});
