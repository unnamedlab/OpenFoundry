<script lang="ts">
  /**
   * Bulk-register dialog. Foundry doc anchor: § "Bulk registration"
   * (img_004). The component shows the same browser tree on the
   * left + an editable list of staged entries on the right; submit
   * fans out to `POST /v1/sources/{rid}/virtual-tables/bulk-register`
   * which re-uses the per-row registration path on the server.
   */
  import { virtualTables, type DiscoveredEntry, type RegisterVirtualTableRequest, type TableType, type VirtualTableProvider } from '$lib/api/virtual-tables';
  import RemoteCatalogBrowser from './RemoteCatalogBrowser.svelte';

  type StagedEntry = {
    entry: DiscoveredEntry;
    name: string;
    parentFolderRid: string;
    tableType: TableType;
  };

  type Props = {
    open: boolean;
    sourceRid: string;
    provider: VirtualTableProvider;
    onClose: () => void;
    onCompleted?: (registered: number, failed: number) => void;
  };

  let { open, sourceRid, provider, onClose, onCompleted }: Props = $props();

  let projectRid = $state('');
  let staged = $state<StagedEntry[]>([]);
  let busy = $state(false);
  let progress = $state<{ done: number; total: number } | null>(null);
  let error = $state('');
  let errors = $state<Array<{ name: string; error: string }>>([]);

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

  function stageEntry(entry: DiscoveredEntry) {
    if (!entry.registrable) return;
    if (staged.some((s) => s.entry.path === entry.path)) return;
    staged = [
      ...staged,
      {
        entry,
        name: entry.display_name,
        parentFolderRid: '',
        tableType: entry.inferred_table_type ?? 'OTHER',
      },
    ];
  }

  function unstage(index: number) {
    staged = staged.filter((_, i) => i !== index);
  }

  function deriveLocator(entry: DiscoveredEntry): RegisterVirtualTableRequest['locator'] {
    const parts = entry.path.split('/').filter(Boolean);
    if (provider === 'AMAZON_S3' || provider === 'AZURE_ABFS' || provider === 'GCS') {
      return {
        kind: 'file',
        bucket: parts[0] ?? entry.path,
        prefix: parts.slice(1).join('/'),
        format: 'parquet',
      };
    }
    return {
      kind: 'tabular',
      database: parts[0] ?? '',
      schema: parts[1] ?? '',
      table: parts[parts.length - 1] ?? entry.display_name,
    };
  }

  async function submit() {
    if (!projectRid.trim()) {
      error = 'Project rid is required';
      return;
    }
    if (staged.length === 0) {
      error = 'Pick at least one entry from the catalog tree';
      return;
    }
    busy = true;
    error = '';
    errors = [];
    progress = { done: 0, total: staged.length };
    try {
      const response = await virtualTables.bulkRegister(sourceRid, {
        project_rid: projectRid.trim(),
        entries: staged.map((s) => ({
          project_rid: projectRid.trim(),
          parent_folder_rid: s.parentFolderRid.trim() || undefined,
          name: s.name.trim() || undefined,
          locator: deriveLocator(s.entry),
          table_type: s.tableType,
        })),
      });
      progress = { done: staged.length, total: staged.length };
      errors = response.errors;
      onCompleted?.(response.registered.length, response.errors.length);
      if (response.errors.length === 0) {
        onClose();
      }
    } catch (err) {
      error = err instanceof Error ? err.message : 'Bulk register failed';
    } finally {
      busy = false;
    }
  }
</script>

{#if open}
  <div class="backdrop" role="dialog" aria-modal="true" data-testid="vt-bulk-modal">
    <div class="modal">
      <header>
        <h3>Bulk register virtual tables</h3>
        <p class="muted">
          Pick entries from the source catalog on the left, edit names /
          folders on the right, then register in one batch.
        </p>
      </header>

      <label class="project">
        <span>Target project rid</span>
        <input type="text" bind:value={projectRid} placeholder="ri.foundry.main.project..." data-testid="vt-bulk-project" />
      </label>

      <div class="split">
        <div class="left">
          <RemoteCatalogBrowser sourceRid={sourceRid} onSelect={stageEntry} />
        </div>
        <div class="right" data-testid="vt-bulk-staged">
          {#if staged.length === 0}
            <div class="muted">No entries staged yet. Click an entry on the left.</div>
          {:else}
            <table>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Parent folder</th>
                  <th>Type</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {#each staged as row, i (row.entry.path)}
                  <tr>
                    <td>
                      <input type="text" bind:value={row.name} data-testid="vt-bulk-row-name" />
                      <div class="path mono">{row.entry.path}</div>
                    </td>
                    <td>
                      <input type="text" bind:value={row.parentFolderRid} placeholder="(default)" />
                    </td>
                    <td>
                      <select bind:value={row.tableType}>
                        {#each TABLE_TYPES as type (type)}
                          <option value={type}>{type}</option>
                        {/each}
                      </select>
                    </td>
                    <td>
                      <button type="button" onclick={() => unstage(i)} aria-label="Remove">
                        ✕
                      </button>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
        </div>
      </div>

      {#if progress && busy}
        <div class="progress">
          Registering {progress.done} / {progress.total}…
        </div>
      {/if}

      {#if errors.length > 0}
        <div class="error" role="alert" data-testid="vt-bulk-errors">
          <p>{errors.length} entries failed:</p>
          <ul>
            {#each errors as e (e.name)}
              <li><strong>{e.name}</strong>: {e.error}</li>
            {/each}
          </ul>
        </div>
      {:else if error}
        <div class="error" role="alert">{error}</div>
      {/if}

      <footer>
        <button type="button" onclick={onClose} disabled={busy}>Close</button>
        <button
          type="button"
          class="primary"
          onclick={submit}
          disabled={busy || staged.length === 0}
          data-testid="vt-bulk-submit"
        >
          {busy ? 'Registering…' : `Register ${staged.length}`}
        </button>
      </footer>
    </div>
  </div>
{/if}

<style>
  .backdrop {
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
    width: min(960px, 96vw);
    max-height: 92vh;
    overflow: auto;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  .modal h3 {
    margin: 0;
  }
  .muted {
    color: var(--color-fg-muted, #6b7280);
    font-size: 0.75rem;
  }
  .project {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    font-size: 0.75rem;
  }
  .project input {
    padding: 0.375rem 0.5rem;
    border: 1px solid var(--color-border, #d1d5db);
    border-radius: 0.25rem;
    font-size: 0.875rem;
  }
  .split {
    display: grid;
    grid-template-columns: minmax(260px, 1fr) minmax(360px, 2fr);
    gap: 0.75rem;
  }
  .right {
    border: 1px solid var(--color-border, #e5e7eb);
    border-radius: 0.5rem;
    padding: 0.5rem;
    background: var(--color-bg-elevated, #fff);
    overflow: auto;
    max-height: 520px;
  }
  table {
    width: 100%;
    border-collapse: collapse;
  }
  th,
  td {
    text-align: left;
    padding: 0.25rem 0.5rem;
    border-bottom: 1px solid var(--color-border, #e5e7eb);
    font-size: 0.875rem;
    vertical-align: top;
  }
  td input,
  td select {
    width: 100%;
    padding: 0.25rem 0.375rem;
    border: 1px solid var(--color-border, #d1d5db);
    border-radius: 0.25rem;
    font-size: 0.75rem;
  }
  .path {
    color: var(--color-fg-muted, #6b7280);
    font-size: 0.625rem;
    font-family: ui-monospace, SFMono-Regular, monospace;
  }
  td button {
    border: none;
    background: transparent;
    cursor: pointer;
    color: #b91c1c;
  }
  .progress,
  .error {
    border: 1px solid var(--color-border, #e5e7eb);
    border-radius: 0.25rem;
    padding: 0.5rem 0.75rem;
    font-size: 0.875rem;
  }
  .error {
    background: #fef2f2;
    border-color: #fecaca;
    color: #b91c1c;
  }
  .error ul {
    margin: 0.25rem 0 0;
    padding-left: 1.25rem;
    font-size: 0.75rem;
  }
  footer {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
  }
  footer button {
    padding: 0.375rem 0.75rem;
    border: 1px solid var(--color-border, #d1d5db);
    border-radius: 0.25rem;
    background: var(--color-bg-elevated, #fff);
    cursor: pointer;
    font-size: 0.875rem;
  }
  footer button.primary {
    background: #1d4ed8;
    color: #fff;
    border-color: #1d4ed8;
  }
</style>
