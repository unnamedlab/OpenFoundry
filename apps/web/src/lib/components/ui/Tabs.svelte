<script lang="ts">
  type TabItem = {
    key: string;
    label: string;
    badge?: number | string | null;
    disabled?: boolean;
  };

  let {
    items,
    activeKey = $bindable(),
    ariaLabel = 'Tabs'
  }: {
    items: TabItem[];
    activeKey: string;
    ariaLabel?: string;
  } = $props();
</script>

<nav
  class="flex gap-1 overflow-x-auto border-b border-slate-200 dark:border-gray-800"
  aria-label={ariaLabel}
>
  {#each items as tab (tab.key)}
    <button
      type="button"
      role="tab"
      aria-selected={activeKey === tab.key}
      disabled={tab.disabled}
      data-testid={`tab-${tab.key}`}
      class={`inline-flex items-center gap-2 whitespace-nowrap border-b-2 px-3 py-2 text-sm font-medium transition-colors ${
        activeKey === tab.key
          ? 'border-blue-600 text-blue-700 dark:text-blue-300'
          : 'border-transparent text-slate-600 hover:border-slate-300 hover:text-slate-900 dark:text-gray-400 dark:hover:text-gray-100'
      } ${tab.disabled ? 'opacity-50 cursor-not-allowed' : ''}`}
      onclick={() => {
        if (!tab.disabled) activeKey = tab.key;
      }}
    >
      <span>{tab.label}</span>
      {#if tab.badge !== undefined && tab.badge !== null && tab.badge !== ''}
        <span
          class="rounded-full bg-slate-100 px-1.5 text-[11px] font-medium text-slate-600 dark:bg-gray-700 dark:text-slate-300"
        >
          {tab.badge}
        </span>
      {/if}
    </button>
  {/each}
</nav>
