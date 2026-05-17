import { useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';

import { ConfirmDialog } from '@components/ConfirmDialog';
import { CreateProjectModal } from '@/lib/components/projects/CreateProjectModal';
import { RequestDataDialog } from '@/lib/components/projects/RequestDataDialog';
import {
  deleteProject,
  listProjects,
  type OntologyProject,
} from '@/lib/api/ontology';
import {
  listSharedWithMe,
  listTrash,
  purgeResource,
  restoreResource,
  type ResourceShare,
  type TrashEntry,
} from '@/lib/api/workspace';
import { useCurrentUser } from '@/lib/stores/auth';

type Section = 'portfolios' | 'projects' | 'your-files' | 'shared' | 'trash';

interface SectionEntry {
  id: Section;
  label: string;
  glyph: 'portfolios' | 'projects' | 'your-files' | 'shared' | 'trash';
}

const SECTIONS: SectionEntry[] = [
  { id: 'portfolios', label: 'Portfolios', glyph: 'portfolios' },
  { id: 'projects', label: 'Projects', glyph: 'projects' },
  { id: 'your-files', label: 'Your files', glyph: 'your-files' },
  { id: 'shared', label: 'Shared with you', glyph: 'shared' },
  { id: 'trash', label: 'Trash', glyph: 'trash' },
];

function projectName(project: OntologyProject) {
  return project.display_name || project.slug;
}

function formatDateTime(value: string | null | undefined) {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return new Intl.DateTimeFormat('en-US', {
    weekday: 'short',
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    hour12: true,
  }).format(date);
}

function formatKind(kind: string) {
  return kind.replace(/_/g, ' ');
}

function trashRetentionLabel(entry: TrashEntry) {
  return `Purge after ${formatDateTime(entry.purge_after)} · ${entry.retention_days}d`;
}

function sharedResourceHref(share: ResourceShare): string | null {
  if (share.resource_kind === 'ontology_project') return `/projects/${share.resource_id}`;
  return null;
}

function isSection(value: string | null): value is Section {
  return (
    value === 'portfolios' ||
    value === 'projects' ||
    value === 'your-files' ||
    value === 'shared' ||
    value === 'trash'
  );
}

// ─── Inline icon set, kept tight to mirror Compass screenshots ────────────────

interface IconProps {
  size?: number;
  color?: string;
}

function FolderClosedIcon({ size = 18, color = '#5f6b7a' }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M3.5 7.5h6l2 2h9v9a1.5 1.5 0 0 1-1.5 1.5H5A1.5 1.5 0 0 1 3.5 18.5z"
        stroke={color}
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function PortfoliosIcon({ size = 18, color = 'currentColor' }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <rect x="4" y="5" width="6" height="14" rx="1" stroke={color} strokeWidth="1.6" />
      <rect x="11.5" y="5" width="6" height="14" rx="1" stroke={color} strokeWidth="1.6" />
      <path d="M19 6.7l1.7.3-1.6 13.6-1.7-.3z" stroke={color} strokeWidth="1.6" strokeLinejoin="round" />
      <path d="M5.5 8.5h3M5.5 11h3M13 8.5h3M13 11h3" stroke={color} strokeWidth="1.4" strokeLinecap="round" />
    </svg>
  );
}

function BriefcaseIcon({ size = 18, color = 'currentColor' }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <rect x="3.5" y="7.5" width="17" height="11" rx="1.5" stroke={color} strokeWidth="1.6" />
      <path d="M9 7.5V6a1.5 1.5 0 0 1 1.5-1.5h3A1.5 1.5 0 0 1 15 6v1.5" stroke={color} strokeWidth="1.6" strokeLinecap="round" />
      <path d="M3.5 12.5h17" stroke={color} strokeWidth="1.6" />
    </svg>
  );
}

function UserIcon({ size = 18, color = 'currentColor' }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="9" r="3.4" stroke={color} strokeWidth="1.6" />
      <path d="M5 19a7 7 0 0 1 14 0" stroke={color} strokeWidth="1.6" strokeLinecap="round" />
    </svg>
  );
}

function GroupIcon({ size = 18, color = 'currentColor' }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="9" cy="9" r="3" stroke={color} strokeWidth="1.6" />
      <circle cx="16.5" cy="10" r="2.4" stroke={color} strokeWidth="1.6" />
      <path d="M3.5 18.5a5.5 5.5 0 0 1 11 0" stroke={color} strokeWidth="1.6" strokeLinecap="round" />
      <path d="M14.5 18.5a4 4 0 0 1 6.5-3.1" stroke={color} strokeWidth="1.6" strokeLinecap="round" />
    </svg>
  );
}

function GearIcon({ size = 14, color = 'currentColor' }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="12" r="2.6" stroke={color} strokeWidth="1.6" />
      <path
        d="M12 4.5v1.8M12 17.7v1.8M4.5 12h1.8M17.7 12h1.8M6.4 6.4l1.3 1.3M16.3 16.3l1.3 1.3M6.4 17.6l1.3-1.3M16.3 7.7l1.3-1.3"
        stroke={color}
        strokeWidth="1.6"
        strokeLinecap="round"
      />
    </svg>
  );
}

function PlusIcon({ size = 13, color = 'currentColor' }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M12 5v14M5 12h14" stroke={color} strokeWidth="2.2" strokeLinecap="round" />
    </svg>
  );
}

function ProjectGlyphIcon({ size = 18, accent = '#7c5dd6' }: { size?: number; accent?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <rect x="3" y="6" width="14" height="11" rx="1.5" stroke="#5f6b7a" strokeWidth="1.6" />
      <path d="M3 9.2h14" stroke="#5f6b7a" strokeWidth="1.6" />
      <circle cx="18.4" cy="17.6" r="3.4" fill={accent} />
      <path d="M17 17.6l1 1 1.8-2" stroke="#fff" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function MoreIcon({ size = 16, color = 'currentColor' }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="6" cy="12" r="1.6" fill={color} />
      <circle cx="12" cy="12" r="1.6" fill={color} />
      <circle cx="18" cy="12" r="1.6" fill={color} />
    </svg>
  );
}

function SectionGlyph({ name, active }: { name: SectionEntry['glyph']; active: boolean }) {
  const color = active ? 'var(--text-strong)' : '#5f6b7a';
  switch (name) {
    case 'portfolios':
      return <PortfoliosIcon color={color} />;
    case 'projects':
      return <BriefcaseIcon color={color} />;
    case 'your-files':
      return <UserIcon color={color} />;
    case 'shared':
      return <GroupIcon color={color} />;
    case 'trash':
      return <TrashIcon color={color} />;
  }
}

function TrashIcon({ size = 18, color = 'currentColor' }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M5 7h14M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M7 7l1 13a1 1 0 0 0 1 1h6a1 1 0 0 0 1-1l1-13" stroke={color} strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M10 11v6M14 11v6" stroke={color} strokeWidth="1.4" strokeLinecap="round" />
    </svg>
  );
}

// ─── Component ───────────────────────────────────────────────────────────────

export function ProjectsListPage() {
  const navigate = useNavigate();
  const currentUser = useCurrentUser();
  const [searchParams, setSearchParams] = useSearchParams();

  const sectionParam = searchParams.get('section');
  const initialSection: Section = isSection(sectionParam) ? sectionParam : 'projects';
  const [section, setSection] = useState<Section>(initialSection);

  const [projects, setProjects] = useState<OntologyProject[]>([]);
  const [shared, setShared] = useState<ResourceShare[]>([]);
  const [trash, setTrash] = useState<TrashEntry[]>([]);
  const [search, setSearch] = useState('');
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [createOpen, setCreateOpen] = useState(false);
  const [requestOpen, setRequestOpen] = useState(false);
  const [requestNotice, setRequestNotice] = useState('');
  const [trashNotice, setTrashNotice] = useState('');
  const [trashOpen, setTrashOpen] = useState(false);
  const [manageOpen, setManageOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<OntologyProject | null>(null);
  const [purgeTarget, setPurgeTarget] = useState<TrashEntry | null>(null);

  const manageRef = useRef<HTMLDivElement>(null);

  const sectionTitle = useMemo(() => {
    if (section === 'portfolios') return 'Portfolios';
    if (section === 'projects') return 'Projects';
    if (section === 'your-files') return 'Your files';
    if (section === 'trash') return 'Trash';
    return 'Shared with you';
  }, [section]);

  const filteredProjects = useMemo(() => {
    const q = search.trim().toLowerCase();
    let rows = projects;
    if (section === 'your-files' && currentUser?.id) {
      rows = rows.filter((project) => project.owner_id === currentUser.id);
    }
    if (!q) return rows;
    return rows.filter((project) => {
      return (
        project.slug.toLowerCase().includes(q) ||
        (project.display_name || '').toLowerCase().includes(q) ||
        (project.description || '').toLowerCase().includes(q)
      );
    });
  }, [projects, search, section, currentUser?.id]);

  async function refreshSection(next: Section) {
    setLoading(true);
    setError('');
    try {
      if (next === 'shared') {
        setShared(await listSharedWithMe({ limit: 200 }));
      } else {
        const res = await listProjects({ per_page: 200 });
        setProjects(res.data);
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  }

  async function loadTrash() {
    setBusy(true);
    setError('');
    try {
      setTrash(await listTrash({ limit: 200 }));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load trash');
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (section === 'projects') next.delete('section');
        else next.set('section', section);
        return next;
      },
      { replace: true },
    );
    if (section === 'trash') {
      void loadTrash();
    } else {
      void refreshSection(section);
    }
  }, [section]);

  useEffect(() => {
    if (!manageOpen) return;
    function onClickOutside(event: MouseEvent) {
      if (!manageRef.current?.contains(event.target as Node)) setManageOpen(false);
    }
    function onEsc(event: KeyboardEvent) {
      if (event.key === 'Escape') setManageOpen(false);
    }
    window.addEventListener('mousedown', onClickOutside);
    window.addEventListener('keydown', onEsc);
    return () => {
      window.removeEventListener('mousedown', onClickOutside);
      window.removeEventListener('keydown', onEsc);
    };
  }, [manageOpen]);

  async function confirmDelete() {
    if (!deleteTarget) return;
    setBusy(true);
    setError('');
    try {
      await deleteProject(deleteTarget.id);
      setDeleteTarget(null);
      await refreshSection(section);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Delete failed');
    } finally {
      setBusy(false);
    }
  }

  async function restore(entry: TrashEntry) {
    setBusy(true);
    setError('');
    setTrashNotice('');
    try {
      const result = await restoreResource(entry.resource_kind, entry.resource_id);
      setTrashNotice(result.banner || `${entry.display_name || entry.resource_id} restored.`);
      await loadTrash();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Restore failed');
    } finally {
      setBusy(false);
    }
  }

  async function confirmPurge() {
    if (!purgeTarget) return;
    setBusy(true);
    setError('');
    try {
      await purgeResource(purgeTarget.resource_kind, purgeTarget.resource_id);
      setPurgeTarget(null);
      await loadTrash();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Purge failed');
    } finally {
      setBusy(false);
    }
  }

  function openTrashOverlay() {
    setManageOpen(false);
    setTrashOpen(true);
    void loadTrash();
  }

  function handleSection(next: Section) {
    setSection(next);
    setSearch('');
  }

  function handleRequestSubmitted(payload: { title: string; description: string; useCase: string }) {
    setRequestOpen(false);
    setRequestNotice(`Request "${payload.title}" queued. The data steward team will follow up.`);
    setTimeout(() => setRequestNotice(''), 5000);
  }

  return (
    <section
      className="of-page"
      style={{ display: 'grid', gap: 0, padding: 0, background: '#fff', minHeight: '100%' }}
    >
      {/* ── Compass-style sub-nav (img_001) ───────────────────────────── */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 16,
          padding: '8px 18px',
          borderBottom: '1px solid var(--border-subtle)',
          background: '#fff',
          position: 'relative',
        }}
      >
        <nav
          aria-label="Compass sections"
          style={{ display: 'flex', alignItems: 'center', gap: 4 }}
        >
          <button
            type="button"
            aria-label="All files"
            onClick={() => handleSection('projects')}
            style={{
              border: 0,
              background: 'transparent',
              padding: 6,
              cursor: 'pointer',
              display: 'inline-flex',
              alignItems: 'center',
            }}
          >
            <FolderClosedIcon size={20} color="#5f6b7a" />
          </button>
          {SECTIONS.map((entry) => {
            const active = section === entry.id;
            return (
              <button
                key={entry.id}
                type="button"
                role="tab"
                aria-selected={active}
                onClick={() => handleSection(entry.id)}
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 8,
                  padding: '6px 10px',
                  border: 0,
                  borderRadius: 3,
                  background: 'transparent',
                  color: active ? 'var(--text-strong)' : 'var(--text-default)',
                  fontSize: 14,
                  fontWeight: active ? 600 : 500,
                  cursor: 'pointer',
                  position: 'relative',
                }}
              >
                <SectionGlyph name={entry.glyph} active={active} />
                <span>{entry.label}</span>
              </button>
            );
          })}
        </nav>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, position: 'relative' }} ref={manageRef}>
          <button
            type="button"
            className="of-button"
            onClick={() => setManageOpen((open) => !open)}
            aria-haspopup="menu"
            aria-expanded={manageOpen}
            style={{ paddingRight: 10 }}
          >
            Manage spaces
            <GearIcon />
          </button>
          {manageOpen ? (
            <div
              role="menu"
              style={{
                position: 'absolute',
                top: '100%',
                right: 0,
                marginTop: 6,
                zIndex: 30,
                minWidth: 220,
                background: '#fff',
                border: '1px solid var(--border-default)',
                borderRadius: 4,
                boxShadow: 'var(--shadow-popover)',
                padding: 4,
              }}
            >
              <MenuItem onClick={openTrashOverlay}>Open trash</MenuItem>
              <MenuItem
                onClick={() => {
                  setManageOpen(false);
                  navigate('/settings');
                }}
              >
                Workspace settings
              </MenuItem>
              <MenuItem
                onClick={() => {
                  setManageOpen(false);
                  if (section === 'trash') void loadTrash();
                  else void refreshSection(section);
                }}
              >
                Refresh
              </MenuItem>
            </div>
          ) : null}
        </div>
      </div>

      {/* ── Section header (title + sub-tabs + actions) ───────────────── */}
      <div style={{ padding: '18px 22px 0', background: '#fff' }}>
        <div
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'flex-end',
            gap: 12,
            flexWrap: 'wrap',
          }}
        >
          <div style={{ display: 'grid', gap: 4 }}>
            <h1
              style={{
                margin: 0,
                fontSize: 22,
                fontWeight: 700,
                color: 'var(--text-strong)',
                letterSpacing: 0,
              }}
            >
              {sectionTitle}
            </h1>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <button type="button" className="of-button" onClick={() => setRequestOpen(true)}>
              Request data
            </button>
            <button
              type="button"
              className="of-button of-button--success"
              onClick={() => setCreateOpen(true)}
              style={{ paddingLeft: 10, paddingRight: 12 }}
            >
              <PlusIcon color="#fff" /> New
            </button>
          </div>
        </div>
        <div
          style={{
            marginTop: 14,
            borderBottom: '1px solid var(--border-subtle)',
          }}
        />
      </div>

      {error ? (
        <div
          className="of-status-danger"
          style={{
            margin: '8px 22px 0',
            padding: '10px 14px',
            borderRadius: 'var(--radius-md)',
            fontSize: 13,
          }}
        >
          {error}
        </div>
      ) : null}

      {requestNotice ? (
        <div
          className="of-status-info"
          style={{
            margin: '8px 22px 0',
            padding: '10px 14px',
            borderRadius: 'var(--radius-md)',
            fontSize: 13,
          }}
        >
          {requestNotice}
        </div>
      ) : null}

      {trashNotice ? (
        <div
          className="of-status-info"
          style={{
            margin: '8px 22px 0',
            padding: '10px 14px',
            borderRadius: 'var(--radius-md)',
            fontSize: 13,
          }}
        >
          {trashNotice}
        </div>
      ) : null}

      {/* ── Body: optional FILTERS rail + main table ──────────────────── */}
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'minmax(0, 1fr)',
          background: '#fff',
          minHeight: 480,
        }}
      >
        <div style={{ display: 'grid', gap: 0 }}>
          {section === 'projects' ? (
            <ProjectsTable
              projects={filteredProjects}
              loading={loading}
              search={search}
              onSearchChange={setSearch}
              onSearchSubmit={() => void refreshSection(section)}
              onDelete={(project) => setDeleteTarget(project)}
              busy={busy}
            />
          ) : null}

          {section === 'your-files' ? (
            <ProjectsTable
              projects={filteredProjects}
              loading={loading}
              search={search}
              onSearchChange={setSearch}
              onSearchSubmit={() => void refreshSection(section)}
              onDelete={(project) => setDeleteTarget(project)}
              busy={busy}
              ownedHint
            />
          ) : null}

          {section === 'shared' ? <SharedTable shared={shared} loading={loading} /> : null}

          {section === 'portfolios' ? <PortfoliosPlaceholder /> : null}

          {section === 'trash' ? (
            <TrashTable
              entries={trash}
              loading={busy}
              onRestore={(entry) => void restore(entry)}
              onPurge={(entry) => setPurgeTarget(entry)}
            />
          ) : null}
        </div>
      </div>

      {/* ── Modals & drawers ──────────────────────────────────────────── */}
      <CreateProjectModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={(project) => {
          setCreateOpen(false);
          navigate(`/projects/${project.id}`);
        }}
      />

      <RequestDataDialog
        open={requestOpen}
        onClose={() => setRequestOpen(false)}
        onSubmit={handleRequestSubmitted}
      />

      <ConfirmDialog
        open={deleteTarget !== null}
        title="Move project to trash"
        message={deleteTarget ? `Move "${projectName(deleteTarget)}" to trash?` : ''}
        confirmLabel="Move to trash"
        danger
        busy={busy}
        onConfirm={() => void confirmDelete()}
        onCancel={() => setDeleteTarget(null)}
      />

      <ConfirmDialog
        open={purgeTarget !== null}
        title="Permanently delete project"
        message={purgeTarget ? `Permanently delete "${purgeTarget.display_name || purgeTarget.resource_id}"?` : ''}
        confirmLabel="Delete permanently"
        danger
        busy={busy}
        onConfirm={() => void confirmPurge()}
        onCancel={() => setPurgeTarget(null)}
      />

      {trashOpen ? (
        <TrashOverlay
          trash={trash}
          loading={busy}
          onClose={() => setTrashOpen(false)}
          onRestore={(entry) => void restore(entry)}
          onPurge={(entry) => setPurgeTarget(entry)}
        />
      ) : null}
    </section>
  );
}

// ─── Sub-views ───────────────────────────────────────────────────────────────

function MenuItem({ children, onClick }: { children: React.ReactNode; onClick: () => void }) {
  return (
    <button
      type="button"
      role="menuitem"
      onClick={onClick}
      style={{
        display: 'block',
        width: '100%',
        textAlign: 'left',
        border: 0,
        background: 'transparent',
        padding: '8px 10px',
        fontSize: 12,
        color: 'var(--text-strong)',
        borderRadius: 3,
        cursor: 'pointer',
      }}
    >
      {children}
    </button>
  );
}


function ProjectsTable({
  projects,
  loading,
  search,
  onSearchChange,
  onSearchSubmit,
  onDelete,
  busy,
  ownedHint,
}: {
  projects: OntologyProject[];
  loading: boolean;
  search: string;
  onSearchChange: (next: string) => void;
  onSearchSubmit: () => void;
  onDelete: (project: OntologyProject) => void;
  busy: boolean;
  ownedHint?: boolean;
}) {
  return (
    <div style={{ display: 'grid', gap: 0 }}>
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          gap: 8,
          padding: '10px 22px',
        }}
      >
        <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
          {ownedHint ? 'Projects where you are the owner.' : 'Open a project to inspect folders, resources, and memberships.'}
        </p>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            onSearchSubmit();
          }}
          style={{ display: 'flex', gap: 6 }}
        >
          <input
            value={search}
            onChange={(event) => onSearchChange(event.target.value)}
            placeholder="Search projects"
            className="of-input"
            style={{ minWidth: 240 }}
          />
        </form>
      </div>

      {loading ? (
        <div style={{ padding: 32, textAlign: 'center' }}>
          <span className="of-text-muted">Loading projects...</span>
        </div>
      ) : (
        <table className="of-table">
          <thead>
            <tr>
              <th style={{ paddingLeft: 22 }}>Name</th>
              <th>Workspace</th>
              <th>Owner</th>
              <th>Updated</th>
              <th style={{ width: 60 }} aria-label="Actions" />
            </tr>
          </thead>
          <tbody>
            {projects.length === 0 ? (
              <tr>
                <td colSpan={5} style={{ padding: 40, textAlign: 'center' }}>
                  <span className="of-text-muted">No projects found.</span>
                </td>
              </tr>
            ) : (
              projects.map((project) => (
                <tr key={project.id}>
                  <td style={{ paddingLeft: 22 }}>
                    <div style={{ display: 'flex', gap: 12, alignItems: 'flex-start' }}>
                      <ProjectGlyphIcon />
                      <div>
                        <Link to={`/projects/${project.id}`} className="of-link">
                          {projectName(project)}
                        </Link>
                        <div className="of-text-soft" style={{ marginTop: 2, fontFamily: 'var(--font-mono)', fontSize: 10 }}>
                          {project.id} / {project.slug}
                        </div>
                        {project.description ? (
                          <div className="of-text-muted" style={{ marginTop: 4, maxWidth: 520, fontSize: 12 }}>
                            {project.description}
                          </div>
                        ) : null}
                      </div>
                    </div>
                  </td>
                  <td className="of-text-muted">{project.workspace_slug || 'default'}</td>
                  <td className="of-text-muted">{project.owner_id || '-'}</td>
                  <td className="of-text-muted">{formatDateTime(project.updated_at)}</td>
                  <td>
                    <button
                      type="button"
                      className="of-button of-button--ghost"
                      aria-label={`More actions for ${projectName(project)}`}
                      onClick={() => onDelete(project)}
                      disabled={busy}
                      style={{ padding: '4px 6px' }}
                    >
                      <MoreIcon />
                    </button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      )}
    </div>
  );
}

function SharedTable({ shared, loading }: { shared: ResourceShare[]; loading: boolean }) {
  if (loading) {
    return (
      <div style={{ padding: 32, textAlign: 'center' }}>
        <span className="of-text-muted">Loading shared resources...</span>
      </div>
    );
  }
  return (
    <table className="of-table">
      <thead>
        <tr>
          <th style={{ paddingLeft: 22 }}>Resource</th>
          <th>Access</th>
          <th>Shared by</th>
          <th>Created</th>
        </tr>
      </thead>
      <tbody>
        {shared.length === 0 ? (
          <tr>
            <td colSpan={4} style={{ padding: 40, textAlign: 'center' }}>
              <span className="of-text-muted">Nothing shared with you.</span>
            </td>
          </tr>
        ) : (
          shared.map((share) => {
            const href = sharedResourceHref(share);
            return (
              <tr key={share.id}>
                <td style={{ paddingLeft: 22 }}>
                  {href ? (
                    <Link to={href} className="of-link">
                      {share.resource_id}
                    </Link>
                  ) : (
                    <code>{share.resource_id}</code>
                  )}
                  <div className="of-text-soft" style={{ marginTop: 2, fontSize: 10 }}>
                    {formatKind(share.resource_kind)}
                  </div>
                </td>
                <td>
                  <span className="of-chip">{share.access_level}</span>
                </td>
                <td className="of-text-muted">{share.sharer_id}</td>
                <td className="of-text-muted">{formatDateTime(share.created_at)}</td>
              </tr>
            );
          })
        )}
      </tbody>
    </table>
  );
}

function PortfoliosPlaceholder() {
  return (
    <div style={{ padding: '48px 24px', display: 'grid', gap: 8, justifyItems: 'center', textAlign: 'center' }}>
      <PortfoliosIcon size={36} color="#aab4c0" />
      <p className="of-heading-sm" style={{ margin: 0 }}>
        No portfolios yet
      </p>
      <p className="of-text-muted" style={{ margin: 0, maxWidth: 460 }}>
        Portfolios group projects across business lines. Create your first portfolio from a project to start consolidating
        access, audit and reporting across teams.
      </p>
    </div>
  );
}

function TrashTable({
  entries,
  loading,
  onRestore,
  onPurge,
}: {
  entries: TrashEntry[];
  loading: boolean;
  onRestore: (entry: TrashEntry) => void;
  onPurge: (entry: TrashEntry) => void;
}) {
  if (loading && entries.length === 0) {
    return (
      <div style={{ padding: 32, textAlign: 'center' }}>
        <span className="of-text-muted">Loading trash...</span>
      </div>
    );
  }
  return (
    <table className="of-table">
      <thead>
        <tr>
          <th style={{ paddingLeft: 22 }}>Resource</th>
          <th>Deleted by</th>
          <th>Retention</th>
          <th style={{ width: 200 }}>Actions</th>
        </tr>
      </thead>
      <tbody>
        {entries.length === 0 ? (
          <tr>
            <td colSpan={4} style={{ padding: 40, textAlign: 'center' }}>
              <span className="of-text-muted">Trash is empty.</span>
            </td>
          </tr>
        ) : (
          entries.map((entry) => (
            <tr key={`${entry.resource_kind}-${entry.resource_id}`}>
              <td style={{ paddingLeft: 22 }}>
                <strong style={{ color: 'var(--text-strong)' }}>{entry.display_name || entry.resource_id}</strong>
                <div className="of-text-soft" style={{ marginTop: 2, fontFamily: 'var(--font-mono)', fontSize: 10 }}>
                  {formatKind(entry.resource_kind)} / {entry.resource_id}
                </div>
              </td>
              <td className="of-text-muted">{entry.deleted_by ?? 'unknown'}</td>
              <td className="of-text-muted">
                {formatDateTime(entry.deleted_at)}
                <div className="of-text-soft" style={{ marginTop: 2 }}>
                  {trashRetentionLabel(entry)}
                </div>
                {entry.restore_target_status === 'project_root' ? (
                  <span className="of-chip" style={{ marginTop: 4, background: 'var(--status-warning-bg)', color: 'var(--status-warning)' }}>
                    Restores to project root
                  </span>
                ) : null}
              </td>
              <td>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                  <button type="button" className="of-button" onClick={() => onRestore(entry)} style={{ fontSize: 11 }}>
                    Restore
                  </button>
                  <button
                    type="button"
                    className="of-button of-btn-danger"
                    onClick={() => onPurge(entry)}
                    style={{ fontSize: 11 }}
                  >
                    Purge
                  </button>
                </div>
              </td>
            </tr>
          ))
        )}
      </tbody>
    </table>
  );
}

function TrashOverlay({
  trash,
  loading,
  onClose,
  onRestore,
  onPurge,
}: {
  trash: TrashEntry[];
  loading: boolean;
  onClose: () => void;
  onRestore: (entry: TrashEntry) => void;
  onPurge: (entry: TrashEntry) => void;
}) {
  return (
    <div
      role="dialog"
      aria-modal="true"
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
        padding: 24,
      }}
    >
      <section
        className="of-panel"
        style={{
          width: '100%',
          maxWidth: 920,
          background: 'var(--bg-panel)',
          boxShadow: 'var(--shadow-popover)',
        }}
      >
        <header
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            gap: 12,
            padding: '12px 16px',
            borderBottom: '1px solid var(--border-default)',
          }}
        >
          <div>
            <p className="of-eyebrow" style={{ margin: 0 }}>
              Manage spaces
            </p>
            <h2 className="of-heading-md" style={{ marginTop: 4 }}>
              Trash
            </h2>
          </div>
          <button type="button" className="of-button of-button--ghost" onClick={onClose}>
            Close
          </button>
        </header>
        {loading ? (
          <div style={{ padding: 24, textAlign: 'center' }}>
            <span className="of-text-muted">Loading trash...</span>
          </div>
        ) : (
          <table className="of-table">
            <thead>
              <tr>
                <th>Resource</th>
                <th>Deleted by</th>
                <th>Retention</th>
                <th style={{ width: 200 }}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {trash.length === 0 ? (
                <tr>
                  <td colSpan={4} style={{ padding: 24, textAlign: 'center' }}>
                    <span className="of-text-muted">Trash is empty.</span>
                  </td>
                </tr>
              ) : (
                trash.map((entry) => (
                  <tr key={`${entry.resource_kind}-${entry.resource_id}`}>
                    <td>
                      <strong style={{ color: 'var(--text-strong)' }}>{entry.display_name || entry.resource_id}</strong>
                      <div className="of-text-soft" style={{ marginTop: 2, fontFamily: 'var(--font-mono)', fontSize: 10 }}>
                        {formatKind(entry.resource_kind)} / {entry.resource_id}
                      </div>
                    </td>
                    <td className="of-text-muted">{entry.deleted_by ?? 'unknown'}</td>
                    <td className="of-text-muted">
                      {formatDateTime(entry.deleted_at)}
                      <div className="of-text-soft" style={{ marginTop: 2 }}>
                        {trashRetentionLabel(entry)}
                      </div>
                      {entry.restore_target_status === 'project_root' ? (
                        <span className="of-chip" style={{ marginTop: 4, background: 'var(--status-warning-bg)', color: 'var(--status-warning)' }}>
                          Restores to project root
                        </span>
                      ) : null}
                    </td>
                    <td>
                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                        <button type="button" className="of-button" onClick={() => onRestore(entry)} style={{ fontSize: 11 }}>
                          Restore
                        </button>
                        <button
                          type="button"
                          className="of-button of-btn-danger"
                          onClick={() => onPurge(entry)}
                          style={{ fontSize: 11 }}
                        >
                          Purge
                        </button>
                      </div>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}
