<!--
  T4.4 — RetentionPoliciesTab

  Reproduces the layout of Datasets_assets/img_001.png:

    * A header row with the title and a "Only show policies relevant
      to this branch" toggle (controls a boolean `relevantOnly` flag
      that the parent forwards to the policy lookup).
    * A scrollable list of policies, each rendered as a row with a
      globe icon (because every policy applies platform-wide unless
      overridden), the policy name, a small "System policy" /
      "Project policy" badge, and a click handler that opens the
      detail modal.
    * The modal surfaces policy metadata + "Last applied at" and
      "Next run".

  This is a *controlled* component: the parent owns the `policies`
  array and refetches when `relevantOnly` flips. We deliberately do
  not fetch from inside the tab so the dataset page can centralise
  loading and error handling.
-->
<script lang="ts">
  type RetentionSelector = {
    dataset_rid?: string;
    project_id?: string;
    marking_id?: string;
    all_datasets?: boolean;
  };

  type RetentionCriteria = {
    transaction_age_seconds?: number;
    transaction_state?: string;
    view_age_seconds?: number;
    last_accessed_seconds?: number;
  };

  export type RetentionPolicy = {
    id: string;
    name: string;
    is_system: boolean;
    target_kind: string;
    purge_mode: string;
    grace_period_minutes: number;
    selector: RetentionSelector;
    criteria: RetentionCriteria;
    last_applied_at?: string | null;
    next_run_at?: string | null;
    rules: string[];
  };

  type Props = {
    policies: RetentionPolicy[];
    /** Controlled — parent flips this and refetches. */
    relevantOnly: boolean;
    onToggleRelevantOnly: (next: boolean) => void;
    /** True while the parent is refetching the policy list. */
    loading?: boolean;
  };

  const { policies, relevantOnly, onToggleRelevantOnly, loading = false }: Props = $props();

  let openPolicyId = $state<string | null>(null);
  const openPolicy = $derived(
    openPolicyId ? policies.find((p) => p.id === openPolicyId) ?? null : null,
  );

  function badgeFor(policy: RetentionPolicy): { label: string; classes: string } {
    if (policy.is_system) {
      return {
        label: 'System policy',
        classes:
          'bg-indigo-100 text-indigo-800 ring-indigo-300 dark:bg-indigo-900/40 dark:text-indigo-200 dark:ring-indigo-700',
      };
    }
    if (policy.selector.project_id) {
      return {
        label: 'Project policy',
        classes:
          'bg-emerald-100 text-emerald-800 ring-emerald-300 dark:bg-emerald-900/40 dark:text-emerald-200 dark:ring-emerald-700',
      };
    }
    return {
      label: 'Custom policy',
      classes:
        'bg-slate-100 text-slate-700 ring-slate-300 dark:bg-slate-800 dark:text-slate-200 dark:ring-slate-600',
    };
  }

  function fmtDate(iso?: string | null): string {
    if (!iso) return '—';
    return new Date(iso).toLocaleString();
  }

  function describeCriteria(c: RetentionCriteria): string[] {
    const parts: string[] = [];
    if (c.transaction_state) parts.push(`transaction_state = ${c.transaction_state}`);
    if (c.transaction_age_seconds !== undefined)
      parts.push(`transaction_age ≥ ${c.transaction_age_seconds}s`);
    if (c.view_age_seconds !== undefined) parts.push(`view_age ≥ ${c.view_age_seconds}s`);
    if (c.last_accessed_seconds !== undefined)
      parts.push(`last_accessed ≥ ${c.last_accessed_seconds}s`);
    return parts.length ? parts : ['No structured criteria'];
  }
</script>

<section class="space-y-4">
  <header class="flex flex-col gap-3 rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-900 md:flex-row md:items-center md:justify-between">
    <div>
      <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Retention policies</div>
      <h2 class="mt-1 text-lg font-semibold">Retention</h2>
      <p class="mt-1 text-sm text-gray-500">
        Policies that govern how transactions, views and files are pruned from this dataset.
      </p>
    </div>

    <label class="inline-flex cursor-pointer items-center gap-2 text-sm">
      <input
        type="checkbox"
        class="h-4 w-4 rounded border-slate-300 text-blue-600 focus:ring-blue-500"
        checked={relevantOnly}
        onchange={(event) => onToggleRelevantOnly((event.target as HTMLInputElement).checked)}
      />
      <span>Only show policies relevant to this branch</span>
    </label>
  </header>

  {#if loading}
    <div class="rounded-xl border border-slate-200 bg-white px-4 py-6 text-center text-sm text-gray-500 dark:border-gray-700 dark:bg-gray-900">
      Loading retention policies…
    </div>
  {:else if policies.length === 0}
    <div class="rounded-xl border border-dashed border-slate-300 bg-white px-4 py-10 text-center text-sm text-gray-500 dark:border-gray-700 dark:bg-gray-900">
      No retention policies apply to this dataset yet.
    </div>
  {:else}
    <ul class="divide-y divide-slate-200 overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm dark:divide-gray-800 dark:border-gray-700 dark:bg-gray-900">
      {#each policies as policy (policy.id)}
        {@const badge = badgeFor(policy)}
        <li>
          <button
            type="button"
            class="flex w-full items-center justify-between gap-4 px-4 py-3 text-left transition-colors hover:bg-slate-50 focus:bg-slate-50 dark:hover:bg-gray-800 dark:focus:bg-gray-800"
            onclick={() => (openPolicyId = policy.id)}
          >
            <span class="flex items-center gap-3">
              <!-- Globe glyph: every policy targets the platform unless scoped down. -->
              <svg
                class="h-5 w-5 text-slate-500 dark:text-slate-300"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="1.6"
                aria-hidden="true"
              >
                <circle cx="12" cy="12" r="9" />
                <path d="M3 12h18M12 3a13.6 13.6 0 0 1 0 18M12 3a13.6 13.6 0 0 0 0 18" />
              </svg>
              <span class="flex flex-col">
                <span class="font-medium">{policy.name}</span>
                <span class="text-xs text-gray-500">
                  {policy.target_kind} · {policy.purge_mode}
                </span>
              </span>
            </span>
            <span class={`inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.16em] ring-1 ${badge.classes}`}>
              {badge.label}
            </span>
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</section>

{#if openPolicy}
  {@const detail = openPolicy}
  <div
    class="fixed inset-0 z-40 flex items-center justify-center bg-slate-900/40 px-4"
    role="dialog"
    aria-modal="true"
    aria-labelledby="retention-policy-title"
    onclick={() => (openPolicyId = null)}
    onkeydown={(event) => {
      if (event.key === 'Escape') openPolicyId = null;
    }}
    tabindex="-1"
  >
    <div
      class="w-full max-w-lg rounded-2xl border border-slate-200 bg-white p-6 shadow-xl dark:border-gray-700 dark:bg-gray-900"
      role="document"
      onclick={(event) => event.stopPropagation()}
      onkeydown={(event) => event.stopPropagation()}
      tabindex="-1"
    >
      <header class="mb-4 flex items-start justify-between gap-3">
        <div>
          <div class="text-xs uppercase tracking-[0.22em] text-gray-400">
            {badgeFor(detail).label}
          </div>
          <h3 id="retention-policy-title" class="mt-1 text-lg font-semibold">
            {detail.name}
          </h3>
        </div>
        <button
          type="button"
          class="rounded-md p-1 text-gray-500 hover:bg-slate-100 dark:hover:bg-gray-800"
          aria-label="Close"
          onclick={() => (openPolicyId = null)}
        >
          ✕
        </button>
      </header>

      <dl class="grid grid-cols-2 gap-3 text-sm">
        <div>
          <dt class="text-xs uppercase tracking-wide text-gray-400">Target kind</dt>
          <dd class="mt-0.5">{detail.target_kind}</dd>
        </div>
        <div>
          <dt class="text-xs uppercase tracking-wide text-gray-400">Purge mode</dt>
          <dd class="mt-0.5">{detail.purge_mode}</dd>
        </div>
        <div>
          <dt class="text-xs uppercase tracking-wide text-gray-400">Grace period</dt>
          <dd class="mt-0.5">{detail.grace_period_minutes} min</dd>
        </div>
        <div>
          <dt class="text-xs uppercase tracking-wide text-gray-400">Last applied at</dt>
          <dd class="mt-0.5">{fmtDate(detail.last_applied_at)}</dd>
        </div>
        <div>
          <dt class="text-xs uppercase tracking-wide text-gray-400">Next run</dt>
          <dd class="mt-0.5">{fmtDate(detail.next_run_at)}</dd>
        </div>
        <div>
          <dt class="text-xs uppercase tracking-wide text-gray-400">Selector</dt>
          <dd class="mt-0.5">
            {#if detail.selector.all_datasets}
              All datasets
            {:else if detail.selector.dataset_rid}
              Dataset {detail.selector.dataset_rid}
            {:else if detail.selector.project_id}
              Project {detail.selector.project_id}
            {:else if detail.selector.marking_id}
              Marking {detail.selector.marking_id}
            {:else}
              —
            {/if}
          </dd>
        </div>
      </dl>

      <div class="mt-4">
        <div class="text-xs uppercase tracking-wide text-gray-400">Criteria</div>
        <ul class="mt-1 space-y-1 text-sm">
          {#each describeCriteria(detail.criteria) as line (line)}
            <li class="rounded bg-slate-50 px-2 py-1 dark:bg-gray-800">{line}</li>
          {/each}
        </ul>
      </div>

      {#if detail.rules.length}
        <div class="mt-4">
          <div class="text-xs uppercase tracking-wide text-gray-400">Rules</div>
          <ul class="mt-1 flex flex-wrap gap-1">
            {#each detail.rules as rule (rule)}
              <li class="rounded-full bg-slate-100 px-2 py-0.5 text-xs text-slate-700 dark:bg-gray-800 dark:text-gray-200">
                {rule}
              </li>
            {/each}
          </ul>
        </div>
      {/if}
    </div>
  </div>
{/if}
