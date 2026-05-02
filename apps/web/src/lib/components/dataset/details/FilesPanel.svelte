<!--
  T5.1 — FilesPanel

  Renders a Foundry-style "Files" tab listing the physical artifacts
  backing the current view: logical path, size, format
  (Parquet/Avro/Text/…) and the transaction id that produced them.

  We're a *controlled* component — the parent fetches via
  `listDatasetFilesystem` and passes the entries down. We only render.
-->
<script lang="ts">
  import type { DatasetFilesystemEntry } from '$lib/api/datasets';

  type Props = {
    entries: DatasetFilesystemEntry[];
    currentVersion: number;
    activeBranch: string;
    loading?: boolean;
    error?: string;
  };

  const { entries, currentVersion, activeBranch, loading = false, error = '' }: Props = $props();

  function formatBytes(bytes?: number): string {
    if (bytes === undefined || bytes === null) return '—';
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
  }

  function formatGuess(entry: DatasetFilesystemEntry): string {
    const ext = entry.name.split('.').pop()?.toLowerCase() ?? '';
    if (ext === 'parquet') return 'Parquet';
    if (ext === 'avro') return 'Avro';
    if (ext === 'csv' || ext === 'tsv' || ext === 'txt') return 'Text';
    if (ext === 'json' || ext === 'ndjson') return 'JSON';
    if (entry.content_type) return entry.content_type;
    return ext ? ext.toUpperCase() : '—';
  }

  function transactionOf(entry: DatasetFilesystemEntry): string {
    const tx = entry.metadata?.['transaction_id'];
    return typeof tx === 'string' ? tx : '—';
  }
</script>

<section class="space-y-4">
  <header class="flex items-center justify-between">
    <div>
      <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Files</div>
      <h2 class="mt-1 text-lg font-semibold">Backing artifacts</h2>
      <p class="mt-1 text-sm text-gray-500">
        Branch <span class="font-mono">{activeBranch}</span>, version
        <span class="font-mono">v{currentVersion}</span>.
      </p>
    </div>
  </header>

  {#if error}
    <div class="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300">
      {error}
    </div>
  {/if}

  {#if loading}
    <div class="rounded-xl border border-slate-200 bg-white px-4 py-6 text-center text-sm text-gray-500 dark:border-gray-700 dark:bg-gray-900">
      Loading files…
    </div>
  {:else if entries.length === 0}
    <div class="rounded-xl border border-dashed border-slate-300 bg-white px-4 py-8 text-center text-sm text-gray-500 dark:border-gray-700 dark:bg-gray-900">
      No files in this view yet. Upload data or run a pipeline that writes here.
    </div>
  {:else}
    <div class="overflow-hidden rounded-xl border border-slate-200 bg-white dark:border-gray-700 dark:bg-gray-900">
      <table class="min-w-full divide-y divide-slate-200 text-sm dark:divide-gray-800">
        <thead class="bg-slate-50 text-left text-xs uppercase tracking-wide text-gray-500 dark:bg-gray-800/60">
          <tr>
            <th class="px-3 py-2">Path</th>
            <th class="px-3 py-2">Format</th>
            <th class="px-3 py-2 text-right">Size</th>
            <th class="px-3 py-2">Last modified</th>
            <th class="px-3 py-2">Introduced by</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100 dark:divide-gray-800">
          {#each entries as entry (entry.path)}
            <tr>
              <td class="px-3 py-2 font-mono text-xs">
                {#if entry.entry_type === 'directory'}📁 {/if}
                {entry.path}
              </td>
              <td class="px-3 py-2">{formatGuess(entry)}</td>
              <td class="px-3 py-2 text-right">{formatBytes(entry.size_bytes)}</td>
              <td class="px-3 py-2 text-gray-500">
                {entry.last_modified ? new Date(entry.last_modified).toLocaleString() : '—'}
              </td>
              <td class="px-3 py-2 font-mono text-[11px] text-gray-500">
                {transactionOf(entry).slice(0, 12)}{transactionOf(entry).length > 12 ? '…' : ''}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>
