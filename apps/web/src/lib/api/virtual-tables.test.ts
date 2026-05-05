import { afterEach, describe, expect, it, vi } from 'vitest';

import api from './client';
import {
  BULK_REGISTER_PROVIDERS,
  capabilityChips,
  defaultCapabilities,
  providerLabel,
  providerSupportsVirtualTables,
  supportsBulkRegister,
  tableTypeLabel,
  virtualTables,
  VIRTUAL_TABLE_PROVIDERS,
  type VirtualTable,
  type VirtualTableProvider,
} from './virtual-tables';

const fakeRow = (rid: string): VirtualTable => ({
  id: 'uuid',
  rid,
  source_rid: 'ri.foundry.main.source.s1',
  project_rid: 'ri.foundry.main.project.p1',
  name: 'orders',
  parent_folder_rid: null,
  locator: { kind: 'tabular', database: 'main', schema: 'public', table: 'orders' },
  table_type: 'TABLE',
  schema_inferred: [],
  capabilities: defaultCapabilities('BIGQUERY', 'TABLE'),
  update_detection_enabled: false,
  update_detection_interval_seconds: null,
  last_observed_version: null,
  last_polled_at: null,
  markings: [],
  properties: {},
  created_by: null,
  created_at: '2026-05-04T00:00:00Z',
  updated_at: '2026-05-04T00:00:00Z',
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('virtual-tables API client', () => {
  it('enableOnSource POSTs to the enable endpoint with the encoded source rid', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({ source_rid: 'ri/x' });
    await virtualTables.enableOnSource('ri/x', { provider: 'BIGQUERY' });
    expect(post).toHaveBeenCalledWith(
      '/v1/sources/ri%2Fx/virtual-tables/enable',
      { provider: 'BIGQUERY' },
    );
  });

  it('discoverRemoteCatalog appends ?path= when a path is given', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({ data: [] });
    await virtualTables.discoverRemoteCatalog('ri.s1', 'main/public');
    expect(get).toHaveBeenCalledWith(
      '/v1/sources/ri.s1/virtual-tables/discover?path=main%2Fpublic',
    );
  });

  it('discoverRemoteCatalog omits ?path= when no path is given', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({ data: [] });
    await virtualTables.discoverRemoteCatalog('ri.s1');
    expect(get).toHaveBeenCalledWith('/v1/sources/ri.s1/virtual-tables/discover');
  });

  it('listVirtualTables encodes filters into the query string', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({ items: [], next_cursor: null });
    await virtualTables.listVirtualTables({
      project: 'ri.proj',
      source: 'ri.src',
      type: 'EXTERNAL_DELTA',
      limit: 25,
    });
    expect(get).toHaveBeenCalledTimes(1);
    const url = get.mock.calls[0][0];
    expect(url.startsWith('/v1/virtual-tables?')).toBe(true);
    expect(url).toContain('project=ri.proj');
    expect(url).toContain('type=EXTERNAL_DELTA');
    expect(url).toContain('limit=25');
  });

  it('updateMarkings PATCHes with the markings array', async () => {
    const patch = vi.spyOn(api, 'patch').mockResolvedValue(fakeRow('ri.vt'));
    await virtualTables.updateMarkings('ri.vt', ['SECRET']);
    expect(patch).toHaveBeenCalledWith('/v1/virtual-tables/ri.vt/markings', {
      markings: ['SECRET'],
    });
  });

  it('refreshSchema POSTs an empty body (server reuses the persisted locator)', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue(fakeRow('ri.vt'));
    await virtualTables.refreshSchema('ri.vt');
    expect(post).toHaveBeenCalledWith('/v1/virtual-tables/ri.vt/refresh-schema', {});
  });

  it('enableAutoRegistration POSTs to the auto-registration endpoint with the encoded source rid', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ source_rid: 'ri/x' });
    await virtualTables.enableAutoRegistration('ri/x', {
      project_name: 'gold-mirror',
      folder_mirror_kind: 'NESTED',
      table_tag_filters: ['gold'],
      poll_interval_seconds: 3600,
    });
    expect(post).toHaveBeenCalledWith(
      '/v1/sources/ri%2Fx/auto-registration',
      {
        project_name: 'gold-mirror',
        folder_mirror_kind: 'NESTED',
        table_tag_filters: ['gold'],
        poll_interval_seconds: 3600,
      },
    );
  });

  it('disableAutoRegistration DELETEs the auto-registration endpoint', async () => {
    const del = vi.spyOn(api, 'delete').mockResolvedValue(undefined);
    await virtualTables.disableAutoRegistration('ri.s1');
    expect(del).toHaveBeenCalledWith('/v1/sources/ri.s1/auto-registration');
  });

  it('scanAutoRegistrationNow POSTs to the scan-now action endpoint', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ added: 1, updated: 0, orphaned: 0 });
    const result = await virtualTables.scanAutoRegistrationNow('ri.s1');
    expect(post).toHaveBeenCalledWith(
      '/v1/sources/ri.s1/auto-registration:scan-now',
      {},
    );
    expect(result.added).toBe(1);
  });

  it('setUpdateDetection PATCHes the toggle endpoint with enabled+interval', async () => {
    const patch = vi.spyOn(api, 'patch').mockResolvedValue(fakeRow('ri.vt'));
    await virtualTables.setUpdateDetection('ri.vt', {
      enabled: true,
      interval_seconds: 3600,
    });
    expect(patch).toHaveBeenCalledWith(
      '/v1/virtual-tables/ri.vt/update-detection',
      { enabled: true, interval_seconds: 3600 },
    );
  });

  it('pollUpdateDetectionNow POSTs to the action endpoint with an empty body', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      virtual_table_rid: 'ri.vt',
      outcome: 'changed',
      observed_version: 'v2',
      previous_version: 'v1',
      latency_ms: 12,
      change_detected: true,
      event_emitted: true,
    });
    const result = await virtualTables.pollUpdateDetectionNow('ri.vt');
    expect(post).toHaveBeenCalledWith(
      '/v1/virtual-tables/ri.vt/update-detection:poll-now',
      {},
    );
    expect(result.outcome).toBe('changed');
    expect(result.event_emitted).toBe(true);
  });

  it('setCodeImportsEnabled PATCHes the code-imports endpoint', async () => {
    const patch = vi
      .spyOn(api, 'patch')
      .mockResolvedValue({ source_rid: 'ri.s1' });
    await virtualTables.setCodeImportsEnabled('ri.s1', true);
    expect(patch).toHaveBeenCalledWith('/v1/sources/ri.s1/code-imports', {
      enabled: true,
    });
  });

  it('setExportControls POSTs the allow-list body', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ source_rid: 'ri.s1' });
    await virtualTables.setExportControls('ri.s1', {
      allowed_markings: ['public'],
      allowed_organizations: ['acme'],
    });
    expect(post).toHaveBeenCalledWith('/v1/sources/ri.s1/export-controls', {
      allowed_markings: ['public'],
      allowed_organizations: ['acme'],
    });
  });

  it('updateDetectionHistory passes the limit query param', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({ data: [] });
    await virtualTables.updateDetectionHistory('ri.vt', 25);
    expect(get).toHaveBeenCalledWith(
      '/v1/virtual-tables/ri.vt/update-detection/history?limit=25',
    );
  });

  it('enableUpdateDetection is a P3 stub that returns deferred=true without hitting the API', async () => {
    const post = vi.spyOn(api, 'post');
    const result = await virtualTables.enableUpdateDetection('ri.vt', { interval_seconds: 3600 });
    expect(result).toEqual({ enabled: true, interval_seconds: 3600, deferred: true });
    expect(post).not.toHaveBeenCalled();
  });
});

describe('virtual-tables UI helpers', () => {
  it('providerLabel covers every provider in VIRTUAL_TABLE_PROVIDERS', () => {
    for (const p of VIRTUAL_TABLE_PROVIDERS) {
      expect(providerLabel(p as VirtualTableProvider)).not.toBe('');
    }
  });

  it('providerSupportsVirtualTables narrows null/unknown providers out', () => {
    expect(providerSupportsVirtualTables('BIGQUERY')).toBe(true);
    expect(providerSupportsVirtualTables('postgresql')).toBe(false);
    expect(providerSupportsVirtualTables(null)).toBe(false);
  });

  it('supportsBulkRegister matches BULK_REGISTER_PROVIDERS', () => {
    for (const p of VIRTUAL_TABLE_PROVIDERS) {
      const expected = BULK_REGISTER_PROVIDERS.includes(p);
      expect(supportsBulkRegister(p)).toBe(expected);
    }
  });

  it('tableTypeLabel covers every table type', () => {
    const types = [
      'TABLE',
      'VIEW',
      'MATERIALIZED_VIEW',
      'EXTERNAL_DELTA',
      'MANAGED_DELTA',
      'MANAGED_ICEBERG',
      'PARQUET_FILES',
      'AVRO_FILES',
      'CSV_FILES',
      'OTHER',
    ] as const;
    for (const t of types) {
      expect(tableTypeLabel(t)).not.toBe('');
    }
  });

  it('defaultCapabilities matches Foundry doc cells we hand-checked', () => {
    const bq = defaultCapabilities('BIGQUERY', 'TABLE');
    expect(bq.read && bq.write).toBe(true);
    expect(bq.compute_pushdown).toBe('ibis');

    const dbxManaged = defaultCapabilities('DATABRICKS', 'MANAGED_DELTA');
    expect(dbxManaged.read).toBe(true);
    expect(dbxManaged.write).toBe(false);
    expect(dbxManaged.compute_pushdown).toBe('pyspark');

    const sfIceberg = defaultCapabilities('SNOWFLAKE', 'MANAGED_ICEBERG');
    expect(sfIceberg.read).toBe(true);
    expect(sfIceberg.write).toBe(false);
    expect(sfIceberg.foundry_compute.python_single_node).toBe(false);
    expect(sfIceberg.foundry_compute.python_spark).toBe(true);

    const s3 = defaultCapabilities('AMAZON_S3', 'PARQUET_FILES');
    expect(s3.read && s3.write).toBe(true);
    expect(s3.compute_pushdown).toBeNull();
    expect(s3.foundry_compute.python_spark).toBe(false);
  });

  it('capabilityChips renders read/write/incremental/versioning/pushdown in order', () => {
    const caps = defaultCapabilities('BIGQUERY', 'TABLE');
    const chips = capabilityChips(caps);
    expect(chips).toEqual(['Read', 'Write', 'Incremental', 'Pushdown: ibis']);
  });
});
