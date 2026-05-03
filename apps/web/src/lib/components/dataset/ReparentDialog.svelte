<!--
  P3 — ReparentDialog

  Wraps the `POST /datasets/{rid}/branches/{branch}:reparent` flow
  with a confirmation modal. Triggered by a drag&drop in
  `BranchGraph.svelte`:

      onReparentRequest({ source, candidateParent })

  The dialog summarises:
    * source branch + current parent + new parent,
    * children that move *with* the source (none — re-parenting only
      changes the branch ancestry record per Foundry guarantees), and
    * the resulting fallback chain (parent name prepended).

  The user must type the source branch name to confirm — same gate as
  the delete dialog.
-->
<script lang="ts">
  import { reparentBranch, type DatasetBranch } from '$lib/api/datasets';
  import { ApiError } from '$lib/api/client';

  type Props = {
    datasetRid: string;
    source: DatasetBranch | null;
    candidateParent: DatasetBranch | null;
    open: boolean;
    onClose: () => void;
    /** Called when the reparent succeeds. Parent re-fetches the branch list. */
    onConfirmed?: (updated: DatasetBranch) => void;
  };

  const { datasetRid, source, candidateParent, open, onClose, onConfirmed }: Props = $props();

  let typed = $state('');
  let busy = $state(false);
  let error = $state('');

  $effect(() => {
    if (open) {
      typed = '';
      error = '';
    }
  });

  const expectedFallback = $derived.by(() => {
    if (!candidateParent) return [];
    const head = candidateParent.name;
    const rest = (source?.fallback_chain ?? []).filter((b) => b !== head);
    return [head, ...rest];
  });

  async function confirm() {
    if (!source || !candidateParent) return;
    if (typed.trim() !== source.name) {
      error = `Type "${source.name}" exactly to confirm`;
      return;
    }
    busy = true;
    error = '';
    try {
      const updated = await reparentBranch(datasetRid, source.name, {
        new_parent_branch: candidateParent.name,
      });
      onConfirmed?.(updated);
      onClose();
    } catch (cause) {
      error = cause instanceof ApiError ? cause.message : 'Reparent failed';
    } finally {
      busy = false;
    }
  }
</script>

{#if open && source && candidateParent}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
    role="dialog"
    aria-modal="true"
    data-testid="reparent-dialog"
  >
    <div class="w-full max-w-lg rounded-2xl border border-slate-200 bg-white p-5 shadow-xl dark:border-gray-700 dark:bg-gray-900">
      <h2 class="text-lg font-semibold">Re-parent branch</h2>
      <p class="mt-1 text-sm text-gray-500">
        Foundry guarantees re-parenting only changes the ancestry
        record — no transactions are moved or rewritten.
      </p>

      <dl class="mt-3 space-y-1 text-sm">
        <div class="flex justify-between gap-3">
          <dt class="text-gray-500">Branch</dt>
          <dd class="font-mono">{source.name}</dd>
        </div>
        <div class="flex justify-between gap-3">
          <dt class="text-gray-500">Current parent</dt>
          <dd class="font-mono">
            {(() => {
              const id = source.parent_branch_id;
              return id ? (id as string).slice(0, 8) + '…' : '— (root)';
            })()}
          </dd>
        </div>
        <div class="flex justify-between gap-3">
          <dt class="text-gray-500">New parent</dt>
          <dd class="font-mono">{candidateParent.name}</dd>
        </div>
      </dl>

      <div class="mt-3 rounded-lg border border-slate-200 bg-slate-50 p-2 text-xs dark:border-gray-700 dark:bg-gray-800">
        <p class="font-semibold">Children moving with this branch</p>
        <p class="mt-1 text-gray-500">
          None — re-parenting only changes the branch ancestry record.
          Direct descendants of <code>{source.name}</code> still point
          at it, so its sub-tree is preserved as-is.
        </p>
      </div>

      <div class="mt-3 rounded-lg border border-slate-200 bg-slate-50 p-2 text-xs dark:border-gray-700 dark:bg-gray-800">
        <p class="font-semibold">Resulting fallback chain</p>
        <p class="mt-1 font-mono text-[11px]" data-testid="reparent-dialog-fallback-preview">
          {expectedFallback.length === 0 ? '—' : expectedFallback.join(' → ')}
        </p>
      </div>

      <label class="mt-4 block text-sm">
        <span class="text-gray-500">Type
          <code>{source.name}</code> to confirm</span>
        <input
          type="text"
          bind:value={typed}
          class="mt-1 block w-full rounded border border-slate-300 px-2 py-1 dark:border-gray-700 dark:bg-gray-800"
          data-testid="reparent-dialog-confirm-input"
        />
      </label>

      {#if error}
        <p class="mt-2 text-xs text-rose-600 dark:text-rose-300">{error}</p>
      {/if}

      <div class="mt-4 flex justify-end gap-2">
        <button
          type="button"
          class="rounded border border-slate-300 px-3 py-1 text-sm hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800"
          onclick={onClose}
          data-testid="reparent-dialog-cancel"
        >Cancel</button>
        <button
          type="button"
          class="rounded bg-blue-600 px-3 py-1 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
          onclick={confirm}
          disabled={busy || typed.trim() !== source.name}
          data-testid="reparent-dialog-confirm"
        >Reparent</button>
      </div>
    </div>
  </div>
{/if}
