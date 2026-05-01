<!--
  NodePalette — sidebar of available transform types (Foundry: "Add node"
  panel in Pipeline Builder).

  Foundry exposes several transform families: Transform data (SQL / Python),
  Join data, Split transform, Pattern mining, Trained model node, Geospatial,
  Create unique IDs, etc. We map each to one of our backend `transform_type`
  primitives (sql / python / llm / wasm / passthrough); the concrete
  behaviour is then refined inside `NodeConfig.svelte` / `TransformEditor`.
-->
<script lang="ts">
  type PaletteEntry = {
    transform: 'sql' | 'python' | 'llm' | 'wasm' | 'passthrough';
    label: string;
    description: string;
    icon: string;
  };

  const CATEGORIES: { name: string; entries: PaletteEntry[] }[] = [
    {
      name: 'Inputs',
      entries: [
        {
          transform: 'passthrough',
          label: 'Input dataset',
          description: 'Bind a Foundry dataset version. Outputs unchanged.',
          icon: '⊟',
        },
      ],
    },
    {
      name: 'Transform',
      entries: [
        {
          transform: 'sql',
          label: 'SQL transform',
          description: 'DataFusion SQL over upstream datasets. Joins, filters, aggregations.',
          icon: 'Σ',
        },
        {
          transform: 'python',
          label: 'Python transform',
          description: 'pyo3 user function. Receives Arrow batches per input.',
          icon: 'py',
        },
      ],
    },
    {
      name: 'AI / Custom',
      entries: [
        {
          transform: 'llm',
          label: 'LLM transform',
          description: 'Call the AI service for row-wise prompt completion.',
          icon: '✸',
        },
        {
          transform: 'wasm',
          label: 'WASM transform',
          description: 'Sandboxed user module (wasmtime). Offline, no network.',
          icon: '◇',
        },
      ],
    },
  ];

  type Props = {
    onAdd: (transform: PaletteEntry['transform']) => void;
    disabled?: boolean;
  };

  let { onAdd, disabled = false }: Props = $props();
</script>

<aside class="palette">
  <h3>Add node</h3>
  {#each CATEGORIES as category (category.name)}
    <section>
      <h4>{category.name}</h4>
      <ul>
        {#each category.entries as entry (entry.transform)}
          <li>
            <button
              type="button"
              class="entry"
              {disabled}
              onclick={() => onAdd(entry.transform)}
              title={entry.description}
            >
              <span class="icon">{entry.icon}</span>
              <span class="meta">
                <strong>{entry.label}</strong>
                <span class="desc">{entry.description}</span>
              </span>
            </button>
          </li>
        {/each}
      </ul>
    </section>
  {/each}
</aside>

<style>
  .palette {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 12px;
    background: #0b1220;
    border: 1px solid #1f2937;
    border-radius: 8px;
    color: #e2e8f0;
    width: 240px;
    flex-shrink: 0;
  }
  h3 {
    margin: 0;
    font-size: 14px;
    color: #f1f5f9;
  }
  h4 {
    margin: 4px 0 6px;
    font-size: 11px;
    color: #94a3b8;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  ul {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .entry {
    display: flex;
    gap: 10px;
    align-items: flex-start;
    width: 100%;
    text-align: left;
    background: #1e293b;
    color: #e2e8f0;
    border: 1px solid #334155;
    border-radius: 6px;
    padding: 8px 10px;
    cursor: pointer;
    font: inherit;
  }
  .entry:hover:not(:disabled) {
    background: #334155;
    border-color: #475569;
  }
  .entry:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .icon {
    font-size: 16px;
    line-height: 1;
    color: #60a5fa;
    flex-shrink: 0;
  }
  .meta {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .meta strong {
    font-size: 12px;
    font-weight: 600;
  }
  .desc {
    font-size: 11px;
    color: #94a3b8;
  }
</style>
