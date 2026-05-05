<script lang="ts">
  /**
   * Right-panel inspector for the selected entry in
   * `RemoteCatalogBrowser`. When the entry is `registrable` the
   * inspector renders a CTA + a preview of the capabilities the
   * Foundry doc compatibility matrix would assign once registered.
   */
  import {
    capabilityChips,
    defaultCapabilities,
    tableTypeLabel,
    type DiscoveredEntry,
    type TableType,
    type VirtualTableProvider,
  } from '$lib/api/virtual-tables';

  type Props = {
    provider: VirtualTableProvider;
    entry: DiscoveredEntry | null;
    onCreate?: (entry: DiscoveredEntry) => void;
  };

  let { provider, entry, onCreate }: Props = $props();

  function inferredOrFallback(entry: DiscoveredEntry): TableType {
    return entry.inferred_table_type ?? 'OTHER';
  }
</script>

<aside class="inspector" data-testid="vt-inspector">
  {#if !entry}
    <div class="muted">
      Select an entry on the left to preview its capabilities and
      schema before registering.
    </div>
  {:else}
    <header>
      <h3>{entry.display_name}</h3>
      <code class="path">{entry.path}</code>
    </header>
    <dl class="kv">
      <dt>Kind</dt>
      <dd>{entry.kind.replace('_', ' ')}</dd>
      <dt>Inferred type</dt>
      <dd>{tableTypeLabel(inferredOrFallback(entry))}</dd>
    </dl>
    {#if entry.registrable}
      {@const caps = defaultCapabilities(provider, inferredOrFallback(entry))}
      <section>
        <h4>Capabilities (preview)</h4>
        <div class="chips" data-testid="vt-inspector-cap-chips">
          {#each capabilityChips(caps) as chip (chip)}
            <span class="chip">{chip}</span>
          {/each}
        </div>
      </section>
      <section>
        <h4>Foundry compute</h4>
        <ul class="kv-list">
          <li>Python single-node: {caps.foundry_compute.python_single_node ? '✓' : '✗'}</li>
          <li>Python Spark: {caps.foundry_compute.python_spark ? '✓' : '✗'}</li>
          <li>
            Pipeline Builder Spark:
            {caps.foundry_compute.pipeline_builder_spark ? '✓' : '✗'}
          </li>
        </ul>
      </section>
      <button
        type="button"
        class="cta"
        onclick={() => onCreate?.(entry!)}
        data-testid="vt-inspector-register-cta"
      >
        Register virtual table
      </button>
    {:else}
      <p class="muted">
        Drill in to a leaf table to register a virtual table from this
        location.
      </p>
    {/if}
  {/if}
</aside>

<style>
  .inspector {
    border: 1px solid var(--color-border, #e5e7eb);
    border-radius: 0.5rem;
    background: var(--color-bg-elevated, #fff);
    padding: 1rem;
    min-height: 360px;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  header h3 {
    margin: 0 0 0.25rem;
    font-size: 1rem;
  }
  .path {
    font-family: ui-monospace, SFMono-Regular, monospace;
    font-size: 0.75rem;
    color: var(--color-fg-muted, #4b5563);
  }
  .kv {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 0.25rem 0.75rem;
    margin: 0;
  }
  .kv dt {
    color: var(--color-fg-muted, #6b7280);
    font-size: 0.75rem;
  }
  .kv dd {
    margin: 0;
    font-size: 0.875rem;
  }
  h4 {
    font-size: 0.75rem;
    text-transform: uppercase;
    color: var(--color-fg-muted, #4b5563);
    margin: 0 0 0.25rem;
    letter-spacing: 0.05em;
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
  .kv-list {
    list-style: none;
    padding: 0;
    margin: 0;
    font-size: 0.875rem;
  }
  .cta {
    margin-top: auto;
    padding: 0.5rem 0.75rem;
    background: #1d4ed8;
    color: #fff;
    border: none;
    border-radius: 0.25rem;
    font-size: 0.875rem;
    cursor: pointer;
  }
  .muted {
    color: var(--color-fg-muted, #6b7280);
    font-size: 0.875rem;
  }
</style>
