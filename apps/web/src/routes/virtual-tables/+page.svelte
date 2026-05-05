<script lang="ts">
  /**
   * `/virtual-tables` — global list view.
   *
   * Foundry doc anchor: `Data connectivity & integration/Core concepts/
   * Virtual tables.md` § "Set up a connection for a virtual table". This
   * page is the platform-wide entry point; the per-source tab lives in
   * `/data-connection/sources/[id]` (cross-deeplinked via the badge in
   * the source page header).
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';

  import {
    capabilityChips,
    providerLabel,
    tableTypeLabel,
    virtualTables,
    type Capabilities,
    type TableType,
    type VirtualTable,
    type VirtualTableProvider,
  } from '$lib/api/virtual-tables';

  let items = $state<VirtualTable[]>([]);
  let loading = $state(true);
  let error = $state('');
  let nextCursor = $state<string | null>(null);

  // URL-synced filters. Each filter writes back to the query string so a
  // shared link reproduces the exact view.
  let projectFilter = $state('');
  let sourceFilter = $state('');
  let nameFilter = $state('');
  let typeFilter = $state<TableType | ''>('');
  let writableOnly = $state(false);
  let updateDetectionOnly = $state(false);

  const TABLE_TYPES: TableType[] = [
    'TABLE',
    'VIEW',
    'MATERIALIZED_VIEW',
    'EXTERNAL_DELTA',
    'MANAGED_DELTA',
    'MANAGED_ICEBERG',
    'PARQUET_FILES',
    'AVRO_FILES',
    'CSV_FILES',
    'OTHER',
  ];

  function readFiltersFromUrl(url: URL) {
    projectFilter = url.searchParams.get('project') ?? '';
    sourceFilter = url.searchParams.get('source') ?? '';
    nameFilter = url.searchParams.get('name') ?? '';
    typeFilter = (url.searchParams.get('type') as TableType | '') ?? '';
    writableOnly = url.searchParams.get('writable') === '1';
    updateDetectionOnly = url.searchParams.get('updates') === '1';
  }

  function syncFiltersToUrl() {
    const params = new URLSearchParams();
    if (projectFilter) params.set('project', projectFilter);
    if (sourceFilter) params.set('source', sourceFilter);
    if (nameFilter) params.set('name', nameFilter);
    if (typeFilter) params.set('type', typeFilter);
    if (writableOnly) params.set('writable', '1');
    if (updateDetectionOnly) params.set('updates', '1');
    const qs = params.toString();
    void goto(`/virtual-tables${qs ? `?${qs}` : ''}`, { keepFocus: true, replaceState: true });
  }

  async function refresh() {
    loading = true;
    error = '';
    try {
      const response = await virtualTables.listVirtualTables({
        project: projectFilter || undefined,
        source: sourceFilter || undefined,
        name: nameFilter || undefined,
        type: (typeFilter || undefined) as TableType | undefined,
        limit: 100,
      });
      // Client-side filters for capability flags — the backend does
      // not yet support them, but the doc asks the UI to expose them.
      items = response.items.filter((row) => {
        if (writableOnly && !row.capabilities?.write) return false;
        if (updateDetectionOnly && !row.update_detection_enabled) return false;
        return true;
      });
      nextCursor = response.next_cursor;
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to load virtual tables';
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    readFiltersFromUrl($page.url);
    void refresh();
  });

  function applyFilters() {
    syncFiltersToUrl();
    void refresh();
  }

  function provider(row: VirtualTable): VirtualTableProvider | null {
    // The provider isn't on the row directly — derive from the
    // capabilities matrix slot. P5 enriches the API to surface this
    // explicitly so the badge can avoid the heuristic; for now we
    // pull it off the source link metadata if the backend embedded it
    // in `properties`.
    const fromProperties = (row.properties?.provider as VirtualTableProvider | undefined) ?? null;
    return fromProperties;
  }

  function chips(caps: Capabilities | undefined): string[] {
    return caps ? capabilityChips(caps) : [];
  }
</script>

<svelte:head>
  <title>Virtual tables · OpenFoundry</title>
</svelte:head>

<section class="virtual-tables-page" data-testid="virtual-tables-page">
  <header class="page-header">
    <div class="title-row">
      <h1>Virtual tables</h1>
      <a class="link-secondary" href="/data-connection">→ Configure on a source</a>
    </div>
    <p class="lede">
      Pointers to tables in supported data platforms (BigQuery, Snowflake,
      Databricks, S3 / GCS / ADLS, Foundry Iceberg). Foundry queries the
      source on demand — no copy.
    </p>
  </header>

  <div class="filters" data-testid="virtual-tables-filters">
    <input
      type="text"
      placeholder="Filter by project rid"
      bind:value={projectFilter}
      onkeydown={(e) => e.key === 'Enter' && applyFilters()}
      data-testid="vt-filter-project"
    />
    <input
      type="text"
      placeholder="Filter by source rid"
      bind:value={sourceFilter}
      onkeydown={(e) => e.key === 'Enter' && applyFilters()}
      data-testid="vt-filter-source"
    />
    <input
      type="text"
      placeholder="Filter by name"
      bind:value={nameFilter}
      onkeydown={(e) => e.key === 'Enter' && applyFilters()}
      data-testid="vt-filter-name"
    />
    <select bind:value={typeFilter} onchange={applyFilters} data-testid="vt-filter-type">
      <option value="">All table types</option>
      {#each TABLE_TYPES as type (type)}
        <option value={type}>{tableTypeLabel(type)}</option>
      {/each}
    </select>
    <label class="toggle">
      <input type="checkbox" bind:checked={writableOnly} onchange={applyFilters} />
      Writable only
    </label>
    <label class="toggle">
      <input
        type="checkbox"
        bind:checked={updateDetectionOnly}
        onchange={applyFilters}
        data-testid="vt-filter-updates"
      />
      Update detection on
    </label>
    <button type="button" onclick={applyFilters}>Apply</button>
  </div>

  {#if loading}
    <div class="loading">Loading…</div>
  {:else if error}
    <div class="error" role="alert" data-testid="vt-error">{error}</div>
  {:else if items.length === 0}
    <div class="empty" data-testid="vt-empty">
      <p>No virtual tables yet.</p>
      <p class="muted">
        Open a Data Connection source from a supported provider (BigQuery,
        Snowflake, Databricks, S3 / GCS / ADLS) and use the
        <strong>Virtual tables</strong> tab to register one.
      </p>
      <a class="cta" href="/data-connection">Go to Data Connection sources →</a>
    </div>
  {:else}
    <table class="vt-grid" data-testid="virtual-tables-grid">
      <thead>
        <tr>
          <th>Name</th>
          <th>Source</th>
          <th>Provider</th>
          <th>Type</th>
          <th>Capabilities</th>
          <th>Project</th>
          <th>Markings</th>
          <th>Created</th>
        </tr>
      </thead>
      <tbody>
        {#each items as row (row.rid)}
          <tr>
            <td>
              <a href={`/virtual-tables/${encodeURIComponent(row.rid)}`} data-testid="vt-row-link">
                {row.name}
              </a>
            </td>
            <td>
              <a
                href={`/data-connection/sources/${encodeURIComponent(row.source_rid)}`}
                class="mono small"
                title={row.source_rid}
              >
                {row.source_rid}
              </a>
            </td>
            <td>
              {#if provider(row)}
                <span class="badge">{providerLabel(provider(row)!)}</span>
              {:else}
                <span class="muted">—</span>
              {/if}
            </td>
            <td>{tableTypeLabel(row.table_type)}</td>
            <td>
              <div class="chips">
                {#each chips(row.capabilities) as chip (chip)}
                  <span class="chip" data-testid="vt-cap-chip">{chip}</span>
                {/each}
                {#if row.update_detection_enabled}
                  <span class="chip update">Update detection</span>
                {/if}
              </div>
            </td>
            <td class="mono small">{row.project_rid}</td>
            <td>
              {#each row.markings as marking (marking)}
                <span class="marking">{marking}</span>
              {/each}
            </td>
            <td class="small">{new Date(row.created_at).toLocaleDateString()}</td>
          </tr>
        {/each}
      </tbody>
    </table>
    {#if nextCursor}
      <div class="next">
        <span class="muted">More results available — refine filters to load.</span>
      </div>
    {/if}
  {/if}
</section>

<style>
  .virtual-tables-page {
    padding: 1.5rem;
    display: flex;
    flex-direction: column;
    gap: 1.5rem;
  }
  .page-header h1 {
    display: inline-block;
    margin: 0;
    font-size: 1.5rem;
  }
  .title-row {
    display: flex;
    align-items: baseline;
    gap: 1rem;
  }
  .link-secondary {
    font-size: 0.875rem;
    color: var(--color-fg-muted, #4b5563);
  }
  .lede {
    color: var(--color-fg-muted, #4b5563);
    margin: 0.25rem 0 0;
  }
  .filters {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
    align-items: center;
  }
  .filters input,
  .filters select,
  .filters button {
    padding: 0.375rem 0.5rem;
    font-size: 0.875rem;
    border: 1px solid var(--color-border, #d1d5db);
    border-radius: 0.25rem;
    background: var(--color-bg-elevated, #fff);
  }
  .toggle {
    font-size: 0.875rem;
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
  }
  .vt-grid {
    width: 100%;
    border-collapse: collapse;
  }
  .vt-grid th,
  .vt-grid td {
    text-align: left;
    padding: 0.5rem 0.75rem;
    border-bottom: 1px solid var(--color-border, #e5e7eb);
    font-size: 0.875rem;
    vertical-align: top;
  }
  .vt-grid th {
    background: var(--color-bg-subtle, #f9fafb);
    font-weight: 600;
  }
  .badge {
    display: inline-block;
    padding: 0.125rem 0.5rem;
    border-radius: 0.25rem;
    font-size: 0.75rem;
    background: #ecfeff;
    color: #155e75;
    border: 1px solid #a5f3fc;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
  }
  .chip {
    display: inline-block;
    padding: 0.0625rem 0.375rem;
    font-size: 0.75rem;
    border-radius: 0.25rem;
    background: var(--color-bg-subtle, #f3f4f6);
    border: 1px solid var(--color-border, #e5e7eb);
  }
  .chip.update {
    background: #fef9c3;
    border-color: #fde047;
    color: #854d0e;
  }
  .mono {
    font-family: ui-monospace, SFMono-Regular, monospace;
  }
  .small {
    font-size: 0.75rem;
  }
  .marking {
    display: inline-block;
    padding: 0.125rem 0.375rem;
    margin-right: 0.25rem;
    border-radius: 0.25rem;
    background: var(--color-bg-subtle, #f3f4f6);
    border: 1px solid var(--color-border, #e5e7eb);
    font-size: 0.75rem;
  }
  .muted {
    color: var(--color-fg-muted, #6b7280);
  }
  .empty,
  .loading,
  .error {
    padding: 1rem;
    border-radius: 0.375rem;
    background: var(--color-bg-subtle, #f9fafb);
    color: var(--color-fg-muted, #4b5563);
  }
  .empty {
    text-align: center;
  }
  .empty .cta {
    display: inline-block;
    margin-top: 0.5rem;
    color: #1d4ed8;
  }
  .error {
    color: #b91c1c;
    background: #fef2f2;
  }
  .next {
    text-align: center;
    padding: 0.5rem 0;
  }
</style>
