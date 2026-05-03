<!--
  P3 — BranchGraph

  Renders the per-dataset branch ancestry tree as a Cytoscape graph.
  Maps to the Foundry "Branch taskbar / Global Branching" navigation
  pattern — a vertical layout with the root branch on top and children
  cascading downwards. Edges run parent → child.

  ### Visual semantics
  * 🌿 root branch (no parent), 🌱 child, 🔓 open transaction (warning ámbar)
  * Hover: tooltip with name, head_transaction_rid (short), relative
    `last_activity_at`, `fallback_chain`, labels, `# transactions` and
    `# downstream pipelines` (the last two come from the `extras` map
    keyed by branch name; the parent route loads them lazily so the
    tree itself stays cheap).
  * Click selects the node ⇒ emits `select` with the branch payload.
  * Doubleclick navigates to /datasets/{rid}/branches/{branch}.

  ### Drag&drop reparent
  When `onReparentRequest` is wired, dragging a node ON TOP of another
  node fires the callback with `{ source, candidateParent }`. The
  parent typically opens `ReparentDialog` for confirmation.

  ### Toolbar
  * zoom in / zoom out / fit / reset
  * "Show retired branches" toggle (off by default)
  * label filter + name search

  ### Theming
  CSS custom properties drive colours so dark/light tokens follow the
  rest of the app. Drops back to safe defaults when the host theme
  doesn't define them.
-->
<script lang="ts">
  import { goto } from '$app/navigation';
  import cytoscape, {
    type Core,
    type EventObjectNode,
    type NodeSingular,
  } from 'cytoscape';
  import { onDestroy, onMount } from 'svelte';
  import type { DatasetBranch } from '$lib/api/datasets';

  type ReparentRequest = { source: DatasetBranch; candidateParent: DatasetBranch };

  type ExtraStats = {
    transactions?: number;
    downstreamPipelines?: number;
  };

  type Props = {
    /** Stable dataset RID — used by the doubleclick navigation. */
    datasetRid: string;
    branches: DatasetBranch[];
    /**
     * Optional per-branch extras. Keyed by branch *name* so callers
     * can populate it incrementally as `GET /transactions?branch=...`
     * resolves.
     */
    extras?: Record<string, ExtraStats>;
    /** Currently selected branch — drives ring highlight. */
    selectedBranch?: string | null;
    /** Optional drag&drop hook used by `ReparentDialog`. */
    onReparentRequest?: (req: ReparentRequest) => void;
    /** Fires on single click — typically updates selection state. */
    onSelect?: (branch: DatasetBranch) => void;
  };

  const {
    datasetRid,
    branches,
    extras = {},
    selectedBranch = null,
    onReparentRequest,
    onSelect,
  }: Props = $props();

  let container = $state<HTMLDivElement | undefined>(undefined);
  let cy = $state<Core | null>(null);
  let showRetired = $state(false);
  let labelFilter = $state('');
  let nameSearch = $state('');
  let tooltip = $state<{
    visible: boolean;
    x: number;
    y: number;
    branch: DatasetBranch | null;
  }>({ visible: false, x: 0, y: 0, branch: null });

  // ── Filtered branch view ──────────────────────────────────────────
  const visible = $derived.by<DatasetBranch[]>(() => {
    return branches.filter((b) => {
      if (!showRetired && (b as { deleted_at?: string }).deleted_at) return false;
      if (labelFilter.trim()) {
        const wanted = labelFilter.trim();
        const labels = b.labels ?? {};
        const matches = Object.entries(labels).some(
          ([k, v]) => `${k}=${v}`.includes(wanted) || k.includes(wanted) || v.includes(wanted),
        );
        if (!matches) return false;
      }
      if (nameSearch.trim() && !b.name.toLowerCase().includes(nameSearch.trim().toLowerCase())) {
        return false;
      }
      return true;
    });
  });

  function shortRid(rid: string | null | undefined): string {
    if (!rid) return '—';
    const tail = rid.split('.').pop() ?? '';
    return tail.length > 12 ? `${tail.slice(0, 8)}…` : tail;
  }

  function relativeTime(ts?: string): string {
    if (!ts) return '—';
    const then = new Date(ts).getTime();
    if (Number.isNaN(then)) return ts;
    const delta = Math.max(0, Date.now() - then);
    const seconds = Math.round(delta / 1000);
    if (seconds < 60) return `${seconds}s ago`;
    const minutes = Math.round(seconds / 60);
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.round(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.round(hours / 24);
    return `${days}d ago`;
  }

  function isRoot(b: DatasetBranch): boolean {
    if (b.parent_branch_id == null) return true;
    return b.is_default && !b.parent_branch_id;
  }

  function nodeIcon(b: DatasetBranch): string {
    if (b.has_open_transaction) return '🔓';
    return isRoot(b) ? '🌿' : '🌱';
  }

  // ── Cytoscape style ───────────────────────────────────────────────
  const baseStyle: cytoscape.StylesheetJsonBlock[] = [
    {
      selector: 'node',
      style: {
        'background-color': '#0f172a',
        'border-width': 2,
        'border-color': '#334155',
        label: 'data(label)',
        color: '#e2e8f0',
        'text-valign': 'bottom',
        'text-margin-y': 8,
        'font-size': '11px',
        width: 44,
        height: 44,
        'text-wrap': 'wrap',
        'text-max-width': '120',
      },
    },
    {
      selector: 'node[icon]',
      style: { content: 'data(icon)', 'text-valign': 'center', 'text-margin-y': 0, 'font-size': '18px' },
    },
    {
      selector: 'node.root',
      style: { 'border-color': '#10b981', 'background-color': '#064e3b' },
    },
    {
      selector: 'node.has-open',
      style: { 'border-color': '#f59e0b', 'background-color': '#78350f' },
    },
    {
      selector: 'node.retired',
      style: { 'border-style': 'dashed', opacity: 0.55 },
    },
    {
      selector: 'node.selected',
      style: { 'border-width': 4, 'border-color': '#3b82f6' },
    },
    {
      selector: 'edge',
      style: {
        width: 2,
        'line-color': '#475569',
        'target-arrow-color': '#475569',
        'target-arrow-shape': 'triangle',
        'curve-style': 'bezier',
      },
    },
  ];

  function buildElements(rows: DatasetBranch[]): cytoscape.ElementDefinition[] {
    const ids = new Set(rows.map((b) => b.id));
    const nodes: cytoscape.ElementDefinition[] = rows.map((b) => ({
      data: {
        id: b.id,
        name: b.name,
        label: `${nodeIcon(b)}\n${b.name}`,
        icon: nodeIcon(b),
        branch: b,
      },
      classes: [
        isRoot(b) ? 'root' : 'child',
        b.has_open_transaction ? 'has-open' : '',
        (b as { deleted_at?: string }).deleted_at ? 'retired' : '',
        selectedBranch && b.name === selectedBranch ? 'selected' : '',
      ]
        .filter(Boolean)
        .join(' '),
    }));
    const edges: cytoscape.ElementDefinition[] = rows
      .filter((b) => b.parent_branch_id && ids.has(b.parent_branch_id))
      .map((b) => ({
        data: {
          id: `${b.parent_branch_id}->${b.id}`,
          source: b.parent_branch_id as string,
          target: b.id,
        },
      }));
    return [...nodes, ...edges];
  }

  function rebuild() {
    if (!cy) return;
    cy.elements().remove();
    cy.add(buildElements(visible));
    cy.layout({
      name: 'breadthfirst',
      directed: true,
      roots: visible.filter(isRoot).map((b) => b.id),
      spacingFactor: 1.4,
      padding: 20,
    } as cytoscape.LayoutOptions).run();
  }

  function attachInteractions(instance: Core) {
    instance.on('tap', 'node', (event: EventObjectNode) => {
      const branch = event.target.data('branch') as DatasetBranch;
      if (onSelect) onSelect(branch);
    });
    instance.on('dblclick', 'node', (event: EventObjectNode) => {
      const branch = event.target.data('branch') as DatasetBranch;
      goto(
        `/datasets/${encodeURIComponent(datasetRid)}/branches/${encodeURIComponent(branch.name)}`,
      );
    });
    instance.on('mouseover', 'node', (event: EventObjectNode) => {
      const node = event.target as NodeSingular;
      const branch = node.data('branch') as DatasetBranch;
      const pos = node.renderedPosition();
      tooltip = { visible: true, x: pos.x + 32, y: pos.y, branch };
    });
    instance.on('mouseout', 'node', () => {
      tooltip = { ...tooltip, visible: false };
    });

    // ── Drag&drop reparent (manual hit-testing) ──
    if (onReparentRequest) {
      let dragging: { id: string; startedAt: number } | null = null;
      instance.on('grab', 'node', (event: EventObjectNode) => {
        dragging = { id: String((event.target as NodeSingular).id()), startedAt: Date.now() };
      });
      instance.on('free', 'node', (event: EventObjectNode) => {
        if (!dragging) return;
        const sourceId = dragging.id;
        const droppedAt = (event.target as NodeSingular).renderedPosition();
        dragging = null;
        const candidate = instance
          .nodes()
          .filter((n) => n.id() !== sourceId)
          .toArray()
          .map((n) => n as NodeSingular)
          .find((n) => {
            const p = n.renderedPosition();
            return Math.abs(p.x - droppedAt.x) < 36 && Math.abs(p.y - droppedAt.y) < 36;
          });
        if (!candidate) return;
        const sourceNode = instance.getElementById(sourceId) as NodeSingular;
        const sourceBranch = sourceNode.data('branch') as DatasetBranch | undefined;
        const parentBranch = candidate.data('branch') as DatasetBranch | undefined;
        if (!sourceBranch || !parentBranch) return;
        if (sourceBranch.id === parentBranch.id) return;
        onReparentRequest({ source: sourceBranch, candidateParent: parentBranch });
      });
    }
  }

  // ── Toolbar ───────────────────────────────────────────────────────
  function zoomBy(factor: number) {
    if (!cy) return;
    const target = cy.zoom() * factor;
    cy.zoom({ level: target, position: { x: cy.width() / 2, y: cy.height() / 2 } });
  }

  function fitView() {
    cy?.fit(undefined, 30);
  }

  // ── Lifecycle ─────────────────────────────────────────────────────
  onMount(() => {
    if (!container) return;
    cy = cytoscape({
      container,
      elements: buildElements(visible),
      style: baseStyle,
      layout: {
        name: 'breadthfirst',
        directed: true,
        spacingFactor: 1.4,
        padding: 20,
      } as cytoscape.LayoutOptions,
      wheelSensitivity: 0.2,
    });
    attachInteractions(cy);
  });

  onDestroy(() => cy?.destroy());

  // Re-render when the underlying branch list (or filters) change.
  $effect(() => {
    void visible;
    void selectedBranch;
    rebuild();
  });
</script>

<div
  class="relative flex h-full min-h-[420px] w-full flex-col rounded-2xl border border-slate-200 bg-white shadow-sm dark:border-gray-700 dark:bg-gray-900"
  data-testid="branch-graph"
>
  <div
    class="flex flex-wrap items-center gap-2 border-b border-slate-200 px-3 py-2 text-xs dark:border-gray-700"
  >
    <button
      type="button"
      class="rounded border border-slate-300 px-2 py-1 hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800"
      onclick={() => zoomBy(1.2)}
      data-testid="branch-graph-zoom-in"
      aria-label="Zoom in"
    >+</button>
    <button
      type="button"
      class="rounded border border-slate-300 px-2 py-1 hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800"
      onclick={() => zoomBy(0.85)}
      data-testid="branch-graph-zoom-out"
      aria-label="Zoom out"
    >−</button>
    <button
      type="button"
      class="rounded border border-slate-300 px-2 py-1 hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800"
      onclick={fitView}
      data-testid="branch-graph-fit"
    >Fit</button>
    <label class="ml-2 inline-flex items-center gap-1">
      <input
        type="checkbox"
        bind:checked={showRetired}
        data-testid="branch-graph-toggle-retired"
      />
      <span>Show retired branches</span>
    </label>
    <input
      type="text"
      bind:value={labelFilter}
      placeholder="Filter by label (e.g. persona=data-eng)"
      class="ml-2 rounded border border-slate-300 px-2 py-1 dark:border-gray-700 dark:bg-gray-800"
      data-testid="branch-graph-label-filter"
    />
    <input
      type="text"
      bind:value={nameSearch}
      placeholder="Search by name"
      class="rounded border border-slate-300 px-2 py-1 dark:border-gray-700 dark:bg-gray-800"
      data-testid="branch-graph-name-search"
    />
  </div>

  <div class="relative grow" bind:this={container} data-testid="branch-graph-canvas"></div>

  {#if tooltip.visible && tooltip.branch}
    {@const b = tooltip.branch}
    {@const e = extras[b.name] ?? {}}
    <div
      class="pointer-events-none absolute z-30 max-w-xs rounded-lg border border-slate-200 bg-white p-2 text-xs shadow-lg dark:border-gray-700 dark:bg-gray-900"
      style="left: {tooltip.x}px; top: {tooltip.y}px;"
      data-testid="branch-graph-tooltip"
    >
      <div class="flex items-center gap-1 font-semibold">
        <span aria-hidden="true">{nodeIcon(b)}</span>
        <span>{b.name}</span>
        {#if b.has_open_transaction}
          <span class="rounded bg-amber-200 px-1 text-[10px] font-medium text-amber-800">OPEN tx</span>
        {/if}
      </div>
      <dl class="mt-1 space-y-0.5">
        <div class="flex justify-between gap-2">
          <dt class="text-gray-500">Head</dt>
          <dd class="font-mono">{shortRid(b.head_transaction_id ?? null)}</dd>
        </div>
        <div class="flex justify-between gap-2">
          <dt class="text-gray-500">Last activity</dt>
          <dd>{relativeTime(b.last_activity_at)}</dd>
        </div>
        <div class="flex justify-between gap-2">
          <dt class="text-gray-500">Fallback</dt>
          <dd class="font-mono">
            {(b.fallback_chain ?? []).join(' → ') || '—'}
          </dd>
        </div>
        {#if b.labels && Object.keys(b.labels).length > 0}
          <div class="flex justify-between gap-2">
            <dt class="text-gray-500">Labels</dt>
            <dd class="text-right">
              {#each Object.entries(b.labels) as [k, v]}
                <span class="ml-1 rounded bg-slate-100 px-1 text-[10px] dark:bg-gray-800">{k}={v}</span>
              {/each}
            </dd>
          </div>
        {/if}
        {#if e.transactions != null}
          <div class="flex justify-between gap-2">
            <dt class="text-gray-500"># transactions</dt>
            <dd>{e.transactions}</dd>
          </div>
        {/if}
        {#if e.downstreamPipelines != null}
          <div class="flex justify-between gap-2">
            <dt class="text-gray-500"># downstream pipelines</dt>
            <dd>{e.downstreamPipelines}</dd>
          </div>
        {/if}
      </dl>
    </div>
  {/if}
</div>
