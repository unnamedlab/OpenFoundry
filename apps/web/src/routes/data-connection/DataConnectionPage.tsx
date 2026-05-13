import { useEffect, useMemo, useState, type CSSProperties } from 'react';
import { Link } from 'react-router-dom';

import {
  FALLBACK_CONNECTOR_CATALOG,
  capabilityLabel,
  dataConnection,
  type ConnectorAgent,
  type ConnectorCapability,
  type Source,
  type SourceStatus,
  type SourceWorker,
} from '@/lib/api/data-connection';
import {
  type VirtualTableProvider,
  type VirtualTableSourceLink,
  providerLabel,
} from '@/lib/api/virtual-tables';
import { AutoRegistrationCard } from '@/lib/components/data-connection/AutoRegistrationCard';
import { CreateAutoRegistrationModal } from '@/lib/components/data-connection/CreateAutoRegistrationModal';
import { RemoteCatalogBrowser } from '@/lib/components/data-connection/RemoteCatalogBrowser';

const STATUS_COLOR: Record<SourceStatus, string> = {
  healthy: '#10b981',
  degraded: '#f59e0b',
  error: '#ef4444',
  configuring: '#3b82f6',
  draft: '#94a3b8',
};

const SOURCE_STATUSES: SourceStatus[] = ['healthy', 'degraded', 'error', 'configuring', 'draft'];
const CAPABILITY_FILTERS: ConnectorCapability[] = [
  'batch_sync',
  'streaming_sync',
  'cdc_sync',
  'file_export',
  'table_export',
  'streaming_export',
  'webhook',
  'virtual_table',
  'exploration',
];
const WORKER_FILTERS: SourceWorker[] = ['foundry', 'agent'];
const SHELL_VIEWS = [
  { id: 'sources', label: 'Sources', description: 'Connections, credentials, worker, and source health.' },
  { id: 'syncs', label: 'Syncs', description: 'Batch, file, table, media, streaming, and CDC ingestion jobs.' },
  { id: 'streams', label: 'Streams', description: 'Streaming datasets, offsets, checkpoints, consumers, and replay.' },
  { id: 'exports', label: 'Exports', description: 'File, table, and streaming exports from OpenFoundry to external systems.' },
  { id: 'webhooks', label: 'Webhooks', description: 'Interactive external requests with typed inputs and outputs.' },
  { id: 'virtual-tables', label: 'Virtual Tables', description: 'External warehouse tables registered without copying data.' },
  { id: 'agents', label: 'Agents', description: 'Agent programs, heartbeats, proxy policy support, and worker status.' },
  { id: 'health', label: 'Health', description: 'Recent failures, degraded resources, and stale connections.' },
] as const;

type ShellViewId = (typeof SHELL_VIEWS)[number]['id'];
type SourceWithOwner = Source & { owner_id?: string | null; owner_name?: string | null };

function providerForConnector(type: string): VirtualTableProvider | null {
  switch (type) {
    case 's3':
      return 'AMAZON_S3';
    case 'gcs':
      return 'GCS';
    case 'onelake':
    case 'abfs':
      return 'AZURE_ABFS';
    case 'bigquery':
      return 'BIGQUERY';
    case 'databricks':
      return 'DATABRICKS';
    case 'snowflake':
      return 'SNOWFLAKE';
    case 'foundry_iceberg':
    case 'iceberg':
      return 'FOUNDRY_ICEBERG';
    default:
      return null;
  }
}

function emptyVirtualLink(sourceRid: string, provider: VirtualTableProvider): VirtualTableSourceLink {
  return {
    source_rid: sourceRid,
    provider,
    virtual_tables_enabled: true,
    code_imports_enabled: false,
    export_controls: {},
    auto_register_project_rid: null,
    auto_register_enabled: false,
    auto_register_interval_seconds: null,
    auto_register_tag_filters: [],
    iceberg_catalog_kind: null,
    iceberg_catalog_config: null,
    created_at: '',
    updated_at: '',
  };
}

function ownerLabel(source: SourceWithOwner): string {
  return source.owner_name ?? source.owner_id ?? 'Unassigned owner';
}

function sourceCapabilities(source: Source): ConnectorCapability[] {
  return FALLBACK_CONNECTOR_CATALOG.find((entry) => entry.type === source.connector_type)?.capabilities ?? [];
}

function sourceTypeLabel(type: string): string {
  return FALLBACK_CONNECTOR_CATALOG.find((entry) => entry.type === type)?.name ?? type;
}

function isRecentFailure(source: Source): boolean {
  return source.status === 'error' || source.status === 'degraded';
}

export function DataConnectionPage() {
  const [sources, setSources] = useState<SourceWithOwner[]>([]);
  const [agents, setAgents] = useState<ConnectorAgent[]>([]);
  const [loading, setLoading] = useState(true);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [error, setError] = useState('');
  const [query, setQuery] = useState('');
  const [statusFilter, setStatusFilter] = useState<SourceStatus | 'all'>('all');
  const [capabilityFilter, setCapabilityFilter] = useState<ConnectorCapability | 'all'>('all');
  const [workerFilter, setWorkerFilter] = useState<SourceWorker | 'all'>('all');
  const [ownerFilter, setOwnerFilter] = useState<string>('all');
  const [sourceTypeFilter, setSourceTypeFilter] = useState<string>('all');
  const [view, setView] = useState<ShellViewId>('sources');
  const [selectedSourceId, setSelectedSourceId] = useState<string | null>(null);
  const [autoRegistrationOpen, setAutoRegistrationOpen] = useState(false);
  const [autoRegistrationLinks, setAutoRegistrationLinks] = useState<Record<string, VirtualTableSourceLink>>({});

  async function load() {
    setLoading(true);
    setError('');
    try {
      const [sourceRes, agentRes] = await Promise.allSettled([
        dataConnection.listSources({ page: 1, per_page: 100 }),
        dataConnection.listConnectorAgents(),
      ]);
      if (sourceRes.status === 'rejected') throw sourceRes.reason;
      const nextSources = sourceRes.value.data as SourceWithOwner[];
      setSources(nextSources);
      setSelectedSourceId((current) => {
        if (current && nextSources.some((source) => source.id === current)) return current;
        return nextSources[0]?.id ?? null;
      });
      setAgents(agentRes.status === 'fulfilled' ? agentRes.value : []);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load Data Connection shell');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  async function handleDelete(id: string) {
    if (typeof window !== 'undefined' && !window.confirm('Delete source?')) return;
    setBusyId(id);
    try {
      await dataConnection.deleteSource(id);
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Delete failed');
    } finally {
      setBusyId(null);
    }
  }

  const ownerOptions = useMemo(() => {
    const options = new Set<string>();
    for (const source of sources) options.add(ownerLabel(source));
    return Array.from(options).sort((a, b) => a.localeCompare(b));
  }, [sources]);

  const sourceTypeOptions = useMemo(() => {
    const options = new Set(sources.map((source) => source.connector_type));
    for (const entry of FALLBACK_CONNECTOR_CATALOG) options.add(entry.type);
    return Array.from(options).sort((a, b) => sourceTypeLabel(a).localeCompare(sourceTypeLabel(b)));
  }, [sources]);

  const filteredSources = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return sources.filter((source) => {
      const capabilities = sourceCapabilities(source);
      if (statusFilter !== 'all' && source.status !== statusFilter) return false;
      if (capabilityFilter !== 'all' && !capabilities.includes(capabilityFilter)) return false;
      if (workerFilter !== 'all' && source.worker !== workerFilter) return false;
      if (ownerFilter !== 'all' && ownerLabel(source) !== ownerFilter) return false;
      if (sourceTypeFilter !== 'all' && source.connector_type !== sourceTypeFilter) return false;
      if (!needle) return true;
      return [
        source.name,
        source.id,
        source.connector_type,
        sourceTypeLabel(source.connector_type),
        source.worker,
        ownerLabel(source),
        source.status,
        ...capabilities.map(capabilityLabel),
      ].some((value) => value.toLowerCase().includes(needle));
    });
  }, [capabilityFilter, ownerFilter, query, sourceTypeFilter, sources, statusFilter, workerFilter]);

  const statusCounts = useMemo(() => {
    const counts = new Map<SourceStatus, number>();
    for (const status of SOURCE_STATUSES) counts.set(status, 0);
    for (const source of sources) counts.set(source.status, (counts.get(source.status) ?? 0) + 1);
    return counts;
  }, [sources]);

  const selectedSource = useMemo(
    () => sources.find((source) => source.id === selectedSourceId) ?? null,
    [selectedSourceId, sources],
  );
  const selectedCatalogEntry = selectedSource
    ? FALLBACK_CONNECTOR_CATALOG.find((entry) => entry.type === selectedSource.connector_type)
    : undefined;
  const selectedProvider = selectedSource ? providerForConnector(selectedSource.connector_type) : null;
  const selectedVirtualLink = selectedSource && selectedProvider
    ? autoRegistrationLinks[selectedSource.id] ?? emptyVirtualLink(selectedSource.id, selectedProvider)
    : null;

  const capabilityCounts = useMemo(() => {
    const counts = new Map<ConnectorCapability, number>();
    for (const capability of CAPABILITY_FILTERS) counts.set(capability, 0);
    for (const source of sources) {
      for (const capability of sourceCapabilities(source)) {
        counts.set(capability, (counts.get(capability) ?? 0) + 1);
      }
    }
    return counts;
  }, [sources]);

  const viewCounts = useMemo<Record<ShellViewId, number>>(() => {
    const countByCapability = (capability: ConnectorCapability) =>
      filteredSources.filter((source) => sourceCapabilities(source).includes(capability)).length;
    return {
      sources: filteredSources.length,
      syncs: filteredSources.filter((source) =>
        sourceCapabilities(source).some((capability) => ['batch_sync', 'streaming_sync', 'cdc_sync', 'media_sync'].includes(capability)),
      ).length,
      streams: countByCapability('streaming_sync') + countByCapability('cdc_sync'),
      exports: filteredSources.filter((source) =>
        sourceCapabilities(source).some((capability) => ['file_export', 'table_export', 'streaming_export'].includes(capability)),
      ).length,
      webhooks: countByCapability('webhook'),
      'virtual-tables': countByCapability('virtual_table'),
      agents: agents.length,
      health: filteredSources.filter(isRecentFailure).length,
    };
  }, [agents.length, filteredSources]);

  const recentFailures = useMemo(
    () => sources.filter(isRecentFailure).slice(0, 6),
    [sources],
  );

  const discoveredSourceTypes = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return FALLBACK_CONNECTOR_CATALOG.filter((entry) => {
      if (capabilityFilter !== 'all' && !entry.capabilities.includes(capabilityFilter)) return false;
      if (workerFilter !== 'all' && !entry.workers.includes(workerFilter)) return false;
      if (sourceTypeFilter !== 'all' && entry.type !== sourceTypeFilter) return false;
      if (!needle) return true;
      return [entry.name, entry.type, entry.description, ...entry.capabilities.map(capabilityLabel)]
        .some((value) => value.toLowerCase().includes(needle));
    });
  }, [capabilityFilter, query, sourceTypeFilter, workerFilter]);

  function rememberAutoRegistrationLink(link: VirtualTableSourceLink) {
    setAutoRegistrationLinks((prev) => ({ ...prev, [link.source_rid]: link }));
  }

  function markAutoRegistrationDisabled() {
    if (!selectedSource || !selectedProvider) return;
    const current = autoRegistrationLinks[selectedSource.id] ?? emptyVirtualLink(selectedSource.id, selectedProvider);
    setAutoRegistrationLinks((prev) => ({
      ...prev,
      [selectedSource.id]: {
        ...current,
        auto_register_enabled: false,
        auto_register_project_rid: null,
        auto_register_interval_seconds: null,
      },
    }));
  }

  const selectedDetailPath = selectedSource
    ? `/data-connection/sources/${encodeURIComponent(selectedSource.id)}`
    : '/data-connection/new';

  return (
    <section className="of-page" style={{ padding: 24, display: 'grid', gap: 16 }}>
      <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 12 }}>
        <div>
          <h1 className="of-heading-xl">Data Connection</h1>
          <p className="of-text-muted" style={{ marginTop: 4, maxWidth: 860 }}>
            Application shell for Sources, Syncs, Streams, Exports, Webhooks, Virtual Tables, Agents, and Health.
            Sources define the connector, credentials, worker, and network path used by each capability.
          </p>
        </div>
        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
          <Link to="/data-connection/new" className="of-button of-button--primary">New source</Link>
          <Link to={selectedDetailPath} className="of-button" style={{ fontSize: 12 }}>Explore</Link>
          <Link to={selectedDetailPath} className="of-button" style={{ fontSize: 12 }}>Create sync</Link>
          <Link to={selectedDetailPath} className="of-button" style={{ fontSize: 12 }}>Create export</Link>
          <Link to={selectedDetailPath} className="of-button" style={{ fontSize: 12 }}>Create webhook</Link>
          <Link to={selectedProvider ? selectedDetailPath : '/virtual-tables'} className="of-button" style={{ fontSize: 12 }}>
            Register virtual table
          </Link>
        </div>
      </header>

      <nav aria-label="Data Connection views" style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(150px, 1fr))', gap: 8 }}>
        {SHELL_VIEWS.map((item) => (
          <button
            key={item.id}
            type="button"
            onClick={() => setView(item.id)}
            className={view === item.id ? 'of-button of-button--primary' : 'of-button'}
            style={{ display: 'grid', gap: 2, justifyItems: 'start', padding: 12, minHeight: 70 }}
            aria-pressed={view === item.id}
            title={item.description}
          >
            <span style={{ fontWeight: 700 }}>{item.label}</span>
            <span style={{ fontSize: 11, opacity: 0.8 }}>{viewCounts[item.id]} matching</span>
          </button>
        ))}
      </nav>

      {error && (
        <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {error}
        </div>
      )}

      {loading ? (
        <p className="of-text-muted">Loading Data Connection shell…</p>
      ) : (
        <>
          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
            <div style={{ display: 'grid', gridTemplateColumns: 'minmax(220px, 1fr) repeat(auto-fit, minmax(150px, 190px))', gap: 8, alignItems: 'center' }}>
              <input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="Global search: source, connector, worker, owner, capability..."
                className="of-input"
              />
              <select value={capabilityFilter} onChange={(event) => setCapabilityFilter(event.target.value as ConnectorCapability | 'all')} className="of-input">
                <option value="all">All capabilities</option>
                {CAPABILITY_FILTERS.map((capability) => (
                  <option key={capability} value={capability}>{capabilityLabel(capability)} ({capabilityCounts.get(capability) ?? 0})</option>
                ))}
              </select>
              <select value={workerFilter} onChange={(event) => setWorkerFilter(event.target.value as SourceWorker | 'all')} className="of-input">
                <option value="all">All workers</option>
                {WORKER_FILTERS.map((worker) => <option key={worker} value={worker}>{worker} worker</option>)}
              </select>
              <select value={ownerFilter} onChange={(event) => setOwnerFilter(event.target.value)} className="of-input">
                <option value="all">All owners</option>
                {ownerOptions.map((owner) => <option key={owner} value={owner}>{owner}</option>)}
              </select>
              <select value={statusFilter} onChange={(event) => setStatusFilter(event.target.value as SourceStatus | 'all')} className="of-input">
                <option value="all">All statuses</option>
                {SOURCE_STATUSES.map((status) => <option key={status} value={status}>{status} ({statusCounts.get(status) ?? 0})</option>)}
              </select>
              <select value={sourceTypeFilter} onChange={(event) => setSourceTypeFilter(event.target.value)} className="of-input">
                <option value="all">All source types</option>
                {sourceTypeOptions.map((type) => <option key={type} value={type}>{sourceTypeLabel(type)}</option>)}
              </select>
              <button type="button" onClick={() => void load()} className="of-button" style={{ fontSize: 12 }}>
                Refresh
              </button>
            </div>
          </section>

          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
            <header style={{ display: 'flex', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
              <div>
                <p className="of-eyebrow">{SHELL_VIEWS.find((item) => item.id === view)?.label} view</p>
                <h2 style={{ margin: '4px 0 0', fontSize: 18 }}>{viewCounts[view]} matching resource{viewCounts[view] === 1 ? '' : 's'}</h2>
              </div>
              <p className="of-text-muted" style={{ margin: 0, maxWidth: 640, fontSize: 12 }}>
                {SHELL_VIEWS.find((item) => item.id === view)?.description}
              </p>
            </header>
            <ViewPreview view={view} sources={filteredSources} agents={agents} onSelectSource={setSelectedSourceId} />
          </section>

          <div style={{ display: 'grid', gap: 16, gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 420px), 1fr))' }}>
            <section className="of-panel" style={{ padding: 16, minWidth: 0 }}>
              <p className="of-eyebrow">Sources ({filteredSources.length} of {sources.length})</p>
              <ul style={{ marginTop: 8, paddingLeft: 0, listStyle: 'none' }}>
                {filteredSources.map((source) => {
                  const active = source.id === selectedSourceId;
                  const capabilities = sourceCapabilities(source);
                  return (
                    <li key={source.id} style={{ borderBottom: '1px solid var(--border-default)', display: 'grid', gap: 8, padding: '10px 0', background: active ? 'var(--bg-subtle)' : undefined }}>
                      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
                        <Link to={`/data-connection/sources/${encodeURIComponent(source.id)}`} style={sourceRowLinkStyle}>
                          <strong style={{ color: 'var(--text-primary)' }}>{source.name}</strong>
                          <span className="of-text-muted" style={{ fontSize: 11, overflowWrap: 'anywhere' }}>
                            {sourceTypeLabel(source.connector_type)} · worker: {source.worker} · owner: {ownerLabel(source)} · last_sync: {source.last_sync_at ?? '—'}
                          </span>
                        </Link>
                        <span style={{ fontSize: 11, padding: '2px 10px', borderRadius: 999, background: STATUS_COLOR[source.status], color: '#fff' }}>
                          {source.status}
                        </span>
                        <button type="button" onClick={() => setSelectedSourceId(source.id)} className={active ? 'of-button of-button--primary' : 'of-button'} style={{ fontSize: 11 }} aria-pressed={active}>
                          Browse
                        </button>
                        <button type="button" onClick={() => void handleDelete(source.id)} disabled={busyId === source.id} className="of-button" style={{ fontSize: 11, color: '#b91c1c', borderColor: '#fecaca' }}>
                          {busyId === source.id ? 'Deleting...' : 'Delete'}
                        </button>
                      </div>
                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4, padding: '0 12px 2px' }}>
                        {capabilities.map((capability) => <span key={capability} style={capabilityChipStyle}>{capabilityLabel(capability)}</span>)}
                      </div>
                    </li>
                  );
                })}
                {filteredSources.length === 0 && <li className="of-text-muted" style={{ padding: 12 }}>No sources match the current filters.</li>}
              </ul>
            </section>

            <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12, alignSelf: 'start', minWidth: 0 }}>
              <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 12 }}>
                <div>
                  <p className="of-eyebrow">Source-type discovery</p>
                  <h2 style={{ margin: '4px 0 0', fontSize: 18 }}>{discoveredSourceTypes.length} connector cards</h2>
                  <p className="of-text-muted" style={{ marginTop: 4, fontSize: 12 }}>
                    Search by connector name or capability before creating a source.
                  </p>
                </div>
                <Link to="/data-connection/new" className="of-button" style={{ fontSize: 12 }}>Open gallery</Link>
              </header>
              <div style={{ display: 'grid', gap: 8 }}>
                {discoveredSourceTypes.slice(0, 6).map((entry) => (
                  <div key={entry.type} style={{ border: '1px solid var(--border-default)', borderRadius: 'var(--radius-md)', padding: 10, display: 'grid', gap: 6 }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
                      <strong>{entry.name}</strong>
                      <span className="of-text-muted" style={{ fontSize: 11 }}>{entry.available ? 'Available' : 'Coming soon'}</span>
                    </div>
                    <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>{entry.description}</p>
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
                      {entry.capabilities.map((capability) => <span key={capability} style={capabilityChipStyle}>{capabilityLabel(capability)}</span>)}
                    </div>
                  </div>
                ))}
              </div>
            </section>

            <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12, alignSelf: 'start', minWidth: 0 }}>
              <header>
                <p className="of-eyebrow">Recent failures</p>
                <h2 style={{ margin: '4px 0 0', fontSize: 18 }}>{recentFailures.length} degraded or failed source{recentFailures.length === 1 ? '' : 's'}</h2>
              </header>
              {recentFailures.length > 0 ? (
                <div style={{ display: 'grid', gap: 8 }}>
                  {recentFailures.map((source) => (
                    <Link key={source.id} to={`/data-connection/sources/${encodeURIComponent(source.id)}`} style={{ color: 'inherit', textDecoration: 'none', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-md)', padding: 10 }}>
                      <strong>{source.name}</strong>
                      <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
                        {source.status} · {sourceTypeLabel(source.connector_type)} · updated {source.updated_at}
                      </p>
                    </Link>
                  ))}
                </div>
              ) : (
                <p className="of-text-muted" style={{ margin: 0, fontSize: 13 }}>No current failures in the loaded source set.</p>
              )}
            </section>

            <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12, alignSelf: 'start', minWidth: 0 }}>
              <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 12 }}>
                <div>
                  <p className="of-eyebrow">Remote catalog</p>
                  <h2 style={{ margin: '4px 0 0', fontSize: 18 }}>{selectedSource?.name ?? 'No source selected'}</h2>
                  {selectedSource && (
                    <p className="of-text-muted" style={{ marginTop: 4, fontSize: 12 }}>
                      {selectedCatalogEntry?.name ?? selectedSource.connector_type}
                      {selectedProvider ? ` · ${providerLabel(selectedProvider)}` : ''}
                    </p>
                  )}
                </div>
                {selectedSource && <Link to={`/data-connection/sources/${encodeURIComponent(selectedSource.id)}`} className="of-button" style={{ fontSize: 12 }}>Open detail</Link>}
              </header>

              {selectedSource ? (
                selectedProvider && selectedVirtualLink ? (
                  <>
                    {selectedCatalogEntry && (
                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
                        {selectedCatalogEntry.capabilities.map((capability) => <span key={capability} style={capabilityChipStyle}>{capabilityLabel(capability)}</span>)}
                      </div>
                    )}
                    <RemoteCatalogBrowser sourceRid={selectedSource.id} />
                    <AutoRegistrationCard sourceRid={selectedSource.id} provider={selectedProvider} link={selectedVirtualLink} onOpenWizard={() => setAutoRegistrationOpen(true)} onChanged={rememberAutoRegistrationLink} onDisabled={markAutoRegistrationDisabled} />
                    <CreateAutoRegistrationModal open={autoRegistrationOpen} sourceRid={selectedSource.id} provider={selectedProvider} onClose={() => setAutoRegistrationOpen(false)} onEnabled={rememberAutoRegistrationLink} />
                  </>
                ) : (
                  <div className="of-text-muted" style={{ fontSize: 13 }}>
                    Remote catalog browsing is available for virtual-table-capable sources.
                  </div>
                )
              ) : (
                <div className="of-text-muted" style={{ fontSize: 13 }}>Create a source or select one from the list.</div>
              )}
            </section>
          </div>
        </>
      )}
    </section>
  );
}

function ViewPreview({
  view,
  sources,
  agents,
  onSelectSource,
}: {
  view: ShellViewId;
  sources: SourceWithOwner[];
  agents: ConnectorAgent[];
  onSelectSource: (id: string) => void;
}) {
  if (view === 'agents') {
    return (
      <div style={previewGridStyle}>
        {agents.slice(0, 4).map((agent) => (
          <Link key={agent.id} to="/data-connection/agents" style={previewCardLinkStyle}>
            <strong>{agent.name}</strong>
            <span className="of-text-muted" style={{ fontSize: 12 }}>{agent.status} · owner {agent.owner_id}</span>
          </Link>
        ))}
        {agents.length === 0 && <p className="of-text-muted" style={{ margin: 0 }}>No agents are registered yet.</p>}
      </div>
    );
  }

  const capabilityByView: Partial<Record<ShellViewId, ConnectorCapability[]>> = {
    syncs: ['batch_sync', 'streaming_sync', 'cdc_sync', 'media_sync'],
    streams: ['streaming_sync', 'cdc_sync'],
    exports: ['file_export', 'table_export', 'streaming_export'],
    webhooks: ['webhook'],
    'virtual-tables': ['virtual_table'],
  };
  const sourceList = view === 'health'
    ? sources.filter(isRecentFailure)
    : sources.filter((source) => {
      const needed = capabilityByView[view];
      if (!needed) return true;
      return sourceCapabilities(source).some((capability) => needed.includes(capability));
    });

  return (
    <div style={previewGridStyle}>
      {sourceList.slice(0, 5).map((source) => (
        <button key={source.id} type="button" onClick={() => onSelectSource(source.id)} className="of-button" style={{ ...previewCardButtonStyle, borderColor: view === 'health' ? STATUS_COLOR[source.status] : undefined }}>
          <strong>{source.name}</strong>
          <span className="of-text-muted" style={{ fontSize: 12 }}>{sourceTypeLabel(source.connector_type)} · {source.status}</span>
        </button>
      ))}
      {sourceList.length === 0 && <p className="of-text-muted" style={{ margin: 0 }}>No matching resources for this view.</p>}
    </div>
  );
}

const sourceRowLinkStyle: CSSProperties = {
  display: 'grid',
  gap: 4,
  minWidth: 0,
  padding: '0 12px',
  textDecoration: 'none',
  flex: '1 1 260px',
};

const capabilityChipStyle: CSSProperties = {
  fontSize: 10,
  padding: '2px 6px',
  background: 'var(--bg-subtle)',
  borderRadius: 999,
};

const previewGridStyle: CSSProperties = {
  display: 'grid',
  gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))',
  gap: 8,
};

const previewCardButtonStyle: CSSProperties = {
  display: 'grid',
  gap: 4,
  justifyItems: 'start',
  padding: 12,
  textAlign: 'left',
};

const previewCardLinkStyle: CSSProperties = {
  ...previewCardButtonStyle,
  color: 'inherit',
  textDecoration: 'none',
  border: '1px solid var(--border-default)',
  borderRadius: 'var(--radius-md)',
};
