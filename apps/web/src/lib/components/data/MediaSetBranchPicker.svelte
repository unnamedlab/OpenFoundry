<!--
  MediaSetBranchPicker — H4 branch switcher for the media-set detail
  page. Mirrors `dataset/BranchPicker.svelte`'s controlled-component
  contract (parent owns the active branch + the list, this component
  renders + emits) so the surface stays consistent across the
  Foundry-style detail pages.

  The picker shows:
    * The current branch as the trigger label.
    * A dropdown listing every branch on the media set, with the
      `main` row pinned at the top.
    * A "+ New branch" affordance that captures `name` + optional
      `from_branch` and emits `onCreate`.

  Reset / merge are exposed through separate dialogs the History tab
  drives — keeping this picker focused on branch *navigation* mirrors
  the dataset surface and avoids overloading the trigger.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { browser } from '$app/environment';
  import type { MediaSetBranch } from '$lib/api/mediaSets';

  type Props = {
    branches: MediaSetBranch[];
    currentBranch: string;
    onSwitch: (branchName: string) => Promise<void> | void;
    onCreate: (params: {
      name: string;
      from_branch: string;
      from_transaction_rid?: string;
    }) => Promise<void> | void;
    onDelete?: (branchName: string) => Promise<void> | void;
    busy?: boolean;
  };

  let {
    branches,
    currentBranch,
    onSwitch,
    onCreate,
    onDelete,
    busy = false,
  }: Props = $props();

  let open = $state(false);
  let creating = $state(false);
  let newName = $state('');
  let newFrom = $state('main');

  const sortedBranches = $derived(
    [...branches].sort((a, b) => {
      if (a.branch_name === 'main') return -1;
      if (b.branch_name === 'main') return 1;
      return a.branch_name.localeCompare(b.branch_name);
    }),
  );

  function toggle() {
    open = !open;
  }

  async function selectBranch(name: string) {
    if (name === currentBranch) {
      open = false;
      return;
    }
    await onSwitch(name);
    open = false;
  }

  async function submitCreate() {
    if (!newName.trim()) return;
    await onCreate({ name: newName.trim(), from_branch: newFrom });
    newName = '';
    creating = false;
    open = false;
  }

  // Close on outside click — same UX as the dataset picker.
  function handleDocumentClick(event: MouseEvent) {
    const target = event.target as HTMLElement;
    if (!target.closest('[data-testid="media-set-branch-picker"]')) {
      open = false;
    }
  }

  onMount(() => {
    if (!browser) return;
    document.addEventListener('click', handleDocumentClick);
    return () => document.removeEventListener('click', handleDocumentClick);
  });
</script>

<div class="relative inline-block" data-testid="media-set-branch-picker">
  <button
    type="button"
    class="inline-flex items-center gap-2 rounded-xl border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium hover:bg-slate-50 dark:border-gray-700 dark:bg-gray-900 dark:hover:bg-gray-800"
    onclick={toggle}
    disabled={busy}
    data-testid="media-set-branch-picker-toggle"
  >
    <svg class="h-4 w-4" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
      <path d="M5 3.25a2.25 2.25 0 1 1 2.5 2.236v3.028a2.25 2.25 0 1 1-2.5 0V5.486A2.251 2.251 0 0 1 5 3.25Zm6 0a2.25 2.25 0 0 1 1.25 4.122v.628c0 1.105-.895 2-2 2h-1.5a.75.75 0 0 1 0-1.5h1.5a.5.5 0 0 0 .5-.5v-.628A2.25 2.25 0 0 1 11 3.25Z"></path>
    </svg>
    <span>{currentBranch}</span>
    <svg class="h-3 w-3" viewBox="0 0 12 12" fill="currentColor" aria-hidden="true">
      <path d="M3 4.5l3 3 3-3" />
    </svg>
  </button>

  {#if open}
    <div
      class="absolute right-0 z-30 mt-1 w-80 rounded-xl border border-slate-200 bg-white p-2 shadow-lg dark:border-gray-700 dark:bg-gray-900"
      data-testid="media-set-branch-picker-menu"
    >
      <ul class="space-y-1 text-sm">
        {#each sortedBranches as branch (branch.branch_rid)}
          <li class="flex items-center justify-between gap-2">
            <button
              type="button"
              class={`flex-1 truncate rounded-lg px-2 py-1 text-left hover:bg-slate-100 dark:hover:bg-gray-800 ${branch.branch_name === currentBranch ? 'font-semibold text-blue-600 dark:text-blue-300' : ''}`}
              data-testid={`media-set-branch-option-${branch.branch_name}`}
              onclick={() => selectBranch(branch.branch_name)}
            >
              {branch.branch_name}
              {#if !branch.parent_branch_rid}
                <span class="ml-2 rounded-full bg-slate-100 px-1.5 py-0.5 text-[10px] uppercase text-slate-600 dark:bg-gray-800 dark:text-slate-300">
                  root
                </span>
              {/if}
            </button>
            {#if onDelete && branch.branch_name !== 'main'}
              <button
                type="button"
                class="rounded p-1 text-rose-500 hover:bg-rose-50 dark:hover:bg-rose-950/40"
                title="Delete branch"
                data-testid={`media-set-branch-delete-${branch.branch_name}`}
                onclick={() => onDelete(branch.branch_name)}
              >
                ×
              </button>
            {/if}
          </li>
        {/each}
      </ul>

      <div class="mt-2 border-t border-slate-200 pt-2 dark:border-gray-800">
        {#if creating}
          <div class="space-y-2">
            <input
              type="text"
              placeholder="branch name"
              class="w-full rounded-lg border border-slate-300 px-2 py-1 text-sm dark:border-gray-700 dark:bg-gray-800"
              bind:value={newName}
              data-testid="media-set-branch-new-name"
            />
            <select
              class="w-full rounded-lg border border-slate-300 px-2 py-1 text-sm dark:border-gray-700 dark:bg-gray-800"
              bind:value={newFrom}
              data-testid="media-set-branch-new-from"
            >
              {#each sortedBranches as branch (branch.branch_rid)}
                <option value={branch.branch_name}>{branch.branch_name}</option>
              {/each}
            </select>
            <div class="flex justify-end gap-1">
              <button
                type="button"
                class="rounded-lg px-2 py-1 text-xs text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-gray-800"
                onclick={() => (creating = false)}
              >
                Cancel
              </button>
              <button
                type="button"
                class="rounded-lg bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700 disabled:opacity-50"
                disabled={!newName.trim() || busy}
                data-testid="media-set-branch-new-submit"
                onclick={submitCreate}
              >
                Create
              </button>
            </div>
          </div>
        {:else}
          <button
            type="button"
            class="w-full rounded-lg px-2 py-1 text-left text-xs text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-gray-800"
            data-testid="media-set-branch-new"
            onclick={() => (creating = true)}
          >
            + New branch
          </button>
        {/if}
      </div>
    </div>
  {/if}
</div>
