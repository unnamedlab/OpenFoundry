<script lang="ts">
  import { onMount } from 'svelte';

  import { listProjects, type OntologyProject } from '$lib/api/ontology';
  import { listStreams, type StreamDefinition } from '$lib/api/streaming';
  import dataConnection from '$lib/api/data-connection';
  import {
    createPipeline,
    type ExternalConfig,
    type IncrementalConfig,
    type PipelineType,
    type StreamingConfig,
  } from '$lib/api/pipelines';

  type Step = 1 | 2 | 3;

  interface Props {
    open: boolean;
    onClose: () => void;
    onCreated: (pipelineId: string) => void;
  }

  const { open, onClose, onCreated }: Props = $props();

  type TypeCard = {
    id: PipelineType;
    title: string;
    summary: string;
    latency: string;
    complexity: string;
    compute: string;
    resilience: string;
  };

  const TYPE_CARDS: TypeCard[] = [
    {
      id: 'BATCH',
      title: 'Batch',
      summary: 'Recompute every dataset on each run. Default for small to medium scale.',
      latency: 'High',
      complexity: 'Low',
      compute: 'Medium',
      resilience: 'Low',
    },
    {
      id: 'FASTER',
      title: 'Faster',
      summary: 'DataFusion-backed batch/incremental for small-to-medium datasets without Spark.',
      latency: 'Medium',
      complexity: 'Low',
      compute: 'Low–Medium',
      resilience: 'Low',
    },
    {
      id: 'INCREMENTAL',
      title: 'Incremental',
      summary: 'Process only the rows that changed since the last build.',
      latency: 'Low',
      complexity: 'Medium',
      compute: 'Low',
      resilience: 'High',
    },
    {
      id: 'STREAMING',
      title: 'Streaming',
      summary: 'Run continuously over an upstream stream. Lowest latency, highest cost.',
      latency: 'Very low',
      complexity: 'High',
      compute: 'High',
      resilience: 'High',
    },
    {
      id: 'EXTERNAL',
      title: 'External',
      summary: 'Push compute down to Databricks or Snowflake via virtual tables.',
      latency: 'Variable',
      complexity: 'Medium',
      compute: 'Pushdown',
      resilience: 'High',
    },
  ];

  let step = $state<Step>(1);
  let pipelineType = $state<PipelineType | null>(null);
  let name = $state('');
  let description = $state('');
  let projectId = $state<string | null>(null);
  let projects = $state<OntologyProject[]>([]);
  let projectsLoaded = $state(false);
  let projectsLoading = $state(false);
  let projectFilter = $state('');

  let streams = $state<StreamDefinition[]>([]);
  let streamsLoaded = $state(false);
  let streamsLoading = $state(false);
  let inputStreamId = $state<string | null>(null);
  let parallelism = $state<number>(1);

  let sources = $state<Array<{ id: string; name: string; connector_type: string }>>([]);
  let sourcesLoaded = $state(false);
  let sourcesLoading = $state(false);
  let externalSystem = $state<'databricks' | 'snowflake'>('databricks');
  let externalSourceId = $state<string | null>(null);
  let computeProfileId = $state('');

  let replayOnDeploy = $state(false);
  let watermarkColumns = $state('');
  let allowedTransactionTypes = $state('APPEND,UPDATE');

  let saving = $state(false);
  let error = $state('');

  // Personal-folder filter — Foundry doc: "pipelines cannot be saved in
  // personal folders". A null/empty workspace_slug or one whose slug
  // starts with "personal" is treated as a personal location.
  const visibleProjects = $derived.by(() => {
    const term = projectFilter.trim().toLowerCase();
    return projects
      .filter((p) => {
        const slug = (p.workspace_slug ?? '').toLowerCase();
        return slug !== '' && !slug.startsWith('personal');
      })
      .filter((p) => {
        if (!term) return true;
        return [p.display_name, p.slug, p.description ?? '', p.workspace_slug ?? ''].some(
          (value) => value.toLowerCase().includes(term),
        );
      });
  });

  const filteredSources = $derived.by(() =>
    sources.filter((s) => s.connector_type === externalSystem),
  );

  const canContinueFromStep1 = $derived(pipelineType !== null);
  const canContinueFromStep2 = $derived(
    name.trim().length > 0 && projectId !== null && pipelineType !== null,
  );
  const canCreate = $derived.by(() => {
    if (!canContinueFromStep2) return false;
    if (pipelineType === 'STREAMING') return inputStreamId !== null;
    if (pipelineType === 'EXTERNAL') return externalSourceId !== null;
    return true;
  });

  $effect(() => {
    if (open && !projectsLoaded && !projectsLoading) {
      void loadProjects();
    }
  });

  $effect(() => {
    if (open && step === 3 && pipelineType === 'STREAMING' && !streamsLoaded && !streamsLoading) {
      void loadStreams();
    }
  });

  $effect(() => {
    if (open && step === 3 && pipelineType === 'EXTERNAL' && !sourcesLoaded && !sourcesLoading) {
      void loadSources();
    }
  });

  async function loadProjects() {
    projectsLoading = true;
    try {
      const response = await listProjects({ per_page: 100 });
      projects = response.data;
      projectsLoaded = true;
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to load projects';
    } finally {
      projectsLoading = false;
    }
  }

  async function loadStreams() {
    streamsLoading = true;
    try {
      const response = await listStreams();
      streams = response.data;
      streamsLoaded = true;
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to load streams';
    } finally {
      streamsLoading = false;
    }
  }

  async function loadSources() {
    sourcesLoading = true;
    try {
      const response = await dataConnection.listSources({ per_page: 100 });
      sources = response.data;
      sourcesLoaded = true;
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to load sources';
    } finally {
      sourcesLoading = false;
    }
  }

  function selectType(id: PipelineType) {
    pipelineType = id;
  }

  function goNext() {
    error = '';
    if (step === 1 && canContinueFromStep1) step = 2;
    else if (step === 2 && canContinueFromStep2) step = 3;
  }

  function goBack() {
    error = '';
    if (step === 3) step = 2;
    else if (step === 2) step = 1;
  }

  function reset() {
    step = 1;
    pipelineType = null;
    name = '';
    description = '';
    projectId = null;
    projectFilter = '';
    inputStreamId = null;
    parallelism = 1;
    externalSystem = 'databricks';
    externalSourceId = null;
    computeProfileId = '';
    replayOnDeploy = false;
    watermarkColumns = '';
    allowedTransactionTypes = 'APPEND,UPDATE';
    error = '';
  }

  async function handleCreate() {
    if (!canCreate || pipelineType === null || projectId === null) return;
    saving = true;
    error = '';
    try {
      const starterNode = {
        id: 'source_data',
        label: 'Imported data',
        transform_type: 'passthrough',
        config: {},
        depends_on: [],
        input_dataset_ids: [],
        output_dataset_id: null,
      };
      const body: Parameters<typeof createPipeline>[0] = {
        name: name.trim(),
        description: description.trim() || undefined,
        nodes: [starterNode],
        pipeline_type: pipelineType,
        project_id: projectId,
      };

      if (pipelineType === 'STREAMING' && inputStreamId) {
        body.streaming = {
          input_stream_id: inputStreamId,
          parallelism: Math.max(1, parallelism),
        } satisfies StreamingConfig;
      }
      if (pipelineType === 'EXTERNAL' && externalSourceId) {
        body.external = {
          source_system: externalSystem,
          source_id: externalSourceId,
          compute_profile_id: computeProfileId.trim() || null,
        } satisfies ExternalConfig;
      }
      if (pipelineType === 'INCREMENTAL') {
        body.incremental = {
          replay_on_deploy: replayOnDeploy,
          watermark_columns: watermarkColumns
            .split(',')
            .map((s) => s.trim())
            .filter(Boolean),
          allowed_transaction_types: allowedTransactionTypes.trim() || 'APPEND,UPDATE',
        } satisfies IncrementalConfig;
      }
      const created = await createPipeline(body);
      onCreated(created.id);
      reset();
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to create pipeline';
    } finally {
      saving = false;
    }
  }

  function handleClose() {
    if (saving) return;
    reset();
    onClose();
  }

  function onKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape' && open) handleClose();
  }

  onMount(() => {
    window.addEventListener('keydown', onKeydown);
    return () => window.removeEventListener('keydown', onKeydown);
  });
</script>

{#if open}
  <div
    class="cpm-backdrop"
    role="dialog"
    aria-modal="true"
    aria-labelledby="cpm-title"
    onclick={handleClose}
  >
    <div class="cpm-shell" role="document" onclick={(event) => event.stopPropagation()}>
      <header class="cpm-header">
        <div>
          <div class="cpm-eyebrow">Pipeline Builder</div>
          <h2 id="cpm-title" class="cpm-title">Create new pipeline</h2>
        </div>
        <button type="button" class="cpm-close" aria-label="Close" onclick={handleClose}>×</button>
      </header>

      <ol class="cpm-steps" aria-label="Wizard steps">
        <li class:active={step === 1} class:done={step > 1}>1. Type</li>
        <li class:active={step === 2} class:done={step > 2}>2. Location &amp; name</li>
        <li class:active={step === 3}>3. Configuration</li>
      </ol>

      {#if error}
        <div class="cpm-error" role="alert">{error}</div>
      {/if}

      <section class="cpm-body">
        {#if step === 1}
          <p class="cpm-help">
            Choose the kind of pipeline you want to author. You can switch later, but the type
            constrains which transforms and runtimes are available.
          </p>
          <div class="cpm-type-grid" data-testid="cpm-type-grid">
            {#each TYPE_CARDS as card (card.id)}
              <button
                type="button"
                class="cpm-type-card"
                class:selected={pipelineType === card.id}
                data-testid={`cpm-type-${card.id.toLowerCase()}`}
                onclick={() => selectType(card.id)}
              >
                <div class="cpm-type-title">{card.title}</div>
                <div class="cpm-type-summary">{card.summary}</div>
                <ul class="cpm-type-meta">
                  <li><span>Latency</span><strong>{card.latency}</strong></li>
                  <li><span>Complexity</span><strong>{card.complexity}</strong></li>
                  <li><span>Compute</span><strong>{card.compute}</strong></li>
                  <li><span>Resilience</span><strong>{card.resilience}</strong></li>
                </ul>
              </button>
            {/each}
          </div>
        {:else if step === 2}
          <p class="cpm-help">
            Pick a project to host the pipeline. <strong>Pipelines cannot be saved in personal folders.</strong>
          </p>
          <label class="cpm-field">
            <span>Pipeline name</span>
            <input
              type="text"
              data-testid="cpm-name"
              bind:value={name}
              placeholder="Orders Pipeline"
            />
          </label>
          <label class="cpm-field">
            <span>Description (optional)</span>
            <input
              type="text"
              data-testid="cpm-description"
              bind:value={description}
              placeholder="Short summary"
            />
          </label>

          <div class="cpm-project-search">
            <input
              type="search"
              data-testid="cpm-project-search"
              bind:value={projectFilter}
              placeholder="Search projects"
            />
          </div>

          {#if projectsLoading}
            <div class="cpm-empty">Loading projects…</div>
          {:else if visibleProjects.length === 0}
            <div class="cpm-empty">
              No shared projects available — pipelines cannot be saved in personal folders.
            </div>
          {:else}
            <ul class="cpm-project-list" data-testid="cpm-project-list">
              {#each visibleProjects as project (project.id)}
                <li>
                  <button
                    type="button"
                    class="cpm-project"
                    class:selected={projectId === project.id}
                    data-testid={`cpm-project-${project.slug}`}
                    onclick={() => (projectId = project.id)}
                  >
                    <div class="cpm-project-name">{project.display_name}</div>
                    <div class="cpm-project-slug">
                      {project.workspace_slug ?? '—'} · {project.slug}
                    </div>
                    {#if project.description}
                      <div class="cpm-project-desc">{project.description}</div>
                    {/if}
                  </button>
                </li>
              {/each}
            </ul>
          {/if}
        {:else}
          {#if pipelineType === 'STREAMING'}
            <p class="cpm-help">
              Streaming pipelines require an input stream — that is the source of records the
              pipeline consumes continuously.
            </p>
            {#if streamsLoading}
              <div class="cpm-empty">Loading streams…</div>
            {:else if streams.length === 0}
              <div class="cpm-empty">
                No streams available. Create one under <a href="/streaming">Streaming</a> first.
              </div>
            {:else}
              <label class="cpm-field">
                <span>Input stream</span>
                <select
                  data-testid="cpm-stream"
                  bind:value={inputStreamId}
                >
                  <option value={null}>Select a stream…</option>
                  {#each streams as stream (stream.id)}
                    <option value={stream.id}>{stream.name}</option>
                  {/each}
                </select>
              </label>
              <label class="cpm-field">
                <span>Parallelism</span>
                <input
                  type="number"
                  min="1"
                  data-testid="cpm-parallelism"
                  bind:value={parallelism}
                />
              </label>
            {/if}
          {:else if pipelineType === 'EXTERNAL'}
            <p class="cpm-help">
              External pipelines push compute down to Databricks or Snowflake via virtual tables.
              No LLM and no media transforms are allowed.
            </p>
            <fieldset class="cpm-radio-group">
              <legend>Source system</legend>
              <label>
                <input
                  type="radio"
                  name="cpm-system"
                  value="databricks"
                  data-testid="cpm-system-databricks"
                  bind:group={externalSystem}
                />
                Databricks
              </label>
              <label>
                <input
                  type="radio"
                  name="cpm-system"
                  value="snowflake"
                  data-testid="cpm-system-snowflake"
                  bind:group={externalSystem}
                />
                Snowflake
              </label>
            </fieldset>

            {#if sourcesLoading}
              <div class="cpm-empty">Loading sources…</div>
            {:else if filteredSources.length === 0}
              <div class="cpm-empty">
                No {externalSystem} sources registered. Create one under
                <a href="/data-connection">Data Connection</a> first.
              </div>
            {:else}
              <label class="cpm-field">
                <span>Source</span>
                <select
                  data-testid="cpm-source"
                  bind:value={externalSourceId}
                >
                  <option value={null}>Select a source…</option>
                  {#each filteredSources as source (source.id)}
                    <option value={source.id}>{source.name}</option>
                  {/each}
                </select>
              </label>
            {/if}

            <label class="cpm-field">
              <span>Compute profile (optional)</span>
              <input
                type="text"
                data-testid="cpm-compute-profile"
                bind:value={computeProfileId}
                placeholder="warehouse-xs"
              />
            </label>
          {:else if pipelineType === 'INCREMENTAL'}
            <p class="cpm-help">
              Incremental pipelines process only rows that changed since the last build. Configure
              how the runner detects new data and whether the next deploy should replay history.
            </p>
            <label class="cpm-checkbox">
              <input
                type="checkbox"
                data-testid="cpm-replay"
                bind:checked={replayOnDeploy}
              />
              <span>Replay on deploy</span>
            </label>
            <label class="cpm-field">
              <span>Watermark columns (comma-separated)</span>
              <input
                type="text"
                data-testid="cpm-watermark"
                bind:value={watermarkColumns}
                placeholder="event_ts"
              />
            </label>
            <label class="cpm-field">
              <span>Allowed transaction types</span>
              <input
                type="text"
                data-testid="cpm-tx-types"
                bind:value={allowedTransactionTypes}
                placeholder="APPEND,UPDATE"
              />
            </label>
          {:else}
            <p class="cpm-help">
              {pipelineType === 'FASTER'
                ? 'Faster pipelines run on DataFusion. No additional configuration is required at creation time.'
                : 'No additional configuration is required at creation time. You can add transforms in the canvas.'}
            </p>
            <dl class="cpm-summary">
              <div><dt>Type</dt><dd>{pipelineType}</dd></div>
              <div><dt>Name</dt><dd>{name}</dd></div>
              <div><dt>Description</dt><dd>{description || '—'}</dd></div>
            </dl>
          {/if}
        {/if}
      </section>

      <footer class="cpm-footer">
        <button type="button" class="cpm-btn ghost" onclick={handleClose} disabled={saving}>
          Cancel
        </button>
        <div class="cpm-footer-spacer"></div>
        {#if step > 1}
          <button type="button" class="cpm-btn secondary" onclick={goBack} disabled={saving}>
            Back
          </button>
        {/if}
        {#if step < 3}
          <button
            type="button"
            class="cpm-btn primary"
            data-testid="cpm-continue"
            onclick={goNext}
            disabled={(step === 1 && !canContinueFromStep1) || (step === 2 && !canContinueFromStep2)}
          >
            Continue
          </button>
        {:else}
          <button
            type="button"
            class="cpm-btn primary"
            data-testid="cpm-create"
            onclick={handleCreate}
            disabled={!canCreate || saving}
          >
            {saving ? 'Creating…' : 'Create pipeline'}
          </button>
        {/if}
      </footer>
    </div>
  </div>
{/if}

<style>
  .cpm-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(15, 23, 42, 0.55);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 60;
    font-family: var(--font-sans);
  }
  .cpm-shell {
    width: min(720px, 95vw);
    max-height: 92vh;
    background: var(--bg-panel);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-popover);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .cpm-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    padding: 18px 22px 12px;
    border-bottom: 1px solid var(--border-subtle);
  }
  .cpm-eyebrow {
    color: var(--text-muted);
    font-size: 12px;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }
  .cpm-title {
    margin: 4px 0 0;
    color: var(--text-strong);
    font-size: 20px;
    font-weight: 700;
  }
  .cpm-close {
    background: transparent;
    border: 0;
    color: var(--text-muted);
    font-size: 24px;
    line-height: 1;
    padding: 4px 8px;
    border-radius: var(--radius-sm);
  }
  .cpm-close:hover {
    background: var(--bg-hover);
    color: var(--text-strong);
  }
  .cpm-steps {
    list-style: none;
    margin: 0;
    padding: 12px 22px;
    display: flex;
    gap: 14px;
    border-bottom: 1px solid var(--border-subtle);
    background: var(--bg-panel-muted);
  }
  .cpm-steps li {
    color: var(--text-muted);
    font-size: 13px;
    font-weight: 500;
  }
  .cpm-steps li.active {
    color: var(--text-strong);
    font-weight: 700;
  }
  .cpm-steps li.done {
    color: var(--status-success);
  }
  .cpm-error {
    margin: 12px 22px 0;
    padding: 10px 14px;
    border-radius: var(--radius-md);
    background: var(--status-danger-bg);
    color: var(--status-danger);
    font-size: 13px;
  }
  .cpm-body {
    padding: 18px 22px;
    overflow-y: auto;
    flex: 1;
    color: var(--text-default);
  }
  .cpm-help {
    color: var(--text-muted);
    margin: 0 0 14px;
    font-size: 13px;
  }
  .cpm-type-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    gap: 12px;
  }
  .cpm-type-card {
    text-align: left;
    background: var(--bg-panel);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    padding: 14px;
    transition: border-color 0.15s, box-shadow 0.15s;
    cursor: pointer;
    color: var(--text-default);
  }
  .cpm-type-card:hover {
    border-color: var(--border-strong);
  }
  .cpm-type-card.selected {
    border-color: var(--border-focus);
    box-shadow: 0 0 0 3px rgba(63, 123, 224, 0.15);
  }
  .cpm-type-title {
    color: var(--text-strong);
    font-size: 15px;
    font-weight: 600;
  }
  .cpm-type-summary {
    margin-top: 4px;
    font-size: 13px;
    color: var(--text-muted);
    min-height: 36px;
  }
  .cpm-type-meta {
    margin: 10px 0 0;
    padding: 0;
    list-style: none;
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 4px;
    font-size: 12px;
  }
  .cpm-type-meta span {
    color: var(--text-muted);
  }
  .cpm-type-meta strong {
    color: var(--text-strong);
    margin-left: 4px;
    font-weight: 600;
  }
  .cpm-field {
    display: block;
    margin-bottom: 12px;
  }
  .cpm-field span,
  .cpm-checkbox span,
  .cpm-radio-group legend {
    display: block;
    font-size: 12px;
    font-weight: 600;
    color: var(--text-strong);
    margin-bottom: 4px;
  }
  .cpm-field input,
  .cpm-field select,
  .cpm-project-search input {
    width: 100%;
    padding: 8px 12px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    background: var(--bg-input);
    color: var(--text-default);
    font: inherit;
  }
  .cpm-field input:focus,
  .cpm-field select:focus,
  .cpm-project-search input:focus {
    outline: 2px solid var(--border-focus);
    outline-offset: 1px;
  }
  .cpm-project-search {
    margin: 8px 0 12px;
  }
  .cpm-project-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-height: 260px;
    overflow-y: auto;
  }
  .cpm-project {
    width: 100%;
    text-align: left;
    background: var(--bg-panel);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    padding: 10px 12px;
    color: var(--text-default);
    cursor: pointer;
  }
  .cpm-project:hover {
    border-color: var(--border-strong);
  }
  .cpm-project.selected {
    border-color: var(--border-focus);
    background: var(--bg-chip-active);
  }
  .cpm-project-name {
    color: var(--text-strong);
    font-weight: 600;
    font-size: 14px;
  }
  .cpm-project-slug {
    margin-top: 2px;
    font-size: 12px;
    color: var(--text-muted);
  }
  .cpm-project-desc {
    margin-top: 4px;
    font-size: 12px;
    color: var(--text-default);
  }
  .cpm-empty {
    padding: 14px;
    border: 1px dashed var(--border-default);
    border-radius: var(--radius-md);
    background: var(--bg-panel-muted);
    color: var(--text-muted);
    font-size: 13px;
    text-align: center;
  }
  .cpm-radio-group {
    border: 0;
    margin: 0 0 12px;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .cpm-radio-group label {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-default);
    font-size: 13px;
  }
  .cpm-checkbox {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 12px;
    color: var(--text-default);
    font-size: 13px;
  }
  .cpm-checkbox span {
    margin: 0;
  }
  .cpm-summary {
    margin: 0;
    display: grid;
    gap: 8px;
  }
  .cpm-summary div {
    display: grid;
    grid-template-columns: 120px 1fr;
    gap: 8px;
    font-size: 13px;
  }
  .cpm-summary dt {
    color: var(--text-muted);
    font-weight: 600;
  }
  .cpm-summary dd {
    margin: 0;
    color: var(--text-strong);
  }
  .cpm-footer {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 14px 22px;
    border-top: 1px solid var(--border-subtle);
    background: var(--bg-panel-muted);
  }
  .cpm-footer-spacer {
    flex: 1;
  }
  .cpm-btn {
    padding: 8px 14px;
    border-radius: var(--radius-md);
    border: 1px solid transparent;
    font: inherit;
    font-weight: 600;
    font-size: 13px;
  }
  .cpm-btn.primary {
    background: var(--bg-chip-strong);
    color: var(--text-inverse);
    border-color: var(--bg-chip-strong);
  }
  .cpm-btn.primary:hover:not(:disabled) {
    filter: brightness(0.95);
  }
  .cpm-btn.secondary {
    background: var(--bg-panel);
    color: var(--text-default);
    border-color: var(--border-default);
  }
  .cpm-btn.secondary:hover:not(:disabled) {
    background: var(--bg-hover);
  }
  .cpm-btn.ghost {
    background: transparent;
    color: var(--text-muted);
    border-color: transparent;
  }
  .cpm-btn:disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }
</style>
