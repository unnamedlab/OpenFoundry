<script lang="ts">
  export type BulkAction = {
    id: string;
    label: string;
    danger?: boolean;
    disabled?: boolean;
  };

  let {
    count,
    actions,
    onAction,
    onClear,
    busy = false,
  }: {
    count: number;
    actions: BulkAction[];
    onAction: (id: string) => void;
    onClear: () => void;
    busy?: boolean;
  } = $props();
</script>

{#if count > 0}
  <div
    class="sticky top-2 z-20 flex flex-wrap items-center justify-between gap-3 rounded-md border border-[var(--border-default)] bg-[#eef4fd] px-4 py-2 shadow-sm"
    role="toolbar"
    aria-label="Bulk actions"
  >
    <div class="flex items-center gap-3 text-sm font-medium text-[var(--text-strong)]">
      <span>{count} selected</span>
      <button
        type="button"
        class="text-xs font-medium text-[var(--text-link)] hover:underline"
        onclick={onClear}
        disabled={busy}
      >
        Clear
      </button>
    </div>
    <div class="flex flex-wrap items-center gap-2">
      {#each actions as action (action.id)}
        <button
          type="button"
          class={`of-btn ${action.danger ? 'text-[#b42318]' : ''}`}
          disabled={busy || action.disabled}
          onclick={() => onAction(action.id)}
        >
          {action.label}
        </button>
      {/each}
    </div>
  </div>
{/if}
