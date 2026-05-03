// P3 — Foundry doc § "Job graph compilation":
//   "Dataset icon color provides information about JobSpecs and
//    branching. If a dataset's icon is gray, this indicates that no
//    JobSpec exists on the master branch. If the dataset icon is
//    blue, a JobSpec is defined on the master branch."
//
// This spec mocks `GET /datasets/{id}/job-specs` to return an empty
// list and asserts the catalog row's `<JobSpecBadge>` shows the grey
// "No JobSpec" variant via the `data-jobspec-on-master="false"` hook.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const DATASET_ID = 'dataset-1';

test.describe('dataset icon coloring', () => {
  test('shows the grey JobSpec badge when no JobSpec exists on master', async ({
    page,
  }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);

    // Empty list ⇒ no JobSpec on master.
    await page.route(`**/api/v1/datasets/${DATASET_ID}/job-specs**`, (route) =>
      route.fulfill({ contentType: 'application/json', body: '[]' }),
    );

    await page.goto('/datasets');
    const badge = page.locator('[data-testid="jobspec-badge"]').first();
    await expect(badge).toBeVisible();
    await expect(badge).toHaveAttribute('data-jobspec-on-master', 'false');
    await expect(badge).toContainText('No JobSpec');
  });

  test('shows the blue JobSpec badge when a JobSpec is defined on master', async ({
    page,
  }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);

    await page.route(`**/api/v1/datasets/${DATASET_ID}/job-specs**`, (route) =>
      route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify([
          {
            id: 'js-1',
            rid: 'ri.foundry.main.jobspec.js-1',
            pipeline_rid: 'ri.foundry.main.pipeline.demo',
            branch_name: 'master',
            output_dataset_rid: `ri.foundry.main.dataset.${DATASET_ID}`,
            output_branch: 'master',
            job_spec_json: {},
            inputs: [],
            content_hash: 'abc',
            version: 1,
            published_by: 'user-1',
            published_at: '2026-05-01T00:00:00Z',
          },
        ]),
      }),
    );

    await page.goto('/datasets');
    const badge = page.locator('[data-testid="jobspec-badge"]').first();
    await expect(badge).toBeVisible();
    await expect(badge).toHaveAttribute('data-jobspec-on-master', 'true');
    await expect(badge).toContainText('JobSpec on master');
  });
});
