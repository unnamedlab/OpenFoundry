// P4 — Build Schedules application: opening a URL with filter
// query params restores the filter state on load (so the page is
// genuinely bookmark/share-able per Foundry doc § "Find and manage
// schedules").

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

test.describe('Build Schedules URL filters', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);

    await page.route('**/api/v1/data-integration/v1/schedules**', (route) =>
      route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({ data: [], total: 0 }),
      }),
    );
  });

  test('files + projects + sort restore from query string', async ({ page }) => {
    await page.goto(
      '/build-schedules?files=ri.foundry.main.dataset.x&projects=ri.foundry.main.project.demo&sort=name',
    );
    await expect(page.getByTestId('build-schedules-page')).toBeVisible();
    // The Files filter chip mirrors the URL.
    await expect(page.getByTestId('filter-files')).toContainText('ri.foundry.main.dataset.x');
    await expect(page.getByTestId('filter-projects')).toContainText(
      'ri.foundry.main.project.demo',
    );
    // The sort selector reflects the URL value.
    await expect(page.getByTestId('sort-select')).toHaveValue('name');
  });
});
