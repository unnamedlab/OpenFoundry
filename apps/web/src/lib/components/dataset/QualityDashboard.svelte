<!--
  P6 — Dataset Quality dashboard.

  Foundry doc § "Data Health" + § "Health checks" — six cards:

    1. Freshness        — colour-coded against the SLA
    2. Last build       — icon + timestamp + "view logs"
    3. Schema drift     — yes/no + diff link
    4. Row / Col count  — counts + sparkline (extras.row_count_history)
    5. Txn failures 24h — bar chart per tx_type
    6. Health-check policies — inline list + "Add health check" CTA

  Self-fetches `getDatasetHealth(rid)` and surfaces graceful failures:
  cards stay visible with em-dash placeholders so the UI never blanks
  out when the quality service is offline.
-->
<script lang="ts">
  import { getDatasetHealth, type DatasetHealthResponse } from '$lib/api/datasets';

  type Props = {
    datasetRid: string;
    /** Freshness SLA in seconds. The freshness card colours green
     *  below this, amber up to 2× the SLA, red beyond. */
    freshnessSlaSeconds?: number;
    onAddHealthCheck?: () => void;
    onViewLogs?: () => void;
    onOpenSchemaDiff?: () => void;
  };

  const {
    datasetRid,
    freshnessSlaSeconds = 24 * 3600,
    onAddHealthCheck,
    onViewLogs,
    onOpenSchemaDiff,
  }: Props = $props();

  let health = $state<DatasetHealthResponse | null>(null);
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
      health = await getDatasetHealth(datasetRid);
    } catch (cause) {
      errorMessage = cause instanceof Error ? cause.message : 'Quality unavailable.';
      health = null;
    } finally {
      loading = false;
    }
  }

  function fmtBytes(value: number): string {
    if (value < 1024) return `${value}`;
    if (value < 1_000_000) return `${(value / 1_000).toFixed(1)}K`;
    if (value < 1_000_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
    return `${(value / 1_000_000_000).toFixed(2)}B`;
  }

  function fmtSeconds(seconds: number): string {
    if (seconds < 60) return `${seconds}s`;
    if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
    if (seconds < 86_400) return `${(seconds / 3600).toFixed(1)}h`;
    return `${(seconds / 86_400).toFixed(1)}d`;
  }

  function fmtPct(value: number): string {
    return `${(value * 100).toFixed(1)}%`;
  }

  function freshnessColor(seconds: number): string {
    if (seconds <= freshnessSlaSeconds)
      return 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-200';
    if (seconds <= 2 * freshnessSlaSeconds)
      return 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-200';
    return 'bg-rose-100 text-rose-800 dark:bg-rose-900/40 dark:text-rose-200';
  }

  function buildStatusIcon(status: string): string {
    switch (status) {
      case 'success':
        return '✓';
      case 'failed':
        return '✕';
      case 'stale':
        return '⏳';
      default:
        return '•';
    }
  }

  // Failure breakdown (per tx_type) lives in `extras.failure_breakdown_24h`.
  const failureBreakdown = $derived.by<Array<{ type: string; count: number }>>(() => {
    const raw = (health?.extras as { failure_breakdown_24h?: Record<string, number> } | undefined)
      ?.failure_breakdown_24h;
    if (!raw) return [];
    return Object.entries(raw)
      .map(([type, count]) => ({ type, count: Number(count) }))
      .sort((a, b) => b.count - a.count);
  });
  const failureMax = $derived(
    failureBreakdown.reduce((m, x) => Math.max(m, x.count), 0),
  );

  // Sparkline points (optional). When the runner emits them under
  // `extras.row_count_history`, render a tiny inline-SVG sparkline so
  // the component stays bundle-free (no ECharts dep yet).
  const sparklinePoints = $derived.by<number[]>(() => {
    const raw = (health?.extras as { row_count_history?: Array<{ value: number }> } | undefined)
      ?.row_count_history;
    if (!raw || raw.length === 0) return [];
    return raw.map((p) => Number(p.value));
  });

  function sparklinePath(points: number[]): string {
    if (points.length < 2) return '';
    const w = 120;
    const h = 32;
    const min = Math.min(...points);
    const max = Math.max(...points);
    const range = max - min || 1;
    const step = w / (points.length - 1);
    return points
      .map((value, idx) => {
        const x = idx * step;
        const y = h - ((value - min) / range) * h;
        return `${idx === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`;
      })
      .join(' ');
  }
</script>

<section class="space-y-3" data-component="quality-dashboard">
  {#if loading}
    <div class="rounded-lg border border-slate-200 bg-white px-4 py-3 text-sm text-slate-600 shadow-sm dark:border-gray-800 dark:bg-gray-950 dark:text-gray-300" role="status">
      Loading quality snapshot…
    </div>
  {:else if errorMessage}
    <div class="rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-800 dark:border-rose-900/50 dark:bg-rose-950/40 dark:text-rose-100" role="alert" data-testid="quality-error">
      {errorMessage}
    </div>
  {:else if health}
    <div class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
      <!-- 1) Freshness -->
      <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950" data-testid="quality-card-freshness">
        <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Freshness</div>
        <div class="mt-2 flex items-center gap-2">
          <span class={`rounded-full px-2 py-0.5 text-sm font-semibold ${freshnessColor(health.freshness_seconds)}`}>
            {fmtSeconds(health.freshness_seconds)}
          </span>
          <span class="text-xs text-slate-500">SLA {fmtSeconds(freshnessSlaSeconds)}</span>
        </div>
        <div class="mt-1 text-xs text-slate-500">
          Last commit
          {health.last_commit_at ? new Date(health.last_commit_at).toLocaleString() : '—'}
        </div>
      </div>

      <!-- 2) Last build -->
      <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950" data-testid="quality-card-last-build">
        <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Last build</div>
        <div class="mt-2 flex items-center gap-2">
          <span class="text-2xl">{buildStatusIcon(health.last_build_status)}</span>
          <span class="text-base font-medium uppercase">{health.last_build_status}</span>
        </div>
        <div class="mt-1 flex items-center justify-between text-xs text-slate-500">
          <span>{health.last_commit_at ? new Date(health.last_commit_at).toLocaleString() : '—'}</span>
          {#if onViewLogs}
            <button type="button" class="text-blue-600 hover:underline dark:text-blue-300" onclick={onViewLogs}>
              view logs
            </button>
          {/if}
        </div>
      </div>

      <!-- 3) Schema drift -->
      <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950" data-testid="quality-card-schema-drift">
        <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Schema drift</div>
        <div class="mt-2 flex items-center gap-2">
          {#if health.schema_drift_flag}
            <span class="rounded-full bg-amber-100 px-2 py-0.5 text-sm font-semibold text-amber-800 dark:bg-amber-900/40 dark:text-amber-200">yes</span>
          {:else}
            <span class="rounded-full bg-emerald-100 px-2 py-0.5 text-sm font-semibold text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-200">no</span>
          {/if}
          {#if onOpenSchemaDiff}
            <button type="button" class="ml-2 text-xs text-blue-600 hover:underline dark:text-blue-300" onclick={onOpenSchemaDiff}>
              view diff
            </button>
          {/if}
        </div>
        <div class="mt-1 text-xs text-slate-500">{health.col_count} column{health.col_count === 1 ? '' : 's'}</div>
      </div>

      <!-- 4) Row / Col counts + sparkline -->
      <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950" data-testid="quality-card-counts">
        <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Row / Col count</div>
        <div class="mt-2 flex items-end gap-3">
          <div class="text-3xl font-semibold">{fmtBytes(health.row_count)}</div>
          <div class="text-sm text-slate-500">rows</div>
          <div class="ml-auto text-sm text-slate-500">{health.col_count} cols</div>
        </div>
        {#if sparklinePoints.length >= 2}
          <svg viewBox="0 0 120 32" class="mt-2 h-8 w-full text-blue-500">
            <path d={sparklinePath(sparklinePoints)} fill="none" stroke="currentColor" stroke-width="1.5" />
          </svg>
        {:else}
          <div class="mt-2 text-xs text-slate-400">No sparkline data yet — refresh after the next commit.</div>
        {/if}
      </div>

      <!-- 5) Txn failures 24h -->
      <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950" data-testid="quality-card-failures">
        <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Txn failures 24h</div>
        <div class="mt-2 text-2xl font-semibold">{fmtPct(health.txn_failure_rate_24h)}</div>
        {#if failureBreakdown.length > 0}
          <ul class="mt-2 space-y-1">
            {#each failureBreakdown as item (item.type)}
              <li class="flex items-center gap-2 text-xs">
                <span class="w-16 font-mono uppercase text-slate-500">{item.type}</span>
                <span class="h-2 flex-1 rounded bg-slate-100 dark:bg-gray-800">
                  <span class="block h-full rounded bg-rose-500" style:width={`${(item.count / Math.max(failureMax, 1)) * 100}%`}></span>
                </span>
                <span class="w-8 text-right">{item.count}</span>
              </li>
            {/each}
          </ul>
        {:else}
          <div class="mt-1 text-xs text-slate-500">No aborted transactions in the last 24h.</div>
        {/if}
      </div>

      <!-- 6) Health-check policies -->
      <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950" data-testid="quality-card-policies">
        <div class="flex items-center justify-between">
          <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Health-check policies</div>
          {#if onAddHealthCheck}
            <button type="button" class="text-xs text-blue-600 hover:underline dark:text-blue-300" onclick={onAddHealthCheck} data-testid="add-health-check">
              + Add health check
            </button>
          {/if}
        </div>
        <p class="mt-2 text-sm text-slate-500">
          Foundry-style policies that flag freshness, txn-failure rate, schema drift,
          and row-drop SLAs. Trigger an audit event when a policy fires.
        </p>
      </div>
    </div>
  {/if}
</section>
