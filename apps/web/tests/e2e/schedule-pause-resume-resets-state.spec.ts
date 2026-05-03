// P2 — Pause / Resume on a schedule resets observed state and surfaces
// the warning banner when the row is auto-paused. Drives the
// `/schedules/{rid}` route (which delegates to ScheduleConfig in
// scheduleRid mode) end-to-end with the pipeline-schedule-service
// REST surface mocked at `/api/v1/data-integration/v1/schedules/...`.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const SCHEDULE_RID = 'ri.foundry.main.schedule.alpha';

const baseSchedule = {
  id: '00000000-0000-0000-0000-0000000000aa',
  rid: SCHEDULE_RID,
  project_rid: 'ri.foundry.main.project.demo',
  name: 'demo schedule',
  description: '',
  trigger: {
    kind: { time: { cron: '0 9 * * *', time_zone: 'UTC', flavor: 'UNIX_5' } },
  },
  target: {
    kind: {
      pipeline_build: {
        pipeline_rid: 'ri.foundry.main.pipeline.demo',
        build_branch: 'master',
      },
    },
  },
  paused: false,
  paused_reason: null,
  paused_at: null,
  auto_pause_exempt: false,
  pending_re_run: false,
  active_run_id: null,
  version: 4,
  created_by: 'tester',
  created_at: '2026-05-01T00:00:00Z',
  updated_at: '2026-05-01T00:00:00Z',
  last_run_at: null,
};

test.describe('schedule pause / resume', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('pause confirmation resets observed state and flips the resume button', async ({
    page,
  }) => {
    let scheduleState = { ...baseSchedule };

    await page.route(
      `**/api/v1/data-integration/v1/schedules/${encodeURIComponent(SCHEDULE_RID)}`,
      (route) =>
        route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify(scheduleState),
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
      `**/api/v1/data-integration/v1/schedules/${encodeURIComponent(SCHEDULE_RID)}/versions**`,
      (route) =>
        route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            schedule_rid: SCHEDULE_RID,
            current_version: scheduleState.version,
            data: [],
          }),
        }),
    );
    await page.route(
      `**/api/v1/data-integration/v1/schedules/${encodeURIComponent(SCHEDULE_RID)}:pause`,
      (route) => {
        scheduleState = {
          ...scheduleState,
          paused: true,
          paused_reason: 'MANUAL',
          paused_at: '2026-05-02T00:00:00Z',
        };
        return route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify(scheduleState),
        });
      },
    );
    await page.route(
      `**/api/v1/data-integration/v1/schedules/${encodeURIComponent(SCHEDULE_RID)}:resume`,
      (route) => {
        scheduleState = { ...scheduleState, paused: false, paused_reason: null, paused_at: null };
        return route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify(scheduleState),
        });
      },
    );

    page.on('dialog', (dialog) => dialog.accept());

    await page.goto(`/schedules/${SCHEDULE_RID}`);
    await expect(page.getByTestId('schedule-config')).toBeVisible();
    await expect(page.getByTestId('pause-button')).toBeVisible();

    await page.getByTestId('pause-button').click();
    await expect(page.getByTestId('resume-button')).toBeVisible();

    await page.getByTestId('resume-button').click();
    await expect(page.getByTestId('pause-button')).toBeVisible();
  });

  test('auto-paused banner surfaces with View failures + Resume CTAs', async ({ page }) => {
    const autoPausedSchedule = {
      ...baseSchedule,
      paused: true,
      paused_reason: 'AUTO_PAUSED_AFTER_FAILURES',
      paused_at: '2026-05-02T00:00:00Z',
    };

    await page.route(
      `**/api/v1/data-integration/v1/schedules/${encodeURIComponent(SCHEDULE_RID)}`,
      (route) =>
        route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify(autoPausedSchedule),
        }),
    );
    await page.route(
      `**/api/v1/data-integration/v1/schedules/${encodeURIComponent(SCHEDULE_RID)}/runs**`,
      (route) =>
        route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            schedule_rid: SCHEDULE_RID,
            data: [
              {
                id: 'r1',
                rid: 'ri.foundry.main.schedule_run.r1',
                schedule_id: autoPausedSchedule.id,
                outcome: 'FAILED',
                build_rid: null,
                failure_reason: 'build-service 500: boom',
                triggered_at: '2026-05-01T09:00:00Z',
                finished_at: '2026-05-01T09:00:01Z',
                trigger_snapshot: { kind: 'cron' },
                schedule_version: 4,
              },
            ],
            total: 1,
          }),
        }),
    );
    await page.route(
      `**/api/v1/data-integration/v1/schedules/${encodeURIComponent(SCHEDULE_RID)}/versions**`,
      (route) =>
        route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            schedule_rid: SCHEDULE_RID,
            current_version: 4,
            data: [],
          }),
        }),
    );

    await page.goto(`/schedules/${SCHEDULE_RID}`);
    await expect(page.getByTestId('auto-paused-banner')).toBeVisible();
    await expect(page.getByTestId('auto-paused-view-failures')).toBeVisible();
    await expect(page.getByTestId('auto-paused-resume')).toBeVisible();

    await page.getByTestId('auto-paused-view-failures').click();
    await expect(page.getByTestId('run-history')).toBeVisible();
    await expect(page.getByTestId('run-badge-failed')).toBeVisible();
  });
});
