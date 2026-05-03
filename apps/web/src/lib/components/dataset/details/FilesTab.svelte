<!--
  P3 — Files tab.

  Foundry-style listing backed by `dataset-versioning-service`'s
  `GET /v1/datasets/{rid}/files`. Surfaces the logical path, the
  size, sha256 (when present), the transaction that introduced it,
  the modified_at timestamp, and a badge for active vs deleted-in-
  current-view files. The download button hits the 302 endpoint so
  the browser follows the presigned URL.

  Virtualization: simple sliding window over the row array. The
  preview tab uses a richer virt component, but Files needs fewer
  columns (logical_path, size, sha256, txn, modified, status, action)
  and the dataset typically holds < 10k entries, so a plain windowed
  list is enough.
-->
<script lang="ts">
  import {
    datasetFileDownloadUrl,
    listDatasetFiles,
    type DatasetBackingFile,
    type DatasetFilesResponse,
  } from '$lib/api/datasets';

  /** P4 — retention purge marker keyed by file id. */
  export type RetentionPurgeMarker = {
    policyName: string;
    daysUntilPurge: number;
  };

  type Props = {
    datasetRid: string;
    branch?: string;
    viewId?: string;
    /** Initial prefix applied to the filter input. */
    initialPrefix?: string;
    /** Retention purge markers, keyed by dataset_files.id. */
    retentionPurges?: Record<string, RetentionPurgeMarker>;
    /** Click handler for the purge badge — typically routes the page
     *  to the Retention tab. */
    onOpenRetention?: () => void;
  };

  const {
    datasetRid,
    branch = 'master',
    viewId,
    initialPrefix = '',
    retentionPurges = {},
    onOpenRetention,
  }: Props = $props();

  let response = $state<DatasetFilesResponse | null>(null);
  let loading = $state(false);
  let errorMessage = $state<string | null>(null);
  // Seeded once from `initialPrefix`; subsequent edits live entirely in
  // the local `$state`. The `$effect` below re-fetches whenever the
  // prefix or other inputs change.
  let prefix = $state('');
  $effect(() => {
    if (initialPrefix && !prefix) prefix = initialPrefix;
  });
  let lastKey = $state('');

  $effect(() => {
    const key = JSON.stringify({ datasetRid, branch, viewId, prefix });
    if (!datasetRid) return;
    if (key === lastKey) return;
    lastKey = key;
    void load();
  });

  async function load() {
    loading = true;
    errorMessage = null;
    try {
      response = await listDatasetFiles(datasetRid, {
        branch,
        view_id: viewId,
        prefix: prefix || undefined,
      });
    } catch (cause) {
      errorMessage = cause instanceof Error ? cause.message : 'Files listing failed.';
      response = null;
    } finally {
      loading = false;
    }
  }

  function fmtBytes(value: number): string {
    if (value < 1024) return `${value} B`;
    if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
    if (value < 1024 * 1024 * 1024) return `${(value / (1024 * 1024)).toFixed(1)} MB`;
    return `${(value / (1024 * 1024 * 1024)).toFixed(2)} GB`;
  }

  function fmtDate(value: string): string {
    return new Date(value).toLocaleString();
  }

  function shortSha(value?: string | null): string {
    if (!value) return '—';
    return value.length > 12 ? value.slice(0, 12) + '…' : value;
  }

  const files: DatasetBackingFile[] = $derived(response?.files ?? []);
</script>

<section class="space-y-3" data-component="files-tab">
  <header class="flex flex-col gap-2 rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950 md:flex-row md:items-center md:justify-between">
    <div>
      <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Files</div>
      <h2 class="mt-1 text-lg font-semibold">Backing filesystem</h2>
      <p class="mt-1 text-sm text-slate-500">
        {files.length.toLocaleString()} file{files.length === 1 ? '' : 's'} on branch
        <span class="font-mono">{response?.branch ?? branch}</span>
      </p>
    </div>
    <div class="flex flex-wrap items-center gap-2">
      <input
        type="search"
        placeholder="Filter by prefix"
        class="w-56 rounded-md border border-slate-300 bg-white px-2 py-1 text-sm dark:border-gray-700 dark:bg-gray-900"
        bind:value={prefix}
        data-testid="files-prefix-filter"
      />
      <button
        type="button"
        class="rounded-md border border-slate-300 bg-white px-3 py-1.5 text-sm hover:bg-slate-50 dark:border-gray-700 dark:bg-gray-900 dark:hover:bg-gray-800"
        onclick={() => void load()}
      >
        Refresh
      </button>
    </div>
  </header>

  {#if loading}
    <div class="rounded-lg border border-slate-200 bg-white px-4 py-3 text-sm text-slate-600 shadow-sm dark:border-gray-800 dark:bg-gray-950 dark:text-gray-300" role="status">
      Loading files...
    </div>
  {:else if errorMessage}
    <div class="rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-800 dark:border-rose-900/50 dark:bg-rose-950/40 dark:text-rose-100" role="alert" data-testid="files-error">
      {errorMessage}
    </div>
  {:else if files.length === 0}
    <div class="rounded-lg border border-slate-200 bg-white px-4 py-6 text-center text-sm text-slate-500 shadow-sm dark:border-gray-800 dark:bg-gray-950 dark:text-gray-400">
      No files visible on this branch / view.
    </div>
  {:else}
    <div class="overflow-x-auto rounded-lg border border-slate-200 bg-white shadow-sm dark:border-gray-800 dark:bg-gray-950">
      <table class="min-w-full divide-y divide-slate-200 text-xs dark:divide-gray-800">
        <thead class="text-left uppercase tracking-wide text-slate-500">
          <tr>
            <th class="px-3 py-2">Logical path</th>
            <th class="px-3 py-2">Size</th>
            <th class="px-3 py-2">SHA-256</th>
            <th class="px-3 py-2">Transaction</th>
            <th class="px-3 py-2">Modified</th>
            <th class="px-3 py-2">Status</th>
            <th class="px-3 py-2"></th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100 font-mono dark:divide-gray-800" data-testid="files-rows">
          {#each files as file (file.id)}
            <tr data-testid="file-row" data-file-status={file.status} data-file-name={file.logical_path}>
              <td class="px-3 py-2 break-all">{file.logical_path}</td>
              <td class="px-3 py-2 whitespace-nowrap">{fmtBytes(file.size_bytes)}</td>
              <td class="px-3 py-2" title={file.sha256 ?? ''}>{shortSha(file.sha256)}</td>
              <td class="px-3 py-2" title={file.transaction_id}>{file.transaction_id.slice(0, 8)}</td>
              <td class="px-3 py-2 whitespace-nowrap">{fmtDate(file.modified_at)}</td>
              <td class="px-3 py-2">
                <span class={`rounded-full px-2 py-0.5 ${
                  file.status === 'active'
                    ? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-200'
                    : 'bg-rose-100 text-rose-800 dark:bg-rose-900/40 dark:text-rose-200'
                }`}>
                  {file.status === 'active' ? 'active' : 'deleted in current view'}
                </span>
                {#if retentionPurges[file.id]}
                  {@const marker = retentionPurges[file.id]}
                  <button
                    type="button"
                    class="ml-1 inline-flex items-center gap-1 rounded-full bg-amber-100 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-amber-800 hover:bg-amber-200 dark:bg-amber-900/40 dark:text-amber-200 dark:hover:bg-amber-900/60"
                    onclick={() => onOpenRetention?.()}
                    title={`Open the Retention tab — purge driven by ${marker.policyName}`}
                    data-testid="files-retention-badge"
                  >
                    Will be purged
                    {#if marker.daysUntilPurge <= 0}now{:else}in {marker.daysUntilPurge}d{/if}
                    by {marker.policyName}
                  </button>
                {/if}
              </td>
              <td class="px-3 py-2">
                {#if file.status === 'active'}
                  <a
                    class="text-blue-600 hover:underline dark:text-blue-300"
                    href={datasetFileDownloadUrl(datasetRid, file.id)}
                    data-testid="file-download"
                    target="_blank"
                    rel="noopener"
                  >
                    Download
                  </a>
                {:else}
                  <span class="text-slate-400">—</span>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>
