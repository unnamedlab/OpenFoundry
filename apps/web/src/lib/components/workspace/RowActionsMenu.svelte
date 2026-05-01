<script lang="ts">
  import Glyph from '$components/ui/Glyph.svelte';

  export type RowAction = {
    id: string;
    label: string;
    icon?: 'pencil' | 'duplicate' | 'move' | 'delete' | 'share';
    danger?: boolean;
    disabled?: boolean;
  };

  let {
    actions,
    onSelect,
    label = 'Row actions',
  }: {
    actions: RowAction[];
    onSelect: (id: string) => void;
    label?: string;
  } = $props();

  let open = $state(false);
  let container = $state<HTMLDivElement | null>(null);

  function toggle(event: MouseEvent) {
    event.stopPropagation();
    open = !open;
  }

  function handleSelect(event: MouseEvent, action: RowAction) {
    event.stopPropagation();
    if (action.disabled) return;
    open = false;
    onSelect(action.id);
  }

  $effect(() => {
    if (!open) return;
    const handler = (event: MouseEvent) => {
      if (container && !container.contains(event.target as Node)) {
        open = false;
      }
    };
    const keyHandler = (event: KeyboardEvent) => {
      if (event.key === 'Escape') open = false;
    };
    document.addEventListener('mousedown', handler);
    document.addEventListener('keydown', keyHandler);
    return () => {
      document.removeEventListener('mousedown', handler);
      document.removeEventListener('keydown', keyHandler);
    };
  });
</script>

<div bind:this={container} class="relative inline-flex">
  <button
    type="button"
    class="inline-flex h-7 w-7 items-center justify-center rounded-md text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-strong)]"
    aria-label={label}
    aria-haspopup="menu"
    aria-expanded={open}
    onclick={toggle}
  >
    <span class="text-base leading-none">⋯</span>
  </button>

  {#if open}
    <div
      role="menu"
      class="absolute right-0 top-full z-30 mt-1 min-w-[180px] rounded-md border border-[var(--border-default)] bg-white py-1 shadow-lg"
    >
      {#each actions as action (action.id)}
        <button
          type="button"
          role="menuitem"
          class={`flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm ${
            action.disabled
              ? 'cursor-not-allowed text-[var(--text-soft)]'
              : action.danger
                ? 'text-[#b42318] hover:bg-[#fff5f5]'
                : 'text-[var(--text-default)] hover:bg-[var(--bg-hover)]'
          }`}
          disabled={action.disabled}
          onclick={(event) => handleSelect(event, action)}
        >
          {#if action.icon === 'share'}
            <Glyph name="users" size={13} />
          {:else if action.icon === 'duplicate'}
            <Glyph name="object" size={13} />
          {:else if action.icon === 'move'}
            <Glyph name="folder" size={13} />
          {:else if action.icon === 'delete'}
            <span aria-hidden="true">🗑</span>
          {:else if action.icon === 'pencil'}
            <span aria-hidden="true">✎</span>
          {/if}
          <span>{action.label}</span>
        </button>
      {/each}
    </div>
  {/if}
</div>
