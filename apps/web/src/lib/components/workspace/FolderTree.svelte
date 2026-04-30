<script lang="ts">
  import Glyph from '$components/ui/Glyph.svelte';

  export interface FolderNode {
    id: string;
    name: string;
    parent_folder_id: string | null;
  }

  let {
    folders,
    selectedId = null,
    rootLabel = 'Project root',
    onSelect,
  }: {
    folders: FolderNode[];
    selectedId?: string | null;
    rootLabel?: string;
    onSelect?: (folderId: string | null) => void;
  } = $props();

  type TreeNode = FolderNode & { children: TreeNode[] };

  // Build adjacency-style tree from a flat list. Folders missing parents
  // are surfaced as roots so a partial list still renders.
  const tree = $derived.by(() => {
    const byId = new Map<string, TreeNode>(
      folders.map((f) => [f.id, { ...f, children: [] }] as const),
    );
    const roots: TreeNode[] = [];
    for (const node of byId.values()) {
      const parent = node.parent_folder_id ? byId.get(node.parent_folder_id) : undefined;
      if (parent) {
        parent.children.push(node);
      } else {
        roots.push(node);
      }
    }
    const sortRec = (list: TreeNode[]) => {
      list.sort((a, b) => a.name.localeCompare(b.name));
      for (const node of list) sortRec(node.children);
    };
    sortRec(roots);
    return roots;
  });

  // Track expanded state locally — by default expand everything that
  // contains the current selection so the user always sees their context.
  let expanded = $state(new Set<string>());

  $effect(() => {
    if (!selectedId) return;
    const byId = new Map(folders.map((f) => [f.id, f] as const));
    let cursor: string | null = selectedId;
    const next = new Set(expanded);
    while (cursor) {
      const node: FolderNode | undefined = byId.get(cursor);
      if (!node) break;
      next.add(node.id);
      cursor = node.parent_folder_id;
    }
    expanded = next;
  });

  function toggle(id: string) {
    const next = new Set(expanded);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    expanded = next;
  }
</script>

<aside class="flex flex-col gap-1 p-2 text-sm">
  <button
    type="button"
    class={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left transition ${
      selectedId === null
        ? 'bg-[#eef4fd] text-[#2458b8] font-semibold'
        : 'text-[var(--text-default)] hover:bg-[var(--bg-hover)]'
    }`}
    onclick={() => onSelect?.(null)}
  >
    <Glyph name="home" size={14} />
    <span class="truncate">{rootLabel}</span>
  </button>

  <ul class="m-0 list-none space-y-0.5 p-0">
    {#each tree as root (root.id)}
      {@render branch(root, 0)}
    {/each}
  </ul>
</aside>

{#snippet branch(node: TreeNode, depth: number)}
  {@const hasChildren = node.children.length > 0}
  {@const isOpen = expanded.has(node.id)}
  {@const isSelected = selectedId === node.id}
  <li class="m-0">
    <div
      class={`flex items-center gap-1 rounded-md py-1 pr-2 transition ${
        isSelected
          ? 'bg-[#eef4fd] text-[#2458b8] font-semibold'
          : 'text-[var(--text-default)] hover:bg-[var(--bg-hover)]'
      }`}
      style={`padding-left: ${8 + depth * 14}px`}
    >
      {#if hasChildren}
        <button
          type="button"
          class="flex h-5 w-5 items-center justify-center rounded text-[var(--text-muted)] hover:text-[var(--text-strong)]"
          aria-label={isOpen ? 'Collapse folder' : 'Expand folder'}
          onclick={() => toggle(node.id)}
        >
          {#if isOpen}
            <Glyph name="chevron-down" size={12} />
          {:else}
            <Glyph name="chevron-right" size={12} />
          {/if}
        </button>
      {:else}
        <span class="inline-block h-5 w-5"></span>
      {/if}

      <button
        type="button"
        class="flex min-w-0 flex-1 items-center gap-2 truncate text-left"
        onclick={() => onSelect?.(node.id)}
      >
        <Glyph name="folder" size={13} />
        <span class="truncate">{node.name}</span>
      </button>
    </div>

    {#if hasChildren && isOpen}
      <ul class="m-0 list-none space-y-0.5 p-0">
        {#each node.children as child (child.id)}
          {@render branch(child, depth + 1)}
        {/each}
      </ul>
    {/if}
  </li>
{/snippet}
