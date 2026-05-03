// P4 — Retention tab e2e.
//
// Foundry's "View retention policies for a dataset [Beta]" surface
// must surface 5 sections: Beta banner, Inherited (Org/Space/Project),
// Explicit, Effective (winner-take), Preview deletions. This spec
// mocks `applicable-policies` and `retention-preview`, opens the
// dataset detail page, and asserts the relevant pieces render.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const DATASET_ID = 'dataset-1';

const applicableFixture = {
  dataset_rid: DATASET_ID,
  context: { project_id: null, marking_id: null, space_id: null, org_id: null },
  inherited: {
    org: [
      {
        id: 'org-1',
        name: 'org-365-days',
        scope: '',
        target_kind: 'transaction',
        retention_days: 365,
        legal_hold: false,
        purge_mode: 'hard-delete-after-ttl',
        rules: [],
        is_system: false,
        selector: { all_datasets: true },
        criteria: {},
        grace_period_minutes: 60,
        created_at: '2026-05-03T10:00:00Z',
        updated_at: '2026-05-03T10:00:00Z',
        active: true,
      },
    ],
    space: [],
    project: [],
  },
  explicit: [
    {
      id: 'explicit-1',
      name: 'explicit-7-days',
      scope: '',
      target_kind: 'transaction',
      retention_days: 7,
      legal_hold: false,
      purge_mode: 'hard-delete-after-ttl',
      rules: [],
      is_system: false,
      selector: { dataset_rid: DATASET_ID },
      criteria: {},
      grace_period_minutes: 60,
      created_at: '2026-05-03T10:30:00Z',
      updated_at: '2026-05-03T10:30:00Z',
      active: true,
    },
  ],
  effective: {
    id: 'explicit-1',
    name: 'explicit-7-days',
    scope: '',
    target_kind: 'transaction',
    retention_days: 7,
    legal_hold: false,
    purge_mode: 'hard-delete-after-ttl',
    rules: [],
    is_system: false,
    selector: { dataset_rid: DATASET_ID },
    criteria: {},
    grace_period_minutes: 60,
    created_at: '2026-05-03T10:30:00Z',
    updated_at: '2026-05-03T10:30:00Z',
    active: true,
  },
  conflicts: [
    { winner_id: 'explicit-1', loser_id: 'org-1', reason: 'winner_has_lower_retention_days' },
  ],
};

const previewFixture = {
  dataset_rid: DATASET_ID,
  as_of_days: 0,
  as_of: '2026-05-03T11:00:00Z',
  effective_policy: applicableFixture.effective,
  transactions: [
    {
      id: '11111111-1111-1111-1111-111111111111',
      tx_type: 'APPEND',
      status: 'ABORTED',
      started_at: '2026-04-01T09:00:00Z',
      committed_at: null,
      would_delete: true,
      policy_id: 'system-1',
      policy_name: 'DELETE_ABORTED_TRANSACTIONS',
      reason: 'transaction_state=ABORTED',
    },
    {
      id: '22222222-2222-2222-2222-222222222222',
      tx_type: 'SNAPSHOT',
      status: 'COMMITTED',
      started_at: '2026-05-01T09:00:00Z',
      committed_at: '2026-05-01T09:01:00Z',
      would_delete: false,
      policy_id: null,
      policy_name: null,
      reason: null,
    },
  ],
  files: [
    {
      id: 'file-aborted-1',
      transaction_id: '11111111-1111-1111-1111-111111111111',
      logical_path: 'data/lost.parquet',
      physical_uri: 'local:///foundry/datasets/lost.parquet',
      size_bytes: 4096,
      policy_id: 'system-1',
      policy_name: 'DELETE_ABORTED_TRANSACTIONS',
      reason: 'transaction_state=ABORTED',
    },
  ],
  summary: {
    transactions_total: 2,
    transactions_would_delete: 1,
    files_total: 1,
    bytes_total: 4096,
  },
  warnings: [],
};

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

    await page.route(
      `**/api/v1/datasets/${DATASET_ID}/applicable-policies**`,
      (route) =>
        route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify(applicableFixture),
        }),
    );
    await page.route(
      `**/api/v1/datasets/${DATASET_ID}/retention-preview**`,
      (route) =>
        route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify(previewFixture),
        }),
    );
  });

  test('renders Beta banner, inherited / explicit / effective sections, and preview', async ({
    page,
  }) => {
    await page.goto(`/datasets/${DATASET_ID}`);
    await page.getByRole('button', { name: /^Retention$/ }).click();

    const tab = page.locator('[data-component="retention-policies-tab"]');
    await expect(tab).toBeVisible();

    // 1) Beta banner.
    await expect(tab.getByTestId('retention-beta-banner')).toBeVisible();
    await expect(tab.getByText('Beta', { exact: true })).toBeVisible();

    // 2) Effective policy block surfaces the explicit-7-days winner.
    await expect(tab.getByTestId('effective-policy-name')).toHaveText('explicit-7-days');

    // 3) Inherited section shows the Org bucket with the 365-day policy.
    await expect(tab.getByTestId('inherited-org')).toBeVisible();
    await expect(tab.getByTestId('inherited-org')).toContainText('org-365-days');
    await expect(tab.getByTestId('inherited-org')).toContainText('inherited from org');

    // 4) Explicit section lists the explicit-7-days policy.
    const explicitRow = tab.locator('[data-testid="explicit-row"]');
    await expect(explicitRow).toContainText('explicit-7-days');

    // 5) Preview shows 1/2 transactions flagged + matches the system policy.
    await expect(tab.getByTestId('preview-tx-count')).toHaveText('1 / 2');
    const previewRow = tab.locator('[data-testid="preview-row"]');
    await expect(previewRow.first()).toContainText('DELETE_ABORTED_TRANSACTIONS');
  });

  test('Files tab shows retention purge badge that opens the Retention tab', async ({
    page,
  }) => {
    // Files endpoint returns the same id as the preview's file row so
    // the badge's lookup hits.
    await page.route(`**/api/v1/datasets/${DATASET_ID}/files**`, (route) =>
      route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          view_id: null,
          branch: 'main',
          total: 1,
          files: [
            {
              id: 'file-aborted-1',
              dataset_id: DATASET_ID,
              transaction_id: '11111111-1111-1111-1111-111111111111',
              logical_path: 'data/lost.parquet',
              physical_uri: 'local:///foundry/datasets/lost.parquet',
              size_bytes: 4096,
              sha256: null,
              created_at: '2026-04-01T09:00:00Z',
              modified_at: '2026-04-01T09:00:00Z',
              status: 'active',
            },
          ],
        }),
      }),
    );

    await page.goto(`/datasets/${DATASET_ID}`);
    await page.getByRole('button', { name: /^Files$/ }).click();

    const badge = page.getByTestId('files-retention-badge');
    await expect(badge).toBeVisible();
    await expect(badge).toContainText('DELETE_ABORTED_TRANSACTIONS');

    // Clicking the badge routes back to the Retention tab.
    await badge.click();
    await expect(page.locator('[data-component="retention-policies-tab"]')).toBeVisible();
  });
});
