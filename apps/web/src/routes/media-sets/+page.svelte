<script lang="ts">
  /**
   * `/media-sets` — list + create + upload screen for Foundry-style
   * media sets, mirroring the Foundry "Media sets (unstructured data)"
   * UX (`docs_original_palantir_foundry/.../Importing media.md`).
   *
   * Scope (per U-spec):
   *   * Table of media sets in the active project, with per-schema
   *     glyph + retention / policy / virtual / markings columns.
   *   * "+ New media set" drawer with the create form.
   *   * Inline uploader drawer per row (drag-drop) once a set exists.
   *   * Empty-state CTA "Upload your first file" (per Foundry U13).
   *
   * Out of scope (later phases):
   *   * Branch picker (H4).
   *   * Real markings (H3) — placeholder badges only.
   *   * Source picker for virtual sets — currently a free-text RID
   *     input until the data-connection picker lands.
   */
  import { onMount } from 'svelte';

  import {
    DEFAULT_MIME_TYPES,
    MEDIA_SET_SCHEMAS,
    createMediaSet,
    deleteMediaSet,
    listItems,
    listMediaSets,
    type MediaItem,
    type MediaSet,
    type MediaSetSchema,
    type TransactionPolicy
  } from '$lib/api/mediaSets';
  import MediaSchemaIcon from '$lib/components/data/MediaSchemaIcon.svelte';
  import MediaSetUploader from '$lib/components/data/MediaSetUploader.svelte';
  import Drawer from '$lib/components/ui/Drawer.svelte';
  import Glyph from '$lib/components/ui/Glyph.svelte';
  import { notifications as toasts } from '$stores/notifications';

  // ── State ─────────────────────────────────────────────────────────
  let mediaSets = $state<MediaSet[]>([]);
  let itemCounts = $state<Record<string, number>>({});
  let totalSizeBytes = $state<Record<string, number>>({});
  let loading = $state(true);
  let error = $state('');
  let projectFilter = $state('');

  // Create-drawer form state.
  let createOpen = $state(false);
  let creating = $state(false);
  let form = $state(initialForm());

  // Upload-drawer state.
  let uploadingFor = $state<MediaSet | null>(null);

  // ── Lifecycle ─────────────────────────────────────────────────────
  onMount(load);

  function initialForm() {
    return {
      name: '',
      project_rid: 'ri.foundry.main.project.default',
      schema: 'IMAGE' as MediaSetSchema,
      allowed_mime_types: [...DEFAULT_MIME_TYPES.IMAGE],
      transaction_policy: 'TRANSACTIONLESS' as TransactionPolicy,
      retention_choice: 'forever' as 'forever' | '7d' | '30d' | '90d' | 'custom',
      retention_custom_seconds: 86_400,
      is_virtual: false,
      source_rid: ''
    };
  }

  function resetForm() {
    form = initialForm();
  }

  // The MIME-type prefill follows the schema unless the operator has
  // already edited the list — once they touch it, we stop overwriting.
  let mimePrefillFollowingSchema = $state(true);
  $effect(() => {
    if (mimePrefillFollowingSchema) {
      form.allowed_mime_types = [...DEFAULT_MIME_TYPES[form.schema]];
    }
  });

  function retentionSeconds(): number {
    switch (form.retention_choice) {
      case 'forever':
        return 0;
      case '7d':
        return 7 * 86_400;
      case '30d':
        return 30 * 86_400;
      case '90d':
        return 90 * 86_400;
      case 'custom':
        return Math.max(0, Math.floor(form.retention_custom_seconds));
    }
  }

  // ── Data loading ──────────────────────────────────────────────────
  async function load() {
    loading = true;
    error = '';
    try {
      const sets = await listMediaSets({
        project_rid: projectFilter || undefined,
        limit: 200
      });
      mediaSets = sets;
      await loadItemCounts(sets);
    } catch (cause) {
      console.error('Failed to load media sets', cause);
      error = cause instanceof Error ? cause.message : 'Failed to load media sets';
    } finally {
      loading = false;
    }
  }

  async function loadItemCounts(sets: MediaSet[]) {
    const counts: Record<string, number> = {};
    const sizes: Record<string, number> = {};
    await Promise.all(
      sets.map(async (set) => {
        try {
          const items = await listItems(set.rid, { branch: 'main', limit: 500 });
          counts[set.rid] = items.length;
          sizes[set.rid] = items.reduce((acc, item) => acc + (item.size_bytes ?? 0), 0);
        } catch {
          counts[set.rid] = 0;
          sizes[set.rid] = 0;
        }
      })
    );
    itemCounts = counts;
    totalSizeBytes = sizes;
  }

  // ── Actions ───────────────────────────────────────────────────────
  async function submitCreate(event: Event) {
    event.preventDefault();
    if (!form.name.trim() || !form.project_rid.trim()) return;
    if (form.is_virtual && !form.source_rid.trim()) {
      toasts.error('Virtual media sets require a source RID');
      return;
    }
    creating = true;
    try {
      const created = await createMediaSet({
        name: form.name.trim(),
        project_rid: form.project_rid.trim(),
        schema: form.schema,
        allowed_mime_types: form.allowed_mime_types,
        transaction_policy: form.transaction_policy,
        retention_seconds: retentionSeconds(),
        virtual_: form.is_virtual,
        source_rid: form.is_virtual ? form.source_rid.trim() : null
      });
      toasts.success(`Created media set "${created.name}"`);
      mediaSets = [created, ...mediaSets];
      itemCounts = { ...itemCounts, [created.rid]: 0 };
      totalSizeBytes = { ...totalSizeBytes, [created.rid]: 0 };
      createOpen = false;
      resetForm();
      // Re-prefill follows the schema again on next open.
      mimePrefillFollowingSchema = true;
      // Drop the operator straight into the uploader — Foundry U13
      // pushes the "upload your first file" flow immediately after
      // creation.
      uploadingFor = created;
    } catch (cause) {
      const msg = cause instanceof Error ? cause.message : 'Create failed';
      toasts.error(msg);
    } finally {
      creating = false;
    }
  }

  async function handleDelete(set: MediaSet) {
    if (!confirm(`Delete media set "${set.name}"? This cannot be undone.`)) return;
    try {
      await deleteMediaSet(set.rid);
      mediaSets = mediaSets.filter((other) => other.rid !== set.rid);
      const { [set.rid]: _count, ...countsRest } = itemCounts;
      const { [set.rid]: _size, ...sizesRest } = totalSizeBytes;
      itemCounts = countsRest;
      totalSizeBytes = sizesRest;
      toasts.info(`Deleted "${set.name}"`);
    } catch (cause) {
      const msg = cause instanceof Error ? cause.message : 'Delete failed';
      toasts.error(msg);
    }
  }

  // ── Formatting helpers ────────────────────────────────────────────

  function formatBytes(n: number) {
    if (n === 0) return '0 B';
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
    return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
  }

  function formatRetention(seconds: number) {
    if (!seconds) return 'Forever';
    if (seconds % 86_400 === 0) return `${seconds / 86_400} days`;
    if (seconds % 3600 === 0) return `${seconds / 3600} hours`;
    return `${seconds}s`;
  }

  function shortRid(rid: string) {
    const tail = rid.split('.').pop() ?? rid;
    return tail.slice(0, 12);
  }

  /**
   * Called once per file the uploader successfully registered. We
   * refresh the counts in the background but keep the drawer open so
   * the operator can see the full upload list (Foundry "Importing
   * media" UX — drag-drop stays mounted with per-row progress).
   */
  function onItemUploaded(_item: MediaItem) {
    if (uploadingFor) {
      void refreshCounts(uploadingFor);
    }
  }

  function uploaderDrawerClosed() {
    uploadingFor = null;
  }

  async function refreshCounts(set: MediaSet) {
    try {
      const items = await listItems(set.rid, { branch: 'main', limit: 500 });
      itemCounts = { ...itemCounts, [set.rid]: items.length };
      totalSizeBytes = {
        ...totalSizeBytes,
        [set.rid]: items.reduce((acc, item) => acc + (item.size_bytes ?? 0), 0)
      };
    } catch {
      // Ignore — counts will refresh next page load.
    }
  }
</script>

<svelte:head>
  <title>Media sets — OpenFoundry</title>
</svelte:head>

<div class="space-y-6">
  <div class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-bold">Media sets</h1>
      <p class="mt-1 text-sm text-gray-500">
        Foundry-style collections of unstructured media (images, audio, video, documents,
        spreadsheets, email).
      </p>
    </div>
    <button
      type="button"
      class="rounded-xl bg-blue-600 px-4 py-2 text-white hover:bg-blue-700"
      data-testid="media-sets-new-button"
      onclick={() => {
        createOpen = true;
      }}
    >
      <span class="inline-flex items-center gap-2">
        <Glyph name="plus" size={16} />
        New media set
      </span>
    </button>
  </div>

  <!-- ── Stats ──────────────────────────────────────────────────── -->
  <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
    <div
      class="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900"
    >
      <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Media sets</div>
      <div class="mt-3 text-3xl font-semibold">{mediaSets.length}</div>
    </div>
    <div
      class="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900"
    >
      <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Total items</div>
      <div class="mt-3 text-3xl font-semibold">
        {Object.values(itemCounts).reduce((a, b) => a + b, 0)}
      </div>
    </div>
    <div
      class="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900"
    >
      <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Total size</div>
      <div class="mt-3 text-3xl font-semibold">
        {formatBytes(Object.values(totalSizeBytes).reduce((a, b) => a + b, 0))}
      </div>
    </div>
    <div
      class="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900"
    >
      <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Virtual sets</div>
      <div class="mt-3 text-3xl font-semibold">
        {mediaSets.filter((set) => set.virtual).length}
      </div>
    </div>
  </div>

  <!-- ── Filter row ─────────────────────────────────────────────── -->
  <div
    class="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900"
  >
    <div class="grid gap-3 md:grid-cols-[1fr,auto]">
      <input
        type="text"
        placeholder="Filter by project RID (ri.foundry.main.project.…)"
        bind:value={projectFilter}
        class="rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700 dark:bg-gray-800"
        data-testid="media-sets-project-filter"
      />
      <button
        type="button"
        class="rounded-xl border border-slate-200 px-4 py-2 hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800"
        onclick={load}
      >
        Refresh
      </button>
    </div>
  </div>

  <!-- ── Body ───────────────────────────────────────────────────── -->
  {#if error}
    <div
      class="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300"
    >
      {error}
    </div>
  {/if}

  {#if loading}
    <!-- Skeleton rows. -->
    <div class="space-y-2" data-testid="media-sets-skeleton">
      {#each Array.from({ length: 4 }) as _, i (i)}
        <div
          class="h-12 animate-pulse rounded-xl border border-slate-200 bg-slate-50 dark:border-gray-700 dark:bg-gray-800/40"
        ></div>
      {/each}
    </div>
  {:else if mediaSets.length === 0}
    <div
      class="rounded-2xl border border-slate-200 bg-white px-6 py-16 text-center shadow-sm dark:border-gray-700 dark:bg-gray-900"
      data-testid="media-sets-empty-state"
    >
      <h2 class="text-lg font-semibold">No media sets yet</h2>
      <p class="mt-2 text-sm text-gray-500">
        Create your first media set, then drag-and-drop to upload files.
      </p>
      <button
        type="button"
        class="mt-6 rounded-xl bg-blue-600 px-4 py-2 text-white hover:bg-blue-700"
        onclick={() => {
          createOpen = true;
        }}
        data-testid="media-sets-empty-cta"
      >
        Upload your first file
      </button>
    </div>
  {:else}
    <div
      class="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm dark:border-gray-700 dark:bg-gray-900"
    >
      <table class="min-w-full divide-y divide-slate-200 dark:divide-gray-700" data-testid="media-sets-table">
        <thead class="bg-slate-50 dark:bg-gray-800/60">
          <tr class="text-left text-xs uppercase tracking-[0.18em] text-slate-500">
            <th scope="col" class="px-4 py-3">Name</th>
            <th scope="col" class="px-4 py-3">Schema</th>
            <th scope="col" class="px-4 py-3">Items</th>
            <th scope="col" class="px-4 py-3">Size</th>
            <th scope="col" class="px-4 py-3">Retention</th>
            <th scope="col" class="px-4 py-3">Policy</th>
            <th scope="col" class="px-4 py-3">Virtual</th>
            <th scope="col" class="px-4 py-3">Markings</th>
            <th scope="col" class="px-4 py-3">Created by</th>
            <th scope="col" class="px-4 py-3">Created</th>
            <th scope="col" class="px-4 py-3 text-right">Actions</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100 text-sm dark:divide-gray-800">
          {#each mediaSets as set (set.rid)}
            <tr class="hover:bg-slate-50 dark:hover:bg-gray-800/40" data-testid="media-set-row">
              <td class="px-4 py-3">
                <a
                  href="/media-sets/{encodeURIComponent(set.rid)}"
                  class="font-medium hover:text-blue-600"
                  data-testid="media-set-name"
                >
                  {set.name}
                </a>
                <div class="text-[11px] text-slate-400">{shortRid(set.rid)}</div>
              </td>
              <td class="px-4 py-3">
                <span class="inline-flex items-center gap-2">
                  <MediaSchemaIcon schema={set.schema} size={16} />
                  <span class="text-xs font-medium uppercase tracking-wide text-slate-600 dark:text-slate-300">
                    {set.schema}
                  </span>
                </span>
              </td>
              <td class="px-4 py-3 tabular-nums">{itemCounts[set.rid] ?? 0}</td>
              <td class="px-4 py-3 tabular-nums">{formatBytes(totalSizeBytes[set.rid] ?? 0)}</td>
              <td class="px-4 py-3">{formatRetention(set.retention_seconds)}</td>
              <td class="px-4 py-3">
                <span
                  class={`rounded-full px-2 py-0.5 text-[11px] font-medium ${
                    set.transaction_policy === 'TRANSACTIONAL'
                      ? 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
                      : 'bg-slate-100 text-slate-600 dark:bg-gray-800 dark:text-slate-300'
                  }`}
                >
                  {set.transaction_policy === 'TRANSACTIONAL' ? 'Transactional' : 'Transactionless'}
                </span>
              </td>
              <td class="px-4 py-3">
                {#if set.virtual}
                  <span
                    class="rounded-full bg-purple-100 px-2 py-0.5 text-[11px] font-medium text-purple-700 dark:bg-purple-900/40 dark:text-purple-300"
                  >
                    Virtual
                  </span>
                {:else}
                  <span class="text-[11px] text-slate-400">—</span>
                {/if}
              </td>
              <td class="px-4 py-3">
                {#if set.markings.length === 0}
                  <span class="text-[11px] text-slate-400">None</span>
                {:else}
                  <div class="flex flex-wrap gap-1">
                    {#each set.markings as marking (marking)}
                      <span
                        class="rounded-full bg-slate-200 px-2 py-0.5 text-[11px] font-medium text-slate-700 dark:bg-gray-700 dark:text-slate-200"
                        title="Marking placeholder — full enforcement lands in H3"
                      >
                        {marking}
                      </span>
                    {/each}
                  </div>
                {/if}
              </td>
              <td class="px-4 py-3 text-xs text-slate-500">{shortRid(set.created_by)}</td>
              <td class="px-4 py-3 text-xs text-slate-500">
                {new Date(set.created_at).toLocaleString()}
              </td>
              <td class="px-4 py-3 text-right">
                <div class="inline-flex items-center gap-2">
                  <button
                    type="button"
                    class="rounded-xl border border-slate-200 px-3 py-1 text-xs hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800"
                    data-testid="media-set-upload"
                    onclick={() => {
                      uploadingFor = set;
                    }}
                  >
                    Upload
                  </button>
                  <button
                    type="button"
                    class="rounded-xl border border-rose-200 px-3 py-1 text-xs text-rose-600 hover:bg-rose-50 dark:border-rose-900/40 dark:hover:bg-rose-950/30"
                    data-testid="media-set-delete"
                    onclick={() => handleDelete(set)}
                  >
                    Delete
                  </button>
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<!-- ── Toast region (subscribe-and-render local renderer) ──────── -->
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
      data-testid="media-sets-toast"
    >
      {toast.message}
    </div>
  {/each}
</div>

<!-- ── Create drawer ───────────────────────────────────────────── -->
<Drawer bind:open={createOpen} title="Create media set" width="540px">
  <form class="space-y-4 text-sm" onsubmit={submitCreate}>
    <div>
      <label class="block text-xs uppercase tracking-[0.18em] text-slate-400" for="ms-name">
        Name
      </label>
      <input
        id="ms-name"
        type="text"
        required
        bind:value={form.name}
        class="mt-1 w-full rounded-xl border border-slate-700 bg-slate-900 px-3 py-2 text-slate-100"
        data-testid="media-set-create-name"
      />
    </div>

    <div>
      <label class="block text-xs uppercase tracking-[0.18em] text-slate-400" for="ms-project">
        Project RID
      </label>
      <input
        id="ms-project"
        type="text"
        required
        bind:value={form.project_rid}
        class="mt-1 w-full rounded-xl border border-slate-700 bg-slate-900 px-3 py-2 text-slate-100"
        data-testid="media-set-create-project"
      />
      <p class="mt-1 text-[11px] text-slate-400">
        Project picker lands in U-Projects; for now paste the RID.
      </p>
    </div>

    <div>
      <span class="block text-xs uppercase tracking-[0.18em] text-slate-400">Schema</span>
      <div class="mt-2 grid grid-cols-3 gap-2">
        {#each MEDIA_SET_SCHEMAS as schema (schema)}
          <button
            type="button"
            class={`flex flex-col items-center gap-1 rounded-xl border px-3 py-2 text-xs ${
              form.schema === schema
                ? 'border-blue-500 bg-blue-500/10 text-blue-300'
                : 'border-slate-700 text-slate-300 hover:border-slate-500'
            }`}
            data-testid="media-set-schema-{schema}"
            onclick={() => {
              form.schema = schema;
              mimePrefillFollowingSchema = true;
            }}
          >
            <MediaSchemaIcon schema={schema} size={20} />
            <span>{schema}</span>
          </button>
        {/each}
      </div>
    </div>

    <div>
      <span class="block text-xs uppercase tracking-[0.18em] text-slate-400">Allowed MIME types</span>
      <div class="mt-2 grid grid-cols-2 gap-1">
        {#each DEFAULT_MIME_TYPES[form.schema] as mime (mime)}
          <label class="inline-flex items-center gap-2 text-xs text-slate-200">
            <input
              type="checkbox"
              checked={form.allowed_mime_types.includes(mime)}
              onchange={(event) => {
                mimePrefillFollowingSchema = false;
                const checked = (event.currentTarget as HTMLInputElement).checked;
                form.allowed_mime_types = checked
                  ? [...form.allowed_mime_types, mime]
                  : form.allowed_mime_types.filter((m) => m !== mime);
              }}
            />
            <span>{mime}</span>
          </label>
        {/each}
      </div>
    </div>

    <div>
      <span class="block text-xs uppercase tracking-[0.18em] text-slate-400">Transaction policy</span>
      <div class="mt-2 flex flex-col gap-1 text-xs text-slate-200">
        <label class="inline-flex items-center gap-2">
          <input
            type="radio"
            name="transaction_policy"
            value="TRANSACTIONLESS"
            checked={form.transaction_policy === 'TRANSACTIONLESS'}
            onchange={() => (form.transaction_policy = 'TRANSACTIONLESS')}
          />
          <span>
            <strong>Transactionless</strong> — items become readable immediately, no rollback.
          </span>
        </label>
        <label class="inline-flex items-center gap-2">
          <input
            type="radio"
            name="transaction_policy"
            value="TRANSACTIONAL"
            checked={form.transaction_policy === 'TRANSACTIONAL'}
            onchange={() => (form.transaction_policy = 'TRANSACTIONAL')}
          />
          <span>
            <strong>Transactional</strong> — writes are staged inside an open transaction.
          </span>
        </label>
      </div>
    </div>

    <div>
      <span class="block text-xs uppercase tracking-[0.18em] text-slate-400">Retention</span>
      <div class="mt-2 grid grid-cols-2 gap-2 text-xs">
        {#each ['7d', '30d', '90d', 'forever'] as choice (choice)}
          <button
            type="button"
            class={`rounded-xl border px-3 py-2 ${
              form.retention_choice === choice
                ? 'border-blue-500 bg-blue-500/10 text-blue-300'
                : 'border-slate-700 text-slate-300 hover:border-slate-500'
            }`}
            onclick={() => (form.retention_choice = choice as typeof form.retention_choice)}
          >
            {choice === 'forever' ? 'Retain forever' : choice.toUpperCase()}
          </button>
        {/each}
        <button
          type="button"
          class={`col-span-2 rounded-xl border px-3 py-2 ${
            form.retention_choice === 'custom'
              ? 'border-blue-500 bg-blue-500/10 text-blue-300'
              : 'border-slate-700 text-slate-300 hover:border-slate-500'
          }`}
          onclick={() => (form.retention_choice = 'custom')}
        >
          Custom (seconds)
        </button>
      </div>
      {#if form.retention_choice === 'custom'}
        <input
          type="number"
          min="0"
          bind:value={form.retention_custom_seconds}
          class="mt-2 w-full rounded-xl border border-slate-700 bg-slate-900 px-3 py-2 text-slate-100"
        />
      {/if}
    </div>

    <div class="rounded-xl border border-slate-700 px-3 py-2">
      <label class="inline-flex items-center gap-2 text-xs text-slate-200">
        <input type="checkbox" bind:checked={form.is_virtual} data-testid="media-set-virtual" />
        <span>
          <strong>Virtual media set</strong> — bytes stay in the source system; only metadata is
          registered.
        </span>
      </label>
      {#if form.is_virtual}
        <input
          type="text"
          required={form.is_virtual}
          placeholder="ri.foundry.main.source.…"
          bind:value={form.source_rid}
          class="mt-2 w-full rounded-xl border border-slate-700 bg-slate-900 px-3 py-2 text-slate-100"
          data-testid="media-set-source-rid"
        />
        <p class="mt-1 text-[11px] text-slate-400">
          Source picker activates once the data-connection app is reachable.
        </p>
      {/if}
    </div>

    <div class="flex justify-end gap-2 pt-2">
      <button
        type="button"
        class="rounded-xl border border-slate-700 px-4 py-2 text-sm text-slate-200 hover:bg-slate-800"
        onclick={() => {
          createOpen = false;
        }}
      >
        Cancel
      </button>
      <button
        type="submit"
        disabled={creating}
        class="rounded-xl bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-60"
        data-testid="media-set-create-submit"
      >
        {creating ? 'Creating…' : 'Create media set'}
      </button>
    </div>
  </form>
</Drawer>

<!-- ── Upload drawer ───────────────────────────────────────────── -->
<Drawer
  open={uploadingFor !== null}
  title={uploadingFor ? `Upload to ${uploadingFor.name}` : ''}
  width="560px"
  onclose={uploaderDrawerClosed}
>
  {#if uploadingFor}
    <MediaSetUploader mediaSet={uploadingFor} onUploaded={onItemUploaded} />
  {/if}
</Drawer>
