<script lang="ts">
  /**
   * `/iceberg-tables` — list view for Foundry Iceberg tables [Beta].
   *
   * Mirrors the Foundry "Iceberg tables" core-concept doc:
   * `docs_original_palantir_foundry/.../Iceberg tables.md`.
   *
   * The data is served by `iceberg-catalog-service` admin endpoints
   * (`/api/v1/iceberg-tables`). External Iceberg clients hit a
   * separate spec-conformant prefix (`/iceberg/v1/...`).
   *
   * Columns (per closing-task spec § 6):
   *   namespace, name, format_version, row_count_estimate,
   *   last_snapshot_at, location, markings.
   */
  import { onMount } from 'svelte';

  import {
    listIcebergTables,
    type IcebergTableSummary,
  } from '$lib/api/icebergTables';

  let tables = $state<IcebergTableSummary[]>([]);
  let loading = $state(true);
  let error = $state('');

  let projectFilter = $state('');
  let namespaceFilter = $state('');
  let nameFilter = $state('');
  let sortField = $state<'name' | 'created_at' | ''>('');

  async function refresh() {
    loading = true;
    error = '';
    try {
      const response = await listIcebergTables({
        project_rid: projectFilter || undefined,
        namespace: namespaceFilter || undefined,
        name: nameFilter || undefined,
        sort: sortField || undefined,
      });
      tables = response.tables;
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to load Iceberg tables';
    } finally {
      loading = false;
    }
  }

  onMount(refresh);

  function formatNamespace(parts: string[]) {
    return parts.length ? parts.join('.') : '(root)';
  }

  function formatRowCount(value: number | null): string {
    if (value === null) return '—';
    if (value > 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
    if (value > 1_000) return `${(value / 1_000).toFixed(1)}K`;
    return value.toString();
  }

  function formatRelativeTime(iso: string | null): string {
    if (!iso) return '—';
    const ts = new Date(iso).getTime();
    const diff = Date.now() - ts;
    const minutes = Math.floor(diff / 60_000);
    if (minutes < 1) return 'just now';
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    return `${Math.floor(hours / 24)}d ago`;
  }
</script>

<svelte:head>
  <title>Iceberg tables · OpenFoundry</title>
</svelte:head>

<section class="iceberg-tables-page">
  <header class="page-header">
    <div class="title-row">
      <h1>Iceberg tables</h1>
      <span class="beta-badge" data-testid="iceberg-beta-banner">Beta</span>
    </div>
    <p class="lede">
      Apache Iceberg tables exposed via Foundry's REST Catalog. External clients
      (PyIceberg, Spark, Trino, Snowflake) authenticate via OAuth2 or bearer
      tokens and read straight from object storage.
    </p>
  </header>

  <div class="filters">
    <input
      type="text"
      placeholder="Filter by project rid"
      bind:value={projectFilter}
      onkeydown={(e) => e.key === 'Enter' && refresh()}
      data-testid="iceberg-filter-project"
    />
    <input
      type="text"
      placeholder="Filter by namespace"
      bind:value={namespaceFilter}
      onkeydown={(e) => e.key === 'Enter' && refresh()}
      data-testid="iceberg-filter-namespace"
    />
    <input
      type="text"
      placeholder="Filter by name"
      bind:value={nameFilter}
      onkeydown={(e) => e.key === 'Enter' && refresh()}
      data-testid="iceberg-filter-name"
    />
    <select bind:value={sortField} onchange={refresh} data-testid="iceberg-sort">
      <option value="">Sort: most recent</option>
      <option value="name">Sort: name</option>
      <option value="created_at">Sort: created at</option>
    </select>
    <button type="button" onclick={refresh}>Apply</button>
  </div>

  {#if loading}
    <div class="loading">Loading…</div>
  {:else if error}
    <div class="error" role="alert">{error}</div>
  {:else if tables.length === 0}
    <div class="empty">
      <p>No Iceberg tables match the current filters.</p>
    </div>
  {:else}
    <table class="iceberg-table-grid" data-testid="iceberg-tables-grid">
      <thead>
        <tr>
          <th>Namespace</th>
          <th>Name</th>
          <th>Format</th>
          <th>Rows (est.)</th>
          <th>Last snapshot</th>
          <th>Location</th>
          <th>Markings</th>
        </tr>
      </thead>
      <tbody>
        {#each tables as table (table.id)}
          <tr>
            <td>{formatNamespace(table.namespace)}</td>
            <td>
              <a href={`/iceberg-tables/${table.id}`} data-testid="iceberg-table-link">
                {table.name}
              </a>
            </td>
            <td>v{table.format_version}</td>
            <td>{formatRowCount(table.row_count_estimate)}</td>
            <td>{formatRelativeTime(table.last_snapshot_at)}</td>
            <td class="mono" title={table.location}>{table.location}</td>
            <td>
              {#each table.markings as marking}
                <span class="marking">{marking}</span>
              {/each}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</section>

<style>
  .iceberg-tables-page {
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
    align-items: center;
    gap: 0.5rem;
  }
  .beta-badge {
    display: inline-block;
    padding: 0.125rem 0.5rem;
    border-radius: 0.25rem;
    font-size: 0.75rem;
    font-weight: 600;
    color: #92400e;
    background: #fef3c7;
    border: 1px solid #fde68a;
  }
  .lede {
    color: var(--color-fg-muted, #4b5563);
    margin: 0.25rem 0 0;
  }
  .filters {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
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
  .iceberg-table-grid {
    width: 100%;
    border-collapse: collapse;
  }
  .iceberg-table-grid th,
  .iceberg-table-grid td {
    text-align: left;
    padding: 0.5rem 0.75rem;
    border-bottom: 1px solid var(--color-border, #e5e7eb);
    font-size: 0.875rem;
  }
  .iceberg-table-grid th {
    background: var(--color-bg-subtle, #f9fafb);
    font-weight: 600;
  }
  .mono {
    font-family: ui-monospace, SFMono-Regular, monospace;
    max-width: 28ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
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
  .empty,
  .loading,
  .error {
    padding: 1rem;
    border-radius: 0.375rem;
    background: var(--color-bg-subtle, #f9fafb);
    color: var(--color-fg-muted, #4b5563);
  }
  .error {
    color: #b91c1c;
    background: #fef2f2;
  }
</style>
