<!--
  MediaPreviewWidget — Workshop "Media Preview" widget (Foundry:
  `docs_original_palantir_foundry/.../Media Preview.md`).

  Resolves a media reference from one of three configurations
  (matching the Foundry widget contract):
    1. `props.media_string` — a raw media URL, blobster RID, or
       `data:` URL.
    2. `props.attachment_property` — when bound to an object set, the
       referenced attachment property's `attachment_rid`.
    3. `props.media_reference_property` — Foundry "media reference"
       JSON `{ mediaSetRid, mediaItemRid, branch, schema }`.

  When the configuration resolves to a media item, the widget renders
  through the `MediaPreview` component built in U14
  (`apps/web/src/lib/components/data/MediaPreview.svelte`). Otherwise
  it falls back to a minimal `<img>` for raw URLs and a friendly empty
  state.

  This widget never instantiates writers — it only reads. Uploads are
  handled by `MediaUploaderWidget.svelte`.
-->
<script lang="ts">
  import type { AppWidget } from '$lib/api/apps';
  import { getItem, type MediaItem, type MediaSetSchema } from '$lib/api/mediaSets';
  import MediaPreview from '$lib/components/data/MediaPreview.svelte';

  type Props = {
    widget: AppWidget;
    runtimeParameters?: Record<string, string>;
  };

  let { widget, runtimeParameters = {} }: Props = $props();

  // ── Prop helpers ─────────────────────────────────────────────────
  function readString(key: string, fallback = ''): string {
    const raw = widget.props[key];
    if (typeof raw !== 'string') return fallback;
    return interpolate(raw, runtimeParameters);
  }

  function interpolate(template: string, params: Record<string, string>) {
    return template.replace(/\{\{\s*([\w.-]+)\s*\}\}/g, (_, key: string) => params[key] ?? '');
  }

  // ── Configuration resolution ────────────────────────────────────
  /**
   * Foundry's three knobs map to one of three resolution paths:
   * `mediaItemRid` (preferred) → fetch through media-sets-service.
   * `attachmentRid`            → inline attachment URL.
   * `mediaString`              → raw URL / data URL fallback.
   *
   * `media_reference_property` accepts either a JSON-encoded media
   * reference or a bare media-item RID (most common case once the
   * ontology service starts emitting MediaReference cells).
   */
  const mediaString = $derived(readString('media_string'));
  const attachmentRid = $derived(readString('attachment_property'));
  const mediaReferenceRaw = $derived(readString('media_reference_property'));

  type ResolvedReference = {
    mediaItemRid: string;
    branch?: string | null;
    schema?: MediaSetSchema | null;
  } | null;

  const reference = $derived<ResolvedReference>((() => {
    const raw = mediaReferenceRaw.trim();
    if (!raw) return null;
    if (raw.startsWith('ri.foundry.main.media_item.')) {
      return { mediaItemRid: raw, branch: null, schema: null };
    }
    try {
      const parsed = JSON.parse(raw);
      if (parsed && typeof parsed === 'object' && typeof parsed.mediaItemRid === 'string') {
        return {
          mediaItemRid: parsed.mediaItemRid as string,
          branch: typeof parsed.branch === 'string' ? parsed.branch : null,
          schema:
            typeof parsed.schema === 'string'
              ? (parsed.schema as MediaSetSchema)
              : null
        };
      }
    } catch {
      // Not JSON — fall through to "no reference".
    }
    return null;
  })());

  let item = $state<MediaItem | null>(null);
  let loadingItem = $state(false);
  let itemError = $state('');

  $effect(() => {
    const r = reference;
    if (!r) {
      item = null;
      itemError = '';
      return;
    }
    loadingItem = true;
    itemError = '';
    let aborted = false;
    void getItem(r.mediaItemRid)
      .then((next) => {
        if (aborted) return;
        item = next;
      })
      .catch((cause) => {
        if (aborted) return;
        itemError = cause instanceof Error ? cause.message : 'Failed to resolve media item';
      })
      .finally(() => {
        if (!aborted) loadingItem = false;
      });
    return () => {
      aborted = true;
    };
  });
</script>

<div
  class="media-preview-widget"
  data-testid="widget-media-preview"
  data-widget-id={widget.id}
>
  <header>
    <strong>{widget.title || 'Media preview'}</strong>
    {#if widget.description}
      <p class="desc">{widget.description}</p>
    {/if}
  </header>

  {#if item}
    <div class="frame" data-testid="widget-media-preview-frame">
      <MediaPreview {item} />
    </div>
  {:else if loadingItem}
    <div class="placeholder" data-testid="widget-media-preview-loading">
      Loading media item…
    </div>
  {:else if itemError}
    <div class="placeholder error" data-testid="widget-media-preview-error">
      {itemError}
    </div>
  {:else if attachmentRid}
    <!-- Attachment property: fall back to the ontology-actions inline URL. -->
    <!-- svelte-ignore a11y_img_redundant_alt -->
    <img
      src={`/api/v1/ontology/actions/uploads/${encodeURIComponent(attachmentRid)}`}
      alt={`Attachment ${attachmentRid}`}
      class="raw-image"
      data-testid="widget-media-preview-attachment"
    />
  {:else if mediaString}
    {#if /^data:/.test(mediaString) || /^https?:\/\//.test(mediaString)}
      <!-- svelte-ignore a11y_img_redundant_alt -->
      <img
        src={mediaString}
        alt="Media preview"
        class="raw-image"
        data-testid="widget-media-preview-string"
      />
    {:else}
      <div class="placeholder" data-testid="widget-media-preview-empty">
        Unsupported media string format. Use a media URL, data URL or media item RID.
      </div>
    {/if}
  {:else}
    <div class="placeholder" data-testid="widget-media-preview-empty">
      Configure a media string, attachment, or media reference property to show a
      preview.
    </div>
  {/if}
</div>

<style>
  .media-preview-widget {
    display: flex;
    flex-direction: column;
    gap: 10px;
    height: 100%;
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
  .frame {
    flex: 1;
    min-height: 240px;
  }
  .placeholder {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    border: 1px dashed #cbd5e1;
    border-radius: 8px;
    color: #64748b;
    font-size: 13px;
    padding: 16px;
    text-align: center;
  }
  .placeholder.error {
    color: #b91c1c;
    border-color: #fecaca;
    background: #fef2f2;
  }
  .raw-image {
    max-width: 100%;
    max-height: 100%;
    border-radius: 8px;
    object-fit: contain;
  }
</style>
