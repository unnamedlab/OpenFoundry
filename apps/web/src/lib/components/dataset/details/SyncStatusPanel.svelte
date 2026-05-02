<!--
  T5.1 — SyncStatusPanel

  Foundry shows the sync status for datasets backed by external
  sources (data-connection ingest, mirror jobs). We expose: last sync
  timestamp, status pill, and a list of recent error messages.

  Controlled component: parent provides `state` (or `null` when no
  sync is configured).
-->
<script lang="ts">
  type SyncState = {
    last_sync_at?: string | null;
    next_sync_at?: string | null;
    status: 'healthy' | 'degraded' | 'failed' | 'paused' | 'never_run' | string;
    rows_synced?: number;
    bytes_synced?: number;
    errors?: Array<{ at: string; message: string }>;
    source?: string;
  };

  type Props = { state: SyncState | null };
  const { state }: Props = $props();

  function pillFor(status: string): string {
    if (status === 'healthy')
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300';
    if (status === 'degraded' || status === 'paused')
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300';
    if (status === 'failed')
      return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300';
    return 'bg-slate-100 text-slate-700 dark:bg-gray-800 dark:text-gray-300';
  }
</script>

<section class="space-y-4">
  <header>
    <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Sync status</div>
    <h2 class="mt-1 text-lg font-semibold">External replication</h2>
    <p class="mt-1 text-sm text-gray-500">
      Health of the connector that keeps this dataset in sync with its source.
    </p>
  </header>

  {#if !state}
    <div class="rounded-xl border border-dashed border-slate-300 px-4 py-8 text-center text-sm text-gray-500 dark:border-gray-700">
      No external sync is configured for this dataset.
    </div>
  {:else}
    <dl class="grid grid-cols-1 gap-3 text-sm md:grid-cols-2">
      <div>
        <dt class="text-xs uppercase tracking-wide text-gray-400">Status</dt>
        <dd class="mt-0.5">
          <span class={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide ${pillFor(state.status)}`}>
            {state.status}
          </span>
        </dd>
      </div>
      {#if state.source}
        <div>
          <dt class="text-xs uppercase tracking-wide text-gray-400">Source</dt>
          <dd class="mt-0.5 font-mono text-xs">{state.source}</dd>
        </div>
      {/if}
      <div>
        <dt class="text-xs uppercase tracking-wide text-gray-400">Last sync</dt>
        <dd class="mt-0.5">
          {state.last_sync_at ? new Date(state.last_sync_at).toLocaleString() : 'Never'}
        </dd>
      </div>
      <div>
        <dt class="text-xs uppercase tracking-wide text-gray-400">Next sync</dt>
        <dd class="mt-0.5">
          {state.next_sync_at ? new Date(state.next_sync_at).toLocaleString() : '—'}
        </dd>
      </div>
      {#if state.rows_synced !== undefined}
        <div>
          <dt class="text-xs uppercase tracking-wide text-gray-400">Rows synced</dt>
          <dd class="mt-0.5">{state.rows_synced.toLocaleString()}</dd>
        </div>
      {/if}
      {#if state.bytes_synced !== undefined}
        <div>
          <dt class="text-xs uppercase tracking-wide text-gray-400">Bytes synced</dt>
          <dd class="mt-0.5">{(state.bytes_synced / (1024 * 1024)).toFixed(1)} MB</dd>
        </div>
      {/if}
    </dl>

    {#if state.errors && state.errors.length > 0}
      <div class="rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm dark:border-rose-900/40 dark:bg-rose-950/30">
        <div class="text-xs uppercase tracking-wide text-rose-700 dark:text-rose-300">
          Recent errors
        </div>
        <ul class="mt-2 space-y-1">
          {#each state.errors as err (err.at)}
            <li class="font-mono text-xs text-rose-700 dark:text-rose-300">
              {new Date(err.at).toLocaleString()} — {err.message}
            </li>
          {/each}
        </ul>
      </div>
    {/if}
  {/if}
</section>
