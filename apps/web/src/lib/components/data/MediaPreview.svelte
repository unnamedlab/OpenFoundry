<script lang="ts">
  /**
   * `MediaPreview` — mime-type-dispatched viewer for a single
   * `MediaItem`, used by the media-set detail page right panel.
   *
   * Mirrors the Foundry "Media Preview" widget behaviour
   * (`docs_original_palantir_foundry/.../Media Preview.md`): a single
   * generic surface that picks the appropriate sub-viewer (image,
   * audio, video, document) based on the media item's MIME.
   *
   * Props:
   *   * `item: MediaItem` — the media item to render.
   *   * `onerror?: (msg) => void` — fired when the presigned download
   *     URL request fails or the underlying media element errors out.
   *
   * Behaviour per kind:
   *   * `image/*`         — `<img>` with zoom slider + rotate button.
   *                          When `width > 4096` the access-pattern
   *                          thumbnail is preferred (placeholder URL
   *                          until H4 wires `RunAccessPattern`).
   *   * `application/pdf` — link "Open externally" because pdf.js is
   *                          not in the bundle (per spec).
   *   * `audio/*`         — `<audio controls>` plus a placeholder
   *                          waveform thumbnail when an access pattern
   *                          of kind `waveform` is registered.
   *   * `video/*`         — `<video controls>`. HLS playback is
   *                          gated on the future `kind=hls` access
   *                          pattern; without it we fall back to
   *                          direct progressive playback.
   *   * everything else   — "Preview not available" + Download CTA.
   *
   * The component never fetches bytes itself — only the presigned URL
   * goes through `getDownloadUrl()`. The `<img|audio|video>` element
   * downloads the body directly from object storage.
   */
  import { getDownloadUrl, type MediaItem } from '$lib/api/mediaSets';

  type Props = {
    item: MediaItem;
    /**
     * The width (in CSS pixels) above which the image preview should
     * prefer the `thumbnail` access pattern. The default mirrors the
     * Foundry recommendation in
     * `docs_original_palantir_foundry/.../Importing media.md`.
     */
    largeImageThresholdPx?: number;
    onerror?: (message: string) => void;
  };

  let { item, largeImageThresholdPx = 4096, onerror }: Props = $props();

  // ── Resolved presigned URL state ──────────────────────────────
  let url = $state<string | null>(null);
  let loadingUrl = $state(false);
  let urlError = $state('');

  /**
   * Refresh the presigned URL whenever the selected item changes. We
   * also reset the per-image transform state (zoom + rotation) so a
   * new image starts at 1× / 0°.
   */
  $effect(() => {
    const target = item;
    let aborted = false;
    loadingUrl = true;
    urlError = '';
    url = null;
    zoom = 1;
    rotation = 0;
    pdfPage = 1;
    void getDownloadUrl(target.rid)
      .then((response) => {
        if (aborted) return;
        url = response.url;
      })
      .catch((cause) => {
        if (aborted) return;
        const msg =
          cause instanceof Error
            ? cause.message
            : 'Failed to resolve download URL';
        urlError = msg;
        onerror?.(msg);
      })
      .finally(() => {
        if (!aborted) loadingUrl = false;
      });
    return () => {
      aborted = true;
    };
  });

  // ── Image transform state ─────────────────────────────────────
  let zoom = $state(1);
  let rotation = $state(0);
  let imageNaturalWidth = $state(0);

  // ── PDF state (paginated placeholder) ─────────────────────────
  let pdfPage = $state(1);
  // PDF page count is unknown without pdf.js. We expose the control
  // anyway and let the user step manually; a real parser plugs in at H4.
  const PDF_PAGE_PLACEHOLDER_TOTAL = 1;

  // ── Kind dispatch ─────────────────────────────────────────────
  type Kind = 'image' | 'audio' | 'video' | 'pdf' | 'csv' | 'office' | 'other';

  const kind = $derived<Kind>(
    item.mime_type.startsWith('image/')
      ? 'image'
      : item.mime_type.startsWith('audio/')
        ? 'audio'
        : item.mime_type.startsWith('video/')
          ? 'video'
          : item.mime_type === 'application/pdf'
            ? 'pdf'
            : item.mime_type === 'text/csv'
              ? 'csv'
              : item.mime_type.startsWith(
                    'application/vnd.openxmlformats-officedocument.'
                  )
                ? 'office'
                : 'other'
  );

  /**
   * Foundry "access pattern" thumbnail URL. The execution provider
   * lands in H4 (proto `AccessPatternService.RunAccessPattern`); for
   * now we synthesise a deterministic placeholder URL the backend will
   * resolve once the runtime ships.
   */
  function thumbnailAccessPatternUrl(rid: string): string {
    return `/api/v1/access-patterns/run?kind=thumbnail&item=${encodeURIComponent(rid)}`;
  }

  function shouldPreferThumbnail() {
    return kind === 'image' && imageNaturalWidth > largeImageThresholdPx;
  }
</script>

<section
  class="flex h-full flex-col gap-3"
  data-testid="media-preview"
  data-kind={kind}
>
  {#if loadingUrl}
    <div class="flex flex-1 items-center justify-center">
      <div
        class="h-32 w-32 animate-pulse rounded-2xl bg-slate-100 dark:bg-gray-800"
        data-testid="media-preview-skeleton"
      ></div>
    </div>
  {:else if urlError}
    <div
      class="flex flex-1 items-center justify-center rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300"
      data-testid="media-preview-error"
    >
      {urlError}
    </div>
  {:else if !url}
    <div class="flex flex-1 items-center justify-center text-sm text-slate-500">
      No preview URL
    </div>
  {:else if kind === 'image'}
    <!-- ── Image viewer — zoom + rotate ──────────────────────── -->
    <div
      class="flex flex-1 items-center justify-center overflow-auto rounded-xl bg-slate-50 p-4 dark:bg-gray-800"
      data-testid="media-preview-image-frame"
    >
      <!-- svelte-ignore a11y_img_redundant_alt -->
      <img
        src={shouldPreferThumbnail() ? thumbnailAccessPatternUrl(item.rid) : url}
        alt={`Preview of ${item.path}`}
        data-testid="media-preview-image"
        class="max-h-full max-w-full select-none"
        style:transform={`scale(${zoom}) rotate(${rotation}deg)`}
        style:transition="transform 120ms ease-out"
        onload={(event) => {
          imageNaturalWidth = (event.currentTarget as HTMLImageElement).naturalWidth;
        }}
        onerror={() => onerror?.('image element failed to load')}
      />
    </div>
    <div class="flex items-center gap-3 text-xs text-slate-500">
      <label class="flex items-center gap-2">
        Zoom
        <input
          type="range"
          min="0.25"
          max="4"
          step="0.05"
          bind:value={zoom}
          aria-label="Zoom"
        />
        <span class="tabular-nums">{(zoom * 100).toFixed(0)}%</span>
      </label>
      <button
        type="button"
        class="rounded border border-slate-300 px-2 py-0.5 hover:bg-slate-100 dark:border-gray-700 dark:hover:bg-gray-800"
        onclick={() => (rotation = (rotation + 90) % 360)}
        aria-label="Rotate 90°"
        data-testid="media-preview-rotate"
      >
        Rotate {rotation}°
      </button>
      {#if shouldPreferThumbnail()}
        <span class="text-[11px] italic text-slate-400">
          Showing thumbnail (image &gt; {largeImageThresholdPx}px wide)
        </span>
      {/if}
    </div>
  {:else if kind === 'pdf'}
    <!-- ── PDF — placeholder until pdf.js lands in H4 ───────── -->
    <div
      class="flex flex-1 flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-slate-300 p-6 text-center dark:border-gray-700"
      data-testid="media-preview-pdf-placeholder"
    >
      <p class="text-sm text-slate-700 dark:text-slate-200">
        Inline PDF viewer ships with H4. Open the file in a new tab to read it now.
      </p>
      <a
        href={url}
        target="_blank"
        rel="noopener"
        class="rounded-xl bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
        data-testid="media-preview-pdf-open"
      >
        Open externally
      </a>
      <div class="flex items-center gap-2 text-xs text-slate-500">
        <button
          type="button"
          class="rounded border border-slate-300 px-2 py-0.5 hover:bg-slate-100 dark:border-gray-700 dark:hover:bg-gray-800"
          onclick={() => (pdfPage = Math.max(1, pdfPage - 1))}
        >
          Prev
        </button>
        <span>Page {pdfPage} / {PDF_PAGE_PLACEHOLDER_TOTAL}</span>
        <button
          type="button"
          class="rounded border border-slate-300 px-2 py-0.5 hover:bg-slate-100 dark:border-gray-700 dark:hover:bg-gray-800"
          onclick={() => (pdfPage += 1)}
        >
          Next
        </button>
      </div>
    </div>
  {:else if kind === 'audio'}
    <!-- ── Audio — controls + waveform placeholder ─────────── -->
    <div
      class="flex flex-1 flex-col items-stretch justify-center gap-3 rounded-xl bg-slate-50 p-4 dark:bg-gray-800"
      data-testid="media-preview-audio-frame"
    >
      <!-- svelte-ignore a11y_media_has_caption -->
      <audio
        controls
        src={url}
        data-testid="media-preview-audio"
        class="w-full"
        onerror={() => onerror?.('audio element failed to load')}
      ></audio>
      <div
        class="h-16 w-full rounded bg-gradient-to-r from-sky-200 via-sky-400 to-sky-200 dark:from-sky-900 dark:via-sky-700 dark:to-sky-900"
        aria-label="Waveform placeholder"
        data-testid="media-preview-waveform-placeholder"
      ></div>
      <p class="text-[11px] italic text-slate-400">
        Waveform rendering activates once the `waveform` access pattern is
        registered.
      </p>
    </div>
  {:else if kind === 'video'}
    <!-- ── Video — direct playback, HLS gates on H4 access pattern ─ -->
    <div
      class="flex flex-1 items-center justify-center overflow-hidden rounded-xl bg-black"
      data-testid="media-preview-video-frame"
    >
      <!-- svelte-ignore a11y_media_has_caption -->
      <video
        controls
        src={url}
        data-testid="media-preview-video"
        class="max-h-full max-w-full"
        onerror={() => onerror?.('video element failed to load')}
      ></video>
    </div>
  {:else if kind === 'csv' || kind === 'office'}
    <!-- ── Tabular fallback ───────────────────────────────── -->
    <div
      class="flex flex-1 flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-slate-300 p-6 text-center dark:border-gray-700"
      data-testid="media-preview-tabular-fallback"
    >
      <p class="text-sm text-slate-700 dark:text-slate-200">
        Preview not available for this file type. Download to view it locally.
      </p>
      <a
        href={url}
        download={item.path}
        class="rounded-xl bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
        data-testid="media-preview-download"
      >
        Download
      </a>
    </div>
  {:else}
    <div
      class="flex flex-1 flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-slate-300 p-6 text-center dark:border-gray-700"
      data-testid="media-preview-other-fallback"
    >
      <p class="text-sm text-slate-700 dark:text-slate-200">
        Inline preview is not supported for {item.mime_type || 'this MIME type'}.
      </p>
      <a
        href={url}
        download={item.path}
        class="rounded-xl bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
      >
        Download
      </a>
    </div>
  {/if}
</section>
