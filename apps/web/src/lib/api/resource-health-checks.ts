export type ResourceHealthCheckKind =
  | 'status'
  | 'duration'
  | 'freshness'
  | 'content'
  | 'size'
  | 'schema'
  | 'sync'
  | 'build'
  | 'job'
  | 'schedule';

export type ResourceHealthCheckSeverity = 'INFO' | 'MODERATE' | 'CRITICAL';
export type ResourceHealthCheckComparator = 'ANY_FAILURE' | 'GT' | 'GTE' | 'LT' | 'LTE' | 'EQ' | 'BETWEEN' | 'CHANGED';
export type ResourceHealthCheckSurface = 'dataset_preview' | 'data_lineage' | 'pipeline_builder';
export type ResourceHealthCheckResourceType =
  | 'dataset'
  | 'schedule'
  | 'pipeline_node'
  | 'lineage_node'
  | 'pipeline'
  | 'streaming_dataset'
  | 'agent'
  | 'object_type'
  | 'function'
  | 'action'
  | 'automation';

export interface ResourceHealthCheckDefinition {
  id: string;
  resource_rid: string;
  resource_name: string;
  resource_type: ResourceHealthCheckResourceType;
  source_surface: ResourceHealthCheckSurface;
  kind: ResourceHealthCheckKind;
  enabled: boolean;
  severity: ResourceHealthCheckSeverity;
  comparator: ResourceHealthCheckComparator;
  threshold: string;
  secondary_threshold: string;
  unit: string;
  column: string;
  group: string;
  monitoring_view: string;
  escalate_after_failures: number;
  notes: string;
  create_issue_on_failure: boolean;
  issue_prompt: string;
  created_at: string;
  updated_at: string;
}

export interface ResourceHealthCheckDraft {
  kind: ResourceHealthCheckKind;
  severity: ResourceHealthCheckSeverity;
  comparator: ResourceHealthCheckComparator;
  threshold: string;
  secondary_threshold: string;
  unit: string;
  column: string;
  group: string;
  monitoring_view: string;
  escalate_after_failures: number;
  notes: string;
  create_issue_on_failure: boolean;
  issue_prompt: string;
}

export interface ResourceHealthCheckCatalogEntry {
  kind: ResourceHealthCheckKind;
  label: string;
  description: string;
  defaultComparator: ResourceHealthCheckComparator;
  defaultThreshold: string;
  defaultSecondaryThreshold: string;
  defaultUnit: string;
  defaultSeverity: ResourceHealthCheckSeverity;
}

export type ResourceHealthSignalAvailability = Partial<Record<ResourceHealthCheckKind, boolean>>;

const STORAGE_KEY = 'openfoundry:resource-health-checks:v1';

export const RESOURCE_HEALTH_CHECK_KINDS: ResourceHealthCheckKind[] = [
  'status',
  'duration',
  'freshness',
  'content',
  'size',
  'schema',
  'sync',
  'build',
  'job',
  'schedule',
];

export const RESOURCE_HEALTH_CHECK_CATALOG: ResourceHealthCheckCatalogEntry[] = [
  {
    kind: 'status',
    label: 'Status',
    description: 'Resource, preview, run, or latest known health state.',
    defaultComparator: 'ANY_FAILURE',
    defaultThreshold: '',
    defaultSecondaryThreshold: '',
    defaultUnit: '',
    defaultSeverity: 'CRITICAL',
  },
  {
    kind: 'duration',
    label: 'Duration',
    description: 'Build, schedule, job, or preview duration when timing data is available.',
    defaultComparator: 'LTE',
    defaultThreshold: '60',
    defaultSecondaryThreshold: '',
    defaultUnit: 'minutes',
    defaultSeverity: 'MODERATE',
  },
  {
    kind: 'freshness',
    label: 'Freshness',
    description: 'Time since data was last updated, synced, or previewed.',
    defaultComparator: 'LTE',
    defaultThreshold: '24',
    defaultSecondaryThreshold: '',
    defaultUnit: 'hours',
    defaultSeverity: 'MODERATE',
  },
  {
    kind: 'content',
    label: 'Content',
    description: 'Row/content quality, parse errors, nulls, or empty preview output.',
    defaultComparator: 'ANY_FAILURE',
    defaultThreshold: '',
    defaultSecondaryThreshold: '',
    defaultUnit: '',
    defaultSeverity: 'CRITICAL',
  },
  {
    kind: 'size',
    label: 'Size',
    description: 'Row count, file count, file size, or preview sample size.',
    defaultComparator: 'GTE',
    defaultThreshold: '1',
    defaultSecondaryThreshold: '',
    defaultUnit: 'rows',
    defaultSeverity: 'MODERATE',
  },
  {
    kind: 'schema',
    label: 'Schema',
    description: 'Schema drift, column count, nullability, or expected preview columns.',
    defaultComparator: 'CHANGED',
    defaultThreshold: '',
    defaultSecondaryThreshold: '',
    defaultUnit: '',
    defaultSeverity: 'CRITICAL',
  },
  {
    kind: 'sync',
    label: 'Sync',
    description: 'Sync freshness, connector state, lag, or source sync references.',
    defaultComparator: 'ANY_FAILURE',
    defaultThreshold: '',
    defaultSecondaryThreshold: '',
    defaultUnit: '',
    defaultSeverity: 'CRITICAL',
  },
  {
    kind: 'build',
    label: 'Build',
    description: 'Latest build state for a target dataset or transform output.',
    defaultComparator: 'ANY_FAILURE',
    defaultThreshold: '',
    defaultSecondaryThreshold: '',
    defaultUnit: '',
    defaultSeverity: 'CRITICAL',
  },
  {
    kind: 'job',
    label: 'Job',
    description: 'Job status or output transaction state for transform work units.',
    defaultComparator: 'ANY_FAILURE',
    defaultThreshold: '',
    defaultSecondaryThreshold: '',
    defaultUnit: '',
    defaultSeverity: 'CRITICAL',
  },
  {
    kind: 'schedule',
    label: 'Schedule',
    description: 'Schedule status, latest run, duration, or pending trigger state.',
    defaultComparator: 'ANY_FAILURE',
    defaultThreshold: '',
    defaultSecondaryThreshold: '',
    defaultUnit: '',
    defaultSeverity: 'CRITICAL',
  },
];

const CATALOG_BY_KIND = Object.fromEntries(
  RESOURCE_HEALTH_CHECK_CATALOG.map((entry) => [entry.kind, entry]),
) as Record<ResourceHealthCheckKind, ResourceHealthCheckCatalogEntry>;

export function resourceHealthCheckLabel(kind: ResourceHealthCheckKind) {
  return CATALOG_BY_KIND[kind].label;
}

export function resourceHealthCheckDescription(kind: ResourceHealthCheckKind) {
  return CATALOG_BY_KIND[kind].description;
}

export function availableResourceHealthCheckKinds(signals: ResourceHealthSignalAvailability) {
  return RESOURCE_HEALTH_CHECK_KINDS.filter((kind) => signals[kind]);
}

export function defaultResourceHealthCheckDraft(input: {
  kind: ResourceHealthCheckKind;
  group?: string;
  monitoringView?: string;
  notes?: string;
}): ResourceHealthCheckDraft {
  const catalog = CATALOG_BY_KIND[input.kind];
  return {
    kind: input.kind,
    severity: catalog.defaultSeverity,
    comparator: catalog.defaultComparator,
    threshold: catalog.defaultThreshold,
    secondary_threshold: catalog.defaultSecondaryThreshold,
    unit: catalog.defaultUnit,
    column: '',
    group: input.group ?? 'Resource health',
    monitoring_view: input.monitoringView ?? '',
    escalate_after_failures: 1,
    notes: input.notes ?? '',
    create_issue_on_failure: false,
    issue_prompt: '',
  };
}

export function materializeResourceHealthCheck(input: {
  draft: ResourceHealthCheckDraft;
  existing?: ResourceHealthCheckDefinition | null;
  resourceRid: string;
  resourceName: string;
  resourceType: ResourceHealthCheckResourceType;
  sourceSurface: ResourceHealthCheckSurface;
}): ResourceHealthCheckDefinition {
  const now = new Date().toISOString();
  return {
    id: input.existing?.id ?? createHealthCheckId(input.resourceRid, input.draft.kind),
    resource_rid: input.resourceRid,
    resource_name: input.resourceName,
    resource_type: input.resourceType,
    source_surface: input.sourceSurface,
    kind: input.draft.kind,
    enabled: input.existing?.enabled ?? true,
    severity: input.draft.severity,
    comparator: input.draft.comparator,
    threshold: input.draft.threshold,
    secondary_threshold: input.draft.secondary_threshold,
    unit: input.draft.unit,
    column: input.draft.column,
    group: input.draft.group,
    monitoring_view: input.draft.monitoring_view,
    escalate_after_failures: Math.max(1, Math.floor(input.draft.escalate_after_failures || 1)),
    notes: input.draft.notes,
    create_issue_on_failure: input.draft.create_issue_on_failure,
    issue_prompt: input.draft.issue_prompt,
    created_at: input.existing?.created_at ?? now,
    updated_at: now,
  };
}

export function resourceHealthCheckSummary(check: ResourceHealthCheckDefinition) {
  const threshold = [check.comparator, check.threshold, check.secondary_threshold ? `and ${check.secondary_threshold}` : '', check.unit]
    .filter(Boolean)
    .join(' ');
  const pieces = [
    resourceHealthCheckLabel(check.kind),
    check.severity.toLowerCase(),
    threshold,
    `escalate after ${check.escalate_after_failures}`,
    check.create_issue_on_failure ? 'creates issue' : 'no issue',
  ].filter(Boolean);
  return pieces.join(' | ');
}

export function listStoredResourceHealthChecks() {
  return readChecks().sort((left, right) => left.resource_name.localeCompare(right.resource_name) || left.kind.localeCompare(right.kind));
}

export function listResourceHealthChecks(resourceRid: string) {
  return readChecks()
    .filter((check) => check.resource_rid === resourceRid)
    .sort((left, right) => left.kind.localeCompare(right.kind));
}

export function upsertResourceHealthCheck(check: ResourceHealthCheckDefinition) {
  const checks = readChecks();
  const next = [check, ...checks.filter((candidate) => candidate.id !== check.id)];
  writeChecks(next);
  return check;
}

export function deleteResourceHealthCheck(id: string) {
  const checks = readChecks().filter((check) => check.id !== id);
  writeChecks(checks);
}

export function setResourceHealthCheckEnabled(id: string, enabled: boolean) {
  const now = new Date().toISOString();
  const checks = readChecks().map((check) => (
    check.id === id ? { ...check, enabled, updated_at: now } : check
  ));
  writeChecks(checks);
}

function createHealthCheckId(resourceRid: string, kind: ResourceHealthCheckKind) {
  const normalized = `${resourceRid}:${kind}`.replace(/[^a-zA-Z0-9:_-]+/g, '-');
  const randomId = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  return `rh-${normalized}-${randomId}`;
}

function readChecks(): ResourceHealthCheckDefinition[] {
  if (typeof localStorage === 'undefined') return [];
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(isStoredCheck);
  } catch {
    return [];
  }
}

function writeChecks(checks: ResourceHealthCheckDefinition[]) {
  if (typeof localStorage === 'undefined') return;
  localStorage.setItem(STORAGE_KEY, JSON.stringify(checks));
}

function isStoredCheck(value: unknown): value is ResourceHealthCheckDefinition {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const candidate = value as Partial<ResourceHealthCheckDefinition>;
  return typeof candidate.id === 'string'
    && typeof candidate.resource_rid === 'string'
    && typeof candidate.resource_name === 'string'
    && typeof candidate.kind === 'string'
    && RESOURCE_HEALTH_CHECK_KINDS.includes(candidate.kind as ResourceHealthCheckKind);
}
