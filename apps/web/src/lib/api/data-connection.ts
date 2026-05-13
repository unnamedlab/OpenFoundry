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

export type ConnectorCategory =
  | 'databases'
  | 'filesystems_blob_stores'
  | 'event_streams'
  | 'message_queues'
  | 'rest_apis'
  | 'productivity_tools'
  | 'saas_applications'
  | 'geospatial_systems'
  | 'media_sources'
  | 'generic_connectors';

export type ConnectorCredentialKind =
  | 'none'
  | 'username_password'
  | 'api_key'
  | 'bearer_token'
  | 'oauth_client'
  | 'cloud_identity'
  | 'service_account_json'
  | 'certificate_key'
  | 'connector_specific';

export interface ConnectorCredentialField {
  key: string;
  label: string;
  kind: ConnectorCredentialKind;
  required: boolean;
  secret: boolean;
  description?: string;
}

export type ConnectorNetworkMode = 'direct_egress' | 'agent_proxy' | 'agent_worker' | 'public_internet' | 'listener';

export interface ConnectorNetworkRequirement {
  modes: ConnectorNetworkMode[];
  defaultPorts: number[];
  privateNetworkSupported: boolean;
  notes: string;
}

export interface ConnectorFeatureFlags {
  supportsDiscovery: boolean;
  supportsConnectionTest: boolean;
  supportsIncrementalSync: boolean;
  supportsStreaming: boolean;
  supportsVirtualTables: boolean;
  supportsExports: boolean;
  supportsWebhooks: boolean;
  supportsMedia: boolean;
}

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
  workerCapabilities?: Partial<Record<SourceWorker, ConnectorCapability[]>>;
  available: boolean;
  category: ConnectorCategory;
  credentialFields: ConnectorCredentialField[];
  network: ConnectorNetworkRequirement;
  setupDocsUrl: string;
  featureFlags: ConnectorFeatureFlags;
  /**
   * Legacy UI grouping retained for backend responses that still send the
   * earlier family shape. Prefer `category` for new source-type registry UI.
   */
  family?: ConnectorFamily;
}

export type ConnectorFamily =
  | 'Storage'
  | 'Streaming'
  | 'SaaS'
  | 'RDBMS'
  | 'API';

export const CONNECTOR_FAMILY_ORDER: ConnectorFamily[] = [
  'Storage',
  'RDBMS',
  'Streaming',
  'SaaS',
  'API',
];

export const CONNECTOR_CATEGORY_ORDER: ConnectorCategory[] = [
  'databases',
  'filesystems_blob_stores',
  'event_streams',
  'message_queues',
  'rest_apis',
  'productivity_tools',
  'saas_applications',
  'geospatial_systems',
  'media_sources',
  'generic_connectors',
];

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

export interface SourceHealthSummary {
  state: SourceStatus;
  last_checked_at: string | null;
  recent_failures: number;
  message: string | null;
}

export interface SourceUsageSummary {
  sync_count: number;
  export_count: number;
  webhook_count: number;
  virtual_table_count: number;
  code_import_count: number;
  last_used_at: string | null;
}

export interface SourceAuditMetadata {
  created_by: string | null;
  updated_by: string | null;
  archived_by?: string | null;
  archived_at?: string | null;
  last_event_id?: string | null;
}

export interface Source {
  id: string;
  name: string;
  description?: string | null;
  connector_type: string;
  project_rid?: string | null;
  folder_rid?: string | null;
  owner_id?: string | null;
  owner_name?: string | null;
  organization_id?: string | null;
  worker: SourceWorker;
  status: SourceStatus;
  network_policy_id?: string | null;
  credential_reference_ids?: string[] | null;
  default_output_location?: string | null;
  supported_capabilities?: ConnectorCapability[] | null;
  health?: SourceHealthSummary | null;
  usage?: SourceUsageSummary | null;
  audit?: SourceAuditMetadata | null;
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
  description?: string;
  project_rid?: string;
  folder_rid?: string;
  owner_id?: string;
  default_output_location?: string;
  config?: Record<string, unknown>;
}

export interface UpdateSourceRequest {
  name?: string;
  description?: string | null;
  worker?: SourceWorker;
  project_rid?: string | null;
  folder_rid?: string | null;
  owner_id?: string | null;
  default_output_location?: string | null;
  config?: Record<string, unknown>;
}

export interface DuplicateSourceRequest {
  name: string;
  description?: string;
  project_rid?: string;
  folder_rid?: string;
  copy_credentials?: boolean;
  copy_network_policies?: boolean;
}

export interface ArchiveSourceRequest {
  reason?: string;
}

// ---------------------------------------------------------------------------
// Credentials
// ---------------------------------------------------------------------------

export type CredentialKind =
  | 'username_password'
  | 'password'
  | 'api_key'
  | 'bearer_token'
  | 'oauth_client_secret'
  | 'oauth_token'
  | 'cloud_identity'
  | 'certificate_key'
  | 'connector_specific'
  | 'aws_keys'
  | 'service_account_json';

export type CredentialStorageMode = 'encrypted_secret' | 'external_secret_reference' | 'cloud_identity_reference';
export type CredentialTestStatus = 'untested' | 'passed' | 'failed' | 'expired';
export type CredentialAuditEventType = 'created' | 'rotated' | 'tested' | 'attached' | 'detached' | 'revoked';

export interface CredentialUsageSummary {
  source_count: number;
  last_used_at: string | null;
  source_ids: string[];
}

export interface CredentialAuditEvent {
  id: string;
  event_type: CredentialAuditEventType | string;
  actor_id: string | null;
  created_at: string;
  message: string;
}

export interface Credential {
  id: string;
  source_id: string;
  kind: CredentialKind;
  storage_mode?: CredentialStorageMode;
  external_secret_ref?: string | null;
  cloud_identity_ref?: string | null;
  // The raw secret is never returned by the API; only a non-reversible
  // fingerprint useful for "you stored a secret on YYYY-MM-DD" UI.
  fingerprint: string;
  secret_version?: string | null;
  last_rotated_at?: string | null;
  created_by?: string | null;
  test_status?: CredentialTestStatus;
  last_tested_at?: string | null;
  usage?: CredentialUsageSummary | null;
  audit_events?: CredentialAuditEvent[] | null;
  created_at: string;
}

export interface SetCredentialRequest {
  kind: CredentialKind;
  storage_mode?: CredentialStorageMode;
  external_secret_ref?: string;
  cloud_identity_ref?: string;
  secret_version?: string;
  // Only sent on POST/PUT, never received.
  value?: string;
}

export interface RotateCredentialRequest extends SetCredentialRequest {
  rotation_reason?: string;
}

export interface TestCredentialResult {
  status: CredentialTestStatus;
  message: string;
  tested_at: string;
}

// ---------------------------------------------------------------------------
// Connector agents
// ---------------------------------------------------------------------------

export interface ConnectorAgent {
  id: string;
  name: string;
  agent_url: string;
  owner_id: string;
  status: string;
  capabilities: Record<string, unknown>;
  metadata: Record<string, unknown>;
  last_heartbeat_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface RegisterConnectorAgentRequest {
  name: string;
  agent_url: string;
  capabilities?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
}

export interface ConnectorAgentHeartbeatRequest {
  capabilities?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
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
export type EgressProtocol = 'tcp' | 'tls' | 'http' | 'https';
export type EgressPolicyStatus = 'draft' | 'pending_review' | 'active' | 'revoked' | 'failed';
export type AgentProxyMode = 'none' | 'http_connect' | 'socks5' | 'mtls_tunnel';

export interface NetworkEgressPolicy {
  id: string;
  name: string;
  description: string;
  kind: EgressPolicyKind;
  address: EgressEndpoint;
  port: EgressPort;
  protocol?: EgressProtocol;
  proxy_mode?: AgentProxyMode;
  status?: EgressPolicyStatus;
  allowed_organizations?: string[];
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
  protocol?: EgressProtocol;
  proxy_mode?: AgentProxyMode;
  status?: EgressPolicyStatus;
  allowed_organizations?: string[];
  is_global: boolean;
  permissions: string[];
}

export interface SourcePolicyBinding {
  source_id: string;
  policy_id: string;
  kind: EgressPolicyKind;
}


export interface EgressPolicyValidationIssue {
  field: string;
  message: string;
  severity: 'error' | 'warning';
}

const HOST_PATTERN = /^(?:\*\.)?(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/i;
const IPV4_PATTERN = /^(?:(?:25[0-5]|2[0-4]\d|1?\d?\d)\.){3}(?:25[0-5]|2[0-4]\d|1?\d?\d)$/;
const CIDR_PATTERN = /^(?:(?:25[0-5]|2[0-4]\d|1?\d?\d)\.){3}(?:25[0-5]|2[0-4]\d|1?\d?\d)\/(?:[0-9]|[1-2]\d|3[0-2])$/;

function validateEndpoint(endpoint: EgressEndpoint): EgressPolicyValidationIssue[] {
  const value = endpoint.value.trim();
  if (!value) return [{ field: 'address', message: 'Destination address is required.', severity: 'error' }];
  if (endpoint.kind === 'host' && !HOST_PATTERN.test(value)) {
    return [{ field: 'address', message: 'Host policies require a DNS name such as api.example.com or *.example.com.', severity: 'error' }];
  }
  if (endpoint.kind === 'ip' && !IPV4_PATTERN.test(value)) {
    return [{ field: 'address', message: 'IP policies require an IPv4 address such as 10.20.30.40.', severity: 'error' }];
  }
  if (endpoint.kind === 'cidr' && !CIDR_PATTERN.test(value)) {
    return [{ field: 'address', message: 'CIDR policies require an IPv4 CIDR block such as 10.20.0.0/16.', severity: 'error' }];
  }
  return [];
}

function validatePort(port: EgressPort): EgressPolicyValidationIssue[] {
  if (port.kind === 'any') return [];
  if (port.kind === 'single') {
    const parsed = Number(port.value);
    if (!Number.isInteger(parsed) || parsed < 1 || parsed > 65535) {
      return [{ field: 'port', message: 'Port must be a number between 1 and 65535.', severity: 'error' }];
    }
    return [];
  }
  const match = port.value.trim().match(/^(\d{1,5})\s*-\s*(\d{1,5})$/);
  if (!match) return [{ field: 'port', message: 'Port range must look like 8000-9000.', severity: 'error' }];
  const start = Number(match[1]);
  const end = Number(match[2]);
  if (!Number.isInteger(start) || !Number.isInteger(end) || start < 1 || end > 65535 || start > end) {
    return [{ field: 'port', message: 'Port range must be between 1 and 65535, with the lower port first.', severity: 'error' }];
  }
  return [];
}

export function validateEgressPolicy(policy: Pick<NetworkEgressPolicy, 'kind' | 'address' | 'port'> & Partial<Pick<NetworkEgressPolicy, 'protocol' | 'proxy_mode' | 'status' | 'allowed_organizations'>>): EgressPolicyValidationIssue[] {
  const issues = [...validateEndpoint(policy.address), ...validatePort(policy.port)];
  if (policy.kind === 'direct' && policy.proxy_mode && policy.proxy_mode !== 'none') {
    issues.push({ field: 'proxy_mode', message: 'Direct egress policies cannot use an agent proxy mode.', severity: 'error' });
  }
  if (policy.kind === 'agent_proxy' && (!policy.proxy_mode || policy.proxy_mode === 'none')) {
    issues.push({ field: 'proxy_mode', message: 'Agent proxy policies require an HTTP CONNECT, SOCKS5, or mTLS tunnel mode.', severity: 'error' });
  }
  if (policy.protocol && !['tcp', 'tls', 'http', 'https'].includes(policy.protocol)) {
    issues.push({ field: 'protocol', message: 'Protocol must be tcp, tls, http, or https.', severity: 'error' });
  }
  if (policy.status && !['draft', 'pending_review', 'active', 'revoked', 'failed'].includes(policy.status)) {
    issues.push({ field: 'status', message: 'Policy status is not recognized.', severity: 'error' });
  }
  for (const organization of policy.allowed_organizations ?? []) {
    if (!organization.trim()) {
      issues.push({ field: 'allowed_organizations', message: 'Allowed organization identifiers cannot be blank.', severity: 'error' });
      break;
    }
  }
  return issues;
}

export interface ConnectionTestPolicyValidationOptions {
  expectedKind: EgressPolicyKind;
  organizationId?: string | null;
}

export function validateEgressPoliciesForConnectionTest(
  policies: NetworkEgressPolicy[],
  { expectedKind, organizationId }: ConnectionTestPolicyValidationOptions,
): EgressPolicyValidationIssue[] {
  if (policies.length === 0) {
    return [{ field: 'policy', message: 'Attach an active egress policy before testing this source.', severity: 'error' }];
  }

  const issues: EgressPolicyValidationIssue[] = [];
  const matching = policies.filter((policy) => policy.kind === expectedKind);
  if (matching.length === 0) {
    issues.push({
      field: 'kind',
      message: `Attach an ${expectedKind === 'direct' ? 'active direct egress' : 'active agent proxy'} policy before testing this source.`,
      severity: 'error',
    });
  }

  for (const policy of matching) {
    const policyIssues = validateEgressPolicy(policy);
    const status = policy.status ?? 'active';
    if (status !== 'active') {
      policyIssues.push({ field: 'status', message: `Policy "${policy.name}" must be active before connection tests can run. Current status: ${status}.`, severity: 'error' });
    }
    if (organizationId && (policy.allowed_organizations ?? []).length > 0 && !policy.allowed_organizations?.includes(organizationId)) {
      policyIssues.push({ field: 'allowed_organizations', message: `Policy "${policy.name}" does not allow organization ${organizationId}.`, severity: 'error' });
    }
    issues.push(...policyIssues.map((issue) => ({ ...issue, field: `${policy.name}.${issue.field}` })));
  }

  if (matching.length > 0 && matching.every((policy) => (policy.status ?? 'active') !== 'active')) {
    issues.push({ field: 'status', message: 'At least one matching egress policy must be active before testing.', severity: 'error' });
  }

  return issues;
}

// ---------------------------------------------------------------------------
// Batch sync defs and runs
// ---------------------------------------------------------------------------

export type SyncRunStatus = 'queued' | 'pending' | 'running' | 'succeeded' | 'failed' | 'cancelled' | 'aborted' | 'retrying' | 'ignored' | 'partially_succeeded';

export type SyncCapabilityType = 'batch_sync' | 'streaming_sync' | 'cdc_sync' | 'media_sync';
export type SyncOutputKind = 'dataset' | 'stream' | 'media_set';
export type SyncWriteMode = 'snapshot' | 'append' | 'upsert' | 'incremental';
export type SyncTransactionMode = 'transactional' | 'external_checkpoint' | 'non_transactional';
export type DatasetTransactionType = 'SNAPSHOT' | 'APPEND' | 'UPDATE';
export type SyncResourceHealthState = 'not_run' | 'healthy' | 'warning' | 'error';
export type FileSyncMode = 'snapshot_mirror' | 'incremental_append' | 'historical_snapshot_incremental';
export type TableBatchSyncMode = 'full_snapshot' | 'incremental';

export interface SyncResourceHealth {
  state: SyncResourceHealthState;
  message: string | null;
  last_checked_at: string | null;
}

export interface SyncRunLogEntry {
  timestamp: string;
  level: 'debug' | 'info' | 'warn' | 'error' | 'fatal';
  message: string;
}

export interface SyncRunBuildLink {
  build_id: string | null;
  job_id: string | null;
  job_spec_id?: string | null;
  build_url?: string | null;
}

export interface SyncRunSourceProgress {
  offsets?: Record<string, unknown> | null;
  file_checkpoints?: string[] | null;
}

export interface SyncRunOutputTransaction {
  transaction_id: string | null;
  transaction_type: DatasetTransactionType | null;
  dataset_id?: string | null;
  stream_id?: string | null;
}

export interface SyncResourceRunSummary {
  status: SyncRunStatus;
  started_at: string | null;
  finished_at: string | null;
  duration_ms?: number | null;
  worker?: SourceWorker | null;
  agent_id?: string | null;
  build?: SyncRunBuildLink | null;
  source_progress?: SyncRunSourceProgress | null;
  output_transaction?: SyncRunOutputTransaction | null;
  rows_written?: number | null;
  files_written?: number | null;
  bytes_written: number;
  records_written?: number | null;
  retry_count?: number;
  logs?: SyncRunLogEntry[];
  error: string | null;
}

export interface SyncResourceSchemaField {
  name: string;
  source_type: string;
  foundry_type: string;
  nullable: boolean;
}

export interface SyncValidationWarning {
  code: string;
  message: string;
  severity: 'warning' | 'error';
}

export interface FileSyncSettings {
  mode: FileSyncMode;
  transaction_type: DatasetTransactionType;
  exclude_already_synced: boolean;
  file_count_limit: number | null;
  include_globs: string[];
  exclude_globs: string[];
  include_path_metadata: boolean;
  path_metadata_columns: string[];
  historical_snapshot_cutoff?: string | null;
  incremental_recent_window?: string | null;
  low_level?: Record<string, unknown> | null;
  warnings?: SyncValidationWarning[];
}

export interface TableBatchSyncSelection {
  source_table: string;
  destination_dataset_id: string;
  source_schema?: SyncResourceSchemaField[] | null;
  destination_schema?: SyncResourceSchemaField[] | null;
  estimated_row_count?: number | null;
  incremental_column?: string | null;
  last_transaction_id?: string | null;
}

export interface TableBatchSyncSettings {
  mode: TableBatchSyncMode;
  selected_tables: TableBatchSyncSelection[];
  infer_schema: boolean;
  incremental_column?: string | null;
  row_count?: number | null;
  transaction_ids?: string[];
  warnings?: SyncValidationWarning[];
}

export interface BatchSyncDef {
  id: string;
  source_id: string;
  capability_type?: SyncCapabilityType;
  output_kind?: SyncOutputKind;
  output_dataset_id: string;
  output_stream_id?: string | null;
  output_media_set_id?: string | null;
  source_selector?: string | null;
  source_path?: string | null;
  source_table?: string | null;
  source_topic?: string | null;
  schema?: SyncResourceSchemaField[] | null;
  write_mode?: SyncWriteMode;
  transaction_mode?: SyncTransactionMode;
  build_integration?: string | null;
  last_run?: SyncResourceRunSummary | null;
  next_run_at?: string | null;
  health?: SyncResourceHealth | null;
  history?: SyncResourceRunSummary[] | null;
  dataset_transaction_type?: DatasetTransactionType;
  file_sync?: FileSyncSettings | null;
  table_sync?: TableBatchSyncSettings | null;
  file_glob: string | null;
  schedule_cron: string | null;
  created_at: string;
}

export interface CreateBatchSyncRequest {
  source_id: string;
  capability_type?: SyncCapabilityType;
  output_kind?: SyncOutputKind;
  output_dataset_id: string;
  output_stream_id?: string;
  output_media_set_id?: string;
  source_selector?: string;
  source_path?: string;
  source_table?: string;
  source_topic?: string;
  schema?: SyncResourceSchemaField[];
  write_mode?: SyncWriteMode;
  transaction_mode?: SyncTransactionMode;
  build_integration?: string;
  create_output_dataset?: boolean;
  output_folder_rid?: string;
  dataset_transaction_type?: DatasetTransactionType;
  file_sync?: FileSyncSettings;
  table_sync?: TableBatchSyncSettings;
  file_glob?: string;
  schedule_cron?: string;
}

export interface SyncRun {
  id: string;
  sync_def_id: string;
  status: SyncRunStatus;
  queued_at?: string | null;
  started_at: string | null;
  finished_at: string | null;
  duration_ms?: number | null;
  worker?: SourceWorker | null;
  agent_id?: string | null;
  build?: SyncRunBuildLink | null;
  source_progress?: SyncRunSourceProgress | null;
  output_transaction?: SyncRunOutputTransaction | null;
  rows_written?: number | null;
  records_written?: number | null;
  bytes_written: number;
  files_written: number;
  retry_count?: number;
  logs?: SyncRunLogEntry[];
  error: string | null;
}


export type StreamingSyncStatus = 'draft' | 'starting' | 'running' | 'stopping' | 'stopped' | 'failed';
export type StreamingStartOffset = 'earliest' | 'latest' | 'timestamp' | 'offset';

export interface StreamingSyncSetup {
  id: string;
  source_id: string;
  output_stream_id: string;
  source_topic: string;
  consumer_group: string | null;
  schema: SyncResourceSchemaField[];
  key_fields: string[];
  start_offset: StreamingStartOffset;
  start_offset_value?: string | number | null;
  consistency_guarantee: 'AT_LEAST_ONCE' | 'EXACTLY_ONCE';
  checkpoint_interval_ms: number;
  output_stream_location: string;
  status: StreamingSyncStatus;
  created_at: string;
  updated_at: string;
}

export interface CreateStreamingSyncRequest {
  source_id: string;
  output_stream_id?: string;
  source_topic: string;
  consumer_group?: string | null;
  schema?: SyncResourceSchemaField[];
  key_fields?: string[];
  start_offset: StreamingStartOffset;
  start_offset_value?: string | number | null;
  consistency_guarantee: 'AT_LEAST_ONCE' | 'EXACTLY_ONCE';
  checkpoint_interval_ms: number;
  output_stream_location: string;
}

export type StreamStorageSource = 'hot' | 'cold' | 'hybrid';
export type StreamReplayStatus = 'available' | 'running' | 'disabled';
export type StreamCheckpointStatus = 'pending' | 'completed' | 'failed' | 'expired';
export type StreamingRuntimeKind = 'foundry_streaming' | 'flink' | 'spark_structured_streaming' | 'agent_runtime';

export interface StreamPermissionSummary {
  readers: string[];
  writers: string[];
  admins: string[];
  markings?: string[];
}

export interface StreamStorageSummary {
  hot_buffer_retention_ms: number;
  hot_buffer_bytes: number | null;
  cold_dataset_id: string | null;
  archive_dataset_id?: string | null;
  archive_interval_ms: number | null;
}

export interface StreamOffsetSummary {
  earliest_offset: number | null;
  latest_offset: number | null;
  committed_offset: number | null;
  lag: number | null;
}

export interface StreamOperatorStateMetadata {
  operator_id: string;
  operator_name: string;
  state_uri: string | null;
  size_bytes: number | null;
}

export interface StreamCheckpointSummary {
  id: string;
  status: StreamCheckpointStatus | string;
  offset: number | null;
  last_processed_source_location?: string | null;
  operator_state?: StreamOperatorStateMetadata[];
  size_bytes?: number | null;
  created_at: string;
  completed_at?: string | null;
  duration_ms: number | null;
}

export interface StreamRestartPlan {
  can_restart: boolean;
  latest_completed_checkpoint_id: string | null;
  restart_from_source_location?: string | null;
  reason: string | null;
}

export interface StreamConsistencySupport {
  requested: 'AT_LEAST_ONCE' | 'EXACTLY_ONCE';
  effective: 'AT_LEAST_ONCE' | 'EXACTLY_ONCE';
  runtime: StreamingRuntimeKind;
  source_supports_exactly_once: boolean;
  sink_supports_exactly_once: boolean;
  downgraded: boolean;
  duplicate_tolerant_consumers_required: boolean;
  reason: string | null;
}

export interface StreamReplayMetadata {
  status: StreamReplayStatus;
  from_offset: number | null;
  to_offset: number | null;
  requested_by: string | null;
  requested_at: string | null;
}

export interface StreamConsumerSummary {
  id: string;
  name: string;
  consumer_group: string | null;
  last_read_offset: number | null;
  lag: number | null;
  status: string;
}

export interface StreamLiveRow {
  offset: number;
  event_time: string;
  payload: Record<string, unknown>;
  source: StreamStorageSource;
}


export interface StreamArchivePolicy {
  enabled: boolean;
  archive_dataset_id: string | null;
  cadence_ms: number | null;
  retention_ms: number | null;
  last_archived_at: string | null;
}

export interface StreamHybridReadMetadata {
  hot_rows: number;
  cold_rows: number;
  from_offset: number | null;
  to_offset: number | null;
  consistency_guarantee: 'AT_LEAST_ONCE' | 'EXACTLY_ONCE';
}

export interface StreamHybridReadResponse {
  stream_id: string;
  source: 'hot' | 'cold' | 'hybrid';
  rows: StreamLiveRow[];
  metadata: StreamHybridReadMetadata;
}


export type PushStreamAuthMode = 'third_party_application' | 'personal_token';

export interface PushStreamEndpointDescriptor {
  stream_id: string;
  dataset_rid: string;
  branch: string;
  url: string;
  auth_mode: PushStreamAuthMode;
  token_reference_id: string | null;
}

export interface PushStreamRecordsRequest {
  dataset_rid: string;
  branch: string;
  records: Record<string, unknown>[];
  token_reference_id: string;
  idempotency_key?: string | null;
}

export interface PushStreamRecordsResponse {
  stream_id: string;
  dataset_rid: string;
  branch: string;
  accepted_record_count: number;
  rejected_record_count: number;
  next_offset: number | null;
  idempotency_key?: string | null;
  rate_limit_remaining?: number | null;
  warnings?: SyncValidationWarning[];
}

export interface PushStreamValidationOptions {
  datasetRid: string;
  branch: string;
  tokenReferenceId: string;
  records: Record<string, unknown>[];
  schema?: SyncResourceSchemaField[] | null;
  maxRecordsPerRequest?: number;
  rateLimitRemaining?: number | null;
  idempotencyKey?: string | null;
}

export type StreamIngestionRecommendationKind = 'streaming_sync' | 'listener' | 'push_api';

export interface StreamIngestionRecommendation {
  kind: StreamIngestionRecommendationKind;
  message: string;
}

export interface StreamIngestionRecommendationOptions {
  sourceConnectorExists: boolean;
  inboundSystemCanAuthenticate: boolean;
  inboundSystemConformsToSchema: boolean;
}

export interface DataConnectionStreamResource {
  id: string;
  rid?: string | null;
  name: string;
  description?: string | null;
  schema: SyncResourceSchemaField[];
  permissions: StreamPermissionSummary;
  branch: string;
  hot_buffer: StreamStorageSummary;
  cold_storage: StreamStorageSummary;
  archive_policy?: StreamArchivePolicy | null;
  hybrid_read?: StreamHybridReadMetadata | null;
  consistency_guarantee: 'AT_LEAST_ONCE' | 'EXACTLY_ONCE';
  offsets: StreamOffsetSummary;
  checkpoints: StreamCheckpointSummary[];
  restart_plan?: StreamRestartPlan | null;
  consistency?: StreamConsistencySupport | null;
  replay: StreamReplayMetadata | null;
  source_sync_ids: string[];
  consumers: StreamConsumerSummary[];
  health: SyncResourceHealth;
  live_view?: StreamLiveRow[];
  archive_view?: StreamLiveRow[];
  created_at: string;
  updated_at: string;
}

export type ConnectionTestCheckStatus = 'pending' | 'passed' | 'failed' | 'skipped';

export interface ConnectionTestCheck {
  name: string;
  status: ConnectionTestCheckStatus;
  message: string;
  latency_ms: number | null;
}

export interface TestConnectionResult {
  success: boolean;
  message: string;
  latency_ms: number | null;
  checks?: ConnectionTestCheck[];
  tested_at?: string;
}

// ---------------------------------------------------------------------------
// Streaming source contracts
// ---------------------------------------------------------------------------

export type StreamingSourceFieldKind = 'string' | 'int' | 'secret';

export interface StreamingSourceFieldDescriptor {
  name: string;
  kind: StreamingSourceFieldKind;
  required: boolean;
  description: string;
}

export interface StreamingSourceContract {
  kind: string;
  display_name: string;
  description: string;
  requires_agent: boolean;
  config_fields: StreamingSourceFieldDescriptor[];
}

export interface StreamingSourceContractResponse {
  data: StreamingSourceContract[];
}


// ---------------------------------------------------------------------------
// REST API sources and webhooks
// ---------------------------------------------------------------------------

export type RestApiAuthKind = 'none' | 'bearer_token' | 'api_key' | 'basic' | 'oauth_client';
export type WebhookHttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
export type WebhookParameterKind = 'boolean' | 'integer' | 'long' | 'double' | 'string' | 'date' | 'timestamp' | 'list' | 'record' | 'optional' | 'attachment';
export type WebhookOutputExtractorKind = 'whole_response' | 'key_path' | 'array_index' | 'json_path' | 'http_status' | 'full_response_string';
export type WebhookInvocationStatus = 'succeeded' | 'failed' | 'retrying' | 'cancelled';
export type WebhookInputMappingSource = 'action_parameter' | 'function_output' | 'literal';

export interface RestApiAuthConfig {
  kind: RestApiAuthKind;
  credential_reference_id?: string | null;
  header_name?: string | null;
  query_param_name?: string | null;
}

export interface RestApiSourceSetupRequest {
  name: string;
  base_domain: string;
  auth: RestApiAuthConfig;
  additional_secret_reference_ids: string[];
  network_policy_id?: string | null;
  worker: SourceWorker;
  permissions: string[];
}


export interface WebhookParameterMetadata {
  name: string;
  type: WebhookParameterKind;
  required: boolean;
  description?: string | null;
  allowed_values?: string[] | null;
  item_type?: WebhookParameterMetadata | null;
  fields?: WebhookParameterMetadata[] | null;
  inner_type?: WebhookParameterMetadata | null;
}

export interface WebhookOutputExtractor {
  kind: WebhookOutputExtractorKind;
  key_path?: string[];
  array_index_path?: number[];
  json_path?: string;
}

export interface WebhookOutputParameterMetadata {
  name: string;
  type: WebhookParameterKind;
  extractor: WebhookOutputExtractor;
  description?: string | null;
}

export interface WebhookInputParameterMapping {
  parameter_name: string;
  source: WebhookInputMappingSource;
  source_path?: string[];
  value?: unknown;
  skip_when_undefined?: boolean;
}

export interface WebhookInputMappingResult {
  should_invoke: boolean;
  inputs: Record<string, unknown>;
  skipped_reason: string | null;
}

export interface WebhookResponseForExtraction {
  status: number;
  body: unknown;
  text?: string | null;
}

export interface WebhookInvocationRedactedMetadata {
  headers?: Record<string, string>;
  query_params?: Record<string, string>;
  body_preview?: string | null;
  body_bytes?: number;
  truncated?: boolean;
}

export interface WebhookInvocationRecord {
  id: string;
  source_id: string;
  webhook_id: string;
  invoked_at: string;
  caller_id: string | null;
  action_type_id?: string | null;
  function_rid?: string | null;
  input_summary: Record<string, string>;
  http_status: number | null;
  parsed_outputs: Record<string, unknown>;
  status: WebhookInvocationStatus;
  error: string | null;
  retry_attempts: number;
  request: WebhookInvocationRedactedMetadata;
  response: WebhookInvocationRedactedMetadata;
  retained_until: string | null;
}

export interface WebhookRetryPolicy {
  max_attempts: number;
  initial_backoff_ms: number;
  max_backoff_ms: number;
}

export interface WebhookHeader {
  name: string;
  value: string;
  secret_reference_id?: string | null;
}

export interface WebhookQueryParam {
  name: string;
  value: string;
}

export interface WebhookDefinition {
  id: string;
  source_id: string;
  name: string;
  method: WebhookHttpMethod;
  relative_path: string;
  query_params: WebhookQueryParam[];
  headers: WebhookHeader[];
  body_template: string | null;
  authorization_reference_id?: string | null;
  input_parameters?: WebhookParameterMetadata[];
  output_parameters?: WebhookOutputParameterMetadata[];
  timeout_ms: number;
  retry: WebhookRetryPolicy;
  created_at: string;
  updated_at: string;
}

export interface CreateWebhookRequest {
  name: string;
  method: WebhookHttpMethod;
  relative_path: string;
  query_params: WebhookQueryParam[];
  headers: WebhookHeader[];
  body_template?: string | null;
  authorization_reference_id?: string | null;
  input_parameters?: WebhookParameterMetadata[];
  output_parameters?: WebhookOutputParameterMetadata[];
  timeout_ms: number;
  retry: WebhookRetryPolicy;
}

// ---------------------------------------------------------------------------
// Media-set syncs (Foundry "Set up a media set sync" — S3 / ABFS)
//
// Mirrors `services/connector-management-service/src/handlers/media_set_syncs.rs`.
// Two flavours are supported:
//   * `MEDIA_SET_SYNC` copies bytes into Foundry storage.
//   * `VIRTUAL_MEDIA_SET_SYNC` only registers metadata (bytes stay
//     in the source). Per Foundry "Virtual media sets.md".
// ---------------------------------------------------------------------------

export type MediaSetSyncKind = 'MEDIA_SET_SYNC' | 'VIRTUAL_MEDIA_SET_SYNC';

export interface MediaSetSyncFilters {
  exclude_already_synced: boolean;
  path_glob: string | null;
  /** Bytes — `null` means "no limit". */
  file_size_limit: number | null;
  ignore_unmatched_schema: boolean;
}

export interface MediaSetSyncDef {
  id: string;
  source_id: string;
  kind: MediaSetSyncKind;
  target_media_set_rid: string;
  subfolder: string;
  filters: MediaSetSyncFilters;
  schedule_cron: string | null;
  created_at: string;
}

export interface CreateMediaSetSyncRequest {
  kind: MediaSetSyncKind;
  target_media_set_rid: string;
  subfolder?: string;
  filters?: Partial<MediaSetSyncFilters>;
  schedule_cron?: string | null;
}

// Registrations / discovery payloads ----------------------------------------

export type RegistrationMode = 'sync' | 'zero_copy';

export type ExplorationNodeKind = 'folder' | 'file' | 'database' | 'schema' | 'table' | 'topic' | 'queue' | 'stream' | 'entity' | 'sample';
export type ExplorationSessionStatus = 'active' | 'completed' | 'expired' | 'failed';

export interface ExplorationSchemaField {
  name: string;
  source_type: string;
  foundry_type?: string | null;
  nullable?: boolean | null;
}

export interface ExplorationNode {
  selector: string;
  display_name: string;
  kind: ExplorationNodeKind | string;
  path?: string | null;
  has_children?: boolean;
  supports_sync?: boolean;
  supports_zero_copy?: boolean;
  source_signature?: string | null;
  schema?: ExplorationSchemaField[] | null;
  sample_rows?: Array<Record<string, unknown>> | null;
  sample_redacted?: boolean;
  unauthorized_sample_count?: number;
  metadata?: Record<string, unknown> | null;
}

export interface ExplorationSession {
  id: string;
  source_id: string;
  status: ExplorationSessionStatus;
  root_selector: string | null;
  selectors_examined: number;
  sample_rows_stored: 0;
  secrets_persisted: false;
  created_by: string | null;
  created_at: string;
  expires_at: string | null;
  audit_event_id?: string | null;
}

export interface ExploreSourceRequest {
  selector?: string;
  cursor?: string;
  include_sample?: boolean;
  sample_limit?: number;
}

export interface ExploreSourceResponse {
  session: ExplorationSession;
  nodes: ExplorationNode[];
  next_cursor?: string | null;
}

export interface DiscoveredSource {
  selector: string;
  display_name?: string | null;
  source_kind?: string | null;
  supports_sync?: boolean;
  supports_zero_copy?: boolean;
  source_signature?: string | null;
  schema?: ExplorationSchemaField[] | null;
  sample_rows?: Array<Record<string, unknown>> | null;
  sample_redacted?: boolean;
  unauthorized_sample_count?: number;
  metadata?: Record<string, unknown> | null;
}

export interface BulkRegistrationItem {
  selector: string;
  display_name?: string;
  source_kind?: string;
  registration_mode?: RegistrationMode | null;
  auto_sync?: boolean;
  update_detection?: boolean;
  target_dataset_id?: string;
  metadata?: Record<string, unknown>;
}

export interface ConnectionRegistration {
  id: string;
  connection_id: string;
  selector: string;
  display_name: string;
  source_kind: string | null;
  registration_mode: RegistrationMode | string | null;
  auto_sync: boolean;
  update_detection: boolean;
  target_dataset_id: string | null;
  last_source_signature: string | null;
  last_dataset_version: number | null;
  metadata?: Record<string, unknown> | null;
  created_at: string;
  updated_at: string;
}

// ---------------------------------------------------------------------------
// REST surface
// ---------------------------------------------------------------------------

const BASE = '/data-connection';

interface ApiListEnvelope<T> {
  data?: T[];
  items?: T[];
}

function listItems<T>(payload: ApiListEnvelope<T> | T[]): T[] {
  if (Array.isArray(payload)) return payload;
  return payload.data ?? payload.items ?? [];
}

export const dataConnection = {
  // Catalog ----------------------------------------------------------------
  getCatalog(): Promise<ConnectorCatalog> {
    return api.get(`${BASE}/catalog`);
  },
  listStreamingSourceContracts(): Promise<StreamingSourceContractResponse> {
    return api.get(`${BASE}/streaming-sources`);
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
  archiveSource(id: string, body: ArchiveSourceRequest = {}): Promise<Source> {
    return api.post(`${BASE}/sources/${id}/archive`, body);
  },
  duplicateSource(id: string, body: DuplicateSourceRequest): Promise<Source> {
    return api.post(`${BASE}/sources/${id}/duplicate`, body);
  },
  deleteSource(id: string): Promise<void> {
    return api.delete(`${BASE}/sources/${id}`);
  },
  testConnection(id: string): Promise<TestConnectionResult> {
    return api.post(`${BASE}/sources/${id}/test-connection`, {});
  },

  // Registrations / discovery (Tarea 10 — wizard step 3) ----------------
  discoverSources(sourceId: string): Promise<{ sources: DiscoveredSource[] }> {
    return api.post(`${BASE}/sources/${sourceId}/registrations/discover`, {});
  },
  startExplorationSession(sourceId: string, body: ExploreSourceRequest = {}): Promise<ExploreSourceResponse> {
    return api.post(`${BASE}/sources/${sourceId}/exploration-sessions`, body);
  },
  exploreSource(sourceId: string, body: ExploreSourceRequest = {}): Promise<ExploreSourceResponse> {
    return api.post(`${BASE}/sources/${sourceId}/explore`, body);
  },
  getExplorationSession(sourceId: string, sessionId: string): Promise<ExplorationSession> {
    return api.get(`${BASE}/sources/${sourceId}/exploration-sessions/${sessionId}`);
  },
  async listRegistrations(sourceId: string): Promise<ConnectionRegistration[]> {
    const response = await api.get<{ registrations: ConnectionRegistration[] }>(
      `${BASE}/sources/${sourceId}/registrations`,
    );
    return response.registrations;
  },
  bulkRegister(
    sourceId: string,
    registrations: BulkRegistrationItem[],
  ): Promise<{ created: ConnectionRegistration[]; errors?: { selector: string; error: string }[] }> {
    return api.post(`${BASE}/sources/${sourceId}/registrations/bulk`, { registrations });
  },
  deleteRegistration(sourceId: string, registrationId: string): Promise<void> {
    return api.delete(`${BASE}/sources/${sourceId}/registrations/${registrationId}`);
  },

  // Credentials ------------------------------------------------------------
  setCredential(sourceId: string, body: SetCredentialRequest): Promise<Credential> {
    return api.post(`${BASE}/sources/${sourceId}/credentials`, body);
  },
  listCredentials(sourceId: string): Promise<Credential[]> {
    return api.get(`${BASE}/sources/${sourceId}/credentials`);
  },
  rotateCredential(sourceId: string, credentialId: string, body: RotateCredentialRequest): Promise<Credential> {
    return api.post(`${BASE}/sources/${sourceId}/credentials/${credentialId}/rotate`, body);
  },
  testCredential(sourceId: string, credentialId: string): Promise<TestCredentialResult> {
    return api.post(`${BASE}/sources/${sourceId}/credentials/${credentialId}/test`, {});
  },

  // Connector agents --------------------------------------------------------
  async listConnectorAgents(): Promise<ConnectorAgent[]> {
    const res = await api.get<ApiListEnvelope<ConnectorAgent> | ConnectorAgent[]>(`${BASE}/agents`);
    return listItems(res);
  },
  registerConnectorAgent(body: RegisterConnectorAgentRequest): Promise<ConnectorAgent> {
    return api.post(`${BASE}/agents`, {
      ...body,
      capabilities: body.capabilities ?? {},
      metadata: body.metadata ?? {},
    });
  },
  heartbeatConnectorAgent(id: string, body: ConnectorAgentHeartbeatRequest = {}): Promise<ConnectorAgent> {
    return api.post(`${BASE}/agents/${id}/heartbeat`, {
      capabilities: body.capabilities ?? {},
      metadata: body.metadata ?? {},
    });
  },
  deleteConnectorAgent(id: string): Promise<void> {
    return api.delete(`${BASE}/agents/${id}`);
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
  createStreamingSync(body: CreateStreamingSyncRequest): Promise<StreamingSyncSetup> {
    return api.post(`${BASE}/streaming-syncs`, body);
  },
  startStreamingSync(syncId: string): Promise<StreamingSyncSetup> {
    return api.post(`${BASE}/streaming-syncs/${syncId}/start`, {});
  },
  stopStreamingSync(syncId: string): Promise<StreamingSyncSetup> {
    return api.post(`${BASE}/streaming-syncs/${syncId}/stop`, {});
  },
  runSync(syncId: string): Promise<SyncRun> {
    return api.post(`${BASE}/syncs/${syncId}/run`, {});
  },
  listRuns(syncId: string): Promise<SyncRun[]> {
    return api.get(`${BASE}/syncs/${syncId}/runs`);
  },

  // Streams ---------------------------------------------------------------
  listStreams(): Promise<DataConnectionStreamResource[]> {
    return api.get(`${BASE}/streams`);
  },
  listSourceStreams(sourceId: string): Promise<DataConnectionStreamResource[]> {
    return api.get(`${BASE}/sources/${sourceId}/streams`);
  },
  getStreamResource(streamId: string): Promise<DataConnectionStreamResource> {
    return api.get(`${BASE}/streams/${streamId}`);
  },
  readStreamHybrid(streamId: string, params: { from_offset?: number; to_offset?: number; limit?: number } = {}): Promise<StreamHybridReadResponse> {
    const search = new URLSearchParams();
    if (params.from_offset !== undefined) search.set('from_offset', String(params.from_offset));
    if (params.to_offset !== undefined) search.set('to_offset', String(params.to_offset));
    if (params.limit !== undefined) search.set('limit', String(params.limit));
    const query = search.toString();
    return api.get(`${BASE}/streams/${streamId}/hybrid-read${query ? `?${query}` : ''}`);
  },
  getPushStreamEndpoint(streamId: string, params: { dataset_rid: string; branch: string; auth_mode?: PushStreamAuthMode }): Promise<PushStreamEndpointDescriptor> {
    const search = new URLSearchParams();
    search.set('dataset_rid', params.dataset_rid);
    search.set('branch', params.branch);
    if (params.auth_mode) search.set('auth_mode', params.auth_mode);
    return api.get(`${BASE}/streams/${streamId}/push-endpoint?${search.toString()}`);
  },
  pushStreamRecords(streamId: string, body: PushStreamRecordsRequest): Promise<PushStreamRecordsResponse> {
    return api.post(`${BASE}/streams/${streamId}/records`, body);
  },

  // REST API sources and webhooks -----------------------------------------
  createRestApiSource(body: RestApiSourceSetupRequest): Promise<Source> {
    return api.post(`${BASE}/rest-api-sources`, body);
  },
  listWebhooks(sourceId: string): Promise<WebhookDefinition[]> {
    return api.get(`${BASE}/sources/${sourceId}/webhooks`);
  },
  createWebhook(sourceId: string, body: CreateWebhookRequest): Promise<WebhookDefinition> {
    return api.post(`${BASE}/sources/${sourceId}/webhooks`, body);
  },
  listWebhookInvocations(sourceId: string, webhookId: string): Promise<WebhookInvocationRecord[]> {
    return api.get(`${BASE}/sources/${sourceId}/webhooks/${webhookId}/invocations`);
  },

  // Media-set syncs (P1.4) -----------------------------------------------
  listMediaSetSyncs(sourceId: string): Promise<MediaSetSyncDef[]> {
    return api.get(`${BASE}/sources/${sourceId}/media-set-syncs`);
  },
  createMediaSetSync(
    sourceId: string,
    body: CreateMediaSetSyncRequest
  ): Promise<MediaSetSyncDef> {
    return api.post(`${BASE}/sources/${sourceId}/media-set-syncs`, body);
  },
};

// ---------------------------------------------------------------------------
// Static catalog used as a fallback when the backend is not yet wired.
// Keeping it client-side makes the gallery render even before the
// connector-management-service exposes /catalog. The list covers the SDC.2 source-type registry categories with real MVP
// connectors plus explicit advertised "coming soon" entries.
// ---------------------------------------------------------------------------

const DOC_BASE = 'https://www.palantir.com/docs/foundry';

const NO_SECRET_CREDENTIALS: ConnectorCredentialField[] = [
  {
    key: 'cloud_identity',
    label: 'Cloud identity / OIDC',
    kind: 'cloud_identity',
    required: false,
    secret: false,
    description: 'Use platform-managed identity when configured; no secret value is stored in the source.',
  },
];

function capabilityFlags(capabilities: ConnectorCapability[]): ConnectorFeatureFlags {
  return {
    supportsDiscovery: capabilities.includes('exploration') || capabilities.includes('virtual_table'),
    supportsConnectionTest: true,
    supportsIncrementalSync: capabilities.includes('cdc_sync') || capabilities.includes('streaming_sync'),
    supportsStreaming: capabilities.includes('streaming_sync') || capabilities.includes('streaming_export'),
    supportsVirtualTables: capabilities.includes('virtual_table'),
    supportsExports: capabilities.some((capability) => ['file_export', 'table_export', 'streaming_export'].includes(capability)),
    supportsWebhooks: capabilities.includes('webhook'),
    supportsMedia: capabilities.includes('media_sync') || capabilities.includes('virtual_media'),
  };
}

function network(
  modes: ConnectorNetworkMode[],
  defaultPorts: number[],
  notes: string,
  privateNetworkSupported = true,
): ConnectorNetworkRequirement {
  return { modes, defaultPorts, privateNetworkSupported, notes };
}

function connector(entry: Omit<ConnectorCatalogEntry, 'featureFlags'> & { featureFlags?: Partial<ConnectorFeatureFlags> }): ConnectorCatalogEntry {
  return {
    ...entry,
    featureFlags: {
      ...capabilityFlags(entry.capabilities),
      ...entry.featureFlags,
    },
  };
}

export const FALLBACK_CONNECTOR_CATALOG: ConnectorCatalogEntry[] = [
  connector({
    type: 'postgresql',
    name: 'PostgreSQL',
    description: 'Relational database table batch syncs, CDC handoff, exports, and schema exploration.',
    capabilities: ['batch_sync', 'cdc_sync', 'table_export', 'exploration'],
    workers: ['foundry', 'agent'],
    workerCapabilities: {
      foundry: ['batch_sync', 'cdc_sync', 'table_export', 'exploration'],
      agent: ['batch_sync', 'cdc_sync', 'exploration'],
    },
    available: true,
    category: 'databases',
    credentialFields: [
      { key: 'username', label: 'Username', kind: 'username_password', required: true, secret: false },
      { key: 'password', label: 'Password', kind: 'username_password', required: true, secret: true },
      { key: 'certificate', label: 'TLS certificate', kind: 'certificate_key', required: false, secret: true },
    ],
    network: network(['direct_egress', 'agent_proxy', 'agent_worker'], [5432], 'Requires host:port reachability to the database listener.'),
    setupDocsUrl: `${DOC_BASE}/available-connectors/postgresql/`,
    family: 'RDBMS',
  }),
  connector({
    type: 'mssql',
    name: 'Microsoft SQL Server',
    description: 'SQL Server source for table syncs, incremental reads, and table exports.',
    capabilities: ['batch_sync', 'cdc_sync', 'table_export', 'exploration'],
    workers: ['foundry', 'agent'],
    workerCapabilities: {
      foundry: ['batch_sync', 'cdc_sync', 'table_export', 'exploration'],
      agent: ['batch_sync', 'cdc_sync', 'exploration'],
    },
    available: false,
    category: 'databases',
    credentialFields: [
      { key: 'username', label: 'Username', kind: 'username_password', required: true, secret: false },
      { key: 'password', label: 'Password', kind: 'username_password', required: true, secret: true },
    ],
    network: network(['direct_egress', 'agent_proxy', 'agent_worker'], [1433], 'Requires SQL Server port access from the selected worker.'),
    setupDocsUrl: `${DOC_BASE}/available-connectors/microsoft-sql-server/`,
    family: 'RDBMS',
  }),
  connector({
    type: 's3',
    name: 'Amazon S3',
    description: 'Sync and export files from an S3 bucket with optional prefix-based exploration.',
    capabilities: ['batch_sync', 'file_export', 'exploration', 'media_sync'],
    workers: ['foundry', 'agent'],
    workerCapabilities: {
      foundry: ['batch_sync', 'file_export', 'exploration', 'media_sync'],
      agent: ['batch_sync', 'exploration', 'media_sync'],
    },
    available: true,
    category: 'filesystems_blob_stores',
    credentialFields: [
      { key: 'cloud_identity', label: 'AWS role / cloud identity', kind: 'cloud_identity', required: false, secret: false },
      { key: 'access_key', label: 'Access key', kind: 'api_key', required: false, secret: true },
      { key: 'secret_key', label: 'Secret key', kind: 'api_key', required: false, secret: true },
    ],
    network: network(['direct_egress', 'agent_proxy'], [443], 'Uses HTTPS to S3 or an S3-compatible endpoint.'),
    setupDocsUrl: `${DOC_BASE}/available-connectors/amazon-s3/`,
    family: 'Storage',
  }),
  connector({
    type: 'gcs',
    name: 'Google Cloud Storage',
    description: 'Sync parquet, CSV, JSON, or media objects directly from a GCS bucket.',
    capabilities: ['batch_sync', 'virtual_table', 'exploration', 'file_export', 'media_sync'],
    workers: ['foundry', 'agent'],
    workerCapabilities: {
      foundry: ['batch_sync', 'virtual_table', 'exploration', 'file_export', 'media_sync'],
      agent: ['batch_sync', 'exploration', 'media_sync'],
    },
    available: true,
    category: 'filesystems_blob_stores',
    credentialFields: [
      { key: 'cloud_identity', label: 'GCP cloud identity', kind: 'cloud_identity', required: false, secret: false },
      { key: 'service_account_json', label: 'Service account JSON', kind: 'service_account_json', required: false, secret: true },
      { key: 'access_token', label: 'Access token', kind: 'bearer_token', required: false, secret: true },
    ],
    network: network(['direct_egress', 'agent_proxy'], [443], 'Uses HTTPS to Google Cloud Storage APIs.'),
    setupDocsUrl: `${DOC_BASE}/available-connectors/google-cloud-storage/`,
    family: 'Storage',
  }),
  connector({
    type: 'onelake',
    name: 'Microsoft OneLake / ABFS',
    description: 'ABFS-compatible source for Microsoft Fabric lakehouses and Azure Data Lake paths.',
    capabilities: ['batch_sync', 'virtual_table', 'exploration', 'file_export'],
    workers: ['foundry', 'agent'],
    workerCapabilities: {
      foundry: ['batch_sync', 'virtual_table', 'exploration', 'file_export'],
      agent: ['batch_sync', 'exploration'],
    },
    available: true,
    category: 'filesystems_blob_stores',
    credentialFields: [
      { key: 'cloud_identity', label: 'Azure managed identity', kind: 'cloud_identity', required: false, secret: false },
      { key: 'client_secret', label: 'Client secret', kind: 'oauth_client', required: false, secret: true },
    ],
    network: network(['direct_egress', 'agent_proxy'], [443], 'Uses HTTPS to OneLake or ABFS endpoints.'),
    setupDocsUrl: `${DOC_BASE}/available-connectors/onelake-azure-blob-filesystem/`,
    family: 'Storage',
  }),
  connector({
    type: 'sftp',
    name: 'SFTP',
    description: 'File syncs and exports over SSH File Transfer Protocol.',
    capabilities: ['batch_sync', 'file_export', 'exploration'],
    workers: ['foundry', 'agent'],
    available: false,
    category: 'filesystems_blob_stores',
    credentialFields: [
      { key: 'username', label: 'Username', kind: 'username_password', required: true, secret: false },
      { key: 'password', label: 'Password', kind: 'username_password', required: false, secret: true },
      { key: 'private_key', label: 'Private key', kind: 'certificate_key', required: false, secret: true },
    ],
    network: network(['direct_egress', 'agent_proxy', 'agent_worker'], [22], 'Requires SSH/SFTP port access.'),
    setupDocsUrl: `${DOC_BASE}/available-connectors/sftp/`,
    family: 'Storage',
  }),
  connector({
    type: 'kafka',
    name: 'Apache Kafka',
    description: 'Subscribe to Kafka topics through the streaming bridge and export records to topics.',
    capabilities: ['streaming_sync', 'streaming_export', 'exploration'],
    workers: ['foundry', 'agent'],
    workerCapabilities: {
      foundry: ['streaming_sync', 'streaming_export', 'exploration'],
      agent: ['streaming_sync', 'exploration'],
    },
    available: true,
    category: 'event_streams',
    credentialFields: [
      { key: 'sasl_username', label: 'SASL username', kind: 'username_password', required: false, secret: false },
      { key: 'sasl_password', label: 'SASL password', kind: 'username_password', required: false, secret: true },
      { key: 'client_certificate', label: 'Client certificate/key', kind: 'certificate_key', required: false, secret: true },
    ],
    network: network(['direct_egress', 'agent_proxy', 'agent_worker'], [9092, 9093], 'Requires broker bootstrap reachability and matching TLS/SASL settings.'),
    setupDocsUrl: `${DOC_BASE}/available-connectors/kafka/`,
    family: 'Streaming',
  }),
  connector({
    type: 'kinesis',
    name: 'Amazon Kinesis',
    description: 'Stream records from a Kinesis Data Stream via shard iterators and checkpoints.',
    capabilities: ['streaming_sync'],
    workers: ['foundry', 'agent'],
    available: true,
    category: 'event_streams',
    credentialFields: NO_SECRET_CREDENTIALS,
    network: network(['direct_egress', 'agent_proxy'], [443], 'Uses HTTPS to AWS Kinesis APIs.'),
    setupDocsUrl: `${DOC_BASE}/available-connectors/amazon-kinesis/`,
    family: 'Streaming',
  }),
  connector({
    type: 'sqs',
    name: 'Amazon SQS',
    description: 'Long-poll SQS queues and acknowledge records after stream ingestion.',
    capabilities: ['streaming_sync'],
    workers: ['foundry', 'agent'],
    available: false,
    category: 'message_queues',
    credentialFields: NO_SECRET_CREDENTIALS,
    network: network(['direct_egress', 'agent_proxy'], [443], 'Uses HTTPS to AWS SQS APIs.'),
    setupDocsUrl: `${DOC_BASE}/available-connectors/other-source-types/`,
    family: 'Streaming',
  }),
  connector({
    type: 'rabbitmq',
    name: 'RabbitMQ',
    description: 'Consume AMQP queues for streaming ingestion through an agent or reachable broker.',
    capabilities: ['streaming_sync'],
    workers: ['agent'],
    available: false,
    category: 'message_queues',
    credentialFields: [
      { key: 'username', label: 'Username', kind: 'username_password', required: true, secret: false },
      { key: 'password', label: 'Password', kind: 'username_password', required: true, secret: true },
    ],
    network: network(['agent_worker', 'agent_proxy'], [5671, 5672], 'Usually private-network AMQP broker access through an agent.'),
    setupDocsUrl: `${DOC_BASE}/available-connectors/other-source-types/`,
    family: 'Streaming',
  }),
  connector({
    type: 'rest_api',
    name: 'REST API',
    description: 'Generic REST endpoint with configurable authentication, pagination, webhooks, and code use.',
    capabilities: ['batch_sync', 'webhook', 'use_in_code', 'exploration'],
    workers: ['foundry', 'agent'],
    workerCapabilities: {
      foundry: ['batch_sync', 'webhook', 'use_in_code', 'exploration'],
      agent: ['batch_sync', 'use_in_code', 'exploration'],
    },
    available: true,
    category: 'rest_apis',
    credentialFields: [
      { key: 'api_key', label: 'API key', kind: 'api_key', required: false, secret: true },
      { key: 'authorization_header', label: 'Authorization header', kind: 'bearer_token', required: false, secret: true },
      { key: 'client_secret', label: 'OAuth client secret', kind: 'oauth_client', required: false, secret: true },
    ],
    network: network(['direct_egress', 'agent_proxy', 'agent_worker'], [443, 80], 'Requires HTTP(S) reachability to the target API.'),
    setupDocsUrl: `${DOC_BASE}/available-connectors/rest-apis/`,
    family: 'API',
  }),
  connector({
    type: 'github',
    name: 'GitHub',
    description: 'Productivity-tool source for repositories, issues, pull requests, and organization metadata.',
    capabilities: ['batch_sync', 'webhook', 'use_in_code'],
    workers: ['foundry'],
    available: false,
    category: 'productivity_tools',
    credentialFields: [
      { key: 'token', label: 'Personal access token or GitHub App token', kind: 'api_key', required: true, secret: true },
    ],
    network: network(['direct_egress'], [443], 'Uses HTTPS to GitHub APIs and webhook endpoints.'),
    setupDocsUrl: `${DOC_BASE}/available-connectors/github/`,
    family: 'SaaS',
  }),
  connector({
    type: 'slack',
    name: 'Slack',
    description: 'Productivity-tool source for channels, messages, users, and listener-style events.',
    capabilities: ['batch_sync', 'webhook'],
    workers: ['foundry'],
    available: false,
    category: 'productivity_tools',
    credentialFields: [
      { key: 'bot_token', label: 'Bot token', kind: 'api_key', required: true, secret: true },
      { key: 'signing_secret', label: 'Signing secret', kind: 'connector_specific', required: false, secret: true },
    ],
    network: network(['direct_egress', 'listener'], [443], 'Uses HTTPS to Slack APIs; webhooks/listeners require inbound listener configuration.'),
    setupDocsUrl: `${DOC_BASE}/available-connectors/slack/`,
    family: 'SaaS',
  }),
  connector({
    type: 'salesforce',
    name: 'Salesforce',
    description: 'Pull SOQL queries from a Salesforce org with cursor pagination and table exports.',
    capabilities: ['batch_sync', 'virtual_table', 'table_export', 'webhook'],
    workers: ['foundry'],
    available: true,
    category: 'saas_applications',
    credentialFields: [
      { key: 'client_id', label: 'OAuth client id', kind: 'oauth_client', required: true, secret: false },
      { key: 'client_secret', label: 'OAuth client secret', kind: 'oauth_client', required: true, secret: true },
      { key: 'refresh_token', label: 'Refresh token', kind: 'bearer_token', required: false, secret: true },
    ],
    network: network(['direct_egress'], [443], 'Uses HTTPS to Salesforce APIs.'),
    setupDocsUrl: `${DOC_BASE}/available-connectors/salesforce/`,
    family: 'SaaS',
  }),
  connector({
    type: 'snowflake',
    name: 'Snowflake',
    description: 'Run statements and register virtual tables with keypair JWT or OAuth authentication.',
    capabilities: ['virtual_table', 'batch_sync', 'table_export', 'exploration'],
    workers: ['foundry', 'agent'],
    workerCapabilities: {
      foundry: ['virtual_table', 'batch_sync', 'table_export', 'exploration'],
      agent: ['batch_sync', 'exploration'],
    },
    available: true,
    category: 'saas_applications',
    credentialFields: [
      { key: 'username', label: 'Username', kind: 'username_password', required: true, secret: false },
      { key: 'private_key', label: 'Private key', kind: 'certificate_key', required: false, secret: true },
      { key: 'oauth_token', label: 'OAuth token', kind: 'bearer_token', required: false, secret: true },
    ],
    network: network(['direct_egress', 'agent_proxy', 'agent_worker'], [443], 'Uses HTTPS to the Snowflake account endpoint.'),
    setupDocsUrl: `${DOC_BASE}/available-connectors/snowflake/`,
    family: 'SaaS',
  }),
  connector({
    type: 'bigquery',
    name: 'Google BigQuery',
    description: 'Execute jobs.query against a project using service account, token, or cloud identity auth.',
    capabilities: ['virtual_table', 'batch_sync', 'table_export', 'exploration'],
    workers: ['foundry'],
    available: true,
    category: 'saas_applications',
    credentialFields: [
      { key: 'cloud_identity', label: 'GCP cloud identity', kind: 'cloud_identity', required: false, secret: false },
      { key: 'service_account_json', label: 'Service account JSON', kind: 'service_account_json', required: false, secret: true },
      { key: 'access_token', label: 'Access token', kind: 'bearer_token', required: false, secret: true },
    ],
    network: network(['direct_egress'], [443], 'Uses HTTPS to BigQuery APIs.'),
    setupDocsUrl: `${DOC_BASE}/available-connectors/bigquery/`,
    family: 'SaaS',
  }),
  connector({
    type: 'wfs',
    name: 'Web Feature Service (WFS)',
    description: 'Geospatial feature source for WFS layers and spatial object metadata.',
    capabilities: ['batch_sync', 'exploration', 'virtual_table'],
    workers: ['foundry', 'agent'],
    available: false,
    category: 'geospatial_systems',
    credentialFields: [
      { key: 'api_key', label: 'API key', kind: 'api_key', required: false, secret: true },
      { key: 'username', label: 'Username', kind: 'username_password', required: false, secret: false },
      { key: 'password', label: 'Password', kind: 'username_password', required: false, secret: true },
    ],
    network: network(['direct_egress', 'agent_proxy', 'agent_worker'], [443, 80], 'Requires HTTP(S) reachability to the geospatial service.'),
    setupDocsUrl: `${DOC_BASE}/available-connectors/web-feature-service-wfs/`,
    family: 'API',
  }),
  connector({
    type: 'dicom_media',
    name: 'DICOM media source',
    description: 'Media-source registry entry for medical imaging or unstructured media handoffs.',
    capabilities: ['media_sync', 'virtual_media', 'exploration'],
    workers: ['foundry', 'agent'],
    available: false,
    category: 'media_sources',
    credentialFields: [
      { key: 'api_key', label: 'API key', kind: 'api_key', required: false, secret: true },
      { key: 'client_certificate', label: 'Client certificate/key', kind: 'certificate_key', required: false, secret: true },
    ],
    network: network(['direct_egress', 'agent_proxy', 'agent_worker'], [443, 104], 'Supports HTTPS media APIs or private DICOM endpoints through an agent.'),
    setupDocsUrl: `${DOC_BASE}/data-integration/media-sets/`,
    family: 'Storage',
  }),
  connector({
    type: 'generic_connector',
    name: 'Generic connector',
    description: 'Fallback source type for systems without a dedicated connector, paired with code-based access.',
    capabilities: ['use_in_code', 'batch_sync', 'webhook', 'exploration'],
    workers: ['agent'],
    available: false,
    category: 'generic_connectors',
    credentialFields: [
      { key: 'connector_specific_secret', label: 'Connector-specific secret', kind: 'connector_specific', required: false, secret: true },
    ],
    network: network(['agent_worker', 'agent_proxy'], [], 'Network requirements are supplied by the custom connector implementation.'),
    setupDocsUrl: `${DOC_BASE}/available-connectors/generic-connector/`,
    family: 'API',
  }),
  connector({
    type: 'iot',
    name: 'MQTT / IoT Broker',
    description: 'Subscribe to MQTT topics on a broker to ingest IoT telemetry.',
    capabilities: ['streaming_sync'],
    workers: ['agent', 'foundry'],
    available: true,
    category: 'event_streams',
    credentialFields: [
      { key: 'username', label: 'Username', kind: 'username_password', required: false, secret: false },
      { key: 'password', label: 'Password', kind: 'username_password', required: false, secret: true },
      { key: 'client_certificate', label: 'Client certificate/key', kind: 'certificate_key', required: false, secret: true },
    ],
    network: network(['agent_worker', 'agent_proxy', 'direct_egress'], [1883, 8883], 'MQTT brokers are often private and agent-mediated; TLS typically uses 8883.'),
    setupDocsUrl: `${DOC_BASE}/available-connectors/other-source-types/`,
    family: 'Streaming',
  }),
  connector({
    type: 'sap',
    name: 'SAP',
    description: 'ERP source family for SAP extraction and HyperAuto-style pipelines.',
    capabilities: ['hyperauto', 'batch_sync'],
    workers: ['foundry', 'agent'],
    available: false,
    category: 'saas_applications',
    credentialFields: [
      { key: 'username', label: 'Username', kind: 'username_password', required: true, secret: false },
      { key: 'password', label: 'Password', kind: 'username_password', required: true, secret: true },
    ],
    network: network(['agent_worker', 'agent_proxy'], [443, 3200, 3300], 'Often deployed through a private-network SAP agent or proxy path.'),
    setupDocsUrl: `${DOC_BASE}/data-connection/sap-overview/`,
    family: 'SaaS',
  }),
];

function categoryFromFamily(family?: ConnectorFamily): ConnectorCategory {
  switch (family) {
    case 'RDBMS':
      return 'databases';
    case 'Storage':
      return 'filesystems_blob_stores';
    case 'Streaming':
      return 'event_streams';
    case 'API':
      return 'rest_apis';
    case 'SaaS':
      return 'saas_applications';
    default:
      return 'generic_connectors';
  }
}

export function connectorCategoryLabel(category: ConnectorCategory): string {
  switch (category) {
    case 'databases':
      return 'Databases';
    case 'filesystems_blob_stores':
      return 'Filesystems & blob stores';
    case 'event_streams':
      return 'Event streams';
    case 'message_queues':
      return 'Message queues';
    case 'rest_apis':
      return 'REST APIs';
    case 'productivity_tools':
      return 'Productivity tools';
    case 'saas_applications':
      return 'SaaS applications';
    case 'geospatial_systems':
      return 'Geospatial systems';
    case 'media_sources':
      return 'Media sources';
    case 'generic_connectors':
      return 'Generic connectors';
  }
}

export function connectorCategoryDescription(category: ConnectorCategory): string {
  switch (category) {
    case 'databases':
      return 'JDBC and warehouse-style systems with table syncs, CDC, exploration, and exports.';
    case 'filesystems_blob_stores':
      return 'Object stores, folders, and file protocols that back file, media, and virtual-table workflows.';
    case 'event_streams':
      return 'Append-oriented topics and streams used by long-running streaming syncs.';
    case 'message_queues':
      return 'Queue systems where messages are consumed, acknowledged, and checkpointed.';
    case 'rest_apis':
      return 'HTTP APIs and webhooks for systems without a table or file protocol.';
    case 'productivity_tools':
      return 'Collaboration and work-management tools such as GitHub, Slack, Jira, and Asana.';
    case 'saas_applications':
      return 'Business applications and cloud warehouses with dedicated connector semantics.';
    case 'geospatial_systems':
      return 'Spatial systems and map services that expose feature layers or geospatial tables.';
    case 'media_sources':
      return 'Unstructured media and binary-object systems that map to media set handoffs.';
    case 'generic_connectors':
      return 'Custom or code-based connectors for systems without a dedicated source type.';
  }
}

export function getConnectorRegistryEntry(entry: ConnectorCatalogEntry): ConnectorCatalogEntry {
  const fallback = FALLBACK_CONNECTOR_CATALOG.find((candidate) => candidate.type === entry.type);
  const capabilities = entry.capabilities ?? fallback?.capabilities ?? [];
  const workers = entry.workers ?? fallback?.workers ?? ['foundry'];
  const workerCapabilities = {
    ...(fallback?.workerCapabilities ?? {}),
    ...(entry.workerCapabilities ?? {}),
  };
  return {
    ...entry,
    capabilities,
    workers,
    workerCapabilities,
    available: entry.available ?? fallback?.available ?? false,
    category: entry.category ?? fallback?.category ?? categoryFromFamily(entry.family ?? fallback?.family),
    credentialFields: entry.credentialFields ?? fallback?.credentialFields ?? [],
    network: entry.network ?? fallback?.network ?? network(['direct_egress'], [443], 'No connector-specific network requirements are registered yet.'),
    setupDocsUrl: entry.setupDocsUrl ?? fallback?.setupDocsUrl ?? `${DOC_BASE}/foundry/data-connection/set-up-source`,
    featureFlags: {
      ...capabilityFlags(capabilities),
      ...(fallback?.featureFlags ?? {}),
      ...(entry.featureFlags ?? {}),
    },
    family: entry.family ?? fallback?.family,
  };
}

export function workerLabel(worker: SourceWorker): string {
  return worker === 'foundry' ? 'OpenFoundry worker' : 'Agent worker';
}

export function capabilitiesForWorker(entry: ConnectorCatalogEntry, worker: SourceWorker): ConnectorCapability[] {
  const registered = getConnectorRegistryEntry(entry);
  if (!registered.workers.includes(worker)) return [];
  return registered.workerCapabilities?.[worker] ?? registered.capabilities;
}

export function unavailableCapabilitiesForWorker(entry: ConnectorCatalogEntry, worker: SourceWorker): ConnectorCapability[] {
  const allowed = new Set(capabilitiesForWorker(entry, worker));
  return getConnectorRegistryEntry(entry).capabilities.filter((capability) => !allowed.has(capability));
}

export interface WorkerCompatibilityResult {
  valid: boolean;
  worker: SourceWorker;
  allowedCapabilities: ConnectorCapability[];
  unavailableCapabilities: ConnectorCapability[];
  reason: string | null;
}

export function validateConnectorWorker(
  entry: ConnectorCatalogEntry,
  worker: SourceWorker,
  capability?: ConnectorCapability,
): WorkerCompatibilityResult {
  const registered = getConnectorRegistryEntry(entry);
  const allowedCapabilities = capabilitiesForWorker(registered, worker);
  const unavailableCapabilities = unavailableCapabilitiesForWorker(registered, worker);
  const supportsWorker = registered.workers.includes(worker);
  const supportsCapability = capability ? allowedCapabilities.includes(capability) : true;
  let reason: string | null = null;
  if (!supportsWorker) {
    reason = `${workerLabel(worker)} is not allowed for ${registered.name}.`;
  } else if (!supportsCapability && capability) {
    reason = `${workerLabel(worker)} cannot configure ${capabilityLabel(capability)} for ${registered.name}.`;
  }
  return { valid: supportsWorker && supportsCapability, worker, allowedCapabilities, unavailableCapabilities, reason };
}








function recordMatchesField(record: Record<string, unknown>, field: SyncResourceSchemaField): boolean {
  const value = record[field.name];
  if (value === undefined || value === null) return field.nullable;
  const type = field.foundry_type.toLowerCase();
  if (type.includes('string') || type.includes('timestamp') || type.includes('date')) return typeof value === 'string';
  if (type.includes('double') || type.includes('float') || type.includes('decimal') || type.includes('integer') || type.includes('long')) return typeof value === 'number' && Number.isFinite(value);
  if (type.includes('boolean')) return typeof value === 'boolean';
  return true;
}

export function validatePushStreamRecords(options: PushStreamValidationOptions): SyncValidationWarning[] {
  const warnings: SyncValidationWarning[] = [];
  if (!options.datasetRid.trim()) {
    warnings.push({ code: 'missing-stream-dataset-rid', severity: 'error', message: 'Provide the dataset resource identifier for the target stream.' });
  }
  if (!options.branch.trim()) {
    warnings.push({ code: 'missing-stream-branch', severity: 'error', message: 'Provide the stream branch to push into.' });
  }
  if (!options.tokenReferenceId.trim()) {
    warnings.push({ code: 'missing-push-token', severity: 'error', message: 'Provide a token reference for authenticating the push request.' });
  }
  if (options.records.length === 0) {
    warnings.push({ code: 'empty-push-records', severity: 'error', message: 'Push ingestion requires at least one record.' });
  }
  const maxRecords = options.maxRecordsPerRequest ?? 500;
  if (options.records.length > maxRecords) {
    warnings.push({ code: 'too-many-push-records', severity: 'error', message: `Push at most ${maxRecords} records per request.` });
  }
  if (options.rateLimitRemaining !== null && options.rateLimitRemaining !== undefined && options.records.length > options.rateLimitRemaining) {
    warnings.push({ code: 'push-rate-limit-exceeded', severity: 'error', message: 'Record count exceeds the remaining push-ingestion rate limit.' });
  }
  if (options.records.length > 1 && !options.idempotencyKey?.trim()) {
    warnings.push({ code: 'missing-idempotency-key', severity: 'warning', message: 'Provide an idempotency key for retry-safe multi-record push requests when supported.' });
  }
  for (const [index, record] of options.records.entries()) {
    for (const field of options.schema ?? []) {
      if (!recordMatchesField(record, field)) {
        warnings.push({ code: 'record-schema-mismatch', severity: 'error', message: `Record ${index + 1} does not match schema field ${field.name} (${field.foundry_type}).` });
        break;
      }
    }
  }
  return warnings;
}

export function recommendStreamIngestion(options: StreamIngestionRecommendationOptions): StreamIngestionRecommendation {
  if (options.sourceConnectorExists) {
    return { kind: 'streaming_sync', message: 'A source connector exists; prefer a managed streaming sync for offsets, checkpointing, and operations.' };
  }
  if (!options.inboundSystemCanAuthenticate || !options.inboundSystemConformsToSchema) {
    return { kind: 'listener', message: 'Use listeners when inbound systems cannot authenticate to the push API or conform to the target stream schema.' };
  }
  return { kind: 'push_api', message: 'Use authenticated push-based ingestion for event producers that can call REST endpoints with schema-conformant records.' };
}

export function pushStreamEndpointUrl(datasetRid: string, branch: string): string {
  const rid = encodeURIComponent(datasetRid.trim());
  const encodedBranch = encodeURIComponent(branch.trim() || 'master');
  return `${BASE}/streams/by-dataset/${rid}/branches/${encodedBranch}/records`;
}

export function validateRestApiSourceSetup(input: RestApiSourceSetupRequest): SyncValidationWarning[] {
  const warnings: SyncValidationWarning[] = [];
  if (!input.name.trim()) warnings.push({ code: 'missing-rest-source-name', severity: 'error', message: 'REST API sources require a name.' });
  try {
    const url = new URL(input.base_domain);
    if (!['http:', 'https:'].includes(url.protocol)) throw new Error('invalid protocol');
    if (url.pathname && url.pathname !== '/') warnings.push({ code: 'base-domain-has-path', severity: 'warning', message: 'REST API source base domains should not include a request path; configure paths on webhooks.' });
  } catch {
    warnings.push({ code: 'invalid-rest-base-domain', severity: 'error', message: 'Provide a valid HTTP(S) base domain for the REST API source.' });
  }
  if (input.auth.kind !== 'none' && !input.auth.credential_reference_id?.trim()) {
    warnings.push({ code: 'missing-rest-auth-reference', severity: 'error', message: 'Select a credential reference for the configured REST API authentication mode.' });
  }
  if (input.auth.kind === 'api_key' && !input.auth.header_name?.trim() && !input.auth.query_param_name?.trim()) {
    warnings.push({ code: 'missing-api-key-location', severity: 'error', message: 'API key auth requires a header name or query parameter name.' });
  }
  return warnings;
}


function valueAtPath(value: unknown, path: Array<string | number> = []): unknown {
  let current = value;
  for (const segment of path) {
    if (current === null || current === undefined) return undefined;
    if (typeof segment === 'number') {
      if (!Array.isArray(current)) return undefined;
      current = current[segment];
    } else if (typeof current === 'object') {
      current = (current as Record<string, unknown>)[segment];
    } else {
      return undefined;
    }
  }
  return current;
}

function jsonPathSegments(path: string): Array<string | number> {
  return path.replace(/^\$?\.?/, '').split(/[./]/).filter(Boolean).map((segment) => /^\d+$/.test(segment) ? Number(segment) : segment);
}

function webhookValueMatchesType(value: unknown, parameter: WebhookParameterMetadata): boolean {
  if (value === undefined || value === null) return !parameter.required || parameter.type === 'optional';
  switch (parameter.type) {
    case 'boolean': return typeof value === 'boolean';
    case 'integer': return typeof value === 'number' && Number.isInteger(value) && value >= -2147483648 && value <= 2147483647;
    case 'long': return typeof value === 'number' && Number.isInteger(value);
    case 'double': return typeof value === 'number' && Number.isFinite(value);
    case 'string': return typeof value === 'string' && (!parameter.allowed_values?.length || parameter.allowed_values.includes(value));
    case 'date': return typeof value === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(value);
    case 'timestamp': return typeof value === 'string' && !Number.isNaN(Date.parse(value));
    case 'attachment': return typeof value === 'object';
    case 'list': return Array.isArray(value) && value.every((item) => !parameter.item_type || webhookValueMatchesType(item, { ...parameter.item_type, required: true }));
    case 'record': return typeof value === 'object' && !Array.isArray(value) && (parameter.fields ?? []).every((field) => webhookValueMatchesType((value as Record<string, unknown>)[field.name], field));
    case 'optional': return !parameter.inner_type || webhookValueMatchesType(value, { ...parameter.inner_type, required: false });
  }
}

export function validateWebhookParameters(input: Pick<CreateWebhookRequest, 'input_parameters' | 'output_parameters'>, options: { worker?: SourceWorker } = {}): SyncValidationWarning[] {
  const warnings: SyncValidationWarning[] = [];
  const names = new Set<string>();
  for (const parameter of input.input_parameters ?? []) {
    if (!parameter.name.trim()) warnings.push({ code: 'missing-webhook-input-name', severity: 'error', message: 'Webhook input parameters require a name.' });
    if (names.has(parameter.name)) warnings.push({ code: 'duplicate-webhook-input-name', severity: 'error', message: `Input parameter ${parameter.name} is duplicated.` });
    names.add(parameter.name);
    if (parameter.type === 'attachment' && options.worker === 'agent') warnings.push({ code: 'agent-attachment-input', severity: 'error', message: 'Attachment webhook inputs are not supported for agent worker sources.' });
    if (parameter.type === 'list' && !parameter.item_type) warnings.push({ code: 'missing-list-item-type', severity: 'error', message: `List input ${parameter.name} requires item type metadata.` });
    if (parameter.type === 'record' && !(parameter.fields?.length)) warnings.push({ code: 'missing-record-fields', severity: 'warning', message: `Record input ${parameter.name} should define expected field metadata.` });
    if (parameter.type === 'optional' && !parameter.inner_type) warnings.push({ code: 'missing-optional-inner-type', severity: 'error', message: `Optional input ${parameter.name} requires inner type metadata.` });
  }
  const outputNames = new Set<string>();
  for (const output of input.output_parameters ?? []) {
    if (!output.name.trim()) warnings.push({ code: 'missing-webhook-output-name', severity: 'error', message: 'Webhook output parameters require a name.' });
    if (outputNames.has(output.name)) warnings.push({ code: 'duplicate-webhook-output-name', severity: 'error', message: `Output parameter ${output.name} is duplicated.` });
    outputNames.add(output.name);
    if (output.extractor.kind === 'key_path' && !(output.extractor.key_path?.length)) warnings.push({ code: 'missing-output-key-path', severity: 'error', message: `Output ${output.name} requires a key path.` });
    if (output.extractor.kind === 'array_index' && !(output.extractor.array_index_path?.length)) warnings.push({ code: 'missing-output-array-index', severity: 'error', message: `Output ${output.name} requires an array index path.` });
    if (output.extractor.kind === 'json_path' && !output.extractor.json_path?.trim()) warnings.push({ code: 'missing-output-json-path', severity: 'error', message: `Output ${output.name} requires a JSON path.` });
  }
  return warnings;
}

export function mapWebhookInputs(parameters: WebhookParameterMetadata[], mappings: WebhookInputParameterMapping[], actionOrFunctionParams: Record<string, unknown>): WebhookInputMappingResult {
  const inputs: Record<string, unknown> = {};
  for (const parameter of parameters) {
    const mapping = mappings.find((candidate) => candidate.parameter_name === parameter.name);
    const value = mapping?.source === 'literal'
      ? mapping.value
      : valueAtPath(actionOrFunctionParams, mapping?.source_path ?? [parameter.name]);
    if (value === undefined && mapping?.skip_when_undefined) {
      return { should_invoke: false, inputs: {}, skipped_reason: `Mapping for ${parameter.name} returned undefined.` };
    }
    if (!webhookValueMatchesType(value, parameter)) {
      return { should_invoke: false, inputs: {}, skipped_reason: `Input ${parameter.name} does not match ${parameter.type} metadata.` };
    }
    if (value !== undefined) inputs[parameter.name] = value;
  }
  return { should_invoke: true, inputs, skipped_reason: null };
}

export function extractWebhookOutputs(parameters: WebhookOutputParameterMetadata[], response: WebhookResponseForExtraction): Record<string, unknown> {
  const outputs: Record<string, unknown> = {};
  const text = response.text ?? (typeof response.body === 'string' ? response.body : JSON.stringify(response.body));
  for (const parameter of parameters) {
    switch (parameter.extractor.kind) {
      case 'whole_response':
      case 'full_response_string':
        outputs[parameter.name] = text;
        break;
      case 'http_status':
        outputs[parameter.name] = response.status;
        break;
      case 'key_path':
        outputs[parameter.name] = valueAtPath(response.body, parameter.extractor.key_path ?? []);
        break;
      case 'array_index':
        outputs[parameter.name] = valueAtPath(response.body, parameter.extractor.array_index_path ?? []);
        break;
      case 'json_path':
        outputs[parameter.name] = valueAtPath(response.body, jsonPathSegments(parameter.extractor.json_path ?? ''));
        break;
    }
  }
  return outputs;
}

export function redactWebhookMetadata(metadata: WebhookInvocationRedactedMetadata, limitBytes = 4096): WebhookInvocationRedactedMetadata {
  const secretPattern = /authorization|token|secret|api[-_]?key|password|cookie/i;
  const headers = Object.fromEntries(Object.entries(metadata.headers ?? {}).map(([key, value]) => [key, secretPattern.test(key) ? '[REDACTED]' : value]));
  const query_params = Object.fromEntries(Object.entries(metadata.query_params ?? {}).map(([key, value]) => [key, secretPattern.test(key) ? '[REDACTED]' : value]));
  const body = metadata.body_preview ?? null;
  const body_bytes = body ? new TextEncoder().encode(body).length : metadata.body_bytes;
  const truncated = Boolean(body && body_bytes !== undefined && body_bytes > limitBytes);
  return {
    ...metadata,
    headers,
    query_params,
    body_preview: body && truncated ? `${body.slice(0, limitBytes)}…` : body,
    body_bytes,
    truncated: metadata.truncated || truncated,
  };
}

export function retainWebhookInvocations(records: WebhookInvocationRecord[], nowIso: string, retentionDays = 183): WebhookInvocationRecord[] {
  const now = Date.parse(nowIso);
  const retentionMs = retentionDays * 24 * 60 * 60 * 1000;
  return records.filter((record) => now - Date.parse(record.invoked_at) <= retentionMs).map((record) => ({
    ...record,
    request: redactWebhookMetadata(record.request),
    response: redactWebhookMetadata(record.response),
    retained_until: record.retained_until ?? new Date(Date.parse(record.invoked_at) + retentionMs).toISOString(),
  }));
}

export function validateWebhookSetup(input: CreateWebhookRequest): SyncValidationWarning[] {
  const warnings: SyncValidationWarning[] = [];
  if (!input.name.trim()) warnings.push({ code: 'missing-webhook-name', severity: 'error', message: 'Webhooks require a name.' });
  if (!input.relative_path.trim().startsWith('/')) warnings.push({ code: 'invalid-webhook-path', severity: 'error', message: 'Webhook relative paths must start with /.' });
  if (input.timeout_ms < 1000 || input.timeout_ms > 120000) warnings.push({ code: 'invalid-webhook-timeout', severity: 'error', message: 'Webhook timeout must be between 1,000 and 120,000 ms.' });
  if (input.retry.max_attempts < 1 || input.retry.max_attempts > 10) warnings.push({ code: 'invalid-webhook-retries', severity: 'error', message: 'Webhook retry attempts must be between 1 and 10.' });
  const seenHeaders = new Set<string>();
  for (const header of input.headers) {
    const key = header.name.trim().toLowerCase();
    if (!key) warnings.push({ code: 'missing-webhook-header-name', severity: 'error', message: 'Webhook header names cannot be blank.' });
    if (seenHeaders.has(key)) warnings.push({ code: 'duplicate-webhook-header', severity: 'warning', message: `Header ${header.name} is configured more than once.` });
    seenHeaders.add(key);
  }
  if ((input.method === 'GET' || input.method === 'DELETE') && input.body_template?.trim()) {
    warnings.push({ code: 'body-on-read-webhook', severity: 'warning', message: 'GET/DELETE webhooks usually should not include a body template.' });
  }
  warnings.push(...validateWebhookParameters(input));
  return warnings;
}

export function latestCompletedCheckpoint(checkpoints: StreamCheckpointSummary[]): StreamCheckpointSummary | null {
  const completed = checkpoints.filter((checkpoint) => checkpoint.status === 'completed');
  completed.sort((a, b) => Date.parse(b.completed_at ?? b.created_at) - Date.parse(a.completed_at ?? a.created_at));
  return completed[0] ?? null;
}

export function restartPlanForStream(stream: Pick<DataConnectionStreamResource, 'checkpoints'>): StreamRestartPlan {
  const checkpoint = latestCompletedCheckpoint(stream.checkpoints);
  if (!checkpoint) {
    return { can_restart: false, latest_completed_checkpoint_id: null, restart_from_source_location: null, reason: 'No completed checkpoint is available.' };
  }
  return {
    can_restart: true,
    latest_completed_checkpoint_id: checkpoint.id,
    restart_from_source_location: checkpoint.last_processed_source_location ?? null,
    reason: null,
  };
}

export function evaluateStreamingConsistency(options: {
  requested: 'AT_LEAST_ONCE' | 'EXACTLY_ONCE';
  runtime: StreamingRuntimeKind;
  sourceSupportsExactlyOnce: boolean;
  sinkSupportsExactlyOnce: boolean;
}): StreamConsistencySupport {
  const canExactlyOnce = options.runtime !== 'agent_runtime' && options.sourceSupportsExactlyOnce && options.sinkSupportsExactlyOnce;
  if (options.requested === 'EXACTLY_ONCE' && !canExactlyOnce) {
    return {
      requested: options.requested,
      effective: 'AT_LEAST_ONCE',
      runtime: options.runtime,
      source_supports_exactly_once: options.sourceSupportsExactlyOnce,
      sink_supports_exactly_once: options.sinkSupportsExactlyOnce,
      downgraded: true,
      duplicate_tolerant_consumers_required: true,
      reason: 'Exactly-once was downgraded because the selected runtime/source/sink combination cannot guarantee it.',
    };
  }
  return {
    requested: options.requested,
    effective: options.requested,
    runtime: options.runtime,
    source_supports_exactly_once: options.sourceSupportsExactlyOnce,
    sink_supports_exactly_once: options.sinkSupportsExactlyOnce,
    downgraded: false,
    duplicate_tolerant_consumers_required: options.requested === 'AT_LEAST_ONCE',
    reason: options.requested === 'AT_LEAST_ONCE' ? 'Consumers must tolerate duplicate records in at-least-once mode.' : null,
  };
}

export function streamingSyncCanStart(status: StreamingSyncStatus): boolean {
  return ['draft', 'stopped', 'failed'].includes(status);
}

export function streamingSyncCanStop(status: StreamingSyncStatus): boolean {
  return ['starting', 'running'].includes(status);
}

export function validateStreamingSyncSetup(input: CreateStreamingSyncRequest): SyncValidationWarning[] {
  const warnings: SyncValidationWarning[] = [];
  if (!input.source_topic.trim()) {
    warnings.push({ code: 'missing-streaming-topic', severity: 'error', message: 'Source topic, queue, or stream is required.' });
  }
  if (!input.output_stream_location.trim() && !input.output_stream_id?.trim()) {
    warnings.push({ code: 'missing-output-stream', severity: 'error', message: 'Output stream location or stream id is required.' });
  }
  if (input.checkpoint_interval_ms < 1000) {
    warnings.push({ code: 'checkpoint-too-frequent', severity: 'warning', message: 'Checkpoint intervals below one second can overwhelm stream storage.' });
  }
  if (input.consistency_guarantee === 'EXACTLY_ONCE' && (input.key_fields ?? []).length === 0) {
    warnings.push({ code: 'exactly-once-without-key', severity: 'warning', message: 'Exactly-once streaming syncs should define key fields for deterministic deduplication.' });
  }
  if (input.consistency_guarantee === 'AT_LEAST_ONCE') {
    warnings.push({ code: 'at-least-once-duplicates', severity: 'warning', message: 'At-least-once mode requires duplicate-tolerant consumers.' });
  }
  return warnings;
}

export function streamArchivePolicyLabel(policy?: StreamArchivePolicy | null): string {
  if (!policy?.enabled) return 'Archiving disabled';
  const cadence = policy.cadence_ms === null ? 'manual cadence' : `${policy.cadence_ms}ms cadence`;
  return `${cadence} → ${policy.archive_dataset_id ?? 'archive dataset pending'}`;
}

export function streamHybridReadLabel(read?: StreamHybridReadMetadata | null): string {
  if (!read) return 'Hybrid reads not configured';
  return `${read.hot_rows} hot + ${read.cold_rows} cold rows (${read.from_offset ?? 'earliest'} → ${read.to_offset ?? 'latest'})`;
}

export function syncRunStatusLabel(status: SyncRunStatus): string {
  switch (status) {
    case 'queued':
      return 'Queued';
    case 'pending':
      return 'Pending';
    case 'running':
      return 'Running';
    case 'succeeded':
      return 'Succeeded';
    case 'failed':
      return 'Failed';
    case 'cancelled':
      return 'Cancelled';
    case 'aborted':
      return 'Aborted';
    case 'retrying':
      return 'Retrying';
    case 'ignored':
      return 'Ignored';
    case 'partially_succeeded':
      return 'Partially succeeded';
  }
}

export function syncRunIsTerminal(status: SyncRunStatus): boolean {
  return ['succeeded', 'failed', 'cancelled', 'aborted', 'ignored', 'partially_succeeded'].includes(status);
}

export function syncRunDurationMs(run: Pick<SyncRun, 'started_at' | 'finished_at' | 'duration_ms'>): number | null {
  if (run.duration_ms !== undefined && run.duration_ms !== null) return run.duration_ms;
  if (!run.started_at || !run.finished_at) return null;
  const started = Date.parse(run.started_at);
  const finished = Date.parse(run.finished_at);
  return Number.isFinite(started) && Number.isFinite(finished) && finished >= started ? finished - started : null;
}

export function buildHistoryHref(build?: SyncRunBuildLink | null): string | null {
  if (!build?.build_id) return null;
  return build.build_url ?? `/builds/${encodeURIComponent(build.build_id)}${build.job_id ? `/jobs/${encodeURIComponent(build.job_id)}` : ''}`;
}

export function streamStorageLabel(source: StreamStorageSource): string {
  switch (source) {
    case 'hot':
      return 'Hot buffer';
    case 'cold':
      return 'Cold/archive dataset';
    case 'hybrid':
      return 'Hybrid hot+cold';
  }
}

export function streamReplayRangeLabel(replay: StreamReplayMetadata | null): string {
  if (!replay || replay.status === 'disabled') return 'Replay disabled';
  const start = replay.from_offset ?? 'earliest';
  const end = replay.to_offset ?? 'latest';
  return `${replay.status}: ${start} → ${end}`;
}

export function datasetTransactionTypeForFileMode(mode: FileSyncMode): DatasetTransactionType {
  switch (mode) {
    case 'snapshot_mirror':
    case 'historical_snapshot_incremental':
      return 'SNAPSHOT';
    case 'incremental_append':
      return 'APPEND';
  }
}

export function datasetTransactionTypeForTableMode(mode: TableBatchSyncMode): DatasetTransactionType {
  return mode === 'full_snapshot' ? 'SNAPSHOT' : 'APPEND';
}

export function fileSyncModeLabel(mode: FileSyncMode): string {
  switch (mode) {
    case 'snapshot_mirror':
      return 'Snapshot mirror';
    case 'incremental_append':
      return 'Incremental append';
    case 'historical_snapshot_incremental':
      return 'Historical snapshot + incremental recent files';
  }
}

export function tableBatchSyncModeLabel(mode: TableBatchSyncMode): string {
  return mode === 'full_snapshot' ? 'Full snapshot' : 'Incremental';
}

export function parseGlobList(raw: string): string[] {
  return Array.from(new Set(raw.split(/[\n,]/).map((item) => item.trim()).filter(Boolean)));
}

export function validateFileSyncSettings(settings: FileSyncSettings): SyncValidationWarning[] {
  const warnings: SyncValidationWarning[] = [];
  if (settings.mode === 'snapshot_mirror' && settings.exclude_already_synced) {
    warnings.push({ code: 'snapshot-excludes-synced', severity: 'warning', message: 'Snapshot mirror normally rewrites the destination; excluding already-synced files may leave deleted source files in the output.' });
  }
  if (settings.mode === 'incremental_append' && !settings.exclude_already_synced) {
    warnings.push({ code: 'incremental-without-dedup', severity: 'warning', message: 'Incremental append should exclude already-synced files to avoid duplicate dataset rows.' });
  }
  if (settings.mode === 'historical_snapshot_incremental' && !settings.historical_snapshot_cutoff) {
    warnings.push({ code: 'missing-historical-cutoff', severity: 'warning', message: 'Historical snapshot + incremental mode should define a cutoff so old files snapshot once and recent files append incrementally.' });
  }
  if (settings.file_count_limit !== null && (!Number.isInteger(settings.file_count_limit) || settings.file_count_limit < 1)) {
    warnings.push({ code: 'invalid-file-count-limit', severity: 'error', message: 'File count limit must be a positive integer when provided.' });
  }
  for (const glob of settings.include_globs) {
    if (settings.exclude_globs.includes(glob)) {
      warnings.push({ code: 'contradictory-glob', severity: 'warning', message: `Glob ${glob} is both included and excluded.` });
    }
  }
  if (settings.path_metadata_columns.length > 0 && !settings.include_path_metadata) {
    warnings.push({ code: 'path-columns-disabled', severity: 'warning', message: 'Path metadata columns are configured but path metadata is disabled.' });
  }
  return warnings;
}

export function makeFileSyncSettings(input: Omit<FileSyncSettings, 'transaction_type' | 'warnings'>): FileSyncSettings {
  const settings: FileSyncSettings = {
    ...input,
    transaction_type: datasetTransactionTypeForFileMode(input.mode),
    include_globs: Array.from(new Set(input.include_globs)),
    exclude_globs: Array.from(new Set(input.exclude_globs)),
    path_metadata_columns: Array.from(new Set(input.path_metadata_columns)),
    warnings: [],
  };
  return { ...settings, warnings: validateFileSyncSettings(settings) };
}

export function validateTableBatchSyncSettings(settings: TableBatchSyncSettings): SyncValidationWarning[] {
  const warnings: SyncValidationWarning[] = [];
  if (settings.selected_tables.length === 0) {
    warnings.push({ code: 'no-tables-selected', severity: 'error', message: 'Select at least one source table before creating a table batch sync.' });
  }
  if (settings.mode === 'incremental') {
    for (const table of settings.selected_tables) {
      if (!(table.incremental_column ?? settings.incremental_column)?.trim()) {
        warnings.push({ code: 'missing-incremental-column', severity: 'warning', message: `Table ${table.source_table} is incremental but has no change detection column.` });
      }
    }
  }
  return warnings;
}

export function makeTableBatchSyncSettings(input: Omit<TableBatchSyncSettings, 'warnings'>): TableBatchSyncSettings {
  const settings: TableBatchSyncSettings = {
    ...input,
    transaction_ids: input.transaction_ids ?? [],
    warnings: [],
  };
  return { ...settings, warnings: validateTableBatchSyncSettings(settings) };
}

export function defaultOutputKindForCapability(capability: SyncCapabilityType): SyncOutputKind {
  switch (capability) {
    case 'streaming_sync':
    case 'cdc_sync':
      return 'stream';
    case 'media_sync':
      return 'media_set';
    case 'batch_sync':
      return 'dataset';
  }
}

export function defaultTransactionModeForCapability(capability: SyncCapabilityType): SyncTransactionMode {
  return capability === 'batch_sync' || capability === 'media_sync' ? 'transactional' : 'external_checkpoint';
}

export function defaultWriteModeForCapability(capability: SyncCapabilityType): SyncWriteMode {
  return capability === 'batch_sync' || capability === 'media_sync' ? 'snapshot' : 'append';
}

export function syncCapabilityLabel(capability: SyncCapabilityType): string {
  switch (capability) {
    case 'batch_sync':
      return 'Batch sync';
    case 'streaming_sync':
      return 'Streaming sync';
    case 'cdc_sync':
      return 'CDC sync';
    case 'media_sync':
      return 'Media sync';
  }
}

export function suggestedOutputDatasetId(source: Pick<Source, 'id' | 'name' | 'default_output_location'>, selector?: string): string {
  const slug = `${source.name}-${selector ?? 'sync'}`
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 80) || source.id;
  return source.default_output_location ? `${source.default_output_location.replace(/\/$/, '')}/${slug}` : `dataset://${slug}`;
}

export const FALLBACK_STREAMING_SOURCE_CONTRACTS: StreamingSourceContract[] = [
  {
    kind: 'streaming_kafka',
    display_name: 'Apache Kafka',
    description: 'Pull records from a Kafka topic via consumer-group offsets.',
    requires_agent: false,
    config_fields: [
      { name: 'bootstrap_servers', kind: 'string', required: true, description: 'Comma-separated host:port list.' },
      { name: 'topic', kind: 'string', required: true, description: 'Topic the sync subscribes to.' },
      { name: 'consumer_group', kind: 'string', required: true, description: 'Kafka consumer group id.' },
      { name: 'auto_offset_reset', kind: 'string', required: false, description: 'earliest / latest.' },
    ],
  },
  {
    kind: 'streaming_kinesis',
    display_name: 'Amazon Kinesis',
    description: 'Pull records from a Kinesis stream shard.',
    requires_agent: false,
    config_fields: [
      { name: 'stream_name', kind: 'string', required: true, description: 'Kinesis stream name.' },
      { name: 'region', kind: 'string', required: true, description: 'AWS region.' },
      { name: 'shard_iterator_type', kind: 'string', required: false, description: 'LATEST / TRIM_HORIZON.' },
      { name: 'max_records_per_shard', kind: 'int', required: false, description: 'Soft cap per pull.' },
    ],
  },
  {
    kind: 'streaming_sqs',
    display_name: 'Amazon SQS',
    description: 'Long-poll an SQS queue with explicit per-message ack.',
    requires_agent: false,
    config_fields: [
      { name: 'queue_url', kind: 'string', required: true, description: 'Full queue URL.' },
      { name: 'region', kind: 'string', required: true, description: 'AWS region.' },
      { name: 'wait_time_seconds', kind: 'int', required: false, description: 'Long-poll seconds (0..=20).' },
      { name: 'visibility_timeout_seconds', kind: 'int', required: false, description: 'Per-message visibility timeout.' },
    ],
  },
  {
    kind: 'streaming_pubsub',
    display_name: 'Google Cloud Pub/Sub',
    description: 'REST-based pull + ack against a subscription.',
    requires_agent: false,
    config_fields: [
      { name: 'project_id', kind: 'string', required: true, description: 'GCP project id.' },
      { name: 'subscription_id', kind: 'string', required: true, description: 'Subscription id.' },
      { name: 'max_messages', kind: 'int', required: false, description: 'Soft cap per pull.' },
      { name: 'ack_deadline_seconds', kind: 'int', required: false, description: 'Per-pull ack-deadline override.' },
    ],
  },
  {
    kind: 'streaming_aveva_pi',
    display_name: 'Aveva PI',
    description: 'Poll the PI Web API for observation deltas.',
    requires_agent: false,
    config_fields: [
      { name: 'base_url', kind: 'string', required: true, description: 'PI Web API base URL.' },
      { name: 'event_stream_web_id', kind: 'string', required: true, description: 'WebID of the event stream.' },
      { name: 'poll_interval_ms', kind: 'int', required: false, description: 'Polling cadence.' },
      { name: 'auth_header', kind: 'secret', required: false, description: 'Authorization header (Bearer / Basic).' },
    ],
  },
  {
    kind: 'streaming_external',
    display_name: 'External transform',
    description: 'Generic webhook hook for sources without a dedicated connector.',
    requires_agent: true,
    config_fields: [
      { name: 'agent_label', kind: 'string', required: true, description: 'Free-form label for the catalogue.' },
      { name: 'agent_token', kind: 'secret', required: true, description: 'Bearer token the agent uses to push records.' },
      { name: 'protocol', kind: 'string', required: true, description: 'activemq | rabbitmq | mqtt | sns | ibm_mq | solace.' },
    ],
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
    const registered = getConnectorRegistryEntry(entry);
    if (registered.type.toLowerCase().includes(q)) return true;
    if (registered.name.toLowerCase().includes(q)) return true;
    if (registered.description.toLowerCase().includes(q)) return true;
    if (connectorCategoryLabel(registered.category).toLowerCase().includes(q)) return true;
    if (registered.capabilities.some((cap) => cap.toLowerCase().includes(q) || capabilityLabel(cap).toLowerCase().includes(q))) return true;
    if (registered.credentialFields.some((field) => field.label.toLowerCase().includes(q) || field.kind.toLowerCase().includes(q))) return true;
    if (registered.network.modes.some((mode) => mode.toLowerCase().includes(q))) return true;
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
