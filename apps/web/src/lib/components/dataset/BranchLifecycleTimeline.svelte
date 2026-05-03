<!--
  P5 — BranchLifecycleTimeline

  Horizontal per-branch timeline. One row per active branch; events
  along the row are colored by their lifecycle step:

    * 🔵 blue   — JobSpec published (when surfaceable; today inferred
                  from `transactions[].metadata.published_jobspec_at`).
    * 🟢 green  — transaction COMMITTED.
    * 🔴 red    — transaction ABORTED.
    * 🟠 amber  — transaction OPEN (current).
    * ⚪ grey   — branch ARCHIVED.

  Hover a point → tooltip with the full payload. Doubleclick → deep
  link into HistoryTimeline.

  Pure-CSS render so it stays cheap with 100+ branches × dozens of
  events; the parent passes the already-loaded transaction list so we
  don't fan out additional API calls.
-->
<script lang="ts">
  import type {
    DatasetBranch,
    DatasetTransaction,
  } from '$lib/api/datasets';

  type Props = {
    branches: DatasetBranch[];
    transactions: DatasetTransaction[];
  };

  const { branches, transactions }: Props = $props();

  type Tone = 'committed' | 'aborted' | 'open' | 'jobspec' | 'archived';

  type Marker = {
    branch: string;
    tone: Tone;
    at: string;
    label: string;
    payload: unknown;
  };

  function tonePalette(t: Tone): { bg: string; ring: string; label: string } {
    switch (t) {
      case 'committed':
        return { bg: 'bg-emerald-500', ring: 'ring-emerald-200', label: 'COMMITTED' };
      case 'aborted':
        return { bg: 'bg-rose-500', ring: 'ring-rose-200', label: 'ABORTED' };
      case 'open':
        return { bg: 'bg-amber-500', ring: 'ring-amber-200', label: 'OPEN' };
      case 'jobspec':
        return { bg: 'bg-blue-500', ring: 'ring-blue-200', label: 'JOBSPEC' };
      case 'archived':
        return { bg: 'bg-slate-500', ring: 'ring-slate-200', label: 'ARCHIVED' };
    }
  }

  function txTone(status: string): Tone {
    const s = status.toUpperCase();
    if (s === 'COMMITTED') return 'committed';
    if (s === 'ABORTED') return 'aborted';
    if (s === 'OPEN') return 'open';
    return 'committed';
  }

  const allMarkers = $derived.by<Marker[]>(() => {
    const out: Marker[] = [];
    for (const tx of transactions) {
      if (!tx.branch_name) continue;
      const at = (tx.committed_at ?? tx.created_at) || '';
      out.push({
        branch: tx.branch_name,
        tone: txTone(tx.status),
        at,
        label: `${tx.operation || 'TX'} · ${tx.status}`,
        payload: tx,
      });
      // Approximate JobSpec markers from a `metadata.published_jobspec_at`
      // field if present — services that don't publish it skip the dot.
      const meta = (tx.metadata ?? {}) as Record<string, unknown>;
      const jobspecAt = meta['published_jobspec_at'];
      if (typeof jobspecAt === 'string') {
        out.push({
          branch: tx.branch_name,
          tone: 'jobspec',
          at: jobspecAt,
          label: 'JobSpec published',
          payload: tx,
        });
      }
    }
    for (const b of branches) {
      if (b.archived_at) {
        out.push({
          branch: b.name,
          tone: 'archived',
          at: b.archived_at,
          label: 'Branch archived',
          payload: b,
        });
      }
    }
    return out;
  });

  const range = $derived.by<{ start: number; end: number }>(() => {
    const times = allMarkers
      .map((m) => Date.parse(m.at))
      .filter((n) => Number.isFinite(n));
    if (times.length === 0) {
      const now = Date.now();
      return { start: now - 24 * 3600 * 1000, end: now };
    }
    return { start: Math.min(...times), end: Math.max(...times) };
  });

  function pct(at: string): number {
    const ts = Date.parse(at);
    if (!Number.isFinite(ts)) return 0;
    const span = range.end - range.start;
    if (span <= 0) return 50;
    return Math.max(0, Math.min(100, ((ts - range.start) / span) * 100));
  }

  let hovered = $state<Marker | null>(null);

  const markersByBranch = $derived.by<Record<string, Marker[]>>(() => {
    const out: Record<string, Marker[]> = {};
    for (const m of allMarkers) {
      if (!out[m.branch]) out[m.branch] = [];
      out[m.branch].push(m);
    }
    for (const arr of Object.values(out)) {
      arr.sort((a, b) => Date.parse(a.at) - Date.parse(b.at));
    }
    return out;
  });
</script>

<section
  class="space-y-2 rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900"
  data-testid="branch-lifecycle-timeline"
>
  <header class="flex items-center justify-between">
    <h2 class="text-sm font-semibold">Branch lifecycle</h2>
    <ul class="flex flex-wrap gap-2 text-[10px] text-gray-500">
      {#each (['jobspec', 'committed', 'aborted', 'open', 'archived'] as Tone[]) as t}
        {@const palette = tonePalette(t)}
        <li class="flex items-center gap-1">
          <span class={`inline-block h-2 w-2 rounded-full ${palette.bg}`}></span>
          <span>{palette.label}</span>
        </li>
      {/each}
    </ul>
  </header>

  <ul class="space-y-1">
    {#each branches as branch (branch.id)}
      {@const markers = markersByBranch[branch.name] ?? []}
      <li class="grid grid-cols-[140px_1fr] items-center gap-2 text-xs">
        <span class="font-mono text-[11px]" data-testid={`branch-lifecycle-${branch.name}`}>
          {branch.name}{branch.is_default ? ' ★' : ''}
        </span>
        <div class="relative h-4 rounded bg-slate-100 dark:bg-gray-800">
          {#each markers as m, i (`${m.branch}-${m.tone}-${m.at}-${i}`)}
            {@const palette = tonePalette(m.tone)}
            <button
              type="button"
              class={`absolute top-1/2 h-3 w-3 -translate-x-1/2 -translate-y-1/2 rounded-full ring-2 ${palette.bg} ${palette.ring}`}
              style={`left: ${pct(m.at)}%`}
              onmouseenter={() => (hovered = m)}
              onmouseleave={() => (hovered = null)}
              aria-label={m.label}
              data-testid={`branch-lifecycle-marker-${m.branch}-${m.tone}`}
            ></button>
          {/each}
        </div>
      </li>
    {/each}
  </ul>

  {#if hovered}
    <aside
      class="rounded-lg border border-slate-200 bg-slate-50 p-2 text-[11px] dark:border-gray-700 dark:bg-gray-800"
      data-testid="branch-lifecycle-tooltip"
    >
      <p class="font-semibold">{hovered.branch} · {hovered.label}</p>
      <p class="text-gray-500">{hovered.at}</p>
    </aside>
  {/if}
</section>
