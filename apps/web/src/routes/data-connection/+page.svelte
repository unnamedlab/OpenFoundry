<script lang="ts">
  import { onMount } from 'svelte';
  import {
    dataConnection,
    type Source,
    type SourceStatus,
  } from '$lib/api/data-connection';

  let sources = $state<Source[]>([]);
  let total = $state(0);
  let loading = $state(true);
  let error = $state('');

  function statusTone(status: SourceStatus) {
    switch (status) {
      case 'healthy':
        return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300';
      case 'degraded':
        return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300';
      case 'error':
        return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300';
      case 'configuring':
        return 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300';
      case 'draft':
      default:
        return 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300';
    }
  }

  function formatDate(value: string | null) {
    if (!value) return '—';
    try {
      return new Date(value).toLocaleString();
    } catch {
      return value;
    }
  }

  async function load() {
    loading = true;
    error = '';
    try {
      const response = await dataConnection.listSources({ page: 1, per_page: 50 });
      sources = response.data;
      total = response.total;
    } catch (cause) {
      console.error('Failed to load sources', cause);
      error = cause instanceof Error ? cause.message : 'Failed to load sources';
      sources = [];
      total = 0;
    } finally {
      loading = false;
    }
  }

  onMount(load);
</script>

<svelte:head>
  <title>Data Connection</title>
</svelte:head>

<div class="space-y-6">
  <div class="flex items-start justify-between">
    <div>
      <h1 class="text-2xl font-bold">Data Connection</h1>
      <p class="mt-1 max-w-2xl text-sm text-gray-500">
        Synchronise data from external systems into Foundry. Create a source to connect to a
        database, blob store, or REST API and configure batch syncs into datasets.
      </p>
    </div>
    <div class="flex items-center gap-2">
      <a
        href="/data-connection/egress-policies"
        class="rounded-xl border border-gray-300 px-3 py-2 text-sm hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-800"
      >
        Egress policies
      </a>
      <a
        href="/data-connection/agents"
        class="rounded-xl border border-gray-300 px-3 py-2 text-sm hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-800"
      >
        Agents
      </a>
      <a
        href="/data-connection/new"
        class="rounded-xl bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
      >
        + New source
      </a>
    </div>
  </div>

  {#if error}
    <div
      class="rounded-xl border border-amber-300 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-700 dark:bg-amber-950/30 dark:text-amber-200"
    >
      <p class="font-medium">Backend not reachable</p>
      <p class="mt-1">{error}</p>
      <p class="mt-2 text-xs opacity-75">
        The Data Connection REST surface
        (<code>/api/v1/data-connection/sources</code>) is not yet wired to the
        connector-management-service binary; this page renders the empty state until the backend is
        live.
      </p>
    </div>
  {/if}

  {#if loading}
    <p class="text-sm text-gray-500">Loading…</p>
  {:else if sources.length === 0}
    <div
      class="rounded-2xl border border-dashed border-gray-300 p-12 text-center dark:border-gray-700"
    >
      <h2 class="text-lg font-semibold">No sources yet</h2>
      <p class="mx-auto mt-2 max-w-md text-sm text-gray-500">
        A source represents a single connection to an external system. Pick a connector to get
        started.
      </p>
      <a
        href="/data-connection/new"
        class="mt-4 inline-block rounded-xl bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
      >
        + New source
      </a>
    </div>
  {:else}
    <div class="overflow-hidden rounded-2xl border border-gray-200 dark:border-gray-800">
      <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-gray-800">
        <thead class="bg-gray-50 dark:bg-gray-900">
          <tr>
            <th class="px-4 py-2 text-left font-medium text-gray-600 dark:text-gray-300">Name</th>
            <th class="px-4 py-2 text-left font-medium text-gray-600 dark:text-gray-300"
              >Connector</th
            >
            <th class="px-4 py-2 text-left font-medium text-gray-600 dark:text-gray-300">Worker</th>
            <th class="px-4 py-2 text-left font-medium text-gray-600 dark:text-gray-300">Status</th>
            <th class="px-4 py-2 text-left font-medium text-gray-600 dark:text-gray-300"
              >Last sync</th
            >
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 dark:divide-gray-900">
          {#each sources as source (source.id)}
            <tr class="hover:bg-gray-50 dark:hover:bg-gray-900">
              <td class="px-4 py-2">
                <a
                  href={`/data-connection/sources/${source.id}`}
                  class="font-medium text-blue-600 hover:underline dark:text-blue-400"
                >
                  {source.name}
                </a>
              </td>
              <td class="px-4 py-2 font-mono text-xs text-gray-600 dark:text-gray-300"
                >{source.connector_type}</td
              >
              <td class="px-4 py-2 text-gray-700 dark:text-gray-200">
                {source.worker === 'foundry' ? 'Foundry worker' : 'Agent worker'}
              </td>
              <td class="px-4 py-2">
                <span
                  class={`inline-block rounded-full px-2 py-0.5 text-xs font-medium ${statusTone(source.status)}`}
                >
                  {source.status}
                </span>
              </td>
              <td class="px-4 py-2 text-gray-500">{formatDate(source.last_sync_at)}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    <p class="text-xs text-gray-500">{total} source{total === 1 ? '' : 's'} total</p>
  {/if}
</div>
