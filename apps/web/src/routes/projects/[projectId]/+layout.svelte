<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { page } from '$app/state';
  import Glyph from '$components/ui/Glyph.svelte';
  import ProjectBreadcrumb, { type BreadcrumbItem } from '$components/workspace/ProjectBreadcrumb.svelte';
  import FolderTree from '$components/workspace/FolderTree.svelte';
  import MoveDialog from '$components/workspace/MoveDialog.svelte';
  import {
    getProject,
    listProjectFolders,
    listProjectResources,
    listProjects,
    type OntologyProject,
    type OntologyProjectFolder,
    type OntologyProjectResourceBinding,
  } from '$lib/api/ontology';
  import { recordAccess } from '$lib/api/workspace';
  import { batchApply, moveResource, type BatchAction } from '$lib/api/workspace';
  import { notifications } from '$stores/notifications';
  import { setProjectWorkspaceContext, type DragSource, type DragTarget } from './context';

  let { children }: { children: any } = $props();

  let project = $state<OntologyProject | null>(null);
  let folders = $state<OntologyProjectFolder[]>([]);
  let resources = $state<OntologyProjectResourceBinding[]>([]);
  let loading = $state(true);
  let error = $state('');

  // Drag bus shared with detail pages via context. Lives at the layout
  // level so a drag started on a card survives a navigation between
  // [projectId] ↔ [folderId] (the FolderTree is the same component).
  let dragSource = $state<DragSource | null>(null);

  // Cross-project move (Phase 6 follow-up) — when the user drops a drag
  // source onto the "Move to other project…" zone we open the existing
  // MoveDialog pre-loaded with the targets and the full project list.
  let allProjects = $state<OntologyProject[]>([]);
  let crossMoveTargets = $state<DragTarget[] | null>(null);
  let crossDragOver = $state(false);

  const projectId = $derived(page.params.projectId ?? '');
  const folderId = $derived(page.params.folderId ?? null);

  // Walk the folder chain from the active folder up to the project root
  // so the breadcrumb matches the URL exactly even after navigation.
  const breadcrumbItems = $derived.by<BreadcrumbItem[]>(() => {
    const items: BreadcrumbItem[] = [
      { id: 'workspace', label: 'Projects', href: '/projects' },
    ];
    if (project) {
      items.push({
        id: project.id,
        label: project.display_name || project.slug,
        href: `/projects/${project.id}`,
      });
    }
    if (folderId) {
      const byId = new Map(folders.map((f) => [f.id, f] as const));
      const chain: OntologyProjectFolder[] = [];
      let cursor: string | null = folderId;
      const guard = new Set<string>();
      while (cursor && !guard.has(cursor)) {
        guard.add(cursor);
        const node = byId.get(cursor);
        if (!node) break;
        chain.unshift(node);
        cursor = node.parent_folder_id;
      }
      for (const folder of chain) {
        items.push({
          id: folder.id,
          label: folder.name,
          href: `/projects/${project?.id ?? projectId}/${folder.id}`,
        });
      }
    }
    return items;
  });

  setProjectWorkspaceContext(() => ({
    project,
    folders,
    resources,
    loading,
    error,
    reload: load,
    dragSource,
    beginDrag: (source) => {
      dragSource = source;
    },
    endDrag: () => {
      dragSource = null;
    },
    tryDrop,
    openCrossProjectMove,
  }));

  // ---------------------------------------------------------------------
  // Drag bus implementation
  // ---------------------------------------------------------------------

  // Walk the parent chain so we can reject dropping a folder into itself
  // or any of its own descendants.
  function isDescendantFolder(candidateId: string, ancestorFolderId: string): boolean {
    if (candidateId === ancestorFolderId) return true;
    const byId = new Map(folders.map((f) => [f.id, f] as const));
    let cursor: string | null = candidateId;
    const guard = new Set<string>();
    while (cursor && !guard.has(cursor)) {
      guard.add(cursor);
      const node = byId.get(cursor);
      if (!node) return false;
      if (node.id === ancestorFolderId) return true;
      cursor = node.parent_folder_id;
    }
    return false;
  }

  function dropAccepts(targetFolderId: string | null): boolean {
    if (!dragSource || dragSource.targets.length === 0) return false;
    if (!project) return false;
    for (const target of dragSource.targets) {
      // No-op: dropping a folder onto its own current location.
      const currentParent = target.parentFolderId ?? null;
      if (target.kind === 'ontology_folder') {
        if (target.id === targetFolderId) return false;
        if (currentParent === targetFolderId) return false;
        if (
          targetFolderId !== null &&
          isDescendantFolder(targetFolderId, target.id)
        ) {
          return false;
        }
      } else if (currentParent === targetFolderId) {
        // Resource binding already lives at this destination.
        return false;
      }
    }
    return true;
  }

  async function tryDrop(targetFolderId: string | null): Promise<boolean> {
    if (!dragSource || !project) return false;
    const source = dragSource;
    if (!dropAccepts(targetFolderId)) {
      dragSource = null;
      return false;
    }
    const targets = source.targets;
    dragSource = null;

    try {
      if (targets.length === 1) {
        const t = targets[0];
        await moveResource(t.kind, t.id, {
          target_project_id: project.id,
          target_folder_id: targetFolderId,
        });
        notifications.success(
          `Moved ${t.label ?? t.kind} to ${targetFolderId ? 'folder' : 'project root'}.`,
        );
      } else {
        const actions: BatchAction[] = targets.map((t) => ({
          op: 'move',
          resource_kind: t.kind,
          resource_id: t.id,
          target_folder_id: targetFolderId ?? undefined,
        }));
        const result = await batchApply(actions);
        const ok = result.results.filter((r) => r.ok).length;
        const failed = result.results.length - ok;
        if (failed === 0) {
          notifications.success(`Moved ${ok} item(s).`);
        } else {
          notifications.warning(
            `Moved ${ok}/${result.results.length} item(s); ${failed} failed.`,
          );
        }
      }
      await load();
      return true;
    } catch (cause) {
      notifications.error(
        cause instanceof Error ? cause.message : 'Move failed.',
      );
      return false;
    }
  }

  function openCrossProjectMove() {
    if (!dragSource || dragSource.targets.length === 0) return;
    const folderTargets = dragSource.targets.filter(
      (t) => t.kind === 'ontology_folder',
    );
    if (folderTargets.length > 0) {
      notifications.warning(
        'Folders cannot be moved between projects yet — only individual resources.',
      );
      dragSource = null;
      return;
    }
    crossMoveTargets = dragSource.targets;
    dragSource = null;
    void ensureAllProjects();
  }

  async function ensureAllProjects() {
    if (allProjects.length > 0) return;
    try {
      const res = await listProjects({ per_page: 200 });
      allProjects = res.data;
    } catch (cause) {
      notifications.error(
        cause instanceof Error ? cause.message : 'Unable to load projects.',
      );
      allProjects = [];
    }
  }

  function handleCrossDragOver(event: DragEvent) {
    if (!dragSource) return;
    event.preventDefault();
    crossDragOver = true;
  }

  function handleCrossDragLeave() {
    crossDragOver = false;
  }

  function handleCrossDrop(event: DragEvent) {
    event.preventDefault();
    crossDragOver = false;
    openCrossProjectMove();
  }

  async function load() {
    if (!projectId) return;
    loading = true;
    error = '';
    try {
      const [projectResponse, folderList, resourceList] = await Promise.all([
        getProject(projectId),
        listProjectFolders(projectId),
        listProjectResources(projectId),
      ]);
      project = projectResponse;
      folders = folderList;
      resources = resourceList;

      // Best-effort recents tracking — failures are silent on purpose.
      void recordAccess({
        resource_kind: 'ontology_project',
        resource_id: projectResponse.id,
      }).catch(() => {});
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Unable to load project';
      project = null;
      folders = [];
      resources = [];
      notifications.error(error);
    } finally {
      loading = false;
    }
  }

  function handleSelectFolder(id: string | null) {
    if (!project) return;
    if (id) {
      void goto(`/projects/${project.id}/${id}`);
    } else {
      void goto(`/projects/${project.id}`);
    }
  }

  onMount(() => {
    void load();
  });

  // Reload when the user navigates between projects via the breadcrumb.
  $effect(() => {
    if (projectId && projectId !== project?.id && !loading) {
      void load();
    }
  });
</script>

<div class="of-panel overflow-hidden">
  <header class="border-b border-[var(--border-default)] bg-white px-4 py-3">
    <ProjectBreadcrumb items={breadcrumbItems} />
    {#if project}
      <div class="mt-3 flex items-start justify-between gap-3">
        <div>
          <h1 class="m-0 text-xl font-semibold text-[var(--text-strong)]">
            {project.display_name || project.slug}
          </h1>
          {#if project.description}
            <p class="m-0 mt-1 max-w-2xl text-sm text-[var(--text-muted)]">
              {project.description}
            </p>
          {/if}
        </div>
        <button
          type="button"
          class="of-btn"
          onclick={() => goto('/projects')}
          aria-label="Back to projects"
        >
          <Glyph name="chevron-right" size={12} />
          <span>All projects</span>
        </button>
      </div>
    {/if}
  </header>

  <div class="grid min-h-[480px] grid-cols-[260px,1fr] bg-[#f5f7fa]">
    <nav
      class="border-r border-[var(--border-default)] bg-white"
      aria-label="Folder navigation"
    >
      {#if loading}
        <div class="p-4 text-sm text-[var(--text-muted)]">Loading folders…</div>
      {:else if error}
        <div class="p-4 text-sm text-[#b42318]">{error}</div>
      {:else if !project}
        <div class="p-4 text-sm text-[var(--text-muted)]">Project not available.</div>
      {:else}
        <FolderTree
          {folders}
          selectedId={folderId}
          rootLabel={project.display_name || project.slug}
          onSelect={handleSelectFolder}
          onDrop={(id) => {
            void tryDrop(id);
          }}
          canDrop={(id) => dropAccepts(id)}
        />
        {#if dragSource && dragSource.targets.every((t) => t.kind !== 'ontology_folder')}
          <div
            class="m-3 rounded-md border border-dashed px-3 py-2 text-xs"
            class:border-[var(--accent-strong)]={crossDragOver}
            class:bg-[var(--accent-soft)]={crossDragOver}
            class:text-[var(--accent-strong)]={crossDragOver}
            class:border-[var(--border-default)]={!crossDragOver}
            class:text-[var(--text-muted)]={!crossDragOver}
            ondragover={handleCrossDragOver}
            ondragleave={handleCrossDragLeave}
            ondrop={handleCrossDrop}
            role="button"
            tabindex="-1"
            aria-label="Move to another project"
          >
            <Glyph name="link" size={14} />
            <span class="ml-1 align-middle">Drop here to move to another project…</span>
          </div>
        {/if}
      {/if}
    </nav>

    <main class="min-w-0 p-4">
      {@render children()}
    </main>
  </div>
</div>

<MoveDialog
  open={crossMoveTargets !== null}
  resourceKind={null}
  resourceId={null}
  projects={allProjects}
  initialProjectId={project?.id ?? null}
  targets={crossMoveTargets?.map((t) => ({
    kind: t.kind,
    id: t.id,
    label: t.label ?? t.id,
  })) ?? []}
  onClose={() => {
    crossMoveTargets = null;
  }}
  onMoved={() => {
    crossMoveTargets = null;
    void load();
  }}
/>
