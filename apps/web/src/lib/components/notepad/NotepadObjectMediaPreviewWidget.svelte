<!--
  NotepadObjectMediaPreviewWidget — H6 Notepad widget that renders a
  media-reference object property inside a Notepad document.

  Per `Object media preview.md`, the widget supports media references
  (the H6 surface) plus media-string URLs and attachment properties
  (deferred). This component handles the canonical media-reference
  case; the widget manifest can compose multiple variants later.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import MediaPreview from '$lib/components/data/MediaPreview.svelte';
  import { getItem, type MediaItem } from '$lib/api/mediaSets';

  type MediaReferenceJson = {
    mediaSetRid?: string;
    mediaItemRid?: string;
    media_set_rid?: string;
    media_item_rid?: string;
    branch?: string;
    schema?: string;
  };

  type Props = {
    /** Heading rendered above the preview. */
    title?: string;
    /** Property value held by the bound object. */
    value: MediaReferenceJson | null | undefined;
    /** When true, the widget expands to fit a multi-page document
     *  on print (mirrors the doc's "Auto-expand widget height on
     *  print" toggle). */
    autoExpandOnPrint?: boolean;
  };

  let { title = 'Object media preview', value, autoExpandOnPrint = false }: Props = $props();

  const itemRid = $derived(value?.mediaItemRid ?? value?.media_item_rid ?? null);

  let item = $state<MediaItem | null>(null);
  let loading = $state(false);
  let error = $state('');

  $effect(() => {
    void itemRid;
    if (!itemRid) {
      item = null;
      return;
    }
    loading = true;
    error = '';
    void getItem(itemRid)
      .then((row) => {
        item = row;
      })
      .catch((cause) => {
        error = cause instanceof Error ? cause.message : 'Failed to load media item';
      })
      .finally(() => {
        loading = false;
      });
  });
</script>

<section
  class="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900"
  class:print-auto-expand={autoExpandOnPrint}
  data-testid="notepad-object-media-preview-widget"
>
  <header class="mb-2 flex items-center justify-between gap-3">
    <h3 class="text-sm font-semibold">{title}</h3>
    {#if itemRid}
      <span
        class="rounded-full bg-slate-100 px-2 py-0.5 font-mono text-[10px] text-slate-600 dark:bg-gray-800 dark:text-slate-300"
        data-testid="notepad-object-media-rid"
      >
        {itemRid}
      </span>
    {/if}
  </header>
  {#if !itemRid}
    <div
      class="flex h-40 items-center justify-center rounded-xl border border-dashed border-slate-300 text-sm text-slate-400 dark:border-gray-700"
      data-testid="notepad-object-media-empty"
    >
      No media attached to this property.
    </div>
  {:else if loading}
    <div
      class="h-40 animate-pulse rounded-xl bg-slate-100 dark:bg-gray-800"
      data-testid="notepad-object-media-loading"
    ></div>
  {:else if error}
    <div
      class="rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300"
      data-testid="notepad-object-media-error"
    >
      {error}
    </div>
  {:else if item}
    <MediaPreview {item} onerror={(msg) => (error = msg)} />
  {/if}
</section>

<style>
  /* Notepad print export: when the operator enables
     "Auto-expand widget height on print", the widget grows to fit
     the full media item rather than truncating mid-document. */
  @media print {
    .print-auto-expand {
      max-height: none !important;
      overflow: visible !important;
    }
  }
</style>
