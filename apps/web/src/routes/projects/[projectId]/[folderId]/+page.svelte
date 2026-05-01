<script lang="ts">
  import { goto } from '$app/navigation';
  import { page } from '$app/state';
  import Glyph from '$components/ui/Glyph.svelte';
  import ResourceDetailsPanel, {
    type ResourceSummary,
  } from '$components/workspace/ResourceDetailsPanel.svelte';
  import { listFavorites, recordAccess, type UserFavorite } from '$lib/api/workspace';
  import { onMount } from 'svelte';
  import { getProjectWorkspaceContext } from '../context';

  const ctx = getProjectWorkspaceContext();
  const view = $derived(ctx());

  const folderId = $derived(page.params.folderId ?? '');
  const folder = $derived(view.folders.find((f) => f.id === folderId) ?? null);
  const childFolders = $derived(view.folders.filter((f) => f.parent_folder_id === folderId));

  let detailsOpen = $state(false);
  let selectedResource = $state<ResourceSummary | null>(null);
  let favoriteKeys = $state(new Set<string>());

  async function loadFavorites() {
    try {
      const all: UserFavorite[] = await listFavorites({ limit: 500 });
      favoriteKeys = new Set(all.map((f) => `${f.resource_kind}:${f.resource_id}`));
    } catch {
      // optional
    }
  }

  onMount(() => {
    void loadFavorites();
  });

  // Whenever the active folder changes, ping the recents log so the
  // sidebar's "recently viewed" reflects what the user actually opens.
  $effect(() => {
    if (!folderId) return;
    void recordAccess({ resource_kind: 'ontology_folder', resource_id: folderId }).catch(
      () => {},
    );
  });

  function isFavorite(kind: string, id: string) {
    return favoriteKeys.has(`${kind}:${id}`);
  }

  function openSubfolder(id: string) {
    if (!view.project) return;
    void goto(`/projects/${view.project.id}/${id}`);
  }

  function openFolderDetails(target: NonNullable<typeof folder>) {
    selectedResource = {
      id: target.id,
      name: target.name,
      kind: 'ontology_folder',
      description: target.description,
      owner_id: target.created_by,
      location: view.project ? `${view.project.display_name || view.project.slug}` : null,
      created_at: target.created_at,
      updated_at: target.updated_at,
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

  // Drag bus (Phase 6) — publish subfolder cards as drag sources so the
  // FolderTree on the layout can serve as a drop target.
  function handleFolderDragStart(
    event: DragEvent,
    target: typeof childFolders[number],
  ) {
    view.beginDrag({
      targets: [
        {
          kind: 'ontology_folder',
          id: target.id,
          parentFolderId: target.parent_folder_id,
          label: target.name,
        },
      ],
    });
    if (event.dataTransfer) {
      event.dataTransfer.effectAllowed = 'move';
      event.dataTransfer.setData('text/plain', `ontology_folder:${target.id}`);
    }
  }

  function handleDragEnd() {
    view.endDrag();
  }
</script>

{#if view.loading}
  <div class="text-sm text-[var(--text-muted)]">Loading folder…</div>
{:else if view.error}
  <div class="text-sm text-[#b42318]">{view.error}</div>
{:else if !folder}
  <div class="rounded-md border border-dashed border-[var(--border-default)] bg-white p-6 text-center text-sm text-[var(--text-muted)]">
    This folder doesn't exist or has been moved.
    <div class="mt-3">
      <button
        type="button"
        class="of-btn"
        onclick={() => view.project && goto(`/projects/${view.project.id}`)}
      >
        Back to project root
      </button>
    </div>
  </div>
{:else}
  <section class="space-y-6">
    <div class="flex items-start justify-between gap-3">
      <div>
        <h2 class="m-0 text-lg font-semibold text-[var(--text-strong)]">{folder.name}</h2>
        {#if folder.description}
          <p class="m-0 mt-1 text-sm text-[var(--text-muted)]">{folder.description}</p>
        {/if}
      </div>
      <button type="button" class="of-btn" onclick={() => openFolderDetails(folder)}>
        <Glyph name="object" size={14} />
        <span>Details</span>
      </button>
    </div>

    <div>
      <h3 class="m-0 text-sm font-semibold uppercase tracking-[0.16em] text-[var(--text-muted)]">
        Subfolders ({childFolders.length})
      </h3>
      {#if childFolders.length === 0}
        <div class="mt-3 rounded-md border border-dashed border-[var(--border-default)] bg-white p-6 text-center text-sm text-[var(--text-muted)]">
          This folder has no subfolders. Resources bound to a specific folder will appear here in a later release.
        </div>
      {:else}
        <div class="mt-3 grid gap-3 md:grid-cols-2 lg:grid-cols-3">
          {#each childFolders as child (child.id)}
            <article
              class="of-panel flex flex-col gap-2 p-3"
              draggable={true}
              ondragstart={(event) => handleFolderDragStart(event, child)}
              ondragend={handleDragEnd}
            >
              <button
                type="button"
                class="flex items-start gap-3 text-left"
                onclick={() => openSubfolder(child.id)}
              >
                <span class="mt-0.5 rounded-md bg-[#eef4fd] p-2 text-[#2458b8]">
                  <Glyph name="folder" size={14} />
                </span>
                <span class="min-w-0 flex-1">
                  <span class="block truncate font-semibold text-[var(--text-strong)]">{child.name}</span>
                  {#if child.description}
                    <span class="mt-1 block text-xs text-[var(--text-muted)]">{child.description}</span>
                  {/if}
                </span>
              </button>
            </article>
          {/each}
        </div>
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
