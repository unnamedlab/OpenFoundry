<!--
  ScheduleDiff — renders a structured diff between two schedule
  versions returned by `GET /v1/schedules/{rid}/versions/diff`.
  Per the Foundry "View schedule edit history" doc, scheduling
  versions need to be browsable side-by-side; the entries are
  per-path JSON deltas, scalar field changes for name/description,
  and a "no changes" empty state.
-->
<script lang="ts">
  import type { VersionDiff } from '$lib/api/schedules';

  type Props = {
    diff: VersionDiff | null;
    fromVersion: number;
    toVersion: number;
  };

  let { diff, fromVersion, toVersion }: Props = $props();

  function formatValue(value: unknown): string {
    if (value === null || value === undefined) return '∅';
    if (typeof value === 'string') return value;
    return JSON.stringify(value);
  }

  let hasChanges = $derived(
    !!diff &&
      ((!!diff.name_diff) ||
        (!!diff.description_diff) ||
        diff.trigger_diff.length > 0 ||
        diff.target_diff.length > 0),
  );
</script>

<section class="diff" data-testid="schedule-diff">
  <header>
    <h4>
      Diff: v{fromVersion} → v{toVersion}
    </h4>
  </header>

  {#if !diff}
    <p class="hint">Loading diff…</p>
  {:else if !hasChanges}
    <p class="hint">No changes between these versions.</p>
  {:else}
    {#if diff.name_diff}
      <div class="entry" data-testid="diff-name">
        <span class="path">name</span>
        <span class="before">{diff.name_diff.before}</span>
        <span class="arrow">→</span>
        <span class="after">{diff.name_diff.after}</span>
      </div>
    {/if}

    {#if diff.description_diff}
      <div class="entry" data-testid="diff-description">
        <span class="path">description</span>
        <span class="before">{diff.description_diff.before}</span>
        <span class="arrow">→</span>
        <span class="after">{diff.description_diff.after}</span>
      </div>
    {/if}

    {#if diff.trigger_diff.length > 0}
      <h5>Trigger</h5>
      {#each diff.trigger_diff as entry (entry.path)}
        <div class="entry" data-testid="diff-trigger">
          <span class="path">{entry.path}</span>
          <span class="before">{formatValue(entry.before)}</span>
          <span class="arrow">→</span>
          <span class="after">{formatValue(entry.after)}</span>
        </div>
      {/each}
    {/if}

    {#if diff.target_diff.length > 0}
      <h5>Target</h5>
      {#each diff.target_diff as entry (entry.path)}
        <div class="entry" data-testid="diff-target">
          <span class="path">{entry.path}</span>
          <span class="before">{formatValue(entry.before)}</span>
          <span class="arrow">→</span>
          <span class="after">{formatValue(entry.after)}</span>
        </div>
      {/each}
    {/if}
  {/if}
</section>

<style>
  .diff {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 12px;
    background: #0b1220;
    border: 1px solid #1f2937;
    border-radius: 8px;
    color: #e2e8f0;
  }
  header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  h4 {
    margin: 0;
    font-size: 13px;
    color: #cbd5e1;
  }
  h5 {
    margin: 8px 0 2px;
    font-size: 11px;
    text-transform: uppercase;
    color: #94a3b8;
    letter-spacing: 0.05em;
  }
  .entry {
    display: grid;
    grid-template-columns: 1fr auto auto auto;
    align-items: center;
    gap: 6px;
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
    font-size: 12px;
    padding: 4px 6px;
    background: #111827;
    border-radius: 4px;
  }
  .path { color: #93c5fd; }
  .before {
    color: #fca5a5;
    text-decoration: line-through;
  }
  .arrow { color: #64748b; }
  .after { color: #86efac; }
  .hint { color: #94a3b8; font-style: italic; font-size: 12px; margin: 0; }
</style>
