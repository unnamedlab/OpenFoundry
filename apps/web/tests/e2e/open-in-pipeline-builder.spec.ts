// P5 — "Open in Pipeline Builder" entry point.
//
// Foundry's "Add a dataset input" doc + the U4 dataset view spec
// require a single-click flow that lands the user in Pipeline Builder
// with the dataset already prefilled as input. The DatasetHeader
// dropdown surfaces the menu; clicking the entry navigates to
// /pipelines/new?input=<rid> (the gateway forwards `seed_dataset_rid`
// to pipeline-authoring-service which synthesises the passthrough
// node).

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const DATASET_ID = 'dataset-1';

test.describe('open in pipeline builder', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);

    // Stub the new-pipeline route so the test can assert the URL it
    // navigated to. We don't need to render anything elaborate —
    // returning a minimal HTML body with a marker is enough.
    await page.route('**/pipelines/new**', (route) =>
      route.fulfill({
        contentType: 'text/html',
        body: `<!doctype html><html><head><title>Pipeline Builder</title></head>
               <body data-testid="pipeline-builder-page">Pipeline Builder</body></html>`,
      }),
    );
  });

  test('opens Pipeline Builder with the dataset prefilled in the URL', async ({ page }) => {
    await page.goto(`/datasets/${DATASET_ID}`);

    // Open the "Open in…" dropdown and click Pipeline Builder.
    await page.getByTestId('open-in-trigger').click();
    await expect(page.getByTestId('open-in-menu')).toBeVisible();
    const target = page.getByTestId('open-in-pipeline_builder');
    await expect(target).toBeVisible();
    await target.click();

    // Page navigates to /pipelines/new?input=<rid>.
    await page.waitForURL(/\/pipelines\/new\?input=/);
    const url = new URL(page.url());
    expect(url.pathname).toBe('/pipelines/new');
    expect(url.searchParams.get('input')).toBe(DATASET_ID);
  });

  test('Coming-soon entries are disabled and surface a tooltip', async ({ page }) => {
    await page.goto(`/datasets/${DATASET_ID}`);

    await page.getByTestId('open-in-trigger').click();
    const contour = page.getByTestId('open-in-contour');
    await expect(contour).toBeVisible();
    await expect(contour).toBeDisabled();
    // The "Coming soon" pill renders inside the row.
    await expect(contour).toContainText('Coming soon');
  });
});
