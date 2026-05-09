import { useEffect, useMemo, useState } from 'react';

import { createApp, type AppDefinition } from '@/lib/api/apps';
import { listProjects, type OntologyProject } from '@/lib/api/ontology';
import { Glyph } from '@/lib/components/ui/Glyph';

interface SaveAsAppModalProps {
  open: boolean;
  defaultName?: string;
  onClose: () => void;
  onSaved: (app: AppDefinition) => void;
}

function deriveSlug(name: string): string {
  return (
    name
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '') || `app-${Date.now().toString(36)}`
  );
}

export function SaveAsAppModal({ open, defaultName, onClose, onSaved }: SaveAsAppModalProps) {
  const [fileName, setFileName] = useState(defaultName ?? '');
  const [projects, setProjects] = useState<OntologyProject[]>([]);
  const [projectId, setProjectId] = useState<string>('');
  const [browseOpen, setBrowseOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!open) return;
    setFileName(defaultName ?? '');
    setError('');
    setSaving(false);
    setBrowseOpen(false);
    void listProjects({ per_page: 200 })
      .then((response) => setProjects(response.data))
      .catch(() => setProjects([]));
  }, [open, defaultName]);

  const selectedProject = useMemo(() => projects.find((entry) => entry.id === projectId) ?? null, [projects, projectId]);

  if (!open) return null;

  async function save() {
    if (!fileName.trim()) {
      setError('File name is required.');
      return;
    }
    setSaving(true);
    setError('');
    try {
      const app = await createApp({
        name: fileName.trim(),
        slug: deriveSlug(fileName),
        status: 'draft',
      });
      onSaved(app);
      onClose();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Save failed');
    } finally {
      setSaving(false);
    }
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="save-as-app-title"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !saving) onClose();
      }}
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 95,
        background: 'rgba(17, 24, 39, 0.42)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: 24,
      }}
    >
      <section style={{ width: '100%', maxWidth: 520, background: '#fff', borderRadius: 6, boxShadow: '0 20px 48px rgba(15, 23, 42, 0.18)', overflow: 'hidden' }}>
        <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '12px 18px', borderBottom: '1px solid var(--border-subtle)' }}>
          <h2 id="save-as-app-title" style={{ margin: 0, fontSize: 15, fontWeight: 600 }}>Save as…</h2>
          <button type="button" aria-label="Close" onClick={onClose} className="of-button of-button--ghost" style={{ padding: 4 }}>
            <Glyph name="x" size={14} />
          </button>
        </header>
        <div style={{ padding: 18, display: 'grid', gap: 14 }}>
          <label style={{ display: 'grid', gap: 4 }}>
            <span style={{ fontSize: 13, fontWeight: 600 }}>File name</span>
            <span style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '6px 10px', border: '1.5px solid var(--status-info)', borderRadius: 4 }}>
              <Glyph name="object" size={13} tone="#7c5dd6" />
              <input
                value={fileName}
                onChange={(event) => setFileName(event.target.value)}
                autoFocus
                style={{ flex: 1, border: 0, outline: 'none', fontSize: 13, fontWeight: 500 }}
              />
              {fileName.trim().length > 0 ? <Glyph name="check" size={14} tone="#15803d" /> : null}
            </span>
          </label>
          <label style={{ display: 'grid', gap: 4 }}>
            <span style={{ fontSize: 13, fontWeight: 600 }}>Location</span>
            <span style={{ display: 'flex', alignItems: 'stretch', gap: 0, position: 'relative' }}>
              <button
                type="button"
                onClick={() => setBrowseOpen((open) => !open)}
                style={{
                  flex: 1,
                  display: 'flex',
                  alignItems: 'center',
                  gap: 8,
                  padding: '8px 10px',
                  border: '1px solid var(--border-default)',
                  borderRadius: '4px 0 0 4px',
                  background: '#f4f6f9',
                  cursor: 'pointer',
                  fontSize: 13,
                  textAlign: 'left',
                }}
              >
                <Glyph name="folder" size={13} tone="#cf923f" />
                <span style={{ flex: 1 }}>{selectedProject?.display_name || selectedProject?.slug || 'Select a project'}</span>
                <Glyph name="chevron-down" size={11} />
              </button>
              <button type="button" className="of-button" style={{ borderRadius: '0 4px 4px 0', borderLeft: 0 }}>
                Browse <Glyph name="chevron-down" size={11} />
              </button>
              {browseOpen ? (
                <div
                  role="menu"
                  style={{
                    position: 'absolute',
                    top: '100%',
                    left: 0,
                    right: 0,
                    background: '#fff',
                    border: '1px solid var(--border-default)',
                    borderRadius: 4,
                    boxShadow: '0 8px 24px rgba(15, 23, 42, 0.16)',
                    padding: 6,
                    maxHeight: 280,
                    overflowY: 'auto',
                    zIndex: 10,
                    marginTop: 2,
                  }}
                >
                  {projects.length === 0 ? (
                    <p className="of-text-muted" style={{ padding: 12, fontSize: 12, margin: 0 }}>No projects available.</p>
                  ) : (
                    projects.map((project) => (
                      <button
                        key={project.id}
                        type="button"
                        onClick={() => {
                          setProjectId(project.id);
                          setBrowseOpen(false);
                        }}
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          gap: 8,
                          width: '100%',
                          padding: '6px 10px',
                          border: 0,
                          background: projectId === project.id ? 'rgba(45, 114, 210, 0.08)' : 'transparent',
                          cursor: 'pointer',
                          fontSize: 13,
                          textAlign: 'left',
                          borderRadius: 4,
                        }}
                      >
                        <Glyph name="folder" size={13} tone="#cf923f" />
                        {project.display_name || project.slug}
                      </button>
                    ))
                  )}
                </div>
              ) : null}
            </span>
          </label>
          {error ? (
            <div role="alert" className="of-status-danger" style={{ padding: '8px 12px', borderRadius: 4, fontSize: 12 }}>
              {error}
            </div>
          ) : null}
        </div>
        <footer style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, padding: 12, borderTop: '1px solid var(--border-subtle)' }}>
          <button type="button" onClick={onClose} className="of-button" disabled={saving}>
            Cancel
          </button>
          <button
            type="button"
            onClick={() => void save()}
            disabled={!fileName.trim() || saving}
            style={{
              padding: '8px 14px',
              border: 0,
              borderRadius: 4,
              background: '#15803d',
              color: '#fff',
              fontSize: 13,
              fontWeight: 600,
              cursor: !fileName.trim() || saving ? 'not-allowed' : 'pointer',
              opacity: !fileName.trim() || saving ? 0.6 : 1,
            }}
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </footer>
      </section>
    </div>
  );
}
