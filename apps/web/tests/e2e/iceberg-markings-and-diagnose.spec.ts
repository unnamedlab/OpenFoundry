// D1.1.8 P3 — UI surfaces for the markings manager + diagnose button.
//
// Validates:
//   * Permissions tab loads `/markings` and renders effective /
//     explicit / inherited buckets via MarkingsManager.
//   * Catalog Access tab's diagnose button posts to
//     `/iceberg/v1/diagnose` and renders per-step results.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const TABLE_ID = '00000000-0000-7000-8000-000000030001';
const TABLE_RID = `ri.foundry.main.iceberg-table.${TABLE_ID}`;

const summary = {
  id: TABLE_ID,
  rid: TABLE_RID,
  project_rid: 'ri.foundry.main.project.demo',
  namespace: ['secured'],
  name: 'users',
  format_version: 2,
  location: 's3://foundry-iceberg-warehouse/secured/users',
  markings: ['pii'],
  last_snapshot_at: '2026-05-04T08:00:00Z',
  row_count_estimate: null,
  created_at: '2026-04-15T08:00:00Z',
};

test.describe('iceberg P3 UI — markings + diagnose', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);

    await page.route(`**/api/v1/iceberg-tables/${TABLE_ID}`, (route) =>
      route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          summary,
          schema: { 'schema-id': 0, type: 'struct', fields: [] },
          properties: {},
          partition_spec: { 'spec-id': 0, fields: [] },
          sort_order: { 'order-id': 0, fields: [] },
          current_metadata_location: `${summary.location}/metadata/v1.metadata.json`,
          current_snapshot_id: 1,
          last_sequence_number: 1,
        }),
      }),
    );

    await page.route(
      `**/iceberg/v1/namespaces/secured/tables/users/markings`,
      (route) => {
        if (route.request().method() === 'PATCH') {
          return route.fulfill({
            contentType: 'application/json',
            body: JSON.stringify({
              effective: [{ marking_id: 'a', name: 'pii', description: 'PII' }],
              explicit: [{ marking_id: 'a', name: 'pii', description: 'PII' }],
              inherited_from_namespace: [],
            }),
          });
        }
        return route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            effective: [{ marking_id: 'a', name: 'pii', description: 'PII' }],
            explicit: [],
            inherited_from_namespace: [
              { marking_id: 'a', name: 'pii', description: 'PII' },
            ],
          }),
        });
      },
    );

    await page.route('**/iceberg/v1/diagnose', (route) =>
      route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          client: 'pyiceberg',
          success: true,
          steps: [
            {
              name: 'list_namespaces',
              ok: true,
              latency_ms: 12,
              detail: '3 namespaces',
            },
            {
              name: 'load_probe_namespace',
              ok: true,
              latency_ms: 7,
              detail: 'probe namespace reachable',
            },
          ],
          total_latency_ms: 19,
        }),
      }),
    );
  });

  test('Permissions tab shows the three marking buckets', async ({ page }) => {
    await page.goto(`/iceberg-tables/${TABLE_ID}`);
    await page.getByTestId('tab-permissions').click();
    await expect(page.getByTestId('panel-permissions')).toBeVisible();
    await expect(page.getByTestId('iceberg-markings-manager')).toBeVisible();
    await expect(page.getByTestId('iceberg-markings-effective')).toContainText('pii');
    await expect(page.getByTestId('iceberg-markings-inherited')).toContainText('pii');
    await expect(page.getByTestId('iceberg-markings-explicit')).toContainText('— none set —');
  });

  test('Catalog Access diagnose button reports per-step results', async ({ page }) => {
    await page.goto(`/iceberg-tables/${TABLE_ID}`);
    await page.getByTestId('tab-catalog-access').click();
    await expect(page.getByTestId('panel-catalog-access')).toBeVisible();
    await expect(page.getByTestId('iceberg-diagnose-row')).toBeVisible();
    await page.getByTestId('iceberg-diagnose-pyiceberg').click();
    const result = page.getByTestId('iceberg-diagnose-result');
    await expect(result).toBeVisible();
    await expect(result).toContainText('list_namespaces');
    await expect(result).toContainText('load_probe_namespace');
  });
});
