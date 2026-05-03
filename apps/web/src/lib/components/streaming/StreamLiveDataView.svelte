<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { previewStream, type PreviewRow } from '$lib/api/streaming';

	export let streamId: string;

	type Mode = 'live' | 'recent' | 'historical';
	let mode: Mode = 'live';
	let limit = 50;
	let rows: PreviewRow[] = [];
	let aggregateSource: 'hot' | 'cold' | 'hybrid' = 'hot';
	let error = '';
	let loading = false;

	let pollTimer: ReturnType<typeof setInterval> | null = null;
	let socket: WebSocket | null = null;

	function modeToServerMode(m: Mode): 'oldest' | 'hot_only' | 'cold_only' {
		switch (m) {
			case 'live':
				return 'hot_only';
			case 'recent':
				return 'oldest';
			case 'historical':
				return 'cold_only';
		}
	}

	async function refresh() {
		loading = true;
		error = '';
		try {
			const res = await previewStream(streamId, {
				mode: modeToServerMode(mode),
				limit
			});
			rows = res.data;
			aggregateSource = res.source;
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		} finally {
			loading = false;
		}
	}

	function openWebSocket() {
		closeWebSocket();
		try {
			const proto = typeof window !== 'undefined' && window.location.protocol === 'https:' ? 'wss' : 'ws';
			const host = typeof window !== 'undefined' ? window.location.host : '';
			socket = new WebSocket(
				`${proto}://${host}/api/v1/streaming/streams/${streamId}/live`
			);
			socket.onmessage = (ev) => {
				try {
					const incoming = JSON.parse(ev.data) as PreviewRow;
					const tagged: PreviewRow = { ...incoming, source: 'hot' };
					rows = [tagged, ...rows].slice(0, limit);
					aggregateSource = 'hot';
				} catch (err) {
					console.warn('live frame parse failed', err);
				}
			};
			socket.onerror = (ev) => {
				console.warn('live socket error', ev);
			};
		} catch (err) {
			console.warn('live socket open failed', err);
		}
	}

	function closeWebSocket() {
		if (socket) {
			try {
				socket.close();
			} catch {
				/* swallow */
			}
			socket = null;
		}
	}

	function startPolling() {
		stopPolling();
		// Polling fallback for the Recent tab and as a complement to
		// the Live socket (the proxy may not be deployed in dev).
		pollTimer = setInterval(refresh, mode === 'live' ? 5_000 : 15_000);
	}

	function stopPolling() {
		if (pollTimer) {
			clearInterval(pollTimer);
			pollTimer = null;
		}
	}

	function setMode(next: Mode) {
		mode = next;
		closeWebSocket();
		stopPolling();
		refresh();
		if (mode === 'live') {
			openWebSocket();
			startPolling();
		} else if (mode === 'recent') {
			startPolling();
		}
	}

	onMount(() => {
		setMode('live');
	});

	onDestroy(() => {
		closeWebSocket();
		stopPolling();
	});

	function badgeClass(source: 'hot' | 'cold'): string {
		return source === 'hot' ? 'live' : 'archived';
	}

	function badgeLabel(source: 'hot' | 'cold'): string {
		return source === 'hot' ? 'live' : 'archived';
	}
</script>

<section class="stream-live-data" data-testid="stream-live-data">
	<div class="toggle-row" role="tablist" aria-label="View mode">
		<button
			type="button"
			role="tab"
			class:active={mode === 'live'}
			on:click={() => setMode('live')}
			data-testid="view-live"
		>Live</button>
		<button
			type="button"
			role="tab"
			class:active={mode === 'recent'}
			on:click={() => setMode('recent')}
			data-testid="view-recent"
		>Recent</button>
		<button
			type="button"
			role="tab"
			class:active={mode === 'historical'}
			on:click={() => setMode('historical')}
			data-testid="view-historical"
		>Historical</button>
		<span class="aggregate-source" data-testid="aggregate-source">
			source: <strong>{aggregateSource}</strong>
		</span>
	</div>

	{#if error}<p class="error">{error}</p>{/if}
	{#if loading && rows.length === 0}
		<p class="hint">Loading…</p>
	{/if}

	<table data-testid="preview-table">
		<thead>
			<tr><th>Time</th><th>Sequence</th><th>Source</th><th>Payload</th></tr>
		</thead>
		<tbody>
			{#each rows as row, idx (idx)}
				<tr>
					<td>{new Date(row.event_time).toLocaleString()}</td>
					<td>{row.sequence_no ?? '—'}</td>
					<td>
						<span
							class="badge {badgeClass(row.source)}"
							data-testid={`row-badge-${row.source}`}
						>
							{badgeLabel(row.source)}
						</span>
					</td>
					<td>
						<pre>{JSON.stringify(row.payload, null, 2)}</pre>
					</td>
				</tr>
			{:else}
				<tr><td colspan="4" class="hint">No records.</td></tr>
			{/each}
		</tbody>
	</table>
</section>

<style>
	.stream-live-data { display: grid; gap: 0.75rem; }
	.toggle-row { display: flex; gap: 0.4rem; align-items: center; }
	.toggle-row button {
		background: #f3f4f6; border: 1px solid #ddd; border-radius: 4px;
		padding: 0.25rem 0.65rem; cursor: pointer; font-size: 0.85rem;
	}
	.toggle-row button.active { background: #246; color: #fff; border-color: #246; }
	.aggregate-source { margin-left: auto; font-size: 0.8rem; color: #555; }
	table { width: 100%; border-collapse: collapse; font-size: 0.85rem; }
	th, td { text-align: left; padding: 0.35rem 0.6rem; border-bottom: 1px solid #eee; vertical-align: top; }
	pre { margin: 0; white-space: pre-wrap; font-family: ui-monospace, monospace; }
	.badge { padding: 0.05rem 0.45rem; border-radius: 3px; font-size: 0.7rem; font-weight: 600; }
	.badge.live { background: #d4f4dd; color: #1f5631; }
	.badge.archived { background: #e5e7eb; color: #4b5563; }
	.error { color: #b00; }
	.hint { color: #666; }
</style>
