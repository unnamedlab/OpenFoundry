<!--
  P4 — single-branch detail page.

  Two tabs:
    * **Retention** — policy editor (INHERITED / FOREVER / TTL_DAYS),
      live `archived_at` status, and a "Restore" button when the
      branch is in the soft-archived state.
    * **Security** — markings projection (effective / explicit /
      inherited from parent) per Foundry "Branch security".
-->
<script lang="ts">
  import { page } from '$app/stores';
  import { onMount } from 'svelte';
  import {
    getBranchMarkings,
    listBranches,
    restoreBranch,
    updateBranchRetention,
    type BranchMarkingsView,
    type DatasetBranch,
  } from '$lib/api/datasets';
  import { ApiError } from '$lib/api/client';

  type Tab = 'retention' | 'security';

  const datasetId = $derived(($page.params.id ?? '') as string);
  const branchName = $derived(($page.params.branch ?? '') as string);

  let branches = $state<DatasetBranch[]>([]);
  let branch = $state<DatasetBranch | null>(null);
  let markings = $state<BranchMarkingsView | null>(null);
  let activeTab = $state<Tab>('retention');
  let policy = $state<'INHERITED' | 'FOREVER' | 'TTL_DAYS'>('INHERITED');
  let ttlDays = $state<number | null>(null);
  let savingRetention = $state(false);
  let restoring = $state(false);
  let error = $state('');

  async function load() {
    error = '';
    try {
      branches = await listBranches(datasetId);
      branch = branches.find((b) => b.name === branchName) ?? null;
      // Default the policy form from the loaded row when present.
      // Legacy payloads omit the field, so we fall back to
      // `INHERITED` (the documented Foundry default).
      policy = branch?.retention_policy ?? 'INHERITED';
      ttlDays = branch?.retention_ttl_days ?? null;
      try {
        markings = await getBranchMarkings(datasetId, branchName);
      } catch {
        // Tolerant: 404 here just means no snapshot rows yet.
        markings = { effective: [], explicit: [], inherited_from_parent: [] };
      }
    } catch (cause) {
      error = cause instanceof ApiError ? cause.message : 'Failed to load branch';
    }
  }

  async function saveRetention() {
    savingRetention = true;
    error = '';
    try {
      await updateBranchRetention(datasetId, branchName, {
        policy,
        ttl_days: policy === 'TTL_DAYS' ? ttlDays : null,
      });
      await load();
    } catch (cause) {
      error = cause instanceof ApiError ? cause.message : 'Failed to update retention';
    } finally {
      savingRetention = false;
    }
  }

  async function restore() {
    restoring = true;
    error = '';
    try {
      await restoreBranch(datasetId, branchName);
      await load();
    } catch (cause) {
      error = cause instanceof ApiError ? cause.message : 'Restore failed';
    } finally {
      restoring = false;
    }
  }

  onMount(() => {
    void load();
  });
</script>

<svelte:head>
  <title>Branch · {branchName}</title>
</svelte:head>

<div class="mx-auto max-w-5xl space-y-4 px-4 py-6" data-testid="branch-detail">
  <header class="flex flex-wrap items-baseline justify-between gap-3">
    <div>
      <p class="text-xs uppercase tracking-[0.22em] text-gray-400">Branch</p>
      <h1 class="mt-1 text-2xl font-bold">{branchName}</h1>
    </div>
    <a
      href={`/datasets/${encodeURIComponent(datasetId)}/branches`}
      class="text-sm text-blue-600 hover:underline"
      data-testid="branch-detail-back"
    >← Back to branches</a>
  </header>

  {#if error}
    <div class="rounded-xl border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-800 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300">
      {error}
    </div>
  {/if}

  <nav class="flex gap-2 border-b border-slate-200 dark:border-gray-800" aria-label="Branch detail tabs">
    {#each [
      { key: 'retention' as const, label: 'Retention' },
      { key: 'security' as const, label: 'Security' },
    ] as tab}
      <button
        type="button"
        class={`border-b-2 px-3 py-2 text-sm font-medium ${
          activeTab === tab.key
            ? 'border-blue-600 text-blue-700 dark:text-blue-300'
            : 'border-transparent text-slate-600 hover:border-slate-300 dark:text-gray-400'
        }`}
        onclick={() => (activeTab = tab.key)}
        data-testid={`branch-detail-tab-${tab.key}`}
      >{tab.label}</button>
    {/each}
  </nav>

  {#if activeTab === 'retention'}
    <section
      class="space-y-3 rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-900"
      data-testid="branch-retention-section"
    >
      <header>
        <h2 class="text-lg font-semibold">Retention</h2>
        <p class="text-xs text-gray-500">
          Foundry "Branch retention": pick one policy. <code>INHERITED</code>
          walks up the parent chain, <code>FOREVER</code> opts out
          permanently, <code>TTL_DAYS</code> archives the branch when
          inactivity exceeds the configured window.
        </p>
      </header>

      {#if branch?.archived_at}
        <div
          class="rounded-lg border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:border-amber-700 dark:bg-amber-950/30 dark:text-amber-100"
          data-testid="branch-archived-banner"
        >
          <p>
            Branch archived at <code>{branch.archived_at}</code>. Restore
            within the grace window to bring it back.
          </p>
          <button
            type="button"
            class="mt-2 rounded bg-emerald-600 px-2 py-1 text-xs font-medium text-white hover:bg-emerald-700 disabled:opacity-50"
            onclick={restore}
            disabled={restoring}
            data-testid="branch-restore-button"
          >Restore branch</button>
        </div>
      {/if}

      <fieldset class="grid gap-2 text-sm">
        <legend class="text-xs uppercase tracking-wide text-gray-500">Policy</legend>
        {#each [
          { value: 'INHERITED' as const, label: 'INHERITED', hint: 'Walk up parent_branch chain.' },
          { value: 'FOREVER' as const, label: 'FOREVER', hint: 'Never archived.' },
          { value: 'TTL_DAYS' as const, label: 'TTL_DAYS', hint: 'Archive after N days of inactivity.' },
        ] as opt}
          <label class="flex gap-2 rounded border border-slate-200 px-2 py-1 dark:border-gray-700">
            <input
              type="radio"
              bind:group={policy}
              value={opt.value}
              data-testid={`branch-retention-policy-${opt.value}`}
            />
            <span>
              <span class="font-medium">{opt.label}</span>
              <span class="block text-xs text-gray-500">{opt.hint}</span>
            </span>
          </label>
        {/each}
      </fieldset>

      {#if policy === 'TTL_DAYS'}
        <label class="block text-sm">
          <span class="text-gray-500">TTL (days)</span>
          <input
            type="number"
            min="1"
            class="mt-1 w-32 rounded border border-slate-300 px-2 py-1 dark:border-gray-700 dark:bg-gray-800"
            bind:value={ttlDays}
            data-testid="branch-retention-ttl-input"
          />
        </label>
      {/if}

      <div class="flex justify-end">
        <button
          type="button"
          class="rounded bg-blue-600 px-3 py-1 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
          onclick={saveRetention}
          disabled={savingRetention}
          data-testid="branch-retention-save"
        >Save retention</button>
      </div>
    </section>
  {:else}
    <section
      class="space-y-3 rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-900"
      data-testid="branch-security-section"
    >
      <header>
        <h2 class="text-lg font-semibold">Security</h2>
        <p class="text-xs text-gray-500">
          Foundry "Branch security" — markings inherited from the parent
          at creation time form the security floor; markings added here
          stack on top.
        </p>
      </header>

      {#if !markings || markings.effective.length === 0}
        <p class="text-sm text-gray-500">No markings on this branch.</p>
      {:else}
        <div class="grid gap-3 md:grid-cols-3">
          {#each [
            { key: 'effective' as const, label: 'Effective', tone: 'bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-200' },
            { key: 'explicit' as const, label: 'Explicit on this branch', tone: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-200' },
            { key: 'inherited_from_parent' as const, label: 'Inherited from parent', tone: 'bg-slate-100 text-slate-700 dark:bg-gray-800 dark:text-gray-200' },
          ] as group}
            {@const ids = markings![group.key]}
            <article
              class="rounded-xl border border-slate-200 p-3 dark:border-gray-700"
              data-testid={`branch-markings-${group.key}`}
            >
              <h3 class="text-xs uppercase tracking-wide text-gray-500">{group.label}</h3>
              {#if ids.length === 0}
                <p class="mt-1 text-xs text-gray-500">—</p>
              {:else}
                <ul class="mt-1 flex flex-wrap gap-1">
                  {#each ids as marking}
                    <li class={`rounded-full px-2 py-0.5 text-[11px] ${group.tone}`}>
                      <code>{marking}</code>
                    </li>
                  {/each}
                </ul>
              {/if}
            </article>
          {/each}
        </div>
      {/if}
    </section>
  {/if}
</div>
