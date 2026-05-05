// FASE 2 — CreatePipelineModal external branch.
//
// Verifies the source-system radio + source picker on step 3 for
// EXTERNAL pipelines, and that Create stays disabled until a source
// matching the selected system is picked.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

type CreatePayload = {
  name: string;
  pipeline_type?: string;
  external?: { source_system?: string; source_id?: string | null } | null;
};

test.describe('CreatePipelineModal — external requires a source', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('filters sources by selected system and submits the pick', async ({ page }) => {
    let lastBody: CreatePayload | null = null;

    await page.route('**/api/v1/data-connection/sources*', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: [
            {
              id: 'source-snow-1',
              name: 'sales-warehouse',
              connector_type: 'snowflake',
              worker: 'foundry',
              status: 'healthy',
              last_sync_at: null,
              created_at: '2026-04-01T00:00:00Z',
              updated_at: '2026-04-01T00:00:00Z',
            },
            {
              id: 'source-dbx-1',
              name: 'analytics-dbx',
              connector_type: 'databricks',
              worker: 'foundry',
              status: 'healthy',
              last_sync_at: null,
              created_at: '2026-04-01T00:00:00Z',
              updated_at: '2026-04-01T00:00:00Z',
            },
          ],
          total: 2,
          page: 1,
          per_page: 100,
        }),
      });
    });

    await page.route('**/api/v1/pipelines', async (route) => {
      if (route.request().method() !== 'POST') return route.fallback();
      const body = JSON.parse(route.request().postData() ?? '{}') as CreatePayload;
      lastBody = body;
      await route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'pipeline-ext-1',
          name: body.name,
          description: '',
          owner_id: 'user-1',
          dag: [],
          status: 'draft',
          schedule_config: { enabled: false, cron: null },
          retry_policy: { max_attempts: 1, retry_on_failure: false, allow_partial_reexecution: true },
          next_run_at: null,
          created_at: '2026-05-05T00:00:00Z',
          updated_at: '2026-05-05T00:00:00Z',
          pipeline_type: 'EXTERNAL',
          lifecycle: 'DRAFT',
        }),
      });
    });

    await page.goto('/pipelines/new');

    await page.getByTestId('cpm-type-external').click();
    await page.getByTestId('cpm-continue').click();

    await page.getByTestId('cpm-name').fill('Snowflake bridge');
    await page.getByTestId('cpm-project-ops-readiness').click();
    await page.getByTestId('cpm-continue').click();

    // Default system is databricks — source picker should show only the
    // databricks entry.
    await expect(page.getByTestId('cpm-source')).toBeVisible();
    await expect(page.getByTestId('cpm-create')).toBeDisabled();
    const dbxOptions = await page.getByTestId('cpm-source').locator('option').allTextContents();
    expect(dbxOptions).toContain('analytics-dbx');
    expect(dbxOptions).not.toContain('sales-warehouse');

    // Switch to snowflake — list filters to the warehouse entry only.
    await page.getByTestId('cpm-system-snowflake').check();
    const snowOptions = await page.getByTestId('cpm-source').locator('option').allTextContents();
    expect(snowOptions).toContain('sales-warehouse');
    expect(snowOptions).not.toContain('analytics-dbx');

    await page.getByTestId('cpm-source').selectOption('source-snow-1');
    await expect(page.getByTestId('cpm-create')).toBeEnabled();
    await page.getByTestId('cpm-create').click();

    await expect.poll(() => (lastBody as CreatePayload | null)?.pipeline_type).toBe('EXTERNAL');
    const body = lastBody as CreatePayload | null;
    if (!body) throw new Error('expected pipeline create payload');
    expect(body.external?.source_system).toBe('snowflake');
    expect(body.external?.source_id).toBe('source-snow-1');
  });
});
