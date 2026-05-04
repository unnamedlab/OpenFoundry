<!--
  DatasetBuildsBlock — Foundry-style "Builds touching this dataset"
  panel. Designed to drop into the dataset detail page (or the Quality
  dashboard) to give operators a one-click jump from any dataset into
  the dedicated Builds application (D1.1.5 closure).

  Calls `GET /v1/datasets/{rid}/builds` and renders a compact list
  with state badge + branch + duration; clicking a row navigates to
  /builds/{rid}.
-->
<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { listDatasetBuildsV1, type Build } from '$lib/api/buildsV1';
	import StateBadge from './StateBadge.svelte';

	type Props = {
		datasetRid: string;
		/** Optional cap on the number of builds shown. Defaults to 10. */
		limit?: number;
	};

	let { datasetRid, limit = 10 }: Props = $props();

	let builds = $state<Build[]>([]);
	let loading = $state<boolean>(true);
	let error = $state<string | null>(null);
	let pollHandle: number | null = null;

	function shortRid(rid: string): string {
		const dot = rid.lastIndexOf('.');
		return dot >= 0 ? rid.slice(dot + 1).slice(0, 8) : rid.slice(0, 8);
	}

	function durationLabel(b: Build): string {
		const start = b.started_at ?? b.queued_at ?? b.created_at;
		const end = b.finished_at ?? new Date().toISOString();
		const ms = Math.max(0, Date.parse(end) - Date.parse(start));
		if (ms < 1000) return `${ms}ms`;
		const s = Math.floor(ms / 1000);
		if (s < 60) return `${s}s`;
		const m = Math.floor(s / 60);
		return `${m}m ${s % 60}s`;
	}

	async function reload() {
		loading = true;
		error = null;
		try {
			const response = await listDatasetBuildsV1(datasetRid);
			builds = response.data.slice(0, limit);
		} catch (e) {
			error = String(e);
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		void reload();
		pollHandle = window.setInterval(() => {
			if (document.visibilityState === 'visible') void reload();
		}, 10_000);
	});

	onDestroy(() => {
		if (pollHandle !== null) clearInterval(pollHandle);
	});
</script>

<section class="dataset-builds" data-testid="dataset-builds-block">
	<header>
		<h4>Recent builds touching this dataset</h4>
		<a href={`/builds?pipeline_rid=&since=&until=&state=&branch=`} class="see-all">
			See all in /builds →
		</a>
	</header>

	{#if error}
		<p class="error" data-testid="dataset-builds-error">{error}</p>
	{:else if loading && builds.length === 0}
		<p class="hint">Loading…</p>
	{:else if builds.length === 0}
		<p class="hint" data-testid="dataset-builds-empty">No builds have touched this dataset yet.</p>
	{:else}
		<ul data-testid="dataset-builds-list">
			{#each builds as b (b.rid)}
				<li>
					<a href={`/builds/${encodeURIComponent(b.rid)}`} data-testid={`dataset-builds-row-${b.rid}`}>
						<code>{shortRid(b.rid)}</code>
						<StateBadge kind="build" state={b.state} size="sm" />
						<span class="branch">{b.build_branch}</span>
						<span class="duration">{durationLabel(b)}</span>
						<span class="requested-by">by <code>{b.requested_by}</code></span>
					</a>
				</li>
			{/each}
		</ul>
	{/if}
</section>

<style>
	.dataset-builds {
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding: 12px 14px;
		background: #0b1220;
		border: 1px solid #1f2937;
		border-radius: 8px;
		color: #e2e8f0;
	}
	header {
		display: flex;
		justify-content: space-between;
		align-items: center;
	}
	h4 {
		margin: 0;
		font-size: 13px;
		color: #f1f5f9;
	}
	.see-all {
		color: #60a5fa;
		font-size: 11px;
		text-decoration: none;
	}
	ul {
		list-style: none;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	li a {
		display: grid;
		grid-template-columns: auto auto 1fr auto auto;
		gap: 10px;
		align-items: center;
		padding: 6px 8px;
		border-radius: 4px;
		font-size: 12px;
		color: #e2e8f0;
		text-decoration: none;
	}
	li a:hover {
		background: #111827;
	}
	li a code {
		font-family: ui-monospace, monospace;
	}
	.branch {
		color: #cbd5e1;
	}
	.duration {
		color: #94a3b8;
	}
	.requested-by {
		color: #94a3b8;
	}
	.hint {
		color: #94a3b8;
		font-size: 12px;
		margin: 0;
		font-style: italic;
	}
	.error {
		color: #ef4444;
		font-size: 12px;
		margin: 0;
	}
</style>
