<!--
  JobGraph — renders the live Flink job-graph (vertex/edge plan) for a
  streaming topology. Reuses cytoscape (already a workspace dep, see
  `LineageView.svelte`). When the backend was built without
  `--features flink-runtime` it falls back to the topology DAG and
  surfaces the `message` field so users know they are seeing a planned
  graph instead of the live one.
-->
<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { getTopologyJobGraph, deployTopologyToFlink, type FlinkJobGraph } from '$lib/api/streaming';

  type Props = {
    topologyId: string;
  };

  let { topologyId }: Props = $props();

  let host: HTMLDivElement;
  let cy: { destroy: () => void } | null = null;

  let graph = $state<FlinkJobGraph | null>(null);
  let loading = $state(false);
  let deploying = $state(false);
  let error = $state<string | null>(null);
  let info = $state<string | null>(null);

  async function load() {
    loading = true;
    error = null;
    try {
      graph = await getTopologyJobGraph(topologyId);
      info = graph.message ?? null;
      await render();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  async function deploy() {
    deploying = true;
    error = null;
    try {
      const resp = await deployTopologyToFlink(topologyId);
      info = resp.message;
      if (resp.sql_warnings.length > 0) {
        info += ` — warnings: ${resp.sql_warnings.join('; ')}`;
      }
      await load();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      deploying = false;
    }
  }

  async function render() {
    if (!host || !graph) return;
    cy?.destroy();
    const cytoscape = (await import('cytoscape')).default;
    cy = cytoscape({
      container: host,
      elements: [
        ...graph.vertices.map((v) => ({
          data: {
            id: v.id,
            label: `${v.name ?? v.id}\n(p=${v.parallelism ?? '?'})`,
            status: v.status ?? 'UNKNOWN',
          },
        })),
        ...graph.edges.map((e, idx) => ({
          data: {
            id: `e${idx}`,
            source: e.source,
            target: e.target,
          },
        })),
      ],
      style: [
        {
          selector: 'node',
          style: {
            label: 'data(label)',
            'text-wrap': 'wrap',
            'font-size': 10,
            color: '#e2e8f0',
            'background-color': '#1e3a8a',
            'text-valign': 'bottom',
            'text-margin-y': 6,
            shape: 'round-rectangle',
            width: 60,
            height: 36,
          },
        },
        {
          selector: 'node[status = "RUNNING"]',
          style: { 'background-color': '#16a34a' },
        },
        {
          selector: 'node[status = "FAILED"]',
          style: { 'background-color': '#dc2626' },
        },
        {
          selector: 'edge',
          style: {
            width: 1.5,
            'line-color': '#475569',
            'target-arrow-color': '#475569',
            'target-arrow-shape': 'triangle',
            'curve-style': 'bezier',
          },
        },
      ],
      layout: { name: 'breadthfirst', directed: true, padding: 20, spacingFactor: 1.4 },
    });
  }

  onMount(() => { void load(); });
  onDestroy(() => cy?.destroy());
</script>

<section class="job-graph">
  <header>
    <h3>Flink job graph</h3>
    <div class="actions">
      <button type="button" onclick={() => void load()} disabled={loading}>
        {loading ? 'Loading…' : 'Refresh'}
      </button>
      <button type="button" onclick={() => void deploy()} disabled={deploying}>
        {deploying ? 'Deploying…' : 'Deploy to Flink'}
      </button>
    </div>
  </header>

  {#if error}
    <p class="error">{error}</p>
  {/if}
  {#if info}
    <p class="info">{info}</p>
  {/if}
  {#if graph?.job_id}
    <p class="job-id">Job ID: <code>{graph.job_id}</code></p>
  {/if}

  <div bind:this={host} class="canvas"></div>
</section>

<style>
  .job-graph { display: flex; flex-direction: column; gap: 0.75rem; }
  header { display: flex; justify-content: space-between; align-items: center; }
  header h3 { margin: 0; font-size: 0.95rem; color: #e2e8f0; }
  .actions { display: flex; gap: 0.5rem; }
  .actions button {
    background: #1e293b;
    color: #e2e8f0;
    border: 1px solid #334155;
    padding: 0.25rem 0.75rem;
    border-radius: 0.25rem;
    cursor: pointer;
    font-size: 0.8rem;
  }
  .actions button:disabled { opacity: 0.5; cursor: not-allowed; }
  .error { color: #fca5a5; font-size: 0.85rem; margin: 0; }
  .info  { color: #fde68a; font-size: 0.85rem; margin: 0; }
  .job-id { color: #94a3b8; font-size: 0.8rem; margin: 0; }
  .job-id code { color: #cbd5e1; }
  .canvas { width: 100%; height: 360px; background: #0f172a; border: 1px solid #1e293b; border-radius: 0.375rem; }
</style>
