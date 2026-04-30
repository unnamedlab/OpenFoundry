<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { page } from '$app/state';
  import Glyph from '$components/ui/Glyph.svelte';
  import ProjectBreadcrumb, { type BreadcrumbItem } from '$components/workspace/ProjectBreadcrumb.svelte';
  import FolderTree from '$components/workspace/FolderTree.svelte';
  import {
    getProject,
    listProjectFolders,
    listProjectResources,
    type OntologyProject,
    type OntologyProjectFolder,
    type OntologyProjectResourceBinding,
  } from '$lib/api/ontology';
  import { recordAccess } from '$lib/api/workspace';
  import { notifications } from '$stores/notifications';
  import { setProjectWorkspaceContext } from './context';

  let { children }: { children: any } = $props();

  let project = $state<OntologyProject | null>(null);
  let folders = $state<OntologyProjectFolder[]>([]);
  let resources = $state<OntologyProjectResourceBinding[]>([]);
  let loading = $state(true);
  let error = $state('');

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
  }));

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
        />
      {/if}
    </nav>

    <main class="min-w-0 p-4">
      {@render children()}
    </main>
  </div>
</div>
