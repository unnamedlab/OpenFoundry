import { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';

import { CredentialsPanel, type CredentialPanelField } from '@/lib/components/data-connection/CredentialsPanel';
import { TestConnectionPanel } from '@/lib/components/data-connection/TestConnectionPanel';
import {
  CONNECTOR_CATEGORY_ORDER,
  FALLBACK_CONNECTOR_CATALOG,
  capabilityLabel,
  connectorCategoryDescription,
  connectorCategoryLabel,
  dataConnection,
  filterCatalog,
  getConnectorRegistryEntry,
  unavailableCapabilitiesForWorker,
  validateConnectorWorker,
  workerLabel,
  type ConnectorCatalogEntry,
  type ConnectorCategory,
  type DiscoveredSource,
  type SourceWorker,
  type TestConnectionResult,
} from '@/lib/api/data-connection';

type Step = 'connector' | 'configure' | 'validate';
type FieldKind = 'text' | 'number' | 'boolean' | 'json';

interface ConfigPanelField {
  key: string;
  label: string;
  kind?: FieldKind;
  required?: boolean;
  placeholder?: string;
  description?: string;
  defaultValue?: string;
}

interface ConnectorWizardTemplate {
  configFields: ConfigPanelField[];
  credentialFields: CredentialPanelField[];
  notes: string[];
}

const STEP_LABELS: { key: Step; label: string }[] = [
  { key: 'connector', label: 'Connector' },
  { key: 'configure', label: 'Configure' },
  { key: 'validate', label: 'Test' },
];

const WORKER_CHOICES: SourceWorker[] = ['foundry', 'agent'];

const SQL_FIELDS: ConfigPanelField[] = [
  { key: 'host', label: 'Host', required: true, placeholder: 'warehouse.internal' },
  { key: 'port', label: 'Port', kind: 'number', placeholder: '5432' },
  { key: 'database', label: 'Database', placeholder: 'analytics' },
  { key: 'schema', label: 'Schema', placeholder: 'public' },
  {
    key: 'tables',
    label: 'Inline catalog tables',
    kind: 'json',
    placeholder: '[{"selector":"public.orders","display_name":"Orders"}]',
    description: 'Optional JSON array used by discovery when a remote catalog endpoint is not configured.',
  },
];

const SQL_CREDENTIALS: CredentialPanelField[] = [
  { key: 'user', label: 'User', kind: 'username', requiredForTest: true },
  { key: 'password', label: 'Password', kind: 'password', requiredForTest: true },
];

const DEFAULT_TEMPLATE: ConnectorWizardTemplate = {
  configFields: [
    { key: 'base_url', label: 'Base URL', placeholder: 'https://connector.example.com' },
    {
      key: 'tables',
      label: 'Inline catalog',
      kind: 'json',
      placeholder: '[{"selector":"default.orders","display_name":"Orders"}]',
      description: 'Optional JSON array for discovery and bulk registration.',
    },
  ],
  credentialFields: [
    { key: 'api_key', label: 'API key', kind: 'api_key', requiredForTest: false },
  ],
  notes: ['Use the advanced JSON editor for connector-specific keys that are not listed here.'],
};

const TEMPLATES: Record<string, ConnectorWizardTemplate> = {
  s3: {
    configFields: [
      { key: 'bucket', label: 'Bucket', required: true, placeholder: 'company-raw-data' },
      { key: 'prefix', label: 'Prefix', placeholder: 'landing/orders/' },
      { key: 'region', label: 'Region', placeholder: 'us-east-1' },
      { key: 'endpoint_url', label: 'Endpoint URL', placeholder: 'https://s3.us-east-1.amazonaws.com' },
      {
        key: 'tables',
        label: 'Inline catalog tables',
        kind: 'json',
        placeholder: '[{"selector":"raw.orders","path":"s3://company-raw-data/orders/"}]',
      },
    ],
    credentialFields: [
      { key: 'access_key', label: 'Access key', kind: 'aws_access_key', requiredForTest: false },
      { key: 'secret_key', label: 'Secret key', kind: 'aws_secret_key', requiredForTest: false },
    ],
    notes: ['S3 can discover inline table definitions immediately, or use bucket and prefix for file sync setup.'],
  },
  gcs: {
    configFields: [
      { key: 'bucket', label: 'Bucket', required: true, placeholder: 'company-raw-data' },
      { key: 'prefix', label: 'Prefix', placeholder: 'landing/orders/' },
      {
        key: 'application_default',
        label: 'Use application default credentials',
        kind: 'boolean',
        defaultValue: 'false',
      },
    ],
    credentialFields: [
      { key: 'access_token', label: 'Access token', kind: 'oauth_token', requiredForTest: false },
      {
        key: 'service_account_json',
        label: 'Service account JSON',
        kind: 'service_account_json',
        multiline: true,
        requiredForTest: false,
      },
    ],
    notes: ['GCS requires a bucket plus an access token, service account JSON, or application default credentials.'],
  },
  google_cloud_storage: {
    configFields: [
      { key: 'bucket', label: 'Bucket', required: true, placeholder: 'company-raw-data' },
      { key: 'prefix', label: 'Prefix', placeholder: 'landing/orders/' },
      {
        key: 'application_default',
        label: 'Use application default credentials',
        kind: 'boolean',
        defaultValue: 'false',
      },
    ],
    credentialFields: [
      { key: 'access_token', label: 'Access token', kind: 'oauth_token', requiredForTest: false },
      {
        key: 'service_account_json',
        label: 'Service account JSON',
        kind: 'service_account_json',
        multiline: true,
        requiredForTest: false,
      },
    ],
    notes: ['GCS requires a bucket plus an access token, service account JSON, or application default credentials.'],
  },
  onelake: {
    configFields: [
      { key: 'workspace', label: 'Workspace', required: true, placeholder: 'Finance' },
      { key: 'lakehouse', label: 'Lakehouse', required: true, placeholder: 'OperationsLakehouse' },
      { key: 'path', label: 'Path', placeholder: 'Tables/orders' },
    ],
    credentialFields: [
      { key: 'oauth_token', label: 'OAuth token', kind: 'oauth_token', requiredForTest: false },
    ],
    notes: ['OneLake sources usually route through ABFS-compatible paths and OAuth credentials.'],
  },
  azure_blob: {
    configFields: [
      { key: 'account_name', label: 'Account name', required: true, placeholder: 'storageacct' },
      { key: 'container', label: 'Container', placeholder: 'landing' },
      { key: 'prefix', label: 'Prefix', placeholder: 'orders/' },
    ],
    credentialFields: [
      { key: 'account_key', label: 'Account key', kind: 'aws_secret_key', requiredForTest: false },
      { key: 'sas_token', label: 'SAS token', kind: 'api_key', requiredForTest: false },
    ],
    notes: ['Use either an account key, a SAS token, or OAuth in advanced JSON.'],
  },
  adls: {
    configFields: [
      { key: 'account_name', label: 'Account name', required: true, placeholder: 'datalakeacct' },
      { key: 'filesystem', label: 'Filesystem', placeholder: 'raw' },
      { key: 'path', label: 'Path', placeholder: 'orders/' },
    ],
    credentialFields: [
      { key: 'account_key', label: 'Account key', kind: 'aws_secret_key', requiredForTest: false },
      { key: 'oauth_token', label: 'OAuth token', kind: 'oauth_token', requiredForTest: false },
    ],
    notes: ['ADLS supports filesystem paths and private network routing through an agent.'],
  },
  postgresql: {
    configFields: SQL_FIELDS,
    credentialFields: SQL_CREDENTIALS,
    notes: ['PostgreSQL supports connection testing, discovery, virtual tables, and bulk registration.'],
  },
  postgres: {
    configFields: SQL_FIELDS,
    credentialFields: SQL_CREDENTIALS,
    notes: ['PostgreSQL supports connection testing, discovery, virtual tables, and bulk registration.'],
  },
  mysql: {
    configFields: SQL_FIELDS,
    credentialFields: SQL_CREDENTIALS,
    notes: ['MySQL uses the shared SQL connector contract with host-based discovery.'],
  },
  mssql: {
    configFields: [
      { key: 'host', label: 'Host', required: true, placeholder: 'sqlserver.internal' },
      { key: 'port', label: 'Port', kind: 'number', placeholder: '1433' },
      { key: 'database', label: 'Database', placeholder: 'analytics' },
      { key: 'schema', label: 'Schema', placeholder: 'dbo' },
    ],
    credentialFields: SQL_CREDENTIALS,
    notes: ['SQL Server sources can use direct egress or connector-agent routing.'],
  },
  snowflake: {
    configFields: [
      { key: 'account', label: 'Account', required: true, placeholder: 'xy12345.us-east-1' },
      { key: 'warehouse', label: 'Warehouse', placeholder: 'COMPUTE_WH' },
      { key: 'database', label: 'Database', placeholder: 'ANALYTICS' },
      { key: 'schema', label: 'Schema', placeholder: 'PUBLIC' },
      { key: 'role', label: 'Role', placeholder: 'TRANSFORMER' },
    ],
    credentialFields: [
      { key: 'user', label: 'User', kind: 'username', requiredForTest: true },
      { key: 'private_key', label: 'Private key', kind: 'private_key', multiline: true, requiredForTest: false },
      { key: 'oauth_token', label: 'OAuth token', kind: 'oauth_token', requiredForTest: false },
    ],
    notes: ['Use either key-pair JWT or OAuth credentials for live tests.'],
  },
  bigquery: {
    configFields: [
      { key: 'project_id', label: 'Project ID', required: true, placeholder: 'company-analytics' },
      { key: 'dataset', label: 'Dataset', placeholder: 'warehouse' },
      { key: 'location', label: 'Location', placeholder: 'US' },
    ],
    credentialFields: [
      {
        key: 'service_account_json',
        label: 'Service account JSON',
        kind: 'service_account_json',
        multiline: true,
        requiredForTest: false,
      },
      { key: 'access_token', label: 'Access token', kind: 'oauth_token', requiredForTest: false },
    ],
    notes: ['BigQuery needs a project id plus service account JSON or a bearer token for query execution.'],
  },
  salesforce: {
    configFields: [
      { key: 'instance_url', label: 'Instance URL', required: true, placeholder: 'https://example.my.salesforce.com' },
      { key: 'api_version', label: 'API version', placeholder: 'v61.0' },
    ],
    credentialFields: [
      { key: 'access_token', label: 'Access token', kind: 'oauth_token', requiredForTest: true },
    ],
    notes: ['Salesforce discovery uses the instance URL and OAuth access token.'],
  },
  rest_api: {
    configFields: [
      { key: 'base_domain', label: 'Base domain', required: true, placeholder: 'https://api.example.com' },
      { key: 'network_policy_id', label: 'Network policy ID', placeholder: 'egress-policy-id' },
      { key: 'permissions', label: 'Source permissions', placeholder: 'group-data-eng,group-ops' },
      { key: 'default_method', label: 'Default method', defaultValue: 'GET', placeholder: 'GET' },
      {
        key: 'default_headers',
        label: 'Default headers JSON',
        kind: 'json',
        placeholder: '{"Accept":"application/json"}',
      },
      { key: 'records_path', label: 'Records path', placeholder: 'data.items' },
    ],
    credentialFields: [
      { key: 'auth_header', label: 'Authorization header', kind: 'auth_header', placeholder: 'Bearer ...' },
      { key: 'api_key', label: 'API key', kind: 'api_key' },
      { key: 'additional_secret', label: 'Additional secret reference', kind: 'api_key' },
    ],
    notes: ['REST API sources capture a base domain, auth configuration, additional secrets, network policy, worker, and permissions. Configure individual calls on the Webhooks tab after creation.'],
  },
  graphql: {
    configFields: [
      { key: 'base_url', label: 'GraphQL endpoint', required: true, placeholder: 'https://api.example.com/graphql' },
      {
        key: 'query',
        label: 'Query',
        placeholder: 'query Orders { orders { id updatedAt } }',
        description: 'Paste the query text, or use advanced JSON for variables.',
      },
    ],
    credentialFields: [
      { key: 'auth_header', label: 'Authorization header', kind: 'auth_header', placeholder: 'Bearer ...' },
    ],
    notes: ['GraphQL connectors use a query endpoint plus optional authorization headers.'],
  },
  kafka: {
    configFields: [
      { key: 'bootstrap_servers', label: 'Bootstrap servers', required: true, placeholder: 'broker-1:9092,broker-2:9092' },
      { key: 'topic', label: 'Topic', required: true, placeholder: 'orders.raw' },
      { key: 'consumer_group', label: 'Consumer group', placeholder: 'openfoundry-orders' },
      { key: 'auto_offset_reset', label: 'Offset reset', defaultValue: 'latest', placeholder: 'latest' },
    ],
    credentialFields: [
      { key: 'username', label: 'Username', kind: 'username', requiredForTest: false },
      { key: 'password', label: 'Password', kind: 'password', requiredForTest: false },
    ],
    notes: ['For streaming-first Kafka setup, use the dedicated streaming source wizard from Data Connection.'],
  },
  kinesis: {
    configFields: [
      { key: 'stream_name', label: 'Stream name', required: true, placeholder: 'orders-raw' },
      { key: 'region', label: 'Region', required: true, placeholder: 'us-east-1' },
      { key: 'shard_iterator_type', label: 'Shard iterator', defaultValue: 'LATEST', placeholder: 'LATEST' },
    ],
    credentialFields: [
      { key: 'access_key', label: 'Access key', kind: 'aws_access_key', requiredForTest: false },
      { key: 'secret_key', label: 'Secret key', kind: 'aws_secret_key', requiredForTest: false },
    ],
    notes: ['For streaming-first Kinesis setup, use the dedicated streaming source wizard from Data Connection.'],
  },
  iot: {
    configFields: [
      { key: 'broker_host', label: 'Broker host', required: true, placeholder: 'mqtt.example.com' },
      { key: 'topic', label: 'Topic', required: true, placeholder: 'sensors/#' },
      { key: 'tls', label: 'Use TLS', kind: 'boolean', defaultValue: 'true' },
    ],
    credentialFields: [
      { key: 'username', label: 'Username', kind: 'username', requiredForTest: false },
      { key: 'password', label: 'Password', kind: 'password', requiredForTest: false },
    ],
    notes: ['IoT sources support topic discovery and private broker connectivity through an agent.'],
  },
};

function catalogCategory(entry: ConnectorCatalogEntry): ConnectorCategory {
  return getConnectorRegistryEntry(entry).category;
}

function templateFor(entry: ConnectorCatalogEntry): ConnectorWizardTemplate {
  return TEMPLATES[entry.type] ?? DEFAULT_TEMPLATE;
}

function initialConfigValues(template: ConnectorWizardTemplate) {
  const values: Record<string, string> = {};
  for (const field of template.configFields) {
    if (field.defaultValue !== undefined) values[field.key] = field.defaultValue;
  }
  return values;
}

function parseJsonField(field: ConfigPanelField, raw: string) {
  if (!raw.trim()) return undefined;
  try {
    return JSON.parse(raw);
  } catch {
    throw new Error(`${field.label} must be valid JSON.`);
  }
}

function coerceFieldValue(field: ConfigPanelField, raw: string) {
  if (field.kind === 'boolean') return raw === 'true';
  if (!raw.trim()) return undefined;
  if (field.kind === 'number') {
    const parsed = Number(raw);
    if (!Number.isFinite(parsed)) throw new Error(`${field.label} must be a number.`);
    return parsed;
  }
  if (field.kind === 'json') return parseJsonField(field, raw);
  return raw.trim();
}

function buildSourceConfig(
  template: ConnectorWizardTemplate,
  configValues: Record<string, string>,
  credentialValues: Record<string, string>,
  advancedJson: string,
) {
  const config: Record<string, unknown> = {};
  for (const field of template.configFields) {
    const raw = configValues[field.key] ?? '';
    const value = coerceFieldValue(field, raw);
    if (value !== undefined) config[field.key] = value;
  }
  for (const field of template.credentialFields) {
    const raw = credentialValues[field.key] ?? '';
    if (raw.trim()) config[field.key] = raw;
  }
  const trimmedAdvanced = advancedJson.trim();
  if (trimmedAdvanced && trimmedAdvanced !== '{}') {
    let parsed: unknown;
    try {
      parsed = JSON.parse(trimmedAdvanced);
    } catch {
      throw new Error('Advanced config must be valid JSON.');
    }
    if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
      throw new Error('Advanced config must be a JSON object.');
    }
    Object.assign(config, parsed);
  }
  return config;
}

function requiredConfigMissing(template: ConnectorWizardTemplate, values: Record<string, string>) {
  return template.configFields.some((field) => field.required && !(values[field.key] ?? '').trim());
}

export function NewSourcePage() {
  const [catalog, setCatalog] = useState<ConnectorCatalogEntry[]>(FALLBACK_CONNECTOR_CATALOG);
  const [query, setQuery] = useState('');
  const [categoryFilter, setCategoryFilter] = useState<ConnectorCategory | 'All'>('All');
  const [step, setStep] = useState<Step>('connector');
  const [selected, setSelected] = useState<ConnectorCatalogEntry | null>(null);
  const [name, setName] = useState('');
  const [worker, setWorker] = useState<SourceWorker>('foundry');
  const [configValues, setConfigValues] = useState<Record<string, string>>({});
  const [credentialValues, setCredentialValues] = useState<Record<string, string>>({});
  const [advancedJson, setAdvancedJson] = useState('{}');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [createdSourceId, setCreatedSourceId] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<TestConnectionResult | null>(null);
  const [discovered, setDiscovered] = useState<DiscoveredSource[]>([]);
  const [selectedSelectors, setSelectedSelectors] = useState<Record<string, boolean>>({});
  const navigate = useNavigate();

  useEffect(() => {
    dataConnection
      .getCatalog()
      .then((res) => setCatalog((res.connectors.length > 0 ? res.connectors : FALLBACK_CONNECTOR_CATALOG).map(getConnectorRegistryEntry)))
      .catch(() => setCatalog(FALLBACK_CONNECTOR_CATALOG.map(getConnectorRegistryEntry)));
  }, []);

  const filtered = useMemo(
    () => filterCatalog(catalog, query).filter((entry) => categoryFilter === 'All' || catalogCategory(entry) === categoryFilter),
    [catalog, query, categoryFilter],
  );

  const grouped = useMemo(() => {
    const map = new Map<ConnectorCategory, ConnectorCatalogEntry[]>();
    for (const entry of filtered) {
      const key = catalogCategory(entry);
      const list = map.get(key) ?? [];
      list.push(entry);
      map.set(key, list);
    }
    const ordered: { category: ConnectorCategory; entries: ConnectorCatalogEntry[] }[] = [];
    for (const category of CONNECTOR_CATEGORY_ORDER) {
      const entries = map.get(category);
      if (entries) ordered.push({ category, entries });
    }
    return ordered;
  }, [filtered]);

  const selectedTemplate = selected ? templateFor(selected) : DEFAULT_TEMPLATE;
  const workerCompatibility = selected ? validateConnectorWorker(selected, worker) : null;
  const unavailableForWorker = selected ? unavailableCapabilitiesForWorker(selected, worker) : [];
  const canConfigure = Boolean(selected);
  const canCreate = Boolean(selected && name.trim())
    && !requiredConfigMissing(selectedTemplate, configValues)
    && !createdSourceId
    && (workerCompatibility?.valid ?? true);

  function pick(entry: ConnectorCatalogEntry) {
    const registered = getConnectorRegistryEntry(entry);
    if (!registered.available) return;
    const template = templateFor(registered);
    setSelected(registered);
    setName(`${registered.name} source`);
    setWorker(registered.workers.includes('foundry') ? 'foundry' : registered.workers[0] ?? 'foundry');
    setConfigValues(initialConfigValues(template));
    setCredentialValues({});
    setAdvancedJson('{}');
    setError('');
    setCreatedSourceId(null);
    setTestResult(null);
    setDiscovered([]);
    setSelectedSelectors({});
    setStep('configure');
  }

  function patchConfigValue(key: string, value: string) {
    setConfigValues((prev) => ({ ...prev, [key]: value }));
  }

  function patchCredentialValue(key: string, value: string) {
    setCredentialValues((prev) => ({ ...prev, [key]: value }));
  }

  async function createSource() {
    if (!selected) return;
    const compatibility = validateConnectorWorker(selected, worker);
    if (!compatibility.valid) {
      setError(compatibility.reason ?? 'Selected worker is not compatible with this connector.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      const config = buildSourceConfig(selectedTemplate, configValues, credentialValues, advancedJson);
      const created = await dataConnection.createSource({
        name: name.trim(),
        connector_type: selected.type,
        worker,
        config,
      });
      setCreatedSourceId(created.id);
      setStep('validate');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Create failed');
    } finally {
      setBusy(false);
    }
  }

  async function testConnection() {
    if (!createdSourceId) return;
    setBusy(true);
    setError('');
    try {
      setTestResult(await dataConnection.testConnection(createdSourceId));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Test failed');
    } finally {
      setBusy(false);
    }
  }

  async function discover() {
    if (!createdSourceId) return;
    setBusy(true);
    setError('');
    try {
      const res = await dataConnection.discoverSources(createdSourceId);
      setDiscovered(res.sources);
      const next: Record<string, boolean> = {};
      for (const source of res.sources) next[source.selector] = false;
      setSelectedSelectors(next);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Discover failed');
    } finally {
      setBusy(false);
    }
  }

  async function bulkRegister() {
    if (!createdSourceId) return;
    const items = discovered
      .filter((source) => selectedSelectors[source.selector])
      .map((source) => ({ selector: source.selector, source_kind: source.source_kind ?? undefined }));
    if (items.length === 0) {
      setError('Select at least one selector to register.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      await dataConnection.bulkRegister(createdSourceId, items);
      navigate(`/data-connection/sources/${createdSourceId}`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Register failed');
    } finally {
      setBusy(false);
    }
  }

  function renderStepButton(item: { key: Step; label: string }, index: number) {
    const isActive = item.key === step;
    const isEnabled =
      item.key === 'connector' ||
      (item.key === 'configure' && canConfigure) ||
      (item.key === 'validate' && Boolean(createdSourceId));

    return (
      <button
        key={item.key}
        type="button"
        onClick={() => {
          if (isEnabled) setStep(item.key);
        }}
        disabled={!isEnabled}
        className={isActive ? 'of-button of-button--primary' : 'of-button'}
        style={{ justifyContent: 'flex-start' }}
      >
        {index + 1}. {item.label}
      </button>
    );
  }

  return (
    <section className="of-page" style={{ padding: 24, display: 'grid', gap: 16 }}>
      <Link to="/data-connection" style={{ color: 'var(--text-muted)', fontSize: 13 }}>
        Back to sources
      </Link>

      <header style={{ display: 'flex', justifyContent: 'space-between', gap: 16, alignItems: 'flex-start' }}>
        <div>
          <h1 className="of-heading-xl">New source</h1>
          <p className="of-text-muted" style={{ marginTop: 4 }}>
            Pick a connector, configure access, create the source, then test and register discovered assets.
          </p>
        </div>
        {createdSourceId ? (
          <Link to={`/data-connection/sources/${createdSourceId}`} className="of-button">
            Source detail
          </Link>
        ) : null}
      </header>

      <nav className="of-toolbar" aria-label="New source steps" style={{ justifyContent: 'flex-start', flexWrap: 'wrap' }}>
        {STEP_LABELS.map(renderStepButton)}
      </nav>

      {error ? (
        <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {error}
        </div>
      ) : null}

      {step === 'connector' ? (
        <>
          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'flex-end', flexWrap: 'wrap' }}>
              <div>
                <p className="of-eyebrow">Connector gallery</p>
                <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
                  Search by connector, type, or capability.
                </p>
              </div>
              <div style={{ display: 'flex', gap: 6, flex: '1 1 420px', maxWidth: 620 }}>
                <input
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  placeholder="Search connectors"
                  className="of-input"
                  style={{ flex: 1, minWidth: 180 }}
                />
                <select
                  value={categoryFilter}
                  onChange={(event) => setCategoryFilter(event.target.value as ConnectorCategory | 'All')}
                  className="of-input"
                  style={{ maxWidth: 180 }}
                >
                  <option value="All">All categories</option>
                  {CONNECTOR_CATEGORY_ORDER.map((category) => (
                    <option key={category} value={category}>
                      {connectorCategoryLabel(category)}
                    </option>
                  ))}
                </select>
              </div>
            </div>
          </section>

          {grouped.map(({ category, entries }) => (
            <section key={category} className="of-panel" style={{ padding: 16, display: 'grid', gap: 10 }}>
              <div>
                <p className="of-eyebrow">{connectorCategoryLabel(category)}</p>
                <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
                  {connectorCategoryDescription(category)}
                </p>
              </div>
              <ul
                style={{
                  margin: 0,
                  paddingLeft: 0,
                  listStyle: 'none',
                  display: 'grid',
                  gap: 8,
                  gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))',
                }}
              >
                {entries.map((entry) => (
                  <li key={entry.type}>
                    <button
                      type="button"
                      onClick={() => pick(entry)}
                      disabled={!entry.available}
                      style={{
                        width: '100%',
                        minHeight: 136,
                        textAlign: 'left',
                        padding: 12,
                        borderRadius: 'var(--radius-md)',
                        border: '1px solid var(--border-default)',
                        background: entry.available ? 'var(--bg-panel)' : 'var(--bg-panel-muted)',
                        cursor: entry.available ? 'pointer' : 'not-allowed',
                        display: 'grid',
                        gap: 8,
                        alignContent: 'start',
                      }}
                    >
                      <span style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'flex-start' }}>
                        <strong style={{ color: 'var(--text-strong)' }}>{entry.name}</strong>
                        {!entry.available ? <span className="of-chip">Soon</span> : null}
                      </span>
                      <span className="of-text-muted" style={{ fontSize: 11 }}>
                        {entry.description}
                      </span>
                      <span style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
                        <span className="of-chip" style={{ fontSize: 10, minHeight: 20 }}>
                          {connectorCategoryLabel(entry.category)}
                        </span>
                        {entry.capabilities.map((capability) => (
                          <span key={capability} className="of-chip" style={{ fontSize: 10, minHeight: 20 }}>
                            {capabilityLabel(capability)}
                          </span>
                        ))}
                      </span>
                      <span className="of-text-muted" style={{ fontSize: 11 }}>
                        Workers: {entry.workers.join(', ')} · credentials: {entry.credentialFields.length === 0 ? 'none' : entry.credentialFields.map((field) => field.label).join(', ')}
                      </span>
                      <span className="of-text-muted" style={{ fontSize: 11 }}>
                        Network: {entry.network.modes.join(', ')}{entry.network.defaultPorts.length > 0 ? ` · ports ${entry.network.defaultPorts.join(', ')}` : ''}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            </section>
          ))}

          {grouped.length === 0 ? (
            <section className="of-panel" style={{ padding: 24 }}>
              <p className="of-text-muted" style={{ margin: 0 }}>
                No connectors match the current filters.
              </p>
            </section>
          ) : null}
        </>
      ) : null}

      {step === 'configure' && selected ? (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 360px), 1fr))',
            gap: 16,
            alignItems: 'start',
          }}
        >
          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 16 }}>
            <div>
              <p className="of-eyebrow">Configure {selected.name}</p>
              <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
                {selected.description}
              </p>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 220px), 1fr))', gap: 10 }}>
              <label style={{ display: 'grid', gap: 4, fontSize: 13 }}>
                <span style={{ color: 'var(--text-strong)', fontWeight: 600 }}>Source name</span>
                <input value={name} onChange={(event) => setName(event.target.value)} className="of-input" />
              </label>
              <label style={{ display: 'grid', gap: 4, fontSize: 13 }}>
                <span style={{ color: 'var(--text-strong)', fontWeight: 600 }}>Worker</span>
                <select
                  value={worker}
                  onChange={(event) => setWorker(event.target.value as SourceWorker)}
                  className="of-input"
                >
                  {WORKER_CHOICES.map((item) => (
                    <option key={item} value={item} disabled={!validateConnectorWorker(selected, item).valid}>
                      {workerLabel(item)}{selected.workers.includes(item) ? '' : ' (unavailable)'}
                    </option>
                  ))}
                </select>
              </label>
            </div>

            {workerCompatibility && (
              <WorkerCompatibilityPanel
                valid={workerCompatibility.valid}
                worker={worker}
                allowed={workerCompatibility.allowedCapabilities}
                unavailable={unavailableForWorker}
                reason={workerCompatibility.reason}
              />
            )}

            <section style={{ display: 'grid', gap: 10 }}>
              <div>
                <p className="of-eyebrow">Connection config</p>
                <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
                  Required fields are validated before the source is created.
                </p>
              </div>
              <div style={{ display: 'grid', gap: 10, gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))' }}>
                {selectedTemplate.configFields.map((field) => {
                  const value = configValues[field.key] ?? '';
                  if (field.kind === 'boolean') {
                    return (
                      <label
                        key={field.key}
                        style={{
                          display: 'flex',
                          gap: 8,
                          alignItems: 'center',
                          minHeight: 30,
                          color: 'var(--text-strong)',
                          fontWeight: 600,
                        }}
                      >
                        <input
                          type="checkbox"
                          checked={value === 'true'}
                          onChange={(event) => patchConfigValue(field.key, event.target.checked ? 'true' : 'false')}
                        />
                        {field.label}
                      </label>
                    );
                  }
                  return (
                    <label key={field.key} style={{ display: 'grid', gap: 4, fontSize: 13 }}>
                      <span style={{ color: 'var(--text-strong)', fontWeight: 600 }}>
                        {field.label}
                        {field.required ? <span className="of-text-muted"> *</span> : null}
                      </span>
                      {field.kind === 'json' ? (
                        <textarea
                          value={value}
                          onChange={(event) => patchConfigValue(field.key, event.target.value)}
                          placeholder={field.placeholder}
                          className="of-textarea"
                          style={{ minHeight: 92, fontFamily: 'var(--font-mono)', fontSize: 11 }}
                        />
                      ) : (
                        <input
                          type={field.kind === 'number' ? 'number' : 'text'}
                          value={value}
                          onChange={(event) => patchConfigValue(field.key, event.target.value)}
                          placeholder={field.placeholder}
                          className="of-input"
                        />
                      )}
                      {field.description ? (
                        <span className="of-text-muted" style={{ fontSize: 11 }}>
                          {field.description}
                        </span>
                      ) : null}
                    </label>
                  );
                })}
              </div>
            </section>

            <CredentialsPanel fields={selectedTemplate.credentialFields} values={credentialValues} onChange={patchCredentialValue} />

            <details>
              <summary style={{ cursor: 'pointer', color: 'var(--text-strong)', fontWeight: 600 }}>
                Advanced JSON
              </summary>
              <textarea
                value={advancedJson}
                onChange={(event) => setAdvancedJson(event.target.value)}
                className="of-textarea"
                style={{ marginTop: 8, minHeight: 160, fontFamily: 'var(--font-mono)', fontSize: 11 }}
              />
            </details>

            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap' }}>
              <button type="button" onClick={() => setStep('connector')} className="of-button">
                Back
              </button>
              <button type="button" onClick={() => void createSource()} disabled={busy || !canCreate} className="of-button of-button--primary">
                {busy ? 'Creating...' : 'Create source'}
              </button>
            </div>
          </section>

          <aside className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
            <div>
              <p className="of-eyebrow">Review</p>
              <h2 className="of-heading-sm" style={{ margin: '4px 0 0' }}>
                {selected.name}
              </h2>
            </div>
            <div style={{ display: 'grid', gap: 8, fontSize: 12 }}>
              <div>
                <span className="of-text-muted">Connector type</span>
                <p style={{ margin: '2px 0 0', fontFamily: 'var(--font-mono)' }}>{selected.type}</p>
              </div>
              <div>
                <span className="of-text-muted">Category</span>
                <p style={{ margin: '2px 0 0' }}>{connectorCategoryLabel(selected.category)}</p>
              </div>
              <div>
                <span className="of-text-muted">Worker</span>
                <p style={{ margin: '2px 0 0' }}>{workerLabel(worker)}</p>
              </div>
              <div>
                <span className="of-text-muted">Credentials</span>
                <p style={{ margin: '2px 0 0' }}>{selected.credentialFields.length === 0 ? 'No secret fields' : selected.credentialFields.map((field) => field.label).join(', ')}</p>
              </div>
              <div>
                <span className="of-text-muted">Network</span>
                <p style={{ margin: '2px 0 0' }}>
                  {selected.network.modes.join(', ')}{selected.network.defaultPorts.length > 0 ? ` · ports ${selected.network.defaultPorts.join(', ')}` : ''}
                </p>
              </div>
              <div>
                <span className="of-text-muted">Setup docs</span>
                <p style={{ margin: '2px 0 0' }}><a href={selected.setupDocsUrl} target="_blank" rel="noreferrer">Open documentation</a></p>
              </div>
              <div>
                <span className="of-text-muted">Capabilities</span>
                <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap', marginTop: 4 }}>
                  {selected.capabilities.map((capability) => (
                    <span key={capability} className="of-chip" style={{ fontSize: 10, minHeight: 20 }}>
                      {capabilityLabel(capability)}
                    </span>
                  ))}
                </div>
                {unavailableForWorker.length > 0 ? (
                  <p className="of-text-muted" style={{ margin: '6px 0 0', fontSize: 11 }}>
                    Unavailable on {workerLabel(worker)}: {unavailableForWorker.map(capabilityLabel).join(', ')}
                  </p>
                ) : null}
              </div>
            </div>
            {selectedTemplate.notes.length > 0 ? (
              <ul style={{ margin: 0, paddingLeft: 18, display: 'grid', gap: 6, fontSize: 12, color: 'var(--text-muted)' }}>
                {selectedTemplate.notes.map((note) => (
                  <li key={note}>{note}</li>
                ))}
              </ul>
            ) : null}
          </aside>
        </div>
      ) : null}

      {step === 'validate' && createdSourceId ? (
        <div style={{ display: 'grid', gap: 16 }}>
          <TestConnectionPanel sourceId={createdSourceId} result={testResult} busy={busy} onTest={() => void testConnection()}>
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
              <button type="button" onClick={() => void discover()} disabled={busy} className="of-button">
                Discover sources
              </button>
              <Link to={`/data-connection/sources/${createdSourceId}`} className="of-button">
                Skip to source detail
              </Link>
            </div>
          </TestConnectionPanel>

          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'flex-start' }}>
              <div>
                <p className="of-eyebrow">Discovered assets</p>
                <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
                  Select tables, files, topics, or entities to register in bulk.
                </p>
              </div>
              {discovered.length > 0 ? (
                <button type="button" onClick={() => void bulkRegister()} disabled={busy} className="of-button of-button--primary">
                  Register selected
                </button>
              ) : null}
            </div>

            {discovered.length > 0 ? (
              <ul style={{ paddingLeft: 0, margin: 0, listStyle: 'none', maxHeight: 360, overflow: 'auto' }}>
                {discovered.map((source) => (
                  <li
                    key={source.selector}
                    style={{
                      padding: '8px 0',
                      borderTop: '1px solid var(--border-subtle)',
                      display: 'flex',
                      alignItems: 'center',
                      gap: 8,
                    }}
                  >
                    <input
                      type="checkbox"
                      checked={Boolean(selectedSelectors[source.selector])}
                      onChange={(event) =>
                        setSelectedSelectors((prev) => ({ ...prev, [source.selector]: event.target.checked }))
                      }
                    />
                    <code style={{ fontSize: 12 }}>{source.selector}</code>
                    {source.source_kind ? <span className="of-chip">{source.source_kind}</span> : null}
                    {source.supports_sync !== false ? <span className="of-chip">sync</span> : null}
                    {source.supports_zero_copy ? <span className="of-chip">zero-copy</span> : null}
                    {(source.schema ?? []).length > 0 ? <span className="of-text-muted" style={{ fontSize: 11 }}>schema: {(source.schema ?? []).slice(0, 3).map((field) => field.name).join(', ')}</span> : null}
                    {source.sample_rows?.length ? <span className="of-text-muted" style={{ fontSize: 11 }}>{source.sample_redacted ? 'redacted sample' : 'sample'}: {source.sample_rows.length} row(s)</span> : null}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="of-text-muted" style={{ margin: 0 }}>
                Run discovery after the connection test, or continue to the source detail page.
              </p>
            )}
          </section>
        </div>
      ) : null}
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
      <strong>{valid ? `${workerLabel(worker)} is compatible with this connector.` : (reason ?? 'Worker is not compatible.')}</strong>
      <span>Available capabilities: {allowed.length > 0 ? allowed.map(capabilityLabel).join(', ') : 'none'}</span>
      {unavailable.length > 0 ? <span>Unavailable capabilities: {unavailable.map(capabilityLabel).join(', ')}</span> : null}
    </section>
  );
}
