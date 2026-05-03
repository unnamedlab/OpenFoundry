import { expect, test } from '@playwright/test';

/**
 * Bloque G — Stream Settings tab.
 *
 * Exercises the new UI + REST surface introduced for Foundry parity:
 *   * the Settings tab renders Stream type / Partitions / Consistency
 *     cards from `GET /streams/{id}/config`;
 *   * the badge in the page header reflects the new
 *     `stream_type` / `pipeline_consistency` fields;
 *   * the ingest consistency control is locked to AT_LEAST_ONCE per
 *     Foundry docs;
 *   * a save round-trips through `PUT /streams/{id}/config`.
 *
 * Tolerant by design: when the backend is offline we skip rather than
 * fail CI.
 */

const STREAM_NAME = `e2e-settings-${Date.now()}`;

test.describe('streaming Settings tab', () => {
  test('renders Foundry stream-config controls and round-trips a save', async ({
    page,
  }, testInfo) => {
    const apiBase = '/api/v1/streaming';

    const probe = await page.request
      .get(`${apiBase}/overview`)
      .catch((e: unknown) => ({ failure: e }));
    if ('failure' in probe || !probe.ok()) {
      testInfo.skip(
        true,
        'event-streaming-service unreachable; skipping settings-tab smoke.',
      );
      return;
    }

    // 1. Seed a stream so the Settings tab has something to render.
    const create = await page.request.post(`${apiBase}/streams`, {
      data: {
        name: STREAM_NAME,
        description: 'Playwright Settings tab smoke',
        retention_hours: 24,
      },
    });
    expect(create.ok()).toBeTruthy();
    const stream = await create.json();
    expect(stream.id).toBeTruthy();

    // 2. Visit the stream detail page and switch to Settings.
    await page.goto(`/streaming/${stream.id}`);
    await expect(page.getByTestId('stream-type-badge')).toHaveText(/STANDARD/);
    await expect(
      page.getByTestId('stream-pipeline-consistency-badge'),
    ).toContainText('AT_LEAST_ONCE');

    await page.getByRole('button', { name: 'Settings' }).click();
    const settingsRoot = page.getByTestId('stream-settings');
    await expect(settingsRoot).toBeVisible();

    // 3. The ingest row is locked.
    await expect(settingsRoot.getByText(/AT_LEAST_ONCE \(locked\)/)).toBeVisible();

    // 4. Flip the pipeline consistency to EXACTLY_ONCE and save.
    await page
      .getByTestId('pipeline-consistency-select')
      .selectOption('EXACTLY_ONCE');

    // 5. Slide partitions up.
    const slider = page.getByTestId('partitions-slider');
    await slider.evaluate((node, value) => {
      const input = node as HTMLInputElement;
      input.value = String(value);
      input.dispatchEvent(new Event('input', { bubbles: true }));
      input.dispatchEvent(new Event('change', { bubbles: true }));
    }, 12);

    await page.getByTestId('stream-settings-save').click();
    await expect(page.getByTestId('stream-settings-saved')).toBeVisible();

    // 6. Verify the persisted config via the REST surface.
    const configResp = await page.request.get(
      `${apiBase}/streams/${stream.id}/config`,
    );
    expect(configResp.ok()).toBeTruthy();
    const config = await configResp.json();
    expect(config.pipeline_consistency).toBe('EXACTLY_ONCE');
    expect(config.partitions).toBeGreaterThanOrEqual(12);
  });

  test('rejects ingest_consistency = EXACTLY_ONCE with a Foundry-coded error', async ({
    page,
  }, testInfo) => {
    const apiBase = '/api/v1/streaming';
    const probe = await page.request
      .get(`${apiBase}/overview`)
      .catch((e: unknown) => ({ failure: e }));
    if ('failure' in probe || !probe.ok()) {
      testInfo.skip(true, 'event-streaming-service unreachable; skipping.');
      return;
    }

    const create = await page.request.post(`${apiBase}/streams`, {
      data: {
        name: `e2e-ingest-rule-${Date.now()}`,
        retention_hours: 24,
      },
    });
    expect(create.ok()).toBeTruthy();
    const stream = await create.json();

    const reject = await page.request.put(
      `${apiBase}/streams/${stream.id}/config`,
      {
        data: { ingest_consistency: 'EXACTLY_ONCE' },
      },
    );
    expect(reject.status()).toBe(422);
    const body = await reject.json();
    expect(body.error).toContain('STREAM_INGEST_EXACTLY_ONCE_NOT_SUPPORTED');
  });
});
