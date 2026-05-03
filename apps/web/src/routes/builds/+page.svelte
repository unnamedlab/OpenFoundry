<!--
  /builds — Foundry "Builds application" landing.

  Lists every build across the platform (Foundry doc § Application
  reference: "Builds application — formerly called Job Tracker —
  allows you to view all builds occurring across Foundry…"). URL-sync
  filters, badge-coloured state column, "Run build" modal that hits
  POST /v1/builds.
-->
<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import {
		listBuildsV1,
		runBuildV1,
		type Build,
		type BuildState,
		type CreateBuildRequest
	} from '$lib/api/buildsV1';
	import StateBadge from '$lib/components/builds/StateBadge.svelte';

	const ALL_STATES: BuildState[] = [
		'BUILD_RESOLUTION',
		'BUILD_QUEUED',
		'BUILD_RUNNING',
		'BUILD_ABORTING',
		'BUILD_FAILED',
		'BUILD_ABORTED',
		'BUILD_COMPLETED'
	];

	let builds = $state<Build[]>([]);
	let loading = $state<boolean>(true);
	let error = $state<string | null>(null);
	let nextCursor = $state<string | null>(null);

	// Filters (URL-sync via $page.url.searchParams).
	let branch = $state<string>('');
	let pipelineRid = $state<string>('');
	let requestedBy = $state<string>('');
	let activeStates = $state<Set<BuildState>>(new Set());
	let since = $state<string>('');
	let until = $state<string>('');
	let hasFailuresOnly = $state<boolean>(false);

	// Run-build modal.
	let showRunModal = $state<boolean>(false);
	let runForm = $state<CreateBuildRequest>({
		pipeline_rid: '',
		build_branch: 'master',
		output_dataset_rids: [],
		force_build: false,
		abort_policy: 'DEPENDENT_ONLY',
		trigger_kind: 'MANUAL'
	});
	let runFormOutputsRaw = $state<string>('');
	let runError = $state<string | null>(null);
	let runBusy = $state<boolean>(false);

	let pollHandle: number | null = null;

	function loadFromUrl() {
		const sp = $page.url.searchParams;
		branch = sp.get('branch') ?? '';
		pipelineRid = sp.get('pipeline_rid') ?? '';
		requestedBy = sp.get('requested_by') ?? '';
		since = sp.get('since') ?? '';
		until = sp.get('until') ?? '';
		hasFailuresOnly = sp.get('failures') === '1';
		const states = sp.get('state');
		activeStates = new Set(
			states ? (states.split(',').filter((s) => ALL_STATES.includes(s as BuildState)) as BuildState[]) : []
		);
	}

	async function reload() {
		loading = true;
		error = null;
		try {
			// One request per active state filter (server only takes a
			// single state). UI dedupes locally.
			const requests =
				activeStates.size > 0
					? Array.from(activeStates).map((s) =>
							listBuildsV1({
								branch: branch || undefined,
								pipeline_rid: pipelineRid || undefined,
								status: s,
								since: since || undefined,
								until: until || undefined,
								limit: 200
							})
						)
					: [
							listBuildsV1({
								branch: branch || undefined,
								pipeline_rid: pipelineRid || undefined,
								since: since || undefined,
								until: until || undefined,
								limit: 200
							})
						];
			const results = await Promise.all(requests);
			const merged = new Map<string, Build>();
			for (const r of results) {
				for (const b of r.data) merged.set(b.rid, b);
			}
			let rows = Array.from(merged.values()).sort(
				(a, b) => Date.parse(b.created_at) - Date.parse(a.created_at)
			);
			if (requestedBy) {
				rows = rows.filter((b) => b.requested_by === requestedBy);
			}
			if (hasFailuresOnly) {
				rows = rows.filter((b) => b.state === 'BUILD_FAILED');
			}
			builds = rows;
			nextCursor = results[0]?.next_cursor ?? null;
		} catch (e) {
			error = String(e);
		} finally {
			loading = false;
		}
	}

	function syncUrl() {
		const sp = new URLSearchParams();
		if (branch) sp.set('branch', branch);
		if (pipelineRid) sp.set('pipeline_rid', pipelineRid);
		if (requestedBy) sp.set('requested_by', requestedBy);
		if (since) sp.set('since', since);
		if (until) sp.set('until', until);
		if (activeStates.size > 0) sp.set('state', Array.from(activeStates).join(','));
		if (hasFailuresOnly) sp.set('failures', '1');
		void goto(`?${sp.toString()}`, { replaceState: true, keepFocus: true, noScroll: true });
	}

	function toggleState(state: BuildState) {
		const next = new Set(activeStates);
		if (next.has(state)) next.delete(state);
		else next.add(state);
		activeStates = next;
		syncUrl();
		void reload();
	}

	function applyFilters() {
		syncUrl();
		void reload();
	}

	function clearFilters() {
		branch = '';
		pipelineRid = '';
		requestedBy = '';
		since = '';
		until = '';
		activeStates = new Set();
		hasFailuresOnly = false;
		syncUrl();
		void reload();
	}

	function exportCsv() {
		const header = [
			'rid',
			'pipeline_rid',
			'branch',
			'state',
			'force',
			'trigger_kind',
			'queued_at',
			'started_at',
			'finished_at',
			'requested_by'
		];
		const rows = builds.map((b) => [
			b.rid,
			b.pipeline_rid,
			b.build_branch,
			b.state,
			String(b.force_build),
			b.trigger_kind,
			b.queued_at ?? '',
			b.started_at ?? '',
			b.finished_at ?? '',
			b.requested_by
		]);
		const csv = [header, ...rows].map((r) => r.map((v) => `"${String(v).replace(/"/g, '""')}"`).join(',')).join('\n');
		const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = `builds-${new Date().toISOString()}.csv`;
		a.click();
		URL.revokeObjectURL(url);
	}

	async function submitRunBuild() {
		runError = null;
		runBusy = true;
		try {
			const outputs = runFormOutputsRaw
				.split(/[\s,]+/)
				.map((s) => s.trim())
				.filter(Boolean);
			if (outputs.length === 0) {
				runError = 'output_dataset_rids must declare at least one dataset';
				return;
			}
			const created = await runBuildV1({ ...runForm, output_dataset_rids: outputs });
			showRunModal = false;
			await reload();
			// Deep-link into the new build.
			void goto(`/builds/ri.foundry.main.build.${created.build_id}`);
		} catch (e) {
			runError = String(e);
		} finally {
			runBusy = false;
		}
	}

	function durationLabel(b: Build): string {
		const start = b.started_at ?? b.queued_at ?? b.created_at;
		const end = b.finished_at ?? new Date().toISOString();
		const ms = Math.max(0, Date.parse(end) - Date.parse(start));
		if (ms < 1000) return `${ms}ms`;
		const s = Math.floor(ms / 1000);
		if (s < 60) return `${s}s`;
		const m = Math.floor(s / 60);
		const r = s % 60;
		return `${m}m ${r}s`;
	}

	function shortRid(rid: string): string {
		const dot = rid.lastIndexOf('.');
		return dot >= 0 ? rid.slice(dot + 1).slice(0, 8) : rid.slice(0, 8);
	}

	function handleKey(e: KeyboardEvent) {
		const tag = (e.target as HTMLElement | null)?.tagName ?? '';
		if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
		if (e.key === 'r' && !e.metaKey && !e.ctrlKey) {
			e.preventDefault();
			showRunModal = true;
		}
		if (e.key === '/') {
			e.preventDefault();
			(document.getElementById('builds-search-pipeline') as HTMLInputElement | null)?.focus();
		}
	}

	onMount(() => {
		loadFromUrl();
		void reload();
		pollHandle = window.setInterval(() => {
			if (document.visibilityState === 'visible') void reload();
		}, 5000);
		window.addEventListener('keydown', handleKey);
	});

	onDestroy(() => {
		if (pollHandle !== null) clearInterval(pollHandle);
		window.removeEventListener('keydown', handleKey);
	});
</script>

<svelte:head>
	<title>Builds — OpenFoundry</title>
</svelte:head>

<section class="builds-app" data-testid="builds-app">
	<header class="page-header">
		<div>
			<h1>Builds</h1>
			<p class="subtitle">
				Foundry-style cross-pipeline build queue. Inspect every build, drill into the
				job graph, follow live logs.
			</p>
		</div>
		<div class="actions">
			<button type="button" class="primary" onclick={() => (showRunModal = true)} data-testid="builds-run-button">
				<span aria-hidden="true">▶</span> Run build
			</button>
			<button type="button" onclick={() => void reload()} data-testid="builds-refresh-button">Refresh</button>
			<button type="button" onclick={exportCsv} disabled={builds.length === 0} data-testid="builds-csv-button">
				Export CSV
			</button>
		</div>
	</header>

	<section class="filters" data-testid="builds-filters">
		<div class="filter">
			<label for="builds-search-pipeline">Pipeline RID</label>
			<input
				id="builds-search-pipeline"
				type="search"
				bind:value={pipelineRid}
				placeholder="ri.foundry.main.pipeline.…"
				onkeyup={(e) => e.key === 'Enter' && applyFilters()}
				data-testid="builds-filter-pipeline"
			/>
		</div>
		<div class="filter">
			<label for="builds-branch">Branch</label>
			<input
				id="builds-branch"
				type="text"
				bind:value={branch}
				placeholder="master"
				onkeyup={(e) => e.key === 'Enter' && applyFilters()}
				data-testid="builds-filter-branch"
			/>
		</div>
		<div class="filter">
			<label for="builds-requestedby">Requested by</label>
			<input id="builds-requestedby" type="text" bind:value={requestedBy} data-testid="builds-filter-requested-by" />
		</div>
		<div class="filter">
			<label for="builds-since">Since</label>
			<input id="builds-since" type="datetime-local" bind:value={since} data-testid="builds-filter-since" />
		</div>
		<div class="filter">
			<label for="builds-until">Until</label>
			<input id="builds-until" type="datetime-local" bind:value={until} data-testid="builds-filter-until" />
		</div>
		<div class="filter">
			<label>
				<input type="checkbox" bind:checked={hasFailuresOnly} data-testid="builds-filter-failures-only" />
				Failures only
			</label>
		</div>
		<div class="filter spread">
			<span class="hint">State</span>
			<div class="state-chips">
				{#each ALL_STATES as state (state)}
					<button
						type="button"
						class="state-chip"
						class:active={activeStates.has(state)}
						onclick={() => toggleState(state)}
						data-testid={`builds-filter-state-${state}`}
					>
						<StateBadge kind="build" {state} size="sm" />
					</button>
				{/each}
			</div>
		</div>
		<div class="filter actions-row">
			<button type="button" onclick={applyFilters} data-testid="builds-filter-apply">Apply</button>
			<button type="button" onclick={clearFilters} data-testid="builds-filter-clear">Clear</button>
		</div>
	</section>

	{#if error}
		<p class="error" data-testid="builds-error">{error}</p>
	{/if}

	<div class="table-wrap">
		<table class="builds" data-testid="builds-table">
			<thead>
				<tr>
					<th>RID</th>
					<th>Pipeline</th>
					<th>Branch</th>
					<th>State</th>
					<th>Force</th>
					<th>Trigger</th>
					<th>Queued</th>
					<th>Started</th>
					<th>Finished</th>
					<th>Duration</th>
					<th>Requested by</th>
				</tr>
			</thead>
			<tbody>
				{#if loading && builds.length === 0}
					{#each Array(8) as _, i (i)}
						<tr class="skeleton" aria-hidden="true">
							{#each Array(11) as _2, j (j)}
								<td><span class="sk-cell"></span></td>
							{/each}
						</tr>
					{/each}
				{:else if builds.length === 0}
					<tr>
						<td colspan="11" class="empty">
							<div class="empty-cta" data-testid="builds-empty">
								<p>No builds match the current filters.</p>
								<a href="/pipelines" class="cta">Open Pipeline Builder →</a>
							</div>
						</td>
					</tr>
				{:else}
					{#each builds as b (b.rid)}
						<tr data-testid={`builds-row-${b.rid}`}>
							<td>
								<a href={`/builds/${encodeURIComponent(b.rid)}`} title={b.rid}>
									<code>{shortRid(b.rid)}</code>
								</a>
							</td>
							<td>
								<a href={`/pipelines/${encodeURIComponent(b.pipeline_rid)}`} title={b.pipeline_rid}>
									<code>{shortRid(b.pipeline_rid)}</code>
								</a>
							</td>
							<td>{b.build_branch}</td>
							<td><StateBadge kind="build" state={b.state} /></td>
							<td>{b.force_build ? '⚡' : ''}</td>
							<td>{b.trigger_kind}</td>
							<td>{b.queued_at ? new Date(b.queued_at).toLocaleTimeString() : '—'}</td>
							<td>{b.started_at ? new Date(b.started_at).toLocaleTimeString() : '—'}</td>
							<td>{b.finished_at ? new Date(b.finished_at).toLocaleTimeString() : '—'}</td>
							<td>{durationLabel(b)}</td>
							<td><code>{b.requested_by}</code></td>
						</tr>
					{/each}
				{/if}
			</tbody>
		</table>
	</div>

	{#if showRunModal}
		<div
			class="modal-backdrop"
			role="presentation"
			onclick={() => (showRunModal = false)}
			data-testid="builds-run-modal-backdrop"
		>
			<div
				class="modal"
				role="dialog"
				aria-modal="true"
				aria-labelledby="run-build-title"
				onclick={(e) => e.stopPropagation()}
				data-testid="builds-run-modal"
			>
				<h2 id="run-build-title">Run build</h2>
				<form
					onsubmit={(e) => {
						e.preventDefault();
						void submitRunBuild();
					}}
				>
					<label>
						Pipeline RID
						<input
							type="text"
							required
							bind:value={runForm.pipeline_rid}
							placeholder="ri.foundry.main.pipeline.…"
							data-testid="builds-run-pipeline-rid"
						/>
					</label>
					<label>
						Build branch
						<input type="text" required bind:value={runForm.build_branch} data-testid="builds-run-branch" />
					</label>
					<label>
						Output dataset RIDs (comma-separated)
						<textarea bind:value={runFormOutputsRaw} data-testid="builds-run-outputs" required></textarea>
					</label>
					<label>
						<input
							type="checkbox"
							bind:checked={runForm.force_build}
							data-testid="builds-run-force-build"
						/>
						Force build (recompute every output regardless of staleness)
					</label>
					<fieldset>
						<legend>Abort policy</legend>
						<label>
							<input
								type="radio"
								name="abort_policy"
								value="DEPENDENT_ONLY"
								checked={runForm.abort_policy === 'DEPENDENT_ONLY'}
								onchange={() => (runForm.abort_policy = 'DEPENDENT_ONLY')}
								data-testid="builds-run-abort-dependent-only"
							/>
							Dependent only (Foundry default)
						</label>
						<label>
							<input
								type="radio"
								name="abort_policy"
								value="ALL_NON_DEPENDENT"
								checked={runForm.abort_policy === 'ALL_NON_DEPENDENT'}
								onchange={() => (runForm.abort_policy = 'ALL_NON_DEPENDENT')}
								data-testid="builds-run-abort-all"
							/>
							All non-dependent
						</label>
					</fieldset>
					{#if runError}
						<p class="error" data-testid="builds-run-error">{runError}</p>
					{/if}
					<div class="actions">
						<button type="button" onclick={() => (showRunModal = false)}>Cancel</button>
						<button type="submit" class="primary" disabled={runBusy} data-testid="builds-run-submit">
							{runBusy ? 'Submitting…' : 'Run build'}
						</button>
					</div>
				</form>
			</div>
		</div>
	{/if}
</section>

<style>
	.builds-app {
		display: flex;
		flex-direction: column;
		gap: 16px;
		padding: 18px 20px;
	}
	.page-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 16px;
	}
	.page-header h1 {
		margin: 0 0 4px;
		font-size: 22px;
	}
	.page-header .subtitle {
		margin: 0;
		color: #94a3b8;
		font-size: 13px;
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
		font-size: 13px;
		cursor: pointer;
	}
	.actions .primary {
		background: #2563eb;
		border-color: #1d4ed8;
		color: white;
	}
	.actions .primary:hover {
		background: #1d4ed8;
	}
	.filters {
		display: flex;
		flex-wrap: wrap;
		gap: 12px;
		padding: 12px 14px;
		background: #0b1220;
		border: 1px solid #1f2937;
		border-radius: 6px;
		align-items: end;
	}
	.filter {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-width: 140px;
	}
	.filter.spread {
		flex: 1 1 100%;
	}
	.filter .hint {
		font-size: 11px;
		color: #94a3b8;
	}
	.filter label {
		font-size: 11px;
		color: #94a3b8;
		display: flex;
		align-items: center;
		gap: 6px;
	}
	.filter input[type='text'],
	.filter input[type='search'],
	.filter input[type='datetime-local'] {
		background: #111827;
		color: #e2e8f0;
		border: 1px solid #334155;
		border-radius: 4px;
		padding: 6px 8px;
		font: inherit;
		font-size: 12px;
	}
	.actions-row {
		flex-direction: row;
		gap: 6px;
	}
	.state-chips {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.state-chip {
		background: transparent;
		border: 1px solid transparent;
		border-radius: 999px;
		padding: 0;
		cursor: pointer;
		opacity: 0.5;
	}
	.state-chip.active {
		opacity: 1;
		border-color: #94a3b8;
	}
	.error {
		color: #ef4444;
		margin: 0;
	}
	.table-wrap {
		overflow-x: auto;
		border: 1px solid #1f2937;
		border-radius: 6px;
	}
	table.builds {
		width: 100%;
		border-collapse: collapse;
		font-size: 12px;
		min-width: 1100px;
	}
	table.builds th {
		text-align: left;
		padding: 8px 10px;
		background: #0b1220;
		color: #94a3b8;
		font-weight: 500;
		border-bottom: 1px solid #1f2937;
	}
	table.builds td {
		padding: 7px 10px;
		border-bottom: 1px solid #111827;
		color: #e2e8f0;
	}
	table.builds tbody tr:hover {
		background: #111827;
	}
	table.builds code {
		font-family: ui-monospace, 'SF Mono', Consolas, monospace;
	}
	tr.skeleton .sk-cell {
		display: inline-block;
		width: 80%;
		height: 10px;
		background: #1f2937;
		border-radius: 3px;
		animation: skeleton 1.4s ease infinite;
	}
	@keyframes skeleton {
		0%, 100% { opacity: 0.5; }
		50% { opacity: 1; }
	}
	.empty {
		text-align: center;
	}
	.empty-cta {
		padding: 32px;
		color: #94a3b8;
	}
	.empty-cta .cta {
		color: #60a5fa;
		text-decoration: none;
	}
	.modal-backdrop {
		position: fixed;
		inset: 0;
		background: rgba(0, 0, 0, 0.5);
		display: flex;
		justify-content: center;
		align-items: center;
		z-index: 100;
	}
	.modal {
		background: #0b1220;
		border: 1px solid #1f2937;
		border-radius: 8px;
		padding: 18px 20px;
		max-width: 560px;
		width: 100%;
	}
	.modal h2 {
		margin: 0 0 12px;
		font-size: 16px;
	}
	.modal form {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}
	.modal label {
		display: flex;
		flex-direction: column;
		gap: 4px;
		font-size: 12px;
		color: #cbd5e1;
	}
	.modal input,
	.modal textarea {
		background: #111827;
		color: #e2e8f0;
		border: 1px solid #334155;
		border-radius: 4px;
		padding: 6px 8px;
		font: inherit;
		font-size: 12px;
	}
	.modal textarea {
		min-height: 80px;
		font-family: ui-monospace, 'SF Mono', Consolas, monospace;
	}
	.modal fieldset {
		border: 1px solid #1f2937;
		border-radius: 6px;
		padding: 8px 12px;
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.modal fieldset legend {
		color: #94a3b8;
		font-size: 11px;
		padding: 0 4px;
	}
</style>
