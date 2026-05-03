<!--
  T5.5 — DatasetHeader

  Foundry-style header strip:

    * Breadcrumbs derived from the storage path (project / sub-folder
      / dataset name).
    * Dataset name + RID (mono).
    * BranchPicker for the active branch.
    * MarkingBadge stack for every effective marking.
    * Three action menus, all stubbed when the integration isn't
      live yet:
        - "All actions ▾" — generic operations (Star, Move, Permissions,
          Delete).
        - "Build ▾" — kick off the producing pipeline.
        - "Analyze in Contour ▾" — open the dataset in Contour.
        - "Explore pipeline ▾" — jump into the lineage / pipeline graph.
-->
<script lang="ts">
  import type { Dataset, DatasetBranch, DatasetJobSpecStatus } from '$lib/api/datasets';
  import BranchPicker from './BranchPicker.svelte';
  import JobSpecBadge from './JobSpecBadge.svelte';
  import MarkingBadge from './MarkingBadge.svelte';

  type EffectiveMarking = {
    id: string;
    label: string;
    level?: 'public' | 'confidential' | 'pii' | 'restricted' | 'unknown';
    source: { kind: 'direct' } | { kind: 'inherited_from_upstream'; upstream_rid: string };
  };

  type Props = {
    dataset: Dataset;
    branches: DatasetBranch[];
    markings: EffectiveMarking[];
    busy?: boolean;
    /**
     * P3 — JobSpec coloring (Foundry doc § "Job graph compilation").
     * When omitted the badge falls back to "no JobSpec on master".
     */
    jobSpecStatus?: DatasetJobSpecStatus;
    /** P5 — true when the current user has dataset-manage role. Drives
     *  the visibility of the "Publish to Marketplace" entry. */
    canManage?: boolean;
    onSwitchBranch: (name: string) => void | Promise<void>;
    onCreateBranch: (params: { name: string; from: string; description?: string }) => void | Promise<void>;
    /** Parent owns the action handlers; when omitted, we wire stubs. */
    onAllActions?: (action: string) => void;
    onBuild?: (target: string) => void;
    onAnalyze?: (target: string) => void;
    onExplorePipeline?: (target: string) => void;
    /** P5 — "Open in…" callback. The parent calls the destination
     *  service's create endpoint with `seed_dataset_rid` and then
     *  navigates to the resulting URL. When omitted we fall back to
     *  query-string-only navigation (no resource creation). */
    onOpenIn?: (target: OpenInTarget) => void | Promise<void>;
    /** P5 — "Publish to Marketplace" callback. Parent opens the
     *  modal and calls `POST /v1/products/from-dataset/{rid}`. */
    onPublishToMarketplace?: () => void;
  };

  /** P5 — destinations supported by the "Open in…" menu. The four
   *  marked `comingSoon` are listed but disabled because their target
   *  services don't have a real `main.rs` yet (Foundry doc parity but
   *  no runtime). */
  export type OpenInTarget =
    | 'pipeline_builder'
    | 'notebook'
    | 'sql_workbench'
    | 'contour'
    | 'fusion'
    | 'workshop'
    | 'code_workspaces';

  const {
    dataset,
    branches,
    markings,
    busy = false,
    jobSpecStatus,
    canManage = false,
    onSwitchBranch,
    onCreateBranch,
    onAllActions,
    onBuild,
    onAnalyze,
    onExplorePipeline,
    onOpenIn,
    onPublishToMarketplace,
  }: Props = $props();

  // Track which menu is open at most one at a time.
  let openMenu = $state<'actions' | 'build' | 'analyze' | 'explore' | 'open_in' | null>(null);

  /** Per-destination metadata. `disabled` = the binary isn't wired yet,
   *  in which case the click is suppressed and we surface a "Coming
   *  soon" tooltip. The `fallbackHref` is what the parent should
   *  navigate to when no callback is wired (a vanilla URL with
   *  `dataset_rid` prefilled in the query string, matching the doc's
   *  contract for cross-app entry points). */
  const OPEN_IN_TARGETS: ReadonlyArray<{
    target: OpenInTarget;
    label: string;
    fallbackHref: (rid: string) => string;
    comingSoon: boolean;
    note?: string;
  }> = [
    {
      target: 'pipeline_builder',
      label: 'Pipeline Builder',
      fallbackHref: (rid) => `/pipelines/new?input=${encodeURIComponent(rid)}`,
      comingSoon: false,
    },
    {
      target: 'notebook',
      label: 'Notebook',
      fallbackHref: (rid) => `/notebooks/new?seed_dataset_rid=${encodeURIComponent(rid)}`,
      comingSoon: true,
      note: 'notebook-runtime-service binary stub',
    },
    {
      target: 'sql_workbench',
      label: 'SQL workbench',
      fallbackHref: (rid) => `/workbench/new?seed_dataset_rid=${encodeURIComponent(rid)}`,
      comingSoon: false,
    },
    {
      target: 'contour',
      label: 'Contour',
      fallbackHref: (rid) => `/contour?dataset_rid=${encodeURIComponent(rid)}`,
      comingSoon: true,
      note: 'Contour binary not wired yet',
    },
    {
      target: 'fusion',
      label: 'Fusion',
      fallbackHref: (rid) => `/fusion?dataset_rid=${encodeURIComponent(rid)}`,
      comingSoon: true,
      note: 'Fusion binary not wired yet',
    },
    {
      target: 'workshop',
      label: 'Workshop',
      fallbackHref: (rid) => `/workshop?dataset_rid=${encodeURIComponent(rid)}`,
      comingSoon: true,
      note: 'Workshop binary not wired yet',
    },
    {
      target: 'code_workspaces',
      label: 'Code Workspaces',
      fallbackHref: (rid) => `/code-workspaces/new?dataset_rid=${encodeURIComponent(rid)}`,
      comingSoon: false,
    },
  ];

  function toggle(menu: NonNullable<typeof openMenu>) {
    openMenu = openMenu === menu ? null : menu;
  }

  function close() {
    openMenu = null;
  }

  function breadcrumbs(): string[] {
    const path = dataset.storage_path || '';
    const parts = path.split('/').filter(Boolean);
    if (parts.length === 0) return [dataset.name];
    return parts;
  }

  function fire(handler: ((arg: string) => void) | undefined, action: string, fallbackHref?: string) {
    close();
    if (handler) {
      handler(action);
      return;
    }
    if (fallbackHref) {
      window.location.assign(fallbackHref);
      return;
    }
    // Stubbed: surface a console message so devs see the wire-up path.
    console.info(`[dataset-header] ${action} (stub)`);
  }
</script>

<svelte:window onclick={close} />

<header class="space-y-3 rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-900">
  <nav class="text-xs text-gray-500" aria-label="Breadcrumb">
    {#each breadcrumbs() as crumb, idx (crumb + idx)}
      <span>{crumb}</span>
      {#if idx < breadcrumbs().length - 1}
        <span class="mx-1">/</span>
      {/if}
    {/each}
  </nav>

  <div class="flex flex-wrap items-center justify-between gap-3">
    <div class="flex flex-wrap items-center gap-3">
      <h1 class="text-2xl font-bold">{dataset.name}</h1>
      <span class="rounded-md bg-slate-100 px-2 py-1 font-mono text-[11px] text-slate-700 dark:bg-gray-800 dark:text-gray-300">
        {dataset.id}
      </span>
      <JobSpecBadge
        hasMasterJobspec={jobSpecStatus?.has_master_jobspec ?? false}
        branchesWithJobspec={jobSpecStatus?.branches_with_jobspec ?? []}
      />
      <BranchPicker
        branches={branches}
        currentBranch={dataset.active_branch}
        onSwitch={onSwitchBranch}
        onCreate={onCreateBranch}
        busy={busy}
      />
      {#if markings.length > 0}
        <div class="flex flex-wrap items-center gap-1">
          {#each markings as m (m.id)}
            <MarkingBadge id={m.id} label={m.label} level={m.level} source={m.source} compact />
          {/each}
        </div>
      {/if}
    </div>

    <div class="flex flex-wrap gap-2" onclick={(e) => e.stopPropagation()} onkeydown={(e) => { if (e.key === 'Escape') close(); }}>
      <!-- All actions menu -->
      <div class="relative">
        <button
          type="button"
          class="rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-sm hover:bg-slate-50 dark:border-gray-700 dark:bg-gray-900 dark:hover:bg-gray-800"
          onclick={() => toggle('actions')}
        >
          All actions ▾
        </button>
        {#if openMenu === 'actions'}
          <div class="absolute right-0 z-20 mt-1 w-48 rounded-lg border border-slate-200 bg-white shadow-lg dark:border-gray-700 dark:bg-gray-900">
            <button class="block w-full px-3 py-1.5 text-left text-sm hover:bg-slate-50 dark:hover:bg-gray-800" onclick={() => fire(onAllActions, 'star')}>★ Star</button>
            <button class="block w-full px-3 py-1.5 text-left text-sm hover:bg-slate-50 dark:hover:bg-gray-800" onclick={() => fire(onAllActions, 'move')}>Move…</button>
            <button class="block w-full px-3 py-1.5 text-left text-sm hover:bg-slate-50 dark:hover:bg-gray-800" onclick={() => fire(onAllActions, 'permissions', `/datasets/${dataset.id}/permissions`)}>Permissions…</button>
            <button class="block w-full px-3 py-1.5 text-left text-sm text-rose-600 hover:bg-rose-50 dark:hover:bg-rose-950/30" onclick={() => fire(onAllActions, 'delete')}>Delete…</button>
          </div>
        {/if}
      </div>

      <!-- Build menu -->
      <div class="relative">
        <button
          type="button"
          class="rounded-lg bg-blue-600 px-3 py-1.5 text-sm text-white hover:bg-blue-700"
          onclick={() => toggle('build')}
        >
          Build ▾
        </button>
        {#if openMenu === 'build'}
          <div class="absolute right-0 z-20 mt-1 w-56 rounded-lg border border-slate-200 bg-white shadow-lg dark:border-gray-700 dark:bg-gray-900">
            <button class="block w-full px-3 py-1.5 text-left text-sm hover:bg-slate-50 dark:hover:bg-gray-800" onclick={() => fire(onBuild, 'build_now')}>Build now</button>
            <button class="block w-full px-3 py-1.5 text-left text-sm hover:bg-slate-50 dark:hover:bg-gray-800" onclick={() => fire(onBuild, 'build_with_options')}>Build with options…</button>
            <button class="block w-full px-3 py-1.5 text-left text-sm hover:bg-slate-50 dark:hover:bg-gray-800" onclick={() => fire(onBuild, 'cancel_running')}>Cancel running build</button>
          </div>
        {/if}
      </div>

      <!-- Analyze in Contour -->
      <div class="relative">
        <button
          type="button"
          class="rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-sm hover:bg-slate-50 dark:border-gray-700 dark:bg-gray-900 dark:hover:bg-gray-800"
          onclick={() => toggle('analyze')}
        >
          Analyze in Contour ▾
        </button>
        {#if openMenu === 'analyze'}
          <div class="absolute right-0 z-20 mt-1 w-56 rounded-lg border border-slate-200 bg-white shadow-lg dark:border-gray-700 dark:bg-gray-900">
            <button class="block w-full px-3 py-1.5 text-left text-sm hover:bg-slate-50 dark:hover:bg-gray-800" onclick={() => fire(onAnalyze, 'open_contour', `/contour?dataset_rid=${dataset.id}`)}>Open in Contour</button>
            <button class="block w-full px-3 py-1.5 text-left text-sm hover:bg-slate-50 dark:hover:bg-gray-800" onclick={() => fire(onAnalyze, 'open_quiver', `/quiver?dataset_rid=${dataset.id}`)}>Open in Quiver</button>
          </div>
        {/if}
      </div>

      <!-- P5 — Open in… cross-app entry points -->
      <div class="relative">
        <button
          type="button"
          class="rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-sm hover:bg-slate-50 dark:border-gray-700 dark:bg-gray-900 dark:hover:bg-gray-800"
          onclick={() => toggle('open_in')}
          data-testid="open-in-trigger"
        >
          Open in… ▾
        </button>
        {#if openMenu === 'open_in'}
          <div class="absolute right-0 z-20 mt-1 w-64 rounded-lg border border-slate-200 bg-white shadow-lg dark:border-gray-700 dark:bg-gray-900" data-testid="open-in-menu">
            {#each OPEN_IN_TARGETS as item (item.target)}
              <button
                type="button"
                class={`block w-full px-3 py-1.5 text-left text-sm ${
                  item.comingSoon
                    ? 'cursor-not-allowed text-gray-400 dark:text-gray-500'
                    : 'hover:bg-slate-50 dark:hover:bg-gray-800'
                }`}
                disabled={item.comingSoon}
                title={item.comingSoon ? `Coming soon — ${item.note ?? ''}` : ''}
                data-testid={`open-in-${item.target}`}
                onclick={() => {
                  if (item.comingSoon) return;
                  close();
                  if (onOpenIn) {
                    void onOpenIn(item.target);
                    return;
                  }
                  window.location.assign(item.fallbackHref(dataset.id));
                }}
              >
                <span class="flex items-center justify-between gap-2">
                  <span>{item.label}</span>
                  {#if item.comingSoon}
                    <span class="rounded-full bg-amber-100 px-2 py-0.5 text-[10px] uppercase tracking-wide text-amber-800 dark:bg-amber-900/40 dark:text-amber-200">
                      Coming soon
                    </span>
                  {/if}
                </span>
              </button>
            {/each}
            {#if canManage}
              <div class="my-1 border-t border-slate-200 dark:border-gray-700"></div>
              <button
                type="button"
                class="block w-full px-3 py-1.5 text-left text-sm text-blue-700 hover:bg-blue-50 dark:text-blue-300 dark:hover:bg-blue-950/30"
                data-testid="publish-to-marketplace"
                onclick={() => {
                  close();
                  onPublishToMarketplace?.();
                }}
              >
                Publish to Marketplace…
              </button>
            {/if}
          </div>
        {/if}
      </div>

      <!-- Explore pipeline -->
      <div class="relative">
        <button
          type="button"
          class="rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-sm hover:bg-slate-50 dark:border-gray-700 dark:bg-gray-900 dark:hover:bg-gray-800"
          onclick={() => toggle('explore')}
        >
          Explore pipeline ▾
        </button>
        {#if openMenu === 'explore'}
          <div class="absolute right-0 z-20 mt-1 w-56 rounded-lg border border-slate-200 bg-white shadow-lg dark:border-gray-700 dark:bg-gray-900">
            <button class="block w-full px-3 py-1.5 text-left text-sm hover:bg-slate-50 dark:hover:bg-gray-800" onclick={() => fire(onExplorePipeline, 'lineage', `/lineage?rid=${dataset.id}`)}>View lineage graph</button>
            <button class="block w-full px-3 py-1.5 text-left text-sm hover:bg-slate-50 dark:hover:bg-gray-800" onclick={() => fire(onExplorePipeline, 'pipeline_builder', `/pipeline-builder?dataset_rid=${dataset.id}`)}>Open in Pipeline Builder</button>
          </div>
        {/if}
      </div>
    </div>
  </div>
</header>
