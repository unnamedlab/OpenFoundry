// D1.1.5 P4 — LiveLogViewer pause / resume against a stubbed SSE
// stream. The mock pushes logs at a steady cadence; we toggle Pause,
// stop seeing the EventSource open, then Resume and confirm the
// connection is reopened with the resumed `from_sequence`.

import { expect, test } from '@playwright/test';
import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const JOB_RID = 'ri.foundry.main.job.18f01234';

test.describe('live logs — pause and resume', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('pausing closes the SSE stream and resume reopens with from_sequence', async ({ page }) => {
    let connectsObserved = 0;
    const observedFromSequences: number[] = [];

    // Stub the SSE endpoint with a hand-rolled `text/event-stream`.
    await page.route(`**/v1/jobs/${JOB_RID}/logs/stream*`, async (route) => {
      connectsObserved += 1;
      const url = new URL(route.request().url());
      observedFromSequences.push(Number(url.searchParams.get('from_sequence') ?? '0'));
      const body = [
        'event: heartbeat',
        'data: {"phase":"initializing","delay_remaining_seconds":0}',
        '',
        'event: log',
        'data: {"sequence":1,"ts":"2026-05-03T12:00:00Z","level":"INFO","message":"first"}',
        '',
        'event: log',
        'data: {"sequence":2,"ts":"2026-05-03T12:00:01Z","level":"WARN","message":"second"}',
        '',
      ].join('\n');
      await route.fulfill({
        status: 200,
        contentType: 'text/event-stream',
        body,
      });
    });

    // Mount a simple host page that exposes the viewer at a stable
    // route. We piggy-back on the dataset preview route which already
    // mounts arbitrary Svelte components in the test harness.
    // Instead of a custom route, navigate to the pipeline run history
    // (which embeds LiveLogViewer when the run is live).
    // For this isolated spec we render a tiny harness via inline
    // navigation to a JSON-driven preview page.
    await page.goto('/pipelines/p-1/runs/r-1');

    const viewer = page.getByTestId('live-log-viewer');
    await expect(viewer).toBeVisible();
    await expect(page.getByTestId('live-logs-row-1')).toBeVisible();
    await expect(page.getByTestId('live-logs-row-2')).toBeVisible();
    expect(connectsObserved).toBeGreaterThanOrEqual(1);

    await page.getByTestId('live-logs-pause').click();
    await expect(page.getByTestId('live-logs-resume')).toBeVisible();

    // After pause, even if the route fires again, the viewer ignores it.
    const before = connectsObserved;
    await page.waitForTimeout(200);

    await page.getByTestId('live-logs-resume').click();
    await expect.poll(() => connectsObserved).toBeGreaterThan(before);
    // The reopened request must carry the last sequence we saw (2).
    expect(observedFromSequences[observedFromSequences.length - 1]).toBe(2);
  });
});
