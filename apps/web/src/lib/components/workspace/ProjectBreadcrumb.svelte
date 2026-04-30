<script lang="ts">
  import Glyph from '$components/ui/Glyph.svelte';

  export interface BreadcrumbItem {
    id: string;
    label: string;
    href?: string;
  }

  let { items, onNavigate }: {
    items: BreadcrumbItem[];
    onNavigate?: (item: BreadcrumbItem, index: number) => void;
  } = $props();
</script>

<nav aria-label="Breadcrumb" class="flex flex-wrap items-center gap-1 text-sm">
  {#each items as item, index (item.id)}
    {#if index > 0}
      <span class="text-[var(--text-soft)]" aria-hidden="true">
        <Glyph name="chevron-right" size={12} />
      </span>
    {/if}

    {#if index === items.length - 1}
      <span class="font-semibold text-[var(--text-strong)]">{item.label}</span>
    {:else if item.href}
      <a
        href={item.href}
        class="text-[var(--text-link)] hover:underline"
        onclick={(event) => {
          if (onNavigate) {
            event.preventDefault();
            onNavigate(item, index);
          }
        }}
      >
        {item.label}
      </a>
    {:else}
      <button
        type="button"
        class="text-[var(--text-link)] hover:underline"
        onclick={() => onNavigate?.(item, index)}
      >
        {item.label}
      </button>
    {/if}
  {/each}
</nav>
