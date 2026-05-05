<!--
  QuiverMediaPropertyCard — H6 Quiver card that renders a media
  reference attached to an object property.

  Foundry's "Media property" detail-card doc lists this as a
  single-object input + flow-end output; the simplest faithful port
  is "given an object + a media-reference property name, embed the
  MediaPreview for the pointed-to item". The card is intentionally
  read-only: edits flow through actions (Upload media) or the
  inline-edit cell.
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
    /** Object label / id surfaced above the preview. */
    objectLabel: string;
    /** Property name (UI-facing) — e.g. "Cover photo". */
    propertyName: string;
    /** The media-reference JSON value held by the property. */
    value: MediaReferenceJson | null | undefined;
  };

  let { objectLabel, propertyName, value }: Props = $props();

  const itemRid = $derived(value?.mediaItemRid ?? value?.media_item_rid ?? null);
  const setRid = $derived(value?.mediaSetRid ?? value?.media_set_rid ?? null);

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

<article
  class="rounded-2xl border border-slate-200 bg-white p-3 shadow-sm dark:border-gray-700 dark:bg-gray-900"
  data-testid="quiver-media-property-card"
  data-set-rid={setRid ?? ''}
  data-item-rid={itemRid ?? ''}
>
  <header class="flex items-baseline justify-between gap-3">
    <div class="min-w-0">
      <h3 class="truncate text-sm font-semibold">{propertyName}</h3>
      <p class="truncate text-xs text-slate-500">{objectLabel}</p>
    </div>
    {#if itemRid}
      <span
        class="rounded-full bg-slate-100 px-2 py-0.5 font-mono text-[10px] text-slate-600 dark:bg-gray-800 dark:text-slate-300"
      >
        {itemRid.split('.').pop()?.slice(0, 8) ?? itemRid}
      </span>
    {/if}
  </header>

  <div class="mt-3 min-h-[140px]">
    {#if !itemRid}
      <div
        class="flex h-32 items-center justify-center rounded-xl border border-dashed border-slate-300 text-xs text-slate-400 dark:border-gray-700"
        data-testid="quiver-media-property-card-empty"
      >
        No media attached.
      </div>
    {:else if loading}
      <div
        class="h-32 animate-pulse rounded-xl bg-slate-100 dark:bg-gray-800"
        data-testid="quiver-media-property-card-loading"
      ></div>
    {:else if error}
      <div
        class="rounded-xl border border-rose-200 bg-rose-50 p-2 text-xs text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300"
        data-testid="quiver-media-property-card-error"
      >
        {error}
      </div>
    {:else if item}
      <MediaPreview
        item={item}
        onerror={(msg) => (error = msg)}
      />
    {/if}
  </div>
</article>
