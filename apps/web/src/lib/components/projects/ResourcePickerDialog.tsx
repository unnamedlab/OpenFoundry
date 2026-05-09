import { useMemo, useState } from 'react';

import { Glyph, type GlyphName } from '@/lib/components/ui/Glyph';

export type ResourcePickerAction =
  | 'folder'
  | 'upload-files'
  | 'web-link'
  | 'bind-existing'
  | 'code-repository'
  | 'cipher-channel'
  | 'artifacts-repository'
  | 'dataset'
  | 'notebook'
  | 'dashboard'
  | 'application'
  | 'pipeline-builder';

type Category =
  | 'all'
  | 'analytics'
  | 'application-development'
  | 'data-integration'
  | 'developer-toolchain'
  | 'models'
  | 'ontology'
  | 'security';

interface ResourceEntry {
  id: ResourcePickerAction;
  name: string;
  description: string;
  icon: GlyphName;
  iconTone: string;
  category: Exclude<Category, 'all'>;
  enabled: boolean;
}

const CATEGORIES: { id: Category; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'analytics', label: 'Analytics & Operations' },
  { id: 'application-development', label: 'Application development' },
  { id: 'data-integration', label: 'Data integration' },
  { id: 'developer-toolchain', label: 'Developer toolchain' },
  { id: 'models', label: 'Models' },
  { id: 'ontology', label: 'Ontology' },
  { id: 'security', label: 'Security & governance' },
];

const RESOURCES: ResourceEntry[] = [
  { id: 'folder', name: 'Folder', description: '', icon: 'folder', iconTone: '#cf923f', category: 'data-integration', enabled: true },
  { id: 'web-link', name: 'Web link', description: 'Save a link to an external website.', icon: 'external-link', iconTone: '#5c7080', category: 'developer-toolchain', enabled: false },
  { id: 'upload-files', name: 'Upload files...', description: 'Upload files directly from your computer.', icon: 'database', iconTone: '#2d72d2', category: 'data-integration', enabled: true },
  { id: 'artifacts-repository', name: 'Artifacts repository', description: 'Publish and consume artifacts in OpenFoundry.', icon: 'artifact', iconTone: '#0891b2', category: 'developer-toolchain', enabled: false },
  { id: 'cipher-channel', name: 'Cipher channel', description: 'Obfuscate data through cryptographic operations.', icon: 'shield', iconTone: '#5c7080', category: 'security', enabled: false },
  { id: 'code-repository', name: 'Code repository', description: 'Write data transformation code in Python.', icon: 'code', iconTone: '#15803d', category: 'developer-toolchain', enabled: false },
  { id: 'dataset', name: 'Dataset', description: 'Bind an existing dataset to this folder.', icon: 'database', iconTone: '#2d72d2', category: 'data-integration', enabled: true },
  { id: 'notebook', name: 'Notebook', description: 'Author analysis in a Jupyter-style notebook.', icon: 'document', iconTone: '#9a5b00', category: 'analytics', enabled: false },
  { id: 'dashboard', name: 'Dashboard', description: 'Build interactive dashboards.', icon: 'graph', iconTone: '#0891b2', category: 'analytics', enabled: false },
  { id: 'application', name: 'Application', description: 'Build operational apps with Workshop.', icon: 'object', iconTone: '#7c5dd6', category: 'application-development', enabled: false },
  { id: 'pipeline-builder', name: 'Pipeline Builder', description: 'Create data pipelines using built-in transformations.', icon: 'run', iconTone: '#15803d', category: 'data-integration', enabled: true },
  { id: 'bind-existing', name: 'Bind existing resource', description: 'Bind a resource that already exists in the platform.', icon: 'link', iconTone: '#5c7080', category: 'data-integration', enabled: true },
];

interface ResourcePickerDialogProps {
  open: boolean;
  onClose: () => void;
  onPick: (action: ResourcePickerAction) => void;
}

export function ResourcePickerDialog({ open, onClose, onPick }: ResourcePickerDialogProps) {
  const [search, setSearch] = useState('');
  const [category, setCategory] = useState<Category>('all');

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return RESOURCES.filter((entry) => {
      if (category !== 'all' && entry.category !== category) return false;
      if (!q) return true;
      return entry.name.toLowerCase().includes(q) || entry.description.toLowerCase().includes(q);
    });
  }, [search, category]);

  if (!open) return null;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="resource-picker-title"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 90,
        background: 'rgba(17, 24, 39, 0.42)',
        display: 'flex',
        alignItems: 'flex-start',
        justifyContent: 'center',
        padding: '64px 24px 24px',
      }}
    >
      <section
        style={{
          width: '100%',
          maxWidth: 720,
          background: '#fff',
          borderRadius: 6,
          boxShadow: '0 12px 32px rgba(15, 23, 42, 0.16)',
          display: 'grid',
          gridTemplateRows: 'auto 1fr',
          maxHeight: 'calc(100vh - 96px)',
          overflow: 'hidden',
        }}
      >
        <header style={{ padding: '12px 14px', borderBottom: '1px solid var(--border-subtle)' }}>
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              padding: '8px 12px',
              border: '1px solid var(--border-default)',
              borderRadius: 4,
              background: '#f4f6f9',
            }}
          >
            <Glyph name="search" size={14} tone="#5c7080" />
            <input
              id="resource-picker-title"
              type="search"
              autoFocus
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Search for apps..."
              style={{
                flex: 1,
                background: 'transparent',
                border: 0,
                outline: 'none',
                fontSize: 13,
                color: 'var(--text-strong)',
              }}
            />
            <button
              type="button"
              onClick={onClose}
              aria-label="Close"
              style={{ border: 0, background: 'transparent', padding: 4, cursor: 'pointer', color: 'var(--text-muted)' }}
            >
              <Glyph name="x" size={14} />
            </button>
          </div>
        </header>

        <div style={{ display: 'grid', gridTemplateColumns: '220px minmax(0, 1fr)', minHeight: 0 }}>
          <aside style={{ borderRight: '1px solid var(--border-subtle)', padding: 8, overflowY: 'auto' }}>
            {CATEGORIES.map((cat) => {
              const active = category === cat.id;
              return (
                <button
                  key={cat.id}
                  type="button"
                  onClick={() => setCategory(cat.id)}
                  style={{
                    display: 'block',
                    width: '100%',
                    padding: '8px 10px',
                    border: 0,
                    background: active ? 'rgba(45, 114, 210, 0.08)' : 'transparent',
                    color: active ? 'var(--status-info)' : 'var(--text-strong)',
                    fontWeight: active ? 600 : 500,
                    fontSize: 13,
                    borderRadius: 4,
                    cursor: 'pointer',
                    textAlign: 'left',
                  }}
                >
                  {cat.label}
                </button>
              );
            })}
          </aside>
          <main style={{ padding: 8, overflowY: 'auto' }}>
            {filtered.length === 0 ? (
              <p className="of-text-muted" style={{ padding: 24, textAlign: 'center', margin: 0 }}>
                No resources match the current filters.
              </p>
            ) : (
              filtered.map((entry) => (
                <button
                  key={entry.id}
                  type="button"
                  disabled={!entry.enabled}
                  onClick={() => entry.enabled && onPick(entry.id)}
                  style={{
                    width: '100%',
                    display: 'flex',
                    alignItems: 'flex-start',
                    gap: 12,
                    padding: '12px 12px',
                    border: 0,
                    background: 'transparent',
                    borderRadius: 4,
                    cursor: entry.enabled ? 'pointer' : 'not-allowed',
                    opacity: entry.enabled ? 1 : 0.5,
                    textAlign: 'left',
                  }}
                  onMouseEnter={(e) => {
                    if (entry.enabled) e.currentTarget.style.background = 'rgba(45, 114, 210, 0.06)';
                  }}
                  onMouseLeave={(e) => (e.currentTarget.style.background = 'transparent')}
                >
                  <Glyph name={entry.icon} size={18} tone={entry.iconTone} />
                  <span style={{ display: 'grid', gap: 2, minWidth: 0 }}>
                    <span style={{ fontSize: 14, fontWeight: 600, color: 'var(--text-strong)' }}>{entry.name}</span>
                    {entry.description ? (
                      <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{entry.description}</span>
                    ) : null}
                  </span>
                </button>
              ))
            )}
          </main>
        </div>
      </section>
    </div>
  );
}
