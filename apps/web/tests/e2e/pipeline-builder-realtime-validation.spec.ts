// FASE 3 — type-safe realtime validation in the Pipeline Builder canvas.
//
// Asserts that:
//   1. Per-node ✓/✗ icons render on the canvas based on the
//      `POST /api/v1/pipelines/{id}/validate` report.
//   2. Selecting an INVALID node surfaces the error list (squiggle
//      style) inside <NodeConfig/>.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const PIPELINE_ID = 'pipeline-1';
const NODE_ID = 'filter-with-bad-predicate';

test.describe('Pipeline Builder — type-safe realtime validation', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('renders ✗ badge and surfaces the error list for an INVALID filter', async ({ page }) => {
    // Override the pipeline payload so the canvas hosts a filter node
    // whose predicate is structurally invalid (returns Integer, not
    // Boolean).
    await page.route(`**/api/v1/pipelines/${PIPELINE_ID}`, async (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: PIPELINE_ID,
          name: 'Demo with bad predicate',
          description: '',
          owner_id: 'user-1',
          dag: [
            {
              id: 'src',
              label: 'Source',
              transform_type: 'passthrough',
              config: { columns: ['age'] },
              depends_on: [],
              input_dataset_ids: [],
              output_dataset_id: null,
            },
            {
              id: NODE_ID,
              label: 'Bad filter',
              transform_type: 'filter',
              config: { predicate: 'age + 1' },
              depends_on: ['src'],
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

    // Per-node validation report — what the canvas paints icons from.
    await page.route(`**/api/v1/pipelines/${PIPELINE_ID}/validate`, async (route) => {
      if (route.request().method() !== 'POST') return route.fallback();
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          pipeline_id: PIPELINE_ID,
          all_valid: false,
          nodes: [
            { node_id: 'src', status: 'VALID', errors: [] },
            {
              node_id: NODE_ID,
              status: 'INVALID',
              errors: [
                {
                  node_id: NODE_ID,
                  column: null,
                  message: 'predicate must return Boolean, got Integer',
                },
              ],
            },
          ],
        }),
      });
    });

    // PUT happens when `runNodeReports` finds the page dirty; on cold
    // load there are no edits, so it shouldn't fire — but mock it
    // defensively to avoid a 404 if it does.
    await page.route(`**/api/v1/pipelines/${PIPELINE_ID}`, async (route) => {
      if (route.request().method() !== 'PUT') return route.fallback();
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: PIPELINE_ID,
          name: 'Demo with bad predicate',
          description: '',
          owner_id: 'user-1',
          dag: [],
          status: 'draft',
          schedule_config: { enabled: false, cron: null },
          retry_policy: { max_attempts: 1, retry_on_failure: false, allow_partial_reexecution: true },
          next_run_at: null,
          created_at: '2026-05-05T00:00:00Z',
          updated_at: '2026-05-05T00:00:00Z',
        }),
      });
    });

    await page.goto(`/pipelines/${PIPELINE_ID}/edit`);

    // Wait for the canvas to render the source node.
    await expect(page.getByTestId('canvas-node-src')).toBeVisible();

    // The filter node's status badge appears once the validation
    // request resolves (debounced ~250 ms).
    await expect(page.getByTestId(`canvas-node-status-${NODE_ID}`)).toHaveAttribute(
      'data-tone',
      'error',
    );

    // Click the filter node to open NodeConfig — the error list should
    // be surfaced under the node id with the predicate diagnostic.
    await page.getByTestId(`canvas-node-${NODE_ID}`).click();
    await expect(page.getByTestId('node-config-errors')).toBeVisible();
    await expect(page.getByTestId('node-config-errors')).toContainText(
      'predicate must return Boolean',
    );
  });
});
