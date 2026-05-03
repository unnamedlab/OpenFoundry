<script lang="ts">
  import Glyph from './Glyph.svelte';

  type Json = unknown;

  let {
    data,
    label = '',
    collapsedByDefault = false
  }: {
    data: Json;
    label?: string;
    collapsedByDefault?: boolean;
  } = $props();

  let expanded = $state(!collapsedByDefault);

  function isObject(value: Json): value is Record<string, Json> {
    return typeof value === 'object' && value !== null && !Array.isArray(value);
  }

  function entries(value: Json): Array<[string, Json]> {
    if (isObject(value)) return Object.entries(value);
    if (Array.isArray(value)) return value.map((item, i) => [String(i), item]);
    return [];
  }

  function isComplex(value: Json) {
    return Array.isArray(value) || isObject(value);
  }

  function formatLeaf(value: Json) {
    if (value === null) return 'null';
    if (value === undefined) return 'undefined';
    if (typeof value === 'string') return JSON.stringify(value);
    return String(value);
  }

  function leafTone(value: Json) {
    if (value === null || value === undefined)
      return 'text-slate-400 dark:text-slate-500';
    if (typeof value === 'string') return 'text-emerald-700 dark:text-emerald-300';
    if (typeof value === 'number') return 'text-blue-700 dark:text-blue-300';
    if (typeof value === 'boolean') return 'text-amber-700 dark:text-amber-300';
    return 'text-slate-700 dark:text-slate-200';
  }
</script>

{#if isComplex(data)}
  <div class="font-mono text-[12px] leading-snug" data-testid="tree">
    {#if label}
      <button
        type="button"
        class="inline-flex items-center gap-1 text-slate-700 hover:text-slate-900 dark:text-slate-200"
        onclick={() => (expanded = !expanded)}
      >
        <Glyph name={expanded ? 'chevron-down' : 'chevron-right'} size={12} />
        <span class="font-semibold">{label}</span>
        <span class="text-[11px] text-slate-400">
          {Array.isArray(data) ? `[${data.length}]` : `{${Object.keys(data as object).length}}`}
        </span>
      </button>
    {/if}
    {#if expanded || !label}
      <ul class="ml-3 mt-1 space-y-0.5 border-l border-slate-200 pl-3 dark:border-gray-700">
        {#each entries(data) as [key, value] (key)}
          <li>
            {#if isComplex(value)}
              <svelte:self data={value} label={key} collapsedByDefault={true} />
            {:else}
              <div class="flex gap-2">
                <span class="text-slate-500 dark:text-slate-400">{key}:</span>
                <span class={leafTone(value)}>{formatLeaf(value)}</span>
              </div>
            {/if}
          </li>
        {/each}
        {#if entries(data).length === 0}
          <li class="text-[11px] italic text-slate-400">empty</li>
        {/if}
      </ul>
    {/if}
  </div>
{:else}
  <div class="font-mono text-[12px]">
    {#if label}<span class="text-slate-500">{label}: </span>{/if}
    <span class={leafTone(data)}>{formatLeaf(data)}</span>
  </div>
{/if}
