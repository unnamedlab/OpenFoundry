<!--
  NodePalette — Foundry "Add node" sidebar for the Pipeline Builder canvas.

  Adds the U21 media-set authoring categories on top of the original
  scalar transforms (sql / python / llm / wasm / passthrough). Each
  entry carries:

    * `transform_type` — backend wire-format string (must match
      `services/pipeline-authoring-service/src/domain/media_nodes.rs`).
    * `defaultConfig`  — per-kind config object that gets merged into
      the new node's `config` (e.g. `{ kind: 'resize', params: {} }` for
      MediaTransform variants which discriminate by `config.kind`).
    * `acceptedSchemas` / `outputKind` — surfaced by the canvas tooltip
      to explain why a node lights up red after the compiler validates
      schema compatibility.

  The component exposes a single `onAdd` callback receiving the full
  entry; the route in `routes/pipelines/[id]/edit/+page.svelte` reads
  `transform_type` + `defaultConfig` and constructs a `PipelineNode`
  with sensible defaults.
-->
<script lang="ts" module>
  import type { MediaSetSchema } from '$lib/api/mediaSets';

  /**
   * Stable shape of a palette entry. Public so the routes that mount
   * the palette (and our integration tests) can type-check the entries
   * they receive in the `onAdd` callback.
   */
  export type NodePaletteEntry = {
    /** Backend `transform_type` discriminator. */
    transform_type: string;
    /** Optional kind suffix for human-readable id / label seeding. */
    kind?: string;
    /** Default config to seed the node with. */
    defaultConfig?: Record<string, unknown>;
    /** UI label. */
    label: string;
    /** Tooltip body. */
    description: string;
    /** Single-character icon glyph rendered next to the label. */
    icon: string;
    /** Foundry media schemas the node can read; empty = any. */
    acceptedSchemas?: MediaSetSchema[];
    /** What the node produces. Drives downstream wiring hints. */
    outputKind?: 'media_items' | 'dataset_rows' | 'sideeffect';
  };
</script>

<script lang="ts">
  type Category = { name: string; entries: NodePaletteEntry[] };

  const CATEGORIES: Category[] = [
    {
      name: 'Inputs',
      entries: [
        {
          transform_type: 'passthrough',
          label: 'Input dataset',
          description: 'Bind a Foundry dataset version. Outputs unchanged.',
          icon: '⊟'
        }
      ]
    },
    {
      name: 'Transform',
      entries: [
        {
          transform_type: 'sql',
          label: 'SQL transform',
          description: 'DataFusion SQL over upstream datasets.',
          icon: 'Σ'
        },
        {
          transform_type: 'python',
          label: 'Python transform',
          description: 'pyo3 user function. Receives Arrow batches per input.',
          icon: 'py'
        }
      ]
    },
    {
      name: 'AI / Custom',
      entries: [
        {
          transform_type: 'llm',
          label: 'LLM transform',
          description: 'Call the AI service for row-wise prompt completion.',
          icon: '✸'
        },
        {
          transform_type: 'wasm',
          label: 'WASM transform',
          description: 'Sandboxed user module (wasmtime). Offline, no network.',
          icon: '◇'
        }
      ]
    },
    {
      name: 'Media: Inputs',
      entries: [
        {
          transform_type: 'media_set_input',
          label: 'Media set input',
          description: 'Read items (or paths) from a Foundry media set.',
          icon: 'IN',
          defaultConfig: { media_set_rid: '', branch: 'main' },
          outputKind: 'media_items'
        }
      ]
    },
    {
      name: 'Media: Outputs',
      entries: [
        {
          transform_type: 'media_set_output',
          label: 'Media set output',
          description: 'Write derived media into an existing or fresh media set.',
          icon: 'OUT',
          defaultConfig: {
            media_set_rid: '',
            branch: 'main',
            write_mode: 'modify'
          },
          outputKind: 'sideeffect'
        }
      ]
    },
    {
      name: 'Media: Transforms',
      entries: [
        {
          transform_type: 'media_transform',
          kind: 'extract_text_ocr',
          label: 'Extract text (OCR)',
          description: 'OCR over IMAGE or DOCUMENT items. Emits dataset rows.',
          icon: 'OCR',
          defaultConfig: { kind: 'extract_text_ocr', params: {} },
          acceptedSchemas: ['IMAGE', 'DOCUMENT'],
          outputKind: 'dataset_rows'
        },
        {
          transform_type: 'media_transform',
          kind: 'resize',
          label: 'Resize image',
          description: 'Resize IMAGE items to width × height pixels.',
          icon: '⛶',
          defaultConfig: { kind: 'resize', params: { width: 256, height: 256 } },
          acceptedSchemas: ['IMAGE'],
          outputKind: 'media_items'
        },
        {
          transform_type: 'media_transform',
          kind: 'rotate',
          label: 'Rotate image',
          description: 'Rotate IMAGE items by N degrees clockwise.',
          icon: '↻',
          defaultConfig: { kind: 'rotate', params: { degrees: 90 } },
          acceptedSchemas: ['IMAGE'],
          outputKind: 'media_items'
        },
        {
          transform_type: 'media_transform',
          kind: 'crop',
          label: 'Crop image',
          description: 'Crop IMAGE items to (x, y, width, height).',
          icon: '▣',
          defaultConfig: {
            kind: 'crop',
            params: { x: 0, y: 0, width: 256, height: 256 }
          },
          acceptedSchemas: ['IMAGE'],
          outputKind: 'media_items'
        },
        {
          transform_type: 'media_transform',
          kind: 'transcribe_audio',
          label: 'Transcribe audio',
          description: 'Transcribe AUDIO or VIDEO items to text rows.',
          icon: '🎙',
          defaultConfig: { kind: 'transcribe_audio', params: {} },
          acceptedSchemas: ['AUDIO', 'VIDEO'],
          outputKind: 'dataset_rows'
        },
        {
          transform_type: 'media_transform',
          kind: 'generate_embedding',
          label: 'Generate embedding',
          description: 'Generate vector embedding for any media kind.',
          icon: '⊕',
          defaultConfig: { kind: 'generate_embedding', params: {} },
          acceptedSchemas: undefined,
          outputKind: 'dataset_rows'
        },
        {
          transform_type: 'media_transform',
          kind: 'render_pdf_page',
          label: 'Render PDF page',
          description: 'Render one page of a DOCUMENT item as an image.',
          icon: 'PDF',
          defaultConfig: { kind: 'render_pdf_page', params: { page: 1 } },
          acceptedSchemas: ['DOCUMENT'],
          outputKind: 'media_items'
        },
        {
          transform_type: 'convert_media_set_to_table_rows',
          label: 'Convert to table rows',
          description: 'Flatten a media set into one row per item.',
          icon: '⊞',
          defaultConfig: {
            source_media_set_rid: '',
            branch: 'main',
            include_media_reference: true
          },
          outputKind: 'dataset_rows'
        },
        {
          transform_type: 'get_media_references',
          label: 'Get media references',
          description: 'Walk a dataset of files and register them in a media set.',
          icon: '🔗',
          defaultConfig: {
            source_dataset_id: '',
            target_media_set_rid: '',
            force_mime_type: null
          },
          outputKind: 'dataset_rows'
        }
      ]
    },
    // ────────────────────────────────────────────────────────────────────
    // D1.1.5 P3 — Foundry's five logic kinds beyond TRANSFORM. The
    // `transform_type` for these is the canonical proto enum name from
    // `proto/pipeline/builds.proto` (`SYNC`, `HEALTH_CHECK`, `ANALYTICAL`,
    // `EXPORT`); the build resolver maps that into a JobSpec.logic_kind.
    // ────────────────────────────────────────────────────────────────────
    {
      name: 'Data Connection: Sync',
      entries: [
        {
          transform_type: 'SYNC',
          label: 'Sync from external source',
          description:
            'Materialise data from a Foundry connector (S3, JDBC, Kafka, …) into a Foundry dataset.',
          icon: '⇄',
          defaultConfig: {
            logic_kind: 'SYNC',
            source_rid: '',
            sync_def_id: '',
            overrides: {}
          },
          outputKind: 'dataset_rows'
        }
      ]
    },
    {
      name: 'Quality: Health check',
      entries: [
        {
          transform_type: 'HEALTH_CHECK',
          label: 'Health check',
          description:
            'Validate row count / schema drift / freshness / custom SQL on a target dataset.',
          icon: '✓',
          defaultConfig: {
            logic_kind: 'HEALTH_CHECK',
            check_kind: 'ROW_COUNT_NONZERO',
            target_dataset_rid: '',
            params: {}
          },
          outputKind: 'sideeffect'
        }
      ]
    },
    {
      name: 'Analytics: Object set materialize',
      entries: [
        {
          transform_type: 'ANALYTICAL',
          label: 'Materialise object set',
          description:
            'Run a Quiver-style object-set query and write the rows into the output dataset.',
          icon: '◎',
          defaultConfig: {
            logic_kind: 'ANALYTICAL',
            object_set_query: {},
            ontology_rid: null,
            output_schema: null
          },
          outputKind: 'dataset_rows'
        }
      ]
    },
    {
      name: 'Export: External destination',
      entries: [
        {
          transform_type: 'EXPORT',
          label: 'Export to external system',
          description:
            'Push a dataset to S3 / GCS / HTTP / JDBC. Requires an ACL alias.',
          icon: '↗',
          defaultConfig: {
            logic_kind: 'EXPORT',
            export_target: 'S3',
            endpoint: '',
            options: {},
            source_dataset_rid: '',
            acl_alias: ''
          },
          outputKind: 'sideeffect'
        }
      ]
    }
  ];

  type Props = {
    onAdd: (entry: NodePaletteEntry) => void;
    disabled?: boolean;
  };

  let { onAdd, disabled = false }: Props = $props();
</script>

<aside class="palette" data-testid="node-palette">
  <h3>Add node</h3>
  {#each CATEGORIES as category (category.name)}
    <section>
      <h4>{category.name}</h4>
      <ul>
        {#each category.entries as entry (`${entry.transform_type}::${entry.kind ?? ''}`)}
          <li>
            <button
              type="button"
              class="entry"
              {disabled}
              data-testid={`palette-entry-${entry.transform_type}${
                entry.kind ? `-${entry.kind}` : ''
              }`}
              onclick={() => onAdd(entry)}
              title={entry.description}
            >
              <span class="icon" aria-hidden="true">{entry.icon}</span>
              <span class="meta">
                <strong>{entry.label}</strong>
                <span class="desc">{entry.description}</span>
                {#if entry.acceptedSchemas && entry.acceptedSchemas.length > 0}
                  <span class="schemas">
                    accepts: {entry.acceptedSchemas.join(' · ')}
                  </span>
                {/if}
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
    max-height: 80vh;
    overflow-y: auto;
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
    font-size: 14px;
    line-height: 1;
    color: #60a5fa;
    flex-shrink: 0;
    width: 22px;
    text-align: center;
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
  .schemas {
    font-size: 10px;
    color: #38bdf8;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
</style>
