<!--
  T5.1 — SchemaPanel

  Renders a Foundry-style schema browser. The wire format here is a
  superset of the simple `{ name, field_type, nullable }` rows
  exposed today by `quality.profile.columns`, plus the recursive
  Foundry types: STRUCT (subSchemas), ARRAY (arraySubType), MAP
  (mapKeyType / mapValueType), DECIMAL (precision/scale), and
  primitives BOOLEAN/BYTE/SHORT/INTEGER/LONG/FLOAT/DOUBLE/STRING/
  BINARY/DATE/TIMESTAMP. A bottom block surfaces format-level
  `options` (e.g. CSV `has_header`, `delimiter`).

  Recursive expansion is local to each row so users can drill into
  one nested column without collapsing siblings.
-->
<script lang="ts" module>
  export type SchemaField = {
    name: string;
    type: string;
    nullable?: boolean;
    description?: string;
    /** STRUCT */
    subSchemas?: SchemaField[];
    /** ARRAY */
    arraySubType?: SchemaField;
    /** MAP */
    mapKeyType?: SchemaField;
    mapValueType?: SchemaField;
    /** DECIMAL */
    precision?: number;
    scale?: number;
  };

  // Stable, deterministic key for a row given its position in the
  // recursion. Using the depth + path of indices avoids the "two
  // STRUCT siblings with the same field name share expansion state"
  // bug while remaining stable across rerenders.
  export function rowKey(path: string): string {
    return path;
  }
</script>

<script lang="ts">
  type Props = {
    fields: SchemaField[];
    /** Format-level options (e.g. CSV `has_header`, `delimiter`). */
    options?: Record<string, string | number | boolean>;
    /** Format string ("PARQUET", "CSV", …) — only used to label the options block. */
    format?: string;
  };

  const { fields, options = {}, format = '' }: Props = $props();

  let expandedKeys = $state<Set<string>>(new Set());

  function toggle(key: string) {
    const next = new Set(expandedKeys);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    expandedKeys = next;
  }

  function isComplex(t: string): boolean {
    const u = t.toUpperCase();
    return u.startsWith('STRUCT') || u.startsWith('ARRAY') || u.startsWith('MAP');
  }

  function renderType(field: SchemaField): string {
    const t = field.type.toUpperCase();
    if (t === 'DECIMAL' && field.precision !== undefined) {
      return `DECIMAL(${field.precision},${field.scale ?? 0})`;
    }
    return t;
  }

  function typeBadge(t: string): string {
    const u = t.toUpperCase();
    if (u === 'STRING' || u === 'BINARY')
      return 'bg-purple-100 text-purple-700 dark:bg-purple-900/40 dark:text-purple-300';
    if (u === 'DATE' || u === 'TIMESTAMP')
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300';
    if (u === 'BOOLEAN') return 'bg-pink-100 text-pink-700 dark:bg-pink-900/40 dark:text-pink-300';
    if (u.startsWith('STRUCT') || u.startsWith('ARRAY') || u.startsWith('MAP'))
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300';
    return 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300';
  }
</script>

<!--
  Recursive snippet:
    {@render row(field, depth)} renders one row. We don't reach for a
    separate component because Svelte 5 snippets handle the recursion
    cleanly and we get to keep state-less rendering.
-->
{#snippet row(field: SchemaField, depth: number, path: string)}
  {@const expandable = isComplex(field.type)}
  {@const expanded = expandedKeys.has(path)}
  <li class="border-b last:border-0 dark:border-gray-800">
    <div
      class="flex items-center gap-2 px-3 py-2 text-sm"
      style:padding-left={`${12 + depth * 16}px`}
    >
      {#if expandable}
        <button
          type="button"
          class="text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
          aria-label={expanded ? 'Collapse' : 'Expand'}
          onclick={() => toggle(path)}
        >
          {expanded ? '▾' : '▸'}
        </button>
      {:else}
        <span class="inline-block w-3"></span>
      {/if}
      <span class="font-mono text-xs">{field.name}</span>
      <span class={`rounded-full px-2 py-0.5 text-[10px] font-medium ${typeBadge(field.type)}`}>
        {renderType(field)}
      </span>
      {#if field.nullable === false}
        <span class="rounded-full bg-slate-100 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-slate-600 dark:bg-gray-800 dark:text-gray-300">
          NOT NULL
        </span>
      {/if}
      {#if field.description}
        <span class="ml-2 truncate text-xs text-gray-500">{field.description}</span>
      {/if}
    </div>

    {#if expandable && expanded}
      <ul class="border-t bg-slate-50/50 dark:border-gray-800 dark:bg-gray-900/40">
        {#if field.subSchemas}
          {#each field.subSchemas as child, idx (child.name + idx)}
            {@render row(child, depth + 1, `${path}.${idx}`)}
          {/each}
        {/if}
        {#if field.arraySubType}
          {@render row({ ...field.arraySubType, name: '[item]' }, depth + 1, `${path}.item`)}
        {/if}
        {#if field.mapKeyType}
          {@render row({ ...field.mapKeyType, name: '[key]' }, depth + 1, `${path}.key`)}
        {/if}
        {#if field.mapValueType}
          {@render row({ ...field.mapValueType, name: '[value]' }, depth + 1, `${path}.value`)}
        {/if}
      </ul>
    {/if}
  </li>
{/snippet}

<section class="space-y-4">
  <header>
    <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Schema</div>
    <h2 class="mt-1 text-lg font-semibold">Columns</h2>
    <p class="mt-1 text-sm text-gray-500">
      {fields.length} top-level field{fields.length === 1 ? '' : 's'}. Click any STRUCT / ARRAY /
      MAP to drill into nested types.
    </p>
  </header>

  {#if fields.length === 0}
    <div class="rounded-xl border border-dashed border-slate-300 px-4 py-8 text-center text-sm text-gray-500 dark:border-gray-700">
      Schema is inferred from the next quality profile or upload.
    </div>
  {:else}
    <ul class="overflow-hidden rounded-xl border border-slate-200 bg-white dark:border-gray-700 dark:bg-gray-900">
      {#each fields as field, idx (field.name + idx)}
        {@render row(field, 0, String(idx))}
      {/each}
    </ul>
  {/if}

  {#if Object.keys(options).length > 0}
    <div class="rounded-xl border border-slate-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-900">
      <div class="text-xs uppercase tracking-[0.22em] text-gray-400">
        Format options{format ? ` · ${format}` : ''}
      </div>
      <dl class="mt-2 grid grid-cols-1 gap-1 text-sm md:grid-cols-2">
        {#each Object.entries(options) as [key, value] (key)}
          <div class="flex items-center justify-between rounded bg-slate-50 px-2 py-1 dark:bg-gray-800">
            <dt class="font-mono text-xs">{key}</dt>
            <dd class="font-mono text-xs">{String(value)}</dd>
          </div>
        {/each}
      </dl>
    </div>
  {/if}
</section>
