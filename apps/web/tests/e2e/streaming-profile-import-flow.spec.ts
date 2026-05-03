import { expect, test } from '@playwright/test';

/**
 * Bloque P3 — Streaming profile import flow.
 *
 * Exercises the new Foundry-parity surface:
 *   * `GET  /v1/streaming-profiles` lists built-in profiles
 *     (Default, High Parallelism, Large State, Large Records).
 *   * `POST /v1/projects/{rid}/streaming-profile-refs/{profile_id}`
 *     imports a profile into a project; restricted profiles require
 *     the Enrollment Resource Administrator role.
 *   * `POST /v1/pipelines/{rid}/streaming-profiles` requires the
 *     project ref to exist (412 STREAMING_PROFILE_NOT_IMPORTED).
 *   * The Control Panel "Streaming profiles" page renders the table
 *     and Import dialog.
 *
 * Tolerant by design: when the backend is offline we skip rather than
 * fail CI.
 */

test.describe('streaming profiles import flow', () => {
  test('imports a profile, attaches to a pipeline, surfaces effective config', async ({
    page,
  }, testInfo) => {
    const apiBase = '/api/v1/streaming';

    const probe = await page.request
      .get(`${apiBase}/overview`)
      .catch((e: unknown) => ({ failure: e }));
    if ('failure' in probe || !probe.ok()) {
      testInfo.skip(true, 'event-streaming-service unreachable; skipping.');
      return;
    }

    // 1. List built-in profiles seeded by 20260504000003_streaming_profiles.sql.
    const list = await page.request.get(`${apiBase}/streaming-profiles`);
    expect(list.ok()).toBeTruthy();
    const body = await list.json();
    const names = body.data.map((p: { name: string }) => p.name);
    expect(names).toContain('Default');
    expect(names).toContain('High Parallelism');
    const defaultProfile = body.data.find(
      (p: { name: string }) => p.name === 'Default',
    );
    expect(defaultProfile).toBeTruthy();

    // 2. Try to attach the Default profile to a pipeline before the
    //    project ref exists — must fail 412 STREAMING_PROFILE_NOT_IMPORTED.
    const projectRid = `ri.compass.main.project.e2e-${Date.now()}`;
    const pipelineRid = `ri.foundry.main.pipeline.e2e-${Date.now()}`;
    const earlyAttach = await page.request.post(
      `${apiBase}/pipelines/${encodeURIComponent(pipelineRid)}/streaming-profiles`,
      {
        data: { project_rid: projectRid, profile_id: defaultProfile.id },
      },
    );
    expect(earlyAttach.status()).toBe(412);
    const earlyBody = await earlyAttach.json();
    expect(earlyBody.error).toContain('STREAMING_PROFILE_NOT_IMPORTED');

    // 3. Import the profile into the project. Default is unrestricted
    //    so any caller with `compass:import-resource-to` succeeds.
    const importResp = await page.request.post(
      `${apiBase}/projects/${encodeURIComponent(projectRid)}/streaming-profile-refs/${defaultProfile.id}`,
    );
    expect(importResp.ok()).toBeTruthy();

    // 4. Now the attach succeeds.
    const attach = await page.request.post(
      `${apiBase}/pipelines/${encodeURIComponent(pipelineRid)}/streaming-profiles`,
      {
        data: { project_rid: projectRid, profile_id: defaultProfile.id },
      },
    );
    expect(attach.ok()).toBeTruthy();

    // 5. The effective Flink config endpoint composes the profile.
    const effective = await page.request.get(
      `${apiBase}/pipelines/${encodeURIComponent(pipelineRid)}/effective-flink-config`,
    );
    expect(effective.ok()).toBeTruthy();
    const effBody = await effective.json();
    expect(Object.keys(effBody.config)).toContain('parallelism.default');

    // 6. Restricted profile (Large State) must be rejected for callers
    //    without `enrollment_resource_administrator`. We can only
    //    assert the *response* shape here because the test runner's
    //    JWT seed depends on the dev environment; if the call
    //    succeeds because the test user has the role, accept that
    //    too. Either way the surface is exercised end-to-end.
    const largeState = body.data.find(
      (p: { name: string }) => p.name === 'Large State',
    );
    if (largeState) {
      const importLarge = await page.request.post(
        `${apiBase}/projects/${encodeURIComponent(projectRid)}/streaming-profile-refs/${largeState.id}`,
      );
      if (!importLarge.ok()) {
        const errBody = await importLarge.json();
        expect(errBody.error).toContain(
          'STREAMING_PROFILE_RESTRICTED_REQUIRES_ENROLLMENT_ADMIN',
        );
      }
    }

    // 7. The Control Panel page renders the table and the restricted
    //    badge for Large State.
    await page.goto('/control-panel/streaming-profiles');
    await expect(page.getByTestId('streaming-profiles-page')).toBeVisible();
    await expect(page.getByTestId('profiles-table')).toContainText('Default');
    if (largeState) {
      await expect(
        page.getByTestId(`restricted-${largeState.id}`),
      ).toBeVisible();
    }
  });
});
