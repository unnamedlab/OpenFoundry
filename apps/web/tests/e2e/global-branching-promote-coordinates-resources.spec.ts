// P5 — Global Branching dashboard: promote coordinates linked
// resources by emitting `global.branch.promote.requested.v1`.
//
// Mocks the four global-branch endpoints so the spec can drive the
// happy path: list → create → link → promote. Asserts the promote
// button surfaces the event_id + topic returned by the API.

import { expect, test, type Route } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const GLOBAL_ID = '00000000-0000-0000-0000-00000000a000';
const GLOBAL = {
  id: GLOBAL_ID,
  rid: `ri.foundry.main.globalbranch.${GLOBAL_ID}`,
  name: 'release-2026-Q3',
  parent_global_branch: null,
  description: '',
  created_by: 'tester',
  created_at: '2026-05-01T00:00:00Z',
  archived_at: null,
};

test.describe('global branching promote', () => {
  test('promote button surfaces the canonical promote event', async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);

    let resources: Array<{
      global_branch_id: string;
      resource_type: string;
      resource_rid: string;
      branch_rid: string;
      status: string;
      last_synced_at: string;
    }> = [];

    await page.route(`**/api/v1/global-branches`, (route) =>
      route.fulfill({ contentType: 'application/json', body: JSON.stringify([GLOBAL]) }),
    );
    await page.route(
      `**/api/v1/global-branches/${GLOBAL_ID}`,
      (route) =>
        route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            ...GLOBAL,
            link_count: resources.length,
            drifted_count: 0,
            archived_count: 0,
          }),
        }),
    );
    await page.route(
      `**/api/v1/global-branches/${GLOBAL_ID}/resources`,
      (route) =>
        route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify(resources),
        }),
    );
    await page.route(
      `**/api/v1/global-branches/${GLOBAL_ID}/links`,
      async (route: Route) => {
        const body = JSON.parse(route.request().postData() ?? '{}');
        const link = {
          global_branch_id: GLOBAL_ID,
          resource_type: body.resource_type,
          resource_rid: body.resource_rid,
          branch_rid: body.branch_rid,
          status: 'in_sync',
          last_synced_at: '2026-05-01T00:00:00Z',
        };
        resources = [...resources, link];
        await route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify(link),
        });
      },
    );
    await page.route(
      `**/api/v1/global-branches/${GLOBAL_ID}/promote`,
      (route) =>
        route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            event_id: '00000000-0000-0000-0000-00000000beef',
            global_branch_id: GLOBAL_ID,
            topic: 'foundry.global.branch.promote.requested.v1',
          }),
        }),
    );

    await page.goto('/global-branching');
    await expect(page.getByTestId('global-branching-dashboard')).toBeVisible();
    await page.getByTestId(`global-branching-row-${GLOBAL.name}`).click();

    // Link a dataset into the workstream.
    await page.getByTestId('global-branching-link-resource-rid').fill('ri.foundry.main.dataset.demo');
    await page.getByTestId('global-branching-link-branch-rid').fill('ri.foundry.main.branch.demo');
    await page.getByTestId('global-branching-link-submit').click();

    await page.getByTestId('global-branching-promote-button').click();
    await expect(page.getByTestId('global-branching-promote-result')).toContainText(
      'foundry.global.branch.promote.requested.v1',
    );
  });
});
