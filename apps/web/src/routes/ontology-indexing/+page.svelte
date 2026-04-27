<script lang="ts">
  import { onMount } from 'svelte';
  import Glyph from '$components/ui/Glyph.svelte';
  import {
    createOntologyFunnelSource,
    deleteOntologyFunnelSource,
    getOntologyFunnelHealth,
    listLinkTypes,
    listObjectTypes,
    listOntologyFunnelRuns,
    listOntologyFunnelSources,
    listProperties,
    triggerOntologyFunnelRun,
    updateOntologyFunnelSource,
    type LinkType,
    type ObjectType,
    type OntologyFunnelHealthSummary,
    type OntologyFunnelPropertyMapping,
    type OntologyFunnelRun,
    type OntologyFunnelSource,
    type Property
  } from '$lib/api/ontology';
  import { listDatasets, previewDataset, type Dataset, type DatasetPreviewResponse } from '$lib/api/datasets';
  import { listPipelines, type Pipeline } from '$lib/api/pipelines';

  type IndexingTab = 'overview' | 'sources' | 'batch' | 'streaming' | 'restrictions' | 'faq';
  type IndexingMode = 'batch' | 'streaming';

  interface SourceDraft {
    name: string;
    description: string;
    object_type_id: string;
    dataset_id: string;
    pipeline_id: string;
    dataset_branch: string;
    dataset_version: string;
    preview_limit: string;
    default_marking: string;
    status: string;
    indexing_mode: IndexingMode;
    replacement_pipeline_enabled: boolean;
    full_reindex_threshold: string;
    stream_compute_profile: string;
    stream_consistency_guarantee: string;
    retention_window_days: string;
    supports_user_edits: boolean;
    property_mappings_json: string;
  }

  interface RestrictionFinding {
    severity: 'critical' | 'warning' | 'info';
    title: string;
    detail: string;
  }

  const tabs: Array<{ id: IndexingTab; label: string; glyph: 'home' | 'folder' | 'history' | 'run' | 'settings' | 'help' }> = [
    { id: 'overview', label: 'Overview', glyph: 'home' },
    { id: 'sources', label: 'Sources', glyph: 'folder' },
    { id: 'batch', label: 'Batch Funnel', glyph: 'history' },
    { id: 'streaming', label: 'Streaming Funnel', glyph: 'run' },
    { id: 'restrictions', label: 'Restrictions', glyph: 'settings' },
    { id: 'faq', label: 'FAQ', glyph: 'help' }
  ];

  const primaryKeyRestrictedTypes = new Set(['geo_point', 'array', 'float']);

  let loading = $state(true);
  let saving = $state(false);
  let running = $state(false);
  let activeTab = $state<IndexingTab>('overview');
  let pageError = $state('');
  let pageSuccess = $state('');

  let objectTypes = $state<ObjectType[]>([]);
  let linkTypes = $state<LinkType[]>([]);
  let datasets = $state<Dataset[]>([]);
  let pipelines = $state<Pipeline[]>([]);
  let sources = $state<OntologyFunnelSource[]>([]);
  let healthSummary = $state<OntologyFunnelHealthSummary | null>(null);
  let runs = $state<OntologyFunnelRun[]>([]);
  let propertiesByType = $state<Record<string, Property[]>>({});
  let datasetPreview = $state<DatasetPreviewResponse | null>(null);

  let selectedSourceId = $state('');
  let staleAfterHours = $state(24);
  let draft = $state<SourceDraft>(createEmptyDraft());

  function createEmptyDraft(): SourceDraft {
    return {
      name: '',
      description: '',
      object_type_id: '',
      dataset_id: '',
      pipeline_id: '',
      dataset_branch: 'main',
      dataset_version: '',
      preview_limit: '500',
      default_marking: 'public',
      status: 'active',
      indexing_mode: 'batch',
      replacement_pipeline_enabled: false,
      full_reindex_threshold: '80',
      stream_compute_profile: 'standard',
      stream_consistency_guarantee: 'exactly_once',
      retention_window_days: '',
      supports_user_edits: true,
      property_mappings_json: '[]'
    };
  }

  const selectedSource = $derived(sources.find((item) => item.id === selectedSourceId) ?? null);
  const selectedSourceHealth = $derived(healthSummary?.sources.find((item) => item.source.id === selectedSourceId) ?? null);
  const selectedObjectType = $derived(objectTypes.find((item) => item.id === (selectedSource?.object_type_id ?? draft.object_type_id)) ?? null);
  const selectedProperties = $derived(propertiesByType[selectedObjectType?.id ?? ''] ?? []);
  const sourceMode = $derived((selectedSource?.trigger_context?.indexing_mode as IndexingMode | undefined) ?? draft.indexing_mode);

  const overviewStats = $derived.by(() => ({
    totalSources: sources.length,
    healthy: healthSummary?.healthy_sources ?? 0,
    degraded: (healthSummary?.degraded_sources ?? 0) + (healthSummary?.failing_sources ?? 0) + (healthSummary?.stale_sources ?? 0),
    batch: sources.filter((source) => ((source.trigger_context?.indexing_mode as string | undefined) ?? 'batch') === 'batch').length,
    streaming: sources.filter((source) => (source.trigger_context?.indexing_mode as string | undefined) === 'streaming').length
  }));

  const batchStages = $derived.by(() => {
    const latestRun = runs[0];
    const runStatus = latestRun?.status ?? 'never_run';
    const stageTone = (kind: 'changelog' | 'merge' | 'indexing' | 'hydration') => {
      if (!latestRun) return 'pending';
      if (runStatus === 'failed') return kind === 'changelog' ? 'failed' : 'blocked';
      if (runStatus.includes('errors')) return kind === 'hydration' ? 'warning' : 'healthy';
      return 'healthy';
    };

    return [
      {
        name: 'Changelog',
        status: stageTone('changelog'),
        detail: latestRun ? `${latestRun.rows_read} rows inspected from the input dataset.` : 'Awaiting first funnel execution.'
      },
      {
        name: 'Merge changes',
        status: stageTone('merge'),
        detail: latestRun ? `${latestRun.inserted_count + latestRun.updated_count} rows merged into the object-type snapshot.` : 'User edits and datasource changes have not been merged yet.'
      },
      {
        name: 'Indexing',
        status: stageTone('indexing'),
        detail: selectedSource?.trigger_context?.full_reindex_threshold
          ? `Full reindex threshold set to ${selectedSource.trigger_context.full_reindex_threshold}% changed rows.`
          : 'Incremental indexing is the default posture.'
      },
      {
        name: 'Hydration',
        status: stageTone('hydration'),
        detail: selectedSourceHealth?.health_reason ?? 'Hydration posture will update after the next successful run.'
      }
    ];
  });

  const restrictionFindings = $derived.by<RestrictionFinding[]>(() => {
    const findings: RestrictionFinding[] = [];
    const primaryKeyName = selectedObjectType?.primary_key_property ?? null;
    const primaryKeyProperty = selectedProperties.find((property) => property.name === primaryKeyName) ?? null;

    if (!selectedObjectType) {
      return findings;
    }

    if (!primaryKeyName) {
      findings.push({
        severity: 'critical',
        title: 'Primary key missing',
        detail: `${selectedObjectType.display_name} has no primary key property configured, so Funnel cannot enforce uniqueness during indexing.`
      });
    } else if (!primaryKeyProperty) {
      findings.push({
        severity: 'critical',
        title: 'Primary key property not found',
        detail: `The configured primary key "${primaryKeyName}" is not present in the current object type schema.`
      });
    } else {
      if (primaryKeyRestrictedTypes.has(primaryKeyProperty.property_type)) {
        findings.push({
          severity: 'critical',
          title: 'Restricted primary key type',
          detail: `Primary key property "${primaryKeyProperty.name}" uses type "${primaryKeyProperty.property_type}", which is not a safe OSv2 primary key posture.`
        });
      }
      if (!primaryKeyProperty.required) {
        findings.push({
          severity: 'warning',
          title: 'Primary key should be required',
          detail: `Primary key property "${primaryKeyProperty.name}" is not marked required, which weakens deterministic indexing behavior.`
        });
      }
    }

    const arrayProperties = selectedProperties.filter((property) => property.property_type === 'array');
    const timeDependentProperties = selectedProperties.filter((property) => property.time_dependent);
    const floatProperties = selectedProperties.filter((property) => property.property_type === 'float');

    if (sourceMode === 'streaming' && selectedProperties.length > 250) {
      findings.push({
        severity: 'critical',
        title: 'Streaming property limit exceeded',
        detail: `${selectedObjectType.display_name} has ${selectedProperties.length} properties. Streaming guidance in the reference product warns above 250 properties per object type.`
      });
    }

    if (arrayProperties.length > 0) {
      findings.push({
        severity: 'info',
        title: 'Array properties need indexing discipline',
        detail: `${arrayProperties.length} array properties exist. Large relationship surfaces may be better modeled as link types than oversized arrays.`
      });
    }

    if (timeDependentProperties.length > 0) {
      findings.push({
        severity: 'info',
        title: 'Time-dependent properties present',
        detail: `${timeDependentProperties.length} properties are time dependent. Validate that streaming or batch semantics still produce deterministic current-state indexing.`
      });
    }

    if (floatProperties.some((property) => property.name === primaryKeyName)) {
      findings.push({
        severity: 'critical',
        title: 'Float primary key detected',
        detail: 'Real-number primary keys are not a stable indexing contract. Move to a string or integer identifier.'
      });
    }

    const duplicatePrimaryKeys = (() => {
      if (!datasetPreview?.rows || !primaryKeyName) return [];
      const counts = new Map<string, number>();
      for (const row of datasetPreview.rows) {
        const value = row[primaryKeyName];
        const key = JSON.stringify(value ?? null);
        counts.set(key, (counts.get(key) ?? 0) + 1);
      }
      return [...counts.entries()].filter(([, count]) => count > 1);
    })();

    if (duplicatePrimaryKeys.length > 0) {
      findings.push({
        severity: 'warning',
        title: 'Duplicate primary keys in preview sample',
        detail: `The current dataset preview contains ${duplicatePrimaryKeys.length} duplicate primary key values in the sampled rows. Incremental indexing will need a last-write strategy or upstream deduplication.`
      });
    }

    return findings;
  });

  const streamingChecks = $derived.by(() => {
    const context = (selectedSource?.trigger_context ?? {}) as Record<string, unknown>;
    const consistency = String(context.stream_consistency_guarantee ?? draft.stream_consistency_guarantee);
    const retention = context.retention_window_days ?? draft.retention_window_days;
    const editsSupported = Boolean(context.supports_user_edits ?? draft.supports_user_edits);
    return [
      {
        label: 'Compute profile',
        value: String(context.stream_compute_profile ?? draft.stream_compute_profile),
        detail: 'Use larger profiles for high-throughput streams and smaller ones to save compute.'
      },
      {
        label: 'Consistency guarantee',
        value: consistency,
        detail: consistency === 'at_least_once'
          ? 'Lower latency, but consumers may see duplicate update notifications.'
          : 'Exactly-once favors deterministic downstream behavior at the cost of extra latency.'
      },
      {
        label: 'Retention window',
        value: retention ? `${retention} days` : 'not set',
        detail: 'A retention window constrains how much historical stream state is replayed during replacement cutovers.'
      },
      {
        label: 'User edits support',
        value: editsSupported ? 'enabled' : 'disabled',
        detail: 'User edits are not supported on streaming object types in the Palantir reference, so auxiliary batch paths are often needed.'
      }
    ];
  });

  function parsePropertyMappings(source: string) {
    try {
      const parsed = JSON.parse(source);
      if (!Array.isArray(parsed)) throw new Error('Mappings must be an array');
      return parsed as OntologyFunnelPropertyMapping[];
    } catch (error) {
      throw new Error(error instanceof Error ? error.message : 'Invalid property mappings JSON');
    }
  }

  function syncDraft(source: OntologyFunnelSource | null) {
    if (!source) {
      draft = createEmptyDraft();
      if (objectTypes[0]) draft.object_type_id = objectTypes[0].id;
      if (datasets[0]) draft.dataset_id = datasets[0].id;
      return;
    }

    const context = (source.trigger_context ?? {}) as Record<string, unknown>;
    draft = {
      name: source.name,
      description: source.description,
      object_type_id: source.object_type_id,
      dataset_id: source.dataset_id,
      pipeline_id: source.pipeline_id ?? '',
      dataset_branch: source.dataset_branch ?? 'main',
      dataset_version: source.dataset_version?.toString() ?? '',
      preview_limit: String(source.preview_limit),
      default_marking: source.default_marking,
      status: source.status,
      indexing_mode: (context.indexing_mode as IndexingMode | undefined) ?? 'batch',
      replacement_pipeline_enabled: Boolean(context.replacement_pipeline_enabled),
      full_reindex_threshold: String(context.full_reindex_threshold ?? 80),
      stream_compute_profile: String(context.stream_compute_profile ?? 'standard'),
      stream_consistency_guarantee: String(context.stream_consistency_guarantee ?? 'exactly_once'),
      retention_window_days: context.retention_window_days == null ? '' : String(context.retention_window_days),
      supports_user_edits: Boolean(context.supports_user_edits ?? true),
      property_mappings_json: JSON.stringify(source.property_mappings ?? [], null, 2)
    };
  }

  async function loadSourceContext(sourceId: string) {
    if (!sourceId) {
      runs = [];
      datasetPreview = null;
      return;
    }

    try {
      const source = sources.find((item) => item.id === sourceId) ?? null;
      if (!source) return;
      const [runsResponse, previewResponse] = await Promise.all([
        listOntologyFunnelRuns(sourceId, { page: 1, per_page: 20 }),
        previewDataset(source.dataset_id, { limit: 12 }).catch(() => null)
      ]);
      runs = runsResponse.data;
      datasetPreview = previewResponse;
      syncDraft(source);
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to load source context';
    }
  }

  async function loadPage() {
    loading = true;
    pageError = '';

    try {
      const [typeResponse, linkResponse, datasetResponse, pipelineResponse, sourceResponse, healthResponse] = await Promise.all([
        listObjectTypes({ page: 1, per_page: 200 }),
        listLinkTypes({ page: 1, per_page: 200 }).catch(() => ({ data: [], total: 0 })),
        listDatasets({ page: 1, per_page: 200 }),
        listPipelines({ page: 1, per_page: 200 }).catch(() => ({ data: [], total: 0, page: 1, per_page: 200 })),
        listOntologyFunnelSources({ page: 1, per_page: 200 }),
        getOntologyFunnelHealth({ stale_after_hours: staleAfterHours }).catch(() => null)
      ]);

      objectTypes = typeResponse.data;
      linkTypes = linkResponse.data;
      datasets = datasetResponse.data;
      pipelines = pipelineResponse.data;
      sources = sourceResponse.data;
      healthSummary = healthResponse;

      const propertyEntries = await Promise.all(
        objectTypes.map(async (objectType) => [objectType.id, await listProperties(objectType.id).catch(() => [])] as const)
      );
      propertiesByType = Object.fromEntries(propertyEntries);

      selectedSourceId = selectedSourceId || sources[0]?.id || '';
      syncDraft(sources.find((item) => item.id === selectedSourceId) ?? null);
      await loadSourceContext(selectedSourceId);
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to load ontology indexing';
    } finally {
      loading = false;
    }
  }

  async function refreshHealth() {
    healthSummary = await getOntologyFunnelHealth({ stale_after_hours: staleAfterHours }).catch(() => null);
  }

  async function saveSource() {
    saving = true;
    pageError = '';
    pageSuccess = '';

    try {
      const trigger_context = {
        indexing_mode: draft.indexing_mode,
        replacement_pipeline_enabled: draft.replacement_pipeline_enabled,
        full_reindex_threshold: Number(draft.full_reindex_threshold || 80),
        stream_compute_profile: draft.stream_compute_profile,
        stream_consistency_guarantee: draft.stream_consistency_guarantee,
        retention_window_days: draft.retention_window_days ? Number(draft.retention_window_days) : null,
        supports_user_edits: draft.supports_user_edits,
        live_pipeline_name: draft.pipeline_id
          ? (pipelines.find((pipeline) => pipeline.id === draft.pipeline_id)?.name ?? 'Selected pipeline')
          : draft.indexing_mode === 'streaming'
            ? 'Streaming indexing pipeline'
            : 'Batch indexing pipeline'
      };

      const body = {
        name: draft.name.trim(),
        description: draft.description.trim() || undefined,
        object_type_id: draft.object_type_id,
        dataset_id: draft.dataset_id,
        pipeline_id: draft.pipeline_id || null,
        dataset_branch: draft.dataset_branch.trim() || null,
        dataset_version: draft.dataset_version.trim() ? Number(draft.dataset_version) : null,
        preview_limit: Number(draft.preview_limit || 500),
        default_marking: draft.default_marking.trim() || 'public',
        status: draft.status,
        property_mappings: parsePropertyMappings(draft.property_mappings_json),
        trigger_context
      };

      if (selectedSourceId) {
        await updateOntologyFunnelSource(selectedSourceId, {
          name: body.name,
          description: body.description,
          pipeline_id: body.pipeline_id,
          dataset_branch: body.dataset_branch,
          dataset_version: body.dataset_version,
          preview_limit: body.preview_limit,
          default_marking: body.default_marking,
          status: body.status,
          property_mappings: body.property_mappings,
          trigger_context: body.trigger_context
        });
        pageSuccess = 'Indexing source updated.';
      } else {
        const created = await createOntologyFunnelSource(body);
        selectedSourceId = created.id;
        pageSuccess = 'Indexing source created.';
      }

      await loadPage();
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to save indexing source';
    } finally {
      saving = false;
    }
  }

  async function removeSource() {
    if (!selectedSourceId) return;
    if (typeof window !== 'undefined' && !window.confirm('Delete this funnel source?')) return;

    saving = true;
    pageError = '';
    pageSuccess = '';

    try {
      await deleteOntologyFunnelSource(selectedSourceId);
      selectedSourceId = '';
      pageSuccess = 'Indexing source deleted.';
      await loadPage();
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to delete indexing source';
    } finally {
      saving = false;
    }
  }

  async function runSource(dryRun: boolean) {
    if (!selectedSourceId) return;
    running = true;
    pageError = '';
    pageSuccess = '';

    try {
      await triggerOntologyFunnelRun(selectedSourceId, {
        dry_run: dryRun,
        skip_pipeline: false,
        limit: Number(draft.preview_limit || 500),
        dataset_branch: draft.dataset_branch.trim() || undefined,
        dataset_version: draft.dataset_version.trim() ? Number(draft.dataset_version) : undefined,
        trigger_context: {
          indexing_mode: draft.indexing_mode,
          initiated_from: dryRun ? 'indexing-preview' : 'indexing-run'
        }
      });
      pageSuccess = dryRun ? 'Dry run triggered.' : 'Funnel run triggered.';
      await Promise.all([loadSourceContext(selectedSourceId), refreshHealth()]);
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to trigger funnel run';
    } finally {
      running = false;
    }
  }

  function resetDraft() {
    selectedSourceId = '';
    syncDraft(null);
    runs = [];
    datasetPreview = null;
    pageError = '';
    pageSuccess = '';
  }

  function pipelineName(id: string | null) {
    if (!id) return 'No pipeline linked';
    return pipelines.find((pipeline) => pipeline.id === id)?.name ?? id;
  }

  function datasetName(id: string) {
    return datasets.find((dataset) => dataset.id === id)?.name ?? id;
  }

  function labelForType(id: string) {
    return objectTypes.find((objectType) => objectType.id === id)?.display_name ?? id;
  }

  function sourceModeOf(source: OntologyFunnelSource) {
    return ((source.trigger_context?.indexing_mode as string | undefined) ?? 'batch') as IndexingMode;
  }

  onMount(() => {
    void loadPage();
  });
</script>

<svelte:head>
  <title>OpenFoundry - Ontology Indexing</title>
</svelte:head>

<div class="mx-auto flex max-w-7xl flex-col gap-6 px-4 py-6">
  <section class="overflow-hidden rounded-[2rem] border border-slate-200 bg-[radial-gradient(circle_at_top_left,_rgba(15,118,110,0.18),_transparent_35%),linear-gradient(135deg,_#f7fcfb_0%,_#e8f7f3_42%,_#f8fbff_100%)] p-6 shadow-sm">
    <div class="grid gap-6 lg:grid-cols-[1.45fr_1fr]">
      <div class="space-y-4">
        <div class="inline-flex items-center gap-2 rounded-full border border-emerald-200 bg-white/80 px-3 py-1 text-xs font-semibold uppercase tracking-[0.24em] text-emerald-700">
          <Glyph name="graph" size={14} />
          Ontology architecture / Indexing
        </div>
        <div class="space-y-3">
          <h1 class="text-3xl font-semibold tracking-tight text-slate-950">Indexing</h1>
          <p class="max-w-3xl text-sm leading-6 text-slate-600">
            Operate Object Data Funnel indexing with dedicated batch and streaming surfaces: source catalog, health, runs, live versus replacement posture, hydration stages, data restrictions, and operational FAQ on top of the real funnel runtime.
          </p>
        </div>
        <div class="flex flex-wrap gap-3 text-xs text-slate-500">
          <a href="/ontologies" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-emerald-300 hover:text-emerald-700">Ontologies</a>
          <a href="/ontology-manager" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-emerald-300 hover:text-emerald-700">Ontology Manager</a>
          <a href="/object-link-types" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-emerald-300 hover:text-emerald-700">Object and Link Types</a>
        </div>
      </div>

      <div class="grid gap-3 sm:grid-cols-2">
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Sources</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{overviewStats.totalSources}</p>
          <p class="mt-1 text-sm text-slate-500">Configured funnel sources.</p>
        </div>
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Healthy</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{overviewStats.healthy}</p>
          <p class="mt-1 text-sm text-slate-500">Sources currently in healthy posture.</p>
        </div>
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Batch</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{overviewStats.batch}</p>
          <p class="mt-1 text-sm text-slate-500">Sources using batch indexing posture.</p>
        </div>
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Streaming</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{overviewStats.streaming}</p>
          <p class="mt-1 text-sm text-slate-500">Sources using streaming indexing posture.</p>
        </div>
      </div>
    </div>
  </section>

  {#if pageError}
    <div class="rounded-3xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{pageError}</div>
  {/if}
  {#if pageSuccess}
    <div class="rounded-3xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700">{pageSuccess}</div>
  {/if}

  {#if loading}
    <div class="rounded-3xl border border-slate-200 bg-white px-5 py-10 text-center text-sm text-slate-500">
      Loading indexing runtime...
    </div>
  {:else}
    <section class="rounded-[2rem] border border-slate-200 bg-white p-4 shadow-sm">
      <div class="flex flex-wrap gap-2">
        {#each tabs as tab}
          <button
            class={`inline-flex items-center gap-2 rounded-full px-4 py-2 text-sm font-medium transition ${
              activeTab === tab.id
                ? 'bg-slate-950 text-white'
                : 'border border-slate-200 bg-white text-slate-600 hover:border-slate-300'
            }`}
            onclick={() => activeTab = tab.id}
          >
            <Glyph name={tab.glyph} size={16} />
            {tab.label}
          </button>
        {/each}
      </div>
    </section>

    {#if activeTab === 'overview'}
      <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="flex items-start justify-between gap-4">
            <div>
              <p class="text-sm font-semibold text-slate-900">Indexing overview</p>
              <p class="mt-1 text-sm text-slate-500">Monitor funnel health across object types, compare batch versus streaming posture, and inspect the last observed run envelope.</p>
            </div>
            <div class="flex items-center gap-3">
              <label class="text-sm text-slate-700">
                <span class="mr-2 font-medium">Stale after</span>
                <input class="w-20 rounded-xl border border-slate-300 px-3 py-2 text-sm outline-none transition focus:border-emerald-500" type="number" min="1" bind:value={staleAfterHours} />
              </label>
              <button class="rounded-full border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:border-emerald-400 hover:text-emerald-700" onclick={() => void refreshHealth()}>
                Refresh health
              </button>
            </div>
          </div>

          <div class="mt-4 grid gap-3">
            {#each healthSummary?.sources ?? [] as item}
              <button
                class={`rounded-2xl border px-4 py-3 text-left transition ${selectedSourceId === item.source.id ? 'border-emerald-400 bg-emerald-50' : 'border-slate-200 bg-white hover:border-slate-300'}`}
                onclick={() => {
                  selectedSourceId = item.source.id;
                  void loadSourceContext(item.source.id);
                }}
              >
                <div class="flex items-start justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">{item.source.name}</p>
                    <p class="mt-1 text-sm text-slate-500">{item.health_reason}</p>
                  </div>
                  <span class={`rounded-full px-2 py-1 text-[11px] uppercase tracking-[0.2em] ${
                    item.health_status === 'healthy'
                      ? 'border border-emerald-200 bg-emerald-50 text-emerald-700'
                      : item.health_status === 'degraded' || item.health_status === 'stale'
                        ? 'border border-amber-200 bg-amber-50 text-amber-700'
                        : 'border border-rose-200 bg-rose-50 text-rose-700'
                  }`}>
                    {item.health_status}
                  </span>
                </div>
                <div class="mt-3 flex flex-wrap gap-2 text-[11px] text-slate-500">
                  <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1">{sourceModeOf(item.source)}</span>
                  <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1">{labelForType(item.source.object_type_id)}</span>
                  <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1">runs {item.total_runs}</span>
                </div>
              </button>
            {/each}
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-sm font-semibold text-slate-900">Selected source</div>
          {#if selectedSource}
            <div class="mt-4 space-y-4">
              <div class="rounded-3xl border border-slate-200 p-4">
                <div class="text-sm font-semibold text-slate-900">{selectedSource.name}</div>
                <div class="mt-2 text-sm text-slate-500">{selectedSource.description || 'No description provided.'}</div>
                <div class="mt-4 grid gap-2 text-sm text-slate-600">
                  <div>Object type: {labelForType(selectedSource.object_type_id)}</div>
                  <div>Dataset: {datasetName(selectedSource.dataset_id)}</div>
                  <div>Pipeline: {pipelineName(selectedSource.pipeline_id)}</div>
                  <div>Latest run: {selectedSourceHealth?.last_run_at ? new Date(selectedSourceHealth.last_run_at).toLocaleString() : 'Never'}</div>
                </div>
              </div>
              <div class="rounded-3xl border border-slate-200 p-4">
                <div class="text-sm font-semibold text-slate-900">Live vs replacement pipeline</div>
                <div class="mt-3 grid gap-3">
                  <div class="rounded-2xl border border-slate-200 bg-slate-50 p-3 text-sm text-slate-600">
                    <div class="font-medium text-slate-900">Live pipeline</div>
                    <div class="mt-1">{pipelineName(selectedSource.pipeline_id)}</div>
                  </div>
                  <div class="rounded-2xl border border-slate-200 bg-slate-50 p-3 text-sm text-slate-600">
                    <div class="font-medium text-slate-900">Replacement pipeline</div>
                    <div class="mt-1">
                      {(selectedSource.trigger_context?.replacement_pipeline_enabled as boolean | undefined)
                        ? 'Provisioned in background for schema or stream cutover changes.'
                        : 'No replacement pipeline currently staged.'}
                    </div>
                  </div>
                </div>
              </div>
            </div>
          {:else}
            <div class="mt-4 rounded-3xl border border-dashed border-slate-300 px-4 py-8 text-center text-sm text-slate-500">
              Create or select a funnel source to inspect its indexing posture.
            </div>
          {/if}
        </section>
      </div>
    {:else if activeTab === 'sources'}
      <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_380px]">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="flex items-start justify-between gap-4">
            <div>
              <p class="text-sm font-semibold text-slate-900">Funnel sources</p>
              <p class="mt-1 text-sm text-slate-500">Create, edit, run, and retire indexing sources across datasets and object types.</p>
            </div>
            <button class="rounded-full border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:border-emerald-400 hover:text-emerald-700" onclick={resetDraft}>
              New source
            </button>
          </div>
          <div class="mt-4 space-y-3">
            {#each sources as source}
              <button
                class={`w-full rounded-2xl border px-4 py-3 text-left transition ${selectedSourceId === source.id ? 'border-emerald-400 bg-emerald-50' : 'border-slate-200 bg-white hover:border-slate-300'}`}
                onclick={() => {
                  selectedSourceId = source.id;
                  void loadSourceContext(source.id);
                }}
              >
                <div class="flex items-start justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">{source.name}</p>
                    <p class="mt-1 text-sm text-slate-500">{source.description || 'No description provided.'}</p>
                  </div>
                  <span class="rounded-full border border-slate-200 bg-white px-2 py-1 text-[11px] uppercase tracking-[0.2em] text-slate-500">
                    {sourceModeOf(source)}
                  </span>
                </div>
              </button>
            {/each}
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-sm font-semibold text-slate-900">{selectedSourceId ? 'Edit source' : 'Create source'}</p>
          <div class="mt-4 space-y-4">
            <label class="space-y-2 text-sm text-slate-700">
              <span class="font-medium">Name</span>
              <input class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500" type="text" bind:value={draft.name} />
            </label>
            <label class="space-y-2 text-sm text-slate-700">
              <span class="font-medium">Description</span>
              <textarea rows="3" class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500" bind:value={draft.description}></textarea>
            </label>
            <div class="grid gap-4 sm:grid-cols-2">
              <label class="space-y-2 text-sm text-slate-700">
                <span class="font-medium">Object type</span>
                <select class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-emerald-500" bind:value={draft.object_type_id}>
                  {#each objectTypes as objectType}
                    <option value={objectType.id}>{objectType.display_name}</option>
                  {/each}
                </select>
              </label>
              <label class="space-y-2 text-sm text-slate-700">
                <span class="font-medium">Dataset</span>
                <select class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-emerald-500" bind:value={draft.dataset_id}>
                  {#each datasets as dataset}
                    <option value={dataset.id}>{dataset.name}</option>
                  {/each}
                </select>
              </label>
            </div>
            <div class="grid gap-4 sm:grid-cols-2">
              <label class="space-y-2 text-sm text-slate-700">
                <span class="font-medium">Linked pipeline</span>
                <select class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-emerald-500" bind:value={draft.pipeline_id}>
                  <option value="">No pipeline</option>
                  {#each pipelines as pipeline}
                    <option value={pipeline.id}>{pipeline.name}</option>
                  {/each}
                </select>
              </label>
              <label class="space-y-2 text-sm text-slate-700">
                <span class="font-medium">Mode</span>
                <select class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-emerald-500" bind:value={draft.indexing_mode}>
                  <option value="batch">batch</option>
                  <option value="streaming">streaming</option>
                </select>
              </label>
            </div>
            <div class="grid gap-4 sm:grid-cols-2">
              <label class="space-y-2 text-sm text-slate-700">
                <span class="font-medium">Dataset branch</span>
                <input class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500" type="text" bind:value={draft.dataset_branch} />
              </label>
              <label class="space-y-2 text-sm text-slate-700">
                <span class="font-medium">Dataset version</span>
                <input class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500" type="number" min="0" bind:value={draft.dataset_version} />
              </label>
            </div>
            <div class="grid gap-4 sm:grid-cols-3">
              <label class="space-y-2 text-sm text-slate-700">
                <span class="font-medium">Preview limit</span>
                <input class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500" type="number" min="1" bind:value={draft.preview_limit} />
              </label>
              <label class="space-y-2 text-sm text-slate-700">
                <span class="font-medium">Marking</span>
                <input class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500" type="text" bind:value={draft.default_marking} />
              </label>
              <label class="space-y-2 text-sm text-slate-700">
                <span class="font-medium">Status</span>
                <select class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-emerald-500" bind:value={draft.status}>
                  <option value="active">active</option>
                  <option value="paused">paused</option>
                </select>
              </label>
            </div>

            {#if draft.indexing_mode === 'streaming'}
              <div class="rounded-3xl border border-slate-200 bg-slate-50 p-4">
                <div class="text-sm font-semibold text-slate-900">Streaming configuration</div>
                <div class="mt-4 grid gap-4 sm:grid-cols-2">
                  <label class="space-y-2 text-sm text-slate-700">
                    <span class="font-medium">Compute profile</span>
                    <select class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-emerald-500" bind:value={draft.stream_compute_profile}>
                      <option value="small">small</option>
                      <option value="standard">standard</option>
                      <option value="high-throughput">high-throughput</option>
                    </select>
                  </label>
                  <label class="space-y-2 text-sm text-slate-700">
                    <span class="font-medium">Consistency</span>
                    <select class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-emerald-500" bind:value={draft.stream_consistency_guarantee}>
                      <option value="exactly_once">exactly_once</option>
                      <option value="at_least_once">at_least_once</option>
                    </select>
                  </label>
                </div>
                <div class="mt-4 grid gap-4 sm:grid-cols-2">
                  <label class="space-y-2 text-sm text-slate-700">
                    <span class="font-medium">Retention window days</span>
                    <input class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500" type="number" min="0" bind:value={draft.retention_window_days} />
                  </label>
                  <label class="flex items-center gap-2 rounded-2xl border border-slate-200 bg-white px-4 py-3 text-sm text-slate-700">
                    <input type="checkbox" checked={draft.supports_user_edits} onchange={(event) => draft.supports_user_edits = (event.currentTarget as HTMLInputElement).checked} />
                    Track auxiliary user edits posture
                  </label>
                </div>
              </div>
            {:else}
              <div class="rounded-3xl border border-slate-200 bg-slate-50 p-4">
                <div class="text-sm font-semibold text-slate-900">Batch configuration</div>
                <div class="mt-4 grid gap-4 sm:grid-cols-2">
                  <label class="space-y-2 text-sm text-slate-700">
                    <span class="font-medium">Full reindex threshold (%)</span>
                    <input class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500" type="number" min="1" max="100" bind:value={draft.full_reindex_threshold} />
                  </label>
                  <label class="flex items-center gap-2 rounded-2xl border border-slate-200 bg-white px-4 py-3 text-sm text-slate-700">
                    <input type="checkbox" checked={draft.replacement_pipeline_enabled} onchange={(event) => draft.replacement_pipeline_enabled = (event.currentTarget as HTMLInputElement).checked} />
                    Provision replacement pipeline posture
                  </label>
                </div>
              </div>
            {/if}

            <label class="space-y-2 text-sm text-slate-700">
              <span class="font-medium">Property mappings JSON</span>
              <textarea rows="8" class="w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 outline-none transition focus:border-emerald-500" bind:value={draft.property_mappings_json} spellcheck="false"></textarea>
            </label>

            <div class="flex flex-wrap gap-3">
              <button class="rounded-full bg-emerald-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-emerald-500 disabled:bg-emerald-300" onclick={() => void saveSource()} disabled={saving}>
                {saving ? 'Saving...' : selectedSourceId ? 'Save changes' : 'Create source'}
              </button>
              {#if selectedSourceId}
                <button class="rounded-full border border-slate-300 bg-white px-5 py-2.5 text-sm font-medium text-slate-700 hover:border-emerald-400 hover:text-emerald-700 disabled:opacity-60" onclick={() => void runSource(true)} disabled={running}>
                  {running ? 'Running...' : 'Dry run'}
                </button>
                <button class="rounded-full border border-slate-300 bg-white px-5 py-2.5 text-sm font-medium text-slate-700 hover:border-emerald-400 hover:text-emerald-700 disabled:opacity-60" onclick={() => void runSource(false)} disabled={running}>
                  {running ? 'Running...' : 'Run indexing'}
                </button>
                <button class="rounded-full border border-rose-200 bg-rose-50 px-5 py-2.5 text-sm font-medium text-rose-700 hover:border-rose-300 disabled:opacity-60" onclick={() => void removeSource()} disabled={saving}>
                  Delete
                </button>
              {/if}
            </div>
          </div>
        </section>
      </div>
    {:else if activeTab === 'batch'}
      <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-sm font-semibold text-slate-900">Funnel batch pipeline</div>
          <div class="mt-4 grid gap-3 xl:grid-cols-2">
            {#each batchStages as stage}
              <div class="rounded-2xl border border-slate-200 p-4">
                <div class="flex items-start justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">{stage.name}</p>
                    <p class="mt-2 text-sm text-slate-500">{stage.detail}</p>
                  </div>
                  <span class={`rounded-full px-2 py-1 text-[11px] uppercase tracking-[0.2em] ${
                    stage.status === 'healthy'
                      ? 'border border-emerald-200 bg-emerald-50 text-emerald-700'
                      : stage.status === 'warning'
                        ? 'border border-amber-200 bg-amber-50 text-amber-700'
                        : stage.status === 'failed' || stage.status === 'blocked'
                          ? 'border border-rose-200 bg-rose-50 text-rose-700'
                          : 'border border-slate-200 bg-slate-50 text-slate-600'
                  }`}>
                    {stage.status}
                  </span>
                </div>
              </div>
            {/each}
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-sm font-semibold text-slate-900">Recent runs</div>
          <div class="mt-4 space-y-3">
            {#each runs as run}
              <div class="rounded-2xl border border-slate-200 p-4">
                <div class="flex items-start justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">{run.status}</p>
                    <p class="mt-1 text-sm text-slate-500">{run.trigger_type} · rows {run.rows_read}</p>
                  </div>
                  <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1 text-[11px] uppercase tracking-[0.2em] text-slate-500">
                    {new Date(run.started_at).toLocaleDateString()}
                  </span>
                </div>
                {#if run.error_message}
                  <div class="mt-3 rounded-2xl border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-700">{run.error_message}</div>
                {/if}
              </div>
            {/each}
            {#if runs.length === 0}
              <div class="rounded-3xl border border-dashed border-slate-300 px-4 py-10 text-center text-sm text-slate-500">No funnel runs recorded yet.</div>
            {/if}
          </div>
        </section>
      </div>
    {:else if activeTab === 'streaming'}
      <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-sm font-semibold text-slate-900">Streaming posture</div>
          <div class="mt-4 grid gap-3">
            {#each streamingChecks as check}
              <div class="rounded-2xl border border-slate-200 p-4">
                <p class="text-sm font-semibold text-slate-900">{check.label}</p>
                <p class="mt-1 text-xs uppercase tracking-[0.2em] text-slate-500">{check.value}</p>
                <p class="mt-2 text-sm text-slate-500">{check.detail}</p>
              </div>
            {/each}
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-sm font-semibold text-slate-900">Current product limitations</div>
          <div class="mt-4 space-y-3 text-sm text-slate-600">
            <div class="rounded-2xl border border-slate-200 p-4">Streaming uses a most-recent-update-wins posture, so upstream event ordering still matters.</div>
            <div class="rounded-2xl border border-slate-200 p-4">Live refresh outside Workshop typically still depends on explicit refresh behavior in consumer apps.</div>
            <div class="rounded-2xl border border-slate-200 p-4">Monitoring and invalid-record metrics are much richer for batch than for streaming posture.</div>
            <div class="rounded-2xl border border-slate-200 p-4">Very wide object types and oversized records remain a poor fit for low-latency stream indexing.</div>
          </div>
        </section>
      </div>
    {:else if activeTab === 'restrictions'}
      <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-sm font-semibold text-slate-900">Data restrictions</div>
          <div class="mt-4 space-y-3">
            {#each restrictionFindings as finding}
              <div class="rounded-2xl border border-slate-200 p-4">
                <div class="flex items-start justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">{finding.title}</p>
                    <p class="mt-2 text-sm text-slate-500">{finding.detail}</p>
                  </div>
                  <span class={`rounded-full px-2 py-1 text-[11px] uppercase tracking-[0.2em] ${
                    finding.severity === 'critical'
                      ? 'border border-rose-200 bg-rose-50 text-rose-700'
                      : finding.severity === 'warning'
                        ? 'border border-amber-200 bg-amber-50 text-amber-700'
                        : 'border border-slate-200 bg-slate-50 text-slate-600'
                  }`}>
                    {finding.severity}
                  </span>
                </div>
              </div>
            {/each}
            {#if restrictionFindings.length === 0}
              <div class="rounded-3xl border border-dashed border-slate-300 px-4 py-10 text-center text-sm text-slate-500">No obvious indexing restrictions detected from the current schema and preview sample.</div>
            {/if}
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-sm font-semibold text-slate-900">Preview sample</div>
          {#if datasetPreview?.rows?.length}
            <div class="mt-4 overflow-x-auto rounded-2xl border border-slate-200">
              <table class="min-w-full text-left text-sm">
                <thead class="bg-slate-50 text-slate-600">
                  <tr>
                    {#each Object.keys(datasetPreview.rows[0] ?? {}).slice(0, 6) as column}
                      <th class="px-4 py-3 font-medium">{column}</th>
                    {/each}
                  </tr>
                </thead>
                <tbody>
                  {#each datasetPreview.rows.slice(0, 5) as row}
                    <tr class="border-t border-slate-200">
                      {#each Object.keys(datasetPreview.rows[0] ?? {}).slice(0, 6) as column}
                        <td class="px-4 py-3 text-slate-600">{JSON.stringify(row[column])}</td>
                      {/each}
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {:else}
            <div class="mt-4 rounded-3xl border border-dashed border-slate-300 px-4 py-10 text-center text-sm text-slate-500">Preview rows unavailable for this source.</div>
          {/if}
        </section>
      </div>
    {:else if activeTab === 'faq'}
      <div class="grid gap-4 xl:grid-cols-2">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-sm font-semibold text-slate-900">Operational FAQ</div>
          <div class="mt-4 grid gap-3">
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="font-medium text-slate-900">How do I know an object type is indexed?</div>
              <p class="mt-2 text-sm text-slate-500">
                {selectedSourceHealth?.health_status === 'healthy'
                  ? 'The selected source is healthy and the latest run completed successfully, so the object type is ready to query.'
                  : 'Check the source health and batch stages. A healthy latest run and completed hydration posture indicate readiness.'}
              </p>
            </div>
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="font-medium text-slate-900">Why might indexing fail now when older systems accepted it?</div>
              <p class="mt-2 text-sm text-slate-500">The restrictions panel surfaces stricter OSv2-like validation pressure around primary keys, array posture, property typing, and schema compatibility.</p>
            </div>
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="font-medium text-slate-900">How do I retry a build?</div>
              <p class="mt-2 text-sm text-slate-500">Use `Dry run` to validate posture first, then `Run indexing` from the source editor to trigger a new funnel run.</p>
            </div>
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="font-medium text-slate-900">When should I prefer streaming?</div>
              <p class="mt-2 text-sm text-slate-500">Prefer streaming when latency-sensitive workflows need near-real-time refresh and your object model remains narrow, ordered, and operationally simple.</p>
            </div>
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-sm font-semibold text-slate-900">Product posture</div>
          <div class="mt-4 grid gap-3">
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="font-medium text-slate-900">Full reindex threshold</div>
              <p class="mt-2 text-sm text-slate-500">
                {selectedSource?.trigger_context?.full_reindex_threshold
                  ? `This source is configured to consider a full reindex around ${selectedSource.trigger_context.full_reindex_threshold}% changed rows.`
                  : 'Incremental indexing is the default posture until a source-specific threshold is configured.'}
              </p>
            </div>
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="font-medium text-slate-900">Streaming latency expectation</div>
              <p class="mt-2 text-sm text-slate-500">
                {sourceMode === 'streaming' && draft.stream_consistency_guarantee === 'at_least_once'
                  ? 'This source is optimized for lower latency with at-least-once delivery semantics.'
                  : 'Exactly-once consistency remains the safer default when duplicate downstream reactions would be costly.'}
              </p>
            </div>
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="font-medium text-slate-900">Many-to-many links</div>
              <p class="mt-2 text-sm text-slate-500">There are {linkTypes.filter((linkType) => linkType.cardinality === 'many_to_many').length} many-to-many link types in the current ontology, which may also participate in streaming posture when modeled explicitly as links instead of giant arrays.</p>
            </div>
          </div>
        </section>
      </div>
    {/if}
  {/if}
</div>
