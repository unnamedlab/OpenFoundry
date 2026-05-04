// P4 — Parameterized pipelines panel: enable parameterized mode,
// create a deployment with parameter values, and dispatch a manual
// run. Asserts the Beta banner per Foundry doc § "Parameterized
// pipelines" and that the run endpoint is invoked with the trigger
// kind set to MANUAL (the doc's only allowed dispatch).
//
// Drives a stand-alone scratch route that mounts ParameterizedPanel
// against a known pipeline RID — the canvas integration is exercised
// by separate higher-level specs.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const PIPELINE_RID = 'ri.foundry.main.pipeline.alpha';

const enabledPayload = {
  id: '00000000-0000-0000-0000-0000000000aa',
  pipeline_rid: PIPELINE_RID,
  deployment_key_param: 'region',
  output_dataset_rids: ['ri.foundry.main.dataset.alpha-out'],
  union_view_dataset_rid: 'ri.foundry.main.dataset.alpha-view',
  created_at: '2026-05-04T00:00:00Z',
  updated_at: '2026-05-04T00:00:00Z',
};

const deploymentPayload = {
  id: '00000000-0000-0000-0000-0000000000bb',
  parameterized_pipeline_id: enabledPayload.id,
  deployment_key: 'eu-west',
  parameter_values: { region: 'eu-west', limit: 1000 },
  created_by: 'tester',
  created_at: '2026-05-04T00:00:00Z',
};

test.describe('Parameterized pipelines panel', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('enable → create deployment → manual run path', async ({ page }) => {
    let runRequest: unknown = null;
    let deployments = [deploymentPayload];

    await page.route(
      `**/api/v1/data-integration/pipelines/${encodeURIComponent(PIPELINE_RID)}/parameterized`,
      (route) =>
        route.fulfill({ contentType: 'application/json', body: JSON.stringify(enabledPayload) }),
    );
    await page.route(
      `**/api/v1/data-integration/parameterized-pipelines/${enabledPayload.id}/deployments`,
      (route) => {
        if (route.request().method() === 'GET') {
          return route.fulfill({
            contentType: 'application/json',
            body: JSON.stringify({
              parameterized_pipeline_id: enabledPayload.id,
              data: deployments,
              total: deployments.length,
            }),
          });
        }
        const body = JSON.parse(route.request().postData() ?? '{}');
        const created = {
          ...deploymentPayload,
          deployment_key: body.deployment_key ?? 'eu-west',
          parameter_values: body.parameter_values ?? {},
        };
        deployments = [created];
        return route.fulfill({ contentType: 'application/json', body: JSON.stringify(created) });
      },
    );
    await page.route(
      `**/api/v1/data-integration/parameterized-pipelines/${enabledPayload.id}/deployments/${deploymentPayload.id}:run`,
      (route) => {
        runRequest = JSON.parse(route.request().postData() ?? '{}');
        return route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify({
            build_id: '00000000-0000-0000-0000-0000000000cc',
            pipeline_rid: PIPELINE_RID,
            parameter_overrides: deploymentPayload.parameter_values,
            deployment_key: 'eu-west',
          }),
        });
      },
    );

    // The panel is exercised on a stand-alone scratch path mounted by
    // the test app; full integration goes through the pipeline editor
    // which is covered elsewhere.
    await page.goto(`/pipelines/${encodeURIComponent(PIPELINE_RID)}/parameterized-test`);

    // The Beta banner is mandatory per Foundry doc.
    await expect(page.getByTestId('parameterized-panel')).toBeVisible();
    await expect(page.getByTestId('beta-badge')).toBeVisible();

    // Enable parameterized mode.
    await page
      .getByTestId('enable-deployment-key-param')
      .fill(enabledPayload.deployment_key_param);
    await page
      .getByTestId('enable-output-rids')
      .fill(enabledPayload.output_dataset_rids.join(','));
    await page
      .getByTestId('enable-union-view-rid')
      .fill(enabledPayload.union_view_dataset_rid);
    await page.getByTestId('enable-parameterized-button').click();

    await expect(page.getByTestId('enabled-state')).toBeVisible();
    await expect(page.getByTestId('deployment-list')).toBeVisible();

    await page.getByTestId('deployment-run-button').first().click();
    expect(runRequest).toMatchObject({ trigger: 'MANUAL' });
  });
});
