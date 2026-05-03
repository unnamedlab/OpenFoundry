<!--
  Pipeline editor route — Foundry's "Pipeline Builder" canvas plus tabs
  for Schedule, Build history and Lineage. Loads the pipeline via
  `getPipeline`, persists with `updatePipeline`, and validates the DAG
  through the canvas itself.

  Architecture matches Foundry's separation of concerns:
    - Authoring (this canvas + node config)        -> pipeline-authoring-service
    - Build history & runs                          -> pipeline-build-service
    - Schedule cron + window preview                -> pipeline-schedule-service
-->
<script lang="ts">
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import {
    getPipeline,
    updatePipeline,
    type Pipeline,
    type PipelineNode,
    type PipelineScheduleConfig,
    type PipelineValidationResponse,
  } from '$lib/api/pipelines';
  import PipelineCanvas from '$lib/components/pipeline/PipelineCanvas.svelte';
  import NodePalette, {
    type NodePaletteEntry
  } from '$lib/components/pipeline/NodePalette.svelte';
  import NodeConfig from '$lib/components/pipeline/NodeConfig.svelte';
  import ScheduleConfig from '$lib/components/pipeline/ScheduleConfig.svelte';
  import RunHistory from '$lib/components/pipeline/RunHistory.svelte';
  import LineageView from '$lib/components/pipeline/LineageView.svelte';

  type Tab = 'canvas' | 'schedule' | 'runs' | 'lineage';

  let pipeline = $state<Pipeline | null>(null);
  let loading = $state(true);
  let saving = $state(false);
  let error = $state<string | null>(null);
  let dirty = $state(false);
  let tab = $state<Tab>('canvas');

  let nodes = $state<PipelineNode[]>([]);
  let scheduleConfig = $state<PipelineScheduleConfig>({ enabled: false, cron: null });
  let selectedNode = $state<PipelineNode | null>(null);
  let validation = $state<PipelineValidationResponse | null>(null);

  let pipelineId = $derived($page.params.id);

  let lineageDatasetId = $derived.by(() => {
    // Prefer the first node with an output dataset; fall back to any input.
    for (const n of nodes) {
      if (n.output_dataset_id) return n.output_dataset_id;
    }
    for (const n of nodes) {
      if (n.input_dataset_ids.length > 0) return n.input_dataset_ids[0];
    }
    return null;
  });

  async function load() {
    if (!pipelineId) return;
    loading = true;
    error = null;
    try {
      const p = await getPipeline(pipelineId);
      pipeline = p;
      nodes = structuredClone(p.dag);
      scheduleConfig = { ...p.schedule_config };
      dirty = false;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  $effect(() => { if (pipelineId) void load(); });

  function genNodeId(seed: string): string {
    const base = `${seed}_node`;
    const used = new Set(nodes.map((n) => n.id));
    let i = 1;
    while (used.has(`${base}_${i}`)) i += 1;
    return `${base}_${i}`;
  }

  function addNode(entry: NodePaletteEntry) {
    const seed = entry.kind ?? entry.transform_type;
    const id = genNodeId(seed);
    const next: PipelineNode = {
      id,
      label: entry.label || id,
      transform_type: entry.transform_type,
      config: { ...(entry.defaultConfig ?? {}) },
      depends_on: [],
      input_dataset_ids: [],
      output_dataset_id: null
    };
    nodes = [...nodes, next];
    selectedNode = next;
    dirty = true;
  }

  function patchNode(updated: PipelineNode) {
    nodes = nodes.map((n) => (n.id === selectedNode?.id ? updated : n));
    selectedNode = updated;
    dirty = true;
  }

  function deleteNode(nodeId: string) {
    nodes = nodes
      .filter((n) => n.id !== nodeId)
      .map((n) => ({ ...n, depends_on: n.depends_on.filter((d) => d !== nodeId) }));
    if (selectedNode?.id === nodeId) selectedNode = null;
    dirty = true;
  }

  function onCanvasChange(next: PipelineNode[]) {
    nodes = next;
    dirty = true;
    // keep selection consistent
    if (selectedNode) {
      selectedNode = next.find((n) => n.id === selectedNode!.id) ?? null;
    }
  }

  async function save() {
    if (!pipeline) return;
    saving = true;
    error = null;
    try {
      const updated = await updatePipeline(pipeline.id, {
        nodes,
        schedule_config: scheduleConfig,
      });
      pipeline = updated;
      nodes = structuredClone(updated.dag);
      scheduleConfig = { ...updated.schedule_config };
      dirty = false;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      saving = false;
    }
  }
</script>

<div class="page">
  <header class="bar">
    <div>
      <button type="button" class="link" onclick={() => goto('/pipelines')}>← Pipelines</button>
      <h1>{pipeline?.name ?? 'Loading…'}</h1>
      {#if pipeline}
        <p class="meta">{pipeline.description || 'No description'}</p>
      {/if}
    </div>
    <div class="actions">
      <button type="button" class="primary" disabled={!dirty || saving} onclick={save}>
        {saving ? 'Saving…' : dirty ? 'Save changes' : 'Saved'}
      </button>
    </div>
  </header>

  {#if error}
    <div class="error">{error}</div>
  {/if}

  {#if loading}
    <div class="loading">Loading pipeline…</div>
  {:else if pipeline}
    <div class="tabs" role="tablist">
      {#each ['canvas', 'schedule', 'runs', 'lineage'] as t (t)}
        <button
          type="button"
          role="tab"
          aria-selected={tab === t}
          class:active={tab === t}
          onclick={() => (tab = t as Tab)}
        >
          {t}
        </button>
      {/each}
    </div>

    {#if tab === 'canvas'}
      <div class="canvas-layout">
        <NodePalette onAdd={addNode} />
        <div class="canvas-host">
          <PipelineCanvas
            bind:nodes
            status={pipeline.status}
            scheduleConfig={scheduleConfig}
            onChange={onCanvasChange}
            onSelect={(n) => (selectedNode = n)}
            onValidate={(v) => (validation = v)}
          />
          {#if validation && validation.next_run_at}
            <p class="hint">Next scheduled run: {new Date(validation.next_run_at).toLocaleString()}</p>
          {/if}
        </div>
        <NodeConfig
          node={selectedNode}
          siblings={nodes}
          onChange={patchNode}
          onDelete={deleteNode}
        />
      </div>
    {:else if tab === 'schedule'}
      <ScheduleConfig
        pipelineId={pipeline.id}
        config={scheduleConfig}
        onChange={(next) => { scheduleConfig = next; dirty = true; }}
      />
    {:else if tab === 'runs'}
      <RunHistory pipelineId={pipeline.id} />
    {:else if tab === 'lineage'}
      {#if lineageDatasetId}
        <LineageView datasetId={lineageDatasetId} />
      {:else}
        <p class="hint">Bind an input or output dataset to a node first to view lineage.</p>
      {/if}
    {/if}
  {/if}
</div>

<style>
  .page {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 20px;
    color: #e2e8f0;
    background: #020617;
    min-height: 100vh;
  }
  .bar {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 12px;
  }
  .bar h1 { margin: 4px 0; font-size: 22px; }
  .meta { margin: 0; color: #94a3b8; font-size: 13px; }
  .link { background: transparent; color: #60a5fa; border: none; cursor: pointer; font: inherit; padding: 0; }
  .actions { display: flex; gap: 8px; }
  .primary {
    background: #1d4ed8;
    color: #f1f5f9;
    border: 1px solid #1e40af;
    border-radius: 6px;
    padding: 8px 16px;
    cursor: pointer;
    font: inherit;
    font-weight: 600;
  }
  .primary:disabled { opacity: 0.5; cursor: not-allowed; }
  .primary:hover:not(:disabled) { background: #2563eb; }
  .tabs {
    display: flex;
    gap: 4px;
    border-bottom: 1px solid #1f2937;
  }
  .tabs button {
    background: transparent;
    color: #94a3b8;
    border: none;
    padding: 8px 16px;
    cursor: pointer;
    font: inherit;
    text-transform: capitalize;
    border-bottom: 2px solid transparent;
  }
  .tabs button.active { color: #f1f5f9; border-bottom-color: #1d4ed8; }
  .tabs button:hover:not(.active) { color: #cbd5e1; }
  .canvas-layout {
    display: flex;
    gap: 12px;
    align-items: flex-start;
  }
  .canvas-host { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 8px; }
  .hint { color: #94a3b8; font-size: 12px; margin: 0; font-style: italic; }
  .error {
    background: #7f1d1d;
    color: #fee2e2;
    border: 1px solid #b91c1c;
    border-radius: 6px;
    padding: 8px 12px;
    font-size: 13px;
  }
  .loading { color: #94a3b8; font-style: italic; }
</style>
