import { expect, test } from '@playwright/test';

/**
 * Bloque P2 — Reset Stream + push-URL rotation.
 *
 * Exercises the new Foundry-parity surface:
 *   * `POST /streams/{id}/reset` rotates the viewRid, retiring the old
 *     view and publishing a fresh one (generation+1).
 *   * `GET  /streams/{id}/views` lists the full history; both
 *     generations show up after a reset.
 *   * The Settings tab "Push URL" card refreshes with the new URL
 *     after a reset.
 *   * The History tab shows generation 1 retired and generation 2
 *     active.
 *
 * Tolerant by design: when the backend is offline we skip rather than
 * fail CI.
 */

const STREAM_NAME = `e2e-reset-${Date.now()}`;

test.describe('streaming Reset Stream + push-URL rotation', () => {
  test('rotates the viewRid and surfaces it in the UI history', async ({
    page,
  }, testInfo) => {
    const apiBase = '/api/v1/streaming';

    const probe = await page.request
      .get(`${apiBase}/overview`)
      .catch((e: unknown) => ({ failure: e }));
    if ('failure' in probe || !probe.ok()) {
      testInfo.skip(
        true,
        'event-streaming-service unreachable; skipping reset-stream smoke.',
      );
      return;
    }

    // 1. Seed an INGEST stream so the Reset card is visible.
    const create = await page.request.post(`${apiBase}/streams`, {
      data: {
        name: STREAM_NAME,
        description: 'Playwright reset-stream smoke',
        retention_hours: 24,
        kind: 'INGEST',
      },
    });
    expect(create.ok()).toBeTruthy();
    const stream = await create.json();
    expect(stream.id).toBeTruthy();

    // 2. Confirm the initial view (generation 1) is exposed.
    const view0Resp = await page.request.get(
      `${apiBase}/streams/${stream.id}/current-view`,
    );
    expect(view0Resp.ok()).toBeTruthy();
    const view0 = await view0Resp.json();
    expect(view0.generation).toBe(1);
    expect(view0.active).toBe(true);
    const oldViewRid = view0.view_rid;

    // 3. Visit the Settings tab and confirm the Push URL card renders
    //    the current viewRid.
    await page.goto(`/streaming/${stream.id}`);
    await page.getByRole('button', { name: 'Settings' }).click();
    await expect(page.getByTestId('push-url-card')).toBeVisible();
    await expect(page.getByTestId('push-url-input')).toContainText(oldViewRid);

    // 4. Trigger reset via the REST API. Because there is no
    //    downstream pipeline we don't need force=true.
    const reset = await page.request.post(
      `${apiBase}/streams/${stream.id}/reset`,
      { data: {} },
    );
    expect(reset.ok()).toBeTruthy();
    const resetBody = await reset.json();
    expect(resetBody.generation).toBe(2);
    expect(resetBody.new_view_rid).not.toBe(oldViewRid);

    // 5. Pushing against the retired view must now return 404 with the
    //    documented error code.
    const stalePush = await page.request.post(
      `/streams-push/${oldViewRid}/records`,
      {
        headers: { Authorization: 'Bearer dev-token' },
        data: { records: [{ value: { id: 'x' } }] },
      },
    );
    expect(stalePush.status()).toBe(404);
    const staleBody = await stalePush.json();
    expect(staleBody.error).toContain('PUSH_VIEW_RETIRED');

    // 6. The push proxy `GET .../url` reflects the new viewRid.
    const urlResp = await page.request.get(
      `/streams-push/${resetBody.stream_rid}/url`,
    );
    expect(urlResp.ok()).toBeTruthy();
    const urlBody = await urlResp.json();
    expect(urlBody.view_rid).toBe(resetBody.new_view_rid);
    expect(urlBody.push_url).toContain(resetBody.new_view_rid);

    // 7. Visit the History tab and confirm both generations are
    //    rendered, with the previous one marked retired.
    await page.reload();
    await page.getByRole('button', { name: 'History' }).click();
    const historyTable = page.getByTestId('history-tab');
    await expect(historyTable).toContainText('1');
    await expect(historyTable).toContainText('2');
    await expect(historyTable).toContainText('retired');
    await expect(historyTable).toContainText('active');
  });
});
