// D1.1.5 5/5 — Foundry doc § Job execution: "previously completed
// jobs may still have written data to their output datasets." The
// build detail "Outputs" tab must reflect that: a child that
// COMPLETED before its sibling failed shows `committed = true` even
// though the build itself ended in BUILD_FAILED.

import { expect, test } from '@playwright/test';
import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const RID = 'ri.foundry.main.build.partial-success';

const envelope = {
	id: '1',
	rid: RID,
	pipeline_rid: 'ri.foundry.main.pipeline.partial',
	build_branch: 'master',
	state: 'BUILD_FAILED',
	trigger_kind: 'MANUAL',
	force_build: false,
	abort_policy: 'DEPENDENT_ONLY',
	queued_at: '2026-05-04T08:00:00Z',
	started_at: '2026-05-04T08:00:01Z',
	finished_at: '2026-05-04T08:00:30Z',
	requested_by: 'tester',
	created_at: '2026-05-04T08:00:00Z',
	job_spec_fallback: [],
	jobs: [
		{
			id: 'j1',
			rid: 'ri.foundry.main.job.early',
			build_id: '1',
			job_spec_rid: 'ri.spec.a',
			state: 'COMPLETED',
			output_transaction_rids: ['ri.foundry.main.transaction.a-1'],
			state_changed_at: '2026-05-04T08:00:10Z',
			attempt: 1,
			stale_skipped: false,
			created_at: '2026-05-04T08:00:00Z'
		},
		{
			id: 'j2',
			rid: 'ri.foundry.main.job.late',
			build_id: '1',
			job_spec_rid: 'ri.spec.b',
			state: 'FAILED',
			output_transaction_rids: ['ri.foundry.main.transaction.b-1'],
			state_changed_at: '2026-05-04T08:00:20Z',
			attempt: 1,
			stale_skipped: false,
			failure_reason: 'boom',
			created_at: '2026-05-04T08:00:00Z'
		}
	]
};

test.describe('Builds — failed build keeps completed-child outputs committed', () => {
	test.beforeEach(async ({ page }) => {
		await mockFrontendApis(page);
		await seedAuthenticatedSession(page);
	});

	test('Outputs tab shows committed=true for the early-finishing job', async ({ page }) => {
		await page.route(`**/v1/builds/${encodeURIComponent(RID)}`, async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(envelope)
			});
		});

		await page.route(`**/v1/jobs/ri.foundry.main.job.early/outputs`, async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					rid: 'ri.foundry.main.job.early',
					state: 'COMPLETED',
					stale_skipped: false,
					outputs: [
						{
							output_dataset_rid: 'ri.foundry.main.dataset.alpha',
							transaction_rid: 'ri.foundry.main.transaction.a-1',
							committed: true,
							aborted: false
						}
					]
				})
			});
		});

		await page.route(`**/v1/jobs/ri.foundry.main.job.late/outputs`, async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					rid: 'ri.foundry.main.job.late',
					state: 'FAILED',
					stale_skipped: false,
					outputs: [
						{
							output_dataset_rid: 'ri.foundry.main.dataset.beta',
							transaction_rid: 'ri.foundry.main.transaction.b-1',
							committed: false,
							aborted: true
						}
					]
				})
			});
		});

		await page.goto(`/builds/${encodeURIComponent(RID)}`);
		await expect(page.getByTestId('build-detail')).toBeVisible();
		await page.getByTestId('build-tab-outputs').click();

		const text = await page.getByTestId('build-outputs').innerText();
		// Default selector is the first job (the early one). It must
		// show its output committed.
		expect(text).toContain('alpha');
		expect(text).toContain('✓');
	});
});
