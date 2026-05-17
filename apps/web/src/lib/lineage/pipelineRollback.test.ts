import { describe, expect, it } from 'vitest';

import type { DatasetTransaction } from '@/lib/api/datasets';
import type { LineageGraph } from '@/lib/api/pipelines';

import { computePipelineRollbackPlan } from './pipelineRollback';

function tx(id: string, committedAt: string, type = 'APPEND'): DatasetTransaction {
  return {
    id,
    dataset_id: 'dataset',
    operation: type,
    tx_type: type,
    transactionType: type,
    status: 'COMMITTED',
    summary: '',
    metadata: {},
    created_at: committedAt,
    committed_at: committedAt,
    closedTime: committedAt,
  };
}

const graph: LineageGraph = {
  nodes: [
    { id: 'source', kind: 'dataset', label: 'Source', marking: 'public', metadata: { rid: 'ri.dataset.source' } },
    { id: 'clean', kind: 'dataset', label: 'Clean', marking: 'public', metadata: { rid: 'ri.dataset.clean', job_spec_updated_at: '2026-02-01T00:00:00Z' } },
    { id: 'media', kind: 'media_set', label: 'Images', marking: 'public', metadata: { resource_type: 'media_set' } },
    { id: 'view', kind: 'dataset', label: 'Restricted view', marking: 'public', metadata: { resource_type: 'restricted_view' } },
  ],
  edges: [
    { id: 'e1', source: 'source', source_kind: 'dataset', target: 'clean', target_kind: 'dataset', relation_kind: 'derives_from', pipeline_id: 'pipe', workflow_id: null, node_id: null, step_id: null, effective_marking: 'public', metadata: {} },
    { id: 'e2', source: 'clean', source_kind: 'dataset', target: 'media', target_kind: 'media_set', relation_kind: 'derives_from', pipeline_id: 'pipe', workflow_id: null, node_id: null, step_id: null, effective_marking: 'public', metadata: {} },
    { id: 'e3', source: 'clean', source_kind: 'dataset', target: 'view', target_kind: 'dataset', relation_kind: 'derives_from', pipeline_id: 'pipe', workflow_id: null, node_id: null, step_id: null, effective_marking: 'public', metadata: {} },
  ],
};

describe('computePipelineRollbackPlan', () => {
  it('plans upstream and downstream transactional rollbacks with exclusions and unsupported resources', () => {
    const selected = tx('source-t1', '2026-01-10T00:00:00Z', 'SNAPSHOT');
    const plan = computePipelineRollbackPlan({
      graph,
      upstreamNodeId: 'source',
      branch: 'master',
      selectedTransaction: selected,
      jobSpecByDatasetId: { source: true, clean: true, view: true },
      excludedNodeIds: ['clean'],
      transactionsByDataset: {
        source: [tx('source-t2', '2026-01-20T00:00:00Z'), selected],
        clean: [tx('clean-t2', '2026-01-18T00:00:00Z'), tx('clean-t1', '2026-01-09T00:00:00Z')],
      },
    });

    expect(plan?.targets).toHaveLength(2);
    expect(plan?.targets[0]).toMatchObject({ node_id: 'source', action: 'rollback', excluded: false });
    expect(plan?.targets[1]).toMatchObject({ node_id: 'clean', action: 'rollback', excluded: true, target_transaction_id: 'clean-t1' });
    expect(plan?.unsupported.map((item) => item.node_id).sort()).toEqual(['media', 'view']);
    expect(plan?.warnings.some((warning) => warning.includes('excluded'))).toBe(true);
  });

  it('warns and marks snapshot recovery when a downstream dataset has no prior transaction', () => {
    const plan = computePipelineRollbackPlan({
      graph,
      upstreamNodeId: 'source',
      branch: 'master',
      selectedTransaction: tx('source-t1', '2026-01-10T00:00:00Z'),
      jobSpecByDatasetId: { source: true, clean: true },
      transactionsByDataset: {
        source: [tx('source-t1', '2026-01-10T00:00:00Z')],
        clean: [tx('clean-t2', '2026-01-18T00:00:00Z')],
      },
    });

    const clean = plan?.targets.find((target) => target.node_id === 'clean');
    expect(clean).toMatchObject({ action: 'force_snapshot', preserve_incrementality: false, target_transaction_id: null });
    expect(clean?.warnings.join(' ')).toContain('snapshot recovery');
  });

  it('warns when transform logic changed after the selected transaction', () => {
    const plan = computePipelineRollbackPlan({
      graph,
      upstreamNodeId: 'source',
      branch: 'master',
      selectedTransaction: tx('source-t1', '2026-01-10T00:00:00Z'),
      jobSpecByDatasetId: { source: true, clean: true },
      transactionsByDataset: {
        source: [tx('source-t1', '2026-01-10T00:00:00Z')],
        clean: [tx('clean-t1', '2026-01-09T00:00:00Z')],
      },
    });

    const clean = plan?.targets.find((target) => target.node_id === 'clean');
    expect(clean?.warnings.some((warning) => warning.includes('Transform logic changed'))).toBe(true);
    expect(plan?.warnings.some((warning) => warning.includes('logic changes'))).toBe(true);
  });
});
