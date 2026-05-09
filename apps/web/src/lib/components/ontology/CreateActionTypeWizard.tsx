import { useEffect, useMemo, useState } from 'react';

import { useAuth } from '@/lib/stores/auth';
import {
  createActionType,
  listObjectTypes,
  listProperties,
  type ActionInputField,
  type ActionType,
  type ObjectType,
  type Property,
} from '@/lib/api/ontology';
import { Glyph } from '@/lib/components/ui/Glyph';

export type ActionWizardCategory = 'object' | 'link' | 'function' | 'webhook' | 'interface' | 'notification';
export type ObjectActionVariant = 'create' | 'modify' | 'modify_or_create' | 'delete';
export type MappingKind = 'parameter' | 'static' | 'property' | 'unique_id' | 'current_user';

interface Mapping {
  property_name: string;
  kind: MappingKind;
  static_value: string;
}

interface SubmissionCriteria {
  audience: 'organization' | 'group' | 'user';
  user_id: string;
  group_id: string;
  organization_id: string;
}

interface CreateActionTypeWizardProps {
  open: boolean;
  defaultObjectTypeId: string;
  onClose: () => void;
  onCreated: (action: ActionType) => void;
}

const STEPS: Array<{ id: number; label: string }> = [
  { id: 1, label: 'Action type' },
  { id: 2, label: 'Mapping' },
  { id: 3, label: 'Metadata' },
  { id: 4, label: 'Submission criteria' },
];

const CATEGORIES: Array<{ id: ActionWizardCategory; label: string }> = [
  { id: 'object', label: 'Object' },
  { id: 'link', label: 'Link' },
  { id: 'function', label: 'Function' },
  { id: 'webhook', label: 'Webhook' },
  { id: 'interface', label: 'Interface' },
  { id: 'notification', label: 'Notification' },
];

const OBJECT_ACTIONS: Array<{ id: ObjectActionVariant; label: string; description: string; glyph: 'cube' | 'pencil' | 'plus' | 'trash' }> = [
  { id: 'create', label: 'Create object', description: 'Configure an action type that adds a new object instance', glyph: 'plus' },
  { id: 'modify', label: 'Modify object(s)', description: 'Configure an action type that edits existing object instances.', glyph: 'pencil' },
  { id: 'modify_or_create', label: 'Modify or create object', description: 'Modify an existing instance, otherwise create a new instance.', glyph: 'cube' },
  { id: 'delete', label: 'Delete object(s)', description: 'Configure an action type that deletes object instances.', glyph: 'trash' },
];

const MAPPING_KIND_LABEL: Record<MappingKind, string> = {
  parameter: 'Parameter',
  static: 'Static value',
  property: 'Property',
  unique_id: 'Unique Identifier',
  current_user: 'Current User',
};

function slugify(name: string): string {
  return (
    name
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '_')
      .replace(/^_+|_+$/g, '') || `action_${Date.now().toString(36)}`
  );
}

function variantToOperationKind(variant: ObjectActionVariant): 'update_object' | 'delete_object' {
  return variant === 'delete' ? 'delete_object' : 'update_object';
}

export function CreateActionTypeWizard({ open, defaultObjectTypeId, onClose, onCreated }: CreateActionTypeWizardProps) {
  const { user } = useAuth();
  const [step, setStep] = useState(1);
  const [category, setCategory] = useState<ActionWizardCategory>('object');
  const [variant, setVariant] = useState<ObjectActionVariant>('modify');
  const [objectTypeId, setObjectTypeId] = useState(defaultObjectTypeId);
  const [objectTypes, setObjectTypes] = useState<ObjectType[]>([]);
  const [objectTypeDropdownOpen, setObjectTypeDropdownOpen] = useState(false);
  const [properties, setProperties] = useState<Property[]>([]);
  const [mappings, setMappings] = useState<Mapping[]>([]);
  const [addPropertyOpen, setAddPropertyOpen] = useState(false);
  const [propertySearch, setPropertySearch] = useState('');
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [submission, setSubmission] = useState<SubmissionCriteria>({ audience: 'user', user_id: '', group_id: '', organization_id: '' });
  const [openMappingMenu, setOpenMappingMenu] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!open) return;
    setStep(1);
    setCategory('object');
    setVariant('modify');
    setObjectTypeId(defaultObjectTypeId);
    setMappings([]);
    setName('');
    setDescription('');
    setError('');
    setSubmitting(false);
    setSubmission({ audience: 'user', user_id: user?.id ?? '', group_id: '', organization_id: user?.organization_id ?? '' });
    void listObjectTypes({ per_page: 200 }).then((response) => setObjectTypes(response.data)).catch(() => setObjectTypes([]));
  }, [open, defaultObjectTypeId, user]);

  useEffect(() => {
    if (!objectTypeId) {
      setProperties([]);
      return;
    }
    let cancelled = false;
    void listProperties(objectTypeId).then((response) => {
      if (!cancelled) setProperties(response);
    }).catch(() => {
      if (!cancelled) setProperties([]);
    });
    return () => {
      cancelled = true;
    };
  }, [objectTypeId]);

  const objectType = objectTypes.find((entry) => entry.id === objectTypeId) ?? null;
  const availableProperties = useMemo(
    () => properties.filter((p) => !mappings.some((m) => m.property_name === p.name) && `${p.display_name} ${p.name}`.toLowerCase().includes(propertySearch.toLowerCase())),
    [properties, mappings, propertySearch],
  );

  if (!open) return null;

  function addMapping(propertyName: string) {
    setMappings((current) => [...current, { property_name: propertyName, kind: 'parameter', static_value: '' }]);
    setAddPropertyOpen(false);
    setPropertySearch('');
  }

  function patchMapping(propertyName: string, patch: Partial<Mapping>) {
    setMappings((current) => current.map((m) => (m.property_name === propertyName ? { ...m, ...patch } : m)));
  }

  function removeMapping(propertyName: string) {
    setMappings((current) => current.filter((m) => m.property_name !== propertyName));
  }

  async function submit() {
    if (!objectTypeId) {
      setError('Select an object type.');
      return;
    }
    if (!name.trim()) {
      setError('Action type name is required.');
      return;
    }
    setSubmitting(true);
    setError('');
    try {
      const inputSchema: ActionInputField[] = mappings
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
        });
      const config = {
        variant,
        category,
        property_mappings: mappings.map((m) => ({
          property_name: m.property_name,
          kind: m.kind,
          static_value: m.kind === 'static' ? m.static_value : undefined,
        })),
        submission,
      };
      const action = await createActionType({
        name: slugify(name),
        display_name: name.trim(),
        description: description.trim(),
        object_type_id: objectTypeId,
        operation_kind: variantToOperationKind(variant),
        input_schema: inputSchema,
        config,
      });
      onCreated(action);
      onClose();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Create failed');
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div role="dialog" aria-modal="true" aria-labelledby="create-action-wizard-title" onMouseDown={(event) => { if (event.target === event.currentTarget && !submitting) onClose(); }} style={{ position: 'fixed', inset: 0, zIndex: 95, background: 'rgba(17, 24, 39, 0.42)', display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 24 }}>
      <section style={{ width: '100%', maxWidth: 880, height: 'min(620px, 92vh)', background: '#fff', borderRadius: 6, boxShadow: '0 20px 48px rgba(15, 23, 42, 0.18)', display: 'grid', gridTemplateRows: 'auto 1fr auto', overflow: 'hidden' }}>
        <header style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '14px 20px', borderBottom: '1px solid var(--border-subtle)' }}>
          <h2 id="create-action-wizard-title" style={{ margin: 0, fontSize: 15, fontWeight: 600 }}>Create a new action type</h2>
          <button type="button" aria-label="Close" onClick={onClose} className="of-button of-button--ghost" style={{ padding: 4 }}>
            <Glyph name="x" size={14} />
          </button>
        </header>

        <div style={{ display: 'grid', gridTemplateColumns: '220px 1fr', minHeight: 0 }}>
          <aside style={{ borderRight: '1px solid var(--border-subtle)', padding: 16, background: '#f7f9fa' }}>
            <ol style={{ margin: 0, padding: 0, listStyle: 'none', display: 'grid', gap: 12 }}>
              {STEPS.map((entry) => {
                const active = step === entry.id;
                const done = step > entry.id;
                return (
                  <li key={entry.id} style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                    <span
                      style={{
                        display: 'inline-flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        width: 26,
                        height: 26,
                        borderRadius: '50%',
                        background: active ? '#2d72d2' : done ? '#2d72d2' : 'transparent',
                        border: active || done ? '0' : '1px solid #aab4c0',
                        color: active || done ? '#fff' : '#5c7080',
                        fontSize: 12,
                        fontWeight: 600,
                      }}
                    >
                      {entry.id}
                    </span>
                    <span style={{ fontSize: 13, fontWeight: active ? 600 : 500, color: active ? 'var(--status-info)' : 'var(--text-strong)' }}>{entry.label}</span>
                  </li>
                );
              })}
            </ol>
          </aside>

          <div style={{ padding: 24, overflowY: 'auto' }}>
            {step === 1 ? (
              <div style={{ display: 'grid', gap: 14 }}>
                <p className="of-eyebrow" style={{ margin: 0 }}>STEP 1</p>
                <h3 style={{ margin: 0, fontSize: 18, fontWeight: 600 }}>Select an action type you want to configure</h3>
                <p className="of-text-muted" style={{ margin: 0, fontSize: 13 }}>Enable users to make changes to the ontology by configuring actions they can execute.</p>

                <div style={{ display: 'flex', gap: 0, borderBottom: '1px solid var(--border-subtle)' }}>
                  {CATEGORIES.map((entry) => {
                    const active = category === entry.id;
                    return (
                      <button
                        key={entry.id}
                        type="button"
                        onClick={() => setCategory(entry.id)}
                        style={{ padding: '10px 16px', border: 0, background: 'transparent', cursor: 'pointer', fontSize: 13, fontWeight: active ? 600 : 500, color: active ? 'var(--status-info)' : 'var(--text-muted)', borderBottom: active ? '2px solid var(--status-info)' : '2px solid transparent' }}
                      >
                        {entry.label}
                      </button>
                    );
                  })}
                </div>

                {category === 'object' ? (
                  <>
                    <div>
                      <p style={{ margin: '0 0 6px', fontSize: 13, fontWeight: 600 }}>Object type</p>
                      <div style={{ position: 'relative' }}>
                        <button
                          type="button"
                          onClick={() => setObjectTypeDropdownOpen((open) => !open)}
                          style={{ width: '100%', display: 'flex', alignItems: 'center', gap: 8, padding: '8px 12px', border: '1px solid var(--border-default)', borderRadius: 4, background: '#fff', cursor: 'pointer', fontSize: 13, textAlign: 'left' }}
                        >
                          <Glyph name="cube" size={13} tone="#2d72d2" />
                          <span style={{ flex: 1 }}>{objectType ? `${objectType.display_name || objectType.name}` : 'Select an object type'}</span>
                          {objectType ? <span className="of-chip" style={{ fontSize: 11 }}>Experimental</span> : null}
                          {objectType ? (
                            <button type="button" aria-label="Clear" onClick={(event) => { event.stopPropagation(); setObjectTypeId(''); }} style={{ border: 0, background: 'transparent', cursor: 'pointer', color: 'var(--text-muted)' }}><Glyph name="x" size={11} /></button>
                          ) : null}
                          <Glyph name="chevron-down" size={11} />
                        </button>
                        {objectTypeDropdownOpen ? (
                          <div role="menu" style={{ position: 'absolute', top: 'calc(100% + 4px)', left: 0, right: 0, background: '#fff', border: '1px solid var(--border-default)', borderRadius: 4, boxShadow: '0 8px 24px rgba(15, 23, 42, 0.12)', padding: 6, zIndex: 5, maxHeight: 220, overflowY: 'auto' }}>
                            {objectTypes.map((type) => (
                              <button
                                key={type.id}
                                type="button"
                                onClick={() => { setObjectTypeId(type.id); setObjectTypeDropdownOpen(false); }}
                                style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%', padding: '6px 10px', border: 0, background: objectTypeId === type.id ? 'rgba(45, 114, 210, 0.06)' : 'transparent', cursor: 'pointer', textAlign: 'left', fontSize: 13, borderRadius: 4 }}
                              >
                                <Glyph name="cube" size={13} tone="#2d72d2" />
                                {type.display_name || type.name}
                              </button>
                            ))}
                          </div>
                        ) : null}
                      </div>
                    </div>

                    <div>
                      <p style={{ margin: '0 0 6px', fontSize: 13, fontWeight: 600 }}>Object actions</p>
                      <div style={{ display: 'grid', gap: 0, border: '1px solid var(--border-subtle)', borderRadius: 4 }}>
                        {OBJECT_ACTIONS.map((action) => {
                          const active = variant === action.id;
                          return (
                            <button
                              key={action.id}
                              type="button"
                              onClick={() => setVariant(action.id)}
                              style={{ display: 'flex', alignItems: 'flex-start', gap: 10, padding: '12px 14px', border: 0, borderTop: action.id === 'create' ? 0 : '1px solid var(--border-subtle)', background: active ? '#f4f6f9' : '#fff', cursor: 'pointer', textAlign: 'left' }}
                            >
                              <span style={{ marginTop: 2, display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 16, height: 16, borderRadius: '50%', border: active ? '5px solid #2d72d2' : '1.5px solid #aab4c0' }} />
                              <Glyph name={action.glyph} size={14} tone="#5c7080" />
                              <div style={{ flex: 1 }}>
                                <div style={{ fontSize: 13, fontWeight: 600 }}>{action.label}</div>
                                <div className="of-text-muted" style={{ fontSize: 12 }}>{action.description}</div>
                              </div>
                            </button>
                          );
                        })}
                      </div>
                    </div>
                  </>
                ) : (
                  <div style={{ padding: 24, border: '1px dashed var(--border-default)', borderRadius: 6, textAlign: 'center', color: 'var(--text-muted)', fontSize: 13 }}>
                    {category[0].toUpperCase() + category.slice(1)} actions coming soon.
                  </div>
                )}
              </div>
            ) : null}

            {step === 2 ? (
              <div style={{ display: 'grid', gap: 14 }}>
                <p className="of-eyebrow" style={{ margin: 0 }}>STEP 2</p>
                <h3 style={{ margin: 0, fontSize: 18, fontWeight: 600 }}>Map action parameters</h3>
                <p className="of-text-muted" style={{ margin: 0, fontSize: 13 }}>Map the action parameters that will be used as inputs to this action</p>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '8px 10px', border: '1px solid var(--border-subtle)', borderRadius: 4, background: '#f4f6f9' }}>
                  <Glyph name="cube" size={13} tone="#2d72d2" />
                  <span style={{ fontSize: 13, fontWeight: 600 }}>{objectType?.display_name || objectType?.name || 'Object'}</span>
                </div>

                <div style={{ display: 'grid', gridTemplateColumns: '1fr auto 1fr', gap: 10, alignItems: 'center', fontSize: 11, textTransform: 'uppercase', color: 'var(--text-muted)', letterSpacing: '0.04em' }}>
                  <span>Property</span>
                  <span aria-hidden="true" />
                  <span>Map to</span>
                </div>

                {mappings.map((mapping) => {
                  const property = properties.find((p) => p.name === mapping.property_name);
                  const menuOpen = openMappingMenu === mapping.property_name;
                  return (
                    <div key={mapping.property_name} style={{ display: 'grid', gridTemplateColumns: '1fr 24px 1fr auto', gap: 10, alignItems: 'center' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '6px 10px', border: '1px solid var(--border-subtle)', borderRadius: 4, background: '#fff' }}>
                        <Glyph name="tag" size={11} tone="#5c7080" />
                        <span style={{ fontSize: 13 }}>{property?.display_name || mapping.property_name}</span>
                      </div>
                      <Glyph name="chevron-right" size={11} tone="#5c7080" />
                      <div style={{ position: 'relative' }}>
                        <button
                          type="button"
                          onClick={() => setOpenMappingMenu(menuOpen ? null : mapping.property_name)}
                          style={{ width: '100%', display: 'flex', alignItems: 'center', gap: 8, padding: '6px 10px', border: '1px solid var(--border-default)', borderRadius: 4, background: '#fff', cursor: 'pointer', fontSize: 13, textAlign: 'left' }}
                        >
                          {mapping.kind === 'static' ? (
                            <>
                              <Glyph name="tag" size={11} tone="#5c7080" />
                              <span style={{ flex: 1 }}>"{mapping.static_value}"</span>
                            </>
                          ) : mapping.kind === 'parameter' ? (
                            <>
                              <Glyph name="tag" size={11} tone="#5c7080" />
                              <span style={{ flex: 1 }}>{property?.display_name || mapping.property_name}</span>
                            </>
                          ) : (
                            <>
                              <Glyph name="tag" size={11} tone="#5c7080" />
                              <span style={{ flex: 1 }}>{MAPPING_KIND_LABEL[mapping.kind]}</span>
                            </>
                          )}
                          <Glyph name="chevron-down" size={11} />
                        </button>
                        {menuOpen ? (
                          <MappingMenu
                            value={mapping}
                            onPick={(kind, staticValue) => {
                              patchMapping(mapping.property_name, { kind, static_value: staticValue ?? mapping.static_value });
                              setOpenMappingMenu(null);
                            }}
                            onClose={() => setOpenMappingMenu(null)}
                          />
                        ) : null}
                      </div>
                      <button type="button" aria-label="Remove" onClick={() => removeMapping(mapping.property_name)} style={{ border: 0, background: 'transparent', cursor: 'pointer', color: 'var(--status-danger)', padding: 4 }}>
                        <Glyph name="x" size={11} />
                      </button>
                    </div>
                  );
                })}

                <div style={{ position: 'relative' }}>
                  <button
                    type="button"
                    onClick={() => setAddPropertyOpen((open) => !open)}
                    style={{ display: 'inline-flex', alignItems: 'center', gap: 6, padding: '8px 14px', border: 0, borderRadius: 4, background: '#2d72d2', color: '#fff', cursor: 'pointer', fontSize: 13, fontWeight: 600 }}
                  >
                    <Glyph name="plus" size={11} /> Add property <Glyph name="chevron-down" size={11} />
                  </button>
                  {addPropertyOpen ? (
                    <div role="menu" style={{ position: 'absolute', top: 'calc(100% + 4px)', left: 0, width: 320, background: '#fff', border: '1px solid var(--border-default)', borderRadius: 4, boxShadow: '0 8px 24px rgba(15, 23, 42, 0.12)', padding: 6, zIndex: 5, maxHeight: 280, overflowY: 'auto' }}>
                      <input
                        autoFocus
                        value={propertySearch}
                        onChange={(event) => setPropertySearch(event.target.value)}
                        placeholder="Search…"
                        style={{ width: '100%', padding: '6px 10px', border: '1px solid var(--border-default)', borderRadius: 4, fontSize: 13, marginBottom: 6 }}
                      />
                      {availableProperties.length === 0 ? (
                        <p className="of-text-muted" style={{ padding: 8, fontSize: 12, margin: 0 }}>No more properties.</p>
                      ) : availableProperties.map((property) => (
                        <button
                          key={property.id}
                          type="button"
                          onClick={() => addMapping(property.name)}
                          style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%', padding: '6px 10px', border: 0, background: 'transparent', cursor: 'pointer', textAlign: 'left', fontSize: 13, borderRadius: 4 }}
                        >
                          <Glyph name="tag" size={11} tone="#5c7080" />
                          <span style={{ flex: 1 }}>{property.display_name || property.name}</span>
                        </button>
                      ))}
                      {availableProperties.length > 0 ? (
                        <div style={{ borderTop: '1px solid var(--border-subtle)', marginTop: 4, paddingTop: 4 }}>
                          <button
                            type="button"
                            onClick={() => {
                              const remaining = properties.filter((p) => !mappings.some((m) => m.property_name === p.name));
                              setMappings((current) => [
                                ...current,
                                ...remaining.map<Mapping>((p) => ({ property_name: p.name, kind: 'parameter', static_value: '' })),
                              ]);
                              setAddPropertyOpen(false);
                              setPropertySearch('');
                            }}
                            style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%', padding: '6px 10px', border: 0, background: 'transparent', cursor: 'pointer', textAlign: 'left', fontSize: 13, color: 'var(--status-info)', borderRadius: 4 }}
                          >
                            Add all above properties
                          </button>
                        </div>
                      ) : null}
                    </div>
                  ) : null}
                </div>
              </div>
            ) : null}

            {step === 3 ? (
              <div style={{ display: 'grid', gap: 14 }}>
                <p className="of-eyebrow" style={{ margin: 0 }}>STEP 3</p>
                <h3 style={{ margin: 0, fontSize: 18, fontWeight: 600 }}>Configure action type metadata</h3>
                <p className="of-text-muted" style={{ margin: 0, fontSize: 13 }}>Use a familiar name or helpful description to enhance this action type's discoverability.</p>

                <label style={{ display: 'grid', gap: 4 }}>
                  <span style={{ fontSize: 12, fontWeight: 600 }}>Action type name</span>
                  <span style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '6px 10px', border: '1.5px solid var(--status-info)', borderRadius: 4 }}>
                    <input
                      autoFocus
                      value={name}
                      onChange={(event) => setName(event.target.value)}
                      placeholder="Action type name"
                      style={{ flex: 1, border: 0, outline: 'none', fontSize: 13 }}
                    />
                    {name ? (
                      <button type="button" aria-label="Clear" onClick={() => setName('')} style={{ border: 0, background: 'transparent', cursor: 'pointer', color: 'var(--text-muted)' }}><Glyph name="x" size={11} /></button>
                    ) : null}
                  </span>
                </label>

                <label style={{ display: 'grid', gap: 4 }}>
                  <span style={{ fontSize: 12, fontWeight: 600 }}>Description <span className="of-text-muted">• optional</span></span>
                  <input
                    value={description}
                    onChange={(event) => setDescription(event.target.value)}
                    placeholder="Enter description…"
                    style={{ padding: '6px 10px', border: '1px solid var(--border-default)', borderRadius: 4, fontSize: 13 }}
                  />
                </label>

                <div style={{ display: 'grid', gap: 4 }}>
                  <span style={{ fontSize: 12, fontWeight: 600 }}>Icon <span className="of-text-muted">• Autogenerated, cannot be modified</span></span>
                  <span style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 36, height: 36, border: '1px solid var(--border-default)', borderRadius: 4, background: '#f4f6f9' }}>
                    <Glyph name="pencil" size={14} tone="#5c7080" />
                  </span>
                </div>
              </div>
            ) : null}

            {step === 4 ? (
              <div style={{ display: 'grid', gap: 14 }}>
                <p className="of-eyebrow" style={{ margin: 0 }}>STEP 4</p>
                <h3 style={{ margin: 0, fontSize: 18, fontWeight: 600 }}>Configure who can execute this action type</h3>
                <p style={{ margin: 0, fontSize: 13 }}>Choose who can submit this action type</p>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 10 }}>
                  {([
                    { id: 'organization' as const, label: 'Organization', icon: 'view-grid' as const },
                    { id: 'group' as const, label: 'Group', icon: 'object' as const },
                    { id: 'user' as const, label: 'User', icon: 'add-user' as const },
                  ]).map((entry) => {
                    const active = submission.audience === entry.id;
                    return (
                      <button
                        key={entry.id}
                        type="button"
                        onClick={() => setSubmission((current) => ({ ...current, audience: entry.id }))}
                        style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '14px 12px', border: active ? '2px solid var(--status-info)' : '1px solid var(--border-default)', borderRadius: 6, background: active ? 'rgba(45, 114, 210, 0.04)' : '#fff', cursor: 'pointer', textAlign: 'left', fontSize: 13 }}
                      >
                        <span style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 18, height: 18, borderRadius: '50%', border: active ? '5px solid #2d72d2' : '1.5px solid #aab4c0' }} />
                        <Glyph name={entry.icon} size={14} tone="#5c7080" />
                        <span>{entry.label}</span>
                      </button>
                    );
                  })}
                </div>

                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '12px 16px', border: '1px solid var(--border-subtle)', borderRadius: 6 }}>
                  <span style={{ fontSize: 13 }}>
                    {submission.audience === 'user' ? 'Only this user can submit' : submission.audience === 'group' ? 'Only this group can submit' : 'Only this organization can submit'}
                  </span>
                  <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, padding: '6px 10px', border: '1px solid var(--border-default)', borderRadius: 4, background: '#fff' }}>
                    <span style={{ fontSize: 13, fontWeight: 500 }}>{submission.audience === 'user' ? user?.name || user?.email || 'You' : submission.audience === 'group' ? 'Default group' : 'Default org'}</span>
                    {submission.audience === 'user' ? <span className="of-chip" style={{ fontSize: 11, color: 'var(--status-info)' }}>You</span> : null}
                    <Glyph name="chevron-down" size={11} />
                  </span>
                </div>

                <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '8px 12px', border: '1px solid var(--border-subtle)', borderRadius: 6, fontSize: 12, color: 'var(--text-muted)' }}>
                  <Glyph name="info" size={12} tone="#5c7080" />
                  <span style={{ flex: 1 }}>Submitting actions</span>
                  <Glyph name="chevron-down" size={11} />
                </div>
              </div>
            ) : null}

            {error ? (
              <div role="alert" className="of-status-danger" style={{ marginTop: 14, padding: '8px 12px', borderRadius: 4, fontSize: 12 }}>
                {error}
              </div>
            ) : null}
          </div>
        </div>

        <footer style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: 12, borderTop: '1px solid var(--border-subtle)' }}>
          <button type="button" className="of-link" onClick={onClose} style={{ background: 'none', border: 0, color: 'var(--text-muted)', cursor: 'pointer', fontSize: 13 }}>Skip</button>
          <div style={{ display: 'flex', gap: 8 }}>
            {step > 1 ? (
              <button type="button" className="of-button" onClick={() => setStep((s) => Math.max(1, s - 1))} disabled={submitting}>
                Back
              </button>
            ) : null}
            {step < 4 ? (
              <button
                type="button"
                onClick={() => {
                  if (step === 1 && !objectTypeId) {
                    setError('Select an object type.');
                    return;
                  }
                  setError('');
                  setStep((s) => Math.min(4, s + 1));
                }}
                style={{ padding: '8px 14px', border: 0, borderRadius: 4, background: '#2d72d2', color: '#fff', fontSize: 13, fontWeight: 600, cursor: 'pointer' }}
              >
                Next
              </button>
            ) : (
              <button
                type="button"
                onClick={() => void submit()}
                disabled={submitting}
                style={{ padding: '8px 14px', border: 0, borderRadius: 4, background: '#2d72d2', color: '#fff', fontSize: 13, fontWeight: 600, cursor: submitting ? 'not-allowed' : 'pointer' }}
              >
                {submitting ? 'Creating…' : 'Create'}
              </button>
            )}
          </div>
        </footer>
      </section>
    </div>
  );
}

function MappingMenu({
  value,
  onPick,
  onClose,
}: {
  value: Mapping;
  onPick: (kind: MappingKind, staticValue?: string) => void;
  onClose: () => void;
}) {
  const [hovered, setHovered] = useState<MappingKind | null>(value.kind);
  const [staticDraft, setStaticDraft] = useState(value.static_value);

  return (
    <div style={{ position: 'absolute', top: 'calc(100% + 4px)', left: 0, zIndex: 6, display: 'grid', gridTemplateColumns: '220px 240px', background: '#fff', border: '1px solid var(--border-default)', borderRadius: 4, boxShadow: '0 8px 24px rgba(15, 23, 42, 0.12)' }}>
      <div style={{ padding: 6, borderRight: '1px solid var(--border-subtle)' }}>
        <p className="of-text-muted" style={{ margin: '4px 6px', fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.05em' }}>Select mapping type</p>
        {(['parameter', 'static', 'property', 'unique_id', 'current_user'] as MappingKind[]).map((kind) => {
          const active = hovered === kind;
          return (
            <button
              key={kind}
              type="button"
              onMouseEnter={() => setHovered(kind)}
              onClick={() => {
                if (kind === 'static') {
                  onPick('static', staticDraft);
                } else {
                  onPick(kind);
                }
              }}
              style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%', padding: '6px 10px', border: 0, background: active ? 'rgba(45, 114, 210, 0.06)' : 'transparent', cursor: 'pointer', textAlign: 'left', fontSize: 13, borderRadius: 4 }}
            >
              {kind === 'parameter' ? <Glyph name="check" size={11} tone="#15803d" /> : kind === 'static' ? <Glyph name="tag" size={11} tone="#5c7080" /> : kind === 'unique_id' ? <Glyph name="search" size={11} tone="#5c7080" /> : kind === 'current_user' ? <Glyph name="add-user" size={11} tone="#5c7080" /> : <Glyph name="cube" size={11} tone="#5c7080" />}
              <span style={{ flex: 1 }}>{MAPPING_KIND_LABEL[kind]}</span>
              {kind === 'static' ? <Glyph name="chevron-right" size={11} tone="#5c7080" /> : null}
            </button>
          );
        })}
      </div>
      <div style={{ padding: 8 }}>
        {hovered === 'static' ? (
          <div style={{ display: 'grid', gap: 4 }}>
            <span className="of-text-muted" style={{ fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.05em' }}>Enter a static value</span>
            <input
              autoFocus
              value={staticDraft}
              onChange={(event) => setStaticDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') onPick('static', staticDraft);
                if (event.key === 'Escape') onClose();
              }}
              style={{ padding: '6px 10px', border: '1px solid var(--border-default)', borderRadius: 4, fontSize: 13 }}
            />
            <button type="button" className="of-button" style={{ marginTop: 4, justifyContent: 'center' }} onClick={() => onPick('static', staticDraft)}>
              Set value
            </button>
          </div>
        ) : (
          <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>Hover an option on the left to preview details.</p>
        )}
      </div>
    </div>
  );
}
