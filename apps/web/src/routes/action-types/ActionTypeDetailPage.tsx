import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';

import {
  getActionType,
  getObjectType,
  listProperties,
  updateActionType,
  type ActionType,
  type ObjectType,
  type Property,
} from '@/lib/api/ontology';
import { Glyph, type GlyphName } from '@/lib/components/ui/Glyph';

type SectionKey = 'overview' | 'rules' | 'parameters' | 'user-interface' | 'capabilities' | 'security' | 'automations' | 'history';

interface SidebarEntry {
  id: SectionKey;
  label: string;
  icon: GlyphName;
  disabled?: boolean;
}

const SIDEBAR: SidebarEntry[] = [
  { id: 'overview', label: 'Overview', icon: 'home' },
  { id: 'rules', label: 'Rules', icon: 'pencil' },
  { id: 'parameters', label: 'Parameters', icon: 'list' },
  { id: 'user-interface', label: 'User Interface', icon: 'view-grid' },
  { id: 'capabilities', label: 'Capabilities', icon: 'badge-check' },
  { id: 'security', label: 'Security & Submission Criteria', icon: 'shield' },
  { id: 'automations', label: 'Automations', icon: 'run', disabled: true },
  { id: 'history', label: 'History', icon: 'autosaved' },
];

interface PropertyMapping {
  property_name: string;
  kind: string;
  static_value?: string;
}

interface FieldOrderingItem {
  id: string;
  property_name: string;
  display_name: string;
  used: boolean;
  is_object_param: boolean;
}

function readMappings(action: ActionType | null): PropertyMapping[] {
  const config = action?.config as { property_mappings?: PropertyMapping[] } | null | undefined;
  return Array.isArray(config?.property_mappings) ? config!.property_mappings : [];
}

export function ActionTypeDetailPage() {
  const { id = '' } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [section, setSection] = useState<SectionKey>('user-interface');
  const [action, setAction] = useState<ActionType | null>(null);
  const [objectType, setObjectType] = useState<ObjectType | null>(null);
  const [properties, setProperties] = useState<Property[]>([]);
  const [defaultLayout, setDefaultLayout] = useState<'form' | 'table'>('form');
  const [allowSwitchingLayouts, setAllowSwitchingLayouts] = useState(false);
  const [mappings, setMappings] = useState<PropertyMapping[]>([]);
  const [originalMappings, setOriginalMappings] = useState<PropertyMapping[]>([]);
  const [reviewOpen, setReviewOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!id) return;
    let cancelled = false;
    setLoading(true);
    setError('');
    void (async () => {
      try {
        const fetched = await getActionType(id);
        if (cancelled) return;
        setAction(fetched);
        const initialMappings = readMappings(fetched);
        setMappings(initialMappings);
        setOriginalMappings(initialMappings);
        const config = fetched.config as { default_layout?: 'form' | 'table'; allow_layout_switching?: boolean } | null | undefined;
        setDefaultLayout(config?.default_layout ?? 'form');
        setAllowSwitchingLayouts(Boolean(config?.allow_layout_switching));
        const [type, props] = await Promise.all([
          getObjectType(fetched.object_type_id).catch(() => null),
          listProperties(fetched.object_type_id).catch(() => [] as Property[]),
        ]);
        if (cancelled) return;
        setObjectType(type);
        setProperties(props);
      } catch (cause) {
        if (cancelled) return;
        setError(cause instanceof Error ? cause.message : 'Failed to load action');
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [id]);

  const fieldOrdering: FieldOrderingItem[] = useMemo(() => {
    const items: FieldOrderingItem[] = [];
    if (objectType && action) {
      items.push({
        id: `__object__:${objectType.id}`,
        property_name: '',
        display_name: objectType.display_name || objectType.name,
        used: true,
        is_object_param: true,
      });
    }
    for (const mapping of mappings) {
      const property = properties.find((p) => p.name === mapping.property_name);
      items.push({
        id: mapping.property_name,
        property_name: mapping.property_name,
        display_name: property?.display_name || mapping.property_name,
        used: mapping.kind === 'parameter' || mapping.kind === 'unique_id',
        is_object_param: false,
      });
    }
    return items;
  }, [objectType, action, mappings, properties]);

  const dirty = useMemo(() => {
    if (mappings.length !== originalMappings.length) return true;
    return mappings.some((m, idx) => {
      const original = originalMappings[idx];
      return !original || original.property_name !== m.property_name || original.kind !== m.kind;
    });
  }, [mappings, originalMappings]);

  function removeMapping(propertyName: string) {
    setMappings((current) => current.filter((m) => m.property_name !== propertyName));
  }

  function reorder(from: number, to: number) {
    if (from === to || from < 0 || to < 0) return;
    setMappings((current) => {
      const next = [...current];
      const [moved] = next.splice(from, 1);
      next.splice(to, 0, moved);
      return next;
    });
  }

  async function save() {
    if (!action) return;
    setSaving(true);
    setError('');
    try {
      const baseConfig = (action.config as Record<string, unknown> | null | undefined) ?? {};
      const updated = await updateActionType(action.id, {
        config: { ...baseConfig, property_mappings: mappings, default_layout: defaultLayout, allow_layout_switching: allowSwitchingLayouts },
        input_schema: mappings
          .filter((m) => m.kind === 'parameter' || m.kind === 'unique_id')
          .map((m) => {
            const property = properties.find((p) => p.name === m.property_name);
            return {
              name: m.property_name,
              display_name: property?.display_name ?? m.property_name,
              description: property?.description ?? '',
              property_type: property?.property_type ?? 'string',
              required: false,
            };
          }),
      });
      setAction(updated);
      const next = readMappings(updated);
      setMappings(next);
      setOriginalMappings(next);
      setReviewOpen(false);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Save failed');
    } finally {
      setSaving(false);
    }
  }

  if (loading || !action) {
    return (
      <div style={{ padding: 24 }}>
        <p className="of-text-muted">{error || 'Loading action…'}</p>
      </div>
    );
  }

  const editCount = (originalMappings.length - mappings.length) + (mappings.length !== originalMappings.length ? 0 : 0);

  return (
    <div style={{ display: 'grid', gridTemplateColumns: '320px 1fr', minHeight: 'calc(100vh - 56px)', background: '#f4f6f9' }}>
      <aside style={{ borderRight: '1px solid var(--border-subtle)', background: '#fff', display: 'grid', gridTemplateRows: 'auto auto 1fr', minHeight: 0 }}>
        <div style={{ padding: '12px 14px', borderBottom: '1px solid var(--border-subtle)' }}>
          <button type="button" onClick={() => navigate(-1)} className="of-button of-button--ghost" style={{ padding: '4px 8px', fontSize: 12 }}>
            <Glyph name="chevron-left" size={11} /> {objectType?.display_name || objectType?.name || 'Back'}
          </button>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, padding: 16, borderBottom: '1px solid var(--border-subtle)' }}>
          <span style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 36, height: 36, borderRadius: 6, background: '#1c2127' }}>
            <Glyph name="pencil" size={16} tone="#fff" />
          </span>
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 14, fontWeight: 600 }}>{action.display_name || action.name}</div>
            <div className="of-text-muted" style={{ fontSize: 11 }}>{action.name}</div>
          </div>
          {dirty ? <Glyph name="info" size={14} tone="#cf923f" /> : null}
        </div>
        <nav style={{ padding: 8, overflowY: 'auto' }}>
          {SIDEBAR.map((entry) => {
            const active = section === entry.id;
            return (
              <button
                key={entry.id}
                type="button"
                onClick={() => !entry.disabled && setSection(entry.id)}
                disabled={entry.disabled}
                style={{
                  display: 'flex', alignItems: 'center', gap: 10,
                  width: '100%', padding: '10px 12px',
                  border: 0, borderRadius: 4, cursor: entry.disabled ? 'not-allowed' : 'pointer',
                  background: active ? 'rgba(45, 114, 210, 0.08)' : 'transparent',
                  color: active ? 'var(--status-info)' : entry.disabled ? '#aab4c0' : 'var(--text-strong)',
                  fontSize: 13, fontWeight: active ? 600 : 500, textAlign: 'left',
                }}
              >
                <Glyph name={entry.icon} size={14} tone={active ? 'var(--status-info)' : entry.disabled ? '#aab4c0' : '#5c7080'} />
                {entry.label}
              </button>
            );
          })}
        </nav>
      </aside>

      <main style={{ display: 'grid', gridTemplateRows: 'auto 1fr', minHeight: 0 }}>
        <header style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 12, padding: '8px 18px', background: '#fff', borderBottom: '1px solid var(--border-subtle)' }}>
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--text-muted)' }}>
            <Glyph name="object" size={12} /> Main
            <Glyph name="chevron-down" size={10} />
          </span>
          {dirty ? <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{Math.max(1, editCount)} edit{editCount === 1 ? '' : 's'}</span> : null}
          {dirty ? (
            <button type="button" className="of-button" onClick={() => { setMappings(originalMappings); }}>
              Discard
            </button>
          ) : null}
          <button
            type="button"
            onClick={() => setReviewOpen(true)}
            disabled={!dirty || saving}
            style={{ padding: '8px 16px', border: 0, borderRadius: 4, background: dirty ? '#15803d' : '#aab4c0', color: '#fff', fontSize: 13, fontWeight: 600, cursor: dirty ? 'pointer' : 'not-allowed' }}
          >
            Save
          </button>
        </header>

        <div style={{ overflowY: 'auto', padding: 18 }}>
          {section === 'user-interface' ? (
            <div style={{ maxWidth: 800, margin: '0 auto', display: 'grid', gap: 14 }}>
              <div className="of-panel" style={{ padding: 0, overflow: 'hidden' }}>
                <header style={{ padding: '14px 16px', borderBottom: '1px solid var(--border-subtle)' }}>
                  <strong style={{ fontSize: 14 }}>Action layout</strong>
                </header>
                <div style={{ padding: 16, display: 'grid', gap: 14 }}>
                  <div style={{ display: 'flex', alignItems: 'flex-start', gap: 10, padding: 12, borderRadius: 4, background: 'rgba(45, 114, 210, 0.06)' }}>
                    <Glyph name="info" size={14} tone="var(--status-info)" />
                    <div style={{ fontSize: 12 }}>
                      <strong style={{ display: 'block', marginBottom: 4 }}>This configuration currently only applies to Actions in Workshop and Slate apps.</strong>
                      In Workshop, you can further configure the layout in the Inline Action widget configuration.
                    </div>
                  </div>
                  <div>
                    <p style={{ margin: 0, fontSize: 13, fontWeight: 600 }}>Default layout</p>
                    <p className="of-text-muted" style={{ margin: '4px 0 12px', fontSize: 12 }}>Select the layout that the action type will take by default. If switching is enabled, users can toggle between layout options.</p>
                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
                      {([
                        { id: 'form' as const, label: 'Form', icon: 'list' as GlyphName },
                        { id: 'table' as const, label: 'Table', icon: 'view-grid' as GlyphName },
                      ]).map((entry) => {
                        const active = defaultLayout === entry.id;
                        return (
                          <button
                            key={entry.id}
                            type="button"
                            onClick={() => setDefaultLayout(entry.id)}
                            style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '14px 16px', border: active ? '2px solid var(--status-info)' : '1px solid var(--border-default)', borderRadius: 6, background: '#fff', cursor: 'pointer', fontSize: 13, textAlign: 'left' }}
                          >
                            <Glyph name={entry.icon} size={14} tone="#5c7080" />
                            <span style={{ flex: 1, fontWeight: 600 }}>{entry.label}</span>
                            <span style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 18, height: 18, borderRadius: '50%', border: active ? '5px solid #2d72d2' : '1.5px solid #aab4c0' }} />
                          </button>
                        );
                      })}
                    </div>
                  </div>
                  <label style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                    <span style={{ position: 'relative', display: 'inline-flex', width: 36, height: 20, borderRadius: 999, background: allowSwitchingLayouts ? '#2d72d2' : '#aab4c0', cursor: 'pointer' }}>
                      <input
                        type="checkbox"
                        checked={allowSwitchingLayouts}
                        onChange={(event) => setAllowSwitchingLayouts(event.target.checked)}
                        style={{ position: 'absolute', inset: 0, opacity: 0, cursor: 'pointer' }}
                      />
                      <span style={{ position: 'absolute', top: 2, left: allowSwitchingLayouts ? 18 : 2, width: 16, height: 16, borderRadius: '50%', background: '#fff', boxShadow: '0 1px 3px rgba(15, 23, 42, 0.18)' }} />
                    </span>
                    <span style={{ fontSize: 13 }}>Allow switching between layouts</span>
                  </label>
                </div>
              </div>

              <div className="of-panel" style={{ padding: 0, overflow: 'hidden' }}>
                <header style={{ padding: '14px 16px', borderBottom: '1px solid var(--border-subtle)' }}>
                  <strong style={{ fontSize: 14 }}>Field ordering</strong>
                </header>
                <div style={{ padding: 16, display: 'grid', gap: 8 }}>
                  {fieldOrdering.map((item, index) => (
                    <FieldOrderingRow
                      key={item.id}
                      item={item}
                      index={index - 1}
                      isLocked={item.is_object_param}
                      onDelete={() => !item.is_object_param && removeMapping(item.property_name)}
                      onReorder={reorder}
                    />
                  ))}
                  {mappings.length === 0 ? (
                    <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>No parameters configured.</p>
                  ) : null}
                  <div style={{ display: 'flex', gap: 8, marginTop: 6 }}>
                    <button type="button" style={{ padding: '8px 14px', border: 0, borderRadius: 4, background: '#2d72d2', color: '#fff', fontSize: 13, fontWeight: 600, cursor: 'pointer', display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                      <Glyph name="plus" size={11} /> Add new parameter
                    </button>
                    <button type="button" className="of-button">
                      Add section
                    </button>
                  </div>
                </div>
              </div>
            </div>
          ) : (
            <div style={{ padding: 24, textAlign: 'center' }}>
              <p className="of-text-muted">{SIDEBAR.find((entry) => entry.id === section)?.label} settings coming soon.</p>
            </div>
          )}

          {error ? (
            <div role="alert" className="of-status-danger" style={{ marginTop: 14, padding: '8px 12px', borderRadius: 4, fontSize: 12 }}>
              {error}
            </div>
          ) : null}
        </div>
      </main>

      {reviewOpen ? (
        <ReviewEditsModal
          action={action}
          objectType={objectType}
          mappings={mappings}
          originalMappings={originalMappings}
          saving={saving}
          onCancel={() => setReviewOpen(false)}
          onSave={() => void save()}
        />
      ) : null}
    </div>
  );
}

function FieldOrderingRow({
  item,
  index,
  isLocked,
  onDelete,
  onReorder,
}: {
  item: FieldOrderingItem;
  index: number;
  isLocked: boolean;
  onDelete: () => void;
  onReorder: (from: number, to: number) => void;
}) {
  function handleDragStart(event: React.DragEvent<HTMLDivElement>) {
    if (isLocked) return;
    event.dataTransfer.setData('text/plain', String(index));
    event.dataTransfer.effectAllowed = 'move';
  }
  function handleDragOver(event: React.DragEvent<HTMLDivElement>) {
    event.preventDefault();
  }
  function handleDrop(event: React.DragEvent<HTMLDivElement>) {
    event.preventDefault();
    const from = Number(event.dataTransfer.getData('text/plain'));
    if (Number.isFinite(from)) onReorder(from, index);
  }

  return (
    <div
      draggable={!isLocked}
      onDragStart={handleDragStart}
      onDragOver={handleDragOver}
      onDrop={handleDrop}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 10,
        padding: '10px 12px',
        border: '1px solid var(--border-subtle)',
        borderRadius: 4,
        background: '#fff',
        cursor: isLocked ? 'default' : 'grab',
      }}
    >
      <span style={{ display: 'inline-flex', flexDirection: 'column', gap: 2 }} aria-hidden="true">
        {[0, 1, 2].map((row) => (
          <span key={row} style={{ display: 'flex', gap: 2 }}>
            <span style={{ width: 3, height: 3, borderRadius: '50%', background: isLocked ? '#d6dde3' : '#aab4c0' }} />
            <span style={{ width: 3, height: 3, borderRadius: '50%', background: isLocked ? '#d6dde3' : '#aab4c0' }} />
          </span>
        ))}
      </span>
      <span style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 24, height: 24, borderRadius: 4, background: 'rgba(45, 114, 210, 0.08)' }}>
        <Glyph name={item.is_object_param ? 'cube' : 'tag'} size={12} tone={item.is_object_param ? '#2d72d2' : '#5c7080'} />
      </span>
      <div style={{ flex: 1 }}>
        <div style={{ fontSize: 13, fontWeight: 500 }}>{item.display_name}</div>
        <div className="of-text-muted" style={{ fontSize: 11 }}>{item.is_object_param ? 'Used in Rules' : item.used ? 'Used in Rules' : 'Used in Rules'}</div>
      </div>
      {item.is_object_param ? null : (
        <span className="of-chip" style={{ background: 'rgba(45, 114, 210, 0.1)', color: 'var(--status-info)', fontSize: 11 }}>New</span>
      )}
      {!item.is_object_param && !item.used ? (
        <span className="of-chip" style={{ background: '#fef3c7', color: '#b45309', fontSize: 11 }}>Unused</span>
      ) : null}
      {!isLocked ? (
        <button type="button" aria-label="Remove parameter" onClick={onDelete} style={{ border: 0, background: 'transparent', cursor: 'pointer', color: 'var(--status-danger)', padding: 4 }}>
          <Glyph name="x" size={11} />
        </button>
      ) : null}
    </div>
  );
}

function ReviewEditsModal({
  action,
  objectType,
  mappings,
  originalMappings,
  saving,
  onCancel,
  onSave,
}: {
  action: ActionType;
  objectType: ObjectType | null;
  mappings: PropertyMapping[];
  originalMappings: PropertyMapping[];
  saving: boolean;
  onCancel: () => void;
  onSave: () => void;
}) {
  const removed = originalMappings.filter((original) => !mappings.some((m) => m.property_name === original.property_name));
  const added = mappings.filter((m) => !originalMappings.some((original) => original.property_name === m.property_name));
  const totalEdits = removed.length + added.length + 1;

  return (
    <div role="dialog" aria-modal="true" onMouseDown={(event) => { if (event.target === event.currentTarget && !saving) onCancel(); }} style={{ position: 'fixed', inset: 0, zIndex: 95, background: 'rgba(17, 24, 39, 0.42)', display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 24 }}>
      <section style={{ width: '100%', maxWidth: 720, maxHeight: '88vh', background: '#fff', borderRadius: 6, boxShadow: '0 20px 48px rgba(15, 23, 42, 0.2)', display: 'grid', gridTemplateRows: 'auto auto 1fr auto', overflow: 'hidden' }}>
        <header style={{ padding: '14px 18px', borderBottom: '1px solid var(--border-subtle)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <h3 style={{ margin: 0, fontSize: 14, fontWeight: 600 }}>Review edits</h3>
          <button type="button" aria-label="Close" onClick={onCancel} className="of-button of-button--ghost" style={{ padding: 4 }}><Glyph name="x" size={12} /></button>
        </header>
        <div style={{ padding: '14px 18px', borderBottom: '1px solid var(--border-subtle)', display: 'flex', alignItems: 'center', gap: 10 }}>
          <div style={{ flex: 1 }}>
            <p style={{ margin: 0, fontSize: 13, fontWeight: 600 }}>Propose your changes</p>
            <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>Selecting this will create a Branch with your Ontology changes and a draft Proposal to approve those changes. Open the Branch to view and add edits. Once all edits are approved, merge the Proposal.</p>
          </div>
          <span style={{ position: 'relative', display: 'inline-flex', width: 36, height: 20, borderRadius: 999, background: '#aab4c0' }}>
            <span style={{ position: 'absolute', top: 2, left: 2, width: 16, height: 16, borderRadius: '50%', background: '#fff' }} />
          </span>
        </div>
        <div style={{ padding: 18, overflowY: 'auto' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, borderBottom: '1px solid var(--border-subtle)', paddingBottom: 8 }}>
            <button type="button" style={{ border: 0, background: 'transparent', borderBottom: '2px solid var(--status-info)', color: 'var(--status-info)', fontWeight: 600, fontSize: 13, padding: '4px 0', cursor: 'pointer' }}>All edits ({totalEdits})</button>
            <button type="button" style={{ border: 0, background: 'transparent', color: 'var(--text-muted)', fontSize: 13, padding: '4px 8px', cursor: 'pointer' }}>Warnings</button>
            <button type="button" style={{ border: 0, background: 'transparent', color: 'var(--text-muted)', fontSize: 13, padding: '4px 8px', cursor: 'pointer' }}>Errors</button>
            <button type="button" style={{ border: 0, background: 'transparent', color: 'var(--text-muted)', fontSize: 13, padding: '4px 8px', cursor: 'pointer' }}>Migrations</button>
            <button type="button" style={{ border: 0, background: 'transparent', color: 'var(--text-muted)', fontSize: 13, padding: '4px 8px', cursor: 'pointer' }}>Conflicts</button>
          </div>
          <div style={{ marginTop: 14, border: '1px solid var(--border-subtle)', borderRadius: 6, overflow: 'hidden' }}>
            <header style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '10px 14px', background: '#f7f9fa', borderBottom: '1px solid var(--border-subtle)' }}>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, fontSize: 13, fontWeight: 600 }}>
                <Glyph name="pencil" size={12} /> {action.display_name || action.name}
              </span>
              <span className="of-chip" style={{ background: '#dcfce7', color: '#15803d', fontSize: 11 }}>Modified</span>
            </header>
            <div style={{ padding: 14, display: 'grid', gap: 10, fontSize: 13 }}>
              <p className="of-eyebrow">PARAMETERS</p>
              {removed.map((mapping) => (
                <div key={`rm-${mapping.property_name}`} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <Glyph name="trash" size={12} tone="var(--status-danger)" />
                  Removed parameter <strong>{mapping.property_name}</strong>
                </div>
              ))}
              {added.map((mapping) => (
                <div key={`add-${mapping.property_name}`} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <Glyph name="plus" size={12} tone="#15803d" />
                  Added parameter <strong>{mapping.property_name}</strong>
                </div>
              ))}
              {removed.length === 0 && added.length === 0 ? (
                <p className="of-text-muted" style={{ fontSize: 12, margin: 0 }}>Field ordering rearranged.</p>
              ) : null}
            </div>
          </div>
          {objectType ? (
            <p className="of-text-muted" style={{ marginTop: 12, fontSize: 11 }}>Object type: {objectType.display_name || objectType.name}</p>
          ) : null}
        </div>
        <footer style={{ padding: 14, borderTop: '1px solid var(--border-subtle)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <button type="button" className="of-button" onClick={onCancel} disabled={saving}>Discard</button>
          <button type="button" onClick={onSave} disabled={saving} style={{ padding: '8px 16px', border: 0, borderRadius: 4, background: '#15803d', color: '#fff', fontSize: 13, fontWeight: 600, cursor: saving ? 'not-allowed' : 'pointer' }}>
            {saving ? 'Saving…' : 'Save to ontology'}
          </button>
        </footer>
      </section>
    </div>
  );
}
