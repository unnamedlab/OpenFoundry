<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import {
    dataConnection,
    type CreateEgressPolicyRequest,
    type EgressEndpointKind,
    type EgressPolicyKind,
    type EgressPortKind,
    type NetworkEgressPolicy,
  } from '$lib/api/data-connection';

  let policies = $state<NetworkEgressPolicy[]>([]);
  let loading = $state(true);
  let error = $state('');

  // Wizard ----------------------------------------------------------------
  let wizardOpen = $state(false);
  let wizardStep = $state<1 | 2>(1);
  let wizardError = $state('');
  let wizardBusy = $state(false);

  // Step 1 fields
  let name = $state('');
  let description = $state('');
  let kind = $state<EgressPolicyKind>('direct');
  let addressKind = $state<EgressEndpointKind>('host');
  let addressValue = $state('');
  let portKind = $state<EgressPortKind>('single');
  let portValue = $state('443');
  let isGlobal = $state(false);

  // Step 2 fields
  let permissionsRaw = $state('');

  function resetWizard() {
    wizardStep = 1;
    wizardError = '';
    name = '';
    description = '';
    kind = 'direct';
    addressKind = 'host';
    addressValue = '';
    portKind = 'single';
    portValue = '443';
    isGlobal = false;
    permissionsRaw = '';
  }

  async function load() {
    loading = true;
    error = '';
    try {
      policies = await dataConnection.listEgressPolicies();
    } catch (cause) {
      console.error('listEgressPolicies failed', cause);
      error = cause instanceof Error ? cause.message : 'Failed to load policies';
      policies = [];
    } finally {
      loading = false;
    }
  }

  function goToStep2(event: SubmitEvent) {
    event.preventDefault();
    wizardError = '';
    if (!name.trim()) {
      wizardError = 'Name is required.';
      return;
    }
    if (!addressValue.trim()) {
      wizardError = 'Address value is required.';
      return;
    }
    if (portKind !== 'any' && !portValue.trim()) {
      wizardError = 'Port value is required.';
      return;
    }
    wizardStep = 2;
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    wizardBusy = true;
    wizardError = '';
    const permissions = permissionsRaw
      .split(/[,\n]/)
      .map((p) => p.trim())
      .filter(Boolean);
    const body: CreateEgressPolicyRequest = {
      name: name.trim(),
      description: description.trim(),
      kind,
      address: { kind: addressKind, value: addressValue.trim() },
      port: { kind: portKind, value: portKind === 'any' ? '' : portValue.trim() },
      is_global: isGlobal,
      permissions,
    };
    try {
      await dataConnection.createEgressPolicy(body);
      wizardOpen = false;
      resetWizard();
      await load();
    } catch (cause) {
      console.error('createEgressPolicy failed', cause);
      wizardError = cause instanceof Error ? cause.message : 'Failed to create policy';
    } finally {
      wizardBusy = false;
    }
  }

  async function remove(policy: NetworkEgressPolicy) {
    if (!confirm(`Delete policy "${policy.name}"?`)) return;
    try {
      await dataConnection.deleteEgressPolicy(policy.id);
      await load();
    } catch (cause) {
      console.error('deleteEgressPolicy failed', cause);
      error = cause instanceof Error ? cause.message : 'Failed to delete policy';
    }
  }

  onMount(async () => {
    await load();
    if ($page.url.searchParams.get('create') === '1') {
      wizardOpen = true;
    }
  });
</script>

<svelte:head>
  <title>Egress policies · Data Connection</title>
</svelte:head>

<div class="space-y-6">
  <div class="flex items-start justify-between">
    <div>
      <a href="/data-connection" class="text-xs text-blue-600 hover:underline dark:text-blue-400"
        >← Back to sources</a
      >
      <h1 class="mt-1 text-2xl font-bold">Network egress policies</h1>
      <p class="mt-1 max-w-2xl text-sm text-gray-500">
        Egress policies define how Foundry-worker sources can reach external systems. Each policy
        whitelists a destination (host / IP / CIDR) and a port range, optionally restricted to
        specific markings or groups.
      </p>
    </div>
    <button
      type="button"
      onclick={() => {
        resetWizard();
        wizardOpen = true;
      }}
      class="rounded-xl bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
    >
      + New policy
    </button>
  </div>

  {#if error}
    <div
      class="rounded-xl border border-amber-300 bg-amber-50 p-3 text-xs text-amber-900 dark:border-amber-700 dark:bg-amber-950/30 dark:text-amber-200"
    >
      {error}
    </div>
  {/if}

  {#if loading}
    <p class="text-sm text-gray-500">Loading…</p>
  {:else if policies.length === 0}
    <div
      class="rounded-2xl border border-dashed border-gray-300 p-12 text-center dark:border-gray-700"
    >
      <h2 class="text-lg font-semibold">No policies yet</h2>
      <p class="mx-auto mt-2 max-w-md text-sm text-gray-500">
        Create your first egress policy to allow a source to reach a destination outside Foundry's
        secure network.
      </p>
    </div>
  {:else}
    <div class="overflow-hidden rounded-2xl border border-gray-200 dark:border-gray-800">
      <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-gray-800">
        <thead class="bg-gray-50 dark:bg-gray-900">
          <tr>
            <th class="px-4 py-2 text-left font-medium text-gray-600 dark:text-gray-300">Name</th>
            <th class="px-4 py-2 text-left font-medium text-gray-600 dark:text-gray-300">Kind</th>
            <th class="px-4 py-2 text-left font-medium text-gray-600 dark:text-gray-300"
              >Destination</th
            >
            <th class="px-4 py-2 text-left font-medium text-gray-600 dark:text-gray-300">Scope</th>
            <th class="px-4 py-2"></th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 dark:divide-gray-900">
          {#each policies as policy (policy.id)}
            <tr>
              <td class="px-4 py-2">
                <div class="font-medium">{policy.name}</div>
                <div class="text-xs text-gray-500">{policy.description}</div>
              </td>
              <td class="px-4 py-2 text-xs text-gray-700 dark:text-gray-200">
                {policy.kind === 'direct' ? 'Direct connection' : 'Agent proxy'}
              </td>
              <td class="px-4 py-2 font-mono text-xs">
                {policy.address.kind}:{policy.address.value} :
                {policy.port.kind === 'any' ? 'any' : policy.port.value}
              </td>
              <td class="px-4 py-2 text-xs">
                {policy.is_global ? 'Global' : `${policy.permissions.length} permission(s)`}
              </td>
              <td class="px-4 py-2 text-right">
                <button
                  type="button"
                  onclick={() => remove(policy)}
                  class="text-xs text-rose-600 hover:underline dark:text-rose-400"
                >
                  Delete
                </button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

{#if wizardOpen}
  <div
    class="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4"
    role="dialog"
    aria-modal="true"
  >
    <div
      class="w-full max-w-xl rounded-2xl border border-gray-200 bg-white p-6 shadow-xl dark:border-gray-800 dark:bg-gray-900"
    >
      <header class="mb-4 flex items-center justify-between">
        <h2 class="text-lg font-semibold">
          New egress policy ·
          <span class="text-xs font-normal text-gray-500">step {wizardStep} of 2</span>
        </h2>
        <button
          type="button"
          onclick={() => (wizardOpen = false)}
          class="text-sm text-gray-500 hover:text-gray-800 dark:hover:text-gray-200"
        >
          ✕
        </button>
      </header>

      {#if wizardStep === 1}
        <form onsubmit={goToStep2} class="space-y-3 text-sm">
          <label class="block">
            <span class="text-xs text-gray-500">Name</span>
            <input
              type="text"
              bind:value={name}
              required
              class="mt-1 w-full rounded-xl border border-gray-300 bg-white px-3 py-2 dark:border-gray-700 dark:bg-gray-950"
            />
          </label>
          <label class="block">
            <span class="text-xs text-gray-500">Description</span>
            <textarea
              bind:value={description}
              rows="2"
              class="mt-1 w-full rounded-xl border border-gray-300 bg-white px-3 py-2 dark:border-gray-700 dark:bg-gray-950"
            ></textarea>
          </label>
          <label class="block">
            <span class="text-xs text-gray-500">Policy kind</span>
            <select
              bind:value={kind}
              class="mt-1 w-full rounded-xl border border-gray-300 bg-white px-2 py-2 dark:border-gray-700 dark:bg-gray-950"
            >
              <option value="direct">Direct connection</option>
              <option value="agent_proxy">Agent proxy</option>
            </select>
          </label>
          <div class="grid grid-cols-[140px_1fr] gap-3">
            <label class="block">
              <span class="text-xs text-gray-500">Address kind</span>
              <select
                bind:value={addressKind}
                class="mt-1 w-full rounded-xl border border-gray-300 bg-white px-2 py-2 dark:border-gray-700 dark:bg-gray-950"
              >
                <option value="host">Hostname</option>
                <option value="ip">IP</option>
                <option value="cidr">CIDR</option>
              </select>
            </label>
            <label class="block">
              <span class="text-xs text-gray-500">Address</span>
              <input
                type="text"
                bind:value={addressValue}
                placeholder={addressKind === 'cidr' ? '10.0.0.0/24' : 's3.amazonaws.com'}
                required
                class="mt-1 w-full rounded-xl border border-gray-300 bg-white px-3 py-2 dark:border-gray-700 dark:bg-gray-950"
              />
            </label>
          </div>
          <div class="grid grid-cols-[140px_1fr] gap-3">
            <label class="block">
              <span class="text-xs text-gray-500">Port kind</span>
              <select
                bind:value={portKind}
                class="mt-1 w-full rounded-xl border border-gray-300 bg-white px-2 py-2 dark:border-gray-700 dark:bg-gray-950"
              >
                <option value="single">Single</option>
                <option value="range">Range</option>
                <option value="any">Any</option>
              </select>
            </label>
            <label class="block">
              <span class="text-xs text-gray-500">Port</span>
              <input
                type="text"
                bind:value={portValue}
                disabled={portKind === 'any'}
                placeholder={portKind === 'range' ? '8000-9000' : '443'}
                class="mt-1 w-full rounded-xl border border-gray-300 bg-white px-3 py-2 disabled:opacity-60 dark:border-gray-700 dark:bg-gray-950"
              />
            </label>
          </div>
          <label class="flex items-center gap-2 text-xs text-gray-700 dark:text-gray-200">
            <input type="checkbox" bind:checked={isGlobal} />
            Make this policy global (any source can attach without explicit permission).
          </label>

          {#if wizardError}
            <p class="text-xs text-rose-600 dark:text-rose-400">{wizardError}</p>
          {/if}

          <footer class="mt-4 flex justify-end gap-2">
            <button
              type="button"
              onclick={() => (wizardOpen = false)}
              class="rounded-xl border border-gray-300 px-3 py-2 text-sm hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-800"
            >
              Cancel
            </button>
            <button
              type="submit"
              class="rounded-xl bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
            >
              Continue
            </button>
          </footer>
        </form>
      {:else}
        <form onsubmit={submit} class="space-y-3 text-sm">
          <p class="text-xs text-gray-500">
            Pick the markings or groups that are allowed to attach this policy. Leave empty to keep
            it restricted to administrators. (Permission resolution is delegated to
            <code>authorization-policy-service</code>.)
          </p>
          <label class="block">
            <span class="text-xs text-gray-500"
              >Markings / groups (comma- or newline-separated identifiers)</span
            >
            <textarea
              bind:value={permissionsRaw}
              rows="4"
              placeholder="marking:secret&#10;group:data-engineering"
              class="mt-1 w-full rounded-xl border border-gray-300 bg-white px-3 py-2 font-mono text-xs dark:border-gray-700 dark:bg-gray-950"
            ></textarea>
          </label>

          <details
            class="rounded-xl border border-gray-200 px-3 py-2 text-xs text-gray-600 dark:border-gray-800 dark:text-gray-300"
          >
            <summary class="cursor-pointer font-medium">Review</summary>
            <pre class="mt-2 whitespace-pre-wrap break-all text-[11px]">{JSON.stringify(
                {
                  name,
                  description,
                  kind,
                  address: { kind: addressKind, value: addressValue },
                  port: { kind: portKind, value: portKind === 'any' ? '' : portValue },
                  is_global: isGlobal,
                  permissions: permissionsRaw
                    .split(/[,\n]/)
                    .map((p) => p.trim())
                    .filter(Boolean),
                },
                null,
                2,
              )}</pre>
          </details>

          {#if wizardError}
            <p class="text-xs text-rose-600 dark:text-rose-400">{wizardError}</p>
          {/if}

          <footer class="mt-4 flex justify-between gap-2">
            <button
              type="button"
              onclick={() => (wizardStep = 1)}
              class="rounded-xl border border-gray-300 px-3 py-2 text-sm hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-800"
            >
              ← Back
            </button>
            <button
              type="submit"
              disabled={wizardBusy}
              class="rounded-xl bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-60"
            >
              {wizardBusy ? 'Creating…' : 'Create policy'}
            </button>
          </footer>
        </form>
      {/if}
    </div>
  </div>
{/if}
