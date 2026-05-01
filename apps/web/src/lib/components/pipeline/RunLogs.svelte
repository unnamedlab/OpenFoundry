<!--
  RunLogs — drilldown for a single run, listing per-node results
  (Foundry: "Build details" pane). Renders the `node_results[]` payload
  attached to the `pipeline_runs` row plus any top-level error.
-->
<script lang="ts">
  import type { PipelineRun, PipelineNodeResult } from '$lib/api/pipelines';

  type Props = {
    run: PipelineRun;
    onClose?: () => void;
  };

  let { run, onClose }: Props = $props();

  function statusClass(s: string): string {
    if (s === 'running') return 'pill running';
    if (s === 'completed') return 'pill ok';
    if (s === 'failed') return 'pill fail';
    if (s === 'aborted' || s === 'skipped') return 'pill warn';
    return 'pill';
  }

  let nodes = $derived<PipelineNodeResult[]>(run.node_results ?? []);
  let durationMs = $derived(
    run.finished_at
      ? new Date(run.finished_at).getTime() - new Date(run.started_at).getTime()
      : null,
  );
</script>

<section class="logs">
  <header>
    <h4>Run <code>{run.id.slice(0, 8)}</code></h4>
    {#if onClose}
      <button type="button" class="close" onclick={onClose} aria-label="Close">×</button>
    {/if}
  </header>

  <dl class="meta">
    <dt>Status</dt><dd><span class={statusClass(run.status)}>{run.status}</span></dd>
    <dt>Trigger</dt><dd>{run.trigger_type}</dd>
    <dt>Attempt</dt><dd>#{run.attempt_number}</dd>
    <dt>Started</dt><dd>{new Date(run.started_at).toLocaleString()}</dd>
    <dt>Finished</dt><dd>{run.finished_at ? new Date(run.finished_at).toLocaleString() : '—'}</dd>
    <dt>Duration</dt><dd>{durationMs !== null ? `${(durationMs / 1000).toFixed(1)}s` : '—'}</dd>
  </dl>

  {#if run.error_message}
    <pre class="error">{run.error_message}</pre>
  {/if}

  <h5>Per-node results</h5>
  {#if nodes.length === 0}
    <p class="hint">No node-level results recorded for this run.</p>
  {:else}
    <ul class="nodes">
      {#each nodes as nr (nr.node_id)}
        <li>
          <header>
            <strong>{nr.label}</strong>
            <code>({nr.transform_type})</code>
            <span class={statusClass(nr.status)}>{nr.status}</span>
          </header>
          <dl>
            <dt>Attempts</dt><dd>{nr.attempts}</dd>
            <dt>Rows</dt><dd>{nr.rows_affected ?? '—'}</dd>
          </dl>
          {#if nr.error}
            <pre class="error">{nr.error}</pre>
          {/if}
          {#if nr.output && Object.keys(nr.output).length > 0}
            <details>
              <summary>Output</summary>
              <pre>{JSON.stringify(nr.output, null, 2)}</pre>
            </details>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .logs {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 12px;
    background: #0f172a;
    border: 1px solid #1f2937;
    border-radius: 6px;
    color: #e2e8f0;
  }
  header { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  h4 { margin: 0; font-size: 13px; }
  h5 { margin: 4px 0; font-size: 12px; color: #94a3b8; text-transform: uppercase; letter-spacing: 0.05em; }
  .close {
    background: transparent;
    color: #94a3b8;
    border: none;
    font-size: 18px;
    cursor: pointer;
    line-height: 1;
  }
  .close:hover { color: #f1f5f9; }
  dl.meta {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 4px 12px;
    margin: 0;
    font-size: 12px;
  }
  dl.meta dt { color: #94a3b8; }
  dl.meta dd { margin: 0; color: #e2e8f0; }
  .pill {
    display: inline-block;
    padding: 2px 8px;
    border-radius: 999px;
    font-size: 11px;
    background: #334155;
    color: #cbd5e1;
  }
  .pill.running { background: #1d4ed8; color: #dbeafe; }
  .pill.ok { background: #166534; color: #d1fae5; }
  .pill.fail { background: #991b1b; color: #fee2e2; }
  .pill.warn { background: #92400e; color: #fde68a; }
  .nodes {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .nodes > li {
    background: #0b1220;
    border: 1px solid #1f2937;
    border-radius: 4px;
    padding: 8px 10px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .nodes > li > header { gap: 6px; justify-content: flex-start; }
  .nodes dl { display: flex; gap: 10px; margin: 0; font-size: 11px; color: #94a3b8; }
  .nodes dt { color: #64748b; }
  .nodes dd { margin: 0 8px 0 4px; color: #cbd5e1; }
  pre {
    background: #020617;
    color: #e2e8f0;
    padding: 6px 8px;
    border-radius: 4px;
    font-size: 11px;
    margin: 0;
    overflow: auto;
    max-height: 240px;
  }
  pre.error { color: #fca5a5; border: 1px solid #7f1d1d; }
  details > summary { cursor: pointer; font-size: 11px; color: #60a5fa; }
  .hint { color: #94a3b8; font-style: italic; font-size: 12px; margin: 0; }
  code { font-family: ui-monospace, 'SF Mono', Consolas, monospace; font-size: 11px; color: #94a3b8; }
</style>
