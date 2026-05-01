<!--
  LineageView — dataset lineage graph (Foundry: Lineage app surface
  consumed from inside Pipeline Builder). Renders upstream/downstream
  datasets and the impact set using cytoscape. Click a node to optionally
  surface metadata via the parent.
-->
<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import type { LineageGraph, LineageImpactAnalysis } from '$lib/api/pipelines';
  import { getDatasetLineage, getDatasetLineageImpact } from '$lib/api/pipelines';

  type Props = {
    datasetId: string;
    onSelect?: (id: string, kind: string) => void;
  };

  let { datasetId, onSelect }: Props = $props();

  let host: HTMLDivElement;
  let cy: { destroy: () => void; on: (ev: string, sel: string, cb: (e: { target: { id: () => string; data: (k: string) => string } }) => void) => void } | null = null;

  let graph = $state<LineageGraph | null>(null);
  let impact = $state<LineageImpactAnalysis | null>(null);
  let loading = $state(false);
  let error = $state<string | null>(null);

  async function load() {
    loading = true;
    error = null;
    try {
      const [g, imp] = await Promise.all([
        getDatasetLineage(datasetId),
        getDatasetLineageImpact(datasetId).catch(() => null),
      ]);
      graph = g;
      impact = imp;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  async function render() {
    if (!host || !graph) return;
    cy?.destroy();
    const cytoscape = (await import('cytoscape')).default;
    cy = cytoscape({
      container: host,
      elements: [
        ...graph.nodes.map((n) => ({
          data: { id: n.id, label: n.label || n.id, kind: n.kind, marking: n.marking },
        })),
        ...graph.edges.map((e) => ({
          data: { id: e.id, source: e.source, target: e.target, relation: e.relation_kind },
        })),
      ],
      style: [
        {
          selector: 'node',
          style: {
            label: 'data(label)',
            'font-size': 10,
            color: '#e2e8f0',
            'background-color': '#1e3a8a',
            'text-valign': 'bottom',
            'text-margin-y': 6,
            width: 32,
            height: 32,
          },
        },
        {
          selector: 'node[kind = "dataset"]',
          style: { 'background-color': '#1d4ed8', shape: 'round-rectangle' },
        },
        {
          selector: 'node[kind = "pipeline"]',
          style: { 'background-color': '#7c3aed', shape: 'diamond' },
        },
        {
          selector: 'node[kind = "workflow"]',
          style: { 'background-color': '#0d9488', shape: 'hexagon' },
        },
        {
          selector: `node[id = "${datasetId}"]`,
          style: { 'background-color': '#f59e0b', 'border-width': 2, 'border-color': '#fbbf24' },
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

    if (onSelect) {
      cy.on('tap', 'node', (evt) => {
        onSelect(evt.target.id(), evt.target.data('kind'));
      });
    }
  }

  onMount(() => { void load(); });
  onDestroy(() => cy?.destroy());

  $effect(() => { if (datasetId) void load(); });
  $effect(() => { if (graph) void render(); });
</script>

<section class="lineage">
  <header>
    <h3>Lineage</h3>
    <button type="button" onclick={load} disabled={loading}>
      {loading ? 'Loading…' : 'Refresh'}
    </button>
  </header>

  {#if error}
    <p class="error">{error}</p>
  {/if}

  <div bind:this={host} class="canvas"></div>

  {#if impact}
    <details>
      <summary>Downstream impact ({impact.downstream?.length ?? 0})</summary>
      {#if (impact.downstream?.length ?? 0) === 0}
        <p class="hint">No downstream consumers detected.</p>
      {:else}
        <ul>
          {#each impact.downstream ?? [] as item (item.id)}
            <li>
              <code>{item.kind}</code> {item.label}
              <span class="dist">d={item.distance}</span>
              {#if item.requires_acknowledgement}
                <span class="warn">requires ack</span>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </details>
  {/if}
</section>

<style>
  .lineage {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 14px;
    background: #0b1220;
    border: 1px solid #1f2937;
    border-radius: 8px;
    color: #e2e8f0;
  }
  header { display: flex; align-items: center; justify-content: space-between; }
  h3 { margin: 0; font-size: 14px; }
  button {
    background: #1e293b;
    color: #cbd5e1;
    border: 1px solid #334155;
    border-radius: 4px;
    padding: 4px 10px;
    cursor: pointer;
    font-size: 12px;
  }
  button:hover:not(:disabled) { background: #334155; }
  button:disabled { opacity: 0.5; cursor: not-allowed; }
  .canvas {
    width: 100%;
    height: 420px;
    background: #020617;
    border: 1px solid #1f2937;
    border-radius: 6px;
  }
  details summary { cursor: pointer; font-size: 12px; color: #60a5fa; }
  ul { margin: 6px 0 0; padding-left: 18px; font-size: 12px; }
  ul li { margin-bottom: 4px; }
  code { font-family: ui-monospace, 'SF Mono', Consolas, monospace; font-size: 11px; color: #94a3b8; }
  .dist { color: #94a3b8; font-size: 11px; margin-left: 6px; }
  .warn { color: #fbbf24; font-size: 11px; margin-left: 6px; }
  .error { color: #fca5a5; font-size: 12px; margin: 0; }
  .hint { color: #94a3b8; font-style: italic; font-size: 12px; margin: 0; }
</style>
