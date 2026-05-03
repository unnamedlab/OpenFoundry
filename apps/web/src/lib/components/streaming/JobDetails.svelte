<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import {
		listCheckpoints,
		getStreamMetrics,
		type Checkpoint,
		type StreamMetricsResponse,
		type TopologyDefinition
	} from '$lib/api/streaming';
	import {
		listMonitorRules,
		listEvaluationsForRule,
		type MonitorRule,
		type MonitorEvaluation
	} from '$lib/api/monitoring';
	import JobGraph from './JobGraph.svelte';

	export let streamId: string;
	export const streamName: string = '';
	export let topology: TopologyDefinition | null = null;

	let perfWindow: '5m' | '30m' | string = '5m';
	let metrics: StreamMetricsResponse | null = null;
	let checkpoints: Checkpoint[] = [];
	let monitors: Array<{ rule: MonitorRule; firing: boolean }> = [];

	let perfChart: import('echarts').ECharts | null = null;
	let perfChartContainer: HTMLDivElement | null = null;

	let perfTimer: ReturnType<typeof setInterval> | null = null;
	let cpTimer: ReturnType<typeof setInterval> | null = null;
	let monitorsTimer: ReturnType<typeof setInterval> | null = null;

	const PERF_HISTORY_MAX = 60;
	const perfHistory = {
		t: [] as number[],
		ingested: [] as number[],
		output: [] as number[],
		lag: [] as number[],
		utilization: [] as number[]
	};

	async function refreshMetrics() {
		try {
			metrics = await getStreamMetrics(streamId, perfWindow);
			if (metrics) {
				perfHistory.t.push(Date.now());
				perfHistory.ingested.push(metrics.records_ingested);
				perfHistory.output.push(metrics.records_output);
				perfHistory.lag.push(metrics.total_lag);
				perfHistory.utilization.push(metrics.utilization_pct * 100);
				if (perfHistory.t.length > PERF_HISTORY_MAX) {
					perfHistory.t.shift();
					perfHistory.ingested.shift();
					perfHistory.output.shift();
					perfHistory.lag.shift();
					perfHistory.utilization.shift();
				}
				renderChart();
			}
		} catch (err) {
			console.warn('refreshMetrics failed', err);
		}
	}

	async function refreshCheckpoints() {
		if (!topology) return;
		try {
			const res = await listCheckpoints(topology.id, 20);
			checkpoints = res.data;
		} catch (err) {
			console.warn('refreshCheckpoints failed', err);
		}
	}

	async function refreshMonitors() {
		try {
			const list = await listMonitorRules({
				resource_rid: streamId,
				resource_type: 'STREAMING_DATASET'
			});
			const enriched = await Promise.all(
				list.data.map(async (rule) => {
					try {
						const evals = await listEvaluationsForRule(rule.id, 1);
						const last: MonitorEvaluation | undefined = evals.data[0];
						return { rule, firing: !!last?.fired };
					} catch {
						return { rule, firing: false };
					}
				})
			);
			monitors = enriched;
		} catch (err) {
			console.warn('refreshMonitors failed', err);
		}
	}

	async function renderChart() {
		if (!perfChartContainer) return;
		if (!perfChart) {
			const echartsModule = await import('echarts');
			perfChart = echartsModule.init(perfChartContainer, undefined, { renderer: 'canvas' });
		}
		const labels = perfHistory.t.map((t) => new Date(t).toLocaleTimeString());
		perfChart.setOption({
			tooltip: { trigger: 'axis' },
			legend: { data: ['Ingested', 'Output', 'Lag (ms)', 'Utilization %'] },
			grid: { top: 40, left: 60, right: 60, bottom: 30 },
			xAxis: { type: 'category', data: labels },
			yAxis: [
				{ type: 'value', name: 'records' },
				{ type: 'value', name: '%', max: 100, position: 'right' }
			],
			series: [
				{ name: 'Ingested', type: 'line', smooth: true, data: perfHistory.ingested },
				{ name: 'Output', type: 'line', smooth: true, data: perfHistory.output },
				{ name: 'Lag (ms)', type: 'line', smooth: true, data: perfHistory.lag },
				{
					name: 'Utilization %',
					type: 'line',
					smooth: true,
					yAxisIndex: 1,
					data: perfHistory.utilization
				}
			]
		});
	}

	function changeWindow(w: '5m' | '30m') {
		perfWindow = w;
		// Reset history on window change so the chart isn't a mix of
		// records-counts at different scales.
		perfHistory.t = [];
		perfHistory.ingested = [];
		perfHistory.output = [];
		perfHistory.lag = [];
		perfHistory.utilization = [];
		refreshMetrics();
	}

	onMount(() => {
		refreshMetrics();
		refreshCheckpoints();
		refreshMonitors();
		perfTimer = setInterval(refreshMetrics, 10_000);
		cpTimer = setInterval(refreshCheckpoints, 10_000);
		monitorsTimer = setInterval(refreshMonitors, 30_000);
	});

	onDestroy(() => {
		if (perfTimer) clearInterval(perfTimer);
		if (cpTimer) clearInterval(cpTimer);
		if (monitorsTimer) clearInterval(monitorsTimer);
		perfChart?.dispose();
	});

	function checkpointStatusClass(status: string): string {
		const upper = status.toUpperCase();
		if (upper === 'COMPLETED' || upper === 'SUCCESS' || upper === 'OK') return 'ok';
		if (upper === 'FAILED' || upper === 'TIMED_OUT') return 'fail';
		return 'pending';
	}
</script>

<section class="job-details" data-testid="job-details">
	<div class="card">
		<header class="card-head">
			<h3>Job Graph</h3>
		</header>
		{#if topology}
			<JobGraph topologyId={topology.id} />
		{:else}
			<p class="hint">Select a related topology to render its live job graph.</p>
		{/if}
	</div>

	<div class="card" data-testid="last-checkpoints-card">
		<header class="card-head">
			<h3>Last checkpoints</h3>
			<small class="hint">Auto-refresh every 10 s · last 20</small>
		</header>
		<table>
			<thead>
				<tr><th>ID</th><th>Started</th><th>Status</th><th>Duration</th><th>Trigger</th></tr>
			</thead>
			<tbody data-testid="checkpoints-tbody">
				{#each checkpoints as cp (cp.id)}
					<tr>
						<td><code>{cp.id.slice(0, 8)}</code></td>
						<td>{new Date(cp.created_at).toLocaleTimeString()}</td>
						<td>
							<span class="badge {checkpointStatusClass(cp.status)}">{cp.status}</span>
						</td>
						<td>{cp.duration_ms} ms</td>
						<td>{cp.trigger}</td>
					</tr>
				{:else}
					<tr><td colspan="5" class="hint">No checkpoints yet.</td></tr>
				{/each}
			</tbody>
		</table>
	</div>

	<div class="card">
		<header class="card-head">
			<h3>Performance</h3>
			<div class="window-picker" role="tablist">
				<button
					type="button"
					class:active={perfWindow === '5m'}
					on:click={() => changeWindow('5m')}
					data-testid="window-5m"
				>5m</button>
				<button
					type="button"
					class:active={perfWindow === '30m'}
					on:click={() => changeWindow('30m')}
					data-testid="window-30m"
				>30m</button>
				<input
					type="text"
					placeholder="custom (e.g. 600s)"
					value={typeof perfWindow === 'string' && perfWindow !== '5m' && perfWindow !== '30m' ? perfWindow : ''}
					on:change={(e) => {
						const v = (e.currentTarget as HTMLInputElement).value.trim();
						if (v) changeWindow(v as '5m' | '30m');
					}}
					data-testid="window-custom"
				/>
			</div>
		</header>
		{#if metrics}
			<dl class="kpi-grid">
				<div><dt>Ingested</dt><dd>{metrics.records_ingested.toLocaleString()}</dd></div>
				<div><dt>Output</dt><dd>{metrics.records_output.toLocaleString()}</dd></div>
				<div><dt>Throughput</dt><dd>{metrics.total_throughput.toFixed(2)} r/s</dd></div>
				<div><dt>Lag</dt><dd>{metrics.total_lag.toLocaleString()} ms</dd></div>
				<div>
					<dt>Utilization</dt>
					<dd>{(metrics.utilization_pct * 100).toFixed(1)} %</dd>
				</div>
			</dl>
		{/if}
		<div bind:this={perfChartContainer} class="perf-chart" data-testid="perf-chart"></div>
	</div>

	<div class="card" data-testid="active-monitors-card">
		<header class="card-head">
			<h3>Active monitors</h3>
			<a class="link" href="/control-panel/data-health">+ Add monitor</a>
		</header>
		<ul class="monitor-list">
			{#each monitors as m (m.rule.id)}
				<li>
					<span>
						<strong>{m.rule.monitor_kind}</strong>
						<small>
							{m.rule.comparator} {m.rule.threshold} · {m.rule.window_seconds}s ·
							<em>{m.rule.severity}</em>
						</small>
					</span>
					{#if m.firing}
						<span class="badge fail">FIRING</span>
					{:else}
						<span class="badge ok">ok</span>
					{/if}
				</li>
			{:else}
				<li class="hint">
					No monitors configured for this stream. Use Data Health to add one.
				</li>
			{/each}
		</ul>
	</div>
</section>

<style>
	.job-details { display: grid; gap: 1rem; }
	.card { background: var(--panel, #fff); border: 1px solid var(--border, #e5e5e5); border-radius: 6px; padding: 1rem; }
	.card-head { display: flex; justify-content: space-between; align-items: center; gap: 0.5rem; margin-bottom: 0.5rem; }
	.card h3 { margin: 0; font-size: 1rem; }
	.window-picker { display: flex; gap: 0.4rem; align-items: center; }
	.window-picker button { padding: 0.2rem 0.6rem; border: 1px solid #ddd; background: #fff; cursor: pointer; border-radius: 4px; }
	.window-picker button.active { background: #246; color: #fff; border-color: #246; }
	.window-picker input { padding: 0.2rem 0.4rem; font-size: 0.85rem; border: 1px solid #ddd; border-radius: 4px; }
	table { width: 100%; border-collapse: collapse; font-size: 0.85rem; }
	th, td { text-align: left; padding: 0.35rem 0.5rem; border-bottom: 1px solid #eee; }
	.badge { display: inline-block; padding: 0.05rem 0.45rem; border-radius: 3px; font-size: 0.7rem; }
	.badge.ok { background: #d4f4dd; color: #1f5631; }
	.badge.fail { background: #fde7e9; color: #720010; font-weight: 600; }
	.badge.pending { background: #fff3cd; color: #5b4500; }
	.kpi-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); gap: 0.5rem 1rem; margin: 0; }
	.kpi-grid dt { font-size: 0.7rem; color: #666; text-transform: uppercase; }
	.kpi-grid dd { margin: 0; font-weight: 600; font-size: 1.1rem; }
	.perf-chart { width: 100%; height: 240px; margin-top: 0.5rem; }
	.monitor-list { margin: 0; padding: 0; list-style: none; display: grid; gap: 0.4rem; }
	.monitor-list li { display: flex; justify-content: space-between; align-items: center; padding: 0.4rem 0.6rem; border: 1px solid #eee; border-radius: 4px; }
	.monitor-list small { display: block; color: #666; font-weight: normal; }
	.link { color: #246; text-decoration: none; font-size: 0.85rem; }
	.hint { color: #666; }
</style>
