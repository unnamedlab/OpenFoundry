<script lang="ts">
  /**
   * `ObjectTimeline.svelte` (T9) — Vertical scrollable history of object
   * revisions with a Monaco JSON diff between consecutive versions and a
   * "Restore to this revision" action.
   *
   * Backend (T9 handlers in libs/ontology-kernel/src/handlers/objects.rs):
   *   GET  /ontology/types/:typeId/objects/:objectId/revisions
   *   POST /ontology/types/:typeId/objects/:objectId/revisions/:n/restore
   */
  import { onDestroy, onMount, tick } from 'svelte';
  import {
    listObjectRevisions,
    restoreObjectRevision,
    type ObjectInstance,
    type ObjectRevision,
  } from '$lib/api/ontology';
  import { notifications } from '$lib/stores/notifications';
  import type * as Monaco from 'monaco-editor/esm/vs/editor/editor.api';

  interface Props {
    typeId: string;
    objectId: string;
    limit?: number;
    onrestore?: (object: ObjectInstance) => void;
  }

  let { typeId, objectId, limit = 100, onrestore }: Props = $props();

  let revisions = $state<ObjectRevision[]>([]);
  let loading = $state(false);
  let error = $state('');
  let selectedRevision = $state<number | null>(null);
  let restoring = $state(false);

  let monaco = $state<typeof import('monaco-editor/esm/vs/editor/editor.api') | null>(null);
  let diffContainer = $state<HTMLDivElement | null>(null);
  let diffEditor: Monaco.editor.IStandaloneDiffEditor | null = null;

  // Sort newest → oldest defensively.
  const sortedRevisions = $derived(
    [...revisions].sort((a, b) => b.revision_number - a.revision_number),
  );

  // Pair (previous, selected) for the diff view.
  const diffPair = $derived.by(() => {
    if (selectedRevision === null) return null;
    const idx = sortedRevisions.findIndex((r) => r.revision_number === selectedRevision);
    if (idx < 0) return null;
    const current = sortedRevisions[idx];
    const previous = sortedRevisions[idx + 1] ?? null;
    return { previous, current };
  });

  async function reload() {
    loading = true;
    error = '';
    try {
      const response = await listObjectRevisions(typeId, objectId, { limit });
      revisions = response.data ?? [];
      if (revisions.length && selectedRevision === null) {
        selectedRevision = revisions[0].revision_number;
      }
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  async function ensureMonaco() {
    if (monaco) return monaco;
    const [api] = await Promise.all([
      import('monaco-editor/esm/vs/editor/editor.api'),
      import('monaco-editor/esm/vs/language/json/monaco.contribution'),
    ]);
    monaco = api;
    return api;
  }

  function formatJson(value: unknown): string {
    try {
      return JSON.stringify(value ?? {}, null, 2);
    } catch {
      return String(value);
    }
  }

  async function renderDiff() {
    await tick();
    if (!diffContainer) return;
    const api = await ensureMonaco();
    if (!diffEditor) {
      diffEditor = api.editor.createDiffEditor(diffContainer, {
        readOnly: true,
        renderSideBySide: true,
        automaticLayout: true,
        scrollBeyondLastLine: false,
        minimap: { enabled: false },
        theme: 'vs-dark',
      });
    }
    const pair = diffPair;
    const left = pair?.previous ? formatJson(pair.previous.properties) : '{}';
    const right = pair?.current ? formatJson(pair.current.properties) : '{}';
    const original = api.editor.createModel(left, 'json');
    const modified = api.editor.createModel(right, 'json');
    const old = diffEditor.getModel();
    diffEditor.setModel({ original, modified });
    if (old) {
      old.original.dispose();
      old.modified.dispose();
    }
  }

  async function onRestoreClick(revisionNumber: number) {
    if (!confirm(`Restore object to revision #${revisionNumber}? A new revision will be appended to the audit trail.`)) {
      return;
    }
    restoring = true;
    try {
      const response = await restoreObjectRevision(typeId, objectId, revisionNumber);
      notifications.success(
        `Restored to revision #${response.restored_from_revision_number} (new revision #${response.new_revision_number}).`,
      );
      onrestore?.(response.object);
      await reload();
      selectedRevision = response.new_revision_number;
      await renderDiff();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      error = msg;
      notifications.error(`Restore failed: ${msg}`);
    } finally {
      restoring = false;
    }
  }

  function operationBadge(op: string): { label: string; cls: string } {
    switch (op) {
      case 'insert': return { label: 'create', cls: 'badge--insert' };
      case 'update': return { label: 'update', cls: 'badge--update' };
      case 'delete': return { label: 'delete', cls: 'badge--delete' };
      default: return { label: op, cls: 'badge--other' };
    }
  }

  function relativeTime(iso: string): string {
    try {
      return new Date(iso).toLocaleString();
    } catch { return iso; }
  }

  onMount(() => { void reload(); });

  $effect(() => {
    void selectedRevision;
    void revisions.length;
    if (selectedRevision !== null) void renderDiff();
  });

  onDestroy(() => {
    if (diffEditor) {
      const m = diffEditor.getModel();
      diffEditor.dispose();
      diffEditor = null;
      if (m) { m.original.dispose(); m.modified.dispose(); }
    }
  });
</script>

<section class="timeline" aria-label="Object revision timeline">
  <header class="timeline__header">
    <div>
      <h3>Revision timeline</h3>
      <p class="hint">Append-only audit log of every write on this object.</p>
    </div>
    <button type="button" class="ghost" onclick={() => void reload()} disabled={loading}>
      {loading ? 'Refreshing…' : 'Refresh'}
    </button>
  </header>

  {#if error}<div class="error" role="alert">{error}</div>{/if}

  <div class="timeline__body">
    <ol class="timeline__list" aria-label="Revisions">
      {#each sortedRevisions as rev (rev.id)}
        {@const badge = operationBadge(rev.operation)}
        <li class="timeline__item" class:active={selectedRevision === rev.revision_number}>
          <button
            type="button"
            class="timeline__entry"
            onclick={() => (selectedRevision = rev.revision_number)}
          >
            <span class="dot" aria-hidden="true"></span>
            <div class="meta">
              <div class="row">
                <span class="rev">#{rev.revision_number}</span>
                <span class="badge {badge.cls}">{badge.label}</span>
                <span class="marking">{rev.marking}</span>
              </div>
              <div class="row sub">
                <span title={rev.changed_by}>by {rev.changed_by.slice(0, 8)}…</span>
                <span class="dot-sep">·</span>
                <span>{relativeTime(rev.written_at)}</span>
              </div>
            </div>
          </button>
          <div class="actions">
            <button
              type="button"
              class="restore"
              onclick={() => void onRestoreClick(rev.revision_number)}
              disabled={restoring || rev.operation === 'delete'}
              title={rev.operation === 'delete' ? 'Cannot restore a delete revision' : 'Restore object to this revision'}
            >
              {restoring ? '…' : 'Restore'}
            </button>
          </div>
        </li>
      {:else}
        {#if !loading}
          <li class="empty">No revisions recorded for this object yet.</li>
        {/if}
      {/each}
    </ol>

    <div class="timeline__diff">
      <header>
        <strong>JSON diff</strong>
        {#if diffPair?.previous}
          <span class="diff-meta">#{diffPair.previous.revision_number} → #{diffPair.current.revision_number}</span>
        {:else if diffPair?.current}
          <span class="diff-meta">#{diffPair.current.revision_number} (initial)</span>
        {/if}
      </header>
      <div bind:this={diffContainer} class="monaco-host"></div>
    </div>
  </div>
</section>

<style>
  .timeline {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    height: 100%;
    min-height: 0;
    color: #e2e8f0;
  }
  .timeline__header { display: flex; justify-content: space-between; align-items: flex-end; gap: 1rem; }
  .timeline__header h3 { margin: 0; font-size: 1rem; }
  .hint { margin: 0.15rem 0 0; color: #94a3b8; font-size: 0.8rem; }
  .ghost { background: transparent; border: 1px solid #334155; color: #cbd5e1; padding: 0.35rem 0.75rem; border-radius: 6px; cursor: pointer; }
  .ghost:hover { background: #1e293b; }
  .ghost:disabled { opacity: 0.5; cursor: not-allowed; }
  .error { padding: 0.5rem 0.75rem; background: #450a0a; color: #fecaca; border-radius: 6px; font-size: 0.85rem; }

  .timeline__body {
    display: grid;
    grid-template-columns: minmax(260px, 320px) 1fr;
    gap: 0.75rem;
    flex: 1;
    min-height: 0;
  }
  .timeline__list {
    list-style: none;
    margin: 0;
    padding: 0.25rem;
    background: #0b1220;
    border: 1px solid #1e293b;
    border-radius: 8px;
    overflow-y: auto;
    max-height: 60vh;
  }
  .timeline__item {
    display: flex;
    align-items: stretch;
    gap: 0.25rem;
    padding: 0.35rem;
    border-radius: 6px;
    border-left: 3px solid transparent;
  }
  .timeline__item.active { background: #1e293b; border-left-color: #60a5fa; }
  .timeline__entry {
    flex: 1;
    background: transparent;
    border: none;
    color: inherit;
    text-align: left;
    cursor: pointer;
    display: flex;
    gap: 0.6rem;
    padding: 0.25rem;
  }
  .dot {
    width: 10px; height: 10px;
    border-radius: 50%;
    background: #2563eb;
    margin-top: 0.4rem;
    flex-shrink: 0;
  }
  .meta { display: flex; flex-direction: column; gap: 0.1rem; min-width: 0; }
  .row { display: flex; gap: 0.4rem; align-items: baseline; flex-wrap: wrap; }
  .row.sub { font-size: 0.75rem; color: #94a3b8; }
  .dot-sep { opacity: 0.5; }
  .rev { font-family: monospace; font-weight: 700; color: #e2e8f0; }
  .marking {
    background: #1e293b;
    color: #cbd5e1;
    padding: 0 0.35rem;
    border-radius: 3px;
    font-family: monospace;
    font-size: 0.7rem;
  }
  .badge {
    padding: 0 0.4rem;
    border-radius: 3px;
    font-size: 0.7rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .badge--insert { background: #064e3b; color: #6ee7b7; }
  .badge--update { background: #1e3a8a; color: #93c5fd; }
  .badge--delete { background: #7f1d1d; color: #fca5a5; }
  .badge--other { background: #334155; color: #cbd5e1; }
  .actions { display: flex; align-items: center; padding: 0 0.25rem; }
  .restore {
    background: #1e293b;
    color: #e2e8f0;
    border: 1px solid #334155;
    padding: 0.25rem 0.5rem;
    border-radius: 5px;
    font-size: 0.75rem;
    cursor: pointer;
  }
  .restore:hover:not(:disabled) { background: #2563eb; border-color: #2563eb; }
  .restore:disabled { opacity: 0.4; cursor: not-allowed; }

  .timeline__diff {
    display: flex;
    flex-direction: column;
    background: #0b1220;
    border: 1px solid #1e293b;
    border-radius: 8px;
    min-width: 0;
    min-height: 0;
  }
  .timeline__diff header {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    padding: 0.5rem 0.75rem;
    border-bottom: 1px solid #1e293b;
  }
  .diff-meta { font-family: monospace; font-size: 0.8rem; color: #94a3b8; }
  .monaco-host { flex: 1; min-height: 360px; }
  .empty {
    padding: 1rem;
    text-align: center;
    color: #94a3b8;
    font-size: 0.85rem;
  }
</style>
