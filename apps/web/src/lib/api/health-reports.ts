import {
  listStoredResourceHealthChecks,
  resourceHealthCheckLabel,
  type ResourceHealthCheckDefinition,
  type ResourceHealthCheckKind,
  type ResourceHealthCheckResourceType,
  type ResourceHealthCheckSeverity,
  type ResourceHealthCheckSurface,
} from './resource-health-checks';

export type HealthReportStatus = 'passing' | 'warning' | 'failing' | 'unknown';
export type HealthAlertChannel = 'in_app' | 'email_digest' | 'slack' | 'pagerduty' | 'webhook';
export type HealthDigestFrequency = 'immediate' | 'hourly' | 'daily' | 'weekly';

export interface HealthReportSignal {
  resource_rid: string;
  resource_name: string;
  resource_type: ResourceHealthCheckResourceType;
  source_surface: ResourceHealthCheckSurface;
  kind: ResourceHealthCheckKind;
  status: 'healthy' | 'warning' | 'critical' | 'unknown' | HealthReportStatus;
  message: string;
  observed_at?: string | null;
  group?: string;
  monitoring_view?: string;
}

export interface HealthCheckReport {
  id: string;
  snapshot_id: string;
  check_id: string | null;
  resource_rid: string;
  resource_name: string;
  resource_type: ResourceHealthCheckResourceType;
  source_surface: ResourceHealthCheckSurface;
  kind: ResourceHealthCheckKind;
  status: HealthReportStatus;
  severity: ResourceHealthCheckSeverity;
  message: string;
  observed_at: string | null;
  evaluated_at: string;
  group: string;
  monitoring_view: string;
  issue_prompt: string;
}

export interface HealthReportSummary {
  passing: number;
  warning: number;
  failing: number;
  unknown: number;
  total: number;
}

export interface HealthReportSnapshot {
  id: string;
  generated_at: string;
  reports: HealthCheckReport[];
  summary: HealthReportSummary;
}

export interface HealthAlertSubscription {
  id: string;
  name: string;
  enabled: boolean;
  scope_resource_rid: string;
  scope_monitoring_view: string;
  minimum_status: Exclude<HealthReportStatus, 'passing'>;
  channels: HealthAlertChannel[];
  email_recipients: string;
  digest_frequency: HealthDigestFrequency;
  slack_destination_ref: string;
  pagerduty_service_ref: string;
  webhook_destination_ref: string;
  created_at: string;
  updated_at: string;
  last_notified_at: string | null;
}

export interface HealthAlertRecord {
  id: string;
  snapshot_id: string;
  report_id: string;
  subscription_id: string;
  title: string;
  body: string;
  status: HealthReportStatus;
  severity: ResourceHealthCheckSeverity;
  channels: HealthAlertChannel[];
  destination_refs: Record<string, string>;
  created_at: string;
  acknowledged_at: string | null;
}

const REPORT_HISTORY_KEY = 'openfoundry:health-report-history:v1';
const SUBSCRIPTIONS_KEY = 'openfoundry:health-alert-subscriptions:v1';
const ALERTS_KEY = 'openfoundry:health-alerts:v1';
const MAX_HISTORY = 40;
const MAX_ALERTS = 200;

export function normalizeHealthReportStatus(status: HealthReportSignal['status']): HealthReportStatus {
  if (status === 'healthy' || status === 'passing') return 'passing';
  if (status === 'critical' || status === 'failing') return 'failing';
  if (status === 'warning') return 'warning';
  return 'unknown';
}

export function healthReportStatusRank(status: HealthReportStatus) {
  if (status === 'failing') return 3;
  if (status === 'warning') return 2;
  if (status === 'unknown') return 1;
  return 0;
}

export function healthReportStatusLabel(status: HealthReportStatus) {
  if (status === 'passing') return 'Passing';
  if (status === 'warning') return 'Warning';
  if (status === 'failing') return 'Failing';
  return 'Unknown';
}

export function healthReportSummary(reports: HealthCheckReport[]): HealthReportSummary {
  return reports.reduce<HealthReportSummary>((summary, report) => {
    summary[report.status] += 1;
    summary.total += 1;
    return summary;
  }, { passing: 0, warning: 0, failing: 0, unknown: 0, total: 0 });
}

export function rollupHealthReportStatus(reports: Pick<HealthCheckReport, 'status'>[]) {
  return reports.reduce<HealthReportStatus>((current, report) => (
    healthReportStatusRank(report.status) > healthReportStatusRank(current) ? report.status : current
  ), reports.length > 0 ? 'passing' : 'unknown');
}

export function generateHealthReportSnapshot(signals: HealthReportSignal[] = []) {
  const generatedAt = new Date().toISOString();
  const snapshotId = createId('health-report');
  const definitions = listStoredResourceHealthChecks();
  const reportMap = new Map<string, HealthCheckReport>();
  const signalByKey = new Map(signals.map((signal) => [signalKey(signal.resource_rid, signal.kind), signal]));
  const definitionKeys = new Set(definitions.map((check) => signalKey(check.resource_rid, check.kind)));

  for (const check of definitions) {
    const signal = signalByKey.get(signalKey(check.resource_rid, check.kind));
    const report = signal
      ? signalToReport(snapshotId, generatedAt, signal, check)
      : definitionToUnknownReport(snapshotId, generatedAt, check);
    reportMap.set(check.id, report);
  }

  for (const signal of signals) {
    const key = signalKey(signal.resource_rid, signal.kind);
    if (definitionKeys.has(key)) continue;
    const report = signalToReport(snapshotId, generatedAt, signal, null);
    reportMap.set(`signal:${key}`, report);
  }

  const reports = Array.from(reportMap.values()).sort((left, right) => (
    healthReportStatusRank(right.status) - healthReportStatusRank(left.status)
    || left.resource_name.localeCompare(right.resource_name)
    || left.kind.localeCompare(right.kind)
  ));

  return {
    id: snapshotId,
    generated_at: generatedAt,
    reports,
    summary: healthReportSummary(reports),
  };
}

export function recordHealthReportSnapshot(snapshot: HealthReportSnapshot) {
  const history = listHealthReportHistory();
  const next = [snapshot, ...history.filter((entry) => entry.id !== snapshot.id)].slice(0, MAX_HISTORY);
  writeStorage(REPORT_HISTORY_KEY, next);
  return snapshot;
}

export function listHealthReportHistory() {
  return readStorageArray<HealthReportSnapshot>(REPORT_HISTORY_KEY).filter(isHealthReportSnapshot);
}

export function latestHealthReportSnapshot() {
  return listHealthReportHistory()[0] ?? null;
}

export function latestReportsForResource(resourceRid: string) {
  const latest = latestHealthReportSnapshot();
  return latest?.reports.filter((report) => report.resource_rid === resourceRid) ?? [];
}

export function latestReportsForResources(resourceRids: string[]) {
  const wanted = new Set(resourceRids);
  const latest = latestHealthReportSnapshot();
  return latest?.reports.filter((report) => wanted.has(report.resource_rid)) ?? [];
}

export function listHealthAlertSubscriptions() {
  return readStorageArray<HealthAlertSubscription>(SUBSCRIPTIONS_KEY).filter(isHealthAlertSubscription);
}

export function upsertHealthAlertSubscription(subscription: HealthAlertSubscription) {
  const subscriptions = listHealthAlertSubscriptions();
  const next = [subscription, ...subscriptions.filter((entry) => entry.id !== subscription.id)];
  writeStorage(SUBSCRIPTIONS_KEY, next);
  return subscription;
}

export function createHealthAlertSubscription(input: {
  name: string;
  scope_resource_rid?: string;
  scope_monitoring_view?: string;
  minimum_status?: Exclude<HealthReportStatus, 'passing'>;
  channels: HealthAlertChannel[];
  email_recipients?: string;
  digest_frequency?: HealthDigestFrequency;
  slack_destination_ref?: string;
  pagerduty_service_ref?: string;
  webhook_destination_ref?: string;
}) {
  const now = new Date().toISOString();
  return upsertHealthAlertSubscription({
    id: createId('health-subscription'),
    name: input.name,
    enabled: true,
    scope_resource_rid: input.scope_resource_rid ?? '',
    scope_monitoring_view: input.scope_monitoring_view ?? '',
    minimum_status: input.minimum_status ?? 'warning',
    channels: input.channels,
    email_recipients: input.email_recipients ?? '',
    digest_frequency: input.digest_frequency ?? 'daily',
    slack_destination_ref: input.slack_destination_ref ?? '',
    pagerduty_service_ref: input.pagerduty_service_ref ?? '',
    webhook_destination_ref: input.webhook_destination_ref ?? '',
    created_at: now,
    updated_at: now,
    last_notified_at: null,
  });
}

export function deleteHealthAlertSubscription(id: string) {
  writeStorage(SUBSCRIPTIONS_KEY, listHealthAlertSubscriptions().filter((subscription) => subscription.id !== id));
}

export function listHealthAlerts() {
  return readStorageArray<HealthAlertRecord>(ALERTS_KEY).filter(isHealthAlertRecord);
}

export function acknowledgeHealthAlert(id: string) {
  const now = new Date().toISOString();
  writeStorage(ALERTS_KEY, listHealthAlerts().map((alert) => (
    alert.id === id ? { ...alert, acknowledged_at: now } : alert
  )));
}

export function dispatchHealthAlerts(snapshot: HealthReportSnapshot, subscriptions = listHealthAlertSubscriptions()) {
  const now = new Date().toISOString();
  const alerts: HealthAlertRecord[] = [];
  const updatedSubscriptions = subscriptions.map((subscription) => {
    if (!subscription.enabled) return subscription;
    const matchingReports = snapshot.reports.filter((report) => subscriptionMatchesReport(subscription, report));
    if (matchingReports.length === 0) return subscription;
    for (const report of matchingReports) {
      alerts.push({
        id: createId('health-alert'),
        snapshot_id: snapshot.id,
        report_id: report.id,
        subscription_id: subscription.id,
        title: `${healthReportStatusLabel(report.status)} ${resourceHealthCheckLabel(report.kind)} check`,
        body: `${report.resource_name}: ${report.message}`,
        status: report.status,
        severity: report.severity,
        channels: subscription.channels,
        destination_refs: destinationRefs(subscription),
        created_at: now,
        acknowledged_at: null,
      });
    }
    return { ...subscription, last_notified_at: now, updated_at: now };
  });

  if (alerts.length > 0) {
    writeStorage(ALERTS_KEY, [...alerts, ...listHealthAlerts()].slice(0, MAX_ALERTS));
    writeStorage(SUBSCRIPTIONS_KEY, updatedSubscriptions);
  }

  return alerts;
}

export function buildHealthEmailDigest(snapshot: HealthReportSnapshot, reports = snapshot.reports) {
  const lines = [
    `OpenFoundry Data Health digest`,
    `Generated: ${snapshot.generated_at}`,
    `Summary: ${snapshot.summary.failing} failing, ${snapshot.summary.warning} warning, ${snapshot.summary.unknown} unknown, ${snapshot.summary.passing} passing`,
    '',
    ...reports.slice(0, 30).map((report) => (
      `- [${healthReportStatusLabel(report.status)}] ${report.resource_name} / ${resourceHealthCheckLabel(report.kind)}: ${report.message}`
    )),
  ];
  return lines.join('\n');
}

export function healthReportsToSignalsFromResource(resource: {
  rid?: string | null;
  id: string;
  name: string;
  resourceClass?: string;
  project?: string | null;
  folder?: string | null;
  checks: Array<{
    kind: string;
    status: 'healthy' | 'warning' | 'critical' | 'unknown';
    message: string;
    observedAt?: string | null;
    label?: string;
  }>;
}): HealthReportSignal[] {
  const resourceRid = resource.rid || resource.id;
  return resource.checks.map((check) => ({
    resource_rid: resourceRid,
    resource_name: resource.name,
    resource_type: resourceClassToReportType(resource.resourceClass),
    source_surface: resourceClassToSurface(resource.resourceClass),
    kind: normalizeReportKind(check.kind),
    status: check.status,
    message: check.message,
    observed_at: check.observedAt ?? null,
    group: resource.resourceClass || 'resource',
    monitoring_view: resource.project || resource.folder || '',
  }));
}

function signalToReport(
  snapshotId: string,
  evaluatedAt: string,
  signal: HealthReportSignal,
  check: ResourceHealthCheckDefinition | null,
): HealthCheckReport {
  const status = check?.enabled === false ? 'unknown' : normalizeHealthReportStatus(signal.status);
  const severity = check?.severity ?? defaultSeverityForStatus(status);
  return {
    id: createId('check-report'),
    snapshot_id: snapshotId,
    check_id: check?.id ?? null,
    resource_rid: signal.resource_rid,
    resource_name: signal.resource_name,
    resource_type: signal.resource_type,
    source_surface: signal.source_surface,
    kind: signal.kind,
    status,
    severity,
    message: check?.enabled === false ? 'Check is paused.' : signal.message,
    observed_at: signal.observed_at ?? null,
    evaluated_at: evaluatedAt,
    group: check?.group || signal.group || '',
    monitoring_view: check?.monitoring_view || signal.monitoring_view || '',
    issue_prompt: check?.issue_prompt ?? '',
  };
}

function signalKey(resourceRid: string, kind: ResourceHealthCheckKind) {
  return `${resourceRid}:${kind}`;
}

function definitionToUnknownReport(
  snapshotId: string,
  evaluatedAt: string,
  check: ResourceHealthCheckDefinition,
): HealthCheckReport {
  return {
    id: createId('check-report'),
    snapshot_id: snapshotId,
    check_id: check.id,
    resource_rid: check.resource_rid,
    resource_name: check.resource_name,
    resource_type: check.resource_type,
    source_surface: check.source_surface,
    kind: check.kind,
    status: 'unknown',
    severity: check.severity,
    message: check.enabled
      ? 'No current local signal matched this saved check during report generation.'
      : 'Check is paused.',
    observed_at: null,
    evaluated_at: evaluatedAt,
    group: check.group,
    monitoring_view: check.monitoring_view,
    issue_prompt: check.issue_prompt,
  };
}

function defaultSeverityForStatus(status: HealthReportStatus): ResourceHealthCheckSeverity {
  if (status === 'failing') return 'CRITICAL';
  if (status === 'warning') return 'MODERATE';
  return 'INFO';
}

function normalizeReportKind(kind: string): ResourceHealthCheckKind {
  const normalized = kind.toLowerCase();
  if (normalized.includes('fresh')) return 'freshness';
  if (normalized.includes('schema')) return 'schema';
  if (normalized.includes('build')) return 'build';
  if (normalized.includes('job')) return 'job';
  if (normalized.includes('schedule') || normalized.includes('trigger') || normalized.includes('pause')) return 'schedule';
  if (normalized.includes('stream') || normalized.includes('sync') || normalized.includes('lag') || normalized.includes('checkpoint')) return 'sync';
  if (normalized.includes('row') || normalized.includes('size') || normalized.includes('file') || normalized.includes('count')) return 'size';
  if (normalized.includes('content') || normalized.includes('quality') || normalized.includes('drift') || normalized.includes('parse')) return 'content';
  if (normalized.includes('duration')) return 'duration';
  return 'status';
}

function resourceClassToReportType(resourceClass?: string): ResourceHealthCheckResourceType {
  if (resourceClass === 'schedule') return 'schedule';
  if (resourceClass === 'pipeline') return 'pipeline';
  if (resourceClass === 'streaming_dataset') return 'streaming_dataset';
  if (resourceClass === 'agent') return 'agent';
  if (resourceClass === 'object_type') return 'object_type';
  if (resourceClass === 'function') return 'function';
  if (resourceClass === 'action') return 'action';
  if (resourceClass === 'automation') return 'automation';
  return 'dataset';
}

function resourceClassToSurface(resourceClass?: string): ResourceHealthCheckSurface {
  if (resourceClass === 'pipeline') return 'pipeline_builder';
  if (resourceClass === 'schedule') return 'data_lineage';
  return 'dataset_preview';
}

function subscriptionMatchesReport(subscription: HealthAlertSubscription, report: HealthCheckReport) {
  if (subscription.scope_resource_rid && subscription.scope_resource_rid !== report.resource_rid) return false;
  if (subscription.scope_monitoring_view && subscription.scope_monitoring_view !== report.monitoring_view) return false;
  return healthReportStatusRank(report.status) >= healthReportStatusRank(subscription.minimum_status);
}

function destinationRefs(subscription: HealthAlertSubscription) {
  return {
    email_recipients: subscription.email_recipients,
    slack_destination_ref: subscription.slack_destination_ref,
    pagerduty_service_ref: subscription.pagerduty_service_ref,
    webhook_destination_ref: subscription.webhook_destination_ref,
  };
}

function createId(prefix: string) {
  const randomId = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  return `${prefix}-${randomId}`;
}

function readStorageArray<T>(key: string): T[] {
  if (typeof localStorage === 'undefined') return [];
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as unknown;
    return Array.isArray(parsed) ? parsed as T[] : [];
  } catch {
    return [];
  }
}

function writeStorage(key: string, value: unknown) {
  if (typeof localStorage === 'undefined') return;
  localStorage.setItem(key, JSON.stringify(value));
}

function isHealthReportSnapshot(value: unknown): value is HealthReportSnapshot {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const candidate = value as Partial<HealthReportSnapshot>;
  return typeof candidate.id === 'string'
    && typeof candidate.generated_at === 'string'
    && Array.isArray(candidate.reports);
}

function isHealthAlertSubscription(value: unknown): value is HealthAlertSubscription {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const candidate = value as Partial<HealthAlertSubscription>;
  return typeof candidate.id === 'string'
    && typeof candidate.name === 'string'
    && Array.isArray(candidate.channels);
}

function isHealthAlertRecord(value: unknown): value is HealthAlertRecord {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const candidate = value as Partial<HealthAlertRecord>;
  return typeof candidate.id === 'string'
    && typeof candidate.report_id === 'string'
    && typeof candidate.title === 'string';
}
