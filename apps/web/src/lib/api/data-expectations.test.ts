import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  approveDataExpectation,
  defaultDataExpectationDraft,
  ensureExpectationChangesReviewed,
  evaluateBuildExpectationGates,
  evaluateDataExpectationsForPreview,
  materializeDataExpectation,
  publishExpectationResultsToDataHealth,
  recordDataExpectationResults,
  runBuildWithExpectationGates,
  upsertDataExpectation,
  withNodeDataExpectations,
} from './data-expectations';
import { latestReportsForResource } from './health-reports';
import type { PipelineNode } from './pipelines';

describe('data expectations', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('evaluates output post-conditions and publishes failing results to Data Health', () => {
    mockLocalStorage();
    const node = sampleNode();
    const definition = materializeDataExpectation({
      draft: {
        ...defaultDataExpectationDraft('output'),
        name: 'customer id present',
        kind: 'not_null',
        column: 'customer_id',
        failure_mode: 'ABORT_BUILD',
      },
      pipelineId: 'ri.pipeline.sales',
      node,
      branchName: 'dev',
    });
    upsertDataExpectation(definition);

    const results = evaluateDataExpectationsForPreview([definition], {
      columns: ['customer_id', 'amount'],
      rows: [{ customer_id: 'C-1', amount: 10 }, { customer_id: null, amount: 42 }],
    });
    recordDataExpectationResults(results);
    publishExpectationResultsToDataHealth(results);

    expect(results[0]).toMatchObject({
      status: 'failed',
      expectation_name: 'customer id present',
      subject_dataset_rid: 'ri.dataset.customers',
    });
    expect(evaluateBuildExpectationGates({ pipelineId: 'ri.pipeline.sales', branchName: 'dev' })).toMatchObject({
      shouldAbort: true,
      requiresReview: false,
    });
    expect(latestReportsForResource('ri.dataset.customers')[0]).toMatchObject({
      status: 'failing',
      group: 'Data Expectations',
    });
  });

  it('requires reviewed expectation changes on protected branches', () => {
    mockLocalStorage();
    const node = sampleNode();
    const definition = materializeDataExpectation({
      draft: {
        ...defaultDataExpectationDraft('output'),
        name: 'minimum customer rows',
        kind: 'row_count_min',
        expected_value: '1',
      },
      pipelineId: 'ri.pipeline.sales',
      node,
      branchName: 'main',
    });
    upsertDataExpectation(definition);

    const nodeWithExpectation = withNodeDataExpectations(node, [definition]);
    expect(() => ensureExpectationChangesReviewed({
      pipelineId: 'ri.pipeline.sales',
      branchName: 'main',
      nodes: [nodeWithExpectation],
    })).toThrow(/require review/);

    approveDataExpectation(definition.id);

    expect(() => ensureExpectationChangesReviewed({
      pipelineId: 'ri.pipeline.sales',
      branchName: 'main',
      nodes: [nodeWithExpectation],
    })).not.toThrow();
  });

  it('aborts build submission before calling the build API when an aborting gate failed', async () => {
    mockLocalStorage();
    const fetch = vi.fn();
    vi.stubGlobal('fetch', fetch);
    const node = sampleNode();
    const definition = materializeDataExpectation({
      draft: {
        ...defaultDataExpectationDraft('output'),
        name: 'customer id present',
        kind: 'not_null',
        column: 'customer_id',
        failure_mode: 'ABORT_BUILD',
      },
      pipelineId: 'ri.pipeline.sales',
      node,
      branchName: 'dev',
    });
    upsertDataExpectation(definition);
    recordDataExpectationResults(evaluateDataExpectationsForPreview([definition], {
      columns: ['customer_id'],
      rows: [{ customer_id: '' }],
    }));

    const response = await runBuildWithExpectationGates({
      pipeline_rid: 'ri.pipeline.sales',
      build_branch: 'dev',
      output_dataset_rids: ['ri.dataset.customers'],
      force_build: false,
      trigger_kind: 'MANUAL',
    });

    expect(response.state).toBe('BUILD_ABORTED');
    expect(response.queued_reason).toContain('customer id present');
    expect(fetch).not.toHaveBeenCalled();
  });
});

function sampleNode(): PipelineNode {
  return {
    id: 'node-customers',
    label: 'Customers output',
    transform_type: 'sql',
    config: { _output: { dataset_rid: 'ri.dataset.customers' } },
    depends_on: [],
    input_dataset_ids: ['ri.dataset.raw-customers'],
    output_dataset_id: 'ri.dataset.customers',
  };
}

function mockLocalStorage() {
  const values = new Map<string, string>();
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => {
      values.set(key, value);
    },
    removeItem: (key: string) => {
      values.delete(key);
    },
    clear: () => {
      values.clear();
    },
    key: (index: number) => Array.from(values.keys())[index] ?? null,
    get length() {
      return values.size;
    },
  });
}
