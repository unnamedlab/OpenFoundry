<!--
  P3 — Storage details tab (manage role only).

  Surfaces the running BackingFsConfig (driver + base_directory +
  bucket / region / fs_id) plus the live GB consumed by the dataset's
  active and soft-deleted files. The data comes from the manage-only
  `GET /v1/datasets/{rid}/storage-details` endpoint, which returns a
  403 to non-manage callers — the parent gates the tab visibility but
  we re-render the 403 here so the failure is observable when a role
  flips mid-session.
-->
<script lang="ts">
  import {
    getDatasetStorageDetails,
    type DatasetStorageDetails,
  } from '$lib/api/datasets';

  type Props = {
    datasetRid: string;
  };

  const { datasetRid }: Props = $props();

  let details = $state<DatasetStorageDetails | null>(null);
  let loading = $state(false);
  let errorMessage = $state<string | null>(null);
  let lastKey = $state('');

  $effect(() => {
    if (!datasetRid || datasetRid === lastKey) return;
    lastKey = datasetRid;
    void load();
  });

  async function load() {
    loading = true;
    errorMessage = null;
    try {
      details = await getDatasetStorageDetails(datasetRid);
    } catch (cause) {
      errorMessage = cause instanceof Error ? cause.message : 'Storage details failed.';
      details = null;
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

  function bucketFromFsId(fsId: string): string {
    if (fsId.startsWith('s3:')) return fsId.slice('s3:'.length);
    if (fsId.startsWith('hdfs:')) return fsId.slice('hdfs:'.length);
    return '—';
  }
</script>

<section class="space-y-3" data-component="storage-details-tab">
  <header class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950">
    <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Storage</div>
    <h2 class="mt-1 text-lg font-semibold">Backing filesystem details</h2>
    <p class="mt-1 text-sm text-slate-500">
      Foundry-style storage configuration for this dataset's underlying file system.
      Visible only to users with dataset-manage permission.
    </p>
  </header>

  {#if loading}
    <div class="rounded-lg border border-slate-200 bg-white px-4 py-3 text-sm text-slate-600 shadow-sm dark:border-gray-800 dark:bg-gray-950 dark:text-gray-300" role="status">
      Loading storage details...
    </div>
  {:else if errorMessage}
    <div class="rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-800 dark:border-rose-900/50 dark:bg-rose-950/40 dark:text-rose-100" role="alert" data-testid="storage-details-error">
      {errorMessage}
    </div>
  {:else if details}
    <div class="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-4" data-testid="storage-details-cards">
      {@render Card('Driver', details.driver.toUpperCase())}
      {@render Card('FS id', details.fs_id, true)}
      {@render Card('Base directory', details.base_directory, true)}
      {@render Card('Bucket / namenode', bucketFromFsId(details.fs_id), true)}
    </div>

    <div class="grid grid-cols-1 gap-3 md:grid-cols-3">
      {@render Card('Active storage', fmtBytes(details.total_active_bytes))}
      {@render Card('Active files', details.total_active_files.toLocaleString())}
      {@render Card('Soft-deleted storage', fmtBytes(details.total_deleted_bytes))}
    </div>

    <div class="rounded-lg border border-slate-200 bg-white p-4 text-xs text-slate-500 shadow-sm dark:border-gray-800 dark:bg-gray-950 dark:text-gray-400">
      Presigned download URLs expire after
      <span class="font-mono">{details.presign_ttl_seconds}s</span>. Adjust via
      <span class="font-mono">OF_BACKING_FS_PRESIGN_TTL_SECONDS</span>.
    </div>
  {/if}
</section>

{#snippet Card(label: string, value: string, mono = false)}
  <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950">
    <div class="text-xs uppercase tracking-[0.18em] text-slate-400">{label}</div>
    <div class={`mt-2 ${mono ? 'font-mono break-all text-sm' : 'text-2xl font-semibold'}`}>{value}</div>
  </div>
{/snippet}
