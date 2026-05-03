<!--
  /builds/[rid] — Foundry "Builds application" detail view.

  Tabs (Foundry parity):
    * Overview     — KPI cards (jobs, duration, datasets touched).
    * Job graph    — DAG canvas with per-job state colour.
    * Jobs         — table with rid, kind, state, attempts, outputs.
    * Live logs    — embeds LiveLogViewer (P4) with a job selector.
    * Inputs       — resolved view filters from `jobs.input_view_resolutions`.
    * Outputs      — `job_outputs` rows + commit/abort flags.
    * Audit        — `job_state_transitions` audit trail.
-->
<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import {
		abortBuildV1,
		getBuildV1,
		getJobInputResolutionsV1,
		getJobOutputsV1,
		runBuildV1,
		type BuildEnvelope,
		type Job,
		type JobInputResolutionsResponse,
		type JobOutputsResponse
	} from '$lib/api/buildsV1';
	import StateBadge from '$lib/components/builds/StateBadge.svelte';
	import LiveLogViewer from '$lib/components/pipeline/LiveLogViewer.svelte';

	type TabId = 'overview' | 'graph' | 'jobs' | 'logs' | 'inputs' | 'outputs' | 'audit';

	let envelope = $state<BuildEnvelope | null>(null);
	let loading = $state<boolean>(true);
	let error = $state<string | null>(null);
	let activeTab = $state<TabId>('overview');
	let activeJobRid = $state<string | null>(null);
	let jobInputs = $state<JobInputResolutionsResponse | null>(null);
	let jobOutputs = $state<JobOutputsResponse | null>(null);
	let auditRows = $state<Array<{ job_rid: string; from_state: string | null; to_state: string; occurred_at: string; reason: string | null }>>([]);
	let toast = $state<string | null>(null);
	let toastTimer: number | null = null;

	let pollHandle: number | null = null;

	const rid = $derived($page.params.rid);

	function showToast(msg: string) {
		toast = msg;
		if (toastTimer !== null) clearTimeout(toastTimer);
		toastTimer = window.setTimeout(() => (toast = null), 4000);
	}

	async function refresh() {
		try {
			envelope = await getBuildV1(rid);
			error = null;
			if (!activeJobRid && envelope?.jobs?.length) {
				activeJobRid = envelope.jobs[0].rid;
			}
		} catch (e) {
			error = String(e);
		} finally {
			loading = false;
		}
	}

	async function refreshActiveJob() {
		if (!activeJobRid) return;
		try {
			const [inputs, outputs] = await Promise.all([
				getJobInputResolutionsV1(activeJobRid),
				getJobOutputsV1(activeJobRid)
			]);
			jobInputs = inputs;
			jobOutputs = outputs;
		} catch (e) {
			console.warn('refresh job aux failed', e);
		}
	}

	async function abortBuild() {
		try {
			await abortBuildV1(rid);
			showToast('Abort signal sent. Jobs in flight will move through ABORT_PENDING.');
			await refresh();
		} catch (e) {
			showToast(`Abort failed: ${e}`);
		}
	}

	async function rerunBuild(force: boolean) {
		if (!envelope) return;
		try {
			const outputs: string[] = [];
			for (const job of envelope.jobs) {
				const txns = job.output_transaction_rids ?? [];
				for (const t of txns) {
					if (t) outputs.push(t);
				}
			}
			const created = await runBuildV1({
				pipeline_rid: envelope.pipeline_rid,
				build_branch: envelope.build_branch,
				output_dataset_rids: outputs.length > 0 ? outputs : ['ri.foundry.main.dataset.unknown'],
				force_build: force,
				abort_policy: envelope.abort_policy,
				trigger_kind: force ? 'FORCE' : 'MANUAL'
			});
			showToast(`Build queued: ${created.state}`);
			void goto(`/builds/ri.foundry.main.build.${created.build_id}`);
		} catch (e) {
			showToast(`Re-run failed: ${e}`);
		}
	}

	function setTab(tab: TabId) {
		activeTab = tab;
		if (tab === 'inputs' || tab === 'outputs') void refreshActiveJob();
	}

	function handleKey(e: KeyboardEvent) {
		const tag = (e.target as HTMLElement | null)?.tagName ?? '';
		if (tag === 'INPUT' || tag === 'TEXTAREA') return;
		if (e.key === 'a' && envelope && !envelope.finished_at) {
			e.preventDefault();
			void abortBuild();
		}
		if (e.key === 'l') {
			e.preventDefault();
			setTab('logs');
		}
	}

	function durationLabel(start?: string | null, end?: string | null): string {
		if (!start) return '—';
		const ms = Math.max(0, Date.parse(end ?? new Date().toISOString()) - Date.parse(start));
		if (ms < 1000) return `${ms}ms`;
		const s = Math.floor(ms / 1000);
		if (s < 60) return `${s}s`;
		const m = Math.floor(s / 60);
		const r = s % 60;
		return `${m}m ${r}s`;
	}

	const kpis = $derived.by(() => {
		const jobs = envelope?.jobs ?? [];
		const completed = jobs.filter((j) => j.state === 'COMPLETED' && !j.stale_skipped).length;
		const skipped = jobs.filter((j) => j.stale_skipped).length;
		const failed = jobs.filter((j) => j.state === 'FAILED').length;
		const aborted = jobs.filter((j) => j.state === 'ABORTED').length;
		const datasets = new Set<string>();
		for (const j of jobs) {
			for (const t of j.output_transaction_rids ?? []) datasets.add(t);
		}
		return {
			total: jobs.length,
			completed,
			skipped,
			failed,
			aborted,
			datasets: datasets.size
		};
	});

	const jobLookup = $derived(
		new Map((envelope?.jobs ?? []).map((j) => [j.rid, j]))
	);

	onMount(() => {
		void refresh();
		pollHandle = window.setInterval(() => {
			if (document.visibilityState === 'visible') void refresh();
		}, 5000);
		window.addEventListener('keydown', handleKey);
	});

	onDestroy(() => {
		if (pollHandle !== null) clearInterval(pollHandle);
		if (toastTimer !== null) clearTimeout(toastTimer);
		window.removeEventListener('keydown', handleKey);
	});
</script>

<svelte:head>
	<title>Build {rid} — OpenFoundry</title>
</svelte:head>

<section class="build-detail" data-testid="build-detail">
	<header class="page-header">
		<div>
			<a href="/builds" class="back">← All builds</a>
			<h1>
				Build <code>{rid.slice(rid.lastIndexOf('.') + 1).slice(0, 12)}</code>
			</h1>
			{#if envelope}
				<div class="header-meta">
					<StateBadge kind="build" state={envelope.state} />
					<span>·</span>
					<a href={`/pipelines/${encodeURIComponent(envelope.pipeline_rid)}`}>
						<code>{envelope.pipeline_rid}</code>
					</a>
					<span>·</span>
					<span>{envelope.build_branch}</span>
					<span>·</span>
					<span>by <code>{envelope.requested_by}</code></span>
					{#if envelope.force_build}
						<span class="force-pill">⚡ Force</span>
					{/if}
				</div>
			{/if}
		</div>
		<div class="actions">
			<button
				type="button"
				onclick={abortBuild}
				disabled={!envelope || envelope.state === 'BUILD_COMPLETED' || envelope.state === 'BUILD_ABORTED' || envelope.state === 'BUILD_FAILED'}
				data-testid="build-abort"
			>
				Abort (a)
			</button>
			<button type="button" onclick={() => void rerunBuild(false)} data-testid="build-rerun">Re-run</button>
			<button
				type="button"
				class="primary"
				onclick={() => void rerunBuild(true)}
				data-testid="build-rerun-force"
			>
				Re-run with force
			</button>
		</div>
	</header>

	{#if error}
		<p class="error" data-testid="build-error">{error}</p>
	{/if}

	{#if toast}
		<div class="toast" role="status" data-testid="build-toast">{toast}</div>
	{/if}

	<nav class="tabs" data-testid="build-tabs">
		{#each ['overview', 'graph', 'jobs', 'logs', 'inputs', 'outputs', 'audit'] as tab}
			<button
				type="button"
				class:active={activeTab === tab}
				onclick={() => setTab(tab as TabId)}
				data-testid={`build-tab-${tab}`}
			>
				{tab}
			</button>
		{/each}
	</nav>

	{#if !envelope && loading}
		<p class="hint" data-testid="build-skeleton">Loading build…</p>
	{:else if !envelope}
		<p class="error">Build not found.</p>
	{:else if activeTab === 'overview'}
		<section class="kpis" data-testid="build-kpis">
			<article><h3>Total jobs</h3><strong>{kpis.total}</strong></article>
			<article><h3>Completed</h3><strong>{kpis.completed}</strong></article>
			<article><h3>Skipped (stale)</h3><strong>{kpis.skipped}</strong></article>
			<article><h3>Failed</h3><strong>{kpis.failed}</strong></article>
			<article><h3>Aborted</h3><strong>{kpis.aborted}</strong></article>
			<article>
				<h3>Wall-clock duration</h3>
				<strong>{durationLabel(envelope.started_at, envelope.finished_at)}</strong>
			</article>
			<article><h3>Datasets touched</h3><strong>{kpis.datasets}</strong></article>
		</section>
	{:else if activeTab === 'graph'}
		<section class="graph-pane" data-testid="build-graph">
			<p class="hint">
				DAG view of jobs in this build. Each node is colour-coded by JobState.
			</p>
			<div class="graph">
				{#each envelope.jobs as job (job.rid)}
					<button
						type="button"
						class="graph-node"
						onclick={() => (activeJobRid = job.rid)}
						class:selected={activeJobRid === job.rid}
						data-testid={`graph-node-${job.rid}`}
					>
						<StateBadge kind="job" state={job.state} size="sm" />
						<code>{job.rid.slice(job.rid.lastIndexOf('.') + 1).slice(0, 8)}</code>
						{#if job.stale_skipped}
							<span class="stale">stale</span>
						{/if}
					</button>
				{/each}
			</div>
		</section>
	{:else if activeTab === 'jobs'}
		<section class="jobs-pane">
			<table data-testid="build-jobs-table">
				<thead>
					<tr>
						<th>RID</th>
						<th>JobSpec</th>
						<th>State</th>
						<th>Attempt</th>
						<th>Stale skipped</th>
						<th>Output transactions</th>
						<th>State changed at</th>
					</tr>
				</thead>
				<tbody>
					{#each envelope.jobs as job (job.rid)}
						<tr data-testid={`build-jobs-row-${job.rid}`}>
							<td><code>{job.rid.slice(job.rid.lastIndexOf('.') + 1).slice(0, 12)}</code></td>
							<td><code>{job.job_spec_rid}</code></td>
							<td><StateBadge kind="job" state={job.state} /></td>
							<td>{job.attempt}</td>
							<td>{job.stale_skipped ? '✓' : ''}</td>
							<td>
								{#each job.output_transaction_rids as t (t)}
									<code>{t.slice(t.lastIndexOf('.') + 1).slice(0, 8)}</code>
								{/each}
							</td>
							<td>{new Date(job.state_changed_at).toLocaleString()}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</section>
	{:else if activeTab === 'logs'}
		<section class="logs-pane">
			<aside class="job-selector" data-testid="build-logs-job-selector">
				{#each envelope.jobs as job (job.rid)}
					<button
						type="button"
						class:active={activeJobRid === job.rid}
						onclick={() => (activeJobRid = job.rid)}
						data-testid={`build-logs-select-${job.rid}`}
					>
						<StateBadge kind="job" state={job.state} size="sm" />
						<code>{job.rid.slice(job.rid.lastIndexOf('.') + 1).slice(0, 8)}</code>
					</button>
				{/each}
			</aside>
			{#if activeJobRid}
				{@const liveStates = new Set(['WAITING', 'RUN_PENDING', 'RUNNING', 'ABORT_PENDING'])}
				{@const job = jobLookup.get(activeJobRid)}
				<LiveLogViewer
					jobRid={activeJobRid}
					mode={job && liveStates.has(job.state) ? 'live' : 'historical'}
				/>
			{:else}
				<p class="hint">Select a job to view its live logs.</p>
			{/if}
		</section>
	{:else if activeTab === 'inputs'}
		<section class="inputs-pane" data-testid="build-inputs">
			<aside class="job-selector">
				{#each envelope.jobs as job (job.rid)}
					<button
						type="button"
						class:active={activeJobRid === job.rid}
						onclick={() => {
							activeJobRid = job.rid;
							void refreshActiveJob();
						}}
					>
						<code>{job.job_spec_rid}</code>
					</button>
				{/each}
			</aside>
			{#if jobInputs}
				<table>
					<thead>
						<tr>
							<th>Dataset RID</th>
							<th>Branch</th>
							<th>Filter kind</th>
							<th>Resolution</th>
						</tr>
					</thead>
					<tbody>
						{#each jobInputs.input_view_resolutions as row (row.dataset_rid + row.branch)}
							<tr>
								<td><code>{row.dataset_rid}</code></td>
								<td>{row.branch}</td>
								<td>{row.filter.kind}</td>
								<td>
									{#if row.resolved_transaction_rid}<code>{row.resolved_transaction_rid}</code>{/if}
									{#if row.range_from_transaction_rid}from <code>{row.range_from_transaction_rid}</code>{/if}
									{#if row.range_to_transaction_rid}→ <code>{row.range_to_transaction_rid}</code>{/if}
									{#if row.note}<span class="note">{row.note}</span>{/if}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			{:else}
				<p class="hint">Pick a job to inspect its resolved inputs.</p>
			{/if}
		</section>
	{:else if activeTab === 'outputs'}
		<section class="outputs-pane" data-testid="build-outputs">
			<aside class="job-selector">
				{#each envelope.jobs as job (job.rid)}
					<button
						type="button"
						class:active={activeJobRid === job.rid}
						onclick={() => {
							activeJobRid = job.rid;
							void refreshActiveJob();
						}}
					>
						<code>{job.job_spec_rid}</code>
					</button>
				{/each}
			</aside>
			{#if jobOutputs}
				<table>
					<thead>
						<tr>
							<th>Output dataset</th>
							<th>Transaction</th>
							<th>Committed</th>
							<th>Aborted</th>
						</tr>
					</thead>
					<tbody>
						{#each jobOutputs.outputs as out (out.transaction_rid)}
							<tr>
								<td>
									<a href={`/datasets/${encodeURIComponent(out.output_dataset_rid)}`}>
										<code>{out.output_dataset_rid}</code>
									</a>
								</td>
								<td><code>{out.transaction_rid}</code></td>
								<td>{out.committed ? '✓' : ''}</td>
								<td>{out.aborted ? '✕' : ''}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			{:else}
				<p class="hint">Pick a job to inspect its outputs.</p>
			{/if}
		</section>
	{:else if activeTab === 'audit'}
		<section class="audit-pane" data-testid="build-audit">
			<p class="hint">Job lifecycle transitions for this build (queried server-side).</p>
			<table>
				<thead>
					<tr>
						<th>Job</th>
						<th>From</th>
						<th>To</th>
						<th>Reason</th>
						<th>Occurred at</th>
					</tr>
				</thead>
				<tbody>
					{#each auditRows as row, i (i)}
						<tr>
							<td><code>{row.job_rid}</code></td>
							<td>{row.from_state ?? '—'}</td>
							<td>{row.to_state}</td>
							<td>{row.reason ?? ''}</td>
							<td>{new Date(row.occurred_at).toLocaleString()}</td>
						</tr>
					{/each}
					{#if auditRows.length === 0}
						<tr><td colspan="5" class="hint">No transitions recorded yet.</td></tr>
					{/if}
				</tbody>
			</table>
		</section>
	{/if}
</section>

<style>
	.build-detail {
		display: flex;
		flex-direction: column;
		gap: 16px;
		padding: 18px 20px;
	}
	.page-header {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		gap: 16px;
	}
	.back {
		color: #94a3b8;
		font-size: 12px;
		text-decoration: none;
	}
	.page-header h1 {
		margin: 4px 0;
		font-size: 18px;
	}
	.header-meta {
		display: flex;
		gap: 8px;
		flex-wrap: wrap;
		align-items: center;
		color: #94a3b8;
		font-size: 12px;
	}
	.header-meta a {
		color: #60a5fa;
	}
	.force-pill {
		background: #b45309;
		color: #fef3c7;
		padding: 1px 8px;
		border-radius: 999px;
		font-size: 11px;
	}
	.actions {
		display: flex;
		gap: 8px;
	}
	.actions button {
		background: #1e293b;
		color: #e2e8f0;
		border: 1px solid #334155;
		border-radius: 6px;
		padding: 6px 12px;
		font: inherit;
		font-size: 12px;
		cursor: pointer;
	}
	.actions .primary {
		background: #b45309;
		border-color: #92400e;
		color: #fef3c7;
	}
	.tabs {
		display: flex;
		gap: 4px;
		border-bottom: 1px solid #1f2937;
		padding-bottom: 4px;
	}
	.tabs button {
		background: transparent;
		color: #94a3b8;
		border: none;
		border-bottom: 2px solid transparent;
		padding: 6px 12px;
		font: inherit;
		font-size: 12px;
		text-transform: capitalize;
		cursor: pointer;
	}
	.tabs button.active {
		color: #f1f5f9;
		border-bottom-color: #60a5fa;
	}
	.kpis {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
		gap: 12px;
	}
	.kpis article {
		background: #0b1220;
		border: 1px solid #1f2937;
		border-radius: 6px;
		padding: 12px 14px;
	}
	.kpis h3 {
		margin: 0;
		font-size: 11px;
		color: #94a3b8;
		font-weight: 500;
		text-transform: uppercase;
	}
	.kpis strong {
		font-size: 22px;
		color: #f1f5f9;
	}
	.graph {
		display: flex;
		flex-wrap: wrap;
		gap: 10px;
	}
	.graph-node {
		background: #0b1220;
		border: 1px solid #1f2937;
		border-radius: 6px;
		padding: 8px 10px;
		font-size: 12px;
		color: #e2e8f0;
		cursor: pointer;
		display: flex;
		gap: 6px;
		align-items: center;
	}
	.graph-node.selected {
		border-color: #60a5fa;
	}
	.graph-node .stale {
		color: #f59e0b;
		font-style: italic;
		font-size: 11px;
	}
	.jobs-pane table,
	.audit-pane table,
	.inputs-pane table,
	.outputs-pane table {
		width: 100%;
		border-collapse: collapse;
		font-size: 12px;
		background: #0b1220;
		border: 1px solid #1f2937;
		border-radius: 6px;
		overflow: hidden;
	}
	th {
		text-align: left;
		padding: 6px 10px;
		background: #111827;
		color: #94a3b8;
		font-weight: 500;
		border-bottom: 1px solid #1f2937;
	}
	td {
		padding: 6px 10px;
		border-bottom: 1px solid #111827;
		color: #e2e8f0;
	}
	td code {
		font-family: ui-monospace, 'SF Mono', Consolas, monospace;
	}
	.logs-pane,
	.inputs-pane,
	.outputs-pane {
		display: grid;
		grid-template-columns: 200px 1fr;
		gap: 12px;
	}
	.job-selector {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.job-selector button {
		background: transparent;
		color: #cbd5e1;
		border: 1px solid #1f2937;
		border-radius: 6px;
		padding: 6px 10px;
		font: inherit;
		font-size: 12px;
		text-align: left;
		cursor: pointer;
		display: flex;
		gap: 6px;
		align-items: center;
	}
	.job-selector button.active {
		border-color: #60a5fa;
	}
	.note {
		color: #94a3b8;
		font-style: italic;
		margin-left: 6px;
	}
	.hint {
		color: #94a3b8;
		font-size: 12px;
		margin: 0;
	}
	.error {
		color: #ef4444;
	}
	.toast {
		position: fixed;
		bottom: 16px;
		right: 16px;
		background: #1e293b;
		color: #e2e8f0;
		padding: 10px 14px;
		border-radius: 6px;
		border: 1px solid #334155;
		box-shadow: 0 10px 25px rgba(0, 0, 0, 0.4);
		font-size: 12px;
		z-index: 200;
	}
</style>
