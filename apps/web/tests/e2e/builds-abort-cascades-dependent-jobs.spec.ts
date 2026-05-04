// D1.1.5 5/5 — Abort button on the build detail page POSTs
// /v1/builds/{rid}:abort. After the abort the running children
// transition to ABORTED via the cascade implemented in
// `domain::build_executor::compute_cascade`.

import { expect, test } from '@playwright/test';
import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const RID = 'ri.foundry.main.build.cascade-1';

const initialEnvelope = {
	id: '1',
	rid: RID,
	pipeline_rid: 'ri.foundry.main.pipeline.cascade',
	build_branch: 'master',
	state: 'BUILD_RUNNING',
	trigger_kind: 'MANUAL',
	force_build: false,
	abort_policy: 'DEPENDENT_ONLY',
	queued_at: '2026-05-04T08:00:00Z',
	started_at: '2026-05-04T08:00:01Z',
	finished_at: null,
	requested_by: 'tester',
	created_at: '2026-05-04T08:00:00Z',
	job_spec_fallback: [],
	jobs: [
		{
			id: 'j1',
			rid: 'ri.foundry.main.job.parent',
			build_id: '1',
			job_spec_rid: 'ri.spec.parent',
			state: 'RUNNING',
			output_transaction_rids: [],
			state_changed_at: '2026-05-04T08:00:01Z',
			attempt: 0,
			stale_skipped: false,
			created_at: '2026-05-04T08:00:00Z'
		},
		{
			id: 'j2',
			rid: 'ri.foundry.main.job.child',
			build_id: '1',
			job_spec_rid: 'ri.spec.child',
			state: 'WAITING',
			output_transaction_rids: [],
			state_changed_at: '2026-05-04T08:00:01Z',
			attempt: 0,
			stale_skipped: false,
			created_at: '2026-05-04T08:00:00Z'
		}
	]
};

const aftermath = {
	...initialEnvelope,
	state: 'BUILD_ABORTING',
	jobs: initialEnvelope.jobs.map((j) =>
		j.state === 'RUNNING' ? { ...j, state: 'ABORT_PENDING' } : { ...j, state: 'ABORTED' }
	)
};

test.describe('Builds — abort cascades dependents', () => {
	test.beforeEach(async ({ page }) => {
		await mockFrontendApis(page);
		await seedAuthenticatedSession(page);
	});

	test('abort POSTs the right URL and propagates to dependents', async ({ page }) => {
		let aborted = false;

		await page.route(`**/v1/builds/${encodeURIComponent(RID)}:abort`, async (route) => {
			expect(route.request().method()).toBe('POST');
			aborted = true;
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ rid: RID, state: 'BUILD_ABORTING' })
			});
		});

		await page.route(`**/v1/builds/${encodeURIComponent(RID)}`, async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(aborted ? aftermath : initialEnvelope)
			});
		});

		await page.goto(`/builds/${encodeURIComponent(RID)}`);
		await expect(page.getByTestId('build-detail')).toBeVisible();
		await page.getByTestId('build-abort').click();
		await expect.poll(() => aborted).toBe(true);

		// After abort the detail page polls and shows ABORTING + the
		// child cascaded to ABORTED.
		await page.getByTestId('build-tab-jobs').click();
		await expect.poll(async () => {
			const text = await page.getByTestId('build-jobs-table').innerText();
			return text.includes('ABORT_PENDING') && text.includes('ABORTED');
		}).toBe(true);
	});
});
