<!--
  MediaUploaderWidget — Workshop "Media Uploader" widget
  (Foundry: `docs_original_palantir_foundry/.../Media Uploader.md`).

  Foundry contract:
    * The widget is a button-styled dropzone that lets the user pick
      one or more media files; on submit, the files are uploaded to a
      configured *destination* (dataset, Compass folder, or media set)
      and the resulting RIDs are passed to a downstream action.
    * **Uploads are deferred**. From the docs: "Media files uploaded in
      action forms are only uploaded to the backing media set upon
      successful form submission, to ensure that canceled or failed
      submissions do not result in orphaned media files in media sets."
      We therefore stage files in component state and only POST through
      the media-sets-service `uploadItem` API when the operator clicks
      *Submit*.

  This widget is wired through the Workshop `onAction` callback so the
  parent can chain a downstream action (e.g. `set_parameters` with the
  uploaded item RID, or `execute_query` with the new media reference).
-->
<script lang="ts">
  import type { AppWidget, WidgetEvent } from '$lib/api/apps';
  import { uploadItem } from '$lib/api/mediaSets';
  import { notifications as toasts } from '$stores/notifications';

  type Props = {
    widget: AppWidget;
    runtimeParameters?: Record<string, string>;
    onAction?: (event: WidgetEvent, payload?: Record<string, unknown>) => Promise<void> | void;
  };

  let { widget, runtimeParameters = {}, onAction }: Props = $props();

  // ── Configuration ────────────────────────────────────────────────
  function readString(key: string, fallback = ''): string {
    const raw = widget.props[key];
    if (typeof raw !== 'string') return fallback;
    return interpolate(raw, runtimeParameters);
  }

  function readBool(key: string, fallback = false): boolean {
    const raw = widget.props[key];
    return typeof raw === 'boolean' ? raw : fallback;
  }

  function interpolate(template: string, params: Record<string, string>) {
    return template.replace(/\{\{\s*([\w.-]+)\s*\}\}/g, (_, key: string) => params[key] ?? '');
  }

  const buttonText = $derived(readString('text', 'Upload media'));
  const intent = $derived(readString('intent', 'primary'));
  const allowedExtensions = $derived(readString('allowed_extensions', ''));
  const allowMultiple = $derived(readBool('multi_file', false));
  const destinationRid = $derived(readString('destination_rid'));
  const branch = $derived(readString('branch', 'main'));

  // ── Staged files (deferred upload) ───────────────────────────────
  type StagedFile = {
    id: string;
    file: File;
    status: 'staged' | 'uploading' | 'done' | 'error';
    error?: string;
    itemRid?: string;
    mediaSetRid?: string;
  };

  let staged = $state<StagedFile[]>([]);
  let inputEl: HTMLInputElement | null = $state(null);
  let submitting = $state(false);

  function makeId() {
    return crypto.randomUUID?.() ?? `up-${Math.random().toString(36).slice(2)}`;
  }

  function stageFiles(files: FileList | File[]) {
    const list = Array.from(files);
    const next: StagedFile[] = list.map((file) => ({
      id: makeId(),
      file,
      status: 'staged' as const
    }));
    if (allowMultiple) {
      staged = [...staged, ...next];
    } else {
      // Single-file mode replaces the staged item (Foundry default).
      staged = next.slice(-1);
    }
  }

  function removeStaged(id: string) {
    staged = staged.filter((s) => s.id !== id);
  }

  function patch(id: string, partial: Partial<StagedFile>) {
    staged = staged.map((s) => (s.id === id ? { ...s, ...partial } : s));
  }

  // ── Submit (the deferred-upload boundary, per Foundry docs) ──────
  async function submit() {
    if (staged.length === 0 || !destinationRid) {
      toasts.warning(
        destinationRid
          ? 'No files staged'
          : 'Configure the upload destination before submitting'
      );
      return;
    }
    submitting = true;
    const onUpload = widget.events.find((event) => event.trigger === 'on_upload');
    try {
      for (const entry of staged) {
        if (entry.status === 'done') continue;
        patch(entry.id, { status: 'uploading', error: undefined });
        try {
          const { item } = await uploadItem(destinationRid, entry.file, { branch });
          patch(entry.id, {
            status: 'done',
            itemRid: item.rid,
            mediaSetRid: item.media_set_rid
          });
          if (onUpload && onAction) {
            await onAction(onUpload, {
              file_identifier: item.rid,
              media_set_rid: item.media_set_rid,
              filename: entry.file.name
            });
          }
        } catch (cause) {
          patch(entry.id, {
            status: 'error',
            error: cause instanceof Error ? cause.message : 'Upload failed'
          });
        }
      }
    } finally {
      submitting = false;
    }
  }

  function intentClass(intent: string) {
    switch (intent) {
      case 'success':
        return 'bg-emerald-600 hover:bg-emerald-700';
      case 'warning':
        return 'bg-amber-500 hover:bg-amber-600';
      case 'danger':
        return 'bg-rose-600 hover:bg-rose-700';
      case 'none':
        return 'bg-slate-500 hover:bg-slate-600';
      default:
        return 'bg-blue-600 hover:bg-blue-700';
    }
  }

  function statusLabel(entry: StagedFile) {
    switch (entry.status) {
      case 'staged':
        return 'Ready';
      case 'uploading':
        return 'Uploading…';
      case 'done':
        return 'Uploaded';
      case 'error':
        return entry.error ?? 'Error';
    }
  }
</script>

<div
  class="media-uploader-widget"
  data-testid="widget-media-uploader"
  data-widget-id={widget.id}
>
  <header>
    <strong>{widget.title || 'Upload media'}</strong>
    {#if widget.description}
      <p class="desc">{widget.description}</p>
    {/if}
  </header>

  <div class="row">
    <button
      type="button"
      class={`btn ${intentClass(intent)}`}
      data-testid="widget-media-uploader-pick"
      onclick={() => inputEl?.click()}
    >
      {buttonText}
    </button>
    <input
      bind:this={inputEl}
      type="file"
      multiple={allowMultiple}
      accept={allowedExtensions || undefined}
      class="hidden"
      data-testid="widget-media-uploader-input"
      onchange={(event) => {
        const files = (event.currentTarget as HTMLInputElement).files;
        if (files && files.length > 0) {
          stageFiles(files);
          (event.currentTarget as HTMLInputElement).value = '';
        }
      }}
    />
  </div>

  {#if staged.length > 0}
    <ul class="staged" data-testid="widget-media-uploader-staged">
      {#each staged as entry (entry.id)}
        <li class={`staged-row state-${entry.status}`}>
          <div class="meta">
            <span class="name">{entry.file.name}</span>
            <span class="size">{Math.ceil(entry.file.size / 1024)} KB</span>
          </div>
          <span class="status">{statusLabel(entry)}</span>
          {#if entry.status === 'staged'}
            <button
              type="button"
              class="link"
              onclick={() => removeStaged(entry.id)}
              aria-label="Remove staged file"
            >
              ×
            </button>
          {/if}
        </li>
      {/each}
    </ul>
    <button
      type="button"
      class="submit"
      data-testid="widget-media-uploader-submit"
      disabled={submitting || !destinationRid}
      onclick={submit}
    >
      {submitting ? 'Uploading…' : 'Submit'}
    </button>
    {#if !destinationRid}
      <p class="hint">
        Set <code>destination_rid</code> in the widget props before submitting.
      </p>
    {/if}
  {:else}
    <p class="hint" data-testid="widget-media-uploader-hint">
      Files are staged locally and only uploaded when you press <strong>Submit</strong> —
      cancelled forms leave no orphaned items behind (per Foundry contract).
    </p>
  {/if}
</div>

<style>
  .media-uploader-widget {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 12px;
    border-radius: 12px;
    border: 1px solid #e2e8f0;
    background: #ffffff;
    color: #0f172a;
  }
  header {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  header strong {
    font-size: 14px;
  }
  .desc {
    margin: 0;
    font-size: 12px;
    color: #64748b;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .btn {
    color: #ffffff;
    border: none;
    border-radius: 8px;
    padding: 8px 14px;
    cursor: pointer;
    font: inherit;
    font-weight: 500;
    font-size: 13px;
  }
  .hidden {
    display: none;
  }
  .staged {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
    border: 1px solid #e2e8f0;
    border-radius: 8px;
    overflow: hidden;
  }
  .staged-row {
    display: grid;
    grid-template-columns: 1fr auto auto;
    gap: 8px;
    align-items: center;
    padding: 6px 10px;
    font-size: 12px;
    background: #f8fafc;
    border-bottom: 1px solid #e2e8f0;
  }
  .staged-row:last-child {
    border-bottom: none;
  }
  .staged-row.state-error {
    background: #fef2f2;
    color: #b91c1c;
  }
  .staged-row.state-done {
    background: #ecfdf5;
    color: #047857;
  }
  .meta {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .name {
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .size {
    color: #94a3b8;
    font-size: 11px;
  }
  .status {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .submit {
    background: #1d4ed8;
    color: #ffffff;
    border: none;
    border-radius: 8px;
    padding: 8px 14px;
    cursor: pointer;
    font: inherit;
    font-weight: 600;
    font-size: 13px;
  }
  .submit:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }
  .link {
    background: transparent;
    border: none;
    color: #64748b;
    font-size: 18px;
    cursor: pointer;
    padding: 0 4px;
  }
  .hint {
    color: #64748b;
    font-style: italic;
    font-size: 11px;
    margin: 0;
  }
  code {
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
    background: #f1f5f9;
    padding: 0 4px;
    border-radius: 3px;
  }
</style>
