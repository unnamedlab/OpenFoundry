// T7.2 — Dataset retention tab E2E.
//
// The retention sub-panel under Details lists policies returned by
// /v1/datasets/{id}/retention-policies. The system policy
// `DELETE_ABORTED_TRANSACTIONS` is always present and rendered with a
// "System policy" badge.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const DATASET_ID = 'dataset-1';

test.describe('dataset retention tab', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);

    await page.route(`**/api/v1/datasets/${DATASET_ID}/transactions`, (route) =>
      route.fulfill({ contentType: 'application/json', body: '[]' }),
    );
    await page.route(`**/api/v1/datasets/${DATASET_ID}/branches`, (route) =>
      route.fulfill({ contentType: 'application/json', body: '[]' }),
    );

    await page.route(`**/api/v1/datasets/${DATASET_ID}/retention-policies**`, (route) =>
      route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify([
          {
            id: 'policy-system-aborted',
            policy_kind: 'DELETE_ABORTED_TRANSACTIONS',
            display_name: 'Delete aborted transactions',
            description: 'Reaps physical files from transactions that never committed.',
            is_system: true,
            grace_seconds: 86400,
            scope: { kind: 'dataset', dataset_id: DATASET_ID },
          },
          {
            id: 'policy-org-90d',
            policy_kind: 'TIME_BASED',
            display_name: '90-day retention',
            description: 'Snapshots older than 90 days are pruned.',
            is_system: false,
            grace_seconds: 86400 * 90,
            scope: { kind: 'organization', organization_id: 'org-1' },
          },
        ]),
      }),
    );
  });

  test('shows the system DELETE_ABORTED_TRANSACTIONS policy with a System policy badge', async ({ page }) => {
    await page.goto(`/datasets/${DATASET_ID}`);
    await page.getByRole('button', { name: 'Details' }).click();
    await page.getByRole('button', { name: /retention policies/i }).click();
    // The policy table renders the human display name and the badge.
    await expect(page.getByText('Delete aborted transactions')).toBeVisible();
    await expect(page.getByText('System policy').first()).toBeVisible();
  });
});
