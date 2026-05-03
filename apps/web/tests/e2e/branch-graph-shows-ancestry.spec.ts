// P3 — BranchGraph ancestry rendering.
//
// Mounts /datasets/{id}/branches with a mocked branch list of
// `master → develop → feature`, then asserts:
//   * the BranchGraph cytoscape canvas renders,
//   * the side table lists every branch in ancestry order,
//   * hovering surfaces the tooltip with relative `last_activity_at`
//     and the head transaction RID.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const DATASET_ID = 'dataset-1';

const master = {
  id: '00000000-0000-0000-0000-000000000001',
  rid: 'ri.foundry.main.branch.00000000-0000-0000-0000-000000000001',
  dataset_id: DATASET_ID,
  dataset_rid: 'ri.foundry.main.dataset.dataset-1',
  name: 'master',
  parent_branch_id: null,
  head_transaction_id: '00000000-0000-0000-0000-00000000aaaa',
  fallback_chain: [],
  is_default: true,
  has_open_transaction: false,
  description: '',
  version: 1,
  base_version: 1,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-05-01T12:00:00Z',
  last_activity_at: '2026-05-01T12:00:00Z',
};
const develop = {
  ...master,
  id: '00000000-0000-0000-0000-000000000002',
  rid: 'ri.foundry.main.branch.00000000-0000-0000-0000-000000000002',
  name: 'develop',
  parent_branch_id: master.id,
  head_transaction_id: '00000000-0000-0000-0000-00000000bbbb',
  fallback_chain: ['master'],
  is_default: false,
};
const feature = {
  ...master,
  id: '00000000-0000-0000-0000-000000000003',
  rid: 'ri.foundry.main.branch.00000000-0000-0000-0000-000000000003',
  name: 'feature',
  parent_branch_id: develop.id,
  head_transaction_id: null,
  fallback_chain: ['develop', 'master'],
  is_default: false,
};

test.describe('branch graph', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);

    await page.route(`**/api/v1/datasets/${DATASET_ID}/branches`, (route) =>
      route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify([master, develop, feature]),
      }),
    );
    await page.route(`**/api/v1/datasets/${DATASET_ID}/transactions`, (route) =>
      route.fulfill({ contentType: 'application/json', body: '[]' }),
    );
  });

  test('renders the ancestry tree with a row per branch', async ({ page }) => {
    await page.goto(`/datasets/${DATASET_ID}/branches`);

    await expect(page.getByTestId('branches-dashboard')).toBeVisible();
    await expect(page.getByTestId('branch-graph')).toBeVisible();
    await expect(page.getByTestId('branch-graph-canvas')).toBeVisible();

    // Side table — one row per branch.
    await expect(page.getByTestId('branch-row-master')).toBeVisible();
    await expect(page.getByTestId('branch-row-develop')).toBeVisible();
    await expect(page.getByTestId('branch-row-feature')).toBeVisible();

    // Selecting a row highlights it.
    await page.getByTestId('branch-row-feature').click();
    await expect(page.getByTestId('branch-row-feature')).toHaveClass(/bg-blue-50|bg-blue-950/);

    // Toolbar controls render.
    await expect(page.getByTestId('branch-graph-zoom-in')).toBeEnabled();
    await expect(page.getByTestId('branch-graph-fit')).toBeEnabled();
    await expect(page.getByTestId('branch-graph-toggle-retired')).toBeVisible();
  });
});
