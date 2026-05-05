<script lang="ts">
  /**
   * `/iceberg-tables/[id]` — detail view for one Iceberg table.
   *
   * Tabs (per closing-task spec § 6 + Foundry doc img_001 / img_002):
   *
   *  • Overview       — name, namespace, format, location, markings
   *  • Schema         — current schema_json rendered as a tree
   *  • Snapshots      — table history with operation badges
   *                     (cubre img_002 del doc)
   *  • Metadata       — current metadata.json (read-only) +
   *                     download / open-at-storage
   *                     (cubre img_001 del doc)
   *  • Branches       — refs map (`main`, tags, retention bounds)
   *  • Catalog Access — connection snippets for PyIceberg / Spark /
   *                     Trino / Snowflake + token issuance
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';

  import {
    createIcebergApiToken,
    getIcebergMetadata,
    getIcebergTable,
    getIcebergTableMarkingsByPath,
    listIcebergBranches,
    listIcebergSnapshots,
    runIcebergDiagnose,
    updateIcebergTableMarkings,
    type IcebergBranchesResponse,
    type IcebergDiagnoseResponse,
    type IcebergMetadataResponse,
    type IcebergSnapshotsResponse,
    type IcebergTableDetail,
    type IcebergTableMarkings,
  } from '$lib/api/icebergTables';
  import MarkingsManager from '$lib/components/iceberg/MarkingsManager.svelte';
  import SnapshotBadge from '$lib/components/iceberg/SnapshotBadge.svelte';

  type Tab =
    | 'overview'
    | 'schema'
    | 'snapshots'
    | 'metadata'
    | 'branches'
    | 'permissions'
    | 'activity'
    | 'catalog-access';

  const TABS: { key: Tab; label: string }[] = [
    { key: 'overview', label: 'Overview' },
    { key: 'schema', label: 'Schema' },
    { key: 'snapshots', label: 'Snapshots' },
    { key: 'metadata', label: 'Metadata' },
    { key: 'branches', label: 'Branches' },
    { key: 'permissions', label: 'Permissions' },
    { key: 'activity', label: 'Activity' },
    { key: 'catalog-access', label: 'Catalog Access' },
  ];

  let activeTab = $state<Tab>('overview');
  let detail = $state<IcebergTableDetail | null>(null);
  let snapshots = $state<IcebergSnapshotsResponse | null>(null);
  let metadata = $state<IcebergMetadataResponse | null>(null);
  let branches = $state<IcebergBranchesResponse | null>(null);
  let tableMarkings = $state<IcebergTableMarkings | null>(null);
  let markingsError = $state('');
  let diagnoseRunning = $state(false);
  let diagnoseResult = $state<IcebergDiagnoseResponse | null>(null);
  let expandedSnapshot = $state<number | null>(null);
  let loading = $state(true);
  let error = $state('');

  let issuedToken = $state<string | null>(null);
  let issuingToken = $state(false);

  const id = $derived($page.params.id);

  onMount(async () => {
    try {
      detail = await getIcebergTable(id);
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to load table';
    } finally {
      loading = false;
    }
  });

  async function loadSnapshots() {
    if (snapshots) return;
    snapshots = await listIcebergSnapshots(id);
  }

  async function loadMetadata() {
    if (metadata) return;
    metadata = await getIcebergMetadata(id);
  }

  async function loadBranches() {
    if (branches) return;
    branches = await listIcebergBranches(id);
  }

  async function loadMarkings() {
    if (tableMarkings || !detail) return;
    markingsError = '';
    try {
      const namespace = detail.summary.namespace.join('.');
      tableMarkings = await getIcebergTableMarkingsByPath(namespace, detail.summary.name);
    } catch (err) {
      markingsError = err instanceof Error ? err.message : 'Failed to load markings';
    }
  }

  async function saveMarkings(next: string[]) {
    if (!detail) return;
    const namespace = detail.summary.namespace.join('.');
    tableMarkings = await updateIcebergTableMarkings(
      namespace,
      detail.summary.name,
      next,
    );
  }

  async function runDiagnose(client: string) {
    diagnoseRunning = true;
    diagnoseResult = null;
    try {
      diagnoseResult = await runIcebergDiagnose(client, detail?.summary.project_rid);
    } catch (err) {
      diagnoseResult = {
        client,
        success: false,
        steps: [
          {
            name: 'request',
            ok: false,
            latency_ms: 0,
            detail: err instanceof Error ? err.message : 'Unknown error',
          },
        ],
        total_latency_ms: 0,
      };
    } finally {
      diagnoseRunning = false;
    }
  }

  async function generateToken() {
    issuingToken = true;
    try {
      const result = await createIcebergApiToken('PyIceberg client');
      issuedToken = result.raw_token;
    } catch (err) {
      issuedToken = `Error: ${err instanceof Error ? err.message : err}`;
    } finally {
      issuingToken = false;
    }
  }

  function downloadMetadata() {
    if (!metadata) return;
    const blob = new Blob([JSON.stringify(metadata.metadata, null, 2)], {
      type: 'application/json',
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${detail?.summary.name ?? 'table'}.metadata.json`;
    a.click();
    URL.revokeObjectURL(url);
  }

  $effect(() => {
    if (activeTab === 'snapshots') loadSnapshots();
    if (activeTab === 'metadata') loadMetadata();
    if (activeTab === 'branches') loadBranches();
    if (activeTab === 'permissions') loadMarkings();
  });
</script>

<svelte:head>
  <title>{detail?.summary.name ?? 'Iceberg table'} · OpenFoundry</title>
</svelte:head>

{#if loading}
  <p class="loading">Loading…</p>
{:else if error}
  <p class="error" role="alert">{error}</p>
{:else if detail}
  <section class="detail-page">
    <header class="page-header">
      <div class="title-row">
        <a class="back-link" href="/iceberg-tables">← Iceberg tables</a>
        <h1>{detail.summary.name}</h1>
        <span class="beta-badge">Beta</span>
      </div>
      <div class="meta-row">
        <span class="muted">Namespace:</span>
        <code>{detail.summary.namespace.join('.') || '(root)'}</code>
        <span class="muted">RID:</span>
        <code class="rid">{detail.summary.rid}</code>
      </div>
      <div class="schema-strict-row" data-testid="iceberg-schema-strict-banner">
        <span class="schema-pill">
          Schema v{
            (typeof detail.schema === 'object' &&
              detail.schema &&
              'schema-id' in detail.schema &&
              (detail.schema as { 'schema-id'?: number })['schema-id']) ?? 0
          }
          — strict mode
        </span>
        <a
          href="/docs/foundry/iceberg/schema-evolution/"
          target="_blank"
          rel="noopener noreferrer"
          class="muted"
        >
          How to ALTER schema →
        </a>
      </div>
    </header>

    <nav class="tabs" role="tablist">
      {#each TABS as tab}
        <button
          type="button"
          class="tab"
          class:active={activeTab === tab.key}
          role="tab"
          aria-selected={activeTab === tab.key}
          data-testid={`tab-${tab.key}`}
          onclick={() => (activeTab = tab.key)}
        >
          {tab.label}
        </button>
      {/each}
    </nav>

    {#if activeTab === 'overview'}
      <div class="tab-panel" data-testid="panel-overview">
        <dl class="kv">
          <dt>Format version</dt>
          <dd>v{detail.summary.format_version}</dd>
          <dt>Location</dt>
          <dd class="mono">{detail.summary.location}</dd>
          <dt>Current snapshot</dt>
          <dd>{detail.current_snapshot_id ?? '—'}</dd>
          <dt>Last sequence number</dt>
          <dd>{detail.last_sequence_number}</dd>
          <dt>Markings</dt>
          <dd>
            {#each detail.summary.markings as m}
              <span class="marking">{m}</span>
            {/each}
          </dd>
          <dt>Project rid</dt>
          <dd class="mono">{detail.summary.project_rid}</dd>
        </dl>
      </div>
    {:else if activeTab === 'schema'}
      <div class="tab-panel" data-testid="panel-schema">
        <pre class="json">{JSON.stringify(detail.schema, null, 2)}</pre>
      </div>
    {:else if activeTab === 'snapshots'}
      <div class="tab-panel" data-testid="panel-snapshots">
        {#if !snapshots}
          <p>Loading snapshots…</p>
        {:else if snapshots.snapshots.length === 0}
          <p class="muted">No snapshots yet.</p>
        {:else}
          <table class="snapshot-table">
            <thead>
              <tr>
                <th>Snapshot</th>
                <th>Operation → Foundry</th>
                <th>Parent</th>
                <th>Timestamp</th>
                <th>Summary</th>
              </tr>
            </thead>
            <tbody>
              {#each snapshots.snapshots as s (s.snapshot_id)}
                {@const removed = parseInt(s.summary['deleted-data-files'] ?? '0', 10) || 0}
                {@const added = parseInt(s.summary['added-data-files'] ?? '0', 10) || 0}
                {@const overwriteKind = (s.operation === 'overwrite' && added > 0 && removed >= added ? 'full' : 'partial') as 'full' | 'partial'}
                <tr
                  data-testid={`snapshot-row-${s.snapshot_id}`}
                  onclick={() =>
                    (expandedSnapshot =
                      expandedSnapshot === s.snapshot_id ? null : s.snapshot_id)}
                >
                  <td>{s.snapshot_id}</td>
                  <td>
                    <SnapshotBadge operation={s.operation} overwrite_kind={overwriteKind} />
                  </td>
                  <td>{s.parent_snapshot_id ?? '—'}</td>
                  <td>{s.timestamp ?? '—'}</td>
                  <td>
                    added: {s.summary['added-data-files'] ?? '0'},
                    deleted: {s.summary['deleted-data-files'] ?? '0'},
                    +rows: {s.summary['added-records'] ?? '0'},
                    −rows: {s.summary['deleted-records'] ?? '0'}
                  </td>
                </tr>
                {#if expandedSnapshot === s.snapshot_id}
                  <tr class="expanded">
                    <td colspan="5">
                      <pre class="json">{JSON.stringify(s, null, 2)}</pre>
                    </td>
                  </tr>
                {/if}
              {/each}
            </tbody>
          </table>
        {/if}
      </div>
    {:else if activeTab === 'metadata'}
      <div class="tab-panel" data-testid="panel-metadata">
        {#if !metadata}
          <p>Loading metadata…</p>
        {:else}
          <div class="metadata-actions">
            <button type="button" onclick={downloadMetadata} data-testid="metadata-download">
              Download
            </button>
            <a
              class="open-link"
              href={metadata.metadata_location}
              target="_blank"
              rel="noopener noreferrer"
              data-testid="metadata-open"
            >
              Open metadata location
            </a>
            <span class="muted mono">{metadata.metadata_location}</span>
          </div>
          <pre class="json metadata-json" data-testid="metadata-json-readonly">{JSON.stringify(
              metadata.metadata,
              null,
              2,
            )}</pre>
          {#if metadata.history.length > 1}
            <h3>History</h3>
            <ul class="history">
              {#each metadata.history as item}
                <li>
                  v{item.version} — <span class="mono">{item.path}</span>
                </li>
              {/each}
            </ul>
          {/if}
        {/if}
      </div>
    {:else if activeTab === 'branches'}
      <div class="tab-panel" data-testid="panel-branches">
        {#if !branches}
          <p>Loading branches…</p>
        {:else if branches.branches.length === 0}
          <p class="muted">No branches or tags. Foundry treats <code>main</code> and
            <code>master</code> as equivalent (per Iceberg tables doc).</p>
        {:else}
          <table class="branches-table">
            <thead>
              <tr><th>Name</th><th>Kind</th><th>Snapshot</th></tr>
            </thead>
            <tbody>
              {#each branches.branches as b (b.name)}
                <tr>
                  <td>{b.name}</td>
                  <td>{b.kind}</td>
                  <td>{b.snapshot_id}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
      </div>
    {:else if activeTab === 'permissions'}
      <div class="tab-panel" data-testid="panel-permissions">
        {#if markingsError}
          <p class="error" role="alert">{markingsError}</p>
        {:else if !tableMarkings}
          <p>Loading markings…</p>
        {:else}
          <MarkingsManager markings={tableMarkings} canManage={true} onUpdate={saveMarkings} />
        {/if}
      </div>
    {:else if activeTab === 'activity'}
      <div class="tab-panel" data-testid="panel-activity">
        <p class="lede">
          Audit events scoped to this table. The full timeline lives in the
          platform Audit page; the panel below is a server-rendered slice
          filtered by <code>{detail.summary.rid}</code>.
        </p>
        <iframe
          src={`/audit?resource_id=${encodeURIComponent(detail.summary.rid)}&embedded=1`}
          title="Iceberg activity"
          loading="lazy"
          class="activity-frame"
          data-testid="iceberg-activity-frame"
        ></iframe>
        <p class="muted small">
          Tracked events: <code>iceberg.namespace.*</code>,
          <code>iceberg.table.*</code>, <code>iceberg.markings.*</code>,
          <code>iceberg.access.denied</code>, <code>iceberg.diagnose.executed</code>.
        </p>
      </div>
    {:else if activeTab === 'catalog-access'}
      <div class="tab-panel" data-testid="panel-catalog-access">
        <p class="lede">
          External Iceberg clients authenticate against Foundry's Iceberg REST
          Catalog. See
          <a
            href="/docs/foundry/iceberg/snowflake-iceberg-integration/"
            target="_blank"
            rel="noreferrer">Connect Foundry's Iceberg catalog to Snowflake</a
          > for the Snowflake adapter.
        </p>

        <div class="connector-grid">
          <article class="connector-card">
            <h3>PyIceberg</h3>
            <p>Add this catalog to <code>~/.pyiceberg.yaml</code>:</p>
            <pre class="snippet">{`catalog:
  foundry:
    uri: https://your.foundry/iceberg
    oauth2-server-uri: https://your.foundry/iceberg/v1/oauth/tokens
    credential: <client_id>:<client_secret>
    scope: api:iceberg-read api:iceberg-write`}</pre>
          </article>

          <article class="connector-card">
            <h3>Spark</h3>
            <pre class="snippet">{`spark.sql.catalog.foundry = org.apache.iceberg.spark.SparkCatalog
spark.sql.catalog.foundry.type = rest
spark.sql.catalog.foundry.uri = https://your.foundry/iceberg
spark.sql.catalog.foundry.credential = <client_id>:<client_secret>`}</pre>
          </article>

          <article class="connector-card">
            <h3>Trino</h3>
            <pre class="snippet">{`connector.name=iceberg
iceberg.catalog.type=rest
iceberg.rest-catalog.uri=https://your.foundry/iceberg
iceberg.rest-catalog.security=OAUTH2`}</pre>
          </article>

          <article class="connector-card">
            <h3>Snowflake</h3>
            <p>
              Use the Snowflake connector for Iceberg-managed tables — see the
              <a
                href="/docs/foundry/iceberg/snowflake-iceberg-integration/"
                target="_blank"
                rel="noreferrer">integration guide</a
              >.
            </p>
          </article>
        </div>

        <div class="auth-row">
          <button
            type="button"
            onclick={generateToken}
            disabled={issuingToken}
            data-testid="generate-token"
          >
            {issuingToken ? 'Generating…' : 'Generate API token'}
          </button>
          <a class="muted" href="/control-panel/oauth-clients">Create OAuth2 client →</a>
        </div>
        {#if issuedToken}
          <p class="warn">
            Save this token — it will not be shown again:
            <code class="token">{issuedToken}</code>
          </p>
        {/if}

        <div class="diagnose-row" data-testid="iceberg-diagnose-row">
          <h3>Test connection</h3>
          <p class="muted small">
            Runs a deterministic ListNamespaces → LoadTable probe against the
            catalog and reports per-step latencies. The result is recorded in
            the audit trail (<code>iceberg.diagnose.executed</code>).
          </p>
          <div class="diagnose-buttons">
            {#each ['pyiceberg', 'spark', 'trino', 'snowflake', 'databricks'] as client}
              <button
                type="button"
                disabled={diagnoseRunning}
                onclick={() => runDiagnose(client)}
                data-testid={`iceberg-diagnose-${client}`}
              >
                {client}
              </button>
            {/each}
          </div>
          {#if diagnoseResult}
            <table class="diagnose-table" data-testid="iceberg-diagnose-result">
              <thead>
                <tr><th>Step</th><th>OK?</th><th>Latency (ms)</th><th>Detail</th></tr>
              </thead>
              <tbody>
                {#each diagnoseResult.steps as step}
                  <tr>
                    <td>{step.name}</td>
                    <td class={step.ok ? 'ok' : 'ko'}>{step.ok ? '✓' : '✗'}</td>
                    <td>{step.latency_ms}</td>
                    <td>{step.detail ?? ''}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
            <p class="muted small">
              Total: {diagnoseResult.total_latency_ms} ms — overall {diagnoseResult.success ? 'OK' : 'FAIL'}
            </p>
          {/if}
        </div>
      </div>
    {/if}
  </section>
{/if}

<style>
  .detail-page {
    padding: 1.5rem;
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }
  .page-header h1 {
    display: inline-block;
    margin: 0;
    font-size: 1.5rem;
  }
  .title-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .back-link {
    color: var(--color-accent, #2563eb);
    font-size: 0.875rem;
  }
  .meta-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
    font-size: 0.875rem;
  }
  .schema-strict-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-top: 0.5rem;
    font-size: 0.75rem;
  }
  .schema-pill {
    display: inline-block;
    padding: 0.125rem 0.5rem;
    border-radius: 0.25rem;
    font-weight: 600;
    color: #1e3a8a;
    background: #dbeafe;
    border: 1px solid #bfdbfe;
  }
  .muted {
    color: var(--color-fg-muted, #6b7280);
  }
  .rid {
    overflow-wrap: anywhere;
  }
  .beta-badge {
    display: inline-block;
    padding: 0.125rem 0.5rem;
    border-radius: 0.25rem;
    font-size: 0.75rem;
    font-weight: 600;
    color: #92400e;
    background: #fef3c7;
    border: 1px solid #fde68a;
  }
  .tabs {
    display: flex;
    gap: 0.25rem;
    border-bottom: 1px solid var(--color-border, #e5e7eb);
  }
  .tab {
    padding: 0.5rem 1rem;
    background: transparent;
    border: 0;
    border-bottom: 2px solid transparent;
    cursor: pointer;
    font-size: 0.875rem;
  }
  .tab.active {
    border-bottom-color: var(--color-accent, #2563eb);
    font-weight: 600;
  }
  .tab-panel {
    padding: 1rem 0;
  }
  .kv {
    display: grid;
    grid-template-columns: max-content 1fr;
    column-gap: 1rem;
    row-gap: 0.5rem;
  }
  .kv dt {
    font-weight: 600;
    color: var(--color-fg-muted, #4b5563);
  }
  .kv dd {
    margin: 0;
  }
  .json {
    background: var(--color-bg-subtle, #f9fafb);
    padding: 1rem;
    border-radius: 0.375rem;
    overflow-x: auto;
    font-family: ui-monospace, SFMono-Regular, monospace;
    font-size: 0.75rem;
  }
  .metadata-json {
    max-height: 60vh;
    overflow: auto;
  }
  .metadata-actions {
    display: flex;
    gap: 0.5rem;
    align-items: center;
    margin-bottom: 0.75rem;
  }
  .metadata-actions button,
  .metadata-actions .open-link {
    padding: 0.375rem 0.75rem;
    border-radius: 0.25rem;
    border: 1px solid var(--color-border, #d1d5db);
    background: var(--color-bg-elevated, #fff);
    color: inherit;
    text-decoration: none;
    font-size: 0.875rem;
  }
  .snapshot-table,
  .branches-table {
    width: 100%;
    border-collapse: collapse;
  }
  .snapshot-table th,
  .snapshot-table td,
  .branches-table th,
  .branches-table td {
    text-align: left;
    padding: 0.5rem;
    border-bottom: 1px solid var(--color-border, #e5e7eb);
    font-size: 0.875rem;
  }
  .snapshot-table tr {
    cursor: pointer;
  }
  .badge {
    display: inline-block;
    padding: 0.125rem 0.5rem;
    border-radius: 0.25rem;
    font-size: 0.75rem;
    font-weight: 600;
  }
  .marking {
    display: inline-block;
    margin-right: 0.25rem;
    padding: 0.125rem 0.375rem;
    border-radius: 0.25rem;
    background: var(--color-bg-subtle, #f3f4f6);
    border: 1px solid var(--color-border, #e5e7eb);
    font-size: 0.75rem;
  }
  .connector-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: 1rem;
    margin: 1rem 0;
  }
  .connector-card {
    border: 1px solid var(--color-border, #e5e7eb);
    border-radius: 0.5rem;
    padding: 1rem;
  }
  .snippet {
    background: var(--color-bg-subtle, #f9fafb);
    padding: 0.75rem;
    border-radius: 0.375rem;
    font-family: ui-monospace, SFMono-Regular, monospace;
    font-size: 0.75rem;
    overflow-x: auto;
  }
  .auth-row {
    display: flex;
    gap: 0.75rem;
    align-items: center;
    margin-top: 1rem;
  }
  .auth-row button {
    padding: 0.5rem 1rem;
    border-radius: 0.375rem;
    border: 1px solid var(--color-border, #d1d5db);
    background: var(--color-accent, #2563eb);
    color: white;
    font-size: 0.875rem;
    cursor: pointer;
  }
  .auth-row button[disabled] {
    opacity: 0.6;
    cursor: not-allowed;
  }
  .warn {
    margin-top: 0.75rem;
    padding: 0.75rem 1rem;
    border-radius: 0.375rem;
    background: #fef3c7;
    border: 1px solid #fde68a;
    color: #78350f;
  }
  .token {
    display: block;
    margin-top: 0.5rem;
    font-family: ui-monospace, SFMono-Regular, monospace;
    word-break: break-all;
    font-size: 0.75rem;
  }
  .mono {
    font-family: ui-monospace, SFMono-Regular, monospace;
    font-size: 0.75rem;
  }
  .loading,
  .error {
    padding: 2rem;
    text-align: center;
  }
  .error {
    color: #b91c1c;
  }
  .history {
    list-style: none;
    padding: 0;
    margin: 0.5rem 0;
  }
  .diagnose-row {
    margin-top: 1.5rem;
    padding-top: 1rem;
    border-top: 1px solid var(--color-border, #e5e7eb);
  }
  .diagnose-row h3 {
    margin: 0 0 0.25rem;
    font-size: 1rem;
  }
  .small {
    font-size: 0.75rem;
  }
  .diagnose-buttons {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
    margin: 0.5rem 0;
  }
  .diagnose-buttons button {
    padding: 0.25rem 0.75rem;
    border-radius: 0.25rem;
    border: 1px solid var(--color-border, #d1d5db);
    background: var(--color-bg-elevated, #fff);
    color: inherit;
    text-transform: capitalize;
    font-size: 0.875rem;
    cursor: pointer;
  }
  .diagnose-table {
    width: 100%;
    border-collapse: collapse;
    margin-top: 0.5rem;
  }
  .diagnose-table th,
  .diagnose-table td {
    text-align: left;
    padding: 0.375rem 0.5rem;
    border-bottom: 1px solid var(--color-border, #e5e7eb);
    font-size: 0.875rem;
  }
  .diagnose-table .ok {
    color: #047857;
    font-weight: 600;
  }
  .diagnose-table .ko {
    color: #b91c1c;
    font-weight: 600;
  }
  .activity-frame {
    width: 100%;
    height: 480px;
    border: 1px solid var(--color-border, #e5e7eb);
    border-radius: 0.375rem;
    background: var(--color-bg-elevated, #fff);
  }
  .history li {
    padding: 0.25rem 0;
    border-bottom: 1px solid var(--color-border, #e5e7eb);
    font-size: 0.875rem;
  }
</style>
