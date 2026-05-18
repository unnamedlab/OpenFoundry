import { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';

import { CreateProjectModal } from '@/lib/components/projects/CreateProjectModal';
import { Glyph, type GlyphName } from '@/lib/components/ui/Glyph';
import { listDatasets, type Dataset } from '@/lib/api/datasets';
import { listProjects, type OntologyProject } from '@/lib/api/ontology';
import { listSharedWithMe, type ResourceShare } from '@/lib/api/workspace';
import { projectStablePath, workspaceResourceStablePath } from '@/lib/compass/stableResourceUrls';
import { useAuth } from '@/lib/stores/auth';

type SpaceTab = 'data-catalog' | 'portfolios' | 'projects' | 'your-files' | 'shared';
type SubTab = 'collections' | 'files';

interface SpaceDef {
  id: SpaceTab;
  label: string;
  icon: GlyphName | 'check-circle';
}

const SPACES: SpaceDef[] = [
  { id: 'data-catalog', label: 'Data Catalog', icon: 'check-circle' },
  { id: 'portfolios', label: 'Portfolios', icon: 'bookmark' },
  { id: 'projects', label: 'Projects', icon: 'folder' },
  { id: 'your-files', label: 'Your files', icon: 'document' },
  { id: 'shared', label: 'Shared with you', icon: 'users' },
];

const NEW_ACTIONS: { label: string; to: string; description: string }[] = [
  { label: 'New collection', to: '/projects', description: 'Create a project to group resources.' },
  { label: 'New dataset', to: '/datasets', description: 'Register a dataset.' },
  { label: 'New pipeline', to: '/pipelines/new', description: 'Author a batch or streaming pipeline.' },
  { label: 'Upload data', to: '/datasets/upload', description: 'Upload files to a new dataset.' },
];

const FALLBACK_OWNER_HINT = 'Demo collection';
const PAGE_SIZE = 50;

function formatDate(value: string | null | undefined): string {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return new Intl.DateTimeFormat('en-US', { dateStyle: 'medium', timeStyle: 'short' }).format(date);
}

function projectName(project: OntologyProject): string {
  return project.display_name || project.slug;
}

function spaceIcon(name: SpaceDef['icon'], color: string) {
  if (name === 'check-circle') {
    return (
      <svg width={18} height={18} viewBox="0 0 24 24" fill="none" aria-hidden="true">
        <circle cx={12} cy={12} r={9} fill={color} />
        <path d="M8 12.5l2.5 2.5L16 9.5" stroke="#ffffff" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    );
  }
  return <Glyph name={name} size={18} tone={color} />;
}

export function Home() {
  const navigate = useNavigate();
  const { user } = useAuth();
  const [activeSpace, setActiveSpace] = useState<SpaceTab>('data-catalog');
  const [activeSubTab, setActiveSubTab] = useState<SubTab>('collections');
  const [search, setSearch] = useState('');
  const [newMenuOpen, setNewMenuOpen] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);

  const [projects, setProjects] = useState<OntologyProject[]>([]);
  const [datasets, setDatasets] = useState<Dataset[]>([]);
  const [shared, setShared] = useState<ResourceShare[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError('');
    Promise.all([
      listProjects({ per_page: PAGE_SIZE }).then((res) => res.data),
      listDatasets({ per_page: PAGE_SIZE }).then((res) => res.data),
      listSharedWithMe({ limit: PAGE_SIZE }),
    ])
      .then(([nextProjects, nextDatasets, nextShared]) => {
        if (cancelled) return;
        setProjects(nextProjects);
        setDatasets(nextDatasets);
        setShared(nextShared);
      })
      .catch((cause) => {
        if (cancelled) return;
        setError(cause instanceof Error ? cause.message : 'Failed to load workspace');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const filteredProjects = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return projects;
    return projects.filter((p) =>
      [projectName(p), p.slug, p.description].some((value) => (value || '').toLowerCase().includes(q)),
    );
  }, [projects, search]);

  const filteredDatasets = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return datasets;
    return datasets.filter((d) =>
      [d.name, d.id, d.description, ...(d.tags ?? [])].some((value) => (value || '').toLowerCase().includes(q)),
    );
  }, [datasets, search]);

  const yourFiles = useMemo(() => {
    if (!user) return [] as Dataset[];
    return datasets.filter((d) => d.owner_id === user.id);
  }, [datasets, user]);

  const showFiltersSidebar = activeSpace === 'data-catalog' && activeSubTab === 'files';

  return (
    <section className="of-page" style={{ display: 'grid', gap: 12 }}>
      {/* Top spaces strip */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          borderBottom: '1px solid var(--border-default)',
          paddingBottom: 0,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 0 }}>
          {SPACES.map((space) => {
            const isActive = activeSpace === space.id;
            const color = isActive ? 'var(--status-info)' : 'var(--text-soft)';
            return (
              <button
                key={space.id}
                type="button"
                onClick={() => {
                  setActiveSpace(space.id);
                  if (space.id === 'data-catalog') setActiveSubTab('collections');
                }}
                className={`of-tab ${isActive ? 'of-tab-active' : ''}`}
                style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 13 }}
              >
                {spaceIcon(space.icon, color)}
                <span>{space.label}</span>
              </button>
            );
          })}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, paddingBottom: 6 }}>
          <Link to="/settings" className="of-button">
            Manage spaces
          </Link>
          <Link to="/settings" className="of-button of-button--ghost" aria-label="Settings">
            <Glyph name="settings" size={16} />
          </Link>
        </div>
      </div>

      {error ? (
        <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {error}
        </div>
      ) : null}

      {activeSpace === 'data-catalog' ? (
        <DataCatalogView
          activeSubTab={activeSubTab}
          onSubTab={setActiveSubTab}
          projects={filteredProjects}
          datasets={filteredDatasets}
          loading={loading}
          search={search}
          onSearch={setSearch}
          onNewMenuOpen={newMenuOpen}
          setNewMenuOpen={setNewMenuOpen}
          onCreateProject={() => setCreateOpen(true)}
          showFiltersSidebar={showFiltersSidebar}
        />
      ) : null}

      {activeSpace === 'portfolios' ? (
        <EmptySpace
          title="Portfolios"
          description="Group collections into portfolios to give stakeholders a curated entry point. No portfolios have been created yet."
        />
      ) : null}

      {activeSpace === 'projects' ? (
        <ProjectsListView projects={filteredProjects} loading={loading} search={search} onSearch={setSearch} />
      ) : null}

      {activeSpace === 'your-files' ? (
        <FilesView
          datasets={yourFiles}
          loading={loading}
          search={search}
          onSearch={setSearch}
          emptyMessage={user ? 'You do not own any datasets yet.' : 'Sign in to see your files.'}
        />
      ) : null}

      {activeSpace === 'shared' ? (
        <SharedView shared={shared} loading={loading} />
      ) : null}

      <CreateProjectModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={(project) => {
          setCreateOpen(false);
          navigate(projectStablePath(project));
        }}
      />
    </section>
  );
}

interface DataCatalogViewProps {
  activeSubTab: SubTab;
  onSubTab: (tab: SubTab) => void;
  projects: OntologyProject[];
  datasets: Dataset[];
  loading: boolean;
  search: string;
  onSearch: (value: string) => void;
  onNewMenuOpen: boolean;
  setNewMenuOpen: (open: boolean) => void;
  onCreateProject: () => void;
  showFiltersSidebar: boolean;
}

function DataCatalogView({
  activeSubTab,
  onSubTab,
  projects,
  datasets,
  loading,
  search,
  onSearch,
  onNewMenuOpen,
  setNewMenuOpen,
  onCreateProject,
  showFiltersSidebar,
}: DataCatalogViewProps) {
  return (
    <>
      <header style={{ display: 'flex', alignItems: 'flex-end', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
        <div>
          <h1 className="of-heading-xl" style={{ marginTop: 0, marginBottom: 4 }}>
            Data Catalog
          </h1>
          <div className="of-tabbar" style={{ marginTop: 6, gap: 4, paddingBottom: 0, border: 0 }}>
            {(['collections', 'files'] as SubTab[]).map((tab) => (
              <button
                key={tab}
                type="button"
                onClick={() => onSubTab(tab)}
                className={`of-tab ${activeSubTab === tab ? 'of-tab-active' : ''}`}
                style={{ fontSize: 13 }}
              >
                {tab === 'collections' ? 'Collections' : 'Files'}
              </button>
            ))}
          </div>
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center', position: 'relative' }}>
          <button type="button" className="of-button">
            Request data
          </button>
          <div style={{ position: 'relative' }}>
            <button
              type="button"
              className="of-button of-button--success"
              onClick={() => setNewMenuOpen(!onNewMenuOpen)}
              aria-haspopup="menu"
              aria-expanded={onNewMenuOpen}
            >
              <Glyph name="plus" size={14} tone="#ffffff" /> New
              <span style={{ marginLeft: 4, opacity: 0.85 }}>▾</span>
            </button>
            {onNewMenuOpen ? (
              <div
                role="menu"
                className="of-popover"
                style={{
                  position: 'absolute',
                  top: 'calc(100% + 6px)',
                  right: 0,
                  minWidth: 240,
                  padding: 4,
                  display: 'grid',
                  gap: 2,
                  zIndex: 20,
                }}
                onMouseLeave={() => setNewMenuOpen(false)}
              >
                {NEW_ACTIONS.map((action) =>
                  action.label === 'New collection' ? (
                    <button
                      key={action.label}
                      type="button"
                      onClick={() => {
                        setNewMenuOpen(false);
                        onCreateProject();
                      }}
                      role="menuitem"
                      style={{
                        display: 'grid',
                        gap: 2,
                        padding: '8px 10px',
                        background: 'transparent',
                        border: 0,
                        textAlign: 'left',
                        borderRadius: 'var(--radius-sm)',
                        cursor: 'pointer',
                      }}
                    >
                      <span style={{ color: 'var(--text-strong)', fontWeight: 600, fontSize: 13 }}>{action.label}</span>
                      <span className="of-text-muted" style={{ fontSize: 11 }}>{action.description}</span>
                    </button>
                  ) : (
                    <Link
                      key={action.to}
                      to={action.to}
                      onClick={() => setNewMenuOpen(false)}
                      role="menuitem"
                      style={{
                        display: 'grid',
                        gap: 2,
                        padding: '8px 10px',
                        color: 'var(--text-default)',
                        borderRadius: 'var(--radius-sm)',
                      }}
                    >
                      <span style={{ color: 'var(--text-strong)', fontWeight: 600, fontSize: 13 }}>{action.label}</span>
                      <span className="of-text-muted" style={{ fontSize: 11 }}>{action.description}</span>
                    </Link>
                  ),
                )}
              </div>
            ) : null}
          </div>
        </div>
      </header>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: showFiltersSidebar ? 'minmax(180px, 220px) minmax(0, 1fr)' : 'minmax(0, 1fr)',
          gap: 12,
          alignItems: 'start',
        }}
      >
        {showFiltersSidebar ? <FiltersSidebar /> : null}
        <section className="of-panel" style={{ overflow: 'hidden' }}>
          <div
            className="of-toolbar"
            style={{
              border: 0,
              borderBottom: '1px solid var(--border-subtle)',
              borderRadius: 0,
              justifyContent: 'flex-end',
              padding: '8px 12px',
            }}
          >
            <input
              className="of-input"
              placeholder={activeSubTab === 'collections' ? 'Search collections' : 'Search files'}
              value={search}
              onChange={(e) => onSearch(e.target.value)}
              style={{ maxWidth: 280 }}
            />
          </div>
          {activeSubTab === 'collections' ? (
            <CollectionsTable projects={projects} loading={loading} search={search} />
          ) : (
            <FilesTable datasets={datasets} loading={loading} search={search} />
          )}
        </section>
      </div>
    </>
  );
}

function FiltersSidebar() {
  return (
    <aside className="of-panel" style={{ padding: '12px 12px 8px', display: 'grid', gap: 8 }}>
      <p className="of-eyebrow" style={{ margin: 0 }}>Filters</p>
      <FilterGroup label="Tags" />
      <FilterGroup label="Type" />
    </aside>
  );
}

function FilterGroup({ label }: { label: string }) {
  const [open, setOpen] = useState(false);
  return (
    <div style={{ borderTop: '1px solid var(--border-subtle)', padding: '8px 0 4px' }}>
      <button
        type="button"
        onClick={() => setOpen(!open)}
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          width: '100%',
          padding: 0,
          background: 'transparent',
          border: 0,
          color: 'var(--text-strong)',
          fontWeight: 700,
          fontSize: 11,
          letterSpacing: '0.04em',
          textTransform: 'uppercase',
          cursor: 'pointer',
        }}
        aria-expanded={open}
      >
        <span>{label}</span>
        <Glyph name="plus" size={14} />
      </button>
      {open ? (
        <p className="of-text-muted" style={{ marginTop: 6, fontSize: 11 }}>
          No options registered.
        </p>
      ) : null}
    </div>
  );
}

interface CollectionsTableProps {
  projects: OntologyProject[];
  loading: boolean;
  search: string;
}

function CollectionsTable({ projects, loading, search }: CollectionsTableProps) {
  return (
    <table className="of-table">
      <thead>
        <tr>
          <th style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            Name
            <span style={{ color: 'var(--status-info)' }}>▲</span>
          </th>
          <th style={{ width: 140, textAlign: 'right' }}>Files</th>
        </tr>
      </thead>
      <tbody>
        {loading && projects.length === 0 ? (
          <tr>
            <td colSpan={2} style={{ padding: 24, textAlign: 'center' }}>
              <span className="of-text-muted">Loading collections…</span>
            </td>
          </tr>
        ) : projects.length === 0 ? (
          <tr>
            <td colSpan={2} style={{ padding: 24, textAlign: 'center' }}>
              <span className="of-text-muted">
                {search ? `No collections match “${search}”` : 'No collections yet.'}
              </span>
            </td>
          </tr>
        ) : (
          projects.map((project) => (
            <tr key={project.id}>
              <td>
                <div style={{ display: 'flex', alignItems: 'flex-start', gap: 8 }}>
                  <span style={{ marginTop: 2 }}>
                    <Glyph name="bookmark" size={16} tone="var(--status-info)" />
                  </span>
                  <div style={{ display: 'grid', gap: 2 }}>
                    <Link to={projectStablePath(project)} className="of-link" style={{ fontWeight: 600 }}>
                      {projectName(project)}
                    </Link>
                    <span className="of-text-muted" style={{ fontSize: 11 }}>
                      {project.description?.trim() || FALLBACK_OWNER_HINT}{' '}
                      <span className="of-text-soft" style={{ fontFamily: 'var(--font-mono)' }}>{project.slug}</span>
                    </span>
                  </div>
                </div>
              </td>
              <td style={{ textAlign: 'right' }}>
                <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                  <Glyph name="folder" size={14} tone="var(--text-muted)" />
                  <span className="of-text-muted">-</span>
                </span>
              </td>
            </tr>
          ))
        )}
      </tbody>
    </table>
  );
}

interface FilesTableProps {
  datasets: Dataset[];
  loading: boolean;
  search: string;
  emptyMessage?: string;
}

function FilesTable({ datasets, loading, search, emptyMessage }: FilesTableProps) {
  return (
    <table className="of-table">
      <thead>
        <tr>
          <th style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            Name
            <span style={{ color: 'var(--status-info)' }}>▲</span>
          </th>
          <th style={{ width: 220 }}>Last updated</th>
          <th style={{ width: 240 }}>Tags</th>
        </tr>
      </thead>
      <tbody>
        {loading && datasets.length === 0 ? (
          <tr>
            <td colSpan={3} style={{ padding: 24, textAlign: 'center' }}>
              <span className="of-text-muted">Loading files…</span>
            </td>
          </tr>
        ) : datasets.length === 0 ? (
          <tr>
            <td colSpan={3} style={{ padding: 24, textAlign: 'center' }}>
              <span className="of-text-muted">
                {search ? `No files match “${search}”` : (emptyMessage ?? 'No datasets registered yet.')}
              </span>
            </td>
          </tr>
        ) : (
          datasets.map((dataset) => (
            <tr key={dataset.id}>
              <td>
                <div style={{ display: 'flex', alignItems: 'flex-start', gap: 8 }}>
                  <span style={{ marginTop: 2 }}>
                    <Glyph name="spreadsheet" size={16} tone="var(--status-info)" />
                  </span>
                  <div style={{ display: 'grid', gap: 2 }}>
                    <Link to={`/datasets/${dataset.id}`} className="of-link" style={{ fontWeight: 600 }}>
                      {dataset.name}
                    </Link>
                    <span className="of-text-soft" style={{ fontFamily: 'var(--font-mono)', fontSize: 10 }}>
                      {dataset.storage_path || `/${dataset.id}`}
                    </span>
                  </div>
                </div>
              </td>
              <td className="of-text-muted">{formatDate(dataset.updated_at)}</td>
              <td>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
                  {(dataset.tags ?? []).slice(0, 3).map((tag) => (
                    <span key={tag} className="of-chip">{tag}</span>
                  ))}
                  {(dataset.tags?.length ?? 0) > 3 ? (
                    <span className="of-text-muted" style={{ fontSize: 11 }}>+{(dataset.tags?.length ?? 0) - 3}</span>
                  ) : null}
                  {(dataset.tags?.length ?? 0) === 0 ? <span className="of-text-soft" style={{ fontSize: 11 }}>—</span> : null}
                </div>
              </td>
            </tr>
          ))
        )}
      </tbody>
    </table>
  );
}

function ProjectsListView({
  projects,
  loading,
  search,
  onSearch,
}: {
  projects: OntologyProject[];
  loading: boolean;
  search: string;
  onSearch: (value: string) => void;
}) {
  return (
    <section className="of-panel" style={{ overflow: 'hidden' }}>
      <header
        style={{
          display: 'flex',
          alignItems: 'flex-end',
          justifyContent: 'space-between',
          padding: '10px 12px',
          borderBottom: '1px solid var(--border-subtle)',
        }}
      >
        <h1 className="of-heading-xl" style={{ margin: 0 }}>Projects</h1>
        <input
          className="of-input"
          placeholder="Search projects"
          value={search}
          onChange={(e) => onSearch(e.target.value)}
          style={{ maxWidth: 280 }}
        />
      </header>
      <CollectionsTable projects={projects} loading={loading} search={search} />
    </section>
  );
}

function FilesView({
  datasets,
  loading,
  search,
  onSearch,
  emptyMessage,
}: {
  datasets: Dataset[];
  loading: boolean;
  search: string;
  onSearch: (value: string) => void;
  emptyMessage: string;
}) {
  return (
    <section className="of-panel" style={{ overflow: 'hidden' }}>
      <header
        style={{
          display: 'flex',
          alignItems: 'flex-end',
          justifyContent: 'space-between',
          padding: '10px 12px',
          borderBottom: '1px solid var(--border-subtle)',
        }}
      >
        <h1 className="of-heading-xl" style={{ margin: 0 }}>Your files</h1>
        <input
          className="of-input"
          placeholder="Search files"
          value={search}
          onChange={(e) => onSearch(e.target.value)}
          style={{ maxWidth: 280 }}
        />
      </header>
      <FilesTable datasets={datasets} loading={loading} search={search} emptyMessage={emptyMessage} />
    </section>
  );
}

function SharedView({ shared, loading }: { shared: ResourceShare[]; loading: boolean }) {
  return (
    <section className="of-panel" style={{ overflow: 'hidden' }}>
      <header style={{ padding: '10px 12px', borderBottom: '1px solid var(--border-subtle)' }}>
        <h1 className="of-heading-xl" style={{ margin: 0 }}>Shared with you</h1>
        <p className="of-text-muted" style={{ marginTop: 4, fontSize: 12 }}>
          Resources other users granted you access to.
        </p>
      </header>
      <table className="of-table">
        <thead>
          <tr>
            <th>Resource</th>
            <th style={{ width: 120 }}>Access</th>
            <th style={{ width: 200 }}>Shared by</th>
            <th style={{ width: 200 }}>Created</th>
          </tr>
        </thead>
        <tbody>
          {loading && shared.length === 0 ? (
            <tr>
              <td colSpan={4} style={{ padding: 24, textAlign: 'center' }}>
                <span className="of-text-muted">Loading shares…</span>
              </td>
            </tr>
          ) : shared.length === 0 ? (
            <tr>
              <td colSpan={4} style={{ padding: 24, textAlign: 'center' }}>
                <span className="of-text-muted">Nothing shared with you.</span>
              </td>
            </tr>
          ) : (
            shared.map((share) => {
              const href = share.resource_kind === 'ontology_project'
                ? workspaceResourceStablePath(share.resource_kind, share.resource_id)
                : share.resource_kind === 'dataset'
                  ? workspaceResourceStablePath(share.resource_kind, share.resource_id)
                  : null;
              const label = share.resource_id;
              return (
                <tr key={share.id}>
                  <td>
                    {href ? (
                      <Link to={href} className="of-link">{label}</Link>
                    ) : (
                      <span style={{ color: 'var(--text-strong)', fontWeight: 600 }}>{label}</span>
                    )}
                    <div className="of-text-soft" style={{ marginTop: 2, fontSize: 10 }}>
                      {share.resource_kind.replace(/_/g, ' ')}
                    </div>
                  </td>
                  <td><span className="of-chip">{share.access_level}</span></td>
                  <td className="of-text-muted">{share.sharer_id}</td>
                  <td className="of-text-muted">{formatDate(share.created_at)}</td>
                </tr>
              );
            })
          )}
        </tbody>
      </table>
    </section>
  );
}

function EmptySpace({ title, description }: { title: string; description: string }) {
  return (
    <section className="of-panel" style={{ padding: '32px 24px', textAlign: 'center' }}>
      <h2 className="of-heading-lg" style={{ margin: 0 }}>{title}</h2>
      <p className="of-text-muted" style={{ marginTop: 8, maxWidth: 480, marginInline: 'auto', fontSize: 13 }}>
        {description}
      </p>
    </section>
  );
}
