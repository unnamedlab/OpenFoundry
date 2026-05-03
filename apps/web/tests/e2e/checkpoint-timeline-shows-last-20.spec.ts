import { expect, test } from '@playwright/test';

/**
 * Bloque P4 — Job Details / "Last checkpoints" card.
 *
 * Exercises the new tab + the `?last=N` query support added in P4.
 * We seed checkpoints by triggering them via the existing
 * `POST /streaming/topologies/{id}/checkpoints` endpoint and then
 * confirm the table renders them. Tolerant on backend absence.
 */

test.describe('Job Details — last 20 checkpoints', () => {
  test('renders the last 20 checkpoints with status badges', async ({
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

    // 1. Seed a stream + topology + a few checkpoints. The dev seed
    //    migration provides at least one stream/topology pair.
    const streams = await page.request.get(`${apiBase}/streams`);
    expect(streams.ok()).toBeTruthy();
    const streamsBody = await streams.json();
    const stream = streamsBody.data[0];
    if (!stream) {
      testInfo.skip(true, 'no streams in dev seed; skipping.');
      return;
    }

    const tops = await page.request.get(`${apiBase}/topologies`);
    expect(tops.ok()).toBeTruthy();
    const topsBody = await tops.json();
    const topology = topsBody.data.find((t: { source_stream_ids: string[] }) =>
      t.source_stream_ids.includes(stream.id),
    );
    if (!topology) {
      testInfo.skip(true, 'no topology consumes the seed stream; skipping.');
      return;
    }

    // 2. Trigger 3 checkpoints — enough to populate the table.
    for (let i = 0; i < 3; i += 1) {
      await page.request.post(
        `${apiBase}/topologies/${topology.id}/checkpoints`,
        { data: { trigger: 'manual' } },
      );
    }

    // 3. Visit the streaming detail page → Job Details.
    await page.goto(`/streaming/${stream.id}`);
    await page.getByRole('button', { name: 'Job Details' }).click();
    await expect(page.getByTestId('job-details')).toBeVisible();
    await expect(page.getByTestId('last-checkpoints-card')).toBeVisible();

    // The auto-refresh interval is 10s; give the first tick a moment.
    await page.waitForTimeout(1500);

    // 4. The table should have at least one row (and ≤ 20).
    const rows = await page
      .getByTestId('checkpoints-tbody')
      .locator('tr')
      .count();
    expect(rows).toBeGreaterThanOrEqual(1);
    expect(rows).toBeLessThanOrEqual(20);

    // 5. Confirm the REST `?last=N` knob caps at the request.
    const list = await page.request.get(
      `${apiBase}/topologies/${topology.id}/checkpoints?last=5`,
    );
    expect(list.ok()).toBeTruthy();
    const list5 = await list.json();
    expect(list5.data.length).toBeLessThanOrEqual(5);
  });
});
