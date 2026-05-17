import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  acknowledgeHealthAlert,
  buildHealthEmailDigest,
  createHealthAlertSubscription,
  dispatchHealthAlerts,
  generateHealthReportSnapshot,
  healthReportsToSignalsFromResource,
  latestReportsForResource,
  listHealthAlertSubscriptions,
  listHealthAlerts,
  listHealthReportHistory,
  recordHealthReportSnapshot,
} from './health-reports';
import {
  defaultResourceHealthCheckDraft,
  materializeResourceHealthCheck,
  upsertResourceHealthCheck,
} from './resource-health-checks';

describe('health reports', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('generates latest and historical check reports from saved checks and signals', () => {
    mockLocalStorage();
    const draft = defaultResourceHealthCheckDraft({
      kind: 'freshness',
      group: 'Dataset Preview',
      monitoringView: 'project-alpha',
    });
    upsertResourceHealthCheck(materializeResourceHealthCheck({
      draft: { ...draft, severity: 'CRITICAL' },
      resourceRid: 'ri.dataset.sales',
      resourceName: 'Sales',
      resourceType: 'dataset',
      sourceSurface: 'dataset_preview',
    }));

    const snapshot = recordHealthReportSnapshot(generateHealthReportSnapshot([{
      resource_rid: 'ri.dataset.sales',
      resource_name: 'Sales',
      resource_type: 'dataset',
      source_surface: 'dataset_preview',
      kind: 'freshness',
      status: 'critical',
      message: 'Freshness is 36 hours behind.',
      monitoring_view: 'project-alpha',
    }]));

    expect(snapshot.summary).toMatchObject({ failing: 1, total: 1 });
    expect(listHealthReportHistory()[0].id).toBe(snapshot.id);
    expect(latestReportsForResource('ri.dataset.sales')[0]).toMatchObject({
      status: 'failing',
      severity: 'CRITICAL',
      group: 'Dataset Preview',
      monitoring_view: 'project-alpha',
    });
  });

  it('dispatches alert records with extension destination refs and acknowledge state', () => {
    mockLocalStorage();
    const subscription = createHealthAlertSubscription({
      name: 'Ops health',
      minimum_status: 'warning',
      channels: ['in_app', 'email_digest', 'slack', 'pagerduty', 'webhook'],
      email_recipients: 'ops@example.com',
      slack_destination_ref: 'slack:ops-data',
      pagerduty_service_ref: 'pagerduty:data-platform',
      webhook_destination_ref: 'webhook:data-health',
    });
    const snapshot = recordHealthReportSnapshot(generateHealthReportSnapshot([{
      resource_rid: 'ri.dataset.sales',
      resource_name: 'Sales',
      resource_type: 'dataset',
      source_surface: 'dataset_preview',
      kind: 'schema',
      status: 'warning',
      message: 'Schema drift detected.',
    }]));

    const alerts = dispatchHealthAlerts(snapshot, [subscription]);

    expect(alerts).toHaveLength(1);
    expect(alerts[0].channels).toContain('slack');
    expect(alerts[0].destination_refs).toMatchObject({
      email_recipients: 'ops@example.com',
      slack_destination_ref: 'slack:ops-data',
      pagerduty_service_ref: 'pagerduty:data-platform',
      webhook_destination_ref: 'webhook:data-health',
    });
    expect(listHealthAlertSubscriptions()[0].last_notified_at).toEqual(expect.any(String));

    acknowledgeHealthAlert(alerts[0].id);
    expect(listHealthAlerts()[0].acknowledged_at).toEqual(expect.any(String));
  });

  it('builds email digest text and maps Data Health resources to report signals', () => {
    mockLocalStorage();
    const signals = healthReportsToSignalsFromResource({
      id: 'ri.object.customer',
      name: 'Customer Object',
      resourceClass: 'object_type',
      project: 'Customer 360',
      checks: [{
        kind: 'status',
        status: 'healthy',
        message: 'Published object type is reachable.',
      }],
    });
    const snapshot = recordHealthReportSnapshot(generateHealthReportSnapshot(signals));
    const digest = buildHealthEmailDigest(snapshot);

    expect(signals[0]).toMatchObject({
      resource_type: 'object_type',
      source_surface: 'dataset_preview',
      monitoring_view: 'Customer 360',
    });
    expect(digest).toContain('OpenFoundry Data Health digest');
    expect(digest).toContain('Customer Object / Status');
  });
});

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
