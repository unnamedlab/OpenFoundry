<!--
  ScheduleConfig — cron + enable toggle for pipeline schedules
  (Foundry: "Schedules" tab inside Pipeline Builder). Uses
  `previewScheduleWindows` to show the next N planned executions.

  P2 additions when a `scheduleRid` prop is supplied:
    - Run history with Succeeded / Ignored / Failed badges.
    - Pause/Resume button with state-reset confirmation.
    - Auto-paused banner with "View failures" + "Resume" CTAs.
    - Versions tab with diff selector backed by ScheduleDiff.svelte.
-->
<script lang="ts">
  import type { PipelineScheduleConfig, ScheduleWindow } from '$lib/api/pipelines';
  import { previewScheduleWindows } from '$lib/api/pipelines';
  import {
    AUTO_PAUSED_REASON,
    type Schedule,
    type ScheduleRun,
    type ScheduleVersion,
    type VersionDiff,
    convertToProjectScope,
    getSchedule,
    getScheduleVersionDiff,
    listScheduleRuns,
    listScheduleVersions,
    pauseSchedule,
    patchSchedule,
    resumeSchedule,
  } from '$lib/api/schedules';
  import ScheduleDiff from './ScheduleDiff.svelte';

  type Props = {
    pipelineId?: string;
    /** When set, the P2 governance UI (history / pause / versions) is rendered. */
    scheduleRid?: string;
    config: PipelineScheduleConfig;
    readonly?: boolean;
    onChange: (next: PipelineScheduleConfig) => void;
  };

  let { pipelineId, scheduleRid, config, readonly = false, onChange }: Props = $props();

  let preview = $state<ScheduleWindow[]>([]);
  let previewError = $state<string | null>(null);
  let previewLoading = $state(false);

  // P2 state.
  type Tab = 'config' | 'history' | 'versions';
  let activeTab = $state<Tab>('config');
  let schedule = $state<Schedule | null>(null);
  let runs = $state<ScheduleRun[]>([]);
  let runsError = $state<string | null>(null);
  let versions = $state<ScheduleVersion[]>([]);
  let versionsError = $state<string | null>(null);
  let diff = $state<VersionDiff | null>(null);
  let diffFrom = $state<number | null>(null);
  let diffTo = $state<number | null>(null);
  let pauseError = $state<string | null>(null);

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

  async function loadSchedule() {
    if (!scheduleRid) return;
    try {
      schedule = await getSchedule(scheduleRid);
    } catch (err) {
      pauseError = err instanceof Error ? err.message : String(err);
    }
  }

  async function loadRuns() {
    if (!scheduleRid) return;
    try {
      runsError = null;
      const res = await listScheduleRuns(scheduleRid, { limit: 50 });
      runs = res.data;
    } catch (err) {
      runsError = err instanceof Error ? err.message : String(err);
    }
  }

  async function loadVersions() {
    if (!scheduleRid) return;
    try {
      versionsError = null;
      const res = await listScheduleVersions(scheduleRid, { limit: 50 });
      versions = res.data;
      // Default the diff selector to the current vs. previous version.
      if (versions.length > 0) {
        diffTo = res.current_version;
        diffFrom = versions[0].version;
      }
    } catch (err) {
      versionsError = err instanceof Error ? err.message : String(err);
    }
  }

  async function refreshDiff() {
    if (!scheduleRid || diffFrom === null || diffTo === null) {
      diff = null;
      return;
    }
    try {
      diff = await getScheduleVersionDiff(scheduleRid, diffFrom, diffTo);
    } catch (err) {
      diff = null;
      versionsError = err instanceof Error ? err.message : String(err);
    }
  }

  async function onPauseClicked() {
    if (!scheduleRid || !schedule) return;
    const ok = window.confirm(
      'Pausing this schedule resets its trigger state and forgets all observed events. Continue?',
    );
    if (!ok) return;
    try {
      pauseError = null;
      schedule = await pauseSchedule(scheduleRid, 'MANUAL');
      await loadRuns();
    } catch (err) {
      pauseError = err instanceof Error ? err.message : String(err);
    }
  }

  async function onResumeClicked() {
    if (!scheduleRid || !schedule) return;
    try {
      pauseError = null;
      schedule = await resumeSchedule(scheduleRid);
      await loadRuns();
    } catch (err) {
      pauseError = err instanceof Error ? err.message : String(err);
    }
  }

  // ---- Run-as / project-scope (P3) ----------------------------------------

  let runAsTab = $state<'user' | 'project'>('user');
  let projectScopeInput = $state('');

  $effect(() => {
    if (schedule) {
      runAsTab = schedule.scope_kind === 'PROJECT_SCOPED' ? 'project' : 'user';
      projectScopeInput = schedule.project_scope_rids.join(',');
    }
  });

  async function onConvertToProjectScopeClicked() {
    if (!scheduleRid) return;
    const rids = projectScopeInput
      .split(',')
      .map((r) => r.trim())
      .filter((r) => r.length > 0);
    if (rids.length === 0) {
      pauseError = 'Provide at least one project RID before converting.';
      return;
    }
    try {
      pauseError = null;
      const res = await convertToProjectScope(scheduleRid, { project_scope_rids: rids });
      schedule = res.schedule;
    } catch (err) {
      pauseError = err instanceof Error ? err.message : String(err);
    }
  }

  async function onSaveEditClicked() {
    if (!scheduleRid) return;
    const comment =
      window.prompt('Optional change comment (will appear in the version history):', '') ?? '';
    try {
      schedule = await patchSchedule(scheduleRid, {
        change_comment: comment,
      });
      await loadVersions();
    } catch (err) {
      pauseError = err instanceof Error ? err.message : String(err);
    }
  }

  function setEnabled(next: boolean) {
    onChange({ ...config, enabled: next });
  }
  function setCron(next: string) {
    onChange({ ...config, cron: next.trim() === '' ? null : next.trim() });
  }

  function badgeClass(outcome: string): string {
    switch (outcome) {
      case 'SUCCEEDED':
        return 'badge succeeded';
      case 'IGNORED':
        return 'badge ignored';
      case 'FAILED':
        return 'badge failed';
      default:
        return 'badge';
    }
  }

  $effect(() => {
    void config.enabled;
    void config.cron;
    void pipelineId;
    refreshPreview();
  });

  $effect(() => {
    void scheduleRid;
    if (scheduleRid) {
      loadSchedule();
      loadRuns();
      loadVersions();
    }
  });

  $effect(() => {
    void diffFrom;
    void diffTo;
    refreshDiff();
  });

  let isAutoPaused = $derived(
    !!schedule && schedule.paused && schedule.paused_reason === AUTO_PAUSED_REASON,
  );
</script>

<section class="schedule" data-testid="schedule-config">
  <h3>Schedule</h3>

  {#if scheduleRid}
    <nav class="tabs" role="tablist">
      <button
        type="button"
        role="tab"
        aria-selected={activeTab === 'config'}
        class:active={activeTab === 'config'}
        onclick={() => (activeTab = 'config')}
      >
        Config
      </button>
      <button
        type="button"
        role="tab"
        data-testid="run-history-tab"
        aria-selected={activeTab === 'history'}
        class:active={activeTab === 'history'}
        onclick={() => (activeTab = 'history')}
      >
        Run history
      </button>
      <button
        type="button"
        role="tab"
        data-testid="versions-tab"
        aria-selected={activeTab === 'versions'}
        class:active={activeTab === 'versions'}
        onclick={() => (activeTab = 'versions')}
      >
        Versions
      </button>
    </nav>
  {/if}

  {#if isAutoPaused}
    <div class="banner auto-paused" role="alert" data-testid="auto-paused-banner">
      <strong>Schedule auto-paused after consecutive failures.</strong>
      <span>Review the failures and resume to keep building.</span>
      <div class="banner-actions">
        <button
          type="button"
          data-testid="auto-paused-view-failures"
          onclick={() => (activeTab = 'history')}
        >
          View failures
        </button>
        <button
          type="button"
          data-testid="auto-paused-resume"
          onclick={onResumeClicked}
        >
          Resume
        </button>
      </div>
    </div>
  {/if}

  {#if pauseError}
    <p class="error" role="alert">{pauseError}</p>
  {/if}

  {#if !scheduleRid || activeTab === 'config'}
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

    {#if scheduleRid && schedule}
      <section class="run-as" data-testid="run-as-section">
        <h4>Run as</h4>
        <div class="run-as-tabs" role="tablist">
          <button
            type="button"
            role="tab"
            aria-selected={runAsTab === 'user'}
            class:active={runAsTab === 'user'}
            data-testid="run-as-user-tab"
            onclick={() => (runAsTab = 'user')}
          >
            User
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={runAsTab === 'project'}
            class:active={runAsTab === 'project'}
            data-testid="run-as-project-tab"
            onclick={() => (runAsTab = 'project')}
          >
            Project-scoped
          </button>
        </div>

        {#if runAsTab === 'user'}
          <p class="run-as-summary">
            Runs as <strong>{schedule.run_as_user_id ?? schedule.created_by}</strong>.
          </p>
          {#if schedule.scope_kind === 'USER'}
            <div class="banner warn" role="status" data-testid="run-as-user-banner">
              Schedule will fail if user permissions change.
            </div>
          {/if}
        {:else}
          {#if schedule.scope_kind === 'PROJECT_SCOPED' && schedule.service_principal_id}
            <p class="run-as-summary">
              <span class="sp-badge" data-testid="run-as-sp-badge">Service principal</span>
              <code>{schedule.service_principal_id}</code>
            </p>
            <p class="run-as-summary">
              Project scope: <code>{schedule.project_scope_rids.join(', ')}</code>
            </p>
          {:else}
            <label class="project-scope-input">
              <span>Project RIDs (comma-separated)</span>
              <input
                type="text"
                bind:value={projectScopeInput}
                placeholder="ri.foundry.main.project.alpha,ri.foundry.main.project.beta"
                data-testid="project-scope-input"
              />
            </label>
            <button
              type="button"
              data-testid="convert-to-project-scope-button"
              onclick={onConvertToProjectScopeClicked}
            >
              Convert to project-scoped
            </button>
          {/if}
        {/if}
      </section>

      <div class="governance">
        {#if schedule.paused}
          <button
            type="button"
            data-testid="resume-button"
            onclick={onResumeClicked}
          >
            Resume
          </button>
        {:else}
          <button
            type="button"
            data-testid="pause-button"
            onclick={onPauseClicked}
          >
            Pause
          </button>
        {/if}
        <button
          type="button"
          data-testid="edit-button"
          onclick={onSaveEditClicked}
        >
          Edit (with comment)
        </button>
      </div>
    {/if}

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
  {:else if activeTab === 'history'}
    <section class="history" data-testid="run-history">
      {#if runsError}
        <p class="error">{runsError}</p>
      {:else if runs.length === 0}
        <p class="hint">No runs yet.</p>
      {:else}
        <ul class="run-list">
          {#each runs as run (run.id)}
            <li class="run-row" data-testid="run-row">
              <span class={badgeClass(run.outcome)} data-testid="run-badge-{run.outcome.toLowerCase()}">{run.outcome}</span>
              <span class="ts">{new Date(run.triggered_at).toLocaleString()}</span>
              {#if run.build_rid}
                <a href={`/builds/${run.build_rid}`} class="build-link">{run.build_rid}</a>
              {:else}
                <span class="build-link missing">no build</span>
              {/if}
              {#if run.failure_reason}
                <span class="reason" title={run.failure_reason}>{run.failure_reason}</span>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  {:else if activeTab === 'versions'}
    <section class="versions" data-testid="versions-panel">
      {#if versionsError}
        <p class="error">{versionsError}</p>
      {:else if versions.length === 0}
        <p class="hint">No prior versions yet.</p>
      {:else}
        <div class="version-selector">
          <label>
            <span>From</span>
            <select
              data-testid="diff-from"
              bind:value={diffFrom}
            >
              {#each versions as v (v.version)}
                <option value={v.version}>v{v.version}</option>
              {/each}
            </select>
          </label>
          <label>
            <span>To</span>
            <select
              data-testid="diff-to"
              bind:value={diffTo}
            >
              {#if schedule}
                <option value={schedule.version}>v{schedule.version} (current)</option>
              {/if}
              {#each versions as v (v.version)}
                <option value={v.version}>v{v.version}</option>
              {/each}
            </select>
          </label>
        </div>
        <ScheduleDiff
          {diff}
          fromVersion={diffFrom ?? 0}
          toVersion={diffTo ?? 0}
        />
        <ul class="version-list">
          {#each versions as v (v.version)}
            <li>
              <span class="ver">v{v.version}</span>
              <span class="ts">{new Date(v.edited_at).toLocaleString()}</span>
              <span class="actor">{v.edited_by}</span>
              {#if v.comment}
                <span class="comment">{v.comment}</span>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </section>
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
  input[type='text'], select {
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

  /* P2 additions */
  .tabs {
    display: flex;
    gap: 4px;
    border-bottom: 1px solid #1f2937;
    margin-bottom: 4px;
  }
  .tabs button {
    background: transparent;
    border: none;
    color: #94a3b8;
    padding: 6px 10px;
    font-size: 12px;
    cursor: pointer;
    border-bottom: 2px solid transparent;
  }
  .tabs button.active {
    color: #f1f5f9;
    border-bottom-color: #38bdf8;
  }
  .banner.auto-paused {
    display: flex;
    flex-direction: column;
    gap: 6px;
    background: #422006;
    border: 1px solid #b45309;
    color: #fcd34d;
    padding: 10px 12px;
    border-radius: 6px;
    font-size: 12px;
  }
  .banner-actions {
    display: flex;
    gap: 8px;
  }
  .banner-actions button {
    background: #b45309;
    color: #fff7ed;
    border: none;
    padding: 4px 10px;
    border-radius: 4px;
    font-size: 11px;
    cursor: pointer;
  }
  .governance {
    display: flex;
    gap: 8px;
  }
  .governance button {
    background: #1e293b;
    color: #cbd5e1;
    border: 1px solid #334155;
    border-radius: 4px;
    padding: 4px 10px;
    font-size: 11px;
    cursor: pointer;
  }
  .run-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
    max-height: 320px;
    overflow-y: auto;
  }
  .run-row {
    display: grid;
    grid-template-columns: auto auto 1fr 2fr;
    gap: 8px;
    align-items: center;
    padding: 6px 8px;
    background: #111827;
    border-radius: 4px;
    font-size: 12px;
  }
  .badge {
    display: inline-block;
    padding: 2px 6px;
    border-radius: 3px;
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.05em;
  }
  .badge.succeeded { background: #064e3b; color: #6ee7b7; }
  .badge.ignored { background: #334155; color: #cbd5e1; }
  .badge.failed { background: #7f1d1d; color: #fecaca; }
  .ts {
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
    color: #94a3b8;
    font-size: 11px;
  }
  .build-link {
    color: #93c5fd;
    text-decoration: underline;
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
    font-size: 11px;
  }
  .build-link.missing { color: #475569; text-decoration: none; }
  .reason {
    color: #fca5a5;
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .versions {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .version-selector {
    display: flex;
    gap: 12px;
  }
  .version-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 200px;
    overflow-y: auto;
    border-top: 1px solid #1f2937;
    padding-top: 6px;
  }
  .version-list li {
    display: grid;
    grid-template-columns: auto auto 1fr 2fr;
    gap: 8px;
    align-items: baseline;
    padding: 4px 6px;
    font-size: 11px;
  }
  .ver { color: #93c5fd; font-weight: 600; }
  .actor { color: #94a3b8; font-style: italic; }
  .comment { color: #cbd5e1; }

  /* Run-as (P3) */
  .run-as {
    display: flex;
    flex-direction: column;
    gap: 6px;
    background: #111827;
    border: 1px solid #1f2937;
    border-radius: 6px;
    padding: 8px 10px;
    font-size: 12px;
  }
  .run-as-tabs {
    display: flex;
    gap: 6px;
  }
  .run-as-tabs button {
    background: transparent;
    border: 1px solid #334155;
    color: #94a3b8;
    padding: 4px 10px;
    border-radius: 4px;
    cursor: pointer;
  }
  .run-as-tabs button.active {
    color: #f1f5f9;
    background: #1e293b;
    border-color: #38bdf8;
  }
  .run-as-summary { margin: 4px 0; color: #cbd5e1; }
  .banner.warn {
    background: #422006;
    border: 1px solid #b45309;
    color: #fcd34d;
    padding: 6px 8px;
    border-radius: 4px;
  }
  .sp-badge {
    background: #064e3b;
    color: #6ee7b7;
    padding: 2px 6px;
    border-radius: 3px;
    font-size: 10px;
    margin-right: 6px;
  }
  .project-scope-input {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .project-scope-input input {
    background: #1e293b;
    color: #f1f5f9;
    border: 1px solid #334155;
    border-radius: 4px;
    padding: 4px 6px;
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
  }
</style>
