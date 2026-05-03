<!--
  P4 — RetentionPoliciesTab.

  Foundry's "View retention policies for a dataset [Beta]" surface
  (Datasets.md § "Retention"). Five sections, top to bottom:

    1. Beta banner.
    2. Inherited policies (Org → Space → Project), grouped tables.
    3. Explicit policies on this dataset (manage role: inline CRUD).
    4. Effective policy (winner-take, with the "why" line).
    5. Preview deletions (slider, hits the retention-preview endpoint).

  Self-fetches via the new `getApplicablePolicies` /
  `getRetentionPreview` helpers in `$lib/api/datasets`.
-->
<script lang="ts" module>
  import type { RetentionPolicy } from '$lib/api/datasets';
  export type Mode = 'view' | 'manage';
  export type { RetentionPolicy };
</script>

<script lang="ts">
  import {
    createRetentionPolicy,
    deleteRetentionPolicy,
    getApplicablePolicies,
    getRetentionPreview,
    type ApplicablePoliciesResponse,
    type RetentionPreviewResponse,
  } from '$lib/api/datasets';

  type Props = {
    datasetRid: string;
    /** Pass dataset metadata so the resolver can match
     *  inherited policies. The page already has these. */
    projectId?: string;
    spaceId?: string;
    orgId?: string;
    /** True when the current user has manage permission. Drives
     *  CRUD visibility on the Explicit section. */
    canManage?: boolean;
  };

  const {
    datasetRid,
    projectId,
    spaceId,
    orgId,
    canManage = false,
  }: Props = $props();

  let applicable = $state<ApplicablePoliciesResponse | null>(null);
  let preview = $state<RetentionPreviewResponse | null>(null);
  let asOfDays = $state(0);
  let loadingApplicable = $state(false);
  let loadingPreview = $state(false);
  let error = $state<string | null>(null);
  let saving = $state(false);

  let lastApplicableKey = $state('');
  let lastPreviewKey = $state('');

  $effect(() => {
    const key = JSON.stringify({ datasetRid, projectId, spaceId, orgId });
    if (!datasetRid || key === lastApplicableKey) return;
    lastApplicableKey = key;
    void loadApplicable();
  });

  $effect(() => {
    const key = JSON.stringify({ datasetRid, asOfDays });
    if (!datasetRid || key === lastPreviewKey) return;
    lastPreviewKey = key;
    void loadPreview();
  });

  async function loadApplicable() {
    loadingApplicable = true;
    error = null;
    try {
      applicable = await getApplicablePolicies(datasetRid, {
        project_id: projectId,
        space_id: spaceId,
        org_id: orgId,
      });
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Applicable-policies failed.';
    } finally {
      loadingApplicable = false;
    }
  }

  async function loadPreview() {
    loadingPreview = true;
    try {
      preview = await getRetentionPreview(datasetRid, asOfDays, {
        project_id: projectId,
        space_id: spaceId,
        org_id: orgId,
      });
    } catch (cause) {
      // Preview failures are non-fatal — the rest of the tab still works.
      preview = null;
      console.warn('retention-preview failed', cause);
    } finally {
      loadingPreview = false;
    }
  }

  // Inline create form (manage role only).
  let newName = $state('');
  let newDays = $state<number>(30);

  async function createExplicitPolicy() {
    if (!newName.trim()) return;
    saving = true;
    try {
      await createRetentionPolicy({
        name: newName.trim(),
        target_kind: 'transaction',
        retention_days: newDays,
        purge_mode: 'hard-delete-after-ttl',
        selector: { dataset_rid: datasetRid },
        updated_by: 'web-ui',
      });
      newName = '';
      newDays = 30;
      lastApplicableKey = '';
      await loadApplicable();
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Could not create policy.';
    } finally {
      saving = false;
    }
  }

  async function removeExplicit(id: string) {
    if (!confirm('Delete this retention policy?')) return;
    try {
      await deleteRetentionPolicy(id);
      lastApplicableKey = '';
      await loadApplicable();
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Could not delete policy.';
    }
  }

  function fmtBytes(value: number): string {
    if (value < 1024) return `${value} B`;
    if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
    if (value < 1024 * 1024 * 1024) return `${(value / (1024 * 1024)).toFixed(1)} MB`;
    return `${(value / (1024 * 1024 * 1024)).toFixed(2)} GB`;
  }

  function describePolicy(policy: RetentionPolicy): string {
    const parts: string[] = [];
    parts.push(`${policy.target_kind}, ${policy.retention_days}d`);
    if (policy.legal_hold) parts.push('legal hold');
    if (policy.criteria.transaction_state)
      parts.push(`state=${policy.criteria.transaction_state}`);
    parts.push(`grace ${policy.grace_period_minutes}m`);
    return parts.join(' · ');
  }

  function inheritedTotal(a: ApplicablePoliciesResponse | null): number {
    if (!a) return 0;
    return a.inherited.org.length + a.inherited.space.length + a.inherited.project.length;
  }

  function effectiveExplanation(a: ApplicablePoliciesResponse | null): string {
    if (!a || !a.effective) return '';
    if (a.conflicts.length === 0) {
      return `Sole applicable policy.`;
    }
    const reasons = new Set(a.conflicts.map((c) => c.reason));
    if (reasons.has('winner_has_legal_hold')) {
      return `Wins because legal_hold = true overrides every other policy.`;
    }
    if (reasons.has('winner_has_lower_retention_days')) {
      return `Wins as the most restrictive policy: lowest retention_days (${a.effective.retention_days}d).`;
    }
    return `Wins by specificity (explicit > project > space > org).`;
  }
</script>

<section class="space-y-4" data-component="retention-policies-tab">
  <!-- 1) Beta banner -->
  <div class="flex flex-wrap items-center gap-2 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:border-amber-900/50 dark:bg-amber-950/40 dark:text-amber-100" data-testid="retention-beta-banner">
    <span class="rounded-full bg-amber-200 px-2 py-0.5 text-xs font-semibold uppercase tracking-wide text-amber-900 dark:bg-amber-900 dark:text-amber-100">Beta</span>
    <span title="Per Foundry docs, this view is in Beta phase">
      Per Foundry docs, this view is in Beta phase.
    </span>
  </div>

  {#if error}
    <div class="rounded-lg border border-rose-200 bg-rose-50 px-4 py-2 text-sm text-rose-800 dark:border-rose-900/50 dark:bg-rose-950/40 dark:text-rose-100" role="alert" data-testid="retention-error">
      {error}
    </div>
  {/if}

  <!-- 4) Effective policy summary up top: it's what users want first. -->
  <section class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950" data-testid="retention-effective">
    <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Effective policy</div>
    {#if loadingApplicable}
      <div class="mt-2 text-sm text-slate-500">Resolving applicable policies…</div>
    {:else if applicable?.effective}
      <div class="mt-2 flex flex-col gap-1">
        <div class="flex flex-wrap items-center gap-2">
          <span class="text-base font-semibold" data-testid="effective-policy-name">{applicable.effective.name}</span>
          {#if applicable.effective.is_system}
            <span class="rounded-full bg-indigo-100 px-2 py-0.5 text-xs font-medium text-indigo-800 dark:bg-indigo-900/40 dark:text-indigo-200">System</span>
          {/if}
          {#if applicable.effective.legal_hold}
            <span class="rounded-full bg-rose-100 px-2 py-0.5 text-xs font-medium text-rose-800 dark:bg-rose-900/40 dark:text-rose-200">Legal hold</span>
          {/if}
        </div>
        <div class="text-xs text-slate-500">{describePolicy(applicable.effective)}</div>
        <div class="text-xs italic text-slate-500">{effectiveExplanation(applicable)}</div>
      </div>
    {:else}
      <div class="mt-2 text-sm text-slate-500" data-testid="retention-empty">
        No retention policies apply to this dataset yet.
      </div>
    {/if}
  </section>

  <!-- 2) Inherited policies, one section per level. -->
  <section class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950" data-testid="retention-inherited">
    <div class="flex items-center justify-between">
      <div>
        <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Inherited policies</div>
        <p class="mt-1 text-sm text-slate-500">
          Policies that apply via the org / space / project hierarchy.
          {inheritedTotal(applicable)} inherited policy{inheritedTotal(applicable) === 1 ? '' : 's'}.
        </p>
      </div>
    </div>

    {#if applicable && inheritedTotal(applicable) > 0}
      <div class="mt-3 space-y-3">
        {@render InheritedGroup('Org', applicable.inherited.org, 'org')}
        {@render InheritedGroup('Space', applicable.inherited.space, 'space')}
        {@render InheritedGroup('Project', applicable.inherited.project, 'project')}
      </div>
    {:else if !loadingApplicable}
      <div class="mt-3 text-sm text-slate-500">No inherited policies.</div>
    {/if}
  </section>

  <!-- 3) Explicit policies on this dataset. -->
  <section class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950" data-testid="retention-explicit">
    <div class="flex items-center justify-between">
      <div>
        <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Explicit policies on this dataset</div>
        <p class="mt-1 text-sm text-slate-500">
          {applicable?.explicit.length ?? 0} explicit polic{(applicable?.explicit.length ?? 0) === 1 ? 'y' : 'ies'}.
        </p>
      </div>
    </div>

    {#if applicable?.explicit && applicable.explicit.length > 0}
      <ul class="mt-3 divide-y divide-slate-100 dark:divide-gray-800">
        {#each applicable.explicit as policy (policy.id)}
          <li class="flex flex-col gap-1 py-2 text-sm md:flex-row md:items-center md:justify-between" data-testid="explicit-row">
            <div class="flex flex-col">
              <span class="font-medium">{policy.name}</span>
              <span class="text-xs text-slate-500">{describePolicy(policy)}</span>
            </div>
            {#if canManage}
              <button
                type="button"
                class="self-start rounded-md border border-rose-300 px-2 py-1 text-xs text-rose-700 hover:bg-rose-50 dark:border-rose-900/50 dark:text-rose-200 dark:hover:bg-rose-950/40"
                onclick={() => void removeExplicit(policy.id)}
              >
                Remove
              </button>
            {/if}
          </li>
        {/each}
      </ul>
    {:else}
      <div class="mt-3 text-sm text-slate-500">No explicit policies on this dataset.</div>
    {/if}

    {#if canManage}
      <form
        class="mt-4 flex flex-wrap items-end gap-2 rounded-md bg-slate-50 p-3 text-sm dark:bg-gray-900"
        onsubmit={(event) => {
          event.preventDefault();
          void createExplicitPolicy();
        }}
        data-testid="explicit-create-form"
      >
        <label class="flex flex-col">
          <span class="text-xs uppercase tracking-wide text-slate-400">Name</span>
          <input
            class="mt-1 rounded-md border border-slate-300 bg-white px-2 py-1 dark:border-gray-700 dark:bg-gray-900"
            placeholder="e.g. operational-30d"
            bind:value={newName}
          />
        </label>
        <label class="flex flex-col">
          <span class="text-xs uppercase tracking-wide text-slate-400">Retention days</span>
          <input
            type="number"
            min="0"
            class="mt-1 w-28 rounded-md border border-slate-300 bg-white px-2 py-1 dark:border-gray-700 dark:bg-gray-900"
            bind:value={newDays}
          />
        </label>
        <button
          type="submit"
          class="rounded-md bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
          disabled={saving || !newName.trim()}
        >
          {saving ? 'Saving…' : 'Add policy'}
        </button>
      </form>
    {/if}
  </section>

  <!-- 5) Preview deletions. -->
  <section class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950" data-testid="retention-preview">
    <div class="flex flex-wrap items-end justify-between gap-3">
      <div>
        <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Preview deletions</div>
        <p class="mt-1 text-sm text-slate-500">
          Simulate which transactions and files would be purged
          {#if asOfDays === 0}today{:else}{asOfDays} day{asOfDays === 1 ? '' : 's'} from now{/if}.
        </p>
      </div>
      <label class="flex w-full max-w-sm items-center gap-2 text-sm">
        <span class="text-xs uppercase tracking-wide text-slate-400">As of</span>
        <input
          type="range"
          min="0"
          max="365"
          step="1"
          bind:value={asOfDays}
          class="flex-1"
          data-testid="retention-preview-slider"
        />
        <span class="w-16 text-right font-mono text-xs">{asOfDays}d</span>
      </label>
    </div>

    {#if loadingPreview}
      <div class="mt-3 text-sm text-slate-500">Computing preview…</div>
    {:else if preview}
      <div class="mt-3 grid grid-cols-1 gap-3 md:grid-cols-3">
        <div class="rounded-md bg-slate-50 px-3 py-2 dark:bg-gray-900">
          <div class="text-xs uppercase tracking-wide text-slate-400">Transactions</div>
          <div class="mt-1 text-lg font-semibold" data-testid="preview-tx-count">
            {preview.summary.transactions_would_delete} / {preview.summary.transactions_total}
          </div>
        </div>
        <div class="rounded-md bg-slate-50 px-3 py-2 dark:bg-gray-900">
          <div class="text-xs uppercase tracking-wide text-slate-400">Files</div>
          <div class="mt-1 text-lg font-semibold">{preview.summary.files_total}</div>
        </div>
        <div class="rounded-md bg-slate-50 px-3 py-2 dark:bg-gray-900">
          <div class="text-xs uppercase tracking-wide text-slate-400">Total size</div>
          <div class="mt-1 text-lg font-semibold">{fmtBytes(preview.summary.bytes_total)}</div>
        </div>
      </div>

      {#if preview.warnings.length > 0}
        <div class="mt-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/40 dark:text-amber-100">
          {preview.warnings.join('; ')}
        </div>
      {/if}

      {#if preview.transactions.some((t) => t.would_delete)}
        <div class="mt-3 max-h-72 overflow-y-auto rounded-md border border-slate-200 dark:border-gray-800">
          <table class="min-w-full text-xs">
            <thead class="text-left uppercase tracking-wide text-slate-500">
              <tr>
                <th class="px-2 py-1">Transaction</th>
                <th class="px-2 py-1">Status</th>
                <th class="px-2 py-1">Policy</th>
                <th class="px-2 py-1">Reason</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-slate-100 font-mono dark:divide-gray-800">
              {#each preview.transactions.filter((t) => t.would_delete) as txn (txn.id)}
                <tr data-testid="preview-row">
                  <td class="px-2 py-1" title={txn.id}>{txn.id.slice(0, 8)}</td>
                  <td class="px-2 py-1">{txn.status}</td>
                  <td class="px-2 py-1">{txn.policy_name ?? '—'}</td>
                  <td class="px-2 py-1 text-slate-500">{txn.reason ?? '—'}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    {/if}
  </section>
</section>

{#snippet InheritedGroup(label: string, items: RetentionPolicy[], slug: string)}
  {#if items.length > 0}
    <div data-testid={`inherited-${slug}`}>
      <div class="text-xs font-semibold uppercase tracking-wide text-slate-500">{label}</div>
      <ul class="mt-1 divide-y divide-slate-100 dark:divide-gray-800">
        {#each items as policy (policy.id)}
          <li class="flex flex-col gap-1 py-1 text-sm md:flex-row md:items-center md:justify-between">
            <span class="flex items-center gap-2">
              <span>{policy.name}</span>
              <span class="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] uppercase tracking-wide text-slate-700 dark:bg-gray-800 dark:text-gray-200" title={`Inherited from ${label}`}>
                inherited from {label.toLowerCase()}
              </span>
            </span>
            <span class="text-xs text-slate-500">{describePolicy(policy)}</span>
          </li>
        {/each}
      </ul>
    </div>
  {/if}
{/snippet}
