// D1.1.5 P4 — LiveLogViewer renders the Foundry color palette by
// level. Asserts the inline `style` attribute (the component sets
// `color: <#hex>` directly so the assertion is robust to CSS
// changes) for the four documented buckets.

import { expect, test } from '@playwright/test';
import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const JOB_RID = 'ri.foundry.main.job.18f0color';

const PALETTE = {
  INFO: '#3B82F6',
  WARN: '#F59E0B',
  ERROR: '#EF4444',
  DEBUG: '#6B7280'
} as const;

function eventStreamFixture(): string {
  return [
    'event: heartbeat',
    'data: {"phase":"initializing","delay_remaining_seconds":0}',
    '',
    'event: log',
    'data: {"sequence":1,"ts":"2026-05-03T12:00:00Z","level":"INFO","message":"info-msg"}',
    '',
    'event: log',
    'data: {"sequence":2,"ts":"2026-05-03T12:00:01Z","level":"WARN","message":"warn-msg"}',
    '',
    'event: log',
    'data: {"sequence":3,"ts":"2026-05-03T12:00:02Z","level":"ERROR","message":"err-msg"}',
    '',
    'event: log',
    'data: {"sequence":4,"ts":"2026-05-03T12:00:03Z","level":"DEBUG","message":"dbg-msg"}',
    '',
  ].join('\n');
}

test.describe('live logs — color coding', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('each level row carries the documented Foundry color', async ({ page }) => {
    await page.route(`**/v1/jobs/${JOB_RID}/logs/stream*`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'text/event-stream',
        body: eventStreamFixture(),
      });
    });

    await page.goto('/pipelines/p-1/runs/r-color');
    await expect(page.getByTestId('live-log-viewer')).toBeVisible();

    for (const [level, hex] of Object.entries(PALETTE)) {
      const row = page.getByTestId(
        `live-logs-row-${level === 'INFO' ? 1 : level === 'WARN' ? 2 : level === 'ERROR' ? 3 : 4}`
      );
      const styleAttr = await row.locator('.level').getAttribute('style');
      expect(styleAttr ?? '').toMatch(new RegExp(`color:\\s*${hex.toLowerCase()}`, 'i'));
    }

    // The persistent banner (Foundry doc: "Live logs are streamed in
    // real-time. Time range filters do not apply.") is always present.
    await expect(page.getByTestId('live-logs-banner')).toContainText(
      'Live logs are streamed in real-time'
    );
  });
});
