// Iceberg tables [Beta] — covers the list view and the Metadata tab
// (img_001 of the Foundry Iceberg tables doc).
//
// The test exercises:
//   * `/iceberg-tables` lists tables returned by the catalog admin API.
//   * Clicking a row opens `/iceberg-tables/{id}` with the Overview tab.
//   * Switching to the Metadata tab loads `metadata.json` and renders
//     the readonly viewer (data-testid="metadata-json-readonly").
//   * Snapshots tab renders the operation badge.
//
// Backend calls are mocked at the route level via Playwright.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const TABLE_ID = '00000000-0000-7000-8000-000000010001';
const TABLE_RID = `ri.foundry.main.iceberg-table.${TABLE_ID}`;

const summary = {
  id: TABLE_ID,
  rid: TABLE_RID,
  project_rid: 'ri.foundry.main.project.demo',
  namespace: ['analytics', 'web'],
  name: 'pageviews',
  format_version: 2,
  location: 's3://foundry-iceberg-warehouse/analytics.web/pageviews',
  markings: ['public'],
  last_snapshot_at: '2026-05-04T08:00:00Z',
  row_count_estimate: 12_500,
  created_at: '2026-04-15T08:00:00Z',
};

const metadata = {
  'format-version': 2,
  'table-uuid': '5b6c1f8e-3a01-4f02-8e1c-aa0d65cf6c2c',
  location: summary.location,
  'last-sequence-number': 3,
  'last-updated-ms': 1_700_000_000_000,
  'current-schema-id': 0,
  schemas: [
    {
      'schema-id': 0,
      type: 'struct',
      fields: [
        { id: 1, name: 'session_id', required: true, type: 'string' },
        { id: 2, name: 'ts', required: true, type: 'timestamptz' },
      ],
    },
  ],
  'default-spec-id': 0,
  'partition-specs': [{ 'spec-id': 0, fields: [] }],
  'last-partition-id': 1000,
  'default-sort-order-id': 0,
  'sort-orders': [{ 'order-id': 0, fields: [] }],
  properties: { format: 'parquet' },
  'current-snapshot-id': 42,
  refs: { main: { 'snapshot-id': 42, type: 'branch' } },
  snapshots: [
    {
      'snapshot-id': 42,
      'parent-snapshot-id': null,
      'sequence-number': 3,
      'timestamp-ms': 1_700_000_000_000,
      summary: {
        operation: 'append',
        'added-data-files': '4',
        'deleted-data-files': '0',
        'added-records': '12500',
        'deleted-records': '0',
      },
      'manifest-list':
        's3://foundry-iceberg-warehouse/analytics.web/pageviews/metadata/snap-42-manifest-list.avro',
      'schema-id': 0,
    },
  ],
  'snapshot-log': [
    { 'timestamp-ms': 1_700_000_000_000, 'snapshot-id': 42 },
  ],
  'metadata-log': [],
};

test.describe('iceberg-tables list and metadata tab', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);

    await page.route('**/api/v1/iceberg-tables*', (route) => {
      const url = new URL(route.request().url());
      if (!url.pathname.includes('/iceberg-tables/')) {
        return route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({ tables: [summary] }),
        });
      }
      return route.continue();
    });

    await page.route(`**/api/v1/iceberg-tables/${TABLE_ID}`, (route) =>
      route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          summary,
          schema: metadata.schemas[0],
          properties: metadata.properties,
          partition_spec: metadata['partition-specs'][0],
          sort_order: metadata['sort-orders'][0],
          current_metadata_location: `${summary.location}/metadata/v3.metadata.json`,
          current_snapshot_id: 42,
          last_sequence_number: 3,
        }),
      }),
    );

    await page.route(
      `**/api/v1/iceberg-tables/${TABLE_ID}/snapshots`,
      (route) =>
        route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            snapshots: [
              {
                snapshot_id: 42,
                parent_snapshot_id: null,
                operation: 'append',
                timestamp: '2026-05-04T08:00:00Z',
                sequence_number: 3,
                manifest_list:
                  's3://foundry-iceberg-warehouse/analytics.web/pageviews/metadata/snap-42-manifest-list.avro',
                schema_id: 0,
                summary: metadata.snapshots[0].summary,
              },
            ],
          }),
        }),
    );

    await page.route(
      `**/api/v1/iceberg-tables/${TABLE_ID}/metadata`,
      (route) =>
        route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            metadata,
            metadata_location: `${summary.location}/metadata/v3.metadata.json`,
            history: [
              {
                version: 3,
                path: `${summary.location}/metadata/v3.metadata.json`,
                created_at: '2026-05-04T08:00:00Z',
              },
              {
                version: 2,
                path: `${summary.location}/metadata/v2.metadata.json`,
                created_at: '2026-05-03T08:00:00Z',
              },
            ],
          }),
        }),
    );

    await page.route(
      `**/api/v1/iceberg-tables/${TABLE_ID}/branches`,
      (route) =>
        route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            branches: [{ name: 'main', kind: 'branch', snapshot_id: 42 }],
          }),
        }),
    );
  });

  test('list shows the Beta banner and the seeded table', async ({ page }) => {
    await page.goto('/iceberg-tables');

    await expect(page.getByTestId('iceberg-beta-banner')).toBeVisible();
    await expect(page.getByTestId('iceberg-tables-grid')).toBeVisible();
    await expect(page.getByText('pageviews')).toBeVisible();
  });

  test('clicking a table opens detail and Metadata tab renders metadata.json', async ({
    page,
  }) => {
    await page.goto('/iceberg-tables');

    await page.getByTestId('iceberg-table-link').click();
    await expect(page.getByTestId('panel-overview')).toBeVisible();

    await page.getByTestId('tab-metadata').click();
    await expect(page.getByTestId('panel-metadata')).toBeVisible();
    const metadataPanel = page.getByTestId('metadata-json-readonly');
    await expect(metadataPanel).toBeVisible();
    await expect(metadataPanel).toContainText('"format-version": 2');
    await expect(metadataPanel).toContainText('"table-uuid"');
    await expect(page.getByTestId('metadata-download')).toBeVisible();
    await expect(page.getByTestId('metadata-open')).toBeVisible();
  });

  test('Snapshots tab renders the append operation badge with summary counts', async ({
    page,
  }) => {
    await page.goto(`/iceberg-tables/${TABLE_ID}`);
    await page.getByTestId('tab-snapshots').click();
    await expect(page.getByTestId('panel-snapshots')).toBeVisible();
    const row = page.getByTestId('snapshot-row-42');
    await expect(row).toBeVisible();
    await expect(row).toContainText('append');
    await expect(row).toContainText('added: 4');
    await expect(row).toContainText('+rows: 12500');
  });
});
