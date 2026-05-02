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
  import type { Dataset, DatasetBranch } from '$lib/api/datasets';
  import BranchPicker from './BranchPicker.svelte';
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
    onSwitchBranch: (name: string) => void | Promise<void>;
    onCreateBranch: (params: { name: string; from: string; description?: string }) => void | Promise<void>;
    /** Parent owns the action handlers; when omitted, we wire stubs. */
    onAllActions?: (action: string) => void;
    onBuild?: (target: string) => void;
    onAnalyze?: (target: string) => void;
    onExplorePipeline?: (target: string) => void;
  };

  const {
    dataset,
    branches,
    markings,
    busy = false,
    onSwitchBranch,
    onCreateBranch,
    onAllActions,
    onBuild,
    onAnalyze,
    onExplorePipeline,
  }: Props = $props();

  // Track which menu is open at most one at a time.
  let openMenu = $state<'actions' | 'build' | 'analyze' | 'explore' | null>(null);

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
