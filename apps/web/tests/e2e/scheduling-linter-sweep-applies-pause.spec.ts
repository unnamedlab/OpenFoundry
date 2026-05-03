// P3 — Sweep linter UI. Drives `/build-schedules/sweep`, runs the
// rule catalogue against a mocked sweep response, ticks one finding,
// and asserts the apply call goes out with the expected payload shape.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const FINDING_ID = '00000000-0000-0000-0000-0000000000aa';

const sweepResponse = {
  dry_run: true,
  by_rule: ['SCH-001'],
  findings: [
    {
      id: FINDING_ID,
      rule_id: 'Sch001InactiveLastNinety',
      severity: 'Warning',
      schedule_rid: 'ri.foundry.main.schedule.idle',
      project_rid: 'ri.foundry.main.project.demo',
      message: 'Schedule has not run in the last 90 days.',
      recommended_action: 'Pause',
    },
  ],
};

test.describe('scheduling linter sweep page', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('apply pauses the selected SCH-001 finding', async ({ page }) => {
    let captured: unknown = null;

    await page.route('**/api/v1/data-integration/v1/scheduling-linter/sweep**', (route) =>
      route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify(sweepResponse),
      }),
    );
    await page.route(
      '**/api/v1/data-integration/v1/scheduling-linter/sweep:apply',
      (route) => {
        captured = JSON.parse(route.request().postData() ?? '{}');
        return route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            applied: [
              {
                finding_id: FINDING_ID,
                schedule_rid: 'ri.foundry.main.schedule.idle',
                action: 'Pause',
                result: 'paused',
              },
            ],
          }),
        });
      },
    );

    await page.goto('/build-schedules/sweep');
    await page.getByTestId('sweep-run-button').click();
    await expect(page.getByTestId('rule-bucket-Sch001InactiveLastNinety')).toBeVisible();

    await page.getByTestId('sweep-apply-button').click();
    await expect(page.getByTestId('sweep-apply-result')).toBeVisible();
    expect(captured).toBeTruthy();
    const body = captured as { finding_ids?: string[] };
    expect(body.finding_ids).toContain(FINDING_ID);
  });
});
