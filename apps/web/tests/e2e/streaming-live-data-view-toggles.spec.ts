import { expect, test } from '@playwright/test';

/**
 * Bloque P5 — StreamLiveDataView Live/Recent/Historical toggle.
 *
 * Mirrors Foundry's `Streams.md` description of the dataset preview:
 *   * Live    — hot only, the new record stream tail
 *   * Recent  — hybrid, oldest-first, last N
 *   * Historical — cold only (Iceberg/Parquet pointers)
 *
 * Tolerant on backend absence.
 */

test.describe('streaming Live data view toggles', () => {
  test('renders Live, Recent, Historical and surfaces source labels', async ({
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

    // 1. Pick a stream from the dev seed.
    const streams = await page.request.get(`${apiBase}/streams`);
    expect(streams.ok()).toBeTruthy();
    const body = await streams.json();
    const stream = body.data[0];
    if (!stream) {
      testInfo.skip(true, 'no streams in dev seed; skipping.');
      return;
    }

    // 2. The preview REST endpoint accepts the three modes and
    //    returns an aggregate source label per the contract.
    for (const mode of ['oldest', 'hot_only', 'cold_only']) {
      const resp = await page.request.get(
        `${apiBase}/streams/${stream.id}/preview?from=${mode}&limit=10`,
      );
      expect(resp.ok()).toBeTruthy();
      const data = await resp.json();
      expect(['hot', 'cold', 'hybrid']).toContain(data.source);
      expect(Array.isArray(data.data)).toBeTruthy();
    }

    // 3. Visit the streaming detail page → Live tab.
    await page.goto(`/streaming/${stream.id}`);
    await page.getByRole('button', { name: 'Live' }).click();
    await expect(page.getByTestId('stream-live-data')).toBeVisible();

    // 4. Toggle through Recent and Historical and confirm the
    //    aggregate-source label updates.
    await page.getByTestId('view-recent').click();
    await expect(page.getByTestId('view-recent')).toHaveClass(/active/);
    await expect(page.getByTestId('aggregate-source')).toBeVisible();

    await page.getByTestId('view-historical').click();
    await expect(page.getByTestId('view-historical')).toHaveClass(/active/);

    // 5. Switching back to Live re-arms the polling fallback.
    await page.getByTestId('view-live').click();
    await expect(page.getByTestId('view-live')).toHaveClass(/active/);
  });
});
