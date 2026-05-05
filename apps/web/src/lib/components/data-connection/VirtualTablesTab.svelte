<script lang="ts">
  /**
   * "Virtual tables" tab body for `/data-connection/sources/[id]`.
   *
   * Foundry doc anchors:
   *   * § "Set up a connection for a virtual table" (img_002)
   *     — enable banner + browse / inspect layout.
   *   * § "Create virtual tables" (img_003) — manual register modal.
   *   * § "Bulk registration" (img_004) — bulk dialog.
   *
   * The component is self-contained: it owns the link row state, the
   * source-validation banner, and the two modals so the source page
   * just renders `<VirtualTablesTab sourceRid={...} provider={...} />`.
   */
  import { onMount } from 'svelte';

  import {
    BULK_REGISTER_PROVIDERS,
    providerLabel,
    virtualTables,
    type DiscoveredEntry,
    type VirtualTable,
    type VirtualTableProvider,
    type VirtualTableSourceLink,
  } from '$lib/api/virtual-tables';

  import RemoteCatalogBrowser from './RemoteCatalogBrowser.svelte';
  import VirtualTableInspector from './VirtualTableInspector.svelte';
  import CreateVirtualTableModal from './CreateVirtualTableModal.svelte';
  import BulkRegisterDialog from './BulkRegisterDialog.svelte';
  import AutoRegistrationCard from './AutoRegistrationCard.svelte';
  import CreateAutoRegistrationModal from './CreateAutoRegistrationModal.svelte';

  type Props = {
    sourceRid: string;
    provider: VirtualTableProvider;
  };

  let { sourceRid, provider }: Props = $props();

  let link = $state<VirtualTableSourceLink | null>(null);
  let loading = $state(true);
  let banner = $state('');
  let busy = $state(false);
  let selected = $state<DiscoveredEntry | null>(null);
  let createOpen = $state(false);
  let bulkOpen = $state(false);
  let autoRegOpen = $state(false);
  let lastCreated = $state<VirtualTable | null>(null);
  let bulkSummary = $state<{ registered: number; failed: number } | null>(null);

  const supportsBulk = $derived(
    BULK_REGISTER_PROVIDERS.includes(provider),
  );

  async function loadLink() {
    loading = true;
    banner = '';
    try {
      // The discover endpoint requires the link row to exist; we use
      // a simple list-by-source filter to detect whether the source
      // has been enabled. A 404 from listVirtualTables is fine —
      // there are simply no rows yet.
      // We treat the absence of any virtual table for this source
      // plus the inability to discover as "not enabled" and prompt
      // the user with the enable banner.
      const ping = await virtualTables
        .discoverRemoteCatalog(sourceRid)
        .catch(() => null);
      if (ping) {
        // Discover succeeded → the link row exists. Build a synthetic
        // link object for the UI (the dedicated GET-source-link
        // endpoint lands in P3.next).
        link = {
          source_rid: sourceRid,
          provider,
          virtual_tables_enabled: true,
          code_imports_enabled: false,
          export_controls: {},
          auto_register_project_rid: null,
          auto_register_enabled: false,
          auto_register_interval_seconds: null,
          auto_register_tag_filters: [],
          iceberg_catalog_kind: null,
          iceberg_catalog_config: null,
          created_at: '',
          updated_at: '',
        };
      } else {
        link = null;
      }
    } finally {
      loading = false;
    }
  }

  onMount(loadLink);

  async function enable() {
    busy = true;
    banner = '';
    try {
      link = await virtualTables.enableOnSource(sourceRid, { provider });
    } catch (err) {
      banner = err instanceof Error ? err.message : 'Failed to enable virtual tables';
    } finally {
      busy = false;
    }
  }

  function onCreated(table: VirtualTable) {
    lastCreated = table;
  }

  function onBulkCompleted(registered: number, failed: number) {
    bulkSummary = { registered, failed };
  }
</script>

<section class="vt-tab" data-testid="vt-source-tab">
  {#if loading}
    <div class="loading">Loading…</div>
  {:else if !link?.virtual_tables_enabled}
    <div class="enable-banner" data-testid="vt-enable-banner">
      <h3>Enable virtual tables on this source</h3>
      <p>
        Virtual tables let Foundry query <strong>{providerLabel(provider)}</strong>
        directly without copying data. Once enabled you can browse the
        remote catalog, register tables manually, or bulk-register a
        list at once.
      </p>
      <p class="muted small">
        See Foundry docs § "Set up a connection for a virtual table"
        for the full walk-through.
      </p>
      <button type="button" onclick={enable} disabled={busy} data-testid="vt-enable-cta">
        {busy ? 'Enabling…' : 'Enable virtual tables'}
      </button>
      {#if banner}
        <div class="error">{banner}</div>
      {/if}
    </div>
  {:else}
    <AutoRegistrationCard
      sourceRid={sourceRid}
      provider={provider}
      link={link}
      onOpenWizard={() => (autoRegOpen = true)}
      onChanged={(updated) => {
        link = updated;
      }}
      onDisabled={() => {
        if (link) link = { ...link, auto_register_enabled: false };
      }}
    />

    <header class="actions">
      <h2>Virtual tables</h2>
      <div class="cta-row">
        <button type="button" onclick={() => (createOpen = true)} data-testid="vt-create-button">
          Create virtual table
        </button>
        {#if supportsBulk}
          <button
            type="button"
            class="secondary"
            onclick={() => (bulkOpen = true)}
            data-testid="vt-bulk-button"
          >
            Bulk register
          </button>
        {/if}
      </div>
    </header>

    {#if lastCreated}
      <div class="success" data-testid="vt-created-toast">
        Created
        <a href={`/virtual-tables/${encodeURIComponent(lastCreated.rid)}`}>
          {lastCreated.name}
        </a>
        in project <code>{lastCreated.project_rid}</code>.
        <button class="dismiss" onclick={() => (lastCreated = null)} aria-label="Dismiss">×</button>
      </div>
    {/if}
    {#if bulkSummary}
      <div class={bulkSummary.failed > 0 ? 'warning' : 'success'} data-testid="vt-bulk-summary">
        Bulk register: {bulkSummary.registered} registered,
        {bulkSummary.failed} failed.
        <button class="dismiss" onclick={() => (bulkSummary = null)} aria-label="Dismiss">×</button>
      </div>
    {/if}

    <div class="split">
      <div class="left">
        <RemoteCatalogBrowser
          sourceRid={sourceRid}
          onSelect={(entry) => {
            selected = entry;
          }}
        />
      </div>
      <div class="right">
        <VirtualTableInspector
          provider={provider}
          entry={selected}
          onCreate={(entry) => {
            selected = entry;
            createOpen = true;
          }}
        />
      </div>
    </div>
  {/if}
</section>

<CreateVirtualTableModal
  open={createOpen}
  sourceRid={sourceRid}
  provider={provider}
  entry={selected}
  onClose={() => (createOpen = false)}
  onCreated={onCreated}
/>
<BulkRegisterDialog
  open={bulkOpen}
  sourceRid={sourceRid}
  provider={provider}
  onClose={() => (bulkOpen = false)}
  onCompleted={onBulkCompleted}
/>
<CreateAutoRegistrationModal
  open={autoRegOpen}
  sourceRid={sourceRid}
  provider={provider}
  onClose={() => (autoRegOpen = false)}
  onEnabled={(updated) => {
    link = updated;
  }}
/>

<style>
  .vt-tab {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  .actions {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    flex-wrap: wrap;
  }
  .actions h2 {
    margin: 0;
    font-size: 1rem;
  }
  .cta-row {
    display: flex;
    gap: 0.5rem;
  }
  .cta-row button {
    padding: 0.375rem 0.75rem;
    background: #1d4ed8;
    color: #fff;
    border: 1px solid #1d4ed8;
    border-radius: 0.25rem;
    cursor: pointer;
    font-size: 0.875rem;
  }
  .cta-row button.secondary {
    background: var(--color-bg-elevated, #fff);
    color: #1d4ed8;
  }
  .enable-banner {
    border: 1px solid var(--color-border, #e5e7eb);
    border-radius: 0.5rem;
    padding: 1.25rem;
    background: var(--color-bg-elevated, #fff);
  }
  .enable-banner h3 {
    margin: 0 0 0.5rem;
  }
  .enable-banner button {
    margin-top: 0.5rem;
    padding: 0.5rem 0.75rem;
    background: #1d4ed8;
    color: #fff;
    border: none;
    border-radius: 0.25rem;
    cursor: pointer;
    font-size: 0.875rem;
  }
  .enable-banner button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .split {
    display: grid;
    grid-template-columns: minmax(240px, 1fr) minmax(280px, 1fr);
    gap: 0.75rem;
  }
  .loading,
  .error,
  .success,
  .warning {
    padding: 0.5rem 0.75rem;
    border-radius: 0.25rem;
    font-size: 0.875rem;
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .error {
    background: #fef2f2;
    color: #b91c1c;
    border: 1px solid #fecaca;
  }
  .success {
    background: #ecfdf5;
    color: #047857;
    border: 1px solid #6ee7b7;
  }
  .warning {
    background: #fef9c3;
    color: #854d0e;
    border: 1px solid #fde047;
  }
  .dismiss {
    margin-left: auto;
    background: transparent;
    border: none;
    cursor: pointer;
    font-size: 1rem;
  }
  .muted {
    color: var(--color-fg-muted, #6b7280);
  }
  .small {
    font-size: 0.75rem;
  }
</style>
