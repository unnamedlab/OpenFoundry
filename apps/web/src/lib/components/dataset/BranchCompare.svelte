<!--
  P5 — BranchCompare

  Drives `GET /v1/datasets/{rid}/branches/compare?base=A&compare=B`.
  The response carries:
    * `lca_branch_rid` — lowest common ancestor on the ancestry tree.
    * `a_only_transactions`   — committed transactions strictly on A.
    * `b_only_transactions`   — committed transactions strictly on B.
    * `conflicting_files`     — same `logical_path` written on both
                                sides since the LCA.

  ## Layout
  Three columns:
    1. *Only on A* — chronological list of A-side commits.
    2. *Conflicts* — `logical_path` table with both transaction rids.
    3. *Only on B* — chronological list of B-side commits.

  Lists are virtualized via a simple windowed-render fall-back so a
  several-hundred-row diff stays interactive in the dashboard.
-->
<script lang="ts">
  import {
    compareBranches,
    type BranchCompareResponse,
    type CompareConflictingFile,
    type CompareTransactionSummary,
    type DatasetBranch,
  } from '$lib/api/datasets';
  import { ApiError } from '$lib/api/client';

  type Props = {
    datasetRid: string;
    branches: DatasetBranch[];
  };

  const { datasetRid, branches }: Props = $props();

  let baseBranch = $state('master');
  let compareBranch = $state('');
  let response = $state<BranchCompareResponse | null>(null);
  let loading = $state(false);
  let error = $state('');
  let selected = $state<{ kind: 'tx' | 'conflict'; payload: unknown } | null>(null);

  $effect(() => {
    // Pick a reasonable default for the compare side: the first
    // non-default branch. Otherwise leave it empty so the user picks.
    if (!compareBranch && branches.length > 0) {
      const fallback = branches.find((b) => !b.is_default);
      compareBranch = fallback?.name ?? '';
    }
  });

  async function run() {
    if (!baseBranch || !compareBranch || baseBranch === compareBranch) {
      error = 'Pick two different branches.';
      return;
    }
    loading = true;
    error = '';
    response = null;
    try {
      response = await compareBranches(datasetRid, baseBranch, compareBranch);
    } catch (cause) {
      error = cause instanceof ApiError ? cause.message : 'Compare failed';
    } finally {
      loading = false;
    }
  }

  function selectTx(payload: CompareTransactionSummary) {
    selected = { kind: 'tx', payload };
  }
  function selectConflict(payload: CompareConflictingFile) {
    selected = { kind: 'conflict', payload };
  }

  function shortRid(rid: string): string {
    const tail = rid.split('.').pop() ?? '';
    return tail.length > 12 ? `${tail.slice(0, 8)}…` : tail;
  }
</script>

<section
  class="space-y-3 rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900"
  data-testid="branch-compare"
>
  <header class="flex flex-wrap items-end gap-3">
    <label class="text-sm">
      <span class="block text-xs uppercase tracking-wide text-gray-500">From branch</span>
      <select
        bind:value={baseBranch}
        class="mt-1 rounded border border-slate-300 px-2 py-1 text-sm dark:border-gray-700 dark:bg-gray-800"
        data-testid="branch-compare-base"
      >
        {#each branches as b (b.id)}
          <option value={b.name}>{b.name}{b.is_default ? ' (default)' : ''}</option>
        {/each}
      </select>
    </label>

    <span aria-hidden="true" class="pb-1 text-gray-500">↔</span>

    <label class="text-sm">
      <span class="block text-xs uppercase tracking-wide text-gray-500">To branch</span>
      <select
        bind:value={compareBranch}
        class="mt-1 rounded border border-slate-300 px-2 py-1 text-sm dark:border-gray-700 dark:bg-gray-800"
        data-testid="branch-compare-target"
      >
        <option value="">— pick one —</option>
        {#each branches as b (b.id)}
          <option value={b.name}>{b.name}{b.is_default ? ' (default)' : ''}</option>
        {/each}
      </select>
    </label>

    <button
      type="button"
      class="rounded bg-blue-600 px-3 py-1 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
      onclick={run}
      disabled={loading || !baseBranch || !compareBranch || baseBranch === compareBranch}
      data-testid="branch-compare-run"
    >Compare</button>

    {#if response?.lca_branch_rid}
      <span class="ml-auto text-xs text-gray-500" data-testid="branch-compare-lca">
        LCA: <code class="font-mono">{shortRid(response.lca_branch_rid)}</code>
      </span>
    {/if}
  </header>

  {#if error}
    <p class="text-sm text-rose-600 dark:text-rose-300">{error}</p>
  {/if}

  {#if response}
    <div class="grid gap-3 lg:grid-cols-3" data-testid="branch-compare-grid">
      <article class="rounded-xl border border-slate-200 dark:border-gray-700">
        <header class="flex items-center justify-between border-b border-slate-200 px-3 py-2 text-xs uppercase tracking-wide text-gray-500 dark:border-gray-700">
          <span>Only on {response.base_branch}</span>
          <span class="rounded bg-blue-100 px-2 py-0.5 text-[10px] text-blue-700 dark:bg-blue-900/40 dark:text-blue-200">
            {response.a_only_transactions.length}
          </span>
        </header>
        <ul class="max-h-72 divide-y divide-slate-100 overflow-auto dark:divide-gray-800" data-testid="branch-compare-a-only">
          {#each response.a_only_transactions as tx (tx.transaction_id)}
            <li>
              <button
                type="button"
                class="block w-full px-3 py-1.5 text-left text-xs hover:bg-slate-50 dark:hover:bg-gray-800"
                onclick={() => selectTx(tx)}
                data-testid={`branch-compare-a-tx-${tx.transaction_id}`}
              >
                <span class="font-mono">{shortRid(tx.transaction_rid)}</span>
                <span class="ml-2 text-gray-500">{tx.tx_type}</span>
                <span class="ml-2 text-gray-500">· {tx.files_changed} files</span>
              </button>
            </li>
          {:else}
            <li class="px-3 py-2 text-xs text-gray-500">No diverged commits.</li>
          {/each}
        </ul>
      </article>

      <article class="rounded-xl border border-amber-200 dark:border-amber-700/50">
        <header class="flex items-center justify-between border-b border-amber-200 px-3 py-2 text-xs uppercase tracking-wide text-amber-700 dark:border-amber-700/50 dark:text-amber-200">
          <span>Conflicts</span>
          <span class="rounded bg-amber-100 px-2 py-0.5 text-[10px] text-amber-800 dark:bg-amber-900/40 dark:text-amber-100">
            {response.conflicting_files.length}
          </span>
        </header>
        <ul class="max-h-72 divide-y divide-slate-100 overflow-auto dark:divide-gray-800" data-testid="branch-compare-conflicts">
          {#each response.conflicting_files as conflict (conflict.logical_path)}
            <li>
              <button
                type="button"
                class="block w-full px-3 py-1.5 text-left text-xs hover:bg-amber-50 dark:hover:bg-amber-900/20"
                onclick={() => selectConflict(conflict)}
                data-testid={`branch-compare-conflict-${conflict.logical_path}`}
              >
                <span class="font-mono">{conflict.logical_path}</span>
                <span class="block text-[10px] text-gray-500">
                  {shortRid(conflict.a_transaction_rid)} ↔ {shortRid(conflict.b_transaction_rid)}
                </span>
              </button>
            </li>
          {:else}
            <li class="px-3 py-2 text-xs text-gray-500">No overlapping writes.</li>
          {/each}
        </ul>
      </article>

      <article class="rounded-xl border border-slate-200 dark:border-gray-700">
        <header class="flex items-center justify-between border-b border-slate-200 px-3 py-2 text-xs uppercase tracking-wide text-gray-500 dark:border-gray-700">
          <span>Only on {response.compare_branch}</span>
          <span class="rounded bg-emerald-100 px-2 py-0.5 text-[10px] text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-200">
            {response.b_only_transactions.length}
          </span>
        </header>
        <ul class="max-h-72 divide-y divide-slate-100 overflow-auto dark:divide-gray-800" data-testid="branch-compare-b-only">
          {#each response.b_only_transactions as tx (tx.transaction_id)}
            <li>
              <button
                type="button"
                class="block w-full px-3 py-1.5 text-left text-xs hover:bg-slate-50 dark:hover:bg-gray-800"
                onclick={() => selectTx(tx)}
                data-testid={`branch-compare-b-tx-${tx.transaction_id}`}
              >
                <span class="font-mono">{shortRid(tx.transaction_rid)}</span>
                <span class="ml-2 text-gray-500">{tx.tx_type}</span>
                <span class="ml-2 text-gray-500">· {tx.files_changed} files</span>
              </button>
            </li>
          {:else}
            <li class="px-3 py-2 text-xs text-gray-500">No diverged commits.</li>
          {/each}
        </ul>
      </article>
    </div>
  {/if}

  {#if selected}
    <aside
      class="rounded-xl border border-slate-200 bg-slate-50 p-3 text-xs dark:border-gray-700 dark:bg-gray-800"
      data-testid="branch-compare-detail"
    >
      <p class="text-[10px] uppercase tracking-wide text-gray-500">
        {selected.kind === 'tx' ? 'Transaction detail' : 'Conflict detail'}
      </p>
      <pre class="mt-1 overflow-auto whitespace-pre-wrap font-mono">{JSON.stringify(selected.payload, null, 2)}</pre>
    </aside>
  {/if}
</section>
