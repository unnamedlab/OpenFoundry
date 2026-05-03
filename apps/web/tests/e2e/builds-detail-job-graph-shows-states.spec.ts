// D1.1.5 5/5 — Build detail job-graph tab shows per-job state.

import { expect, test } from '@playwright/test';
import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const BUILD_RID = 'ri.foundry.main.build.detail-1';

test.describe('Build detail — job graph', () => {
	test.beforeEach(async ({ page }) => {
		await mockFrontendApis(page);
		await seedAuthenticatedSession(page);
		await page.route(`**/v1/builds/${encodeURIComponent(BUILD_RID)}`, async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					id: '1',
					rid: BUILD_RID,
					pipeline_rid: 'ri.foundry.main.pipeline.detail',
					build_branch: 'master',
					state: 'BUILD_RUNNING',
					trigger_kind: 'MANUAL',
					force_build: true,
					abort_policy: 'DEPENDENT_ONLY',
					queued_at: '2026-05-03T10:00:00Z',
					started_at: '2026-05-03T10:00:01Z',
					finished_at: null,
					requested_by: 'tester',
					created_at: '2026-05-03T10:00:00Z',
					job_spec_fallback: [],
					jobs: [
						{
							id: 'j1',
							rid: 'ri.foundry.main.job.aaa',
							build_id: '1',
							job_spec_rid: 'ri.spec.a',
							state: 'COMPLETED',
							output_transaction_rids: [],
							state_changed_at: '2026-05-03T10:00:10Z',
							attempt: 0,
							stale_skipped: false,
							created_at: '2026-05-03T10:00:00Z'
						},
						{
							id: 'j2',
							rid: 'ri.foundry.main.job.bbb',
							build_id: '1',
							job_spec_rid: 'ri.spec.b',
							state: 'RUNNING',
							output_transaction_rids: [],
							state_changed_at: '2026-05-03T10:00:11Z',
							attempt: 0,
							stale_skipped: false,
							created_at: '2026-05-03T10:00:00Z'
						},
						{
							id: 'j3',
							rid: 'ri.foundry.main.job.ccc',
							build_id: '1',
							job_spec_rid: 'ri.spec.c',
							state: 'WAITING',
							output_transaction_rids: [],
							state_changed_at: '2026-05-03T10:00:12Z',
							attempt: 0,
							stale_skipped: false,
							created_at: '2026-05-03T10:00:00Z'
						}
					]
				})
			});
		});
	});

	test('graph tab renders one node per job with state badges', async ({ page }) => {
		await page.goto(`/builds/${encodeURIComponent(BUILD_RID)}`);
		await expect(page.getByTestId('build-detail')).toBeVisible();
		await page.getByTestId('build-tab-graph').click();
		await expect(page.getByTestId('build-graph')).toBeVisible();

		await expect(page.getByTestId('graph-node-ri.foundry.main.job.aaa')).toBeVisible();
		await expect(page.getByTestId('graph-node-ri.foundry.main.job.bbb')).toBeVisible();
		await expect(page.getByTestId('graph-node-ri.foundry.main.job.ccc')).toBeVisible();

		await expect(page.getByTestId('state-badge-COMPLETED').first()).toBeVisible();
		await expect(page.getByTestId('state-badge-RUNNING').first()).toBeVisible();
		await expect(page.getByTestId('state-badge-WAITING').first()).toBeVisible();

		// "Force" pill rendered in header.
		await expect(page.getByText('⚡ Force')).toBeVisible();
	});
});
