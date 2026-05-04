// P4 — Build Schedules application: search by Files filter and the
// URL-shareable filter contract.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

test.describe('Build Schedules application — search', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('Files filter narrows the table and persists in the URL', async ({ page }) => {
    let lastUrl: URL | null = null;

    await page.route('**/api/v1/data-integration/v1/schedules**', (route) => {
      lastUrl = new URL(route.request().url());
      const filesFiltered = lastUrl.searchParams.getAll('files');
      const data = filesFiltered.length === 0
        ? [
            {
              id: '00000000-0000-0000-0000-000000000001',
              rid: 'ri.foundry.main.schedule.daily',
              project_rid: 'ri.foundry.main.project.demo',
              name: 'daily schedule',
              description: '',
              trigger: { kind: { time: { cron: '0 9 * * *', time_zone: 'UTC', flavor: 'UNIX_5' } } },
              target: { kind: { sync_run: { sync_rid: 's', source_rid: 't' } } },
              paused: false,
              paused_reason: null,
              paused_at: null,
              auto_pause_exempt: false,
              pending_re_run: false,
              active_run_id: null,
              version: 1,
              created_by: 'tester',
              created_at: '2026-05-01T00:00:00Z',
              updated_at: '2026-05-01T00:00:00Z',
              last_run_at: null,
              scope_kind: 'USER',
              project_scope_rids: [],
              run_as_user_id: null,
              service_principal_id: null,
            },
          ]
        : [
            {
              id: '00000000-0000-0000-0000-000000000002',
              rid: 'ri.foundry.main.schedule.dataset-x',
              project_rid: 'ri.foundry.main.project.demo',
              name: 'dataset x event',
              description: '',
              trigger: {
                kind: {
                  event: {
                    type: 'DATA_UPDATED',
                    target_rid: 'ri.foundry.main.dataset.x',
                    branch_filter: [],
                  },
                },
              },
              target: { kind: { sync_run: { sync_rid: 's', source_rid: 't' } } },
              paused: false,
              paused_reason: null,
              paused_at: null,
              auto_pause_exempt: false,
              pending_re_run: false,
              active_run_id: null,
              version: 1,
              created_by: 'tester',
              created_at: '2026-05-01T00:00:00Z',
              updated_at: '2026-05-01T00:00:00Z',
              last_run_at: null,
              scope_kind: 'USER',
              project_scope_rids: [],
              run_as_user_id: null,
              service_principal_id: null,
            },
          ];
      return route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({ data, total: data.length }),
      });
    });

    await page.goto('/build-schedules');
    await expect(page.getByTestId('build-schedules-page')).toBeVisible();
    await expect(page.getByTestId('owner-only-banner')).toBeVisible();

    // Initial unfiltered render.
    await expect(page.getByTestId('schedules-table')).toBeVisible();
    await expect(page.locator('[data-testid="schedule-row"]').first()).toContainText('daily');

    // Add a Files filter; the request must include `files=ri…dataset.x`.
    await page
      .getByTestId('filter-files-input')
      .fill('ri.foundry.main.dataset.x');
    await page.getByTestId('filter-files-input').press('Enter');

    await expect(page.locator('[data-testid="schedule-row"]').first()).toContainText(
      'dataset x event',
    );
    expect(lastUrl).not.toBeNull();
    expect(lastUrl!.searchParams.getAll('files')).toContain('ri.foundry.main.dataset.x');

    // The page URL itself carries the filter (bookmark-able).
    await expect.poll(() => page.url()).toContain('files=ri.foundry.main.dataset.x');
  });
});
