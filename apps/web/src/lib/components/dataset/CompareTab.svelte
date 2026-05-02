<!--
  T5.4 — CompareTab

  Diff two transactions or two branches. The parent owns the lookup
  of file lists / schema fields per side; we only present the diff.

  Fields-side diff classifies each schema field as `added`,
  `removed`, or `modified` (same name, different type / nullable).
  Files-side diff classifies file paths the same way.
-->
<script lang="ts">
  import type { DatasetBranch, DatasetTransaction, DatasetFilesystemEntry } from '$lib/api/datasets';
  import type { SchemaField } from './details/SchemaPanel.svelte';

  export type CompareSide = {
    /** Display label, e.g. "v3" or "feature-branch @ HEAD". */
    label: string;
    schema: SchemaField[];
    files: DatasetFilesystemEntry[];
  };

  type Selector = { kind: 'transaction' | 'branch'; value: string };

  type Props = {
    transactions: DatasetTransaction[];
    branches: DatasetBranch[];
    sideA: CompareSide | null;
    sideB: CompareSide | null;
    /** What's currently selected in the pickers (so the parent can refetch). */
    selectorA: Selector;
    selectorB: Selector;
    loading?: boolean;
    error?: string;
    onChangeSelector: (which: 'A' | 'B', selector: Selector) => void;
  };

  const {
    transactions,
    branches,
    sideA,
    sideB,
    selectorA,
    selectorB,
    loading = false,
    error = '',
    onChangeSelector,
  }: Props = $props();

  type SchemaDiff = {
    added: SchemaField[];
    removed: SchemaField[];
    modified: Array<{ name: string; before: SchemaField; after: SchemaField }>;
  };

  type FileDiff = {
    added: DatasetFilesystemEntry[];
    removed: DatasetFilesystemEntry[];
    modified: Array<{ path: string; before: DatasetFilesystemEntry; after: DatasetFilesystemEntry }>;
  };

  const schemaDiff = $derived<SchemaDiff>(diffSchemas(sideA?.schema ?? [], sideB?.schema ?? []));
  const fileDiff = $derived<FileDiff>(diffFiles(sideA?.files ?? [], sideB?.files ?? []));

  function diffSchemas(a: SchemaField[], b: SchemaField[]): SchemaDiff {
    const aMap = new Map(a.map((f) => [f.name, f]));
    const bMap = new Map(b.map((f) => [f.name, f]));
    const added: SchemaField[] = [];
    const removed: SchemaField[] = [];
    const modified: SchemaDiff['modified'] = [];
    for (const [name, after] of bMap) {
      const before = aMap.get(name);
      if (!before) added.push(after);
      else if (before.type !== after.type || before.nullable !== after.nullable)
        modified.push({ name, before, after });
    }
    for (const [name, before] of aMap) {
      if (!bMap.has(name)) removed.push(before);
    }
    return { added, removed, modified };
  }

  function diffFiles(a: DatasetFilesystemEntry[], b: DatasetFilesystemEntry[]): FileDiff {
    const aMap = new Map(a.map((f) => [f.path, f]));
    const bMap = new Map(b.map((f) => [f.path, f]));
    const added: DatasetFilesystemEntry[] = [];
    const removed: DatasetFilesystemEntry[] = [];
    const modified: FileDiff['modified'] = [];
    for (const [path, after] of bMap) {
      const before = aMap.get(path);
      if (!before) added.push(after);
      else if ((before.size_bytes ?? 0) !== (after.size_bytes ?? 0))
        modified.push({ path, before, after });
    }
    for (const [path, before] of aMap) {
      if (!bMap.has(path)) removed.push(before);
    }
    return { added, removed, modified };
  }

  function pickerOptions(): Array<{ value: string; label: string; selector: Selector }> {
    const out: Array<{ value: string; label: string; selector: Selector }> = [];
    for (const b of branches) {
      out.push({ value: `branch:${b.name}`, label: `Branch ${b.name}`, selector: { kind: 'branch', value: b.name } });
    }
    for (const tx of transactions.slice(0, 50)) {
      out.push({
        value: `transaction:${tx.id}`,
        label: `${tx.operation} ${tx.id.slice(0, 8)} · ${new Date(tx.created_at).toLocaleDateString()}`,
        selector: { kind: 'transaction', value: tx.id },
      });
    }
    return out;
  }

  function selectorValue(s: Selector): string {
    return `${s.kind}:${s.value}`;
  }

  function onPickerChange(which: 'A' | 'B', event: Event) {
    const value = (event.target as HTMLSelectElement).value;
    const opts = pickerOptions();
    const opt = opts.find((o) => o.value === value);
    if (opt) onChangeSelector(which, opt.selector);
  }
</script>

<section class="space-y-4">
  <header>
    <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Compare</div>
    <h2 class="mt-1 text-lg font-semibold">Schema and file diff</h2>
    <p class="mt-1 text-sm text-gray-500">Pick two transactions or branches to compare.</p>
  </header>

  <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
    {#each [{ key: 'A', selector: selectorA, side: sideA }, { key: 'B', selector: selectorB, side: sideB }] as picker (picker.key)}
      <div class="rounded-xl border border-slate-200 bg-white p-3 dark:border-gray-700 dark:bg-gray-900">
        <label class="text-xs uppercase tracking-wide text-gray-400" for={`compare-${picker.key}`}>
          Side {picker.key}
        </label>
        <select
          id={`compare-${picker.key}`}
          class="mt-1 w-full rounded-lg border border-slate-200 px-2 py-1 text-sm dark:border-gray-700 dark:bg-gray-800"
          value={selectorValue(picker.selector)}
          onchange={(e) => onPickerChange(picker.key as 'A' | 'B', e)}
        >
          {#each pickerOptions() as opt (opt.value)}
            <option value={opt.value}>{opt.label}</option>
          {/each}
        </select>
        {#if picker.side}
          <div class="mt-1 text-xs text-gray-500">
            {picker.side.schema.length} field{picker.side.schema.length === 1 ? '' : 's'} ·
            {picker.side.files.length} file{picker.side.files.length === 1 ? '' : 's'}
          </div>
        {/if}
      </div>
    {/each}
  </div>

  {#if error}
    <div class="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300">
      {error}
    </div>
  {/if}

  {#if loading}
    <div class="rounded-xl border border-slate-200 bg-white px-4 py-6 text-center text-sm text-gray-500 dark:border-gray-700 dark:bg-gray-900">
      Loading comparison…
    </div>
  {:else if sideA && sideB}
    <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
      <!-- Schema diff -->
      <div class="rounded-xl border border-slate-200 bg-white p-3 dark:border-gray-700 dark:bg-gray-900">
        <div class="text-xs uppercase tracking-wide text-gray-400">Schema diff</div>
        <ul class="mt-2 space-y-1 text-sm">
          {#each schemaDiff.added as f (f.name)}
            <li class="flex items-center gap-2 rounded bg-emerald-50 px-2 py-1 dark:bg-emerald-950/30">
              <span class="text-emerald-600 dark:text-emerald-300">+</span>
              <span class="font-mono text-xs">{f.name}</span>
              <span class="text-xs text-gray-500">{f.type}</span>
            </li>
          {/each}
          {#each schemaDiff.removed as f (f.name)}
            <li class="flex items-center gap-2 rounded bg-rose-50 px-2 py-1 dark:bg-rose-950/30">
              <span class="text-rose-600 dark:text-rose-300">−</span>
              <span class="font-mono text-xs">{f.name}</span>
              <span class="text-xs text-gray-500">{f.type}</span>
            </li>
          {/each}
          {#each schemaDiff.modified as m (m.name)}
            <li class="flex items-center gap-2 rounded bg-amber-50 px-2 py-1 dark:bg-amber-950/30">
              <span class="text-amber-600 dark:text-amber-300">~</span>
              <span class="font-mono text-xs">{m.name}</span>
              <span class="text-xs text-gray-500">{m.before.type} → {m.after.type}</span>
            </li>
          {/each}
          {#if schemaDiff.added.length + schemaDiff.removed.length + schemaDiff.modified.length === 0}
            <li class="text-xs text-gray-500">No schema changes.</li>
          {/if}
        </ul>
      </div>

      <!-- Files diff -->
      <div class="rounded-xl border border-slate-200 bg-white p-3 dark:border-gray-700 dark:bg-gray-900">
        <div class="text-xs uppercase tracking-wide text-gray-400">Files diff</div>
        <ul class="mt-2 space-y-1 text-sm">
          {#each fileDiff.added as f (f.path)}
            <li class="flex items-center gap-2 rounded bg-emerald-50 px-2 py-1 dark:bg-emerald-950/30">
              <span class="text-emerald-600 dark:text-emerald-300">+</span>
              <span class="truncate font-mono text-xs">{f.path}</span>
            </li>
          {/each}
          {#each fileDiff.removed as f (f.path)}
            <li class="flex items-center gap-2 rounded bg-rose-50 px-2 py-1 dark:bg-rose-950/30">
              <span class="text-rose-600 dark:text-rose-300">−</span>
              <span class="truncate font-mono text-xs">{f.path}</span>
            </li>
          {/each}
          {#each fileDiff.modified as m (m.path)}
            <li class="flex items-center gap-2 rounded bg-amber-50 px-2 py-1 dark:bg-amber-950/30">
              <span class="text-amber-600 dark:text-amber-300">~</span>
              <span class="truncate font-mono text-xs">{m.path}</span>
              <span class="text-xs text-gray-500">
                {(m.before.size_bytes ?? 0).toLocaleString()} → {(m.after.size_bytes ?? 0).toLocaleString()} B
              </span>
            </li>
          {/each}
          {#if fileDiff.added.length + fileDiff.removed.length + fileDiff.modified.length === 0}
            <li class="text-xs text-gray-500">No file changes.</li>
          {/if}
        </ul>
      </div>
    </div>
  {/if}
</section>
