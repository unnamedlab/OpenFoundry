import { describe, expect, it } from 'vitest';

import {
  datasetTransactionTypeForFileMode,
  buildHistoryHref,
  datasetTransactionTypeForTableMode,
  defaultOutputKindForCapability,
  defaultTransactionModeForCapability,
  defaultWriteModeForCapability,
  makeFileSyncSettings,
  makeTableBatchSyncSettings,
  evaluateStreamingConsistency,
  latestCompletedCheckpoint,
  restartPlanForStream,
  pushStreamEndpointUrl,
  recommendStreamIngestion,
  streamArchivePolicyLabel,
  streamHybridReadLabel,
  streamReplayRangeLabel,
  streamingSyncCanStart,
  streamingSyncCanStop,
  validateStreamingSyncSetup,
  validatePushStreamRecords,
  validateRestApiSourceSetup,
  validateWebhookSetup,
  mapWebhookInputs,
  extractWebhookOutputs,
  redactWebhookMetadata,
  retainWebhookInvocations,
  validateWebhookParameters,
  syncRunDurationMs,
  syncRunIsTerminal,
  suggestedOutputDatasetId,
  validateFileSyncSettings,
  validateTableBatchSyncSettings,
  validateEgressPoliciesForConnectionTest,
  validateEgressPolicy,
  type NetworkEgressPolicy,
} from './data-connection';

function policy(overrides: Partial<NetworkEgressPolicy> = {}): NetworkEgressPolicy {
  return {
    id: 'policy-1',
    name: 'warehouse',
    description: '',
    kind: 'direct',
    address: { kind: 'host', value: 'db.example.com' },
    port: { kind: 'single', value: '5432' },
    protocol: 'tls',
    proxy_mode: 'none',
    status: 'active',
    allowed_organizations: ['org-main'],
    is_global: false,
    permissions: [],
    created_at: '2026-05-13T00:00:00Z',
    ...overrides,
  };
}

describe('data connection egress validation', () => {
  it('rejects invalid endpoints, ports, and proxy combinations', () => {
    expect(validateEgressPolicy(policy({ address: { kind: 'host', value: 'not a host' } }))).toEqual(
      expect.arrayContaining([expect.objectContaining({ field: 'address', severity: 'error' })]),
    );
    expect(validateEgressPolicy(policy({ port: { kind: 'single', value: '70000' } }))).toEqual(
      expect.arrayContaining([expect.objectContaining({ field: 'port', severity: 'error' })]),
    );
    expect(validateEgressPolicy(policy({ kind: 'agent_proxy', proxy_mode: 'none' }))).toEqual(
      expect.arrayContaining([expect.objectContaining({ field: 'proxy_mode', severity: 'error' })]),
    );
  });

  it('requires an active matching policy and allowed organization before testing', () => {
    expect(
      validateEgressPoliciesForConnectionTest([
        policy({ kind: 'agent_proxy', proxy_mode: 'http_connect', status: 'pending_review', allowed_organizations: ['org-other'] }),
      ], { expectedKind: 'agent_proxy', organizationId: 'org-main' }),
    ).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ field: 'warehouse.status', severity: 'error' }),
        expect.objectContaining({ field: 'warehouse.allowed_organizations', severity: 'error' }),
      ]),
    );
  });
});

describe('generic sync resource helpers', () => {
  it('maps capabilities to output, write, and transaction defaults', () => {
    expect(defaultOutputKindForCapability('batch_sync')).toBe('dataset');
    expect(defaultOutputKindForCapability('streaming_sync')).toBe('stream');
    expect(defaultOutputKindForCapability('media_sync')).toBe('media_set');
    expect(defaultWriteModeForCapability('batch_sync')).toBe('snapshot');
    expect(defaultWriteModeForCapability('streaming_sync')).toBe('append');
    expect(defaultTransactionModeForCapability('batch_sync')).toBe('transactional');
    expect(defaultTransactionModeForCapability('cdc_sync')).toBe('external_checkpoint');
  });

  it('suggests stable dataset outputs under the source default location', () => {
    expect(
      suggestedOutputDatasetId(
        { id: 'source-1', name: 'Sales Warehouse', default_output_location: 'rid.folder.outputs/' },
        'public.orders',
      ),
    ).toBe('rid.folder.outputs/sales-warehouse-public-orders');
  });
});


describe('file-based sync mode helpers', () => {
  it('maps file sync modes to dataset transaction types', () => {
    expect(datasetTransactionTypeForFileMode('snapshot_mirror')).toBe('SNAPSHOT');
    expect(datasetTransactionTypeForFileMode('incremental_append')).toBe('APPEND');
    expect(datasetTransactionTypeForFileMode('historical_snapshot_incremental')).toBe('SNAPSHOT');
  });

  it('warns on contradictory file sync settings', () => {
    const settings = makeFileSyncSettings({
      mode: 'incremental_append',
      exclude_already_synced: false,
      file_count_limit: 0,
      include_globs: ['**/*.csv'],
      exclude_globs: ['**/*.csv'],
      include_path_metadata: false,
      path_metadata_columns: ['source_path'],
      historical_snapshot_cutoff: null,
      incremental_recent_window: null,
      low_level: null,
    });

    expect(validateFileSyncSettings(settings)).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ code: 'incremental-without-dedup', severity: 'warning' }),
        expect.objectContaining({ code: 'invalid-file-count-limit', severity: 'error' }),
        expect.objectContaining({ code: 'contradictory-glob', severity: 'warning' }),
        expect.objectContaining({ code: 'path-columns-disabled', severity: 'warning' }),
      ]),
    );
  });
});

describe('table batch sync helpers', () => {
  it('maps table sync modes to dataset transaction types', () => {
    expect(datasetTransactionTypeForTableMode('full_snapshot')).toBe('SNAPSHOT');
    expect(datasetTransactionTypeForTableMode('incremental')).toBe('APPEND');
  });

  it('captures incremental warnings when a table lacks change detection', () => {
    const settings = makeTableBatchSyncSettings({
      mode: 'incremental',
      infer_schema: true,
      selected_tables: [
        {
          source_table: 'public.orders',
          destination_dataset_id: 'dataset://orders',
          incremental_column: null,
          estimated_row_count: 123,
        },
      ],
      incremental_column: null,
      row_count: 123,
      transaction_ids: ['tx-1'],
    });

    expect(validateTableBatchSyncSettings(settings)).toEqual(
      expect.arrayContaining([expect.objectContaining({ code: 'missing-incremental-column', severity: 'warning' })]),
    );
  });
});


describe('sync run lifecycle helpers', () => {
  it('classifies terminal statuses and derives duration/build links', () => {
    expect(syncRunIsTerminal('succeeded')).toBe(true);
    expect(syncRunIsTerminal('retrying')).toBe(false);
    expect(syncRunDurationMs({ started_at: '2026-05-13T00:00:00Z', finished_at: '2026-05-13T00:00:03Z' })).toBe(3000);
    expect(buildHistoryHref({ build_id: 'build 1', job_id: 'job 2' })).toBe('/builds/build%201/jobs/job%202');
  });
});

describe('stream resource helpers', () => {
  it('describes replay ranges safely', () => {
    expect(streamReplayRangeLabel(null)).toBe('Replay disabled');
    expect(streamReplayRangeLabel({ status: 'available', from_offset: 10, to_offset: null, requested_by: 'u1', requested_at: '2026-05-13T00:00:00Z' })).toBe('available: 10 → latest');
  });
});


describe('streaming sync setup helpers', () => {
  it('validates long-running streaming sync configuration and start/stop states', () => {
    expect(streamingSyncCanStart('stopped')).toBe(true);
    expect(streamingSyncCanStop('running')).toBe(true);
    expect(validateStreamingSyncSetup({
      source_id: 'source-1',
      source_topic: '',
      key_fields: [],
      start_offset: 'latest',
      consistency_guarantee: 'EXACTLY_ONCE',
      checkpoint_interval_ms: 500,
      output_stream_location: '',
    })).toEqual(expect.arrayContaining([
      expect.objectContaining({ code: 'missing-streaming-topic', severity: 'error' }),
      expect.objectContaining({ code: 'missing-output-stream', severity: 'error' }),
      expect.objectContaining({ code: 'checkpoint-too-frequent', severity: 'warning' }),
      expect.objectContaining({ code: 'exactly-once-without-key', severity: 'warning' }),
    ]));
  });
});

describe('stream hot/cold storage helpers', () => {
  it('summarizes archive policy and hybrid read metadata', () => {
    expect(streamArchivePolicyLabel({ enabled: true, archive_dataset_id: 'dataset.archive', cadence_ms: 60000, retention_ms: 3600000, last_archived_at: null })).toBe('60000ms cadence → dataset.archive');
    expect(streamHybridReadLabel({ hot_rows: 10, cold_rows: 20, from_offset: 1, to_offset: 30, consistency_guarantee: 'AT_LEAST_ONCE' })).toBe('10 hot + 20 cold rows (1 → 30)');
  });
});


describe('stream checkpoint restart helpers', () => {
  it('finds the latest completed checkpoint and builds a restart plan', () => {
    const stream = {
      checkpoints: [
        { id: 'cp-old', status: 'completed', offset: 10, last_processed_source_location: 'topic:0:10', created_at: '2026-05-13T00:00:00Z', completed_at: '2026-05-13T00:00:01Z', duration_ms: 1000 },
        { id: 'cp-new', status: 'completed', offset: 20, last_processed_source_location: 'topic:0:20', created_at: '2026-05-13T00:01:00Z', completed_at: '2026-05-13T00:01:01Z', duration_ms: 1000 },
      ],
    };

    expect(latestCompletedCheckpoint(stream.checkpoints)?.id).toBe('cp-new');
    expect(restartPlanForStream(stream)).toMatchObject({ can_restart: true, latest_completed_checkpoint_id: 'cp-new', restart_from_source_location: 'topic:0:20' });
  });
});

describe('streaming consistency helpers', () => {
  it('downgrades exactly-once when runtime/source/sink cannot guarantee it', () => {
    expect(evaluateStreamingConsistency({ requested: 'EXACTLY_ONCE', runtime: 'agent_runtime', sourceSupportsExactlyOnce: true, sinkSupportsExactlyOnce: true })).toMatchObject({
      effective: 'AT_LEAST_ONCE',
      downgraded: true,
      duplicate_tolerant_consumers_required: true,
    });
    expect(evaluateStreamingConsistency({ requested: 'AT_LEAST_ONCE', runtime: 'flink', sourceSupportsExactlyOnce: true, sinkSupportsExactlyOnce: true })).toMatchObject({
      effective: 'AT_LEAST_ONCE',
      duplicate_tolerant_consumers_required: true,
    });
  });
});


describe('push-based stream ingestion helpers', () => {
  it('validates push API requests and recommends the right ingestion pattern', () => {
    expect(pushStreamEndpointUrl('ri.foundry.main.dataset.123', 'master')).toContain('/streams/by-dataset/ri.foundry.main.dataset.123/branches/master/records');
    expect(validatePushStreamRecords({
      datasetRid: 'ri.foundry.main.dataset.123',
      branch: 'master',
      tokenReferenceId: 'token-1',
      records: [{ sensor_id: 's1', temperature: 72.5 }],
      schema: [
        { name: 'sensor_id', source_type: 'string', foundry_type: 'String', nullable: false },
        { name: 'temperature', source_type: 'double', foundry_type: 'Double', nullable: false },
      ],
    })).toEqual([]);
    expect(validatePushStreamRecords({ datasetRid: '', branch: '', tokenReferenceId: '', records: [] })).toEqual(expect.arrayContaining([
      expect.objectContaining({ code: 'missing-stream-dataset-rid', severity: 'error' }),
      expect.objectContaining({ code: 'missing-stream-branch', severity: 'error' }),
      expect.objectContaining({ code: 'missing-push-token', severity: 'error' }),
      expect.objectContaining({ code: 'empty-push-records', severity: 'error' }),
    ]));
    expect(recommendStreamIngestion({ sourceConnectorExists: true, inboundSystemCanAuthenticate: true, inboundSystemConformsToSchema: true }).kind).toBe('streaming_sync');
    expect(recommendStreamIngestion({ sourceConnectorExists: false, inboundSystemCanAuthenticate: false, inboundSystemConformsToSchema: true }).kind).toBe('listener');
  });
});

describe('REST API source and webhook setup helpers', () => {
  it('validates REST source auth and webhook request options', () => {
    expect(validateRestApiSourceSetup({
      name: 'Orders API',
      base_domain: 'https://api.example.com',
      auth: { kind: 'api_key', credential_reference_id: 'cred-1', header_name: 'X-API-Key' },
      additional_secret_reference_ids: [],
      worker: 'foundry',
      permissions: ['team-data'],
    })).toEqual([]);
    expect(validateRestApiSourceSetup({
      name: '',
      base_domain: 'not-a-url',
      auth: { kind: 'api_key', credential_reference_id: 'cred-1' },
      additional_secret_reference_ids: [],
      worker: 'foundry',
      permissions: [],
    })).toEqual(expect.arrayContaining([
      expect.objectContaining({ code: 'missing-rest-source-name', severity: 'error' }),
      expect.objectContaining({ code: 'invalid-rest-base-domain', severity: 'error' }),
      expect.objectContaining({ code: 'missing-api-key-location', severity: 'error' }),
    ]));
    expect(validateWebhookSetup({
      name: 'Create ticket',
      method: 'POST',
      relative_path: '/tickets',
      query_params: [],
      headers: [{ name: 'Content-Type', value: 'application/json' }],
      body_template: '{"title":"{{title}}"}',
      timeout_ms: 30000,
      retry: { max_attempts: 3, initial_backoff_ms: 1000, max_backoff_ms: 10000 },
    })).toEqual([]);
    expect(validateWebhookSetup({
      name: '',
      method: 'GET',
      relative_path: 'tickets',
      query_params: [],
      headers: [{ name: '', value: 'x' }],
      body_template: '{}',
      timeout_ms: 100,
      retry: { max_attempts: 0, initial_backoff_ms: 1000, max_backoff_ms: 10000 },
    })).toEqual(expect.arrayContaining([
      expect.objectContaining({ code: 'missing-webhook-name', severity: 'error' }),
      expect.objectContaining({ code: 'invalid-webhook-path', severity: 'error' }),
      expect.objectContaining({ code: 'invalid-webhook-timeout', severity: 'error' }),
      expect.objectContaining({ code: 'invalid-webhook-retries', severity: 'error' }),
      expect.objectContaining({ code: 'body-on-read-webhook', severity: 'warning' }),
    ]));
  });
});


describe('webhook parameter mapping and extraction helpers', () => {
  it('maps typed inputs, supports conditional skip, and extracts response outputs', () => {
    const inputParameters = [
      { name: 'approved', type: 'boolean' as const, required: true },
      { name: 'tags', type: 'list' as const, required: false, item_type: { name: 'tag', type: 'string' as const, required: true } },
      { name: 'assignee', type: 'optional' as const, required: false, inner_type: { name: 'assignee', type: 'string' as const, required: false } },
    ];
    expect(validateWebhookParameters({ input_parameters: inputParameters, output_parameters: [] })).toEqual([]);
    expect(mapWebhookInputs(inputParameters, [
      { parameter_name: 'approved', source: 'action_parameter', source_path: ['decision'] },
      { parameter_name: 'tags', source: 'literal', value: ['p0', 'customer'] },
      { parameter_name: 'assignee', source: 'function_output', source_path: ['owner'], skip_when_undefined: true },
    ], { decision: true })).toMatchObject({ should_invoke: false, skipped_reason: 'Mapping for assignee returned undefined.' });
    expect(mapWebhookInputs(inputParameters.slice(0, 2), [
      { parameter_name: 'approved', source: 'action_parameter', source_path: ['decision'] },
      { parameter_name: 'tags', source: 'literal', value: ['p0'] },
    ], { decision: true })).toMatchObject({ should_invoke: true, inputs: { approved: true, tags: ['p0'] } });
    expect(extractWebhookOutputs([
      { name: 'ticket_id', type: 'string', extractor: { kind: 'key_path', key_path: ['result', 'id'] } },
      { name: 'status', type: 'integer', extractor: { kind: 'http_status' } },
      { name: 'raw', type: 'string', extractor: { kind: 'full_response_string' } },
    ], { status: 201, body: { result: { id: 'T-1' }, warnings: ['slow'] } })).toMatchObject({ ticket_id: 'T-1', status: 201 });
    expect(extractWebhookOutputs([
      { name: 'first_warning', type: 'string', extractor: { kind: 'array_index', array_index_path: [0] } },
    ], { status: 201, body: ['slow'] })).toEqual({ first_warning: 'slow' });
    expect(extractWebhookOutputs([
      { name: 'first_warning', type: 'string', extractor: { kind: 'json_path', json_path: '$.warnings.0' } },
    ], { status: 201, body: { warnings: ['slow'] } })).toEqual({ first_warning: 'slow' });
  });
});

describe('webhook invocation history helpers', () => {
  it('redacts secrets and enforces retention windows', () => {
    expect(redactWebhookMetadata({ headers: { Authorization: 'Bearer secret', Accept: 'application/json' }, query_params: { api_key: 'secret', q: 'orders' }, body_preview: 'abcdef', body_bytes: 6 }, 3)).toMatchObject({
      headers: { Authorization: '[REDACTED]', Accept: 'application/json' },
      query_params: { api_key: '[REDACTED]', q: 'orders' },
      body_preview: 'abc…',
      truncated: true,
    });
    const retained = retainWebhookInvocations([
      { id: 'old', source_id: 's', webhook_id: 'w', invoked_at: '2025-01-01T00:00:00Z', caller_id: 'u', input_summary: {}, http_status: 200, parsed_outputs: {}, status: 'succeeded', error: null, retry_attempts: 0, request: {}, response: {}, retained_until: null },
      { id: 'new', source_id: 's', webhook_id: 'w', invoked_at: '2026-05-01T00:00:00Z', caller_id: 'u', input_summary: {}, http_status: 500, parsed_outputs: {}, status: 'failed', error: 'boom', retry_attempts: 2, request: { headers: { Authorization: 'secret' } }, response: {}, retained_until: null },
    ], '2026-05-13T00:00:00Z', 183);
    expect(retained).toHaveLength(1);
    expect(retained[0]).toMatchObject({ id: 'new', request: { headers: { Authorization: '[REDACTED]' } } });
  });
});
