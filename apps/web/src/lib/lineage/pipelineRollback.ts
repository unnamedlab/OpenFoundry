import type { DatasetTransaction } from '@/lib/api/datasets';
import type { LineageGraph, LineageNode } from '@/lib/api/pipelines';

export type PipelineRollbackAction = 'rollback' | 'force_snapshot' | 'unchanged';

export interface PipelineRollbackTarget {
  node_id: string;
  label: string;
  dataset_rid: string;
  branch: string;
  distance: number;
  is_upstream: boolean;
  action: PipelineRollbackAction;
  excluded: boolean;
  target_transaction_id: string | null;
  target_transaction_rid: string | null;
  target_transaction_time: string | null;
  target_transaction_type: string | null;
  current_transaction_id: string | null;
  current_transaction_type: string | null;
  preserve_incrementality: boolean;
  warnings: string[];
}

export interface PipelineRollbackUnsupportedResource {
  node_id: string;
  label: string;
  kind: string;
  resource_type: string;
  distance: number;
  reason: string;
}

export interface PipelineRollbackPlan {
  root_node_id: string;
  root_label: string;
  branch: string;
  transaction_id: string;
  transaction_rid: string | null;
  transaction_time: string;
  targets: PipelineRollbackTarget[];
  unsupported: PipelineRollbackUnsupportedResource[];
  warnings: string[];
  excluded_node_ids: string[];
}

export type PipelineRollbackTransactionIndex = Record<string, DatasetTransaction[]>;
export type PipelineRollbackJobSpecIndex = Record<string, boolean>;

const UNSUPPORTED_RESOURCE_TYPES = new Set([
  'streaming_dataset',
  'streaming dataset',
  'stream',
  'media_set',
  'media set',
  'mediaset',
  'virtual_table',
  'virtual table',
  'restricted_view',
  'restricted view',
  'logical_view',
  'logical view',
]);

const LOGIC_UPDATED_KEYS = [
  'logic_updated_at',
  'last_logic_updated_at',
  'job_spec_updated_at',
  'jobSpecUpdatedAt',
  'code_updated_at',
  'repository_updated_at',
  'transform_updated_at',
  'deployment_updated_at',
];

export function computePipelineRollbackPlan(input: {
  graph: LineageGraph | null;
  upstreamNodeId: string;
  branch: string;
  selectedTransaction: DatasetTransaction | null;
  transactionsByDataset: PipelineRollbackTransactionIndex;
  jobSpecByDatasetId?: PipelineRollbackJobSpecIndex;
  excludedNodeIds?: string[];
}): PipelineRollbackPlan | null {
  const { graph, upstreamNodeId, branch, selectedTransaction } = input;
  if (!graph || !selectedTransaction) return null;
  const nodesByID = new Map(graph.nodes.map((node) => [node.id, node]));
  const root = nodesByID.get(upstreamNodeId);
  if (!root || root.kind !== 'dataset') return null;

  const selectedTime = transactionTime(selectedTransaction);
  if (!selectedTime) return null;
  const selectedTimestamp = selectedTime.getTime();
  const reachable = downstreamReachable(graph, root.id);
  const excluded = new Set(input.excludedNodeIds ?? []);
  const targets: PipelineRollbackTarget[] = [];
  const unsupported: PipelineRollbackUnsupportedResource[] = [];
  const warnings: string[] = [];

  for (const entry of reachable) {
    const node = nodesByID.get(entry.nodeId);
    if (!node) continue;
    const unsupportedReason = unsupportedReasonForNode(node, input.jobSpecByDatasetId ?? {});
    if (unsupportedReason) {
      unsupported.push({
        node_id: node.id,
        label: node.label,
        kind: node.kind,
        resource_type: resourceTypeForNode(node),
        distance: entry.distance,
        reason: unsupportedReason,
      });
      continue;
    }

    const rows = input.transactionsByDataset[node.id] ?? input.transactionsByDataset[datasetRidForNode(node)] ?? [];
    const current = latestCommittedTransaction(rows);
    const target = node.id === root.id ? selectedTransaction : latestCommittedTransactionAtOrBefore(rows, selectedTimestamp);
    const targetTime = target ? transactionTime(target) : null;
    const targetType = target ? transactionType(target) : null;
    const currentType = current ? transactionType(current) : null;
    const logicWarning = logicChangedWarning(node, selectedTimestamp);
    const rowWarnings: string[] = [];
    if (logicWarning) rowWarnings.push(logicWarning);

    let action: PipelineRollbackAction = 'rollback';
    let preserveIncrementality = true;
    if (!target) {
      action = 'force_snapshot';
      preserveIncrementality = false;
      rowWarnings.push('No committed transaction existed at the selected rollback point. The next build should run as a snapshot recovery.');
    } else if (current?.id === target.id) {
      action = 'unchanged';
      rowWarnings.push('Already at the selected rollback point; no rollback transaction is needed.');
    }
    if (target && !targetTime) {
      rowWarnings.push('Target transaction has no closed timestamp, so preview ordering may be approximate.');
    }

    const isUpstream = node.id === root.id;
    targets.push({
      node_id: node.id,
      label: node.label,
      dataset_rid: datasetRidForNode(node),
      branch,
      distance: entry.distance,
      is_upstream: isUpstream,
      action,
      excluded: !isUpstream && excluded.has(node.id),
      target_transaction_id: target?.id ?? null,
      target_transaction_rid: transactionRid(target),
      target_transaction_time: targetTime?.toISOString() ?? null,
      target_transaction_type: targetType,
      current_transaction_id: current?.id ?? null,
      current_transaction_type: currentType,
      preserve_incrementality: preserveIncrementality,
      warnings: rowWarnings,
    });
  }

  targets.sort((a, b) => a.distance - b.distance || a.label.localeCompare(b.label));
  unsupported.sort((a, b) => a.distance - b.distance || a.label.localeCompare(b.label));
  if (targets.length === 0) warnings.push('No transactional datasets with rollback support were found from the selected upstream dataset.');
  const excludedCount = targets.filter((target) => target.excluded).length;
  if (excludedCount > 0) warnings.push(`${excludedCount} downstream dataset(s) are excluded from rollback execution.`);
  const forceSnapshots = targets.filter((target) => !target.excluded && target.action === 'force_snapshot').length;
  if (forceSnapshots > 0) warnings.push(`${forceSnapshots} dataset(s) will only be marked for snapshot recovery because no prior transaction exists.`);
  const logicChanged = targets.filter((target) => target.warnings.some((warning) => warning.toLowerCase().includes('logic changed'))).length;
  if (logicChanged > 0) warnings.push(`${logicChanged} dataset(s) have transform logic changes after the selected transaction.`);

  return {
    root_node_id: root.id,
    root_label: root.label,
    branch,
    transaction_id: selectedTransaction.id,
    transaction_rid: transactionRid(selectedTransaction),
    transaction_time: selectedTime.toISOString(),
    targets,
    unsupported,
    warnings,
    excluded_node_ids: [...excluded],
  };
}

export function downstreamDatasetIds(graph: LineageGraph | null, upstreamNodeId: string) {
  if (!graph) return [];
  const nodesByID = new Map(graph.nodes.map((node) => [node.id, node]));
  return downstreamReachable(graph, upstreamNodeId)
    .map((entry) => nodesByID.get(entry.nodeId))
    .filter((node): node is LineageNode => Boolean(node && node.kind === 'dataset'))
    .map((node) => node.id);
}

export function transactionTime(tx: DatasetTransaction | null | undefined) {
  if (!tx) return null;
  const raw = tx.closedTime ?? tx.committed_at ?? tx.started_at ?? tx.created_at ?? tx.createdTime;
  if (!raw) return null;
  const date = new Date(raw);
  return Number.isNaN(date.getTime()) ? null : date;
}

export function transactionRid(tx: DatasetTransaction | null | undefined) {
  return tx?.transaction_rid ?? tx?.rid ?? null;
}

export function transactionType(tx: DatasetTransaction | null | undefined) {
  return tx?.transactionType ?? tx?.tx_type ?? tx?.operation ?? null;
}

function latestCommittedTransaction(rows: DatasetTransaction[]) {
  return committedNewestFirst(rows)[0] ?? null;
}

function latestCommittedTransactionAtOrBefore(rows: DatasetTransaction[], cutoffMs: number) {
  return committedNewestFirst(rows).find((tx) => {
    const time = transactionTime(tx);
    return Boolean(time && time.getTime() <= cutoffMs);
  }) ?? null;
}

function committedNewestFirst(rows: DatasetTransaction[]) {
  return rows
    .filter((tx) => tx.status === 'COMMITTED')
    .slice()
    .sort((a, b) => (transactionTime(b)?.getTime() ?? 0) - (transactionTime(a)?.getTime() ?? 0));
}

function relevantEdges(graph: LineageGraph) {
  return graph.edges.filter((edge) => {
    if (edge.relation_kind === 'schedules' || edge.relation_kind === 'build_execution') return false;
    if (edge.relation_kind === 'ontology_output' || edge.relation_kind === 'workflow_handoff') return false;
    return true;
  });
}

function downstreamReachable(graph: LineageGraph, rootNodeId: string) {
  const outgoing = new Map<string, string[]>();
  for (const edge of relevantEdges(graph)) {
    const list = outgoing.get(edge.source) ?? [];
    list.push(edge.target);
    outgoing.set(edge.source, list);
  }
  const out: Array<{ nodeId: string; distance: number }> = [];
  const seen = new Set<string>();
  const queue: Array<{ nodeId: string; distance: number }> = [{ nodeId: rootNodeId, distance: 0 }];
  while (queue.length > 0) {
    const current = queue.shift();
    if (!current || seen.has(current.nodeId)) continue;
    seen.add(current.nodeId);
    out.push(current);
    for (const next of outgoing.get(current.nodeId) ?? []) {
      if (!seen.has(next)) queue.push({ nodeId: next, distance: current.distance + 1 });
    }
  }
  return out;
}

function unsupportedReasonForNode(node: LineageNode, jobSpecByDatasetId: PipelineRollbackJobSpecIndex) {
  const resourceType = resourceTypeForNode(node);
  if (UNSUPPORTED_RESOURCE_TYPES.has(resourceType)) {
    return `${resourceTypeLabel(resourceType)} cannot be rolled back by transactional dataset rollback.`;
  }
  if (node.kind !== 'dataset') {
    return `${node.kind} resources are shown for awareness but remain unchanged.`;
  }
  if (explicitlyFalse(node.metadata ?? {}, 'can_edit') || explicitlyFalse(node.metadata ?? {}, 'editable')) {
    return 'You do not have edit access to this dataset.';
  }
  if (jobSpecByDatasetId[node.id] === false) {
    return 'No JobSpec is available; uploaded or manually managed datasets are unsupported for pipeline rollback.';
  }
  return '';
}

function resourceTypeForNode(node: LineageNode) {
  const metadata = node.metadata ?? {};
  return (
    metadataString(metadata, 'resource_type')
    || metadataString(metadata, 'type')
    || metadataString(metadata, 'kind')
    || metadataString(metadata, 'view_kind')
    || node.kind
  ).toLowerCase().replace(/-/g, '_');
}

function resourceTypeLabel(resourceType: string) {
  return resourceType.replace(/_/g, ' ');
}

function logicChangedWarning(node: LineageNode, selectedTimestamp: number) {
  const metadata = node.metadata ?? {};
  for (const key of LOGIC_UPDATED_KEYS) {
    const raw = metadataString(metadata, key);
    if (!raw) continue;
    const date = new Date(raw);
    if (!Number.isNaN(date.getTime()) && date.getTime() > selectedTimestamp) {
      return `Transform logic changed after the selected transaction (${date.toLocaleString()}); review whether a rollback remains valid.`;
    }
  }
  return '';
}

function datasetRidForNode(node: LineageNode) {
  return metadataString(node.metadata ?? {}, 'rid') || metadataString(node.metadata ?? {}, 'dataset_rid') || node.id;
}

function metadataString(metadata: Record<string, unknown>, key: string) {
  const value = metadata[key];
  return typeof value === 'string' && value.trim() ? value : '';
}

function explicitlyFalse(metadata: Record<string, unknown>, key: string) {
  const value = metadata[key];
  return value === false || value === 'false' || value === 0 || value === '0';
}
