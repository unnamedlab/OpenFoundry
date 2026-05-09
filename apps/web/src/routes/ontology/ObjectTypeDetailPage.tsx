import { useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';

import {
  createObject,
  getObjectType,
  listActionTypes,
  listLinkTypes,
  listProperties,
  listRules,
  listTypeSharedPropertyTypes,
  type ActionType,
  type LinkType,
  type ObjectInstance,
  type ObjectType,
  type OntologyRule,
  type Property,
  type SharedPropertyType,
} from '@/lib/api/ontology';
import { listDatasets, type Dataset } from '@/lib/api/datasets';
import { CreateActionTypeWizard } from '@/lib/components/ontology/CreateActionTypeWizard';
import { ObjectDetailDrawer } from '@/lib/components/ontology/ObjectDetailDrawer';
import { ObjectExplorer } from '@/lib/components/ontology/ObjectExplorer';
import { PropertyPanel } from '@/lib/components/ontology/PropertyPanel';
import { SaveAsAppModal } from '@/lib/components/apps/SaveAsAppModal';
import { Tabs } from '@/lib/components/Tabs';
import { Glyph, type GlyphName } from '@/lib/components/ui/Glyph';

type Tab = 'overview' | 'properties' | 'objects' | 'actions' | 'datasources' | 'links' | 'rules' | 'shared';

interface DatasourceSettings {
  backing_dataset_id: string | null;
  allow_edits: boolean;
  track_user_edit_history: boolean;
  conflict_resolution: 'apply_user_edits' | 'apply_most_recent';
}

const DEFAULT_DATASOURCE_SETTINGS: DatasourceSettings = {
  backing_dataset_id: null,
  allow_edits: false,
  track_user_edit_history: false,
  conflict_resolution: 'apply_user_edits',
};

function readDatasourceSettings(typeId: string): DatasourceSettings {
  if (!typeId || typeof window === 'undefined') return { ...DEFAULT_DATASOURCE_SETTINGS };
  try {
    const raw = window.localStorage.getItem(`of:ontology:datasource:${typeId}`);
    if (!raw) return { ...DEFAULT_DATASOURCE_SETTINGS };
    return { ...DEFAULT_DATASOURCE_SETTINGS, ...(JSON.parse(raw) as Partial<DatasourceSettings>) };
  } catch {
    return { ...DEFAULT_DATASOURCE_SETTINGS };
  }
}

function writeDatasourceSettings(typeId: string, settings: DatasourceSettings) {
  if (!typeId || typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(`of:ontology:datasource:${typeId}`, JSON.stringify(settings));
  } catch {
    /* ignore */
  }
}
type DependentKind =
  | 'developer-console'
  | 'function'
  | 'graph-template'
  | 'map-layer'
  | 'map-template'
  | 'process'
  | 'quiver'
  | 'use-cases'
  | 'workshop';

const DEPENDENT_KINDS: Array<{ id: DependentKind; label: string; icon: GlyphName; tone: string; emptyTitle: string; emptyBody: string; createLabel?: string }> = [
  { id: 'developer-console', label: 'Developer Console App', icon: 'code', tone: '#0891b2', emptyTitle: 'No Developer Console apps', emptyBody: 'Build a custom backend integration on this object type.' },
  { id: 'function', label: 'Function', icon: 'code', tone: '#7c5dd6', emptyTitle: 'No functions', emptyBody: 'Define computations on this object type.' },
  { id: 'graph-template', label: 'Graph Template', icon: 'graph', tone: '#5c7080', emptyTitle: 'No graph templates', emptyBody: 'Visualise this object type as a graph.' },
  { id: 'map-layer', label: 'Map Layer', icon: 'graph', tone: '#5c7080', emptyTitle: 'No map layers', emptyBody: 'Render this object type on a map.' },
  { id: 'map-template', label: 'Map Template', icon: 'graph', tone: '#5c7080', emptyTitle: 'No map templates', emptyBody: 'Reusable map configuration.' },
  { id: 'process', label: 'Process', icon: 'run', tone: '#15803d', emptyTitle: 'No processes', emptyBody: 'Operationalise workflows on this object type.' },
  { id: 'quiver', label: 'Quiver Dashboard', icon: 'graph', tone: '#0891b2', emptyTitle: 'No Quiver dashboards', emptyBody: 'Build interactive analytics.' },
  { id: 'use-cases', label: 'Use cases', icon: 'list', tone: '#5c7080', emptyTitle: 'No use cases', emptyBody: 'Document operational workflows.' },
  { id: 'workshop', label: 'Workshop', icon: 'object', tone: '#7c5dd6', emptyTitle: 'No Workshop modules', emptyBody: 'Workshop enables users to build interactive and high-quality applications for operational users.', createLabel: 'Create your first' },
];

function formatDate(value: string | null | undefined) {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function propertyCountLabel(count: number) {
  return `${count} propert${count === 1 ? 'y' : 'ies'}`;
}

export function ObjectTypeDetailPage() {
  const { id = '' } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [tab, setTab] = useState<Tab>('overview');
  const [dependentKind, setDependentKind] = useState<DependentKind>('workshop');
  const [saveAsOpen, setSaveAsOpen] = useState(false);
  const [type, setType] = useState<ObjectType | null>(null);
  const [properties, setProperties] = useState<Property[]>([]);
  const [objectsReload, setObjectsReload] = useState(0);
  const [actions, setActions] = useState<ActionType[]>([]);
  const [links, setLinks] = useState<LinkType[]>([]);
  const [rules, setRules] = useState<OntologyRule[]>([]);
  const [shared, setShared] = useState<SharedPropertyType[]>([]);
  const [datasets, setDatasets] = useState<Dataset[]>([]);
  const [datasourceSettings, setDatasourceSettings] = useState<DatasourceSettings>({ ...DEFAULT_DATASOURCE_SETTINGS });
  const [datasourceDirty, setDatasourceDirty] = useState(false);
  const [datasourceSaved, setDatasourceSaved] = useState(false);
  const [datasetPickerOpen, setDatasetPickerOpen] = useState(false);
  const [actionWizardOpen, setActionWizardOpen] = useState(false);
  const [selectedObject, setSelectedObject] = useState<ObjectInstance | null>(null);
  const [createPropsJson, setCreatePropsJson] = useState('{}');
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  async function loadOverview() {
    if (!id) return;
    setLoading(true);
    setError('');
    try {
      setType(await getObjectType(id));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load type');
    } finally {
      setLoading(false);
    }
  }

  async function ensureProperties() {
    if (!id || properties.length > 0) return;
    setProperties(await listProperties(id));
  }

  async function ensureActions() {
    if (!id || actions.length > 0) return;
    setActions((await listActionTypes({ object_type_id: id, per_page: 100 })).data);
  }

  async function loadTab(next: Tab) {
    setTab(next);
    if (!id) return;
    setError('');
    try {
      if (next === 'properties' || next === 'objects') await ensureProperties();
      if (next === 'objects' || next === 'actions') await ensureActions();
      if (next === 'links' && links.length === 0) setLinks((await listLinkTypes({ object_type_id: id, per_page: 100 })).data);
      if (next === 'rules' && rules.length === 0) setRules((await listRules({ object_type_id: id })).data);
      if (next === 'shared' && shared.length === 0) setShared(await listTypeSharedPropertyTypes(id));
      if (next === 'datasources') {
        if (datasets.length === 0) {
          try {
            const response = await listDatasets({ per_page: 100 });
            setDatasets(response.data);
          } catch {
            setDatasets([]);
          }
        }
        setDatasourceSettings(readDatasourceSettings(id));
        setDatasourceDirty(false);
        setDatasourceSaved(false);
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load tab');
    }
  }

  useEffect(() => {
    setProperties([]);
    setActions([]);
    setLinks([]);
    setRules([]);
    setShared([]);
    setSelectedObject(null);
    void loadOverview();
  }, [id]);

  async function createObj() {
    if (!type) return;
    setBusy(true);
    setError('');
    try {
      const propertiesBody = JSON.parse(createPropsJson || '{}') as Record<string, unknown>;
      const created = await createObject(type.id, { properties: propertiesBody });
      setSelectedObject(created);
      setCreatePropsJson('{}');
      setObjectsReload((value) => value + 1);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Create object failed');
    } finally {
      setBusy(false);
    }
  }

  function handleObjectUpdated(next: ObjectInstance) {
    setSelectedObject(next);
    setObjectsReload((value) => value + 1);
  }

  if (loading) {
    return (
      <section className="of-page" style={{ padding: 24 }}>
        <p className="of-text-muted">Loading...</p>
      </section>
    );
  }

  if (!type) {
    return (
      <section className="of-page" style={{ padding: 24 }}>
        <Link to="/ontology" style={{ color: 'var(--text-muted)', fontSize: 13 }}>Back to ontology</Link>
        <p className="of-status-danger" style={{ marginTop: 12 }}>{error || 'Object type not found'}</p>
      </section>
    );
  }

  return (
    <section className="of-page" style={{ padding: 24, display: 'grid', gap: 16 }}>
      <Link to="/ontology" style={{ color: 'var(--text-muted)', fontSize: 13 }}>Back to ontology</Link>

      <header style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
        <div style={{ display: 'flex', alignItems: 'flex-start', gap: 12, minWidth: 0 }}>
          <div
            aria-hidden="true"
            style={{
              width: 56,
              height: 56,
              background: type.color || '#4d8cf0',
              borderRadius: 8,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: 'white',
              fontSize: 24,
              flexShrink: 0,
            }}
          >
            {type.icon || type.display_name.slice(0, 1).toUpperCase()}
          </div>
          <div style={{ minWidth: 0 }}>
            <h1 className="of-heading-xl">{type.display_name}</h1>
            <p className="of-text-muted" style={{ marginTop: 4, fontSize: 12, overflowWrap: 'anywhere' }}>
              {type.id} / name: {type.name} / pk: {type.primary_key_property ?? '-'}
            </p>
            {type.description && <p style={{ margin: '8px 0 0', maxWidth: 760 }}>{type.description}</p>}
          </div>
        </div>
        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
          <Link to="/object-link-types" className="of-button">Manage schema</Link>
          <Link to="/action-types" className="of-button">Action types</Link>
        </div>
      </header>

      {error && (
        <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {error}
        </div>
      )}

      <Tabs
        tabs={[
          { id: 'overview', label: 'Overview' },
          { id: 'properties', label: properties.length ? `Properties (${properties.length})` : 'Properties' },
          { id: 'objects', label: 'Objects' },
          { id: 'actions', label: actions.length ? `Actions (${actions.length})` : 'Actions' },
          { id: 'datasources', label: 'Datasources' },
          { id: 'links', label: links.length ? `Links (${links.length})` : 'Links' },
          { id: 'rules', label: rules.length ? `Rules (${rules.length})` : 'Rules' },
          { id: 'shared', label: shared.length ? `Shared (${shared.length})` : 'Shared' },
        ] as const}
        active={tab}
        onChange={(next) => void loadTab(next)}
      />

      {tab === 'overview' && (
        <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
          <div style={{ display: 'grid', gap: 10, gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
            <div>
              <p className="of-eyebrow">Identifier</p>
              <p style={{ margin: '4px 0 0', fontFamily: 'var(--font-mono)', fontSize: 12 }}>{type.name}</p>
            </div>
            <div>
              <p className="of-eyebrow">Primary key</p>
              <p style={{ margin: '4px 0 0', fontSize: 12 }}>{type.primary_key_property ?? '-'}</p>
            </div>
            <div>
              <p className="of-eyebrow">Owner</p>
              <p style={{ margin: '4px 0 0', fontFamily: 'var(--font-mono)', fontSize: 12, overflowWrap: 'anywhere' }}>{type.owner_id}</p>
            </div>
            <div>
              <p className="of-eyebrow">Updated</p>
              <p style={{ margin: '4px 0 0', fontSize: 12 }}>{formatDate(type.updated_at)}</p>
            </div>
          </div>
          <pre style={{ padding: 12, background: 'var(--bg-subtle)', fontSize: 11, fontFamily: 'var(--font-mono)', borderRadius: 8, overflow: 'auto' }}>
            {JSON.stringify(type, null, 2)}
          </pre>

          <div
            style={{
              marginTop: 6,
              border: '1px solid var(--border-subtle)',
              borderRadius: 6,
              overflow: 'hidden',
              display: 'grid',
              gridTemplateColumns: '260px minmax(0, 1fr)',
            }}
          >
            <div style={{ borderRight: '1px solid var(--border-subtle)', background: '#fff' }}>
              <div style={{ padding: '10px 14px', borderBottom: '1px solid var(--border-subtle)', display: 'flex', alignItems: 'center', gap: 8 }}>
                <span style={{ fontSize: 13, fontWeight: 600 }}>Dependents</span>
                <span className="of-chip" style={{ fontSize: 11 }}>{DEPENDENT_KINDS.length}</span>
              </div>
              <ul style={{ margin: 0, padding: 0, listStyle: 'none' }}>
                {DEPENDENT_KINDS.map((kind) => {
                  const active = dependentKind === kind.id;
                  return (
                    <li key={kind.id}>
                      <button
                        type="button"
                        onClick={() => setDependentKind(kind.id)}
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'space-between',
                          width: '100%',
                          padding: '8px 14px',
                          border: 0,
                          background: active ? 'rgba(45, 114, 210, 0.06)' : 'transparent',
                          color: active ? 'var(--status-info)' : 'var(--text-strong)',
                          fontWeight: active ? 600 : 500,
                          cursor: 'pointer',
                          textAlign: 'left',
                          fontSize: 13,
                        }}
                      >
                        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
                          <Glyph name={kind.icon} size={13} tone={kind.tone} />
                          {kind.label}
                        </span>
                        <span style={{ color: 'var(--text-muted)', fontVariantNumeric: 'tabular-nums' }}>0</span>
                      </button>
                    </li>
                  );
                })}
              </ul>
            </div>
            <div style={{ padding: 18, display: 'grid', placeContent: 'center', textAlign: 'center', gap: 10, background: '#fafbfc' }}>
              {(() => {
                const kind = DEPENDENT_KINDS.find((entry) => entry.id === dependentKind);
                if (!kind) return null;
                return (
                  <>
                    <span style={{ display: 'inline-flex', justifyContent: 'center' }}>
                      <Glyph name="search" size={28} tone="#aab4c0" />
                    </span>
                    <p style={{ margin: 0, fontSize: 14, fontWeight: 600 }}>{kind.emptyTitle}</p>
                    <p className="of-text-muted" style={{ margin: '0 auto', maxWidth: 360, fontSize: 12.5 }}>
                      {kind.emptyBody} <button type="button" className="of-link" style={{ background: 'none', border: 0, padding: 0, color: 'var(--status-info)', cursor: 'pointer', fontSize: 12.5 }}>Learn more</button>
                    </p>
                    {kind.createLabel ? (
                      <button
                        type="button"
                        onClick={() => setSaveAsOpen(true)}
                        style={{
                          justifySelf: 'center',
                          display: 'inline-flex',
                          alignItems: 'center',
                          gap: 6,
                          padding: '8px 14px',
                          border: '1px solid var(--border-default)',
                          borderRadius: 4,
                          background: '#fff',
                          fontSize: 13,
                          fontWeight: 600,
                          color: 'var(--status-info)',
                          cursor: 'pointer',
                        }}
                      >
                        <Glyph name="plus" size={13} tone="var(--status-info)" /> {kind.createLabel}
                      </button>
                    ) : null}
                  </>
                );
              })()}
            </div>
          </div>
        </section>
      )}

      {tab === 'properties' && (
        <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 10 }}>
          <p className="of-eyebrow">{propertyCountLabel(properties.length)}</p>
          {properties.map((property) => (
            <PropertyPanel
              key={property.id}
              property={property}
              typeId={type.id}
              isPrimaryKey={type.primary_key_property === property.name}
              onUpdated={(updated) => setProperties((current) => current.map((item) => (item.id === updated.id ? updated : item)))}
            />
          ))}
          {properties.length === 0 && <p className="of-text-muted">No properties.</p>}
        </section>
      )}

      {tab === 'objects' && (
        <div style={{ display: 'grid', gap: 12 }}>
          <section className="of-panel" style={{ padding: 16 }}>
            <p className="of-eyebrow">Create object</p>
            <textarea
              value={createPropsJson}
              onChange={(event) => setCreatePropsJson(event.target.value)}
              className="of-input"
              style={{ marginTop: 6, fontFamily: 'var(--font-mono)', fontSize: 11, minHeight: 120 }}
            />
            <button type="button" onClick={() => void createObj()} disabled={busy} className="of-button of-button--primary" style={{ marginTop: 6 }}>
              {busy ? 'Creating...' : 'Create'}
            </button>
          </section>

          <ObjectExplorer
            typeId={type.id}
            objectType={type}
            properties={properties}
            editable
            reloadSignal={objectsReload}
            onSelect={setSelectedObject}
            onObjectUpdated={handleObjectUpdated}
          />
        </div>
      )}

      {tab === 'actions' && (
        <section className="of-panel" style={{ padding: 16 }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
            <p className="of-eyebrow" style={{ margin: 0 }}>Action types ({actions.length})</p>
            <button type="button" className="of-button" onClick={() => setActionWizardOpen(true)}>
              <Glyph name="plus" size={11} tone="var(--status-info)" /> New
            </button>
          </div>
          <ul style={{ marginTop: 8, paddingLeft: 0, listStyle: 'none', display: 'grid', gap: 8 }}>
            {actions.map((action) => (
              <li key={action.id}>
                <button
                  type="button"
                  onClick={() => navigate(`/action-types/${action.id}`)}
                  style={{ width: '100%', padding: 10, border: '1px solid var(--border-subtle)', borderRadius: 8, background: '#fff', cursor: 'pointer', textAlign: 'left' }}
                >
                  <strong>{action.display_name}</strong> <span className="of-text-muted">/ {action.name} / {action.operation_kind}</span>
                  {action.description && <p className="of-text-muted" style={{ fontSize: 12, margin: '4px 0 0' }}>{action.description}</p>}
                </button>
              </li>
            ))}
            {actions.length === 0 && <li className="of-text-muted">No actions for this type.</li>}
          </ul>
        </section>
      )}

      <CreateActionTypeWizard
        open={actionWizardOpen}
        defaultObjectTypeId={id}
        onClose={() => setActionWizardOpen(false)}
        onCreated={(action) => {
          setActions((current) => [action, ...current]);
          setActionWizardOpen(false);
        }}
      />


      {tab === 'datasources' && (() => {
        const backingDataset = datasets.find((d) => d.id === datasourceSettings.backing_dataset_id) ?? null;
        function patchSettings(patch: Partial<DatasourceSettings>) {
          setDatasourceSettings((current) => {
            const next = { ...current, ...patch };
            return next;
          });
          setDatasourceDirty(true);
          setDatasourceSaved(false);
        }
        function saveDatasource() {
          if (!id) return;
          writeDatasourceSettings(id, datasourceSettings);
          setDatasourceDirty(false);
          setDatasourceSaved(true);
        }
        return (
          <section style={{ display: 'grid', gap: 14 }}>
            <div className="of-panel" style={{ padding: 0, overflow: 'hidden' }}>
              <header style={{ padding: '14px 16px', borderBottom: '1px solid var(--border-subtle)', display: 'flex', alignItems: 'center', gap: 8 }}>
                <Glyph name="database" size={14} tone="#5c7080" />
                <strong style={{ fontSize: 14 }}>Backing datasource</strong>
              </header>
              <div style={{ padding: 16, display: 'grid', gap: 12 }}>
                <p className="of-text-muted" style={{ fontSize: 12, margin: 0 }}>
                  Configure the backing datasource for this object type. The datasource is required, but can be changed.
                </p>
                {backingDataset ? (
                  <div style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '10px 12px', border: '1px solid var(--border-subtle)', borderRadius: 6, background: '#fff' }}>
                    <Glyph name="database" size={14} tone="#0d9488" />
                    <div style={{ flex: 1 }}>
                      <div style={{ fontSize: 13, fontWeight: 600 }}>{backingDataset.name}</div>
                      {backingDataset.description ? (
                        <div className="of-text-muted" style={{ fontSize: 11 }}>{backingDataset.description}</div>
                      ) : null}
                    </div>
                    <button type="button" className="of-button" onClick={() => setDatasetPickerOpen(true)}>
                      <Glyph name="pencil" size={11} /> Replace
                    </button>
                    <button type="button" aria-label="More" className="of-button of-button--ghost" style={{ padding: 6 }}>
                      <Glyph name="settings" size={11} />
                    </button>
                  </div>
                ) : null}
                <button type="button" className="of-button" onClick={() => setDatasetPickerOpen(true)} style={{ alignSelf: 'flex-start' }}>
                  <Glyph name="plus" size={12} /> Add new backing datasource
                </button>
              </div>
            </div>

            <div className="of-panel" style={{ padding: 0, overflow: 'hidden' }}>
              <header style={{ padding: '14px 16px', borderBottom: '1px solid var(--border-subtle)', display: 'flex', alignItems: 'center', gap: 8 }}>
                <Glyph name="pencil" size={14} tone="#5c7080" />
                <strong style={{ fontSize: 14 }}>Edits</strong>
              </header>
              <div style={{ padding: 16, display: 'grid', gap: 14 }}>
                <label style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12 }}>
                  <span>
                    <span style={{ display: 'block', fontSize: 13, fontWeight: 600 }}>Allow edits</span>
                    <span className="of-text-muted" style={{ fontSize: 12 }}>Disabling edits will not remove existing edits</span>
                  </span>
                  <span style={{ position: 'relative', display: 'inline-flex', width: 36, height: 20, borderRadius: 999, background: datasourceSettings.allow_edits ? '#2d72d2' : '#aab4c0', cursor: 'pointer', transition: 'background 0.12s' }}>
                    <input
                      type="checkbox"
                      checked={datasourceSettings.allow_edits}
                      onChange={(event) => patchSettings({ allow_edits: event.target.checked })}
                      style={{ position: 'absolute', inset: 0, opacity: 0, cursor: 'pointer' }}
                      aria-label="Allow edits"
                    />
                    <span style={{ position: 'absolute', top: 2, left: datasourceSettings.allow_edits ? 18 : 2, width: 16, height: 16, borderRadius: '50%', background: '#fff', boxShadow: '0 1px 3px rgba(15, 23, 42, 0.18)', transition: 'left 0.12s' }} />
                  </span>
                </label>
                <label style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12 }}>
                  <span>
                    <span style={{ display: 'block', fontSize: 13, fontWeight: 600 }}>Track user edit history</span>
                    <span className="of-text-muted" style={{ fontSize: 12 }}>Logs user edits to objects of this type and displays those logs in Edit History widgets.</span>
                  </span>
                  <span style={{ position: 'relative', display: 'inline-flex', width: 36, height: 20, borderRadius: 999, background: datasourceSettings.track_user_edit_history ? '#2d72d2' : '#aab4c0', cursor: 'pointer' }}>
                    <input
                      type="checkbox"
                      checked={datasourceSettings.track_user_edit_history}
                      onChange={(event) => patchSettings({ track_user_edit_history: event.target.checked })}
                      style={{ position: 'absolute', inset: 0, opacity: 0, cursor: 'pointer' }}
                      aria-label="Track user edit history"
                    />
                    <span style={{ position: 'absolute', top: 2, left: datasourceSettings.track_user_edit_history ? 18 : 2, width: 16, height: 16, borderRadius: '50%', background: '#fff', boxShadow: '0 1px 3px rgba(15, 23, 42, 0.18)' }} />
                  </span>
                </label>
              </div>
            </div>

            <div className="of-panel" style={{ padding: 0, overflow: 'hidden' }}>
              <header style={{ padding: '14px 16px', borderBottom: '1px solid var(--border-subtle)' }}>
                <strong style={{ fontSize: 14 }}>Conflict resolution</strong>
              </header>
              <div style={{ padding: 16, display: 'grid', gap: 12 }}>
                <p className="of-text-muted" style={{ fontSize: 12, margin: 0 }}>
                  Configure what values to keep for properties of this object type.
                </p>
                <p className="of-text-muted" style={{ fontSize: 12, margin: 0 }}>
                  Resolution happens on a property-by-property basis. Properties that have not received user edits will continue to use latest pipeline data. Regardless of resolution, all values are still written. Edit-only properties without a backing column always use the latest user edit.
                </p>
                {backingDataset ? (
                  <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 6, padding: 12, display: 'grid', gap: 10 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                      <Glyph name="database" size={13} tone="#0d9488" />
                      <strong style={{ fontSize: 13 }}>{backingDataset.name}</strong>
                    </div>
                    <span className="of-text-muted" style={{ fontSize: 11 }}>{properties.length} properties</span>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                      <span style={{ fontSize: 13 }}>Conflict resolution strategy</span>
                      <div style={{ display: 'inline-flex', borderRadius: 4, overflow: 'hidden', border: '1px solid var(--border-default)' }}>
                        {([
                          { id: 'apply_user_edits', label: 'Apply user edits', tag: 'Default' as string | undefined },
                          { id: 'apply_most_recent', label: 'Apply most recent value', tag: undefined as string | undefined },
                        ] as const).map((option) => (
                          <button
                            key={option.id}
                            type="button"
                            onClick={() => patchSettings({ conflict_resolution: option.id })}
                            style={{ padding: '6px 12px', border: 0, background: datasourceSettings.conflict_resolution === option.id ? '#1c2127' : '#fff', color: datasourceSettings.conflict_resolution === option.id ? '#fff' : 'var(--text-strong)', cursor: 'pointer', fontSize: 12 }}
                          >
                            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                              <span style={{ width: 8, height: 8, borderRadius: '50%', background: datasourceSettings.conflict_resolution === option.id ? '#fff' : '#aab4c0', display: 'inline-block' }} />
                              {option.label}
                              {option.tag ? <span className="of-chip" style={{ fontSize: 10 }}>{option.tag}</span> : null}
                            </span>
                          </button>
                        ))}
                      </div>
                    </div>
                  </div>
                ) : (
                  <p className="of-text-muted" style={{ fontSize: 12 }}>Select a backing datasource to configure conflict resolution.</p>
                )}
              </div>
            </div>

            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, alignItems: 'center' }}>
              {datasourceSaved ? <span style={{ fontSize: 12, color: '#15803d' }}>Saved</span> : null}
              {datasourceDirty ? <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>1 edit</span> : null}
              <button
                type="button"
                onClick={saveDatasource}
                disabled={!datasourceDirty}
                style={{ padding: '8px 16px', border: 0, borderRadius: 4, background: datasourceDirty ? '#15803d' : '#aab4c0', color: '#fff', fontSize: 13, fontWeight: 600, cursor: datasourceDirty ? 'pointer' : 'not-allowed' }}
              >
                Save
              </button>
            </div>

            {datasetPickerOpen ? (
              <div role="dialog" aria-modal="true" onMouseDown={(event) => { if (event.target === event.currentTarget) setDatasetPickerOpen(false); }} style={{ position: 'fixed', inset: 0, zIndex: 90, background: 'rgba(17, 24, 39, 0.4)', display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 24 }}>
                <section style={{ width: '100%', maxWidth: 520, maxHeight: '80vh', background: '#fff', borderRadius: 6, boxShadow: '0 20px 48px rgba(15, 23, 42, 0.2)', display: 'grid', gridTemplateRows: 'auto 1fr auto' }}>
                  <header style={{ padding: '12px 18px', borderBottom: '1px solid var(--border-subtle)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <h3 style={{ margin: 0, fontSize: 14, fontWeight: 600 }}>Select backing dataset</h3>
                    <button type="button" aria-label="Close" onClick={() => setDatasetPickerOpen(false)} className="of-button of-button--ghost" style={{ padding: 4 }}><Glyph name="x" size={12} /></button>
                  </header>
                  <div style={{ overflowY: 'auto', padding: 8 }}>
                    {datasets.length === 0 ? (
                      <p className="of-text-muted" style={{ padding: 12, fontSize: 12, margin: 0 }}>No datasets available.</p>
                    ) : datasets.map((dataset) => (
                      <button
                        key={dataset.id}
                        type="button"
                        onClick={() => { patchSettings({ backing_dataset_id: dataset.id }); setDatasetPickerOpen(false); }}
                        style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%', padding: '8px 10px', border: 0, background: datasourceSettings.backing_dataset_id === dataset.id ? 'rgba(45, 114, 210, 0.06)' : 'transparent', cursor: 'pointer', textAlign: 'left', fontSize: 13, borderRadius: 4 }}
                      >
                        <Glyph name="database" size={13} tone="#0d9488" />
                        <span style={{ flex: 1 }}>{dataset.name}</span>
                        {datasourceSettings.backing_dataset_id === dataset.id ? <Glyph name="check" size={12} tone="#15803d" /> : null}
                      </button>
                    ))}
                  </div>
                  <footer style={{ padding: 12, display: 'flex', justifyContent: 'flex-end', borderTop: '1px solid var(--border-subtle)' }}>
                    <button type="button" onClick={() => setDatasetPickerOpen(false)} className="of-button">Cancel</button>
                  </footer>
                </section>
              </div>
            ) : null}
          </section>
        );
      })()}

      {tab === 'links' && (
        <section className="of-panel" style={{ padding: 16 }}>
          <p className="of-eyebrow">Link types ({links.length})</p>
          <ul style={{ marginTop: 8, paddingLeft: 0, listStyle: 'none', display: 'grid', gap: 8 }}>
            {links.map((link) => (
              <li key={link.id} style={{ padding: 10, border: '1px solid var(--border-subtle)', borderRadius: 8 }}>
                <strong>{link.display_name}</strong> <span className="of-text-muted">/ {link.name}</span>
                <p className="of-text-muted" style={{ fontSize: 12, margin: '4px 0 0' }}>
                  {link.source_type_id} to {link.target_type_id} / {link.cardinality}
                </p>
              </li>
            ))}
            {links.length === 0 && <li className="of-text-muted">No links.</li>}
          </ul>
        </section>
      )}

      {tab === 'rules' && (
        <section className="of-panel" style={{ padding: 16 }}>
          <p className="of-eyebrow">Rules ({rules.length})</p>
          <ul style={{ marginTop: 8, paddingLeft: 0, listStyle: 'none', display: 'grid', gap: 8 }}>
            {rules.map((rule) => (
              <li key={rule.id} style={{ padding: 10, border: '1px solid var(--border-subtle)', borderRadius: 8 }}>
                <strong>{rule.display_name || rule.name}</strong> <span className="of-text-muted">/ {rule.evaluation_mode}</span>
              </li>
            ))}
            {rules.length === 0 && <li className="of-text-muted">No rules.</li>}
          </ul>
        </section>
      )}

      {tab === 'shared' && (
        <section className="of-panel" style={{ padding: 16 }}>
          <p className="of-eyebrow">Shared property types ({shared.length})</p>
          <ul style={{ marginTop: 8, paddingLeft: 18, fontSize: 12 }}>
            {shared.map((property) => (
              <li key={property.id}>
                <strong>{property.display_name}</strong> / {property.name} / {property.property_type}
              </li>
            ))}
            {shared.length === 0 && <li className="of-text-muted">None attached.</li>}
          </ul>
        </section>
      )}

      <ObjectDetailDrawer
        open={selectedObject !== null}
        typeId={type.id}
        objectId={selectedObject?.id ?? null}
        objectType={type}
        initialObject={selectedObject}
        properties={properties}
        actions={actions}
        onClose={() => setSelectedObject(null)}
        onObjectUpdated={handleObjectUpdated}
      />

      <SaveAsAppModal
        open={saveAsOpen}
        defaultName={type ? `${type.display_name || type.name} Inbox` : ''}
        onClose={() => setSaveAsOpen(false)}
        onSaved={(app) => {
          setSaveAsOpen(false);
          navigate(`/apps/${encodeURIComponent(app.id)}/workshop`);
        }}
      />
    </section>
  );
}
