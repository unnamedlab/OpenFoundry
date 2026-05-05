<!--
  FASE 4 — NodePreviewPanel.

  Lower panel of the Pipeline Builder editor. Renders a virtualised
  table with the sample rows the backend produced for the currently
  selected node (cast → filter → join chain materialised in memory).
  Mirrors Foundry's "Preview each transformation step" behaviour —
  every node carries its own preview, refreshed automatically when
  the selection changes.
-->
<script lang="ts">
  import {
    previewPipelineNode,
    type PipelineNode,
    type PipelinePreviewOutput,
  } from '$lib/api/pipelines';
  import VirtualizedPreviewTable from '$components/dataset/VirtualizedPreviewTable.svelte';

  type Props = {
    pipelineId: string;
    node: PipelineNode | null;
  };

  const { pipelineId, node }: Props = $props();

  let preview = $state<PipelinePreviewOutput | null>(null);
  let loading = $state(false);
  let error = $state<string | null>(null);
  let lastFetchedNodeId = $state<string | null>(null);

  let columns = $derived.by(() =>
    (preview?.columns ?? []).map((name) => ({ name })),
  );

  let rows = $derived(preview?.rows ?? []);

  let freshnessLabel = $derived.by(() => {
    if (!preview) return '';
    const generated = new Date(preview.generated_at).getTime();
    const elapsed = Math.max(0, Math.round((Date.now() - generated) / 1000));
    if (elapsed === 0) return 'just now';
    if (elapsed < 60) return `${elapsed}s ago`;
    return `${Math.floor(elapsed / 60)}m ago`;
  });

  $effect(() => {
    if (!node) {
      preview = null;
      lastFetchedNodeId = null;
      return;
    }
    if (node.id === lastFetchedNodeId) return;
    lastFetchedNodeId = node.id;
    void refresh();
  });

  async function refresh() {
    if (!node || !pipelineId) return;
    loading = true;
    error = null;
    try {
      preview = await previewPipelineNode(pipelineId, node.id);
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to load preview';
    } finally {
      loading = false;
    }
  }
</script>

<section class="cpp-shell" data-testid="node-preview-panel">
  <header>
    <div>
      <div class="cpp-eyebrow">Preview</div>
      <h3 class="cpp-title">{node ? node.label || node.id : 'No node selected'}</h3>
      {#if preview}
        <p class="cpp-meta">
          {preview.sample_size} rows · chain {preview.source_chain.join(' → ')} ·
          last refreshed
          <span data-testid="preview-freshness">{freshnessLabel}</span>
        </p>
      {/if}
    </div>
    <button
      type="button"
      class="cpp-refresh"
      data-testid="preview-refresh"
      disabled={loading || !node}
      onclick={refresh}
    >
      {loading ? 'Refreshing…' : 'Refresh'}
    </button>
  </header>

  {#if error}
    <div class="cpp-error" data-testid="preview-error">{error}</div>
  {:else if !node}
    <div class="cpp-empty">Select a node on the canvas to preview the data after that step.</div>
  {:else if loading && !preview}
    <div class="cpp-empty">Loading preview…</div>
  {:else if preview && rows.length === 0}
    <div class="cpp-empty">No rows match the upstream chain at this step.</div>
  {:else if preview}
    <div class="cpp-table">
      <VirtualizedPreviewTable
        columns={columns}
        rows={rows}
        transactions={[]}
      />
    </div>
  {/if}
</section>

<style>
  .cpp-shell {
    display: flex;
    flex-direction: column;
    gap: 8px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    background: var(--bg-panel);
    padding: 12px 16px;
    color: var(--text-default);
    font-family: var(--font-sans);
    margin-top: 12px;
  }
  header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 12px;
  }
  .cpp-eyebrow {
    color: var(--text-muted);
    font-size: 12px;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }
  .cpp-title {
    margin: 4px 0 0;
    font-size: 14px;
    color: var(--text-strong);
    font-weight: 600;
  }
  .cpp-meta {
    margin: 4px 0 0;
    font-size: 12px;
    color: var(--text-muted);
  }
  .cpp-refresh {
    padding: 6px 12px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    background: var(--bg-panel);
    color: var(--text-default);
    font-size: 12px;
    font-weight: 600;
  }
  .cpp-refresh:hover:not(:disabled) {
    background: var(--bg-hover);
  }
  .cpp-refresh:disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }
  .cpp-error {
    border: 1px solid var(--status-danger);
    background: var(--status-danger-bg);
    color: var(--status-danger);
    border-radius: var(--radius-md);
    padding: 8px 12px;
    font-size: 12px;
  }
  .cpp-empty {
    border: 1px dashed var(--border-default);
    border-radius: var(--radius-md);
    background: var(--bg-panel-muted);
    color: var(--text-muted);
    padding: 14px;
    font-size: 12px;
    text-align: center;
  }
  .cpp-table {
    max-height: 280px;
    overflow: auto;
  }
</style>
