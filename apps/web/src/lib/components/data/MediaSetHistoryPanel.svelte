<!--
  MediaSetHistoryPanel — VersionTimeline-equivalent for media sets.

  Lists every transaction on the media set with the per-transaction
  diff (`items_added`, `items_modified`, `items_deleted`) the
  `GET /media-sets/{rid}/transactions` endpoint computes server-side.
  Each row exposes a "Restore to this point" affordance that mints a
  new branch off the transaction RID via the existing
  `POST /media-sets/{rid}/branches` endpoint with `from_transaction_rid`.

  Why a fresh branch instead of in-place rewind: per
  `Core concepts/Branching.md`, Foundry never mutates committed
  transactions; the canonical "restore" is a new branch whose head
  pointer is the historical transaction. The UI follows the same
  contract.
-->
<script lang="ts">
  import {
    createMediaSetBranch,
    listMediaSetTransactions,
    type MediaSet,
    type MediaSetTransactionHistoryEntry,
  } from '$lib/api/mediaSets';
  import { notifications as toasts } from '$stores/notifications';

  type Props = {
    mediaSet: MediaSet;
    /** Notified after a successful "Restore" so the parent can switch
     *  branches via the picker. */
    onBranchCreated?: (branchName: string) => void;
  };

  let { mediaSet, onBranchCreated }: Props = $props();

  let entries = $state<MediaSetTransactionHistoryEntry[]>([]);
  let loading = $state(true);
  let error = $state('');
  let restoringRid = $state<string | null>(null);

  $effect(() => {
    void mediaSet.rid;
    void load();
  });

  async function load() {
    loading = true;
    error = '';
    try {
      entries = await listMediaSetTransactions(mediaSet.rid);
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to load history';
    } finally {
      loading = false;
    }
  }

  async function restore(entry: MediaSetTransactionHistoryEntry) {
    if (restoringRid) return;
    const branchName = window.prompt(
      `Restore as a new branch off transaction ${entry.rid.slice(-12)}.\nBranch name:`,
      `restore-${entry.rid.split('.').pop()?.slice(0, 8) ?? 'point'}`,
    );
    if (!branchName) return;
    restoringRid = entry.rid;
    try {
      const branch = await createMediaSetBranch(mediaSet.rid, {
        name: branchName,
        from_branch: entry.branch,
        from_transaction_rid: entry.rid,
      });
      toasts.success(`Branch '${branch.branch_name}' created at this point`);
      onBranchCreated?.(branch.branch_name);
    } catch (cause) {
      toasts.error(
        cause instanceof Error ? cause.message : 'Failed to create branch',
      );
    } finally {
      restoringRid = null;
    }
  }

  function badgeTone(state: string) {
    switch (state) {
      case 'COMMITTED':
        return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300';
      case 'ABORTED':
        return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300';
      case 'OPEN':
        return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300';
      default:
        return 'bg-slate-100 text-slate-700 dark:bg-gray-800 dark:text-slate-300';
    }
  }
</script>

<section class="space-y-4" data-testid="media-set-history-panel">
  <header class="flex items-start justify-between gap-3">
    <div>
      <h2 class="text-base font-semibold">History</h2>
      <p class="mt-1 text-xs text-slate-500">
        Every transaction recorded on this media set, newest first.
        Each entry shows the items added, modified (path-deduplicated)
        and deleted in that batch. "Restore to this point" mints a new
        branch off the historical transaction (per Foundry's
        immutable-history contract).
      </p>
    </div>
    <button
      type="button"
      class="rounded-xl border border-slate-200 px-3 py-1.5 text-xs hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800"
      data-testid="media-set-history-refresh"
      onclick={load}
      disabled={loading}
    >
      {loading ? 'Refreshing…' : 'Refresh'}
    </button>
  </header>

  {#if error}
    <div
      class="rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300"
      data-testid="media-set-history-error"
    >
      {error}
    </div>
  {:else if loading && entries.length === 0}
    <div
      class="h-24 animate-pulse rounded-xl border border-slate-200 bg-slate-100 dark:border-gray-700 dark:bg-gray-800"
      data-testid="media-set-history-skeleton"
    ></div>
  {:else if entries.length === 0}
    <div
      class="rounded-xl border border-dashed border-slate-300 p-6 text-center text-sm text-slate-500 dark:border-gray-700"
      data-testid="media-set-history-empty"
    >
      No transactions yet. Open a transaction on a transactional set or
      upload directly to a transactionless set to start the history.
    </div>
  {:else}
    <ul class="space-y-2" data-testid="media-set-history-entries">
      {#each entries as entry (entry.rid)}
        <li
          class="rounded-xl border border-slate-200 p-3 dark:border-gray-700"
          data-testid="media-set-history-entry"
          data-transaction-rid={entry.rid}
        >
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <div class="flex items-center gap-2 text-sm">
                <span class="font-medium">{entry.branch}</span>
                <span class={`rounded-full px-2 py-0.5 text-[10px] uppercase tracking-wide ${badgeTone(entry.state)}`}>
                  {entry.state}
                </span>
                <span class="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] uppercase tracking-wide text-slate-600 dark:bg-gray-800 dark:text-slate-300">
                  {entry.write_mode}
                </span>
              </div>
              <div class="mt-0.5 text-[11px] text-slate-500">
                {entry.opened_by || 'unknown'} ·
                {new Date(entry.opened_at).toLocaleString()}
                {#if entry.closed_at}
                  · closed {new Date(entry.closed_at).toLocaleString()}
                {/if}
              </div>
              <div class="mt-1 font-mono text-[10px] text-slate-400">{entry.rid}</div>
              <div class="mt-2 flex flex-wrap gap-2 text-[11px]">
                <span
                  class="rounded-full bg-emerald-50 px-2 py-0.5 text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-300"
                  data-testid="media-set-history-added"
                >
                  +{entry.items_added} added
                </span>
                <span
                  class="rounded-full bg-amber-50 px-2 py-0.5 text-amber-700 dark:bg-amber-950/30 dark:text-amber-300"
                  data-testid="media-set-history-modified"
                >
                  ~{entry.items_modified} modified
                </span>
                <span
                  class="rounded-full bg-rose-50 px-2 py-0.5 text-rose-700 dark:bg-rose-950/30 dark:text-rose-300"
                  data-testid="media-set-history-deleted"
                >
                  −{entry.items_deleted} deleted
                </span>
              </div>
            </div>
            <button
              type="button"
              class="rounded-xl border border-slate-200 px-3 py-1.5 text-xs hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800 disabled:opacity-50"
              data-testid="media-set-history-restore"
              disabled={restoringRid === entry.rid || entry.state !== 'COMMITTED'}
              title={entry.state !== 'COMMITTED'
                ? 'Only committed transactions can be used as a restore point.'
                : 'Create a new branch off this transaction.'}
              onclick={() => restore(entry)}
            >
              {restoringRid === entry.rid ? 'Restoring…' : 'Restore to this point'}
            </button>
          </div>
        </li>
      {/each}
    </ul>
  {/if}
</section>
