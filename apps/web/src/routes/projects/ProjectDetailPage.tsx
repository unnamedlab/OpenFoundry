import { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';

import {
  bindProjectResource,
  createProjectFolder,
  deleteProject,
  deleteProjectMembership,
  getProject,
  listProjectFolders,
  listProjectMemberships,
  listProjectResources,
  listProjects,
  unbindProjectResource,
  updateProject,
  upsertProjectMembership,
  type OntologyProject,
  type OntologyProjectFolder,
  type OntologyProjectMembership,
  type OntologyProjectResourceBinding,
  type OntologyProjectRole,
} from '@/lib/api/ontology';
import {
  createFavorite,
  deleteFavorite,
  duplicateResource,
  listFavorites,
  listTrash,
  purgeResource,
  recordAccess,
  resolveResourceLabels,
  restoreResource,
  softDeleteResource,
  type ResourceKind,
  type TrashEntry,
} from '@/lib/api/workspace';
import { ConfirmDialog } from '@/lib/components/workspace/ConfirmDialog';
import { resourceRIDForKind } from '@/lib/compass/resourceTypeRegistry';
import { MoveDialog } from '@/lib/components/workspace/MoveDialog';
import { buildProjectFolderBreadcrumbItems, ProjectBreadcrumb } from '@/lib/components/workspace/ProjectBreadcrumb';
import { OpenWithMenu } from '@/lib/components/workspace/OpenWithMenu';
import { RenameDialog } from '@/lib/components/workspace/RenameDialog';
import { ResourceDetailsPanel, type ResourceSummary } from '@/lib/components/workspace/ResourceDetailsPanel';
import { ResourcePermissionsDrawer, type AccessGraphMembership } from '@/lib/components/workspace/ResourcePermissionsDrawer';
import { ShareDialog } from '@/lib/components/workspace/ShareDialog';
import { Glyph, type GlyphName } from '@/lib/components/ui/Glyph';
import { ProjectHealthSummary } from '@/lib/components/health/HealthReportsPanel';
import { ResourcePickerDialog, type ResourcePickerAction } from '@/lib/components/projects/ResourcePickerDialog';
import { UploadFilesDialog } from '@/lib/components/projects/UploadFilesDialog';

type Tab =
  | 'cover-page'
  | 'files'
  | 'autosaved'
  | 'catalog'
  | 'file-references'
  | 'external-references'
  | 'trash'
  | 'memberships';

type ResourceLabel = {
  label: string | null;
  description: string | null;
};

type ProjectResourceView = {
  binding: OntologyProjectResourceBinding;
  summary: ResourceSummary;
};

type RowItem =
  | { kind: 'folder'; folder: OntologyProjectFolder }
  | { kind: 'resource'; resource: ProjectResourceView };

type Confirmation =
  | { kind: 'project-delete' }
  | { kind: 'resource-delete'; resource: ProjectResourceView }
  | { kind: 'resource-unbind'; resource: ProjectResourceView }
  | { kind: 'trash-purge'; entry: TrashEntry }
  | { kind: 'membership-delete'; membership: OntologyProjectMembership };

const RESOURCE_KIND_OPTIONS: ResourceKind[] = [
  'dataset',
  'pipeline',
  'notebook',
  'app',
  'dashboard',
  'report',
  'model',
  'workflow',
  'other',
];

const ALL_RESOURCE_KINDS: ResourceKind[] = [
  'ontology_project',
  'ontology_folder',
  'ontology_resource_binding',
  ...RESOURCE_KIND_OPTIONS,
];

const RESOURCE_KIND_LABELS: Record<ResourceKind, string> = {
  ontology_project: 'Project',
  ontology_folder: 'Folder',
  ontology_resource_binding: 'Binding',
  dataset: 'Dataset',
  pipeline: 'Pipeline',
  notebook: 'Notebook',
  app: 'App',
  dashboard: 'Dashboard',
  report: 'Report',
  model: 'Model',
  workflow: 'Workflow',
  other: 'Other',
};

const RESOURCE_KIND_GLYPH: Record<ResourceKind, GlyphName> = {
  ontology_project: 'project',
  ontology_folder: 'folder',
  ontology_resource_binding: 'link',
  dataset: 'spreadsheet',
  pipeline: 'graph',
  notebook: 'code',
  app: 'app',
  dashboard: 'graph',
  report: 'document',
  model: 'cube',
  workflow: 'list',
  other: 'object',
};

const RESOURCE_KIND_TONE: Record<ResourceKind, string> = {
  ontology_project: '#5c7080',
  ontology_folder: '#cf923f',
  ontology_resource_binding: '#5c7080',
  dataset: '#16a34a',
  pipeline: '#7c3aed',
  notebook: '#0891b2',
  app: '#3f7be0',
  dashboard: '#ea580c',
  report: '#0891b2',
  model: '#7c3aed',
  workflow: '#0f6a32',
  other: '#5c7080',
};

const MEMBER_ROLES: OntologyProjectRole[] = ['viewer', 'editor', 'owner'];

const NAV_ACCENT = '#dbe9fb';
const NAV_ACCENT_TEXT = '#1a4f9e';
const SIDEBAR_BG = '#ffffff';
const PAGE_BG = '#f5f7fa';
const ROW_HIGHLIGHT_BG = '#1f6fd1';

function resourceKey(kind: string, id: string) {
  return `${kind}:${id}`;
}

function toResourceKind(kind: string): ResourceKind {
  return ALL_RESOURCE_KINDS.includes(kind as ResourceKind) ? (kind as ResourceKind) : 'other';
}

function toAccessGraphMemberships(memberships: OntologyProjectMembership[]): AccessGraphMembership[] {
  return memberships
    .filter((membership) => membership.role === 'viewer' || membership.role === 'editor' || membership.role === 'owner')
    .map((membership) => ({ user_id: membership.user_id, role: membership.role }));
}

function fallbackResourceName(kind: string, id: string) {
  const shortId = id.split('/').filter(Boolean).at(-1) ?? id;
  return shortId || RESOURCE_KIND_LABELS[toResourceKind(kind)];
}

function fmtDate(value: string | null | undefined) {
  if (!value) return '—';
  try {
    const date = new Date(value);
    return date.toLocaleString(undefined, {
      weekday: 'short',
      month: 'short',
      day: 'numeric',
      year: 'numeric',
      hour: 'numeric',
      minute: '2-digit',
      second: '2-digit',
    });
  } catch {
    return value;
  }
}

function matchesSearch(text: string, search: string) {
  return text.toLowerCase().includes(search.trim().toLowerCase());
}

export function ProjectDetailPage() {
  const { projectId = '' } = useParams<{ projectId: string }>();
  const navigate = useNavigate();
  const [tab, setTab] = useState<Tab>('cover-page');
  const [refsExpanded, setRefsExpanded] = useState(true);
  const [project, setProject] = useState<OntologyProject | null>(null);
  const [availableProjects, setAvailableProjects] = useState<OntologyProject[]>([]);
  const [folders, setFolders] = useState<OntologyProjectFolder[]>([]);
  const [resources, setResources] = useState<OntologyProjectResourceBinding[]>([]);
  const [memberships, setMemberships] = useState<OntologyProjectMembership[]>([]);
  const [trash, setTrash] = useState<TrashEntry[]>([]);
  const [favoriteKeys, setFavoriteKeys] = useState<Set<string>>(new Set());
  const [resourceLabels, setResourceLabels] = useState<Record<string, ResourceLabel>>({});
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  const [displayName, setDisplayName] = useState('');
  const [description, setDescription] = useState('');
  const [workspaceSlug, setWorkspaceSlug] = useState('');

  const [search, setSearch] = useState('');
  const [kindFilter, setKindFilter] = useState<ResourceKind | 'all'>('all');
  const [createdByFilter, setCreatedByFilter] = useState('');

  const [showCreateMenu, setShowCreateMenu] = useState(false);
  const [uploadOpen, setUploadOpen] = useState(false);
  const [createMode, setCreateMode] = useState<null | 'folder' | 'binding' | 'reference'>(null);

  function handleResourcePick(action: ResourcePickerAction) {
    setShowCreateMenu(false);
    if (action === 'folder') setCreateMode('folder');
    else if (action === 'upload-files') setUploadOpen(true);
    else if (action === 'bind-existing' || action === 'dataset') setCreateMode('binding');
    else if (action === 'pipeline-builder' && project) {
      navigate(`/pipelines/new?project_id=${project.id}`);
    }
  }
  const [folderName, setFolderName] = useState('');
  const [folderDescription, setFolderDescription] = useState('');
  const [folderParent, setFolderParent] = useState('');

  const [bindKind, setBindKind] = useState<ResourceKind>('dataset');
  const [bindId, setBindId] = useState('');

  const [newMemberId, setNewMemberId] = useState('');
  const [newMemberRole, setNewMemberRole] = useState<OntologyProjectRole>('viewer');
  const [membershipDraft, setMembershipDraft] = useState<OntologyProjectMembership | null>(null);

  const [highlightedKey, setHighlightedKey] = useState<string | null>(null);
  const [selectedResource, setSelectedResource] = useState<ProjectResourceView | null>(null);
  const [shareTarget, setShareTarget] = useState<ResourceSummary | null>(null);
  const [permissionsTarget, setPermissionsTarget] = useState<ResourceSummary | null>(null);
  const [moveTarget, setMoveTarget] = useState<ProjectResourceView | null>(null);
  const [renameTarget, setRenameTarget] = useState<ProjectResourceView | null>(null);
  const [confirm, setConfirm] = useState<Confirmation | null>(null);
  const [trashRowMenu, setTrashRowMenu] = useState<string | null>(null);

  async function refreshFolders(projectRid = project?.id) {
    if (!projectRid) return;
    setFolders(await listProjectFolders(projectRid));
  }

  async function refreshResources(projectRid = project?.id) {
    if (!projectRid) return;
    setResources(await listProjectResources(projectRid));
  }

  async function refreshMemberships(projectRid = project?.id) {
    if (!projectRid) return;
    setMemberships(await listProjectMemberships(projectRid));
  }

  async function refreshTrash(projectRid = project?.id) {
    if (!projectRid) return;
    const rows = await listTrash({ limit: 300 });
    setTrash(rows.filter((entry) => entry.project_id === projectRid));
  }

  async function refreshFavorites() {
    const next = await listFavorites({ limit: 500 }).catch(() => []);
    setFavoriteKeys(new Set(next.map((entry) => resourceKey(entry.resource_kind, entry.resource_id))));
  }

  async function loadAll() {
    if (!projectId) return;
    setLoading(true);
    setError('');
    try {
      const [
        nextProject,
        nextFolders,
        nextResources,
        nextMemberships,
        projectsResponse,
        nextFavorites,
        nextTrash,
      ] = await Promise.all([
        getProject(projectId),
        listProjectFolders(projectId).catch(() => [] as OntologyProjectFolder[]),
        listProjectResources(projectId).catch(() => [] as OntologyProjectResourceBinding[]),
        listProjectMemberships(projectId).catch(() => [] as OntologyProjectMembership[]),
        listProjects({ per_page: 200 }).catch(() => ({ data: [] as OntologyProject[] })),
        listFavorites({ limit: 500 }).catch(() => []),
        listTrash({ limit: 300 }).catch(() => [] as TrashEntry[]),
      ]);

      const projects = projectsResponse.data.some((entry) => entry.id === nextProject.id)
        ? projectsResponse.data
        : [nextProject, ...projectsResponse.data];

      setProject(nextProject);
      setAvailableProjects(projects);
      setFolders(nextFolders);
      setResources(nextResources);
      setMemberships(nextMemberships);
      setTrash(nextTrash.filter((entry) => entry.project_id === nextProject.id));
      setFavoriteKeys(new Set(nextFavorites.map((entry) => resourceKey(entry.resource_kind, entry.resource_id))));
      setDisplayName(nextProject.display_name);
      setDescription(nextProject.description);
      setWorkspaceSlug(nextProject.workspace_slug ?? '');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load project');
      setProject(null);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void loadAll();
  }, [projectId]);

  useEffect(() => {
    if (!projectId) return;
    recordAccess({ resource_kind: 'ontology_project', resource_id: projectId }).catch(() => {});
  }, [projectId]);

  useEffect(() => {
    if (resources.length === 0) {
      setResourceLabels({});
      return;
    }

    let cancelled = false;
    resolveResourceLabels(resources.map((entry) => ({
      resource_kind: toResourceKind(entry.resource_kind),
      resource_id: entry.resource_id,
    })))
      .then((response) => {
        if (cancelled) return;
        const next: Record<string, ResourceLabel> = {};
        for (const entry of response.data) {
          next[resourceKey(entry.resource_kind, entry.resource_id)] = {
            label: entry.label,
            description: entry.description,
          };
        }
        setResourceLabels(next);
      })
      .catch(() => {
        if (!cancelled) setResourceLabels({});
      });

    return () => {
      cancelled = true;
    };
  }, [resources]);

  useEffect(() => {
    setHighlightedKey(null);
  }, [tab]);

  const projectIsFavorite = useMemo(() => (
    project ? favoriteKeys.has(resourceKey('ontology_project', project.id)) : false
  ), [favoriteKeys, project]);

  const projectResources = useMemo<ProjectResourceView[]>(() => {
    return resources.map((binding) => {
      const kind = toResourceKind(binding.resource_kind);
      const label = resourceLabels[resourceKey(kind, binding.resource_id)];
      return {
        binding,
        summary: {
          id: binding.resource_id,
          rid: resourceRIDForKind(kind, binding.resource_id),
          kind,
          name: label?.label || fallbackResourceName(binding.resource_kind, binding.resource_id),
          description: label?.description,
          owner_id: binding.bound_by,
          location: project ? `/${project.display_name || project.slug}` : undefined,
          project_id: project?.id,
          project_rid: project ? resourceRIDForKind('ontology_project', project.id) : null,
          created_at: binding.created_at,
          updated_at: binding.created_at,
        },
      };
    });
  }, [project, resourceLabels, resources]);

  const projectHealthResourceRids = useMemo(() => (
    [
      project?.id,
      ...projectResources.map((entry) => entry.summary.id),
    ].filter((value): value is string => Boolean(value))
  ), [project?.id, projectResources]);

  const filteredResources = useMemo(() => {
    return projectResources.filter((entry) => {
      if (kindFilter !== 'all' && entry.summary.kind !== kindFilter) return false;
      if (createdByFilter && !matchesSearch(entry.binding.bound_by, createdByFilter)) return false;
      if (search.trim()) {
        const haystack = [
          entry.summary.name,
          entry.summary.id,
          entry.summary.kind,
          entry.summary.description ?? '',
          entry.binding.bound_by,
        ].join(' ');
        if (!matchesSearch(haystack, search)) return false;
      }
      return true;
    });
  }, [projectResources, kindFilter, createdByFilter, search]);

  const fileRows = useMemo<RowItem[]>(() => {
    const rootFolders = folders.filter((folder) => folder.parent_folder_id === null);
    const folderRows: RowItem[] = rootFolders.map((folder) => ({ kind: 'folder', folder }));
    const resourceRows: RowItem[] = filteredResources.map((resource) => ({ kind: 'resource', resource }));
    const filteredFolders = search.trim()
      ? folderRows.filter((row) => row.kind === 'folder' && matchesSearch(`${row.folder.name} ${row.folder.description ?? ''}`, search))
      : folderRows;
    return [...filteredFolders, ...resourceRows];
  }, [folders, filteredResources, search]);

  const highlightedRow = useMemo<ProjectResourceView | null>(() => {
    if (!highlightedKey) return null;
    return projectResources.find((entry) => resourceKey(entry.binding.resource_kind, entry.binding.resource_id) === highlightedKey) ?? null;
  }, [highlightedKey, projectResources]);

  const accessGraphMemberships = useMemo(() => toAccessGraphMemberships(memberships), [memberships]);

  const breadcrumbItems = useMemo(
    () => (project ? buildProjectFolderBreadcrumbItems(project) : []),
    [project],
  );

  const projectShareSummary: ResourceSummary | null = project
    ? {
        id: project.id,
        rid: resourceRIDForKind('ontology_project', project.id),
        kind: 'ontology_project',
        name: project.display_name || project.slug,
        description: project.description,
        owner_id: project.owner_id,
        location: project.workspace_slug ?? undefined,
        project_id: project.id,
        project_rid: resourceRIDForKind('ontology_project', project.id),
        created_at: project.created_at,
        updated_at: project.updated_at,
      }
    : null;

  async function toggleProjectFavorite() {
    if (!project) return;
    const key = resourceKey('ontology_project', project.id);
    try {
      if (favoriteKeys.has(key)) {
        await deleteFavorite('ontology_project', project.id);
      } else {
        await createFavorite({ resource_kind: 'ontology_project', resource_id: project.id });
      }
      await refreshFavorites();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Favorite update failed');
    }
  }

  async function toggleResourceFavorite(resource: ProjectResourceView) {
    const key = resourceKey(resource.summary.kind, resource.summary.id);
    try {
      if (favoriteKeys.has(key)) {
        await deleteFavorite(resource.summary.kind, resource.summary.id);
      } else {
        await createFavorite({ resource_kind: resource.summary.kind, resource_id: resource.summary.id });
      }
      await refreshFavorites();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Favorite update failed');
    }
  }

  async function save() {
    if (!project) return;
    setBusy(true);
    setError('');
    try {
      const updated = await updateProject(project.id, {
        display_name: displayName,
        description,
        workspace_slug: workspaceSlug || null,
      });
      setProject(updated);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Save failed');
    } finally {
      setBusy(false);
    }
  }

  async function addFolder() {
    if (!project || !folderName.trim()) return;
    setBusy(true);
    setError('');
    try {
      await createProjectFolder(project.id, {
        name: folderName.trim(),
        description: folderDescription.trim() || undefined,
        parent_folder_id: folderParent || null,
      });
      setFolderName('');
      setFolderDescription('');
      setFolderParent('');
      setCreateMode(null);
      await refreshFolders(project.id);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Folder create failed');
    } finally {
      setBusy(false);
    }
  }

  async function bind() {
    if (!project || !bindId.trim()) return;
    setBusy(true);
    setError('');
    try {
      await bindProjectResource(project.id, {
        resource_kind: bindKind,
        resource_id: bindId.trim(),
      });
      setBindId('');
      setCreateMode(null);
      await refreshResources(project.id);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Bind failed');
    } finally {
      setBusy(false);
    }
  }

  async function addMembership() {
    if (!project || !newMemberId.trim()) return;
    setBusy(true);
    setError('');
    try {
      await upsertProjectMembership(project.id, {
        user_id: newMemberId.trim(),
        role: newMemberRole,
      });
      setNewMemberId('');
      setNewMemberRole('viewer');
      await refreshMemberships(project.id);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Membership update failed');
    } finally {
      setBusy(false);
    }
  }

  async function saveMembershipDraft() {
    if (!project || !membershipDraft) return;
    setBusy(true);
    setError('');
    try {
      await upsertProjectMembership(project.id, {
        user_id: membershipDraft.user_id,
        role: membershipDraft.role,
      });
      setMembershipDraft(null);
      await refreshMemberships(project.id);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Membership update failed');
    } finally {
      setBusy(false);
    }
  }

  function openResource(resource: ProjectResourceView) {
    setSelectedResource(resource);
    recordAccess({ resource_kind: resource.summary.kind, resource_id: resource.summary.id }).catch(() => {});
  }

  async function copyResourceLink(resource: ProjectResourceView) {
    if (typeof navigator === 'undefined' || !navigator.clipboard) return;
    const url = `${window.location.origin}${window.location.pathname}?resource=${encodeURIComponent(resourceKey(resource.summary.kind, resource.summary.id))}`;
    try {
      await navigator.clipboard.writeText(url);
    } catch {
      /* ignore */
    }
  }

  async function duplicate(resource: ProjectResourceView) {
    setBusy(true);
    setError('');
    try {
      await duplicateResource(resource.summary.kind, resource.summary.id, {
        target_folder_id: null,
      });
      await refreshResources(project?.id);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Duplicate failed');
    } finally {
      setBusy(false);
    }
  }

  async function restoreTrashEntry(entry: TrashEntry) {
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const result = await restoreResource(entry.resource_kind, entry.resource_id);
      if (result.banner) setNotice(result.banner);
      else setNotice(`${entry.display_name || entry.resource_id} restored.`);
      await Promise.all([refreshTrash(), refreshResources()]);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Restore failed');
    } finally {
      setBusy(false);
    }
  }

  function confirmationCopy() {
    if (!confirm) return null;
    if (confirm.kind === 'project-delete') {
      return {
        title: 'Move project to trash',
        message: `Move ${project?.display_name || project?.slug || 'this project'} to trash?`,
        confirmLabel: 'Delete project',
        danger: true,
      };
    }
    if (confirm.kind === 'resource-delete') {
      return {
        title: 'Delete resource',
        message: `Move ${confirm.resource.summary.name} to trash?`,
        confirmLabel: 'Delete',
        danger: true,
      };
    }
    if (confirm.kind === 'resource-unbind') {
      return {
        title: 'Unbind resource',
        message: `Remove ${confirm.resource.summary.name} from this project without deleting the underlying resource?`,
        confirmLabel: 'Unbind',
        danger: false,
      };
    }
    if (confirm.kind === 'trash-purge') {
      return {
        title: 'Permanently delete',
        message: `Permanently delete ${confirm.entry.display_name || confirm.entry.resource_id}?`,
        confirmLabel: 'Purge',
        danger: true,
      };
    }
    return {
      title: 'Remove member',
      message: `Remove ${confirm.membership.user_id} from this project?`,
      confirmLabel: 'Remove',
      danger: true,
    };
  }

  async function runConfirmed() {
    if (!project || !confirm) return;
    setBusy(true);
    setError('');
    try {
      if (confirm.kind === 'project-delete') {
        await deleteProject(project.id);
        navigate('/projects');
        return;
      }
      if (confirm.kind === 'resource-delete') {
        await softDeleteResource(confirm.resource.summary.kind, confirm.resource.summary.id);
        if (selectedResource?.summary.kind === confirm.resource.summary.kind
          && selectedResource.summary.id === confirm.resource.summary.id) {
          setSelectedResource(null);
        }
        setHighlightedKey(null);
        await Promise.all([refreshResources(project.id), refreshTrash(project.id)]);
      }
      if (confirm.kind === 'resource-unbind') {
        await unbindProjectResource(
          project.id,
          confirm.resource.binding.resource_kind,
          confirm.resource.binding.resource_id,
        );
        if (selectedResource?.summary.kind === confirm.resource.summary.kind
          && selectedResource.summary.id === confirm.resource.summary.id) {
          setSelectedResource(null);
        }
        setHighlightedKey(null);
        await refreshResources(project.id);
      }
      if (confirm.kind === 'trash-purge') {
        await purgeResource(confirm.entry.resource_kind, confirm.entry.resource_id);
        await refreshTrash(project.id);
      }
      if (confirm.kind === 'membership-delete') {
        await deleteProjectMembership(project.id, confirm.membership.user_id);
        await refreshMemberships(project.id);
      }
      setConfirm(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Action failed');
    } finally {
      setBusy(false);
    }
  }

  const confirmDialog = confirmationCopy();

  if (loading) {
    return (
      <section className="of-page" style={{ padding: 24 }}>
        <p className="of-text-muted">Loading project…</p>
      </section>
    );
  }

  if (!project) {
    return (
      <section className="of-page" style={{ padding: 24 }}>
        <Link to="/projects" style={{ color: 'var(--text-muted)', fontSize: 13 }}>← Projects</Link>
        <p className="of-status-danger" style={{ marginTop: 12, padding: 10 }}>
          {error || 'Project not found'}
        </p>
      </section>
    );
  }

  const tabContext: 'files' | 'references' | 'catalog' | 'trash' | 'cover' | 'members' = (() => {
    if (tab === 'cover-page') return 'cover';
    if (tab === 'files' || tab === 'autosaved') return 'files';
    if (tab === 'catalog') return 'catalog';
    if (tab === 'file-references' || tab === 'external-references') return 'references';
    if (tab === 'memberships') return 'members';
    return 'trash';
  })();

  return (
    <section
      className="of-page"
      style={{
        padding: 0,
        background: PAGE_BG,
        display: 'flex',
        flexDirection: 'column',
        minHeight: 'calc(100vh - 56px)',
      }}
    >
      <ProjectTopTabs />

      <header
        style={{
          background: '#fff',
          borderBottom: '1px solid var(--border-default)',
          padding: '14px 24px 14px 24px',
          display: 'flex',
          gap: 16,
          alignItems: 'flex-start',
          justifyContent: 'space-between',
        }}
      >
        <div style={{ minWidth: 0, flex: '1 1 auto' }}>
          <ProjectBreadcrumb items={breadcrumbItems} />
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <span style={{
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              width: 28,
              height: 28,
              border: '1px solid #c5cdd9',
              borderRadius: 4,
              background: '#f3f5f7',
            }}>
              <Glyph name="cover-page" size={16} tone="#5c7080" />
            </span>
            <h1 style={{
              margin: 0,
              fontSize: 22,
              fontWeight: 600,
              color: '#1c2127',
              letterSpacing: '-0.01em',
            }}>
              {project.display_name || project.slug}
            </h1>
            <button
              type="button"
              aria-label={projectIsFavorite ? 'Remove from favorites' : 'Add to favorites'}
              onClick={() => void toggleProjectFavorite()}
              style={{
                background: 'transparent',
                border: 'none',
                cursor: 'pointer',
                padding: 2,
                color: projectIsFavorite ? '#f0b323' : '#8a96a6',
                display: 'inline-flex',
                alignItems: 'center',
              }}
            >
              <Glyph
                name={projectIsFavorite ? 'star-filled' : 'star'}
                size={18}
                tone={projectIsFavorite ? '#f0b323' : '#8a96a6'}
              />
            </button>
          </div>
          <p style={{ margin: '4px 0 0 38px', fontSize: 13, color: '#5c7080' }}>
            {project.description || 'No description.'}
          </p>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: 6, position: 'relative' }}>
          {tabContext === 'references' && (
            <button
              type="button"
              className="of-button of-button--primary"
              onClick={() => setCreateMode('reference')}
            >
              <Glyph name="plus" size={13} />
              Add reference
            </button>
          )}
          {(tabContext === 'files' || tabContext === 'cover') && (
            <button
              type="button"
              className="of-button of-button--success"
              onClick={() => setShowCreateMenu((value) => !value)}
            >
              <Glyph name="plus" size={13} />
              New
              <Glyph name="chevron-down" size={11} />
            </button>
          )}
          {tabContext === 'members' && (
            <button
              type="button"
              className="of-button of-button--primary"
              onClick={() => setCreateMode('binding')}
            >
              <Glyph name="add-user" size={14} />
              Add member
            </button>
          )}
          <button
            type="button"
            className="of-button of-button--ghost"
            aria-label="Toggle list view"
            style={{
              padding: 4,
              minHeight: 30,
              width: 30,
              border: '1px solid #c5cdd9',
              borderRadius: 4,
            }}
          >
            <Glyph name="view-grid" size={16} tone="#5c7080" />
          </button>
          <button
            type="button"
            onClick={() => projectShareSummary && setShareTarget(projectShareSummary)}
            className="of-button"
            aria-label="Share project"
          >
            <Glyph name="users" size={14} />
            Share
          </button>
          <OpenWithMenu
            resourceKind="ontology_project"
            resourceId={project.id}
            resourceRid={projectShareSummary?.rid}
            projectId={project.id}
            projectRid={projectShareSummary?.project_rid}
          />
          <button
            type="button"
            onClick={() => setConfirm({ kind: 'project-delete' })}
            disabled={busy}
            className="of-button"
            style={{ color: 'var(--status-danger)', borderColor: '#d6a9a9' }}
            aria-label="Delete project"
          >
            <Glyph name="trash" size={14} />
          </button>

          <ResourcePickerDialog
            open={showCreateMenu}
            onClose={() => setShowCreateMenu(false)}
            onPick={handleResourcePick}
          />

          <UploadFilesDialog
            open={uploadOpen}
            projectId={project?.id ?? null}
            onClose={() => setUploadOpen(false)}
            onUploaded={() => {
              if (project) void refreshResources(project.id);
            }}
          />
        </div>
      </header>

      {error && (
        <div className="of-status-danger" style={{ padding: '10px 24px', fontSize: 13 }}>
          {error}
        </div>
      )}
      {notice && (
        <div className="of-status-info" style={{ padding: '10px 24px', fontSize: 13 }}>
          {notice}
        </div>
      )}

      <div style={{ display: 'flex', flex: '1 1 auto', minHeight: 0 }}>
        <aside
          style={{
            width: 260,
            flex: '0 0 260px',
            background: SIDEBAR_BG,
            borderRight: '1px solid var(--border-default)',
            display: 'flex',
            flexDirection: 'column',
            padding: '14px 8px 0 8px',
          }}
        >
          <SidebarSectionHeader
            title="Preview"
            subtitle="Visible to others"
            count={1}
            badge={<Glyph name="eye" size={14} tone="#5c7080" />}
          />
          <SidebarItem
            id="cover-page"
            label="Cover page"
            icon="cover-page"
            active={tab === 'cover-page'}
            onClick={() => setTab('cover-page')}
          />

          <SidebarSectionHeader
            title="Project workspace"
            subtitle="Members only"
            count={1}
            style={{ marginTop: 14 }}
          />
          <SidebarItem
            id="files"
            label="Files"
            icon="folder"
            iconTone="#cf923f"
            active={tab === 'files'}
            onClick={() => setTab('files')}
          />
          <SidebarItem
            id="autosaved"
            label="Autosaved"
            icon="autosaved"
            active={tab === 'autosaved'}
            onClick={() => setTab('autosaved')}
          />
          <SidebarItem
            id="catalog"
            label="Project Catalog"
            icon="badge-check"
            iconTone="#0f6a32"
            active={tab === 'catalog'}
            onClick={() => setTab('catalog')}
          />
          <SidebarItem
            id="references"
            label="References"
            icon="asterisk"
            iconTone="#5c7080"
            active={tab === 'file-references' || tab === 'external-references'}
            expandable
            expanded={refsExpanded}
            onToggleExpand={() => setRefsExpanded((value) => !value)}
            trailing={(
              <span title="References point to data outside this project" style={{ display: 'inline-flex' }}>
                <span style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  width: 16,
                  height: 16,
                  borderRadius: 999,
                  background: '#5c7080',
                  color: '#fff',
                  fontSize: 10,
                  fontWeight: 700,
                }}>i</span>
              </span>
            )}
          />
          {refsExpanded && (
            <>
              <SidebarSubItem
                label="File references"
                active={tab === 'file-references'}
                onClick={() => setTab('file-references')}
              />
              <SidebarSubItem
                label="External references"
                active={tab === 'external-references'}
                onClick={() => setTab('external-references')}
              />
            </>
          )}
          <SidebarItem
            id="trash"
            label="Trash"
            icon="trash"
            iconTone="#5c7080"
            active={tab === 'trash'}
            onClick={() => setTab('trash')}
          />
          <SidebarItem
            id="memberships"
            label="Memberships"
            icon="users"
            iconTone="#5c7080"
            active={tab === 'memberships'}
            onClick={() => setTab('memberships')}
          />

          <div style={{ height: 1, background: 'var(--border-subtle)', margin: '14px 4px' }} />

          <SidebarItem
            id="usage"
            label="Project usage"
            icon="pie-chart"
            iconTone="#5c7080"
            external
            onClick={() => navigate('/control-panel/data-health')}
          />
          <SidebarItem
            id="access-graph"
            label="Access graph"
            icon="graph"
            iconTone="#5c7080"
            external
            onClick={() => setPermissionsTarget(projectShareSummary)}
          />

          <div style={{ marginTop: 'auto', padding: '14px 6px 14px 6px' }}>
            <button
              type="button"
              style={{
                width: '100%',
                display: 'inline-flex',
                alignItems: 'center',
                justifyContent: 'center',
                gap: 8,
                padding: '8px 12px',
                background: 'transparent',
                border: 'none',
                color: '#3f7be0',
                fontWeight: 600,
                fontSize: 13,
                cursor: 'pointer',
                borderRadius: 4,
              }}
            >
              <Glyph name="sparkles" size={14} tone="#3f7be0" />
              Take a project tour
            </button>
          </div>
        </aside>

        <main style={{ flex: '1 1 auto', minWidth: 0, background: '#fff', display: 'flex', flexDirection: 'column' }}>
          {tab === 'cover-page' && (
            <div style={{ padding: '20px 24px', display: 'grid', gap: 14 }}>
              <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
                <div>
                  <p className="of-eyebrow">Project metadata</p>
                  <p className="of-text-muted" style={{ marginTop: 4, fontSize: 12 }}>
                    Owner <code>{project.owner_id}</code> · Created {fmtDate(project.created_at)} · Updated {fmtDate(project.updated_at)}
                  </p>
                </div>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 10 }}>
                  <label style={{ fontSize: 12 }}>
                    Display name
                    <input
                      value={displayName}
                      onChange={(event) => setDisplayName(event.target.value)}
                      className="of-input"
                      style={{ marginTop: 4 }}
                    />
                  </label>
                  <label style={{ fontSize: 12 }}>
                    Workspace slug
                    <input
                      value={workspaceSlug}
                      onChange={(event) => setWorkspaceSlug(event.target.value)}
                      className="of-input"
                      style={{ marginTop: 4 }}
                    />
                  </label>
                  <div style={{ display: 'flex', alignItems: 'end' }}>
                    <button
                      type="button"
                      onClick={() => void save()}
                      disabled={busy}
                      className="of-button of-button--primary"
                    >
                      {busy ? 'Saving…' : 'Save changes'}
                    </button>
                  </div>
                </div>
                <label style={{ fontSize: 12 }}>
                  Description
                  <textarea
                    value={description}
                    onChange={(event) => setDescription(event.target.value)}
                    rows={4}
                    className="of-input"
                    style={{ marginTop: 4, minHeight: 96, resize: 'vertical' }}
                  />
                </label>
              </section>

              <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 10 }}>
                <p className="of-eyebrow">Snapshot</p>
                <div style={{
                  display: 'grid',
                  gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))',
                  gap: 12,
                }}>
                  <SnapshotCard label="Folders" value={folders.length} icon="folder" tone="#cf923f" />
                  <SnapshotCard label="Resources" value={resources.length} icon="cube" tone="#3f7be0" />
                  <SnapshotCard label="Members" value={memberships.length} icon="users" tone="#7c3aed" />
                  <SnapshotCard label="Trash" value={trash.length} icon="trash" tone="#cb2431" />
                </div>
              </section>

              <ProjectHealthSummary
                resourceRids={projectHealthResourceRids}
                title={`${project.display_name || project.slug} health`}
              />
            </div>
          )}

          {(tab === 'files' || tab === 'autosaved') && (
            <FilesView
              tabKey={tab}
              project={project}
              folders={folders}
              fileRows={tab === 'autosaved' ? [] : fileRows}
              search={search}
              onSearchChange={setSearch}
              kindFilter={kindFilter}
              onKindFilterChange={setKindFilter}
              createdByFilter={createdByFilter}
              onCreatedByFilterChange={setCreatedByFilter}
              highlighted={highlightedRow}
              onHighlight={(row) => {
                if (!row) return setHighlightedKey(null);
                setHighlightedKey(resourceKey(row.binding.resource_kind, row.binding.resource_id));
              }}
              onClearHighlight={() => setHighlightedKey(null)}
              onOpenResource={openResource}
              onShare={(row) => setShareTarget(row.summary)}
              onPermissions={(row) => setPermissionsTarget(row.summary)}
              onMove={(row) => setMoveTarget(row)}
              onRename={(row) => setRenameTarget(row)}
              onDelete={(row) => setConfirm({ kind: 'resource-delete', resource: row })}
              onUnbind={(row) => setConfirm({ kind: 'resource-unbind', resource: row })}
              onCopyLink={(row) => void copyResourceLink(row)}
              onDuplicate={(row) => void duplicate(row)}
              favoriteKeys={favoriteKeys}
              onToggleFavorite={(row) => void toggleResourceFavorite(row)}
            />
          )}

          {tab === 'catalog' && (
            <PlaceholderView
              icon="badge-check"
              title="Project Catalog"
              message="Curated catalog entries for this project will appear here. Mark resources as catalog-ready to publish them to consumers without granting access to the underlying project workspace."
            />
          )}

          {tab === 'file-references' && (
            <PlaceholderView
              icon="link"
              title="File references"
              message="Inbound references from datasets, notebooks and apps in other projects appear here. Use the Add reference action above to include a resource hosted in another project without copying it."
              cta={{ label: 'Add reference', onClick: () => setCreateMode('reference') }}
            />
          )}

          {tab === 'external-references' && (
            <PlaceholderView
              icon="external-link"
              title="External references"
              message="External references reach back into other Foundry-compatible domains and include datasets, dashboards or apps that consume this project's outputs."
            />
          )}

          {tab === 'trash' && (
            <TrashView
              entries={trash}
              busy={busy}
              menuOpenForKey={trashRowMenu}
              onToggleMenu={(key) => setTrashRowMenu((value) => (value === key ? null : key))}
              onRestore={(entry) => { setTrashRowMenu(null); void restoreTrashEntry(entry); }}
              onPurge={(entry) => { setTrashRowMenu(null); setConfirm({ kind: 'trash-purge', entry }); }}
            />
          )}

          {tab === 'memberships' && (
            <MembershipsView
              project={project}
              memberships={memberships}
              busy={busy}
              newMemberId={newMemberId}
              newMemberRole={newMemberRole}
              onNewMemberIdChange={setNewMemberId}
              onNewMemberRoleChange={setNewMemberRole}
              onAdd={() => void addMembership()}
              onEdit={(membership) => setMembershipDraft(membership)}
              onRemove={(membership) => setConfirm({ kind: 'membership-delete', membership })}
            />
          )}
        </main>
      </div>

      <ResourceDetailsPanel
        open={Boolean(selectedResource)}
        resource={selectedResource?.summary ?? null}
        isFavorite={selectedResource ? favoriteKeys.has(resourceKey(selectedResource.summary.kind, selectedResource.summary.id)) : false}
        projectLabel={project.display_name || project.slug}
        projectMemberships={accessGraphMemberships}
        onClose={() => setSelectedResource(null)}
        onFavoriteToggle={(next) => {
          if (!selectedResource) return;
          const key = resourceKey(selectedResource.summary.kind, selectedResource.summary.id);
          setFavoriteKeys((previous) => {
            const updated = new Set(previous);
            if (next) updated.add(key);
            else updated.delete(key);
            return updated;
          });
          if (next) {
            createFavorite({ resource_kind: selectedResource.summary.kind, resource_id: selectedResource.summary.id }).catch(() => {});
          } else {
            deleteFavorite(selectedResource.summary.kind, selectedResource.summary.id).catch(() => {});
          }
        }}
      />

      <ResourcePermissionsDrawer
        open={Boolean(permissionsTarget)}
        resourceKind={permissionsTarget?.kind ?? null}
        resourceId={permissionsTarget?.id ?? null}
        resourceLabel={permissionsTarget?.name}
        ownerId={permissionsTarget?.owner_id}
        projectLabel={project.display_name || project.slug}
        projectMemberships={accessGraphMemberships}
        onClose={() => setPermissionsTarget(null)}
      />

      <ShareDialog
        open={Boolean(shareTarget)}
        resourceKind={shareTarget?.kind ?? null}
        resourceId={shareTarget?.id ?? null}
        resourceLabel={shareTarget?.name}
        onClose={() => setShareTarget(null)}
        onShared={() => setShareTarget(null)}
      />

      <MoveDialog
        open={Boolean(moveTarget)}
        resourceKind={moveTarget?.summary.kind ?? null}
        resourceId={moveTarget?.summary.id ?? null}
        resourceLabel={moveTarget?.summary.name}
        projects={availableProjects}
        initialProjectId={project.id}
        onClose={() => setMoveTarget(null)}
        onMoved={() => {
          setMoveTarget(null);
          void Promise.all([refreshResources(project.id), refreshTrash(project.id)]);
        }}
      />

      <RenameDialog
        open={Boolean(renameTarget)}
        resourceKind={renameTarget?.summary.kind ?? null}
        resourceId={renameTarget?.summary.id ?? null}
        currentName={renameTarget?.summary.name ?? ''}
        onClose={() => setRenameTarget(null)}
        onRenamed={() => {
          setRenameTarget(null);
          void refreshResources(project.id);
        }}
      />

      {createMode === 'folder' && (
        <CompactDialog
          title="New folder"
          onCancel={() => !busy && setCreateMode(null)}
          onSubmit={() => void addFolder()}
          submitLabel={busy ? 'Creating…' : 'Create folder'}
          submitDisabled={busy || !folderName.trim()}
        >
          <label style={{ fontSize: 12 }}>
            Name
            <input
              value={folderName}
              onChange={(event) => setFolderName(event.target.value)}
              className="of-input"
              autoFocus
              style={{ marginTop: 4 }}
            />
          </label>
          <label style={{ fontSize: 12 }}>
            Parent folder
            <select
              value={folderParent}
              onChange={(event) => setFolderParent(event.target.value)}
              className="of-input"
              style={{ marginTop: 4 }}
            >
              <option value="">root</option>
              {folders.map((folder) => (
                <option key={folder.id} value={folder.id}>{folder.name}</option>
              ))}
            </select>
          </label>
          <label style={{ fontSize: 12 }}>
            Description
            <input
              value={folderDescription}
              onChange={(event) => setFolderDescription(event.target.value)}
              className="of-input"
              style={{ marginTop: 4 }}
            />
          </label>
        </CompactDialog>
      )}

      {(createMode === 'binding' || createMode === 'reference') && (
        <CompactDialog
          title={createMode === 'reference' ? 'Add reference' : 'Bind resource'}
          onCancel={() => !busy && setCreateMode(null)}
          onSubmit={() => void bind()}
          submitLabel={busy ? 'Saving…' : createMode === 'reference' ? 'Add reference' : 'Bind'}
          submitDisabled={busy || !bindId.trim()}
        >
          <label style={{ fontSize: 12 }}>
            Resource kind
            <select
              value={bindKind}
              onChange={(event) => setBindKind(event.target.value as ResourceKind)}
              className="of-input"
              style={{ marginTop: 4 }}
            >
              {RESOURCE_KIND_OPTIONS.map((kind) => (
                <option key={kind} value={kind}>{RESOURCE_KIND_LABELS[kind]}</option>
              ))}
            </select>
          </label>
          <label style={{ fontSize: 12 }}>
            Resource id
            <input
              value={bindId}
              onChange={(event) => setBindId(event.target.value)}
              placeholder="dataset RID, pipeline id, …"
              className="of-input"
              autoFocus
              style={{ marginTop: 4 }}
            />
          </label>
        </CompactDialog>
      )}

      {membershipDraft && (
        <CompactDialog
          title="Edit membership"
          onCancel={() => !busy && setMembershipDraft(null)}
          onSubmit={() => void saveMembershipDraft()}
          submitLabel={busy ? 'Saving…' : 'Save'}
          submitDisabled={busy}
        >
          <label style={{ fontSize: 12 }}>
            User
            <input value={membershipDraft.user_id} readOnly className="of-input" style={{ marginTop: 4 }} />
          </label>
          <label style={{ fontSize: 12 }}>
            Role
            <select
              value={membershipDraft.role}
              onChange={(event) => setMembershipDraft({ ...membershipDraft, role: event.target.value as OntologyProjectRole })}
              className="of-input"
              style={{ marginTop: 4 }}
            >
              {MEMBER_ROLES.map((role) => (
                <option key={role} value={role}>{role}</option>
              ))}
            </select>
          </label>
        </CompactDialog>
      )}

      {confirmDialog && (
        <ConfirmDialog
          open={Boolean(confirm)}
          title={confirmDialog.title}
          message={confirmDialog.message}
          confirmLabel={confirmDialog.confirmLabel}
          danger={confirmDialog.danger}
          busy={busy}
          onCancel={() => setConfirm(null)}
          onConfirm={() => void runConfirmed()}
        />
      )}
    </section>
  );
}

function ProjectTopTabs() {
  type TopTab = { id: 'data-catalog' | 'projects' | 'your-files' | 'shared'; label: string; icon: GlyphName };
  const tabs: TopTab[] = [
    { id: 'data-catalog', label: 'Data Catalog', icon: 'badge-check' },
    { id: 'projects', label: 'Projects', icon: 'project' },
    { id: 'your-files', label: 'Your files', icon: 'document' },
    { id: 'shared', label: 'Shared with you', icon: 'users' },
  ];
  return (
    <div
      style={{
        background: '#eef2f6',
        display: 'flex',
        alignItems: 'stretch',
        padding: '0 12px',
        borderBottom: '1px solid var(--border-default)',
      }}
    >
      {tabs.map((entry) => {
        const active = entry.id === 'projects';
        return (
          <Link
            key={entry.id}
            to={entry.id === 'projects' ? '/projects' : entry.id === 'your-files' ? '/' : entry.id === 'shared' ? '/?shared=1' : '/datasets'}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 8,
              padding: '12px 16px',
              fontSize: 14,
              fontWeight: 500,
              color: active ? '#1c2127' : '#5c7080',
              background: active ? '#fff' : 'transparent',
              borderTop: active ? '2px solid #2d72d2' : '2px solid transparent',
              borderRight: active ? '1px solid var(--border-default)' : 'none',
              borderLeft: active ? '1px solid var(--border-default)' : 'none',
              textDecoration: 'none',
              marginBottom: -1,
            }}
          >
            <Glyph name={entry.icon} size={16} tone={active ? '#2d72d2' : '#5c7080'} />
            {entry.label}
          </Link>
        );
      })}
    </div>
  );
}

function SidebarSectionHeader({
  title,
  subtitle,
  count,
  badge,
  style,
}: {
  title: string;
  subtitle?: string;
  count?: number;
  badge?: React.ReactNode;
  style?: React.CSSProperties;
}) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'flex-start',
        justifyContent: 'space-between',
        padding: '4px 10px 8px 10px',
        ...style,
      }}
    >
      <div>
        <div style={{ fontSize: 13, fontWeight: 700, color: '#1c2127' }}>{title}</div>
        {subtitle && (
          <div style={{ fontSize: 11, color: '#8a96a6', marginTop: 1 }}>{subtitle}</div>
        )}
      </div>
      <div style={{ display: 'inline-flex', alignItems: 'center', gap: 4, color: '#5c7080', fontSize: 12 }}>
        {badge}
        <Glyph name="project" size={12} tone="#5c7080" />
        {typeof count === 'number' && <span>{count}</span>}
      </div>
    </div>
  );
}

function SidebarItem({
  label,
  icon,
  iconTone,
  active,
  external,
  expandable,
  expanded,
  onToggleExpand,
  trailing,
  onClick,
}: {
  id: string;
  label: string;
  icon?: GlyphName;
  iconTone?: string;
  active?: boolean;
  external?: boolean;
  expandable?: boolean;
  expanded?: boolean;
  onToggleExpand?: () => void;
  trailing?: React.ReactNode;
  onClick?: () => void;
}) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 4,
        margin: '0 0 2px',
      }}
    >
      <button
        type="button"
        onClick={onClick}
        style={{
          flex: 1,
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          minHeight: 30,
          padding: '0 10px',
          border: 'none',
          borderRadius: 4,
          background: active ? NAV_ACCENT : 'transparent',
          color: active ? NAV_ACCENT_TEXT : '#1c2127',
          fontSize: 13,
          fontWeight: active ? 600 : 500,
          textAlign: 'left',
          cursor: 'pointer',
        }}
      >
        {icon && <Glyph name={icon} size={15} tone={active ? NAV_ACCENT_TEXT : iconTone ?? '#5c7080'} />}
        <span style={{ flex: 1 }}>{label}</span>
        {trailing}
        {external && <Glyph name="external-link" size={13} tone="#8a96a6" />}
      </button>
      {expandable && (
        <button
          type="button"
          aria-label={expanded ? 'Collapse' : 'Expand'}
          onClick={onToggleExpand}
          style={{
            background: 'transparent',
            border: 'none',
            cursor: 'pointer',
            color: '#5c7080',
            padding: 4,
          }}
        >
          <Glyph name={expanded ? 'chevron-down' : 'chevron-right'} size={12} />
        </button>
      )}
    </div>
  );
}

function SidebarSubItem({
  label,
  active,
  onClick,
}: {
  label: string;
  active?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 8,
        minHeight: 28,
        padding: '0 10px 0 32px',
        margin: '0 0 2px',
        border: 'none',
        borderLeft: '1px solid var(--border-subtle)',
        borderRadius: 0,
        background: active ? NAV_ACCENT : 'transparent',
        color: active ? NAV_ACCENT_TEXT : '#1c2127',
        fontSize: 13,
        fontWeight: active ? 600 : 500,
        textAlign: 'left',
        cursor: 'pointer',
        marginLeft: 18,
      }}
    >
      {label}
    </button>
  );
}

function SnapshotCard({
  label,
  value,
  icon,
  tone,
}: {
  label: string;
  value: number;
  icon: GlyphName;
  tone: string;
}) {
  return (
    <div className="of-card" style={{ display: 'flex', flexDirection: 'row', alignItems: 'center', gap: 12 }}>
      <span style={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        width: 32,
        height: 32,
        borderRadius: 6,
        background: '#f3f5f7',
      }}>
        <Glyph name={icon} size={18} tone={tone} />
      </span>
      <div style={{ display: 'flex', flexDirection: 'column' }}>
        <span style={{ fontSize: 12, color: '#5c7080', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.04em' }}>{label}</span>
        <span style={{ fontSize: 22, fontWeight: 600, color: '#1c2127', lineHeight: 1.2 }}>{value}</span>
      </div>
    </div>
  );
}

interface FilesViewProps {
  tabKey: 'files' | 'autosaved';
  project: OntologyProject;
  folders: OntologyProjectFolder[];
  fileRows: RowItem[];
  search: string;
  onSearchChange: (value: string) => void;
  kindFilter: ResourceKind | 'all';
  onKindFilterChange: (value: ResourceKind | 'all') => void;
  createdByFilter: string;
  onCreatedByFilterChange: (value: string) => void;
  highlighted: ProjectResourceView | null;
  onHighlight: (row: ProjectResourceView | null) => void;
  onClearHighlight: () => void;
  onOpenResource: (resource: ProjectResourceView) => void;
  onShare: (row: ProjectResourceView) => void;
  onPermissions: (row: ProjectResourceView) => void;
  onMove: (row: ProjectResourceView) => void;
  onRename: (row: ProjectResourceView) => void;
  onDelete: (row: ProjectResourceView) => void;
  onUnbind: (row: ProjectResourceView) => void;
  onCopyLink: (row: ProjectResourceView) => void;
  onDuplicate: (row: ProjectResourceView) => void;
  favoriteKeys: Set<string>;
  onToggleFavorite: (row: ProjectResourceView) => void;
}

function FilesView(props: FilesViewProps) {
  const {
    tabKey,
    project,
    folders,
    fileRows,
    search,
    onSearchChange,
    kindFilter,
    onKindFilterChange,
    createdByFilter,
    onCreatedByFilterChange,
    highlighted,
    onHighlight,
    onClearHighlight,
    onOpenResource,
    onShare,
    onPermissions,
    onMove,
    onRename,
    onDelete,
    onUnbind,
    onCopyLink,
    onDuplicate,
    favoriteKeys,
    onToggleFavorite,
  } = props;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', flex: 1, minHeight: 0 }}>
      <FilesToolbar
        highlighted={highlighted}
        project={project}
        search={search}
        onSearchChange={onSearchChange}
        kindFilter={kindFilter}
        onKindFilterChange={onKindFilterChange}
        createdByFilter={createdByFilter}
        onCreatedByFilterChange={onCreatedByFilterChange}
        onClearHighlight={onClearHighlight}
        onShare={onShare}
        onPermissions={onPermissions}
        onMove={onMove}
        onRename={onRename}
        onDelete={onDelete}
        onUnbind={onUnbind}
        onCopyLink={onCopyLink}
        onDuplicate={onDuplicate}
      />

      <div style={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
        <table className="of-table" style={{ tableLayout: 'fixed' }}>
          <colgroup>
            <col style={{ width: 'auto' }} />
            <col style={{ width: 220 }} />
            <col style={{ width: 200 }} />
            <col style={{ width: 58 }} />
          </colgroup>
          <thead style={{ position: 'sticky', top: 0, zIndex: 1 }}>
            <tr>
              <th>Name</th>
              <th>Last updated</th>
              <th>Tags</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {tabKey === 'autosaved' && fileRows.length === 0 && (
              <tr>
                <td colSpan={4} style={{ padding: '40px 24px', textAlign: 'center' }}>
                  <span className="of-text-muted">
                    No autosaved drafts yet. Foundry-style autosaves of in-progress notebooks, dashboards and apps appear here.
                  </span>
                </td>
              </tr>
            )}

            {tabKey === 'files' && fileRows.length === 0 && (
              <tr>
                <td colSpan={4} style={{ padding: '40px 24px', textAlign: 'center' }}>
                  <span className="of-text-muted">No files. Use the New button to create folders or bind resources.</span>
                </td>
              </tr>
            )}

            {tabKey === 'files' && fileRows.map((row) => {
              if (row.kind === 'folder') {
                return (
                  <FolderRow
                    key={`folder:${row.folder.id}`}
                    folder={row.folder}
                    childCount={folders.filter((f) => f.parent_folder_id === row.folder.id).length}
                    projectPath={`/${project.display_name || project.slug}`}
                    onOpen={() => onClearHighlight() /* navigation handled via Link */}
                    project={project}
                  />
                );
              }
              const key = resourceKey(row.resource.binding.resource_kind, row.resource.binding.resource_id);
              const isHighlighted = highlighted && resourceKey(highlighted.binding.resource_kind, highlighted.binding.resource_id) === key;
              const isFavorite = favoriteKeys.has(resourceKey(row.resource.summary.kind, row.resource.summary.id));
              return (
                <ResourceRow
                  key={key}
                  resource={row.resource}
                  highlighted={!!isHighlighted}
                  isFavorite={isFavorite}
                  onSingleClick={() => onHighlight(row.resource)}
                  onDoubleClick={() => onOpenResource(row.resource)}
                  onToggleFavorite={() => onToggleFavorite(row.resource)}
                />
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function FilesToolbar({
  highlighted,
  project,
  search,
  onSearchChange,
  kindFilter,
  onKindFilterChange,
  createdByFilter,
  onCreatedByFilterChange,
  onClearHighlight,
  onShare,
  onPermissions,
  onMove,
  onRename,
  onDelete,
  onUnbind,
  onCopyLink,
  onDuplicate,
}: {
  highlighted: ProjectResourceView | null;
  project: OntologyProject;
  search: string;
  onSearchChange: (value: string) => void;
  kindFilter: ResourceKind | 'all';
  onKindFilterChange: (value: ResourceKind | 'all') => void;
  createdByFilter: string;
  onCreatedByFilterChange: (value: string) => void;
  onClearHighlight: () => void;
  onShare: (row: ProjectResourceView) => void;
  onPermissions: (row: ProjectResourceView) => void;
  onMove: (row: ProjectResourceView) => void;
  onRename: (row: ProjectResourceView) => void;
  onDelete: (row: ProjectResourceView) => void;
  onUnbind: (row: ProjectResourceView) => void;
  onCopyLink: (row: ProjectResourceView) => void;
  onDuplicate: (row: ProjectResourceView) => void;
}) {
  if (highlighted) {
    return (
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          padding: '10px 16px',
          background: '#fff',
          borderBottom: '1px solid var(--border-default)',
          minHeight: 48,
        }}
      >
        <button
          type="button"
          onClick={onClearHighlight}
          aria-label="Clear selection"
          className="of-button of-button--ghost"
          style={{ padding: 4, minHeight: 28, width: 28 }}
        >
          <Glyph name="x" size={14} tone="#5c7080" />
        </button>
        <div style={{ flex: 1, minWidth: 0, fontSize: 13, color: '#1c2127', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
          <span title={highlighted.summary.name}>{highlighted.summary.name}</span>
          <span className="of-text-muted" style={{ marginLeft: 8, fontSize: 12 }}>
            in /{project.display_name || project.slug}
          </span>
        </div>
        <ToolbarIconButton label="Move" icon="move" onClick={() => onMove(highlighted)} />
        <ToolbarIconButton label="Copy link" icon="link" onClick={() => onCopyLink(highlighted)} />
        <ToolbarIconButton label="Duplicate" icon="duplicate" onClick={() => onDuplicate(highlighted)} />
        <ToolbarIconButton label="Rename" icon="pencil" onClick={() => onRename(highlighted)} />
        <ToolbarIconButton label="Permissions" icon="lock" onClick={() => onPermissions(highlighted)} />
        <ToolbarIconButton label="Members" icon="users" onClick={() => onShare(highlighted)} />
        <ToolbarIconButton label="Add member" icon="add-user" onClick={() => onShare(highlighted)} />
        <ToolbarIconButton label="Mark catalog" icon="shield" onClick={() => onPermissions(highlighted)} />
        <ToolbarIconButton label="Tag" icon="tag" onClick={() => onRename(highlighted)} />
        <ToolbarIconButton label="Approve" icon="badge-check" onClick={() => onPermissions(highlighted)} />
        <ToolbarIconButton label="Unbind" icon="circle-x" onClick={() => onUnbind(highlighted)} />
        <ToolbarIconButton label="Move to trash" icon="trash" onClick={() => onDelete(highlighted)} danger />
      </div>
    );
  }

  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 12,
        padding: '12px 16px',
        background: '#fff',
        borderBottom: '1px solid var(--border-default)',
      }}
    >
      <div style={{ flex: '1 1 420px', display: 'flex', alignItems: 'center', gap: 8, padding: '0 10px', border: '1px solid var(--border-default)', borderRadius: 4, background: '#fff', minHeight: 32 }}>
        <Glyph name="search" size={14} tone="#8a96a6" />
        <input
          value={search}
          onChange={(event) => onSearchChange(event.target.value)}
          placeholder="Search files…"
          style={{ flex: 1, border: 'none', outline: 'none', fontSize: 13, background: 'transparent', minHeight: 28 }}
        />
      </div>
      <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 12, color: '#5c7080' }}>
        File type
        <select
          value={kindFilter}
          onChange={(event) => onKindFilterChange(event.target.value as ResourceKind | 'all')}
          className="of-input"
          style={{ minHeight: 30, fontSize: 12, paddingRight: 22 }}
        >
          <option value="all">All</option>
          {RESOURCE_KIND_OPTIONS.map((kind) => (
            <option key={kind} value={kind}>{RESOURCE_KIND_LABELS[kind]}</option>
          ))}
        </select>
      </label>
      <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 12, color: '#5c7080' }}>
        Created by
        <input
          value={createdByFilter}
          onChange={(event) => onCreatedByFilterChange(event.target.value)}
          placeholder="any"
          className="of-input"
          style={{ minHeight: 30, fontSize: 12, width: 140 }}
        />
      </label>
    </div>
  );
}

function ToolbarIconButton({
  label,
  icon,
  onClick,
  danger,
}: {
  label: string;
  icon: GlyphName;
  onClick: () => void;
  danger?: boolean;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onClick}
      className="of-button of-button--ghost"
      style={{
        padding: 4,
        minHeight: 30,
        width: 30,
        color: danger ? 'var(--status-danger)' : '#5c7080',
      }}
    >
      <Glyph name={icon} size={15} tone={danger ? 'var(--status-danger)' : '#5c7080'} />
    </button>
  );
}

function FolderRow({
  folder,
  childCount,
  projectPath,
  project,
}: {
  folder: OntologyProjectFolder;
  childCount: number;
  projectPath: string;
  onOpen: () => void;
  project: OntologyProject;
}) {
  return (
    <tr>
      <td>
        <Link
          to={`/projects/${project.id}/${folder.id}`}
          style={{ display: 'inline-flex', alignItems: 'center', gap: 8, color: '#1c2127', textDecoration: 'none' }}
        >
          <Glyph name="folder" size={16} tone="#cf923f" filled={false} />
          <span style={{ fontWeight: 500 }}>{folder.name}</span>
          {childCount > 0 && (
            <span className="of-text-muted" style={{ fontSize: 12 }}>· {childCount} item{childCount === 1 ? '' : 's'}</span>
          )}
        </Link>
        <div className="of-text-soft" style={{ fontSize: 11, marginTop: 2, marginLeft: 24 }}>
          {projectPath}
        </div>
      </td>
      <td className="of-text-muted" style={{ fontSize: 12 }}>{fmtDate(folder.updated_at)}</td>
      <td className="of-text-muted">
        {folder.description ? <span className="of-chip">{folder.description}</span> : null}
      </td>
      <td style={{ textAlign: 'right' }}>
        <OpenWithMenu
          compact
          resourceKind="ontology_folder"
          resourceId={folder.id}
          resourceRid={folder.rid}
          projectId={project.id}
          projectRid={folder.project_rid}
        />
      </td>
    </tr>
  );
}

function ResourceRow({
  resource,
  highlighted,
  isFavorite,
  onSingleClick,
  onDoubleClick,
  onToggleFavorite,
}: {
  resource: ProjectResourceView;
  highlighted: boolean;
  isFavorite: boolean;
  onSingleClick: () => void;
  onDoubleClick: () => void;
  onToggleFavorite: () => void;
}) {
  const tone = RESOURCE_KIND_TONE[resource.summary.kind];
  const glyph = RESOURCE_KIND_GLYPH[resource.summary.kind];
  return (
    <tr
      onClick={onSingleClick}
      onDoubleClick={onDoubleClick}
      style={{
        cursor: 'pointer',
        background: highlighted ? ROW_HIGHLIGHT_BG : undefined,
        color: highlighted ? '#fff' : undefined,
      }}
    >
      <td>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <Glyph name={glyph} size={16} tone={highlighted ? '#fff' : tone} />
          <span style={{ fontWeight: 500 }}>{resource.summary.name}</span>
          <button
            type="button"
            onClick={(event) => { event.stopPropagation(); onToggleFavorite(); }}
            aria-label={isFavorite ? 'Remove favorite' : 'Add favorite'}
            style={{
              background: 'transparent',
              border: 'none',
              cursor: 'pointer',
              color: isFavorite ? '#f0b323' : highlighted ? '#fff' : 'transparent',
              padding: 0,
            }}
          >
            <Glyph name={isFavorite ? 'star-filled' : 'star'} size={13} tone={isFavorite ? '#f0b323' : highlighted ? 'rgba(255,255,255,0.6)' : '#c5cdd9'} />
          </button>
        </div>
        <div
          style={{
            fontSize: 11,
            marginTop: 2,
            marginLeft: 24,
            color: highlighted ? 'rgba(255,255,255,0.85)' : '#8a96a6',
          }}
        >
          {resource.summary.location}
        </div>
      </td>
      <td style={{ fontSize: 12, color: highlighted ? 'rgba(255,255,255,0.92)' : '#5c7080' }}>
        {fmtDate(resource.summary.updated_at)}
      </td>
      <td>
        <span
          className="of-chip"
          style={{ background: highlighted ? 'rgba(255,255,255,0.16)' : undefined, color: highlighted ? '#fff' : undefined }}
        >
          {RESOURCE_KIND_LABELS[resource.summary.kind]}
        </span>
      </td>
      <td
        style={{
          textAlign: 'right',
          color: highlighted ? '#fff' : undefined,
        }}
      >
        <OpenWithMenu
          compact
          resourceKind={resource.summary.kind}
          resourceId={resource.summary.id}
          resourceRid={resource.summary.rid}
          projectId={resource.summary.project_id}
          projectRid={resource.summary.project_rid}
          openUrl={resource.summary.open_url}
          onOpen={() => onDoubleClick()}
        />
      </td>
    </tr>
  );
}

function PlaceholderView({
  icon,
  title,
  message,
  cta,
}: {
  icon: GlyphName;
  title: string;
  message: string;
  cta?: { label: string; onClick: () => void };
}) {
  return (
    <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 40 }}>
      <div
        style={{
          maxWidth: 480,
          textAlign: 'center',
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          gap: 12,
        }}
      >
        <span
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: 56,
            height: 56,
            borderRadius: 999,
            background: '#eef2f7',
          }}
        >
          <Glyph name={icon} size={26} tone="#5c7080" />
        </span>
        <h3 style={{ margin: 0, fontSize: 18, fontWeight: 600, color: '#1c2127' }}>{title}</h3>
        <p style={{ margin: 0, fontSize: 13, color: '#5c7080', lineHeight: 1.5 }}>{message}</p>
        {cta && (
          <button type="button" className="of-button of-button--primary" onClick={cta.onClick}>
            <Glyph name="plus" size={13} />
            {cta.label}
          </button>
        )}
      </div>
    </div>
  );
}

function TrashView({
  entries,
  busy,
  menuOpenForKey,
  onToggleMenu,
  onRestore,
  onPurge,
}: {
  entries: TrashEntry[];
  busy: boolean;
  menuOpenForKey: string | null;
  onToggleMenu: (key: string) => void;
  onRestore: (entry: TrashEntry) => void;
  onPurge: (entry: TrashEntry) => void;
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', flex: 1, minHeight: 0 }}>
      <div style={{
        display: 'flex',
        alignItems: 'center',
        gap: 12,
        padding: '12px 16px',
        background: '#fff',
        borderBottom: '1px solid var(--border-default)',
      }}>
        <span style={{ fontSize: 13, color: '#5c7080' }}>Trash · {entries.length} item{entries.length === 1 ? '' : 's'}</span>
      </div>
      <div style={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
        <table className="of-table">
          <colgroup>
            <col />
            <col style={{ width: 220 }} />
            <col style={{ width: 100 }} />
          </colgroup>
          <thead>
            <tr>
              <th>Name</th>
              <th>Retention</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {entries.length === 0 && (
              <tr>
                <td colSpan={3} style={{ padding: '40px 24px', textAlign: 'center' }}>
                  <span className="of-text-muted">Trash is empty.</span>
                </td>
              </tr>
            )}
            {entries.map((entry) => {
              const key = resourceKey(entry.resource_kind, entry.resource_id);
              const open = menuOpenForKey === key;
              return (
                <tr key={key}>
                  <td>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                      <Glyph name={RESOURCE_KIND_GLYPH[entry.resource_kind]} size={16} tone={RESOURCE_KIND_TONE[entry.resource_kind]} />
                      <span style={{ fontWeight: 500 }}>{entry.display_name || fallbackResourceName(entry.resource_kind, entry.resource_id)}</span>
                      <span className="of-chip" style={{ marginLeft: 4, background: '#fbe6e8', color: '#a63232' }}>In trash</span>
                    </div>
                    <div className="of-text-soft" style={{ fontSize: 11, marginTop: 2, marginLeft: 24 }}>
                      Deleted by <code>{entry.deleted_by ?? 'unknown'}</code>
                      {entry.restore_target_status === 'project_root' && (
                        <span className="of-chip" style={{ marginLeft: 6, background: 'var(--status-warning-bg)', color: 'var(--status-warning)' }}>
                          Restores to project root
                        </span>
                      )}
                    </div>
                  </td>
                  <td className="of-text-muted" style={{ fontSize: 12 }}>
                    {fmtDate(entry.deleted_at)}
                    <div className="of-text-soft" style={{ marginTop: 2 }}>
                      Purge after {fmtDate(entry.purge_after)} · {entry.retention_days}d
                    </div>
                  </td>
                  <td style={{ position: 'relative' }}>
                    <button
                      type="button"
                      aria-label="Open trash actions"
                      onClick={() => onToggleMenu(key)}
                      className="of-button of-button--ghost"
                      style={{ padding: 4, minHeight: 28, width: 28 }}
                    >
                      <Glyph name="circle-x" size={14} tone="#5c7080" />
                    </button>
                    {open && (
                      <div
                        role="menu"
                        style={{
                          position: 'absolute',
                          right: 8,
                          top: '100%',
                          marginTop: 2,
                          background: '#fff',
                          border: '1px solid var(--border-default)',
                          borderRadius: 4,
                          boxShadow: '0 8px 24px rgba(15,23,42,0.18)',
                          padding: 4,
                          minWidth: 200,
                          zIndex: 30,
                        }}
                      >
                        <button
                          type="button"
                          onClick={() => onRestore(entry)}
                          disabled={busy}
                          style={menuItemStyle('#1c2127')}
                        >
                          <Glyph name="undo" size={13} tone="#5c7080" />
                          Restore
                        </button>
                        <button
                          type="button"
                          onClick={() => onPurge(entry)}
                          disabled={busy}
                          style={menuItemStyle('#a63232')}
                        >
                          <Glyph name="circle-x" size={13} tone="#a63232" />
                          Delete permanently…
                        </button>
                      </div>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function menuItemStyle(color: string): React.CSSProperties {
  return {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    width: '100%',
    padding: '6px 10px',
    background: 'transparent',
    border: 'none',
    color,
    fontSize: 13,
    textAlign: 'left',
    cursor: 'pointer',
    borderRadius: 4,
  };
}

function MembershipsView({
  project,
  memberships,
  busy,
  newMemberId,
  newMemberRole,
  onNewMemberIdChange,
  onNewMemberRoleChange,
  onAdd,
  onEdit,
  onRemove,
}: {
  project: OntologyProject;
  memberships: OntologyProjectMembership[];
  busy: boolean;
  newMemberId: string;
  newMemberRole: OntologyProjectRole;
  onNewMemberIdChange: (value: string) => void;
  onNewMemberRoleChange: (value: OntologyProjectRole) => void;
  onAdd: () => void;
  onEdit: (membership: OntologyProjectMembership) => void;
  onRemove: (membership: OntologyProjectMembership) => void;
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', flex: 1, minHeight: 0 }}>
      <section style={{ padding: 16, borderBottom: '1px solid var(--border-default)', background: '#fff', display: 'grid', gap: 10 }}>
        <p className="of-eyebrow">Add member to {project.display_name || project.slug}</p>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))', gap: 8, alignItems: 'end' }}>
          <label style={{ fontSize: 12 }}>
            User id
            <input
              value={newMemberId}
              onChange={(event) => onNewMemberIdChange(event.target.value)}
              className="of-input"
              style={{ marginTop: 4 }}
            />
          </label>
          <label style={{ fontSize: 12 }}>
            Role
            <select
              value={newMemberRole}
              onChange={(event) => onNewMemberRoleChange(event.target.value as OntologyProjectRole)}
              className="of-input"
              style={{ marginTop: 4 }}
            >
              {MEMBER_ROLES.map((role) => (
                <option key={role} value={role}>{role}</option>
              ))}
            </select>
          </label>
          <button
            type="button"
            onClick={onAdd}
            disabled={busy || !newMemberId.trim()}
            className="of-button of-button--primary"
          >
            <Glyph name="add-user" size={14} />
            Add
          </button>
        </div>
      </section>
      <div style={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
        <table className="of-table">
          <thead>
            <tr>
              <th>User</th>
              <th>Role</th>
              <th>Updated</th>
              <th style={{ width: 160 }}>Actions</th>
            </tr>
          </thead>
          <tbody>
            {memberships.length === 0 && (
              <tr>
                <td colSpan={4} style={{ padding: '40px 24px', textAlign: 'center' }}>
                  <span className="of-text-muted">No memberships.</span>
                </td>
              </tr>
            )}
            {memberships.map((membership) => (
              <tr key={`${membership.project_id}-${membership.user_id}`}>
                <td>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <span
                      style={{
                        display: 'inline-flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        width: 24,
                        height: 24,
                        borderRadius: 999,
                        background: '#eef2f7',
                        color: '#5c7080',
                        fontSize: 11,
                        fontWeight: 700,
                      }}
                    >
                      {membership.user_id.slice(0, 2).toUpperCase()}
                    </span>
                    <code>{membership.user_id}</code>
                  </div>
                </td>
                <td><span className="of-chip">{membership.role}</span></td>
                <td className="of-text-muted" style={{ fontSize: 12 }}>{fmtDate(membership.updated_at)}</td>
                <td>
                  <div style={{ display: 'flex', gap: 6 }}>
                    <button type="button" onClick={() => onEdit(membership)} className="of-button">Edit</button>
                    <button
                      type="button"
                      onClick={() => onRemove(membership)}
                      className="of-button"
                      style={{ color: 'var(--status-danger)', borderColor: '#d6a9a9' }}
                    >
                      Remove
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function CompactDialog({
  title,
  children,
  onCancel,
  onSubmit,
  submitLabel,
  submitDisabled,
}: {
  title: string;
  children: React.ReactNode;
  onCancel: () => void;
  onSubmit: () => void;
  submitLabel: string;
  submitDisabled?: boolean;
}) {
  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={title}
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(15,23,42,0.4)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: 16,
        zIndex: 100,
      }}
    >
      <div style={{
        width: '100%',
        maxWidth: 440,
        background: '#fff',
        color: '#1c2127',
        border: '1px solid var(--border-default)',
        borderRadius: 6,
        boxShadow: '0 20px 50px rgba(15,23,42,0.4)',
      }}>
        <div style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          borderBottom: '1px solid var(--border-default)',
          padding: '12px 16px',
        }}>
          <div style={{ fontSize: 14, fontWeight: 600 }}>{title}</div>
          <button
            type="button"
            onClick={onCancel}
            style={{
              background: 'transparent',
              border: 'none',
              cursor: 'pointer',
              color: '#5c7080',
            }}
            aria-label="Close"
          >
            <Glyph name="x" size={14} />
          </button>
        </div>
        <div style={{ display: 'grid', gap: 10, padding: 16 }}>{children}</div>
        <div style={{
          display: 'flex',
          justifyContent: 'flex-end',
          gap: 8,
          borderTop: '1px solid var(--border-default)',
          padding: '12px 16px',
        }}>
          <button type="button" onClick={onCancel} className="of-button">Cancel</button>
          <button
            type="button"
            onClick={onSubmit}
            disabled={submitDisabled}
            className="of-button of-button--primary"
          >
            {submitLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
