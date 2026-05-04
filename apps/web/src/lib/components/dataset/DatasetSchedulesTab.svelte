<!--
  Foundry-parity Schedules tab for the Dataset detail page. Lists
  every schedule whose trigger or target references this dataset
  (server-side `files=<rid>` filter on `GET /v1/schedules`).

  The "+ Schedule" CTA opens the wizard at `/schedules/new` with the
  dataset RID prefilled into an Event trigger — Foundry's
  view-and-modify-schedules entry point.
-->
<script lang="ts">
  import { type Schedule, listSchedules } from '$lib/api/schedules';

  type Props = {
    datasetRid: string;
  };

  let { datasetRid }: Props = $props();

  let schedules = $state<Schedule[]>([]);
  let loading = $state(false);
  let errorMsg = $state<string | null>(null);

  async function refresh() {
    loading = true;
    errorMsg = null;
    try {
      const res = await listSchedules({ files: [datasetRid] });
      schedules = res.data;
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : String(err);
      schedules = [];
    } finally {
      loading = false;
    }
  }

  function summarize(s: Schedule): string {
    if ('time' in s.trigger.kind) return `Time · ${s.trigger.kind.time.cron}`;
    if ('event' in s.trigger.kind)
      return `Event · ${s.trigger.kind.event.type}`;
    if ('compound' in s.trigger.kind)
      return `Compound · ${s.trigger.kind.compound.op}`;
    return 'unknown';
  }

  $effect(() => {
    void datasetRid;
    refresh();
  });

  let newScheduleHref = $derived(
    `/schedules/new?event_target=${encodeURIComponent(datasetRid)}`,
  );
</script>

<section class="dataset-schedules-tab" data-testid="dataset-schedules-tab">
  <header>
    <h3>Schedules</h3>
    <a class="add-schedule" href={newScheduleHref} data-testid="dataset-add-schedule">
      + Schedule
    </a>
  </header>

  {#if errorMsg}
    <p class="error" role="alert">{errorMsg}</p>
  {/if}

  {#if loading}
    <p class="hint">Loading…</p>
  {:else if schedules.length === 0}
    <p class="hint">No schedules reference this dataset yet.</p>
  {:else}
    <ul>
      {#each schedules as s (s.rid)}
        <li data-testid="dataset-schedule-row">
          <a href={`/schedules/${s.rid}`} class="name">{s.name}</a>
          <code class="summary">{summarize(s)}</code>
          <span class="paused">{s.paused ? '⏸︎' : '▶︎'}</span>
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .dataset-schedules-tab {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    background: #0b1220;
    border: 1px solid #1f2937;
    border-radius: 8px;
    color: #e2e8f0;
  }
  header { display: flex; justify-content: space-between; align-items: baseline; }
  h3 { margin: 0; font-size: 13px; }
  .add-schedule {
    background: #1e293b;
    color: #f1f5f9;
    border: 1px solid #334155;
    padding: 4px 10px;
    border-radius: 4px;
    font-size: 11px;
    text-decoration: none;
  }
  ul { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; }
  li {
    display: grid;
    grid-template-columns: 1fr auto auto;
    gap: 8px;
    align-items: baseline;
    background: #111827;
    padding: 6px 8px;
    border-radius: 4px;
    font-size: 12px;
  }
  .name { color: #93c5fd; text-decoration: underline; }
  .summary {
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
    color: #cbd5e1;
    font-size: 11px;
  }
  .paused { color: #fcd34d; }
  .hint { color: #94a3b8; font-style: italic; font-size: 12px; margin: 0; }
  .error { color: #fca5a5; font-size: 12px; margin: 0; }
</style>
