// FASE 4 — node-level preview panel.
//
// Asserts that selecting a node in the canvas drives a request to the
// per-node preview endpoint and renders the returned rows + freshness
// indicator, with a working manual refresh button.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const PIPELINE_ID = 'pipeline-1';
const NODE_ID = 'high_value_filter';

test.describe('Pipeline Builder — node preview panel', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('renders preview rows for the selected node and refreshes on demand', async ({ page }) => {
    let previewCalls = 0;

    await page.route(`**/api/v1/pipelines/${PIPELINE_ID}`, async (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: PIPELINE_ID,
          name: 'Preview demo pipeline',
          description: '',
          owner_id: 'user-1',
          dag: [
            {
              id: 'orders',
              label: 'Orders',
              transform_type: 'passthrough',
              config: {},
              depends_on: [],
              input_dataset_ids: [],
              output_dataset_id: null,
            },
            {
              id: NODE_ID,
              label: 'Filter > 100',
              transform_type: 'filter',
              config: { predicate: 'amount > 100' },
              depends_on: ['orders'],
              input_dataset_ids: [],
              output_dataset_id: null,
            },
          ],
          status: 'draft',
          schedule_config: { enabled: false, cron: null },
          retry_policy: { max_attempts: 1, retry_on_failure: false, allow_partial_reexecution: true },
          next_run_at: null,
          created_at: '2026-05-05T00:00:00Z',
          updated_at: '2026-05-05T00:00:00Z',
        }),
      });
    });

    await page.route(`**/api/v1/pipelines/${PIPELINE_ID}/validate`, async (route) => {
      if (route.request().method() !== 'POST') return route.fallback();
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          pipeline_id: PIPELINE_ID,
          all_valid: true,
          nodes: [
            { node_id: 'orders', status: 'VALID', errors: [] },
            { node_id: NODE_ID, status: 'VALID', errors: [] },
          ],
        }),
      });
    });

    await page.route(
      `**/api/v1/pipelines/${PIPELINE_ID}/nodes/${NODE_ID}/preview**`,
      async (route) => {
        if (route.request().method() !== 'POST') return route.fallback();
        previewCalls += 1;
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            pipeline_id: PIPELINE_ID,
            node_id: NODE_ID,
            columns: ['order_id', 'amount'],
            rows: [
              { order_id: 'o1', amount: 250 },
              { order_id: 'o3', amount: 125 },
            ],
            sample_size: 2,
            generated_at: new Date().toISOString(),
            seed: 42,
            source_chain: ['orders', NODE_ID],
            fresh: true,
          }),
        });
      },
    );

    await page.goto(`/pipelines/${PIPELINE_ID}/edit`);

    // Pick the filter node so the preview panel kicks in.
    await page.getByTestId(`canvas-node-${NODE_ID}`).click();

    const panel = page.getByTestId('node-preview-panel');
    await expect(panel).toBeVisible();
    await expect(panel).toContainText('order_id');
    await expect(panel).toContainText('o1');
    await expect(panel).toContainText('o3');

    // Freshness indicator is rendered.
    await expect(page.getByTestId('preview-freshness')).toBeVisible();

    // Manual refresh fires another request.
    const callsBefore = previewCalls;
    await page.getByTestId('preview-refresh').click();
    await expect.poll(() => previewCalls).toBeGreaterThan(callsBefore);
  });
});
