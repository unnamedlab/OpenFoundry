<!--
  P3 — Branches dashboard for a single dataset.

  Two-pane layout:
    * Main panel — `<BranchGraph>` with drag&drop reparent + tooltips.
    * Side table — every branch with quick-stats columns (parent,
      head_tx, last_activity, # tx, has_open_tx, # downstream
      pipelines, fallback_chain).

  Header buttons:
    * "+ Create branch" — opens `<CreateBranchDialog>`.
    * "Compare branches" — placeholder; the actual diff lives in P5.
-->
<script lang="ts">
  import { page } from '$app/stores';
  import { onMount } from 'svelte';
  import {
    getDataset,
    listBranches,
    listDatasetTransactions,
    type Dataset,
    type DatasetBranch,
    type DatasetTransaction,
  } from '$lib/api/datasets';
  import { ApiError } from '$lib/api/client';
  import BranchGraph from '$lib/components/dataset/BranchGraph.svelte';
  import BranchCompare from '$lib/components/dataset/BranchCompare.svelte';
  import BranchLifecycleTimeline from '$lib/components/dataset/BranchLifecycleTimeline.svelte';
  import CreateBranchDialog from '$lib/components/dataset/CreateBranchDialog.svelte';
  import DeleteBranchDialog from '$lib/components/dataset/DeleteBranchDialog.svelte';
  import ReparentDialog from '$lib/components/dataset/ReparentDialog.svelte';

  // SvelteKit types `params.id` as `string | undefined`; the route is
  // `/datasets/[id]/branches`, so the param is always present at runtime
  // — we widen-narrow with `as string` and let the lifecycle guard
  // surface the (impossible) missing case as an empty string.
  const datasetId = $derived(($page.params.id ?? '') as string);

  let dataset = $state<Dataset | null>(null);
  let branches = $state<DatasetBranch[]>([]);
  let transactions = $state<DatasetTransaction[]>([]);
  let loading = $state(true);
  let error = $state('');
  let selected = $state<DatasetBranch | null>(null);

  let createOpen = $state(false);
  let compareOpen = $state(false);
  let deleteTarget = $state<DatasetBranch | null>(null);
  let reparentRequest = $state<{
    source: DatasetBranch;
    candidateParent: DatasetBranch;
  } | null>(null);

  const extras = $derived.by(() => {
    const out: Record<string, { transactions: number; downstreamPipelines: number }> = {};
    for (const b of branches) {
      const txCount = transactions.filter((t) => t.branch_name === b.name).length;
      out[b.name] = { transactions: txCount, downstreamPipelines: 0 };
    }
    return out;
  });

  function parentName(b: DatasetBranch): string {
    if (!b.parent_branch_id) return '— (root)';
    const parent = branches.find((p) => p.id === b.parent_branch_id);
    return parent ? parent.name : (b.parent_branch_id as string).slice(0, 8) + '…';
  }

  async function load() {
    loading = true;
    error = '';
    try {
      dataset = await getDataset(datasetId);
      branches = await listBranches(datasetId);
      transactions = await listDatasetTransactions(datasetId).catch(() => []);
    } catch (cause) {
      error = cause instanceof ApiError ? cause.message : 'Failed to load branches';
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    void load();
  });
</script>

<svelte:head>
  <title>Branches · {dataset?.name ?? ''}</title>
</svelte:head>

<div class="mx-auto max-w-7xl space-y-4 px-4 py-6" data-testid="branches-dashboard">
  <header class="flex flex-wrap items-center justify-between gap-3">
    <div>
      <p class="text-xs uppercase tracking-[0.22em] text-gray-400">Dataset</p>
      <h1 class="mt-1 text-2xl font-bold">
        Branches
        {#if dataset}<span class="text-base font-normal text-gray-500"> · {dataset.name}</span>{/if}
      </h1>
    </div>
    <div class="flex flex-wrap gap-2">
      <button
        type="button"
        class="rounded-xl bg-blue-600 px-3 py-1.5 text-sm text-white hover:bg-blue-700"
        onclick={() => (createOpen = true)}
        data-testid="branches-create-button"
      >+ Create branch</button>
      <button
        type="button"
        class="rounded-xl border border-slate-300 px-3 py-1.5 text-sm hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800"
        onclick={() => (compareOpen = !compareOpen)}
        data-testid="branches-compare-button"
      >{compareOpen ? 'Hide compare' : 'Compare branches'}</button>
    </div>
  </header>

  {#if error}
    <div class="rounded-xl border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-800 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300">
      {error}
    </div>
  {/if}

  {#if loading}
    <div class="text-center py-12 text-gray-500">Loading branches…</div>
  {:else}
    <div class="grid gap-4 lg:grid-cols-[3fr,2fr]">
      <BranchGraph
        datasetRid={datasetId}
        {branches}
        extras={extras}
        selectedBranch={selected?.name ?? null}
        onSelect={(b) => (selected = b)}
        onReparentRequest={(req) => (reparentRequest = req)}
      />

      <div class="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm dark:border-gray-700 dark:bg-gray-900">
        <table class="w-full text-xs" data-testid="branches-table">
          <thead class="border-b border-slate-200 bg-slate-50 text-left text-[11px] uppercase tracking-wide text-gray-500 dark:border-gray-700 dark:bg-gray-800/50">
            <tr>
              <th class="px-3 py-2">Name</th>
              <th class="px-3 py-2">Parent</th>
              <th class="px-3 py-2">Head tx</th>
              <th class="px-3 py-2">Last activity</th>
              <th class="px-3 py-2">#tx</th>
              <th class="px-3 py-2">Open?</th>
              <th class="px-3 py-2">Fallback</th>
              <th class="px-3 py-2"></th>
            </tr>
          </thead>
          <tbody>
            {#each branches as branch (branch.id)}
              {@const isSelected = selected?.id === branch.id}
              <tr
                class={`cursor-pointer border-b border-slate-100 hover:bg-slate-50 dark:border-gray-800 dark:hover:bg-gray-800/40 ${
                  isSelected ? 'bg-blue-50 dark:bg-blue-950/30' : ''
                }`}
                onclick={() => (selected = branch)}
                data-testid={`branch-row-${branch.name}`}
              >
                <td class="px-3 py-2 font-mono text-[11px]">{branch.name}{branch.is_default ? ' ★' : ''}</td>
                <td class="px-3 py-2">{parentName(branch)}</td>
                <td class="px-3 py-2 font-mono">
                  {(() => {
                    const id = branch.head_transaction_id;
                    if (!id) return '—';
                    const s = id as string;
                    return s.slice(0, 8) + '…';
                  })()}
                </td>
                <td class="px-3 py-2 text-gray-500">{branch.last_activity_at?.slice(0, 16) ?? '—'}</td>
                <td class="px-3 py-2">{extras[branch.name]?.transactions ?? 0}</td>
                <td class="px-3 py-2">
                  {#if branch.has_open_transaction}
                    <span class="rounded bg-amber-100 px-1.5 py-0.5 text-[10px] font-medium text-amber-800 dark:bg-amber-900/40 dark:text-amber-200">OPEN</span>
                  {:else}
                    <span class="text-gray-400">—</span>
                  {/if}
                </td>
                <td class="px-3 py-2 font-mono text-[10px]">{(branch.fallback_chain ?? []).join(' → ') || '—'}</td>
                <td class="px-3 py-2 text-right">
                  <button
                    type="button"
                    class="rounded px-1.5 py-0.5 text-rose-600 hover:bg-rose-50 disabled:opacity-50 dark:text-rose-300 dark:hover:bg-rose-950/30"
                    onclick={(e) => {
                      e.stopPropagation();
                      deleteTarget = branch;
                    }}
                    aria-label={`Delete ${branch.name}`}
                    data-testid={`branch-delete-${branch.name}`}
                  >×</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>

    <BranchLifecycleTimeline {branches} {transactions} />

    {#if compareOpen}
      <BranchCompare datasetRid={datasetId} {branches} />
    {/if}
  {/if}
</div>

<CreateBranchDialog
  datasetRid={datasetId}
  open={createOpen}
  branches={branches}
  onClose={() => (createOpen = false)}
  onCreated={() => void load()}
/>

<DeleteBranchDialog
  datasetRid={datasetId}
  branch={deleteTarget}
  open={Boolean(deleteTarget)}
  onClose={() => (deleteTarget = null)}
  onDeleted={() => void load()}
/>

<ReparentDialog
  datasetRid={datasetId}
  source={reparentRequest?.source ?? null}
  candidateParent={reparentRequest?.candidateParent ?? null}
  open={Boolean(reparentRequest)}
  onClose={() => (reparentRequest = null)}
  onConfirmed={() => void load()}
/>
