<script lang="ts">
  /**
   * 5-step wizard for `POST /v1/sources/{rid}/auto-registration`.
   *
   * Foundry doc § "Auto-registration" (img_005, img_006). The
   * Databricks tag-filter step is gated on provider so non-Databricks
   * sources skip it (the doc only blesses tag filtering for
   * Databricks).
   */
  import {
    virtualTables,
    type EnableAutoRegistrationRequest,
    type FolderMirrorKind,
    type VirtualTableProvider,
    type VirtualTableSourceLink,
  } from '$lib/api/virtual-tables';

  type Props = {
    open: boolean;
    sourceRid: string;
    provider: VirtualTableProvider;
    onClose: () => void;
    onEnabled?: (link: VirtualTableSourceLink) => void;
  };

  let { open, sourceRid, provider, onClose, onEnabled }: Props = $props();

  type StepId = 'project' | 'layout' | 'tags' | 'interval' | 'review';

  const steps: StepId[] = $derived(
    provider === 'DATABRICKS'
      ? (['project', 'layout', 'tags', 'interval', 'review'] as const).slice()
      : (['project', 'layout', 'interval', 'review'] as const).slice(),
  );

  let stepIndex = $state(0);
  let projectName = $state('');
  let layout = $state<FolderMirrorKind>('NESTED');
  let tagFilters = $state<string[]>([]);
  let tagDraft = $state('');
  let pollIntervalSeconds = $state(3600);
  let busy = $state(false);
  let error = $state('');

  const POLL_OPTIONS = [
    { label: '15 minutes', value: 900 },
    { label: '1 hour', value: 3600 },
    { label: '6 hours', value: 21600 },
    { label: '24 hours', value: 86400 },
  ];

  function currentStep(): StepId {
    return steps[Math.min(stepIndex, steps.length - 1)] ?? 'review';
  }

  function next() {
    if (stepIndex < steps.length - 1) stepIndex += 1;
  }

  function back() {
    if (stepIndex > 0) stepIndex -= 1;
  }

  function addTag() {
    const t = tagDraft.trim();
    if (!t) return;
    if (!tagFilters.includes(t)) tagFilters = [...tagFilters, t];
    tagDraft = '';
  }

  function removeTag(tag: string) {
    tagFilters = tagFilters.filter((t) => t !== tag);
  }

  function isStepValid(): boolean {
    switch (currentStep()) {
      case 'project':
        return projectName.trim().length > 0;
      case 'layout':
        return layout === 'FLAT' || layout === 'NESTED';
      case 'tags':
        return true; // tags optional
      case 'interval':
        return pollIntervalSeconds >= 60;
      case 'review':
        return true;
    }
  }

  async function submit() {
    busy = true;
    error = '';
    const body: EnableAutoRegistrationRequest = {
      project_name: projectName.trim(),
      folder_mirror_kind: layout,
      table_tag_filters: provider === 'DATABRICKS' ? tagFilters : [],
      poll_interval_seconds: pollIntervalSeconds,
    };
    try {
      const link = await virtualTables.enableAutoRegistration(sourceRid, body);
      onEnabled?.(link);
      onClose();
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to enable auto-registration';
    } finally {
      busy = false;
    }
  }
</script>

{#if open}
  <div class="backdrop" role="dialog" aria-modal="true" data-testid="vt-auto-reg-wizard">
    <div class="modal">
      <header>
        <h3>Enable auto-registration</h3>
        <p class="muted">
          Step {stepIndex + 1} of {steps.length} ·
          <span class="step-name">{currentStep().replace('-', ' ')}</span>
        </p>
        <ol class="stepper">
          {#each steps as id, i (id)}
            <li class={i === stepIndex ? 'active' : i < stepIndex ? 'done' : ''}>
              {id}
            </li>
          {/each}
        </ol>
      </header>

      {#if currentStep() === 'project'}
        <section>
          <label>
            <span>Foundry-managed project name</span>
            <input
              type="text"
              bind:value={projectName}
              placeholder="warehouse mirror"
              data-testid="vt-auto-reg-project-name"
            />
          </label>
          <p class="hint">
            A new project will be created and managed by
            <code>virtual-table-service</code>. Users cannot create or
            modify resources in it directly.
          </p>
        </section>
      {:else if currentStep() === 'layout'}
        <section class="layout-pick">
          <label class="radio" data-testid="vt-auto-reg-layout-nested">
            <input type="radio" bind:group={layout} value={'NESTED'} />
            <div>
              <strong>Nested</strong>
              <p class="hint">
                <code>{'<project>/<database>/<schema>/<table>'}</code>
              </p>
              <pre class="preview">main
└── public
    └── orders</pre>
            </div>
          </label>
          <label class="radio" data-testid="vt-auto-reg-layout-flat">
            <input type="radio" bind:group={layout} value={'FLAT'} />
            <div>
              <strong>Flat</strong>
              <p class="hint">
                <code>{'<project>/<database>__<schema>__<table>'}</code>
              </p>
              <pre class="preview">main__public__orders</pre>
            </div>
          </label>
        </section>
      {:else if currentStep() === 'tags'}
        <section>
          <p class="hint">
            Only Databricks tables tagged with at least one of these
            tags will be auto-registered. Reads
            <code>INFORMATION_SCHEMA.TABLE_TAGS</code> on the
            workspace metastore.
          </p>
          <div class="tag-input">
            <input
              type="text"
              placeholder="gold, pii, certified…"
              bind:value={tagDraft}
              onkeydown={(e) => e.key === 'Enter' && (e.preventDefault(), addTag())}
              data-testid="vt-auto-reg-tag-input"
            />
            <button type="button" onclick={addTag}>Add</button>
          </div>
          <ul class="chips" data-testid="vt-auto-reg-tag-chips">
            {#each tagFilters as tag (tag)}
              <li class="chip">
                {tag}
                <button type="button" onclick={() => removeTag(tag)} aria-label={`Remove ${tag}`}>
                  ✕
                </button>
              </li>
            {/each}
          </ul>
        </section>
      {:else if currentStep() === 'interval'}
        <section>
          <p class="hint">
            How often the scanner re-runs against the source. Per
            Foundry doc the recommendation is 1 hour — too short
            costs compute, too long delays new-table discovery.
          </p>
          <div class="interval-row">
            {#each POLL_OPTIONS as opt (opt.value)}
              <label class="radio">
                <input
                  type="radio"
                  bind:group={pollIntervalSeconds}
                  value={opt.value}
                />
                {opt.label}
              </label>
            {/each}
            <label class="radio custom">
              <input
                type="number"
                min="60"
                bind:value={pollIntervalSeconds}
                data-testid="vt-auto-reg-interval-custom"
              />
              <span>seconds</span>
            </label>
          </div>
        </section>
      {:else if currentStep() === 'review'}
        <section class="review">
          <dl>
            <dt>Project</dt>
            <dd>{projectName}</dd>
            <dt>Layout</dt>
            <dd>{layout}</dd>
            {#if provider === 'DATABRICKS'}
              <dt>Tag filters</dt>
              <dd>
                {#if tagFilters.length === 0}
                  <span class="muted">none — every accessible table is mirrored</span>
                {:else}
                  {tagFilters.join(', ')}
                {/if}
              </dd>
            {/if}
            <dt>Poll interval</dt>
            <dd>{pollIntervalSeconds} seconds</dd>
          </dl>
        </section>
      {/if}

      {#if error}
        <div class="error" role="alert">{error}</div>
      {/if}

      <footer>
        <button type="button" onclick={onClose} disabled={busy}>Cancel</button>
        {#if stepIndex > 0}
          <button type="button" onclick={back} disabled={busy}>Back</button>
        {/if}
        {#if stepIndex < steps.length - 1}
          <button
            type="button"
            class="primary"
            onclick={next}
            disabled={!isStepValid() || busy}
            data-testid="vt-auto-reg-next"
          >
            Next
          </button>
        {:else}
          <button
            type="button"
            class="primary"
            onclick={submit}
            disabled={busy}
            data-testid="vt-auto-reg-submit"
          >
            {busy ? 'Enabling…' : 'Enable'}
          </button>
        {/if}
      </footer>
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.4);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
  }
  .modal {
    background: var(--color-bg-elevated, #fff);
    border-radius: 0.5rem;
    padding: 1.25rem;
    width: min(620px, 92vw);
    max-height: 92vh;
    overflow: auto;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  header h3 {
    margin: 0 0 0.25rem;
  }
  .stepper {
    display: flex;
    gap: 0.5rem;
    list-style: none;
    padding: 0;
    margin: 0.5rem 0 0;
    font-size: 0.625rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .stepper li {
    padding: 0.125rem 0.5rem;
    border-radius: 0.25rem;
    background: var(--color-bg-subtle, #f3f4f6);
    color: var(--color-fg-muted, #6b7280);
  }
  .stepper li.active {
    background: #1d4ed8;
    color: #fff;
  }
  .stepper li.done {
    background: #d1fae5;
    color: #065f46;
  }
  .step-name {
    text-transform: capitalize;
  }
  .hint {
    margin: 0.25rem 0 0;
    color: var(--color-fg-muted, #4b5563);
    font-size: 0.75rem;
  }
  .layout-pick {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0.75rem;
  }
  .radio {
    display: flex;
    gap: 0.5rem;
    align-items: flex-start;
    padding: 0.5rem;
    border: 1px solid var(--color-border, #e5e7eb);
    border-radius: 0.5rem;
    cursor: pointer;
    font-size: 0.875rem;
  }
  .radio input[type='radio'] {
    margin-top: 0.25rem;
  }
  .radio.custom {
    flex-direction: row;
    align-items: center;
  }
  .radio.custom input[type='number'] {
    width: 8rem;
    padding: 0.25rem 0.5rem;
    border: 1px solid var(--color-border, #d1d5db);
    border-radius: 0.25rem;
  }
  .preview {
    margin: 0.25rem 0 0;
    padding: 0.5rem;
    background: var(--color-bg-subtle, #f9fafb);
    border-radius: 0.25rem;
    font-family: ui-monospace, SFMono-Regular, monospace;
    font-size: 0.75rem;
  }
  .tag-input {
    display: flex;
    gap: 0.5rem;
  }
  .tag-input input {
    flex: 1;
    padding: 0.375rem 0.5rem;
    border: 1px solid var(--color-border, #d1d5db);
    border-radius: 0.25rem;
    font-size: 0.875rem;
  }
  .tag-input button {
    padding: 0.375rem 0.75rem;
    border-radius: 0.25rem;
    border: 1px solid var(--color-border, #d1d5db);
    background: var(--color-bg-elevated, #fff);
    font-size: 0.875rem;
    cursor: pointer;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
    list-style: none;
    padding: 0;
    margin: 0.5rem 0 0;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    padding: 0.0625rem 0.375rem;
    background: var(--color-bg-subtle, #f3f4f6);
    border: 1px solid var(--color-border, #e5e7eb);
    border-radius: 0.25rem;
    font-size: 0.75rem;
  }
  .chip button {
    border: none;
    background: transparent;
    cursor: pointer;
    color: #b91c1c;
    font-size: 0.75rem;
  }
  .interval-row {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
  }
  .interval-row .radio {
    flex: 0 0 auto;
  }
  .review dl {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 0.25rem 0.75rem;
    margin: 0;
    font-size: 0.875rem;
  }
  .review dt {
    color: var(--color-fg-muted, #6b7280);
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    align-self: center;
  }
  .review dd {
    margin: 0;
  }
  .muted {
    color: var(--color-fg-muted, #6b7280);
  }
  .error {
    background: #fef2f2;
    color: #b91c1c;
    border: 1px solid #fecaca;
    border-radius: 0.25rem;
    padding: 0.5rem 0.75rem;
    font-size: 0.875rem;
  }
  footer {
    display: flex;
    gap: 0.5rem;
    justify-content: flex-end;
  }
  footer button {
    padding: 0.375rem 0.75rem;
    border: 1px solid var(--color-border, #d1d5db);
    border-radius: 0.25rem;
    background: var(--color-bg-elevated, #fff);
    cursor: pointer;
    font-size: 0.875rem;
  }
  footer button.primary {
    background: #1d4ed8;
    color: #fff;
    border-color: #1d4ed8;
  }
  footer button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
