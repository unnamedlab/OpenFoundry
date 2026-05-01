<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import Glyph from '$components/ui/Glyph.svelte';
  import {
    batchApply,
    duplicateResource,
    listSharedWithMe,
    listTrash,
    moveResource,
    purgeResource,
    restoreResource,
    softDeleteResource,
    type BatchAction,
    type ResourceKind,
    type ResourceShare,
    type TrashEntry,
  } from '$lib/api/workspace';
  import RowActionsMenu, {
    type RowAction,
  } from '$components/workspace/RowActionsMenu.svelte';
  import RenameDialog from '$components/workspace/RenameDialog.svelte';
  import MoveDialog from '$components/workspace/MoveDialog.svelte';
  import BulkActionsToolbar from '$components/workspace/BulkActionsToolbar.svelte';
  import ConfirmDialog from '$components/workspace/ConfirmDialog.svelte';
  import {
    cachedLabel,
    resolveLabels,
    setLabel as setResourceLabel,
  } from '$lib/utils/resource-labels';
  import {
    createProject,
    listProjectFolders,
    listProjects,
    type OntologyProjectFolder,
    type OntologyProject,
  } from '$lib/api/ontology';
  import { listSpaces } from '$lib/api/nexus';
  import { notifications } from '$lib/stores/notifications';
  import {
    FALLBACK_SPACE_OPTIONS,
    buildSpaceOptions,
    getPreferredWorkspaceSlug,
    resolveSelectedSpaceId,
    resolveSpaceLabel as lookupSpaceLabel,
    type SpaceOption,
  } from '$lib/utils/projects-and-files';
  import { auth } from '$stores/auth';

  type WorkspaceView = 'all' | 'portfolios' | 'projects' | 'your-files' | 'shared' | 'trash';
  type WorkspaceKind = 'project' | 'portfolio' | 'file' | 'folder';
  type ProjectTemplateId = 'blank' | 'operations' | 'analytics' | 'ontology';

  type ProjectTemplate = {
    id: ProjectTemplateId;
    label: string;
    description: string;
    suggestedPortfolio: string;
    starterFolders: string[];
    suggestedDescription: string;
  };

  type WorkspaceRow = {
    id: string;
    name: string;
    kind: WorkspaceKind;
    role: string;
    tags: string[];
    portfolio: string;
    views: number;
    modifiedAt: string;
    spaceId: string;
    view: WorkspaceView;
    description: string;
    location: string;
  };

  type ProjectMeta = {
    spaceId: string;
    templateId: ProjectTemplateId;
  };

  type CreateProjectDraft = {
    spaceId: string;
    templateId: ProjectTemplateId;
    name: string;
    description: string;
    createStarterFolder: boolean;
    starterFolderName: string;
  };

  const currentUser = auth.user;
  const isAuthenticated = auth.isAuthenticated;
  const PROJECT_VIEW_SEED = 4;
  const PROJECT_VIEW_MODULO = 12;

  const viewTabs: Array<{ id: WorkspaceView; label: string }> = [
    { id: 'portfolios', label: 'Portfolios' },
    { id: 'projects', label: 'Projects' },
    { id: 'your-files', label: 'Your files' },
    { id: 'shared', label: 'Shared with you' },
    { id: 'trash', label: 'Trash' },
  ];

  const projectTemplates: ProjectTemplate[] = [
    {
      id: 'blank',
      label: 'Blank project',
      description: 'Start with an empty container and add folders or files later.',
      suggestedPortfolio: 'General',
      starterFolders: ['Documentation', 'Drafts'],
      suggestedDescription: 'Workspace prepared for a new cross-functional project.',
    },
    {
      id: 'operations',
      label: 'Operations review',
      description: 'Prepares a project for weekly operating reviews and delivery planning.',
      suggestedPortfolio: 'Operations',
      starterFolders: ['Planning', 'KPIs', 'Decisions'],
      suggestedDescription: 'Operating review workspace with planning, KPI, and decision assets.',
    },
    {
      id: 'analytics',
      label: 'Analytics launchpad',
      description: 'Designed for files, reports, and dashboards that support analysis work.',
      suggestedPortfolio: 'Analytics',
      starterFolders: ['Datasets', 'Reports', 'Dashboards'],
      suggestedDescription: 'Analytics workspace with starter folders for datasets and outputs.',
    },
    {
      id: 'ontology',
      label: 'Ontology workspace',
      description: 'Organizes ontology resources, reviews, and related project files.',
      suggestedPortfolio: 'Ontology',
      starterFolders: ['Models', 'Reviews', 'Exports'],
      suggestedDescription: 'Ontology delivery workspace for models, reviews, and exports.',
    },
  ];

  const promotedItems = [
    'Create a secure project container for a new initiative',
    'Pin frequently used workspaces in a shared portfolio',
    'Organize starter files so teams avoid using home folders',
  ];

  const staticRows: WorkspaceRow[] = [
    {
      id: 'portfolio-operations',
      name: 'Operations transformation',
      kind: 'portfolio',
      role: 'Lead',
      tags: ['portfolio', 'promoted'],
      portfolio: 'Operations',
      views: 42,
      modifiedAt: '2026-04-26T14:00:00Z',
      spaceId: 'operations',
      view: 'portfolios',
      description: 'Grouping of operational projects for planning, reviews, and issue resolution.',
      location: 'Portfolios / Operations',
    },
    {
      id: 'portfolio-analytics',
      name: 'Commercial analytics',
      kind: 'portfolio',
      role: 'Editor',
      tags: ['portfolio'],
      portfolio: 'Analytics',
      views: 27,
      modifiedAt: '2026-04-24T09:30:00Z',
      spaceId: 'data-platform',
      view: 'portfolios',
      description: 'Portfolio grouping for dashboards, reports, and launch tracking.',
      location: 'Portfolios / Analytics',
    },
    {
      id: 'file-scorecard',
      name: 'Weekly scorecard.xlsx',
      kind: 'file',
      role: 'Owner',
      tags: ['file', 'finance'],
      portfolio: 'Analytics',
      views: 17,
      modifiedAt: '2026-04-27T11:10:00Z',
      spaceId: 'data-platform',
      view: 'your-files',
      description: 'Personal working file pinned into the shared analytics workspace.',
      location: 'Your files / Weekly packs',
    },
    {
      id: 'file-shared',
      name: 'Fleet readiness review',
      kind: 'file',
      role: 'Viewer',
      tags: ['shared', 'briefing'],
      portfolio: 'Operations',
      views: 33,
      modifiedAt: '2026-04-25T18:40:00Z',
      spaceId: 'operations',
      view: 'shared',
      description: 'Shared briefing document prepared by another team.',
      location: 'Shared with you / Briefings',
    },
    {
      id: 'file-trash',
      name: 'Archive backlog draft',
      kind: 'file',
      role: 'Owner',
      tags: ['trash'],
      portfolio: 'General',
      views: 2,
      modifiedAt: '2026-04-16T08:15:00Z',
      spaceId: 'research',
      view: 'trash',
      description: 'Removed draft retained temporarily in trash.',
      location: 'Trash / April',
    },
  ];

  let loading = $state(true);
  let loadingCreate = $state(false);
  let error = $state('');
  let search = $state('');
  let activeView = $state<WorkspaceView>('all');
  let activeSpaceFilter = $state('all');
  let filtersVisible = $state(true);
  let projects = $state<OntologyProject[]>([]);
  let spaceOptions = $state<SpaceOption[]>(FALLBACK_SPACE_OPTIONS);
  let folders = $state<OntologyProjectFolder[]>([]);
  let createdProjectMeta = $state<Record<string, ProjectMeta>>({});
  let createModalOpen = $state(false);
  let createStep = $state(1);
  let draft = $state<CreateProjectDraft>({
    spaceId: '',
    templateId: 'operations',
    name: '',
    description: '',
    createStarterFolder: true,
    starterFolderName: 'Learning',
  });

  const preferredWorkspaceSlug = $derived.by(() => getPreferredWorkspaceSlug($currentUser?.attributes));
  const creatableSpaceCount = $derived.by(() => spaceOptions.filter((option) => option.canCreateProject).length);
  const canCreateProjects = $derived.by(() => creatableSpaceCount > 0);
  const selectedSpace = $derived.by(() => {
    const selectedSpaceId = resolveSelectedSpaceId(spaceOptions, draft.spaceId, preferredWorkspaceSlug);
    const match = spaceOptions.find((option) => option.id === selectedSpaceId);
    if (match) {
      return match;
    }

    return spaceOptions[0] ?? FALLBACK_SPACE_OPTIONS[0];
  });
  const selectedTemplate = $derived.by(
    () => projectTemplates.find((option) => option.id === draft.templateId) ?? projectTemplates[0],
  );
  const projectsById = $derived.by(() => new Map(projects.map((project) => [project.id, project])));
  const allRows = $derived.by(() => [
    ...projects.map((project) => mapProjectToRow(project)),
    ...folders
      .map((folder) => mapFolderToRow(folder))
      .filter((row): row is WorkspaceRow => row !== null),
    ...staticRows,
  ]);
  const visibleRows = $derived.by(() => {
    const query = search.trim().toLowerCase();
    return allRows.filter((row) => {
      if (activeView !== 'all' && row.view !== activeView) {
        return false;
      }

      if (activeSpaceFilter !== 'all' && row.spaceId !== activeSpaceFilter) {
        return false;
      }

      if (!query) {
        return true;
      }

      return [
        row.name,
        row.description,
        row.portfolio,
        row.location,
        row.tags.join(' '),
        resolveSpaceLabel(row.spaceId),
      ]
        .join(' ')
        .toLowerCase()
        .includes(query);
    });
  });
  const totalProjects = $derived.by(() => projects.length);
  const totalFiles = $derived.by(() => allRows.filter((row) => row.kind === 'file').length);
  const totalShared = $derived.by(() => allRows.filter((row) => row.view === 'shared').length);
  const activeFilterCount = $derived.by(() => {
    let count = 0;
    if (activeView !== 'all') count += 1;
    if (activeSpaceFilter !== 'all') count += 1;
    if (search.trim().length > 0) count += 1;
    return count;
  });

  function normalizeSlug(value: string) {
    return value
      .toLowerCase()
      .normalize('NFD')
      .replace(/[\u0300-\u036f]/g, '')
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '')
      .slice(0, 48);
  }

  function resolveSpaceLabel(spaceId: string) {
    return lookupSpaceLabel(spaceOptions, spaceId);
  }

  function inferProjectMeta(project: OntologyProject): ProjectMeta {
    const explicit = createdProjectMeta[project.id];
    if (explicit) {
      return explicit;
    }

    const space =
      spaceOptions.find((option) => option.workspaceSlug === project.workspace_slug) ??
      spaceOptions.find((option) => option.workspaceSlug === preferredWorkspaceSlug) ??
      spaceOptions[0] ??
      FALLBACK_SPACE_OPTIONS[0];

    const description = project.description.toLowerCase();
    const template =
      projectTemplates.find((option) =>
        description.includes(option.suggestedPortfolio.toLowerCase()),
      ) ?? projectTemplates[0];

    return {
      spaceId: space.id,
      templateId: template.id,
    };
  }

  function mapProjectToRow(project: OntologyProject): WorkspaceRow {
    const meta = inferProjectMeta(project);
    const template =
      projectTemplates.find((option) => option.id === meta.templateId) ?? projectTemplates[0];

    return {
      id: `project-${project.id}`,
      name: project.display_name || project.slug,
      kind: 'project',
      role: project.owner_id === $currentUser?.id ? 'Owner' : 'Editor',
      tags: ['project', template.label],
      portfolio: template.suggestedPortfolio,
      views: Math.max(
        PROJECT_VIEW_SEED,
        ((project.display_name || project.slug).length % PROJECT_VIEW_MODULO) + PROJECT_VIEW_SEED,
      ),
      modifiedAt: project.updated_at,
      spaceId: meta.spaceId,
      view: 'projects',
      description:
        project.description ||
        `${template.label} in ${resolveSpaceLabel(meta.spaceId)} for secure shared work.`,
      location: `${resolveSpaceLabel(meta.spaceId)} / ${project.workspace_slug ?? 'workspace'}`,
    };
  }

  function mapFolderToRow(folder: OntologyProjectFolder): WorkspaceRow | null {
    const project = projectsById.get(folder.project_id);
    if (!project) {
      return null;
    }

    const meta = inferProjectMeta(project);
    return {
      id: `folder-${folder.id}`,
      name: folder.name,
      kind: 'folder',
      role: project.owner_id === $currentUser?.id ? 'Owner' : 'Editor',
      tags: ['folder', 'starter'],
      portfolio:
        projectTemplates.find((option) => option.id === meta.templateId)?.suggestedPortfolio ??
        'General',
      views: 1,
      modifiedAt: folder.updated_at,
      spaceId: meta.spaceId,
      view: 'all',
      description:
        folder.description || `Starter folder inside ${project.display_name || project.slug}.`,
      location: `${resolveSpaceLabel(meta.spaceId)} / ${project.display_name || project.slug}`,
    };
  }

  async function loadProjects() {
    loading = true;
    error = '';
    try {
      const response = await listProjects({ page: 1, per_page: 100 });
      projects = response.data;
      // Seed the resource label cache so ConfirmDialog/MoveDialog/Trash
      // entries display human-readable names instead of the
      // "kind · id-prefix" placeholder.
      for (const project of response.data) {
        setResourceLabel('ontology_project', project.id, project.display_name || project.slug);
      }
      const folderGroups = await Promise.all(
        response.data.map(async (project) => listProjectFolders(project.id).catch(() => [])),
      );
      folders = folderGroups.flat();
      for (const folder of folders) {
        setResourceLabel('ontology_folder', folder.id, folder.name);
      }
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Unable to load projects';
      notifications.error(error);
      projects = [];
      folders = [];
    } finally {
      loading = false;
    }
  }

  async function loadSpaceOptions() {
    try {
      const response = await listSpaces();
      const nextSpaceOptions = buildSpaceOptions(response.items, $currentUser);
      spaceOptions = nextSpaceOptions;
      const selectedSpaceId = resolveSelectedSpaceId(
        nextSpaceOptions,
        draft.spaceId,
        preferredWorkspaceSlug,
      );
      if (selectedSpaceId !== draft.spaceId) {
        draft = { ...draft, spaceId: selectedSpaceId };
      }
    } catch (cause) {
      console.warn('Unable to load workspace spaces', cause);
      spaceOptions = FALLBACK_SPACE_OPTIONS;
      const selectedSpaceId = resolveSelectedSpaceId(
        FALLBACK_SPACE_OPTIONS,
        draft.spaceId,
        preferredWorkspaceSlug,
      );
      if (selectedSpaceId !== draft.spaceId) {
        draft = { ...draft, spaceId: selectedSpaceId };
      }
    }
  }

  function resetDraft() {
    draft = {
      spaceId: resolveSelectedSpaceId(spaceOptions, '', preferredWorkspaceSlug),
      templateId: 'operations',
      name: '',
      description: '',
      createStarterFolder: true,
      starterFolderName: 'Learning',
    };
    createStep = 1;
  }

  function openCreateProject() {
    if (!$isAuthenticated) {
      notifications.warning('Sign in to create a project.');
      return;
    }

    if (!canCreateProjects) {
      notifications.warning('No organization space currently allows project creation.');
      return;
    }

    resetDraft();
    createModalOpen = true;
  }

  function closeCreateProject() {
    createModalOpen = false;
    createStep = 1;
    loadingCreate = false;
  }

  function handleNameInput(event: Event) {
    const value = (event.currentTarget as HTMLInputElement).value;
    const previousName = draft.name;
    const shouldSyncFolderName =
      draft.starterFolderName.trim().length === 0 ||
      draft.starterFolderName === 'Learning' ||
      draft.starterFolderName === previousName;

    draft = {
      ...draft,
      name: value,
      starterFolderName: shouldSyncFolderName ? value || 'Learning' : draft.starterFolderName,
    };
  }

  async function submitProject() {
    const trimmedName = draft.name.trim();
    if (!trimmedName) {
      notifications.warning('Add a project name before creating it.');
      return;
    }

    if (!draft.spaceId || !draft.templateId) {
      notifications.warning('Select an organization space and a template.');
      return;
    }

    const slug = normalizeSlug(trimmedName);
    if (!slug) {
      notifications.warning('Use a project name with letters or numbers.');
      return;
    }

    const template =
      projectTemplates.find((option) => option.id === draft.templateId) ?? projectTemplates[0];
    const space = spaceOptions.find((option) => option.id === draft.spaceId) ?? spaceOptions[0];
    if (!space?.canCreateProject) {
      notifications.warning(
        space?.createPermissionReason ?? 'You do not have permission to create a project in this space.',
      );
      return;
    }

    loadingCreate = true;
    error = '';

    try {
      const requestedFolders = draft.createStarterFolder
        ? (() => {
            const customName = draft.starterFolderName.trim();
            const folderList =
              customName.length > 0
                ? [customName, ...template.starterFolders.filter((name) => name !== customName)]
                : template.starterFolders;

            return folderList.map((name) => ({
              name,
              description: `Starter folder inside ${trimmedName}.`,
            }));
          })()
        : [];

      const created = await createProject({
        slug,
        display_name: trimmedName,
        description: draft.description.trim() || template.suggestedDescription,
        workspace_slug: space.workspaceSlug,
        folders: requestedFolders,
      });

      const meta = { spaceId: space.id, templateId: template.id };
      createdProjectMeta = { ...createdProjectMeta, [created.id]: meta };
      projects = [created, ...projects];

      if (requestedFolders.length > 0) {
        try {
          const persistedFolders = await listProjectFolders(created.id);
          folders = [...persistedFolders, ...folders];
        } catch (folderError) {
          console.warn('Unable to reload persisted folders after project creation', folderError);
          notifications.warning('Project created, but starter folders could not be refreshed yet.');
        }
      }

      notifications.success(`Project ${created.display_name || created.slug} created.`);
      activeView = 'projects';
      search = '';
      closeCreateProject();
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Unable to create project';
      notifications.error(error);
    } finally {
      loadingCreate = false;
    }
  }

  function formatDate(value: string) {
    return new Intl.DateTimeFormat('en', {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
    }).format(new Date(value));
  }

  function kindLabel(kind: WorkspaceKind) {
    if (kind === 'portfolio') return 'Portfolio';
    if (kind === 'project') return 'Project';
    if (kind === 'folder') return 'Folder';
    return 'File';
  }

  // ---------------------------------------------------------------------------
  // Phase 3 / Phase 1 backend wiring: real "Shared with you" + "Trash"
  // ---------------------------------------------------------------------------

  let sharedItems = $state<ResourceShare[]>([]);
  let sharedLoading = $state(false);
  let sharedError = $state('');
  let sharedLoaded = false;

  let trashItems = $state<TrashEntry[]>([]);
  let trashLoading = $state(false);
  let trashError = $state('');
  let trashLoaded = false;
  let trashBusyKey = $state<string | null>(null);
  let trashLimit = $state(50);
  let trashHasMore = $state(false);

  async function loadShared(force = false) {
    if (sharedLoading || (!force && sharedLoaded)) return;
    sharedLoading = true;
    sharedError = '';
    try {
      sharedItems = await listSharedWithMe({ limit: 100 });
      sharedLoaded = true;
    } catch (cause) {
      sharedError = cause instanceof Error ? cause.message : 'Unable to load shared items';
      sharedItems = [];
    } finally {
      sharedLoading = false;
    }
  }

  async function loadTrash(force = false) {
    if (trashLoading || (!force && trashLoaded)) return;
    trashLoading = true;
    trashError = '';
    try {
      trashItems = await listTrash({ limit: trashLimit });
      trashHasMore = trashItems.length >= trashLimit;
      trashLoaded = true;
      // Best-effort label resolution for ontology kinds not pre-cached.
      const missing = trashItems
        .filter((entry) => cachedLabel(entry.resource_kind, entry.resource_id).startsWith(`${entry.resource_kind} · `))
        .map((entry) => ({ kind: entry.resource_kind, id: entry.resource_id }));
      if (missing.length > 0) void resolveLabels(missing);
    } catch (cause) {
      trashError = cause instanceof Error ? cause.message : 'Unable to load trash';
      trashItems = [];
    } finally {
      trashLoading = false;
    }
  }

  async function handleRestore(entry: TrashEntry) {
    const key = `${entry.resource_kind}:${entry.resource_id}`;
    trashBusyKey = key;
    try {
      await restoreResource(entry.resource_kind, entry.resource_id);
      notifications.success('Resource restored');
      await loadTrash(true);
      await loadProjects();
    } catch (cause) {
      notifications.error(cause instanceof Error ? cause.message : 'Unable to restore resource');
    } finally {
      trashBusyKey = null;
    }
  }

  async function handlePurge(entry: TrashEntry) {
    const key = `${entry.resource_kind}:${entry.resource_id}`;
    askConfirm({
      title: `Permanently delete ${entry.display_name}?`,
      message: 'This cannot be undone. The resource will be removed from trash and the database.',
      confirmLabel: 'Delete forever',
      danger: true,
      run: async () => {
        trashBusyKey = key;
        try {
          await purgeResource(entry.resource_kind, entry.resource_id);
          notifications.success('Resource permanently deleted');
          await loadTrash(true);
        } catch (cause) {
          notifications.error(cause instanceof Error ? cause.message : 'Unable to purge resource');
        } finally {
          trashBusyKey = null;
        }
      },
    });
  }

  // Lazy-load each tab the first time the user activates it. Keeps the
  // initial render fast and avoids fetching trash for users who never
  // open it.
  $effect(() => {
    if (activeView === 'shared') void loadShared();
    else if (activeView === 'trash') void loadTrash();
  });

  function formatRelative(value: string) {
    const date = new Date(value);
    return new Intl.DateTimeFormat('en', {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    }).format(date);
  }

  // ---------------------------------------------------------------------------
  // Phase 4: row actions, multi-select & bulk ops
  // ---------------------------------------------------------------------------

  type ActionableTarget = { kind: ResourceKind; id: string; label: string; projectId: string | null };

  let selectedKeys = $state<Set<string>>(new Set());
  let bulkBusy = $state(false);
  let trashSelectedKeys = $state<Set<string>>(new Set());
  let trashBulkBusy = $state(false);

  let renameTarget = $state<ActionableTarget | null>(null);
  let moveTarget = $state<ActionableTarget | null>(null);
  let bulkMoveOpen = $state(false);
  let actionBusyKey = $state<string | null>(null);

  // ConfirmDialog: a single pending request encapsulates title + body +
  // executor; the dialog is rendered at template root.
  type ConfirmRequest = {
    title: string;
    message: string;
    confirmLabel: string;
    danger?: boolean;
    run: () => Promise<void>;
  };
  let confirmRequest = $state<ConfirmRequest | null>(null);
  let confirmBusy = $state(false);

  function askConfirm(req: ConfirmRequest) {
    confirmRequest = req;
  }

  async function runConfirm() {
    if (!confirmRequest) return;
    confirmBusy = true;
    try {
      await confirmRequest.run();
      confirmRequest = null;
    } finally {
      confirmBusy = false;
    }
  }

  function cancelConfirm() {
    if (confirmBusy) return;
    confirmRequest = null;
  }

  function rowToTarget(row: WorkspaceRow): ActionableTarget | null {
    if (row.kind === 'project') {
      const id = row.id.replace(/^project-/, '');
      return { kind: 'ontology_project', id, label: row.name, projectId: id };
    }
    if (row.kind === 'folder') {
      const id = row.id.replace(/^folder-/, '');
      const folder = folders.find((f) => f.id === id);
      return {
        kind: 'ontology_folder',
        id,
        label: row.name,
        projectId: folder?.project_id ?? null,
      };
    }
    return null;
  }

  function rowKey(target: ActionableTarget) {
    return `${target.kind}:${target.id}`;
  }

  function isRowSelected(row: WorkspaceRow) {
    const target = rowToTarget(row);
    return target ? selectedKeys.has(rowKey(target)) : false;
  }

  function toggleRowSelected(row: WorkspaceRow) {
    const target = rowToTarget(row);
    if (!target) return;
    const next = new Set(selectedKeys);
    const key = rowKey(target);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    selectedKeys = next;
  }

  function clearSelection() {
    selectedKeys = new Set();
  }

  // ---------------------------------------------------------------------------
  // Drag-and-drop: drag folder rows onto folder rows (move into folder) or
  // project rows (move to project root). Uses the same backend endpoints as
  // the Move dialog (`moveResource` for one item, `batchApply` for multi).
  // ---------------------------------------------------------------------------

  type DragSource = { targets: ActionableTarget[] };
  let dragSource = $state<DragSource | null>(null);
  let dragOverKey = $state<string | null>(null);

  function isDescendantFolder(candidateId: string, ancestorFolderId: string): boolean {
    // Walk up from candidate's chain; if we reach ancestorFolderId, candidate
    // is inside ancestor (or equal). Used to forbid dropping a folder into
    // itself or its own subtree.
    let cursor: string | null = candidateId;
    const byId = new Map(folders.map((f) => [f.id, f] as const));
    while (cursor) {
      if (cursor === ancestorFolderId) return true;
      const node = byId.get(cursor);
      cursor = node?.parent_folder_id ?? null;
    }
    return false;
  }

  function handleRowDragStart(event: DragEvent, row: WorkspaceRow) {
    const target = rowToTarget(row);
    // Only ontology folders can be dragged today; projects can't be moved.
    if (!target || target.kind !== 'ontology_folder') {
      event.preventDefault();
      return;
    }

    // If the dragged row is part of the current selection, drag the whole
    // selection (folders only). Otherwise drag just this row.
    const all = selectedTargets().filter((t) => t.kind === 'ontology_folder');
    const inSelection = all.some((t) => rowKey(t) === rowKey(target));
    const targets = inSelection && all.length > 0 ? all : [target];
    dragSource = { targets };

    if (event.dataTransfer) {
      event.dataTransfer.effectAllowed = 'move';
      // Some browsers refuse to start a drag without a data payload.
      event.dataTransfer.setData('text/plain', targets.map((t) => rowKey(t)).join(','));
    }
  }

  function handleRowDragEnd() {
    dragSource = null;
    dragOverKey = null;
  }

  function dropTargetForRow(row: WorkspaceRow): {
    projectId: string;
    folderId: string | null;
    label: string;
  } | null {
    if (row.kind === 'project') {
      const projectId = row.id.replace(/^project-/, '');
      return { projectId, folderId: null, label: row.name };
    }
    if (row.kind === 'folder') {
      const folderId = row.id.replace(/^folder-/, '');
      const folder = folders.find((f) => f.id === folderId);
      if (!folder) return null;
      return { projectId: folder.project_id, folderId, label: row.name };
    }
    return null;
  }

  function dropAccepts(row: WorkspaceRow): boolean {
    if (!dragSource || dragSource.targets.length === 0) return false;
    const dest = dropTargetForRow(row);
    if (!dest) return false;
    // Forbid dropping onto self or any descendant of any dragged folder.
    return dragSource.targets.every((src) => {
      if (src.kind !== 'ontology_folder') return true;
      if (dest.folderId && isDescendantFolder(dest.folderId, src.id)) return false;
      return true;
    });
  }

  function handleRowDragOver(event: DragEvent, row: WorkspaceRow) {
    if (!dropAccepts(row)) return;
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'move';
    const dest = dropTargetForRow(row);
    if (dest) dragOverKey = `${row.kind}:${dest.folderId ?? dest.projectId}`;
  }

  function handleRowDragLeave(row: WorkspaceRow) {
    const dest = dropTargetForRow(row);
    if (!dest) return;
    const key = `${row.kind}:${dest.folderId ?? dest.projectId}`;
    if (dragOverKey === key) dragOverKey = null;
  }

  async function handleRowDrop(event: DragEvent, row: WorkspaceRow) {
    if (!dropAccepts(row)) return;
    event.preventDefault();
    const dest = dropTargetForRow(row);
    const src = dragSource;
    dragOverKey = null;
    dragSource = null;
    if (!dest || !src) return;

    try {
      if (src.targets.length === 1) {
        const t = src.targets[0];
        await moveResource(t.kind, t.id, {
          target_project_id: dest.projectId,
          target_folder_id: dest.folderId,
        });
        notifications.success(`Moved into ${dest.label}.`);
      } else {
        const actions: BatchAction[] = src.targets.map((t) => ({
          op: 'move',
          resource_kind: t.kind,
          resource_id: t.id,
          target_folder_id: dest.folderId,
        }));
        const { results } = await batchApply(actions);
        const failed = results.filter((r) => !r.ok);
        if (failed.length === 0) {
          notifications.success(`Moved ${results.length} item(s) into ${dest.label}.`);
        } else {
          notifications.warning(
            `${results.length - failed.length} succeeded, ${failed.length} failed.`,
          );
        }
        clearSelection();
      }
      await loadProjects();
    } catch (cause) {
      notifications.error(cause instanceof Error ? cause.message : 'Drag move failed');
    }
  }

  function isRowDropTarget(row: WorkspaceRow): boolean {
    if (!dragSource) return false;
    const dest = dropTargetForRow(row);
    if (!dest) return false;
    return dragOverKey === `${row.kind}:${dest.folderId ?? dest.projectId}`;
  }

  function selectedTargets(): ActionableTarget[] {
    const targets: ActionableTarget[] = [];
    for (const row of allRows) {
      const target = rowToTarget(row);
      if (target && selectedKeys.has(rowKey(target))) targets.push(target);
    }
    return targets;
  }

  function rowActionsFor(row: WorkspaceRow): RowAction[] {
    return [
      { id: 'rename', label: 'Rename', icon: 'pencil' },
      { id: 'duplicate', label: 'Duplicate', icon: 'duplicate' },
      { id: 'move', label: 'Move…', icon: 'move', disabled: row.kind === 'project' },
      { id: 'delete', label: 'Move to trash', icon: 'delete', danger: true },
    ];
  }

  async function handleRowAction(row: WorkspaceRow, actionId: string) {
    const target = rowToTarget(row);
    if (!target) return;
    if (actionId === 'rename') {
      renameTarget = target;
      return;
    }
    if (actionId === 'move') {
      moveTarget = target;
      return;
    }
    if (actionId === 'duplicate') {
      const key = rowKey(target);
      actionBusyKey = key;
      try {
        await duplicateResource(target.kind, target.id, { suffix: ' (copy)' });
        notifications.success('Duplicated successfully.');
        await loadProjects();
      } catch (cause) {
        notifications.error(cause instanceof Error ? cause.message : 'Unable to duplicate');
      } finally {
        actionBusyKey = null;
      }
      return;
    }
    if (actionId === 'delete') {
      askConfirm({
        title: 'Move to trash?',
        message: `Move "${target.label}" to trash. You can restore it later from the Trash tab.`,
        confirmLabel: 'Move to trash',
        danger: true,
        run: async () => {
          const key = rowKey(target);
          actionBusyKey = key;
          try {
            await softDeleteResource(target.kind, target.id);
            notifications.success('Moved to trash.');
            await loadProjects();
            if (trashLoaded) await loadTrash(true);
            const next = new Set(selectedKeys);
            next.delete(key);
            selectedKeys = next;
          } catch (cause) {
            notifications.error(cause instanceof Error ? cause.message : 'Unable to delete');
          } finally {
            actionBusyKey = null;
          }
        },
      });
    }
  }

  async function handleBulkAction(actionId: string) {
    const targets = selectedTargets();
    if (targets.length === 0) return;
    if (actionId === 'move') {
      bulkMoveOpen = true;
      return;
    }
    if (actionId === 'delete') {
      askConfirm({
        title: `Move ${targets.length} item(s) to trash?`,
        message: 'Selected projects and folders will be soft-deleted. Restore them from the Trash tab.',
        confirmLabel: 'Move to trash',
        danger: true,
        run: async () => {
          bulkBusy = true;
          try {
            const actions: BatchAction[] = targets.map((t) => ({
              op: 'soft_delete',
              resource_kind: t.kind,
              resource_id: t.id,
            }));
            const { results } = await batchApply(actions);
            const failed = results.filter((r) => !r.ok);
            if (failed.length === 0) {
              notifications.success(`Moved ${results.length} item(s) to trash.`);
            } else {
              notifications.warning(
                `${results.length - failed.length} succeeded, ${failed.length} failed.`,
              );
            }
            clearSelection();
            await loadProjects();
            if (trashLoaded) await loadTrash(true);
          } catch (cause) {
            notifications.error(cause instanceof Error ? cause.message : 'Bulk delete failed');
          } finally {
            bulkBusy = false;
          }
        },
      });
    }
  }

  // ---------------------------------------------------------------------------
  // Trash multi-select
  // ---------------------------------------------------------------------------

  function trashKey(entry: TrashEntry) {
    return `${entry.resource_kind}:${entry.resource_id}`;
  }

  function isTrashSelected(entry: TrashEntry) {
    return trashSelectedKeys.has(trashKey(entry));
  }

  function toggleTrashSelected(entry: TrashEntry) {
    const key = trashKey(entry);
    const next = new Set(trashSelectedKeys);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    trashSelectedKeys = next;
  }

  function clearTrashSelection() {
    trashSelectedKeys = new Set();
  }

  async function handleTrashBulk(actionId: 'restore' | 'purge') {
    const selected = trashItems.filter((entry) => trashSelectedKeys.has(trashKey(entry)));
    if (selected.length === 0) return;
    const exec = async () => {
      trashBulkBusy = true;
      try {
        const fn = actionId === 'restore' ? restoreResource : purgeResource;
        const results = await Promise.allSettled(
          selected.map((entry) => fn(entry.resource_kind, entry.resource_id)),
        );
        const failed = results.filter((r) => r.status === 'rejected').length;
        if (failed === 0) {
          notifications.success(
            actionId === 'restore'
              ? `Restored ${results.length} item(s).`
              : `Permanently deleted ${results.length} item(s).`,
          );
        } else {
          notifications.warning(`${results.length - failed} succeeded, ${failed} failed.`);
        }
        clearTrashSelection();
        await loadTrash(true);
        if (actionId === 'restore') await loadProjects();
      } finally {
        trashBulkBusy = false;
      }
    };
    if (actionId === 'purge') {
      askConfirm({
        title: `Permanently delete ${selected.length} item(s)?`,
        message: 'This cannot be undone. Selected resources will be removed from trash and the database.',
        confirmLabel: 'Delete forever',
        danger: true,
        run: exec,
      });
    } else {
      await exec();
    }
  }

  onMount(() => {
    resetDraft();
    void loadSpaceOptions();
    void loadProjects();
  });
</script>

<svelte:head>
  <title>OpenFoundry — Projects & files</title>
</svelte:head>

<div class="of-panel overflow-hidden">
  <div class="border-b border-[var(--border-default)] bg-white">
    <div class="flex flex-wrap items-center justify-between gap-3 px-4 py-2">
      <div class="flex flex-wrap items-center gap-1">
        <button type="button" class="rounded-md bg-[#e6eefc] p-2 text-[#356dcb]">
          <Glyph name="folder" size={17} />
        </button>

        {#each viewTabs as tab}
          <button
            type="button"
            class={`inline-flex items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition ${
              activeView === tab.id
                ? 'bg-[#eef4fd] text-[#2458b8]'
                : 'text-[var(--text-default)] hover:bg-[var(--bg-hover)]'
            }`}
            onclick={() => {
              activeView = tab.id;
            }}
          >
            {#if tab.id === 'portfolios'}
              <Glyph name="bookmark" size={14} />
            {:else if tab.id === 'projects'}
              <Glyph name="folder" size={14} />
            {:else if tab.id === 'your-files'}
              <Glyph name="object" size={14} />
            {:else if tab.id === 'shared'}
              <Glyph name="users" size={14} />
            {:else}
              <span aria-hidden="true">🗑</span>
            {/if}
            <span>{tab.label}</span>
          </button>
        {/each}
      </div>

      <button
        type="button"
        class="of-btn of-btn-primary"
        disabled={!canCreateProjects}
        onclick={openCreateProject}
      >
        <Glyph name="plus" size={14} />
        <span>New project</span>
      </button>
    </div>

    <div class="flex flex-wrap items-center gap-3 border-t border-[var(--border-subtle)] bg-[#fafbfd] px-4 py-3">
      <button
        type="button"
        class="inline-flex items-center gap-2 rounded-md px-2 py-1 text-sm font-semibold text-[var(--text-strong)] hover:bg-white"
        onclick={() => {
          activeView = 'all';
        }}
      >
        <Glyph name="artifact" size={14} />
        <span>All files</span>
      </button>

      <label class="inline-flex items-center gap-2 rounded-md px-2 py-1 text-sm font-semibold text-[var(--text-default)] hover:bg-white">
        <Glyph name="folder" size={14} />
        <span class="sr-only">Filter by space</span>
        <select bind:value={activeSpaceFilter} class="bg-transparent outline-none">
          <option value="all">All spaces</option>
          {#each spaceOptions as option}
            <option value={option.id}>{option.label}</option>
          {/each}
        </select>
        <Glyph name="chevron-down" size={12} />
      </label>
    </div>
  </div>

  <div class="space-y-4 bg-[#f5f7fa] p-4">
    {#if filtersVisible}
      <section class="of-panel p-4">
        <div class="mb-3 flex items-center justify-between gap-3">
          <div>
            <div class="text-sm font-semibold text-[var(--text-strong)]">Quick filters</div>
            <p class="m-0 mt-1 text-sm text-[var(--text-muted)]">
              Jump straight into the Files workspace surfaces highlighted in the requested flow.
            </p>
          </div>
          <button
            type="button"
            class="text-sm font-medium text-[var(--text-link)]"
            onclick={() => {
              filtersVisible = false;
            }}
          >
            Hide
          </button>
        </div>

        <div class="grid gap-3 lg:grid-cols-[1fr,1fr,1fr]">
          <article class="rounded-md border border-[var(--border-default)] bg-[#fbfcfe] p-4">
            <div class="flex items-start justify-between gap-3">
              <div>
                <div class="flex items-center gap-2 text-sm font-semibold text-[var(--text-strong)]">
                  <Glyph name="bookmark" size={14} />
                  <span>Portfolios</span>
                </div>
                <p class="m-0 mt-2 text-sm text-[var(--text-muted)]">
                  Organize related projects into reusable groupings by mission, program, or team.
                </p>
              </div>
              <button type="button" class="text-sm font-medium text-[var(--text-link)]" onclick={() => (activeView = 'portfolios')}>
                Apply
              </button>
            </div>
          </article>

          <article class="rounded-md border border-[#7ca3e6] bg-[#eef4fd] p-4 shadow-[inset_0_0_0_1px_rgba(63,123,224,0.08)]">
            <div class="flex items-start justify-between gap-3">
              <div>
                <div class="flex items-center gap-2 text-sm font-semibold text-[var(--text-strong)]">
                  <Glyph name="folder" size={14} />
                  <span>Projects</span>
                </div>
                <p class="m-0 mt-2 text-sm text-[var(--text-default)]">
                  Secure containers for related files with one place to manage permissions and starter folders.
                </p>
              </div>
              <button type="button" class="text-sm font-medium text-[var(--text-link)]" onclick={() => (activeView = 'projects')}>
                Apply
              </button>
            </div>
          </article>

          <article class="rounded-md border border-[var(--border-default)] bg-[#fbfcfe] p-4">
            <div class="flex items-start justify-between gap-3">
              <div>
                <div class="flex items-center gap-2 text-sm font-semibold text-[var(--text-strong)]">
                  <span class="inline-flex h-4 w-4 items-center justify-center rounded-full bg-[#7258d6] text-[10px] text-white">✓</span>
                  <span>Promoted items</span>
                </div>
                <p class="m-0 mt-2 text-sm text-[var(--text-muted)]">
                  Surface the most helpful projects, folders, and files to help users start safely.
                </p>
              </div>
              <button
                type="button"
                class="text-sm font-medium text-[var(--text-link)]"
                onclick={() => {
                  search = 'project';
                  activeView = 'all';
                }}
              >
                Apply
              </button>
            </div>
          </article>
        </div>
      </section>
    {:else}
      <div class="flex justify-end">
        <button
          type="button"
          class="text-sm font-medium text-[var(--text-link)]"
          onclick={() => {
            filtersVisible = true;
          }}
        >
          Show quick filters
        </button>
      </div>
    {/if}

    {#if activeView === 'shared'}
      <section class="of-panel p-4">
        <div class="flex items-center justify-between gap-3">
          <div>
            <div class="text-sm font-semibold text-[var(--text-strong)]">Shared with you</div>
            <p class="m-0 mt-1 text-sm text-[var(--text-muted)]">
              Resources other users have explicitly shared with you. Expired shares are filtered out automatically.
            </p>
          </div>
          <button
            type="button"
            class="text-sm font-medium text-[var(--text-link)]"
            onclick={() => loadShared(true)}
          >
            Refresh
          </button>
        </div>

        {#if sharedLoading}
          <div class="mt-4 text-sm text-[var(--text-muted)]">Loading shared items…</div>
        {:else if sharedError}
          <div class="mt-4 text-sm text-[#b42318]">{sharedError}</div>
        {:else if sharedItems.length === 0}
          <div class="mt-4 rounded-md border border-dashed border-[var(--border-default)] bg-[#fbfcfe] p-6 text-center text-sm text-[var(--text-muted)]">
            Nothing has been shared with you yet.
          </div>
        {:else}
          <ul class="m-0 mt-4 list-none space-y-2 p-0">
            {#each sharedItems as share (share.id)}
              <li class="flex items-center justify-between gap-3 rounded-md border border-[var(--border-default)] bg-[#fbfcfe] px-3 py-3">
                <div class="flex min-w-0 items-start gap-3">
                  <span class="mt-0.5 rounded-md border border-[var(--border-default)] bg-white p-2 text-[var(--text-muted)]">
                    <Glyph name="users" size={14} />
                  </span>
                  <div class="min-w-0">
                    <div class="flex flex-wrap items-center gap-2">
                      <span class="truncate font-medium text-[var(--text-strong)]">
                        {share.resource_kind} · {share.resource_id.slice(0, 8)}…
                      </span>
                      <span class="of-chip">{share.access_level}</span>
                    </div>
                    <div class="mt-1 text-xs text-[var(--text-muted)]">
                      Shared by {share.sharer_id.slice(0, 8)}… · {formatRelative(share.created_at)}
                      {#if share.expires_at} · expires {formatRelative(share.expires_at)}{/if}
                    </div>
                    {#if share.note}
                      <div class="mt-1 text-xs text-[var(--text-soft)]">{share.note}</div>
                    {/if}
                  </div>
                </div>
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    {/if}

    {#if activeView === 'trash'}
      <section class="of-panel p-4">
        <div class="flex items-center justify-between gap-3">
          <div>
            <div class="text-sm font-semibold text-[var(--text-strong)]">Trash</div>
            <p class="m-0 mt-1 text-sm text-[var(--text-muted)]">
              Soft-deleted projects, folders and resources. Restore puts them back where they were; purge removes them permanently.
            </p>
          </div>
          <button
            type="button"
            class="text-sm font-medium text-[var(--text-link)]"
            onclick={() => loadTrash(true)}
          >
            Refresh
          </button>
        </div>

        {#if trashLoading}
          <div class="mt-4 text-sm text-[var(--text-muted)]">Loading trash…</div>
        {:else if trashError}
          <div class="mt-4 text-sm text-[#b42318]">{trashError}</div>
        {:else if trashItems.length === 0}
          <div class="mt-4 rounded-md border border-dashed border-[var(--border-default)] bg-[#fbfcfe] p-6 text-center text-sm text-[var(--text-muted)]">
            Trash is empty.
          </div>
        {:else}
          <BulkActionsToolbar
            count={trashSelectedKeys.size}
            busy={trashBulkBusy}
            onAction={(id) => void handleTrashBulk(id as 'restore' | 'purge')}
            onClear={clearTrashSelection}
            actions={[
              { id: 'restore', label: 'Restore selected' },
              { id: 'purge', label: 'Purge selected', danger: true },
            ]}
          />
          <ul class="m-0 mt-4 list-none space-y-2 p-0">
            {#each trashItems as entry (`${entry.resource_kind}:${entry.resource_id}`)}
              {@const key = `${entry.resource_kind}:${entry.resource_id}`}
              {@const busy = trashBusyKey === key}
              <li class="flex items-center justify-between gap-3 rounded-md border border-[var(--border-default)] bg-[#fbfcfe] px-3 py-3">
                <div class="flex min-w-0 items-start gap-3">
                  <input
                    type="checkbox"
                    class="mt-2"
                    aria-label={`Select ${entry.display_name}`}
                    checked={isTrashSelected(entry)}
                    onchange={() => toggleTrashSelected(entry)}
                  />
                  <span class="mt-0.5 rounded-md border border-[var(--border-default)] bg-white p-2 text-[var(--text-muted)]">
                    <Glyph name="folder" size={14} />
                  </span>
                  <div class="min-w-0">
                    <div class="flex flex-wrap items-center gap-2">
                      <span class="truncate font-medium text-[var(--text-strong)]">
                        {cachedLabel(entry.resource_kind, entry.resource_id) ?? entry.display_name}
                      </span>
                      <span class="of-chip">{entry.resource_kind}</span>
                    </div>
                    <div class="mt-1 text-xs text-[var(--text-muted)]">
                      Deleted {formatRelative(entry.deleted_at)} by {entry.deleted_by.slice(0, 8)}…
                    </div>
                  </div>
                </div>
                <div class="flex items-center gap-2">
                  <button
                    type="button"
                    class="of-btn"
                    disabled={busy}
                    onclick={() => handleRestore(entry)}
                  >
                    Restore
                  </button>
                  <button
                    type="button"
                    class="of-btn"
                    disabled={busy}
                    onclick={() => handlePurge(entry)}
                  >
                    Purge
                  </button>
                </div>
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    {/if}

    <div class="of-search-shell">
      <div class="of-search-filter">
        <Glyph name="search" size={15} />
      </div>
      <div class="of-search-input-wrap">
        <input
          bind:value={search}
          class="of-search-input"
          placeholder="Search all portfolios, projects, folders and files..."
          type="search"
        />
      </div>
    </div>

    <BulkActionsToolbar
      count={selectedKeys.size}
      busy={bulkBusy}
      onAction={(id) => void handleBulkAction(id)}
      onClear={clearSelection}
      actions={[
        { id: 'move', label: 'Move…' },
        { id: 'delete', label: 'Move to trash', danger: true },
      ]}
    />

    <section class="of-panel overflow-hidden">
      <div class="grid min-h-[420px] grid-cols-[44px,1fr]">
        <div class="border-r border-[var(--border-default)] bg-[#f8fafc]">
          <div class="flex h-14 items-center justify-center border-b border-[var(--border-default)] text-[var(--text-muted)]">
            <Glyph name="menu" size={14} />
          </div>
          <div class="flex items-center justify-center py-3">
            <span class="of-badge">{activeFilterCount}</span>
          </div>
        </div>

        <div class="flex flex-col">
          <div class="grid grid-cols-[minmax(0,2.6fr),80px,120px,180px,160px,130px] border-b border-[var(--border-default)] bg-[#f7f9fc] px-4 py-3 text-[11px] font-bold uppercase tracking-[0.08em] text-[var(--text-muted)]">
            <div>File name</div>
            <div>Views</div>
            <div>Your role</div>
            <div>Tags</div>
            <div>Portfolio</div>
            <div>Last modified</div>
          </div>

          {#if error}
            <div class="border-b border-[var(--border-subtle)] bg-[#fff7f7] px-4 py-3 text-sm text-[#b42318]">
              {error}
            </div>
          {/if}

          {#if loading}
            <div class="flex min-h-[360px] items-center justify-center text-sm text-[var(--text-muted)]">
              Loading projects and files…
            </div>
          {:else if visibleRows.length === 0}
            <div class="flex min-h-[360px] flex-col items-center justify-center gap-3 px-6 text-center">
              <div class="rounded-full bg-[#eef4fd] p-3 text-[#356dcb]">
                <Glyph name="folder" size={20} />
              </div>
              <div class="text-lg font-semibold text-[var(--text-strong)]">No matching items</div>
              <p class="m-0 max-w-xl text-sm text-[var(--text-muted)]">
                Try another filter or create a new project so this workspace has a secure place for files and starter folders.
              </p>
            </div>
          {:else}
            <div class="divide-y divide-[var(--border-subtle)]">
              {#each visibleRows as row (row.id)}
                {@const projectIdRaw = row.kind === 'project' ? row.id.replace(/^project-/, '') : ''}
                {@const folderIdRaw = row.kind === 'folder' ? row.id.replace(/^folder-/, '') : ''}
                {@const folderProjectId = folderIdRaw
                  ? folders.find((f) => f.id === folderIdRaw)?.project_id ?? ''
                  : ''}
                {@const navigationHref =
                  row.kind === 'project'
                    ? `/projects/${projectIdRaw}`
                    : row.kind === 'folder' && folderProjectId
                      ? `/projects/${folderProjectId}/${folderIdRaw}`
                      : ''}
                <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
                <div
                  class={`grid grid-cols-[minmax(0,2.6fr),80px,120px,180px,160px,130px] items-start gap-3 px-4 py-4 transition ${
                    isRowDropTarget(row)
                      ? 'bg-[#eef4fd] ring-2 ring-inset ring-[#3f7be0]'
                      : navigationHref
                        ? 'cursor-pointer hover:bg-[#fbfdff]'
                        : 'hover:bg-[#fbfdff]'
                  }`}
                  role={navigationHref ? 'link' : undefined}
                  tabindex={navigationHref ? 0 : undefined}
                  draggable={row.kind === 'folder'}
                  ondragstart={(event) => handleRowDragStart(event, row)}
                  ondragend={handleRowDragEnd}
                  ondragover={(event) => handleRowDragOver(event, row)}
                  ondragleave={() => handleRowDragLeave(row)}
                  ondrop={(event) => void handleRowDrop(event, row)}
                  onclick={() => {
                    if (navigationHref) void goto(navigationHref);
                  }}
                  onkeydown={(event) => {
                    if (!navigationHref) return;
                    if (event.key === 'Enter' || event.key === ' ') {
                      event.preventDefault();
                      void goto(navigationHref);
                    }
                  }}
                >
                  <div class="min-w-0">
                    <div class="flex items-start gap-3">
                      {#if row.kind === 'project' || row.kind === 'folder'}
                        <input
                          type="checkbox"
                          class="mt-2"
                          aria-label={`Select ${row.name}`}
                          checked={isRowSelected(row)}
                          onclick={(event) => event.stopPropagation()}
                          onchange={() => toggleRowSelected(row)}
                        />
                      {/if}
                      <div class="mt-0.5 rounded-md border border-[var(--border-default)] bg-white p-2 text-[var(--text-muted)]">
                        {#if row.kind === 'portfolio'}
                          <Glyph name="bookmark" size={14} />
                        {:else if row.kind === 'project'}
                          <Glyph name="folder" size={14} />
                        {:else if row.kind === 'folder'}
                          <Glyph name="folder" size={14} />
                        {:else}
                          <Glyph name="artifact" size={14} />
                        {/if}
                      </div>
                      <div class="min-w-0 flex-1">
                        <div class="flex flex-wrap items-center gap-2">
                          <div class="truncate font-semibold text-[var(--text-strong)]">{row.name}</div>
                          <span class="of-chip">{kindLabel(row.kind)}</span>
                          <span class="of-chip">{resolveSpaceLabel(row.spaceId)}</span>
                        </div>
                        <div class="mt-1 text-sm text-[var(--text-muted)]">{row.description}</div>
                        <div class="mt-2 text-xs text-[var(--text-soft)]">{row.location}</div>
                      </div>
                      {#if row.kind === 'project' || row.kind === 'folder'}
                        <RowActionsMenu
                          actions={rowActionsFor(row)}
                          onSelect={(actionId) => void handleRowAction(row, actionId)}
                          label={`Actions for ${row.name}`}
                        />
                      {/if}
                    </div>
                  </div>

                  <div class="pt-1 text-sm text-[var(--text-default)]">{row.views}</div>
                  <div class="pt-1 text-sm text-[var(--text-default)]">{row.role}</div>
                  <div class="flex flex-wrap gap-2 pt-1">
                    {#each row.tags as tag}
                      <span class="of-chip">{tag}</span>
                    {/each}
                  </div>
                  <div class="pt-1 text-sm text-[var(--text-default)]">{row.portfolio}</div>
                  <div class="pt-1 text-sm text-[var(--text-default)]">{formatDate(row.modifiedAt)}</div>
                </div>
              {/each}
            </div>
          {/if}
        </div>
      </div>
    </section>

    <section class="grid gap-4 xl:grid-cols-[1.2fr,0.8fr]">
      <div class="of-panel p-4">
        <div class="flex items-center justify-between gap-3">
          <div>
            <div class="text-sm font-semibold text-[var(--text-strong)]">Workspace summary</div>
            <p class="m-0 mt-1 text-sm text-[var(--text-muted)]">
              Mirror the Files landing page with project creation and safer entry points than home folders.
            </p>
          </div>
        </div>

        <div class="mt-4 grid gap-3 sm:grid-cols-3">
          <div class="rounded-md border border-[var(--border-default)] bg-[#fbfcfe] p-4">
            <div class="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--text-muted)]">Projects</div>
            <div class="mt-2 text-3xl font-semibold text-[var(--text-strong)]">{totalProjects}</div>
          </div>
          <div class="rounded-md border border-[var(--border-default)] bg-[#fbfcfe] p-4">
            <div class="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--text-muted)]">Files</div>
            <div class="mt-2 text-3xl font-semibold text-[var(--text-strong)]">{totalFiles}</div>
          </div>
          <div class="rounded-md border border-[var(--border-default)] bg-[#fbfcfe] p-4">
            <div class="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--text-muted)]">Shared</div>
            <div class="mt-2 text-3xl font-semibold text-[var(--text-strong)]">{totalShared}</div>
          </div>
        </div>
      </div>

      <div class="of-panel p-4">
        <div class="text-sm font-semibold text-[var(--text-strong)]">Promoted items</div>
        <ul class="m-0 mt-3 space-y-3 pl-5 text-sm text-[var(--text-default)]">
          {#each promotedItems as item}
            <li>{item}</li>
          {/each}
        </ul>
      </div>
    </section>
  </div>
</div>

{#if createModalOpen}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-[rgba(15,23,42,0.45)] p-4">
    <div class="w-full max-w-3xl overflow-hidden rounded-xl border border-[var(--border-default)] bg-white shadow-[0_18px_48px_rgba(15,23,42,0.22)]">
      <div class="flex items-start justify-between gap-4 border-b border-[var(--border-default)] px-6 py-5">
        <div>
          <div class="text-xs font-semibold uppercase tracking-[0.2em] text-[var(--text-muted)]">
            Step {createStep} of 2
          </div>
          <h1 class="m-0 mt-2 text-2xl font-semibold text-[var(--text-strong)]">Create a project</h1>
          <p class="m-0 mt-2 text-sm text-[var(--text-muted)]">
            Choose a secure organization space, start from a template, and avoid working from home folders.
          </p>
        </div>
        <button type="button" class="of-btn of-btn-ghost" onclick={closeCreateProject}>Close</button>
      </div>

      <div class="grid gap-6 px-6 py-5 lg:grid-cols-[1.25fr,0.75fr]">
        <div class="space-y-5">
          {#if createStep === 1}
            <section class="space-y-4">
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-[var(--text-strong)]"
                  for="project-space"
                >
                  Organization space
                </label>
                <select bind:value={draft.spaceId} class="of-select" id="project-space">
                  {#each spaceOptions as option}
                    <option disabled={!option.canCreateProject} value={option.id}>
                      {option.label}{option.canCreateProject ? '' : ' — unavailable'}
                    </option>
                  {/each}
                </select>
                <p class="m-0 mt-2 text-sm text-[var(--text-muted)]">
                  {selectedSpace.description}
                </p>
                <p class="m-0 mt-2 text-xs text-[var(--text-soft)]">
                  {creatableSpaceCount} space{creatableSpaceCount === 1 ? '' : 's'} currently allow
                  project creation. Spaces without access are disabled.
                </p>
                {#if !selectedSpace.canCreateProject && selectedSpace.createPermissionReason}
                  <p class="m-0 mt-2 text-sm text-[#b42318]">{selectedSpace.createPermissionReason}</p>
                {/if}
              </div>

              <div>
                <div class="mb-2 block text-sm font-medium text-[var(--text-strong)]">Template</div>
                <div class="grid gap-3 md:grid-cols-2">
                  {#each projectTemplates as template}
                    <button
                      type="button"
                      class={`rounded-md border p-4 text-left transition ${
                        draft.templateId === template.id
                          ? 'border-[#3f7be0] bg-[#eef4fd]'
                          : 'border-[var(--border-default)] bg-[#fbfcfe] hover:border-[#b7c7df]'
                      }`}
                      onclick={() => {
                        draft = { ...draft, templateId: template.id };
                      }}
                    >
                      <div class="font-semibold text-[var(--text-strong)]">{template.label}</div>
                      <div class="mt-2 text-sm text-[var(--text-muted)]">{template.description}</div>
                    </button>
                  {/each}
                </div>
              </div>

              <div>
                <label
                  class="mb-2 block text-sm font-medium text-[var(--text-strong)]"
                  for="project-name"
                >
                  Project name
                </label>
                <input
                  class="of-input"
                  id="project-name"
                  oninput={handleNameInput}
                  placeholder="Learning"
                  value={draft.name}
                />
              </div>

              <div>
                <label
                  class="mb-2 block text-sm font-medium text-[var(--text-strong)]"
                  for="project-description"
                >
                  Description
                </label>
                <textarea
                  bind:value={draft.description}
                  class="of-textarea !min-h-[110px]"
                  id="project-description"
                  placeholder={selectedTemplate.suggestedDescription}
                ></textarea>
              </div>
            </section>
          {:else}
            <section class="space-y-4">
              <div class="rounded-md border border-[var(--border-default)] bg-[#fbfcfe] p-4">
                <div class="text-sm font-semibold text-[var(--text-strong)]">Starter content</div>
                <p class="m-0 mt-2 text-sm text-[var(--text-muted)]">
                  Optionally seed the new project with a folder structure so users can continue the Files workflow safely.
                </p>

                <label class="mt-4 flex items-start gap-3 text-sm text-[var(--text-default)]">
                  <input
                    bind:checked={draft.createStarterFolder}
                    class="mt-1"
                    type="checkbox"
                  />
                  <span>Create starter folders for this project</span>
                </label>

                <div class="mt-4">
                  <label
                    class="mb-2 block text-sm font-medium text-[var(--text-strong)]"
                    for="project-primary-folder"
                  >
                    Primary folder name
                  </label>
                  <input
                    bind:value={draft.starterFolderName}
                    class="of-input"
                    disabled={!draft.createStarterFolder}
                    id="project-primary-folder"
                    placeholder="Learning"
                  />
                </div>
              </div>

              <div class="rounded-md border border-[var(--border-default)] bg-white p-4">
                <div class="text-sm font-semibold text-[var(--text-strong)]">Review</div>
                <dl class="m-0 mt-3 space-y-3 text-sm">
                  <div class="flex items-start justify-between gap-4">
                    <dt class="text-[var(--text-muted)]">Space</dt>
                    <dd class="m-0 text-right text-[var(--text-strong)]">{selectedSpace.label}</dd>
                  </div>
                  <div class="flex items-start justify-between gap-4">
                    <dt class="text-[var(--text-muted)]">Template</dt>
                    <dd class="m-0 text-right text-[var(--text-strong)]">{selectedTemplate.label}</dd>
                  </div>
                  <div class="flex items-start justify-between gap-4">
                    <dt class="text-[var(--text-muted)]">Project name</dt>
                    <dd class="m-0 text-right text-[var(--text-strong)]">{draft.name || '—'}</dd>
                  </div>
                  <div class="flex items-start justify-between gap-4">
                    <dt class="text-[var(--text-muted)]">Workspace slug</dt>
                    <dd class="m-0 text-right text-[var(--text-strong)]">{selectedSpace.workspaceSlug}</dd>
                  </div>
                </dl>
              </div>
            </section>
          {/if}
        </div>

        <aside class="space-y-4 rounded-lg bg-[#f8fafc] p-4">
          <div>
            <div class="text-sm font-semibold text-[var(--text-strong)]">Template preview</div>
            <p class="m-0 mt-2 text-sm text-[var(--text-muted)]">
              {selectedTemplate.description}
            </p>
          </div>

          <div>
            <div class="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--text-muted)]">
              Suggested portfolio
            </div>
            <div class="mt-2 text-sm font-medium text-[var(--text-strong)]">
              {selectedTemplate.suggestedPortfolio}
            </div>
          </div>

          <div>
            <div class="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--text-muted)]">
              Starter folders
            </div>
            <div class="mt-3 flex flex-wrap gap-2">
              {#each selectedTemplate.starterFolders as folder}
                <span class="of-chip">{folder}</span>
              {/each}
            </div>
          </div>

          <div class="rounded-md border border-[var(--border-default)] bg-white p-4 text-sm text-[var(--text-muted)]">
            Project creation is limited to spaces your organization can actively use.
          </div>
        </aside>
      </div>

      <div class="flex items-center justify-between gap-3 border-t border-[var(--border-default)] bg-[#fafbfd] px-6 py-4">
        <div class="text-sm text-[var(--text-muted)]">
          {#if createStep === 1}
            Select space, template, and name before continuing.
          {:else}
            Create the project and optionally seed starter folders.
          {/if}
        </div>

        <div class="flex items-center gap-3">
          {#if createStep === 2}
            <button
              type="button"
              class="of-btn"
              onclick={() => {
                createStep = 1;
              }}
            >
              Back
            </button>
          {/if}

          {#if createStep === 1}
            <button
              type="button"
              class="of-btn of-btn-primary"
              disabled={!selectedSpace.canCreateProject}
              onclick={() => {
                if (!draft.name.trim()) {
                  notifications.warning('Add a project name before continuing.');
                  return;
                }
                if (!selectedSpace.canCreateProject) {
                  notifications.warning(
                    selectedSpace.createPermissionReason ??
                      'You do not have permission to create a project in this space.',
                  );
                  return;
                }
                createStep = 2;
              }}
            >
              Continue
            </button>
          {:else}
            <button type="button" class="of-btn of-btn-primary" disabled={loadingCreate} onclick={submitProject}>
              {#if loadingCreate}Creating…{:else}Create project{/if}
            </button>
          {/if}
        </div>
      </div>
    </div>
  </div>
{/if}

<RenameDialog
  open={renameTarget !== null}
  resourceKind={renameTarget?.kind ?? null}
  resourceId={renameTarget?.id ?? null}
  currentName={renameTarget?.label ?? ''}
  onClose={() => (renameTarget = null)}
  onRenamed={() => void loadProjects()}
/>

<MoveDialog
  open={moveTarget !== null}
  resourceKind={moveTarget?.kind ?? null}
  resourceId={moveTarget?.id ?? null}
  resourceLabel={moveTarget?.label}
  initialProjectId={moveTarget?.projectId ?? null}
  {projects}
  onClose={() => (moveTarget = null)}
  onMoved={() => void loadProjects()}
/>

<MoveDialog
  open={bulkMoveOpen}
  resourceKind={null}
  resourceId={null}
  initialProjectId={null}
  targets={selectedTargets().map((t) => ({ kind: t.kind, id: t.id, label: t.label }))}
  {projects}
  onClose={() => (bulkMoveOpen = false)}
  onMoved={() => {
    bulkMoveOpen = false;
    clearSelection();
    void loadProjects();
  }}
/>

<ConfirmDialog
  open={confirmRequest !== null}
  title={confirmRequest?.title ?? ''}
  message={confirmRequest?.message ?? ''}
  confirmLabel={confirmRequest?.confirmLabel ?? 'Confirm'}
  danger={confirmRequest?.danger ?? false}
  busy={confirmBusy}
  onConfirm={() => void runConfirm()}
  onCancel={cancelConfirm}
/>
