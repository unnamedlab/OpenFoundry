import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  availableResourceHealthCheckKinds,
  defaultResourceHealthCheckDraft,
  deleteResourceHealthCheck,
  listResourceHealthChecks,
  materializeResourceHealthCheck,
  resourceHealthCheckSummary,
  upsertResourceHealthCheck,
} from './resource-health-checks';

describe('resource health checks', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('orders available kinds according to the public catalogue', () => {
    expect(availableResourceHealthCheckKinds({
      schema: true,
      status: true,
      freshness: true,
      job: false,
      schedule: true,
    })).toEqual(['status', 'freshness', 'schema', 'schedule']);
  });

  it('persists resource-level check definitions by resource RID', () => {
    mockLocalStorage();
    const draft = defaultResourceHealthCheckDraft({
      kind: 'freshness',
      group: 'Dataset Preview',
      monitoringView: 'project-alpha',
    });
    const check = materializeResourceHealthCheck({
      draft: {
        ...draft,
        severity: 'CRITICAL',
        threshold: '12',
        create_issue_on_failure: true,
        issue_prompt: 'Open a data freshness incident.',
      },
      resourceRid: 'ri.foundry.main.dataset.sales',
      resourceName: 'Sales',
      resourceType: 'dataset',
      sourceSurface: 'dataset_preview',
    });

    upsertResourceHealthCheck(check);

    const stored = listResourceHealthChecks('ri.foundry.main.dataset.sales');
    expect(stored).toHaveLength(1);
    expect(stored[0]).toMatchObject({
      kind: 'freshness',
      severity: 'CRITICAL',
      threshold: '12',
      group: 'Dataset Preview',
      monitoring_view: 'project-alpha',
      create_issue_on_failure: true,
    });
    expect(resourceHealthCheckSummary(stored[0])).toContain('creates issue');

    deleteResourceHealthCheck(check.id);
    expect(listResourceHealthChecks('ri.foundry.main.dataset.sales')).toEqual([]);
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
