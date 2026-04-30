<script lang="ts">
  import { goto } from '$app/navigation';
  import Glyph from '$components/ui/Glyph.svelte';
  import ResourceDetailsPanel, {
    type ResourceSummary,
  } from '$components/workspace/ResourceDetailsPanel.svelte';
  import { listFavorites, type UserFavorite } from '$lib/api/workspace';
  import { onMount } from 'svelte';
  import { getProjectWorkspaceContext } from './context';

  const ctx = getProjectWorkspaceContext();
  const view = $derived(ctx());

  // Top-level entries only — folders without a parent and resources
  // bound to the project root.
  const rootFolders = $derived(view.folders.filter((f) => f.parent_folder_id === null));
  const resources = $derived(view.resources);

  let detailsOpen = $state(false);
  let selectedResource = $state<ResourceSummary | null>(null);

  let favoriteKeys = $state(new Set<string>());

  async function loadFavorites() {
    try {
      const all: UserFavorite[] = await listFavorites({ limit: 500 });
      favoriteKeys = new Set(all.map((f) => `${f.resource_kind}:${f.resource_id}`));
    } catch {
      // Favorites are optional adornment; ignore failures.
    }
  }

  onMount(() => {
    void loadFavorites();
  });

  function isFavorite(kind: string, id: string) {
    return favoriteKeys.has(`${kind}:${id}`);
  }

  function openFolder(folderId: string) {
    if (!view.project) return;
    void goto(`/projects/${view.project.id}/${folderId}`);
  }

  function openResource(binding: typeof resources[number]) {
    selectedResource = {
      id: binding.resource_id,
      name: `${binding.resource_kind} · ${binding.resource_id.slice(0, 8)}…`,
      kind: binding.resource_kind as ResourceSummary['kind'],
      description: null,
      owner_id: binding.bound_by,
      location: view.project ? `${view.project.display_name || view.project.slug} / root` : null,
      created_at: binding.created_at,
      updated_at: binding.created_at,
      tags: [binding.resource_kind],
    };
    detailsOpen = true;
  }

  function openFolderDetails(folder: typeof rootFolders[number]) {
    selectedResource = {
      id: folder.id,
      name: folder.name,
      kind: 'ontology_folder',
      description: folder.description,
      owner_id: folder.created_by,
      location: view.project ? `${view.project.display_name || view.project.slug}` : null,
      created_at: folder.created_at,
      updated_at: folder.updated_at,
      tags: ['folder'],
    };
    detailsOpen = true;
  }

  function handleFavoriteToggle(next: boolean) {
    if (!selectedResource) return;
    const key = `${selectedResource.kind}:${selectedResource.id}`;
    const updated = new Set(favoriteKeys);
    if (next) updated.add(key);
    else updated.delete(key);
    favoriteKeys = updated;
  }
</script>

{#if view.loading}
  <div class="text-sm text-[var(--text-muted)]">Loading project contents…</div>
{:else if view.error}
  <div class="text-sm text-[#b42318]">{view.error}</div>
{:else}
  <section class="space-y-6">
    <div>
      <h2 class="m-0 text-sm font-semibold uppercase tracking-[0.16em] text-[var(--text-muted)]">
        Folders ({rootFolders.length})
      </h2>
      {#if rootFolders.length === 0}
        <div class="mt-3 rounded-md border border-dashed border-[var(--border-default)] bg-white p-6 text-center text-sm text-[var(--text-muted)]">
          No folders yet at the project root. Create folders from the project actions menu.
        </div>
      {:else}
        <div class="mt-3 grid gap-3 md:grid-cols-2 lg:grid-cols-3">
          {#each rootFolders as folder (folder.id)}
            <article class="of-panel flex flex-col gap-2 p-3">
              <button
                type="button"
                class="flex items-start gap-3 text-left"
                onclick={() => openFolder(folder.id)}
              >
                <span class="mt-0.5 rounded-md bg-[#eef4fd] p-2 text-[#2458b8]">
                  <Glyph name="folder" size={14} />
                </span>
                <span class="min-w-0 flex-1">
                  <span class="block truncate font-semibold text-[var(--text-strong)]">{folder.name}</span>
                  {#if folder.description}
                    <span class="mt-1 block text-xs text-[var(--text-muted)]">{folder.description}</span>
                  {/if}
                </span>
              </button>
              <div class="flex items-center justify-between text-xs text-[var(--text-soft)]">
                <span>{view.folders.filter((f) => f.parent_folder_id === folder.id).length} subfolder(s)</span>
                <button
                  type="button"
                  class="text-[var(--text-link)] hover:underline"
                  onclick={() => openFolderDetails(folder)}
                >
                  Details
                </button>
              </div>
            </article>
          {/each}
        </div>
      {/if}
    </div>

    <div>
      <h2 class="m-0 text-sm font-semibold uppercase tracking-[0.16em] text-[var(--text-muted)]">
        Resources ({resources.length})
      </h2>
      {#if resources.length === 0}
        <div class="mt-3 rounded-md border border-dashed border-[var(--border-default)] bg-white p-6 text-center text-sm text-[var(--text-muted)]">
          No resources are bound to this project yet.
        </div>
      {:else}
        <ul class="m-0 mt-3 list-none space-y-2 p-0">
          {#each resources as binding (`${binding.resource_kind}-${binding.resource_id}`)}
            <li>
              <button
                type="button"
                class="of-panel flex w-full items-center justify-between gap-3 px-3 py-2 text-left transition hover:bg-[#fbfdff]"
                onclick={() => openResource(binding)}
              >
                <span class="flex min-w-0 items-center gap-3">
                  <span class="rounded-md border border-[var(--border-default)] bg-white p-2 text-[var(--text-muted)]">
                    <Glyph name="artifact" size={14} />
                  </span>
                  <span class="min-w-0">
                    <span class="block truncate font-medium text-[var(--text-strong)]">
                      {binding.resource_kind} · {binding.resource_id.slice(0, 8)}…
                    </span>
                    <span class="block truncate text-xs text-[var(--text-muted)]">
                      Bound by {binding.bound_by.slice(0, 8)}…
                    </span>
                  </span>
                </span>
                <span class="of-chip">{binding.resource_kind}</span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  </section>

  <ResourceDetailsPanel
    open={detailsOpen}
    resource={selectedResource}
    isFavorite={selectedResource ? isFavorite(selectedResource.kind, selectedResource.id) : false}
    onClose={() => {
      detailsOpen = false;
    }}
    onFavoriteToggle={handleFavoriteToggle}
  />
{/if}
