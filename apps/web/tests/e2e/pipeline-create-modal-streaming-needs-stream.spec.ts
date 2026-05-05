// FASE 2 — CreatePipelineModal streaming branch.
//
// Asserts that picking STREAMING surfaces the input-stream picker on
// step 3 and that the Create button stays disabled until the operator
// selects a stream.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

type CreatePayload = {
  name: string;
  pipeline_type?: string;
  streaming?: { input_stream_id?: string; parallelism?: number } | null;
};

test.describe('CreatePipelineModal — streaming requires input stream', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('blocks Create until an input stream is selected', async ({ page }) => {
    let lastBody: CreatePayload | null = null;

    await page.route('**/api/v1/streaming/streams', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: [
            {
              id: 'stream-001',
              name: 'orders-cdc',
              description: 'Order CDC stream',
              status: 'active',
              schema: { fields: [] },
              source_binding: {},
              retention_hours: 24,
              partitions: 1,
              consistency_guarantee: 'AT_LEAST_ONCE',
              stream_profile: { id: 'p', name: 'default' },
              stream_type: 'records',
              compression: false,
              ingest_consistency: 'AT_LEAST_ONCE',
              pipeline_consistency: 'AT_LEAST_ONCE',
              checkpoint_interval_ms: 5000,
              kind: 'unbounded',
              created_at: '2026-05-05T00:00:00Z',
              updated_at: '2026-05-05T00:00:00Z',
            },
          ],
          total: 1,
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
          id: 'pipeline-stream-1',
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
          pipeline_type: 'STREAMING',
          lifecycle: 'DRAFT',
        }),
      });
    });

    await page.goto('/pipelines/new');

    await page.getByTestId('cpm-type-streaming').click();
    await page.getByTestId('cpm-continue').click();

    await page.getByTestId('cpm-name').fill('Orders Streaming');
    await page.getByTestId('cpm-project-ops-readiness').click();
    await page.getByTestId('cpm-continue').click();

    // The step-3 stream picker becomes visible once streams resolve.
    await expect(page.getByTestId('cpm-stream')).toBeVisible();
    await expect(page.getByTestId('cpm-create')).toBeDisabled();

    await page.getByTestId('cpm-stream').selectOption('stream-001');
    await expect(page.getByTestId('cpm-create')).toBeEnabled();
    await page.getByTestId('cpm-create').click();

    await expect.poll(() => (lastBody as CreatePayload | null)?.pipeline_type).toBe('STREAMING');
    const body = lastBody as CreatePayload | null;
    if (!body) throw new Error('expected pipeline create payload');
    expect(body.streaming?.input_stream_id).toBe('stream-001');
    expect(body.streaming?.parallelism ?? 0).toBeGreaterThanOrEqual(1);
  });
});
