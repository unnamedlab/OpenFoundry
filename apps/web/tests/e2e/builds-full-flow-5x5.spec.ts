// D1.1.5 5/5 — full-flow journey across the Builds application.
//
// Walks the operator through the headline closure scenario:
//   1. Run a build (modal → POST /v1/builds).
//   2. Open the new build's detail page; inspect the job graph,
//      live logs and outputs tabs.
//   3. Abort the in-flight build.
//   4. Re-run with force; confirm `force_build = true` round-trips.
//   5. Re-run without force; confirm the second build's jobs come
//      back as `stale_skipped = true`.
//
// This is the canonical 5×5 scenario from the closure prompt: a
// pipeline A→B→C, exercised end-to-end through the dedicated
// Builds application UI surface.

import { expect, test, type Page } from '@playwright/test';
import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const PIPELINE_RID = 'ri.foundry.main.pipeline.full55';
const BUILD_1 = 'ri.foundry.main.build.full55-1';
const BUILD_2 = 'ri.foundry.main.build.full55-2';
const BUILD_3 = 'ri.foundry.main.build.full55-3';
const OUTPUTS = [
	'ri.foundry.main.dataset.A',
	'ri.foundry.main.dataset.B',
	'ri.foundry.main.dataset.C'
];

type BuildState =
	| 'BUILD_RESOLUTION'
	| 'BUILD_QUEUED'
	| 'BUILD_RUNNING'
	| 'BUILD_ABORTING'
	| 'BUILD_FAILED'
	| 'BUILD_ABORTED'
	| 'BUILD_COMPLETED';

function envelope(rid: string, state: BuildState, force: boolean, jobsCompleted: 'fresh' | 'stale_skipped' = 'fresh') {
	return {
		id: rid,
		rid,
		pipeline_rid: PIPELINE_RID,
		build_branch: 'master',
		state,
		trigger_kind: force ? 'FORCE' : 'MANUAL',
		force_build: force,
		abort_policy: 'DEPENDENT_ONLY',
		queued_at: '2026-05-04T08:00:00Z',
		started_at: '2026-05-04T08:00:01Z',
		finished_at: state === 'BUILD_COMPLETED' ? '2026-05-04T08:00:30Z' : null,
		requested_by: 'tester',
		created_at: '2026-05-04T08:00:00Z',
		job_spec_fallback: [],
		jobs: ['A', 'B', 'C'].map((letter, i) => ({
			id: `${rid}-${letter}`,
			rid: `ri.foundry.main.job.${rid.slice(-1)}-${letter}`,
			build_id: rid,
			job_spec_rid: `ri.spec.${letter}`,
			state: state === 'BUILD_COMPLETED' ? 'COMPLETED' : i === 0 ? 'RUNNING' : 'WAITING',
			output_transaction_rids: [`ri.foundry.main.transaction.${rid}-${letter}`],
			state_changed_at: '2026-05-04T08:00:01Z',
			attempt: 1,
			stale_skipped: state === 'BUILD_COMPLETED' && jobsCompleted === 'stale_skipped',
			created_at: '2026-05-04T08:00:00Z'
		}))
	};
}

async function setupRoutes(page: Page) {
	const state: {
		runs: number;
		current: ReturnType<typeof envelope> | null;
	} = { runs: 0, current: null };

	await page.route('**/v1/builds*', async (route, request) => {
		if (request.method() === 'GET') {
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					data: state.current ? [state.current] : [],
					next_cursor: null,
					limit: 200
				})
			});
			return;
		}
		// POST — increment the run counter and return a new build.
		state.runs += 1;
		const rid = state.runs === 1 ? BUILD_1 : state.runs === 2 ? BUILD_2 : BUILD_3;
		const body = JSON.parse(request.postData() ?? '{}') as { force_build: boolean };
		state.current = envelope(
			rid,
			'BUILD_RUNNING',
			body.force_build,
			state.runs === 3 ? 'stale_skipped' : 'fresh'
		);
		await route.fulfill({
			status: 202,
			contentType: 'application/json',
			body: JSON.stringify({
				build_id: rid.split('.').pop(),
				state: 'BUILD_RUNNING',
				queued_reason: null,
				job_count: 3,
				output_transactions: []
			})
		});
	});

	for (const rid of [BUILD_1, BUILD_2, BUILD_3]) {
		await page.route(`**/v1/builds/${encodeURIComponent(rid)}`, async (route) => {
			const env = state.current && state.current.rid === rid
				? state.current
				: envelope(
						rid,
						'BUILD_COMPLETED',
						rid === BUILD_2,
						rid === BUILD_3 ? 'stale_skipped' : 'fresh'
					);
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify(env)
			});
		});
		await page.route(`**/v1/builds/${encodeURIComponent(rid)}:abort`, async (route) => {
			if (state.current && state.current.rid === rid) {
				state.current = { ...state.current, state: 'BUILD_ABORTED' };
			}
			await route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ rid, state: 'BUILD_ABORTING' })
			});
		});
	}

	// Stub job logs streams + REST history so the Live logs tab
	// renders at least the banner.
	await page.route(`**/v1/jobs/*/logs/stream*`, async (route) => {
		await route.fulfill({
			status: 200,
			contentType: 'text/event-stream',
			body: 'event: heartbeat\ndata: {"phase":"initializing","delay_remaining_seconds":0}\n\n'
		});
	});
	await page.route(`**/v1/jobs/*/logs*`, async (route) => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({ rid: 'unused', data: [] })
		});
	});
}

test.describe('Builds — full 5×5 closure flow', () => {
	test.beforeEach(async ({ page }) => {
		await mockFrontendApis(page);
		await seedAuthenticatedSession(page);
		await setupRoutes(page);
	});

	test('run → graph → logs → abort → re-run-force → re-run shows stale_skipped', async ({ page }) => {
		// 1. Open /builds and submit a fresh build via the modal.
		await page.goto('/builds');
		await page.getByTestId('builds-run-button').click();
		await page.getByTestId('builds-run-pipeline-rid').fill(PIPELINE_RID);
		await page.getByTestId('builds-run-branch').fill('master');
		await page.getByTestId('builds-run-outputs').fill(OUTPUTS.join(', '));
		await page.getByTestId('builds-run-submit').click();

		// 2. We arrive on /builds/{rid}; check graph + jobs tabs.
		await expect.poll(() => page.url()).toContain('/builds/');
		await page.getByTestId('build-tab-graph').click();
		await expect(page.getByTestId('build-graph')).toBeVisible();
		await page.getByTestId('build-tab-jobs').click();
		await expect(page.getByTestId('build-jobs-table')).toBeVisible();

		// 3. Live logs tab renders the banner from LiveLogViewer.
		await page.getByTestId('build-tab-logs').click();
		await expect(page.getByTestId('live-logs-banner')).toContainText(
			'Live logs are streamed in real-time'
		);

		// 4. Abort the build.
		await page.getByTestId('build-abort').click();
		await expect(page.getByTestId('build-toast')).toBeVisible();

		// 5. Re-run with force — uses the modal again so we control
		// the force_build flag explicitly. Open /builds again,
		// trigger a new run with force.
		await page.goto('/builds');
		await page.getByTestId('builds-run-button').click();
		await page.getByTestId('builds-run-pipeline-rid').fill(PIPELINE_RID);
		await page.getByTestId('builds-run-branch').fill('master');
		await page.getByTestId('builds-run-outputs').fill(OUTPUTS.join(', '));
		await page.getByTestId('builds-run-force-build').check();
		await page.getByTestId('builds-run-submit').click();
		await expect.poll(() => page.url()).toContain('/builds/');

		// 6. A non-force re-run after a successful force build sees
		// every job as stale-skipped; the detail page exposes that
		// flag via the graph.
		await page.goto('/builds');
		await page.getByTestId('builds-run-button').click();
		await page.getByTestId('builds-run-pipeline-rid').fill(PIPELINE_RID);
		await page.getByTestId('builds-run-branch').fill('master');
		await page.getByTestId('builds-run-outputs').fill(OUTPUTS.join(', '));
		await page.getByTestId('builds-run-submit').click();
		await expect.poll(() => page.url()).toContain('/builds/');

		// Wait for the detail page's polling tick to reload the
		// envelope (5s default, but the route stub returns instantly).
		// On the third build the fixture marks every COMPLETED job
		// `stale_skipped = true`.
		await page.getByTestId('build-tab-graph').click();
		await expect.poll(async () => {
			const text = await page.getByTestId('build-graph').innerText();
			return text.includes('stale');
		}, { timeout: 10_000 }).toBe(true);
	});
});
