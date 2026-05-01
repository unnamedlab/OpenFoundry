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

  type Transform = 'sql' | 'python' | 'llm' | 'wasm' | 'passthrough';

  type Props = {
    transformType: Transform;
    value: string;
    readonly?: boolean;
    onChange: (next: string) => void;
  };

  let { transformType, value, readonly = false, onChange }: Props = $props();

  const LANGUAGE: Record<Transform, string> = {
    sql: 'sql',
    python: 'python',
    llm: 'markdown',
    wasm: 'plaintext',
    passthrough: 'plaintext',
  };

  let host: HTMLDivElement;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let monacoEditor: any = null;
  let useFallback = $state(false);

  onMount(async () => {
    if (transformType === 'passthrough') return;
    try {
      const monaco = await import('monaco-editor');
      monacoEditor = monaco.editor.create(host, {
        value,
        language: LANGUAGE[transformType],
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
{:else if useFallback}
  <textarea
    class="fallback"
    {value}
    {readonly}
    oninput={(e) => onChange((e.currentTarget as HTMLTextAreaElement).value)}
    spellcheck="false"
    placeholder={transformType === 'sql' ? 'SELECT * FROM input' : 'def transform(df):\n    return df'}
  ></textarea>
{:else}
  <div bind:this={host} class="monaco-host"></div>
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
  .hint {
    color: #94a3b8;
    font-style: italic;
    padding: 8px 4px;
    margin: 0;
  }
</style>
