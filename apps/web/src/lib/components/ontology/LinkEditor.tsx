import { useEffect, useMemo, useState } from 'react';

import {
  createLinkType,
  deleteLinkType,
  linkTypeCardinalityLabel,
  linkTypeHasDatasourceMapping,
  listLinkTypes,
  listObjectTypes,
  updateLinkType,
  type LinkType,
  type LinkTypeCardinality,
  type ObjectType,
} from '@/lib/api/ontology';

type Cardinality = LinkTypeCardinality;

interface LinkEditorProps {
  linkId?: string | null;
  defaultSourceTypeId?: string;
  defaultTargetTypeId?: string;
  onCreated?: (link: LinkType) => void;
  onUpdated?: (link: LinkType) => void;
  onDeleted?: (id: string) => void;
}

const CARDINALITIES: Cardinality[] = [
  'one_to_one',
  'one_to_many',
  'many_to_one',
  'many_to_many',
];

const VISIBILITIES = ['normal', 'hidden', 'experimental'];

function emptyDraft(defaultSourceTypeId = '', defaultTargetTypeId = '') {
  return {
    name: '',
    display_name: '',
    description: '',
    source_type_id: defaultSourceTypeId,
    target_type_id: defaultTargetTypeId,
    cardinality: 'many_to_many' as Cardinality,
    label: '',
    reverse_label: '',
    visibility: 'normal',
    datasource_id: '',
    source_key: '',
    target_key: '',
  };
}

export function LinkEditor({ linkId = null, defaultSourceTypeId = '', defaultTargetTypeId = '', onCreated, onUpdated, onDeleted }: LinkEditorProps) {
  const [objectTypes, setObjectTypes] = useState<ObjectType[]>([]);
  const [editing, setEditing] = useState<LinkType | null>(null);
  const [draft, setDraft] = useState(emptyDraft(defaultSourceTypeId, defaultTargetTypeId));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const typeById = useMemo(() => new Map(objectTypes.map((type) => [type.id, type])), [objectTypes]);
  const isManyToMany = draft.cardinality === 'many_to_many';

  useEffect(() => {
    listObjectTypes({ per_page: 200 }).then((r) => setObjectTypes(r.data)).catch(() => {});
  }, []);

  useEffect(() => {
    if (!linkId) {
      setEditing(null);
      setDraft(emptyDraft(defaultSourceTypeId, defaultTargetTypeId));
      return;
    }
    listLinkTypes({ per_page: 200 }).then((r) => {
      const found = r.data.find((l) => l.id === linkId) ?? null;
      setEditing(found);
      if (found) {
        setDraft({
          name: found.name,
          display_name: found.display_name,
          description: found.description,
          source_type_id: found.source_type_id,
          target_type_id: found.target_type_id,
          cardinality: found.cardinality as Cardinality,
          label: found.label || '',
          reverse_label: found.reverse_label || '',
          visibility: found.visibility || 'normal',
          datasource_id: found.link_datasource_mapping?.datasource_id || '',
          source_key: found.link_datasource_mapping?.source_key || '',
          target_key: found.link_datasource_mapping?.target_key || '',
        });
      }
    }).catch(() => {});
  }, [linkId, defaultSourceTypeId, defaultTargetTypeId]);

  function validate() {
    if (!draft.name.trim() && !editing) return 'API name is required.';
    if (!draft.source_type_id || !draft.target_type_id) return 'Pick source and target object types.';
    if (isManyToMany && (!draft.datasource_id.trim() || !draft.source_key.trim() || !draft.target_key.trim())) {
      return 'Many-to-many link types require a link datasource plus source and target key mappings.';
    }
    return '';
  }

  async function save() {
    const validationError = validate();
    if (validationError) {
      setError(validationError);
      return;
    }
    setSaving(true);
    setError('');
    const link_datasource_mapping = isManyToMany
      ? {
          datasource_id: draft.datasource_id.trim(),
          source_key: draft.source_key.trim(),
          target_key: draft.target_key.trim(),
        }
      : null;
    try {
      if (editing) {
        const updated = await updateLinkType(editing.id, {
          display_name: draft.display_name,
          description: draft.description,
          cardinality: draft.cardinality,
          label: draft.label,
          reverse_label: draft.reverse_label,
          visibility: draft.visibility,
          link_datasource_mapping,
        });
        onUpdated?.(updated);
      } else {
        const created = await createLinkType({
          name: draft.name,
          display_name: draft.display_name,
          description: draft.description,
          source_type_id: draft.source_type_id,
          target_type_id: draft.target_type_id,
          cardinality: draft.cardinality,
          label: draft.label,
          reverse_label: draft.reverse_label,
          visibility: draft.visibility,
          link_datasource_mapping,
        });
        onCreated?.(created);
        setDraft(emptyDraft(defaultSourceTypeId, defaultTargetTypeId));
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSaving(false);
    }
  }

  async function remove() {
    if (!editing) return;
    if (typeof window !== 'undefined' && !window.confirm(`Delete link "${editing.name}"?`)) return;
    setSaving(true);
    try {
      await deleteLinkType(editing.id);
      onDeleted?.(editing.id);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSaving(false);
    }
  }

  return (
    <article style={{ display: 'flex', flexDirection: 'column', gap: 10, padding: 12, background: '#0f172a', border: '1px solid #1e293b', borderRadius: 6, color: '#e2e8f0' }}>
      <div>
        <h3 style={{ margin: 0, fontSize: 14 }}>{editing ? 'Edit link type' : 'New link type'}</h3>
        <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 11 }}>
          Create typed relationships, including self-links and one/many cardinality combinations.
        </p>
      </div>
      <label style={{ fontSize: 12 }}>
        API name
        <input value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} disabled={!!editing} className="of-input" style={{ marginTop: 4, fontFamily: 'var(--font-mono)' }} placeholder="TrailRunsThroughPark" />
      </label>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
        <label style={{ fontSize: 12 }}>
          Display name
          <input value={draft.display_name} onChange={(e) => setDraft({ ...draft, display_name: e.target.value })} className="of-input" style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Visibility
          <select value={draft.visibility} onChange={(e) => setDraft({ ...draft, visibility: e.target.value })} className="of-input" style={{ marginTop: 4 }}>
            {VISIBILITIES.map((visibility) => <option key={visibility} value={visibility}>{visibility}</option>)}
          </select>
        </label>
      </div>
      <label style={{ fontSize: 12 }}>
        Description
        <textarea value={draft.description} onChange={(e) => setDraft({ ...draft, description: e.target.value })} rows={2} className="of-input" style={{ marginTop: 4 }} />
      </label>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
        <label style={{ fontSize: 12 }}>
          Source type
          <select value={draft.source_type_id} onChange={(e) => setDraft({ ...draft, source_type_id: e.target.value })} disabled={!!editing} className="of-input" style={{ marginTop: 4 }}>
            <option value="">— pick —</option>
            {objectTypes.map((t) => <option key={t.id} value={t.id}>{t.display_name} ({t.name})</option>)}
          </select>
        </label>
        <label style={{ fontSize: 12 }}>
          Target type
          <select value={draft.target_type_id} onChange={(e) => setDraft({ ...draft, target_type_id: e.target.value })} disabled={!!editing} className="of-input" style={{ marginTop: 4 }}>
            <option value="">— pick —</option>
            {objectTypes.map((t) => <option key={t.id} value={t.id}>{t.display_name} ({t.name})</option>)}
          </select>
        </label>
      </div>
      {draft.source_type_id && draft.source_type_id === draft.target_type_id && (
        <p className="of-text-muted" style={{ margin: 0, fontSize: 11 }}>This is a self-link on {typeById.get(draft.source_type_id)?.display_name || draft.source_type_id}.</p>
      )}
      <label style={{ fontSize: 12 }}>
        Cardinality
        <select value={draft.cardinality} onChange={(e) => setDraft({ ...draft, cardinality: e.target.value as Cardinality })} className="of-input" style={{ marginTop: 4 }}>
          {CARDINALITIES.map((cardinality) => <option key={cardinality} value={cardinality}>{linkTypeCardinalityLabel(cardinality)}</option>)}
        </select>
      </label>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
        <label style={{ fontSize: 12 }}>
          Forward label
          <input value={draft.label} onChange={(e) => setDraft({ ...draft, label: e.target.value })} className="of-input" style={{ marginTop: 4 }} placeholder="runs through" />
        </label>
        <label style={{ fontSize: 12 }}>
          Reverse label
          <input value={draft.reverse_label} onChange={(e) => setDraft({ ...draft, reverse_label: e.target.value })} className="of-input" style={{ marginTop: 4 }} placeholder="contains trails" />
        </label>
      </div>
      {isManyToMany && (
        <fieldset style={{ border: '1px solid #334155', borderRadius: 6, padding: 10 }}>
          <legend style={{ padding: '0 4px', fontSize: 12 }}>Link datasource mapping</legend>
          <p className="of-text-muted" style={{ margin: '0 0 8px', fontSize: 11 }}>
            Many-to-many links are backed by a link datasource with columns that map to source and target object keys.
          </p>
          <div style={{ display: 'grid', gridTemplateColumns: '1.4fr 1fr 1fr', gap: 8 }}>
            <label style={{ fontSize: 12 }}>
              Datasource RID or ID
              <input value={draft.datasource_id} onChange={(e) => setDraft({ ...draft, datasource_id: e.target.value })} className="of-input" style={{ marginTop: 4 }} />
            </label>
            <label style={{ fontSize: 12 }}>
              Source key
              <input value={draft.source_key} onChange={(e) => setDraft({ ...draft, source_key: e.target.value })} className="of-input" style={{ marginTop: 4 }} />
            </label>
            <label style={{ fontSize: 12 }}>
              Target key
              <input value={draft.target_key} onChange={(e) => setDraft({ ...draft, target_key: e.target.value })} className="of-input" style={{ marginTop: 4 }} />
            </label>
          </div>
        </fieldset>
      )}
      {editing && !linkTypeHasDatasourceMapping(editing) && (
        <p style={{ color: '#fbbf24', fontSize: 11, margin: 0 }}>This many-to-many link is missing datasource key mapping.</p>
      )}
      {error && <p style={{ color: '#fca5a5', fontSize: 11, margin: 0 }}>{error}</p>}
      <div style={{ display: 'flex', gap: 6 }}>
        <button type="button" onClick={() => void save()} disabled={saving} className="of-button of-button--primary" style={{ fontSize: 11 }}>
          {saving ? 'Saving…' : editing ? 'Update' : 'Create'}
        </button>
        {editing && (
          <button type="button" onClick={() => void remove()} disabled={saving} className="of-button" style={{ fontSize: 11, color: '#fca5a5', borderColor: '#7f1d1d' }}>Delete</button>
        )}
      </div>
    </article>
  );
}
