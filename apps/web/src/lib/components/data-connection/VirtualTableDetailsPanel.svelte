<script lang="ts">
  /**
   * Foundry-doc-aligned "Details" panel for a virtual table.
   *
   * Doc § "Viewing virtual table details" prescribes four cards:
   *   * Incremental
   *   * Versioning
   *   * Update detection (with toggle)
   *   * Compute pushdown
   *
   * The panel is a self-contained component so the same render lives
   * inside both the global `/virtual-tables/[rid]` detail route and
   * the Dataset Preview drawer for datasets that resolve to a
   * `VIRTUAL_TABLE` backing.
   *
   * P5 wires the update-detection toggle + poll-now action; P6 will
   * wire compute pushdown badges to the source's actual engine.
   */
  import {
    capabilityChips,
    virtualTables,
    type Capabilities,
    type UpdateDetectionPollResult,
    type VirtualTable,
  } from '$lib/api/virtual-tables';

  type Props = {
    table: VirtualTable;
    onChanged?: (next: VirtualTable) => void;
  };

  let { table, onChanged }: Props = $props();

  let busy = $state<'toggle' | 'poll' | null>(null);
  let error = $state<string | null>(null);
  let lastPoll = $state<UpdateDetectionPollResult | null>(null);
  let intervalSeconds = $state<number>(table.update_detection_interval_seconds ?? 3600);

  const caps: Capabilities = $derived(table.capabilities);
  const orphaned = $derived(
    Boolean((table.properties as Record<string, unknown>)?.orphaned),
  );

  async function toggle() {
    busy = 'toggle';
    error = null;
    try {
      const next = await virtualTables.setUpdateDetection(table.rid, {
        enabled: !table.update_detection_enabled,
        interval_seconds: Math.max(60, Number(intervalSeconds) || 3600),
      });
      table = next;
      onChanged?.(next);
    } catch (err) {
      error = err instanceof Error ? err.message : 'Toggle failed';
    } finally {
      busy = null;
    }
  }

  async function pollNow() {
    busy = 'poll';
    error = null;
    try {
      lastPoll = await virtualTables.pollUpdateDetectionNow(table.rid);
    } catch (err) {
      error = err instanceof Error ? err.message : 'Poll failed';
    } finally {
      busy = null;
    }
  }

  function pushdownLabel(): string {
    if (!caps?.compute_pushdown) return 'None';
    const map: Record<string, string> = {
      ibis: 'Ibis',
      pyspark: 'PySpark',
      snowpark: 'Snowpark',
    };
    return map[caps.compute_pushdown] ?? caps.compute_pushdown;
  }
</script>

<section class="vt-details-panel" data-testid="vt-details-panel">
  {#if orphaned}
    <div class="banner danger" role="alert" data-testid="vt-orphan-banner">
      <strong>Backing source table no longer exists.</strong>
      Reads will fail with <code>410 GONE_AT_SOURCE</code>. The
      virtual table is preserved per Foundry doc § "Auto-registration"
      so downstream lineage stays traceable; remove it manually if you
      no longer need the pointer.
    </div>
  {/if}

  <div class="grid">
    <article class="card" data-testid="vt-card-incremental">
      <header>
        <h4>Incremental</h4>
        {#if caps?.incremental}
          <span class="chip on">Supported</span>
        {:else}
          <span class="chip off">Not supported</span>
        {/if}
      </header>
      <p class="hint">
        {#if caps?.incremental}
          Foundry can configure incremental pipelines so downstream
          builds only process new rows.
        {:else}
          Pipelines reading this table re-process the full snapshot on
          every build.
        {/if}
      </p>
      {#if caps?.incremental}
        <a class="cta" href={`/pipelines/new?virtual_table=${encodeURIComponent(table.rid)}`}>
          Configure incremental pipeline →
        </a>
      {/if}
    </article>

    <article class="card" data-testid="vt-card-versioning">
      <header>
        <h4>Versioning</h4>
        {#if caps?.versioning}
          <span class="chip on">Supported</span>
        {:else}
          <span class="chip off">Not supported</span>
        {/if}
      </header>
      <p class="hint">
        {#if caps?.versioning}
          Foundry detects source-side changes via snapshot id / commit
          time and skips downstream builds when the data has not
          changed.
        {:else}
          Without versioning, every poll is treated as a potential
          update — downstream schedules may run on each tick.
        {/if}
      </p>
    </article>

    <article class="card" data-testid="vt-card-update-detection">
      <header>
        <h4>Update detection</h4>
        <label class="switch">
          <input
            type="checkbox"
            checked={table.update_detection_enabled}
            onchange={toggle}
            disabled={busy !== null}
            data-testid="vt-update-detection-toggle"
          />
          <span>{table.update_detection_enabled ? 'On' : 'Off'}</span>
        </label>
      </header>
      {#if table.update_detection_enabled}
        <dl class="kv">
          <dt>Interval</dt>
          <dd>
            <input
              type="number"
              min="60"
              bind:value={intervalSeconds}
              onblur={toggle}
              data-testid="vt-update-detection-interval"
            />
            seconds
          </dd>
          <dt>Last polled at</dt>
          <dd>{table.last_polled_at ?? '—'}</dd>
          <dt>Last observed version</dt>
          <dd class="mono">{table.last_observed_version ?? '—'}</dd>
        </dl>
        <button
          type="button"
          onclick={pollNow}
          disabled={busy !== null}
          data-testid="vt-update-detection-poll-now"
        >
          {busy === 'poll' ? 'Polling…' : 'Poll now'}
        </button>
        {#if lastPoll}
          <p class="poll-result" data-testid="vt-update-detection-last-poll">
            Last poll: <strong>{lastPoll.outcome}</strong>
            {#if lastPoll.event_emitted}
              · downstream event emitted
            {/if}
            {#if lastPoll.observed_version}
              · v=<code>{lastPoll.observed_version}</code>
            {/if}
            ({lastPoll.latency_ms} ms)
          </p>
        {/if}
      {:else}
        <p class="hint">
          Enable polling to wake any downstream schedule that
          registered an <code>EventTrigger { '{ type: DATA_UPDATED }' }</code>
          on this virtual table.
        </p>
      {/if}
    </article>

    <article class="card" data-testid="vt-card-pushdown">
      <header>
        <h4>Compute pushdown</h4>
        <span class={caps?.compute_pushdown ? 'chip on' : 'chip off'}>
          {pushdownLabel()}
        </span>
      </header>
      <p class="hint">
        {#if caps?.compute_pushdown}
          Queries can be pushed down to the source system to limit
          data transfer. Activated via the Code Repositories +
          Pipeline Builder integrations (P6).
        {:else}
          No native pushdown engine for this source × table type.
        {/if}
      </p>
      <div class="chips">
        {#each capabilityChips(caps) as chip (chip)}
          <span class="chip muted">{chip}</span>
        {/each}
      </div>
    </article>
  </div>

  {#if error}
    <div class="banner error" role="alert">{error}</div>
  {/if}
</section>

<style>
  .vt-details-panel {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
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
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .card header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .card h4 {
    margin: 0;
    font-size: 0.875rem;
  }
  .chip {
    display: inline-block;
    padding: 0.0625rem 0.5rem;
    font-size: 0.75rem;
    border-radius: 0.25rem;
    background: var(--color-bg-subtle, #f3f4f6);
    border: 1px solid var(--color-border, #e5e7eb);
  }
  .chip.on {
    background: #d1fae5;
    color: #065f46;
    border-color: #6ee7b7;
  }
  .chip.off {
    background: var(--color-bg-subtle, #f3f4f6);
    color: var(--color-fg-muted, #6b7280);
  }
  .chip.muted {
    background: #eef2ff;
    color: #1e3a8a;
    border-color: #c7d2fe;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
  }
  .hint {
    margin: 0;
    color: var(--color-fg-muted, #4b5563);
    font-size: 0.75rem;
  }
  .cta {
    color: #1d4ed8;
    font-size: 0.875rem;
    text-decoration: none;
  }
  .cta:hover {
    text-decoration: underline;
  }
  .switch {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    font-size: 0.75rem;
    cursor: pointer;
  }
  .kv {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 0.25rem 0.75rem;
    margin: 0;
    font-size: 0.75rem;
  }
  .kv dt {
    color: var(--color-fg-muted, #6b7280);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .kv dd {
    margin: 0;
  }
  .kv input[type='number'] {
    width: 6rem;
    padding: 0.125rem 0.375rem;
    border: 1px solid var(--color-border, #d1d5db);
    border-radius: 0.25rem;
  }
  .mono {
    font-family: ui-monospace, SFMono-Regular, monospace;
  }
  .poll-result {
    margin: 0;
    font-size: 0.75rem;
    color: var(--color-fg-muted, #4b5563);
  }
  button {
    align-self: flex-start;
    padding: 0.25rem 0.5rem;
    border: 1px solid var(--color-border, #d1d5db);
    border-radius: 0.25rem;
    background: var(--color-bg-elevated, #fff);
    cursor: pointer;
    font-size: 0.75rem;
  }
  button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .banner {
    border: 1px solid var(--color-border, #e5e7eb);
    border-radius: 0.5rem;
    padding: 0.5rem 0.75rem;
    font-size: 0.875rem;
  }
  .banner.danger {
    background: #fef2f2;
    color: #991b1b;
    border-color: #fecaca;
  }
  .banner.error {
    background: #fef2f2;
    color: #b91c1c;
    border-color: #fecaca;
  }
</style>
