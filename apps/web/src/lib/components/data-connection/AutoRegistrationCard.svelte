<script lang="ts">
  /**
   * "Auto-registration" card shown at the top of the source's
   * Virtual tables tab. Foundry doc § "Auto-registration"
   * (img_005, img_006).
   *
   * The card has two states:
   *   * disabled — banner with description + "Enable auto-registration"
   *     CTA. Opens the wizard.
   *   * enabled  — toggle off + project deep-link + last-run summary
   *     (added / updated / orphaned counts) + "Trigger now" action.
   */
  import {
    virtualTables,
    type FolderMirrorKind,
    type VirtualTableProvider,
    type VirtualTableSourceLink,
  } from '$lib/api/virtual-tables';

  type Props = {
    sourceRid: string;
    provider: VirtualTableProvider;
    link: VirtualTableSourceLink | null;
    onOpenWizard: () => void;
    onChanged?: (link: VirtualTableSourceLink) => void;
    onDisabled?: () => void;
  };

  let { sourceRid, provider, link, onOpenWizard, onChanged, onDisabled }: Props = $props();

  let busy = $state<'scan' | 'disable' | null>(null);
  let banner = $state<string | null>(null);
  let lastScan = $state<{ added: number; updated: number; orphaned: number } | null>(null);

  async function scanNow() {
    busy = 'scan';
    banner = null;
    try {
      lastScan = await virtualTables.scanAutoRegistrationNow(sourceRid);
    } catch (err) {
      banner = err instanceof Error ? err.message : 'Scan failed';
    } finally {
      busy = null;
    }
  }

  async function disable() {
    busy = 'disable';
    banner = null;
    try {
      await virtualTables.disableAutoRegistration(sourceRid);
      onDisabled?.();
    } catch (err) {
      banner = err instanceof Error ? err.message : 'Disable failed';
    } finally {
      busy = null;
    }
  }

  function layoutLabel(kind: string | null | undefined): string {
    if (kind === 'FLAT') return 'Flat (database__schema__table)';
    return 'Nested (database / schema / table)';
  }

  // Pre-shadow `onChanged` so the linter doesn't complain when only some
  // pages wire it. Keeps the prop optional.
  const _changed = onChanged;
  void _changed;
</script>

<section class="auto-reg-card" data-testid="vt-auto-reg-card">
  <header>
    <h3>
      Auto-registration
      <span class="muted">({provider})</span>
    </h3>
    {#if link?.auto_register_enabled}
      <span class="badge active">on</span>
    {:else}
      <span class="badge muted">off</span>
    {/if}
  </header>

  {#if !link?.auto_register_enabled}
    <p class="lede">
      Mirror this source's catalog into a Foundry-managed project.
      Tables in the source are auto-registered as virtual tables and
      kept in sync on a schedule. Per Foundry doc, deleting a table
      at the source does <em>not</em> delete the virtual table —
      reads return <code>410 GONE_AT_SOURCE</code> instead.
    </p>
    <button
      type="button"
      class="primary"
      onclick={onOpenWizard}
      data-testid="vt-auto-reg-enable"
    >
      Enable auto-registration
    </button>
  {:else}
    <dl class="kv" data-testid="vt-auto-reg-summary">
      <dt>Managed project</dt>
      <dd>
        {#if link.auto_register_project_rid}
          <a
            href={`/projects/${encodeURIComponent(link.auto_register_project_rid)}`}
            class="mono"
            data-testid="vt-auto-reg-project-link"
          >
            {link.auto_register_project_rid}
          </a>
        {:else}
          <span class="muted">—</span>
        {/if}
      </dd>
      <dt>Layout</dt>
      <dd>{layoutLabel((link as unknown as { auto_register_folder_mirror_kind?: string }).auto_register_folder_mirror_kind)}</dd>
      <dt>Tag filters</dt>
      <dd>
        {#each (link as unknown as { auto_register_table_tag_filters?: string[] }).auto_register_table_tag_filters ?? [] as tag (tag)}
          <span class="chip">{tag}</span>
        {:else}
          <span class="muted">none</span>
        {/each}
      </dd>
      <dt>Poll interval</dt>
      <dd>
        {link.auto_register_interval_seconds ?? '—'}
        {link.auto_register_interval_seconds ? 's' : ''}
      </dd>
      <dt>Last run</dt>
      <dd>
        {#if lastScan}
          <span data-testid="vt-auto-reg-last-run">
            +{lastScan.added} / ~{lastScan.updated} / ✗{lastScan.orphaned}
          </span>
        {:else}
          <span class="muted">no run yet</span>
        {/if}
      </dd>
    </dl>
    <div class="actions">
      <button
        type="button"
        onclick={scanNow}
        disabled={busy !== null}
        data-testid="vt-auto-reg-scan-now"
      >
        {busy === 'scan' ? 'Scanning…' : 'Trigger now'}
      </button>
      <button
        type="button"
        class="danger"
        onclick={disable}
        disabled={busy !== null}
        data-testid="vt-auto-reg-disable"
      >
        {busy === 'disable' ? 'Disabling…' : 'Disable'}
      </button>
    </div>
  {/if}

  {#if banner}
    <div class="error" role="alert">{banner}</div>
  {/if}
</section>

<style>
  .auto-reg-card {
    border: 1px solid var(--color-border, #e5e7eb);
    border-radius: 0.5rem;
    padding: 1rem;
    background: var(--color-bg-elevated, #fff);
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  header h3 {
    margin: 0;
    font-size: 0.95rem;
  }
  .lede {
    margin: 0;
    color: var(--color-fg-muted, #4b5563);
    font-size: 0.875rem;
  }
  .badge {
    margin-left: auto;
    font-size: 0.625rem;
    padding: 0.125rem 0.5rem;
    border-radius: 999px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .badge.active {
    background: #d1fae5;
    color: #065f46;
  }
  .badge.muted {
    background: var(--color-bg-subtle, #f3f4f6);
    color: var(--color-fg-muted, #4b5563);
  }
  .primary {
    align-self: flex-start;
    background: #1d4ed8;
    color: #fff;
    border: 1px solid #1d4ed8;
    border-radius: 0.25rem;
    padding: 0.5rem 0.75rem;
    font-size: 0.875rem;
    cursor: pointer;
  }
  .kv {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 0.25rem 0.75rem;
    margin: 0;
    font-size: 0.875rem;
  }
  .kv dt {
    color: var(--color-fg-muted, #6b7280);
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    align-self: center;
  }
  .kv dd {
    margin: 0;
  }
  .chip {
    display: inline-block;
    padding: 0.0625rem 0.375rem;
    margin-right: 0.25rem;
    font-size: 0.75rem;
    border-radius: 0.25rem;
    background: var(--color-bg-subtle, #f3f4f6);
    border: 1px solid var(--color-border, #e5e7eb);
  }
  .mono {
    font-family: ui-monospace, SFMono-Regular, monospace;
  }
  .muted {
    color: var(--color-fg-muted, #6b7280);
  }
  .actions {
    display: flex;
    gap: 0.5rem;
  }
  .actions button {
    padding: 0.375rem 0.75rem;
    border: 1px solid var(--color-border, #d1d5db);
    border-radius: 0.25rem;
    background: var(--color-bg-elevated, #fff);
    cursor: pointer;
    font-size: 0.875rem;
  }
  .actions .danger {
    color: #b91c1c;
    border-color: #fca5a5;
  }
  .error {
    background: #fef2f2;
    color: #b91c1c;
    border: 1px solid #fecaca;
    border-radius: 0.25rem;
    padding: 0.375rem 0.5rem;
    font-size: 0.875rem;
  }
</style>
