<!--
  MediaTransformEditor — per-kind config editor for media-set pipeline
  nodes. Mirrors the validators in
  `services/pipeline-authoring-service/src/domain/media_nodes.rs` so the
  shape submitted from the UI matches what the backend's
  `validate_media_node` accepts.

  Five top-level transform_types are handled:
    * `media_set_input`               — read items from a media set.
    * `media_set_output`              — write to an existing or new set.
    * `media_transform`               — apply a kind-specific transform
                                        (OCR, resize, rotate, …).
    * `convert_media_set_to_table_rows`
    * `get_media_references`

  The component is purposely self-contained — `NodeConfig.svelte`
  detects a media transform_type and renders this in lieu of the
  standard body editor, leaving the rest of the inspector (id, label,
  dependencies, dataset ids) untouched.
-->
<script lang="ts">
  import type { PipelineNode } from '$lib/api/pipelines';

  type Props = {
    node: PipelineNode;
    readonly?: boolean;
    onChange: (next: PipelineNode) => void;
  };

  let { node, readonly = false, onChange }: Props = $props();

  const TRANSFORM_KINDS = [
    'extract_text_ocr',
    'resize',
    'rotate',
    'crop',
    'transcribe_audio',
    'generate_embedding',
    'render_pdf_page',
    'extract_layout_aware'
  ] as const;
  type TransformKind = (typeof TRANSFORM_KINDS)[number];

  const SCHEMAS = ['IMAGE', 'AUDIO', 'VIDEO', 'DOCUMENT', 'SPREADSHEET', 'EMAIL'] as const;

  // ── Generic helpers ───────────────────────────────────────────────
  function patch(partial: Record<string, unknown>) {
    onChange({ ...node, config: { ...(node.config ?? {}), ...partial } });
  }

  function readString(key: string, fallback = ''): string {
    const raw = (node.config ?? {})[key];
    return typeof raw === 'string' ? raw : fallback;
  }

  function readBool(key: string, fallback = false): boolean {
    const raw = (node.config ?? {})[key];
    return typeof raw === 'boolean' ? raw : fallback;
  }

  function readObject(key: string): Record<string, unknown> {
    const raw = (node.config ?? {})[key];
    return raw && typeof raw === 'object' && !Array.isArray(raw)
      ? (raw as Record<string, unknown>)
      : {};
  }

  // ── media_transform (kind + params) ───────────────────────────────
  function patchParams(partial: Record<string, unknown>) {
    const params = { ...readObject('params'), ...partial };
    patch({ params });
  }

  function setKind(kind: TransformKind) {
    // Reset params to empty when switching kinds; the user re-fills
    // the per-kind fields below.
    onChange({ ...node, config: { ...(node.config ?? {}), kind, params: {} } });
  }

  function readNumber(obj: Record<string, unknown>, key: string, fallback = 0): number {
    const raw = obj[key];
    return typeof raw === 'number' ? raw : fallback;
  }
</script>

<section class="editor" data-testid="media-transform-editor">
  <h4>Media node config</h4>

  {#if node.transform_type === 'media_set_input'}
    <label>
      <span>Media set RID</span>
      <input
        type="text"
        value={readString('media_set_rid')}
        readonly={readonly}
        placeholder="ri.foundry.main.media_set.…"
        data-testid="media-input-rid"
        onchange={(event) =>
          patch({ media_set_rid: (event.currentTarget as HTMLInputElement).value.trim() })}
      />
    </label>
    <label>
      <span>Branch</span>
      <input
        type="text"
        value={readString('branch', 'main')}
        readonly={readonly}
        onchange={(event) =>
          patch({ branch: (event.currentTarget as HTMLInputElement).value.trim() || 'main' })}
      />
    </label>
    <label>
      <span>Path prefix (optional)</span>
      <input
        type="text"
        value={readString('path_prefix')}
        readonly={readonly}
        placeholder="screens/"
        onchange={(event) =>
          patch({
            path_prefix: (event.currentTarget as HTMLInputElement).value.trim() || null
          })}
      />
    </label>
  {:else if node.transform_type === 'media_set_output'}
    <label>
      <span>Existing media set RID</span>
      <input
        type="text"
        value={readString('media_set_rid')}
        readonly={readonly}
        placeholder="ri.foundry.main.media_set.…"
        data-testid="media-output-rid"
        onchange={(event) =>
          patch({
            media_set_rid: (event.currentTarget as HTMLInputElement).value.trim() || null
          })}
      />
      <p class="hint">Leave empty to create a new media set on first run.</p>
    </label>
    <label>
      <span>Branch</span>
      <input
        type="text"
        value={readString('branch', 'main')}
        readonly={readonly}
        onchange={(event) =>
          patch({ branch: (event.currentTarget as HTMLInputElement).value.trim() || 'main' })}
      />
    </label>
    <label>
      <span>Write mode</span>
      <select
        value={readString('write_mode', 'modify')}
        disabled={readonly}
        onchange={(event) =>
          patch({ write_mode: (event.currentTarget as HTMLSelectElement).value })}
      >
        <option value="modify">modify (append / overwrite paths)</option>
        <option value="replace">replace (TRANSACTIONAL only)</option>
      </select>
    </label>
  {:else if node.transform_type === 'media_transform'}
    <label>
      <span>Kind</span>
      <select
        value={readString('kind', 'extract_text_ocr')}
        disabled={readonly}
        data-testid="media-transform-kind"
        onchange={(event) =>
          setKind((event.currentTarget as HTMLSelectElement).value as TransformKind)}
      >
        {#each TRANSFORM_KINDS as kind (kind)}
          <option value={kind}>{kind}</option>
        {/each}
      </select>
    </label>

    {@const kind = readString('kind', 'extract_text_ocr')}
    {@const params = readObject('params')}

    {#if kind === 'resize'}
      <label>
        <span>Width (px)</span>
        <input
          type="number"
          min="1"
          value={readNumber(params, 'width', 256)}
          readonly={readonly}
          onchange={(event) =>
            patchParams({
              width: Number((event.currentTarget as HTMLInputElement).value) || 0
            })}
        />
      </label>
      <label>
        <span>Height (px)</span>
        <input
          type="number"
          min="1"
          value={readNumber(params, 'height', 256)}
          readonly={readonly}
          onchange={(event) =>
            patchParams({
              height: Number((event.currentTarget as HTMLInputElement).value) || 0
            })}
        />
      </label>
    {:else if kind === 'rotate'}
      <label>
        <span>Degrees clockwise</span>
        <input
          type="number"
          min="-360"
          max="360"
          value={readNumber(params, 'degrees', 90)}
          readonly={readonly}
          onchange={(event) =>
            patchParams({
              degrees: Number((event.currentTarget as HTMLInputElement).value) || 0
            })}
        />
      </label>
    {:else if kind === 'crop'}
      <div class="grid">
        {#each ['x', 'y', 'width', 'height'] as field (field)}
          <label>
            <span>{field}</span>
            <input
              type="number"
              min="0"
              value={readNumber(params, field, 0)}
              readonly={readonly}
              onchange={(event) =>
                patchParams({
                  [field]: Number((event.currentTarget as HTMLInputElement).value) || 0
                })}
            />
          </label>
        {/each}
      </div>
    {:else if kind === 'render_pdf_page'}
      <label>
        <span>Page number (1-based)</span>
        <input
          type="number"
          min="1"
          value={readNumber(params, 'page', 1)}
          readonly={readonly}
          onchange={(event) =>
            patchParams({
              page: Number((event.currentTarget as HTMLInputElement).value) || 1
            })}
        />
      </label>
    {:else}
      <p class="hint">
        No required parameters for <code>{kind}</code>. Free-form params can be edited
        as JSON in the body section below.
      </p>
    {/if}

    <p class="hint">
      Accepted input schemas:
      {#if kind === 'extract_text_ocr'}
        IMAGE · DOCUMENT
      {:else if kind === 'resize' || kind === 'rotate' || kind === 'crop'}
        IMAGE
      {:else if kind === 'transcribe_audio'}
        AUDIO · VIDEO
      {:else if kind === 'render_pdf_page' || kind === 'extract_layout_aware'}
        DOCUMENT
      {:else}
        any ({SCHEMAS.join(' · ')})
      {/if}
    </p>
  {:else if node.transform_type === 'convert_media_set_to_table_rows'}
    <label>
      <span>Source media set RID</span>
      <input
        type="text"
        value={readString('source_media_set_rid')}
        readonly={readonly}
        placeholder="ri.foundry.main.media_set.…"
        onchange={(event) =>
          patch({
            source_media_set_rid: (event.currentTarget as HTMLInputElement).value.trim()
          })}
      />
    </label>
    <label>
      <span>Branch</span>
      <input
        type="text"
        value={readString('branch', 'main')}
        readonly={readonly}
        onchange={(event) =>
          patch({ branch: (event.currentTarget as HTMLInputElement).value.trim() || 'main' })}
      />
    </label>
    <label class="checkbox">
      <input
        type="checkbox"
        checked={readBool('include_media_reference', true)}
        disabled={readonly}
        onchange={(event) =>
          patch({
            include_media_reference: (event.currentTarget as HTMLInputElement).checked
          })}
      />
      <span>Include media reference column</span>
    </label>
  {:else if node.transform_type === 'get_media_references'}
    <label>
      <span>Source dataset id (UUID)</span>
      <input
        type="text"
        value={readString('source_dataset_id')}
        readonly={readonly}
        placeholder="018f0000-0000-0000-0000-…"
        onchange={(event) =>
          patch({
            source_dataset_id: (event.currentTarget as HTMLInputElement).value.trim()
          })}
      />
    </label>
    <label>
      <span>Target media set RID</span>
      <input
        type="text"
        value={readString('target_media_set_rid')}
        readonly={readonly}
        placeholder="ri.foundry.main.media_set.…"
        onchange={(event) =>
          patch({
            target_media_set_rid: (event.currentTarget as HTMLInputElement).value.trim()
          })}
      />
    </label>
    <label>
      <span>Force MIME type (optional)</span>
      <input
        type="text"
        value={readString('force_mime_type')}
        readonly={readonly}
        placeholder="image/png"
        onchange={(event) =>
          patch({
            force_mime_type:
              (event.currentTarget as HTMLInputElement).value.trim() || null
          })}
      />
    </label>
  {/if}
</section>

<style>
  .editor {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px 0;
    border-top: 1px solid #1f2937;
  }
  h4 {
    margin: 0;
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
  }
  input[type='text'],
  input[type='number'],
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
  .grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 6px;
  }
  .hint {
    color: #94a3b8;
    font-style: italic;
    margin: 0;
    font-size: 11px;
  }
  code {
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
    font-size: 11px;
    color: #e2e8f0;
  }
</style>
