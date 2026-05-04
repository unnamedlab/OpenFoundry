// D1.1.5 5/5 — Per-build live-log streamer (Foundry doc § Live logs)
// renders the documented colour palette. Distinct from the older
// `live-logs-color-coding.spec.ts` (P4 component-level) — this one
// drives the colour-coded viewer through the /builds/{rid} detail
// page so the wiring of LiveLogViewer inside the Builds application
// is exercised end-to-end.

import { expect, test } from '@playwright/test';
import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const RID = 'ri.foundry.main.build.color-1';
const JOB_RID = 'ri.foundry.main.job.colored';

const ENVELOPE = {
	id: '1',
	rid: RID,
	pipeline_rid: 'ri.foundry.main.pipeline.colors',
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
			rid: JOB_RID,
			build_id: '1',
			job_spec_rid: 'ri.spec.s',
			state: 'RUNNING',
			output_transaction_rids: [],
			state_changed_at: '2026-05-04T08:00:01Z',
			attempt: 1,
			stale_skipped: false,
			created_at: '2026-05-04T08:00:00Z'
		}
	]
};

const STREAM_BODY = [
	'event: heartbeat',
	'data: {"phase":"initializing","delay_remaining_seconds":0}',
	'',
	'event: log',
	'data: {"sequence":1,"ts":"2026-05-04T08:00:01Z","level":"INFO","message":"info"}',
	'',
	'event: log',
	'data: {"sequence":2,"ts":"2026-05-04T08:00:02Z","level":"WARN","message":"warn"}',
	'',
	'event: log',
	'data: {"sequence":3,"ts":"2026-05-04T08:00:03Z","level":"ERROR","message":"err"}',
	'',
	'event: log',
	'data: {"sequence":4,"ts":"2026-05-04T08:00:04Z","level":"DEBUG","message":"dbg"}',
	'',
].join('\n');

test.describe('Builds — live logs in /builds/[rid] are colour-coded', () => {
	test.beforeEach(async ({ page }) => {
		await mockFrontendApis(page);
		await seedAuthenticatedSession(page);
	});

	test('per-level rows carry the documented Foundry hex palette', async ({ page }) => {
		await page.route(`**/v1/builds/${encodeURIComponent(RID)}`, async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(ENVELOPE)
			});
		});

		await page.route(`**/v1/jobs/${JOB_RID}/logs/stream*`, async (route) => {
			await route.fulfill({
				status: 200,
				contentType: 'text/event-stream',
				body: STREAM_BODY
			});
		});

		await page.goto(`/builds/${encodeURIComponent(RID)}`);
		await expect(page.getByTestId('build-detail')).toBeVisible();
		await page.getByTestId('build-tab-logs').click();
		await expect(page.getByTestId('live-log-viewer')).toBeVisible();

		// Style attribute carries `color: <hex>` for each row.
		for (const [seq, hex] of [
			[1, '#3b82f6'],
			[2, '#f59e0b'],
			[3, '#ef4444'],
			[4, '#6b7280']
		] as const) {
			const row = page.getByTestId(`live-logs-row-${seq}`);
			const styleAttr = await row.locator('.level').getAttribute('style');
			expect(styleAttr ?? '').toMatch(new RegExp(`color:\\s*${hex}`, 'i'));
		}

		// Persistent banner per Foundry doc.
		await expect(page.getByTestId('live-logs-banner')).toContainText(
			'Live logs are streamed in real-time'
		);
	});
});
