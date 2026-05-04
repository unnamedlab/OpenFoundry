<script lang="ts">
  /**
   * `/media-sets/[id]` — Foundry-style media set detail page.
   *
   * Layout (mirrors the Foundry "Media sets" detail screenshot
   * referenced in `Importing media.md`):
   *   * Header: name + schema + key chips + delete action.
   *   * `Tabs.svelte`: Overview | Items | Schedules | Permissions | History.
   *   * Items tab: 2-column layout
   *       — left:  paginated items grid (cursor-based, "Load more").
   *       — right: `MediaPreview` + metadata + contextual actions.
   *
   * Out of scope (later phases):
   *   * Schedules wiring (lands when sync runs are surfaced).
   *   * History tab UI (H4 — branches + transactions).
   */
  import { goto } from '$app/navigation';
  import { page } from '$app/stores';
  import {
    createMediaSetBranch,
    deleteItem,
    deleteMediaSet,
    deleteMediaSetBranch,
    getDownloadUrl,
    getMediaSet,
    listItems,
    listMediaSetBranches,
    type MediaItem,
    type MediaSet,
    type MediaSetBranch
  } from '$lib/api/mediaSets';
  import MediaPermissionsPanel from '$lib/components/data/MediaPermissionsPanel.svelte';
  import MediaPreview from '$lib/components/data/MediaPreview.svelte';
  import MediaSchemaIcon from '$lib/components/data/MediaSchemaIcon.svelte';
  import MediaSetActivityPanel from '$lib/components/data/MediaSetActivityPanel.svelte';
  import MediaSetBranchPicker from '$lib/components/data/MediaSetBranchPicker.svelte';
  import MediaSetHistoryPanel from '$lib/components/data/MediaSetHistoryPanel.svelte';
  import MediaSetUsagePanel from '$lib/components/data/MediaSetUsagePanel.svelte';
  import Glyph from '$lib/components/ui/Glyph.svelte';
  import Tabs from '$lib/components/ui/Tabs.svelte';
  import Tree from '$lib/components/ui/Tree.svelte';
  import { notifications as toasts } from '$stores/notifications';

  type Tab =
    | 'overview'
    | 'items'
    | 'schedules'
    | 'permissions'
    | 'activity'
    | 'history'
    | 'usage';

  const PAGE_SIZE = 24;

  const mediaSetRid = $derived(decodeURIComponent($page.params.id ?? ''));

  let mediaSet = $state<MediaSet | null>(null);
  let setLoading = $state(true);
  let setError = $state('');

  let activeTab = $state<Tab>('overview');

  // ── Branches (H4) ──────────────────────────────────────────────
  let branches = $state<MediaSetBranch[]>([]);
  let activeBranch = $state<string>('main');
  let branchBusy = $state(false);

  async function refreshBranches() {
    if (!mediaSetRid) return;
    try {
      branches = await listMediaSetBranches(mediaSetRid);
      if (!branches.some((b) => b.branch_name === activeBranch)) {
        activeBranch = 'main';
      }
    } catch (cause) {
      toasts.error(
        cause instanceof Error ? cause.message : 'Failed to load branches',
      );
    }
  }

  // ── Items tab state ────────────────────────────────────────────
  let items = $state<MediaItem[]>([]);
  let itemsLoading = $state(false);
  let itemsError = $state('');
  let nextCursor = $state<string | null>(null);
  let hasMore = $state(false);
  let selectedItem = $state<MediaItem | null>(null);

  $effect(() => {
    const rid = mediaSetRid;
    setLoading = true;
    setError = '';
    void refreshBranches();
    void getMediaSet(rid)
      .then((set) => {
        mediaSet = set;
      })
      .catch((cause) => {
        setError = cause instanceof Error ? cause.message : 'Failed to load media set';
      })
      .finally(() => {
        setLoading = false;
      });
  });

  /** Load items lazily once the operator opens the Items tab. */
  $effect(() => {
    if (activeTab === 'items' && items.length === 0 && !itemsLoading && !itemsError) {
      void loadItems(true);
    }
  });

  const tabs = $derived([
    { key: 'overview', label: 'Overview' },
    { key: 'items', label: 'Items', badge: items.length || undefined },
    { key: 'schedules', label: 'Schedules' },
    { key: 'permissions', label: 'Permissions' },
    { key: 'activity', label: 'Activity' },
    { key: 'history', label: 'History' },
    { key: 'usage', label: 'Usage' }
  ]);

  async function loadItems(reset = false) {
    if (!mediaSetRid) return;
    itemsLoading = true;
    itemsError = '';
    try {
      const next = await listItems(mediaSetRid, {
        branch: activeBranch,
        limit: PAGE_SIZE,
        cursor: reset ? undefined : (nextCursor ?? undefined)
      });
      const merged = reset ? next : [...items, ...next];
      items = merged;
      hasMore = next.length === PAGE_SIZE;
      nextCursor = next.length > 0 ? next[next.length - 1].path : null;
      if (reset && next.length > 0 && !selectedItem) {
        selectedItem = next[0];
      }
    } catch (cause) {
      itemsError = cause instanceof Error ? cause.message : 'Failed to load items';
    } finally {
      itemsLoading = false;
    }
  }

  // ── Actions ────────────────────────────────────────────────────
  async function handleDownload(item: MediaItem) {
    try {
      const { url } = await getDownloadUrl(item.rid);
      window.open(url, '_blank', 'noopener');
    } catch (cause) {
      toasts.error(cause instanceof Error ? cause.message : 'Download failed');
    }
  }

  async function handleDelete(item: MediaItem) {
    if (!confirm(`Delete item "${item.path}"? This cannot be undone.`)) return;
    try {
      await deleteItem(item.rid);
      items = items.filter((other) => other.rid !== item.rid);
      if (selectedItem?.rid === item.rid) {
        selectedItem = items[0] ?? null;
      }
      toasts.info(`Deleted ${item.path}`);
    } catch (cause) {
      toasts.error(cause instanceof Error ? cause.message : 'Delete failed');
    }
  }

  async function handleCopyMediaReference(item: MediaItem) {
    if (!mediaSet) return;
    // Foundry "media reference" JSON contract — same shape as
    // `core_models::MediaReference::to_foundry_json()` in P1.1.
    const reference = {
      mediaSetRid: item.media_set_rid,
      mediaItemRid: item.rid,
      branch: item.branch,
      schema: mediaSet.schema
    };
    const payload = JSON.stringify(reference);
    try {
      await navigator.clipboard.writeText(payload);
      toasts.success('Media reference copied to clipboard');
    } catch {
      // Older browsers / lacking permission: surface the JSON in a
      // prompt so the operator can copy manually.
      try {
        window.prompt('Copy this media reference:', payload);
      } catch {
        toasts.error('Could not copy media reference');
      }
    }
  }

  async function handleDeleteSet() {
    if (!mediaSet) return;
    if (!confirm(`Delete media set "${mediaSet.name}"? This cannot be undone.`)) return;
    try {
      await deleteMediaSet(mediaSet.rid);
      toasts.info(`Deleted "${mediaSet.name}"`);
      void goto('/media-sets');
    } catch (cause) {
      toasts.error(cause instanceof Error ? cause.message : 'Delete failed');
    }
  }

  // ── Formatters ─────────────────────────────────────────────────
  function formatBytes(n: number) {
    if (!n) return '0 B';
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
    return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
  }

  function shortRid(rid: string | null | undefined) {
    if (!rid) return '—';
    return rid.split('.').pop()?.slice(0, 12) ?? rid;
  }

</script>

<svelte:head>
  <title>{mediaSet?.name ?? 'Media set'} — OpenFoundry</title>
</svelte:head>

<div class="space-y-6">
  <!-- ── Header ─────────────────────────────────────────────── -->
  <div class="flex items-start justify-between gap-4">
    <div class="space-y-2">
      <a
        href="/media-sets"
        class="text-xs text-slate-500 hover:text-slate-700 dark:hover:text-slate-300"
      >
        ← All media sets
      </a>
      {#if setLoading}
        <div class="h-7 w-64 animate-pulse rounded bg-slate-200 dark:bg-gray-700"></div>
      {:else if setError}
        <h1 class="text-2xl font-bold text-rose-600">{setError}</h1>
      {:else if mediaSet}
        <div class="flex items-center gap-3">
          <MediaSchemaIcon schema={mediaSet.schema} size={28} />
          <h1 class="text-2xl font-bold">{mediaSet.name}</h1>
        </div>
        <div class="flex flex-wrap gap-2 text-xs text-slate-500">
          <span
            class="rounded-full bg-slate-100 px-2 py-0.5 font-medium uppercase tracking-wide dark:bg-gray-800"
          >
            {mediaSet.schema}
          </span>
          <span
            class={`rounded-full px-2 py-0.5 font-medium ${
              mediaSet.transaction_policy === 'TRANSACTIONAL'
                ? 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
                : 'bg-slate-100 text-slate-600 dark:bg-gray-800 dark:text-slate-300'
            }`}
          >
            {mediaSet.transaction_policy === 'TRANSACTIONAL'
              ? 'Transactional'
              : 'Transactionless'}
          </span>
          {#if mediaSet.virtual}
            <span
              class="rounded-full bg-purple-100 px-2 py-0.5 font-medium text-purple-700 dark:bg-purple-900/40 dark:text-purple-300"
            >
              Virtual
            </span>
          {/if}
          {#if mediaSet.source_rid}
            <!--
              Cross-app deep link to the source that backs this media set.
              The Foundry RID format is `ri.foundry.main.source.<uuid>`;
              when present we navigate via the bare uuid the data-connection
              page resolves. Anything that doesn't match the prefix gets
              passed through verbatim — the sources route shows a 404 if
              it doesn't resolve (Foundry-style).
            -->
            {@const sourceHref = `/data-connection/sources/${encodeURIComponent(
              mediaSet.source_rid.startsWith('ri.foundry.main.source.')
                ? mediaSet.source_rid.slice('ri.foundry.main.source.'.length)
                : mediaSet.source_rid
            )}`}
            <a
              href={sourceHref}
              class="rounded-full border border-blue-200 bg-blue-50 px-2 py-0.5 font-medium text-blue-700 hover:bg-blue-100 dark:border-blue-900/60 dark:bg-blue-950/40 dark:text-blue-300 dark:hover:bg-blue-950/60"
              data-testid="media-set-source-badge"
            >
              Source: {shortRid(mediaSet.source_rid)} ↗
            </a>
          {/if}
          <span class="font-mono">{shortRid(mediaSet.rid)}</span>
        </div>
      {/if}
    </div>
    {#if mediaSet}
      <div class="flex items-center gap-2">
        <MediaSetBranchPicker
          {branches}
          currentBranch={activeBranch}
          busy={branchBusy}
          onSwitch={(name) => {
            activeBranch = name;
            // Reset items state so the next visit to the Items tab
            // refetches from the new branch.
            items = [];
            nextCursor = null;
            hasMore = false;
            selectedItem = null;
          }}
          onCreate={async ({ name, from_branch }) => {
            branchBusy = true;
            try {
              const created = await createMediaSetBranch(mediaSetRid, {
                name,
                from_branch,
              });
              await refreshBranches();
              activeBranch = created.branch_name;
              toasts.success(`Branch '${created.branch_name}' created`);
            } catch (cause) {
              toasts.error(
                cause instanceof Error
                  ? cause.message
                  : 'Failed to create branch',
              );
            } finally {
              branchBusy = false;
            }
          }}
          onDelete={async (name) => {
            if (
              !confirm(
                `Delete branch "${name}"? Items on this branch will be soft-deleted.`,
              )
            )
              return;
            branchBusy = true;
            try {
              await deleteMediaSetBranch(mediaSetRid, name);
              await refreshBranches();
              if (activeBranch === name) activeBranch = 'main';
              toasts.info(`Branch '${name}' deleted`);
            } catch (cause) {
              toasts.error(
                cause instanceof Error
                  ? cause.message
                  : 'Failed to delete branch',
              );
            } finally {
              branchBusy = false;
            }
          }}
        />
        <button
          type="button"
          class="rounded-xl border border-rose-200 px-3 py-2 text-sm text-rose-600 hover:bg-rose-50 dark:border-rose-900/40 dark:hover:bg-rose-950/30"
          data-testid="media-set-detail-delete"
          onclick={handleDeleteSet}
        >
          Delete media set
        </button>
      </div>
    {/if}
  </div>

  <!-- ── Tabs ────────────────────────────────────────────── -->
  <Tabs items={tabs} bind:activeKey={activeTab} ariaLabel="Media set views" />

  <!-- ── Tab content ─────────────────────────────────────── -->
  {#if activeTab === 'overview'}
    {#if mediaSet}
      <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        <div
          class="space-y-1 rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900"
        >
          <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Project</div>
          <div class="break-all font-mono text-sm">{mediaSet.project_rid}</div>
        </div>
        <div
          class="space-y-1 rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900"
        >
          <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Retention</div>
          <div class="text-sm">
            {mediaSet.retention_seconds
              ? `${mediaSet.retention_seconds} seconds`
              : 'Retain forever'}
          </div>
        </div>
        <div
          class="space-y-1 rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900"
        >
          <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Allowed MIME types</div>
          <ul class="text-sm">
            {#each mediaSet.allowed_mime_types as mime (mime)}
              <li class="font-mono">{mime}</li>
            {:else}
              <li class="text-slate-400">Any</li>
            {/each}
          </ul>
        </div>
        {#if mediaSet.virtual}
          <div
            class="space-y-1 rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900"
          >
            <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Source</div>
            <div class="break-all font-mono text-sm">{mediaSet.source_rid ?? '—'}</div>
          </div>
        {/if}
        <div
          class="space-y-1 rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900"
        >
          <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Created</div>
          <div class="text-sm">{new Date(mediaSet.created_at).toLocaleString()}</div>
          <div class="text-xs text-slate-500">by {shortRid(mediaSet.created_by)}</div>
        </div>
      </div>
    {/if}
  {:else if activeTab === 'items'}
    <div class="grid gap-4 lg:grid-cols-[minmax(0,3fr),minmax(0,4fr)]">
      <!-- ── Left: items grid ─────────────────────────────── -->
      <div
        class="rounded-2xl border border-slate-200 bg-white p-3 shadow-sm dark:border-gray-700 dark:bg-gray-900"
      >
        {#if itemsLoading && items.length === 0}
          <div class="grid grid-cols-2 gap-2 sm:grid-cols-3" data-testid="items-skeleton">
            {#each Array.from({ length: 6 }) as _, i (i)}
              <div
                class="h-24 animate-pulse rounded-xl bg-slate-100 dark:bg-gray-800"
              ></div>
            {/each}
          </div>
        {:else if itemsError}
          <div
            class="rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300"
          >
            {itemsError}
          </div>
        {:else if items.length === 0}
          <div class="py-12 text-center text-sm text-slate-500" data-testid="items-empty">
            No items yet. Use the "Upload" button on
            <a href="/media-sets" class="text-blue-600 hover:underline">
              the media-sets list
            </a>
            to drop files.
          </div>
        {:else}
          <ul
            class="grid grid-cols-2 gap-2 overflow-auto sm:grid-cols-3"
            style="max-height: calc(100vh - 320px);"
            data-testid="items-grid"
          >
            {#each items as item (item.rid)}
              <li>
                <button
                  type="button"
                  data-testid="item-card"
                  data-item-rid={item.rid}
                  class={`flex w-full flex-col items-start gap-1 rounded-xl border p-2 text-left text-xs transition-colors ${
                    selectedItem?.rid === item.rid
                      ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20'
                      : 'border-slate-200 bg-white hover:border-blue-400 dark:border-gray-700 dark:bg-gray-900'
                  }`}
                  onclick={() => (selectedItem = item)}
                >
                  <div class="flex items-center gap-1 text-slate-500">
                    {#if mediaSet}
                      <MediaSchemaIcon schema={mediaSet.schema} size={14} />
                    {:else}
                      <Glyph name="cube" size={14} />
                    {/if}
                    <span class="truncate font-mono text-[10px]">
                      {item.mime_type || 'unknown'}
                    </span>
                  </div>
                  <div class="line-clamp-2 break-all font-medium text-slate-700 dark:text-slate-100">
                    {item.path}
                  </div>
                  <div class="text-[10px] text-slate-400 tabular-nums">
                    {formatBytes(item.size_bytes)}
                  </div>
                </button>
              </li>
            {/each}
          </ul>
          {#if hasMore}
            <button
              type="button"
              class="mt-3 w-full rounded-xl border border-slate-200 px-3 py-2 text-xs hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800"
              disabled={itemsLoading}
              data-testid="items-load-more"
              onclick={() => loadItems(false)}
            >
              {itemsLoading ? 'Loading…' : 'Load more'}
            </button>
          {/if}
        {/if}
      </div>

      <!-- ── Right: preview + metadata + actions ──────────── -->
      <div
        class="flex min-h-[400px] flex-col rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900"
      >
        {#if !selectedItem}
          <div
            class="flex flex-1 items-center justify-center text-sm text-slate-500"
            data-testid="preview-empty"
          >
            Select an item to preview it.
          </div>
        {:else}
          <div
            class="flex flex-wrap items-center justify-between gap-2 border-b border-slate-100 pb-3 dark:border-gray-800"
          >
            <div class="min-w-0">
              <div class="truncate font-medium" data-testid="preview-path">
                {selectedItem.path}
              </div>
              <div class="text-[11px] text-slate-500">
                {selectedItem.mime_type || 'unknown'} ·
                {formatBytes(selectedItem.size_bytes)}
              </div>
            </div>
            <div class="flex flex-wrap items-center gap-2 text-xs">
              <button
                type="button"
                class="rounded-xl border border-slate-200 px-3 py-1 hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800"
                data-testid="action-download"
                onclick={() => selectedItem && handleDownload(selectedItem)}
              >
                Download
              </button>
              <button
                type="button"
                class="rounded-xl border border-slate-200 px-3 py-1 hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800"
                data-testid="action-copy-reference"
                onclick={() => selectedItem && handleCopyMediaReference(selectedItem)}
              >
                Copy media reference
              </button>
              <button
                type="button"
                class="rounded-xl border border-rose-200 px-3 py-1 text-rose-600 hover:bg-rose-50 dark:border-rose-900/40 dark:hover:bg-rose-950/30"
                data-testid="action-delete"
                onclick={() => selectedItem && handleDelete(selectedItem)}
              >
                Delete
              </button>
            </div>
          </div>

          <!-- Preview viewer. -->
          <div class="my-3 min-h-0 flex-1">
            {#key selectedItem.rid}
              <MediaPreview
                item={selectedItem}
                onerror={(msg) => toasts.error(msg)}
              />
            {/key}
          </div>

          <!-- Metadata panel. -->
          <div
            class="space-y-2 border-t border-slate-100 pt-3 text-xs dark:border-gray-800"
            data-testid="preview-metadata"
          >
            <div class="grid grid-cols-2 gap-1">
              <span class="text-slate-500">Path:</span>
              <span class="break-all font-mono">{selectedItem.path}</span>
              <span class="text-slate-500">MIME:</span>
              <span class="font-mono">{selectedItem.mime_type || '—'}</span>
              <span class="text-slate-500">Size:</span>
              <span class="font-mono">{selectedItem.size_bytes} bytes</span>
              <span class="text-slate-500">SHA-256:</span>
              <span class="break-all font-mono">{selectedItem.sha256 || '—'}</span>
              <span class="text-slate-500">Created at:</span>
              <span class="font-mono">{new Date(selectedItem.created_at).toLocaleString()}</span>
              <span class="text-slate-500">Transaction:</span>
              <span class="font-mono">{selectedItem.transaction_rid || '—'}</span>
              {#if selectedItem.deduplicated_from}
                <span class="text-slate-500">Replaces:</span>
                <span class="break-all font-mono">{selectedItem.deduplicated_from}</span>
              {/if}
            </div>
            <div>
              <div class="mb-1 text-slate-500">Custom metadata:</div>
              <Tree data={selectedItem.metadata ?? {}} label="metadata" />
            </div>
          </div>
        {/if}
      </div>
    </div>
  {:else if activeTab === 'schedules'}
    <div
      class="rounded-2xl border border-dashed border-slate-300 bg-white p-10 text-center text-sm text-slate-500 dark:border-gray-700 dark:bg-gray-900"
      data-testid="tab-schedules-placeholder"
    >
      Schedules surface lights up once `connector-management-service` exposes
      the per-set sync runs (P1.4 already persists them).
    </div>
  {:else if activeTab === 'permissions'}
    {#if mediaSet}
      <MediaPermissionsPanel
        {mediaSet}
        onChanged={(next) => (mediaSet = next)}
      />
    {/if}
  {:else if activeTab === 'activity'}
    {#if mediaSet}
      <MediaSetActivityPanel {mediaSet} />
    {/if}
  {:else if activeTab === 'history'}
    {#if mediaSet}
      <MediaSetHistoryPanel
        mediaSet={mediaSet}
        onBranchCreated={(name) => {
          activeBranch = name;
          void refreshBranches();
        }}
      />
    {/if}
  {:else if activeTab === 'usage'}
    {#if mediaSet}
      <MediaSetUsagePanel {mediaSet} />
    {/if}
  {/if}
</div>

<!-- Local toast region — shared store, page-local renderer. -->
<div class="pointer-events-none fixed bottom-4 right-4 z-[100] flex flex-col gap-2">
  {#each $toasts as toast (toast.id)}
    <div
      role="status"
      class={`pointer-events-auto rounded-xl border px-4 py-2 text-sm shadow-md ${
        toast.type === 'success'
          ? 'border-emerald-200 bg-emerald-50 text-emerald-800'
          : toast.type === 'error'
            ? 'border-rose-200 bg-rose-50 text-rose-800'
            : toast.type === 'warning'
              ? 'border-amber-200 bg-amber-50 text-amber-800'
              : 'border-slate-200 bg-white text-slate-700'
      }`}
      data-testid="media-set-detail-toast"
    >
      {toast.message}
    </div>
  {/each}
</div>
