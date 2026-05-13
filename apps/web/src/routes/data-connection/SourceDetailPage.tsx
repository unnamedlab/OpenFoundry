import { useEffect, useState } from 'react';
import type { CSSProperties } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';

import { Tabs } from '@/lib/components/Tabs';
import { VirtualTablesTab } from '@/lib/components/data-connection/VirtualTablesTab';
import {
  capabilityLabel,
  connectorCategoryLabel,
  dataConnection,
  FALLBACK_CONNECTOR_CATALOG,
  datasetTransactionTypeForFileMode,
  datasetTransactionTypeForTableMode,
  defaultOutputKindForCapability,
  buildHistoryHref,
  defaultTransactionModeForCapability,
  defaultWriteModeForCapability,
  fileSyncModeLabel,
  getConnectorRegistryEntry,
  makeFileSyncSettings,
  makeTableBatchSyncSettings,
  parseGlobList,
  evaluateStreamingConsistency,
  pushStreamEndpointUrl,
  recommendStreamIngestion,
  restartPlanForStream,
  streamArchivePolicyLabel,
  streamHybridReadLabel,
  streamingSyncCanStart,
  streamingSyncCanStop,
  validateStreamingSyncSetup,
  validatePushStreamRecords,
  validateWebhookSetup,
  retainWebhookInvocations,
  unavailableCapabilitiesForWorker,
  validateConnectorWorker,
  validateEgressPoliciesForConnectionTest,
  suggestedOutputDatasetId,
  tableBatchSyncModeLabel,
  streamReplayRangeLabel,
  streamStorageLabel,
  syncCapabilityLabel,
  syncRunDurationMs,
  syncRunStatusLabel,
  validateEgressPolicy,
  workerLabel,
  type BatchSyncDef,
  type BulkRegistrationItem,
  type ConnectionRegistration,
  type CreateStreamingSyncRequest,
  type CreateWebhookRequest,
  type DataConnectionStreamResource,
  type ConnectorCatalogEntry,
  type Credential,
  type CredentialKind,
  type DiscoveredSource,
  type ExplorationNode,
  type ExplorationSession,
  type MediaSetSyncDef,
  type AgentProxyMode,
  type CredentialStorageMode,
  type EgressEndpointKind,
  type EgressPolicyKind,
  type EgressProtocol,
  type EgressPortKind,
  type NetworkEgressPolicy,
  type RegistrationMode,
  type Source,
  type SourceWorker,
  type FileSyncMode,
  type SyncCapabilityType,
  type SyncRun,
  type SyncTransactionMode,
  type SyncWriteMode,
  type StreamingStartOffset,
  type StreamingRuntimeKind,
  type StreamingSyncSetup,
  type WebhookDefinition,
  type WebhookHttpMethod,
  type WebhookInvocationRecord,
  type WebhookOutputParameterMetadata,
  type WebhookParameterMetadata,
  type TableBatchSyncMode,
  type TableBatchSyncSelection,
  type TestConnectionResult,
} from '@/lib/api/data-connection';
import type { VirtualTableProvider } from '@/lib/api/virtual-tables';

type Tab = 'overview' | 'configuration' | 'credentials' | 'networking' | 'explore' | 'syncs' | 'streams' | 'exports' | 'webhooks' | 'virtual-tables' | 'code-imports' | 'permissions' | 'history' | 'capabilities' | 'media-syncs';

const MEDIA_SYNC_CONNECTORS = new Set(['s3', 'onelake', 'abfs']);
const WORKER_CHOICES: SourceWorker[] = ['foundry', 'agent'];
const CREDENTIAL_KINDS: CredentialKind[] = [
  'username_password',
  'api_key',
  'bearer_token',
  'oauth_client_secret',
  'cloud_identity',
  'certificate_key',
  'connector_specific',
  'service_account_json',
];
const STORAGE_MODES: CredentialStorageMode[] = ['encrypted_secret', 'external_secret_reference', 'cloud_identity_reference'];
const EGRESS_PROTOCOLS: EgressProtocol[] = ['tcp', 'tls', 'http', 'https'];
const PROXY_MODES: AgentProxyMode[] = ['none', 'http_connect', 'socks5', 'mtls_tunnel'];
const SYNC_CAPABILITIES: SyncCapabilityType[] = ['batch_sync', 'streaming_sync', 'cdc_sync', 'media_sync'];
const SYNC_WRITE_MODES: SyncWriteMode[] = ['snapshot', 'append', 'upsert', 'incremental'];
const SYNC_TRANSACTION_MODES: SyncTransactionMode[] = ['transactional', 'external_checkpoint', 'non_transactional'];
const FILE_SYNC_MODES: FileSyncMode[] = ['snapshot_mirror', 'incremental_append', 'historical_snapshot_incremental'];
const TABLE_SYNC_MODES: TableBatchSyncMode[] = ['full_snapshot', 'incremental'];
const STREAMING_START_OFFSETS: StreamingStartOffset[] = ['latest', 'earliest', 'timestamp', 'offset'];
const STREAMING_RUNTIMES: StreamingRuntimeKind[] = ['foundry_streaming', 'flink', 'spark_structured_streaming', 'agent_runtime'];
const WEBHOOK_METHODS: WebhookHttpMethod[] = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE'];


const CONNECTOR_PROVIDER: Record<string, VirtualTableProvider> = {
  abfs: 'AZURE_ABFS',
  adls: 'AZURE_ABFS',
  azure_blob: 'AZURE_ABFS',
  bigquery: 'BIGQUERY',
  databricks: 'DATABRICKS',
  foundry_iceberg: 'FOUNDRY_ICEBERG',
  gcs: 'GCS',
  google_cloud_storage: 'GCS',
  iceberg: 'FOUNDRY_ICEBERG',
  onelake: 'AZURE_ABFS',
  open_table_catalog: 'FOUNDRY_ICEBERG',
  s3: 'AMAZON_S3',
  snowflake: 'SNOWFLAKE',
};

function virtualTableProviderFor(connectorType: string): VirtualTableProvider | null {
  return CONNECTOR_PROVIDER[connectorType.toLowerCase()] ?? null;
}

function discoveredLabel(source: DiscoveredSource): string {
  return source.display_name || source.selector;
}

function sourceCapabilities(source: Source, registryEntry: ConnectorCatalogEntry | null) {
  return source.supported_capabilities && source.supported_capabilities.length > 0
    ? source.supported_capabilities
    : registryEntry?.capabilities ?? [];
}

function ownerLabel(source: Source): string {
  return source.owner_name ?? source.owner_id ?? 'Unassigned owner';
}

function sourceHealth(source: Source) {
  return source.health ?? {
    state: source.status,
    last_checked_at: source.updated_at,
    recent_failures: source.status === 'error' || source.status === 'degraded' ? 1 : 0,
    message: source.status === 'healthy' ? 'Source is healthy.' : null,
  };
}

function sourceUsage(source: Source) {
  return source.usage ?? {
    sync_count: 0,
    export_count: 0,
    webhook_count: 0,
    virtual_table_count: 0,
    code_import_count: 0,
    last_used_at: source.last_sync_at,
  };
}

function sourceAudit(source: Source) {
  return source.audit ?? {
    created_by: null,
    updated_by: null,
    last_event_id: null,
  };
}

function parseCsvLines(raw: string): string[] {
  return Array.from(new Set(raw.split(/[,\n]/).map((item) => item.trim()).filter(Boolean)));
}

function parseNameValueLines(raw: string) {
  return raw.split(/\n/).map((line) => line.trim()).filter(Boolean).map((line) => {
    const separator = line.includes(':') ? ':' : '=';
    const [name, ...rest] = line.split(separator);
    return { name: name.trim(), value: rest.join(separator).trim() };
  }).filter((item) => item.name);
}

function makeDraftPolicyForValidation({
  kind,
  host,
  port,
  protocol,
  proxyMode,
  allowedOrganizations,
}: {
  kind: EgressPolicyKind;
  host: string;
  port: string;
  protocol: EgressProtocol;
  proxyMode: AgentProxyMode;
  allowedOrganizations: string[];
}) {
  return {
    kind,
    address: { kind: 'host' as EgressEndpointKind, value: host },
    port: { kind: 'single' as EgressPortKind, value: port },
    protocol,
    proxy_mode: kind === 'agent_proxy' ? proxyMode : 'none' as AgentProxyMode,
    status: 'pending_review' as const,
    allowed_organizations: allowedOrganizations,
  };
}

export function SourceDetailPage() {
  const { id = '' } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [tab, setTab] = useState<Tab>('overview');
  const [source, setSource] = useState<Source | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [editOpen, setEditOpen] = useState(false);
  const [editName, setEditName] = useState('');
  const [editDescription, setEditDescription] = useState('');
  const [editProjectRid, setEditProjectRid] = useState('');
  const [editFolderRid, setEditFolderRid] = useState('');
  const [editOwnerId, setEditOwnerId] = useState('');
  const [editWorker, setEditWorker] = useState<SourceWorker>('foundry');
  const [editOutputLocation, setEditOutputLocation] = useState('');
  const [duplicateName, setDuplicateName] = useState('');
  const [archiveReason, setArchiveReason] = useState('');

  // networking
  const [attached, setAttached] = useState<NetworkEgressPolicy[]>([]);
  const [available, setAvailable] = useState<NetworkEgressPolicy[]>([]);
  const [pickPolicyId, setPickPolicyId] = useState('');
  const [newPolicyName, setNewPolicyName] = useState('');
  const [newPolicyKind, setNewPolicyKind] = useState<EgressPolicyKind>('direct');
  const [newPolicyHost, setNewPolicyHost] = useState('');
  const [newPolicyPort, setNewPolicyPort] = useState('443');
  const [newPolicyProtocol, setNewPolicyProtocol] = useState<EgressProtocol>('https');
  const [newPolicyProxyMode, setNewPolicyProxyMode] = useState<AgentProxyMode>('none');
  const [newPolicyOrgs, setNewPolicyOrgs] = useState('');

  // credentials
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [credKind, setCredKind] = useState<CredentialKind>('api_key');
  const [credValue, setCredValue] = useState('');
  const [credStorageMode, setCredStorageMode] = useState<CredentialStorageMode>('encrypted_secret');
  const [credExternalRef, setCredExternalRef] = useState('');
  const [credCloudIdentityRef, setCredCloudIdentityRef] = useState('');
  const [credVersion, setCredVersion] = useState('v1');

  // test
  const [testResult, setTestResult] = useState<TestConnectionResult | null>(null);

  // registrations / discovery
  const [registrations, setRegistrations] = useState<ConnectionRegistration[]>([]);
  const [registrationsLoading, setRegistrationsLoading] = useState(false);
  const [discovered, setDiscovered] = useState<DiscoveredSource[]>([]);
  const [selectedSelectors, setSelectedSelectors] = useState<Record<string, boolean>>({});
  const [registrationMode, setRegistrationMode] = useState<RegistrationMode>('sync');
  const [autoSync, setAutoSync] = useState(false);
  const [updateDetection, setUpdateDetection] = useState(true);
  const [targetDatasetId, setTargetDatasetId] = useState('');
  const [bulkDialogOpen, setBulkDialogOpen] = useState(false);
  const [registrationMessage, setRegistrationMessage] = useState('');
  const [registrationErrors, setRegistrationErrors] = useState<Array<{ selector: string; error: string }>>([]);
  const [explorationSession, setExplorationSession] = useState<ExplorationSession | null>(null);
  const [explorationNodes, setExplorationNodes] = useState<ExplorationNode[]>([]);
  const [exploreSelector, setExploreSelector] = useState('');
  const [includeSamples, setIncludeSamples] = useState(false);
  const [nextExploreCursor, setNextExploreCursor] = useState<string | null>(null);

  // syncs / runs
  const [syncs, setSyncs] = useState<BatchSyncDef[]>([]);
  const [runsBySync, setRunsBySync] = useState<Record<string, SyncRun[]>>({});
  const [streams, setStreams] = useState<DataConnectionStreamResource[]>([]);
  const [streamingSyncs, setStreamingSyncs] = useState<StreamingSyncSetup[]>([]);
  const [newSyncCapability, setNewSyncCapability] = useState<SyncCapabilityType>('batch_sync');
  const [newOutputDataset, setNewOutputDataset] = useState('');
  const [createOutputDataset, setCreateOutputDataset] = useState(true);
  const [newSourceSelector, setNewSourceSelector] = useState('');
  const [newFileGlob, setNewFileGlob] = useState('');
  const [newWriteMode, setNewWriteMode] = useState<SyncWriteMode>('snapshot');
  const [newTransactionMode, setNewTransactionMode] = useState<SyncTransactionMode>('transactional');
  const [newBuildIntegration, setNewBuildIntegration] = useState('');
  const [newScheduleCron, setNewScheduleCron] = useState('');
  const [fileSyncMode, setFileSyncMode] = useState<FileSyncMode>('snapshot_mirror');
  const [excludeAlreadySynced, setExcludeAlreadySynced] = useState(false);
  const [fileCountLimit, setFileCountLimit] = useState('');
  const [includeGlobsRaw, setIncludeGlobsRaw] = useState('');
  const [excludeGlobsRaw, setExcludeGlobsRaw] = useState('');
  const [includePathMetadata, setIncludePathMetadata] = useState(true);
  const [pathMetadataColumnsRaw, setPathMetadataColumnsRaw] = useState('source_path,source_filename');
  const [historicalCutoff, setHistoricalCutoff] = useState('');
  const [incrementalRecentWindow, setIncrementalRecentWindow] = useState('P7D');
  const [tableSyncMode, setTableSyncMode] = useState<TableBatchSyncMode>('full_snapshot');
  const [tableNamesRaw, setTableNamesRaw] = useState('');
  const [tableIncrementalColumn, setTableIncrementalColumn] = useState('');
  const [inferTableSchema, setInferTableSchema] = useState(true);
  const [estimatedRowCount, setEstimatedRowCount] = useState('');
  const [streamTopic, setStreamTopic] = useState('');
  const [streamConsumerGroup, setStreamConsumerGroup] = useState('');
  const [streamKeyFieldsRaw, setStreamKeyFieldsRaw] = useState('');
  const [streamStartOffset, setStreamStartOffset] = useState<StreamingStartOffset>('latest');
  const [streamStartOffsetValue, setStreamStartOffsetValue] = useState('');
  const [streamConsistency, setStreamConsistency] = useState<'AT_LEAST_ONCE' | 'EXACTLY_ONCE'>('AT_LEAST_ONCE');
  const [streamCheckpointInterval, setStreamCheckpointInterval] = useState('60000');
  const [streamOutputLocation, setStreamOutputLocation] = useState('');
  const [streamRuntime, setStreamRuntime] = useState<StreamingRuntimeKind>('foundry_streaming');
  const [streamSourceExactlyOnce, setStreamSourceExactlyOnce] = useState(true);
  const [streamSinkExactlyOnce, setStreamSinkExactlyOnce] = useState(true);
  const [pushStreamId, setPushStreamId] = useState('');
  const [pushDatasetRid, setPushDatasetRid] = useState('');
  const [pushBranch, setPushBranch] = useState('master');
  const [pushTokenRef, setPushTokenRef] = useState('');
  const [pushIdempotencyKey, setPushIdempotencyKey] = useState('');
  const [pushRecordsJson, setPushRecordsJson] = useState('[{"sensor_id":"sensor-1","temperature":72.5}]');
  const [pushSourceConnectorExists, setPushSourceConnectorExists] = useState(false);
  const [pushCanAuthenticate, setPushCanAuthenticate] = useState(true);
  const [pushConformsToSchema, setPushConformsToSchema] = useState(true);

  // webhooks
  const [webhooks, setWebhooks] = useState<WebhookDefinition[]>([]);
  const [webhookName, setWebhookName] = useState('');
  const [webhookMethod, setWebhookMethod] = useState<WebhookHttpMethod>('POST');
  const [webhookPath, setWebhookPath] = useState('/');
  const [webhookQueryRaw, setWebhookQueryRaw] = useState('');
  const [webhookHeadersRaw, setWebhookHeadersRaw] = useState('Content-Type: application/json');
  const [webhookBody, setWebhookBody] = useState('{}');
  const [webhookAuthRef, setWebhookAuthRef] = useState('');
  const [webhookTimeoutMs, setWebhookTimeoutMs] = useState('30000');
  const [webhookRetryAttempts, setWebhookRetryAttempts] = useState('3');
  const [webhookInputsJson, setWebhookInputsJson] = useState('[{"name":"request_id","type":"string","required":true}]');
  const [webhookOutputsJson, setWebhookOutputsJson] = useState('[{"name":"status","type":"integer","extractor":{"kind":"http_status"}}]');
  const [webhookInvocations, setWebhookInvocations] = useState<Record<string, WebhookInvocationRecord[]>>({});

  // media-syncs
  const [mediaSyncs, setMediaSyncs] = useState<MediaSetSyncDef[]>([]);

  const catalogEntry: ConnectorCatalogEntry | undefined = source
    ? FALLBACK_CONNECTOR_CATALOG.find((e) => e.type === source.connector_type)
    : undefined;
  const registryEntry = catalogEntry ? getConnectorRegistryEntry(catalogEntry) : null;
  const virtualTableProvider = source ? virtualTableProviderFor(source.connector_type) : null;
  const selectedDiscovered = discovered.filter((d) => selectedSelectors[d.selector]);

  async function loadOverview() {
    setLoading(true);
    setError('');
    try {
      const loaded = await dataConnection.getSource(id);
      setSource(loaded);
      setEditName(loaded.name);
      setEditDescription(loaded.description ?? '');
      setEditProjectRid(loaded.project_rid ?? '');
      setEditFolderRid(loaded.folder_rid ?? '');
      setEditOwnerId(loaded.owner_id ?? '');
      setEditWorker(loaded.worker);
      setEditOutputLocation(loaded.default_output_location ?? '');
      setDuplicateName(`${loaded.name} copy`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load source');
    } finally {
      setLoading(false);
    }
  }

  async function loadNetworking() {
    try {
      const [att, all] = await Promise.all([
        dataConnection.listSourcePolicies(id),
        dataConnection.listEgressPolicies(),
      ]);
      setAttached(att);
      const attachedIds = new Set(att.map((p) => p.id));
      setAvailable(all.filter((p) => !attachedIds.has(p.id)));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load networking');
    }
  }

  async function loadCredentials() {
    try {
      setCredentials(await dataConnection.listCredentials(id));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load credentials');
    }
  }

  async function loadRegistrations() {
    setRegistrationsLoading(true);
    try {
      setRegistrations(await dataConnection.listRegistrations(id));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load registrations');
    } finally {
      setRegistrationsLoading(false);
    }
  }

  async function loadSyncs() {
    try {
      const list = await dataConnection.listSyncs(id);
      setSyncs(list);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load syncs');
    }
  }

  async function loadStreams() {
    try {
      const list = await dataConnection.listSourceStreams(id);
      setStreams(list);
      if (!pushStreamId && list[0]) {
        setPushStreamId(list[0].id);
        setPushDatasetRid(list[0].rid ?? list[0].id);
        setPushBranch(list[0].branch);
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load streams');
    }
  }

  async function loadRuns(syncId: string) {
    try {
      setRunsBySync((prev) => ({ ...prev, [syncId]: [] }));
      const runs = await dataConnection.listRuns(syncId);
      setRunsBySync((prev) => ({ ...prev, [syncId]: runs }));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load runs');
    }
  }

  async function loadWebhooks() {
    try {
      setWebhooks(await dataConnection.listWebhooks(id));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load webhooks');
    }
  }

  async function loadWebhookInvocations(webhookId: string) {
    try {
      const records = await dataConnection.listWebhookInvocations(id, webhookId);
      setWebhookInvocations((prev) => ({ ...prev, [webhookId]: retainWebhookInvocations(records, new Date().toISOString()) }));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load webhook history');
    }
  }

  async function loadMediaSyncs() {
    try {
      setMediaSyncs(await dataConnection.listMediaSetSyncs(id));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load media syncs');
    }
  }

  useEffect(() => {
    if (id) void loadOverview();
  }, [id]);

  function selectTab(next: Tab) {
    setTab(next);
    if (next === 'networking') void loadNetworking();
    if (next === 'credentials') void loadCredentials();
    if (next === 'explore') void loadRegistrations();
    if (next === 'syncs') void loadSyncs();
    if (next === 'streams') void loadStreams();
    if (next === 'webhooks') void loadWebhooks();
    if (next === 'media-syncs') void loadMediaSyncs();
  }

  async function deleteSource() {
    if (typeof window !== 'undefined' && !window.confirm('Delete source?')) return;
    setBusy(true);
    try {
      await dataConnection.deleteSource(id);
      navigate('/data-connection');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Delete failed');
      setBusy(false);
    }
  }

  async function saveConfiguration() {
    if (registryEntry) {
      const compatibility = validateConnectorWorker(registryEntry, editWorker);
      if (!compatibility.valid) {
        setError(compatibility.reason ?? 'Selected worker is not compatible with this connector.');
        return;
      }
    }
    setBusy(true);
    setError('');
    try {
      const updated = await dataConnection.updateSource(id, {
        name: editName.trim() || source?.name,
        description: editDescription.trim() || null,
        project_rid: editProjectRid.trim() || null,
        folder_rid: editFolderRid.trim() || null,
        owner_id: editOwnerId.trim() || null,
        worker: editWorker,
        default_output_location: editOutputLocation.trim() || null,
      });
      setSource(updated);
      setEditOpen(false);
      await loadOverview();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Update failed');
    } finally {
      setBusy(false);
    }
  }

  async function duplicateSource() {
    if (!duplicateName.trim()) return;
    setBusy(true);
    setError('');
    try {
      const duplicate = await dataConnection.duplicateSource(id, {
        name: duplicateName.trim(),
        description: source?.description ?? undefined,
        project_rid: source?.project_rid ?? undefined,
        folder_rid: source?.folder_rid ?? undefined,
        copy_credentials: true,
        copy_network_policies: true,
      });
      navigate(`/data-connection/sources/${encodeURIComponent(duplicate.id)}`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Duplicate failed');
    } finally {
      setBusy(false);
    }
  }

  async function archiveSource() {
    setBusy(true);
    setError('');
    try {
      const archived = await dataConnection.archiveSource(id, { reason: archiveReason.trim() || undefined });
      setSource(archived);
      await loadOverview();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Archive failed');
    } finally {
      setBusy(false);
    }
  }

  async function validateEgressBeforeTest() {
    const expectedKind: EgressPolicyKind = source?.worker === 'agent' ? 'agent_proxy' : 'direct';
    const policies = attached.length > 0 ? attached : await dataConnection.listSourcePolicies(id);
    setAttached(policies);
    const validationIssues = validateEgressPoliciesForConnectionTest(policies, {
      expectedKind,
      organizationId: source?.organization_id,
    }).filter((issue) => issue.severity === 'error');
    if (validationIssues.length > 0) {
      throw new Error(validationIssues.map((issue) => issue.message).join(' '));
    }
  }

  async function testConnection() {
    setBusy(true);
    setError('');
    try {
      await validateEgressBeforeTest();
      setTestResult(await dataConnection.testConnection(id));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Test failed');
    } finally {
      setBusy(false);
    }
  }

  async function exploreSource(selector = exploreSelector, cursor?: string | null) {
    setBusy(true);
    setError('');
    setRegistrationMessage('');
    try {
      const response = await dataConnection.exploreSource(id, {
        selector: selector.trim() || undefined,
        cursor: cursor ?? undefined,
        include_sample: includeSamples,
        sample_limit: includeSamples ? 10 : 0,
      });
      setExplorationSession(response.session);
      setExplorationNodes(cursor ? [...explorationNodes, ...response.nodes] : response.nodes);
      setNextExploreCursor(response.next_cursor ?? null);
      setRegistrationMessage(`Exploration session ${response.session.id} inspected ${response.nodes.length} node${response.nodes.length === 1 ? '' : 's'} without storing secrets or sample rows.`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Explore failed');
    } finally {
      setBusy(false);
    }
  }

  async function discoverRegistrations() {
    setBusy(true);
    setError('');
    setRegistrationMessage('');
    setRegistrationErrors([]);
    try {
      const res = await dataConnection.discoverSources(id);
      const alreadyRegistered = new Set(registrations.map((r) => r.selector));
      const nextSelected: Record<string, boolean> = {};
      for (const item of res.sources) {
        nextSelected[item.selector] = !alreadyRegistered.has(item.selector);
      }
      setDiscovered(res.sources);
      setExplorationNodes(res.sources.map((item) => ({
        selector: item.selector,
        display_name: discoveredLabel(item),
        kind: item.source_kind ?? 'entity',
        supports_sync: item.supports_sync,
        supports_zero_copy: item.supports_zero_copy,
        source_signature: item.source_signature,
        schema: item.schema,
        sample_rows: item.sample_rows,
        sample_redacted: item.sample_redacted ?? true,
        unauthorized_sample_count: item.unauthorized_sample_count ?? 0,
        metadata: item.metadata,
      })));
      setSelectedSelectors(nextSelected);
      setRegistrationMessage(`Discovered ${res.sources.length} registrable source${res.sources.length === 1 ? '' : 's'}.`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Discover failed');
    } finally {
      setBusy(false);
    }
  }

  function setAllDiscovered(checked: boolean) {
    const next: Record<string, boolean> = {};
    for (const item of discovered) next[item.selector] = checked;
    setSelectedSelectors(next);
  }

  async function bulkRegisterSelected() {
    if (selectedDiscovered.length === 0) {
      setRegistrationErrors([{ selector: 'selection', error: 'Select at least one discovered source.' }]);
      return;
    }

    const target = targetDatasetId.trim();
    const registrationsBody: BulkRegistrationItem[] = selectedDiscovered.map((item) => ({
      selector: item.selector,
      display_name: discoveredLabel(item),
      source_kind: item.source_kind ?? undefined,
      registration_mode: registrationMode,
      auto_sync: autoSync,
      update_detection: updateDetection,
      target_dataset_id: target || undefined,
      metadata: item.metadata ?? undefined,
    }));

    setBusy(true);
    setRegistrationErrors([]);
    setRegistrationMessage('');
    try {
      const response = await dataConnection.bulkRegister(id, registrationsBody);
      const errors = response.errors ?? [];
      setRegistrationErrors(errors);
      setRegistrationMessage(`Registered ${response.created.length} source${response.created.length === 1 ? '' : 's'}${errors.length ? ` with ${errors.length} error${errors.length === 1 ? '' : 's'}` : ''}.`);
      await loadRegistrations();
      if (errors.length === 0) setBulkDialogOpen(false);
    } catch (cause) {
      setRegistrationErrors([{ selector: 'bulk register', error: cause instanceof Error ? cause.message : 'Register failed' }]);
    } finally {
      setBusy(false);
    }
  }

  async function deleteRegistration(registrationId: string) {
    if (typeof window !== 'undefined' && !window.confirm('Delete registration?')) return;
    setBusy(true);
    setRegistrationMessage('');
    try {
      await dataConnection.deleteRegistration(id, registrationId);
      await loadRegistrations();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Delete registration failed');
    } finally {
      setBusy(false);
    }
  }

  async function attachPolicy() {
    if (!pickPolicyId) return;
    setBusy(true);
    try {
      await dataConnection.attachPolicy(id, pickPolicyId);
      setPickPolicyId('');
      await loadNetworking();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Attach failed');
    } finally {
      setBusy(false);
    }
  }

  async function detachPolicy(policyId: string) {
    setBusy(true);
    try {
      await dataConnection.detachPolicy(id, policyId);
      await loadNetworking();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Detach failed');
    } finally {
      setBusy(false);
    }
  }

  async function setCredential() {
    const needsSecretValue = credStorageMode === 'encrypted_secret';
    if (needsSecretValue && !credValue.trim()) {
      setError('Encrypted credentials require a write-only secret value.');
      return;
    }
    if (credStorageMode === 'external_secret_reference' && !credExternalRef.trim()) {
      setError('External secret credentials require a secret reference.');
      return;
    }
    if (credStorageMode === 'cloud_identity_reference' && !credCloudIdentityRef.trim()) {
      setError('Cloud identity credentials require an identity reference.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      await dataConnection.setCredential(id, {
        kind: credKind,
        storage_mode: credStorageMode,
        value: needsSecretValue ? credValue : undefined,
        external_secret_ref: credStorageMode === 'external_secret_reference' ? credExternalRef.trim() : undefined,
        cloud_identity_ref: credStorageMode === 'cloud_identity_reference' ? credCloudIdentityRef.trim() : undefined,
        secret_version: credVersion.trim() || undefined,
      });
      setCredValue('');
      await loadCredentials();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Set credential failed');
    } finally {
      setBusy(false);
    }
  }

  async function createAndAttachPolicy() {
    const host = newPolicyHost.trim();
    const allowedOrganizations = parseCsvLines(newPolicyOrgs);
    const validationErrors = validateEgressPolicy(makeDraftPolicyForValidation({
      kind: newPolicyKind,
      host,
      port: newPolicyPort,
      protocol: newPolicyProtocol,
      proxyMode: newPolicyProxyMode,
      allowedOrganizations,
    })).filter((issue) => issue.severity === 'error');
    if (validationErrors.length > 0) {
      setError(validationErrors[0].message);
      return;
    }
    setBusy(true);
    setError('');
    try {
      const created = await dataConnection.createEgressPolicy({
        name: newPolicyName.trim() || `${source?.name ?? 'source'} ${newPolicyKind} egress`,
        description: `Created from source ${source?.id ?? id}`,
        kind: newPolicyKind,
        address: { kind: 'host' as EgressEndpointKind, value: host },
        port: { kind: 'single' as EgressPortKind, value: String(Number(newPolicyPort)) },
        protocol: newPolicyProtocol,
        proxy_mode: newPolicyKind === 'agent_proxy' ? newPolicyProxyMode : 'none',
        status: 'pending_review',
        allowed_organizations: allowedOrganizations,
        is_global: false,
        permissions: [],
      });
      await dataConnection.attachPolicy(id, created.id, created.kind);
      setNewPolicyName('');
      setNewPolicyHost('');
      setNewPolicyPort('443');
      setNewPolicyOrgs('');
      await loadNetworking();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Create and attach policy failed');
    } finally {
      setBusy(false);
    }
  }

  async function createSync() {
    const selectedOutputKind = defaultOutputKindForCapability(newSyncCapability);
    const selector = newSourceSelector.trim() || selectedDiscovered[0]?.selector || explorationNodes.find((node) => node.supports_sync !== false)?.selector;
    const outputDataset = selectedOutputKind === 'dataset'
      ? (newOutputDataset.trim() || (source ? suggestedOutputDatasetId(source, selector) : ''))
      : newOutputDataset.trim();
    if (selectedOutputKind === 'dataset' && !outputDataset) {
      setError('Select or create an output dataset for batch syncs.');
      return;
    }
    const includeGlobs = parseGlobList(includeGlobsRaw);
    const excludeGlobs = parseGlobList(excludeGlobsRaw);
    const pathMetadataColumns = parseGlobList(pathMetadataColumnsRaw);
    const fileLimit = fileCountLimit.trim() ? Number(fileCountLimit) : null;
    const fileSync = makeFileSyncSettings({
      mode: fileSyncMode,
      exclude_already_synced: excludeAlreadySynced,
      file_count_limit: fileLimit,
      include_globs: includeGlobs,
      exclude_globs: excludeGlobs,
      include_path_metadata: includePathMetadata,
      path_metadata_columns: pathMetadataColumns,
      historical_snapshot_cutoff: historicalCutoff.trim() || null,
      incremental_recent_window: incrementalRecentWindow.trim() || null,
      low_level: {
        file_glob: newFileGlob.trim() || null,
        emitted_transaction_type: datasetTransactionTypeForFileMode(fileSyncMode),
      },
    });
    const tableNames = parseGlobList(tableNamesRaw || selector || '');
    const tableSelections: TableBatchSyncSelection[] = tableNames.map((tableName) => ({
      source_table: tableName,
      destination_dataset_id: newOutputDataset.trim() || (source ? suggestedOutputDatasetId(source, tableName) : outputDataset),
      source_schema: null,
      destination_schema: null,
      estimated_row_count: estimatedRowCount.trim() ? Number(estimatedRowCount) : null,
      incremental_column: tableIncrementalColumn.trim() || null,
      last_transaction_id: null,
    }));
    const tableSync = makeTableBatchSyncSettings({
      mode: tableSyncMode,
      selected_tables: tableSelections,
      infer_schema: inferTableSchema,
      incremental_column: tableIncrementalColumn.trim() || null,
      row_count: estimatedRowCount.trim() ? Number(estimatedRowCount) : null,
      transaction_ids: [],
    });
    const warnings = [...fileSync.warnings ?? [], ...tableSync.warnings ?? []];
    if (warnings.some((warning) => warning.severity === 'error')) {
      setError(warnings.filter((warning) => warning.severity === 'error').map((warning) => warning.message).join(' '));
      return;
    }

    setBusy(true);
    setError(warnings.map((warning) => warning.message).join(' '));
    try {
      await dataConnection.createSync({
        source_id: id,
        capability_type: newSyncCapability,
        output_kind: selectedOutputKind,
        output_dataset_id: outputDataset,
        source_selector: selector,
        source_path: selector,
        source_table: tableSelections[0]?.source_table ?? selector,
        source_topic: selector,
        write_mode: tableNames.length > 0 ? (tableSyncMode === 'full_snapshot' ? 'snapshot' : 'incremental') : newWriteMode,
        transaction_mode: newTransactionMode,
        build_integration: newBuildIntegration.trim() || undefined,
        create_output_dataset: createOutputDataset,
        output_folder_rid: source?.folder_rid ?? undefined,
        dataset_transaction_type: tableNames.length > 0 ? datasetTransactionTypeForTableMode(tableSyncMode) : datasetTransactionTypeForFileMode(fileSyncMode),
        file_sync: fileSync,
        table_sync: tableNames.length > 0 ? tableSync : undefined,
        file_glob: newFileGlob || undefined,
        schedule_cron: newScheduleCron || undefined,
      });
      setNewOutputDataset('');
      setNewSourceSelector('');
      setNewFileGlob('');
      setNewBuildIntegration('');
      setNewScheduleCron('');
      await loadSyncs();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Create sync failed');
    } finally {
      setBusy(false);
    }
  }


  async function createStreamingSync() {
    const body: CreateStreamingSyncRequest = {
      source_id: id,
      source_topic: streamTopic.trim(),
      consumer_group: streamConsumerGroup.trim() || null,
      schema: [],
      key_fields: parseGlobList(streamKeyFieldsRaw),
      start_offset: streamStartOffset,
      start_offset_value: streamStartOffsetValue.trim() || null,
      consistency_guarantee: streamConsistency,
      checkpoint_interval_ms: Number(streamCheckpointInterval) || 60000,
      output_stream_location: streamOutputLocation.trim(),
    };
    const consistency = evaluateStreamingConsistency({
      requested: streamConsistency,
      runtime: streamRuntime,
      sourceSupportsExactlyOnce: streamSourceExactlyOnce,
      sinkSupportsExactlyOnce: streamSinkExactlyOnce,
    });
    const warnings = validateStreamingSyncSetup(body);
    if (consistency.downgraded || consistency.duplicate_tolerant_consumers_required) {
      warnings.push({ code: consistency.downgraded ? 'exactly-once-downgraded' : 'duplicate-tolerant-consumers', severity: 'warning', message: consistency.reason ?? 'Streaming consistency requirements changed.' });
    }
    if (warnings.some((warning) => warning.severity === 'error')) {
      setError(warnings.filter((warning) => warning.severity === 'error').map((warning) => warning.message).join(' '));
      return;
    }
    setBusy(true);
    setError(warnings.map((warning) => warning.message).join(' '));
    try {
      const created = await dataConnection.createStreamingSync(body);
      setStreamingSyncs((prev) => [created, ...prev.filter((sync) => sync.id !== created.id)]);
      setStreamTopic('');
      setStreamConsumerGroup('');
      setStreamKeyFieldsRaw('');
      setStreamStartOffsetValue('');
      await loadStreams();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Create streaming sync failed');
    } finally {
      setBusy(false);
    }
  }

  async function pushStreamRecords() {
    let records: Record<string, unknown>[];
    try {
      const parsed = JSON.parse(pushRecordsJson);
      records = Array.isArray(parsed) ? parsed : [parsed];
      if (!records.every((record) => record && typeof record === 'object' && !Array.isArray(record))) {
        throw new Error('Records must be a JSON object or array of objects.');
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Push records must be valid JSON.');
      return;
    }
    const stream = streams.find((item) => item.id === pushStreamId);
    const warnings = validatePushStreamRecords({
      datasetRid: pushDatasetRid,
      branch: pushBranch,
      tokenReferenceId: pushTokenRef,
      records,
      schema: stream?.schema,
      idempotencyKey: pushIdempotencyKey,
    });
    if (warnings.some((warning) => warning.severity === 'error')) {
      setError(warnings.map((warning) => warning.message).join(' '));
      return;
    }
    setBusy(true);
    setError(warnings.map((warning) => warning.message).join(' '));
    try {
      await dataConnection.pushStreamRecords(pushStreamId, {
        dataset_rid: pushDatasetRid.trim(),
        branch: pushBranch.trim(),
        token_reference_id: pushTokenRef.trim(),
        idempotency_key: pushIdempotencyKey.trim() || null,
        records,
      });
      await loadStreams();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Push ingestion failed');
    } finally {
      setBusy(false);
    }
  }

  async function createWebhook() {
    let inputParameters: WebhookParameterMetadata[] = [];
    let outputParameters: WebhookOutputParameterMetadata[] = [];
    try {
      inputParameters = webhookInputsJson.trim() ? JSON.parse(webhookInputsJson) : [];
      outputParameters = webhookOutputsJson.trim() ? JSON.parse(webhookOutputsJson) : [];
      if (!Array.isArray(inputParameters) || !Array.isArray(outputParameters)) throw new Error('Webhook parameters must be JSON arrays.');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Webhook parameter metadata must be valid JSON arrays.');
      return;
    }
    const body: CreateWebhookRequest = {
      name: webhookName.trim(),
      method: webhookMethod,
      relative_path: webhookPath.trim(),
      query_params: parseNameValueLines(webhookQueryRaw),
      headers: parseNameValueLines(webhookHeadersRaw),
      body_template: webhookBody.trim() || null,
      authorization_reference_id: webhookAuthRef.trim() || null,
      input_parameters: inputParameters,
      output_parameters: outputParameters,
      timeout_ms: Number(webhookTimeoutMs) || 30000,
      retry: {
        max_attempts: Number(webhookRetryAttempts) || 1,
        initial_backoff_ms: 1000,
        max_backoff_ms: 30000,
      },
    };
    const warnings = validateWebhookSetup(body);
    if (warnings.some((warning) => warning.severity === 'error')) {
      setError(warnings.map((warning) => warning.message).join(' '));
      return;
    }
    setBusy(true);
    setError(warnings.map((warning) => warning.message).join(' '));
    try {
      const created = await dataConnection.createWebhook(id, body);
      setWebhooks((prev) => [created, ...prev.filter((webhook) => webhook.id !== created.id)]);
      setWebhookName('');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Create webhook failed');
    } finally {
      setBusy(false);
    }
  }

  async function startStreamingSync(syncId: string) {
    setBusy(true);
    try {
      const updated = await dataConnection.startStreamingSync(syncId);
      setStreamingSyncs((prev) => prev.map((sync) => sync.id === syncId ? updated : sync));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Start streaming sync failed');
    } finally {
      setBusy(false);
    }
  }

  async function stopStreamingSync(syncId: string) {
    setBusy(true);
    try {
      const updated = await dataConnection.stopStreamingSync(syncId);
      setStreamingSyncs((prev) => prev.map((sync) => sync.id === syncId ? updated : sync));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Stop streaming sync failed');
    } finally {
      setBusy(false);
    }
  }

  async function runSync(syncId: string) {
    setBusy(true);
    try {
      await dataConnection.runSync(syncId);
      await loadRuns(syncId);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Run sync failed');
    } finally {
      setBusy(false);
    }
  }

  if (loading) {
    return (
      <section className="of-page" style={{ padding: 24 }}>
        <p className="of-text-muted">Loading source…</p>
      </section>
    );
  }

  if (!source) {
    return (
      <section className="of-page" style={{ padding: 24 }}>
        <Link to="/data-connection" style={{ color: 'var(--text-muted)', fontSize: 13 }}>← Sources</Link>
        <p className="of-status-danger" style={{ marginTop: 12 }}>{error || 'Source not found'}</p>
      </section>
    );
  }

  const capabilities = sourceCapabilities(source, registryEntry);
  const health = sourceHealth(source);
  const usage = sourceUsage(source);
  const audit = sourceAudit(source);
  const editWorkerCompatibility = registryEntry ? validateConnectorWorker(registryEntry, editWorker) : null;
  const unavailableForEditWorker = registryEntry ? unavailableCapabilitiesForWorker(registryEntry, editWorker) : [];
  const tabs: Array<Tab | { id: Tab; label: string }> = [
    'overview',
    'configuration',
    'credentials',
    'networking',
    'explore',
    { id: 'syncs', label: 'Syncs' },
    { id: 'streams', label: 'Streams' },
    'exports',
    'webhooks',
    { id: 'virtual-tables', label: 'Virtual tables' },
    { id: 'code-imports', label: 'Code imports' },
    'permissions',
    'history',
    'capabilities',
  ];
  if (MEDIA_SYNC_CONNECTORS.has(source.connector_type)) tabs.push({ id: 'media-syncs', label: 'Media syncs' });

  return (
    <section className="of-page" style={{ padding: 24, display: 'grid', gap: 16 }}>
      <Link to="/data-connection" style={{ color: 'var(--text-muted)', fontSize: 13 }}>← Sources</Link>
      <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 12 }}>
        <div>
          <h1 className="of-heading-xl">{source.name}</h1>
          <p className="of-text-muted" style={{ marginTop: 4, fontSize: 12 }}>
            {source.id} · {source.connector_type} · worker: {source.worker} · status: {source.status}
          </p>
        </div>
        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
          <button type="button" onClick={() => void testConnection()} disabled={busy} className="of-button">Test connection</button>
          <button type="button" onClick={() => setEditOpen((open) => !open)} disabled={busy} className="of-button">Edit</button>
          <button type="button" onClick={() => void duplicateSource()} disabled={busy || !duplicateName.trim()} className="of-button">Duplicate</button>
          <button type="button" onClick={() => void archiveSource()} disabled={busy} className="of-button" style={{ color: '#92400e', borderColor: '#fde68a' }}>Archive</button>
          <button type="button" onClick={() => void deleteSource()} disabled={busy} className="of-button" style={{ color: '#b91c1c', borderColor: '#fecaca' }}>
            Delete
          </button>
        </div>
      </header>

      {error && (
        <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {error}
        </div>
      )}

      {testResult && (
        <div style={{ padding: 10, background: testResult.success ? '#d1fae5' : '#fee2e2', borderRadius: 8, fontSize: 12, display: 'grid', gap: 6 }}>
          <div>
            <strong>{testResult.success ? '✓' : '✗'}</strong> {testResult.message}
            {testResult.latency_ms !== null && ` · ${testResult.latency_ms}ms`}
            {testResult.tested_at ? ` · ${testResult.tested_at}` : ''}
          </div>
          {(testResult.checks ?? []).length > 0 ? (
            <ul style={{ margin: 0, paddingLeft: 18 }}>
              {(testResult.checks ?? []).map((check) => (
                <li key={check.name}>
                  {check.status} · {check.name}: {check.message}{check.latency_ms !== null ? ` · ${check.latency_ms}ms` : ''}
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      )}

      <Tabs tabs={tabs} active={tab} onChange={selectTab} />

      {editOpen && (
        <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
          <div>
            <p className="of-eyebrow">Edit source</p>
            <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
              Update source metadata without exposing stored secret values.
            </p>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 10 }}>
            <LabeledInput label="Name" value={editName} onChange={setEditName} />
            <LabeledInput label="Description" value={editDescription} onChange={setEditDescription} />
            <LabeledInput label="Project RID" value={editProjectRid} onChange={setEditProjectRid} />
            <LabeledInput label="Folder RID" value={editFolderRid} onChange={setEditFolderRid} />
            <LabeledInput label="Owner ID" value={editOwnerId} onChange={setEditOwnerId} />
            <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
              Worker
              <select value={editWorker} onChange={(event) => setEditWorker(event.target.value as SourceWorker)} className="of-input">
                {WORKER_CHOICES.map((item) => {
                  const compatibility = registryEntry ? validateConnectorWorker(registryEntry, item) : null;
                  return (
                    <option key={item} value={item} disabled={compatibility ? !compatibility.valid : false}>
                      {workerLabel(item)}{compatibility && !compatibility.valid ? ' (unavailable)' : ''}
                    </option>
                  );
                })}
              </select>
            </label>
            <LabeledInput label="Default output location" value={editOutputLocation} onChange={setEditOutputLocation} />
            <LabeledInput label="Duplicate name" value={duplicateName} onChange={setDuplicateName} />
            <LabeledInput label="Archive reason" value={archiveReason} onChange={setArchiveReason} />
          </div>
          {editWorkerCompatibility && (
            <WorkerCompatibilityPanel
              valid={editWorkerCompatibility.valid}
              worker={editWorker}
              allowed={editWorkerCompatibility.allowedCapabilities}
              unavailable={unavailableForEditWorker}
              reason={editWorkerCompatibility.reason}
            />
          )}
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
            <button type="button" onClick={() => setEditOpen(false)} disabled={busy} className="of-button">Cancel</button>
            <button type="button" onClick={() => void saveConfiguration()} disabled={busy || !editName.trim() || (editWorkerCompatibility ? !editWorkerCompatibility.valid : false)} className="of-button of-button--primary">Save changes</button>
          </div>
        </section>
      )}

      {tab === 'overview' && (
        <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 14 }}>
          <div>
            <p className="of-eyebrow">Source overview</p>
            <h2 className="of-section-title" style={{ marginTop: 4 }}>{source.name}</h2>
            <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
              {source.description || 'No description yet.'}
            </p>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(190px, 1fr))', gap: 10, fontSize: 12 }}>
            <RegistryField label="Connector type" value={source.connector_type} />
            <RegistryField label="Project / folder" value={`${source.project_rid ?? 'No project'} / ${source.folder_rid ?? 'No folder'}`} />
            <RegistryField label="Owner" value={ownerLabel(source)} />
            <RegistryField label="Worker" value={workerLabel(source.worker)} />
            <RegistryField label="Network policy" value={source.network_policy_id ?? 'No network policy attached'} />
            <RegistryField label="Credential references" value={(source.credential_reference_ids ?? []).length > 0 ? (source.credential_reference_ids ?? []).join(', ') : 'No credential references'} />
            <RegistryField label="Default output" value={source.default_output_location ?? 'No default output location'} />
            <RegistryField label="Health" value={`${health.state}${health.message ? ` · ${health.message}` : ''}`} />
            <RegistryField label="Usage" value={`${usage.sync_count} syncs · ${usage.export_count} exports · ${usage.webhook_count} webhooks · ${usage.virtual_table_count} virtual tables`} />
            <RegistryField label="Audit" value={`created by ${audit.created_by ?? 'unknown'} · updated by ${audit.updated_by ?? 'unknown'}${audit.last_event_id ? ` · event ${audit.last_event_id}` : ''}`} />
          </div>
          <div>
            <span className="of-text-muted" style={{ fontSize: 12 }}>Supported capabilities</span>
            <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap', marginTop: 4 }}>
              {capabilities.map((capability) => (
                <span key={capability} style={{ fontSize: 10, padding: '2px 6px', background: 'var(--bg-subtle)', borderRadius: 999 }}>
                  {capabilityLabel(capability)}
                </span>
              ))}
              {capabilities.length === 0 ? <span className="of-text-muted" style={{ fontSize: 12 }}>No capabilities registered.</span> : null}
            </div>
          </div>
          {registryEntry ? (
            <div style={{ display: 'grid', gap: 12 }}>
              <div>
                <p className="of-eyebrow">Connector registry</p>
                <h2 className="of-section-title" style={{ marginTop: 4 }}>{registryEntry.name}</h2>
                <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>{registryEntry.description}</p>
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(190px, 1fr))', gap: 10, fontSize: 12 }}>
                <RegistryField label="Category" value={connectorCategoryLabel(registryEntry.category)} />
                <RegistryField label="Workers" value={registryEntry.workers.join(', ')} />
                <RegistryField label="Credentials" value={registryEntry.credentialFields.length === 0 ? 'No secret fields' : registryEntry.credentialFields.map((field) => `${field.label}${field.required ? ' *' : ''}`).join(', ')} />
                <RegistryField label="Network" value={`${registryEntry.network.modes.join(', ')}${registryEntry.network.defaultPorts.length > 0 ? ` · ports ${registryEntry.network.defaultPorts.join(', ')}` : ''}`} />
                <RegistryField label="Setup docs" value={registryEntry.setupDocsUrl} href={registryEntry.setupDocsUrl} />
              </div>
              <div>
                <span className="of-text-muted" style={{ fontSize: 12 }}>Capabilities</span>
                <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap', marginTop: 4 }}>
                  {registryEntry.capabilities.map((capability) => (
                    <span key={capability} style={{ fontSize: 10, padding: '2px 6px', background: 'var(--bg-subtle)', borderRadius: 999 }}>
                      {capabilityLabel(capability)}
                    </span>
                  ))}
                </div>
              </div>
            </div>
          ) : null}
          <pre style={{ padding: 12, background: 'var(--bg-subtle)', fontSize: 11, fontFamily: 'var(--font-mono)', borderRadius: 12, overflow: 'auto' }}>
            {JSON.stringify(source, null, 2)}
          </pre>
        </section>
      )}

      {tab === 'configuration' && (
        <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 14 }}>
          <div>
            <p className="of-eyebrow">Configuration</p>
            <h2 className="of-section-title" style={{ marginTop: 4 }}>Source configuration metadata</h2>
            <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
              Secrets remain write-only; this page shows safe metadata and editable source routing fields.
            </p>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(190px, 1fr))', gap: 10, fontSize: 12 }}>
            <RegistryField label="Name" value={source.name} />
            <RegistryField label="Description" value={source.description ?? 'No description'} />
            <RegistryField label="Connector type" value={source.connector_type} />
            <RegistryField label="Worker" value={source.worker} />
            <RegistryField label="Project RID" value={source.project_rid ?? 'No project'} />
            <RegistryField label="Folder RID" value={source.folder_rid ?? 'No folder'} />
            <RegistryField label="Owner" value={ownerLabel(source)} />
            <RegistryField label="Default output location" value={source.default_output_location ?? 'No default output'} />
          </div>
          <pre style={{ padding: 12, background: 'var(--bg-subtle)', fontSize: 11, fontFamily: 'var(--font-mono)', borderRadius: 12, overflow: 'auto' }}>
            {JSON.stringify({
              id: source.id,
              connector_type: source.connector_type,
              worker: source.worker,
              project_rid: source.project_rid ?? null,
              folder_rid: source.folder_rid ?? null,
              owner_id: source.owner_id ?? null,
              network_policy_id: source.network_policy_id ?? null,
              credential_reference_ids: source.credential_reference_ids ?? [],
              default_output_location: source.default_output_location ?? null,
              supported_capabilities: capabilities,
            }, null, 2)}
          </pre>
        </section>
      )}

      {tab === 'explore' && (
        <>
          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
            <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
              <div>
                <p className="of-eyebrow">Discovery</p>
                <h2 className="of-section-title" style={{ marginTop: 4 }}>Registrable sources</h2>
              </div>
              <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                <button type="button" onClick={() => void discoverRegistrations()} disabled={busy} className="of-button">
                  Discover
                </button>
                <button type="button" onClick={() => setAllDiscovered(true)} disabled={busy || discovered.length === 0} className="of-button">
                  Select all
                </button>
                <button type="button" onClick={() => setAllDiscovered(false)} disabled={busy || discovered.length === 0} className="of-button">
                  Clear
                </button>
                <button type="button" onClick={() => setBulkDialogOpen(true)} disabled={busy || selectedDiscovered.length === 0} className="of-button of-button--primary">
                  Bulk register
                </button>
              </div>
            </header>


            <section className="of-panel-muted" style={{ padding: 12, display: 'grid', gap: 10 }}>
              <div>
                <p className="of-eyebrow">Exploration session</p>
                <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
                  Browse folders, schemas, tables, topics, queues, and redacted samples. Sessions persist selectors and audit metadata, not secrets or unauthorized sample data.
                </p>
              </div>
              <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
                <input value={exploreSelector} onChange={(event) => setExploreSelector(event.target.value)} placeholder="optional selector/path" className="of-input" />
                <label style={{ display: 'inline-flex', gap: 6, alignItems: 'center', fontSize: 12 }}>
                  <input type="checkbox" checked={includeSamples} onChange={(event) => setIncludeSamples(event.target.checked)} />
                  Request redacted sample rows
                </label>
                <button type="button" onClick={() => void exploreSource()} disabled={busy} className="of-button of-button--primary">
                  Explore
                </button>
                {nextExploreCursor ? (
                  <button type="button" onClick={() => void exploreSource(exploreSelector, nextExploreCursor)} disabled={busy} className="of-button">
                    Load more
                  </button>
                ) : null}
              </div>
              {explorationSession ? (
                <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', fontSize: 11 }}>
                  <span className="of-chip">session {explorationSession.id}</span>
                  <span className="of-chip">{explorationSession.status}</span>
                  <span className="of-chip">selectors {explorationSession.selectors_examined}</span>
                  <span className="of-chip">sample rows stored {explorationSession.sample_rows_stored}</span>
                  <span className="of-chip">secrets persisted: {String(explorationSession.secrets_persisted)}</span>
                </div>
              ) : null}
            </section>

            {registrationMessage && (
              <div style={{ padding: '8px 10px', borderRadius: 6, background: '#ecfdf5', color: '#047857', fontSize: 12 }}>
                {registrationMessage}
              </div>
            )}

            {registrationErrors.length > 0 && (
              <div className="of-status-danger" style={{ padding: '8px 10px', borderRadius: 6, fontSize: 12 }}>
                <ul style={{ margin: 0, paddingLeft: 18 }}>
                  {registrationErrors.map((item) => (
                    <li key={`${item.selector}-${item.error}`}>
                      <code>{item.selector}</code>: {item.error}
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {discovered.length === 0 && explorationNodes.length === 0 ? (
              <div className="of-panel-muted" style={{ padding: 16, color: 'var(--text-muted)', fontSize: 13 }}>
                No discovery or exploration results loaded.
              </div>
            ) : (
              <div style={{ overflow: 'auto', border: '1px solid var(--border-subtle)', borderRadius: 8 }}>
                <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
                  <thead>
                    <tr>
                      <th style={tableHeaderStyle}>Pick</th>
                      <th style={tableHeaderStyle}>Source</th>
                      <th style={tableHeaderStyle}>Kind</th>
                      <th style={tableHeaderStyle}>Mode</th>
                      <th style={tableHeaderStyle}>Signature</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(discovered.length > 0 ? discovered : explorationNodes.map((node) => ({
                      selector: node.selector,
                      display_name: node.display_name,
                      source_kind: node.kind,
                      supports_sync: node.supports_sync,
                      supports_zero_copy: node.supports_zero_copy,
                      source_signature: node.source_signature,
                      schema: node.schema,
                      sample_rows: node.sample_rows,
                      sample_redacted: node.sample_redacted,
                      unauthorized_sample_count: node.unauthorized_sample_count,
                    }))).map((item) => (
                      <tr key={item.selector}>
                        <td style={tableCellStyle}>
                          <input
                            type="checkbox"
                            checked={Boolean(selectedSelectors[item.selector])}
                            onChange={(event) => setSelectedSelectors((prev) => ({ ...prev, [item.selector]: event.target.checked }))}
                            aria-label={`Select ${discoveredLabel(item)}`}
                          />
                        </td>
                        <td style={tableCellStyle}>
                          <strong>{discoveredLabel(item)}</strong>
                          <div style={{ marginTop: 2, fontFamily: 'var(--font-mono)', color: 'var(--text-muted)' }}>{item.selector}</div>
                        </td>
                        <td style={tableCellStyle}>{item.source_kind ?? '-'}</td>
                        <td style={tableCellStyle}>
                          {item.supports_zero_copy ? <span className="of-chip">zero-copy</span> : null}
                          {item.supports_sync !== false ? <span className="of-chip" style={{ marginLeft: item.supports_zero_copy ? 4 : 0 }}>sync</span> : null}
                        </td>
                        <td style={{ ...tableCellStyle, fontFamily: 'var(--font-mono)', color: 'var(--text-muted)' }}>
                          {item.source_signature ?? '-'}
                          {(item.schema ?? []).length > 0 ? <div>{(item.schema ?? []).slice(0, 3).map((field) => field.name).join(', ')}</div> : null}
                          {item.sample_rows?.length ? <div>{item.sample_redacted ? 'redacted sample' : 'sample'}: {item.sample_rows.length} row(s)</div> : null}
                          {item.unauthorized_sample_count ? <div>{item.unauthorized_sample_count} unauthorized sample row(s) withheld</div> : null}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>

          <section className="of-panel" style={{ padding: 16 }}>
            <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
              <div>
                <p className="of-eyebrow">Registered ({registrations.length})</p>
                <h2 className="of-section-title" style={{ marginTop: 4 }}>Current registrations</h2>
              </div>
              <button type="button" onClick={() => void loadRegistrations()} disabled={busy || registrationsLoading} className="of-button">
                Refresh
              </button>
            </header>
            {registrationsLoading ? (
              <p className="of-text-muted" style={{ marginTop: 12, fontSize: 13 }}>Loading registrations...</p>
            ) : registrations.length === 0 ? (
              <p className="of-text-muted" style={{ marginTop: 12, fontSize: 13 }}>No registrations yet.</p>
            ) : (
              <div style={{ marginTop: 12, overflow: 'auto', border: '1px solid var(--border-subtle)', borderRadius: 8 }}>
                <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
                  <thead>
                    <tr>
                      <th style={tableHeaderStyle}>Name</th>
                      <th style={tableHeaderStyle}>Selector</th>
                      <th style={tableHeaderStyle}>Mode</th>
                      <th style={tableHeaderStyle}>Target</th>
                      <th style={tableHeaderStyle}>Automation</th>
                      <th style={tableHeaderStyle}></th>
                    </tr>
                  </thead>
                  <tbody>
                    {registrations.map((registration) => (
                      <tr key={registration.id}>
                        <td style={tableCellStyle}>
                          <strong>{registration.display_name || registration.selector}</strong>
                          <div style={{ color: 'var(--text-muted)' }}>{registration.source_kind ?? '-'}</div>
                        </td>
                        <td style={{ ...tableCellStyle, fontFamily: 'var(--font-mono)' }}>{registration.selector}</td>
                        <td style={tableCellStyle}>{registration.registration_mode ?? '-'}</td>
                        <td style={{ ...tableCellStyle, fontFamily: 'var(--font-mono)' }}>{registration.target_dataset_id ?? '-'}</td>
                        <td style={tableCellStyle}>
                          {registration.auto_sync ? <span className="of-chip">auto sync</span> : null}
                          {registration.update_detection ? <span className="of-chip" style={{ marginLeft: registration.auto_sync ? 4 : 0 }}>updates</span> : null}
                          {!registration.auto_sync && !registration.update_detection ? '-' : null}
                        </td>
                        <td style={tableCellStyle}>
                          <button type="button" onClick={() => void deleteRegistration(registration.id)} disabled={busy} className="of-button" style={{ fontSize: 11, color: '#b91c1c', borderColor: '#fecaca' }}>
                            Delete
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        </>
      )}

      {tab === 'networking' && (
        <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 14 }}>
          <div>
            <p className="of-eyebrow">Attached policies ({attached.length})</p>
            <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
              Connection tests validate host, port, protocol, proxy mode, policy status, and allowed organizations before contacting external systems.
            </p>
          </div>
          <ul style={{ margin: 0, paddingLeft: 0, listStyle: 'none' }}>
            {attached.map((p) => (
              <li key={p.id} style={{ padding: 8, borderBottom: '1px solid var(--border-subtle)', display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
                <span>
                  <strong>{p.name}</strong> · <code>{p.address.kind}:{p.address.value}:{p.port.kind === 'any' ? 'any' : p.port.value}</code>
                  <span className="of-text-muted" style={{ display: 'block', fontSize: 11 }}>
                    {p.kind} · {p.protocol ?? 'tcp'} · proxy {p.proxy_mode ?? 'none'} · status {p.status ?? 'active'} · orgs {(p.allowed_organizations ?? []).join(', ') || 'any'}
                  </span>
                </span>
                <button type="button" onClick={() => void detachPolicy(p.id)} disabled={busy} className="of-button" style={{ fontSize: 11, color: '#b91c1c', borderColor: '#fecaca' }}>
                  Detach
                </button>
              </li>
            ))}
            {attached.length === 0 && <li className="of-text-muted">No attached policies.</li>}
          </ul>
          <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
            <select value={pickPolicyId} onChange={(e) => setPickPolicyId(e.target.value)} className="of-input">
              <option value="">— pick existing policy —</option>
              {available.map((p) => (
                <option key={p.id} value={p.id}>{p.name} · {p.kind} · {p.status ?? 'active'}</option>
              ))}
            </select>
            <button type="button" onClick={() => void attachPolicy()} disabled={busy || !pickPolicyId} className="of-button of-button--primary">
              Attach existing
            </button>
          </div>
          <section className="of-panel-muted" style={{ padding: 12, display: 'grid', gap: 10 }}>
            <p className="of-eyebrow">Create and attach source policy</p>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))', gap: 8 }}>
              <LabeledInput label="Policy name" value={newPolicyName} onChange={setNewPolicyName} />
              <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>Kind
                <select value={newPolicyKind} onChange={(event) => setNewPolicyKind(event.target.value as EgressPolicyKind)} className="of-input">
                  <option value="direct">Direct egress</option>
                  <option value="agent_proxy">Agent proxy</option>
                </select>
              </label>
              <LabeledInput label="Host" value={newPolicyHost} onChange={setNewPolicyHost} />
              <LabeledInput label="Port" value={newPolicyPort} onChange={setNewPolicyPort} />
              <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>Protocol
                <select value={newPolicyProtocol} onChange={(event) => setNewPolicyProtocol(event.target.value as EgressProtocol)} className="of-input">
                  {EGRESS_PROTOCOLS.map((protocol) => <option key={protocol} value={protocol}>{protocol}</option>)}
                </select>
              </label>
              <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>Proxy mode
                <select value={newPolicyProxyMode} onChange={(event) => setNewPolicyProxyMode(event.target.value as AgentProxyMode)} className="of-input" disabled={newPolicyKind !== 'agent_proxy'}>
                  {PROXY_MODES.map((mode) => <option key={mode} value={mode}>{mode}</option>)}
                </select>
              </label>
              <LabeledInput label="Allowed orgs" value={newPolicyOrgs} onChange={setNewPolicyOrgs} />
            </div>
            <button type="button" onClick={() => void createAndAttachPolicy()} disabled={busy || !newPolicyHost.trim()} className="of-button of-button--primary" style={{ justifySelf: 'start' }}>
              Create and attach
            </button>
          </section>
        </section>
      )}

      {tab === 'credentials' && (
        <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 14 }}>
          <div>
            <p className="of-eyebrow">Credentials ({credentials.length})</p>
            <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
              Secret values are write-only. The UI shows encrypted/external-reference metadata, rotation, test status, usage, and audit events.
            </p>
          </div>
          <div style={{ display: 'grid', gap: 8 }}>
            {credentials.map((c) => (
              <div key={c.id} style={{ border: '1px solid var(--border-subtle)', borderRadius: 8, padding: 10, display: 'grid', gap: 6, fontSize: 12 }}>
                <strong>{c.kind}</strong>
                <span>storage: {c.storage_mode ?? 'encrypted_secret'} · version {c.secret_version ?? 'v1'} · fingerprint <code>{c.fingerprint}</code></span>
                <span>rotated: {c.last_rotated_at ?? c.created_at} · created by {c.created_by ?? 'unknown'} · test {c.test_status ?? 'untested'}{c.last_tested_at ? ` at ${c.last_tested_at}` : ''}</span>
                <span>usage: {c.usage?.source_count ?? 1} source(s) · last used {c.usage?.last_used_at ?? 'never'} · external ref {c.external_secret_ref ?? c.cloud_identity_ref ?? 'none'}</span>
                {(c.audit_events ?? []).length > 0 ? (
                  <ul style={{ margin: 0, paddingLeft: 18 }}>
                    {(c.audit_events ?? []).slice(0, 3).map((event) => (
                      <li key={event.id}>{event.created_at} · {event.event_type} · {event.message}</li>
                    ))}
                  </ul>
                ) : null}
              </div>
            ))}
            {credentials.length === 0 && <p className="of-text-muted" style={{ margin: 0 }}>No credentials stored.</p>}
          </div>
          <div style={{ display: 'grid', gap: 8, maxWidth: 720 }}>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 8 }}>
              <label style={{ fontSize: 13 }}>Kind
                <select value={credKind} onChange={(e) => setCredKind(e.target.value as CredentialKind)} className="of-input" style={{ marginTop: 4 }}>
                  {CREDENTIAL_KINDS.map((k) => <option key={k} value={k}>{k}</option>)}
                </select>
              </label>
              <label style={{ fontSize: 13 }}>Storage
                <select value={credStorageMode} onChange={(e) => setCredStorageMode(e.target.value as CredentialStorageMode)} className="of-input" style={{ marginTop: 4 }}>
                  {STORAGE_MODES.map((mode) => <option key={mode} value={mode}>{mode}</option>)}
                </select>
              </label>
              <LabeledInput label="Secret version" value={credVersion} onChange={setCredVersion} />
              <LabeledInput label="External secret ref" value={credExternalRef} onChange={setCredExternalRef} />
              <LabeledInput label="Cloud identity ref" value={credCloudIdentityRef} onChange={setCredCloudIdentityRef} />
            </div>
            <label style={{ fontSize: 13 }}>
              Value (write-only)
              <input type="password" value={credValue} onChange={(e) => setCredValue(e.target.value)} className="of-input" style={{ marginTop: 4 }} disabled={credStorageMode !== 'encrypted_secret'} />
            </label>
            <button type="button" onClick={() => void setCredential()} disabled={busy || (credStorageMode === 'encrypted_secret' && !credValue.trim())} className="of-button of-button--primary" style={{ justifySelf: 'start' }}>
              Save credential metadata
            </button>
          </div>
        </section>
      )}

      {tab === 'capabilities' && (
        <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
          {registryEntry ? (
            <>
              <div>
                <p className="of-eyebrow">{registryEntry.name}</p>
                <p className="of-text-muted" style={{ fontSize: 12 }}>{registryEntry.description}</p>
              </div>
              <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
                <span style={{ fontSize: 10, padding: '2px 6px', background: 'var(--bg-subtle)', borderRadius: 999 }}>
                  {connectorCategoryLabel(registryEntry.category)}
                </span>
                {registryEntry.capabilities.map((c) => (
                  <span key={c} style={{ fontSize: 10, padding: '2px 6px', background: 'var(--bg-subtle)', borderRadius: 999 }}>{capabilityLabel(c)}</span>
                ))}
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(190px, 1fr))', gap: 10, fontSize: 12 }}>
                <RegistryField label="Worker compatibility" value={registryEntry.workers.join(', ')} />
                <RegistryField label="Credential fields" value={registryEntry.credentialFields.length === 0 ? 'No secret fields' : registryEntry.credentialFields.map((field) => `${field.label}${field.secret ? ' (secret)' : ''}`).join(', ')} />
                <RegistryField label="Network requirements" value={registryEntry.network.notes} />
                <RegistryField label="Feature flags" value={Object.entries(registryEntry.featureFlags).filter(([, enabled]) => enabled).map(([flag]) => flag).join(', ') || 'None enabled'} />
              </div>
            </>
          ) : (
            <p className="of-text-muted">No catalog entry for connector type {source.connector_type}.</p>
          )}
        </section>
      )}

      {tab === 'virtual-tables' && (
        virtualTableProvider ? (
          <VirtualTablesTab sourceRid={source.id} provider={virtualTableProvider} />
        ) : (
          <PlaceholderPanel title="Virtual tables" description="This connector does not currently advertise virtual table support." />
        )
      )}

      {tab === 'syncs' && (
        <>
          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 10 }}>
            <div>
              <p className="of-eyebrow">Create streaming sync</p>
              <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
                Long-running syncs read supported topics, queues, or streams into OpenFoundry streams and use start/stop controls instead of one-shot runs.
              </p>
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 8 }}>
              <LabeledInput label="Source topic/queue/stream" value={streamTopic} onChange={setStreamTopic} />
              <LabeledInput label="Consumer group" value={streamConsumerGroup} onChange={setStreamConsumerGroup} />
              <LabeledInput label="Key fields" value={streamKeyFieldsRaw} onChange={setStreamKeyFieldsRaw} />
              <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>Start offset
                <select value={streamStartOffset} onChange={(event) => setStreamStartOffset(event.target.value as StreamingStartOffset)} className="of-input">
                  {STREAMING_START_OFFSETS.map((offset) => <option key={offset} value={offset}>{offset}</option>)}
                </select>
              </label>
              <LabeledInput label="Start offset value" value={streamStartOffsetValue} onChange={setStreamStartOffsetValue} />
              <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>Consistency
                <select value={streamConsistency} onChange={(event) => setStreamConsistency(event.target.value as 'AT_LEAST_ONCE' | 'EXACTLY_ONCE')} className="of-input">
                  <option value="AT_LEAST_ONCE">At least once</option>
                  <option value="EXACTLY_ONCE">Exactly once</option>
                </select>
              </label>
              <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>Runtime
                <select value={streamRuntime} onChange={(event) => setStreamRuntime(event.target.value as StreamingRuntimeKind)} className="of-input">
                  {STREAMING_RUNTIMES.map((runtime) => <option key={runtime} value={runtime}>{runtime}</option>)}
                </select>
              </label>
              <LabeledInput label="Checkpoint interval ms" value={streamCheckpointInterval} onChange={setStreamCheckpointInterval} />
              <LabeledInput label="Output stream location" value={streamOutputLocation} onChange={setStreamOutputLocation} />
            </div>
            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', fontSize: 12 }}>
              <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                <input type="checkbox" checked={streamSourceExactlyOnce} onChange={(event) => setStreamSourceExactlyOnce(event.target.checked)} />
                Source supports exactly-once
              </label>
              <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                <input type="checkbox" checked={streamSinkExactlyOnce} onChange={(event) => setStreamSinkExactlyOnce(event.target.checked)} />
                Sink supports exactly-once
              </label>
            </div>
            <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
              Consistency evaluation: {evaluateStreamingConsistency({ requested: streamConsistency, runtime: streamRuntime, sourceSupportsExactlyOnce: streamSourceExactlyOnce, sinkSupportsExactlyOnce: streamSinkExactlyOnce }).effective}
              {evaluateStreamingConsistency({ requested: streamConsistency, runtime: streamRuntime, sourceSupportsExactlyOnce: streamSourceExactlyOnce, sinkSupportsExactlyOnce: streamSinkExactlyOnce }).reason ? ` · ${evaluateStreamingConsistency({ requested: streamConsistency, runtime: streamRuntime, sourceSupportsExactlyOnce: streamSourceExactlyOnce, sinkSupportsExactlyOnce: streamSinkExactlyOnce }).reason}` : ''}
            </p>
            <button type="button" onClick={() => void createStreamingSync()} disabled={busy || !streamTopic.trim()} className="of-button of-button--primary" style={{ justifySelf: 'start' }}>
              Create streaming sync
            </button>
            {streamingSyncs.length > 0 ? (
              <ul style={{ margin: 0, paddingLeft: 18, fontSize: 12 }}>
                {streamingSyncs.map((sync) => (
                  <li key={sync.id}>
                    <strong>{sync.source_topic}</strong> → {sync.output_stream_id || sync.output_stream_location} · {sync.status} · checkpoint {sync.checkpoint_interval_ms}ms
                    <button type="button" onClick={() => void startStreamingSync(sync.id)} disabled={busy || !streamingSyncCanStart(sync.status)} className="of-button" style={{ marginLeft: 8, fontSize: 11 }}>Start</button>
                    <button type="button" onClick={() => void stopStreamingSync(sync.id)} disabled={busy || !streamingSyncCanStop(sync.status)} className="of-button" style={{ marginLeft: 4, fontSize: 11 }}>Stop</button>
                  </li>
                ))}
              </ul>
            ) : null}
          </section>

          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 10 }}>
            <div>
              <p className="of-eyebrow">Create sync resource</p>
              <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
                Define the source selector, capability, output resource, schema/write behavior, schedule, and build integration. Batch syncs create or select an OpenFoundry dataset output.
              </p>
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 8 }}>
              <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>Capability
                <select value={newSyncCapability} onChange={(event) => {
                  const next = event.target.value as SyncCapabilityType;
                  setNewSyncCapability(next);
                  setNewWriteMode(defaultWriteModeForCapability(next));
                  setNewTransactionMode(defaultTransactionModeForCapability(next));
                }} className="of-input">
                  {SYNC_CAPABILITIES.filter((capability) => capabilities.includes(capability)).map((capability) => <option key={capability} value={capability}>{syncCapabilityLabel(capability)}</option>)}
                </select>
              </label>
              <LabeledInput label="Source path/table/topic" value={newSourceSelector} onChange={setNewSourceSelector} />
              <LabeledInput label="Output dataset / stream / media set" value={newOutputDataset} onChange={setNewOutputDataset} />
              <LabeledInput label="File glob / filter" value={newFileGlob} onChange={setNewFileGlob} />
              <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>Write mode
                <select value={newWriteMode} onChange={(event) => setNewWriteMode(event.target.value as SyncWriteMode)} className="of-input">
                  {SYNC_WRITE_MODES.map((mode) => <option key={mode} value={mode}>{mode}</option>)}
                </select>
              </label>
              <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>Transaction mode
                <select value={newTransactionMode} onChange={(event) => setNewTransactionMode(event.target.value as SyncTransactionMode)} className="of-input">
                  {SYNC_TRANSACTION_MODES.map((mode) => <option key={mode} value={mode}>{mode}</option>)}
                </select>
              </label>
              <LabeledInput label="Schedule / cron" value={newScheduleCron} onChange={setNewScheduleCron} />
              <LabeledInput label="Build integration" value={newBuildIntegration} onChange={setNewBuildIntegration} />
            </div>
            <section className="of-panel-muted" style={{ padding: 12, display: 'grid', gap: 10 }}>
              <div>
                <p className="of-eyebrow">File sync modes</p>
                <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
                  Configure snapshot mirrors, incremental appends, or a historical snapshot followed by recent-file incrementals. Low-level filters are persisted for backend transaction planning.
                </p>
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 8 }}>
                <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>Mode
                  <select value={fileSyncMode} onChange={(event) => {
                    const mode = event.target.value as FileSyncMode;
                    setFileSyncMode(mode);
                    setNewWriteMode(mode === 'snapshot_mirror' ? 'snapshot' : 'append');
                  }} className="of-input">
                    {FILE_SYNC_MODES.map((mode) => <option key={mode} value={mode}>{fileSyncModeLabel(mode)}</option>)}
                  </select>
                </label>
                <LabeledInput label="Include globs" value={includeGlobsRaw} onChange={setIncludeGlobsRaw} />
                <LabeledInput label="Exclude globs" value={excludeGlobsRaw} onChange={setExcludeGlobsRaw} />
                <LabeledInput label="File count limit" value={fileCountLimit} onChange={setFileCountLimit} />
                <LabeledInput label="Historical cutoff" value={historicalCutoff} onChange={setHistoricalCutoff} />
                <LabeledInput label="Recent window" value={incrementalRecentWindow} onChange={setIncrementalRecentWindow} />
                <LabeledInput label="Path metadata columns" value={pathMetadataColumnsRaw} onChange={setPathMetadataColumnsRaw} />
              </div>
              <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', fontSize: 12 }}>
                <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                  <input type="checkbox" checked={excludeAlreadySynced} onChange={(event) => setExcludeAlreadySynced(event.target.checked)} />
                  Exclude already-synced files
                </label>
                <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                  <input type="checkbox" checked={includePathMetadata} onChange={(event) => setIncludePathMetadata(event.target.checked)} />
                  Include path metadata columns
                </label>
              </div>
              <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
                Dataset transaction: {datasetTransactionTypeForFileMode(fileSyncMode)}
              </p>
            </section>

            <section className="of-panel-muted" style={{ padding: 12, display: 'grid', gap: 10 }}>
              <div>
                <p className="of-eyebrow">Table batch syncs</p>
                <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
                  Select one or more discovered tables, infer schemas, and capture row-count and transaction metadata for snapshot or incremental table syncs.
                </p>
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 8 }}>
                <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>Mode
                  <select value={tableSyncMode} onChange={(event) => setTableSyncMode(event.target.value as TableBatchSyncMode)} className="of-input">
                    {TABLE_SYNC_MODES.map((mode) => <option key={mode} value={mode}>{tableBatchSyncModeLabel(mode)}</option>)}
                  </select>
                </label>
                <LabeledInput label="Tables (comma/newline)" value={tableNamesRaw} onChange={setTableNamesRaw} />
                <LabeledInput label="Incremental column" value={tableIncrementalColumn} onChange={setTableIncrementalColumn} />
                <LabeledInput label="Estimated row count" value={estimatedRowCount} onChange={setEstimatedRowCount} />
              </div>
              <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 12 }}>
                <input type="checkbox" checked={inferTableSchema} onChange={(event) => setInferTableSchema(event.target.checked)} />
                Infer source and destination schemas before the first run
              </label>
              <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
                Dataset transaction: {datasetTransactionTypeForTableMode(tableSyncMode)}
              </p>
            </section>

            <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 12 }}>
              <input type="checkbox" checked={createOutputDataset} onChange={(event) => setCreateOutputDataset(event.target.checked)} />
              Create output dataset when no existing dataset is selected
            </label>
            <button type="button" onClick={() => void createSync()} disabled={busy} className="of-button of-button--primary" style={{ justifySelf: 'start' }}>
              Create sync
            </button>
          </section>

          <section className="of-panel" style={{ padding: 16 }}>
            <p className="of-eyebrow">Syncs ({syncs.length})</p>
            <ul style={{ marginTop: 8, paddingLeft: 0, listStyle: 'none' }}>
              {syncs.map((s) => (
                <li key={s.id} style={{ padding: 10, borderBottom: '1px solid var(--border-subtle)' }}>
                  <strong>{s.id}</strong> · {syncCapabilityLabel(s.capability_type ?? 'batch_sync')} → {s.output_dataset_id || s.output_stream_id || s.output_media_set_id}
                  {s.source_selector && <> · source: <code>{s.source_selector}</code></>}
                  {s.write_mode && <> · write: {s.write_mode}</>}
                  {s.transaction_mode && <> · transaction: {s.transaction_mode}</>}
                  {s.health?.state && <> · health: {s.health.state}</>}
                  {s.next_run_at && <> · next: {s.next_run_at}</>}
                  {s.file_glob && <> · glob: <code>{s.file_glob}</code></>}
                  {s.schedule_cron && <> · cron: <code>{s.schedule_cron}</code></>}
                  {s.dataset_transaction_type && <> · dataset tx: {s.dataset_transaction_type}</>}
                  {s.file_sync && <> · file mode: {fileSyncModeLabel(s.file_sync.mode)}</>}
                  {s.table_sync && <> · table mode: {tableBatchSyncModeLabel(s.table_sync.mode)} · tables: {s.table_sync.selected_tables.map((table) => table.source_table).join(', ')}</>}
                  <div style={{ display: 'flex', gap: 6, marginTop: 6 }}>
                    <button type="button" onClick={() => void runSync(s.id)} disabled={busy} className="of-button" style={{ fontSize: 11 }}>
                      Run sync
                    </button>
                    <button type="button" onClick={() => void loadRuns(s.id)} disabled={busy} className="of-button" style={{ fontSize: 11 }}>
                      Refresh runs
                    </button>
                  </div>
                  {runsBySync[s.id] && (
                    <ul style={{ marginTop: 6, paddingLeft: 18, fontSize: 11 }}>
                      {runsBySync[s.id].map((r) => (
                        <li key={r.id}>
                          {syncRunStatusLabel(r.status)} · {r.started_at ? new Date(r.started_at).toLocaleString() : r.queued_at ?? 'not started'} · {syncRunDurationMs(r) !== null ? `${syncRunDurationMs(r)}ms · ` : ''}{r.bytes_written} bytes · {r.files_written} files · rows {r.rows_written ?? r.records_written ?? 0} · retries {r.retry_count ?? 0}
                          {r.worker && ` · worker ${r.worker}`}{r.agent_id && ` · agent ${r.agent_id}`}{r.output_transaction?.transaction_id && ` · tx ${r.output_transaction.transaction_id}`}
                          {r.source_progress?.file_checkpoints?.length ? ` · checkpoints ${r.source_progress.file_checkpoints.length}` : ''}
                          {buildHistoryHref(r.build) ? <> · <a href={buildHistoryHref(r.build) ?? undefined}>Build history</a></> : null}
                          {r.error && ` · ${r.error}`}
                          {(r.logs ?? []).slice(-2).map((log) => <div key={`${r.id}-${log.timestamp}-${log.message}`} className="of-text-muted">{log.timestamp} · {log.level}: {log.message}</div>)}
                        </li>
                      ))}
                      {runsBySync[s.id].length === 0 && <li className="of-text-muted">No runs.</li>}
                    </ul>
                  )}
                </li>
              ))}
              {syncs.length === 0 && <li className="of-text-muted">No syncs yet.</li>}
            </ul>
          </section>
        </>
      )}


      {tab === 'streams' && (
        <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
          <header style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center' }}>
            <div>
              <p className="of-eyebrow">Streams ({streams.length})</p>
              <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
                Tabular streaming resources with hot buffer, cold/archive dataset, branches, checkpoints, replay, consumers, and source sync links.
              </p>
            </div>
            <button type="button" className="of-button" onClick={() => void loadStreams()} disabled={busy}>Refresh streams</button>
          </header>
          <div className="of-panel-muted" style={{ padding: 12, display: 'grid', gap: 10 }}>
            <div>
              <p className="of-eyebrow">Push-based ingestion</p>
              <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
                Authenticated REST push endpoint: {pushStreamEndpointUrl(pushDatasetRid || 'ri.foundry.main.dataset...', pushBranch || 'master')}. {recommendStreamIngestion({ sourceConnectorExists: pushSourceConnectorExists, inboundSystemCanAuthenticate: pushCanAuthenticate, inboundSystemConformsToSchema: pushConformsToSchema }).message}
              </p>
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 8 }}>
              <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>Stream
                <select value={pushStreamId} onChange={(event) => {
                  const next = streams.find((stream) => stream.id === event.target.value);
                  setPushStreamId(event.target.value);
                  if (next) {
                    setPushDatasetRid(next.rid ?? next.id);
                    setPushBranch(next.branch);
                  }
                }} className="of-input">
                  <option value="">Select stream</option>
                  {streams.map((stream) => <option key={stream.id} value={stream.id}>{stream.name}</option>)}
                </select>
              </label>
              <LabeledInput label="Dataset RID" value={pushDatasetRid} onChange={setPushDatasetRid} />
              <LabeledInput label="Branch" value={pushBranch} onChange={setPushBranch} />
              <LabeledInput label="Token reference" value={pushTokenRef} onChange={setPushTokenRef} />
              <LabeledInput label="Idempotency key" value={pushIdempotencyKey} onChange={setPushIdempotencyKey} />
            </div>
            <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>Records JSON
              <textarea className="of-input" value={pushRecordsJson} onChange={(event) => setPushRecordsJson(event.target.value)} rows={4} />
            </label>
            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', fontSize: 12 }}>
              <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}><input type="checkbox" checked={pushSourceConnectorExists} onChange={(event) => setPushSourceConnectorExists(event.target.checked)} />Source connector exists</label>
              <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}><input type="checkbox" checked={pushCanAuthenticate} onChange={(event) => setPushCanAuthenticate(event.target.checked)} />Inbound system can authenticate</label>
              <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}><input type="checkbox" checked={pushConformsToSchema} onChange={(event) => setPushConformsToSchema(event.target.checked)} />Records conform to stream schema</label>
            </div>
            <button type="button" className="of-button of-button--primary" onClick={() => void pushStreamRecords()} disabled={busy || !pushStreamId}>Push records</button>
          </div>
          {streams.length === 0 ? <p className="of-text-muted">No streams linked to this source yet.</p> : null}
          {streams.map((stream) => (
            <article key={stream.id} className="of-panel-muted" style={{ padding: 12, display: 'grid', gap: 10 }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
                <div>
                  <strong>{stream.name}</strong>
                  <div className="of-text-muted" style={{ fontSize: 12 }}>{stream.id} · branch {stream.branch} · {stream.consistency_guarantee}</div>
                </div>
                <span className="of-chip">{stream.health.state}</span>
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(190px, 1fr))', gap: 8, fontSize: 12 }}>
                <RegistryField label="Hot buffer" value={`${stream.hot_buffer.hot_buffer_retention_ms}ms retention · ${stream.hot_buffer.hot_buffer_bytes ?? 0} bytes`} />
                <RegistryField label="Cold/archive dataset" value={stream.cold_storage.cold_dataset_id ?? stream.cold_storage.archive_dataset_id ?? 'Not archived'} />
                <RegistryField label="Archive policy" value={streamArchivePolicyLabel(stream.archive_policy)} />
                <RegistryField label="Hybrid read" value={streamHybridReadLabel(stream.hybrid_read)} />
                <RegistryField label="Offsets" value={`earliest ${stream.offsets.earliest_offset ?? '-'} · latest ${stream.offsets.latest_offset ?? '-'} · committed ${stream.offsets.committed_offset ?? '-'} · lag ${stream.offsets.lag ?? '-'}`} />
                <RegistryField label="Replay" value={streamReplayRangeLabel(stream.replay)} />
                <RegistryField label="Restart plan" value={(stream.restart_plan ?? restartPlanForStream(stream)).can_restart ? `Restart from ${(stream.restart_plan ?? restartPlanForStream(stream)).latest_completed_checkpoint_id}` : ((stream.restart_plan ?? restartPlanForStream(stream)).reason ?? 'Unavailable')} />
                <RegistryField label="Consistency mode" value={stream.consistency ? `${stream.consistency.effective}${stream.consistency.downgraded ? ' (downgraded)' : ''}${stream.consistency.duplicate_tolerant_consumers_required ? ' · duplicate-tolerant consumers required' : ''}` : stream.consistency_guarantee} />
                <RegistryField label="Permissions" value={`readers ${stream.permissions.readers.length} · writers ${stream.permissions.writers.length} · admins ${stream.permissions.admins.length}`} />
                <RegistryField label="Source syncs" value={stream.source_sync_ids.join(', ') || 'None'} />
              </div>
              <div style={{ overflow: 'auto' }}>
                <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
                  <thead><tr><th style={tableHeaderStyle}>Schema</th><th style={tableHeaderStyle}>Checkpoints</th><th style={tableHeaderStyle}>Consumers</th><th style={tableHeaderStyle}>Live/archive view</th></tr></thead>
                  <tbody><tr>
                    <td style={tableCellStyle}>{stream.schema.slice(0, 5).map((field) => `${field.name}:${field.foundry_type}`).join(', ') || '-'}</td>
                    <td style={tableCellStyle}>{stream.checkpoints.slice(0, 3).map((checkpoint) => `${checkpoint.status}@${checkpoint.offset ?? '-'}${checkpoint.last_processed_source_location ? ` ${checkpoint.last_processed_source_location}` : ''}${checkpoint.size_bytes ? ` ${checkpoint.size_bytes}B` : ''}`).join(', ') || '-'}</td>
                    <td style={tableCellStyle}>{stream.consumers.slice(0, 3).map((consumer) => `${consumer.name} lag ${consumer.lag ?? '-'}`).join(', ') || '-'}</td>
                    <td style={tableCellStyle}>{[...(stream.live_view ?? []), ...(stream.archive_view ?? [])].slice(0, 3).map((row) => `${streamStorageLabel(row.source)}#${row.offset}`).join(', ') || '-'}</td>
                  </tr></tbody>
                </table>
              </div>
            </article>
          ))}
        </section>
      )}

      {tab === 'exports' && (
        <PlaceholderPanel
          title="Exports"
          description="File, table, and streaming export tasks will be listed here with status, destination, and audit metadata."
          actionLabel="Create export"
        />
      )}

      {tab === 'webhooks' && (
        <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
          <header style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center' }}>
            <div>
              <p className="of-eyebrow">Webhooks ({webhooks.length})</p>
              <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
                Outbound REST webhooks are associated with this source and inherit its base domain, auth references, network policy, worker, and permissions. Use listeners instead for inbound systems calling Foundry.
              </p>
            </div>
            <button type="button" className="of-button" onClick={() => void loadWebhooks()} disabled={busy}>Refresh webhooks</button>
          </header>
          <div className="of-panel-muted" style={{ padding: 12, display: 'grid', gap: 10 }}>
            <p className="of-eyebrow">Create REST webhook</p>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 8 }}>
              <LabeledInput label="Webhook name" value={webhookName} onChange={setWebhookName} />
              <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>Method
                <select value={webhookMethod} onChange={(event) => setWebhookMethod(event.target.value as WebhookHttpMethod)} className="of-input">
                  {WEBHOOK_METHODS.map((method) => <option key={method} value={method}>{method}</option>)}
                </select>
              </label>
              <LabeledInput label="Relative path" value={webhookPath} onChange={setWebhookPath} />
              <LabeledInput label="Authorization reference" value={webhookAuthRef} onChange={setWebhookAuthRef} />
              <LabeledInput label="Timeout ms" value={webhookTimeoutMs} onChange={setWebhookTimeoutMs} />
              <LabeledInput label="Retry attempts" value={webhookRetryAttempts} onChange={setWebhookRetryAttempts} />
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 8 }}>
              <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>Query parameters (name=value)
                <textarea className="of-input" value={webhookQueryRaw} onChange={(event) => setWebhookQueryRaw(event.target.value)} rows={3} />
              </label>
              <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>Headers (Name: value)
                <textarea className="of-input" value={webhookHeadersRaw} onChange={(event) => setWebhookHeadersRaw(event.target.value)} rows={3} />
              </label>
              <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>Body template
                <textarea className="of-input" value={webhookBody} onChange={(event) => setWebhookBody(event.target.value)} rows={3} />
              </label>
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))', gap: 8 }}>
              <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>Input parameters JSON
                <textarea className="of-input" value={webhookInputsJson} onChange={(event) => setWebhookInputsJson(event.target.value)} rows={5} />
              </label>
              <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>Output parameters JSON
                <textarea className="of-input" value={webhookOutputsJson} onChange={(event) => setWebhookOutputsJson(event.target.value)} rows={5} />
              </label>
            </div>
            <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
              Inputs support Boolean, integer, long, double, string, date, timestamp, list, record, optional, and attachment metadata. Outputs can extract the whole response, key paths, array indexes, JSON paths, HTTP status, or the full response string.
            </p>
            <button type="button" onClick={() => void createWebhook()} disabled={busy || !webhookName.trim()} className="of-button of-button--primary" style={{ justifySelf: 'start' }}>Create webhook</button>
          </div>
          <ul style={{ margin: 0, paddingLeft: 18, fontSize: 12 }}>
            {webhooks.map((webhook) => (
              <li key={webhook.id}>
                <strong>{webhook.name}</strong> · {webhook.method} {webhook.relative_path} · inputs {webhook.input_parameters?.length ?? 0} · outputs {webhook.output_parameters?.length ?? 0} · timeout {webhook.timeout_ms}ms · retries {webhook.retry.max_attempts}
                <button type="button" onClick={() => void loadWebhookInvocations(webhook.id)} disabled={busy} className="of-button" style={{ marginLeft: 8, fontSize: 11 }}>History</button>
                {webhookInvocations[webhook.id] ? (
                  <ul style={{ marginTop: 6, paddingLeft: 18 }}>
                    {webhookInvocations[webhook.id].map((invocation) => (
                      <li key={invocation.id}>
                        {invocation.invoked_at} · caller {invocation.caller_id ?? 'unknown'} · {invocation.status} · HTTP {invocation.http_status ?? '-'} · retries {invocation.retry_attempts} · outputs {Object.keys(invocation.parsed_outputs).join(', ') || 'none'}{invocation.error ? ` · ${invocation.error}` : ''}
                      </li>
                    ))}
                    {webhookInvocations[webhook.id].length === 0 && <li className="of-text-muted">No retained invocation history.</li>}
                  </ul>
                ) : null}
              </li>
            ))}
            {webhooks.length === 0 && <li className="of-text-muted">No webhooks configured for this source.</li>}
          </ul>
        </section>
      )}

      {tab === 'code-imports' && (
        <PlaceholderPanel
          title="Code imports"
          description="Generated imports and allowlisted source references for Python transforms, external functions, and compute modules."
        />
      )}

      {tab === 'permissions' && (
        <PlaceholderPanel
          title="Permissions"
          description="Source ownership, markings, export-control eligibility, and groups allowed to explore or run syncs."
        />
      )}

      {tab === 'history' && (
        <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
          <p className="of-eyebrow">History</p>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(190px, 1fr))', gap: 10, fontSize: 12 }}>
            <RegistryField label="Created" value={`${source.created_at} · by ${audit.created_by ?? 'unknown'}`} />
            <RegistryField label="Updated" value={`${source.updated_at} · by ${audit.updated_by ?? 'unknown'}`} />
            <RegistryField label="Archived" value={audit.archived_at ? `${audit.archived_at} · by ${audit.archived_by ?? 'unknown'}` : 'Not archived'} />
            <RegistryField label="Last event" value={audit.last_event_id ?? 'No event id'} />
          </div>
        </section>
      )}

      {tab === 'media-syncs' && (
        <section className="of-panel" style={{ padding: 16 }}>
          <p className="of-eyebrow">Media set syncs ({mediaSyncs.length})</p>
          <ul style={{ marginTop: 8, paddingLeft: 18, fontSize: 12 }}>
            {mediaSyncs.map((m) => (
              <li key={m.id}>
                {m.id} · {m.kind} · target {m.target_media_set_rid} · subfolder <code>{m.subfolder || '/'}</code>
              </li>
            ))}
            {mediaSyncs.length === 0 && <li className="of-text-muted">No media syncs configured.</li>}
          </ul>
        </section>
      )}

      {bulkDialogOpen && (
        <div role="dialog" aria-modal="true" aria-labelledby="source-bulk-register-title" style={dialogBackdropStyle}>
          <section className="of-panel" style={dialogPanelStyle}>
            <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 12 }}>
              <div>
                <p className="of-eyebrow">Bulk register</p>
                <h2 id="source-bulk-register-title" className="of-section-title" style={{ marginTop: 4 }}>
                  {selectedDiscovered.length} selected source{selectedDiscovered.length === 1 ? '' : 's'}
                </h2>
              </div>
              <button type="button" onClick={() => setBulkDialogOpen(false)} disabled={busy} className="of-button">
                Close
              </button>
            </header>

            <div style={{ display: 'grid', gap: 10, gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
              <label style={{ fontSize: 12, display: 'grid', gap: 4 }}>
                Registration mode
                <select value={registrationMode} onChange={(event) => setRegistrationMode(event.target.value as RegistrationMode)} className="of-input">
                  <option value="sync">sync</option>
                  <option value="zero_copy">zero_copy</option>
                </select>
              </label>
              <label style={{ fontSize: 12, display: 'grid', gap: 4 }}>
                Target dataset id
                <input value={targetDatasetId} onChange={(event) => setTargetDatasetId(event.target.value)} placeholder="optional UUID" className="of-input" />
              </label>
            </div>

            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', fontSize: 12 }}>
              <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                <input type="checkbox" checked={autoSync} onChange={(event) => setAutoSync(event.target.checked)} />
                Auto sync
              </label>
              <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                <input type="checkbox" checked={updateDetection} onChange={(event) => setUpdateDetection(event.target.checked)} />
                Update detection
              </label>
            </div>

            <div style={{ overflow: 'auto', border: '1px solid var(--border-subtle)', borderRadius: 8, maxHeight: 260 }}>
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
                <thead>
                  <tr>
                    <th style={tableHeaderStyle}>Source</th>
                    <th style={tableHeaderStyle}>Selector</th>
                    <th style={tableHeaderStyle}>Kind</th>
                  </tr>
                </thead>
                <tbody>
                  {selectedDiscovered.map((item) => (
                    <tr key={item.selector}>
                      <td style={tableCellStyle}>{discoveredLabel(item)}</td>
                      <td style={{ ...tableCellStyle, fontFamily: 'var(--font-mono)' }}>{item.selector}</td>
                      <td style={tableCellStyle}>{item.source_kind ?? '-'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {registrationErrors.length > 0 && (
              <div className="of-status-danger" style={{ padding: '8px 10px', borderRadius: 6, fontSize: 12 }}>
                <ul style={{ margin: 0, paddingLeft: 18 }}>
                  {registrationErrors.map((item) => (
                    <li key={`${item.selector}-${item.error}`}>
                      <code>{item.selector}</code>: {item.error}
                    </li>
                  ))}
                </ul>
              </div>
            )}

            <footer style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
              <button type="button" onClick={() => setBulkDialogOpen(false)} disabled={busy} className="of-button">
                Cancel
              </button>
              <button type="button" onClick={() => void bulkRegisterSelected()} disabled={busy || selectedDiscovered.length === 0} className="of-button of-button--primary">
                {busy ? 'Registering...' : 'Register selected'}
              </button>
            </footer>
          </section>
        </div>
      )}
    </section>
  );
}

function WorkerCompatibilityPanel({
  valid,
  worker,
  allowed,
  unavailable,
  reason,
}: {
  valid: boolean;
  worker: SourceWorker;
  allowed: ConnectorCatalogEntry['capabilities'];
  unavailable: ConnectorCatalogEntry['capabilities'];
  reason: string | null;
}) {
  return (
    <section
      className={valid ? 'of-status-success' : 'of-status-danger'}
      style={{ padding: '10px 12px', borderRadius: 'var(--radius-md)', display: 'grid', gap: 6, fontSize: 12 }}
    >
      <strong>{valid ? `${workerLabel(worker)} is compatible with this source.` : (reason ?? 'Worker is not compatible.')}</strong>
      <span>Available capabilities: {allowed.length > 0 ? allowed.map(capabilityLabel).join(', ') : 'none'}</span>
      {unavailable.length > 0 ? <span>Unavailable capabilities: {unavailable.map(capabilityLabel).join(', ')}</span> : null}
    </section>
  );
}

function LabeledInput({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
      {label}
      <input value={value} onChange={(event) => onChange(event.target.value)} className="of-input" />
    </label>
  );
}

function PlaceholderPanel({ title, description, actionLabel }: { title: string; description: string; actionLabel?: string }) {
  return (
    <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 10 }}>
      <div>
        <p className="of-eyebrow">{title}</p>
        <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>{description}</p>
      </div>
      {actionLabel ? <button type="button" className="of-button of-button--primary" style={{ justifySelf: 'start' }}>{actionLabel}</button> : null}
    </section>
  );
}

function RegistryField({ label, value, href }: { label: string; value: string; href?: string }) {
  return (
    <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 8, padding: 10 }}>
      <span className="of-text-muted" style={{ fontSize: 11 }}>{label}</span>
      {href ? (
        <p style={{ margin: '4px 0 0', overflowWrap: 'anywhere' }}><a href={href} target="_blank" rel="noreferrer">{value}</a></p>
      ) : (
        <p style={{ margin: '4px 0 0', overflowWrap: 'anywhere' }}>{value}</p>
      )}
    </div>
  );
}

const tableHeaderStyle: CSSProperties = {
  padding: '8px 10px',
  borderBottom: '1px solid var(--border-subtle)',
  background: 'var(--bg-subtle)',
  color: 'var(--text-muted)',
  fontSize: 11,
  fontWeight: 600,
  textAlign: 'left',
  whiteSpace: 'nowrap',
};

const tableCellStyle: CSSProperties = {
  padding: '8px 10px',
  borderBottom: '1px solid var(--border-subtle)',
  verticalAlign: 'top',
};

const dialogBackdropStyle: CSSProperties = {
  position: 'fixed',
  inset: 0,
  zIndex: 100,
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  padding: 16,
  background: 'rgba(15, 23, 42, 0.42)',
};

const dialogPanelStyle: CSSProperties = {
  width: 'min(820px, 100%)',
  maxHeight: 'calc(100vh - 32px)',
  overflow: 'auto',
  padding: 16,
  display: 'grid',
  gap: 12,
  boxShadow: 'var(--shadow-popover)',
};
