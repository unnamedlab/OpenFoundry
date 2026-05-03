// D1.1.5 5/5 — /builds list filters builds by branch + state.
// Stubs `GET /v1/builds` and asserts the filter chips drive the
// outgoing request's query params.

import { expect, test } from '@playwright/test';
import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const BASE = '**/v1/builds*';

const FIXTURE_BUILDS = [
	{
		id: '1',
		rid: 'ri.foundry.main.build.aaaaaaaa',
		pipeline_rid: 'ri.foundry.main.pipeline.demo',
		build_branch: 'master',
		state: 'BUILD_RUNNING',
		trigger_kind: 'MANUAL',
		force_build: false,
		abort_policy: 'DEPENDENT_ONLY',
		queued_at: '2026-05-03T10:00:00Z',
		started_at: '2026-05-03T10:00:01Z',
		finished_at: null,
		requested_by: 'tester',
		created_at: '2026-05-03T10:00:00Z',
		job_spec_fallback: []
	},
	{
		id: '2',
		rid: 'ri.foundry.main.build.bbbbbbbb',
		pipeline_rid: 'ri.foundry.main.pipeline.demo',
		build_branch: 'feature',
		state: 'BUILD_FAILED',
		trigger_kind: 'MANUAL',
		force_build: false,
		abort_policy: 'DEPENDENT_ONLY',
		queued_at: '2026-05-03T09:00:00Z',
		started_at: '2026-05-03T09:00:01Z',
		finished_at: '2026-05-03T09:00:30Z',
		requested_by: 'tester',
		created_at: '2026-05-03T09:00:00Z',
		job_spec_fallback: []
	}
];

test.describe('Builds list — filters', () => {
	test.beforeEach(async ({ page }) => {
		await mockFrontendApis(page);
		await seedAuthenticatedSession(page);
	});

	test('filtering by branch + state hits /v1/builds with the right query', async ({ page }) => {
		const observedUrls: string[] = [];
		await page.route(BASE, async (route) => {
			const url = route.request().url();
			observedUrls.push(url);
			const sp = new URL(url).searchParams;
			let data = FIXTURE_BUILDS.slice();
			if (sp.get('branch')) data = data.filter((b) => b.build_branch === sp.get('branch'));
			if (sp.get('status')) data = data.filter((b) => b.state === sp.get('status'));
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ data, next_cursor: null, limit: 200 })
			});
		});

		await page.goto('/builds');
		await expect(page.getByTestId('builds-table')).toBeVisible();
		await expect(page.getByTestId('builds-row-ri.foundry.main.build.aaaaaaaa')).toBeVisible();
		await expect(page.getByTestId('builds-row-ri.foundry.main.build.bbbbbbbb')).toBeVisible();

		// Filter by branch.
		await page.getByTestId('builds-filter-branch').fill('feature');
		await page.getByTestId('builds-filter-apply').click();
		await expect.poll(() =>
			observedUrls.some((u) => u.includes('branch=feature'))
		).toBeTruthy();

		// Filter by state — toggle the FAILED chip.
		await page.getByTestId('builds-filter-state-BUILD_FAILED').click();
		await expect.poll(() =>
			observedUrls.some((u) => u.includes('status=BUILD_FAILED'))
		).toBeTruthy();
	});
});
