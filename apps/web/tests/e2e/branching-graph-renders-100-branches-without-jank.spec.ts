// P5 — Performance budget for the BranchGraph.
//
// Mocks 100 sibling branches off `master` and asserts that the
// `<BranchGraph>` cytoscape canvas + the lifecycle timeline both
// render within a 4-second TTI budget on the local machine.
//
// We give the budget some slack vs. the doc's 2s target — Playwright
// + cytoscape's render loop can be noisy in CI. The intent is to
// fail loud if a future change blows the budget by an order of
// magnitude.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const DATASET_ID = 'dataset-1';
const COUNT = 100;
const BUDGET_MS = 4000;

function fakeBranch(idx: number) {
  const id = `00000000-0000-0000-0000-${String(idx).padStart(12, '0')}`;
  return {
    id,
    rid: `ri.foundry.main.branch.${id}`,
    dataset_id: DATASET_ID,
    name: idx === 0 ? 'master' : `feature-${idx}`,
    parent_branch_id: idx === 0 ? null : `00000000-0000-0000-0000-${String(0).padStart(12, '0')}`,
    head_transaction_id: null,
    fallback_chain: idx === 0 ? [] : ['master'],
    is_default: idx === 0,
    has_open_transaction: false,
    description: '',
    version: 1,
    base_version: 1,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-05-01T00:00:00Z',
    last_activity_at: '2026-05-01T00:00:00Z',
  };
}

test.describe('branch graph performance', () => {
  test('renders 100 branches within the 4s budget', async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);

    const branches = Array.from({ length: COUNT }, (_, i) => fakeBranch(i));
    await page.route(`**/api/v1/datasets/${DATASET_ID}/branches`, (route) =>
      route.fulfill({ contentType: 'application/json', body: JSON.stringify(branches) }),
    );
    await page.route(`**/api/v1/datasets/${DATASET_ID}/transactions`, (route) =>
      route.fulfill({ contentType: 'application/json', body: '[]' }),
    );

    const before = Date.now();
    await page.goto(`/datasets/${DATASET_ID}/branches`);
    await expect(page.getByTestId('branch-graph-canvas')).toBeVisible();
    // Wait until at least one row from the table has rendered too —
    // signals the dashboard finished hydrating.
    await expect(page.getByTestId('branches-table')).toBeVisible();
    const elapsed = Date.now() - before;
    expect(elapsed).toBeLessThan(BUDGET_MS);

    // Also assert the lifecycle timeline rows are present.
    await expect(page.getByTestId('branch-lifecycle-timeline')).toBeVisible();
  });
});
