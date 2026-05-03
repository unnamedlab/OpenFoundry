// P5 — Publish-to-Marketplace flow.
//
// The dataset header surfaces a "Publish to Marketplace…" entry
// inside the "Open in…" dropdown when the current user has manage
// permission. The modal collects name + version + manifest scope +
// include_* toggles + bootstrap_mode and POSTs to
// `/marketplace/products/from-dataset/{rid}`. On success we surface a
// link to `/marketplace/products/{id}`.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const DATASET_ID = 'dataset-1';

test.describe('marketplace publish dataset', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('opens modal, posts payload, surfaces marketplace link', async ({ page }) => {
    let receivedBody: unknown = null;
    await page.route(
      `**/api/v1/marketplace/products/from-dataset/${DATASET_ID}`,
      async (route) => {
        receivedBody = JSON.parse((await route.request().postData()) ?? '{}');
        await route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            id: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
            name: 'Aircraft health telemetry product',
            source_dataset_rid: DATASET_ID,
            entity_type: 'dataset',
            version: '1.0.0',
            project_id: null,
            published_by: null,
            export_includes_data: false,
            include_schema: true,
            include_branches: false,
            include_retention: true,
            include_schedules: false,
            manifest: {
              entity: 'dataset',
              version: '1.0.0',
              schema: null,
              retention: [],
              branching_policy: null,
              schedules: [],
              bootstrap: { mode: 'schema-only' },
            },
            bootstrap_mode: 'schema-only',
            published_at: '2026-05-03T10:00:00Z',
            created_at: '2026-05-03T10:00:00Z',
          }),
        });
      },
    );

    await page.goto(`/datasets/${DATASET_ID}`);

    // Open the dropdown + click Publish to Marketplace.
    await page.getByTestId('open-in-trigger').click();
    const publish = page.getByTestId('publish-to-marketplace');
    await expect(publish).toBeVisible();
    await publish.click();

    // Modal renders. Toggle retention on, keep schema on, leave the
    // rest as defaults. Submit.
    await expect(page.getByTestId('publish-modal')).toBeVisible();
    await expect(page.getByTestId('publish-include-schema')).toBeChecked();
    await page.getByTestId('publish-include-retention').check();
    await page.getByTestId('publish-submit').click();

    // Server received the right payload.
    await expect.poll(() => receivedBody).not.toBeNull();
    expect(receivedBody).toMatchObject({
      include_schema: true,
      include_retention: true,
      bootstrap_mode: 'schema-only',
    });
    // Success banner with link to the marketplace.
    await expect(page.getByTestId('publish-success')).toBeVisible();
    await expect(page.getByTestId('publish-success-link')).toHaveAttribute(
      'href',
      '/marketplace/products/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
    );
  });
});
