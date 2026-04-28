<script lang="ts">
  import { onMount } from 'svelte';
  import Glyph from '$components/ui/Glyph.svelte';
  import {
    createProject,
    listProjects,
    type OntologyProject,
  } from '$lib/api/ontology';
  import { notifications } from '$lib/stores/notifications';
  import { auth } from '$stores/auth';

  type WorkspaceView = 'all' | 'portfolios' | 'projects' | 'your-files' | 'shared' | 'trash';
  type WorkspaceKind = 'project' | 'portfolio' | 'file' | 'folder';
  type ProjectTemplateId = 'blank' | 'operations' | 'analytics' | 'ontology';

  type SpaceOption = {
    id: string;
    label: string;
    workspaceSlug: string;
    description: string;
  };

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

  const viewTabs: Array<{ id: WorkspaceView; label: string }> = [
    { id: 'portfolios', label: 'Portfolios' },
    { id: 'projects', label: 'Projects' },
    { id: 'your-files', label: 'Your files' },
    { id: 'shared', label: 'Shared with you' },
    { id: 'trash', label: 'Trash' },
  ];

  const spaceOptions: SpaceOption[] = [
    {
      id: 'operations',
      label: 'Operations',
      workspaceSlug: 'operations',
      description: 'Shared space for operational workflows and secure project containers.',
    },
    {
      id: 'data-platform',
      label: 'Data Platform',
      workspaceSlug: 'data-platform',
      description: 'Central engineering space for data products, pipelines, and platform tools.',
    },
    {
      id: 'research',
      label: 'Research',
      workspaceSlug: 'research',
      description: 'Sandboxed space for experiments, notebooks, and exploratory delivery.',
    },
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
  let filtersVisible = $state(true);
  let projects = $state<OntologyProject[]>([]);
  let localFolders = $state<WorkspaceRow[]>([]);
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

  const selectedSpace = $derived.by(
    () => spaceOptions.find((option) => option.id === draft.spaceId) ?? spaceOptions[0],
  );
  const selectedTemplate = $derived.by(
    () => projectTemplates.find((option) => option.id === draft.templateId) ?? projectTemplates[0],
  );
  const allRows = $derived.by(() => [
    ...projects.map((project) => mapProjectToRow(project)),
    ...localFolders,
    ...staticRows,
  ]);
  const visibleRows = $derived.by(() => {
    const query = search.trim().toLowerCase();
    return allRows.filter((row) => {
      if (activeView !== 'all' && row.view !== activeView) {
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
    return spaceOptions.find((option) => option.id === spaceId)?.label ?? 'Operations';
  }

  function inferProjectMeta(project: OntologyProject): ProjectMeta {
    const explicit = createdProjectMeta[project.id];
    if (explicit) {
      return explicit;
    }

    const space =
      spaceOptions.find((option) => option.workspaceSlug === project.workspace_slug) ??
      spaceOptions.find((option) => option.id === $currentUser?.organization_id) ??
      spaceOptions[0];

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
      views: Math.max(4, ((project.display_name || project.slug).length % 12) + 4),
      modifiedAt: project.updated_at,
      spaceId: meta.spaceId,
      view: 'projects',
      description:
        project.description ||
        `${template.label} in ${resolveSpaceLabel(meta.spaceId)} for secure shared work.`,
      location: `${resolveSpaceLabel(meta.spaceId)} / ${project.workspace_slug ?? 'workspace'}`,
    };
  }

  function createFolderRow(project: OntologyProject, folderName: string, meta: ProjectMeta): WorkspaceRow {
    return {
      id: `folder-${project.id}-${normalizeSlug(folderName)}-${crypto.randomUUID()}`,
      name: folderName,
      kind: 'folder',
      role: 'Owner',
      tags: ['folder', 'starter'],
      portfolio:
        projectTemplates.find((option) => option.id === meta.templateId)?.suggestedPortfolio ??
        'General',
      views: 1,
      modifiedAt: new Date().toISOString(),
      spaceId: meta.spaceId,
      view: 'all',
      description: `Starter folder inside ${project.display_name || project.slug}.`,
      location: `${resolveSpaceLabel(meta.spaceId)} / ${project.display_name || project.slug}`,
    };
  }

  async function loadProjects() {
    loading = true;
    error = '';
    try {
      const response = await listProjects({ page: 1, per_page: 100 });
      projects = response.data;
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Unable to load projects';
      notifications.error(error);
      projects = [];
    } finally {
      loading = false;
    }
  }

  function resetDraft() {
    draft = {
      spaceId:
        spaceOptions.find((option) => option.id === $currentUser?.organization_id)?.id ??
        spaceOptions[0].id,
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
    const shouldSyncFolderName =
      draft.starterFolderName.trim().length === 0 ||
      draft.starterFolderName === 'Learning' ||
      draft.starterFolderName === draft.name;

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

    loadingCreate = true;
    error = '';

    try {
      const created = await createProject({
        slug,
        display_name: trimmedName,
        description: draft.description.trim() || template.suggestedDescription,
        workspace_slug: space.workspaceSlug,
      });

      const meta = { spaceId: space.id, templateId: template.id };
      createdProjectMeta = { ...createdProjectMeta, [created.id]: meta };
      projects = [created, ...projects];

      if (draft.createStarterFolder) {
        const customName = draft.starterFolderName.trim();
        const folderList =
          customName.length > 0
            ? [customName, ...template.starterFolders.filter((name) => name !== customName)]
            : template.starterFolders;

        localFolders = [
          ...folderList.map((folderName) => createFolderRow(created, folderName, meta)),
          ...localFolders,
        ];
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

  onMount(() => {
    resetDraft();
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

      <button type="button" class="of-btn of-btn-primary" onclick={openCreateProject}>
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

      <button type="button" class="inline-flex items-center gap-2 rounded-md px-2 py-1 text-sm font-semibold text-[var(--text-default)] hover:bg-white">
        <Glyph name="folder" size={14} />
        <span>All spaces</span>
        <Glyph name="chevron-down" size={12} />
      </button>
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
                <div class="grid grid-cols-[minmax(0,2.6fr),80px,120px,180px,160px,130px] items-start gap-3 px-4 py-4 hover:bg-[#fbfdff]">
                  <div class="min-w-0">
                    <div class="flex items-start gap-3">
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
                      <div class="min-w-0">
                        <div class="flex flex-wrap items-center gap-2">
                          <div class="truncate font-semibold text-[var(--text-strong)]">{row.name}</div>
                          <span class="of-chip">{kindLabel(row.kind)}</span>
                          <span class="of-chip">{resolveSpaceLabel(row.spaceId)}</span>
                        </div>
                        <div class="mt-1 text-sm text-[var(--text-muted)]">{row.description}</div>
                        <div class="mt-2 text-xs text-[var(--text-soft)]">{row.location}</div>
                      </div>
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
                    <option value={option.id}>{option.label}</option>
                  {/each}
                </select>
                <p class="m-0 mt-2 text-sm text-[var(--text-muted)]">
                  {selectedSpace.description}
                </p>
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
            If you do not have permission to create a project yet, this flow makes the required fields explicit so the UI can be connected to permission checks later.
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
              onclick={() => {
                if (!draft.name.trim()) {
                  notifications.warning('Add a project name before continuing.');
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
