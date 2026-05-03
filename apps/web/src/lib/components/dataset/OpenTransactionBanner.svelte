<!--
  P3 — OpenTransactionBanner

  Persistent ámbar banner shown on `/datasets/[id]` when the active
  branch carries `has_open_transaction = true`. Mirrors the Foundry
  guarantee: while a branch points at an OPEN transaction, no further
  transactions can be started until the OPEN one is committed or
  aborted.

  Wired actions:
    * "View transaction"  — deep links into `?tab=history&txn=<id>`,
      which scrolls HistoryTimeline to the highlighted row.
    * "Commit"  — `POST /branches/{branch}/transactions/{txn}:commit`
      (gated by `canManage`).
    * "Abort"   — `POST /branches/{branch}/transactions/{txn}:abort`.

  The component is dumb about the open-tx ID: the parent page resolves
  it (typically by listing transactions where `status = OPEN`) and
  passes the value down. When `openTransactionId` is null the banner
  is rendered in a degraded "No transaction id available" state for
  diagnostic visibility.
-->
<script lang="ts">
  import {
    abortTransaction,
    commitTransaction,
  } from '$lib/api/datasets';
  import { ApiError } from '$lib/api/client';

  type Props = {
    datasetId: string;
    branch: string;
    openTransactionId: string | null;
    canManage: boolean;
    onResolved?: () => void;
  };

  const { datasetId, branch, openTransactionId, canManage, onResolved }: Props = $props();

  let busy = $state(false);
  let error = $state('');

  async function commit() {
    if (!openTransactionId) return;
    busy = true;
    error = '';
    try {
      await commitTransaction(datasetId, branch, openTransactionId);
      onResolved?.();
    } catch (cause) {
      error = cause instanceof ApiError ? cause.message : 'Commit failed';
    } finally {
      busy = false;
    }
  }

  async function abort() {
    if (!openTransactionId) return;
    busy = true;
    error = '';
    try {
      await abortTransaction(datasetId, branch, openTransactionId);
      onResolved?.();
    } catch (cause) {
      error = cause instanceof ApiError ? cause.message : 'Abort failed';
    } finally {
      busy = false;
    }
  }
</script>

<div
  class="rounded-2xl border border-amber-300 bg-amber-50 px-4 py-3 text-sm text-amber-900 shadow-sm dark:border-amber-700 dark:bg-amber-950/40 dark:text-amber-100"
  data-testid="open-transaction-banner"
  role="status"
>
  <div class="flex flex-wrap items-center justify-between gap-3">
    <div class="flex items-start gap-2">
      <span aria-hidden="true">🔓</span>
      <div>
        <p class="font-semibold">An open transaction is in progress on this branch.</p>
        <p class="mt-0.5 text-xs opacity-80">
          New transactions cannot be started until the current one is
          committed or aborted.
        </p>
      </div>
    </div>
    <div class="flex flex-wrap gap-2">
      {#if openTransactionId}
        <a
          href={`/datasets/${encodeURIComponent(datasetId)}?tab=history&txn=${encodeURIComponent(openTransactionId)}`}
          class="rounded border border-amber-400 px-2 py-1 text-xs hover:bg-amber-100 dark:border-amber-600 dark:hover:bg-amber-900/40"
          data-testid="open-transaction-view"
        >View transaction</a>
        {#if canManage}
          <button
            type="button"
            class="rounded bg-emerald-600 px-2 py-1 text-xs font-medium text-white hover:bg-emerald-700 disabled:opacity-50"
            onclick={commit}
            disabled={busy}
            data-testid="open-transaction-commit"
          >Commit</button>
          <button
            type="button"
            class="rounded bg-rose-600 px-2 py-1 text-xs font-medium text-white hover:bg-rose-700 disabled:opacity-50"
            onclick={abort}
            disabled={busy}
            data-testid="open-transaction-abort"
          >Abort</button>
        {/if}
      {:else}
        <span class="text-xs italic opacity-70">No transaction id available — refresh to load.</span>
      {/if}
    </div>
  </div>

  {#if error}
    <p class="mt-2 text-xs text-rose-700 dark:text-rose-300" data-testid="open-transaction-error">{error}</p>
  {/if}
</div>
