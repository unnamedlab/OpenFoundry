<!--
  RunHistory — runs table for a pipeline (Foundry: "Build history" tab).
  Polls every 5s while at least one run is `running`. Surfaces buttons to
  trigger a fresh run, retry a finished run, abort an in-flight run, and
  open <RunLogs/> for per-node detail.
-->
<script lang="ts">
  import type { PipelineRun } from '$lib/api/pipelines';
  import { listRuns, triggerRun, retryPipelineRun, abortBuild } from '$lib/api/pipelines';
  import RunLogs from './RunLogs.svelte';

  type Props = {
    pipelineId: string;
    readonly?: boolean;
  };

  let { pipelineId, readonly = false }: Props = $props();

  let runs = $state<PipelineRun[]>([]);
  let loading = $state(false);
  let error = $state<string | null>(null);
  let selectedRunId = $state<string | null>(null);
  let busy = $state<string | null>(null); // run_id or "trigger"

  let pollHandle: ReturnType<typeof setInterval> | null = null;

  async function reload() {
    loading = true;
    error = null;
    try {
      const res = await listRuns(pipelineId, { per_page: 25 });
      runs = res.data ?? [];
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  async function trigger() {
    busy = 'trigger';
    try {
      await triggerRun(pipelineId);
      await reload();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = null;
    }
  }

  async function retry(runId: string) {
    busy = runId;
    try {
      await retryPipelineRun(pipelineId, runId);
      await reload();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = null;
    }
  }

  async function abort(runId: string) {
    if (!confirm('Abort this build?')) return;
    busy = runId;
    try {
      await abortBuild(runId);
      await reload();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = null;
    }
  }

  function statusClass(s: string): string {
    if (s === 'running') return 'pill running';
    if (s === 'completed') return 'pill ok';
    if (s === 'failed') return 'pill fail';
    if (s === 'aborted') return 'pill warn';
    return 'pill';
  }

  function fmt(ts: string | null): string {
    return ts ? new Date(ts).toLocaleString() : '—';
  }

  $effect(() => {
    if (!pipelineId) return;
    void reload();
  });

  $effect(() => {
    const hasRunning = runs.some((r) => r.status === 'running');
    if (hasRunning && !pollHandle) {
      pollHandle = setInterval(reload, 5000);
    } else if (!hasRunning && pollHandle) {
      clearInterval(pollHandle);
      pollHandle = null;
    }
    return () => {
      if (pollHandle) {
        clearInterval(pollHandle);
        pollHandle = null;
      }
    };
  });

  let selectedRun = $derived(runs.find((r) => r.id === selectedRunId) ?? null);
</script>

<section class="history">
  <header>
    <h3>Build history</h3>
    <div class="actions">
      <button type="button" onclick={reload} disabled={loading}>Refresh</button>
      {#if !readonly}
        <button
          type="button"
          class="primary"
          onclick={trigger}
          disabled={busy === 'trigger'}
        >
          {busy === 'trigger' ? 'Triggering…' : 'Trigger run'}
        </button>
      {/if}
    </div>
  </header>

  {#if error}
    <p class="error">{error}</p>
  {/if}

  {#if runs.length === 0 && !loading}
    <p class="hint">No runs yet.</p>
  {:else}
    <table>
      <thead>
        <tr>
          <th>Status</th>
          <th>Trigger</th>
          <th>Started</th>
          <th>Finished</th>
          <th>Attempt</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
        {#each runs as run (run.id)}
          <tr class:selected={run.id === selectedRunId}>
            <td><span class={statusClass(run.status)}>{run.status}</span></td>
            <td>{run.trigger_type}</td>
            <td>{fmt(run.started_at)}</td>
            <td>{fmt(run.finished_at)}</td>
            <td>#{run.attempt_number}</td>
            <td class="row-actions">
              <button type="button" onclick={() => (selectedRunId = run.id)}>Logs</button>
              {#if !readonly && run.status === 'running'}
                <button type="button" onclick={() => abort(run.id)} disabled={busy === run.id}>Abort</button>
              {/if}
              {#if !readonly && (run.status === 'failed' || run.status === 'aborted')}
                <button type="button" onclick={() => retry(run.id)} disabled={busy === run.id}>Retry</button>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}

  {#if selectedRun}
    <RunLogs run={selectedRun} onClose={() => (selectedRunId = null)} />
  {/if}
</section>

<style>
  .history {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 14px;
    background: #0b1220;
    border: 1px solid #1f2937;
    border-radius: 8px;
    color: #e2e8f0;
  }
  header { display: flex; justify-content: space-between; align-items: center; }
  h3 { margin: 0; font-size: 14px; }
  .actions { display: flex; gap: 6px; }
  .actions button, .row-actions button {
    background: #1e293b;
    color: #cbd5e1;
    border: 1px solid #334155;
    border-radius: 4px;
    padding: 4px 10px;
    cursor: pointer;
    font-size: 12px;
  }
  .actions button:hover:not(:disabled), .row-actions button:hover:not(:disabled) {
    background: #334155;
  }
  .actions button:disabled, .row-actions button:disabled { opacity: 0.5; cursor: not-allowed; }
  button.primary { background: #1e3a8a; color: #f1f5f9; border-color: #1d4ed8; }
  button.primary:hover:not(:disabled) { background: #1d4ed8; }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }
  th, td {
    text-align: left;
    padding: 6px 8px;
    border-bottom: 1px solid #1f2937;
  }
  th { color: #94a3b8; font-weight: 500; text-transform: uppercase; font-size: 11px; }
  tr.selected td { background: #1e293b; }
  .row-actions { display: flex; gap: 4px; }
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
  .hint { color: #94a3b8; font-style: italic; margin: 0; font-size: 12px; }
  .error { color: #fca5a5; font-size: 12px; margin: 0; }
</style>
