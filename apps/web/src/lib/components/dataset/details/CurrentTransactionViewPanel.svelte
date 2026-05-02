<!--
  T5.1 — CurrentTransactionViewPanel

  Foundry's "Current transaction view" surfaces the transaction at
  HEAD of the active branch: type, author, timestamp, file count,
  total bytes, plus a list of every transaction whose contribution is
  still part of the live view.
-->
<script lang="ts">
  import type { DatasetTransaction } from '$lib/api/datasets';

  type Props = {
    head: DatasetTransaction | null;
    composedOf: DatasetTransaction[];
    fileCount: number;
    totalBytes: number;
  };

  const { head, composedOf, fileCount, totalBytes }: Props = $props();

  function operationTone(op: string): string {
    const u = op.toUpperCase();
    if (u === 'SNAPSHOT')
      return 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300';
    if (u === 'APPEND')
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300';
    if (u === 'UPDATE')
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300';
    if (u === 'DELETE')
      return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300';
    return 'bg-slate-100 text-slate-700 dark:bg-gray-800 dark:text-gray-300';
  }

  function fmtBytes(b: number): string {
    if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`;
    return `${(b / (1024 * 1024)).toFixed(1)} MB`;
  }
</script>

<section class="space-y-4">
  <header>
    <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Current transaction view</div>
    <h2 class="mt-1 text-lg font-semibold">HEAD of branch</h2>
    <p class="mt-1 text-sm text-gray-500">The transaction whose contents make up the live view.</p>
  </header>

  {#if !head}
    <div class="rounded-xl border border-dashed border-slate-300 px-4 py-8 text-center text-sm text-gray-500 dark:border-gray-700">
      No transactions have been committed yet.
    </div>
  {:else}
    <div class="rounded-xl border border-slate-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-900">
      <div class="flex items-center justify-between gap-3">
        <span class="font-mono text-xs">{head.id}</span>
        <span class={`rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide ${operationTone(head.operation)}`}>
          {head.operation}
        </span>
      </div>
      <dl class="mt-3 grid grid-cols-2 gap-3 text-sm">
        <div>
          <dt class="text-xs uppercase tracking-wide text-gray-400">Status</dt>
          <dd class="mt-0.5">{head.status}</dd>
        </div>
        <div>
          <dt class="text-xs uppercase tracking-wide text-gray-400">Branch</dt>
          <dd class="mt-0.5 font-mono text-xs">{head.branch_name ?? '—'}</dd>
        </div>
        <div>
          <dt class="text-xs uppercase tracking-wide text-gray-400">Created</dt>
          <dd class="mt-0.5">{new Date(head.created_at).toLocaleString()}</dd>
        </div>
        <div>
          <dt class="text-xs uppercase tracking-wide text-gray-400">Committed</dt>
          <dd class="mt-0.5">
            {head.committed_at ? new Date(head.committed_at).toLocaleString() : '—'}
          </dd>
        </div>
        <div>
          <dt class="text-xs uppercase tracking-wide text-gray-400">Files</dt>
          <dd class="mt-0.5">{fileCount.toLocaleString()}</dd>
        </div>
        <div>
          <dt class="text-xs uppercase tracking-wide text-gray-400">Total size</dt>
          <dd class="mt-0.5">{fmtBytes(totalBytes)}</dd>
        </div>
      </dl>
      {#if head.summary}
        <p class="mt-3 text-sm text-gray-600 dark:text-gray-300">{head.summary}</p>
      {/if}
    </div>
  {/if}

  {#if composedOf.length > 0}
    <div>
      <div class="text-xs uppercase tracking-wide text-gray-400">Composed of</div>
      <ul class="mt-2 divide-y divide-slate-100 overflow-hidden rounded-xl border border-slate-200 bg-white dark:divide-gray-800 dark:border-gray-700 dark:bg-gray-900">
        {#each composedOf as tx (tx.id)}
          <li class="flex items-center justify-between gap-3 px-3 py-2 text-sm">
            <span class="flex items-center gap-2">
              <span class={`rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide ${operationTone(tx.operation)}`}>
                {tx.operation}
              </span>
              <span class="font-mono text-xs">{tx.id.slice(0, 12)}…</span>
            </span>
            <span class="text-xs text-gray-500">
              {new Date(tx.created_at).toLocaleString()}
            </span>
          </li>
        {/each}
      </ul>
    </div>
  {/if}
</section>
