<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import {
    capabilityLabel,
    CONNECTOR_FAMILY_ORDER,
    dataConnection,
    FALLBACK_CONNECTOR_CATALOG,
    filterCatalog,
    type ConnectorCatalogEntry,
    type ConnectorFamily,
    type DiscoveredSource,
  } from '$lib/api/data-connection';

  let catalog = $state<ConnectorCatalogEntry[]>(FALLBACK_CONNECTOR_CATALOG);
  let query = $state('');
  let loading = $state(true);
  let creating = $state(false);
  let error = $state('');
  let createError = $state('');

  // Wizard step state — Tarea 10. Step 1: pick family + connector.
  // Step 2: credentials. Step 3: test_connection → discover_sources →
  // bulk_register selected tables.
  let step = $state<1 | 2 | 3>(1);
  let familyFilter = $state<ConnectorFamily | 'All'>('All');
  let selected = $state<ConnectorCatalogEntry | null>(null);
  let nameInput = $state('');
  let configInput = $state<Record<string, string>>({});

  // Step 3 state — created source + discovery results.
  let createdSourceId = $state<string | null>(null);
  let testing = $state(false);
  let testResult = $state<{ success: boolean; message: string; latency_ms: number | null } | null>(
    null,
  );
  let discovering = $state(false);
  let discovered = $state<DiscoveredSource[]>([]);
  let selectedSelectors = $state<Record<string, boolean>>({});
  let registering = $state(false);
  let registrationResult = $state<{ created: number; errors: number } | null>(null);

  const filtered = $derived(
    filterCatalog(catalog, query).filter(
      (entry) => familyFilter === 'All' || entry.family === familyFilter,
    ),
  );

  // Group filtered catalog by family for the gallery sectioning.
  const grouped = $derived.by(() => {
    const map = new Map<ConnectorFamily | 'Other', ConnectorCatalogEntry[]>();
    for (const entry of filtered) {
      const key = (entry.family ?? 'Other') as ConnectorFamily | 'Other';
      const list = map.get(key) ?? [];
      list.push(entry);
      map.set(key, list);
    }
    const ordered: { family: ConnectorFamily | 'Other'; entries: ConnectorCatalogEntry[] }[] = [];
    for (const fam of CONNECTOR_FAMILY_ORDER) {
      const entries = map.get(fam);
      if (entries && entries.length > 0) ordered.push({ family: fam, entries });
    }
    const other = map.get('Other');
    if (other && other.length > 0) ordered.push({ family: 'Other', entries: other });
    return ordered;
  });

  // Per-connector config schemas. Mirrors the backend `validate_config` checks
  // in services/connector-management-service/src/connectors/*.rs. Keep field
  // ids identical to what the Rust validator inspects.
  type ConfigField = {
    key: string;
    label: string;
    type?: 'text' | 'password' | 'number' | 'url';
    placeholder?: string;
    required?: boolean;
    help?: string;
  };

  const CONFIG_SCHEMAS: Record<string, ConfigField[]> = {
    postgresql: [
      { key: 'host', label: 'Host', placeholder: 'db.example.com', required: true },
      { key: 'port', label: 'Port', type: 'number', placeholder: '5432', required: true },
      { key: 'database', label: 'Database', placeholder: 'analytics', required: true },
      { key: 'user', label: 'User', placeholder: 'foundry_reader', required: true },
      {
        key: 'password',
        label: 'Password',
        type: 'password',
        required: true,
        help: 'Stored as part of the source config in this MVP. Rotate via the credentials tab once available.',
      },
    ],
    mysql: [
      { key: 'host', label: 'Host', placeholder: 'mysql.internal', required: true },
      { key: 'port', label: 'Port', type: 'number', placeholder: '3306', required: true },
      { key: 'database', label: 'Database', placeholder: 'analytics', required: true },
      { key: 'user', label: 'User', placeholder: 'foundry_reader', required: true },
      {
        key: 'password',
        label: 'Password',
        type: 'password',
        help: 'Reads route through the connector agent; provide credentials only when running with a direct connection.',
      },
    ],
    rest_api: [
      {
        key: 'base_url',
        label: 'Base URL',
        type: 'url',
        placeholder: 'https://api.example.com',
        required: true,
      },
    ],
    s3: [
      {
        key: 'url',
        label: 'Bucket URL',
        placeholder: 's3://my-bucket/prefix/',
        required: true,
        help: 'Use the s3:// scheme with a trailing slash, matching the Foundry Amazon S3 source.',
      },
      {
        key: 'endpoint',
        label: 'Endpoint',
        placeholder: 's3.us-east-1.amazonaws.com',
      },
      { key: 'region', label: 'Region', placeholder: 'us-east-1' },
      { key: 'access_key_id', label: 'Access Key ID' },
      { key: 'secret_access_key', label: 'Secret Access Key', type: 'password' },
    ],
    parquet: [
      {
        key: 'url',
        label: 'File URL',
        type: 'url',
        placeholder: 'https://example.com/data.parquet',
        help: 'Provide either a remote URL or a local path; the connector validates the PAR1 magic markers before sync.',
      },
      { key: 'path', label: 'File path', placeholder: '/var/data/orders.parquet' },
    ],
    gcs: [
      {
        key: 'bucket',
        label: 'Bucket',
        placeholder: 'analytics-prod',
        required: true,
      },
      {
        key: 'prefix',
        label: 'Prefix (optional)',
        placeholder: 'raw/orders/',
        help: 'Narrows the listing. Discovery uses list_with_delimiter under this prefix.',
      },
      {
        key: 'access_token',
        label: 'OAuth2 access token',
        type: 'password',
        help: 'Static bearer token. Use only for short-lived demos.',
      },
      {
        key: 'service_account_json',
        label: 'Service account JSON',
        type: 'password',
        help: 'Paste the JSON key file contents. Alternative to access_token / Workload Identity.',
      },
    ],
    onelake: [
      {
        key: 'workspace',
        label: 'Fabric workspace',
        placeholder: 'my-workspace',
        required: true,
      },
      {
        key: 'lakehouse',
        label: 'Lakehouse name',
        placeholder: 'sales_lakehouse',
        required: true,
        help: 'Fabric lakehouse name without the .Lakehouse suffix.',
      },
      {
        key: 'namespace',
        label: 'Namespace',
        placeholder: 'Files',
        help: 'Files (default) or Tables.',
      },
      {
        key: 'oauth_token',
        label: 'Entra ID bearer token',
        type: 'password',
        help: 'Alternative to a tenant_id/client_id/client_secret service principal.',
      },
      { key: 'tenant_id', label: 'Tenant ID' },
      { key: 'client_id', label: 'Client ID' },
      { key: 'client_secret', label: 'Client secret', type: 'password' },
    ],
    bigquery: [
      { key: 'project_id', label: 'Project ID', placeholder: 'my-gcp-project', required: true },
      {
        key: 'service_account_json',
        label: 'Service account JSON',
        type: 'password',
        help: 'Paste the JSON key file contents. Used to mint an OAuth2 access token.',
      },
      {
        key: 'access_token',
        label: 'OAuth access token',
        type: 'password',
        help: 'Alternative to service_account_json. Bearer token with the bigquery scope.',
      },
      { key: 'location', label: 'Default location', placeholder: 'EU' },
    ],
    snowflake: [
      {
        key: 'account',
        label: 'Account locator',
        placeholder: 'xy12345.eu-central-1',
        required: true,
      },
      { key: 'database', label: 'Database', required: true },
      { key: 'schema', label: 'Schema', required: true },
      { key: 'warehouse', label: 'Warehouse' },
      { key: 'role', label: 'Role' },
      { key: 'user', label: 'User (uppercase)', help: 'Required for keypair JWT auth.' },
      {
        key: 'private_key_pem',
        label: 'Private key PEM',
        type: 'password',
        help: 'PKCS#8 RSA private key. Use either keypair JWT or oauth_token.',
      },
      {
        key: 'public_key_fingerprint',
        label: 'Public key fingerprint',
        help: 'SHA256:... (run SHOW USERS to retrieve RSA_PUBLIC_KEY_FP).',
      },
      {
        key: 'oauth_token',
        label: 'OAuth bearer token',
        type: 'password',
        help: 'Alternative to keypair JWT.',
      },
    ],
    salesforce: [
      {
        key: 'instance_url',
        label: 'Instance URL',
        placeholder: 'https://my-org.my.salesforce.com',
        required: true,
      },
      {
        key: 'access_token',
        label: 'OAuth access token',
        type: 'password',
        required: true,
      },
      { key: 'api_version', label: 'API version', placeholder: 'v60.0' },
      {
        key: 'include_deleted',
        label: 'Include deleted records (queryAll)',
        help: 'When true uses /queryAll instead of /query.',
      },
    ],
    kinesis: [
      { key: 'stream_name', label: 'Stream name', placeholder: 'orders-stream', required: true },
      { key: 'region', label: 'AWS region', placeholder: 'us-east-1' },
      { key: 'access_key_id', label: 'Access key ID' },
      { key: 'secret_access_key', label: 'Secret access key', type: 'password' },
      { key: 'session_token', label: 'Session token (optional)', type: 'password' },
      {
        key: 'endpoint',
        label: 'Endpoint override',
        placeholder: 'https://kinesis.local:4566',
        help: 'Useful for LocalStack or VPC endpoints.',
      },
      {
        key: 'iterator_type',
        label: 'Iterator type',
        placeholder: 'TRIM_HORIZON',
        help: 'TRIM_HORIZON | LATEST | AT_SEQUENCE_NUMBER | AFTER_SEQUENCE_NUMBER | AT_TIMESTAMP.',
      },
      {
        key: 'starting_sequence_number',
        label: 'Starting sequence number',
        help: 'Required for AT_SEQUENCE_NUMBER / AFTER_SEQUENCE_NUMBER.',
      },
      { key: 'max_records', label: 'Max records per sync', type: 'number', placeholder: '1000' },
      {
        key: 'max_iterations',
        label: 'Max GetRecords iterations',
        type: 'number',
        placeholder: '25',
      },
    ],
    iot: [
      { key: 'broker_host', label: 'Broker host', placeholder: 'broker.example.com', required: true },
      {
        key: 'broker_port',
        label: 'Broker port',
        type: 'number',
        placeholder: '1883',
        help: 'Defaults to 1883 (or 8883 when TLS is enabled).',
      },
      { key: 'client_id', label: 'Client ID', placeholder: 'openfoundry-sync-1' },
      { key: 'username', label: 'Username' },
      { key: 'password', label: 'Password', type: 'password' },
      {
        key: 'tls',
        label: 'Enable TLS',
        help: 'When true the connection uses native rustls roots over MQTTS.',
      },
      {
        key: 'topic',
        label: 'Topic filter',
        placeholder: 'sensors/+/telemetry',
        help: 'MQTT topic filter to subscribe to. Use a JSON array via the API for multiple filters.',
      },
      {
        key: 'qos',
        label: 'QoS (0/1/2)',
        type: 'number',
        placeholder: '0',
      },
      {
        key: 'discovery_window_ms',
        label: 'Discovery window (ms)',
        type: 'number',
        placeholder: '2000',
      },
      {
        key: 'max_messages',
        label: 'Max messages per sync',
        type: 'number',
        placeholder: '1000',
      },
      {
        key: 'max_duration_ms',
        label: 'Max sync window (ms)',
        type: 'number',
        placeholder: '5000',
      },
    ],
  };

  function schemaFor(type: string | undefined): ConfigField[] {
    if (!type) return [];
    return CONFIG_SCHEMAS[type] ?? [];
  }

  function buildConfigPayload(
    schema: ConfigField[],
    raw: Record<string, string>,
  ): Record<string, unknown> {
    const out: Record<string, unknown> = {};
    for (const field of schema) {
      const value = (raw[field.key] ?? '').trim();
      if (value === '') continue;
      out[field.key] = field.type === 'number' ? Number(value) : value;
    }
    return out;
  }

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
    // Reset config inputs to defaults derived from the schema's placeholders
    // (port is the main one — we want 5432 prefilled for Postgres).
    const next: Record<string, string> = {};
    for (const field of schemaFor(entry.type)) {
      if (field.type === 'number' && field.placeholder) {
        next[field.key] = field.placeholder;
      } else {
        next[field.key] = '';
      }
    }
    configInput = next;
    createError = '';
    step = 2;
  }

  function backToCatalog() {
    selected = null;
    createError = '';
    step = 1;
  }

  function backToCredentials() {
    step = 2;
    testResult = null;
    discovered = [];
    selectedSelectors = {};
    registrationResult = null;
  }

  async function createSource(event: SubmitEvent) {
    event.preventDefault();
    if (!selected) return;
    if (!nameInput.trim()) {
      createError = 'A source name is required.';
      return;
    }
    const schema = schemaFor(selected.type);
    const missing = schema
      .filter((f) => f.required && (configInput[f.key] ?? '').trim() === '')
      .map((f) => f.label);
    if (missing.length > 0) {
      createError = `Missing required fields: ${missing.join(', ')}.`;
      return;
    }
    creating = true;
    createError = '';
    try {
      const source = await dataConnection.createSource({
        name: nameInput.trim(),
        connector_type: selected.type,
        worker: 'foundry',
        config: buildConfigPayload(schema, configInput),
      });
      createdSourceId = source.id;
      step = 3;
      // Kick off a test_connection automatically — Tarea 10 wizard step 3
      // mirrors the Foundry "verify connection" pane.
      await runTestConnection();
    } catch (cause) {
      console.error('createSource failed', cause);
      createError = cause instanceof Error ? cause.message : 'Failed to create source';
    } finally {
      creating = false;
    }
  }

  async function runTestConnection() {
    if (!createdSourceId) return;
    testing = true;
    testResult = null;
    try {
      testResult = await dataConnection.testConnection(createdSourceId);
      if (testResult.success) {
        await runDiscovery();
      }
    } catch (cause) {
      testResult = {
        success: false,
        message: cause instanceof Error ? cause.message : 'test_connection failed',
        latency_ms: null,
      };
    } finally {
      testing = false;
    }
  }

  async function runDiscovery() {
    if (!createdSourceId) return;
    discovering = true;
    discovered = [];
    selectedSelectors = {};
    try {
      const response = await dataConnection.discoverSources(createdSourceId);
      discovered = response.sources ?? [];
    } catch (cause) {
      console.warn('discoverSources failed', cause);
      discovered = [];
    } finally {
      discovering = false;
    }
  }

  async function registerSelected() {
    if (!createdSourceId) return;
    const items = discovered
      .filter((s) => selectedSelectors[s.selector])
      .map((s) => ({
        selector: s.selector,
        source_kind: s.source_kind ?? undefined,
      }));
    if (items.length === 0) {
      registrationResult = { created: 0, errors: 0 };
      return;
    }
    registering = true;
    registrationResult = null;
    try {
      const response = await dataConnection.bulkRegister(createdSourceId, items);
      registrationResult = {
        created: response.created?.length ?? 0,
        errors: response.errors?.length ?? 0,
      };
    } catch (cause) {
      console.error('bulkRegister failed', cause);
      registrationResult = { created: 0, errors: items.length };
    } finally {
      registering = false;
    }
  }

  function finishWizard() {
    if (createdSourceId) {
      goto(`/data-connection/sources/${createdSourceId}`);
    } else {
      goto('/data-connection');
    }
  }

  onMount(load);
</script>

<svelte:head>
  <title>New source · Data Connection</title>
</svelte:head>

<div class="space-y-6">
  <div>
    <a href="/data-connection" class="text-xs text-blue-600 hover:underline dark:text-blue-400"
      >← Back to sources</a
    >
    <h1 class="mt-1 text-2xl font-bold">New connector</h1>
    <p class="mt-1 max-w-2xl text-sm text-gray-500">
      A guided 3-step wizard: pick a connector by family, configure credentials, then verify the
      connection and register the tables you want to expose as datasets.
    </p>
  </div>

  <!-- Stepper -->
  <ol class="flex items-center gap-2 text-xs font-medium">
    {#each [{ n: 1, label: 'Connector' }, { n: 2, label: 'Credentials' }, { n: 3, label: 'Verify & register' }] as { n, label } (n)}
      <li
        class={`flex items-center gap-2 rounded-full border px-3 py-1 ${
          step === n
            ? 'border-blue-500 bg-blue-50 text-blue-700 dark:border-blue-400 dark:bg-blue-950/40 dark:text-blue-300'
            : step > n
              ? 'border-emerald-500 bg-emerald-50 text-emerald-700 dark:border-emerald-400 dark:bg-emerald-950/40 dark:text-emerald-300'
              : 'border-gray-300 text-gray-500 dark:border-gray-700'
        }`}
      >
        <span class="font-semibold">{n}.</span>
        <span>{label}</span>
      </li>
    {/each}
  </ol>

  {#if error}
    <div
      class="rounded-xl border border-amber-300 bg-amber-50 p-3 text-xs text-amber-900 dark:border-amber-700 dark:bg-amber-950/30 dark:text-amber-200"
    >
      {error}
    </div>
  {/if}

  <!-- ============================================================ -->
  <!-- Step 1 — pick a connector, grouped by family.                -->
  <!-- ============================================================ -->
  {#if step === 1}
    <div class="flex flex-wrap items-center gap-2">
      <button
        type="button"
        class={`rounded-full px-3 py-1 text-xs ${familyFilter === 'All' ? 'bg-blue-600 text-white' : 'border border-gray-300 dark:border-gray-700'}`}
        onclick={() => (familyFilter = 'All')}
      >
        All families
      </button>
      {#each CONNECTOR_FAMILY_ORDER as fam (fam)}
        <button
          type="button"
          class={`rounded-full px-3 py-1 text-xs ${familyFilter === fam ? 'bg-blue-600 text-white' : 'border border-gray-300 dark:border-gray-700'}`}
          onclick={() => (familyFilter = fam)}
        >
          {fam}
        </button>
      {/each}
      <input
        type="search"
        bind:value={query}
        placeholder="Search connectors or capabilities…"
        class="ml-auto w-72 rounded-xl border border-gray-300 bg-white px-3 py-2 text-sm focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-900"
      />
    </div>

    {#if loading}
      <p class="text-sm text-gray-500">Loading catalog…</p>
    {:else if grouped.length === 0}
      <p class="text-sm text-gray-500">No connectors match this filter.</p>
    {:else}
      <div class="space-y-6">
        {#each grouped as group (group.family)}
          <section>
            <header class="mb-2 flex items-baseline gap-2">
              <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-700 dark:text-gray-300">
                {group.family}
              </h2>
              <span class="text-xs text-gray-400">{group.entries.length}</span>
            </header>
            <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {#each group.entries as entry (entry.type)}
                <button
                  type="button"
                  onclick={() => pick(entry)}
                  disabled={!entry.available}
                  class={`flex flex-col gap-2 rounded-2xl border p-4 text-left transition ${
                    entry.available
                      ? 'border-gray-200 hover:border-blue-500 hover:bg-blue-50/40 dark:border-gray-800 dark:hover:bg-blue-950/20'
                      : 'cursor-not-allowed border-gray-200 bg-gray-50 opacity-60 dark:border-gray-800 dark:bg-gray-900'
                  }`}
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
          </section>
        {/each}
      </div>
    {/if}
  {/if}

  <!-- ============================================================ -->
  <!-- Step 2 — credentials form for the chosen connector.          -->
  <!-- ============================================================ -->
  {#if step === 2 && selected}
    <form
      onsubmit={createSource}
      class="rounded-2xl border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-800 dark:bg-gray-900"
    >
      <div class="flex items-baseline justify-between">
        <h2 class="text-lg font-semibold">{selected.name} — credentials</h2>
        <span class="text-xs text-gray-500">
          Encrypted at rest with AES-256-GCM via the credential vault.
        </span>
      </div>

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

      {#if schemaFor(selected.type).length > 0}
        <div class="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2">
          {#each schemaFor(selected.type) as field (field.key)}
            <label class="block">
              <span class="text-xs font-medium text-gray-600 dark:text-gray-300">
                {field.label}{#if field.required}<span class="ml-0.5 text-rose-500">*</span>{/if}
              </span>
              <input
                type={field.type ?? 'text'}
                bind:value={configInput[field.key]}
                placeholder={field.placeholder ?? ''}
                required={field.required}
                autocomplete={field.type === 'password' ? 'new-password' : 'off'}
                class="mt-1 w-full rounded-xl border border-gray-300 bg-white px-3 py-2 text-sm focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-950"
              />
              {#if field.help}
                <span class="mt-1 block text-[11px] text-gray-500">{field.help}</span>
              {/if}
            </label>
          {/each}
        </div>
      {/if}

      {#if createError}
        <p class="mt-3 text-xs text-rose-600 dark:text-rose-400">{createError}</p>
      {/if}

      <div class="mt-4 flex items-center justify-between gap-2">
        <button
          type="button"
          onclick={backToCatalog}
          class="rounded-xl border border-gray-300 px-3 py-2 text-sm hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-800"
        >
          ← Back
        </button>
        <button
          type="submit"
          disabled={creating}
          class="rounded-xl bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-60"
        >
          {creating ? 'Creating…' : 'Create & verify'}
        </button>
      </div>
    </form>
  {/if}

  <!-- ============================================================ -->
  <!-- Step 3 — test_connection + discover_sources + bulk_register. -->
  <!-- ============================================================ -->
  {#if step === 3 && selected}
    <div class="space-y-4 rounded-2xl border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-800 dark:bg-gray-900">
      <div class="flex items-baseline justify-between">
        <h2 class="text-lg font-semibold">{selected.name} — verify & register</h2>
        <button
          type="button"
          onclick={runTestConnection}
          disabled={testing}
          class="rounded-xl border border-gray-300 px-3 py-2 text-xs hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-800 disabled:opacity-60"
        >
          {testing ? 'Testing…' : 'Re-run test'}
        </button>
      </div>

      {#if testResult}
        <div
          class={`rounded-xl border p-3 text-xs ${
            testResult.success
              ? 'border-emerald-300 bg-emerald-50 text-emerald-900 dark:border-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-200'
              : 'border-rose-300 bg-rose-50 text-rose-900 dark:border-rose-700 dark:bg-rose-950/30 dark:text-rose-200'
          }`}
        >
          <p class="font-medium">
            {testResult.success ? '✓ Connection succeeded' : '✗ Connection failed'}
            {#if testResult.latency_ms !== null}
              <span class="ml-2 font-normal opacity-75">({testResult.latency_ms} ms)</span>
            {/if}
          </p>
          <p class="mt-1 opacity-90">{testResult.message}</p>
        </div>
      {/if}

      {#if testResult?.success}
        <div>
          <h3 class="text-sm font-semibold">Discovered tables</h3>
          {#if discovering}
            <p class="mt-2 text-xs text-gray-500">Discovering…</p>
          {:else if discovered.length === 0}
            <p class="mt-2 text-xs text-gray-500">
              No selectors discovered. You can register tables manually from the source detail page.
            </p>
          {:else}
            <ul class="mt-2 max-h-72 space-y-1 overflow-y-auto rounded-xl border border-gray-200 p-2 dark:border-gray-700">
              {#each discovered as src (src.selector)}
                <li class="flex items-center gap-2 text-xs">
                  <input
                    type="checkbox"
                    bind:checked={selectedSelectors[src.selector]}
                    class="h-4 w-4"
                  />
                  <span class="font-mono">{src.selector}</span>
                  {#if src.source_kind}
                    <span class="ml-auto rounded-full bg-gray-100 px-2 py-0.5 text-[10px] text-gray-600 dark:bg-gray-800 dark:text-gray-300">
                      {src.source_kind}
                    </span>
                  {/if}
                </li>
              {/each}
            </ul>
          {/if}
        </div>
      {/if}

      {#if registrationResult}
        <div class="rounded-xl border border-blue-300 bg-blue-50 p-3 text-xs text-blue-900 dark:border-blue-700 dark:bg-blue-950/30 dark:text-blue-200">
          Registered {registrationResult.created} table(s).
          {#if registrationResult.errors > 0}
            {registrationResult.errors} error(s) — see backend logs.
          {/if}
        </div>
      {/if}

      <div class="flex items-center justify-between gap-2">
        <button
          type="button"
          onclick={backToCredentials}
          class="rounded-xl border border-gray-300 px-3 py-2 text-sm hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-800"
        >
          ← Back to credentials
        </button>
        <div class="flex gap-2">
          <button
            type="button"
            onclick={registerSelected}
            disabled={registering || !testResult?.success || discovered.length === 0}
            class="rounded-xl border border-blue-500 px-3 py-2 text-sm font-medium text-blue-600 hover:bg-blue-50 disabled:opacity-60 dark:hover:bg-blue-950/30"
          >
            {registering ? 'Registering…' : 'Register selected tables'}
          </button>
          <button
            type="button"
            onclick={finishWizard}
            class="rounded-xl bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
          >
            Finish & open source
          </button>
        </div>
      </div>
    </div>
  {/if}
</div>
