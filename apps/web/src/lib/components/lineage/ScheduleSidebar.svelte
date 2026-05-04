<!--
  Lineage Schedule sidebar — Foundry-parity surface for the
  "View and modify schedules" workflow described in
  Schedules.md § "Find and manage schedules":

    "Schedules can be edited, managed, and updated in the schedule
     sidebar of the Data lineage application."

  The sidebar lists every schedule whose trigger or target references
  a dataset in the currently-selected lineage subgraph, with an
  expand/collapse row that opens an inline editor (same shape as the
  P2 schedule detail page).

  Drag-and-drop: dropping a dataset onto the sidebar prepopulates the
  "+ New schedule" wizard with an Event trigger pointed at that dataset.
-->
<script lang="ts">
  import { type Schedule, listSchedules } from '$lib/api/schedules';

  type Props = {
    /** Dataset RIDs in the current lineage subgraph. */
    selectedDatasetRids: string[];
    /** Emitted when the user drops a dataset on the sidebar. */
    onCreateForDataset?: (datasetRid: string) => void;
  };

  let { selectedDatasetRids, onCreateForDataset }: Props = $props();

  let schedules = $state<Schedule[]>([]);
  let loading = $state(false);
  let errorMsg = $state<string | null>(null);
  let expandedRid = $state<string | null>(null);
  let dragHover = $state(false);

  async function refresh() {
    if (selectedDatasetRids.length === 0) {
      schedules = [];
      return;
    }
    loading = true;
    errorMsg = null;
    try {
      // Server filters by `files`; the listSchedules helper stitches
      // them into the URL `files` query repeated param.
      const res = await listSchedules({ files: selectedDatasetRids });
      schedules = res.data;
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : String(err);
      schedules = [];
    } finally {
      loading = false;
    }
  }

  function summarizeTrigger(s: Schedule): string {
    if ('time' in s.trigger.kind) return `Time · ${s.trigger.kind.time.cron}`;
    if ('event' in s.trigger.kind)
      return `Event · ${s.trigger.kind.event.type} ${s.trigger.kind.event.target_rid}`;
    if ('compound' in s.trigger.kind)
      return `Compound · ${s.trigger.kind.compound.op}(${s.trigger.kind.compound.components.length})`;
    return 'unknown';
  }

  function onDragOver(e: DragEvent) {
    e.preventDefault();
    dragHover = true;
  }

  function onDragLeave() {
    dragHover = false;
  }

  function onDrop(e: DragEvent) {
    e.preventDefault();
    dragHover = false;
    const datasetRid = e.dataTransfer?.getData('application/x-dataset-rid');
    if (datasetRid && onCreateForDataset) {
      onCreateForDataset(datasetRid);
    }
  }

  $effect(() => {
    void selectedDatasetRids;
    refresh();
  });
</script>

<aside
  class="lineage-schedule-sidebar {dragHover ? 'drag-hover' : ''}"
  data-testid="lineage-schedule-sidebar"
  ondragover={onDragOver}
  ondragleave={onDragLeave}
  ondrop={onDrop}
  role="complementary"
  aria-label="Schedules touching the current lineage selection"
>
  <header>
    <h3>Manage Schedules</h3>
    <span class="hint">{schedules.length} schedule{schedules.length === 1 ? '' : 's'}</span>
  </header>

  {#if errorMsg}
    <p class="error" role="alert">{errorMsg}</p>
  {/if}

  {#if dragHover}
    <p class="drop-hint" data-testid="drop-hint">
      Drop the dataset to create a new schedule with an Event trigger.
    </p>
  {/if}

  {#if selectedDatasetRids.length === 0}
    <p class="hint">Select datasets in the lineage canvas to see schedules touching them.</p>
  {:else if loading}
    <p class="hint">Loading…</p>
  {:else if schedules.length === 0}
    <p class="hint">No schedules touch the current selection.</p>
  {:else}
    <ul class="schedule-list">
      {#each schedules as s (s.rid)}
        <li class="schedule-row" data-testid="lineage-schedule-row">
          <button
            type="button"
            class="schedule-toggle"
            onclick={() => (expandedRid = expandedRid === s.rid ? null : s.rid)}
            aria-expanded={expandedRid === s.rid}
          >
            <span class="schedule-name">{s.name}</span>
            <span class="schedule-summary">{summarizeTrigger(s)}</span>
            <span class="schedule-paused">{s.paused ? '⏸︎' : '▶︎'}</span>
          </button>
          {#if expandedRid === s.rid}
            <div class="inline-editor" data-testid="lineage-schedule-inline-editor">
              <p class="hint">
                Open <a href={`/schedules/${s.rid}`}>full editor</a> to change trigger, target,
                or governance settings. Use
                <a href={`/build-schedules/${s.rid}`}>Metrics</a> for run history + version diff.
              </p>
            </div>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</aside>

<style>
  .lineage-schedule-sidebar {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    background: #0b1220;
    border: 1px solid #1f2937;
    border-radius: 8px;
    color: #e2e8f0;
    width: 320px;
    box-sizing: border-box;
    transition: background 0.1s ease, border-color 0.1s ease;
  }
  .lineage-schedule-sidebar.drag-hover {
    background: #111827;
    border-color: #38bdf8;
  }
  header {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
  }
  h3 { margin: 0; font-size: 13px; }
  .hint { color: #94a3b8; font-size: 11px; font-style: italic; margin: 0; }
  .drop-hint {
    background: #064e3b;
    color: #6ee7b7;
    padding: 6px 8px;
    border-radius: 4px;
    font-size: 11px;
    text-align: center;
  }
  .error { color: #fca5a5; font-size: 12px; margin: 0; }
  .schedule-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
    max-height: 480px;
    overflow-y: auto;
  }
  .schedule-row { background: #111827; border-radius: 4px; }
  .schedule-toggle {
    width: 100%;
    background: transparent;
    border: none;
    color: #cbd5e1;
    padding: 6px 8px;
    cursor: pointer;
    display: grid;
    grid-template-columns: 1fr auto auto;
    gap: 8px;
    align-items: baseline;
    text-align: left;
    font-size: 12px;
  }
  .schedule-name { color: #f1f5f9; font-weight: 500; }
  .schedule-summary {
    color: #94a3b8;
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
    font-size: 11px;
  }
  .schedule-paused { color: #fcd34d; }
  .inline-editor {
    padding: 6px 8px 8px;
    border-top: 1px solid #1f2937;
  }
  a { color: #93c5fd; }
</style>
