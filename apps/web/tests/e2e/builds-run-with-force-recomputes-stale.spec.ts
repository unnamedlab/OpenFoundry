// D1.1.5 5/5 — "Run build" modal sends `force_build = true` so the
// backend skips the staleness check (Foundry doc § Staleness:
// "force build, which recomputes all datasets as part of the build,
// regardless of whether they are already up-to-date.").

import { expect, test } from '@playwright/test';
import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

test.describe('Builds — run with force recomputes stale', () => {
	test.beforeEach(async ({ page }) => {
		await mockFrontendApis(page);
		await seedAuthenticatedSession(page);
	});

	test('toggling force passes force_build=true to POST /v1/builds', async ({ page }) => {
		await page.route('**/v1/builds*', async (route, request) => {
			if (request.method() === 'GET') {
				await route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({ data: [], next_cursor: null, limit: 200 })
				});
				return;
			}
			const body = JSON.parse(request.postData() ?? '{}') as {
				force_build: boolean;
				abort_policy: string;
				output_dataset_rids: string[];
			};
			expect(body.force_build).toBe(true);
			expect(body.abort_policy).toBe('DEPENDENT_ONLY');
			expect(body.output_dataset_rids).toEqual([
				'ri.foundry.main.dataset.alpha',
				'ri.foundry.main.dataset.beta'
			]);
			await route.fulfill({
				status: 202,
				contentType: 'application/json',
				body: JSON.stringify({
					build_id: 'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee',
					state: 'BUILD_RESOLUTION',
					queued_reason: null,
					job_count: 1,
					output_transactions: []
				})
			});
		});

		await page.goto('/builds');
		await page.getByTestId('builds-run-button').click();
		await expect(page.getByTestId('builds-run-modal')).toBeVisible();

		await page.getByTestId('builds-run-pipeline-rid').fill('ri.foundry.main.pipeline.demo');
		await page.getByTestId('builds-run-branch').fill('master');
		await page.getByTestId('builds-run-outputs').fill(
			'ri.foundry.main.dataset.alpha, ri.foundry.main.dataset.beta'
		);
		await page.getByTestId('builds-run-force-build').check();
		await page.getByTestId('builds-run-submit').click();

		// Modal closes + we navigate to the new build.
		await expect.poll(() => page.url()).toContain('/builds/');
	});
});
