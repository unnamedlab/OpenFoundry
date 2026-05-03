<!--
  T6.x — SchemaViewer.

  Foundry-parity schema browser/editor. The wire format matches
  `DatasetSchemaPayload` in `$lib/api/datasets.ts`, which mirrors the
  Rust `DatasetSchema` in `services/dataset-versioning-service`.

  Modes:
   * `view` — collapsed/expanded tree for STRUCT, ARRAY, MAP nesting,
     plus DECIMAL(precision,scale).
   * `edit` — same tree but every node is editable: type picker per
     field, dedicated controls for DECIMAL precision/scale, ARRAY sub
     type, MAP key/value pickers, STRUCT children with reorder buttons.

  Schema options (CSV parsing) are surfaced in a collapsed section that
  is only visible when `file_format = TEXT`.

  When mode = edit and `viewId` is set, the "Save schema" button POSTs
  to `/v1/datasets/{rid}/views/{view_id}/schema` and renders an
  added/removed/changed diff vs. the schema that was loaded.
-->
<script lang="ts" module>
  import type {
    DatasetField,
    DatasetFieldType,
    DatasetFileFormat,
    DatasetSchemaPayload,
  } from '$lib/api/datasets';

  export type Mode = 'view' | 'edit';

  export type SchemaDiff = {
    added: string[];
    removed: string[];
    changed: string[];
  };

  /** Field type names recognised by the picker UI. */
  export const FIELD_TYPES: DatasetFieldType['type'][] = [
    'BOOLEAN',
    'BYTE',
    'SHORT',
    'INTEGER',
    'LONG',
    'FLOAT',
    'DOUBLE',
    'STRING',
    'BINARY',
    'DATE',
    'TIMESTAMP',
    'DECIMAL',
    'ARRAY',
    'MAP',
    'STRUCT',
  ];

  export function emptyField(name = 'new_field', type: DatasetFieldType['type'] = 'STRING'): DatasetField {
    const base: DatasetField = { name, type, nullable: true };
    if (type === 'DECIMAL') {
      base.precision = 38;
      base.scale = 18;
    } else if (type === 'ARRAY') {
      base.arraySubType = { name: 'item', type: 'STRING', nullable: true };
    } else if (type === 'MAP') {
      base.mapKeyType = { name: 'key', type: 'STRING', nullable: false };
      base.mapValueType = { name: 'value', type: 'STRING', nullable: true };
    } else if (type === 'STRUCT') {
      base.subSchemas = [{ name: 'field_1', type: 'STRING', nullable: true }];
    }
    return base;
  }

  export function diffSchemas(prev: DatasetField[], next: DatasetField[]): SchemaDiff {
    const added: string[] = [];
    const removed: string[] = [];
    const changed: string[] = [];
    const prevByName = new Map(prev.map((f) => [f.name, f]));
    const nextByName = new Map(next.map((f) => [f.name, f]));
    for (const [name, field] of nextByName) {
      const before = prevByName.get(name);
      if (!before) added.push(name);
      else if (renderType(before) !== renderType(field) || (before.nullable ?? true) !== (field.nullable ?? true))
        changed.push(name);
    }
    for (const name of prevByName.keys()) {
      if (!nextByName.has(name)) removed.push(name);
    }
    return { added, removed, changed };
  }

  export function renderType(field: DatasetField): string {
    const t = field.type;
    if (t === 'DECIMAL') return `DECIMAL(${field.precision ?? '?'},${field.scale ?? '?'})`;
    if (t === 'ARRAY') return field.arraySubType ? `ARRAY<${renderType(field.arraySubType)}>` : 'ARRAY<?>';
    if (t === 'MAP') {
      const k = field.mapKeyType ? renderType(field.mapKeyType) : '?';
      const v = field.mapValueType ? renderType(field.mapValueType) : '?';
      return `MAP<${k}, ${v}>`;
    }
    if (t === 'STRUCT') return `STRUCT(${(field.subSchemas ?? []).map((s) => s.name).join(', ')})`;
    return t;
  }

  export function isComplex(t: DatasetFieldType['type']): boolean {
    return t === 'ARRAY' || t === 'MAP' || t === 'STRUCT';
  }

  export function defaultCsvOptions() {
    return {
      delimiter: ',',
      quote: '"',
      escape: '\\',
      header: true,
      null_value: '',
      charset: 'UTF-8',
    } as const;
  }
</script>

<script lang="ts">
  import { putViewSchema, type DatasetSchemaResponse } from '$lib/api/datasets';

  type Props = {
    /** Foundry-parity payload. Treated as the *baseline* in edit mode for the diff. */
    schema: DatasetSchemaPayload;
    /** `view` (read-only) or `edit` (UI controls + save button). */
    mode?: Mode;
    /** Dataset RID. Required to call `POST .../schema`. */
    datasetRid?: string;
    /** View id this schema applies to. Surfaced in the "Schema applies to" footer. */
    viewId?: string;
    /** History tab callback — clicked from the "View history" link. */
    onOpenHistory?: () => void;
    /** Called after a successful save with the new server response. */
    onSaved?: (response: DatasetSchemaResponse) => void;
  };

  const { schema, mode = 'view', datasetRid, viewId, onOpenHistory, onSaved }: Props = $props();

  // Working copy. In view mode this is just `schema`; in edit mode we
  // mutate `working` and diff it against `baseline`. The parent may
  // swap the `schema` prop when the user navigates to a different view;
  // the effect below re-seeds both copies in that case.
  let baseline = $state<DatasetSchemaPayload>({ fields: [], file_format: 'PARQUET' });
  let working = $state<DatasetSchemaPayload>({ fields: [], file_format: 'PARQUET' });

  $effect(() => {
    baseline = structuredClone(schema);
    working = structuredClone(schema);
  });

  let expanded = $state<Set<string>>(new Set());

  let saving = $state(false);
  let saveError = $state<string | null>(null);
  let lastDiff = $state<SchemaDiff | null>(null);

  const fileFormat: DatasetFileFormat = $derived(working.file_format ?? 'PARQUET');
  const csv = $derived(working.custom_metadata?.csv);

  function toggle(key: string) {
    const next = new Set(expanded);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    expanded = next;
  }

  function setFileFormat(next: DatasetFileFormat) {
    working = { ...working, file_format: next };
    if (next !== 'TEXT') {
      working = { ...working, custom_metadata: null };
    } else if (!working.custom_metadata?.csv) {
      working = {
        ...working,
        custom_metadata: { csv: { ...defaultCsvOptions() } },
      };
    }
  }

  function patchCsv(patch: Partial<NonNullable<NonNullable<DatasetSchemaPayload['custom_metadata']>['csv']>>) {
    const current = working.custom_metadata?.csv ?? { ...defaultCsvOptions() };
    working = {
      ...working,
      custom_metadata: { csv: { ...current, ...patch } },
    };
  }

  function addField() {
    working = { ...working, fields: [...working.fields, emptyField(`field_${working.fields.length + 1}`)] };
  }

  function removeField(idx: number) {
    const next = working.fields.slice();
    next.splice(idx, 1);
    working = { ...working, fields: next };
  }

  function moveField(idx: number, delta: number) {
    const target = idx + delta;
    if (target < 0 || target >= working.fields.length) return;
    const next = working.fields.slice();
    [next[idx], next[target]] = [next[target], next[idx]];
    working = { ...working, fields: next };
  }

  function patchField(path: number[], patch: (field: DatasetField) => DatasetField) {
    const fields = working.fields.slice();
    const target = navigate(fields, path);
    target.parent[target.key] = patch(target.parent[target.key]);
    working = { ...working, fields };
  }

  // Walk a field tree by an index path (top-level idx, then subSchemas/arraySubType...).
  // Returns a {parent, key} pair so callers can write back in place.
  function navigate(fields: DatasetField[], path: number[]): { parent: any; key: any } {
    if (path.length === 1) return { parent: fields, key: path[0] };
    let cursor: any = fields[path[0]];
    for (let i = 1; i < path.length - 1; i++) {
      const idx = path[i];
      cursor = drillInto(cursor, idx);
    }
    const lastIdx = path[path.length - 1];
    return descend(cursor, lastIdx);
  }

  function drillInto(field: DatasetField, idx: number): any {
    if (field.type === 'STRUCT') return (field.subSchemas as DatasetField[])[idx];
    if (field.type === 'ARRAY') return field.arraySubType;
    if (field.type === 'MAP') return idx === 0 ? field.mapKeyType : field.mapValueType;
    return field;
  }

  function descend(field: DatasetField, idx: number): { parent: any; key: any } {
    if (field.type === 'STRUCT') return { parent: field.subSchemas as DatasetField[], key: idx };
    if (field.type === 'ARRAY') return { parent: field, key: 'arraySubType' };
    if (field.type === 'MAP') return { parent: field, key: idx === 0 ? 'mapKeyType' : 'mapValueType' };
    return { parent: field, key: 'name' };
  }

  function changeType(field: DatasetField, next: DatasetFieldType['type']): DatasetField {
    const fresh = emptyField(field.name, next);
    fresh.nullable = field.nullable;
    fresh.description = field.description;
    return fresh;
  }

  async function save() {
    if (!datasetRid || !viewId) return;
    saving = true;
    saveError = null;
    try {
      const response = await putViewSchema(datasetRid, viewId, working);
      lastDiff = diffSchemas(baseline.fields, response.schema.fields);
      baseline = structuredClone(response.schema);
      working = structuredClone(response.schema);
      onSaved?.(response);
    } catch (cause) {
      saveError = cause instanceof Error ? cause.message : 'Save failed.';
    } finally {
      saving = false;
    }
  }
</script>

<section class="space-y-4" data-component="schema-viewer">
  <header class="flex flex-col gap-2 rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950 md:flex-row md:items-center md:justify-between">
    <div>
      <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Schema</div>
      <h2 class="mt-1 text-lg font-semibold">
        {mode === 'edit' ? 'Edit schema' : 'View schema'}
      </h2>
      <p class="mt-1 text-sm text-slate-500">
        {working.fields.length} field{working.fields.length === 1 ? '' : 's'} · file format
        <span class="font-mono">{fileFormat}</span>
      </p>
    </div>
    <div class="flex flex-wrap items-center gap-2">
      {#if mode === 'edit'}
        <label class="text-sm">
          <span class="mr-2 text-slate-500">File format</span>
          <select
            class="rounded-md border border-slate-300 bg-white px-2 py-1 text-sm dark:border-gray-700 dark:bg-gray-900"
            value={fileFormat}
            onchange={(e) => setFileFormat((e.currentTarget as HTMLSelectElement).value as DatasetFileFormat)}
            data-testid="schema-file-format"
          >
            <option value="PARQUET">PARQUET</option>
            <option value="AVRO">AVRO</option>
            <option value="TEXT">TEXT</option>
          </select>
        </label>
        <button
          type="button"
          class="rounded-md border border-slate-300 bg-white px-3 py-1.5 text-sm hover:bg-slate-50 dark:border-gray-700 dark:bg-gray-900 dark:hover:bg-gray-800"
          onclick={addField}
          data-testid="schema-add-field"
        >
          Add field
        </button>
        <button
          type="button"
          class="rounded-md bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
          onclick={() => void save()}
          disabled={saving || !datasetRid || !viewId}
          data-testid="schema-save"
        >
          {saving ? 'Saving...' : 'Save schema'}
        </button>
      {/if}
    </div>
  </header>

  {#if saveError}
    <div class="rounded-lg border border-rose-200 bg-rose-50 px-4 py-2 text-sm text-rose-800 dark:border-rose-900/50 dark:bg-rose-950/40 dark:text-rose-100" role="alert">
      {saveError}
    </div>
  {/if}

  {#if lastDiff && (lastDiff.added.length || lastDiff.removed.length || lastDiff.changed.length)}
    <div class="rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-900 dark:border-emerald-900/50 dark:bg-emerald-950/40 dark:text-emerald-100" data-testid="schema-diff">
      <div class="font-medium">Schema saved.</div>
      <ul class="mt-1 list-disc pl-5 text-xs">
        {#if lastDiff.added.length}
          <li><span class="font-mono">+ {lastDiff.added.join(', ')}</span></li>
        {/if}
        {#if lastDiff.removed.length}
          <li><span class="font-mono">- {lastDiff.removed.join(', ')}</span></li>
        {/if}
        {#if lastDiff.changed.length}
          <li><span class="font-mono">~ {lastDiff.changed.join(', ')}</span></li>
        {/if}
      </ul>
    </div>
  {/if}

  <div class="rounded-lg border border-slate-200 bg-white shadow-sm dark:border-gray-800 dark:bg-gray-950">
    <table class="min-w-full divide-y divide-slate-200 text-sm dark:divide-gray-800">
      <thead class="text-left text-xs uppercase tracking-wide text-slate-500">
        <tr>
          <th class="py-2 pl-4 pr-3">Field</th>
          <th class="py-2 pr-3">Type</th>
          <th class="py-2 pr-3">Nullable</th>
          {#if mode === 'edit'}
            <th class="py-2 pr-3">Actions</th>
          {/if}
        </tr>
      </thead>
      <tbody class="divide-y divide-slate-100 dark:divide-gray-800" data-testid="schema-fields">
        {#each working.fields as field, idx (idx + ':' + field.name)}
          {@render fieldRow(field, [idx], 0)}
        {/each}
      </tbody>
    </table>
  </div>

  {#if fileFormat === 'TEXT'}
    <details class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950" data-testid="schema-options">
      <summary class="cursor-pointer text-sm font-medium">Schema options (CSV parsing)</summary>
      <div class="mt-3 grid grid-cols-1 gap-3 text-sm md:grid-cols-2">
        {@render csvField('delimiter', 'Delimiter', csv?.delimiter ?? ',')}
        {@render csvField('quote', 'Quote', csv?.quote ?? '"')}
        {@render csvField('escape', 'Escape', csv?.escape ?? '\\')}
        <label class="flex items-center gap-2">
          <input
            type="checkbox"
            checked={csv?.header ?? true}
            disabled={mode === 'view'}
            onchange={(e) => patchCsv({ header: (e.currentTarget as HTMLInputElement).checked })}
            data-testid="csv-header"
          />
          Header row
        </label>
        {@render csvField('null_value', 'Null value', csv?.null_value ?? '')}
        {@render csvField('date_format', 'Date format', csv?.date_format ?? '')}
        {@render csvField('timestamp_format', 'Timestamp format', csv?.timestamp_format ?? '')}
        {@render csvField('charset', 'Charset', csv?.charset ?? 'UTF-8')}
      </div>
    </details>
  {/if}

  {#if viewId}
    <footer class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-slate-200 bg-white px-4 py-2 text-xs text-slate-500 shadow-sm dark:border-gray-800 dark:bg-gray-950 dark:text-gray-400" data-testid="schema-view-link">
      <span>
        Schema applies to view
        <span class="font-mono text-slate-700 dark:text-gray-200">{viewId}</span>
      </span>
      {#if onOpenHistory}
        <button type="button" class="text-blue-600 hover:underline dark:text-blue-300" onclick={onOpenHistory}>
          View history
        </button>
      {/if}
    </footer>
  {/if}
</section>

{#snippet fieldRow(field: DatasetField, path: number[], depth: number)}
  {@const key = path.join(':')}
  {@const open = expanded.has(key)}
  {@const composite = isComplex(field.type)}
  <tr data-testid="schema-field" data-field-name={field.name} class="align-top">
    <td class="py-2 pl-4 pr-3" style:padding-left={`${1 + depth * 1.25}rem`}>
      <div class="flex items-center gap-2">
        {#if composite}
          <button type="button" class="text-slate-400 hover:text-slate-700 dark:hover:text-gray-200" onclick={() => toggle(key)} aria-label="Expand">
            {open ? '▾' : '▸'}
          </button>
        {:else}
          <span class="inline-block w-3"></span>
        {/if}
        {#if mode === 'edit'}
          <input
            class="w-40 rounded-md border border-slate-300 bg-white px-2 py-1 font-mono text-xs dark:border-gray-700 dark:bg-gray-900"
            value={field.name}
            onchange={(e) => patchField(path, (f) => ({ ...f, name: (e.currentTarget as HTMLInputElement).value }))}
            data-testid="field-name"
          />
        {:else}
          <span class="font-mono text-xs">{field.name}</span>
        {/if}
      </div>
    </td>
    <td class="py-2 pr-3">
      {#if mode === 'edit'}
        <select
          class="rounded-md border border-slate-300 bg-white px-2 py-1 text-xs dark:border-gray-700 dark:bg-gray-900"
          value={field.type}
          onchange={(e) => patchField(path, (f) => changeType(f, (e.currentTarget as HTMLSelectElement).value as DatasetFieldType['type']))}
          data-testid="field-type"
        >
          {#each FIELD_TYPES as t (t)}
            <option value={t}>{t}</option>
          {/each}
        </select>
        {#if field.type === 'DECIMAL'}
          <span class="ml-2 inline-flex items-center gap-1 text-xs">
            (
            <input
              class="w-12 rounded-md border border-slate-300 bg-white px-1 py-0.5 text-xs dark:border-gray-700 dark:bg-gray-900"
              type="number"
              min="1"
              max="38"
              value={field.precision ?? 38}
              onchange={(e) => patchField(path, (f) => ({ ...f, precision: Number((e.currentTarget as HTMLInputElement).value) }))}
              data-testid="decimal-precision"
            />,
            <input
              class="w-12 rounded-md border border-slate-300 bg-white px-1 py-0.5 text-xs dark:border-gray-700 dark:bg-gray-900"
              type="number"
              min="0"
              value={field.scale ?? 18}
              onchange={(e) => patchField(path, (f) => ({ ...f, scale: Number((e.currentTarget as HTMLInputElement).value) }))}
              data-testid="decimal-scale"
            />
            )
          </span>
        {/if}
      {:else}
        <span class="font-mono text-xs">{renderType(field)}</span>
      {/if}
    </td>
    <td class="py-2 pr-3">
      {#if mode === 'edit'}
        <input
          type="checkbox"
          checked={field.nullable ?? true}
          onchange={(e) => patchField(path, (f) => ({ ...f, nullable: (e.currentTarget as HTMLInputElement).checked }))}
          data-testid="field-nullable"
        />
      {:else}
        {field.nullable === false ? 'no' : 'yes'}
      {/if}
    </td>
    {#if mode === 'edit'}
      <td class="py-2 pr-3 text-xs">
        {#if path.length === 1}
          <button type="button" class="text-slate-500 hover:text-slate-800 dark:hover:text-gray-200" onclick={() => moveField(path[0], -1)} aria-label="Move up">↑</button>
          <button type="button" class="text-slate-500 hover:text-slate-800 dark:hover:text-gray-200" onclick={() => moveField(path[0], 1)} aria-label="Move down">↓</button>
          <button type="button" class="ml-1 text-rose-600 hover:underline" onclick={() => removeField(path[0])} data-testid="field-remove">remove</button>
        {/if}
      </td>
    {/if}
  </tr>
  {#if composite && open}
    {@render compositeChildren(field, path, depth + 1)}
  {/if}
{/snippet}

{#snippet compositeChildren(field: DatasetField, path: number[], depth: number)}
  {#if field.type === 'STRUCT' && field.subSchemas}
    {#each field.subSchemas as child, j (path.join(':') + ':struct:' + j + ':' + child.name)}
      {@render fieldRow(child, [...path, j], depth)}
    {/each}
    {#if mode === 'edit'}
      <tr>
        <td colspan="4" class="py-1 pl-4 text-xs" style:padding-left={`${1 + depth * 1.25}rem`}>
          <button
            type="button"
            class="text-blue-600 hover:underline"
            onclick={() => patchField(path, (f) => ({
              ...f,
              subSchemas: [...(f.subSchemas ?? []), emptyField(`field_${(f.subSchemas ?? []).length + 1}`)],
            }))}
            data-testid="struct-add-child"
          >
            + add child
          </button>
        </td>
      </tr>
    {/if}
  {:else if field.type === 'ARRAY' && field.arraySubType}
    {@render fieldRow(field.arraySubType, [...path, 0], depth)}
  {:else if field.type === 'MAP'}
    {#if field.mapKeyType}{@render fieldRow(field.mapKeyType, [...path, 0], depth)}{/if}
    {#if field.mapValueType}{@render fieldRow(field.mapValueType, [...path, 1], depth)}{/if}
  {/if}
{/snippet}

{#snippet csvField(key: 'delimiter' | 'quote' | 'escape' | 'null_value' | 'date_format' | 'timestamp_format' | 'charset', label: string, value: string)}
  <label class="flex flex-col gap-1">
    <span class="text-xs uppercase tracking-wide text-slate-400">{label}</span>
    <input
      class="rounded-md border border-slate-300 bg-white px-2 py-1 font-mono text-xs dark:border-gray-700 dark:bg-gray-900"
      value={value}
      readOnly={mode === 'view'}
      onchange={(e) => patchCsv({ [key]: (e.currentTarget as HTMLInputElement).value } as any)}
      data-testid={`csv-${key}`}
    />
  </label>
{/snippet}
