<!--
  P5 — Publish-to-Marketplace modal.

  Lives next to DatasetHeader.svelte. Opens from the "Publish to
  Marketplace…" entry in the Open in… dropdown (manage role only).
  Calls `publishDatasetProduct` on submit and surfaces a link to the
  newly created product in `/marketplace`.
-->
<script lang="ts">
  import {
    publishDatasetProduct,
    type Dataset,
    type DatasetProduct,
    type PublishDatasetProductRequest,
  } from '$lib/api/datasets';

  type Props = {
    dataset: Dataset;
    open: boolean;
    onClose: () => void;
    onPublished?: (product: DatasetProduct) => void;
  };

  const { dataset, open, onClose, onPublished }: Props = $props();

  let name = $state('');
  let version = $state('1.0.0');
  let projectId = $state('');
  let bootstrapMode = $state<'schema-only' | 'with-snapshot'>('schema-only');
  let includeSchema = $state(true);
  let includeBranches = $state(false);
  let includeRetention = $state(false);
  let includeSchedules = $state(false);
  let exportIncludesData = $state(false);
  let saving = $state(false);
  let errorMessage = $state<string | null>(null);
  let publishedUrl = $state<string | null>(null);

  // Sync default name with dataset.name on first open.
  $effect(() => {
    if (open && !name) name = dataset.name;
  });

  async function submit() {
    if (!name.trim()) {
      errorMessage = 'name is required';
      return;
    }
    saving = true;
    errorMessage = null;
    publishedUrl = null;
    try {
      const payload: PublishDatasetProductRequest = {
        name: name.trim(),
        version: version.trim() || '1.0.0',
        bootstrap_mode: bootstrapMode,
        include_schema: includeSchema,
        include_branches: includeBranches,
        include_retention: includeRetention,
        include_schedules: includeSchedules,
        export_includes_data: exportIncludesData,
      };
      if (projectId.trim()) payload.project_id = projectId.trim();
      const product = await publishDatasetProduct(dataset.id, payload);
      publishedUrl = `/marketplace/products/${product.id}`;
      onPublished?.(product);
    } catch (cause) {
      errorMessage = cause instanceof Error ? cause.message : 'Publish failed.';
    } finally {
      saving = false;
    }
  }
</script>

{#if open}
  <div
    class="fixed inset-0 z-40 flex items-center justify-center bg-slate-900/40 px-4"
    role="dialog"
    aria-modal="true"
    aria-labelledby="publish-modal-title"
    data-testid="publish-modal"
    onclick={onClose}
    onkeydown={(event) => {
      if (event.key === 'Escape') onClose();
    }}
    tabindex="-1"
  >
    <div
      class="w-full max-w-lg rounded-2xl border border-slate-200 bg-white p-6 shadow-xl dark:border-gray-700 dark:bg-gray-900"
      role="document"
      onclick={(event) => event.stopPropagation()}
      onkeydown={(event) => event.stopPropagation()}
      tabindex="-1"
    >
      <header class="mb-4 flex items-start justify-between gap-3">
        <div>
          <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Marketplace</div>
          <h3 id="publish-modal-title" class="mt-1 text-lg font-semibold">
            Publish dataset as product
          </h3>
          <p class="mt-1 text-sm text-slate-500">
            Captures a manifest snapshot of <span class="font-mono">{dataset.id}</span>
            so it can be re-installed in another project.
          </p>
        </div>
        <button
          type="button"
          class="rounded-md p-1 text-gray-500 hover:bg-slate-100 dark:hover:bg-gray-800"
          aria-label="Close"
          onclick={onClose}
        >
          ✕
        </button>
      </header>

      {#if publishedUrl}
        <div class="rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-900 dark:border-emerald-900/50 dark:bg-emerald-950/40 dark:text-emerald-100" data-testid="publish-success">
          Published. View it in the
          <a class="font-semibold underline" href={publishedUrl} data-testid="publish-success-link">Marketplace</a>.
        </div>
      {:else}
        <form
          class="space-y-3"
          onsubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          <label class="block text-sm">
            <span class="font-medium">Product name</span>
            <input
              class="mt-1 w-full rounded-md border border-slate-300 bg-white px-2 py-1 dark:border-gray-700 dark:bg-gray-900"
              bind:value={name}
              data-testid="publish-name"
            />
          </label>
          <div class="flex flex-wrap gap-3 text-sm">
            <label class="flex flex-1 flex-col">
              <span class="font-medium">Version</span>
              <input
                class="mt-1 w-full rounded-md border border-slate-300 bg-white px-2 py-1 dark:border-gray-700 dark:bg-gray-900"
                bind:value={version}
                data-testid="publish-version"
              />
            </label>
            <label class="flex flex-1 flex-col">
              <span class="font-medium">Project ID (optional)</span>
              <input
                class="mt-1 w-full rounded-md border border-slate-300 bg-white px-2 py-1 font-mono text-xs dark:border-gray-700 dark:bg-gray-900"
                placeholder="UUID"
                bind:value={projectId}
              />
            </label>
          </div>

          <fieldset class="rounded-md border border-slate-200 px-3 py-2 dark:border-gray-700">
            <legend class="px-1 text-xs uppercase tracking-wide text-slate-400">Manifest scope</legend>
            <label class="block py-1 text-sm">
              <input type="radio" bind:group={bootstrapMode} value="schema-only" data-testid="publish-mode-schema-only" />
              schema-only
              <span class="text-xs text-slate-500">— recreate dataset row + schema only</span>
            </label>
            <label class="block py-1 text-sm">
              <input type="radio" bind:group={bootstrapMode} value="with-snapshot" data-testid="publish-mode-with-snapshot" />
              with-snapshot
              <span class="text-xs text-slate-500">— additionally copy the current view's bytes</span>
            </label>
          </fieldset>

          <fieldset class="rounded-md border border-slate-200 px-3 py-2 dark:border-gray-700">
            <legend class="px-1 text-xs uppercase tracking-wide text-slate-400">Include</legend>
            <label class="block py-1 text-sm">
              <input type="checkbox" bind:checked={includeSchema} data-testid="publish-include-schema" />
              Schema
            </label>
            <label class="block py-1 text-sm">
              <input type="checkbox" bind:checked={includeBranches} />
              Branching policy
            </label>
            <label class="block py-1 text-sm">
              <input type="checkbox" bind:checked={includeRetention} data-testid="publish-include-retention" />
              Retention policies
            </label>
            <label class="block py-1 text-sm">
              <input type="checkbox" bind:checked={includeSchedules} />
              Pipeline schedules upstream
            </label>
            <label class="block py-1 text-sm">
              <input type="checkbox" bind:checked={exportIncludesData} />
              Include raw data (large)
            </label>
          </fieldset>

          {#if errorMessage}
            <div class="rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-800 dark:border-rose-900/50 dark:bg-rose-950/40 dark:text-rose-100" role="alert" data-testid="publish-error">
              {errorMessage}
            </div>
          {/if}

          <div class="flex justify-end gap-2">
            <button
              type="button"
              class="rounded-md border border-slate-300 px-3 py-1.5 text-sm hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800"
              onclick={onClose}
            >
              Cancel
            </button>
            <button
              type="submit"
              class="rounded-md bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
              disabled={saving}
              data-testid="publish-submit"
            >
              {saving ? 'Publishing…' : 'Publish'}
            </button>
          </div>
        </form>
      {/if}
    </div>
  </div>
{/if}
