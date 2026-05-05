<script lang="ts">
  /**
   * Lazy tree of the remote catalog under a Virtual-Tables-enabled
   * source. Foundry doc anchor: § "Set up a connection for a virtual
   * table" — img_002 / img_003 show the same browse pattern.
   *
   * The component owns its own 60-second cache (per the prompt) so
   * expand-collapse-expand cycles do not hammer the backend; cache
   * keys include the source rid so two sources never share entries.
   */
  import { virtualTables, type DiscoveredEntry } from '$lib/api/virtual-tables';

  type Props = {
    sourceRid: string;
    onSelect?: (entry: DiscoveredEntry) => void;
  };

  let { sourceRid, onSelect }: Props = $props();

  type NodeState = {
    entry: DiscoveredEntry;
    expanded: boolean;
    loading: boolean;
    error: string | null;
    children: NodeState[] | null;
  };

  let roots = $state<NodeState[]>([]);
  let rootError = $state('');
  let rootLoading = $state(false);
  let selectedPath = $state<string | null>(null);
  let search = $state('');
  let debouncedSearch = $state('');
  let debounceTimer: ReturnType<typeof setTimeout> | null = null;

  // Per-(source, path) entry cache keyed by `${sourceRid}::${path}`.
  // Values are tuples of the response array + the timestamp the
  // response was cached at; we expire after 60s per the prompt.
  const cache = new Map<string, { entries: DiscoveredEntry[]; cachedAt: number }>();
  const CACHE_TTL_MS = 60_000;

  function cacheKey(path: string | undefined): string {
    return `${sourceRid}::${path ?? ''}`;
  }

  async function fetchEntries(path?: string): Promise<DiscoveredEntry[]> {
    const key = cacheKey(path);
    const hit = cache.get(key);
    if (hit && Date.now() - hit.cachedAt < CACHE_TTL_MS) {
      return hit.entries;
    }
    const response = await virtualTables.discoverRemoteCatalog(sourceRid, path);
    cache.set(key, { entries: response.data, cachedAt: Date.now() });
    return response.data;
  }

  async function loadRoots() {
    rootLoading = true;
    rootError = '';
    try {
      const entries = await fetchEntries();
      roots = entries.map(toNode);
    } catch (err) {
      rootError = err instanceof Error ? err.message : 'Failed to load catalog';
    } finally {
      rootLoading = false;
    }
  }

  function toNode(entry: DiscoveredEntry): NodeState {
    return { entry, expanded: false, loading: false, error: null, children: null };
  }

  async function toggle(node: NodeState) {
    if (node.expanded) {
      node.expanded = false;
      return;
    }
    node.expanded = true;
    if (node.children !== null) return;
    node.loading = true;
    node.error = null;
    try {
      const children = await fetchEntries(node.entry.path);
      node.children = children.map(toNode);
    } catch (err) {
      node.error = err instanceof Error ? err.message : 'Failed to load children';
    } finally {
      node.loading = false;
    }
  }

  function select(node: NodeState) {
    selectedPath = node.entry.path;
    onSelect?.(node.entry);
  }

  function iconFor(kind: DiscoveredEntry['kind']): string {
    switch (kind) {
      case 'database':
        return '🗄';
      case 'schema':
        return '📁';
      case 'table':
        return '🟦';
      case 'view':
        return '🪟';
      case 'materialized_view':
        return '🟪';
      case 'iceberg_namespace':
        return '🧊';
      case 'iceberg_table':
        return '❄️';
      case 'file_prefix':
        return '🗂';
      default:
        return '·';
    }
  }

  function matches(node: NodeState): boolean {
    if (!debouncedSearch) return true;
    return node.entry.display_name.toLowerCase().includes(debouncedSearch);
  }

  function onSearchInput(value: string) {
    search = value;
    if (debounceTimer) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      debouncedSearch = value.trim().toLowerCase();
    }, 300);
  }

  $effect(() => {
    if (sourceRid) {
      void loadRoots();
    }
  });
</script>

<div class="catalog-browser" data-testid="vt-catalog-browser">
  <div class="search">
    <input
      type="text"
      placeholder="Filter…"
      value={search}
      oninput={(e) => onSearchInput((e.currentTarget as HTMLInputElement).value)}
      data-testid="vt-catalog-search"
    />
  </div>
  {#if rootLoading}
    <div class="loading">Loading remote catalog…</div>
  {:else if rootError}
    <div class="error" role="alert">{rootError}</div>
  {:else if roots.length === 0}
    <div class="muted">No entries.</div>
  {:else}
    <ul class="tree" role="tree">
      {#each roots.filter(matches) as node, i (node.entry.path || `root-${i}`)}
        {@render branch(node, 0)}
      {/each}
    </ul>
  {/if}
</div>

{#snippet branch(node: NodeState, depth: number)}
  <li role="treeitem" aria-expanded={node.expanded}>
    <div
      class={node.entry.path === selectedPath ? 'row selected' : 'row'}
      style:padding-left={`${depth * 12 + 4}px`}
    >
      <button
        type="button"
        class="caret"
        aria-label={node.expanded ? 'Collapse' : 'Expand'}
        onclick={() => toggle(node)}
        disabled={node.entry.kind === 'table' || node.entry.kind === 'iceberg_table'}
        data-testid="vt-catalog-toggle"
      >
        {node.expanded ? '▾' : '▸'}
      </button>
      <button
        type="button"
        class="label"
        onclick={() => select(node)}
        data-testid="vt-catalog-row"
      >
        <span class="icon" aria-hidden="true">{iconFor(node.entry.kind)}</span>
        <span class="name">{node.entry.display_name}</span>
        {#if node.entry.registrable}
          <span class="badge" title={node.entry.inferred_table_type ?? ''}>registrable</span>
        {/if}
      </button>
    </div>
    {#if node.expanded}
      {#if node.loading}
        <div class="loading-child" style:padding-left={`${depth * 12 + 28}px`}>Loading…</div>
      {:else if node.error}
        <div class="error" style:padding-left={`${depth * 12 + 28}px`}>{node.error}</div>
      {:else if node.children}
        <ul role="group">
          {#each node.children.filter(matches) as child, j (child.entry.path || `child-${j}`)}
            {@render branch(child, depth + 1)}
          {/each}
        </ul>
      {/if}
    {/if}
  </li>
{/snippet}

<style>
  .catalog-browser {
    border: 1px solid var(--color-border, #e5e7eb);
    border-radius: 0.5rem;
    background: var(--color-bg-elevated, #fff);
    display: flex;
    flex-direction: column;
    min-height: 360px;
    max-height: 520px;
  }
  .search {
    border-bottom: 1px solid var(--color-border, #e5e7eb);
    padding: 0.5rem;
  }
  .search input {
    width: 100%;
    padding: 0.375rem 0.5rem;
    font-size: 0.875rem;
    border: 1px solid var(--color-border, #d1d5db);
    border-radius: 0.25rem;
  }
  .tree {
    list-style: none;
    padding: 0.25rem 0;
    margin: 0;
    overflow: auto;
    flex: 1;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    padding-right: 0.5rem;
  }
  .row.selected {
    background: #ecfeff;
  }
  .caret {
    width: 1.25rem;
    background: transparent;
    border: none;
    cursor: pointer;
    color: var(--color-fg-muted, #6b7280);
  }
  .caret:disabled {
    cursor: default;
    opacity: 0.3;
  }
  .label {
    flex: 1;
    text-align: left;
    background: transparent;
    border: none;
    cursor: pointer;
    display: flex;
    gap: 0.5rem;
    align-items: center;
    padding: 0.25rem 0;
    font-size: 0.875rem;
  }
  .icon {
    width: 1rem;
  }
  .badge {
    margin-left: auto;
    font-size: 0.625rem;
    background: #ecfdf5;
    color: #047857;
    padding: 0.0625rem 0.375rem;
    border-radius: 0.25rem;
    border: 1px solid #6ee7b7;
  }
  .loading,
  .loading-child,
  .error,
  .muted {
    padding: 0.5rem 0.75rem;
    font-size: 0.875rem;
    color: var(--color-fg-muted, #4b5563);
  }
  .error {
    color: #b91c1c;
  }
</style>
