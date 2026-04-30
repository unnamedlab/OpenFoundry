<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import {
    capabilityLabel,
    dataConnection,
    FALLBACK_CONNECTOR_CATALOG,
    filterCatalog,
    type ConnectorCatalogEntry,
  } from '$lib/api/data-connection';

  let catalog = $state<ConnectorCatalogEntry[]>(FALLBACK_CONNECTOR_CATALOG);
  let query = $state('');
  let loading = $state(true);
  let creating = $state(false);
  let error = $state('');
  let createError = $state('');

  let selected = $state<ConnectorCatalogEntry | null>(null);
  let nameInput = $state('');

  const filtered = $derived(filterCatalog(catalog, query));

  async function load() {
    loading = true;
    error = '';
    try {
      const response = await dataConnection.getCatalog();
      // The backend may return a stricter list; fall back to the static one if empty.
      catalog = response.connectors.length > 0 ? response.connectors : FALLBACK_CONNECTOR_CATALOG;
    } catch (cause) {
      console.warn('Catalog fetch failed, using fallback', cause);
      catalog = FALLBACK_CONNECTOR_CATALOG;
      error =
        cause instanceof Error
          ? `Could not load /catalog (${cause.message}). Showing the local fallback list.`
          : 'Could not load /catalog. Showing the local fallback list.';
    } finally {
      loading = false;
    }
  }

  function pick(entry: ConnectorCatalogEntry) {
    if (!entry.available) return;
    selected = entry;
    nameInput = `${entry.name} source`;
    createError = '';
  }

  async function createSource(event: SubmitEvent) {
    event.preventDefault();
    if (!selected) return;
    if (!nameInput.trim()) {
      createError = 'A source name is required.';
      return;
    }
    creating = true;
    createError = '';
    try {
      const source = await dataConnection.createSource({
        name: nameInput.trim(),
        connector_type: selected.type,
        worker: 'foundry',
      });
      await goto(`/data-connection/sources/${source.id}`);
    } catch (cause) {
      console.error('createSource failed', cause);
      createError = cause instanceof Error ? cause.message : 'Failed to create source';
    } finally {
      creating = false;
    }
  }

  onMount(load);
</script>

<svelte:head>
  <title>New source · Data Connection</title>
</svelte:head>

<div class="space-y-6">
  <div class="flex items-start justify-between gap-4">
    <div>
      <a href="/data-connection" class="text-xs text-blue-600 hover:underline dark:text-blue-400"
        >← Back to sources</a
      >
      <h1 class="mt-1 text-2xl font-bold">Select a connector</h1>
      <p class="mt-1 max-w-2xl text-sm text-gray-500">
        Pick the system you want to connect to. Search by name or by capability tag (e.g. "virtual"
        to find connectors that support virtual tables).
      </p>
    </div>
    <input
      type="search"
      bind:value={query}
      placeholder="Search connectors or capabilities…"
      class="w-72 rounded-xl border border-gray-300 bg-white px-3 py-2 text-sm focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-900"
    />
  </div>

  {#if error}
    <div
      class="rounded-xl border border-amber-300 bg-amber-50 p-3 text-xs text-amber-900 dark:border-amber-700 dark:bg-amber-950/30 dark:text-amber-200"
    >
      {error}
    </div>
  {/if}

  {#if loading}
    <p class="text-sm text-gray-500">Loading catalog…</p>
  {:else if filtered.length === 0}
    <p class="text-sm text-gray-500">No connectors match this search.</p>
  {:else}
    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {#each filtered as entry (entry.type)}
        <button
          type="button"
          onclick={() => pick(entry)}
          disabled={!entry.available}
          class={`flex flex-col gap-2 rounded-2xl border p-4 text-left transition ${
            entry.available
              ? 'border-gray-200 hover:border-blue-500 hover:bg-blue-50/40 dark:border-gray-800 dark:hover:bg-blue-950/20'
              : 'cursor-not-allowed border-gray-200 bg-gray-50 opacity-60 dark:border-gray-800 dark:bg-gray-900'
          } ${selected?.type === entry.type ? 'ring-2 ring-blue-500' : ''}`}
        >
          <div class="flex items-center justify-between">
            <span class="font-semibold">{entry.name}</span>
            {#if !entry.available}
              <span
                class="rounded-full bg-gray-200 px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide text-gray-700 dark:bg-gray-800 dark:text-gray-300"
              >
                Coming soon
              </span>
            {/if}
          </div>
          <p class="text-xs text-gray-500">{entry.description}</p>
          <div class="mt-auto flex flex-wrap gap-1">
            {#each entry.capabilities as cap (cap)}
              <span
                class="rounded-full bg-gray-100 px-2 py-0.5 text-[10px] text-gray-600 dark:bg-gray-800 dark:text-gray-300"
              >
                {capabilityLabel(cap)}
              </span>
            {/each}
          </div>
        </button>
      {/each}
    </div>
  {/if}

  {#if selected}
    <form
      onsubmit={createSource}
      class="rounded-2xl border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-800 dark:bg-gray-900"
    >
      <h2 class="text-lg font-semibold">Create {selected.name} source</h2>
      <p class="mt-1 text-xs text-gray-500">
        The source runs on the Foundry worker by default. You'll be able to add credentials and
        attach an egress policy after creation.
      </p>

      <label class="mt-4 block">
        <span class="text-xs font-medium text-gray-600 dark:text-gray-300">Source name</span>
        <input
          type="text"
          bind:value={nameInput}
          required
          minlength="1"
          maxlength="120"
          class="mt-1 w-full rounded-xl border border-gray-300 bg-white px-3 py-2 text-sm focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-950"
        />
      </label>

      {#if createError}
        <p class="mt-3 text-xs text-rose-600 dark:text-rose-400">{createError}</p>
      {/if}

      <div class="mt-4 flex items-center justify-end gap-2">
        <button
          type="button"
          onclick={() => (selected = null)}
          class="rounded-xl border border-gray-300 px-3 py-2 text-sm hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-800"
        >
          Cancel
        </button>
        <button
          type="submit"
          disabled={creating}
          class="rounded-xl bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-60"
        >
          {creating ? 'Creating…' : 'Create source'}
        </button>
      </div>
    </form>
  {/if}
</div>
