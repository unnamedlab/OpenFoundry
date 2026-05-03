// P4 — Branch retention tab.
//
// Loads `/datasets/{id}/branches/{branch}` with a mocked branch +
// markings response and asserts:
//   * Retention tab is the default,
//   * the policy radios render and allow switching to TTL_DAYS,
//   * the TTL input appears once TTL_DAYS is selected,
//   * the Security tab surfaces the markings projection.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const DATASET_ID = 'dataset-1';
const BRANCH = 'feature';

const branchRow = {
  id: '00000000-0000-0000-0000-000000000010',
  rid: 'ri.foundry.main.branch.00000000-0000-0000-0000-000000000010',
  dataset_id: DATASET_ID,
  name: BRANCH,
  parent_branch_id: '00000000-0000-0000-0000-000000000001',
  head_transaction_id: null,
  fallback_chain: ['master'],
  is_default: false,
  has_open_transaction: false,
  description: '',
  version: 1,
  base_version: 1,
  retention_policy: 'INHERITED',
  retention_ttl_days: null,
  archived_at: null,
  created_at: '2026-05-01T00:00:00Z',
  updated_at: '2026-05-01T00:00:00Z',
  last_activity_at: '2026-05-01T00:00:00Z',
};

test.describe('branch retention tab', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);

    await page.route(`**/api/v1/datasets/${DATASET_ID}/branches`, (route) =>
      route.fulfill({ contentType: 'application/json', body: JSON.stringify([branchRow]) }),
    );
    await page.route(
      `**/api/v1/datasets/${DATASET_ID}/branches/${BRANCH}/markings`,
      (route) =>
        route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            effective: ['00000000-0000-0000-0000-000000000aaa'],
            explicit: [],
            inherited_from_parent: ['00000000-0000-0000-0000-000000000aaa'],
          }),
        }),
    );
    await page.route(
      `**/api/v1/datasets/${DATASET_ID}/branches/${BRANCH}/retention`,
      async (route) => {
        const body = JSON.parse(route.request().postData() ?? '{}');
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ branch: BRANCH, policy: body.policy, ttl_days: body.ttl_days }),
        });
      },
    );
  });

  test('switches policy to TTL_DAYS and shows the TTL input', async ({ page }) => {
    await page.goto(`/datasets/${DATASET_ID}/branches/${BRANCH}`);

    await expect(page.getByTestId('branch-detail')).toBeVisible();
    await expect(page.getByTestId('branch-retention-section')).toBeVisible();
    await page.getByTestId('branch-retention-policy-TTL_DAYS').check();
    await expect(page.getByTestId('branch-retention-ttl-input')).toBeVisible();
    await page.getByTestId('branch-retention-ttl-input').fill('30');
    await expect(page.getByTestId('branch-retention-save')).toBeEnabled();
  });

  test('security tab shows effective + explicit + inherited markings', async ({ page }) => {
    await page.goto(`/datasets/${DATASET_ID}/branches/${BRANCH}`);

    await page.getByTestId('branch-detail-tab-security').click();
    await expect(page.getByTestId('branch-security-section')).toBeVisible();
    await expect(page.getByTestId('branch-markings-effective')).toBeVisible();
    await expect(page.getByTestId('branch-markings-inherited_from_parent')).toBeVisible();
    await expect(page.getByTestId('branch-markings-explicit')).toBeVisible();
  });
});
