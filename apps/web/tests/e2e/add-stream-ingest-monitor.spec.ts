import { expect, test } from '@playwright/test';

/**
 * Bloque P4 — Add stream-ingest monitor flow.
 *
 * Mirrors the Foundry tutorial:
 *   1. Open Data Health.
 *   2. Create / pick a Monitoring View.
 *   3. Wizard: Streaming dataset → resource RID → Records ingested
 *      → 5min window, threshold 0 → save.
 *   4. The new rule shows up in the Manage Monitors table.
 *
 * Tolerant by design: when the backend is offline we skip rather
 * than fail CI.
 */

const VIEW_NAME = `e2e-monitor-${Date.now()}`;
const PROJECT_RID = `ri.compass.main.project.e2e-monitor-${Date.now()}`;

test.describe('Data Health — add stream ingest monitor', () => {
  test('5min window, threshold 0 — record-ingested rule round-trips', async ({
    page,
  }, testInfo) => {
    const monitoring = '/api/v1/monitoring';
    const probe = await page.request
      .get(`${monitoring}/monitoring-views`)
      .catch((e: unknown) => ({ failure: e }));
    if ('failure' in probe || !probe.ok()) {
      testInfo.skip(
        true,
        'monitoring-rules-service unreachable; skipping monitor smoke.',
      );
      return;
    }

    // 1. Seed a Monitoring View via the API so the wizard has
    //    something to attach to.
    const viewResp = await page.request.post(
      `${monitoring}/monitoring-views`,
      {
        data: {
          name: VIEW_NAME,
          description: 'Playwright add-monitor smoke',
          project_rid: PROJECT_RID,
        },
      },
    );
    expect(viewResp.ok()).toBeTruthy();
    const view = await viewResp.json();

    // 2. Visit Data Health and switch to Manage Monitors.
    await page.goto('/control-panel/data-health');
    await expect(page.getByTestId('data-health-page')).toBeVisible();
    await page.getByRole('button', { name: 'Manage Monitors' }).click();

    // 3. Open the wizard.
    await page.getByTestId('add-monitor-btn').click();
    await expect(page.getByTestId('add-monitor-wizard')).toBeVisible();

    // Step 1 — choose Streaming dataset.
    await page.getByText('Streaming dataset').click();
    await page.getByTestId('wizard-next').click();

    // Step 2 — resource RID.
    await page
      .getByTestId('wizard-resource-rid')
      .fill('ri.streams.main.stream.demo');
    await page.getByTestId('wizard-next').click();

    // Step 3 — Records ingested.
    await page.getByTestId('wizard-kind-INGEST_RECORDS').click();
    await page.getByTestId('wizard-next').click();

    // Step 4 — 5min window, comparator default LTE, threshold 0.
    // The default <select> already shows "5 minutes" / 300s and
    // comparator "≤", so we just have to fix the threshold.
    await page.getByTestId('wizard-threshold').fill('0');
    await page.getByTestId('wizard-next').click();

    // Step 5 — review and save.
    await page.getByTestId('wizard-save').click();

    // 4. The rule appears in the table for the active view.
    await expect(page.getByTestId('rules-table')).toContainText(
      'INGEST_RECORDS',
    );
    await expect(page.getByTestId('rules-table')).toContainText(
      'ri.streams.main.stream.demo',
    );

    // 5. Confirm via REST that the rule was persisted.
    const rules = await page.request.get(
      `${monitoring}/monitoring-views/${view.id}/rules`,
    );
    expect(rules.ok()).toBeTruthy();
    const body = await rules.json();
    const created = body.data.find(
      (r: { resource_rid: string; monitor_kind: string }) =>
        r.resource_rid === 'ri.streams.main.stream.demo' &&
        r.monitor_kind === 'INGEST_RECORDS',
    );
    expect(created).toBeTruthy();
    expect(created.threshold).toBe(0);
    expect(created.window_seconds).toBe(300);
  });
});
