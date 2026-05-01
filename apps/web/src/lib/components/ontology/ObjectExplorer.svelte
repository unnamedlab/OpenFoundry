<script lang="ts">
  /**
   * `ObjectExplorer` — tabla de instancias de un `ObjectType` con filtros por
   * propiedad, facets de marking y paginación.
   *
   * Llama a `listObjects` para listados completos y a `queryObjects` cuando
   * hay filtros igualdad activos. La virtualización se hace por slicing de
   * página (per_page); para cargar más, paginación clásica. Una virtualización
   * row-level con `@tanstack/svelte-virtual` queda como TODO si el dataset
   * supera ~5k filas.
   *
   * Contrato Svelte 5:
   * - `typeId: string`
   * - `properties?: Property[]`            — para columnas y selector de filtros.
   * - `pageSize?: number = 50`
   * - `onselect?: (object: ObjectInstance) => void`
   */
  import {
    listObjects,
    listProperties,
    queryObjects,
    type ObjectInstance,
    type Property,
  } from '$lib/api/ontology';
  import ObjectCard from './ObjectCard.svelte';

  type Props = {
    typeId: string;
    properties?: Property[];
    pageSize?: number;
    onselect?: (object: ObjectInstance) => void;
  };

  const { typeId, properties: propertiesProp, pageSize = 50, onselect }: Props = $props();

  let loading = $state(false);
  let error = $state('');
  let page = $state(1);
  let total = $state(0);
  let objects = $state<ObjectInstance[]>([]);
  let properties = $state<Property[]>([]);
  let filters = $state<Array<{ name: string; value: string }>>([]);
  let markingFacet = $state<string>(''); // empty = all
  let layout = $state<'table' | 'cards'>('table');

  $effect(() => {
    if (propertiesProp) {
      properties = propertiesProp;
      return;
    }
    void loadProperties();
  });

  $effect(() => {
    void load();
  });

  // Re-load when typeId/page change.
  // (Filters use explicit "Apply" button to avoid request storm.)

  async function loadProperties() {
    try {
      properties = await listProperties(typeId);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  async function load() {
    loading = true;
    error = '';
    try {
      const activeFilters = filters.filter((f) => f.name && f.value !== '');
      if (activeFilters.length === 0) {
        const response = await listObjects(typeId, { page, per_page: pageSize });
        objects = applyFacet(response.data ?? []);
        total = response.total ?? objects.length;
      } else {
        const equals: Record<string, unknown> = {};
        for (const filter of activeFilters) equals[filter.name] = filter.value;
        const response = await queryObjects(typeId, { equals, limit: pageSize });
        objects = applyFacet(response.data ?? []);
        total = response.total ?? objects.length;
      }
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  function applyFacet(rows: ObjectInstance[]): ObjectInstance[] {
    if (!markingFacet) return rows;
    return rows.filter((row) => (row.marking ?? 'public') === markingFacet);
  }

  const markingFacets = $derived(
    Array.from(
      objects.reduce((acc, obj) => {
        const key = obj.marking ?? 'public';
        acc.set(key, (acc.get(key) ?? 0) + 1);
        return acc;
      }, new Map<string, number>()),
    ).sort((a, b) => b[1] - a[1]),
  );

  const visibleColumns = $derived(properties.slice(0, 6));
  const totalPages = $derived(Math.max(1, Math.ceil(total / pageSize)));

  function addFilter() {
    filters = [...filters, { name: properties[0]?.name ?? '', value: '' }];
  }
  function removeFilter(index: number) {
    filters = filters.filter((_, i) => i !== index);
  }
</script>

<section class="explorer" aria-busy={loading}>
  <header>
    <h2>Objects ({total})</h2>
    <div class="layout-toggle">
      <button type="button" class:active={layout === 'table'} onclick={() => (layout = 'table')}>Table</button>
      <button type="button" class:active={layout === 'cards'} onclick={() => (layout = 'cards')}>Cards</button>
    </div>
  </header>

  {#if error}<p class="error" role="alert">{error}</p>{/if}

  <section class="filters">
    <div class="filter-row">
      <strong>Filters</strong>
      <button type="button" onclick={addFilter}>+ filter</button>
      <button type="button" onclick={() => void load()}>Apply</button>
    </div>
    {#each filters as filter, index}
      <div class="filter">
        <select bind:value={filter.name}>
          {#each properties as property (property.id)}
            <option value={property.name}>{property.display_name || property.name}</option>
          {/each}
        </select>
        <input bind:value={filter.value} placeholder="value (= equality)" />
        <button type="button" onclick={() => removeFilter(index)}>×</button>
      </div>
    {/each}
  </section>

  {#if markingFacets.length > 1}
    <section class="facets">
      <strong>Marking</strong>
      <button type="button" class:active={!markingFacet} onclick={() => { markingFacet = ''; void load(); }}>
        all ({total})
      </button>
      {#each markingFacets as [marking, count]}
        <button
          type="button"
          class:active={markingFacet === marking}
          onclick={() => { markingFacet = marking; void load(); }}
        >
          {marking} ({count})
        </button>
      {/each}
    </section>
  {/if}

  {#if layout === 'table'}
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>id</th>
            {#each visibleColumns as col (col.id)}
              <th>{col.display_name || col.name}</th>
            {/each}
            <th>marking</th>
          </tr>
        </thead>
        <tbody>
          {#each objects as object (object.id)}
            <tr onclick={() => onselect?.(object)}>
              <td class="mono">{object.id.slice(0, 8)}…</td>
              {#each visibleColumns as col (col.id)}
                <td>{formatCell(object.properties?.[col.name])}</td>
              {/each}
              <td><span class="marking" data-marking={object.marking ?? 'public'}>{object.marking ?? 'public'}</span></td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {:else}
    <div class="cards">
      {#each objects as object (object.id)}
        <ObjectCard
          {object}
          {properties}
          onclick={() => onselect?.(object)}
        />
      {/each}
    </div>
  {/if}

  <footer class="pager">
    <button type="button" disabled={page <= 1 || loading} onclick={() => { page -= 1; void load(); }}>
      ‹ prev
    </button>
    <span>Page {page} / {totalPages}</span>
    <button
      type="button"
      disabled={page >= totalPages || loading}
      onclick={() => { page += 1; void load(); }}
    >
      next ›
    </button>
  </footer>
</section>

<script lang="ts" module>
  function formatCell(value: unknown): string {
    if (value === null || value === undefined) return '—';
    if (typeof value === 'string') return value;
    if (typeof value === 'number' || typeof value === 'boolean') return String(value);
    return JSON.stringify(value);
  }
</script>

<style>
  .explorer { display: flex; flex-direction: column; gap: 0.75rem; }
  header { display: flex; justify-content: space-between; align-items: center; }
  .layout-toggle button { background: #1e293b; }
  .layout-toggle button.active { background: #2563eb; }
  .error { color: #fca5a5; }
  .filters, .facets { display: flex; flex-wrap: wrap; gap: 0.5rem; align-items: center; }
  .filter { display: flex; gap: 0.25rem; align-items: center; }
  .facets button.active { background: #2563eb; color: white; }
  .table-wrap { overflow: auto; max-height: 60vh; border: 1px solid #1e293b; border-radius: 6px; }
  table { width: 100%; border-collapse: collapse; }
  th, td { padding: 0.4rem 0.6rem; text-align: left; border-bottom: 1px solid #1e293b; font-size: 0.85rem; }
  tbody tr { cursor: pointer; }
  tbody tr:hover { background: #1e293b; }
  .mono { font-family: monospace; color: #94a3b8; }
  .marking { padding: 0.1rem 0.35rem; border-radius: 3px; font-size: 0.7rem; }
  .marking[data-marking='public'] { background: #064e3b; color: #6ee7b7; }
  .marking[data-marking='confidential'] { background: #7c2d12; color: #fdba74; }
  .marking[data-marking='pii'] { background: #831843; color: #f9a8d4; }
  .cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 0.75rem; }
  .pager { display: flex; gap: 0.75rem; align-items: center; justify-content: center; }
</style>
