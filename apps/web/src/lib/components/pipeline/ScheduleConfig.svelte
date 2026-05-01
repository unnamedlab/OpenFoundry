<!--
  ScheduleConfig — cron + enable toggle for pipeline schedules
  (Foundry: "Schedules" tab inside Pipeline Builder). Uses
  `previewScheduleWindows` to show the next N planned executions.
-->
<script lang="ts">
  import type { PipelineScheduleConfig, ScheduleWindow } from '$lib/api/pipelines';
  import { previewScheduleWindows } from '$lib/api/pipelines';

  type Props = {
    pipelineId?: string;
    config: PipelineScheduleConfig;
    readonly?: boolean;
    onChange: (next: PipelineScheduleConfig) => void;
  };

  let { pipelineId, config, readonly = false, onChange }: Props = $props();

  let preview = $state<ScheduleWindow[]>([]);
  let previewError = $state<string | null>(null);
  let previewLoading = $state(false);

  const PRESETS: { label: string; cron: string }[] = [
    { label: 'Every 15 minutes', cron: '*/15 * * * *' },
    { label: 'Hourly', cron: '0 * * * *' },
    { label: 'Daily 02:00', cron: '0 2 * * *' },
    { label: 'Weekly Mon 06:00', cron: '0 6 * * 1' },
  ];

  async function refreshPreview() {
    if (!pipelineId || !config.enabled || !config.cron) {
      preview = [];
      previewError = null;
      return;
    }
    previewLoading = true;
    previewError = null;
    try {
      const now = new Date();
      const horizon = new Date(now.getTime() + 7 * 24 * 60 * 60 * 1000);
      const res = await previewScheduleWindows({
        target_kind: 'pipeline',
        target_id: pipelineId,
        start_at: now.toISOString(),
        end_at: horizon.toISOString(),
        limit: 10,
      });
      preview = res.data ?? [];
    } catch (err) {
      previewError = err instanceof Error ? err.message : String(err);
      preview = [];
    } finally {
      previewLoading = false;
    }
  }

  function setEnabled(next: boolean) {
    onChange({ ...config, enabled: next });
  }
  function setCron(next: string) {
    onChange({ ...config, cron: next.trim() === '' ? null : next.trim() });
  }

  $effect(() => {
    // Re-fetch when relevant inputs change.
    void config.enabled; void config.cron; void pipelineId;
    refreshPreview();
  });
</script>

<section class="schedule">
  <h3>Schedule</h3>

  <label class="toggle">
    <input
      type="checkbox"
      checked={config.enabled}
      disabled={readonly}
      onchange={(e) => setEnabled((e.currentTarget as HTMLInputElement).checked)}
    />
    <span>Enable scheduled runs</span>
  </label>

  <label>
    <span>Cron expression (UTC)</span>
    <input
      type="text"
      value={config.cron ?? ''}
      placeholder="0 2 * * *"
      readonly={readonly}
      disabled={!config.enabled}
      oninput={(e) => setCron((e.currentTarget as HTMLInputElement).value)}
    />
  </label>

  <div class="presets">
    {#each PRESETS as preset (preset.cron)}
      <button
        type="button"
        disabled={readonly || !config.enabled}
        onclick={() => setCron(preset.cron)}
      >
        {preset.label}
      </button>
    {/each}
  </div>

  {#if pipelineId && config.enabled && config.cron}
    <section class="preview">
      <h4>Next 10 windows</h4>
      {#if previewLoading}
        <p class="hint">Loading…</p>
      {:else if previewError}
        <p class="error">{previewError}</p>
      {:else if preview.length === 0}
        <p class="hint">No upcoming windows in the next 7 days.</p>
      {:else}
        <ol>
          {#each preview as win, i (i)}
            <li>
              <code>{new Date(win.scheduled_for).toLocaleString()}</code>
            </li>
          {/each}
        </ol>
      {/if}
    </section>
  {:else if !pipelineId}
    <p class="hint">Save the pipeline to preview upcoming windows.</p>
  {/if}
</section>

<style>
  .schedule {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 14px;
    background: #0b1220;
    border: 1px solid #1f2937;
    border-radius: 8px;
    color: #e2e8f0;
  }
  h3 { margin: 0; font-size: 14px; }
  h4 {
    margin: 6px 0 4px;
    font-size: 11px;
    text-transform: uppercase;
    color: #94a3b8;
    letter-spacing: 0.05em;
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 12px;
    color: #cbd5e1;
  }
  .toggle { flex-direction: row; align-items: center; gap: 8px; }
  input[type='text'] {
    background: #1e293b;
    color: #f1f5f9;
    border: 1px solid #334155;
    border-radius: 4px;
    padding: 6px 8px;
    font: inherit;
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
  }
  input[disabled] { opacity: 0.5; }
  .presets {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .presets button {
    background: #1e293b;
    color: #cbd5e1;
    border: 1px solid #334155;
    border-radius: 4px;
    padding: 4px 10px;
    font-size: 11px;
    cursor: pointer;
  }
  .presets button:hover:not(:disabled) {
    background: #334155;
  }
  .presets button:disabled { opacity: 0.4; cursor: not-allowed; }
  .preview ol {
    margin: 0;
    padding-left: 18px;
    color: #cbd5e1;
    font-size: 12px;
  }
  .preview li code {
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
    font-size: 12px;
  }
  .hint { color: #94a3b8; font-style: italic; font-size: 12px; margin: 0; }
  .error { color: #fca5a5; font-size: 12px; margin: 0; }
</style>
