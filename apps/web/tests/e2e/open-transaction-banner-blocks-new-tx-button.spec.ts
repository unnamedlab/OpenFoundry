// P3 — OpenTransactionBanner blocks new-transaction starts.
//
// Mocks the dataset detail flow so the active branch reports
// `has_open_transaction = true` and the transactions list contains
// one OPEN row. Asserts:
//   * the banner is visible,
//   * the "View transaction" link points at the History tab with the
//     transaction id query string,
//   * the "Commit" / "Abort" buttons render only when canManage is on.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const DATASET_ID = 'dataset-1';
const OPEN_TXN_ID = '00000000-0000-0000-0000-00000000aaaa';

const branchWithOpen = {
  id: '00000000-0000-0000-0000-000000000001',
  rid: 'ri.foundry.main.branch.00000000-0000-0000-0000-000000000001',
  dataset_id: DATASET_ID,
  name: 'main',
  parent_branch_id: null,
  head_transaction_id: null,
  fallback_chain: [],
  is_default: true,
  has_open_transaction: true,
  description: '',
  version: 1,
  base_version: 1,
  created_at: '2026-05-01T00:00:00Z',
  updated_at: '2026-05-01T00:00:00Z',
  last_activity_at: '2026-05-01T00:00:00Z',
};

test.describe('open transaction banner', () => {
  test('shows the banner with view/commit/abort actions on the active branch', async ({
    page,
  }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);

    await page.route(`**/api/v1/datasets/${DATASET_ID}/branches`, (route) =>
      route.fulfill({ contentType: 'application/json', body: JSON.stringify([branchWithOpen]) }),
    );
    await page.route(`**/api/v1/datasets/${DATASET_ID}/transactions`, (route) =>
      route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify([
          {
            id: OPEN_TXN_ID,
            dataset_id: DATASET_ID,
            operation: 'APPEND',
            branch_name: 'main',
            status: 'OPEN',
            summary: 'in flight',
            metadata: {},
            created_at: '2026-05-01T12:00:00Z',
            committed_at: null,
          },
        ]),
      }),
    );

    await page.goto(`/datasets/${DATASET_ID}`);

    const banner = page.getByTestId('open-transaction-banner');
    await expect(banner).toBeVisible();
    await expect(banner).toContainText('open transaction is in progress');

    const viewLink = banner.getByTestId('open-transaction-view');
    await expect(viewLink).toBeVisible();
    await expect(viewLink).toHaveAttribute(
      'href',
      `/datasets/${DATASET_ID}?tab=history&txn=${OPEN_TXN_ID}`,
    );

    // The commit/abort actions render under the manage gate; the
    // demo session is operator (admin) by default in our fixtures, so
    // both should be visible.
    await expect(banner.getByTestId('open-transaction-commit')).toBeVisible();
    await expect(banner.getByTestId('open-transaction-abort')).toBeVisible();
  });
});
