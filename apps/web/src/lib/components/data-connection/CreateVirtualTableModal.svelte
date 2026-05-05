<script lang="ts">
  /**
   * Form modal opened from the inspector / browser. Foundry doc
   * anchor: § "Create virtual tables" (img_003). Submits to
   * `POST /v1/sources/{rid}/virtual-tables/register` and surfaces
   * the structured 412 errors from the Foundry-worker / egress
   * enforcement (P2) verbatim with the remediation hint.
   */
  import {
    capabilityChips,
    defaultCapabilities,
    tableTypeLabel,
    virtualTables,
    type DiscoveredEntry,
    type IncompatibleSourceError,
    type Locator,
    type RegisterVirtualTableRequest,
    type TableType,
    type VirtualTable,
    type VirtualTableProvider,
  } from '$lib/api/virtual-tables';

  type Props = {
    open: boolean;
    sourceRid: string;
    provider: VirtualTableProvider;
    /** When set, pre-fills the form using the discovered entry. */
    entry: DiscoveredEntry | null;
    onClose: () => void;
    onCreated?: (table: VirtualTable) => void;
  };

  let { open, sourceRid, provider, entry, onClose, onCreated }: Props = $props();

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

  let projectRid = $state('');
  let parentFolderRid = $state('');
  let name = $state('');
  let tableType = $state<TableType>('TABLE');
  let locatorJson = $state('{}');
  let busy = $state(false);
  let error = $state<string | null>(null);
  let remediation = $state<string | null>(null);

  $effect(() => {
    if (entry) {
      name = entry.display_name;
      tableType = entry.inferred_table_type ?? 'OTHER';
      locatorJson = JSON.stringify(deriveLocator(entry, provider), null, 2);
    }
  });

  function deriveLocator(entry: DiscoveredEntry, prov: VirtualTableProvider): Locator {
    const parts = entry.path.split('/').filter(Boolean);
    if (prov === 'AMAZON_S3' || prov === 'AZURE_ABFS' || prov === 'GCS') {
      return {
        kind: 'file',
        bucket: parts[0] ?? entry.path,
        prefix: parts.slice(1).join('/'),
        format: 'parquet',
      };
    }
    if (entry.kind === 'iceberg_table' || entry.kind === 'iceberg_namespace') {
      return {
        kind: 'iceberg',
        catalog: parts[0] ?? '',
        namespace: parts[1] ?? 'default',
        table: parts[parts.length - 1] ?? entry.display_name,
      };
    }
    return {
      kind: 'tabular',
      database: parts[0] ?? '',
      schema: parts[1] ?? '',
      table: parts[parts.length - 1] ?? entry.display_name,
    };
  }

  async function submit(event: Event) {
    event.preventDefault();
    if (!projectRid.trim()) {
      error = 'Project rid is required';
      return;
    }
    let locator: Locator;
    try {
      locator = JSON.parse(locatorJson) as Locator;
    } catch {
      error = 'Locator JSON is not valid';
      return;
    }
    busy = true;
    error = null;
    remediation = null;
    try {
      const body: RegisterVirtualTableRequest = {
        project_rid: projectRid.trim(),
        parent_folder_rid: parentFolderRid.trim() || undefined,
        name: name.trim() || undefined,
        locator,
        table_type: tableType,
      };
      const table = await virtualTables.registerVirtualTable(sourceRid, body);
      onCreated?.(table);
      onClose();
    } catch (err) {
      const incompatible = err as IncompatibleSourceError;
      if (incompatible?.error === 'VIRTUAL_TABLES_INCOMPATIBLE_SOURCE_CONFIG') {
        error = `Source incompatible: ${incompatible.code}`;
        remediation = incompatible.reason?.remediation ?? null;
      } else if (err instanceof Error) {
        error = err.message;
      } else {
        error = 'Failed to register virtual table';
      }
    } finally {
      busy = false;
    }
  }
</script>

{#if open}
  <div class="backdrop" role="dialog" aria-modal="true" data-testid="vt-create-modal">
    <form class="modal" onsubmit={submit}>
      <header>
        <h3>Create virtual table</h3>
        <p class="muted">From source <code>{sourceRid}</code></p>
      </header>
      <label>
        <span>Project rid <span class="required">*</span></span>
        <input type="text" bind:value={projectRid} placeholder="ri.foundry.main.project..." required data-testid="vt-form-project" />
      </label>
      <label>
        <span>Parent folder rid (optional)</span>
        <input type="text" bind:value={parentFolderRid} placeholder="ri.compass.main.folder..." />
      </label>
      <label>
        <span>Name</span>
        <input type="text" bind:value={name} data-testid="vt-form-name" />
      </label>
      <label>
        <span>Table type</span>
        <select bind:value={tableType}>
          {#each TABLE_TYPES as type (type)}
            <option value={type}>{tableTypeLabel(type)}</option>
          {/each}
        </select>
      </label>
      <label class="full">
        <span>Locator (JSON)</span>
        <textarea bind:value={locatorJson} rows={6} data-testid="vt-form-locator"></textarea>
      </label>

      <section class="preview">
        <h4>Capabilities (preview)</h4>
        <div class="chips" data-testid="vt-form-preview-chips">
          {#each capabilityChips(defaultCapabilities(provider, tableType)) as chip (chip)}
            <span class="chip">{chip}</span>
          {/each}
        </div>
      </section>

      {#if error}
        <div class="error" role="alert" data-testid="vt-form-error">
          {error}
          {#if remediation}
            <p class="remediation">{remediation}</p>
          {/if}
        </div>
      {/if}

      <footer>
        <button type="button" onclick={onClose} disabled={busy}>Cancel</button>
        <button type="submit" disabled={busy} class="primary" data-testid="vt-form-submit">
          {busy ? 'Creating…' : 'Create'}
        </button>
      </footer>
    </form>
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
    width: min(560px, 92vw);
    max-height: 90vh;
    overflow: auto;
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0.75rem;
  }
  .modal header,
  .modal section.preview,
  .modal .error,
  .modal footer,
  .modal label.full {
    grid-column: 1 / -1;
  }
  .modal h3 {
    margin: 0;
  }
  .muted {
    color: var(--color-fg-muted, #6b7280);
    font-size: 0.75rem;
  }
  label {
    display: flex;
    flex-direction: column;
    font-size: 0.75rem;
    gap: 0.25rem;
  }
  .required {
    color: #b91c1c;
  }
  input,
  select,
  textarea {
    border: 1px solid var(--color-border, #d1d5db);
    border-radius: 0.25rem;
    padding: 0.375rem 0.5rem;
    font-size: 0.875rem;
    background: var(--color-bg-elevated, #fff);
    font-family: inherit;
  }
  textarea {
    font-family: ui-monospace, SFMono-Regular, monospace;
  }
  .preview h4 {
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    margin: 0 0 0.25rem;
    color: var(--color-fg-muted, #4b5563);
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
  }
  .chip {
    background: var(--color-bg-subtle, #f3f4f6);
    border: 1px solid var(--color-border, #e5e7eb);
    border-radius: 0.25rem;
    font-size: 0.75rem;
    padding: 0.0625rem 0.375rem;
  }
  .error {
    background: #fef2f2;
    border: 1px solid #fecaca;
    color: #b91c1c;
    border-radius: 0.25rem;
    padding: 0.5rem 0.75rem;
    font-size: 0.875rem;
  }
  .remediation {
    margin: 0.25rem 0 0;
    color: #7f1d1d;
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
    font-size: 0.875rem;
    cursor: pointer;
  }
  footer button.primary {
    background: #1d4ed8;
    color: #fff;
    border-color: #1d4ed8;
  }
</style>
