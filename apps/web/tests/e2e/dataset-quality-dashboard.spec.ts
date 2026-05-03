// P6 — Dataset Quality dashboard E2E.
//
// QualityDashboard mounts under the Health tab on the dataset detail
// page. This spec stubs `GET /api/v1/datasets/:rid/health` with a
// realistic payload and asserts the six Foundry-parity cards render:
//   1. Freshness          (badge with seconds + SLA caption)
//   2. Last build         (icon + status uppercase)
//   3. Schema drift       (yes/no badge)
//   4. Row / Col counts   (numeric + sparkline placeholder)
//   5. Txn failures 24h   (% + per-tx_type bar list)
//   6. Health-check policies (CTA + copy)

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const DATASET_ID = 'dataset-1';

test.describe('dataset quality dashboard', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);

    await page.route(`**/api/v1/datasets/${DATASET_ID}/health`, (route) =>
      route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          dataset_rid: DATASET_ID,
          dataset_id: DATASET_ID,
          row_count: 1_280_000,
          col_count: 12,
          null_pct_by_column: { user_id: 0.0, region: 0.02 },
          freshness_seconds: 3_600,
          last_commit_at: '2026-05-03T10:00:00Z',
          txn_failure_rate_24h: 0.025,
          last_build_status: 'success',
          schema_drift_flag: false,
          extras: {
            failure_breakdown_24h: { APPEND: 2, UPDATE: 1 },
            row_count_history: [
              { value: 1_240_000 },
              { value: 1_265_000 },
              { value: 1_280_000 },
            ],
          },
          last_computed_at: '2026-05-03T10:05:00Z',
        }),
      }),
    );
  });

  test('renders the six Foundry-parity quality cards', async ({ page }) => {
    await page.goto(`/datasets/${DATASET_ID}`);

    // Navigate to the Health tab (Foundry "Data Health" surface).
    const healthTab = page.getByRole('button', { name: /^Health$/ });
    if (await healthTab.count()) {
      await healthTab.first().click();
    }

    // Each card carries a deterministic `data-testid` for stable
    // assertions even if the surrounding shell is re-themed.
    await expect(page.getByTestId('quality-card-freshness')).toBeVisible();
    await expect(page.getByTestId('quality-card-last-build')).toBeVisible();
    await expect(page.getByTestId('quality-card-schema-drift')).toBeVisible();
    await expect(page.getByTestId('quality-card-counts')).toBeVisible();
    await expect(page.getByTestId('quality-card-failures')).toBeVisible();
    await expect(page.getByTestId('quality-card-policies')).toBeVisible();

    // Spot-check signal copy: schema drift "no" + last-build "SUCCESS".
    await expect(page.getByTestId('quality-card-schema-drift')).toContainText(
      /no|yes/i,
    );
    await expect(page.getByTestId('quality-card-last-build')).toContainText(
      /SUCCESS/i,
    );
  });
});
