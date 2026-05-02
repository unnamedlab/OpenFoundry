<!--
  T5.1 — ResourceUsagePanel

  Surface storage / file count / row count statistics for the
  dataset. Numbers are derived from the parent's already-loaded
  Dataset row + the filesystem listing, so this component is pure
  presentation.
-->
<script lang="ts">
  type Props = {
    sizeBytes: number;
    fileCount: number;
    rowCount: number;
    /** Optional historical samples [{ ts, bytes }] for a sparkline. */
    history?: Array<{ ts: string; bytes: number }>;
  };

  const { sizeBytes, fileCount, rowCount, history = [] }: Props = $props();

  function fmtBytes(b: number): string {
    if (b < 1024) return `${b} B`;
    if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`;
    if (b < 1024 * 1024 * 1024) return `${(b / (1024 * 1024)).toFixed(1)} MB`;
    return `${(b / (1024 * 1024 * 1024)).toFixed(2)} GB`;
  }

  // Inline mini sparkline (SVG) so we don't pull in echarts for what's
  // essentially a 60-pixel-wide line.
  const points = $derived(() => {
    if (history.length === 0) return '';
    const max = Math.max(...history.map((h) => h.bytes), 1);
    const w = 200;
    const h = 40;
    return history
      .map((sample, i) => {
        const x = (i / Math.max(history.length - 1, 1)) * w;
        const y = h - (sample.bytes / max) * h;
        return `${x.toFixed(1)},${y.toFixed(1)}`;
      })
      .join(' ');
  });
</script>

<section class="space-y-4">
  <header>
    <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Resource usage metrics</div>
    <h2 class="mt-1 text-lg font-semibold">Storage footprint</h2>
  </header>

  <div class="grid grid-cols-1 gap-3 md:grid-cols-3">
    <div class="rounded-xl border border-slate-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-900">
      <div class="text-xs uppercase tracking-wide text-gray-400">Storage</div>
      <div class="mt-1 text-2xl font-semibold">{fmtBytes(sizeBytes)}</div>
    </div>
    <div class="rounded-xl border border-slate-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-900">
      <div class="text-xs uppercase tracking-wide text-gray-400">Files</div>
      <div class="mt-1 text-2xl font-semibold">{fileCount.toLocaleString()}</div>
    </div>
    <div class="rounded-xl border border-slate-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-900">
      <div class="text-xs uppercase tracking-wide text-gray-400">Rows (estimated)</div>
      <div class="mt-1 text-2xl font-semibold">{rowCount.toLocaleString()}</div>
    </div>
  </div>

  {#if history.length > 1}
    <div class="rounded-xl border border-slate-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-900">
      <div class="text-xs uppercase tracking-wide text-gray-400">Storage trend</div>
      <svg viewBox="0 0 200 40" class="mt-2 h-10 w-full" aria-hidden="true">
        <polyline
          fill="none"
          stroke="currentColor"
          stroke-width="1.5"
          class="text-blue-500"
          points={points()}
        />
      </svg>
    </div>
  {/if}
</section>
