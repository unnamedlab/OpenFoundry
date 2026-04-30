import api from './client';

// ---------------------------------------------------------------------------
// Catalog
// ---------------------------------------------------------------------------

/**
 * Capabilities a connector can support. Mirrors the Foundry "Data Connection"
 * core concepts page; only a subset is wired in the MVP backend.
 */
export type ConnectorCapability =
  | 'batch_sync'
  | 'streaming_sync'
  | 'cdc_sync'
  | 'media_sync'
  | 'hyperauto'
  | 'file_export'
  | 'table_export'
  | 'streaming_export'
  | 'webhook'
  | 'virtual_table'
  | 'virtual_media'
  | 'exploration'
  | 'use_in_code';

export type SourceWorker = 'foundry' | 'agent';

/**
 * Available connector type listed in the gallery. `available: false` means we
 * advertise the connector but do not allow source creation yet — explicit
 * about the MVP scope so we don't promise capabilities we don't ship.
 */
export interface ConnectorCatalogEntry {
  type: string;
  name: string;
  description: string;
  capabilities: ConnectorCapability[];
  workers: SourceWorker[];
  available: boolean;
}

export interface ConnectorCatalog {
  connectors: ConnectorCatalogEntry[];
}

// ---------------------------------------------------------------------------
// Sources
// ---------------------------------------------------------------------------

export type SourceStatus =
  | 'draft'
  | 'configuring'
  | 'healthy'
  | 'degraded'
  | 'error';

export interface Source {
  id: string;
  name: string;
  connector_type: string;
  worker: SourceWorker;
  status: SourceStatus;
  last_sync_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface SourceListResponse {
  data: Source[];
  total: number;
  page: number;
  per_page: number;
}

export interface CreateSourceRequest {
  name: string;
  connector_type: string;
  worker?: SourceWorker;
  config?: Record<string, unknown>;
}

export interface UpdateSourceRequest {
  name?: string;
  worker?: SourceWorker;
  config?: Record<string, unknown>;
}

// ---------------------------------------------------------------------------
// Credentials
// ---------------------------------------------------------------------------

export type CredentialKind =
  | 'password'
  | 'api_key'
  | 'oauth_token'
  | 'aws_keys'
  | 'service_account_json';

export interface Credential {
  id: string;
  source_id: string;
  kind: CredentialKind;
  // The raw secret is never returned by the API; only a non-reversible
  // fingerprint useful for "you stored a secret on YYYY-MM-DD" UI.
  fingerprint: string;
  created_at: string;
}

export interface SetCredentialRequest {
  kind: CredentialKind;
  // Only sent on POST/PUT, never received.
  value: string;
}

// ---------------------------------------------------------------------------
// Egress policies
// ---------------------------------------------------------------------------

export type EgressEndpointKind = 'host' | 'ip' | 'cidr';
export type EgressPortKind = 'single' | 'range' | 'any';

export interface EgressEndpoint {
  kind: EgressEndpointKind;
  value: string;
}

export interface EgressPort {
  kind: EgressPortKind;
  // For 'single' value is "443"; for 'range' value is "8000-9000"; ignored for 'any'.
  value: string;
}

export type EgressPolicyKind = 'direct' | 'agent_proxy';

export interface NetworkEgressPolicy {
  id: string;
  name: string;
  description: string;
  kind: EgressPolicyKind;
  address: EgressEndpoint;
  port: EgressPort;
  is_global: boolean;
  // Permissions are modelled as opaque marking / group identifiers; the
  // authorization-policy-service is responsible for resolving them.
  permissions: string[];
  created_at: string;
}

export interface CreateEgressPolicyRequest {
  name: string;
  description: string;
  kind: EgressPolicyKind;
  address: EgressEndpoint;
  port: EgressPort;
  is_global: boolean;
  permissions: string[];
}

export interface SourcePolicyBinding {
  source_id: string;
  policy_id: string;
  kind: EgressPolicyKind;
}

// ---------------------------------------------------------------------------
// Batch sync defs and runs
// ---------------------------------------------------------------------------

export type SyncRunStatus = 'pending' | 'running' | 'succeeded' | 'failed' | 'aborted';

export interface BatchSyncDef {
  id: string;
  source_id: string;
  output_dataset_id: string;
  file_glob: string | null;
  schedule_cron: string | null;
  created_at: string;
}

export interface CreateBatchSyncRequest {
  source_id: string;
  output_dataset_id: string;
  file_glob?: string;
  schedule_cron?: string;
}

export interface SyncRun {
  id: string;
  sync_def_id: string;
  status: SyncRunStatus;
  started_at: string;
  finished_at: string | null;
  bytes_written: number;
  files_written: number;
  error: string | null;
}

export interface TestConnectionResult {
  success: boolean;
  message: string;
  latency_ms: number | null;
}

// ---------------------------------------------------------------------------
// REST surface
// ---------------------------------------------------------------------------

const BASE = '/data-connection';

export const dataConnection = {
  // Catalog ----------------------------------------------------------------
  getCatalog(): Promise<ConnectorCatalog> {
    return api.get(`${BASE}/catalog`);
  },

  // Sources ----------------------------------------------------------------
  listSources(params: { page?: number; per_page?: number } = {}): Promise<SourceListResponse> {
    const search = new URLSearchParams();
    if (params.page) search.set('page', String(params.page));
    if (params.per_page) search.set('per_page', String(params.per_page));
    const query = search.toString();
    return api.get(`${BASE}/sources${query ? `?${query}` : ''}`);
  },
  getSource(id: string): Promise<Source> {
    return api.get(`${BASE}/sources/${id}`);
  },
  createSource(body: CreateSourceRequest): Promise<Source> {
    return api.post(`${BASE}/sources`, body);
  },
  updateSource(id: string, body: UpdateSourceRequest): Promise<Source> {
    return api.patch(`${BASE}/sources/${id}`, body);
  },
  deleteSource(id: string): Promise<void> {
    return api.delete(`${BASE}/sources/${id}`);
  },
  testConnection(id: string): Promise<TestConnectionResult> {
    return api.post(`${BASE}/sources/${id}/test-connection`, {});
  },

  // Credentials ------------------------------------------------------------
  setCredential(sourceId: string, body: SetCredentialRequest): Promise<Credential> {
    return api.post(`${BASE}/sources/${sourceId}/credentials`, body);
  },
  listCredentials(sourceId: string): Promise<Credential[]> {
    return api.get(`${BASE}/sources/${sourceId}/credentials`);
  },

  // Egress policy bindings -------------------------------------------------
  listSourcePolicies(sourceId: string): Promise<NetworkEgressPolicy[]> {
    return api.get(`${BASE}/sources/${sourceId}/egress-policies`);
  },
  attachPolicy(sourceId: string, policyId: string, kind: EgressPolicyKind = 'direct'): Promise<SourcePolicyBinding> {
    return api.post(`${BASE}/sources/${sourceId}/egress-policies`, { policy_id: policyId, kind });
  },
  detachPolicy(sourceId: string, policyId: string): Promise<void> {
    return api.delete(`${BASE}/sources/${sourceId}/egress-policies/${policyId}`);
  },

  // Egress policies (global) -----------------------------------------------
  listEgressPolicies(): Promise<NetworkEgressPolicy[]> {
    return api.get(`${BASE}/egress-policies`);
  },
  createEgressPolicy(body: CreateEgressPolicyRequest): Promise<NetworkEgressPolicy> {
    return api.post(`${BASE}/egress-policies`, body);
  },
  deleteEgressPolicy(id: string): Promise<void> {
    return api.delete(`${BASE}/egress-policies/${id}`);
  },

  // Batch syncs ------------------------------------------------------------
  listSyncs(sourceId: string): Promise<BatchSyncDef[]> {
    return api.get(`${BASE}/sources/${sourceId}/syncs`);
  },
  createSync(body: CreateBatchSyncRequest): Promise<BatchSyncDef> {
    return api.post(`${BASE}/syncs`, body);
  },
  runSync(syncId: string): Promise<SyncRun> {
    return api.post(`${BASE}/syncs/${syncId}/run`, {});
  },
  listRuns(syncId: string): Promise<SyncRun[]> {
    return api.get(`${BASE}/syncs/${syncId}/runs`);
  },
};

// ---------------------------------------------------------------------------
// Static catalog used as a fallback when the backend is not yet wired.
// Keeping it client-side makes the gallery render even before the
// connector-management-service exposes /catalog. The list mirrors the
// "MVP-real" subset (S3, REST API, Postgres) plus advertised "coming soon".
// ---------------------------------------------------------------------------

export const FALLBACK_CONNECTOR_CATALOG: ConnectorCatalogEntry[] = [
  {
    type: 's3',
    name: 'Amazon S3',
    description: 'Sync files from an S3 bucket as a file batch sync.',
    capabilities: ['batch_sync', 'file_export', 'exploration'],
    workers: ['foundry'],
    available: true,
  },
  {
    type: 'rest_api',
    name: 'REST API',
    description: 'Generic REST endpoint with configurable authentication.',
    capabilities: ['batch_sync', 'webhook', 'use_in_code'],
    workers: ['foundry'],
    available: true,
  },
  {
    type: 'postgresql',
    name: 'PostgreSQL',
    description: 'Relational database table batch syncs and exploration.',
    capabilities: ['batch_sync', 'cdc_sync', 'table_export', 'exploration'],
    workers: ['foundry'],
    available: true,
  },
  {
    type: 'mssql',
    name: 'Microsoft SQL Server',
    description: 'Coming soon.',
    capabilities: ['batch_sync', 'cdc_sync', 'table_export'],
    workers: ['foundry'],
    available: false,
  },
  {
    type: 'sftp',
    name: 'SFTP',
    description: 'Coming soon.',
    capabilities: ['batch_sync', 'file_export'],
    workers: ['foundry'],
    available: false,
  },
  {
    type: 'salesforce',
    name: 'Salesforce',
    description: 'Coming soon.',
    capabilities: ['batch_sync', 'table_export'],
    workers: ['foundry'],
    available: false,
  },
  {
    type: 'snowflake',
    name: 'Snowflake',
    description: 'Coming soon.',
    capabilities: ['virtual_table', 'batch_sync', 'table_export'],
    workers: ['foundry'],
    available: false,
  },
  {
    type: 'kafka',
    name: 'Apache Kafka',
    description: 'Coming soon.',
    capabilities: ['streaming_sync', 'streaming_export'],
    workers: ['foundry'],
    available: false,
  },
  {
    type: 'sap',
    name: 'SAP',
    description: 'Coming soon.',
    capabilities: ['hyperauto', 'batch_sync'],
    workers: ['foundry'],
    available: false,
  },
];

/**
 * Filter the catalog by free-text query, matching connector name/type or any
 * capability tag (so a search for "virtual" surfaces Snowflake, mirroring the
 * Foundry docs example).
 */
export function filterCatalog(
  catalog: ConnectorCatalogEntry[],
  query: string,
): ConnectorCatalogEntry[] {
  const q = query.trim().toLowerCase();
  if (!q) return catalog;
  return catalog.filter((entry) => {
    if (entry.type.toLowerCase().includes(q)) return true;
    if (entry.name.toLowerCase().includes(q)) return true;
    if (entry.capabilities.some((cap) => cap.toLowerCase().includes(q))) return true;
    return false;
  });
}

/**
 * Human label for a capability tag. Used for the chips on connector cards
 * and in the source detail capabilities tab.
 */
export function capabilityLabel(capability: ConnectorCapability): string {
  switch (capability) {
    case 'batch_sync':
      return 'Batch sync';
    case 'streaming_sync':
      return 'Streaming sync';
    case 'cdc_sync':
      return 'CDC sync';
    case 'media_sync':
      return 'Media sync';
    case 'hyperauto':
      return 'HyperAuto';
    case 'file_export':
      return 'File export';
    case 'table_export':
      return 'Table export';
    case 'streaming_export':
      return 'Streaming export';
    case 'webhook':
      return 'Webhook';
    case 'virtual_table':
      return 'Virtual table';
    case 'virtual_media':
      return 'Virtual media';
    case 'exploration':
      return 'Exploration';
    case 'use_in_code':
      return 'Use in code';
  }
}

export default dataConnection;
