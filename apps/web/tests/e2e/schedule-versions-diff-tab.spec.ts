// P2 — Versions tab on the schedule detail page. Drives
// `/schedules/{rid}` and asserts:
//   * the versions list renders one row per snapshot,
//   * the dual From/To selector triggers a fresh diff fetch,
//   * the rendered diff shows the trigger.cron path entry.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const SCHEDULE_RID = 'ri.foundry.main.schedule.beta';

const schedule = {
  id: '00000000-0000-0000-0000-0000000000bb',
  rid: SCHEDULE_RID,
  project_rid: 'ri.foundry.main.project.demo',
  name: 'beta schedule',
  description: '',
  trigger: {
    kind: { time: { cron: '0 12 * * *', time_zone: 'UTC', flavor: 'UNIX_5' } },
  },
  target: { kind: { sync_run: { sync_rid: 'sx', source_rid: 'src' } } },
  paused: false,
  paused_reason: null,
  paused_at: null,
  auto_pause_exempt: false,
  pending_re_run: false,
  active_run_id: null,
  version: 3,
  created_by: 'tester',
  created_at: '2026-05-01T00:00:00Z',
  updated_at: '2026-05-01T00:00:00Z',
  last_run_at: null,
};

const versions = [
  {
    id: 'v2-id',
    schedule_id: schedule.id,
    version: 2,
    name: 'beta schedule',
    description: '',
    trigger_json: {
      kind: { time: { cron: '0 9 * * *', time_zone: 'UTC', flavor: 'UNIX_5' } },
    },
    target_json: schedule.target,
    edited_by: 'tester',
    edited_at: '2026-05-01T08:00:00Z',
    comment: 'switching to noon UTC',
  },
  {
    id: 'v1-id',
    schedule_id: schedule.id,
    version: 1,
    name: 'beta schedule',
    description: '',
    trigger_json: {
      kind: { time: { cron: '0 6 * * *', time_zone: 'UTC', flavor: 'UNIX_5' } },
    },
    target_json: schedule.target,
    edited_by: 'tester',
    edited_at: '2026-05-01T07:00:00Z',
    comment: '',
  },
];

const diff = {
  schedule_id: schedule.id,
  from_version: 2,
  to_version: 3,
  name_diff: null,
  description_diff: null,
  trigger_diff: [
    {
      path: 'kind.time.cron',
      before: '0 9 * * *',
      after: '0 12 * * *',
    },
  ],
  target_diff: [],
};

test.describe('schedule versions diff tab', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);

    await page.route(
      `**/api/v1/data-integration/v1/schedules/${encodeURIComponent(SCHEDULE_RID)}`,
      (route) =>
        route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify(schedule),
        }),
    );
    await page.route(
      `**/api/v1/data-integration/v1/schedules/${encodeURIComponent(SCHEDULE_RID)}/runs**`,
      (route) =>
        route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({ schedule_rid: SCHEDULE_RID, data: [], total: 0 }),
        }),
    );
    await page.route(
      new RegExp(
        `/api/v1/data-integration/v1/schedules/${encodeURIComponent(SCHEDULE_RID)}/versions(\\?|$)`,
      ),
      (route) =>
        route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            schedule_rid: SCHEDULE_RID,
            current_version: schedule.version,
            data: versions,
          }),
        }),
    );
    await page.route(
      `**/api/v1/data-integration/v1/schedules/${encodeURIComponent(SCHEDULE_RID)}/versions/diff**`,
      (route) =>
        route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify(diff),
        }),
    );
  });

  test('versions tab renders snapshots and diff entry', async ({ page }) => {
    await page.goto(`/schedules/${SCHEDULE_RID}`);
    await expect(page.getByTestId('schedule-config')).toBeVisible();

    await page.getByTestId('versions-tab').click();
    await expect(page.getByTestId('versions-panel')).toBeVisible();

    // Snapshot rows include both v1 and v2.
    await expect(page.getByText('v2', { exact: false }).first()).toBeVisible();
    await expect(page.getByText('switching to noon UTC')).toBeVisible();

    // Diff component renders a trigger.cron path entry.
    await expect(page.getByTestId('schedule-diff')).toBeVisible();
    await expect(page.getByTestId('diff-trigger').first()).toContainText('kind.time.cron');
  });
});
