import { useEffect, useMemo, useState, type CSSProperties } from 'react';
import { Link } from 'react-router-dom';

import {
  getDatasetHealth,
  listDatasets,
  type Dataset,
  type DatasetHealthResponse,
} from '@/lib/api/datasets';
import {
  dataConnection,
  type ConnectorAgent,
  type DataConnectionStreamResource,
  type Source,
} from '@/lib/api/data-connection';
import {
  listActionTypes,
  listFunctionPackages,
  listObjectTypes,
  type ActionType,
  type FunctionPackage,
  type ObjectType,
} from '@/lib/api/ontology';
import {
  getFullLineage,
  listPipelines,
  type LineageGraph,
  type LineageNode,
  type Pipeline,
} from '@/lib/api/pipelines';
import { listSchedules, type Schedule } from '@/lib/api/schedules';
import { listWorkflows, type WorkflowDefinition } from '@/lib/api/workflows';
import { healthReportsToSignalsFromResource } from '@/lib/api/health-reports';
import { HealthReportsPanel } from '@/lib/components/health/HealthReportsPanel';

type MonitorResourceClass =
  | 'dataset'
  | 'schedule'
  | 'streaming_dataset'
  | 'agent'
  | 'object_type'
  | 'function'
  | 'action'
  | 'automation'
  | 'pipeline';

type RollupStatus = 'healthy' | 'warning' | 'critical' | 'unknown';
type ScopeKind = 'all' | 'project' | 'folder' | 'resource' | 'resource_type';
type CheckMode = 'all' | 'watched' | 'attention';

interface HealthCheck {
  id: string;
  kind: string;
  label: string;
  status: RollupStatus;
  message: string;
  observedAt?: string | null;
  recommendation?: string;
  watched: boolean;
}

interface MonitorableResource {
  id: string;
  rid?: string | null;
  name: string;
  resourceClass: MonitorResourceClass;
  project?: string | null;
  folder?: string | null;
  owner?: string | null;
  branch?: string | null;
  href?: string;
  updatedAt?: string | null;
  status: RollupStatus;
  checks: HealthCheck[];
  metadata: Record<string, unknown>;
}

interface SavedHealthView {
  id: string;
  name: string;
  scopeKind: ScopeKind;
  scopeValue: string;
  resourceType: MonitorResourceClass;
  checkMode: CheckMode;
  search: string;
  createdAt: string;
}

const WATCHED_CHECKS_KEY = 'openfoundry:data-health:watched-checks:v1';
const SAVED_VIEWS_KEY = 'openfoundry:data-health:monitoring-views:v1';
const MAX_DATASET_HEALTH_PROBES = 80;

const RESOURCE_CLASS_OPTIONS: Array<{ value: MonitorResourceClass; label: string }> = [
  { value: 'dataset', label: 'Datasets' },
  { value: 'schedule', label: 'Schedules' },
  { value: 'streaming_dataset', label: 'Streaming datasets' },
  { value: 'agent', label: 'Agents' },
  { value: 'object_type', label: 'Object types' },
  { value: 'function', label: 'Functions' },
  { value: 'action', label: 'Actions' },
  { value: 'automation', label: 'Automations' },
  { value: 'pipeline', label: 'Pipeline resources' },
];

const RESOURCE_CLASS_LABELS: Record<MonitorResourceClass, string> = Object.fromEntries(
  RESOURCE_CLASS_OPTIONS.map((option) => [option.value, option.label]),
) as Record<MonitorResourceClass, string>;

const STATUS_LABELS: Record<RollupStatus, string> = {
  healthy: 'Healthy',
  warning: 'Warning',
  critical: 'Critical',
  unknown: 'Unknown',
};

const STATUS_RANK: Record<RollupStatus, number> = {
  healthy: 0,
  unknown: 1,
  warning: 2,
  critical: 3,
};

const SCOPE_LABELS: Record<ScopeKind, string> = {
  all: 'All resources',
  project: 'Project',
  folder: 'Folder',
  resource: 'Single resource',
  resource_type: 'Resource type',
};

const CHECK_MODE_LABELS: Record<CheckMode, string> = {
  all: 'All checks',
  watched: 'Watched checks',
  attention: 'Needs attention',
};

function readStorageArray<T>(key: string): T[] {
  if (typeof window === 'undefined') return [];
  try {
    const raw = window.localStorage.getItem(key);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as unknown;
    return Array.isArray(parsed) ? parsed as T[] : [];
  } catch {
    return [];
  }
}

function writeStorage(key: string, value: unknown) {
  if (typeof window === 'undefined') return;
  window.localStorage.setItem(key, JSON.stringify(value));
}

function createLocalId(prefix: string) {
  const randomId = globalThis.crypto?.randomUUID?.();
  if (randomId) return `${prefix}-${randomId}`;
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

function errorMessage(cause: unknown, fallback: string) {
  return cause instanceof Error ? cause.message : fallback;
}

function settledValue<T>(result: PromiseSettledResult<T>, label: string, errors: string[]): T | null {
  if (result.status === 'fulfilled') return result.value;
  errors.push(`${label}: ${errorMessage(result.reason, 'failed to load')}`);
  return null;
}

function lower(value: unknown) {
  return typeof value === 'string' ? value.trim().toLowerCase() : '';
}

function firstString(...values: unknown[]) {
  for (const value of values) {
    if (typeof value === 'string' && value.trim()) return value.trim();
    if (typeof value === 'number' && Number.isFinite(value)) return String(value);
  }
  return '';
}

function metadataString(metadata: Record<string, unknown> | null | undefined, keys: string[]) {
  if (!metadata) return '';
  for (const key of keys) {
    const value = metadata[key];
    if (typeof value === 'string' && value.trim()) return value.trim();
    if (typeof value === 'number' && Number.isFinite(value)) return String(value);
  }
  return '';
}

function formatDate(value: string | null | undefined) {
  if (!value) return 'Never';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date);
}

function formatFreshness(seconds: number) {
  if (!Number.isFinite(seconds)) return 'Unknown';
  if (seconds < 60) return `${Math.round(seconds)} sec`;
  if (seconds < 3600) return `${Math.round(seconds / 60)} min`;
  if (seconds < 86_400) return `${Math.round(seconds / 3600)} hr`;
  return `${Math.round(seconds / 86_400)} day`;
}

function statusTone(status: RollupStatus) {
  if (status === 'healthy') return 'of-status-success';
  if (status === 'warning') return 'of-status-warning';
  if (status === 'critical') return 'of-status-danger';
  return 'of-status-info';
}

function statusBorder(status: RollupStatus): CSSProperties {
  if (status === 'healthy') return { borderLeft: '4px solid var(--status-success)' };
  if (status === 'warning') return { borderLeft: '4px solid var(--status-warning)' };
  if (status === 'critical') return { borderLeft: '4px solid var(--status-danger)' };
  return { borderLeft: '4px solid var(--status-info)' };
}

function normalizeStatus(status: unknown): RollupStatus {
  const value = lower(status);
  if (!value) return 'unknown';
  if (['healthy', 'ok', 'success', 'succeeded', 'active', 'online', 'running', 'published', 'deployed', 'passed'].includes(value)) {
    return 'healthy';
  }
  if (['warning', 'warn', 'degraded', 'stale', 'ignored', 'paused', 'draft', 'experimental', 'not_run', 'queued', 'pending', 'configuring'].includes(value)) {
    return 'warning';
  }
  if (['critical', 'error', 'failed', 'failure', 'offline', 'cancelled', 'canceled', 'aborted', 'deprecated'].includes(value)) {
    return 'critical';
  }
  return 'unknown';
}

function rollupStatus(checks: HealthCheck[]) {
  if (checks.length === 0) return 'unknown';
  return checks.reduce<RollupStatus>((current, check) => (
    STATUS_RANK[check.status] > STATUS_RANK[current] ? check.status : current
  ), 'healthy');
}

function createCheck(
  resourceId: string,
  kind: string,
  label: string,
  status: RollupStatus,
  message: string,
  watchedChecks: Set<string>,
  options: { observedAt?: string | null; recommendation?: string } = {},
): HealthCheck {
  const id = `${resourceId}:${kind}`;
  return {
    id,
    kind,
    label,
    status,
    message,
    observedAt: options.observedAt,
    recommendation: options.recommendation,
    watched: watchedChecks.has(id),
  };
}

function datasetRid(dataset: Dataset) {
  return dataset.rid || dataset.id;
}

function datasetName(dataset: Dataset) {
  return dataset.display_name || dataset.name || dataset.id;
}

function isStreamingDataset(dataset: Dataset) {
  const text = [
    dataset.format,
    ...(dataset.tags ?? []),
    metadataString(dataset.metadata, ['kind', 'type', 'resource_type', 'source_type']),
  ].join(' ').toLowerCase();
  return text.includes('stream');
}

function freshnessStatus(seconds: number) {
  if (!Number.isFinite(seconds)) return 'unknown';
  if (seconds > 2 * 86_400) return 'critical';
  if (seconds > 86_400) return 'warning';
  return 'healthy';
}

function datasetChecks(
  dataset: Dataset,
  health: DatasetHealthResponse | null | undefined,
  watchedChecks: Set<string>,
  resourceId: string,
) {
  const checks: HealthCheck[] = [];
  checks.push(createCheck(
    resourceId,
    'resource_health',
    'Resource health',
    health ? 'healthy' : normalizeStatus(dataset.health_status),
    health
      ? `Health snapshot computed ${formatDate(health.last_computed_at)}.`
      : dataset.health_status
        ? `Latest catalog health status is ${dataset.health_status}.`
        : 'No dataset health snapshot has been reported yet.',
    watchedChecks,
    { observedAt: health?.last_computed_at ?? dataset.updated_at },
  ));
  checks.push(createCheck(
    resourceId,
    'freshness',
    'Freshness',
    health ? freshnessStatus(health.freshness_seconds) : 'unknown',
    health
      ? `Last committed data is ${formatFreshness(health.freshness_seconds)} old.`
      : 'Freshness is not available for this dataset.',
    watchedChecks,
    { observedAt: health?.last_commit_at ?? dataset.updated_at },
  ));
  checks.push(createCheck(
    resourceId,
    'last_build',
    'Last build',
    health ? normalizeStatus(health.last_build_status) : 'unknown',
    health ? `Last build status is ${health.last_build_status}.` : 'No build status snapshot is available.',
    watchedChecks,
    { observedAt: health?.last_computed_at ?? dataset.updated_at },
  ));
  checks.push(createCheck(
    resourceId,
    'transaction_failures',
    'Transaction failures',
    health ? (health.txn_failure_rate_24h > 0.15 ? 'critical' : health.txn_failure_rate_24h > 0 ? 'warning' : 'healthy') : 'unknown',
    health ? `24h transaction failure rate is ${(health.txn_failure_rate_24h * 100).toFixed(1)}%.` : 'Transaction failure rate is unknown.',
    watchedChecks,
    { observedAt: health?.last_computed_at ?? dataset.updated_at },
  ));
  checks.push(createCheck(
    resourceId,
    'schema_drift',
    'Schema drift',
    health ? (health.schema_drift_flag ? 'critical' : 'healthy') : 'unknown',
    health ? (health.schema_drift_flag ? 'Schema drift detected.' : 'No schema drift detected.') : 'Schema drift has not been evaluated.',
    watchedChecks,
    { observedAt: health?.last_computed_at ?? dataset.updated_at },
  ));
  return checks;
}

function datasetResource(
  dataset: Dataset,
  healthByRid: Record<string, DatasetHealthResponse | null>,
  watchedChecks: Set<string>,
  resourceClass: 'dataset' | 'streaming_dataset',
): MonitorableResource {
  const rid = datasetRid(dataset);
  const id = `${resourceClass}:${rid}`;
  const checks = datasetChecks(dataset, healthByRid[rid], watchedChecks, id);
  return {
    id,
    rid,
    name: datasetName(dataset),
    resourceClass,
    project: dataset.project_rid || dataset.project_id || null,
    folder: dataset.folder_path || dataset.parent_folder_rid || dataset.path || null,
    owner: dataset.owner_id || null,
    branch: dataset.active_branch || null,
    href: dataset.links?.preview || `/datasets/${encodeURIComponent(dataset.id)}`,
    updatedAt: dataset.updated_at,
    status: rollupStatus(checks),
    checks,
    metadata: {
      rows: dataset.row_count,
      size_bytes: dataset.size_bytes,
      format: dataset.format,
      visibility: dataset.resource_visibility,
    },
  };
}

function scheduleChecks(schedule: Schedule, watchedChecks: Set<string>, resourceId: string) {
  const checks: HealthCheck[] = [];
  const outcome = schedule.last_run_outcome;
  checks.push(createCheck(
    resourceId,
    'latest_run',
    'Latest run',
    outcome ? normalizeStatus(outcome) : 'warning',
    outcome
      ? `Latest run ${outcome.toLowerCase()}${schedule.last_run_build_rid ? ` with build ${schedule.last_run_build_rid}` : ''}.`
      : 'Schedule has no recorded run yet.',
    watchedChecks,
    { observedAt: schedule.last_run_at },
  ));
  checks.push(createCheck(
    resourceId,
    'pause_state',
    'Pause state',
    schedule.paused ? 'warning' : 'healthy',
    schedule.paused ? `Paused${schedule.paused_reason ? `: ${schedule.paused_reason}` : '.'}` : 'Schedule is active.',
    watchedChecks,
    { observedAt: schedule.paused_at ?? schedule.updated_at },
  ));
  checks.push(createCheck(
    resourceId,
    'pending_trigger',
    'Pending trigger',
    schedule.pending_re_run ? 'warning' : 'healthy',
    schedule.pending_re_run
      ? 'A trigger fired while another run was active and is pending.'
      : schedule.active_run_id
        ? `Run ${schedule.active_run_id} is active.`
        : 'No pending trigger is recorded.',
    watchedChecks,
    { observedAt: schedule.last_triggered_at ?? schedule.updated_at },
  ));
  checks.push(createCheck(
    resourceId,
    'target_coverage',
    'Target coverage',
    schedule.target_rids.length > 0 ? 'healthy' : 'critical',
    schedule.target_rids.length > 0
      ? `${schedule.target_rids.length} target resource(s) are indexed.`
      : 'No build targets are indexed for this schedule.',
    watchedChecks,
    { observedAt: schedule.updated_at },
  ));
  return checks;
}

function scheduleResource(schedule: Schedule, watchedChecks: Set<string>): MonitorableResource {
  const id = `schedule:${schedule.rid || schedule.id}`;
  const checks = scheduleChecks(schedule, watchedChecks, id);
  return {
    id,
    rid: schedule.rid,
    name: schedule.name || schedule.rid,
    resourceClass: 'schedule',
    project: schedule.project_rid || null,
    folder: schedule.folder_rid || null,
    owner: schedule.owner || schedule.created_by || null,
    branch: schedule.branch || null,
    href: `/schedules/${encodeURIComponent(schedule.rid || schedule.id)}`,
    updatedAt: schedule.updated_at,
    status: rollupStatus(checks),
    checks,
    metadata: {
      build_strategy: schedule.build_strategy,
      target_count: schedule.target_rids.length,
      run_as_identity: schedule.run_as_identity,
    },
  };
}

function pipelineChecks(pipeline: Pipeline, watchedChecks: Set<string>, resourceId: string) {
  const checks: HealthCheck[] = [];
  checks.push(createCheck(
    resourceId,
    'pipeline_status',
    'Pipeline status',
    normalizeStatus(pipeline.status),
    `Pipeline status is ${pipeline.status || 'unknown'}.`,
    watchedChecks,
    { observedAt: pipeline.updated_at },
  ));
  checks.push(createCheck(
    resourceId,
    'published_logic',
    'Published logic',
    pipeline.published_at || pipeline.active_version_id ? 'healthy' : 'warning',
    pipeline.published_at || pipeline.active_version_id
      ? `Published logic is available${pipeline.active_version_id ? ` at version ${pipeline.active_version_id}` : ''}.`
      : 'No published pipeline version is active.',
    watchedChecks,
    { observedAt: pipeline.published_at ?? pipeline.updated_at },
  ));
  checks.push(createCheck(
    resourceId,
    'schedule_config',
    'Schedule config',
    pipeline.schedule_config?.enabled ? (pipeline.next_run_at ? 'healthy' : 'warning') : 'unknown',
    pipeline.schedule_config?.enabled
      ? pipeline.next_run_at
        ? `Next run is ${formatDate(pipeline.next_run_at)}.`
        : 'Schedule is enabled but no next run is available.'
      : 'Pipeline has no enabled schedule.',
    watchedChecks,
    { observedAt: pipeline.updated_at },
  ));
  if (pipeline.streaming_config) {
    checks.push(createCheck(
      resourceId,
      'streaming_runtime',
      'Streaming runtime',
      'healthy',
      `Streaming profile ${pipeline.streaming_config.streaming_profile_id || 'default'} with parallelism ${pipeline.streaming_config.parallelism}.`,
      watchedChecks,
      { observedAt: pipeline.updated_at },
    ));
  }
  return checks;
}

function pipelineResource(pipeline: Pipeline, watchedChecks: Set<string>): MonitorableResource {
  const id = `pipeline:${pipeline.id}`;
  const checks = pipelineChecks(pipeline, watchedChecks, id);
  return {
    id,
    rid: pipeline.id,
    name: pipeline.name || pipeline.id,
    resourceClass: 'pipeline',
    project: pipeline.project_id || null,
    folder: null,
    owner: pipeline.owner_id || null,
    branch: pipeline.branch_name || null,
    href: `/pipelines/${encodeURIComponent(pipeline.id)}/edit`,
    updatedAt: pipeline.updated_at,
    status: rollupStatus(checks),
    checks,
    metadata: {
      lifecycle: pipeline.lifecycle,
      type: pipeline.pipeline_type,
      compute_profile_id: pipeline.compute_profile_id,
    },
  };
}

function streamResource(stream: DataConnectionStreamResource, watchedChecks: Set<string>): MonitorableResource {
  const rid = stream.rid || stream.id;
  const id = `streaming_dataset:${rid}`;
  const failedCheckpoints = (stream.checkpoints ?? []).filter((checkpoint) => ['failed', 'error'].includes(String(checkpoint.status).toLowerCase()));
  const lag = stream.offsets?.lag ?? 0;
  const checks = [
    createCheck(
      id,
      'stream_health',
      'Stream health',
      normalizeStatus(stream.health?.state),
      stream.health?.message || `Stream health is ${stream.health?.state || 'unknown'}.`,
      watchedChecks,
      { observedAt: stream.health?.last_checked_at ?? stream.updated_at },
    ),
    createCheck(
      id,
      'stream_lag',
      'Stream lag',
      lag > 100_000 ? 'critical' : lag > 0 ? 'warning' : 'healthy',
      lag > 0 ? `${lag} record(s) of lag.` : 'No visible stream lag.',
      watchedChecks,
      { observedAt: stream.updated_at },
    ),
    createCheck(
      id,
      'checkpoint_failures',
      'Checkpoint failures',
      failedCheckpoints.length > 0 ? 'critical' : 'healthy',
      failedCheckpoints.length > 0
        ? `${failedCheckpoints.length} failed checkpoint(s) require attention.`
        : 'No failed checkpoints are visible.',
      watchedChecks,
      { observedAt: stream.updated_at },
    ),
  ];
  return {
    id,
    rid,
    name: stream.name,
    resourceClass: 'streaming_dataset',
    project: null,
    folder: stream.hot_buffer?.cold_dataset_id || stream.cold_storage?.cold_dataset_id || null,
    owner: null,
    branch: stream.branch,
    href: `/streaming/${encodeURIComponent(stream.id)}`,
    updatedAt: stream.updated_at,
    status: rollupStatus(checks),
    checks,
    metadata: {
      consistency: stream.consistency_guarantee,
      consumers: stream.consumers.length,
      source_sync_ids: stream.source_sync_ids,
    },
  };
}

function sourceById(sources: Source[]) {
  return Object.fromEntries(sources.map((source) => [source.id, source]));
}

function agentResource(
  agent: ConnectorAgent,
  sources: Record<string, Source>,
  watchedChecks: Set<string>,
): MonitorableResource {
  const id = `agent:${agent.id}`;
  const firstSource = agent.connected_sources.map((source) => sources[source.source_id]).find(Boolean);
  const checks = [
    createCheck(
      id,
      'agent_health',
      'Agent health',
      normalizeStatus(agent.health?.state || agent.status),
      agent.health?.message || `Agent status is ${agent.health?.state || agent.status || 'unknown'}.`,
      watchedChecks,
      { observedAt: agent.last_heartbeat_at ?? agent.updated_at },
    ),
    createCheck(
      id,
      'heartbeat',
      'Heartbeat',
      agent.health?.stale || !agent.last_heartbeat_at ? 'warning' : 'healthy',
      agent.last_heartbeat_at
        ? `Last heartbeat ${formatDate(agent.last_heartbeat_at)}.`
        : 'No heartbeat has been recorded.',
      watchedChecks,
      { observedAt: agent.last_heartbeat_at ?? agent.updated_at },
    ),
    createCheck(
      id,
      'connection_failures',
      'Connection failures',
      agent.connection_failures.length > 0 ? 'critical' : 'healthy',
      agent.connection_failures.length > 0
        ? `${agent.connection_failures.length} connection failure(s) are visible.`
        : 'No connector failure is visible.',
      watchedChecks,
      { observedAt: agent.updated_at },
    ),
    createCheck(
      id,
      'connected_sources',
      'Connected sources',
      agent.connected_sources.length > 0 ? 'healthy' : 'warning',
      `${agent.connected_sources.length} connected source(s).`,
      watchedChecks,
      { observedAt: agent.updated_at },
    ),
  ];
  return {
    id,
    rid: agent.id,
    name: agent.name || agent.id,
    resourceClass: 'agent',
    project: metadataString(agent.metadata, ['project_rid', 'project_id', 'project']) || firstSource?.project_rid || null,
    folder: metadataString(agent.metadata, ['folder_rid', 'folder_path', 'folder']) || firstSource?.folder_rid || null,
    owner: agent.owner_id || firstSource?.owner_id || null,
    branch: null,
    href: '/data-connection/agents',
    updatedAt: agent.updated_at,
    status: rollupStatus(checks),
    checks,
    metadata: {
      environment: agent.environment,
      host: agent.host,
      version: agent.version,
    },
  };
}

function objectTypeResource(objectType: ObjectType, watchedChecks: Set<string>): MonitorableResource {
  const rid = objectType.rid || objectType.id;
  const id = `object_type:${rid}`;
  const checks = [
    createCheck(
      id,
      'object_type_status',
      'Object type status',
      normalizeStatus(objectType.status || 'active'),
      `Object type status is ${objectType.status || 'active'}.`,
      watchedChecks,
      { observedAt: objectType.updated_at },
    ),
    createCheck(
      id,
      'backing_resource',
      'Backing resource',
      objectType.backing_dataset_rid || objectType.backing_dataset_id || objectType.backing_restricted_view_id ? 'healthy' : 'warning',
      objectType.backing_dataset_rid || objectType.backing_dataset_id || objectType.backing_restricted_view_id
        ? 'Backing dataset or restricted view is configured.'
        : 'No backing dataset or restricted view is visible.',
      watchedChecks,
      { observedAt: objectType.updated_at },
    ),
    createCheck(
      id,
      'primary_key',
      'Primary key',
      objectType.primary_key_property || objectType.primary_key ? 'healthy' : 'warning',
      objectType.primary_key_property || objectType.primary_key
        ? `Primary key is ${objectType.primary_key_property || objectType.primary_key}.`
        : 'Primary key is not configured.',
      watchedChecks,
      { observedAt: objectType.updated_at },
    ),
  ];
  return {
    id,
    rid,
    name: objectType.display_name || objectType.name || objectType.id,
    resourceClass: 'object_type',
    project: objectType.pipeline_rid || null,
    folder: null,
    owner: objectType.owner_id || null,
    branch: null,
    href: `/ontology/${encodeURIComponent(objectType.id)}`,
    updatedAt: objectType.updated_at,
    status: rollupStatus(checks),
    checks,
    metadata: {
      api_name: objectType.api_name,
      visibility: objectType.visibility,
      backing_dataset_rid: objectType.backing_dataset_rid,
    },
  };
}

function actionResource(action: ActionType, watchedChecks: Set<string>): MonitorableResource {
  const id = `action:${action.id}`;
  const checks = [
    createCheck(
      id,
      'authorization',
      'Authorization',
      action.permission_key ? 'healthy' : 'warning',
      action.permission_key ? `Permission key ${action.permission_key} is configured.` : 'No permission key is configured.',
      watchedChecks,
      { observedAt: action.updated_at },
    ),
    createCheck(
      id,
      'form_schema',
      'Form schema',
      action.input_schema.length > 0 ? 'healthy' : 'warning',
      `${action.input_schema.length} input field(s) are configured.`,
      watchedChecks,
      { observedAt: action.updated_at },
    ),
    createCheck(
      id,
      'operation_kind',
      'Operation kind',
      action.operation_kind ? 'healthy' : 'unknown',
      `Operation kind is ${action.operation_kind || 'unknown'}.`,
      watchedChecks,
      { observedAt: action.updated_at },
    ),
  ];
  return {
    id,
    rid: action.id,
    name: action.display_name || action.name || action.id,
    resourceClass: 'action',
    project: action.object_type_id || action.interface_id || null,
    folder: null,
    owner: action.owner_id || null,
    branch: null,
    href: `/action-types/${encodeURIComponent(action.id)}`,
    updatedAt: action.updated_at,
    status: rollupStatus(checks),
    checks,
    metadata: {
      object_type_id: action.object_type_id,
      interface_id: action.interface_id,
      operation_kind: action.operation_kind,
    },
  };
}

function functionResource(fn: FunctionPackage, watchedChecks: Set<string>): MonitorableResource {
  const id = `function:${fn.id}`;
  const checks = [
    createCheck(
      id,
      'runtime',
      'Runtime',
      fn.runtime ? 'healthy' : 'warning',
      fn.runtime ? `Runtime is ${fn.runtime}.` : 'Runtime is not configured.',
      watchedChecks,
      { observedAt: fn.updated_at },
    ),
    createCheck(
      id,
      'entrypoint',
      'Entrypoint',
      fn.entrypoint ? 'healthy' : 'critical',
      fn.entrypoint ? `Entrypoint is ${fn.entrypoint}.` : 'Entrypoint is missing.',
      watchedChecks,
      { observedAt: fn.updated_at },
    ),
    createCheck(
      id,
      'source',
      'Source',
      fn.source ? 'healthy' : 'warning',
      fn.source ? 'Function source is available.' : 'No function source is visible.',
      watchedChecks,
      { observedAt: fn.updated_at },
    ),
  ];
  return {
    id,
    rid: fn.id,
    name: fn.display_name || fn.name || fn.id,
    resourceClass: 'function',
    project: null,
    folder: null,
    owner: fn.owner_id || null,
    branch: null,
    href: '/functions',
    updatedAt: fn.updated_at,
    status: rollupStatus(checks),
    checks,
    metadata: {
      runtime: fn.runtime,
      version: fn.version,
      entrypoint: fn.entrypoint,
    },
  };
}

function automationResource(workflow: WorkflowDefinition, watchedChecks: Set<string>): MonitorableResource {
  const id = `automation:${workflow.id}`;
  const checks = [
    createCheck(
      id,
      'workflow_status',
      'Automation status',
      normalizeStatus(workflow.status),
      `Automation status is ${workflow.status || 'unknown'}.`,
      watchedChecks,
      { observedAt: workflow.updated_at },
    ),
    createCheck(
      id,
      'steps',
      'Steps',
      workflow.steps.length > 0 ? 'healthy' : 'critical',
      `${workflow.steps.length} step(s) are configured.`,
      watchedChecks,
      { observedAt: workflow.updated_at },
    ),
    createCheck(
      id,
      'trigger',
      'Trigger',
      workflow.trigger_type ? 'healthy' : 'warning',
      workflow.trigger_type ? `Trigger is ${workflow.trigger_type}.` : 'Trigger is not configured.',
      watchedChecks,
      { observedAt: workflow.last_triggered_at ?? workflow.updated_at },
    ),
  ];
  return {
    id,
    rid: workflow.id,
    name: workflow.name || workflow.id,
    resourceClass: 'automation',
    project: null,
    folder: null,
    owner: workflow.owner_id || null,
    branch: null,
    href: '/workflows',
    updatedAt: workflow.updated_at,
    status: rollupStatus(checks),
    checks,
    metadata: {
      trigger_type: workflow.trigger_type,
      next_run_at: workflow.next_run_at,
      last_triggered_at: workflow.last_triggered_at,
    },
  };
}

function lineageClass(node: LineageNode): MonitorResourceClass | null {
  const kind = node.kind.replaceAll('-', '_').toLowerCase();
  if (['object_type', 'ontology_object_type'].includes(kind)) return 'object_type';
  if (['function', 'function_package', 'logic_function'].includes(kind)) return 'function';
  if (['action', 'action_type'].includes(kind)) return 'action';
  if (['automation', 'automate', 'workflow', 'workflow_handoff'].includes(kind)) return 'automation';
  if (['agent', 'aip_agent', 'connector_agent'].includes(kind)) return 'agent';
  if (['pipeline', 'transform', 'pipeline_resource'].includes(kind)) return 'pipeline';
  return null;
}

function lineageResource(node: LineageNode, watchedChecks: Set<string>): MonitorableResource | null {
  const resourceClass = lineageClass(node);
  if (!resourceClass) return null;
  const id = `${resourceClass}:lineage:${node.id}`;
  const statusValue = firstString(
    node.metadata.health_status,
    node.metadata.status,
    node.metadata.state,
    node.metadata.last_run_status,
  );
  const checks = [
    createCheck(
      id,
      'lineage_health',
      'Lineage health',
      statusValue ? normalizeStatus(statusValue) : 'unknown',
      statusValue ? `Lineage metadata reports ${statusValue}.` : 'No dedicated health signal is present in lineage metadata.',
      watchedChecks,
      { observedAt: firstString(node.metadata.updated_at, node.metadata.last_observed_at) || null },
    ),
    createCheck(
      id,
      'source_reference',
      'Code/source reference',
      firstString(node.metadata.repository, node.metadata.code_url, node.metadata.source_ref, node.metadata.path) ? 'healthy' : 'unknown',
      firstString(node.metadata.repository, node.metadata.code_url, node.metadata.source_ref, node.metadata.path)
        ? 'Source or code reference is attached.'
        : 'No source reference is attached in lineage metadata.',
      watchedChecks,
      { observedAt: firstString(node.metadata.updated_at, node.metadata.last_observed_at) || null },
    ),
  ];
  return {
    id,
    rid: node.id,
    name: node.label || node.id,
    resourceClass,
    project: metadataString(node.metadata, ['project_rid', 'project_id', 'project']),
    folder: metadataString(node.metadata, ['folder_rid', 'folder_path', 'folder', 'path']),
    owner: metadataString(node.metadata, ['owner_id', 'owner']),
    branch: metadataString(node.metadata, ['branch', 'branch_name']),
    href: '/lineage',
    updatedAt: firstString(node.metadata.updated_at, node.metadata.last_observed_at) || null,
    status: rollupStatus(checks),
    checks,
    metadata: node.metadata,
  };
}

function matchesScope(resource: MonitorableResource, scopeKind: ScopeKind, scopeValue: string, resourceType: MonitorResourceClass) {
  const value = scopeValue.trim().toLowerCase();
  if (scopeKind === 'all') return true;
  if (scopeKind === 'resource_type') return resource.resourceClass === resourceType;
  if (!value) return true;
  if (scopeKind === 'project') return [resource.project, resource.metadata.project, resource.metadata.project_rid, resource.metadata.project_id].some((item) => lower(item).includes(value));
  if (scopeKind === 'folder') return [resource.folder, resource.metadata.folder, resource.metadata.folder_rid, resource.metadata.folder_path, resource.metadata.path].some((item) => lower(item).includes(value));
  return [
    resource.id,
    resource.rid,
    resource.name,
    resource.owner,
    resource.branch,
    ...resource.checks.flatMap((check) => [check.label, check.message]),
  ].some((item) => lower(item).includes(value));
}

function matchesSearch(resource: MonitorableResource, query: string) {
  const value = query.trim().toLowerCase();
  if (!value) return true;
  return [
    resource.id,
    resource.rid,
    resource.name,
    resource.owner,
    resource.project,
    resource.folder,
    resource.branch,
    RESOURCE_CLASS_LABELS[resource.resourceClass],
    ...resource.checks.flatMap((check) => [check.label, check.message, check.kind]),
  ].some((item) => lower(item).includes(value));
}

function visibleChecks(checks: HealthCheck[], mode: CheckMode) {
  if (mode === 'watched') return checks.filter((check) => check.watched);
  if (mode === 'attention') return checks.filter((check) => check.status !== 'healthy');
  return checks;
}

function sortResources(resources: MonitorableResource[]) {
  return [...resources].sort((left, right) => (
    STATUS_RANK[right.status] - STATUS_RANK[left.status]
    || left.resourceClass.localeCompare(right.resourceClass)
    || left.name.localeCompare(right.name)
  ));
}

function uniqueValues(resources: MonitorableResource[], selector: (resource: MonitorableResource) => string | null | undefined) {
  return Array.from(new Set(resources.map(selector).filter((value): value is string => Boolean(value)))).sort();
}

export function DataHealthPage() {
  const [datasets, setDatasets] = useState<Dataset[]>([]);
  const [datasetHealth, setDatasetHealth] = useState<Record<string, DatasetHealthResponse | null>>({});
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [pipelines, setPipelines] = useState<Pipeline[]>([]);
  const [sources, setSources] = useState<Source[]>([]);
  const [agents, setAgents] = useState<ConnectorAgent[]>([]);
  const [streams, setStreams] = useState<DataConnectionStreamResource[]>([]);
  const [objectTypes, setObjectTypes] = useState<ObjectType[]>([]);
  const [actionTypes, setActionTypes] = useState<ActionType[]>([]);
  const [functionPackages, setFunctionPackages] = useState<FunctionPackage[]>([]);
  const [workflows, setWorkflows] = useState<WorkflowDefinition[]>([]);
  const [lineage, setLineage] = useState<LineageGraph | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [partialErrors, setPartialErrors] = useState<string[]>([]);
  const [scopeKind, setScopeKind] = useState<ScopeKind>('all');
  const [scopeValue, setScopeValue] = useState('');
  const [resourceType, setResourceType] = useState<MonitorResourceClass>('dataset');
  const [checkMode, setCheckMode] = useState<CheckMode>('all');
  const [search, setSearch] = useState('');
  const [viewName, setViewName] = useState('');
  const [watchedChecks, setWatchedChecks] = useState<Set<string>>(
    () => new Set(readStorageArray<string>(WATCHED_CHECKS_KEY)),
  );
  const [savedViews, setSavedViews] = useState<SavedHealthView[]>(
    () => readStorageArray<SavedHealthView>(SAVED_VIEWS_KEY),
  );

  async function refresh() {
    setLoading(true);
    setError('');
    const errors: string[] = [];
    try {
      const [
        datasetResult,
        scheduleResult,
        pipelineResult,
        sourceResult,
        agentResult,
        streamResult,
        lineageResult,
        objectTypeResult,
        actionTypeResult,
        functionResult,
        workflowResult,
      ] = await Promise.allSettled([
        listDatasets({ page: 1, per_page: 100, limit: 200 }),
        listSchedules({ limit: 200 }),
        listPipelines({ page: 1, per_page: 100 }),
        dataConnection.listSources({ page: 1, per_page: 100 }),
        dataConnection.listConnectorAgents(),
        dataConnection.listStreams(),
        getFullLineage(),
        listObjectTypes({ page: 1, per_page: 100 }),
        listActionTypes({ page: 1, per_page: 100 }),
        listFunctionPackages({ page: 1, per_page: 100 }),
        listWorkflows({ page: 1, per_page: 100 }),
      ]);

      const datasetPayload = settledValue(datasetResult, 'Datasets', errors);
      const loadedDatasets = datasetPayload?.data ?? [];
      setDatasets(loadedDatasets);
      setSchedules(settledValue(scheduleResult, 'Schedules', errors)?.data ?? []);
      setPipelines(settledValue(pipelineResult, 'Pipelines', errors)?.data ?? []);
      setSources(settledValue(sourceResult, 'Data connection sources', errors)?.data ?? []);
      setAgents(settledValue(agentResult, 'Connector agents', errors) ?? []);
      setStreams(settledValue(streamResult, 'Streaming datasets', errors) ?? []);
      setLineage(settledValue(lineageResult, 'Lineage graph', errors));
      setObjectTypes(settledValue(objectTypeResult, 'Object types', errors)?.data ?? []);
      setActionTypes(settledValue(actionTypeResult, 'Actions', errors)?.data ?? []);
      setFunctionPackages(settledValue(functionResult, 'Functions', errors)?.data ?? []);
      setWorkflows(settledValue(workflowResult, 'Automations', errors)?.data ?? []);

      const healthEntries = await Promise.all(loadedDatasets.slice(0, MAX_DATASET_HEALTH_PROBES).map(async (dataset) => {
        const rid = datasetRid(dataset);
        try {
          return [rid, await getDatasetHealth(rid)] as const;
        } catch {
          return [rid, null] as const;
        }
      }));
      setDatasetHealth(Object.fromEntries(healthEntries));
      setPartialErrors(errors);
    } catch (cause) {
      setError(errorMessage(cause, 'Failed to load Data Health resources'));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  useEffect(() => {
    writeStorage(WATCHED_CHECKS_KEY, Array.from(watchedChecks));
  }, [watchedChecks]);

  useEffect(() => {
    writeStorage(SAVED_VIEWS_KEY, savedViews);
  }, [savedViews]);

  const resources = useMemo(() => {
    const sourceLookup = sourceById(sources);
    const baseResources: MonitorableResource[] = [
      ...datasets.map((dataset) => datasetResource(dataset, datasetHealth, watchedChecks, 'dataset')),
      ...datasets.filter(isStreamingDataset).map((dataset) => datasetResource(dataset, datasetHealth, watchedChecks, 'streaming_dataset')),
      ...schedules.map((schedule) => scheduleResource(schedule, watchedChecks)),
      ...pipelines.map((pipeline) => pipelineResource(pipeline, watchedChecks)),
      ...streams.map((stream) => streamResource(stream, watchedChecks)),
      ...agents.map((agent) => agentResource(agent, sourceLookup, watchedChecks)),
      ...objectTypes.map((objectType) => objectTypeResource(objectType, watchedChecks)),
      ...actionTypes.map((action) => actionResource(action, watchedChecks)),
      ...functionPackages.map((fn) => functionResource(fn, watchedChecks)),
      ...workflows.map((workflow) => automationResource(workflow, watchedChecks)),
    ];
    const existingIds = new Set(baseResources.map((resource) => resource.id));
    const lineageResources = (lineage?.nodes ?? [])
      .map((node) => lineageResource(node, watchedChecks))
      .filter((resource): resource is MonitorableResource => Boolean(resource))
      .filter((resource) => !existingIds.has(resource.id.replace(':lineage:', ':')));
    return sortResources([...baseResources, ...lineageResources]);
  }, [
    actionTypes,
    agents,
    datasetHealth,
    datasets,
    functionPackages,
    lineage,
    objectTypes,
    pipelines,
    schedules,
    sources,
    streams,
    watchedChecks,
    workflows,
  ]);

  const scopedResources = useMemo(() => resources.filter((resource) => (
    matchesScope(resource, scopeKind, scopeValue, resourceType) && matchesSearch(resource, search)
  )), [resources, resourceType, scopeKind, scopeValue, search]);

  const displayResources = useMemo(() => scopedResources
    .map((resource) => ({ ...resource, checks: visibleChecks(resource.checks, checkMode) }))
    .filter((resource) => checkMode === 'all' || resource.checks.length > 0), [checkMode, scopedResources]);

  const reportSignals = useMemo(() => scopedResources.flatMap(healthReportsToSignalsFromResource), [scopedResources]);

  const rollup = useMemo(() => {
    const counts: Record<RollupStatus, number> = { healthy: 0, warning: 0, critical: 0, unknown: 0 };
    const byClass = Object.fromEntries(RESOURCE_CLASS_OPTIONS.map((option) => [option.value, 0])) as Record<MonitorResourceClass, number>;
    let checkCount = 0;
    let watchedCount = 0;
    for (const resource of scopedResources) {
      counts[resource.status] += 1;
      byClass[resource.resourceClass] += 1;
      checkCount += resource.checks.length;
      watchedCount += resource.checks.filter((check) => check.watched).length;
    }
    return {
      counts,
      byClass,
      checkCount,
      watchedCount,
      overall: rollupStatus(scopedResources.flatMap((resource) => resource.checks)),
    };
  }, [scopedResources]);

  const watchedCheckItems = useMemo(() => scopedResources.flatMap((resource) => (
    resource.checks
      .filter((check) => check.watched)
      .map((check) => ({ resource, check }))
  )), [scopedResources]);

  const projects = useMemo(() => uniqueValues(resources, (resource) => resource.project), [resources]);
  const folders = useMemo(() => uniqueValues(resources, (resource) => resource.folder), [resources]);

  function toggleWatched(checkId: string) {
    setWatchedChecks((current) => {
      const next = new Set(current);
      if (next.has(checkId)) next.delete(checkId);
      else next.add(checkId);
      return next;
    });
  }

  function saveCurrentView() {
    const nextView: SavedHealthView = {
      id: createLocalId('health-view'),
      name: viewName.trim() || `${SCOPE_LABELS[scopeKind]} view`,
      scopeKind,
      scopeValue,
      resourceType,
      checkMode,
      search,
      createdAt: new Date().toISOString(),
    };
    setSavedViews((current) => [nextView, ...current.filter((view) => view.name !== nextView.name)].slice(0, 24));
    setViewName('');
  }

  function applySavedView(view: SavedHealthView) {
    setScopeKind(view.scopeKind);
    setScopeValue(view.scopeValue);
    setResourceType(view.resourceType);
    setCheckMode(view.checkMode);
    setSearch(view.search);
  }

  function deleteSavedView(id: string) {
    setSavedViews((current) => current.filter((view) => view.id !== id));
  }

  return (
    <section className="of-page" style={{ padding: 24, display: 'grid', gap: 16 }}>
      <Link to="/control-panel" style={{ color: 'var(--text-muted)', fontSize: 13 }}>Control panel</Link>
      <header style={{ display: 'grid', gap: 8 }}>
        <div style={{ display: 'flex', alignItems: 'start', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
          <div>
            <h1 className="of-heading-xl">Data Health</h1>
            <p className="of-text-muted" style={{ marginTop: 4, maxWidth: 820 }}>
              Scope-based monitoring views for projects, folders, single resources, and resource types with watched checks and aggregate status rollups.
            </p>
          </div>
          <button type="button" className="of-button" onClick={() => void refresh()} disabled={loading}>
            {loading ? 'Refreshing' : 'Refresh'}
          </button>
        </div>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          <Link to="/lineage" className="of-button">Lineage</Link>
          <Link to="/datasets" className="of-button">Datasets</Link>
          <Link to="/build-schedules" className="of-button">Schedules</Link>
          <Link to="/data-connection" className="of-button">Data connection</Link>
        </div>
      </header>

      {error && (
        <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {error}
        </div>
      )}

      {partialErrors.length > 0 && (
        <div className="of-status-warning" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          Some monitorable classes are unavailable: {partialErrors.slice(0, 4).join(' | ')}
          {partialErrors.length > 4 ? ` | ${partialErrors.length - 4} more` : ''}
        </div>
      )}

      <section style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(150px, 1fr))', gap: 12 }}>
        <RollupCard label="Overall" value={STATUS_LABELS[rollup.overall]} status={rollup.overall} />
        <RollupCard label="Resources" value={scopedResources.length.toLocaleString()} />
        <RollupCard label="Critical" value={rollup.counts.critical.toLocaleString()} status="critical" />
        <RollupCard label="Warning" value={rollup.counts.warning.toLocaleString()} status="warning" />
        <RollupCard label="Healthy" value={rollup.counts.healthy.toLocaleString()} status="healthy" />
        <RollupCard label="Watched checks" value={rollup.watchedCount.toLocaleString()} status={rollup.watchedCount > 0 ? 'warning' : 'unknown'} />
      </section>

      <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 12 }}>
          <label style={{ fontSize: 13 }}>
            Monitoring scope
            <select value={scopeKind} onChange={(event) => setScopeKind(event.target.value as ScopeKind)} className="of-input" style={{ marginTop: 4 }}>
              {Object.entries(SCOPE_LABELS).map(([value, label]) => (
                <option key={value} value={value}>{label}</option>
              ))}
            </select>
          </label>
          {scopeKind === 'resource_type' ? (
            <label style={{ fontSize: 13 }}>
              Resource type
              <select value={resourceType} onChange={(event) => setResourceType(event.target.value as MonitorResourceClass)} className="of-input" style={{ marginTop: 4 }}>
                {RESOURCE_CLASS_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>{option.label}</option>
                ))}
              </select>
            </label>
          ) : (
            <label style={{ fontSize: 13 }}>
              Scope value
              <input
                value={scopeValue}
                onChange={(event) => setScopeValue(event.target.value)}
                placeholder={scopeKind === 'project' ? 'Project RID or name' : scopeKind === 'folder' ? 'Folder path or RID' : scopeKind === 'resource' ? 'RID, name, owner, branch' : 'Optional'}
                list={scopeKind === 'project' ? 'health-projects' : scopeKind === 'folder' ? 'health-folders' : undefined}
                className="of-input"
                style={{ marginTop: 4 }}
              />
            </label>
          )}
          <label style={{ fontSize: 13 }}>
            Check filter
            <select value={checkMode} onChange={(event) => setCheckMode(event.target.value as CheckMode)} className="of-input" style={{ marginTop: 4 }}>
              {Object.entries(CHECK_MODE_LABELS).map(([value, label]) => (
                <option key={value} value={value}>{label}</option>
              ))}
            </select>
          </label>
          <label style={{ fontSize: 13 }}>
            Search
            <input value={search} onChange={(event) => setSearch(event.target.value)} className="of-input" style={{ marginTop: 4 }} placeholder="Name, RID, owner, branch, check" />
          </label>
        </div>
        <datalist id="health-projects">
          {projects.map((project) => <option key={project} value={project} />)}
        </datalist>
        <datalist id="health-folders">
          {folders.map((folder) => <option key={folder} value={folder} />)}
        </datalist>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
          <input
            value={viewName}
            onChange={(event) => setViewName(event.target.value)}
            className="of-input"
            placeholder="Monitoring view name"
            style={{ maxWidth: 260 }}
          />
          <button type="button" className="of-button" onClick={saveCurrentView}>Save view</button>
          <span className="of-text-muted" style={{ fontSize: 12 }}>
            {rollup.checkCount.toLocaleString()} checks across {scopedResources.length.toLocaleString()} resource(s).
          </span>
        </div>
        {savedViews.length > 0 && (
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            {savedViews.map((view) => (
              <span key={view.id} className="of-chip" style={{ gap: 6 }}>
                <button type="button" onClick={() => applySavedView(view)} style={chipButtonStyle}>
                  {view.name}
                </button>
                <button type="button" onClick={() => deleteSavedView(view.id)} style={chipButtonStyle} title="Delete saved monitoring view">
                  x
                </button>
              </span>
            ))}
          </div>
        )}
      </section>

      <section className="of-panel" style={{ padding: 16, display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        {RESOURCE_CLASS_OPTIONS.map((option) => (
          <button
            key={option.value}
            type="button"
            className={`of-chip ${scopeKind === 'resource_type' && resourceType === option.value ? 'of-chip-active' : ''}`}
            onClick={() => {
              setScopeKind('resource_type');
              setResourceType(option.value);
            }}
            style={{ border: 0, cursor: 'pointer' }}
          >
            {option.label} {rollup.byClass[option.value].toLocaleString()}
          </button>
        ))}
      </section>

      <HealthReportsPanel signals={reportSignals} />

      {loading ? (
        <p className="of-text-muted">Loading Data Health resources...</p>
      ) : (
        <section style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 360px), 1fr))', gap: 16, alignItems: 'start' }}>
          <div style={{ display: 'grid', gap: 12, minWidth: 0 }}>
            {displayResources.length === 0 ? (
              <section className="of-panel" style={{ padding: 24, color: 'var(--text-muted)', fontSize: 13 }}>
                No resources match the active monitoring view.
              </section>
            ) : (
              displayResources.map((resource) => (
                <ResourcePanel key={resource.id} resource={resource} onToggleWatched={toggleWatched} />
              ))
            )}
          </div>

          <aside className="of-panel" style={{ padding: 16, display: 'grid', gap: 12, alignContent: 'start' }}>
            <div>
              <p className="of-eyebrow">Watched checks</p>
              <h2 className="of-heading-sm" style={{ marginTop: 4 }}>
                {watchedCheckItems.length.toLocaleString()} in this view
              </h2>
            </div>
            {watchedCheckItems.length === 0 ? (
              <p className="of-text-muted" style={{ fontSize: 13 }}>
                Watch individual checks from any resource to keep them pinned here.
              </p>
            ) : (
              watchedCheckItems.slice(0, 20).map(({ resource, check }) => (
                <div key={check.id} style={{ display: 'grid', gap: 4, borderTop: '1px solid var(--border-subtle)', paddingTop: 10 }}>
                  <div style={{ display: 'flex', gap: 8, justifyContent: 'space-between', alignItems: 'center' }}>
                    <strong style={{ fontSize: 13 }}>{check.label}</strong>
                    <span className={`of-chip ${statusTone(check.status)}`}>{STATUS_LABELS[check.status]}</span>
                  </div>
                  <span className="of-text-muted" style={{ fontSize: 12 }}>{resource.name} | {RESOURCE_CLASS_LABELS[resource.resourceClass]}</span>
                  <p style={{ margin: 0, fontSize: 12 }}>{check.message}</p>
                </div>
              ))
            )}
          </aside>
        </section>
      )}
    </section>
  );
}

const chipButtonStyle: CSSProperties = {
  border: 0,
  background: 'transparent',
  color: 'inherit',
  padding: 0,
  cursor: 'pointer',
  font: 'inherit',
};

function RollupCard({ label, value, status = 'unknown' }: { label: string; value: string; status?: RollupStatus }) {
  return (
    <article className="of-panel-muted" style={{ padding: 14, display: 'grid', gap: 4, ...statusBorder(status) }}>
      <span className="of-text-muted" style={{ fontSize: 12 }}>{label}</span>
      <strong style={{ fontSize: 22 }}>{value}</strong>
    </article>
  );
}

function ResourcePanel({
  resource,
  onToggleWatched,
}: {
  resource: MonitorableResource;
  onToggleWatched: (checkId: string) => void;
}) {
  return (
    <article className="of-panel" style={{ padding: 16, display: 'grid', gap: 12, minWidth: 0, ...statusBorder(resource.status) }}>
      <header style={{ display: 'flex', gap: 10, justifyContent: 'space-between', alignItems: 'start', flexWrap: 'wrap' }}>
        <div style={{ minWidth: 0 }}>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
            <span className={`of-chip ${statusTone(resource.status)}`}>{STATUS_LABELS[resource.status]}</span>
            <span className="of-chip">{RESOURCE_CLASS_LABELS[resource.resourceClass]}</span>
            {resource.branch ? <span className="of-chip">Branch {resource.branch}</span> : null}
          </div>
          <h2 className="of-heading-sm" style={{ marginTop: 8, overflowWrap: 'anywhere' }}>{resource.name}</h2>
          <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12, overflowWrap: 'anywhere' }}>
            {resource.project ? `Project ${resource.project}` : 'No project scope'}
            {' | '}
            {resource.folder ? `Folder ${resource.folder}` : 'No folder scope'}
            {' | '}
            {resource.owner ? `Owner ${resource.owner}` : 'No owner'}
          </p>
        </div>
        {resource.href ? (
          <Link to={resource.href} className="of-button" style={{ whiteSpace: 'nowrap' }}>Open</Link>
        ) : null}
      </header>

      <div style={{ display: 'grid', gap: 8 }}>
        {resource.checks.map((check) => (
          <div key={check.id} style={{ display: 'grid', gap: 6, padding: 10, border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-md)' }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap' }}>
              <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
                <strong style={{ fontSize: 13 }}>{check.label}</strong>
                <span className={`of-chip ${statusTone(check.status)}`}>{STATUS_LABELS[check.status]}</span>
              </div>
              <button type="button" className="of-button" onClick={() => onToggleWatched(check.id)}>
                {check.watched ? 'Watched' : 'Watch'}
              </button>
            </div>
            <p style={{ margin: 0, fontSize: 13, overflowWrap: 'anywhere' }}>{check.message}</p>
            <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
              {check.observedAt ? `Observed ${formatDate(check.observedAt)}` : 'No observation timestamp'}
              {check.recommendation ? ` | ${check.recommendation}` : ''}
            </p>
          </div>
        ))}
      </div>

      {resource.updatedAt ? (
        <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>Updated {formatDate(resource.updatedAt)}</p>
      ) : null}
    </article>
  );
}
