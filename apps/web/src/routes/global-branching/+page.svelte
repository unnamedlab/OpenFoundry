<!--
  P4 — Global Branching dashboard.

  Foundry "Global Branching application": list every global branch,
  surface link counts (in_sync / drifted / archived) per branch, and
  emit `:promote` requests with one click. Creating a branch + linking
  resources happens in the same form so the operator can stand up a
  workstream end-to-end without leaving the page.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import {
    addGlobalBranchLink,
    createGlobalBranch,
    getGlobalBranch,
    listGlobalBranchResources,
    listGlobalBranches,
    promoteGlobalBranch,
    type GlobalBranch,
    type GlobalBranchLink,
    type GlobalBranchSummary,
  } from '$lib/api/global-branches';
  import { ApiError } from '$lib/api/client';

  let branches = $state<GlobalBranch[]>([]);
  let selected = $state<GlobalBranchSummary | null>(null);
  let resources = $state<GlobalBranchLink[]>([]);
  let loading = $state(true);
  let error = $state('');

  // Create form
  let createName = $state('');
  let createDescription = $state('');
  let creating = $state(false);

  // Link form
  let linkResourceType = $state('dataset');
  let linkResourceRid = $state('');
  let linkBranchRid = $state('');
  let linking = $state(false);

  let promoting = $state(false);
  let promoteResult = $state<string | null>(null);

  async function load() {
    loading = true;
    try {
      branches = await listGlobalBranches();
      if (branches.length > 0 && !selected) {
        await select(branches[0].id);
      }
    } catch (cause) {
      error = cause instanceof ApiError ? cause.message : 'Failed to load global branches';
    } finally {
      loading = false;
    }
  }

  async function select(id: string) {
    try {
      selected = await getGlobalBranch(id);
      resources = await listGlobalBranchResources(id);
      promoteResult = null;
    } catch (cause) {
      error = cause instanceof ApiError ? cause.message : 'Failed to load branch detail';
    }
  }

  async function submitCreate(event: SubmitEvent) {
    event.preventDefault();
    creating = true;
    error = '';
    try {
      const created = await createGlobalBranch({
        name: createName.trim(),
        description: createDescription.trim() || undefined,
      });
      createName = '';
      createDescription = '';
      await load();
      await select(created.id);
    } catch (cause) {
      error = cause instanceof ApiError ? cause.message : 'Failed to create global branch';
    } finally {
      creating = false;
    }
  }

  async function submitLink(event: SubmitEvent) {
    event.preventDefault();
    if (!selected) return;
    linking = true;
    error = '';
    try {
      await addGlobalBranchLink(selected.id, {
        resource_type: linkResourceType,
        resource_rid: linkResourceRid.trim(),
        branch_rid: linkBranchRid.trim(),
      });
      linkResourceRid = '';
      linkBranchRid = '';
      await select(selected.id);
    } catch (cause) {
      error = cause instanceof ApiError ? cause.message : 'Failed to add link';
    } finally {
      linking = false;
    }
  }

  async function promote() {
    if (!selected) return;
    promoting = true;
    error = '';
    try {
      const res = await promoteGlobalBranch(selected.id);
      promoteResult = `event_id=${res.event_id} → ${res.topic}`;
    } catch (cause) {
      error = cause instanceof ApiError ? cause.message : 'Promote failed';
    } finally {
      promoting = false;
    }
  }

  function statusTone(status: GlobalBranchLink['status']): string {
    if (status === 'in_sync')
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300';
    if (status === 'drifted')
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300';
    return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300';
  }

  onMount(() => {
    void load();
  });
</script>

<svelte:head>
  <title>Global Branching</title>
</svelte:head>

<div class="mx-auto max-w-7xl space-y-4 px-4 py-6" data-testid="global-branching-dashboard">
  <header>
    <p class="text-xs uppercase tracking-[0.22em] text-gray-400">Developer toolchain</p>
    <h1 class="mt-1 text-2xl font-bold">Global Branching</h1>
    <p class="mt-1 text-sm text-gray-500">
      Coordinate cross-plane branches. Every Foundry plane (datasets,
      ontology, pipelines, code repos) keeps owning its local branches;
      a global branch labels a workstream that spans them.
    </p>
  </header>

  {#if error}
    <div class="rounded-xl border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-800 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300">
      {error}
    </div>
  {/if}

  <div class="grid gap-4 lg:grid-cols-[1fr,2fr]">
    <aside class="space-y-3">
      <form
        class="space-y-2 rounded-2xl border border-slate-200 bg-white p-3 shadow-sm dark:border-gray-700 dark:bg-gray-900"
        onsubmit={submitCreate}
        data-testid="global-branching-create-form"
      >
        <h2 class="text-sm font-semibold">Create global branch</h2>
        <input
          class="w-full rounded border border-slate-300 px-2 py-1 text-sm dark:border-gray-700 dark:bg-gray-800"
          placeholder="Name (e.g. release-2026-Q3)"
          bind:value={createName}
          data-testid="global-branching-name-input"
          required
        />
        <input
          class="w-full rounded border border-slate-300 px-2 py-1 text-sm dark:border-gray-700 dark:bg-gray-800"
          placeholder="Description (optional)"
          bind:value={createDescription}
        />
        <div class="flex justify-end">
          <button
            type="submit"
            class="rounded bg-blue-600 px-3 py-1 text-xs font-medium text-white hover:bg-blue-700 disabled:opacity-50"
            disabled={creating || !createName.trim()}
            data-testid="global-branching-create-submit"
          >Create</button>
        </div>
      </form>

      <div class="rounded-2xl border border-slate-200 bg-white shadow-sm dark:border-gray-700 dark:bg-gray-900" data-testid="global-branching-list">
        {#if loading}
          <p class="px-3 py-2 text-sm text-gray-500">Loading…</p>
        {:else if branches.length === 0}
          <p class="px-3 py-2 text-sm text-gray-500">No global branches yet.</p>
        {:else}
          <ul>
            {#each branches as b (b.id)}
              <li>
                <button
                  type="button"
                  class={`block w-full px-3 py-2 text-left text-sm hover:bg-slate-50 dark:hover:bg-gray-800 ${selected?.id === b.id ? 'bg-blue-50 dark:bg-blue-950/30' : ''}`}
                  onclick={() => void select(b.id)}
                  data-testid={`global-branching-row-${b.name}`}
                >
                  <span class="font-medium">{b.name}</span>
                  <span class="block text-xs text-gray-500">{b.rid}</span>
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </div>
    </aside>

    <section class="space-y-3">
      {#if selected}
        <div class="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900">
          <header class="flex flex-wrap items-baseline justify-between gap-3">
            <div>
              <h2 class="text-lg font-semibold">{selected.name}</h2>
              <p class="text-xs text-gray-500">{selected.rid}</p>
            </div>
            <button
              type="button"
              class="rounded bg-purple-600 px-3 py-1 text-sm font-medium text-white hover:bg-purple-700 disabled:opacity-50"
              onclick={promote}
              disabled={promoting}
              data-testid="global-branching-promote-button"
            >Promote</button>
          </header>
          {#if promoteResult}
            <p class="mt-1 text-xs text-emerald-700 dark:text-emerald-300" data-testid="global-branching-promote-result">{promoteResult}</p>
          {/if}
          <dl class="mt-2 grid grid-cols-3 gap-2 text-xs">
            <div>
              <dt class="text-gray-500">Links</dt>
              <dd>{selected.link_count}</dd>
            </div>
            <div>
              <dt class="text-gray-500">Drifted</dt>
              <dd>{selected.drifted_count}</dd>
            </div>
            <div>
              <dt class="text-gray-500">Archived</dt>
              <dd>{selected.archived_count}</dd>
            </div>
          </dl>
        </div>

        <form
          class="grid gap-2 rounded-2xl border border-slate-200 bg-white p-3 shadow-sm dark:border-gray-700 dark:bg-gray-900 md:grid-cols-[1fr,2fr,2fr,auto]"
          onsubmit={submitLink}
          data-testid="global-branching-link-form"
        >
          <select
            bind:value={linkResourceType}
            class="rounded border border-slate-300 px-2 py-1 text-sm dark:border-gray-700 dark:bg-gray-800"
            data-testid="global-branching-link-type"
          >
            <option value="dataset">dataset</option>
            <option value="pipeline">pipeline</option>
            <option value="ontology">ontology</option>
            <option value="code_repo">code_repo</option>
          </select>
          <input
            class="rounded border border-slate-300 px-2 py-1 text-sm dark:border-gray-700 dark:bg-gray-800"
            placeholder="Resource RID"
            bind:value={linkResourceRid}
            data-testid="global-branching-link-resource-rid"
            required
          />
          <input
            class="rounded border border-slate-300 px-2 py-1 text-sm dark:border-gray-700 dark:bg-gray-800"
            placeholder="Branch RID (ri.foundry.main.branch.…)"
            bind:value={linkBranchRid}
            data-testid="global-branching-link-branch-rid"
            required
          />
          <button
            type="submit"
            class="rounded bg-blue-600 px-3 py-1 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
            disabled={linking}
            data-testid="global-branching-link-submit"
          >Link</button>
        </form>

        <div class="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm dark:border-gray-700 dark:bg-gray-900">
          <table class="w-full text-xs" data-testid="global-branching-resources-table">
            <thead class="border-b border-slate-200 bg-slate-50 text-left text-[11px] uppercase tracking-wide text-gray-500 dark:border-gray-700 dark:bg-gray-800/50">
              <tr>
                <th class="px-3 py-2">Resource type</th>
                <th class="px-3 py-2">Resource RID</th>
                <th class="px-3 py-2">Branch RID</th>
                <th class="px-3 py-2">Status</th>
                <th class="px-3 py-2">Last synced</th>
              </tr>
            </thead>
            <tbody>
              {#each resources as link}
                <tr class="border-b border-slate-100 dark:border-gray-800">
                  <td class="px-3 py-2 font-mono text-[11px]">{link.resource_type}</td>
                  <td class="px-3 py-2 font-mono text-[11px]">{link.resource_rid}</td>
                  <td class="px-3 py-2 font-mono text-[11px]">{link.branch_rid}</td>
                  <td class="px-3 py-2">
                    <span class={`rounded-full px-2 py-0.5 text-[10px] font-medium ${statusTone(link.status)}`}>
                      {link.status}
                    </span>
                  </td>
                  <td class="px-3 py-2 text-gray-500">{link.last_synced_at.slice(0, 16)}</td>
                </tr>
              {:else}
                <tr><td colspan="5" class="px-3 py-3 text-center text-gray-500">No resources linked yet.</td></tr>
              {/each}
            </tbody>
          </table>
        </div>
      {:else if !loading}
        <p class="text-sm text-gray-500">Select a global branch to inspect.</p>
      {/if}
    </section>
  </div>
</div>
