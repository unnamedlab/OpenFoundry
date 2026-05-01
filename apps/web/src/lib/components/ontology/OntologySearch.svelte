<script lang="ts">
  /**
   * `OntologySearch.svelte` — Global Cmd+K command palette for the ontology
   * (T8). Calls `searchOntology` (T3) and groups results by `kind`.
   *
   * Mounted once in the root `+layout.svelte`. Other components open it via
   * `ontologySearch.open()`. Keyboard:
   *   - Cmd/Ctrl+K        toggle palette
   *   - Esc               close
   *   - ↑/↓               move selection
   *   - Enter             navigate to selected result
   */
  import { goto } from '$app/navigation';
  import { onDestroy, tick } from 'svelte';
  import { searchOntology, type SearchResult } from '$lib/api/ontology';
  import { ontologySearch } from '$lib/stores/ontologySearch';

  let open = $state(false);
  let query = $state('');
  let loading = $state(false);
  let error = $state('');
  let results = $state<SearchResult[]>([]);
  let highlight = $state(0);
  let inputEl: HTMLInputElement | null = $state(null);
  let debounceId: ReturnType<typeof setTimeout> | null = null;
  let lastQuery = '';

  const KIND_LABELS: Record<string, string> = {
    object_type: 'Object types',
    interface: 'Interfaces',
    link_type: 'Link types',
    action_type: 'Actions',
    object_instance: 'Objects',
    object: 'Objects',
    link: 'Links',
    action: 'Actions',
  };

  const KIND_ORDER = [
    'object_type',
    'interface',
    'link_type',
    'action_type',
    'object_instance',
    'object',
    'link',
    'action',
  ];

  // Subscribe to the global store imperatively (so we react to open/close from
  // any component without prop drilling).
  const unsub = ontologySearch.subscribe(async (state) => {
    if (state.open && !open) {
      open = true;
      query = state.initialQuery ?? '';
      results = [];
      highlight = 0;
      error = '';
      await tick();
      inputEl?.focus();
      if (query.trim()) scheduleSearch();
    } else if (!state.open && open) {
      open = false;
    }
  });
  onDestroy(unsub);

  function close() {
    ontologySearch.close();
  }

  function scheduleSearch() {
    if (debounceId) clearTimeout(debounceId);
    debounceId = setTimeout(() => void runSearch(), 180);
  }

  async function runSearch() {
    const q = query.trim();
    if (!q) {
      results = [];
      lastQuery = '';
      return;
    }
    if (q === lastQuery) return;
    lastQuery = q;
    loading = true;
    error = '';
    try {
      const response = await searchOntology({ query: q, limit: 30 });
      results = response.data ?? [];
      highlight = 0;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      results = [];
    } finally {
      loading = false;
    }
  }

  // Group + flatten preserves a stable order for ↑/↓ navigation.
  const grouped = $derived.by(() => {
    const buckets = new Map<string, SearchResult[]>();
    for (const r of results) {
      const k = r.kind || 'other';
      if (!buckets.has(k)) buckets.set(k, []);
      buckets.get(k)!.push(r);
    }
    const orderedKeys = [
      ...KIND_ORDER.filter((k) => buckets.has(k)),
      ...[...buckets.keys()].filter((k) => !KIND_ORDER.includes(k)),
    ];
    return orderedKeys.map((k) => ({ kind: k, label: KIND_LABELS[k] ?? k, items: buckets.get(k)! }));
  });

  const flat = $derived(grouped.flatMap((g) => g.items));

  function activate(result: SearchResult) {
    close();
    if (result.route) void goto(result.route);
  }

  function onKeydown(event: KeyboardEvent) {
    // Global Cmd/Ctrl+K toggle.
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
      event.preventDefault();
      ontologySearch.toggle();
      return;
    }
    if (!open) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      close();
    } else if (event.key === 'ArrowDown') {
      event.preventDefault();
      if (flat.length) highlight = (highlight + 1) % flat.length;
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      if (flat.length) highlight = (highlight - 1 + flat.length) % flat.length;
    } else if (event.key === 'Enter') {
      event.preventDefault();
      const target = flat[highlight];
      if (target) activate(target);
    }
  }

  $effect(() => {
    // React to query typing.
    void query;
    if (open) scheduleSearch();
  });
</script>

<svelte:window onkeydown={onKeydown} />

{#if open}
  <div
    class="palette-backdrop"
    role="presentation"
    onclick={close}
    onkeydown={(e) => { if (e.key === 'Escape') close(); }}
  >
    <div
      class="palette"
      role="dialog"
      aria-modal="true"
      aria-label="Ontology search"
      tabindex="-1"
      onclick={(e) => e.stopPropagation()}
      onkeydown={(e) => e.stopPropagation()}
    >
      <div class="palette__input">
        <span class="palette__icon" aria-hidden="true">⌘K</span>
        <input
          bind:this={inputEl}
          bind:value={query}
          type="search"
          placeholder="Search ontology — types, objects, links, actions…"
          aria-label="Search ontology"
          autocomplete="off"
          spellcheck="false"
        />
        {#if loading}<span class="palette__loading">Searching…</span>{/if}
      </div>

      <div class="palette__body" role="listbox">
        {#if error}
          <div class="palette__empty palette__error">{error}</div>
        {:else if !query.trim()}
          <div class="palette__empty">
            Type to search across object types, interfaces, links and actions.
            <div class="palette__hint">Tip: ↑↓ to navigate · Enter to open · Esc to close</div>
          </div>
        {:else if !loading && flat.length === 0}
          <div class="palette__empty">No results for “{query}”.</div>
        {:else}
          {#each grouped as group (group.kind)}
            <section class="palette__group">
              <header class="palette__group-header">{group.label} · {group.items.length}</header>
              <ul>
                {#each group.items as item (item.kind + ':' + item.id)}
                  {@const idx = flat.indexOf(item)}
                  <li
                    class="palette__item"
                    class:palette__item--active={idx === highlight}
                    role="option"
                    aria-selected={idx === highlight}
                    onmouseenter={() => (highlight = idx)}
                    onclick={() => activate(item)}
                    onkeydown={(e) => { if (e.key === 'Enter') activate(item); }}
                    tabindex="-1"
                  >
                    <div class="palette__item-main">
                      <span class="palette__item-title">{item.title}</span>
                      {#if item.subtitle}<span class="palette__item-subtitle">{item.subtitle}</span>{/if}
                    </div>
                    {#if item.snippet}
                      <div class="palette__item-snippet">{item.snippet}</div>
                    {/if}
                    <div class="palette__item-meta">
                      <span class="palette__kind">{item.kind}</span>
                      <span class="palette__score">score {item.score.toFixed(3)}</span>
                    </div>
                  </li>
                {/each}
              </ul>
            </section>
          {/each}
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .palette-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(15, 23, 42, 0.55);
    z-index: 100;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding-top: 12vh;
  }
  .palette {
    width: min(720px, 92vw);
    max-height: 70vh;
    display: flex;
    flex-direction: column;
    background: #ffffff;
    color: #0f172a;
    border-radius: 16px;
    box-shadow: 0 30px 80px rgba(15, 23, 42, 0.35);
    overflow: hidden;
    border: 1px solid #e2e8f0;
  }
  .palette__input {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.85rem 1rem;
    border-bottom: 1px solid #e2e8f0;
    background: #f8fafc;
  }
  .palette__icon {
    font-size: 0.7rem;
    font-weight: 600;
    color: #64748b;
    background: #e2e8f0;
    padding: 0.15rem 0.4rem;
    border-radius: 4px;
  }
  .palette__input input {
    flex: 1;
    border: none;
    outline: none;
    background: transparent;
    font-size: 1rem;
    color: inherit;
  }
  .palette__loading {
    font-size: 0.75rem;
    color: #64748b;
  }
  .palette__body {
    overflow-y: auto;
    padding: 0.25rem 0;
  }
  .palette__empty {
    padding: 1.5rem;
    text-align: center;
    color: #64748b;
    font-size: 0.9rem;
  }
  .palette__error { color: #b91c1c; }
  .palette__hint {
    margin-top: 0.5rem;
    font-size: 0.75rem;
    color: #94a3b8;
  }
  .palette__group { padding: 0.25rem 0; }
  .palette__group-header {
    padding: 0.4rem 1rem;
    font-size: 0.7rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: #64748b;
  }
  .palette__group ul { list-style: none; margin: 0; padding: 0; }
  .palette__item {
    padding: 0.55rem 1rem;
    cursor: pointer;
    border-left: 3px solid transparent;
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
  }
  .palette__item--active {
    background: #eff6ff;
    border-left-color: #2563eb;
  }
  .palette__item-main {
    display: flex;
    gap: 0.5rem;
    align-items: baseline;
  }
  .palette__item-title { font-weight: 600; color: #0f172a; }
  .palette__item-subtitle { color: #64748b; font-size: 0.8rem; }
  .palette__item-snippet {
    font-size: 0.8rem;
    color: #475569;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .palette__item-meta {
    display: flex;
    gap: 0.5rem;
    font-size: 0.7rem;
    color: #94a3b8;
  }
  .palette__kind {
    background: #f1f5f9;
    color: #334155;
    padding: 0 0.35rem;
    border-radius: 3px;
    font-family: monospace;
  }

  @media (prefers-color-scheme: dark) {
    .palette {
      background: #0f172a;
      color: #e2e8f0;
      border-color: #1e293b;
    }
    .palette__input { background: #0b1220; border-color: #1e293b; }
    .palette__icon { background: #1e293b; color: #cbd5e1; }
    .palette__item--active { background: #1e293b; border-left-color: #60a5fa; }
    .palette__item-title { color: #e2e8f0; }
    .palette__item-snippet { color: #cbd5e1; }
    .palette__kind { background: #1e293b; color: #cbd5e1; }
  }
</style>
