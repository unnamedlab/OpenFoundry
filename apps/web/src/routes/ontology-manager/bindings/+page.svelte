<script lang="ts">
  /**
   * `bindings/+page.svelte` — Wizard "Bind dataset → ObjectType" (UI of T2).
   *
   * Steps:
   *   1. Pick an Object Type and a Dataset (with search).
   *   2. Preview dataset schema + first rows; auto-suggest column→property mapping.
   *   3. Mapping editor (column→property) + primary-key selection.
   *   4. Sync mode (snapshot/incremental/view) + default marking + preview limit.
   *      → "Create binding" then optional "Materialize now".
   *
   * Backend: `createObjectTypeBinding`, `materializeObjectTypeBinding`
   * (T2 handlers in libs/ontology-kernel/src/handlers/bindings.rs).
   */
  import { goto } from '$app/navigation';
  import { onMount } from 'svelte';
  import { listDatasets, previewDataset, type Dataset, type DatasetPreviewResponse } from '$lib/api/datasets';
  import {
    createObjectTypeBinding,
    listObjectTypes,
    listProperties,
    materializeObjectTypeBinding,
    type MaterializeBindingResponse,
    type ObjectType,
    type ObjectTypeBinding,
    type ObjectTypeBindingPropertyMapping,
    type ObjectTypeBindingSyncMode,
    type Property,
  } from '$lib/api/ontology';
  import { notifications } from '$lib/stores/notifications';

  type Step = 1 | 2 | 3 | 4;

  let step = $state<Step>(1);
  let loading = $state(false);
  let error = $state('');

  // ---- Step 1 -------------------------------------------------------------
  let objectTypes = $state<ObjectType[]>([]);
  let datasets = $state<Dataset[]>([]);
  let datasetSearch = $state('');
  let selectedTypeId = $state('');
  let selectedDatasetId = $state('');

  // ---- Step 2 -------------------------------------------------------------
  let preview = $state<DatasetPreviewResponse | null>(null);
  let typeProperties = $state<Property[]>([]);

  // ---- Step 3 -------------------------------------------------------------
  let mapping = $state<ObjectTypeBindingPropertyMapping[]>([]);
  let primaryKeyColumn = $state('');
  // Drag/drop state
  let draggingColumn: string | null = null;

  // ---- Step 4 -------------------------------------------------------------
  let syncMode = $state<ObjectTypeBindingSyncMode>('snapshot');
  let defaultMarking = $state('public');
  let previewLimit = $state(1000);
  let datasetBranch = $state('');
  let datasetVersion = $state<number | ''>('');

  // ---- Result -------------------------------------------------------------
  let createdBinding = $state<ObjectTypeBinding | null>(null);
  let materializeResult = $state<MaterializeBindingResponse | null>(null);
  let materializing = $state(false);

  onMount(async () => {
    loading = true;
    try {
      const [types, ds] = await Promise.all([
        listObjectTypes({ per_page: 200 }),
        listDatasets({ page: 1, per_page: 200 }),
      ]);
      objectTypes = types.data ?? [];
      datasets = ds.data ?? [];
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  });

  const filteredDatasets = $derived(
    datasetSearch.trim()
      ? datasets.filter((d) => d.name.toLowerCase().includes(datasetSearch.toLowerCase()))
      : datasets,
  );
  const selectedType = $derived(objectTypes.find((t) => t.id === selectedTypeId) ?? null);
  const selectedDataset = $derived(datasets.find((d) => d.id === selectedDatasetId) ?? null);
  const previewColumns = $derived(preview?.columns ?? []);
  const unmappedColumns = $derived(
    previewColumns.filter((col) => !mapping.some((m) => m.source_column === col.name)),
  );
  const unmappedProperties = $derived(
    typeProperties.filter((p) => !mapping.some((m) => m.target_property === p.name)),
  );

  function next() {
    if (step < 4) step = (step + 1) as Step;
  }
  function back() {
    if (step > 1) step = (step - 1) as Step;
  }

  async function goToStep2() {
    if (!selectedTypeId) return notifications.error('Choose an Object Type');
    if (!selectedDatasetId) return notifications.error('Choose a Dataset');
    loading = true;
    error = '';
    try {
      const [pv, props] = await Promise.all([
        previewDataset(selectedDatasetId, { limit: 25 }),
        listProperties(selectedTypeId),
      ]);
      preview = pv;
      typeProperties = props;
      // Auto-suggest mapping by name match.
      mapping = autoMapping(pv.columns ?? [], props);
      // Pre-pick PK = first column whose name matches type's primary_key_property
      if (selectedType?.primary_key_property) {
        const pk = (pv.columns ?? []).find(
          (c) => c.name.toLowerCase() === (selectedType?.primary_key_property ?? '').toLowerCase(),
        );
        if (pk) primaryKeyColumn = pk.name;
      } else {
        primaryKeyColumn = (pv.columns ?? [])[0]?.name ?? '';
      }
      step = 2;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      notifications.error(error);
    } finally {
      loading = false;
    }
  }

  function autoMapping(
    columns: NonNullable<DatasetPreviewResponse['columns']>,
    properties: Property[],
  ): ObjectTypeBindingPropertyMapping[] {
    const out: ObjectTypeBindingPropertyMapping[] = [];
    for (const col of columns) {
      const match = properties.find(
        (p) => p.name.toLowerCase() === col.name.toLowerCase(),
      );
      if (match) {
        out.push({ source_column: col.name, target_property: match.name });
      }
    }
    return out;
  }

  function addMapping(sourceColumn: string, targetProperty: string) {
    if (!sourceColumn || !targetProperty) return;
    mapping = [
      ...mapping.filter((m) => m.source_column !== sourceColumn && m.target_property !== targetProperty),
      { source_column: sourceColumn, target_property: targetProperty },
    ];
  }

  function removeMapping(index: number) {
    mapping = mapping.filter((_, i) => i !== index);
  }

  function onDragStart(event: DragEvent, sourceColumn: string) {
    draggingColumn = sourceColumn;
    event.dataTransfer?.setData('text/plain', sourceColumn);
  }
  function onDragOver(event: DragEvent) {
    event.preventDefault();
  }
  function onDropOnProperty(event: DragEvent, targetProperty: string) {
    event.preventDefault();
    const source = event.dataTransfer?.getData('text/plain') || draggingColumn || '';
    if (source) addMapping(source, targetProperty);
    draggingColumn = null;
  }

  async function createBinding() {
    if (!selectedTypeId || !selectedDatasetId) return;
    if (!primaryKeyColumn) return notifications.error('Pick a primary key column');
    loading = true;
    error = '';
    try {
      const binding = await createObjectTypeBinding(selectedTypeId, {
        dataset_id: selectedDatasetId,
        dataset_branch: datasetBranch.trim() || undefined,
        dataset_version: datasetVersion === '' ? undefined : Number(datasetVersion),
        primary_key_column: primaryKeyColumn,
        property_mapping: mapping,
        sync_mode: syncMode,
        default_marking: defaultMarking || undefined,
        preview_limit: previewLimit,
      });
      createdBinding = binding;
      notifications.success('Binding created');
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      notifications.error(`Create failed: ${error}`);
    } finally {
      loading = false;
    }
  }

  async function runMaterialize(dryRun = false) {
    if (!createdBinding) return;
    materializing = true;
    error = '';
    try {
      const result = await materializeObjectTypeBinding(
        createdBinding.object_type_id,
        createdBinding.id,
        { dry_run: dryRun },
      );
      materializeResult = result;
      notifications.success(
        `Materialized: read=${result.rows_read} ins=${result.inserted} upd=${result.updated} err=${result.errors}`,
      );
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      notifications.error(`Materialize failed: ${error}`);
    } finally {
      materializing = false;
    }
  }

  function reset() {
    step = 1;
    selectedTypeId = '';
    selectedDatasetId = '';
    preview = null;
    typeProperties = [];
    mapping = [];
    primaryKeyColumn = '';
    syncMode = 'snapshot';
    defaultMarking = 'public';
    previewLimit = 1000;
    datasetBranch = '';
    datasetVersion = '';
    createdBinding = null;
    materializeResult = null;
  }
</script>

<svelte:head><title>Bind dataset to ObjectType — OpenFoundry</title></svelte:head>

<main class="wizard">
  <header>
    <h1>Bind dataset → ObjectType</h1>
    <p class="lead">Turn a dataset into an Object Type; rows become object instances.</p>
    <ol class="steps" aria-label="Wizard steps">
      <li class:active={step === 1} class:done={step > 1}>1 · Source</li>
      <li class:active={step === 2} class:done={step > 2}>2 · Schema</li>
      <li class:active={step === 3} class:done={step > 3}>3 · Mapping</li>
      <li class:active={step === 4}>4 · Sync</li>
    </ol>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </header>

  {#if step === 1}
    <section class="step">
      <div class="picker">
        <h2>Object Type</h2>
        <select bind:value={selectedTypeId} disabled={loading} size={10}>
          {#each objectTypes as ot (ot.id)}
            <option value={ot.id}>{ot.display_name || ot.name}</option>
          {/each}
        </select>
      </div>
      <div class="picker">
        <h2>Dataset</h2>
        <input bind:value={datasetSearch} placeholder="Filter datasets…" />
        <select bind:value={selectedDatasetId} disabled={loading} size={10}>
          {#each filteredDatasets as ds (ds.id)}
            <option value={ds.id}>{ds.name} · {ds.format} · {ds.row_count} rows</option>
          {/each}
        </select>
      </div>
    </section>

    <footer class="nav">
      <a href="/ontology-manager" class="back">← Back to manager</a>
      <button type="button" class="primary" disabled={loading || !selectedTypeId || !selectedDatasetId} onclick={() => void goToStep2()}>
        {loading ? 'Loading…' : 'Continue →'}
      </button>
    </footer>
  {/if}

  {#if step === 2 && preview}
    <section class="step">
      <h2>Schema preview ({previewColumns.length} columns, {preview.total_rows ?? preview.row_count ?? '?'} rows)</h2>
      <div class="schema-table">
        <table>
          <thead>
            <tr><th>Column</th><th>Type</th><th>Nullable</th><th>Sample</th></tr>
          </thead>
          <tbody>
            {#each previewColumns as col (col.name)}
              <tr>
                <td class="mono">{col.name}</td>
                <td>{col.data_type ?? col.field_type ?? '—'}</td>
                <td>{col.nullable === false ? 'no' : 'yes'}</td>
                <td class="mono small">{formatSample(preview?.rows?.[0]?.[col.name])}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </section>

    <footer class="nav">
      <button type="button" onclick={back}>← Back</button>
      <button type="button" class="primary" onclick={next}>Continue →</button>
    </footer>
  {/if}

  {#if step === 3}
    <section class="step">
      <h2>Map columns → properties</h2>
      <p class="hint">Drag a dataset column onto an Object Type property. Pick the primary-key column at the bottom.</p>
      <div class="mapping">
        <div class="cols">
          <h3>Dataset columns ({unmappedColumns.length}/{previewColumns.length})</h3>
          <ul>
            {#each previewColumns as col (col.name)}
              <li
                draggable="true"
                ondragstart={(e) => onDragStart(e, col.name)}
                class:mapped={!unmappedColumns.includes(col)}
              >
                <span class="mono">{col.name}</span>
                <span class="ty">{col.data_type ?? col.field_type ?? ''}</span>
              </li>
            {/each}
          </ul>
        </div>

        <div class="props">
          <h3>Object Type properties ({unmappedProperties.length}/{typeProperties.length})</h3>
          <ul>
            {#each typeProperties as prop (prop.id)}
              <li
                ondragover={onDragOver}
                ondrop={(e) => onDropOnProperty(e, prop.name)}
                class:mapped={!unmappedProperties.includes(prop)}
              >
                <span class="mono">{prop.name}</span>
                <span class="ty">{prop.property_type}</span>
                {#if prop.required}<span class="badge req">required</span>{/if}
              </li>
            {/each}
          </ul>
        </div>

        <div class="mappings">
          <h3>Mappings ({mapping.length})</h3>
          <ul>
            {#each mapping as m, idx (m.source_column + ':' + m.target_property)}
              <li>
                <span class="mono">{m.source_column}</span>
                <span class="arrow">→</span>
                <span class="mono">{m.target_property}</span>
                <button type="button" class="x" onclick={() => removeMapping(idx)} aria-label="Remove">×</button>
              </li>
            {/each}
          </ul>
        </div>
      </div>

      <div class="pk">
        <label>
          <span>Primary key column</span>
          <select bind:value={primaryKeyColumn}>
            {#each previewColumns as col (col.name)}
              <option value={col.name}>{col.name}</option>
            {/each}
          </select>
        </label>
        {#if selectedType?.primary_key_property}
          <p class="hint">Object type expects this column to project into property <code>{selectedType.primary_key_property}</code>.</p>
        {/if}
      </div>
    </section>

    <footer class="nav">
      <button type="button" onclick={back}>← Back</button>
      <button type="button" class="primary" onclick={next} disabled={!primaryKeyColumn}>
        Continue →
      </button>
    </footer>
  {/if}

  {#if step === 4}
    <section class="step">
      <h2>Sync configuration</h2>
      <div class="grid">
        <label>
          <span>Sync mode</span>
          <select bind:value={syncMode}>
            <option value="snapshot">snapshot — full overwrite</option>
            <option value="incremental">incremental — upsert by PK</option>
            <option value="view">view — read-through</option>
          </select>
        </label>
        <label>
          <span>Default marking</span>
          <select bind:value={defaultMarking}>
            <option value="public">public</option>
            <option value="confidential">confidential</option>
            <option value="pii">pii</option>
          </select>
        </label>
        <label>
          <span>Preview limit (rows)</span>
          <input type="number" bind:value={previewLimit} min="1" max="100000" />
        </label>
        <label>
          <span>Dataset branch (optional)</span>
          <input bind:value={datasetBranch} placeholder="main" />
        </label>
        <label>
          <span>Dataset version (optional)</span>
          <input type="number" bind:value={datasetVersion} placeholder="latest" />
        </label>
      </div>

      {#if !createdBinding}
        <div class="actions">
          <button type="button" onclick={back}>← Back</button>
          <button type="button" class="primary" onclick={() => void createBinding()} disabled={loading}>
            {loading ? 'Creating…' : 'Create binding'}
          </button>
        </div>
      {:else}
        <section class="result">
          <h3>Binding created ✓</h3>
          <dl>
            <dt>id</dt><dd class="mono">{createdBinding.id}</dd>
            <dt>sync_mode</dt><dd>{createdBinding.sync_mode}</dd>
            <dt>marking</dt><dd>{createdBinding.default_marking}</dd>
          </dl>

          <div class="actions">
            <button type="button" onclick={() => void runMaterialize(true)} disabled={materializing}>
              {materializing ? 'Running…' : 'Dry-run'}
            </button>
            <button type="button" class="primary" onclick={() => void runMaterialize(false)} disabled={materializing}>
              {materializing ? 'Materializing…' : 'Materialize now'}
            </button>
            <button type="button" onclick={() => void goto(`/ontology-manager`)}>Done</button>
            <button type="button" onclick={reset}>Bind another</button>
          </div>

          {#if materializeResult}
            <section class="audit" data-status={materializeResult.status}>
              <strong>Result · {materializeResult.status}{materializeResult.dry_run ? ' (dry-run)' : ''}</strong>
              <ul>
                <li>rows_read: {materializeResult.rows_read}</li>
                <li>inserted: {materializeResult.inserted}</li>
                <li>updated: {materializeResult.updated}</li>
                <li>skipped: {materializeResult.skipped}</li>
                <li>errors: {materializeResult.errors}</li>
              </ul>
              {#if materializeResult.error_details && materializeResult.error_details.length}
                <details>
                  <summary>Error details ({materializeResult.error_details.length})</summary>
                  <pre>{JSON.stringify(materializeResult.error_details, null, 2)}</pre>
                </details>
              {/if}
            </section>
          {/if}
        </section>
      {/if}
    </section>
  {/if}
</main>

<script lang="ts" module>
  function formatSample(value: unknown): string {
    if (value === null || value === undefined) return '∅';
    if (typeof value === 'string') return value.length > 40 ? value.slice(0, 37) + '…' : value;
    if (typeof value === 'number' || typeof value === 'boolean') return String(value);
    return JSON.stringify(value).slice(0, 60);
  }
</script>

<style>
  .wizard { max-width: 1100px; margin: 0 auto; padding: 1.5rem; color: #e2e8f0; }
  header { margin-bottom: 1.5rem; }
  h1 { margin: 0 0 0.25rem; font-size: 1.5rem; }
  .lead { margin: 0 0 1rem; color: #94a3b8; }
  .steps { display: flex; gap: 0.5rem; list-style: none; padding: 0; margin: 0; }
  .steps li { padding: 0.4rem 0.75rem; background: #1e293b; border-radius: 6px; font-size: 0.85rem; color: #94a3b8; }
  .steps li.active { background: #2563eb; color: white; }
  .steps li.done { background: #064e3b; color: #6ee7b7; }
  .error { color: #fca5a5; margin: 0.5rem 0 0; }
  .step { background: #0f172a; border: 1px solid #1e293b; border-radius: 8px; padding: 1rem; }
  .step h2 { margin: 0 0 0.75rem; font-size: 1.1rem; }
  .picker { display: flex; flex-direction: column; gap: 0.5rem; }
  section.step { display: grid; grid-template-columns: 1fr 1fr; gap: 1.25rem; }
  section.step:has(.schema-table), section.step:has(.mapping), section.step:has(.grid) { display: block; }
  select, input { background: #0b1220; color: inherit; border: 1px solid #334155; padding: 0.4rem 0.5rem; border-radius: 6px; font: inherit; }
  select[size] { padding: 0; }
  .nav { display: flex; justify-content: space-between; margin-top: 1rem; }
  .nav .back { color: #94a3b8; align-self: center; text-decoration: none; }
  .nav .back:hover { color: #cbd5e1; }
  button { background: #1e293b; color: #e2e8f0; border: 1px solid #334155; padding: 0.5rem 1rem; border-radius: 6px; cursor: pointer; }
  button:hover { background: #334155; }
  button.primary { background: #2563eb; border-color: #2563eb; color: white; }
  button:disabled { opacity: 0.5; cursor: not-allowed; }
  .schema-table { max-height: 50vh; overflow: auto; border: 1px solid #1e293b; border-radius: 6px; }
  table { width: 100%; border-collapse: collapse; font-size: 0.85rem; }
  th, td { padding: 0.4rem 0.6rem; border-bottom: 1px solid #1e293b; text-align: left; }
  .mono { font-family: monospace; color: #cbd5e1; }
  .small { font-size: 0.75rem; color: #94a3b8; }
  .hint { color: #64748b; font-size: 0.85rem; margin: 0 0 0.75rem; }
  .mapping { display: grid; grid-template-columns: 1fr 1fr 1.4fr; gap: 0.75rem; }
  .mapping h3 { font-size: 0.9rem; margin: 0 0 0.5rem; color: #94a3b8; }
  .mapping ul { list-style: none; padding: 0; margin: 0; max-height: 50vh; overflow: auto; display: flex; flex-direction: column; gap: 0.25rem; }
  .mapping li { display: flex; gap: 0.5rem; align-items: center; padding: 0.35rem 0.5rem; border: 1px solid #1e293b; border-radius: 5px; background: #0b1220; }
  .cols li { cursor: grab; }
  .cols li.mapped { opacity: 0.4; }
  .props li.mapped { background: #064e3b; }
  .props li { min-height: 32px; }
  .ty { font-size: 0.7rem; color: #64748b; padding: 0 0.3rem; background: #1e293b; border-radius: 3px; }
  .badge.req { background: #b91c1c; color: white; padding: 0 0.3rem; border-radius: 3px; font-size: 0.65rem; }
  .arrow { color: #64748b; }
  .x { background: transparent; border: none; color: #94a3b8; padding: 0 0.25rem; font-size: 1rem; }
  .pk { margin-top: 1rem; padding-top: 0.75rem; border-top: 1px dashed #1e293b; }
  .pk label { display: flex; gap: 0.5rem; align-items: center; }
  .pk code { background: #1e293b; padding: 0 0.25rem; border-radius: 3px; }
  .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0.75rem 1rem; }
  .grid label { display: flex; flex-direction: column; gap: 0.25rem; font-size: 0.85rem; }
  .actions { display: flex; gap: 0.5rem; margin-top: 1rem; flex-wrap: wrap; }
  .result { margin-top: 1rem; }
  .result dl { display: grid; grid-template-columns: max-content 1fr; gap: 0.25rem 0.75rem; font-size: 0.85rem; }
  .result dt { color: #64748b; }
  .audit { margin-top: 1rem; padding: 0.75rem; border-radius: 6px; background: #0b1220; border: 1px solid #1e293b; }
  .audit[data-status='completed'] { border-color: #047857; }
  .audit[data-status='completed_with_errors'] { border-color: #b45309; }
  .audit[data-status='failed'] { border-color: #b91c1c; }
  .audit ul { display: flex; gap: 0.75rem; list-style: none; padding: 0; margin: 0.5rem 0 0; font-size: 0.85rem; }
  pre { font-size: 0.75rem; color: #cbd5e1; max-height: 240px; overflow: auto; }
</style>
