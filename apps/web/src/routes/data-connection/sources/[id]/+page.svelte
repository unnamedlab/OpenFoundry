<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import ConfirmDialog from '$components/workspace/ConfirmDialog.svelte';
  import {
    capabilityLabel,
    dataConnection,
    FALLBACK_CONNECTOR_CATALOG,
    type BatchSyncDef,
    type ConnectorCatalogEntry,
    type Credential,
    type CredentialKind,
    type NetworkEgressPolicy,
    type Source,
    type SourceWorker,
    type SyncRun,
    type TestConnectionResult,
  } from '$lib/api/data-connection';

  type Tab = 'overview' | 'networking' | 'credentials' | 'capabilities' | 'runs';

  const sourceId = $derived(($page.params.id ?? '') as string);

  let activeTab = $state<Tab>('overview');
  let source = $state<Source | null>(null);
  let loading = $state(true);
  let loadError = $state('');

  // Networking
  let attachedPolicies = $state<NetworkEgressPolicy[]>([]);
  let availablePolicies = $state<NetworkEgressPolicy[]>([]);
  let policiesError = $state('');
  let attachingPolicyId = $state<string | null>(null);
  let showAttachPicker = $state(false);

  // Credentials
  let credentials = $state<Credential[]>([]);
  let credentialKind = $state<CredentialKind>('api_key');
  let credentialValue = $state('');
  let credentialBusy = $state(false);
  let credentialError = $state('');

  // Capabilities (resolved from the catalog by connector_type)
  const catalogEntry = $derived<ConnectorCatalogEntry | undefined>(
    source
      ? FALLBACK_CONNECTOR_CATALOG.find((entry) => entry.type === source!.connector_type)
      : undefined,
  );

  // Test connection
  let testing = $state(false);
  let testResult = $state<TestConnectionResult | null>(null);

  // Runs
  let syncs = $state<BatchSyncDef[]>([]);
  let runsBySync = $state<Record<string, SyncRun[]>>({});
  let runsError = $state('');
  let createSyncOpen = $state(false);
  let newOutputDataset = $state('');
  let newFileGlob = $state('');
  let createSyncBusy = $state(false);
  let createSyncError = $state('');
  let runningSyncId = $state<string | null>(null);
  let deleteConfirm = $state<{ busy: boolean } | null>(null);

  async function loadSource() {
    loading = true;
    loadError = '';
    try {
      source = await dataConnection.getSource(sourceId);
    } catch (cause) {
      console.error('getSource failed', cause);
      loadError = cause instanceof Error ? cause.message : 'Failed to load source';
    } finally {
      loading = false;
    }
  }

  async function loadPolicies() {
    policiesError = '';
    try {
      const [attached, all] = await Promise.all([
        dataConnection.listSourcePolicies(sourceId).catch(() => []),
        dataConnection.listEgressPolicies().catch(() => []),
      ]);
      attachedPolicies = attached;
      const attachedIds = new Set(attached.map((p) => p.id));
      availablePolicies = all.filter((p) => !attachedIds.has(p.id));
    } catch (cause) {
      console.error('loadPolicies failed', cause);
      policiesError = cause instanceof Error ? cause.message : 'Failed to load egress policies';
    }
  }

  async function loadCredentials() {
    try {
      credentials = await dataConnection.listCredentials(sourceId);
    } catch (cause) {
      console.warn('listCredentials failed', cause);
      credentials = [];
    }
  }

  async function loadRuns() {
    runsError = '';
    try {
      syncs = await dataConnection.listSyncs(sourceId);
      const entries = await Promise.all(
        syncs.map(async (sync) => {
          try {
            return [sync.id, await dataConnection.listRuns(sync.id)] as const;
          } catch {
            return [sync.id, [] as SyncRun[]] as const;
          }
        }),
      );
      runsBySync = Object.fromEntries(entries);
    } catch (cause) {
      console.warn('loadRuns failed', cause);
      runsError = cause instanceof Error ? cause.message : 'Failed to load runs';
      syncs = [];
      runsBySync = {};
    }
  }

  async function attachPolicy(policy: NetworkEgressPolicy) {
    attachingPolicyId = policy.id;
    try {
      await dataConnection.attachPolicy(sourceId, policy.id, policy.kind);
      showAttachPicker = false;
      await loadPolicies();
    } catch (cause) {
      console.error('attachPolicy failed', cause);
      policiesError = cause instanceof Error ? cause.message : 'Failed to attach policy';
    } finally {
      attachingPolicyId = null;
    }
  }

  async function detachPolicy(policy: NetworkEgressPolicy) {
    try {
      await dataConnection.detachPolicy(sourceId, policy.id);
      await loadPolicies();
    } catch (cause) {
      console.error('detachPolicy failed', cause);
      policiesError = cause instanceof Error ? cause.message : 'Failed to detach policy';
    }
  }

  async function switchWorker(worker: SourceWorker) {
    if (!source) return;
    try {
      source = await dataConnection.updateSource(sourceId, { worker });
    } catch (cause) {
      console.error('switchWorker failed', cause);
      loadError = cause instanceof Error ? cause.message : 'Failed to update worker';
    }
  }

  async function saveCredential(event: SubmitEvent) {
    event.preventDefault();
    if (!credentialValue) {
      credentialError = 'A secret value is required.';
      return;
    }
    credentialBusy = true;
    credentialError = '';
    try {
      await dataConnection.setCredential(sourceId, {
        kind: credentialKind,
        value: credentialValue,
      });
      credentialValue = '';
      await loadCredentials();
    } catch (cause) {
      console.error('setCredential failed', cause);
      credentialError = cause instanceof Error ? cause.message : 'Failed to save credential';
    } finally {
      credentialBusy = false;
    }
  }

  async function runTest() {
    testing = true;
    testResult = null;
    try {
      testResult = await dataConnection.testConnection(sourceId);
    } catch (cause) {
      testResult = {
        success: false,
        message: cause instanceof Error ? cause.message : 'Test connection failed',
        latency_ms: null,
      };
    } finally {
      testing = false;
    }
  }

  async function createSync(event: SubmitEvent) {
    event.preventDefault();
    if (!newOutputDataset.trim()) {
      createSyncError = 'An output dataset id is required.';
      return;
    }
    createSyncBusy = true;
    createSyncError = '';
    try {
      await dataConnection.createSync({
        source_id: sourceId,
        output_dataset_id: newOutputDataset.trim(),
        file_glob: newFileGlob.trim() || undefined,
      });
      newOutputDataset = '';
      newFileGlob = '';
      createSyncOpen = false;
      await loadRuns();
    } catch (cause) {
      console.error('createSync failed', cause);
      createSyncError = cause instanceof Error ? cause.message : 'Failed to create sync';
    } finally {
      createSyncBusy = false;
    }
  }

  async function runSync(syncId: string) {
    runningSyncId = syncId;
    try {
      await dataConnection.runSync(syncId);
      await loadRuns();
    } catch (cause) {
      console.error('runSync failed', cause);
      runsError = cause instanceof Error ? cause.message : 'Failed to start sync';
    } finally {
      runningSyncId = null;
    }
  }

  async function deleteSource() {
    deleteConfirm = { busy: false };
  }

  async function confirmDelete() {
    if (!deleteConfirm) return;
    deleteConfirm = { busy: true };
    try {
      await dataConnection.deleteSource(sourceId);
      await goto('/data-connection');
    } catch (cause) {
      console.error('deleteSource failed', cause);
      loadError = cause instanceof Error ? cause.message : 'Failed to delete source';
    } finally {
      deleteConfirm = null;
    }
  }

  function selectTab(tab: Tab) {
    activeTab = tab;
    if (tab === 'networking' && attachedPolicies.length === 0 && availablePolicies.length === 0)
      void loadPolicies();
    if (tab === 'credentials' && credentials.length === 0) void loadCredentials();
    if (tab === 'runs' && syncs.length === 0) void loadRuns();
  }

  onMount(async () => {
    await loadSource();
    // Eagerly hydrate the side-tabs we know we'll need shortly so users see
    // counts in tab labels without an extra click.
    void loadPolicies();
    void loadCredentials();
    void loadRuns();
  });
</script>

<svelte:head>
  <title>{source?.name ?? 'Source'} · Data Connection</title>
</svelte:head>

<div class="space-y-6">
  <a href="/data-connection" class="text-xs text-blue-600 hover:underline dark:text-blue-400"
    >← Back to sources</a
  >

  {#if loading}
    <p class="text-sm text-gray-500">Loading source…</p>
  {:else if loadError && !source}
    <div
      class="rounded-xl border border-rose-300 bg-rose-50 p-4 text-sm text-rose-900 dark:border-rose-700 dark:bg-rose-950/30 dark:text-rose-200"
    >
      {loadError}
    </div>
  {:else if source}
    <header class="flex items-start justify-between gap-4">
      <div>
        <h1 class="text-2xl font-bold">{source.name}</h1>
        <p class="mt-1 text-xs text-gray-500">
          <span class="font-mono">{source.connector_type}</span> ·
          {source.worker === 'foundry' ? 'Foundry worker' : 'Agent worker'} ·
          status <span class="font-medium">{source.status}</span>
        </p>
      </div>
      <div class="flex gap-2">
        <button
          type="button"
          onclick={runTest}
          disabled={testing}
          class="rounded-xl border border-gray-300 px-3 py-2 text-sm hover:bg-gray-50 disabled:opacity-60 dark:border-gray-700 dark:hover:bg-gray-800"
        >
          {testing ? 'Testing…' : 'Test connection'}
        </button>
        <button
          type="button"
          onclick={deleteSource}
          class="rounded-xl border border-rose-300 px-3 py-2 text-sm text-rose-700 hover:bg-rose-50 dark:border-rose-800 dark:text-rose-300 dark:hover:bg-rose-950/40"
        >
          Delete
        </button>
      </div>
    </header>

    {#if testResult}
      <div
        class={`rounded-xl p-3 text-sm ${
          testResult.success
            ? 'border border-emerald-300 bg-emerald-50 text-emerald-900 dark:border-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-200'
            : 'border border-rose-300 bg-rose-50 text-rose-900 dark:border-rose-700 dark:bg-rose-950/30 dark:text-rose-200'
        }`}
      >
        <p class="font-medium">{testResult.success ? 'Connection succeeded' : 'Connection failed'}</p>
        <p class="mt-1 text-xs opacity-90">{testResult.message}</p>
        {#if testResult.latency_ms !== null}
          <p class="mt-1 text-xs opacity-75">Latency: {testResult.latency_ms} ms</p>
        {/if}
      </div>
    {/if}

    <nav class="flex gap-1 border-b border-gray-200 dark:border-gray-800">
      {#each ['overview', 'networking', 'credentials', 'capabilities', 'runs'] as Tab[] as tab (tab)}
        <button
          type="button"
          onclick={() => selectTab(tab)}
          class={`border-b-2 px-3 py-2 text-sm capitalize ${
            activeTab === tab
              ? 'border-blue-600 font-medium text-blue-700 dark:border-blue-400 dark:text-blue-300'
              : 'border-transparent text-gray-500 hover:text-gray-800 dark:hover:text-gray-200'
          }`}
        >
          {tab}
          {#if tab === 'networking'}
            <span class="ml-1 rounded-full bg-gray-100 px-1.5 text-[10px] dark:bg-gray-800"
              >{attachedPolicies.length}</span
            >
          {:else if tab === 'credentials'}
            <span class="ml-1 rounded-full bg-gray-100 px-1.5 text-[10px] dark:bg-gray-800"
              >{credentials.length}</span
            >
          {:else if tab === 'runs'}
            <span class="ml-1 rounded-full bg-gray-100 px-1.5 text-[10px] dark:bg-gray-800"
              >{syncs.length}</span
            >
          {/if}
        </button>
      {/each}
    </nav>

    {#if activeTab === 'overview'}
      <section class="grid grid-cols-1 gap-4 md:grid-cols-2">
        <div class="rounded-2xl border border-gray-200 p-4 text-sm dark:border-gray-800">
          <h2 class="text-base font-semibold">Identity</h2>
          <dl class="mt-3 grid grid-cols-[120px_1fr] gap-y-2 text-xs">
            <dt class="text-gray-500">Name</dt>
            <dd>{source.name}</dd>
            <dt class="text-gray-500">Connector</dt>
            <dd class="font-mono">{source.connector_type}</dd>
            <dt class="text-gray-500">Worker</dt>
            <dd>{source.worker === 'foundry' ? 'Foundry worker' : 'Agent worker'}</dd>
            <dt class="text-gray-500">Created</dt>
            <dd>{new Date(source.created_at).toLocaleString()}</dd>
            <dt class="text-gray-500">Last sync</dt>
            <dd>{source.last_sync_at ? new Date(source.last_sync_at).toLocaleString() : '—'}</dd>
          </dl>
        </div>
        <div class="rounded-2xl border border-gray-200 p-4 text-sm dark:border-gray-800">
          <h2 class="text-base font-semibold">At a glance</h2>
          <ul class="mt-3 space-y-2 text-xs">
            <li>
              <span class="text-gray-500">Egress policies attached:</span>
              <span class="font-medium">{attachedPolicies.length}</span>
            </li>
            <li>
              <span class="text-gray-500">Stored credentials:</span>
              <span class="font-medium">{credentials.length}</span>
            </li>
            <li>
              <span class="text-gray-500">Batch sync definitions:</span>
              <span class="font-medium">{syncs.length}</span>
            </li>
          </ul>
        </div>
      </section>
    {:else if activeTab === 'networking'}
      <!--
        Reproduces the empty-state captured in screenshot bf55df40 when no
        policies are attached, and switches to a list view once at least one
        policy is bound to the source. Both modes coexist (direct + agent_proxy
        per the Foundry Data Connection core concepts page).
      -->
      <section
        class="rounded-2xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900"
      >
        <header
          class="flex items-center justify-between border-b border-gray-200 px-5 py-3 dark:border-gray-800"
        >
          <h2 class="text-base font-semibold">Network Connectivity</h2>
          {#if source.worker === 'foundry'}
            <button
              type="button"
              onclick={() => switchWorker('agent')}
              class="text-xs text-blue-600 hover:underline dark:text-blue-400"
            >
              ⇄ Switch to connect via Agent
            </button>
          {:else}
            <button
              type="button"
              onclick={() => switchWorker('foundry')}
              class="text-xs text-blue-600 hover:underline dark:text-blue-400"
            >
              ⇄ Switch to connect via Foundry worker
            </button>
          {/if}
        </header>

        {#if source.worker === 'agent'}
          <div class="space-y-2 px-5 py-8 text-sm">
            <p class="font-medium">Agent worker networking</p>
            <p class="text-xs text-gray-500">
              For agent worker sources, networking is configured directly on the agent host's
              firewall and proxy settings — there are no egress policies to manage from Foundry.
            </p>
            <p class="text-xs text-gray-500">
              Agent worker is in the legacy phase; new sources should prefer Foundry worker.
            </p>
          </div>
        {:else if policiesError}
          <p class="px-5 py-4 text-xs text-rose-600 dark:text-rose-400">{policiesError}</p>
        {:else if attachedPolicies.length === 0}
          <div class="flex flex-col items-center gap-3 px-5 py-12 text-center">
            <div
              class="flex h-12 w-12 items-center justify-center rounded-full bg-gray-100 text-gray-500 dark:bg-gray-800"
            >
              <!-- Cloud glyph mirroring the screenshot empty-state icon. -->
              <svg
                width="24"
                height="24"
                viewBox="0 0 24 24"
                fill="none"
                xmlns="http://www.w3.org/2000/svg"
                aria-hidden="true"
              >
                <path
                  d="M7 18a4 4 0 0 1-.7-7.94 5 5 0 0 1 9.7-.86 4.5 4.5 0 0 1 .5 8.8H7z"
                  stroke="currentColor"
                  stroke-width="1.6"
                  stroke-linejoin="round"
                />
              </svg>
            </div>
            <h3 class="text-base font-semibold">Select a policy</h3>
            <p class="max-w-md text-xs text-gray-500">
              A network egress policy is required to connect to sources outside Foundry's secure
              network. Select an existing policy if one already exists, otherwise create a new
              policy.
            </p>
            <a
              href="https://www.palantir.com/docs/foundry/data-connection/core-concepts/"
              target="_blank"
              rel="noopener noreferrer"
              class="text-xs text-blue-600 hover:underline dark:text-blue-400"
            >
              Learn more
            </a>
            <div class="mt-2 flex items-center gap-2">
              <button
                type="button"
                onclick={() => {
                  showAttachPicker = true;
                  void loadPolicies();
                }}
                class="rounded-xl border border-gray-300 px-3 py-2 text-xs hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-800"
              >
                + Use existing policy
              </button>
              <a
                href="/data-connection/egress-policies?create=1"
                class="rounded-xl bg-blue-600 px-3 py-2 text-xs font-medium text-white hover:bg-blue-700"
              >
                🌑 Create new policy
              </a>
            </div>
          </div>
        {:else}
          <ul class="divide-y divide-gray-100 dark:divide-gray-800">
            {#each attachedPolicies as policy (policy.id)}
              <li class="flex items-start justify-between gap-4 px-5 py-3 text-sm">
                <div>
                  <p class="font-medium">{policy.name}</p>
                  <p class="text-xs text-gray-500">{policy.description}</p>
                  <p class="mt-1 text-[11px] text-gray-500">
                    {policy.kind === 'direct' ? 'Direct connection' : 'Agent proxy'} · {policy
                      .address.kind}:{policy.address.value} · port {policy.port.kind === 'any'
                      ? 'any'
                      : policy.port.value}
                    {policy.is_global ? '· global' : ''}
                  </p>
                </div>
                <button
                  type="button"
                  onclick={() => detachPolicy(policy)}
                  class="text-xs text-rose-600 hover:underline dark:text-rose-400"
                >
                  Detach
                </button>
              </li>
            {/each}
          </ul>
          <footer
            class="flex items-center justify-end gap-2 border-t border-gray-200 px-5 py-3 dark:border-gray-800"
          >
            <button
              type="button"
              onclick={() => {
                showAttachPicker = true;
                void loadPolicies();
              }}
              class="text-xs text-blue-600 hover:underline dark:text-blue-400"
            >
              + Add another policy
            </button>
          </footer>
        {/if}

        {#if showAttachPicker && source.worker === 'foundry'}
          <div
            class="border-t border-gray-200 bg-gray-50 px-5 py-4 dark:border-gray-800 dark:bg-gray-950"
          >
            <div class="flex items-center justify-between">
              <h3 class="text-sm font-medium">Attach an existing policy</h3>
              <button
                type="button"
                onclick={() => (showAttachPicker = false)}
                class="text-xs text-gray-500 hover:text-gray-800 dark:hover:text-gray-200"
              >
                Close
              </button>
            </div>
            {#if availablePolicies.length === 0}
              <p class="mt-2 text-xs text-gray-500">
                No unattached policies available. <a
                  href="/data-connection/egress-policies?create=1"
                  class="text-blue-600 hover:underline dark:text-blue-400">Create one</a
                >.
              </p>
            {:else}
              <ul class="mt-3 space-y-2">
                {#each availablePolicies as policy (policy.id)}
                  <li
                    class="flex items-center justify-between rounded-xl border border-gray-200 bg-white px-3 py-2 text-xs dark:border-gray-800 dark:bg-gray-900"
                  >
                    <div>
                      <p class="font-medium">{policy.name}</p>
                      <p class="text-gray-500">
                        {policy.kind} · {policy.address.value}:{policy.port.value || '-'}
                      </p>
                    </div>
                    <button
                      type="button"
                      onclick={() => attachPolicy(policy)}
                      disabled={attachingPolicyId === policy.id}
                      class="rounded-lg border border-gray-300 px-2 py-1 hover:bg-gray-100 disabled:opacity-60 dark:border-gray-700 dark:hover:bg-gray-800"
                    >
                      {attachingPolicyId === policy.id ? 'Attaching…' : 'Attach'}
                    </button>
                  </li>
                {/each}
              </ul>
            {/if}
          </div>
        {/if}
      </section>
    {:else if activeTab === 'credentials'}
      <section class="space-y-4">
        <div class="rounded-2xl border border-gray-200 p-4 text-sm dark:border-gray-800">
          <h2 class="text-base font-semibold">Stored credentials</h2>
          {#if credentials.length === 0}
            <p class="mt-2 text-xs text-gray-500">
              No credentials stored. Some sources can authenticate without a stored secret (e.g.
              cloud identity, OpenID connect).
            </p>
          {:else}
            <ul class="mt-3 divide-y divide-gray-100 text-xs dark:divide-gray-800">
              {#each credentials as cred (cred.id)}
                <li class="flex items-center justify-between py-2">
                  <span>
                    <span class="font-medium">{cred.kind}</span>
                    <span class="ml-2 font-mono text-gray-500">{cred.fingerprint}</span>
                  </span>
                  <span class="text-gray-500">{new Date(cred.created_at).toLocaleString()}</span>
                </li>
              {/each}
            </ul>
          {/if}
        </div>

        <form
          onsubmit={saveCredential}
          class="rounded-2xl border border-gray-200 p-4 text-sm dark:border-gray-800"
        >
          <h3 class="text-sm font-medium">Add a credential</h3>
          <p class="mt-1 text-xs text-gray-500">
            Secrets are encrypted at rest by the platform. The raw value is never returned.
          </p>
          <div class="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-[200px_1fr_auto]">
            <label class="block">
              <span class="text-xs text-gray-500">Kind</span>
              <select
                bind:value={credentialKind}
                class="mt-1 w-full rounded-xl border border-gray-300 bg-white px-2 py-2 text-sm dark:border-gray-700 dark:bg-gray-900"
              >
                <option value="api_key">API key</option>
                <option value="password">Password</option>
                <option value="oauth_token">OAuth token</option>
                <option value="aws_keys">AWS access key</option>
                <option value="service_account_json">Service account JSON</option>
              </select>
            </label>
            <label class="block">
              <span class="text-xs text-gray-500">Secret value</span>
              <input
                type="password"
                bind:value={credentialValue}
                autocomplete="off"
                class="mt-1 w-full rounded-xl border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-900"
              />
            </label>
            <button
              type="submit"
              disabled={credentialBusy}
              class="self-end rounded-xl bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-60"
            >
              {credentialBusy ? 'Saving…' : 'Save'}
            </button>
          </div>
          {#if credentialError}
            <p class="mt-2 text-xs text-rose-600 dark:text-rose-400">{credentialError}</p>
          {/if}
        </form>
      </section>
    {:else if activeTab === 'capabilities'}
      <section class="rounded-2xl border border-gray-200 p-4 text-sm dark:border-gray-800">
        <h2 class="text-base font-semibold">Capabilities</h2>
        {#if catalogEntry}
          <p class="mt-1 text-xs text-gray-500">{catalogEntry.description}</p>
          <ul class="mt-3 flex flex-wrap gap-2">
            {#each catalogEntry.capabilities as cap (cap)}
              <li
                class="rounded-full bg-gray-100 px-2 py-1 text-xs text-gray-700 dark:bg-gray-800 dark:text-gray-200"
              >
                {capabilityLabel(cap)}
              </li>
            {/each}
          </ul>
        {:else}
          <p class="mt-2 text-xs text-gray-500">
            No capability metadata available for connector type
            <code>{source.connector_type}</code>.
          </p>
        {/if}
      </section>
    {:else if activeTab === 'runs'}
      <section class="space-y-4">
        <div class="flex items-center justify-between">
          <h2 class="text-base font-semibold">Batch syncs</h2>
          <button
            type="button"
            onclick={() => (createSyncOpen = !createSyncOpen)}
            class="rounded-xl border border-gray-300 px-3 py-2 text-xs hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-800"
          >
            {createSyncOpen ? 'Cancel' : '+ New batch sync'}
          </button>
        </div>

        {#if createSyncOpen}
          <form
            onsubmit={createSync}
            class="rounded-2xl border border-gray-200 p-4 text-sm dark:border-gray-800"
          >
            <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <label class="block">
                <span class="text-xs text-gray-500">Output dataset id</span>
                <input
                  type="text"
                  bind:value={newOutputDataset}
                  required
                  class="mt-1 w-full rounded-xl border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-900"
                />
              </label>
              <label class="block">
                <span class="text-xs text-gray-500">File glob (optional)</span>
                <input
                  type="text"
                  bind:value={newFileGlob}
                  placeholder="raw/**/*.csv"
                  class="mt-1 w-full rounded-xl border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-900"
                />
              </label>
            </div>
            {#if createSyncError}
              <p class="mt-2 text-xs text-rose-600 dark:text-rose-400">{createSyncError}</p>
            {/if}
            <div class="mt-3 flex justify-end">
              <button
                type="submit"
                disabled={createSyncBusy}
                class="rounded-xl bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-60"
              >
                {createSyncBusy ? 'Creating…' : 'Create sync'}
              </button>
            </div>
          </form>
        {/if}

        {#if runsError}
          <p class="text-xs text-rose-600 dark:text-rose-400">{runsError}</p>
        {/if}

        {#if syncs.length === 0}
          <p class="text-xs text-gray-500">No syncs defined yet.</p>
        {:else}
          <div class="space-y-3">
            {#each syncs as sync (sync.id)}
              <article
                class="rounded-2xl border border-gray-200 p-4 text-sm dark:border-gray-800"
              >
                <header class="flex items-center justify-between">
                  <div>
                    <p class="font-medium">→ {sync.output_dataset_id}</p>
                    <p class="text-xs text-gray-500">
                      {sync.file_glob ?? 'no glob'} ·
                      {sync.schedule_cron ?? 'manual'}
                    </p>
                  </div>
                  <button
                    type="button"
                    onclick={() => runSync(sync.id)}
                    disabled={runningSyncId === sync.id}
                    class="rounded-xl bg-blue-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-blue-700 disabled:opacity-60"
                  >
                    {runningSyncId === sync.id ? 'Starting…' : 'Run now'}
                  </button>
                </header>
                {#if runsBySync[sync.id] && runsBySync[sync.id].length > 0}
                  <ul class="mt-3 divide-y divide-gray-100 text-xs dark:divide-gray-800">
                    {#each runsBySync[sync.id] as run (run.id)}
                      <li class="flex items-center justify-between py-2">
                        <span>
                          <span
                            class={`mr-2 inline-block rounded-full px-2 py-0.5 text-[10px] font-medium ${
                              run.status === 'succeeded'
                                ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
                                : run.status === 'failed' || run.status === 'aborted'
                                  ? 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300'
                                  : run.status === 'running'
                                    ? 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300'
                                    : 'bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300'
                            }`}>{run.status}</span
                          >
                          {new Date(run.started_at).toLocaleString()}
                        </span>
                        <span class="text-gray-500">
                          {run.files_written} files · {run.bytes_written} bytes
                        </span>
                      </li>
                    {/each}
                  </ul>
                {:else}
                  <p class="mt-2 text-xs text-gray-500">No runs yet.</p>
                {/if}
              </article>
            {/each}
          </div>
        {/if}
      </section>
    {/if}
  {/if}
</div>

<ConfirmDialog
  open={deleteConfirm !== null}
  title="Delete data source"
  message="Delete this source? Any attached syncs will stop running and run history is preserved but inaccessible."
  confirmLabel="Delete"
  danger
  busy={deleteConfirm?.busy ?? false}
  onConfirm={() => void confirmDelete()}
  onCancel={() => (deleteConfirm = null)}
/>
