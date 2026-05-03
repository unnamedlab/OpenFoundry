// P3 — Convert a USER schedule to PROJECT_SCOPED via the
// `/schedules/{rid}` page.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const SCHEDULE_RID = 'ri.foundry.main.schedule.gamma';

const userScopeSchedule = {
  id: '00000000-0000-0000-0000-0000000000cc',
  rid: SCHEDULE_RID,
  project_rid: 'ri.foundry.main.project.demo',
  name: 'gamma schedule',
  description: '',
  trigger: {
    kind: { time: { cron: '0 9 * * *', time_zone: 'UTC', flavor: 'UNIX_5' } },
  },
  target: {
    kind: {
      pipeline_build: {
        pipeline_rid: 'ri.foundry.main.pipeline.alpha',
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
  version: 1,
  created_by: '00000000-0000-0000-0000-000000000111',
  created_at: '2026-05-01T00:00:00Z',
  updated_at: '2026-05-01T00:00:00Z',
  last_run_at: null,
  scope_kind: 'USER' as const,
  project_scope_rids: [],
  run_as_user_id: '00000000-0000-0000-0000-000000000111',
  service_principal_id: null,
};

test.describe('schedule convert-to-project-scoped', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('convert flow flips scope_kind and surfaces the service principal badge', async ({
    page,
  }) => {
    let scheduleState = { ...userScopeSchedule };

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
      `**/api/v1/data-integration/v1/schedules/${encodeURIComponent(SCHEDULE_RID)}:convert-to-project-scope`,
      (route) => {
        scheduleState = {
          ...scheduleState,
          scope_kind: 'PROJECT_SCOPED',
          project_scope_rids: ['ri.foundry.main.project.alpha'],
          run_as_user_id: null,
          service_principal_id: '00000000-0000-0000-0000-0000000000ff',
          version: scheduleState.version + 1,
        };
        return route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            schedule: scheduleState,
            service_principal: { id: '00000000-0000-0000-0000-0000000000ff' },
          }),
        });
      },
    );

    await page.goto(`/schedules/${SCHEDULE_RID}`);
    await expect(page.getByTestId('schedule-config')).toBeVisible();
    await expect(page.getByTestId('run-as-section')).toBeVisible();
    await expect(page.getByTestId('run-as-user-banner')).toBeVisible();

    await page.getByTestId('run-as-project-tab').click();
    await page.getByTestId('project-scope-input').fill('ri.foundry.main.project.alpha');
    await page.getByTestId('convert-to-project-scope-button').click();

    await expect(page.getByTestId('run-as-sp-badge')).toBeVisible();
  });
});
