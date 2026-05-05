// P2 — UI surfaces for the Foundry transaction layer:
//   * Snapshots tab renders the operation badge with the
//     "operation → Foundry equivalent" hint.
//   * Header surfaces the "Schema vN — strict mode" pill plus the
//     "How to ALTER schema" link (per `Iceberg tables/Notable
//     differences` § "Automatic schema evolution").

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const TABLE_ID = '00000000-0000-7000-8000-000000020001';
const TABLE_RID = `ri.foundry.main.iceberg-table.${TABLE_ID}`;

const summary = {
  id: TABLE_ID,
  rid: TABLE_RID,
  project_rid: 'ri.foundry.main.project.demo',
  namespace: ['analytics'],
  name: 'orders',
  format_version: 2,
  location: 's3://foundry-iceberg-warehouse/analytics/orders',
  markings: ['public'],
  last_snapshot_at: '2026-05-04T08:00:00Z',
  row_count_estimate: 200,
  created_at: '2026-04-15T08:00:00Z',
};

test.describe('iceberg P2 UI — snapshot badge + schema strict mode', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);

    await page.route(`**/api/v1/iceberg-tables/${TABLE_ID}`, (route) =>
      route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          summary,
          schema: {
            'schema-id': 3,
            type: 'struct',
            fields: [{ id: 1, name: 'id', required: true, type: 'long' }],
          },
          properties: {},
          partition_spec: { 'spec-id': 0, fields: [] },
          sort_order: { 'order-id': 0, fields: [] },
          current_metadata_location: `${summary.location}/metadata/v3.metadata.json`,
          current_snapshot_id: 100,
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
                snapshot_id: 100,
                parent_snapshot_id: 99,
                operation: 'overwrite',
                timestamp: '2026-05-04T08:00:00Z',
                sequence_number: 3,
                manifest_list:
                  's3://foundry-iceberg-warehouse/analytics/orders/metadata/snap-100-manifest-list.avro',
                schema_id: 3,
                summary: {
                  operation: 'overwrite',
                  'added-data-files': '5',
                  'deleted-data-files': '5',
                  'added-records': '500',
                  'deleted-records': '500',
                },
              },
              {
                snapshot_id: 99,
                parent_snapshot_id: null,
                operation: 'append',
                timestamp: '2026-05-04T07:50:00Z',
                sequence_number: 1,
                manifest_list:
                  's3://foundry-iceberg-warehouse/analytics/orders/metadata/snap-99-manifest-list.avro',
                schema_id: 3,
                summary: {
                  operation: 'append',
                  'added-data-files': '5',
                  'deleted-data-files': '0',
                  'added-records': '500',
                  'deleted-records': '0',
                },
              },
            ],
          }),
        }),
    );
  });

  test('header shows the schema-strict pill at the current schema id', async ({ page }) => {
    await page.goto(`/iceberg-tables/${TABLE_ID}`);
    const banner = page.getByTestId('iceberg-schema-strict-banner');
    await expect(banner).toBeVisible();
    await expect(banner).toContainText('Schema v3');
    await expect(banner).toContainText('strict mode');
    await expect(banner.getByRole('link', { name: 'How to ALTER schema' })).toBeVisible();
  });

  test('snapshots tab shows the iceberg→foundry badge for overwrite + append', async ({ page }) => {
    await page.goto(`/iceberg-tables/${TABLE_ID}`);
    await page.getByTestId('tab-snapshots').click();
    await expect(page.getByTestId('panel-snapshots')).toBeVisible();

    const overwriteBadge = page.getByTestId('iceberg-snapshot-badge-overwrite').first();
    await expect(overwriteBadge).toBeVisible();
    // Full sweep (added==removed) → SNAPSHOT.
    await expect(overwriteBadge).toContainText('overwrite');
    await expect(overwriteBadge).toContainText('SNAPSHOT');

    const appendBadge = page.getByTestId('iceberg-snapshot-badge-append').first();
    await expect(appendBadge).toBeVisible();
    await expect(appendBadge).toContainText('append');
    await expect(appendBadge).toContainText('APPEND');
  });
});
