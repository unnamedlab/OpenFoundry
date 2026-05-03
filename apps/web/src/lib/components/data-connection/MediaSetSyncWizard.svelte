<!--
  MediaSetSyncWizard — five-step wizard mirroring the Foundry "Set up a
  media set sync" flow (Connectivity → Data Connection → Syncs):

    1. Type            — MEDIA_SET_SYNC vs VIRTUAL_MEDIA_SET_SYNC.
    2. File types      — schema (one of six) + allowed MIME types.
    3. Schedule        — reuses `pipeline/ScheduleConfig.svelte`.
    4. Subfolder       — text input (path under the source's bucket).
    5. Filters         — exclude_already_synced, path_glob,
                         file_size_limit (MB), ignore_unmatched_schema.

  After step 5, "Save" submits the wizard via
  `dataConnection.createMediaSetSync(sourceId, payload)`. If the
  configured `target_media_set_rid` is empty the wizard surfaces an
  inline "Create new media set" panel that calls
  `mediaSets.createMediaSet({ ...; virtual_: kind === 'VIRTUAL_…' })`
  and re-uses the freshly-minted RID for the sync.

  Props:
    * `sourceId`       — UUID of the source the sync is bound to.
    * `sourceName?`    — display label.
    * `defaultProjectRid?` — pre-fill for the inline create form.
    * `onSaved(sync)`  — fired with the persisted sync on success.
    * `onCancel()`     — close the wizard.
-->
<script lang="ts">
  import { dataConnection, type MediaSetSyncDef } from '$lib/api/data-connection';
  import {
    DEFAULT_MIME_TYPES,
    MEDIA_SET_SCHEMAS,
    createMediaSet,
    type MediaSet,
    type MediaSetSchema
  } from '$lib/api/mediaSets';
  import ScheduleConfig from '$lib/components/pipeline/ScheduleConfig.svelte';
  import type { PipelineScheduleConfig } from '$lib/api/pipelines';

  type Props = {
    sourceId: string;
    sourceName?: string;
    defaultProjectRid?: string;
    onSaved: (sync: MediaSetSyncDef) => void;
    onCancel: () => void;
  };

  let {
    sourceId,
    sourceName = 'source',
    defaultProjectRid = 'ri.foundry.main.project.default',
    onSaved,
    onCancel
  }: Props = $props();

  // ── Wizard state ──────────────────────────────────────────────────
  type Step = 1 | 2 | 3 | 4 | 5;
  let step = $state<Step>(1);

  let kind = $state<'MEDIA_SET_SYNC' | 'VIRTUAL_MEDIA_SET_SYNC'>('MEDIA_SET_SYNC');

  let schema = $state<MediaSetSchema>('IMAGE');
  let allowedMimeTypes = $state<string[]>([...DEFAULT_MIME_TYPES.IMAGE]);
  let mimePrefillFollowingSchema = $state(true);

  let scheduleConfig = $state<PipelineScheduleConfig>({ enabled: false, cron: null });

  let subfolder = $state('');

  // Filters
  let excludeAlreadySynced = $state(true);
  let pathGlob = $state('');
  let fileSizeLimitMb = $state<number | null>(null);
  let ignoreUnmatchedSchema = $state(true);

  // Target media set
  let targetMediaSetRid = $state('');
  let createInline = $state(false);
  let createInlineName = $state('');
  let createInlineProjectRid = $state(defaultProjectRid);
  let createInlineBusy = $state(false);
  let createInlineError = $state('');
  let lastCreatedSet = $state<MediaSet | null>(null);

  let saving = $state(false);
  let saveError = $state('');

  // Re-prefill MIME types whenever the schema changes — until the
  // operator overrides them, which switches to "manual" mode.
  $effect(() => {
    if (mimePrefillFollowingSchema) {
      allowedMimeTypes = [...DEFAULT_MIME_TYPES[schema]];
    }
  });

  function next() {
    if (step < 5) step = (step + 1) as Step;
  }

  function back() {
    if (step > 1) step = (step - 1) as Step;
  }

  function toggleMime(mime: string, checked: boolean) {
    mimePrefillFollowingSchema = false;
    allowedMimeTypes = checked
      ? Array.from(new Set([...allowedMimeTypes, mime]))
      : allowedMimeTypes.filter((m) => m !== mime);
  }

  async function createInlineSet() {
    if (!createInlineName.trim() || !createInlineProjectRid.trim()) {
      createInlineError = 'Name and project RID are required.';
      return;
    }
    createInlineBusy = true;
    createInlineError = '';
    try {
      const created = await createMediaSet({
        name: createInlineName.trim(),
        project_rid: createInlineProjectRid.trim(),
        schema,
        allowed_mime_types: allowedMimeTypes,
        transaction_policy: 'TRANSACTIONLESS',
        retention_seconds: 0,
        virtual_: kind === 'VIRTUAL_MEDIA_SET_SYNC',
        // Bind the virtual set back to this source so the cross-app
        // badge on /media-sets/[id] resolves immediately.
        source_rid: kind === 'VIRTUAL_MEDIA_SET_SYNC' ? sourceId : null
      });
      lastCreatedSet = created;
      targetMediaSetRid = created.rid;
      createInline = false;
    } catch (cause) {
      createInlineError =
        cause instanceof Error ? cause.message : 'Failed to create media set';
    } finally {
      createInlineBusy = false;
    }
  }

  async function save() {
    if (!targetMediaSetRid.trim()) {
      saveError = 'Pick or create a target media set first.';
      return;
    }
    saving = true;
    saveError = '';
    try {
      const persisted = await dataConnection.createMediaSetSync(sourceId, {
        kind,
        target_media_set_rid: targetMediaSetRid.trim(),
        subfolder: subfolder.trim(),
        filters: {
          exclude_already_synced: excludeAlreadySynced,
          path_glob: pathGlob.trim() || null,
          file_size_limit:
            fileSizeLimitMb && fileSizeLimitMb > 0 ? fileSizeLimitMb * 1024 * 1024 : null,
          ignore_unmatched_schema: ignoreUnmatchedSchema
        },
        schedule_cron: scheduleConfig.enabled ? scheduleConfig.cron : null
      });
      onSaved(persisted);
    } catch (cause) {
      saveError = cause instanceof Error ? cause.message : 'Failed to save sync';
    } finally {
      saving = false;
    }
  }

  function stepDisabled(target: Step) {
    if (target === 5) {
      return !targetMediaSetRid.trim();
    }
    return false;
  }
</script>

<div class="wizard" data-testid="media-set-sync-wizard" role="dialog" aria-modal="true">
  <header>
    <h2>Media set sync · {sourceName}</h2>
    <button type="button" class="link" onclick={onCancel} aria-label="Close wizard">
      ×
    </button>
  </header>

  <ol class="steps">
    {#each [1, 2, 3, 4, 5] as i (i)}
      <li class={step === i ? 'active' : step > i ? 'done' : ''} data-testid={`wizard-step-${i}`}>
        <span>{i}</span>
        {#if i === 1}Type{/if}
        {#if i === 2}File types{/if}
        {#if i === 3}Schedule{/if}
        {#if i === 4}Subfolder{/if}
        {#if i === 5}Filters{/if}
      </li>
    {/each}
  </ol>

  <div class="body">
    {#if step === 1}
      <!-- ── Step 1: Sync flavour ─────────────────────────────── -->
      <fieldset>
        <legend>How should bytes be handled?</legend>
        <label class={`option ${kind === 'MEDIA_SET_SYNC' ? 'selected' : ''}`}>
          <input
            type="radio"
            name="kind"
            value="MEDIA_SET_SYNC"
            checked={kind === 'MEDIA_SET_SYNC'}
            data-testid="wizard-kind-MEDIA_SET_SYNC"
            onchange={() => (kind = 'MEDIA_SET_SYNC')}
          />
          <span>
            <strong>Media set sync</strong>
            Copies files into Foundry storage. Items survive deletes in the source.
          </span>
        </label>
        <label class={`option ${kind === 'VIRTUAL_MEDIA_SET_SYNC' ? 'selected' : ''}`}>
          <input
            type="radio"
            name="kind"
            value="VIRTUAL_MEDIA_SET_SYNC"
            checked={kind === 'VIRTUAL_MEDIA_SET_SYNC'}
            data-testid="wizard-kind-VIRTUAL_MEDIA_SET_SYNC"
            onchange={() => (kind = 'VIRTUAL_MEDIA_SET_SYNC')}
          />
          <span>
            <strong>Virtual media set sync</strong>
            Only metadata is registered. Bytes stay in the source. Per Foundry
            "Virtual media sets" — no awareness of external deletions.
          </span>
        </label>
      </fieldset>
    {:else if step === 2}
      <!-- ── Step 2: File types ───────────────────────────────── -->
      <fieldset>
        <legend>Schema</legend>
        <div class="schemas">
          {#each MEDIA_SET_SCHEMAS as s (s)}
            <button
              type="button"
              class={`pill ${schema === s ? 'selected' : ''}`}
              data-testid={`wizard-schema-${s}`}
              onclick={() => {
                schema = s;
                mimePrefillFollowingSchema = true;
              }}
            >
              {s}
            </button>
          {/each}
        </div>
      </fieldset>
      <fieldset>
        <legend>Allowed MIME types</legend>
        <div class="mime-grid">
          {#each DEFAULT_MIME_TYPES[schema] as mime (mime)}
            <label class="checkbox">
              <input
                type="checkbox"
                checked={allowedMimeTypes.includes(mime)}
                onchange={(event) =>
                  toggleMime(mime, (event.currentTarget as HTMLInputElement).checked)}
              />
              <span>{mime}</span>
            </label>
          {/each}
        </div>
      </fieldset>
    {:else if step === 3}
      <!-- ── Step 3: Schedule ────────────────────────────────── -->
      <ScheduleConfig
        config={scheduleConfig}
        onChange={(next) => (scheduleConfig = next)}
      />
      <p class="hint">
        Leave disabled to run the sync only when an operator clicks <strong>Run</strong>
        on the source page.
      </p>
    {:else if step === 4}
      <!-- ── Step 4: Subfolder ───────────────────────────────── -->
      <fieldset>
        <legend>Subfolder inside the source</legend>
        <input
          type="text"
          value={subfolder}
          placeholder="Leave empty to sync from the bucket root"
          data-testid="wizard-subfolder"
          oninput={(event) =>
            (subfolder = (event.currentTarget as HTMLInputElement).value)}
        />
        <p class="hint">
          Use a slash-delimited path (e.g. <code>screenshots/2026/</code>). The
          configured subfolder is the prefix for every Path matches glob in
          step 5.
        </p>
      </fieldset>
    {:else}
      <!-- ── Step 5: Filters + target ────────────────────────── -->
      <fieldset>
        <legend>Sync filters</legend>
        <label class="checkbox">
          <input
            type="checkbox"
            bind:checked={excludeAlreadySynced}
            data-testid="wizard-filter-exclude-synced"
          />
          <span>Exclude files already synced</span>
        </label>
        <label>
          <span>Path matches (glob)</span>
          <input
            type="text"
            placeholder="e.g. **/*.png"
            value={pathGlob}
            data-testid="wizard-filter-path-glob"
            oninput={(event) =>
              (pathGlob = (event.currentTarget as HTMLInputElement).value)}
          />
        </label>
        <label>
          <span>File size limit (MB)</span>
          <input
            type="number"
            min="0"
            step="1"
            value={fileSizeLimitMb ?? ''}
            placeholder="No limit"
            data-testid="wizard-filter-size-limit"
            onchange={(event) => {
              const v = Number((event.currentTarget as HTMLInputElement).value);
              fileSizeLimitMb = Number.isFinite(v) && v > 0 ? v : null;
            }}
          />
        </label>
        <label class="checkbox">
          <input
            type="checkbox"
            bind:checked={ignoreUnmatchedSchema}
            data-testid="wizard-filter-ignore-schema"
          />
          <span>Ignore items not matching schema (silently skip vs surface as error)</span>
        </label>
      </fieldset>

      <fieldset>
        <legend>Target media set</legend>
        <label>
          <span>Existing media set RID</span>
          <input
            type="text"
            value={targetMediaSetRid}
            placeholder="ri.foundry.main.media_set.…"
            data-testid="wizard-target-rid"
            oninput={(event) =>
              (targetMediaSetRid = (event.currentTarget as HTMLInputElement).value)}
          />
        </label>
        {#if !targetMediaSetRid.trim() && !createInline}
          <button
            type="button"
            class="link"
            data-testid="wizard-open-create-inline"
            onclick={() => {
              createInlineName = createInlineName || `${kind === 'VIRTUAL_MEDIA_SET_SYNC' ? 'Virtual ' : ''}${schema.toLowerCase()} sync target`;
              createInline = true;
            }}
          >
            + Create new media set
          </button>
        {/if}
        {#if createInline}
          <div class="inline-create" data-testid="wizard-create-inline">
            <h4>Create media set</h4>
            <label>
              <span>Name</span>
              <input
                type="text"
                bind:value={createInlineName}
                data-testid="wizard-create-inline-name"
              />
            </label>
            <label>
              <span>Project RID</span>
              <input
                type="text"
                bind:value={createInlineProjectRid}
                data-testid="wizard-create-inline-project"
              />
            </label>
            {#if createInlineError}
              <p class="error">{createInlineError}</p>
            {/if}
            <div class="row">
              <button
                type="button"
                class="primary"
                disabled={createInlineBusy}
                onclick={createInlineSet}
                data-testid="wizard-create-inline-submit"
              >
                {createInlineBusy ? 'Creating…' : 'Create'}
              </button>
              <button type="button" class="link" onclick={() => (createInline = false)}>
                Cancel
              </button>
            </div>
            <p class="hint">
              The new media set is created with the schema and MIME types you picked
              in step 2. Virtual sync? It will be linked to this source automatically.
            </p>
          </div>
        {/if}
        {#if lastCreatedSet}
          <p class="hint">
            Linked to freshly-created
            <a href={`/media-sets/${encodeURIComponent(lastCreatedSet.rid)}`}>
              {lastCreatedSet.name}
            </a>.
          </p>
        {/if}
      </fieldset>

      {#if saveError}
        <p class="error" role="alert">{saveError}</p>
      {/if}
    {/if}
  </div>

  <footer>
    {#if step > 1}
      <button type="button" class="secondary" onclick={back}>Back</button>
    {/if}
    <button type="button" class="link" onclick={onCancel}>Cancel</button>
    {#if step < 5}
      <button
        type="button"
        class="primary"
        onclick={next}
        data-testid="wizard-next"
      >
        Next
      </button>
    {:else}
      <button
        type="button"
        class="primary"
        onclick={save}
        disabled={saving || stepDisabled(5)}
        data-testid="wizard-save"
      >
        {saving ? 'Saving…' : 'Save media set sync'}
      </button>
    {/if}
  </footer>
</div>

<style>
  .wizard {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 16px;
    background: #0b1220;
    border: 1px solid #1f2937;
    border-radius: 12px;
    color: #e2e8f0;
    width: min(680px, 96vw);
    /*
     * The backdrop (parent) carries `overflow-y: auto`, so we let the
     * wizard grow naturally and the backdrop's scroll handles
     * overflow. Capping the wizard's own height pinned the footer
     * outside the viewport when the body grew (inline-create panel),
     * which made Playwright's `scrollIntoViewIfNeeded` ineffective.
     */
  }
  header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  header h2 {
    margin: 0;
    font-size: 16px;
  }
  .steps {
    display: flex;
    gap: 8px;
    list-style: none;
    margin: 0;
    padding: 0;
    flex-wrap: wrap;
  }
  .steps li {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 11px;
    color: #94a3b8;
    padding: 4px 10px;
    border-radius: 999px;
    background: #1e293b;
  }
  .steps li span {
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: #334155;
    text-align: center;
    line-height: 18px;
    font-weight: 600;
    color: #e2e8f0;
  }
  .steps li.active {
    background: #1d4ed8;
    color: #f1f5f9;
  }
  .steps li.active span {
    background: #2563eb;
  }
  .steps li.done span {
    background: #047857;
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: 14px;
    min-height: 220px;
  }
  fieldset {
    border: 1px solid #1f2937;
    border-radius: 8px;
    padding: 10px 12px;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  legend {
    font-size: 11px;
    color: #94a3b8;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding: 0 4px;
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 12px;
    color: #cbd5e1;
  }
  label.checkbox {
    flex-direction: row;
    align-items: center;
    gap: 8px;
  }
  input[type='text'],
  input[type='number'] {
    background: #1e293b;
    color: #f1f5f9;
    border: 1px solid #334155;
    border-radius: 4px;
    padding: 6px 8px;
    font: inherit;
  }
  .option {
    flex-direction: row;
    align-items: flex-start;
    gap: 10px;
    padding: 8px;
    border: 1px solid #334155;
    border-radius: 8px;
    cursor: pointer;
  }
  .option.selected {
    border-color: #1d4ed8;
    background: rgba(29, 78, 216, 0.15);
  }
  .option strong {
    display: block;
    color: #f1f5f9;
    margin-bottom: 2px;
  }
  .schemas {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .pill {
    background: #1e293b;
    color: #cbd5e1;
    border: 1px solid #334155;
    border-radius: 999px;
    padding: 4px 12px;
    font-size: 12px;
    cursor: pointer;
    font: inherit;
  }
  .pill.selected {
    background: #1d4ed8;
    color: #f1f5f9;
    border-color: #2563eb;
  }
  .mime-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 4px;
  }
  .inline-create {
    border: 1px dashed #334155;
    border-radius: 8px;
    padding: 10px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .inline-create h4 {
    margin: 0;
    font-size: 12px;
    color: #f1f5f9;
  }
  .row {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  footer {
    display: flex;
    justify-content: flex-end;
    gap: 6px;
  }
  .primary {
    background: #1d4ed8;
    color: #f1f5f9;
    border: 1px solid #1e40af;
    border-radius: 6px;
    padding: 6px 14px;
    cursor: pointer;
    font: inherit;
    font-weight: 600;
    font-size: 13px;
  }
  .primary:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .secondary {
    background: #1e293b;
    color: #e2e8f0;
    border: 1px solid #334155;
    border-radius: 6px;
    padding: 6px 14px;
    cursor: pointer;
    font: inherit;
    font-size: 13px;
  }
  .link {
    background: transparent;
    border: none;
    color: #60a5fa;
    cursor: pointer;
    font: inherit;
  }
  .hint {
    color: #94a3b8;
    font-size: 11px;
    font-style: italic;
    margin: 0;
  }
  .error {
    color: #fca5a5;
    font-size: 12px;
    margin: 0;
  }
  code {
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
    font-size: 11px;
  }
</style>
