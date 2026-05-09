import { useEffect, useMemo, useState } from 'react';

import { Glyph } from '@/lib/components/ui/Glyph';
import {
  createObjectType,
  createProperty,
  type ObjectType,
} from '@/lib/api/ontology';
import { listDatasets, previewDataset, type Dataset } from '@/lib/api/datasets';

interface CreateObjectTypeWizardProps {
  open: boolean;
  onClose: () => void;
  onCreated: (objectType: ObjectType) => void;
}

type StepId = 'datasource' | 'metadata' | 'properties' | 'actions';

interface PropertyRow {
  source_column: string;
  property_name: string;
  display_name: string;
  property_type: string;
}

const STEPS: Array<{ id: StepId; label: string }> = [
  { id: 'datasource', label: 'Datasource' },
  { id: 'metadata', label: 'Metadata' },
  { id: 'properties', label: 'Properties' },
  { id: 'actions', label: 'Actions' },
];

const PROPERTY_TYPES = ['STRING', 'INTEGER', 'LONG', 'DOUBLE', 'BOOLEAN', 'TIMESTAMP', 'DATE'];

function snakeToTitle(value: string): string {
  return value
    .replace(/[_-]+/g, ' ')
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .replace(/\b\w/g, (m) => m.toUpperCase());
}

function snakeIdent(value: string): string {
  return value
    .toLowerCase()
    .replace(/([a-z0-9])([A-Z])/g, '$1_$2')
    .replace(/[^a-z0-9_]+/g, '_')
    .replace(/^_+|_+$/g, '');
}

function inferPropertyType(rawType: string | undefined): string {
  if (!rawType) return 'STRING';
  const upper = rawType.toUpperCase();
  if (upper.includes('TIMESTAMP')) return 'TIMESTAMP';
  if (upper.includes('DATE')) return 'DATE';
  if (upper.includes('INT') && upper.includes('64')) return 'LONG';
  if (upper.includes('LONG')) return 'LONG';
  if (upper.includes('INT')) return 'INTEGER';
  if (upper.includes('DOUBLE') || upper.includes('FLOAT') || upper.includes('DECIMAL')) return 'DOUBLE';
  if (upper.includes('BOOL')) return 'BOOLEAN';
  return 'STRING';
}

export function CreateObjectTypeWizard({ open, onClose, onCreated }: CreateObjectTypeWizardProps) {
  const [step, setStep] = useState<StepId>('datasource');
  const [datasourceMode, setDatasourceMode] = useState<'use' | 'continue'>('use');
  const [datasets, setDatasets] = useState<Dataset[]>([]);
  const [datasetSearch, setDatasetSearch] = useState('');
  const [datasetPickerOpen, setDatasetPickerOpen] = useState(false);
  const [selectedDataset, setSelectedDataset] = useState<Dataset | null>(null);
  const [columns, setColumns] = useState<Array<{ name: string; type: string }>>([]);

  const [name, setName] = useState('');
  const [pluralName, setPluralName] = useState('');
  const [description, setDescription] = useState('');

  const [propertyRows, setPropertyRows] = useState<PropertyRow[]>([]);
  const [primaryKeyColumn, setPrimaryKeyColumn] = useState('');
  const [titleColumn, setTitleColumn] = useState('');

  const [actions, setActions] = useState<{ create: boolean; modify: boolean; delete: boolean }>({
    create: false,
    modify: false,
    delete: false,
  });

  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!open) return;
    setStep('datasource');
    setDatasourceMode('use');
    setSelectedDataset(null);
    setDatasets([]);
    setColumns([]);
    setName('');
    setPluralName('');
    setDescription('');
    setPropertyRows([]);
    setPrimaryKeyColumn('');
    setTitleColumn('');
    setActions({ create: false, modify: false, delete: false });
    setError('');
  }, [open]);

  useEffect(() => {
    if (!open || datasets.length > 0) return;
    void listDatasets({ per_page: 200 })
      .then((response) => setDatasets(response.data))
      .catch(() => setDatasets([]));
  }, [open, datasets.length]);

  useEffect(() => {
    if (!selectedDataset) {
      setColumns([]);
      return;
    }
    let cancelled = false;
    previewDataset(selectedDataset.id, { limit: 1 })
      .then((response) => {
        if (cancelled) return;
        const cols = response.columns?.map((column) => ({ name: column.name, type: column.field_type ?? column.data_type ?? 'string' })) ?? [];
        setColumns(cols);
        const rows: PropertyRow[] = cols.map((column) => ({
          source_column: column.name,
          property_name: snakeIdent(column.name),
          display_name: snakeToTitle(column.name),
          property_type: inferPropertyType(column.type),
        }));
        setPropertyRows(rows);
        const idCandidate = cols.find((column) => /(^|_)id$/i.test(column.name));
        if (idCandidate) setPrimaryKeyColumn(idCandidate.name);
        const titleCandidate = cols.find((column) => /name|title/i.test(column.name));
        if (titleCandidate) setTitleColumn(titleCandidate.name);
      })
      .catch(() => {
        if (!cancelled) setColumns([]);
      });
    return () => {
      cancelled = true;
    };
  }, [selectedDataset]);

  if (!open) return null;

  const currentStepIndex = STEPS.findIndex((entry) => entry.id === step);
  const canGoNext = (() => {
    if (step === 'datasource') return datasourceMode === 'continue' || Boolean(selectedDataset);
    if (step === 'metadata') return name.trim().length > 0;
    if (step === 'properties') return propertyRows.length > 0 ? Boolean(primaryKeyColumn) : true;
    return true;
  })();

  function patchRow(index: number, patch: Partial<PropertyRow>) {
    setPropertyRows((current) => current.map((row, i) => (i === index ? { ...row, ...patch } : row)));
  }

  async function submit() {
    setSubmitting(true);
    setError('');
    try {
      const created = await createObjectType({
        name: snakeIdent(name) || 'new_object_type',
        display_name: name.trim() || undefined,
        description: description.trim() || undefined,
        primary_key_property: primaryKeyColumn ? snakeIdent(primaryKeyColumn) : undefined,
      });
      const propertyResults = await Promise.allSettled(
        propertyRows.map((row) =>
          createProperty(created.id, {
            name: row.property_name,
            display_name: row.display_name,
            property_type: row.property_type,
            required: false,
          }),
        ),
      );
      const propertyFailures = propertyResults.filter((result) => result.status === 'rejected').length;
      if (propertyFailures > 0) {
        setError(`Object type created but ${propertyFailures} property write failed.`);
      }
      onCreated(created);
      onClose();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Create failed');
    } finally {
      setSubmitting(false);
    }
  }

  const filteredDatasets = useMemo(() => {
    const q = datasetSearch.trim().toLowerCase();
    if (!q) return datasets;
    return datasets.filter((entry) => entry.name.toLowerCase().includes(q));
  }, [datasetSearch, datasets]);

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="otw-title"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !submitting) onClose();
      }}
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 95,
        background: 'rgba(17, 24, 39, 0.4)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: 32,
      }}
    >
      <section
        style={{
          width: '100%',
          maxWidth: 880,
          height: 'min(640px, calc(100vh - 64px))',
          background: '#fff',
          borderRadius: 6,
          boxShadow: '0 20px 60px rgba(15, 23, 42, 0.18)',
          display: 'grid',
          gridTemplateRows: 'auto 1fr auto',
          overflow: 'hidden',
        }}
      >
        <header style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '12px 18px', borderBottom: '1px solid var(--border-subtle)' }}>
          <h2 id="otw-title" style={{ margin: 0, fontSize: 15, fontWeight: 600 }}>Create a new object type</h2>
          <button type="button" aria-label="Close" onClick={onClose} className="of-button of-button--ghost" style={{ padding: 4 }}>
            <Glyph name="x" size={14} />
          </button>
        </header>

        <div style={{ display: 'grid', gridTemplateColumns: '220px minmax(0, 1fr)', minHeight: 0 }}>
          <aside style={{ borderRight: '1px solid var(--border-subtle)', padding: 12, display: 'grid', gap: 4, alignContent: 'start' }}>
            {STEPS.map((entry, index) => {
              const active = entry.id === step;
              const reachable = index <= currentStepIndex;
              return (
                <button
                  key={entry.id}
                  type="button"
                  onClick={() => reachable && setStep(entry.id)}
                  disabled={!reachable}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 10,
                    padding: '8px 10px',
                    border: 0,
                    background: active ? 'rgba(45, 114, 210, 0.06)' : 'transparent',
                    color: active ? 'var(--status-info)' : reachable ? 'var(--text-strong)' : 'var(--text-muted)',
                    fontWeight: active ? 600 : 500,
                    fontSize: 13,
                    borderRadius: 4,
                    cursor: reachable ? 'pointer' : 'not-allowed',
                    textAlign: 'left',
                  }}
                >
                  <span
                    style={{
                      display: 'inline-flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      width: 24,
                      height: 24,
                      borderRadius: 999,
                      fontSize: 12,
                      fontWeight: 600,
                      background: active ? 'var(--status-info)' : '#aab4c0',
                      color: '#fff',
                    }}
                  >
                    {index + 1}
                  </span>
                  {entry.label}
                </button>
              );
            })}
          </aside>

          <main style={{ overflowY: 'auto', padding: '20px 24px' }}>
            {step === 'datasource' ? (
              <div style={{ display: 'grid', gap: 18 }}>
                <div>
                  <p className="of-text-muted" style={{ margin: 0, fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.06em' }}>Step 1</p>
                  <h3 style={{ margin: '4px 0 12px', fontSize: 18, fontWeight: 600 }}>Object type backing</h3>
                  <p style={{ margin: '0 0 10px', fontSize: 13, fontWeight: 600 }}>Datasource</p>
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 }}>
                    <button
                      type="button"
                      onClick={() => setDatasourceMode('continue')}
                      style={cardStyle(datasourceMode === 'continue')}
                    >
                      <Glyph name="document" size={18} tone="#5c7080" />
                      <div style={{ display: 'grid', gap: 2, textAlign: 'left' }}>
                        <strong style={{ fontSize: 13 }}>Continue without datasource</strong>
                        <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Generate a dataset for permissions purposes</span>
                      </div>
                    </button>
                    <button
                      type="button"
                      onClick={() => setDatasourceMode('use')}
                      style={cardStyle(datasourceMode === 'use')}
                    >
                      <Glyph name="database" size={18} tone="#2d72d2" />
                      <div style={{ display: 'grid', gap: 2, textAlign: 'left' }}>
                        <strong style={{ fontSize: 13 }}>Use existing datasource</strong>
                        <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Select a preexisting Foundry datasource</span>
                      </div>
                      {datasourceMode === 'use' ? <Glyph name="check" size={14} tone="var(--status-info)" /> : null}
                    </button>
                  </div>
                </div>

                {datasourceMode === 'use' ? (
                  <div>
                    <p style={{ margin: '0 0 6px', fontSize: 13, fontWeight: 600 }}>Select a datasource to back this object type</p>
                    {selectedDataset ? (
                      <div style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '10px 12px', border: '1px solid var(--border-default)', borderRadius: 4 }}>
                        <Glyph name="database" size={14} tone="#2d72d2" />
                        <div style={{ flex: 1, display: 'grid' }}>
                          <span style={{ fontSize: 13, fontWeight: 600 }}>{selectedDataset.name}</span>
                          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{selectedDataset.storage_path || selectedDataset.id}</span>
                        </div>
                        <button type="button" className="of-button" onClick={() => setDatasetPickerOpen(true)} style={{ fontSize: 12 }}>
                          <Glyph name="pencil" size={12} /> Replace
                        </button>
                      </div>
                    ) : (
                      <button type="button" className="of-button" onClick={() => setDatasetPickerOpen(true)} style={{ fontSize: 13 }}>
                        <Glyph name="database" size={13} /> Select datasource
                      </button>
                    )}
                    {datasetPickerOpen ? (
                      <div style={{ marginTop: 8, border: '1px solid var(--border-default)', borderRadius: 4, background: '#fff', padding: 8, maxHeight: 300, overflowY: 'auto' }}>
                        <input
                          autoFocus
                          value={datasetSearch}
                          onChange={(event) => setDatasetSearch(event.target.value)}
                          placeholder="Search datasets..."
                          style={{ width: '100%', padding: '6px 10px', border: '1px solid var(--border-default)', borderRadius: 4, fontSize: 13, marginBottom: 6 }}
                        />
                        {filteredDatasets.map((dataset) => (
                          <button
                            key={dataset.id}
                            type="button"
                            onClick={() => {
                              setSelectedDataset(dataset);
                              setDatasetPickerOpen(false);
                            }}
                            style={{
                              display: 'flex',
                              alignItems: 'center',
                              gap: 8,
                              width: '100%',
                              padding: '6px 8px',
                              border: 0,
                              background: 'transparent',
                              cursor: 'pointer',
                              textAlign: 'left',
                              fontSize: 13,
                            }}
                          >
                            <Glyph name="database" size={13} tone="#2d72d2" />
                            <span>{dataset.name}</span>
                          </button>
                        ))}
                        {filteredDatasets.length === 0 ? (
                          <p className="of-text-muted" style={{ margin: 8, fontSize: 12 }}>No datasets.</p>
                        ) : null}
                      </div>
                    ) : null}
                  </div>
                ) : null}
              </div>
            ) : null}

            {step === 'metadata' ? (
              <div style={{ display: 'grid', gap: 14 }}>
                <div>
                  <p className="of-text-muted" style={{ margin: 0, fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.06em' }}>Step 2</p>
                  <h3 style={{ margin: '4px 0 0', fontSize: 18, fontWeight: 600 }}>Configure object type metadata</h3>
                </div>
                <div style={{ display: 'grid', gridTemplateColumns: '60px 1fr 1fr', gap: 12 }}>
                  <Field label="Icon">
                    <button type="button" style={{ width: 40, height: 40, border: '1px solid var(--border-default)', borderRadius: 4, background: '#f4f6f9', cursor: 'pointer' }}>
                      <Glyph name="cube" size={18} tone="#2d72d2" />
                    </button>
                  </Field>
                  <Field label="Name">
                    <input
                      value={name}
                      onChange={(event) => {
                        setName(event.target.value);
                        if (!pluralName.trim()) setPluralName(`${event.target.value}s`);
                      }}
                      style={inputStyle()}
                      placeholder="Order"
                    />
                  </Field>
                  <Field label="Plural name">
                    <input value={pluralName} onChange={(event) => setPluralName(event.target.value)} style={inputStyle()} placeholder="Orders" />
                  </Field>
                </div>
                <Field label="Description">
                  <input value={description} onChange={(event) => setDescription(event.target.value)} style={inputStyle()} placeholder="Enter optional description…" />
                </Field>
                <Field label="Groups">
                  <button type="button" className="of-button" disabled style={{ fontSize: 12, justifyContent: 'flex-start' }}>
                    <Glyph name="plus" size={12} tone="var(--status-info)" /> Add to group
                  </button>
                </Field>
              </div>
            ) : null}

            {step === 'properties' ? (
              <div style={{ display: 'grid', gap: 14 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-end' }}>
                  <div>
                    <p className="of-text-muted" style={{ margin: 0, fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.06em' }}>Step 3</p>
                    <h3 style={{ margin: '4px 0 0', fontSize: 18, fontWeight: 600 }}>Properties</h3>
                  </div>
                  <button type="button" className="of-button" disabled style={{ fontSize: 12 }}>
                    <Glyph name="plus" size={12} tone="var(--status-info)" /> Implement interface
                  </button>
                </div>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr auto 1fr 140px', gap: 8, fontSize: 12, color: 'var(--text-muted)' }}>
                  <span>Source</span>
                  <span />
                  <span>Property</span>
                  <span>Type</span>
                </div>
                <div style={{ display: 'grid', gap: 6, maxHeight: 280, overflowY: 'auto' }}>
                  {propertyRows.length === 0 ? (
                    <p className="of-text-muted" style={{ fontSize: 12 }}>No columns from datasource. Pick a datasource in Step 1 to auto-map columns.</p>
                  ) : (
                    propertyRows.map((row, index) => (
                      <div key={row.source_column} style={{ display: 'grid', gridTemplateColumns: '1fr auto 1fr 140px', gap: 8, alignItems: 'center' }}>
                        <span style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '6px 10px', border: '1px solid var(--border-subtle)', borderRadius: 4, background: '#fff', fontSize: 13 }}>
                          <Glyph name="database" size={12} tone="#2d72d2" />
                          {row.source_column}
                        </span>
                        <Glyph name="chevron-right" size={12} tone="#5c7080" />
                        <input
                          value={row.display_name}
                          onChange={(event) => patchRow(index, { display_name: event.target.value, property_name: snakeIdent(event.target.value) })}
                          style={inputStyle()}
                        />
                        <select value={row.property_type} onChange={(event) => patchRow(index, { property_type: event.target.value })} style={inputStyle()}>
                          {PROPERTY_TYPES.map((type) => (
                            <option key={type} value={type}>{type}</option>
                          ))}
                        </select>
                      </div>
                    ))
                  )}
                </div>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 14 }}>
                  <Field label="Primary key">
                    <select value={primaryKeyColumn} onChange={(event) => setPrimaryKeyColumn(event.target.value)} style={inputStyle()}>
                      <option value="">Select primary key</option>
                      {columns.map((column) => (
                        <option key={column.name} value={column.name}>{column.name}</option>
                      ))}
                    </select>
                  </Field>
                  <Field label="Title">
                    <select value={titleColumn} onChange={(event) => setTitleColumn(event.target.value)} style={inputStyle()}>
                      <option value="">Select title column</option>
                      {columns.map((column) => (
                        <option key={column.name} value={column.name}>{column.name}</option>
                      ))}
                    </select>
                  </Field>
                </div>
              </div>
            ) : null}

            {step === 'actions' ? (
              <div style={{ display: 'grid', gap: 14 }}>
                <div>
                  <p className="of-text-muted" style={{ margin: 0, fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.06em' }}>Step 4</p>
                  <h3 style={{ margin: '4px 0 0', fontSize: 18, fontWeight: 600 }}>Generate action types</h3>
                </div>
                <p className="of-text-muted" style={{ margin: 0, fontSize: 13 }}>Select action types to generate</p>
                <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 6 }}>
                  <ActionTypeRow
                    icon="object"
                    title={`Create ${name || 'object'}`}
                    description={`Set ${propertyRows.slice(0, 3).map((row) => row.display_name).join(', ') || '—'}, and ${Math.max(propertyRows.length - 3, 0)} more properties`}
                    checked={actions.create}
                    onToggle={() => setActions((current) => ({ ...current, create: !current.create }))}
                  />
                  <ActionTypeRow
                    icon="pencil"
                    title={`Modify ${name || 'object'}`}
                    description={`Modify ${propertyRows.slice(0, 3).map((row) => row.display_name).join(', ') || '—'}, and ${Math.max(propertyRows.length - 3, 0)} more properties`}
                    checked={actions.modify}
                    onToggle={() => setActions((current) => ({ ...current, modify: !current.modify }))}
                  />
                  <ActionTypeRow
                    icon="object"
                    title={`Delete ${name || 'object'}`}
                    description="Allows deleting object instances and all of their properties"
                    checked={actions.delete}
                    onToggle={() => setActions((current) => ({ ...current, delete: !current.delete }))}
                  />
                </div>
                <Field label="Select who can execute these action types">
                  <input className="of-input" disabled placeholder="Search users or groups..." style={inputStyle()} />
                </Field>
              </div>
            ) : null}

            {error ? (
              <div role="alert" className="of-status-danger" style={{ marginTop: 14, padding: '8px 12px', fontSize: 12, borderRadius: 4 }}>
                {error}
              </div>
            ) : null}
          </main>
        </div>

        <footer style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '12px 18px', borderTop: '1px solid var(--border-subtle)' }}>
          <button
            type="button"
            onClick={() => {
              const previous = STEPS[currentStepIndex - 1];
              if (previous) setStep(previous.id);
            }}
            className="of-button"
            disabled={currentStepIndex === 0 || submitting}
          >
            Back
          </button>
          {step === 'actions' ? (
            <button
              type="button"
              onClick={() => void submit()}
              disabled={submitting}
              style={{
                padding: '8px 14px',
                border: 0,
                borderRadius: 4,
                background: '#15803d',
                color: '#fff',
                fontSize: 13,
                fontWeight: 600,
                cursor: submitting ? 'not-allowed' : 'pointer',
              }}
            >
              {submitting ? 'Creating...' : 'Create'}
            </button>
          ) : (
            <button
              type="button"
              onClick={() => {
                const next = STEPS[currentStepIndex + 1];
                if (next) setStep(next.id);
              }}
              disabled={!canGoNext}
              style={{
                padding: '8px 14px',
                border: 0,
                borderRadius: 4,
                background: '#2d72d2',
                color: '#fff',
                fontSize: 13,
                fontWeight: 600,
                cursor: canGoNext ? 'pointer' : 'not-allowed',
                opacity: canGoNext ? 1 : 0.6,
              }}
            >
              Next
            </button>
          )}
        </footer>
      </section>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label style={{ display: 'grid', gap: 4 }}>
      <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{label}</span>
      {children}
    </label>
  );
}

function ActionTypeRow({
  icon,
  title,
  description,
  checked,
  onToggle,
}: {
  icon: 'object' | 'pencil';
  title: string;
  description: string;
  checked: boolean;
  onToggle: () => void;
}) {
  return (
    <label style={{ display: 'flex', alignItems: 'flex-start', gap: 12, padding: '12px 14px', borderBottom: '1px solid var(--border-subtle)', cursor: 'pointer' }}>
      <input type="checkbox" checked={checked} onChange={onToggle} style={{ accentColor: 'var(--status-info)', marginTop: 2 }} />
      <span
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          width: 28,
          height: 28,
          borderRadius: 4,
          background: 'rgba(45, 114, 210, 0.12)',
          color: 'var(--status-info)',
        }}
      >
        <Glyph name={icon} size={14} tone="var(--status-info)" />
      </span>
      <span style={{ display: 'grid', gap: 2 }}>
        <strong style={{ fontSize: 13 }}>{title}</strong>
        <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{description}</span>
      </span>
    </label>
  );
}

function cardStyle(active: boolean): React.CSSProperties {
  return {
    display: 'flex',
    alignItems: 'center',
    gap: 12,
    padding: '14px 16px',
    border: active ? '2px solid var(--status-info)' : '1px solid var(--border-default)',
    borderRadius: 6,
    background: active ? 'rgba(45, 114, 210, 0.04)' : '#fff',
    cursor: 'pointer',
    textAlign: 'left',
    width: '100%',
  };
}

function inputStyle(): React.CSSProperties {
  return {
    padding: '6px 10px',
    border: '1px solid var(--border-default)',
    borderRadius: 4,
    background: '#fff',
    fontSize: 13,
    color: 'var(--text-strong)',
    width: '100%',
  };
}
