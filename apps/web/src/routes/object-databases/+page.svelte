<script lang="ts">
  import { onMount } from 'svelte';
  import Glyph from '$components/ui/Glyph.svelte';
  import {
    getOntologyStorageInsights,
    type OntologyStorageInsights
  } from '$lib/api/ontology';

  interface LayerCard {
    title: string;
    glyph: 'database' | 'graph' | 'search' | 'run' | 'folder';
    tone: string;
    metric: string;
    detail: string;
    href: string;
    cta: string;
  }

  interface RuntimeMilestone {
    label: string;
    value: string;
    detail: string;
  }

  const numberFormatter = new Intl.NumberFormat('en-US');
  const dateFormatter = new Intl.DateTimeFormat('en-GB', {
    dateStyle: 'medium',
    timeStyle: 'short'
  });

  let loading = $state(true);
  let pageError = $state('');
  let insights = $state<OntologyStorageInsights | null>(null);

  function formatCount(value: number) {
    return numberFormatter.format(value);
  }

  function formatDate(value: string | null | undefined) {
    if (!value) return 'No activity recorded';
    const parsed = new Date(value);
    return Number.isNaN(parsed.getTime()) ? 'No activity recorded' : dateFormatter.format(parsed);
  }

  function getMetricCount(key: string) {
    return insights?.table_metrics.find((metric) => metric.key === key)?.record_count ?? 0;
  }

  const headlineCards = $derived.by(() => [
    {
      label: 'Object rows',
      value: formatCount(getMetricCount('object_instances')),
      detail: 'Canonical ontology instances persisted in the transactional store.'
    },
    {
      label: 'Link rows',
      value: formatCount(getMetricCount('link_instances')),
      detail: 'Relationship edges materialized for graph traversal and object views.'
    },
    {
      label: 'Search documents',
      value: formatCount(insights?.search_documents_total ?? 0),
      detail: 'Projection documents synthesized by the ontology indexer for search surfaces.'
    },
    {
      label: 'Funnel sources',
      value: formatCount(getMetricCount('funnel_sources')),
      detail: 'Ingress definitions that hydrate object rows from datasets and pipelines.'
    }
  ]);

  const layerCards = $derived.by<LayerCard[]>(() => [
    {
      title: 'Transactional object store',
      glyph: 'database',
      tone: 'bg-[#e7f3ff] text-[#2458b8]',
      metric: `${formatCount(getMetricCount('object_instances'))} rows`,
      detail:
        'Objects land in PostgreSQL-backed `object_instances`, with schema contracts held in `object_types`, `properties`, `interfaces`, and shared property bindings.',
      href: '/object-link-types',
      cta: 'Review schema'
    },
    {
      title: 'Graph relationship store',
      glyph: 'graph',
      tone: 'bg-[#eaf7ef] text-[#2f6d35]',
      metric: `${formatCount(getMetricCount('link_instances'))} edges`,
      detail:
        'Links are first-class rows in `link_instances`, keyed by `link_types`, which gives OpenFoundry a concrete object-graph substrate rather than a docs-only abstraction.',
      href: '/vertex',
      cta: 'Open graph product'
    },
    {
      title: 'Search projection layer',
      glyph: 'search',
      tone: 'bg-[#f1ecff] text-[#6f42c1]',
      metric: `${formatCount(insights?.search_documents_total ?? 0)} docs`,
      detail:
        'The ontology indexer materializes searchable projection documents from types, interfaces, links, actions, and accessible objects for Object Explorer and semantic search.',
      href: '/object-explorer',
      cta: 'Explore search'
    },
    {
      title: 'Ingestion and hydration runtime',
      glyph: 'run',
      tone: 'bg-[#fff1df] text-[#a35a11]',
      metric: `${formatCount(getMetricCount('funnel_runs'))} runs`,
      detail:
        'Funnel sources and runs form the hydration control plane, bridging datasets and pipelines into persisted ontology rows with batch and streaming posture.',
      href: '/ontology-indexing',
      cta: 'Operate indexing'
    },
    {
      title: 'Governance and scoping',
      glyph: 'folder',
      tone: 'bg-[#f3efe7] text-[#6e5330]',
      metric: `${formatCount(getMetricCount('projects'))} projects`,
      detail:
        'Ontology projects, resource bindings, and manager surfaces define how persisted objects and schema resources are segmented, reviewed, and shipped.',
      href: '/ontology-manager',
      cta: 'Open manager'
    }
  ]);

  const groupedTables = $derived.by(() => {
    const groups = [
      { label: 'Schema tables', role: 'Schema' },
      { label: 'Runtime tables', role: 'Runtime' },
      { label: 'Ingestion tables', role: 'Ingestion' },
      { label: 'Governance tables', role: 'Governance' }
    ];

    return groups
      .map((group) => ({
        ...group,
        tables: (insights?.table_metrics ?? []).filter((metric) => metric.role === group.role)
      }))
      .filter((group) => group.tables.length > 0);
  });

  const runtimeMilestones = $derived.by<RuntimeMilestone[]>(() => [
    {
      label: 'Latest object write',
      value: formatDate(insights?.latest_object_write_at),
      detail: 'Most recent `updated_at` observed in `object_instances`.'
    },
    {
      label: 'Latest link write',
      value: formatDate(insights?.latest_link_write_at),
      detail: 'Most recent materialized relationship row in `link_instances`.'
    },
    {
      label: 'Latest funnel run',
      value: formatDate(insights?.latest_funnel_run_at),
      detail: 'Most recent ingestion attempt across all ontology funnel sources.'
    }
  ]);

  async function load() {
    loading = true;
    pageError = '';

    try {
      insights = await getOntologyStorageInsights();
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to load object database insights';
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    load();
  });
</script>

<svelte:head>
  <title>OpenFoundry - Object Databases</title>
</svelte:head>

<div class="min-h-screen bg-[radial-gradient(circle_at_top_left,_rgba(36,88,184,0.10),_transparent_34%),linear-gradient(180deg,_#f8fbff_0%,_#ffffff_38%,_#f6f3ea_100%)]">
  <div class="mx-auto max-w-7xl px-6 py-10">
    <section class="overflow-hidden rounded-[32px] border border-slate-200/80 bg-white/90 shadow-[0_30px_90px_-48px_rgba(15,23,42,0.48)] backdrop-blur">
      <div class="grid gap-8 px-8 py-9 lg:grid-cols-[1.4fr_0.8fr]">
        <div>
          <div class="inline-flex items-center gap-2 rounded-full border border-slate-200 bg-slate-50 px-3 py-1 text-[11px] font-semibold uppercase tracking-[0.24em] text-slate-500">
            <Glyph name="database" size={14} />
            Object Databases
          </div>
          <h1 class="mt-4 max-w-3xl text-4xl font-semibold tracking-tight text-slate-900">
            Inspect the real ontology storage topology behind OpenFoundry.
          </h1>
          <p class="mt-4 max-w-3xl text-sm leading-7 text-slate-600">
            This product turns the architecture topic into a first-class operational surface: PostgreSQL-backed object rows, graph edges, search projections, Funnel hydration, and governance tables are all shown from the live ontology runtime.
          </p>
          <div class="mt-6 flex flex-wrap gap-3">
            <button
              type="button"
              class="inline-flex items-center gap-2 rounded-full bg-slate-900 px-4 py-2 text-sm font-medium text-white transition hover:bg-slate-800"
              onclick={load}
            >
              <Glyph name="history" size={16} />
              Refresh insights
            </button>
            <a
              href="/ontology"
              class="inline-flex items-center gap-2 rounded-full border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 transition hover:border-slate-400 hover:bg-slate-50"
            >
              <Glyph name="ontology" size={16} />
              Back to ontology hub
            </a>
          </div>
        </div>
        <div class="rounded-[28px] border border-slate-200 bg-slate-950 px-6 py-6 text-slate-100">
          <p class="text-xs font-semibold uppercase tracking-[0.28em] text-slate-400">Storage runtime</p>
          <div class="mt-4 space-y-4 text-sm">
            <div class="rounded-2xl border border-white/10 bg-white/5 p-4">
              <div class="flex items-center justify-between gap-3">
                <span class="text-slate-400">Primary backend</span>
                <span class="font-medium">{insights?.database_backend ?? 'Loading...'}</span>
              </div>
              <div class="mt-2 flex items-center justify-between gap-3">
                <span class="text-slate-400">Access driver</span>
                <span class="font-medium">{insights?.access_driver ?? 'Loading...'}</span>
              </div>
            </div>
            <div class="rounded-2xl border border-white/10 bg-white/5 p-4">
              <div class="flex items-center justify-between gap-3">
                <span class="text-slate-400">Graph projection</span>
                <span class="text-right font-medium">{insights?.graph_projection ?? 'Loading...'}</span>
              </div>
              <div class="mt-2 flex items-center justify-between gap-3">
                <span class="text-slate-400">Search projection</span>
                <span class="text-right font-medium">{insights?.search_projection ?? 'Loading...'}</span>
              </div>
              <div class="mt-2 flex items-center justify-between gap-3">
                <span class="text-slate-400">Hydration runtime</span>
                <span class="text-right font-medium">{insights?.funnel_runtime ?? 'Loading...'}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>

    {#if pageError}
      <div class="mt-6 rounded-3xl border border-rose-200 bg-rose-50 px-5 py-4 text-sm text-rose-700">
        {pageError}
      </div>
    {/if}

    {#if loading}
      <div class="mt-6 rounded-[28px] border border-slate-200 bg-white px-6 py-14 text-center text-sm text-slate-500">
        Loading object database insights...
      </div>
    {:else if insights}
      <section class="mt-6 grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        {#each headlineCards as card}
          <article class="rounded-[28px] border border-slate-200 bg-white px-5 py-5 shadow-sm">
            <p class="text-xs font-semibold uppercase tracking-[0.24em] text-slate-400">{card.label}</p>
            <p class="mt-3 text-3xl font-semibold tracking-tight text-slate-900">{card.value}</p>
            <p class="mt-2 text-sm leading-6 text-slate-500">{card.detail}</p>
          </article>
        {/each}
      </section>

      <section class="mt-8">
        <div class="mb-4 flex items-end justify-between gap-4">
          <div>
            <p class="text-xs font-semibold uppercase tracking-[0.28em] text-slate-400">Topology</p>
            <h2 class="mt-2 text-2xl font-semibold tracking-tight text-slate-900">Storage layers mapped to real products</h2>
          </div>
        </div>
        <div class="grid gap-4 xl:grid-cols-5">
          {#each layerCards as layer}
            <article class="flex h-full flex-col rounded-[28px] border border-slate-200 bg-white px-5 py-5 shadow-sm">
              <div class={`inline-flex w-fit items-center gap-2 rounded-full px-3 py-1 text-xs font-semibold ${layer.tone}`}>
                <Glyph name={layer.glyph} size={14} />
                {layer.title}
              </div>
              <p class="mt-4 text-2xl font-semibold tracking-tight text-slate-900">{layer.metric}</p>
              <p class="mt-3 flex-1 text-sm leading-6 text-slate-500">{layer.detail}</p>
              <a href={layer.href} class="mt-4 inline-flex items-center gap-2 text-sm font-medium text-slate-900 transition hover:text-slate-700">
                {layer.cta}
                <Glyph name="chevron-right" size={15} />
              </a>
            </article>
          {/each}
        </div>
      </section>

      <section class="mt-8 grid gap-6 xl:grid-cols-[1.05fr_0.95fr]">
        <article class="rounded-[28px] border border-slate-200 bg-white px-6 py-6 shadow-sm">
          <div class="flex items-start justify-between gap-4">
            <div>
              <p class="text-xs font-semibold uppercase tracking-[0.28em] text-slate-400">Tables</p>
              <h2 class="mt-2 text-2xl font-semibold tracking-tight text-slate-900">Persistent storage inventory</h2>
            </div>
            <div class="rounded-full bg-slate-100 px-3 py-1 text-xs font-medium text-slate-600">
              {formatCount(insights.table_metrics.length)} tracked tables
            </div>
          </div>
          <div class="mt-6 space-y-5">
            {#each groupedTables as group}
              <div>
                <h3 class="text-sm font-semibold text-slate-900">{group.label}</h3>
                <div class="mt-3 overflow-hidden rounded-3xl border border-slate-200">
                  <table class="min-w-full divide-y divide-slate-200 text-left text-sm">
                    <thead class="bg-slate-50 text-slate-500">
                      <tr>
                        <th class="px-4 py-3 font-medium">Label</th>
                        <th class="px-4 py-3 font-medium">Table</th>
                        <th class="px-4 py-3 font-medium text-right">Rows</th>
                      </tr>
                    </thead>
                    <tbody class="divide-y divide-slate-100 bg-white">
                      {#each group.tables as metric}
                        <tr>
                          <td class="px-4 py-3 font-medium text-slate-900">{metric.label}</td>
                          <td class="px-4 py-3 font-mono text-xs text-slate-500">{metric.table_name}</td>
                          <td class="px-4 py-3 text-right text-slate-700">{formatCount(metric.record_count)}</td>
                        </tr>
                      {/each}
                    </tbody>
                  </table>
                </div>
              </div>
            {/each}
          </div>
        </article>

        <article class="rounded-[28px] border border-slate-200 bg-white px-6 py-6 shadow-sm">
          <p class="text-xs font-semibold uppercase tracking-[0.28em] text-slate-400">Activity</p>
          <h2 class="mt-2 text-2xl font-semibold tracking-tight text-slate-900">Runtime milestones</h2>
          <div class="mt-6 space-y-4">
            {#each runtimeMilestones as milestone}
              <div class="rounded-3xl border border-slate-200 bg-slate-50 px-4 py-4">
                <div class="flex items-center justify-between gap-3">
                  <p class="text-sm font-medium text-slate-900">{milestone.label}</p>
                  <p class="text-sm text-slate-600">{milestone.value}</p>
                </div>
                <p class="mt-2 text-sm leading-6 text-slate-500">{milestone.detail}</p>
              </div>
            {/each}
          </div>

          <div class="mt-8">
            <p class="text-xs font-semibold uppercase tracking-[0.28em] text-slate-400">Search kinds</p>
            <div class="mt-4 space-y-3">
              {#each insights.search_documents_by_kind as metric}
                <div>
                  <div class="mb-1 flex items-center justify-between gap-3 text-sm">
                    <span class="font-medium capitalize text-slate-800">{metric.kind.replaceAll('_', ' ')}</span>
                    <span class="text-slate-500">{formatCount(metric.count)}</span>
                  </div>
                  <div class="h-2 overflow-hidden rounded-full bg-slate-100">
                    <div
                      class="h-full rounded-full bg-slate-900"
                      style={`width: ${Math.max(8, insights.search_documents_total > 0 ? (metric.count / insights.search_documents_total) * 100 : 0)}%`}
                    ></div>
                  </div>
                </div>
              {/each}
            </div>
          </div>
        </article>
      </section>

      <section class="mt-8 grid gap-6 xl:grid-cols-2">
        <article class="rounded-[28px] border border-slate-200 bg-white px-6 py-6 shadow-sm">
          <p class="text-xs font-semibold uppercase tracking-[0.28em] text-slate-400">Distribution</p>
          <h2 class="mt-2 text-2xl font-semibold tracking-tight text-slate-900">Object rows by type</h2>
          <div class="mt-6 space-y-3">
            {#each insights.object_type_distribution as metric}
              <div>
                <div class="mb-1 flex items-center justify-between gap-3 text-sm">
                  <span class="font-medium text-slate-800">{metric.label}</span>
                  <span class="text-slate-500">{formatCount(metric.count)}</span>
                </div>
                <div class="h-2 overflow-hidden rounded-full bg-slate-100">
                  <div
                    class="h-full rounded-full bg-[#2458b8]"
                    style={`width: ${Math.max(8, getMetricCount('object_instances') > 0 ? (metric.count / getMetricCount('object_instances')) * 100 : 0)}%`}
                  ></div>
                </div>
              </div>
            {/each}
          </div>
        </article>

        <article class="rounded-[28px] border border-slate-200 bg-white px-6 py-6 shadow-sm">
          <p class="text-xs font-semibold uppercase tracking-[0.28em] text-slate-400">Distribution</p>
          <h2 class="mt-2 text-2xl font-semibold tracking-tight text-slate-900">Link rows by type</h2>
          <div class="mt-6 space-y-3">
            {#each insights.link_type_distribution as metric}
              <div>
                <div class="mb-1 flex items-center justify-between gap-3 text-sm">
                  <span class="font-medium text-slate-800">{metric.label}</span>
                  <span class="text-slate-500">{formatCount(metric.count)}</span>
                </div>
                <div class="h-2 overflow-hidden rounded-full bg-slate-100">
                  <div
                    class="h-full rounded-full bg-[#2f6d35]"
                    style={`width: ${Math.max(8, getMetricCount('link_instances') > 0 ? (metric.count / getMetricCount('link_instances')) * 100 : 0)}%`}
                  ></div>
                </div>
              </div>
            {/each}
          </div>
        </article>
      </section>

      <section class="mt-8 rounded-[28px] border border-slate-200 bg-white px-6 py-6 shadow-sm">
        <div class="flex items-end justify-between gap-4">
          <div>
            <p class="text-xs font-semibold uppercase tracking-[0.28em] text-slate-400">Indexes</p>
            <h2 class="mt-2 text-2xl font-semibold tracking-tight text-slate-900">Database access paths</h2>
            <p class="mt-2 max-w-3xl text-sm leading-6 text-slate-500">
              These definitions come directly from PostgreSQL metadata. They make the object store, relationship traversal, Funnel hydration, and schema browsing surfaces materially inspectable from the repo itself.
            </p>
          </div>
          <div class="rounded-full bg-slate-100 px-3 py-1 text-xs font-medium text-slate-600">
            {formatCount(insights.index_definitions.length)} indexes
          </div>
        </div>
        <div class="mt-6 grid gap-4 xl:grid-cols-2">
          {#each insights.index_definitions as indexDef}
            <article class="rounded-3xl border border-slate-200 bg-slate-50 px-4 py-4">
              <div class="flex items-center justify-between gap-3">
                <p class="font-medium text-slate-900">{indexDef.index_name}</p>
                <span class="rounded-full bg-white px-2.5 py-1 font-mono text-[11px] text-slate-500">{indexDef.table_name}</span>
              </div>
              <p class="mt-3 overflow-x-auto font-mono text-xs leading-6 text-slate-600">{indexDef.index_definition}</p>
            </article>
          {/each}
        </div>
      </section>
    {/if}
  </div>
</div>
