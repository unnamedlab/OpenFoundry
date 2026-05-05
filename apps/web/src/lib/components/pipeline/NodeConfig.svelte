<!--
  NodeConfig — right-hand inspector for the selected node (Foundry: the
  "Properties" / "Settings" pane in Pipeline Builder). Edits scalar fields
  and delegates the body to <TransformEditor/>.
-->
<script lang="ts">
  import type { NodeValidationReport, PipelineNode } from '$lib/api/pipelines';
  import MediaTransformEditor from './MediaTransformEditor.svelte';
  import TransformEditor from './TransformEditor.svelte';

  type Transform = 'sql' | 'python' | 'llm' | 'wasm' | 'passthrough';
  const TRANSFORMS: Transform[] = ['passthrough', 'sql', 'python', 'llm', 'wasm'];

  // Media node `transform_type` values seeded by NodePalette. The
  // inspector swaps the body editor for `MediaTransformEditor` and
  // hides the transform-switch dropdown for these (changing the
  // discriminator from a media kind to e.g. SQL would silently drop
  // the per-kind config and break validation).
  const MEDIA_TRANSFORM_TYPES = new Set([
    'media_set_input',
    'media_set_output',
    'media_transform',
    'convert_media_set_to_table_rows',
    'get_media_references'
  ]);

  function isMedia(transform_type: string) {
    return MEDIA_TRANSFORM_TYPES.has(transform_type);
  }

  type Props = {
    node: PipelineNode | null;
    siblings: PipelineNode[];
    readonly?: boolean;
    onChange: (next: PipelineNode) => void;
    onDelete?: (nodeId: string) => void;
    /** FASE 3 — type-safe validation report for the active node. */
    validation?: NodeValidationReport | null;
  };

  let { node, siblings, readonly = false, onChange, onDelete, validation = null }: Props = $props();

  // Map the body field per transform_type to a single string we hand to
  // <TransformEditor/>. Mirrors the backend executor's body extraction.
  const BODY_KEY: Record<Transform, string | null> = {
    sql: 'sql',
    python: 'python_source',
    llm: 'prompt',
    wasm: 'wasm_module_b64',
    passthrough: null,
  };

  function bodyValue(n: PipelineNode): string {
    const key = BODY_KEY[n.transform_type as Transform];
    if (!key) return '';
    const raw = (n.config ?? {})[key];
    return typeof raw === 'string' ? raw : '';
  }

  function patch(partial: Partial<PipelineNode>) {
    if (!node) return;
    onChange({ ...node, ...partial });
  }

  function setBody(next: string) {
    if (!node) return;
    const key = BODY_KEY[node.transform_type as Transform];
    if (!key) return;
    onChange({ ...node, config: { ...(node.config ?? {}), [key]: next } });
  }

  function setTransform(next: Transform) {
    if (!node) return;
    // Preserve foreign keys but drop the previous body field to avoid
    // confusing the executor (Foundry's behaviour when switching node type).
    const cleanConfig: Record<string, unknown> = { ...(node.config ?? {}) };
    for (const v of Object.values(BODY_KEY)) {
      if (v) delete cleanConfig[v];
    }
    onChange({ ...node, transform_type: next, config: cleanConfig });
  }

  function toggleDependency(id: string) {
    if (!node) return;
    const set = new Set(node.depends_on);
    if (set.has(id)) set.delete(id); else set.add(id);
    patch({ depends_on: [...set] });
  }

  function setDatasetList(field: 'input_dataset_ids', csv: string) {
    const ids = csv
      .split(/[,\s]+/)
      .map((s) => s.trim())
      .filter(Boolean);
    patch({ [field]: ids } as Partial<PipelineNode>);
  }

  let dependencyOptions = $derived(siblings.filter((s) => node && s.id !== node.id));
</script>

{#if !node}
  <aside class="config empty">
    <p>Select a node on the canvas to edit its properties.</p>
  </aside>
{:else}
  <aside class="config">
    <header>
      <h3>Node properties</h3>
      {#if onDelete && !readonly}
        <button type="button" class="danger" onclick={() => onDelete(node.id)}>Delete</button>
      {/if}
    </header>

    {#if validation && validation.status !== 'VALID' && validation.errors.length > 0}
      <ul class="validation-errors" data-testid="node-config-errors">
        {#each validation.errors as error (error.message)}
          <li>
            {#if error.column}
              <code class="squiggle">{error.column}</code>
              —
            {/if}
            {error.message}
          </li>
        {/each}
      </ul>
    {/if}

    <label>
      <span>ID</span>
      <input
        type="text"
        value={node.id}
        readonly={readonly}
        onchange={(e) => patch({ id: (e.currentTarget as HTMLInputElement).value.trim() })}
      />
    </label>

    <label>
      <span>Label</span>
      <input
        type="text"
        value={node.label}
        readonly={readonly}
        oninput={(e) => patch({ label: (e.currentTarget as HTMLInputElement).value })}
      />
    </label>

    {#if isMedia(node.transform_type)}
      <label>
        <span>Transform</span>
        <input type="text" value={node.transform_type} readonly />
        <p class="hint">
          Media node — switch transform via the palette to keep config in sync.
        </p>
      </label>
    {:else}
      <label>
        <span>Transform</span>
        <select
          value={node.transform_type}
          disabled={readonly}
          onchange={(e) => setTransform((e.currentTarget as HTMLSelectElement).value as Transform)}
        >
          {#each TRANSFORMS as t (t)}
            <option value={t}>{t}</option>
          {/each}
        </select>
      </label>
    {/if}

    <fieldset>
      <legend>Depends on</legend>
      {#if dependencyOptions.length === 0}
        <p class="hint">No other nodes available.</p>
      {:else}
        {#each dependencyOptions as opt (opt.id)}
          <label class="checkbox">
            <input
              type="checkbox"
              checked={node.depends_on.includes(opt.id)}
              disabled={readonly}
              onchange={() => toggleDependency(opt.id)}
            />
            <span>{opt.label} <code>({opt.id})</code></span>
          </label>
        {/each}
      {/if}
    </fieldset>

    <label>
      <span>Input datasets (comma-separated UUIDs)</span>
      <input
        type="text"
        value={node.input_dataset_ids.join(', ')}
        readonly={readonly}
        onchange={(e) => setDatasetList('input_dataset_ids', (e.currentTarget as HTMLInputElement).value)}
      />
    </label>

    <label>
      <span>Output dataset (UUID)</span>
      <input
        type="text"
        value={node.output_dataset_id ?? ''}
        readonly={readonly}
        onchange={(e) => {
          const v = (e.currentTarget as HTMLInputElement).value.trim();
          patch({ output_dataset_id: v ? v : null });
        }}
      />
    </label>

    {#if isMedia(node.transform_type)}
      <MediaTransformEditor node={node} readonly={readonly} onChange={onChange} />
    {:else}
      <section class="body">
        <h4>Body</h4>
        <TransformEditor
          transformType={node.transform_type as Transform}
          value={bodyValue(node)}
          readonly={readonly}
          onChange={setBody}
        />
      </section>
    {/if}
  </aside>
{/if}

<style>
  .config {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 14px;
    background: #0b1220;
    border: 1px solid #1f2937;
    border-radius: 8px;
    color: #e2e8f0;
    width: 360px;
    flex-shrink: 0;
  }
  .config.empty {
    color: #94a3b8;
    font-style: italic;
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  h3 {
    margin: 0;
    font-size: 14px;
  }
  h4 {
    margin: 8px 0 4px;
    font-size: 12px;
    color: #94a3b8;
    text-transform: uppercase;
    letter-spacing: 0.05em;
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
    padding: 2px 0;
  }
  input[type='text'],
  select {
    background: #1e293b;
    color: #f1f5f9;
    border: 1px solid #334155;
    border-radius: 4px;
    padding: 6px 8px;
    font: inherit;
  }
  input[readonly] {
    opacity: 0.7;
  }
  fieldset {
    border: 1px solid #1f2937;
    border-radius: 6px;
    padding: 8px 10px;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  legend {
    font-size: 11px;
    color: #94a3b8;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding: 0 4px;
  }
  .hint {
    color: #94a3b8;
    font-style: italic;
    margin: 0;
    font-size: 12px;
  }
  .danger {
    background: #7f1d1d;
    color: #fee2e2;
    border: 1px solid #b91c1c;
    border-radius: 4px;
    padding: 4px 10px;
    cursor: pointer;
    font: inherit;
    font-size: 12px;
  }
  .danger:hover {
    background: #b91c1c;
  }
  .validation-errors {
    list-style: none;
    margin: 0;
    padding: 8px 10px;
    border: 1px solid #b91c1c;
    border-radius: 4px;
    background: rgba(127, 29, 29, 0.18);
    color: #fecaca;
    font-size: 12px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .validation-errors .squiggle {
    text-decoration: underline wavy #f87171;
    text-underline-offset: 3px;
    background: rgba(220, 38, 38, 0.18);
    padding: 0 2px;
    border-radius: 2px;
  }
  code {
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
    font-size: 11px;
    color: #94a3b8;
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
</style>
