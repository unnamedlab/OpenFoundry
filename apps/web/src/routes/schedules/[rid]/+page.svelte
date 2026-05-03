<!--
  Schedule detail page — drives ScheduleConfig in `scheduleRid` mode
  (P2 governance UI: pause/resume, run history, versions diff). Cron
  config remains a no-op here; this surface is read-only over the
  trigger but lets operators run the pause/resume + audit flows.
-->
<script lang="ts">
  import { page } from '$app/stores';
  import ScheduleConfig from '$lib/components/pipeline/ScheduleConfig.svelte';

  let rid = $derived($page.params.rid);
  // Minimum config the pipeline-mode UI needs; the scheduleRid flow
  // in ScheduleConfig.svelte fetches the canonical state itself.
  let placeholder = $state({ enabled: true, cron: null, parameters: {} });
</script>

<main class="schedule-page" data-testid="schedule-detail-page">
  <header>
    <h1>Schedule</h1>
    <code class="rid">{rid}</code>
  </header>

  <ScheduleConfig
    config={placeholder}
    scheduleRid={rid}
    onChange={(next) => (placeholder = next)}
  />
</main>

<style>
  .schedule-page {
    padding: 24px;
    max-width: 960px;
    margin: 0 auto;
    color: #e2e8f0;
  }
  header {
    display: flex;
    align-items: baseline;
    gap: 12px;
    margin-bottom: 16px;
  }
  h1 { margin: 0; font-size: 18px; }
  .rid {
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
    font-size: 12px;
    color: #94a3b8;
  }
</style>
