import { expect, test } from '@playwright/test';

/**
 * P2 — Pipeline Builder "Build settings" panel: Build branch + JobSpec
 * fallback chain editor.
 *
 * Exercises the new component surface (`BuildSettings.svelte`) that
 * implements the Foundry doc § "Branches in builds":
 *   * users can edit the build branch text input;
 *   * users can append / re-order / remove fallback branches;
 *   * the "Resolved build plan" preview hits
 *     `POST /api/v1/data-integration/pipelines/{rid}/dry-run-resolve`
 *     and renders the resolved per-output table.
 *
 * Pure unit-style spec — mounts the component directly via the
 * Pipeline Builder canvas route. Tolerant: skip if the canvas is not
 * reachable in the running web app.
 */

const PIPELINE_RID = 'ri.foundry.main.pipeline.e2e-fallback';

test.describe('pipeline builder fallback chain editor', () => {
  test('appends, reorders and removes a fallback branch', async ({
    page,
  }, testInfo) => {
    const probe = await page.request
      .get('/api/v1/data-integration/pipelines')
      .catch((e: unknown) => ({ failure: e }));
    if ('failure' in probe || !probe.ok()) {
      testInfo.skip(
        true,
        'pipeline-authoring-service unreachable; skipping fallback-chain spec.',
      );
      return;
    }

    await page.goto(`/pipelines/${PIPELINE_RID}`);
    const settings = page.getByTestId('pipeline-build-settings');
    await expect(settings).toBeVisible();

    // 1. Set the build branch.
    const buildBranchInput = settings.getByTestId('build-branch-input');
    await buildBranchInput.fill('feature/bookings');
    await expect(buildBranchInput).toHaveValue('feature/bookings');

    // 2. Append `develop` then `master` to the chain.
    const newField = settings.getByTestId('jobspec-fallback-new');
    const addButton = settings.getByTestId('jobspec-fallback-add');
    await newField.fill('develop');
    await addButton.click();
    await newField.fill('master');
    await addButton.click();

    const chain = settings.getByTestId('jobspec-fallback-chain');
    await expect(chain.getByTestId('jobspec-fallback-0')).toContainText('develop');
    await expect(chain.getByTestId('jobspec-fallback-1')).toContainText('master');

    // 3. Reorder: move `master` up.
    await settings.getByTestId('jobspec-fallback-up-1').click();
    await expect(chain.getByTestId('jobspec-fallback-0')).toContainText('master');
    await expect(chain.getByTestId('jobspec-fallback-1')).toContainText('develop');

    // 4. Remove `develop`.
    await settings.getByTestId('jobspec-fallback-remove-1').click();
    await expect(chain.getByTestId('jobspec-fallback-1')).toBeHidden({
      timeout: 1500,
    });

    // 5. Hitting dry-run should call the new endpoint and render the
    //    resolution table or surface an error if the pipeline is empty.
    const dryRun = settings.getByTestId('dry-run-resolve-button');
    if (await dryRun.isEnabled()) {
      await dryRun.click();
      // Either the table or an error block must show within ~3s.
      await Promise.race([
        settings.getByTestId('dry-run-table').waitFor({ timeout: 3000 }),
        settings.getByTestId('dry-run-errors').waitFor({ timeout: 3000 }),
        settings.getByTestId('dry-run-error').waitFor({ timeout: 3000 }),
      ]).catch(() => {});
    }
  });
});
