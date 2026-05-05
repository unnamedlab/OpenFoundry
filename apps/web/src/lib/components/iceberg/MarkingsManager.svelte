<script lang="ts">
  /**
   * Iceberg markings manager.
   *
   * Shows the three projections (effective / explicit / inherited)
   * exactly the way the API returns them, plus a "what-if" simulator
   * that previews which principals would lose access if a marking is
   * removed. The simulator is a Beta affordance: the actual diff is
   * computed server-side in the Permissions tab — this component
   * surfaces a stub list pulled from the parent state.
   *
   * Props:
   *   * markings — TableMarkings projection.
   *   * canManage — whether the caller may PATCH the table markings.
   *   * onUpdate — callback invoked when the operator clicks Save.
   */

  import type { IcebergTableMarkings } from '$lib/api/icebergTables';

  let {
    markings,
    canManage,
    onUpdate,
  }: {
    markings: IcebergTableMarkings;
    canManage: boolean;
    onUpdate?: (next: string[]) => Promise<void> | void;
  } = $props();

  let editing = $state(false);
  let draft = $state<string[]>([]);
  let saving = $state(false);
  let error = $state('');

  function startEdit() {
    draft = markings.explicit.map((p) => p.name);
    editing = true;
  }

  function cancelEdit() {
    editing = false;
    error = '';
  }

  async function save() {
    if (!onUpdate) return;
    saving = true;
    error = '';
    try {
      await onUpdate(draft);
      editing = false;
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to save markings';
    } finally {
      saving = false;
    }
  }

  function toggleMarking(name: string) {
    if (draft.includes(name)) {
      draft = draft.filter((m) => m !== name);
    } else {
      draft = [...draft, name];
    }
  }

  const knownMarkings = ['public', 'confidential', 'pii', 'restricted', 'secret'];
</script>

<section class="markings-manager" data-testid="iceberg-markings-manager">
  <header class="manager-header">
    <h2>Markings</h2>
    {#if canManage && !editing}
      <button type="button" onclick={startEdit} data-testid="iceberg-markings-edit">
        Edit
      </button>
    {/if}
  </header>

  <div class="grid">
    <article class="bucket" data-testid="iceberg-markings-effective">
      <h3>Effective</h3>
      {#if markings.effective.length === 0}
        <p class="muted">— no markings —</p>
      {:else}
        <ul>
          {#each markings.effective as item (item.marking_id)}
            <li>
              <span class="badge">{item.name}</span>
              <span class="muted small">{item.description}</span>
            </li>
          {/each}
        </ul>
      {/if}
    </article>

    <article class="bucket" data-testid="iceberg-markings-explicit">
      <h3>Explicit</h3>
      {#if markings.explicit.length === 0}
        <p class="muted">— none set —</p>
      {:else}
        <ul>
          {#each markings.explicit as item (item.marking_id)}
            <li><span class="badge explicit">{item.name}</span></li>
          {/each}
        </ul>
      {/if}
    </article>

    <article class="bucket" data-testid="iceberg-markings-inherited">
      <h3>Inherited from namespace</h3>
      {#if markings.inherited_from_namespace.length === 0}
        <p class="muted">— none inherited —</p>
      {:else}
        <ul>
          {#each markings.inherited_from_namespace as item (item.marking_id)}
            <li><span class="badge inherited">{item.name}</span></li>
          {/each}
        </ul>
      {/if}
    </article>
  </div>

  {#if editing}
    <div class="editor" data-testid="iceberg-markings-editor">
      <p class="hint">
        Toggle the explicit overrides applied <strong>in addition to</strong> the
        inherited set. Inherited markings cannot be removed here; manage them on
        the namespace.
      </p>
      <div class="checks">
        {#each knownMarkings as name}
          <label>
            <input
              type="checkbox"
              checked={draft.includes(name)}
              onchange={() => toggleMarking(name)}
              data-testid={`iceberg-markings-check-${name}`}
            />
            {name}
          </label>
        {/each}
      </div>
      {#if error}
        <p class="error" role="alert">{error}</p>
      {/if}
      <div class="actions">
        <button
          type="button"
          onclick={save}
          disabled={saving}
          data-testid="iceberg-markings-save"
        >
          {saving ? 'Saving…' : 'Save'}
        </button>
        <button type="button" onclick={cancelEdit} class="ghost">Cancel</button>
      </div>
    </div>

    {#if draft.length < markings.explicit.length}
      <aside class="what-if" data-testid="iceberg-markings-what-if">
        <strong>What-if simulator:</strong>
        Removing a marking widens table access. Verify that users currently
        relying on this clearance retain access through other roles before
        saving. The Permissions tab lists the impacted principals.
      </aside>
    {/if}
  {/if}
</section>

<style>
  .markings-manager {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }
  .manager-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .manager-header h2 {
    margin: 0;
    font-size: 1.125rem;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    gap: 1rem;
  }
  .bucket {
    border: 1px solid var(--color-border, #e5e7eb);
    border-radius: 0.5rem;
    padding: 0.75rem;
    background: var(--color-bg-elevated, #fff);
  }
  .bucket h3 {
    margin: 0 0 0.5rem;
    font-size: 0.875rem;
    color: var(--color-fg-muted, #4b5563);
  }
  .bucket ul {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }
  .badge {
    display: inline-block;
    padding: 0.125rem 0.5rem;
    border-radius: 0.25rem;
    font-size: 0.75rem;
    background: var(--color-bg-subtle, #f3f4f6);
    border: 1px solid var(--color-border, #e5e7eb);
  }
  .badge.explicit {
    background: #fef3c7;
    border-color: #fde68a;
    color: #92400e;
  }
  .badge.inherited {
    background: #dbeafe;
    border-color: #bfdbfe;
    color: #1e3a8a;
  }
  .small {
    font-size: 0.7rem;
  }
  .muted {
    color: var(--color-fg-muted, #6b7280);
  }
  .editor {
    border-top: 1px solid var(--color-border, #e5e7eb);
    padding-top: 0.75rem;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .checks {
    display: flex;
    gap: 1rem;
    flex-wrap: wrap;
  }
  .actions {
    display: flex;
    gap: 0.5rem;
  }
  button {
    padding: 0.375rem 0.75rem;
    border-radius: 0.25rem;
    border: 1px solid var(--color-border, #d1d5db);
    background: var(--color-accent, #2563eb);
    color: white;
    font-size: 0.875rem;
    cursor: pointer;
  }
  button.ghost {
    background: transparent;
    color: inherit;
  }
  button[disabled] {
    opacity: 0.6;
    cursor: not-allowed;
  }
  .error {
    color: #b91c1c;
    background: #fef2f2;
    padding: 0.5rem 0.75rem;
    border-radius: 0.25rem;
  }
  .what-if {
    border: 1px dashed #fde68a;
    background: #fef3c7;
    color: #78350f;
    padding: 0.5rem 0.75rem;
    border-radius: 0.375rem;
    font-size: 0.8rem;
  }
  .hint {
    font-size: 0.8rem;
    color: var(--color-fg-muted, #4b5563);
    margin: 0;
  }
</style>
