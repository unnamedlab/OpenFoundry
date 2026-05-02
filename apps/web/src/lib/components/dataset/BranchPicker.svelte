<!--
  BranchPicker — dropdown to switch the active dataset branch.

  Lives next to the dataset title in the dataset detail page header.
  Distinct from the older `BranchSelector.svelte` (empty stub kept for
  backwards compat); this component is the Foundry-style picker:
    * lists every branch with the current one marked,
    * lets the user create a new branch from any source branch,
    * lets the user delete the current branch (with confirm),
    * persists the active branch in the URL (?branch=…) so the choice is
      deep-linkable and survives reloads,
    * optionally renders a small schema-diff table vs `master` when the
      caller wires `schemaCurrent` and `schemaMaster` props.

  The component is a *controlled* component: the parent owns
  `currentBranch` / `branches` and reacts to the `onSwitch`, `onCreate`,
  `onDelete` callbacks. URL sync is fired-and-forgotten via SvelteKit's
  `goto`.
-->
<script lang="ts">
  import { goto } from '$app/navigation';
  import { page } from '$app/stores';
  import type { DatasetBranch } from '$lib/api/datasets';

  type SchemaField = { name: string; type?: string };

  type Props = {
    branches: DatasetBranch[];
    currentBranch: string;
    /** Async — the parent should switch the dataset, refresh data, etc. */
    onSwitch: (branchName: string) => Promise<void> | void;
    /**
     * Async — the parent should call `createDatasetBranch` and refresh
     * the branch list. `from` is the source branch name (defaults to the
     * current branch if omitted by the user).
     */
    onCreate: (params: { name: string; from: string; description?: string }) => Promise<void> | void;
    /** Async — typically wraps a DELETE call against the branches API. */
    onDelete?: (branchName: string) => Promise<void> | void;
    /** Optional — current branch schema for diff vs master. */
    schemaCurrent?: SchemaField[];
    /** Optional — master schema for diff vs current. */
    schemaMaster?: SchemaField[];
    /** Disable interactions while a request is in flight. */
    busy?: boolean;
  };

  let {
    branches,
    currentBranch,
    onSwitch,
    onCreate,
    onDelete,
    schemaCurrent,
    schemaMaster,
    busy = false,
  }: Props = $props();

  let open = $state(false);
  let creating = $state(false);
  let confirmingDelete = $state(false);
  let newName = $state('');
  let newFrom = $state('');
  let newDescription = $state('');
  let error = $state('');

  // Derived view: branch list sorted with default first, then alphabetically.
  // Matches the Foundry doc convention where `master` shows on top.
  const sortedBranches = $derived(
    [...branches].sort((a, b) => {
      if (a.is_default !== b.is_default) return a.is_default ? -1 : 1;
      return a.name.localeCompare(b.name);
    }),
  );

  // Pick a sensible default for "new branch from…": current branch,
  // falling back to master if it exists, otherwise the first branch.
  $effect(() => {
    if (!newFrom && branches.length > 0) {
      newFrom = currentBranch || branches.find((b) => b.is_default)?.name || branches[0].name;
    }
  });

  // Schema diff (cheap, client-side). Returns `null` when one side is
  // missing so the panel can render a "no diff available" stub.
  type DiffRow = {
    column: string;
    kind: 'added' | 'removed' | 'changed';
    fromType?: string;
    toType?: string;
  };

  const diff = $derived.by<DiffRow[] | null>(() => {
    if (!schemaCurrent || !schemaMaster) return null;
    const masterByName = new Map(schemaMaster.map((f) => [f.name, f]));
    const currentByName = new Map(schemaCurrent.map((f) => [f.name, f]));
    const rows: DiffRow[] = [];
    for (const f of schemaCurrent) {
      const m = masterByName.get(f.name);
      if (!m) {
        rows.push({ column: f.name, kind: 'added', toType: f.type });
      } else if ((m.type ?? '') !== (f.type ?? '')) {
        rows.push({ column: f.name, kind: 'changed', fromType: m.type, toType: f.type });
      }
    }
    for (const m of schemaMaster) {
      if (!currentByName.has(m.name)) {
        rows.push({ column: m.name, kind: 'removed', fromType: m.type });
      }
    }
    return rows;
  });

  async function selectBranch(name: string) {
    if (busy || name === currentBranch) {
      open = false;
      return;
    }
    error = '';
    try {
      await onSwitch(name);
      // Persist in the URL so a reload keeps the same branch and the
      // user can share a deep link to a specific branch view.
      const url = new URL($page.url);
      url.searchParams.set('branch', name);
      goto(`${url.pathname}${url.search}`, { replaceState: true, keepFocus: true, noScroll: true });
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to switch branch';
    } finally {
      open = false;
    }
  }

  async function submitCreate(event: SubmitEvent) {
    event.preventDefault();
    if (!newName.trim()) {
      error = 'Branch name is required';
      return;
    }
    if (busy) return;
    error = '';
    try {
      await onCreate({
        name: newName.trim(),
        from: newFrom || currentBranch,
        description: newDescription.trim() || undefined,
      });
      newName = '';
      newDescription = '';
      creating = false;
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to create branch';
    }
  }

  async function confirmDelete() {
    if (!onDelete) return;
    error = '';
    try {
      await onDelete(currentBranch);
      confirmingDelete = false;
      open = false;
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to delete branch';
    }
  }

  function toggle() {
    if (busy) return;
    open = !open;
  }

  function diffBadgeClass(kind: DiffRow['kind']) {
    if (kind === 'added') return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300';
    if (kind === 'removed') return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300';
    return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300';
  }
</script>

<div class="relative inline-block text-left">
  <button
    type="button"
    class="inline-flex items-center gap-2 rounded border border-gray-300 bg-white px-3 py-1 text-sm font-medium hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:bg-gray-900 dark:hover:bg-gray-800"
    aria-haspopup="true"
    aria-expanded={open}
    disabled={busy}
    onclick={toggle}
    data-testid="branch-picker-toggle"
  >
    <svg class="h-4 w-4" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
      <path d="M5 3.25a.75.75 0 1 1 1.5 0 .75.75 0 0 1-1.5 0Zm0 9.5a.75.75 0 1 1 1.5 0 .75.75 0 0 1-1.5 0ZM10 8a.75.75 0 1 1 1.5 0 .75.75 0 0 1-1.5 0ZM5.75 4.5a2.25 2.25 0 1 0 0-4.5 2.25 2.25 0 0 0 0 4.5Zm0 11.5a2.25 2.25 0 1 0 0-4.5 2.25 2.25 0 0 0 0 4.5ZM12.5 9.5A2.25 2.25 0 1 0 12.5 5a2.25 2.25 0 0 0 0 4.5Zm-1.5-3.75A.75.75 0 0 1 11.75 5h-.5A.75.75 0 0 1 11 5.75v3.5a3.25 3.25 0 0 1-3.25 3.25H6.5v-1.5h1.25A1.75 1.75 0 0 0 9.5 9.25v-3.5Z"/>
    </svg>
    <span>{currentBranch || '(no branch)'}</span>
    <svg class="h-3 w-3" viewBox="0 0 12 12" fill="currentColor" aria-hidden="true">
      <path d="M3 4.5 6 8l3-3.5H3Z"/>
    </svg>
  </button>

  {#if open}
    <div
      class="absolute right-0 z-20 mt-1 w-80 origin-top-right rounded-md border border-gray-200 bg-white p-2 shadow-lg dark:border-gray-700 dark:bg-gray-900"
      role="menu"
    >
      <div class="max-h-56 overflow-auto">
        {#each sortedBranches as branch (branch.id)}
          <button
            type="button"
            role="menuitem"
            class="flex w-full items-center justify-between rounded px-2 py-1.5 text-left text-sm hover:bg-gray-100 dark:hover:bg-gray-800"
            class:font-semibold={branch.name === currentBranch}
            onclick={() => selectBranch(branch.name)}
          >
            <span class="flex items-center gap-2">
              {#if branch.name === currentBranch}
                <span aria-hidden="true">✓</span>
              {:else}
                <span class="inline-block w-3" aria-hidden="true"></span>
              {/if}
              <span>{branch.name}</span>
              {#if branch.is_default}
                <span class="rounded bg-blue-100 px-1.5 py-0.5 text-xs text-blue-700 dark:bg-blue-900/40 dark:text-blue-200">default</span>
              {/if}
            </span>
            <span class="text-xs text-gray-500">v{branch.version}</span>
          </button>
        {:else}
          <div class="px-2 py-2 text-sm text-gray-500">No branches yet.</div>
        {/each}
      </div>

      <div class="my-2 border-t border-gray-200 dark:border-gray-700"></div>

      {#if !creating}
        <button
          type="button"
          class="block w-full rounded px-2 py-1.5 text-left text-sm hover:bg-gray-100 dark:hover:bg-gray-800"
          onclick={() => { creating = true; error = ''; }}
        >+ New branch from…</button>
      {:else}
        <form class="space-y-2 px-2 py-1" onsubmit={submitCreate}>
          <label class="block text-xs font-medium text-gray-700 dark:text-gray-300">
            Name
            <input
              class="mt-1 block w-full rounded border border-gray-300 px-2 py-1 text-sm dark:border-gray-700 dark:bg-gray-900"
              bind:value={newName}
              placeholder="feature-x"
              required
            />
          </label>
          <label class="block text-xs font-medium text-gray-700 dark:text-gray-300">
            From
            <select
              class="mt-1 block w-full rounded border border-gray-300 px-2 py-1 text-sm dark:border-gray-700 dark:bg-gray-900"
              bind:value={newFrom}
            >
              {#each sortedBranches as branch (branch.id)}
                <option value={branch.name}>{branch.name}</option>
              {/each}
            </select>
          </label>
          <label class="block text-xs font-medium text-gray-700 dark:text-gray-300">
            Description (optional)
            <input
              class="mt-1 block w-full rounded border border-gray-300 px-2 py-1 text-sm dark:border-gray-700 dark:bg-gray-900"
              bind:value={newDescription}
            />
          </label>
          <div class="flex justify-end gap-2 pt-1">
            <button
              type="button"
              class="rounded px-2 py-1 text-xs text-gray-600 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-800"
              onclick={() => { creating = false; error = ''; }}
            >Cancel</button>
            <button
              type="submit"
              class="rounded bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700 disabled:opacity-50"
              disabled={busy || !newName.trim()}
            >Create</button>
          </div>
        </form>
      {/if}

      {#if onDelete && currentBranch}
        {#if !confirmingDelete}
          <button
            type="button"
            class="block w-full rounded px-2 py-1.5 text-left text-sm text-rose-600 hover:bg-rose-50 dark:text-rose-300 dark:hover:bg-rose-900/30"
            onclick={() => { confirmingDelete = true; error = ''; }}
          >Delete branch “{currentBranch}”</button>
        {:else}
          <div class="space-y-2 rounded bg-rose-50 px-2 py-2 text-xs text-rose-700 dark:bg-rose-900/30 dark:text-rose-200">
            <p>Children of <code>{currentBranch}</code> will be re-parented. The branch is soft-deleted (audit retained).</p>
            <div class="flex justify-end gap-2">
              <button
                type="button"
                class="rounded px-2 py-1 hover:bg-rose-100 dark:hover:bg-rose-900/60"
                onclick={() => (confirmingDelete = false)}
              >Cancel</button>
              <button
                type="button"
                class="rounded bg-rose-600 px-2 py-1 font-medium text-white hover:bg-rose-700 disabled:opacity-50"
                onclick={confirmDelete}
                disabled={busy}
              >Delete</button>
            </div>
          </div>
        {/if}
      {/if}

      {#if error}
        <p class="mt-2 px-2 text-xs text-rose-600 dark:text-rose-300">{error}</p>
      {/if}

      {#if diff !== null}
        <div class="mt-3 border-t border-gray-200 pt-2 dark:border-gray-700">
          <p class="px-2 text-xs font-semibold uppercase text-gray-500">Schema diff vs master</p>
          {#if diff.length === 0}
            <p class="px-2 py-1 text-xs text-gray-500">Identical.</p>
          {:else}
            <table class="mt-1 w-full text-xs">
              <thead class="text-left text-gray-500">
                <tr>
                  <th class="px-2 py-1">Column</th>
                  <th class="px-2 py-1">Change</th>
                  <th class="px-2 py-1">Type</th>
                </tr>
              </thead>
              <tbody>
                {#each diff as row (row.column + row.kind)}
                  <tr class="border-t border-gray-100 dark:border-gray-800">
                    <td class="px-2 py-1 font-mono">{row.column}</td>
                    <td class="px-2 py-1">
                      <span class="rounded px-1.5 py-0.5 text-xs {diffBadgeClass(row.kind)}">{row.kind}</span>
                    </td>
                    <td class="px-2 py-1 font-mono text-gray-600 dark:text-gray-400">
                      {#if row.kind === 'changed'}
                        <span>{row.fromType ?? '?'} → {row.toType ?? '?'}</span>
                      {:else if row.kind === 'added'}
                        <span>{row.toType ?? ''}</span>
                      {:else}
                        <span>{row.fromType ?? ''}</span>
                      {/if}
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
        </div>
      {/if}
    </div>
  {/if}
</div>
