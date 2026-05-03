<script lang="ts">
  /**
   * `MediaSetUploader` — drag-and-drop / click-to-pick uploader for a
   * single media set. Mirrors the Foundry "Importing media → Direct
   * upload" UX:
   *
   * * MIME types are validated against the parent set's
   *   `allowed_mime_types` *before* any byte leaves the browser.
   * * No confirmation prompt is shown when a file's path collides with
   *   an existing live item — the docs explicitly state that
   *   "no warning or confirmation will appear before an upload
   *   overwrites an existing media item." We surface a non-blocking
   *   toast (via `$stores/notifications`) so the operator at least
   *   notices the dedup happened.
   * * One progress row per file with a percentage bar; rows persist
   *   so the operator sees the full batch outcome.
   *
   * Wire:
   *   <MediaSetUploader mediaSet={set} onUploaded={() => refreshItems()} />
   */
  import { uploadItem, type MediaSet, type MediaItem } from '$lib/api/mediaSets';
  import { notifications as toasts } from '$stores/notifications';

  type Props = {
    mediaSet: MediaSet;
    /** Fires once per successfully-registered item. */
    onUploaded?: (item: MediaItem) => void;
  };

  let { mediaSet, onUploaded }: Props = $props();

  type UploadStatus = 'queued' | 'uploading' | 'done' | 'rejected' | 'error';

  type UploadRow = {
    id: string;
    name: string;
    size: number;
    mime: string;
    status: UploadStatus;
    progress: number;
    /** For rejected / errored rows. */
    detail?: string;
  };

  let dragging = $state(false);
  let rows = $state<UploadRow[]>([]);
  let inputEl: HTMLInputElement | null = $state(null);

  // Empty `allowed_mime_types` means "any MIME accepted by the schema".
  // The validator therefore short-circuits to `true` when the parent
  // set was created without an explicit allow-list.
  function isMimeAllowed(file: File): boolean {
    if (mediaSet.allowed_mime_types.length === 0) return true;
    const mime = file.type || 'application/octet-stream';
    return mediaSet.allowed_mime_types.some(
      (allowed) => allowed.toLowerCase() === mime.toLowerCase()
    );
  }

  function makeId() {
    return crypto.randomUUID?.() ?? `up-${Math.random().toString(36).slice(2)}`;
  }

  function appendRow(file: File): UploadRow {
    const row: UploadRow = {
      id: makeId(),
      name: file.name,
      size: file.size,
      mime: file.type || 'application/octet-stream',
      status: 'queued',
      progress: 0
    };
    rows = [...rows, row];
    return row;
  }

  function patchRow(id: string, patch: Partial<UploadRow>) {
    rows = rows.map((row) => (row.id === id ? { ...row, ...patch } : row));
  }

  async function uploadOne(row: UploadRow, file: File) {
    patchRow(row.id, { status: 'uploading', progress: 0 });
    try {
      const { item } = await uploadItem(mediaSet.rid, file, {
        branch: 'main',
        onProgress: (fraction) => patchRow(row.id, { progress: fraction })
      });
      patchRow(row.id, { status: 'done', progress: 1 });
      if (item.deduplicated_from) {
        // Foundry path-dedup contract: surface as a passive toast,
        // never a confirmation prompt.
        toasts.warning(`Path overwrites existing item: ${item.path}`);
      } else {
        toasts.success(`Uploaded ${item.path}`);
      }
      onUploaded?.(item);
    } catch (cause) {
      patchRow(row.id, {
        status: 'error',
        detail: cause instanceof Error ? cause.message : String(cause)
      });
      toasts.error(`Failed to upload ${file.name}`);
    }
  }

  async function ingest(files: FileList | File[]) {
    const list = Array.from(files);
    for (const file of list) {
      if (!isMimeAllowed(file)) {
        const row = appendRow(file);
        patchRow(row.id, {
          status: 'rejected',
          detail: `MIME ${file.type || 'unknown'} not allowed by ${mediaSet.schema} schema`
        });
        toasts.error(`${file.name}: MIME type not allowed`);
        continue;
      }
      const row = appendRow(file);
      // Sequential to keep progress feedback ordered + to avoid
      // hammering the storage backend during a smoke test.
      await uploadOne(row, file);
    }
  }

  function onDragOver(event: DragEvent) {
    event.preventDefault();
    dragging = true;
  }

  function onDragLeave(event: DragEvent) {
    event.preventDefault();
    dragging = false;
  }

  function onDrop(event: DragEvent) {
    event.preventDefault();
    dragging = false;
    if (!event.dataTransfer) return;
    void ingest(event.dataTransfer.files);
  }

  function onPick(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    if (input.files && input.files.length > 0) {
      void ingest(input.files);
      input.value = '';
    }
  }

  function statusLabel(row: UploadRow) {
    switch (row.status) {
      case 'queued':
        return 'Queued';
      case 'uploading':
        return `Uploading… ${Math.round(row.progress * 100)}%`;
      case 'done':
        return 'Uploaded';
      case 'rejected':
        return 'Rejected';
      case 'error':
        return 'Failed';
    }
  }

  function statusClass(row: UploadRow) {
    switch (row.status) {
      case 'done':
        return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300';
      case 'rejected':
      case 'error':
        return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300';
      case 'uploading':
        return 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300';
      default:
        return 'bg-slate-100 text-slate-700 dark:bg-gray-800 dark:text-slate-300';
    }
  }

  function formatBytes(n: number) {
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${(n / 1024 / 1024).toFixed(1)} MB`;
  }
</script>

<div class="space-y-4">
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="rounded-2xl border-2 border-dashed p-8 text-center transition-colors {dragging
      ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20'
      : 'border-slate-300 dark:border-gray-700'}"
    role="button"
    tabindex="0"
    aria-label="Upload media files"
    data-testid="media-set-uploader-dropzone"
    ondragover={onDragOver}
    ondragleave={onDragLeave}
    ondrop={onDrop}
    onclick={() => inputEl?.click()}
  >
    <p class="text-sm font-semibold text-slate-700 dark:text-slate-200">
      Drop files here to upload
    </p>
    <p class="mt-1 text-xs text-slate-500">
      Or
      <button
        type="button"
        class="underline hover:text-blue-600"
        onclick={(event) => {
          event.stopPropagation();
          inputEl?.click();
        }}>choose from your computer</button
      >
    </p>
    {#if mediaSet.allowed_mime_types.length > 0}
      <p class="mt-3 text-[11px] text-slate-400">
        Accepted MIME types: {mediaSet.allowed_mime_types.join(', ')}
      </p>
    {/if}
    <input
      bind:this={inputEl}
      type="file"
      multiple
      class="hidden"
      data-testid="media-set-uploader-input"
      onchange={onPick}
    />
  </div>

  {#if rows.length > 0}
    <ul class="space-y-2" data-testid="media-set-uploader-rows">
      {#each rows as row (row.id)}
        <li
          class="rounded-xl border border-slate-200 bg-white p-3 dark:border-gray-700 dark:bg-gray-900"
        >
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div class="min-w-0">
              <div class="truncate text-sm font-medium text-slate-700 dark:text-slate-100">
                {row.name}
              </div>
              <div class="text-[11px] text-slate-500">
                {row.mime} · {formatBytes(row.size)}
              </div>
              {#if row.detail}
                <div class="mt-1 text-[11px] text-rose-600 dark:text-rose-300">
                  {row.detail}
                </div>
              {/if}
            </div>
            <span class={`rounded-full px-2.5 py-1 text-xs font-medium ${statusClass(row)}`}>
              {statusLabel(row)}
            </span>
          </div>
          {#if row.status === 'uploading' || (row.status === 'done' && row.progress > 0)}
            <div class="mt-2 h-1.5 overflow-hidden rounded-full bg-slate-200 dark:bg-gray-700">
              <div
                class="h-full bg-blue-500 transition-[width] duration-150 ease-linear"
                style="width: {Math.round(row.progress * 100)}%;"
              ></div>
            </div>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>
