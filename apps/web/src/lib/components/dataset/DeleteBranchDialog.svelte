<!--
  P3 — DeleteBranchDialog

  Wraps `GET /datasets/{rid}/branches/{branch}/preview-delete` +
  `DELETE /datasets/{rid}/branches/{branch}`. The pre-call surfaces:
    * the children that will be re-parented to the grandparent,
    * the head transaction RID at deletion time, and
    * the immutable "transactions are NOT deleted" warning.

  The user must type the branch name to confirm.
-->
<script lang="ts">
  import {
    deleteDatasetBranch,
    previewDeleteBranch,
    type DatasetBranch,
    type DatasetBranchDeleteResponse,
    type DatasetBranchPreviewDelete,
  } from '$lib/api/datasets';
  import { ApiError } from '$lib/api/client';

  type Props = {
    datasetRid: string;
    branch: DatasetBranch | null;
    open: boolean;
    onClose: () => void;
    onDeleted?: (response: DatasetBranchDeleteResponse) => void;
  };

  const { datasetRid, branch, open, onClose, onDeleted }: Props = $props();

  let preview = $state<DatasetBranchPreviewDelete | null>(null);
  let typed = $state('');
  let busy = $state(false);
  let error = $state('');

  $effect(() => {
    if (open && branch) {
      preview = null;
      typed = '';
      error = '';
      busy = true;
      previewDeleteBranch(datasetRid, branch.name)
        .then((p) => {
          preview = p;
        })
        .catch((cause) => {
          error = cause instanceof ApiError ? cause.message : 'Failed to load preview';
        })
        .finally(() => {
          busy = false;
        });
    }
  });

  async function confirm() {
    if (!branch) return;
    if (typed.trim() !== branch.name) {
      error = `Type "${branch.name}" exactly to confirm`;
      return;
    }
    busy = true;
    error = '';
    try {
      const result = await deleteDatasetBranch(datasetRid, branch.name);
      onDeleted?.(result);
      onClose();
    } catch (cause) {
      error = cause instanceof ApiError ? cause.message : 'Delete failed';
    } finally {
      busy = false;
    }
  }
</script>

{#if open && branch}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
    role="dialog"
    aria-modal="true"
    data-testid="delete-branch-dialog"
  >
    <div class="w-full max-w-lg rounded-2xl border border-slate-200 bg-white p-5 shadow-xl dark:border-gray-700 dark:bg-gray-900">
      <h2 class="text-lg font-semibold text-rose-600 dark:text-rose-300">Delete branch “{branch.name}”</h2>
      <p class="mt-1 text-sm text-gray-500">
        Soft-delete: the branch row is hidden from active listings but
        the underlying transactions and audit history stay intact.
      </p>

      {#if busy && !preview}
        <p class="mt-3 text-sm text-gray-500" data-testid="delete-branch-loading">Loading preview…</p>
      {:else if preview}
        <div class="mt-3 rounded-lg border border-amber-200 bg-amber-50 p-2 text-xs text-amber-800 dark:border-amber-900/40 dark:bg-amber-900/30 dark:text-amber-100">
          ⚠ Transactions are <b>not</b> deleted. The branch pointer
          ({preview.head_transaction?.rid ?? 'no head'}) is removed
          from the active set; re-creating a branch with the same name
          starts fresh from the parent.
        </div>

        <section class="mt-3" data-testid="delete-branch-reparent-plan">
          <p class="text-sm font-semibold">Children to re-parent</p>
          {#if preview.children_to_reparent.length === 0}
            <p class="mt-1 text-xs text-gray-500">No children — only this branch will be removed.</p>
          {:else}
            <ul class="mt-1 space-y-1 text-xs">
              {#each preview.children_to_reparent as child}
                <li class="flex justify-between gap-2 rounded bg-slate-50 px-2 py-1 dark:bg-gray-800">
                  <span class="font-mono">{child.branch}</span>
                  <span class="text-gray-500">→ {child.new_parent ?? '(root)'}</span>
                </li>
              {/each}
            </ul>
          {/if}
        </section>
      {/if}

      <label class="mt-4 block text-sm">
        <span class="text-gray-500">Type
          <code>{branch.name}</code> to confirm</span>
        <input
          type="text"
          bind:value={typed}
          class="mt-1 block w-full rounded border border-slate-300 px-2 py-1 dark:border-gray-700 dark:bg-gray-800"
          data-testid="delete-branch-confirm-input"
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
          data-testid="delete-branch-cancel"
        >Cancel</button>
        <button
          type="button"
          class="rounded bg-rose-600 px-3 py-1 text-sm font-medium text-white hover:bg-rose-700 disabled:opacity-50"
          onclick={confirm}
          disabled={busy || typed.trim() !== branch.name}
          data-testid="delete-branch-confirm"
        >Delete</button>
      </div>
    </div>
  </div>
{/if}
