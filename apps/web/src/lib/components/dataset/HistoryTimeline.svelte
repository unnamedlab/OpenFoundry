<!--
  T5.3 — HistoryTimeline

  Vertical timeline of every transaction on the active branch. Each
  row carries an icon by transaction type (SNAPSHOT / APPEND / UPDATE
  / DELETE), the author, the +/- file count delta, and two actions:

    * "View at this point in time" — emits an `onView` callback with
      the transaction id so the parent can redirect to the Preview
      tab pinned to that transaction.
    * "Roll back to this transaction" — emits `onRollback` so the
      parent can call the dataset-versioning-service endpoint that
      creates a new SNAPSHOT with the chosen view's contents.
-->
<script lang="ts">
  import type { DatasetTransaction } from '$lib/api/datasets';

  type Props = {
    transactions: DatasetTransaction[];
    /** True while a rollback is in flight so we can disable buttons. */
    rollingBack?: string | null;
    onView?: (tx: DatasetTransaction) => void;
    onRollback?: (tx: DatasetTransaction) => void | Promise<void>;
  };

  const {
    transactions,
    rollingBack = null,
    onView = () => {},
    onRollback = () => {},
  }: Props = $props();

  function icon(op: string): string {
    const u = op.toUpperCase();
    if (u === 'SNAPSHOT') return '📸';
    if (u === 'APPEND') return '➕';
    if (u === 'UPDATE') return '✏️';
    if (u === 'DELETE') return '🗑️';
    return '•';
  }

  function tone(op: string): string {
    const u = op.toUpperCase();
    if (u === 'SNAPSHOT')
      return 'border-blue-300 bg-blue-50 dark:border-blue-900/40 dark:bg-blue-950/30';
    if (u === 'APPEND')
      return 'border-emerald-300 bg-emerald-50 dark:border-emerald-900/40 dark:bg-emerald-950/30';
    if (u === 'UPDATE')
      return 'border-amber-300 bg-amber-50 dark:border-amber-900/40 dark:bg-amber-950/30';
    if (u === 'DELETE')
      return 'border-rose-300 bg-rose-50 dark:border-rose-900/40 dark:bg-rose-950/30';
    return 'border-slate-300 bg-slate-50 dark:border-slate-700 dark:bg-slate-900/40';
  }

  function delta(tx: DatasetTransaction): string {
    const added = Number(tx.metadata?.['files_added'] ?? 0);
    const removed = Number(tx.metadata?.['files_removed'] ?? 0);
    const parts: string[] = [];
    if (added) parts.push(`+${added}`);
    if (removed) parts.push(`−${removed}`);
    return parts.join(' / ') || '—';
  }

  function author(tx: DatasetTransaction): string {
    const a = tx.metadata?.['author'];
    if (typeof a === 'string') return a;
    return 'system';
  }
</script>

<section class="space-y-3">
  <header>
    <div class="text-xs uppercase tracking-[0.22em] text-gray-400">History</div>
    <h2 class="mt-1 text-lg font-semibold">Transaction timeline</h2>
    <p class="mt-1 text-sm text-gray-500">
      {transactions.length} transaction{transactions.length === 1 ? '' : 's'} on the active branch.
    </p>
  </header>

  {#if transactions.length === 0}
    <div class="rounded-xl border border-dashed border-slate-300 px-4 py-8 text-center text-sm text-gray-500 dark:border-gray-700">
      No transactions yet.
    </div>
  {:else}
    <ol class="relative space-y-3 border-l border-slate-200 pl-5 dark:border-gray-700">
      {#each transactions as tx (tx.id)}
        <li class={`rounded-xl border p-3 ${tone(tx.operation)}`}>
          <div class="flex items-start justify-between gap-3">
            <div class="flex items-start gap-2">
              <span class="text-lg leading-none" aria-hidden="true">{icon(tx.operation)}</span>
              <div>
                <div class="flex items-center gap-2 text-sm font-medium">
                  <span class="uppercase">{tx.operation}</span>
                  <span class="rounded-full bg-white/60 px-2 py-0.5 text-[10px] uppercase tracking-wide text-gray-700 dark:bg-gray-800/60 dark:text-gray-200">
                    {tx.status}
                  </span>
                </div>
                <div class="mt-0.5 text-xs text-gray-600 dark:text-gray-300">
                  {author(tx)} · {new Date(tx.created_at).toLocaleString()} · files {delta(tx)}
                </div>
                <div class="mt-0.5 font-mono text-[10px] text-gray-500">{tx.id}</div>
                {#if tx.summary}
                  <p class="mt-1 text-sm">{tx.summary}</p>
                {/if}
              </div>
            </div>
            <div class="flex shrink-0 flex-col gap-1">
              <button
                type="button"
                class="rounded-md border border-slate-300 px-2 py-1 text-xs hover:bg-white dark:border-gray-700 dark:hover:bg-gray-800"
                onclick={() => onView(tx)}
              >
                View at this point in time
              </button>
              <button
                type="button"
                class="rounded-md border border-slate-300 px-2 py-1 text-xs hover:bg-white disabled:opacity-50 dark:border-gray-700 dark:hover:bg-gray-800"
                disabled={rollingBack === tx.id}
                onclick={() => onRollback(tx)}
              >
                {rollingBack === tx.id ? 'Rolling back…' : 'Roll back to this transaction'}
              </button>
            </div>
          </div>
        </li>
      {/each}
    </ol>
  {/if}
</section>
