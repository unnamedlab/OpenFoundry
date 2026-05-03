// P3 — DeleteBranchDialog renders the preview-delete plan.
//
// Mocks `GET /datasets/{id}/branches/{branch}/preview-delete` so the
// dialog's reparent-plan section shows two children. Asserts:
//   * the dialog mounts when the row delete button is clicked,
//   * the children list renders with `branch → new_parent` rows,
//   * the "transactions are not deleted" warning is visible,
//   * the typed-name confirmation gate prevents accidental clicks.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const DATASET_ID = 'dataset-1';

const baseBranch = {
  dataset_id: DATASET_ID,
  fallback_chain: [],
  is_default: false,
  has_open_transaction: false,
  description: '',
  version: 1,
  base_version: 1,
  created_at: '2026-05-01T00:00:00Z',
  updated_at: '2026-05-01T00:00:00Z',
  last_activity_at: '2026-05-01T00:00:00Z',
} as const;

const master = {
  ...baseBranch,
  id: '00000000-0000-0000-0000-000000000001',
  rid: 'ri.foundry.main.branch.00000000-0000-0000-0000-000000000001',
  name: 'master',
  parent_branch_id: null,
  head_transaction_id: null,
  is_default: true,
};
const intermediate = {
  ...baseBranch,
  id: '00000000-0000-0000-0000-000000000002',
  rid: 'ri.foundry.main.branch.00000000-0000-0000-0000-000000000002',
  name: 'intermediate',
  parent_branch_id: master.id,
  head_transaction_id: null,
};
const childA = {
  ...baseBranch,
  id: '00000000-0000-0000-0000-000000000003',
  rid: 'ri.foundry.main.branch.00000000-0000-0000-0000-000000000003',
  name: 'child-a',
  parent_branch_id: intermediate.id,
  head_transaction_id: null,
};
const childB = {
  ...baseBranch,
  id: '00000000-0000-0000-0000-000000000004',
  rid: 'ri.foundry.main.branch.00000000-0000-0000-0000-000000000004',
  name: 'child-b',
  parent_branch_id: intermediate.id,
  head_transaction_id: null,
};

test.describe('delete branch dialog', () => {
  test('shows the children-to-reparent plan and gates on typed name', async ({
    page,
  }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);

    await page.route(`**/api/v1/datasets/${DATASET_ID}/branches`, (route) =>
      route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify([master, intermediate, childA, childB]),
      }),
    );
    await page.route(`**/api/v1/datasets/${DATASET_ID}/transactions`, (route) =>
      route.fulfill({ contentType: 'application/json', body: '[]' }),
    );
    await page.route(
      `**/api/v1/datasets/${DATASET_ID}/branches/intermediate/preview-delete`,
      (route) =>
        route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            branch: 'intermediate',
            branch_rid: intermediate.rid,
            current_parent: 'master',
            current_parent_rid: master.rid,
            children_to_reparent: [
              {
                branch: 'child-a',
                branch_rid: childA.rid,
                new_parent: 'master',
                new_parent_rid: master.rid,
              },
              {
                branch: 'child-b',
                branch_rid: childB.rid,
                new_parent: 'master',
                new_parent_rid: master.rid,
              },
            ],
            transactions_preserved: true,
            head_transaction: null,
          }),
        }),
    );

    await page.goto(`/datasets/${DATASET_ID}/branches`);
    await expect(page.getByTestId('branches-dashboard')).toBeVisible();

    await page.getByTestId('branch-delete-intermediate').click();

    const dialog = page.getByTestId('delete-branch-dialog');
    await expect(dialog).toBeVisible();
    await expect(dialog.getByTestId('delete-branch-reparent-plan')).toBeVisible();
    await expect(dialog).toContainText('Transactions are');
    await expect(dialog).toContainText('child-a');
    await expect(dialog).toContainText('child-b');

    // Confirm button stays disabled until the user types the branch name.
    await expect(dialog.getByTestId('delete-branch-confirm')).toBeDisabled();
    await dialog.getByTestId('delete-branch-confirm-input').fill('intermediate');
    await expect(dialog.getByTestId('delete-branch-confirm')).toBeEnabled();
  });
});
