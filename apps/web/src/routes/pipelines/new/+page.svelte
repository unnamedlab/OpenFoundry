<script lang="ts">
  import { goto } from '$app/navigation';
  import Glyph from '$components/ui/Glyph.svelte';
  import { listDatasets, type Dataset } from '$lib/api/datasets';
  import { createPipeline, updatePipeline, type PipelineNode } from '$lib/api/pipelines';

  type WizardStep = 1 | 2;

  let step = $state<WizardStep>(1);
  let name = $state('');
  let description = $state('');
  let saving = $state(false);
  let loadingDatasets = $state(false);
  let pipelineId = $state('');
  let error = $state('');
  let datasets = $state<Dataset[]>([]);
  let datasetSearch = $state('');
  let selectedDatasetIds = $state<string[]>([]);

  const visibleDatasets = $derived.by(() => {
    const term = datasetSearch.trim().toLowerCase();
    if (!term) return datasets;
    return datasets.filter((dataset) =>
      [dataset.name, dataset.description, dataset.format, dataset.tags.join(' ')].some((value) =>
        value.toLowerCase().includes(term),
      ),
    );
  });

  function createStarterNode(inputDatasetIds: string[] = []): PipelineNode {
    return {
      id: 'source_data',
      label: 'Imported Foundry data',
      transform_type: 'passthrough',
      config: {
        identity_columns: [],
      },
      depends_on: [],
      input_dataset_ids: inputDatasetIds,
      output_dataset_id: null,
    };
  }

  async function ensureDatasetsLoaded() {
    if (datasets.length > 0 || loadingDatasets) return;
    loadingDatasets = true;
    try {
      const response = await listDatasets({ per_page: 100 });
      datasets = response.data;
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to load datasets';
    } finally {
      loadingDatasets = false;
    }
  }

  async function handleCreatePipeline() {
    if (!name.trim()) {
      error = 'Pipeline name is required';
      return;
    }

    saving = true;
    error = '';
    try {
      const pipeline = await createPipeline({
        name: name.trim(),
        description: description.trim() || undefined,
        nodes: [createStarterNode()],
      });
      pipelineId = pipeline.id;
      step = 2;
      await ensureDatasetsLoaded();
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to create pipeline';
    } finally {
      saving = false;
    }
  }

  function toggleDataset(datasetId: string) {
    selectedDatasetIds = selectedDatasetIds.includes(datasetId)
      ? selectedDatasetIds.filter((candidate) => candidate !== datasetId)
      : [...selectedDatasetIds, datasetId];
  }

  async function handleAddData() {
    if (!pipelineId) return;
    saving = true;
    error = '';
    try {
      await updatePipeline(pipelineId, {
        nodes: [createStarterNode(selectedDatasetIds)],
      });
      goto(`/pipelines?pipeline=${pipelineId}`);
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to attach datasets';
    } finally {
      saving = false;
    }
  }

  function continueWithoutData() {
    if (!pipelineId) return;
    goto(`/pipelines?pipeline=${pipelineId}`);
  }
</script>

<div class="mx-auto max-w-6xl space-y-6">
  <div class="rounded-[28px] border border-slate-200 bg-white p-6 shadow-sm dark:border-gray-700 dark:bg-gray-900">
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div>
        <div class="text-xs font-semibold uppercase tracking-[0.24em] text-slate-400">Pipeline Builder</div>
        <h1 class="mt-2 text-3xl font-semibold text-slate-950 dark:text-slate-50">Create a new pipeline</h1>
        <p class="mt-2 max-w-2xl text-sm text-slate-500 dark:text-slate-300">
          Mirror the Foundry flow: first create the pipeline shell, then attach Foundry datasets before continuing in the full builder.
        </p>
      </div>

      <div class="flex items-center gap-3 text-sm">
        <div class={`rounded-full px-4 py-2 font-medium ${step === 1 ? 'bg-emerald-600 text-white' : 'bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-300'}`}>
          1. Create pipeline
        </div>
        <div class={`rounded-full px-4 py-2 font-medium ${step === 2 ? 'bg-emerald-600 text-white' : 'bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-300'}`}>
          2. Add Foundry data
        </div>
      </div>
    </div>
  </div>

  {#if error}
    <div class="rounded-2xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300">
      {error}
    </div>
  {/if}

  {#if step === 1}
    <div class="grid gap-6 xl:grid-cols-[1.1fr,0.9fr]">
      <section class="rounded-[28px] border border-slate-200 bg-white p-6 shadow-sm dark:border-gray-700 dark:bg-gray-900">
        <div class="mb-6">
          <div class="text-xs font-semibold uppercase tracking-[0.24em] text-slate-400">Step 1</div>
          <h2 class="mt-2 text-2xl font-semibold">Create a Pipeline</h2>
          <p class="mt-2 text-sm text-slate-500 dark:text-slate-300">
            Use the same defaults as your training flow: Batch Pipeline and Standard execution profile.
          </p>
        </div>

        <div class="grid gap-4 md:grid-cols-2">
          <div>
            <label for="pipeline-name" class="mb-1 block text-sm font-medium">Pipeline name</label>
            <input
              id="pipeline-name"
              type="text"
              bind:value={name}
              placeholder="Orders Pipeline"
              class="w-full rounded-2xl border border-slate-200 px-4 py-3 dark:border-gray-700 dark:bg-gray-950"
            />
          </div>
          <div>
            <label for="pipeline-description" class="mb-1 block text-sm font-medium">Description</label>
            <input
              id="pipeline-description"
              type="text"
              bind:value={description}
              placeholder="Optional summary for the training pipeline"
              class="w-full rounded-2xl border border-slate-200 px-4 py-3 dark:border-gray-700 dark:bg-gray-950"
            />
          </div>
        </div>

        <div class="mt-6 grid gap-4 md:grid-cols-2">
          <button type="button" class="rounded-[24px] border border-emerald-300 bg-emerald-50 p-5 text-left shadow-sm dark:border-emerald-800 dark:bg-emerald-950/30">
            <div class="flex items-center justify-between gap-3">
              <div>
                <div class="text-base font-semibold text-emerald-800 dark:text-emerald-300">Batch Pipeline</div>
                <div class="mt-1 text-sm text-emerald-700/80 dark:text-emerald-200/80">Recommended for imported training datasets and standard operational builds.</div>
              </div>
              <span class="rounded-full bg-emerald-600 px-3 py-1 text-xs font-semibold text-white">Selected</span>
            </div>
          </button>

          <button type="button" disabled class="rounded-[24px] border border-slate-200 bg-slate-50 p-5 text-left opacity-65 dark:border-gray-700 dark:bg-gray-950/40">
            <div class="flex items-center justify-between gap-3">
              <div>
                <div class="text-base font-semibold">Streaming Pipeline</div>
                <div class="mt-1 text-sm text-slate-500 dark:text-slate-300">Not needed for this training flow.</div>
              </div>
              <span class="rounded-full bg-slate-200 px-3 py-1 text-xs font-semibold text-slate-600 dark:bg-slate-800 dark:text-slate-300">Unavailable</span>
            </div>
          </button>
        </div>

        <div class="mt-4 grid gap-4 md:grid-cols-2">
          <button type="button" class="rounded-[24px] border border-blue-300 bg-blue-50 p-5 text-left shadow-sm dark:border-blue-800 dark:bg-blue-950/30">
            <div class="flex items-center justify-between gap-3">
              <div>
                <div class="text-base font-semibold text-blue-800 dark:text-blue-300">Standard</div>
                <div class="mt-1 text-sm text-blue-700/80 dark:text-blue-200/80">Balanced runtime for training use cases.</div>
              </div>
              <span class="rounded-full bg-blue-600 px-3 py-1 text-xs font-semibold text-white">Selected</span>
            </div>
          </button>

          <button type="button" disabled class="rounded-[24px] border border-slate-200 bg-slate-50 p-5 text-left opacity-65 dark:border-gray-700 dark:bg-gray-950/40">
            <div class="flex items-center justify-between gap-3">
              <div>
                <div class="text-base font-semibold">Faster</div>
                <div class="mt-1 text-sm text-slate-500 dark:text-slate-300">Reserved for low-latency execution paths.</div>
              </div>
              <span class="rounded-full bg-slate-200 px-3 py-1 text-xs font-semibold text-slate-600 dark:bg-slate-800 dark:text-slate-300">Unavailable</span>
            </div>
          </button>
        </div>

        <div class="mt-6 flex flex-wrap gap-3">
          <button type="button" onclick={handleCreatePipeline} disabled={saving} class="rounded-2xl bg-emerald-600 px-5 py-3 text-sm font-semibold text-white hover:bg-emerald-700 disabled:opacity-60">
            {saving ? 'Creating...' : 'Create Pipeline'}
          </button>
          <a href="/pipelines" class="rounded-2xl border border-slate-200 px-5 py-3 text-sm font-semibold hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800">
            Cancel
          </a>
        </div>
      </section>

      <aside class="rounded-[28px] border border-slate-200 bg-slate-50 p-6 shadow-sm dark:border-gray-700 dark:bg-gray-950/40">
        <div class="text-xs font-semibold uppercase tracking-[0.24em] text-slate-400">Why these defaults?</div>
        <ul class="mt-4 space-y-4 text-sm text-slate-600 dark:text-slate-300">
          <li class="rounded-2xl border border-slate-200 bg-white px-4 py-3 dark:border-gray-700 dark:bg-gray-900">
            <div class="font-semibold text-slate-900 dark:text-slate-50">Batch Pipeline</div>
            <div class="mt-1">You do not need high-frequency streaming updates for this use case.</div>
          </li>
          <li class="rounded-2xl border border-slate-200 bg-white px-4 py-3 dark:border-gray-700 dark:bg-gray-900">
            <div class="font-semibold text-slate-900 dark:text-slate-50">Standard profile</div>
            <div class="mt-1">This is enough for normal runs without the extra cost of faster execution.</div>
          </li>
          <li class="rounded-2xl border border-slate-200 bg-white px-4 py-3 dark:border-gray-700 dark:bg-gray-900">
            <div class="font-semibold text-slate-900 dark:text-slate-50">Starter node included</div>
            <div class="mt-1">OpenFoundry creates a starter input node so the selected datasets appear immediately in the full pipeline builder.</div>
          </li>
        </ul>
      </aside>
    </div>
  {:else}
    <div class="grid gap-6 xl:grid-cols-[1.15fr,0.85fr]">
      <section class="rounded-[28px] border border-slate-200 bg-white p-6 shadow-sm dark:border-gray-700 dark:bg-gray-900">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div>
            <div class="text-xs font-semibold uppercase tracking-[0.24em] text-slate-400">Step 2</div>
            <h2 class="mt-2 text-2xl font-semibold">Add Foundry data</h2>
            <p class="mt-2 text-sm text-slate-500 dark:text-slate-300">
              Pick the imported datasets you want to attach to <span class="font-semibold text-slate-900 dark:text-slate-50">{name}</span>.
            </p>
          </div>

          <label class="relative block min-w-[260px]">
            <span class="pointer-events-none absolute inset-y-0 left-4 flex items-center text-slate-400">
              <Glyph name="search" size={14} />
            </span>
            <input
              type="search"
              bind:value={datasetSearch}
              placeholder="Search imported datasets"
              class="w-full rounded-2xl border border-slate-200 py-3 pl-11 pr-4 dark:border-gray-700 dark:bg-gray-950"
            />
          </label>
        </div>

        {#if loadingDatasets}
          <div class="mt-6 rounded-2xl border border-dashed border-slate-300 px-4 py-12 text-center text-sm text-slate-500 dark:border-gray-700">
            Loading datasets...
          </div>
        {:else if datasets.length === 0}
          <div class="mt-6 rounded-2xl border border-dashed border-slate-300 px-4 py-12 text-center text-sm text-slate-500 dark:border-gray-700">
            No datasets are available yet. <a href="/datasets/upload" class="font-semibold text-blue-600 hover:underline">Upload a dataset first</a>.
          </div>
        {:else}
          <div class="mt-6 grid gap-4 md:grid-cols-2">
            {#each visibleDatasets as dataset (dataset.id)}
              <article class={`rounded-[24px] border p-5 transition ${selectedDatasetIds.includes(dataset.id) ? 'border-emerald-400 bg-emerald-50 dark:border-emerald-700 dark:bg-emerald-950/30' : 'border-slate-200 bg-slate-50 dark:border-gray-700 dark:bg-gray-950/30'}`}>
                <div class="flex items-start justify-between gap-3">
                  <div class="min-w-0">
                    <div class="truncate text-base font-semibold">{dataset.name}</div>
                    <div class="mt-1 text-sm text-slate-500 dark:text-slate-300">{dataset.description || 'No description'}</div>
                  </div>
                  <button
                    type="button"
                    onclick={() => toggleDataset(dataset.id)}
                    class={`inline-flex h-10 w-10 items-center justify-center rounded-full border ${selectedDatasetIds.includes(dataset.id) ? 'border-emerald-600 bg-emerald-600 text-white' : 'border-slate-300 bg-white text-slate-700 dark:border-gray-600 dark:bg-gray-900 dark:text-slate-100'}`}
                    aria-label={selectedDatasetIds.includes(dataset.id) ? `Remove ${dataset.name}` : `Add ${dataset.name}`}
                  >
                    <Glyph name={selectedDatasetIds.includes(dataset.id) ? 'x' : 'plus'} size={16} />
                  </button>
                </div>

                <div class="mt-4 flex flex-wrap gap-2 text-xs text-slate-500 dark:text-slate-300">
                  <span class="rounded-full bg-white px-3 py-1 font-medium dark:bg-gray-900">{dataset.format.toUpperCase()}</span>
                  <span class="rounded-full bg-white px-3 py-1 font-medium dark:bg-gray-900">{dataset.row_count.toLocaleString()} rows</span>
                  <span class="rounded-full bg-white px-3 py-1 font-medium dark:bg-gray-900">v{dataset.current_version}</span>
                </div>

                {#if dataset.tags.length > 0}
                  <div class="mt-3 flex flex-wrap gap-2">
                    {#each dataset.tags.slice(0, 4) as tag}
                      <span class="rounded-full bg-slate-200 px-3 py-1 text-xs font-medium text-slate-600 dark:bg-slate-800 dark:text-slate-300">{tag}</span>
                    {/each}
                  </div>
                {/if}
              </article>
            {/each}
          </div>
        {/if}

        <div class="mt-6 flex flex-wrap gap-3">
          <button type="button" onclick={handleAddData} disabled={saving || datasets.length === 0} class="rounded-2xl bg-emerald-600 px-5 py-3 text-sm font-semibold text-white hover:bg-emerald-700 disabled:opacity-60">
            {saving ? 'Saving...' : 'Add Data'}
          </button>
          <button type="button" onclick={continueWithoutData} disabled={saving} class="rounded-2xl border border-slate-200 px-5 py-3 text-sm font-semibold hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800">
            Continue without data
          </button>
        </div>
      </section>

      <aside class="rounded-[28px] border border-slate-200 bg-slate-50 p-6 shadow-sm dark:border-gray-700 dark:bg-gray-950/40">
        <div class="text-xs font-semibold uppercase tracking-[0.24em] text-slate-400">Selection summary</div>
        <div class="mt-4 rounded-2xl border border-slate-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-900">
          <div class="text-sm text-slate-500 dark:text-slate-300">Pipeline</div>
          <div class="mt-1 text-lg font-semibold">{name}</div>
        </div>

        <div class="mt-4 rounded-2xl border border-slate-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-900">
          <div class="flex items-center justify-between">
            <div>
              <div class="text-sm text-slate-500 dark:text-slate-300">Datasets selected</div>
              <div class="mt-1 text-3xl font-semibold">{selectedDatasetIds.length}</div>
            </div>
            <div class="rounded-full bg-emerald-100 px-3 py-1 text-xs font-semibold text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300">
              Source node ready
            </div>
          </div>
          <div class="mt-4 space-y-2">
            {#if selectedDatasetIds.length > 0}
              {#each datasets.filter((dataset) => selectedDatasetIds.includes(dataset.id)) as dataset (dataset.id)}
                <div class="rounded-xl bg-slate-100 px-3 py-2 text-sm dark:bg-slate-800">{dataset.name}</div>
              {/each}
            {:else}
              <div class="text-sm text-slate-500 dark:text-slate-300">Choose one or more imported datasets, then click Add Data.</div>
            {/if}
          </div>
        </div>

        <div class="mt-4 rounded-2xl border border-slate-200 bg-white p-4 text-sm text-slate-600 dark:border-gray-700 dark:bg-gray-900 dark:text-slate-300">
          After this step, OpenFoundry will open the full pipeline builder with a starter node already wired to the datasets you selected.
        </div>
      </aside>
    </div>
  {/if}
</div>
