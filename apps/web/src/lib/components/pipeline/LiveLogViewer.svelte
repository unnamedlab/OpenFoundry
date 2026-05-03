<!--
  LiveLogViewer — Foundry "Live logs" panel (Builds.md § Live logs).

  Two modes:
    * `live`       — subscribes to `/v1/jobs/{rid}/logs/stream` (SSE).
                     The first 10 seconds surface a "Initializing…"
                     badge that decrements with the heartbeat events
                     emitted by the backend, mirroring the doc's
                     "ten-second delay" caveat.
    * `historical` — pulls `/v1/jobs/{rid}/logs` once (paginated) for
                     terminal jobs. No SSE / WS so the panel works on
                     archived runs.

  Color coding follows the doc verbatim:
    * INFO         → blue
    * WARN         → orange
    * ERROR/FATAL  → red
    * DEBUG/TRACE  → gray
  Each row that carries `params` exposes a "Format as JSON" toggle that
  pretty-prints the safe-parameters block.
-->
<script lang="ts" module>
	export type LiveLogEntry = {
		sequence: number;
		ts: string;
		level: 'TRACE' | 'DEBUG' | 'INFO' | 'WARN' | 'ERROR' | 'FATAL';
		message: string;
		params?: Record<string, unknown> | null;
	};

	export type LiveLogMode = 'live' | 'historical';

	export type LevelFilter = LiveLogEntry['level'];
</script>

<script lang="ts">
	import { onDestroy, onMount } from 'svelte';

	type Props = {
		jobRid: string;
		mode?: LiveLogMode;
	};

	let { jobRid, mode = 'live' }: Props = $props();

	const ALL_LEVELS: LevelFilter[] = ['TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'FATAL'];

	let entries = $state<LiveLogEntry[]>([]);
	let lastSequenceSeen = $state<number>(0);
	let paused = $state<boolean>(false);
	let initializingSecondsRemaining = $state<number | null>(null);
	let connectionError = $state<string | null>(null);
	let activeLevels = $state<Set<LevelFilter>>(new Set(ALL_LEVELS));
	let search = $state<string>('');
	let expanded = $state<Set<number>>(new Set());

	let eventSource: EventSource | null = null;

	const COLOR: Record<LevelFilter, string> = {
		INFO: '#3B82F6',
		WARN: '#F59E0B',
		ERROR: '#EF4444',
		FATAL: '#EF4444',
		DEBUG: '#6B7280',
		TRACE: '#6B7280'
	};

	function levelColor(level: LevelFilter): string {
		return COLOR[level] ?? '#94a3b8';
	}

	function appendEntry(entry: LiveLogEntry) {
		entries = [...entries, entry];
		if (entry.sequence > lastSequenceSeen) {
			lastSequenceSeen = entry.sequence;
		}
		// Cap buffer size so a 10k+ log run doesn't OOM the page.
		if (entries.length > 5000) {
			entries = entries.slice(-3000);
		}
	}

	function connectLive() {
		if (mode !== 'live' || paused) return;
		const url = `/v1/jobs/${encodeURIComponent(jobRid)}/logs/stream?from_sequence=${lastSequenceSeen}`;
		try {
			eventSource = new EventSource(url, { withCredentials: true });
		} catch (err) {
			connectionError = String(err);
			return;
		}
		connectionError = null;

		eventSource.addEventListener('heartbeat', (event: MessageEvent) => {
			try {
				const payload = JSON.parse(event.data) as {
					phase: string;
					delay_remaining_seconds: number;
				};
				if (payload.phase === 'initializing') {
					initializingSecondsRemaining = payload.delay_remaining_seconds;
				}
			} catch {
				/* ignore malformed heartbeat */
			}
		});

		eventSource.addEventListener('log', (event: MessageEvent) => {
			try {
				const entry = JSON.parse(event.data) as LiveLogEntry;
				if (paused) return;
				initializingSecondsRemaining = null;
				appendEntry(entry);
			} catch {
				/* ignore malformed entry */
			}
		});

		eventSource.onerror = () => {
			connectionError = 'Disconnected — retrying with last_sequence_seen.';
			eventSource?.close();
			eventSource = null;
			if (!paused) {
				// Auto-reconnect with backoff.
				setTimeout(() => connectLive(), 1500);
			}
		};
	}

	async function loadHistory() {
		try {
			const url = `/v1/jobs/${encodeURIComponent(jobRid)}/logs?limit=1000`;
			const res = await fetch(url, { credentials: 'include' });
			if (!res.ok) {
				connectionError = `failed to load history: ${res.status}`;
				return;
			}
			const body = (await res.json()) as { data: LiveLogEntry[] };
			entries = body.data ?? [];
			if (entries.length > 0) {
				lastSequenceSeen = entries[entries.length - 1].sequence;
			}
		} catch (err) {
			connectionError = String(err);
		}
	}

	function pause() {
		paused = true;
		eventSource?.close();
		eventSource = null;
	}

	function resume() {
		paused = false;
		connectLive();
	}

	function toggleLevel(level: LevelFilter) {
		const next = new Set(activeLevels);
		if (next.has(level)) {
			next.delete(level);
		} else {
			next.add(level);
		}
		activeLevels = next;
	}

	function toggleExpanded(seq: number) {
		const next = new Set(expanded);
		if (next.has(seq)) {
			next.delete(seq);
		} else {
			next.add(seq);
		}
		expanded = next;
	}

	let visible = $derived(
		entries.filter((e) => {
			if (!activeLevels.has(e.level)) return false;
			if (search.trim()) {
				const needle = search.trim().toLowerCase();
				if (!e.message.toLowerCase().includes(needle)) return false;
			}
			return true;
		})
	);

	onMount(() => {
		if (mode === 'live') {
			connectLive();
		} else {
			void loadHistory();
		}
	});

	onDestroy(() => {
		eventSource?.close();
		eventSource = null;
	});
</script>

<section class="live-logs" data-testid="live-log-viewer">
	<header>
		<h4>Live logs <code>{jobRid.slice(0, 32)}</code></h4>
		<div class="actions">
			{#if mode === 'live'}
				{#if paused}
					<button type="button" data-testid="live-logs-resume" onclick={resume}>Resume</button>
				{:else}
					<button type="button" data-testid="live-logs-pause" onclick={pause}>Pause</button>
				{/if}
			{/if}
			<input
				type="search"
				placeholder="Search log message"
				bind:value={search}
				data-testid="live-logs-search"
			/>
		</div>
	</header>

	<p class="hint" data-testid="live-logs-banner">
		Live logs are streamed in real-time. Time range filters do not apply.
	</p>

	{#if mode === 'live' && initializingSecondsRemaining !== null && initializingSecondsRemaining > 0}
		<p class="initializing" data-testid="live-logs-initializing">
			Initializing — logs will appear in ~{initializingSecondsRemaining}s
		</p>
	{/if}
	{#if connectionError}
		<p class="error" data-testid="live-logs-error">{connectionError}</p>
	{/if}

	<div class="filter-row" data-testid="live-logs-filters">
		{#each ALL_LEVELS as level (level)}
			<button
				type="button"
				class="chip"
				class:active={activeLevels.has(level)}
				style:--chip-color={levelColor(level)}
				data-testid={`live-logs-filter-${level}`}
				onclick={() => toggleLevel(level)}
			>
				<span class="chip-dot"></span>
				{level}
			</button>
		{/each}
	</div>

	<ul class="rows" data-testid="live-logs-rows">
		{#each visible as entry (entry.sequence)}
			<li data-testid={`live-logs-row-${entry.sequence}`} data-level={entry.level}>
				<span class="ts" title={entry.ts}>{new Date(entry.ts).toISOString().slice(11, 23)}</span>
				<span class="level" style:color={levelColor(entry.level)}>{entry.level}</span>
				<span class="msg">{entry.message}</span>
				{#if entry.params}
					<button
						type="button"
						class="toggle-json"
						data-testid={`live-logs-json-toggle-${entry.sequence}`}
						onclick={() => toggleExpanded(entry.sequence)}
					>
						{expanded.has(entry.sequence) ? '▾ Hide JSON' : '▸ Format as JSON'}
					</button>
					{#if expanded.has(entry.sequence)}
						<pre
							class="params"
							data-testid={`live-logs-json-${entry.sequence}`}>{JSON.stringify(entry.params, null, 2)}</pre>
					{/if}
				{/if}
			</li>
		{/each}
		{#if visible.length === 0}
			<li class="empty" data-testid="live-logs-empty">No log entries match the current filters.</li>
		{/if}
	</ul>
</section>

<style>
	.live-logs {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
		background: #0b1220;
		color: #e2e8f0;
		border: 1px solid #1f2937;
		border-radius: 8px;
		padding: 0.75rem 1rem;
		font-family: ui-monospace, 'SF Mono', Consolas, monospace;
	}
	header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 0.5rem;
	}
	h4 {
		margin: 0;
		font-size: 13px;
		color: #f1f5f9;
	}
	.actions {
		display: flex;
		gap: 0.5rem;
		align-items: center;
	}
	.actions button {
		background: #1e293b;
		color: #e2e8f0;
		border: 1px solid #334155;
		border-radius: 4px;
		padding: 4px 10px;
		cursor: pointer;
		font: inherit;
		font-size: 12px;
	}
	.actions input {
		background: #111827;
		color: #e2e8f0;
		border: 1px solid #334155;
		border-radius: 4px;
		padding: 4px 8px;
		font: inherit;
		font-size: 12px;
	}
	.hint {
		color: #94a3b8;
		font-style: italic;
		margin: 0;
		font-size: 11px;
	}
	.initializing {
		color: #f59e0b;
		margin: 0;
		font-size: 12px;
	}
	.error {
		color: #ef4444;
		margin: 0;
		font-size: 12px;
	}
	.filter-row {
		display: flex;
		flex-wrap: wrap;
		gap: 0.4rem;
	}
	.chip {
		display: inline-flex;
		align-items: center;
		gap: 0.3rem;
		padding: 2px 8px;
		border-radius: 999px;
		border: 1px solid #334155;
		background: #111827;
		color: #94a3b8;
		font: inherit;
		font-size: 11px;
		cursor: pointer;
	}
	.chip.active {
		border-color: var(--chip-color);
		color: var(--chip-color);
	}
	.chip-dot {
		display: inline-block;
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--chip-color);
	}
	.rows {
		list-style: none;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
		max-height: 360px;
		overflow-y: auto;
	}
	.rows li {
		display: grid;
		grid-template-columns: auto auto 1fr auto;
		gap: 0.5rem;
		font-size: 12px;
		line-height: 1.4;
		padding: 1px 4px;
	}
	.rows li.empty {
		grid-template-columns: 1fr;
		color: #94a3b8;
		font-style: italic;
		font-size: 12px;
	}
	.ts {
		color: #94a3b8;
	}
	.level {
		font-weight: 700;
		min-width: 3.5em;
	}
	.msg {
		white-space: pre-wrap;
		word-break: break-word;
	}
	.toggle-json {
		grid-column: 1 / -1;
		justify-self: start;
		background: transparent;
		color: #60a5fa;
		border: none;
		padding: 0;
		font: inherit;
		font-size: 11px;
		cursor: pointer;
	}
	.params {
		grid-column: 1 / -1;
		background: #111827;
		border: 1px solid #1f2937;
		border-radius: 4px;
		padding: 6px 8px;
		margin: 0;
		font-size: 11px;
		white-space: pre;
		overflow-x: auto;
	}
</style>
