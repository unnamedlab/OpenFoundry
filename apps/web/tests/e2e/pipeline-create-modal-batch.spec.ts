// FASE 2 — CreatePipelineModal happy path for BATCH.
//
// Walks the operator through:
//   1. Open /pipelines/new (modal opens automatically).
//   2. Step 1: pick the Batch type card.
//   3. Step 2: pick the demo project, type a name.
//   4. Step 3: confirm batch needs no extra config, hit Create.
//   5. Backend receives `pipeline_type: 'BATCH'` + `project_id`.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

type CreatePayload = {
  name: string;
  pipeline_type?: string;
  project_id?: string;
  external?: unknown;
  streaming?: unknown;
  incremental?: unknown;
};

test.describe('CreatePipelineModal — batch happy path', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('creates a batch pipeline through the 3-step wizard', async ({ page }) => {
    let createCalls = 0;
    let lastBody: CreatePayload | null = null;

    // The shared mock only handles GET on /api/v1/pipelines; we register a
    // POST handler that wins by being added later (Playwright matches the
    // most recently registered route first).
    await page.route('**/api/v1/pipelines', async (route) => {
      if (route.request().method() !== 'POST') return route.fallback();
      createCalls += 1;
      const body = JSON.parse(route.request().postData() ?? '{}') as CreatePayload;
      lastBody = body;
      await route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'pipeline-created-1',
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
          pipeline_type: body.pipeline_type ?? 'BATCH',
          lifecycle: 'DRAFT',
        }),
      });
    });

    await page.goto('/pipelines/new');

    await expect(page.getByRole('dialog', { name: 'Create new pipeline' })).toBeVisible();

    // Step 1 — type selection.
    await expect(page.getByTestId('cpm-type-grid')).toBeVisible();
    await expect(page.getByTestId('cpm-continue')).toBeDisabled();
    await page.getByTestId('cpm-type-batch').click();
    await expect(page.getByTestId('cpm-continue')).toBeEnabled();
    await page.getByTestId('cpm-continue').click();

    // Step 2 — project + name.
    await page.getByTestId('cpm-name').fill('Orders Pipeline');
    await expect(page.getByTestId('cpm-continue')).toBeDisabled();
    await page.getByTestId('cpm-project-list').waitFor({ state: 'visible' });
    await page.getByTestId('cpm-project-ops-readiness').click();
    await expect(page.getByTestId('cpm-continue')).toBeEnabled();
    await page.getByTestId('cpm-continue').click();

    // Step 3 — batch needs no extra config; Create button is enabled.
    await expect(page.getByTestId('cpm-create')).toBeEnabled();
    await page.getByTestId('cpm-create').click();

    await expect.poll(() => createCalls).toBe(1);
    const body = lastBody as CreatePayload | null;
    if (!body) throw new Error('expected pipeline create payload');
    expect(body.pipeline_type).toBe('BATCH');
    expect(body.name).toBe('Orders Pipeline');
    expect(body.project_id).toBe('project-1');
    expect(body.external).toBeUndefined();
    expect(body.streaming).toBeUndefined();
    expect(body.incremental).toBeUndefined();

    await expect(page).toHaveURL('/pipelines/pipeline-created-1/edit');
  });
});
