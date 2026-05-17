import { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';

import { BulkActionsToolbar, type BulkAction } from '@/lib/components/workspace/BulkActionsToolbar';
import { ConfirmDialog } from '@/lib/components/workspace/ConfirmDialog';
import { resourceRIDForKind } from '@/lib/compass/resourceTypeRegistry';
import { FolderTree } from '@/lib/components/workspace/FolderTree';
import { MoveDialog } from '@/lib/components/workspace/MoveDialog';
import { OpenWithMenu } from '@/lib/components/workspace/OpenWithMenu';
import { buildProjectFolderBreadcrumbItems, ProjectBreadcrumb } from '@/lib/components/workspace/ProjectBreadcrumb';
import { RenameDialog } from '@/lib/components/workspace/RenameDialog';
import { ResourceDetailsPanel, type ResourceSummary } from '@/lib/components/workspace/ResourceDetailsPanel';
import { RowActionsMenu } from '@/lib/components/workspace/RowActionsMenu';
import { ShareDialog } from '@/lib/components/workspace/ShareDialog';
import {
  createProjectFolder,
  getProject,
  listProjectFolders,
  listProjectResources,
  listProjects,
  type OntologyProject,
  type OntologyProjectFolder,
  type OntologyProjectResourceBinding,
} from '@/lib/api/ontology';
import {
  batchApply,
  duplicateResource,
  recordAccess,
  softDeleteResource,
  type ResourceKind,
} from '@/lib/api/workspace';

type FolderExplorerItem = {
  key: string;
  type: 'folder';
  id: string;
  name: string;
  description: string | null;
  kind: 'ontology_folder';
  operationKind: 'ontology_folder';
  shareKind: 'ontology_folder';
  createdAt: string;
  updatedAt: string;
  ownerId: string;
  folder: OntologyProjectFolder;
};

type ResourceExplorerItem = {
  key: string;
  type: 'resource';
  id: string;
  name: string;
  description: string | null;
  kind: ResourceKind;
  operationKind: 'ontology_resource_binding';
  shareKind: ResourceKind;
  createdAt: string;
  updatedAt: string | null;
  ownerId: string;
  binding: OntologyProjectResourceBinding;
};

type ExplorerItem = FolderExplorerItem | ResourceExplorerItem;

type DialogTarget = {
  kind: ResourceKind;
  id: string;
  label: string;
};

const SUPPORTED_RESOURCE_KINDS: ResourceKind[] = [
  'ontology_project',
  'ontology_folder',
  'ontology_resource_binding',
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

function asResourceKind(kind: string): ResourceKind {
  return SUPPORTED_RESOURCE_KINDS.includes(kind as ResourceKind) ? (kind as ResourceKind) : 'other';
}

function shortId(id: string) {
  return id.length > 12 ? `${id.slice(0, 8)}...` : id;
}

function formatDate(value: string | null | undefined) {
  if (!value) return '-';
  try {
    return new Date(value).toLocaleString();
  } catch {
    return value;
  }
}

function isDescendantFolder(folders: OntologyProjectFolder[], sourceId: string, targetId: string | null) {
  if (!targetId) return false;
  const byId = new Map(folders.map((folder) => [folder.id, folder]));
  let cursor: string | null = targetId;
  while (cursor) {
    if (cursor === sourceId) return true;
    cursor = byId.get(cursor)?.parent_folder_id ?? null;
  }
  return false;
}

export function ProjectFolderPage() {
  const { projectId = '', folderId = '' } = useParams<{ projectId: string; folderId: string }>();
  const navigate = useNavigate();
  const [project, setProject] = useState<OntologyProject | null>(null);
  const [projects, setProjects] = useState<OntologyProject[]>([]);
  const [folders, setFolders] = useState<OntologyProjectFolder[]>([]);
  const [resources, setResources] = useState<OntologyProjectResourceBinding[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [childName, setChildName] = useState('');
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const [detailsResource, setDetailsResource] = useState<ResourceSummary | null>(null);
  const [shareTarget, setShareTarget] = useState<DialogTarget | null>(null);
  const [renameTarget, setRenameTarget] = useState<ExplorerItem | null>(null);
  const [moveTarget, setMoveTarget] = useState<ExplorerItem | null>(null);
  const [bulkMoveOpen, setBulkMoveOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<ExplorerItem | null>(null);
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false);

  async function load() {
    if (!projectId) return;
    setLoading(true);
    setError('');
    try {
      const [p, f, r, ps] = await Promise.all([
        getProject(projectId),
        listProjectFolders(projectId),
        listProjectResources(projectId).catch(() => [] as OntologyProjectResourceBinding[]),
        listProjects({ per_page: 200 }).catch(() => null),
      ]);
      setProject(p);
      setFolders(f);
      setResources(r);
      setProjects(ps?.data?.length ? ps.data : [p]);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, [projectId]);

  useEffect(() => {
    if (!folderId) return;
    recordAccess({ resource_kind: 'ontology_folder', resource_id: folderId }).catch(() => {});
  }, [folderId]);

  const folder = useMemo(() => folders.find((f) => f.id === folderId) ?? null, [folders, folderId]);
  const childFolders = useMemo(
    () => folders.filter((f) => f.parent_folder_id === folderId).sort((a, b) => a.name.localeCompare(b.name)),
    [folders, folderId],
  );
  const breadcrumbItems = useMemo(
    () => (project ? buildProjectFolderBreadcrumbItems(project, folders, folderId) : []),
    [folders, folderId, project],
  );
  const breadcrumbLocation = useMemo(
    () => breadcrumbItems.filter((item) => item.kind === 'project' || item.kind === 'folder').map((item) => item.label).join(' / '),
    [breadcrumbItems],
  );
  const items = useMemo<ExplorerItem[]>(() => {
    const folderItems: FolderExplorerItem[] = childFolders.map((child) => ({
      key: `folder:${child.id}`,
      type: 'folder',
      id: child.id,
      name: child.name,
      description: child.description || null,
      kind: 'ontology_folder',
      operationKind: 'ontology_folder',
      shareKind: 'ontology_folder',
      createdAt: child.created_at,
      updatedAt: child.updated_at,
      ownerId: child.created_by,
      folder: child,
    }));
    const resourceItems: ResourceExplorerItem[] = resources.map((binding) => {
      const kind = asResourceKind(binding.resource_kind);
      return {
        key: `resource:${binding.resource_kind}:${binding.resource_id}`,
        type: 'resource',
        id: binding.resource_id,
        name: `${binding.resource_kind} ${shortId(binding.resource_id)}`,
        description: `Bound by ${shortId(binding.bound_by)}`,
        kind,
        operationKind: 'ontology_resource_binding',
        shareKind: kind,
        createdAt: binding.created_at,
        updatedAt: null,
        ownerId: binding.bound_by,
        binding,
      };
    });
    return [...folderItems, ...resourceItems];
  }, [childFolders, resources]);

  const selectedItems = useMemo(() => items.filter((item) => selectedKeys.has(item.key)), [items, selectedKeys]);
  const selectedFoldersOnly = selectedItems.length > 0 && selectedItems.every((item) => item.type === 'folder');

  useEffect(() => {
    const visibleKeys = new Set(items.map((item) => item.key));
    setSelectedKeys((prev) => {
      const next = new Set([...prev].filter((key) => visibleKeys.has(key)));
      return next.size === prev.size ? prev : next;
    });
  }, [items]);

  async function addChild() {
    if (!project || !folder || !childName.trim()) return;
    setBusy(true);
    try {
      await createProjectFolder(project.id, { name: childName.trim(), parent_folder_id: folder.id });
      setChildName('');
      setFolders(await listProjectFolders(project.id));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Folder create failed');
    } finally {
      setBusy(false);
    }
  }

  function setSelected(key: string, selected: boolean) {
    setSelectedKeys((prev) => {
      const next = new Set(prev);
      if (selected) next.add(key);
      else next.delete(key);
      return next;
    });
  }

  function clearSelection() {
    setSelectedKeys(new Set());
  }

  function resourceSummary(item: ExplorerItem): ResourceSummary {
    const rid = item.type === 'folder'
      ? item.folder.rid
      : resourceRIDForKind(item.shareKind, item.id);
    return {
      id: item.id,
      rid,
      name: item.name,
      kind: item.shareKind,
      description: item.description,
      owner_id: item.ownerId,
      location: breadcrumbLocation,
      project_id: project?.id,
      project_rid: item.type === 'folder'
        ? item.folder.project_rid
        : project ? resourceRIDForKind('ontology_project', project.id) : null,
      created_at: item.createdAt,
      updated_at: item.updatedAt,
      tags: item.type === 'folder' ? ['folder'] : [item.binding.resource_kind, 'project-binding'],
    };
  }

  function moveDialogProjects(item: ExplorerItem | null) {
    if (!project) return [];
    if (!item || item.operationKind === 'ontology_folder') return [project];
    return projects.length ? projects : [project];
  }

  function canMoveItemToFolder(item: ExplorerItem | null, targetFolderId: string) {
    if (!item || item.type !== 'folder') return true;
    return targetFolderId !== item.id && !isDescendantFolder(folders, item.id, targetFolderId);
  }

  function canMoveSelectedToFolder(targetFolderId: string) {
    return selectedItems.every((item) => canMoveItemToFolder(item, targetFolderId));
  }

  async function refreshAfterMutation() {
    await load();
    clearSelection();
  }

  async function duplicateItem(item: ExplorerItem) {
    if (item.type !== 'folder') return;
    setBusy(true);
    setError('');
    try {
      await duplicateResource('ontology_folder', item.id, { target_folder_id: folder?.id ?? null });
      await refreshAfterMutation();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Duplicate failed');
    } finally {
      setBusy(false);
    }
  }

  async function duplicateSelected() {
    const targets = selectedItems.filter((item): item is FolderExplorerItem => item.type === 'folder');
    if (!targets.length) return;
    setBusy(true);
    setError('');
    try {
      await Promise.all(targets.map((item) => duplicateResource('ontology_folder', item.id, { target_folder_id: folder?.id ?? null })));
      await refreshAfterMutation();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Bulk duplicate failed');
    } finally {
      setBusy(false);
    }
  }

  async function deleteOne(item: ExplorerItem) {
    setBusy(true);
    setError('');
    try {
      await softDeleteResource(item.operationKind, item.id);
      setDeleteTarget(null);
      await refreshAfterMutation();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Delete failed');
    } finally {
      setBusy(false);
    }
  }

  async function deleteSelected() {
    if (!selectedItems.length) return;
    setBusy(true);
    setError('');
    try {
      const response = await batchApply(selectedItems.map((item) => ({
        op: 'delete',
        resource_kind: item.operationKind,
        resource_id: item.id,
      })));
      const failed = response.results.filter((entry) => !entry.ok);
      if (failed.length > 0) {
        setError(`${failed.length} selected item(s) could not be deleted.`);
      }
      setBulkDeleteOpen(false);
      await refreshAfterMutation();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Bulk delete failed');
    } finally {
      setBusy(false);
    }
  }

  async function moveFolderByTree(targetFolderId: string | null, item: ExplorerItem) {
    if (item.type !== 'folder' || !project) return;
    if (targetFolderId === item.id || isDescendantFolder(folders, item.id, targetFolderId)) {
      setError('A folder cannot be moved into itself or one of its descendants.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      await batchApply([{
        op: 'move',
        resource_kind: 'ontology_folder',
        resource_id: item.id,
        target_folder_id: targetFolderId,
      }]);
      await refreshAfterMutation();
      if (item.id === folderId && targetFolderId) navigate(`/projects/${project.id}/${targetFolderId}`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Move failed');
    } finally {
      setBusy(false);
    }
  }

  function handleRowAction(item: ExplorerItem, action: string) {
    if (!project) return;
    if (action === 'open') {
      if (item.type === 'folder') navigate(`/projects/${project.id}/${item.id}`);
      else setDetailsResource(resourceSummary(item));
    } else if (action === 'details') {
      setDetailsResource(resourceSummary(item));
    } else if (action === 'share') {
      setShareTarget({ kind: item.shareKind, id: item.id, label: item.name });
    } else if (action === 'move') {
      setMoveTarget(item);
    } else if (action === 'rename' && item.type === 'folder') {
      setRenameTarget(item);
    } else if (action === 'duplicate' && item.type === 'folder') {
      void duplicateItem(item);
    } else if (action === 'delete') {
      setDeleteTarget(item);
    }
  }

  function handleBulkAction(action: string) {
    if (action === 'move') setBulkMoveOpen(true);
    else if (action === 'duplicate') void duplicateSelected();
    else if (action === 'delete') setBulkDeleteOpen(true);
  }

  const bulkActions: BulkAction[] = [
    { id: 'move', label: 'Move', disabled: !selectedFoldersOnly },
    { id: 'duplicate', label: 'Duplicate', disabled: !selectedFoldersOnly },
    { id: 'delete', label: 'Move to trash', danger: true },
  ];

  if (loading) {
    return (
      <section className="of-page" style={{ padding: 24 }}>
        <Link to="/projects" style={{ color: 'var(--text-muted)', fontSize: 13 }}>Projects</Link>
        <p className="of-text-muted" style={{ marginTop: 12 }}>Loading...</p>
      </section>
    );
  }

  if (!project) {
    return (
      <section className="of-page" style={{ padding: 24 }}>
        <Link to="/projects" style={{ color: 'var(--text-muted)', fontSize: 13 }}>Projects</Link>
        <p className="of-status-danger" style={{ marginTop: 12 }}>{error || 'Project not found'}</p>
      </section>
    );
  }

  if (!folder) {
    return (
      <section className="of-page" style={{ padding: 24 }}>
        <Link to={`/projects/${project.id}`} style={{ color: 'var(--text-muted)', fontSize: 13 }}>{project.display_name || project.slug}</Link>
        <p className="of-status-danger" style={{ marginTop: 12 }}>Folder {folderId} not found in this project.</p>
      </section>
    );
  }

  return (
    <section className="of-page" style={{ padding: 24, display: 'grid', gap: 16 }}>
      <ProjectBreadcrumb
        items={breadcrumbItems}
        onNavigate={(item) => {
          if (item.href) navigate(item.href);
        }}
      />

      <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 12 }}>
        <div>
          <h1 className="of-heading-xl">{folder.name}</h1>
          <p className="of-text-muted" style={{ marginTop: 4, fontSize: 12 }}>
            {folder.rid} - slug: {folder.slug} - parent: {folder.parent_folder_rid || 'project'}
            {folder.description && ` - ${folder.description}`}
          </p>
        </div>
        <div style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
          <OpenWithMenu
            resourceKind="ontology_folder"
            resourceId={folder.id}
            resourceRid={folder.rid}
            projectId={project.id}
            projectRid={folder.project_rid}
          />
          <button type="button" onClick={() => setDetailsResource(resourceSummary({
            key: `folder:${folder.id}`,
            type: 'folder',
            id: folder.id,
            name: folder.name,
            description: folder.description || null,
            kind: 'ontology_folder',
            operationKind: 'ontology_folder',
            shareKind: 'ontology_folder',
            createdAt: folder.created_at,
            updatedAt: folder.updated_at,
            ownerId: folder.created_by,
            folder,
          }))} className="of-button">
            Details
          </button>
        </div>
      </header>

      {error && (
        <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {error}
        </div>
      )}

      <BulkActionsToolbar
        count={selectedItems.length}
        actions={bulkActions}
        onAction={handleBulkAction}
        onClear={clearSelection}
        busy={busy}
      />

      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(220px, 280px) minmax(0, 1fr)', gap: 16, alignItems: 'start' }}>
        <aside className="of-panel" style={{ padding: 16, position: 'sticky', top: 16 }}>
          <p className="of-eyebrow">Folders</p>
          <div style={{ marginTop: 10 }}>
            <FolderTree
              folders={folders}
              selectedId={folder.id}
              rootLabel={project.display_name || project.slug}
              onSelect={(id) => navigate(id ? `/projects/${project.id}/${id}` : `/projects/${project.id}`)}
              canDrop={(id) => selectedItems.length === 1 && selectedItems[0].type === 'folder' && !isDescendantFolder(folders, selectedItems[0].id, id)}
              onDrop={(id) => {
                if (selectedItems.length === 1) void moveFolderByTree(id, selectedItems[0]);
              }}
            />
          </div>
        </aside>

        <div style={{ display: 'grid', gap: 16, minWidth: 0 }}>
          <section className="of-panel" style={{ padding: 16 }}>
            <p className="of-eyebrow">Add subfolder</p>
            <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', marginTop: 8 }}>
              <input value={childName} onChange={(e) => setChildName(e.target.value)} placeholder="subfolder name" className="of-input" />
              <button type="button" onClick={() => void addChild()} disabled={busy || !childName.trim()} className="of-button of-button--primary">
                Create
              </button>
            </div>
          </section>

          <section className="of-panel" style={{ padding: 0, overflow: 'hidden' }}>
            <div style={{ padding: '14px 16px', borderBottom: '1px solid var(--border-default)', display: 'flex', justifyContent: 'space-between', gap: 12 }}>
              <div>
                <p className="of-eyebrow">Folder contents</p>
                <p className="of-text-muted" style={{ marginTop: 3, fontSize: 12 }}>
                  {childFolders.length} folder(s) - {resources.length} bound resource(s)
                </p>
              </div>
              <button type="button" onClick={() => void load()} disabled={busy} className="of-button" style={{ fontSize: 12 }}>
                Refresh
              </button>
            </div>
            <div style={{ overflowX: 'auto' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse', minWidth: 840, fontSize: 12 }}>
                <thead>
                  <tr style={{ textAlign: 'left', color: 'var(--text-muted)', borderBottom: '1px solid var(--border-default)' }}>
                    <th style={{ padding: '10px 12px', width: 38 }}>
                      <input
                        type="checkbox"
                        aria-label="Select all visible items"
                        checked={items.length > 0 && selectedItems.length === items.length}
                        onChange={(e) => setSelectedKeys(e.target.checked ? new Set(items.map((item) => item.key)) : new Set())}
                      />
                    </th>
                    <th style={{ padding: '10px 12px' }}>Name</th>
                    <th style={{ padding: '10px 12px' }}>Kind</th>
                    <th style={{ padding: '10px 12px' }}>Owner</th>
                    <th style={{ padding: '10px 12px' }}>Updated</th>
                    <th style={{ padding: '10px 12px', width: 58 }} />
                    <th style={{ padding: '10px 12px', width: 52 }} />
                  </tr>
                </thead>
                <tbody>
                  {items.map((item) => (
                    <tr
                      key={item.key}
                      draggable={item.type === 'folder'}
                      onDragStart={() => {
                        clearSelection();
                        setSelected(item.key, true);
                      }}
                      onDoubleClick={() => handleRowAction(item, 'open')}
                      style={{ borderBottom: '1px solid var(--border-subtle)' }}
                    >
                      <td style={{ padding: '10px 12px' }}>
                        <input
                          type="checkbox"
                          aria-label={`Select ${item.name}`}
                          checked={selectedKeys.has(item.key)}
                          onChange={(e) => setSelected(item.key, e.target.checked)}
                        />
                      </td>
                      <td style={{ padding: '10px 12px' }}>
                        <button
                          type="button"
                          onClick={() => handleRowAction(item, 'open')}
                          style={{ border: 'none', background: 'transparent', padding: 0, textAlign: 'left', color: '#60a5fa', cursor: 'pointer', fontWeight: 600 }}
                        >
                          {item.name}
                        </button>
                        {item.description && <p className="of-text-muted" style={{ margin: '3px 0 0', fontSize: 11 }}>{item.description}</p>}
                      </td>
                      <td style={{ padding: '10px 12px' }}>{item.type === 'folder' ? 'folder' : item.binding.resource_kind}</td>
                      <td style={{ padding: '10px 12px', fontFamily: 'var(--font-mono)', fontSize: 11 }}>{shortId(item.ownerId)}</td>
                      <td style={{ padding: '10px 12px' }}>{formatDate(item.updatedAt ?? item.createdAt)}</td>
                      <td style={{ padding: '10px 12px', textAlign: 'right' }}>
                        <OpenWithMenu
                          compact
                          resourceKind={item.shareKind}
                          resourceId={item.id}
                          resourceRid={item.type === 'folder' ? item.folder.rid : resourceRIDForKind(item.shareKind, item.id)}
                          projectId={project.id}
                          projectRid={item.type === 'folder' ? item.folder.project_rid : resourceRIDForKind('ontology_project', project.id)}
                          onOpen={() => {
                            if (item.type !== 'folder') recordAccess({ resource_kind: item.shareKind, resource_id: item.id }).catch(() => {});
                          }}
                        />
                      </td>
                      <td style={{ padding: '10px 12px', textAlign: 'right' }}>
                        <RowActionsMenu
                          actions={[
                            { id: 'open', label: item.type === 'folder' ? 'Open folder' : 'Open details' },
                            { id: 'details', label: 'Details' },
                            { id: 'share', label: 'Share', icon: 'share' },
                            { id: 'move', label: 'Move', icon: 'move' },
                            { id: 'rename', label: 'Rename', icon: 'pencil', disabled: item.type !== 'folder' },
                            { id: 'duplicate', label: 'Duplicate', icon: 'duplicate', disabled: item.type !== 'folder' },
                            { id: 'delete', label: item.type === 'folder' ? 'Move to trash' : 'Remove binding', icon: 'delete', danger: true },
                          ]}
                          onSelect={(action) => handleRowAction(item, action)}
                        />
                      </td>
                    </tr>
                  ))}
                  {items.length === 0 && (
                    <tr>
                      <td colSpan={7} className="of-text-muted" style={{ padding: 16 }}>
                        This folder is empty.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </section>
        </div>
      </div>

      <ResourceDetailsPanel
        open={!!detailsResource}
        resource={detailsResource}
        onClose={() => setDetailsResource(null)}
      />

      <ShareDialog
        open={!!shareTarget}
        resourceKind={shareTarget?.kind ?? null}
        resourceId={shareTarget?.id ?? null}
        resourceLabel={shareTarget?.label}
        onClose={() => setShareTarget(null)}
      />

      <RenameDialog
        open={!!renameTarget}
        resourceKind={renameTarget?.operationKind ?? null}
        resourceId={renameTarget?.id ?? null}
        currentName={renameTarget?.name ?? ''}
        onClose={() => setRenameTarget(null)}
        onRenamed={() => void refreshAfterMutation()}
      />

      <MoveDialog
        open={!!moveTarget}
        resourceKind={moveTarget?.operationKind ?? null}
        resourceId={moveTarget?.id ?? null}
        resourceLabel={moveTarget?.name}
        projects={moveDialogProjects(moveTarget)}
        initialProjectId={project.id}
        canSelectFolder={(targetFolderId) => canMoveItemToFolder(moveTarget, targetFolderId)}
        onClose={() => setMoveTarget(null)}
        onMoved={() => void refreshAfterMutation()}
      />

      <MoveDialog
        open={bulkMoveOpen}
        resourceKind={null}
        resourceId={null}
        projects={[project]}
        initialProjectId={project.id}
        targets={selectedItems.map((item) => ({ kind: item.operationKind, id: item.id, label: item.name }))}
        canSelectFolder={canMoveSelectedToFolder}
        onClose={() => setBulkMoveOpen(false)}
        onMoved={() => {
          setBulkMoveOpen(false);
          void refreshAfterMutation();
        }}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        title={deleteTarget?.type === 'folder' ? 'Move folder to trash' : 'Remove resource binding'}
        message={deleteTarget ? `${deleteTarget.name} will be removed from this folder view.` : ''}
        confirmLabel={deleteTarget?.type === 'folder' ? 'Move to trash' : 'Remove'}
        danger
        busy={busy}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={() => {
          if (deleteTarget) void deleteOne(deleteTarget);
        }}
      />

      <ConfirmDialog
        open={bulkDeleteOpen}
        title="Move selected items to trash"
        message={`${selectedItems.length} selected item(s) will be removed.`}
        confirmLabel="Move to trash"
        danger
        busy={busy}
        onCancel={() => setBulkDeleteOpen(false)}
        onConfirm={() => void deleteSelected()}
      />
    </section>
  );
}
