<!--
  T5.2 — VirtualizedPreviewTable

  Hand-rolled windowed list (no external virt library — keeps the
  bundle lean). We render `rows` for the visible window plus a small
  overscan, padding the top/bottom with spacer divs whose heights add
  up to the missing rows. Scales to millions of rows because the DOM
  only ever has ~50 nodes regardless of dataset size.

  Companion features bolted on top of the table:

    * A column-stat strip above each header column showing min / max /
      null % / distinct (fed via the `stats` prop, indexed by name).
    * A horizontal "view picker" timeline on top of the table so users
      can scrub between historical transactions.
    * The component is *controlled*: parent owns `rows`, `columns`,
      `stats`, `transactions`, and the `onSelectTransaction` callback.
-->
<script lang="ts">
  import type { DatasetTransaction } from '$lib/api/datasets';

  export type ColumnDef = { name: string; field_type?: string };
  export type ColumnStats = {
    min?: string | number | null;
    max?: string | number | null;
    null_rate?: number;
    distinct_count?: number;
  };

  type Props = {
    columns: ColumnDef[];
    rows: Array<Record<string, unknown>>;
    stats?: Record<string, ColumnStats>;
    transactions: DatasetTransaction[];
    selectedTransactionId?: string | null;
    onSelectTransaction?: (txId: string | null) => void;
    /** Pixel height of one row. */
    rowHeight?: number;
    /** Total visible viewport height. */
    viewportHeight?: number;
  };

  const {
    columns,
    rows,
    stats = {},
    transactions,
    selectedTransactionId = null,
    onSelectTransaction = () => {},
    rowHeight = 32,
    viewportHeight = 480,
  }: Props = $props();

  let scrollTop = $state(0);
  let viewport: HTMLDivElement | undefined;

  const overscan = 6;
  const visibleCount = $derived(Math.ceil(viewportHeight / rowHeight) + overscan * 2);
  const startIndex = $derived(
    Math.max(0, Math.floor(scrollTop / rowHeight) - overscan),
  );
  const endIndex = $derived(Math.min(rows.length, startIndex + visibleCount));
  const visibleRows = $derived(rows.slice(startIndex, endIndex));
  const topPad = $derived(startIndex * rowHeight);
  const bottomPad = $derived((rows.length - endIndex) * rowHeight);

  function onScroll(event: Event) {
    scrollTop = (event.target as HTMLDivElement).scrollTop;
  }

  function fmtCell(value: unknown): string {
    if (value === null || value === undefined) return '';
    if (typeof value === 'object') return JSON.stringify(value);
    return String(value);
  }

  function statSummary(name: string): string {
    const s = stats[name];
    if (!s) return '';
    const parts: string[] = [];
    if (s.min !== undefined && s.min !== null) parts.push(`min ${s.min}`);
    if (s.max !== undefined && s.max !== null) parts.push(`max ${s.max}`);
    if (s.null_rate !== undefined) parts.push(`null ${(s.null_rate * 100).toFixed(1)}%`);
    if (s.distinct_count !== undefined) parts.push(`distinct ${s.distinct_count}`);
    return parts.join(' · ');
  }
</script>

<section class="space-y-3">
  {#if transactions.length > 0}
    <!--
      Horizontal transaction timeline. Foundry calls this the "view
      selector". Scroll horizontally with arrow keys / drag.
    -->
    <div class="overflow-x-auto rounded-xl border border-slate-200 bg-white p-2 dark:border-gray-700 dark:bg-gray-900">
      <div class="flex min-w-max items-center gap-1">
        <button
          type="button"
          class={`rounded-md px-2 py-1 text-xs ${selectedTransactionId === null ? 'bg-blue-600 text-white' : 'text-gray-600 hover:bg-slate-100 dark:text-gray-300 dark:hover:bg-gray-800'}`}
          onclick={() => onSelectTransaction(null)}
        >
          HEAD
        </button>
        {#each transactions as tx (tx.id)}
          <button
            type="button"
            class={`flex flex-col items-start rounded-md px-2 py-1 text-left text-[10px] ${selectedTransactionId === tx.id ? 'bg-blue-600 text-white' : 'text-gray-600 hover:bg-slate-100 dark:text-gray-300 dark:hover:bg-gray-800'}`}
            onclick={() => onSelectTransaction(tx.id)}
            title={`${tx.operation} · ${tx.id}`}
          >
            <span class="font-medium uppercase">{tx.operation}</span>
            <span class="font-mono">{new Date(tx.created_at).toLocaleDateString()}</span>
          </button>
        {/each}
      </div>
    </div>
  {/if}

  <div class="overflow-hidden rounded-xl border border-slate-200 bg-white dark:border-gray-700 dark:bg-gray-900">
    <!--
      Header row + per-column stat strip. Sticky so the user keeps
      seeing the column names while scrolling rows.
    -->
    <div class="sticky top-0 z-10 border-b border-slate-200 bg-slate-50 dark:border-gray-700 dark:bg-gray-800/80">
      <div class="grid" style:grid-template-columns={`repeat(${columns.length}, minmax(140px, 1fr))`}>
        {#each columns as col (col.name)}
          <div class="border-r border-slate-200 px-3 py-2 last:border-r-0 dark:border-gray-700">
            <div class="flex items-center justify-between gap-2">
              <span class="font-mono text-xs">{col.name}</span>
              {#if col.field_type}
                <span class="rounded-full bg-slate-200 px-1.5 py-0.5 text-[9px] text-slate-700 dark:bg-gray-700 dark:text-gray-200">
                  {col.field_type}
                </span>
              {/if}
            </div>
            {#if statSummary(col.name)}
              <div class="mt-1 truncate text-[10px] text-gray-500" title={statSummary(col.name)}>
                {statSummary(col.name)}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    </div>

    <div
      bind:this={viewport}
      onscroll={onScroll}
      class="overflow-auto font-mono text-xs"
      style:height={`${viewportHeight}px`}
    >
      <div style:height={`${topPad}px`}></div>
      {#each visibleRows as row, i (startIndex + i)}
        <div
          class="grid border-b border-slate-100 dark:border-gray-800"
          style:height={`${rowHeight}px`}
          style:grid-template-columns={`repeat(${columns.length}, minmax(140px, 1fr))`}
        >
          {#each columns as col (col.name)}
            <div class="truncate border-r border-slate-100 px-3 py-1 last:border-r-0 dark:border-gray-800">
              {fmtCell(row[col.name])}
            </div>
          {/each}
        </div>
      {/each}
      <div style:height={`${bottomPad}px`}></div>
    </div>

    <div class="border-t border-slate-200 px-3 py-1 text-[11px] text-gray-500 dark:border-gray-700">
      Showing rows {startIndex + 1}–{endIndex} of {rows.length.toLocaleString()}
    </div>
  </div>
</section>
