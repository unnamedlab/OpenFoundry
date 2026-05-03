<!--
  TransformEditor — body editor for the active node's user code (Foundry:
  the right-hand "Code" pane of Pipeline Builder when SQL or Python mode
  is chosen). Falls back to a textarea when monaco-editor is unavailable
  (SSR, test runner, etc.).

  Body persistence keys per `transform_type` (must match backend
  `executor::run_node` extraction in `services/pipeline-authoring-service/`):
    sql        → config.sql           : string
    python     → config.python_source : string
    llm        → config.prompt        : string
    wasm       → config.wasm_module_b64 : string (base64 module bytes)
    passthrough → no body
-->
<script lang="ts">
  import { onMount, onDestroy } from 'svelte';

  /**
   * `LogicKind` — D1.1.5 P3. The five Foundry job kinds get a
   * dedicated config panel below the editor; the editor itself is
   * still only displayed for code-bearing kinds (sql / python /
   * llm / wasm).
   */
  type CodeTransform = 'sql' | 'python' | 'llm' | 'wasm' | 'passthrough';
  type LogicKind = 'SYNC' | 'HEALTH_CHECK' | 'ANALYTICAL' | 'EXPORT';
  type Transform = CodeTransform | LogicKind;

  type ViewFilterIncompatibility = {
    /** Human-readable reason for the red border. */
    reason: string;
  };

  type Props = {
    transformType: Transform;
    value: string;
    readonly?: boolean;
    onChange: (next: string) => void;
    /**
     * Optional config object surfaced for the four non-code kinds.
     * Editing it dispatches `onConfigChange`.
     */
    config?: Record<string, unknown>;
    onConfigChange?: (next: Record<string, unknown>) => void;
    /**
     * When non-null the view-filter row renders a red border to flag
     * an incompatibility detected at resolve-time (e.g. "INCREMENTAL
     * but the dataset only has SNAPSHOTs").
     */
    viewFilterIncompatibility?: ViewFilterIncompatibility | null;
  };

  let {
    transformType,
    value,
    readonly = false,
    onChange,
    config = {},
    onConfigChange = () => {},
    viewFilterIncompatibility = null,
  }: Props = $props();

  const LANGUAGE: Record<CodeTransform, string> = {
    sql: 'sql',
    python: 'python',
    llm: 'markdown',
    wasm: 'plaintext',
    passthrough: 'plaintext',
  };

  const LOGIC_KINDS: LogicKind[] = ['SYNC', 'HEALTH_CHECK', 'ANALYTICAL', 'EXPORT'];
  const isLogicKind = (t: Transform): t is LogicKind =>
    (LOGIC_KINDS as Transform[]).includes(t);

  function updateConfig(key: string, next: unknown) {
    onConfigChange({ ...config, [key]: next });
  }

  let host: HTMLDivElement | undefined = $state();
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let monacoEditor: any = null;
  let useFallback = $state(false);

  onMount(async () => {
    if (transformType === 'passthrough' || isLogicKind(transformType)) return;
    if (!host) return;
    try {
      const monaco = await import('monaco-editor');
      monacoEditor = monaco.editor.create(host, {
        value,
        language: LANGUAGE[transformType as CodeTransform],
        theme: 'vs-dark',
        automaticLayout: true,
        minimap: { enabled: false },
        fontSize: 13,
        readOnly: readonly,
      });
      monacoEditor.onDidChangeModelContent(() => {
        if (monacoEditor) onChange(monacoEditor.getValue());
      });
    } catch {
      useFallback = true;
    }
  });

  onDestroy(() => {
    monacoEditor?.dispose();
  });

  $effect(() => {
    if (monacoEditor && monacoEditor.getValue() !== value) {
      monacoEditor.setValue(value);
    }
  });

  $effect(() => {
    monacoEditor?.updateOptions({ readOnly: readonly });
  });
</script>

{#if transformType === 'passthrough'}
  <p class="hint">Passthrough nodes have no body. Configure inputs and dependencies in the side panel.</p>
{:else if isLogicKind(transformType)}
  <!-- D1.1.5 P3 — kind-specific config panel. The transform body
       (`value`) stays empty for these kinds; the runner reads
       `JobSpec.logic_payload` instead. -->
  <section class="kind-config" data-testid={`kind-config-${transformType}`}>
    {#if transformType === 'SYNC'}
      <fieldset>
        <legend>Sync (Data Connection)</legend>
        <label>
          Source RID
          <input
            type="text"
            value={String(config.source_rid ?? '')}
            oninput={(e) => updateConfig('source_rid', (e.currentTarget as HTMLInputElement).value)}
            placeholder="ri.foundry.main.connector.…"
            data-testid="sync-source-rid"
          />
        </label>
        <label>
          Sync def ID
          <input
            type="text"
            value={String(config.sync_def_id ?? '')}
            oninput={(e) => updateConfig('sync_def_id', (e.currentTarget as HTMLInputElement).value)}
            placeholder="UUID published by connector-management"
            data-testid="sync-def-id"
          />
        </label>
      </fieldset>
    {:else if transformType === 'HEALTH_CHECK'}
      <fieldset>
        <legend>Health check</legend>
        <label>
          Check kind
          <select
            value={String(config.check_kind ?? 'ROW_COUNT_NONZERO')}
            onchange={(e) => updateConfig('check_kind', (e.currentTarget as HTMLSelectElement).value)}
            data-testid="health-check-kind"
          >
            <option value="ROW_COUNT_NONZERO">Row count non-zero</option>
            <option value="SCHEMA_DRIFT">Schema drift</option>
            <option value="FRESHNESS_SLA">Freshness SLA</option>
            <option value="CUSTOM_SQL">Custom SQL</option>
          </select>
        </label>
        <label>
          Target dataset RID
          <input
            type="text"
            value={String(config.target_dataset_rid ?? '')}
            oninput={(e) => updateConfig('target_dataset_rid', (e.currentTarget as HTMLInputElement).value)}
            placeholder="must equal output dataset"
            data-testid="health-check-target"
          />
        </label>
      </fieldset>
    {:else if transformType === 'ANALYTICAL'}
      <fieldset>
        <legend>Materialise object set</legend>
        <label>
          Object set query (JSON)
          <textarea
            value={JSON.stringify(config.object_set_query ?? {}, null, 2)}
            oninput={(e) => {
              try {
                updateConfig('object_set_query', JSON.parse((e.currentTarget as HTMLTextAreaElement).value || '{}'));
              } catch {
                /* keep last good value until JSON parses */
              }
            }}
            data-testid="analytical-query"
          ></textarea>
        </label>
        <label>
          Ontology RID
          <input
            type="text"
            value={String(config.ontology_rid ?? '')}
            oninput={(e) => updateConfig('ontology_rid', (e.currentTarget as HTMLInputElement).value)}
            placeholder="ri.ontology.…"
            data-testid="analytical-ontology"
          />
        </label>
      </fieldset>
    {:else if transformType === 'EXPORT'}
      <fieldset>
        <legend>Export</legend>
        <label>
          Target
          <select
            value={String(config.export_target ?? 'S3')}
            onchange={(e) => updateConfig('export_target', (e.currentTarget as HTMLSelectElement).value)}
            data-testid="export-target"
          >
            <option value="S3">S3</option>
            <option value="GCS">GCS</option>
            <option value="HTTP">HTTP</option>
            <option value="JDBC">JDBC</option>
          </select>
        </label>
        <label>
          Endpoint
          <input
            type="text"
            value={String(config.endpoint ?? '')}
            oninput={(e) => updateConfig('endpoint', (e.currentTarget as HTMLInputElement).value)}
            placeholder="s3://bucket/prefix or https://…"
            data-testid="export-endpoint"
          />
        </label>
        <label>
          ACL alias
          <input
            type="text"
            value={String(config.acl_alias ?? '')}
            oninput={(e) => updateConfig('acl_alias', (e.currentTarget as HTMLInputElement).value)}
            placeholder="must be configured before export runs"
            data-testid="export-acl-alias"
          />
        </label>
      </fieldset>
    {/if}
    {#if viewFilterIncompatibility}
      <p class="view-filter-error" data-testid="view-filter-incompatibility">
        Incompatible view_filter: {viewFilterIncompatibility.reason}
      </p>
    {/if}
  </section>
{:else if useFallback}
  <textarea
    class="fallback {viewFilterIncompatibility ? 'incompatible' : ''}"
    {value}
    {readonly}
    oninput={(e) => onChange((e.currentTarget as HTMLTextAreaElement).value)}
    spellcheck="false"
    placeholder={transformType === 'sql' ? 'SELECT * FROM input' : 'def transform(df):\n    return df'}
  ></textarea>
{:else}
  <div bind:this={host} class="monaco-host {viewFilterIncompatibility ? 'incompatible' : ''}"></div>
{/if}

<style>
  .monaco-host {
    width: 100%;
    min-height: 280px;
    border: 1px solid #1f2937;
    border-radius: 6px;
    overflow: hidden;
  }
  .fallback {
    width: 100%;
    min-height: 280px;
    background: #0b1220;
    color: #e2e8f0;
    border: 1px solid #1f2937;
    border-radius: 6px;
    padding: 12px;
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
    font-size: 13px;
  }
  .kind-config fieldset {
    border: 1px solid #1f2937;
    border-radius: 6px;
    padding: 0.75rem 1rem;
    display: grid;
    gap: 0.5rem;
    background: #0b1220;
    color: #e2e8f0;
  }
  .kind-config legend {
    color: #94a3b8;
    padding: 0 0.4rem;
  }
  .kind-config label {
    display: grid;
    gap: 0.25rem;
    font-size: 13px;
  }
  .kind-config input,
  .kind-config select,
  .kind-config textarea {
    background: #111827;
    color: #e2e8f0;
    border: 1px solid #1f2937;
    border-radius: 4px;
    padding: 6px 8px;
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
  }
  .kind-config textarea {
    min-height: 120px;
  }
  .view-filter-error {
    color: #f87171;
    font-size: 13px;
    margin: 0.5rem 0 0;
  }
  .incompatible {
    border-color: #b91c1c !important;
    box-shadow: 0 0 0 1px #b91c1c;
  }
  .hint {
    color: #94a3b8;
    font-style: italic;
    padding: 8px 4px;
    margin: 0;
  }
</style>
