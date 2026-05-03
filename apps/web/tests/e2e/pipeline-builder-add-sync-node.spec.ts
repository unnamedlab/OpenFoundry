// D1.1.5 P3 — Pipeline Builder e2e: add a Data Connection Sync node.
//
// Walks the operator through:
//   1. Open the demo pipeline editor.
//   2. Add the SYNC node from the new "Data Connection: Sync" palette
//      category.
//   3. Fill the kind-specific config (source RID + sync def UUID).
//   4. Trigger Validate and confirm the payload reaches the backend
//      with `transform_type === 'SYNC'` and the right config.

import { expect, test } from '@playwright/test';
import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const PIPELINE_ID = 'pipeline-1';

type ValidationPayload = {
  nodes: Array<{
    id: string;
    transform_type: string;
    config?: Record<string, unknown>;
  }>;
};

test.describe('pipeline builder — Data Connection Sync node', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('adds a SYNC node with kind-specific config', async ({ page }) => {
    let validateCalls = 0;
    let lastBody: ValidationPayload | null = null;

    await page.route('**/api/v1/pipelines/_validate', async (route) => {
      validateCalls += 1;
      const body = JSON.parse(route.request().postData() ?? '{}') as ValidationPayload;
      lastBody = body;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          valid: true,
          errors: [],
          warnings: [],
          next_run_at: null,
          summary: {
            node_count: (body.nodes ?? []).length,
            edge_count: 0,
            root_node_ids: (body.nodes ?? []).map((n) => n.id),
            leaf_node_ids: (body.nodes ?? []).map((n) => n.id)
          }
        })
      });
    });

    await page.goto(`/pipelines/${PIPELINE_ID}/edit`);
    await expect(page.getByTestId('node-palette')).toBeVisible();

    // Add the SYNC node from the new palette category.
    await page.getByTestId('palette-entry-SYNC').click();

    // The kind-specific config panel must render with the SYNC fields.
    await expect(page.getByTestId('kind-config-SYNC')).toBeVisible();
    await page.getByTestId('sync-source-rid').fill(
      'ri.foundry.main.connector.s3-prod'
    );
    await page.getByTestId('sync-def-id').fill(
      '018f1234-1234-7000-8000-abcdefabcdef'
    );

    // Trigger validate and inspect the payload.
    await page.getByRole('button', { name: 'Validate' }).click();
    await expect.poll(() => validateCalls).toBeGreaterThan(0);

    const captured = lastBody as ValidationPayload | null;
    const nodes = captured?.nodes ?? [];
    const sync = nodes.find((n) => n.transform_type === 'SYNC');
    expect(sync, 'SYNC node lands in the validation payload').toBeTruthy();
    const cfg = (sync?.config ?? {}) as {
      source_rid?: string;
      sync_def_id?: string;
      logic_kind?: string;
    };
    expect(cfg.logic_kind).toBe('SYNC');
    expect(cfg.source_rid).toBe('ri.foundry.main.connector.s3-prod');
    expect(cfg.sync_def_id).toBe('018f1234-1234-7000-8000-abcdefabcdef');
  });
});
