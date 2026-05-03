// P3 — Files tab e2e.
//
// The new tab calls `GET /v1/datasets/{rid}/files` (Foundry "Backing
// filesystem" listing). This spec mocks the response to cover both
// active and soft-deleted rows and asserts the download button targets
// the 302 endpoint exposed by dataset-versioning-service.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const DATASET_ID = 'dataset-1';

const filesFixture = {
  view_id: '00000000-0000-0000-0000-0000000000aa',
  branch: 'main',
  total: 3,
  files: [
    {
      id: '11111111-1111-1111-1111-111111111111',
      dataset_id: DATASET_ID,
      transaction_id: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
      logical_path: 'data/part-0.parquet',
      physical_uri: 's3://bucket/foundry/datasets/dataset-1/data/part-0.parquet',
      size_bytes: 1024,
      sha256: 'abc123def456',
      created_at: '2026-05-03T12:00:00Z',
      modified_at: '2026-05-03T12:00:00Z',
      status: 'active',
    },
    {
      id: '22222222-2222-2222-2222-222222222222',
      dataset_id: DATASET_ID,
      transaction_id: 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb',
      logical_path: 'data/part-1.parquet',
      physical_uri: 's3://bucket/foundry/datasets/dataset-1/data/part-1.parquet',
      size_bytes: 2048,
      sha256: 'fedcba98',
      created_at: '2026-05-03T12:05:00Z',
      modified_at: '2026-05-03T12:05:00Z',
      status: 'active',
    },
    {
      id: '33333333-3333-3333-3333-333333333333',
      dataset_id: DATASET_ID,
      transaction_id: 'cccccccc-cccc-cccc-cccc-cccccccccccc',
      logical_path: 'data/old-part.parquet',
      physical_uri: 's3://bucket/foundry/datasets/dataset-1/data/old-part.parquet',
      size_bytes: 512,
      sha256: null,
      created_at: '2026-05-03T11:00:00Z',
      modified_at: '2026-05-03T11:30:00Z',
      status: 'deleted',
    },
  ],
};

test.describe('dataset files tab', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);

    await page.route(`**/api/v1/datasets/${DATASET_ID}/transactions`, (route) =>
      route.fulfill({ contentType: 'application/json', body: '[]' }),
    );
    await page.route(`**/api/v1/datasets/${DATASET_ID}/branches`, (route) =>
      route.fulfill({ contentType: 'application/json', body: '[]' }),
    );
    await page.route(`**/api/v1/datasets/${DATASET_ID}/files**`, (route) => {
      route.fulfill({ contentType: 'application/json', body: JSON.stringify(filesFixture) });
    });
  });

  test('renders active and soft-deleted files with the right badges', async ({ page }) => {
    await page.goto(`/datasets/${DATASET_ID}`);
    await page.getByRole('button', { name: /^Files$/ }).click();

    const tab = page.locator('[data-component="files-tab"]');
    await expect(tab).toBeVisible();

    const rows = page.locator('[data-testid="file-row"]');
    await expect(rows).toHaveCount(3);

    const activeRow = rows.filter({ hasText: 'data/part-0.parquet' });
    await expect(activeRow).toHaveAttribute('data-file-status', 'active');
    await expect(activeRow.getByText('active', { exact: true })).toBeVisible();

    const deletedRow = rows.filter({ hasText: 'data/old-part.parquet' });
    await expect(deletedRow).toHaveAttribute('data-file-status', 'deleted');
    await expect(deletedRow.getByText('deleted in current view')).toBeVisible();

    // Download button on active rows targets the 302 endpoint.
    const downloadLink = activeRow.getByTestId('file-download');
    await expect(downloadLink).toHaveAttribute(
      'href',
      `/api/v1/datasets/${DATASET_ID}/files/11111111-1111-1111-1111-111111111111/download`,
    );
    // Soft-deleted rows have no download — em-dash placeholder.
    await expect(deletedRow.getByTestId('file-download')).toHaveCount(0);
  });

  test('prefix filter narrows the listing', async ({ page }) => {
    let lastRequestUrl = '';
    await page.route(`**/api/v1/datasets/${DATASET_ID}/files**`, (route) => {
      lastRequestUrl = route.request().url();
      route.fulfill({ contentType: 'application/json', body: JSON.stringify(filesFixture) });
    });

    await page.goto(`/datasets/${DATASET_ID}`);
    await page.getByRole('button', { name: /^Files$/ }).click();

    await page.getByTestId('files-prefix-filter').fill('data/part');
    await page.waitForTimeout(50);
    await expect.poll(() => lastRequestUrl).toContain('prefix=data%2Fpart');
  });
});
