import { useEffect, useRef, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';

import {
  listInterfaces,
  listLinkTypes,
  listObjectTypes,
  listProjects,
  listSharedPropertyTypes,
  type LinkType,
  type OntologyInterface,
  type ObjectType,
  type OntologyProject,
  type SharedPropertyType,
} from '@/lib/api/ontology';
import { Glyph } from '@/lib/components/ui/Glyph';
import { CreateObjectTypeWizard } from '@/lib/components/ontology/CreateObjectTypeWizard';

type Section = 'overview' | 'types' | 'interfaces' | 'shared' | 'links' | 'projects';

export function OntologyManagerPage() {
  const navigate = useNavigate();
  const [section, setSection] = useState<Section>('overview');
  const [objectTypes, setObjectTypes] = useState<ObjectType[]>([]);
  const [interfaces, setInterfaces] = useState<OntologyInterface[]>([]);
  const [shared, setShared] = useState<SharedPropertyType[]>([]);
  const [linkTypes, setLinkTypes] = useState<LinkType[]>([]);
  const [projects, setProjects] = useState<OntologyProject[]>([]);
  const [search, setSearch] = useState('');
  const [error, setError] = useState('');
  const [newMenuOpen, setNewMenuOpen] = useState(false);
  const [branchMenuOpen, setBranchMenuOpen] = useState(false);
  const [branchName, setBranchName] = useState('Main');
  const [branchDialogOpen, setBranchDialogOpen] = useState(false);
  const [wizardOpen, setWizardOpen] = useState(false);
  const newMenuRef = useRef<HTMLDivElement>(null);
  const branchMenuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!newMenuOpen && !branchMenuOpen) return;
    function onClickOutside(event: MouseEvent) {
      if (newMenuOpen && newMenuRef.current && !newMenuRef.current.contains(event.target as Node)) setNewMenuOpen(false);
      if (branchMenuOpen && branchMenuRef.current && !branchMenuRef.current.contains(event.target as Node)) setBranchMenuOpen(false);
    }
    window.addEventListener('mousedown', onClickOutside);
    return () => window.removeEventListener('mousedown', onClickOutside);
  }, [newMenuOpen, branchMenuOpen]);

  async function refresh() {
    setError('');
    try {
      const [types, ifs, sh, links, prs] = await Promise.all([
        listObjectTypes({ per_page: 200, search: search || undefined }),
        listInterfaces({ per_page: 200, search: search || undefined }),
        listSharedPropertyTypes({ per_page: 200, search: search || undefined }),
        listLinkTypes({ per_page: 200 }),
        listProjects({ per_page: 200 }),
      ]);
      setObjectTypes(types.data);
      setInterfaces(ifs.data);
      setShared(sh.data);
      setLinkTypes(links.data);
      setProjects(prs.data);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load');
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  return (
    <section className="of-page" style={{ padding: 24, display: 'grid', gap: 16 }}>
      <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 12 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <span style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 32, height: 32, borderRadius: 4, background: 'rgba(45, 114, 210, 0.12)', color: 'var(--status-info)' }}>
            <Glyph name="cube" size={18} tone="var(--status-info)" />
          </span>
          <div>
            <h1 className="of-heading-xl" style={{ margin: 0 }}>Ontology Manager</h1>
            <p className="of-text-muted" style={{ margin: '2px 0 0', fontSize: 12 }}>
              <Glyph name="folder" size={11} tone="#5c7080" /> Sandbox · OpenFoundry Public Ontology
            </p>
          </div>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <Link to="/ontology-manager/bindings" className="of-button" style={{ fontSize: 12 }}>
            <Glyph name="link" size={12} /> Bind dataset
          </Link>
          <div ref={branchMenuRef} style={{ position: 'relative' }}>
            <button type="button" className="of-button" onClick={() => setBranchMenuOpen((open) => !open)}>
              <Glyph name="cube" size={12} tone="#5c7080" /> {branchName} <Glyph name="chevron-down" size={11} />
            </button>
            {branchMenuOpen ? (
              <div role="menu" style={popoverStyle()}>
                <input
                  className="of-input"
                  placeholder="Search branches..."
                  style={{ marginBottom: 6 }}
                />
                <p className="of-text-muted" style={{ margin: '4px 0 4px 6px', fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                  Create a new branch
                </p>
                <button
                  type="button"
                  onClick={() => {
                    setBranchMenuOpen(false);
                    setBranchDialogOpen(true);
                  }}
                  style={menuItemStyle()}
                >
                  <Glyph name="cube" size={12} tone="#5c7080" /> {branchName}/new-branch
                </button>
                <p className="of-text-muted" style={{ margin: '6px 0 4px 6px', fontSize: 11 }}>No results</p>
                <button type="button" disabled style={{ ...menuItemStyle(), justifyContent: 'space-between' }}>
                  Show more Ontology branches <Glyph name="chevron-down" size={11} />
                </button>
                <div style={{ padding: 6 }}>
                  <button
                    type="button"
                    onClick={() => {
                      setBranchMenuOpen(false);
                      setBranchDialogOpen(true);
                    }}
                    style={{
                      width: '100%',
                      display: 'inline-flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      gap: 6,
                      padding: '8px 14px',
                      border: 0,
                      borderRadius: 4,
                      background: '#15803d',
                      color: '#fff',
                      fontSize: 13,
                      fontWeight: 600,
                      cursor: 'pointer',
                    }}
                  >
                    <Glyph name="plus" size={12} /> Create branch
                  </button>
                </div>
              </div>
            ) : null}
          </div>
          <div ref={newMenuRef} style={{ position: 'relative' }}>
            <button type="button" className="of-button of-button--primary" onClick={() => setNewMenuOpen((open) => !open)}>
              New <Glyph name="chevron-down" size={11} />
            </button>
            {newMenuOpen ? (
              <div role="menu" style={popoverStyle({ minWidth: 320 })}>
                <NewMenuItem
                  glyph={<Glyph name="cube" size={14} tone="var(--status-info)" />}
                  label="Object type"
                  description="Map datasets and models to object types"
                  enabled
                  onClick={() => {
                    setNewMenuOpen(false);
                    setWizardOpen(true);
                  }}
                  highlighted
                />
                <NewMenuItem
                  glyph={<Glyph name="link" size={14} tone="#5c7080" />}
                  label="Link type"
                  description="Create relationships between object types"
                />
                <NewMenuItem
                  glyph={<Glyph name="run" size={14} tone="#7c5dd6" />}
                  label="Action type"
                  description="Allow users to writeback to their ontology"
                />
                <div style={{ borderTop: '1px solid var(--border-subtle)', margin: '4px 0' }} />
                <NewMenuItem
                  glyph={<Glyph name="ontology" size={14} tone="#5c7080" />}
                  label="Shared property"
                  description="Create properties that can be shared across object types"
                />
                <NewMenuItem
                  glyph={<Glyph name="artifact" size={14} tone="#5c7080" />}
                  label="Interface"
                  description="Use interfaces to build against abstract types"
                />
                <NewMenuItem
                  glyph={<span style={{ fontFamily: 'serif', fontStyle: 'italic', fontSize: 14, color: '#5c7080' }}>fx</span>}
                  label="Function"
                  description="Define object modifications in code"
                />
              </div>
            ) : null}
          </div>
        </div>
      </header>

      {error && (
        <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {error}
        </div>
      )}

      <section className="of-panel" style={{ padding: 16 }}>
        <div style={{ display: 'flex', gap: 6 }}>
          <input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search…" className="of-input" />
          <button type="button" onClick={() => void refresh()} className="of-button">Apply</button>
        </div>
      </section>

      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4, borderBottom: '1px solid var(--border-default)' }}>
        {(['overview', 'types', 'interfaces', 'shared', 'links', 'projects'] as Section[]).map((s) => (
          <button
            key={s}
            type="button"
            onClick={() => setSection(s)}
            style={{
              fontSize: 12,
              borderBottom: section === s ? '2px solid #1d4ed8' : '2px solid transparent',
              background: 'transparent',
              border: 'none',
              padding: '8px 16px',
              cursor: 'pointer',
              color: section === s ? 'var(--text-default)' : 'var(--text-muted)',
              textTransform: 'capitalize',
            }}
          >
            {s}
          </button>
        ))}
      </div>

      {section === 'overview' && (
        <section className="of-panel" style={{ padding: 16 }}>
          <p className="of-eyebrow">Stats</p>
          <ul style={{ marginTop: 8, paddingLeft: 18, fontSize: 12 }}>
            <li>{objectTypes.length} object types</li>
            <li>{interfaces.length} interfaces</li>
            <li>{shared.length} shared property types</li>
            <li>{linkTypes.length} link types</li>
            <li>{projects.length} projects</li>
          </ul>
          <p className="of-eyebrow" style={{ marginTop: 12 }}>Related routes</p>
          <ul style={{ marginTop: 8, paddingLeft: 18, fontSize: 12 }}>
            <li><Link to="/object-link-types">Object & link types →</Link></li>
            <li><Link to="/interfaces">Interfaces →</Link></li>
            <li><Link to="/ontologies">Ontology projects →</Link></li>
            <li><Link to="/ontology-design">Ontology design →</Link></li>
            <li><Link to="/ontology-indexing">Ontology indexing (Funnel) →</Link></li>
            <li><Link to="/projects">Workspace projects →</Link></li>
            <li><Link to="/ontology-manager/bindings">Dataset → ObjectType bindings →</Link></li>
          </ul>
        </section>
      )}

      {section === 'types' && (
        <section className="of-panel" style={{ padding: 16 }}>
          <p className="of-eyebrow">Object types ({objectTypes.length})</p>
          <ul style={{ marginTop: 8, paddingLeft: 0, listStyle: 'none' }}>
            {objectTypes.map((t) => (
              <li key={t.id} style={{ padding: 8, borderBottom: '1px solid var(--border-subtle)' }}>
                <strong>{t.display_name}</strong> · {t.name} · pk: {t.primary_key_property ?? '—'}
              </li>
            ))}
          </ul>
        </section>
      )}

      {section === 'interfaces' && (
        <section className="of-panel" style={{ padding: 16 }}>
          <p className="of-eyebrow">Interfaces ({interfaces.length})</p>
          <ul style={{ marginTop: 8, paddingLeft: 0, listStyle: 'none' }}>
            {interfaces.map((i) => (
              <li key={i.id} style={{ padding: 8, borderBottom: '1px solid var(--border-subtle)' }}>
                <strong>{i.display_name}</strong> · {i.name}
                {i.description && <p className="of-text-muted" style={{ fontSize: 11, margin: 0 }}>{i.description}</p>}
              </li>
            ))}
          </ul>
        </section>
      )}

      {section === 'shared' && (
        <section className="of-panel" style={{ padding: 16 }}>
          <p className="of-eyebrow">Shared property types ({shared.length})</p>
          <ul style={{ marginTop: 8, paddingLeft: 0, listStyle: 'none' }}>
            {shared.map((s) => (
              <li key={s.id} style={{ padding: 8, borderBottom: '1px solid var(--border-subtle)' }}>
                <strong>{s.display_name}</strong> · {s.name} · {s.property_type}
              </li>
            ))}
          </ul>
        </section>
      )}

      {section === 'links' && (
        <section className="of-panel" style={{ padding: 16 }}>
          <p className="of-eyebrow">Link types ({linkTypes.length})</p>
          <ul style={{ marginTop: 8, paddingLeft: 0, listStyle: 'none' }}>
            {linkTypes.map((l) => (
              <li key={l.id} style={{ padding: 8, borderBottom: '1px solid var(--border-subtle)' }}>
                <strong>{l.display_name}</strong> · {l.name} · {l.source_type_id} → {l.target_type_id}
              </li>
            ))}
          </ul>
        </section>
      )}

      {section === 'projects' && (
        <section className="of-panel" style={{ padding: 16 }}>
          <p className="of-eyebrow">Projects ({projects.length})</p>
          <ul style={{ marginTop: 8, paddingLeft: 0, listStyle: 'none' }}>
            {projects.map((p) => (
              <li key={p.id} style={{ padding: 8, borderBottom: '1px solid var(--border-subtle)' }}>
                <Link to={`/projects/${p.id}`}><strong>{p.display_name || p.slug}</strong></Link> · {p.id}
              </li>
            ))}
          </ul>
        </section>
      )}

      <CreateObjectTypeWizard
        open={wizardOpen}
        onClose={() => setWizardOpen(false)}
        onCreated={(objectType) => {
          setWizardOpen(false);
          void refresh();
          navigate(`/ontology/${objectType.id}`);
        }}
      />

      {branchDialogOpen ? (
        <CreateBranchDialog
          onClose={() => setBranchDialogOpen(false)}
          onCreate={(name) => {
            setBranchName(name);
            setBranchDialogOpen(false);
          }}
        />
      ) : null}
    </section>
  );
}

function NewMenuItem({
  glyph,
  label,
  description,
  enabled,
  highlighted,
  onClick,
}: {
  glyph: React.ReactNode;
  label: string;
  description: string;
  enabled?: boolean;
  highlighted?: boolean;
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      onClick={enabled ? onClick : undefined}
      disabled={!enabled}
      style={{
        display: 'flex',
        alignItems: 'flex-start',
        gap: 10,
        width: '100%',
        padding: '10px 12px',
        border: 0,
        background: highlighted ? 'rgba(45, 114, 210, 0.06)' : 'transparent',
        cursor: enabled ? 'pointer' : 'not-allowed',
        opacity: enabled ? 1 : 0.6,
        textAlign: 'left',
        borderRadius: 4,
      }}
      onMouseEnter={(event) => {
        if (enabled) event.currentTarget.style.background = 'rgba(45, 114, 210, 0.08)';
      }}
      onMouseLeave={(event) => (event.currentTarget.style.background = highlighted ? 'rgba(45, 114, 210, 0.06)' : 'transparent')}
    >
      <span style={{ display: 'inline-flex', marginTop: 2 }}>{glyph}</span>
      <span style={{ display: 'grid', gap: 2 }}>
        <strong style={{ fontSize: 13, color: highlighted ? 'var(--status-info)' : 'var(--text-strong)' }}>{label}</strong>
        <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{description}</span>
      </span>
    </button>
  );
}

function popoverStyle(extra: React.CSSProperties = {}): React.CSSProperties {
  return {
    position: 'absolute',
    top: 'calc(100% + 4px)',
    right: 0,
    background: '#fff',
    border: '1px solid var(--border-default)',
    borderRadius: 6,
    boxShadow: '0 8px 24px rgba(15, 23, 42, 0.16)',
    padding: 6,
    minWidth: 240,
    zIndex: 30,
    ...extra,
  };
}

function menuItemStyle(): React.CSSProperties {
  return {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    width: '100%',
    padding: '6px 10px',
    border: 0,
    background: 'transparent',
    cursor: 'pointer',
    fontSize: 13,
    textAlign: 'left',
    borderRadius: 4,
  };
}

function CreateBranchDialog({ onClose, onCreate }: { onClose: () => void; onCreate: (name: string) => void }) {
  const [name, setName] = useState('username/e2espeedrun');
  const [indexing, setIndexing] = useState(true);
  const [datasetBranch, setDatasetBranch] = useState('');

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="create-branch-title"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 90,
        background: 'rgba(17, 24, 39, 0.42)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: 24,
      }}
    >
      <section style={{ width: '100%', maxWidth: 720, background: '#fff', borderRadius: 6, boxShadow: '0 20px 60px rgba(15, 23, 42, 0.18)', display: 'grid', gridTemplateRows: 'auto 1fr auto', overflow: 'hidden' }}>
        <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '12px 18px', borderBottom: '1px solid var(--border-subtle)' }}>
          <h2 id="create-branch-title" style={{ margin: 0, fontSize: 15, fontWeight: 600 }}>Create branch</h2>
          <button type="button" aria-label="Close" onClick={onClose} className="of-button of-button--ghost" style={{ padding: 4 }}>
            <Glyph name="x" size={14} />
          </button>
        </header>
        <div style={{ padding: 18, display: 'grid', gap: 14 }}>
          <p style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)' }}>
            This will create a Branch with your Ontology changes and a draft Proposal to review those changes. Open the Branch to view and add edits. Once all edits are approved, release the Proposal.
          </p>
          <label style={{ display: 'grid', gap: 4 }}>
            <span style={{ fontSize: 13, fontWeight: 600 }}>Branch name</span>
            <input
              value={name}
              onChange={(event) => setName(event.target.value)}
              autoFocus
              style={{ padding: '8px 10px', border: '1px solid var(--border-default)', borderRadius: 4, fontSize: 13 }}
            />
          </label>
          <div style={{ background: '#f4f6f9', border: '1px solid var(--border-subtle)', borderRadius: 6, padding: 12, display: 'grid', gap: 10 }}>
            <label style={{ display: 'flex', alignItems: 'flex-start', gap: 12, cursor: 'pointer' }}>
              <input type="checkbox" checked={indexing} onChange={(event) => setIndexing(event.target.checked)} style={{ accentColor: 'var(--status-info)', marginTop: 2 }} />
              <span style={{ display: 'grid', gap: 2 }}>
                <strong style={{ fontSize: 13 }}><Glyph name="eye" size={12} tone="var(--status-info)" /> Enable indexing on this branch</strong>
                <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                  Enable indexing if you need to preview schema changes for modified entities. This will use additional storage and compute. Only supported by Object Storage V2.
                </span>
              </span>
            </label>
            <label style={{ display: 'grid', gap: 4 }}>
              <span style={{ fontSize: 13, fontWeight: 600 }}>Dataset branch name (Optional)</span>
              <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Set this if you want to preview data or schema changes for modified entities from a branch other than master.</span>
              <input
                value={datasetBranch}
                onChange={(event) => setDatasetBranch(event.target.value)}
                style={{ padding: '8px 10px', border: '1px solid var(--border-default)', borderRadius: 4, fontSize: 13 }}
              />
            </label>
          </div>
        </div>
        <footer style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, padding: 12, borderTop: '1px solid var(--border-subtle)' }}>
          <button type="button" onClick={onClose} className="of-button">Cancel</button>
          <button
            type="button"
            onClick={() => onCreate(name.trim() || 'new-branch')}
            disabled={!name.trim()}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 6,
              padding: '8px 14px',
              border: 0,
              borderRadius: 4,
              background: '#15803d',
              color: '#fff',
              fontSize: 13,
              fontWeight: 600,
              cursor: name.trim() ? 'pointer' : 'not-allowed',
              opacity: name.trim() ? 1 : 0.6,
            }}
          >
            <Glyph name="plus" size={12} /> Create branch
          </button>
        </footer>
      </section>
    </div>
  );
}
