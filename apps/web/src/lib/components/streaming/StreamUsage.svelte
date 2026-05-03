<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import {
		getStreamUsage,
		type UsageResponse
	} from '$lib/api/streaming';

	export let streamId: string;
	/** USD per compute second. Surfaced in the banner. Defaults to a
	 * placeholder that the operator can override via build-time
	 * `VITE_COMPUTE_SECONDS_TO_COST_FACTOR`. */
	export let costFactor: number = Number(
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		((import.meta as any)?.env?.VITE_COMPUTE_SECONDS_TO_COST_FACTOR as string) ?? '0.0001'
	);

	let usage: UsageResponse | null = null;
	let group: 'hour' | 'day' = 'hour';
	let lookback: '24h' | '7d' | '30d' = '24h';
	let error = '';
	let loading = false;

	let computeChart: import('echarts').ECharts | null = null;
	let recordsChart: import('echarts').ECharts | null = null;
	let computeContainer: HTMLDivElement | null = null;
	let recordsContainer: HTMLDivElement | null = null;

	function rangeFromLookback(lb: '24h' | '7d' | '30d'): { from: string; to: string } {
		const to = new Date();
		const from = new Date(to);
		if (lb === '24h') from.setHours(from.getHours() - 24);
		if (lb === '7d') from.setDate(from.getDate() - 7);
		if (lb === '30d') from.setDate(from.getDate() - 30);
		return { from: from.toISOString(), to: to.toISOString() };
	}

	async function refresh() {
		loading = true;
		error = '';
		try {
			const range = rangeFromLookback(lookback);
			usage = await getStreamUsage(streamId, { ...range, group });
			await render();
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		} finally {
			loading = false;
		}
	}

	async function render() {
		if (!usage) return;
		if (!computeChart && computeContainer) {
			const echartsModule = await import('echarts');
			computeChart = echartsModule.init(computeContainer, undefined, { renderer: 'canvas' });
		}
		if (!recordsChart && recordsContainer) {
			const echartsModule = await import('echarts');
			recordsChart = echartsModule.init(recordsContainer, undefined, { renderer: 'canvas' });
		}
		const labels = usage.buckets.map((b) => new Date(b.bucket_start).toLocaleString());
		computeChart?.setOption({
			tooltip: { trigger: 'axis' },
			grid: { top: 30, left: 60, right: 30, bottom: 30 },
			xAxis: { type: 'category', data: labels },
			yAxis: { type: 'value', name: 'compute s' },
			series: [
				{
					type: 'line',
					smooth: true,
					name: `Compute s / ${group}`,
					data: usage.buckets.map((b) => b.compute_seconds)
				}
			]
		});
		recordsChart?.setOption({
			tooltip: { trigger: 'axis' },
			grid: { top: 30, left: 60, right: 30, bottom: 30 },
			xAxis: { type: 'category', data: labels },
			yAxis: { type: 'value', name: 'records' },
			series: [
				{
					type: 'bar',
					name: `Records / ${group}`,
					data: usage.buckets.map((b) => b.records_processed)
				}
			]
		});
	}

	$: estimatedCost = usage ? usage.total_compute_seconds * costFactor : 0;

	onMount(refresh);
	onDestroy(() => {
		computeChart?.dispose();
		recordsChart?.dispose();
	});
</script>

<section class="usage" data-testid="stream-usage">
	<header>
		<h3>Compute usage</h3>
		<div class="controls">
			<select bind:value={lookback} on:change={refresh}>
				<option value="24h">Last 24 h</option>
				<option value="7d">Last 7 d</option>
				<option value="30d">Last 30 d</option>
			</select>
			<select bind:value={group} on:change={refresh}>
				<option value="hour">Group: hour</option>
				<option value="day">Group: day</option>
			</select>
		</div>
	</header>

	<div class="banner banner-info" role="note">
		<strong>Approximated cost</strong> — this is a projection based on a
		<code>compute_seconds_to_cost_factor</code> of {costFactor.toFixed(6)} USD/s. Actual billing
		may differ.
	</div>

	{#if loading}<p>Loading…</p>{/if}
	{#if error}<p class="error">{error}</p>{/if}

	{#if usage}
		<table class="summary" data-testid="usage-summary">
			<tbody>
				<tr><th>Window</th><td>{lookback}</td></tr>
				<tr>
					<th>Total compute seconds</th>
					<td>{usage.total_compute_seconds.toFixed(2)}</td>
				</tr>
				<tr>
					<th>Total records processed</th>
					<td>{usage.total_records_processed.toLocaleString()}</td>
				</tr>
				<tr>
					<th>Estimated cost</th>
					<td>${estimatedCost.toFixed(4)} (USD)</td>
				</tr>
			</tbody>
		</table>
	{/if}

	<div class="charts">
		<div class="chart" bind:this={computeContainer} data-testid="usage-compute-chart"></div>
		<div class="chart" bind:this={recordsContainer} data-testid="usage-records-chart"></div>
	</div>
</section>

<style>
	.usage { display: grid; gap: 0.75rem; }
	header { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; }
	header h3 { margin: 0; }
	.controls { display: flex; gap: 0.5rem; }
	.banner { padding: 0.5rem 0.75rem; border-radius: 4px; }
	.banner-info { background: #eef5ff; border-left: 4px solid #2a6df0; color: #1a3a8a; font-size: 0.85rem; }
	.summary { width: 100%; border-collapse: collapse; }
	.summary th, .summary td { text-align: left; padding: 0.35rem 0.6rem; border-bottom: 1px solid #eee; }
	.charts { display: grid; grid-template-columns: 1fr 1fr; gap: 0.75rem; }
	.chart { height: 220px; min-width: 0; }
	.error { color: #b00; }
</style>
