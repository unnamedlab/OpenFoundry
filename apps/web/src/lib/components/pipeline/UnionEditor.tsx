import { useEffect, useState } from 'react';

import { Glyph } from '@/lib/components/ui/Glyph';
import { UNION_TYPES, type UnionDraft, type UnionType } from './unionDraft';

interface UnionEditorProps {
  open: boolean;
  draft: UnionDraft | null;
  unionedSchema?: string[];
  preview?: Array<Record<string, unknown>>;
  onClose: () => void;
  onApply: (next: UnionDraft) => void;
}

export function UnionEditor({ open, draft, unionedSchema = [], preview = [], onClose, onApply }: UnionEditorProps) {
  const [working, setWorking] = useState<UnionDraft | null>(null);
  const [created, setCreated] = useState(false);

  useEffect(() => {
    if (!open) return;
    setWorking(draft ? { ...draft } : null);
    setCreated(false);
  }, [open, draft]);

  if (!open || !working) return null;

  function patch<K extends keyof UnionDraft>(key: K, value: UnionDraft[K]) {
    setWorking((current) => (current ? { ...current, [key]: value } : current));
  }

  function removeInput(id: string) {
    setWorking((current) => {
      if (!current) return current;
      const index = current.input_node_ids.indexOf(id);
      if (index === -1) return current;
      const ids = current.input_node_ids.filter((entry) => entry !== id);
      const labels = current.input_node_labels.filter((_, i) => i !== index);
      return { ...current, input_node_ids: ids, input_node_labels: labels };
    });
  }

  function commit() {
    if (working) onApply(working);
    onClose();
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="union-editor-title"
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 95,
        background: '#f4f6f9',
        display: 'grid',
        gridTemplateRows: 'auto 1fr',
      }}
    >
      <header
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 14,
          padding: '10px 18px',
          borderBottom: '1px solid var(--border-subtle)',
          background: '#fff',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0, flex: 1 }}>
          <span
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              width: 28,
              height: 28,
              borderRadius: 4,
              background: 'rgba(180, 35, 24, 0.12)',
              color: '#b42318',
            }}
          >
            <UnionGlyph />
          </span>
          <input
            id="union-editor-title"
            value={working.display_name}
            onChange={(event) => patch('display_name', event.target.value)}
            placeholder="Union name"
            style={{
              border: 0,
              outline: 'none',
              fontSize: 15,
              fontWeight: 600,
              color: 'var(--text-strong)',
              background: 'transparent',
              flex: 1,
              minWidth: 0,
            }}
          />
          <button
            type="button"
            onClick={() => {
              const next = window.prompt('Union description', working.description ?? '');
              if (next !== null) patch('description', next);
            }}
            className="of-button"
            style={{ fontSize: 12 }}
          >
            <Glyph name="pencil" size={12} /> Description
          </button>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <button
            type="button"
            onClick={commit}
            disabled={working.input_node_ids.length === 0}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 6,
              padding: '8px 14px',
              border: 0,
              borderRadius: 4,
              background: '#2d72d2',
              color: '#fff',
              fontSize: 13,
              fontWeight: 600,
              cursor: working.input_node_ids.length === 0 ? 'not-allowed' : 'pointer',
              opacity: working.input_node_ids.length === 0 ? 0.6 : 1,
            }}
          >
            <Glyph name="check" size={13} tone="#fff" />
            Apply
          </button>
          <button type="button" className="of-button" onClick={onClose}>
            <Glyph name="x" size={12} />
            Close
          </button>
        </div>
      </header>

      <div style={{ display: 'grid', gridTemplateColumns: '320px minmax(0, 1fr)', minHeight: 0 }}>
        <aside style={{ borderRight: '1px solid var(--border-subtle)', padding: 16, display: 'grid', gap: 18, alignContent: 'start', overflowY: 'auto' }}>
          <section>
            <p style={{ margin: '0 0 8px', fontSize: 13, fontWeight: 600 }}>Inputs</p>
            <ul style={{ margin: 0, padding: 0, listStyle: 'none', display: 'grid', gap: 4 }}>
              {working.input_node_ids.map((id, index) => (
                <li
                  key={id}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 8,
                    padding: '6px 8px',
                    background: '#fff',
                    border: '1px solid var(--border-subtle)',
                    borderRadius: 4,
                    fontSize: 13,
                  }}
                >
                  <Glyph name="move" size={13} tone="#aab4c0" />
                  <Glyph name="object" size={13} tone="#2d72d2" />
                  <span style={{ flex: 1 }}>{working.input_node_labels[index] ?? id}</span>
                  <button
                    type="button"
                    aria-label="Remove input"
                    onClick={() => removeInput(id)}
                    style={{ border: 0, background: 'transparent', cursor: 'pointer', color: 'var(--text-muted)', padding: 2 }}
                  >
                    <Glyph name="x" size={12} />
                  </button>
                </li>
              ))}
            </ul>
          </section>

          <section style={{ borderTop: '1px solid var(--border-subtle)', paddingTop: 14 }}>
            <p style={{ margin: '0 0 8px', fontSize: 13, fontWeight: 600 }}>Output</p>
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 8,
                padding: '6px 8px',
                background: '#fff',
                border: '1px solid var(--status-info)',
                borderRadius: 4,
                fontSize: 13,
                color: 'var(--status-info)',
              }}
            >
              <UnionGlyph />
              <span>{working.display_name || 'Union'}</span>
            </div>
          </section>

          <section style={{ borderTop: '1px solid var(--border-subtle)', paddingTop: 14 }}>
            <p style={{ margin: '0 0 6px', fontSize: 13, fontWeight: 600 }}>Union type</p>
            <select
              value={working.union_type}
              onChange={(event) => patch('union_type', event.target.value as UnionType)}
              style={{
                padding: '6px 10px',
                border: '1px solid var(--border-default)',
                borderRadius: 4,
                fontSize: 13,
                background: '#fff',
                width: '100%',
              }}
            >
              {UNION_TYPES.map((entry) => (
                <option key={entry.id} value={entry.id}>{entry.label}</option>
              ))}
            </select>
            <p className="of-text-muted" style={{ margin: '6px 0 0', fontSize: 12 }}>
              {UNION_TYPES.find((entry) => entry.id === working.union_type)?.description}
            </p>
          </section>
        </aside>

        <main style={{ overflow: 'auto', padding: 24 }}>
          {!created || preview.length === 0 ? (
            <div style={{ display: 'grid', placeContent: 'center', height: '100%', textAlign: 'center', gap: 12 }}>
              <p className="of-text-muted" style={{ margin: 0 }}>Create union transform to preview the output</p>
              <button
                type="button"
                onClick={() => setCreated(true)}
                disabled={working.input_node_ids.length === 0}
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 6,
                  padding: '8px 14px',
                  border: 0,
                  borderRadius: 4,
                  background: '#2d72d2',
                  color: '#fff',
                  fontSize: 13,
                  fontWeight: 600,
                  cursor: working.input_node_ids.length === 0 ? 'not-allowed' : 'pointer',
                  justifySelf: 'center',
                  opacity: working.input_node_ids.length === 0 ? 0.6 : 1,
                }}
              >
                <Glyph name="check" size={13} tone="#fff" />
                Create union
              </button>
            </div>
          ) : (
            <UnionPreview schema={unionedSchema} rows={preview} />
          )}
        </main>
      </div>
    </div>
  );
}

function UnionPreview({ schema, rows }: { schema: string[]; rows: Array<Record<string, unknown>> }) {
  return (
    <div style={{ display: 'grid', gap: 8 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 14, fontSize: 12, color: 'var(--text-muted)' }}>
        <span>Previewing {rows.length} rows</span>
        <span>{schema.length} columns</span>
      </div>
      <div style={{ overflow: 'auto', border: '1px solid var(--border-subtle)', borderRadius: 4, background: '#fff' }}>
        <table className="of-table" style={{ minWidth: '100%', tableLayout: 'auto' }}>
          <thead>
            <tr>
              {schema.map((column) => (
                <th key={column} style={{ padding: '6px 10px', textAlign: 'left', borderBottom: '1px solid var(--border-subtle)' }}>
                  {column}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.slice(0, 100).map((row, index) => (
              <tr key={index}>
                {schema.map((column) => (
                  <td key={column} style={{ padding: '6px 10px', borderBottom: '1px solid var(--border-subtle)', fontSize: 12 }}>
                    {String(row[column] ?? '')}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function UnionGlyph() {
  return (
    <svg width={14} height={14} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <rect x="4" y="6" width="16" height="5" rx="1.5" stroke="currentColor" strokeWidth="1.6" />
      <rect x="4" y="13" width="16" height="5" rx="1.5" stroke="currentColor" strokeWidth="1.6" />
    </svg>
  );
}
