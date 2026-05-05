<script lang="ts">
  /**
   * `/virtual-tables/[rid]` — detail view.
   *
   * Foundry doc anchor: `Virtual tables.md` § "Viewing virtual table
   * details" + § "Update detection for virtual table inputs". The
   * Pipeline Builder / Contour deep-link buttons are stubs (P5 + P6
   * activate them); the inspector renders the published shape today.
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';

  import {
    capabilityChips,
    providerLabel,
    tableTypeLabel,
    virtualTables,
    type VirtualTable,
    type VirtualTableProvider,
  } from '$lib/api/virtual-tables';
  import VirtualTableDetailsPanel from '$lib/components/data-connection/VirtualTableDetailsPanel.svelte';

  const rid = $derived(decodeURIComponent($page.params.rid ?? ''));

  type Tab =
    | 'overview'
    | 'schema'
    | 'lineage'
    | 'permissions'
    | 'activity'
    | 'update-detection'
    | 'imports';

  let activeTab = $state<Tab>('overview');
  let row = $state<VirtualTable | null>(null);
  let loading = $state(true);
  let error = $state('');
  let busy = $state<'refresh' | 'delete' | null>(null);
  let confirmingDelete = $state(false);

  async function load() {
    loading = true;
    error = '';
    try {
      row = await virtualTables.getVirtualTable(rid);
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to load virtual table';
    } finally {
      loading = false;
    }
  }

  onMount(load);

  async function refreshSchema() {
    if (!row) return;
    busy = 'refresh';
    try {
      row = await virtualTables.refreshSchema(row.rid);
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to refresh schema';
    } finally {
      busy = null;
    }
  }

  async function confirmDelete() {
    if (!row) return;
    busy = 'delete';
    try {
      await virtualTables.deleteVirtualTable(row.rid);
      await goto('/virtual-tables');
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to delete virtual table';
    } finally {
      busy = null;
      confirmingDelete = false;
    }
  }

  function provider(): VirtualTableProvider | null {
    return ((row?.properties?.provider as VirtualTableProvider | undefined) ?? null);
  }

  const tabs: Array<{ id: Tab; label: string; deferred?: boolean }> = [
    { id: 'overview', label: 'Overview' },
    { id: 'schema', label: 'Schema' },
    { id: 'lineage', label: 'Lineage', deferred: true },
    { id: 'permissions', label: 'Permissions' },
    { id: 'activity', label: 'Activity', deferred: true },
    { id: 'update-detection', label: 'Update detection' },
    { id: 'imports', label: 'Imports', deferred: true },
  ];
</script>

<svelte:head>
  <title>{row?.name ?? 'Virtual table'} · OpenFoundry</title>
</svelte:head>

<section class="vt-detail" data-testid="vt-detail-page">
  {#if loading}
    <div class="loading">Loading…</div>
  {:else if error}
    <div class="error" role="alert" data-testid="vt-detail-error">{error}</div>
  {:else if row}
    <header class="page-header">
      <div class="title-row">
        <a class="back" href="/virtual-tables">← All virtual tables</a>
      </div>
      <div class="title-row">
        <h1>{row.name}</h1>
        <span class="badge primary">Virtual table</span>
        {#if provider()}
          <span class="badge">{providerLabel(provider()!)}</span>
        {/if}
        <span class="badge muted">{tableTypeLabel(row.table_type)}</span>
        {#each capabilityChips(row.capabilities) as chip (chip)}
          <span class="chip" data-testid="vt-detail-cap-chip">{chip}</span>
        {/each}
      </div>
      <div class="actions">
        <button
          type="button"
          onclick={refreshSchema}
          disabled={busy !== null}
          data-testid="vt-action-refresh-schema"
        >
          {busy === 'refresh' ? 'Refreshing…' : 'Refresh schema'}
        </button>
        <button type="button" disabled title="Activated in P5">Open in Pipeline Builder</button>
        <button type="button" disabled title="Activated in P6">Open in Contour</button>
        <button
          type="button"
          class="danger"
          disabled={busy !== null}
          onclick={() => (confirmingDelete = true)}
          data-testid="vt-action-delete"
        >
          Delete
        </button>
      </div>
    </header>

    <nav class="tabs" data-testid="vt-detail-tabs">
      {#each tabs as tab (tab.id)}
        <button
          type="button"
          class={tab.id === activeTab ? 'tab active' : 'tab'}
          onclick={() => (activeTab = tab.id)}
          data-testid={`vt-tab-${tab.id}`}
        >
          {tab.label}
          {#if tab.deferred}
            <span class="defer">soon</span>
          {/if}
        </button>
      {/each}
    </nav>

    <div class="tab-content">
      {#if activeTab === 'overview'}
        <section class="grid">
          <article class="card">
            <h3>Source</h3>
            <a
              href={`/data-connection/sources/${encodeURIComponent(row.source_rid)}`}
              class="mono"
              data-testid="vt-overview-source-link"
            >
              {row.source_rid}
            </a>
          </article>
          <article class="card">
            <h3>Project</h3>
            <span class="mono" data-testid="vt-overview-project">{row.project_rid}</span>
          </article>
          <article class="card">
            <h3>Locator</h3>
            <pre class="json" data-testid="vt-overview-locator">{JSON.stringify(
                row.locator,
                null,
                2,
              )}</pre>
          </article>
          <article class="card">
            <h3>Capabilities</h3>
            <ul class="kv">
              <li>Read: {row.capabilities.read ? 'yes' : 'no'}</li>
              <li>Write: {row.capabilities.write ? 'yes' : 'no'}</li>
              <li>Incremental: {row.capabilities.incremental ? 'yes' : 'no'}</li>
              <li>Versioning: {row.capabilities.versioning ? 'yes' : 'no'}</li>
              <li>Compute pushdown: {row.capabilities.compute_pushdown ?? '—'}</li>
              <li>Foundry compute (Python single-node): {row.capabilities.foundry_compute.python_single_node ? 'yes' : 'no'}</li>
              <li>Foundry compute (Python Spark): {row.capabilities.foundry_compute.python_spark ? 'yes' : 'no'}</li>
              <li>Foundry compute (PB Spark): {row.capabilities.foundry_compute.pipeline_builder_spark ? 'yes' : 'no'}</li>
            </ul>
          </article>
          <article class="card">
            <h3>Update detection</h3>
            <ul class="kv">
              <li>Enabled: {row.update_detection_enabled ? 'yes' : 'no'}</li>
              <li>
                Interval:
                {row.update_detection_interval_seconds ?? '—'}
                {row.update_detection_interval_seconds ? 's' : ''}
              </li>
              <li>Last polled at: {row.last_polled_at ?? '—'}</li>
              <li>Last observed version: {row.last_observed_version ?? '—'}</li>
            </ul>
          </article>
        </section>
      {:else if activeTab === 'schema'}
        <section>
          {#if row.schema_inferred.length === 0}
            <p class="muted">
              Schema inference returned no columns. Refresh the schema once
              the source registration completes, or check
              <code>properties.warnings</code> for upstream messages.
            </p>
          {:else}
            <table class="schema-grid" data-testid="vt-schema-grid">
              <thead>
                <tr>
                  <th>Column</th>
                  <th>Inferred type</th>
                  <th>Source type</th>
                  <th>Nullable</th>
                </tr>
              </thead>
              <tbody>
                {#each row.schema_inferred as col (col.name)}
                  <tr>
                    <td class="mono">{col.name}</td>
                    <td>{col.inferred_type}</td>
                    <td class="mono small">{col.source_type}</td>
                    <td>{col.nullable ? 'yes' : 'no'}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
        </section>
      {:else if activeTab === 'permissions'}
        <section>
          <h3>Markings</h3>
          {#if row.markings.length === 0}
            <p class="muted">
              No explicit markings. The virtual table inherits the source's
              markings as a clearance floor (see ADR-NNNN). Update via
              <code>PATCH /v1/virtual-tables/{row.rid}/markings</code>.
            </p>
          {:else}
            <ul class="markings">
              {#each row.markings as marking (marking)}
                <li>{marking}</li>
              {/each}
            </ul>
          {/if}
        </section>
      {:else if activeTab === 'lineage'}
        <p class="muted">Lineage view ships with P6 — pipeline integration.</p>
      {:else if activeTab === 'activity'}
        <p class="muted">
          Audit events for this virtual table are persisted in
          <code>virtual_table_audit</code> and emitted to
          <code>audit-compliance-service</code>. The viewer wires up in P3.next.
        </p>
      {:else if activeTab === 'update-detection'}
        <VirtualTableDetailsPanel
          table={row}
          onChanged={(next) => {
            row = next;
          }}
        />
      {:else if activeTab === 'imports'}
        <p class="muted">
          Imports list ships with P3.next once the
          <code>virtual_table_imports</code> endpoint is exposed.
        </p>
      {/if}
    </div>
  {/if}
</section>

{#if confirmingDelete}
  <div class="modal-backdrop" role="dialog" aria-modal="true">
    <div class="modal">
      <h3>Delete virtual table</h3>
      <p>
        This removes the pointer in Foundry. The remote source table is
        not touched. Imports of this virtual table into other projects
        will be removed in cascade.
      </p>
      <div class="modal-actions">
        <button type="button" onclick={() => (confirmingDelete = false)}>Cancel</button>
        <button
          type="button"
          class="danger"
          onclick={confirmDelete}
          disabled={busy === 'delete'}
          data-testid="vt-confirm-delete"
        >
          {busy === 'delete' ? 'Deleting…' : 'Delete'}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .vt-detail {
    padding: 1.5rem;
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }
  .page-header {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .title-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.5rem;
  }
  .title-row h1 {
    margin: 0;
    font-size: 1.5rem;
  }
  .back {
    color: var(--color-fg-muted, #4b5563);
    font-size: 0.875rem;
  }
  .actions {
    display: flex;
    gap: 0.5rem;
    margin-top: 0.25rem;
  }
  .actions button {
    padding: 0.375rem 0.75rem;
    font-size: 0.875rem;
    border-radius: 0.25rem;
    border: 1px solid var(--color-border, #d1d5db);
    background: var(--color-bg-elevated, #fff);
    cursor: pointer;
  }
  .actions button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .actions .danger {
    color: #b91c1c;
    border-color: #fca5a5;
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
  .badge.primary {
    background: #1d4ed8;
    color: #fff;
    border-color: #1d4ed8;
  }
  .badge.muted {
    background: var(--color-bg-subtle, #f3f4f6);
    color: #4b5563;
    border-color: #e5e7eb;
  }
  .chip {
    display: inline-block;
    padding: 0.0625rem 0.375rem;
    font-size: 0.75rem;
    border-radius: 0.25rem;
    background: var(--color-bg-subtle, #f3f4f6);
    border: 1px solid var(--color-border, #e5e7eb);
  }
  .tabs {
    display: flex;
    gap: 0.25rem;
    border-bottom: 1px solid var(--color-border, #e5e7eb);
  }
  .tab {
    padding: 0.5rem 0.75rem;
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    cursor: pointer;
    font-size: 0.875rem;
    color: var(--color-fg-muted, #4b5563);
  }
  .tab.active {
    color: #1d4ed8;
    border-bottom-color: #1d4ed8;
    font-weight: 600;
  }
  .defer {
    margin-left: 0.25rem;
    font-size: 0.625rem;
    background: #fef9c3;
    color: #854d0e;
    padding: 0 0.25rem;
    border-radius: 0.125rem;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
    gap: 0.75rem;
  }
  .card {
    border: 1px solid var(--color-border, #e5e7eb);
    border-radius: 0.5rem;
    padding: 0.75rem 1rem;
    background: var(--color-bg-elevated, #fff);
  }
  .card h3 {
    margin: 0 0 0.5rem;
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--color-fg-muted, #4b5563);
  }
  .kv {
    list-style: none;
    padding: 0;
    margin: 0;
    font-size: 0.875rem;
  }
  .kv li {
    padding: 0.125rem 0;
  }
  .json {
    font-family: ui-monospace, SFMono-Regular, monospace;
    font-size: 0.75rem;
    background: var(--color-bg-subtle, #f9fafb);
    padding: 0.5rem;
    border-radius: 0.25rem;
    overflow: auto;
  }
  .schema-grid {
    width: 100%;
    border-collapse: collapse;
  }
  .schema-grid th,
  .schema-grid td {
    text-align: left;
    padding: 0.5rem 0.75rem;
    border-bottom: 1px solid var(--color-border, #e5e7eb);
    font-size: 0.875rem;
  }
  .mono {
    font-family: ui-monospace, SFMono-Regular, monospace;
  }
  .small {
    font-size: 0.75rem;
  }
  .markings {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
  }
  .markings li {
    background: var(--color-bg-subtle, #f3f4f6);
    border: 1px solid var(--color-border, #e5e7eb);
    padding: 0.125rem 0.5rem;
    border-radius: 0.25rem;
    font-size: 0.75rem;
  }
  .muted {
    color: var(--color-fg-muted, #6b7280);
  }
  .loading,
  .error {
    padding: 1rem;
    border-radius: 0.375rem;
    background: var(--color-bg-subtle, #f9fafb);
  }
  .error {
    color: #b91c1c;
    background: #fef2f2;
  }
  .modal-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.4);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
  }
  .modal {
    background: var(--color-bg-elevated, #fff);
    border-radius: 0.5rem;
    padding: 1.25rem;
    width: min(420px, 90vw);
  }
  .modal h3 {
    margin: 0 0 0.5rem;
  }
  .modal-actions {
    display: flex;
    gap: 0.5rem;
    justify-content: flex-end;
    margin-top: 0.75rem;
  }
  .modal-actions button {
    padding: 0.375rem 0.75rem;
    font-size: 0.875rem;
    border-radius: 0.25rem;
    border: 1px solid var(--color-border, #d1d5db);
    background: var(--color-bg-elevated, #fff);
    cursor: pointer;
  }
  .modal-actions .danger {
    background: #b91c1c;
    color: #fff;
    border-color: #b91c1c;
  }
</style>
