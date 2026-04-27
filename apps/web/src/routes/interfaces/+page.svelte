<script lang="ts">
  import { onMount } from 'svelte';
  import Glyph from '$components/ui/Glyph.svelte';
  import {
    attachInterfaceToType,
    createInterface,
    createInterfaceProperty,
    deleteInterface,
    deleteInterfaceProperty,
    detachInterfaceFromType,
    listInterfaceProperties,
    listInterfaces,
    listObjectTypes,
    listTypeInterfaces,
    updateInterface,
    updateInterfaceProperty,
    type InterfaceProperty,
    type ObjectType,
    type OntologyInterface
  } from '$lib/api/ontology';

  type WorkbenchTab = 'library' | 'definition' | 'implementation' | 'reference';

  interface TypeInterfaceBindingRow {
    objectType: ObjectType;
    interfaces: OntologyInterface[];
  }

  interface InterfaceStats {
    propertyCount: number;
    implementationCount: number;
    requiredPropertyCount: number;
    timeDependentCount: number;
  }

  const workbenchTabs: { id: WorkbenchTab; label: string; glyph: 'folder' | 'code' | 'link' | 'help' }[] = [
    { id: 'library', label: 'Library', glyph: 'folder' },
    { id: 'definition', label: 'Definition', glyph: 'code' },
    { id: 'implementation', label: 'Implementation', glyph: 'link' },
    { id: 'reference', label: 'Reference', glyph: 'help' }
  ];

  const propertyTypeOptions = [
    'string',
    'integer',
    'float',
    'boolean',
    'date',
    'json',
    'array',
    'reference',
    'geo_point',
    'media_reference',
    'vector'
  ];

  let interfaces = $state<OntologyInterface[]>([]);
  let properties = $state<InterfaceProperty[]>([]);
  let objectTypes = $state<ObjectType[]>([]);
  let typeBindings = $state<TypeInterfaceBindingRow[]>([]);

  let loading = $state(true);
  let contextLoading = $state(false);
  let bindingLoading = $state(false);
  let savingInterface = $state(false);
  let deletingInterface = $state(false);
  let savingProperty = $state(false);
  let bindingTypeId = $state('');

  let pageError = $state('');
  let saveError = $state('');
  let saveSuccess = $state('');
  let propertyError = $state('');
  let propertySuccess = $state('');

  let activeTab = $state<WorkbenchTab>('library');
  let catalogQuery = $state('');
  let selectedInterfaceId = $state('');

  let interfaceName = $state('');
  let interfaceDisplayName = $state('');
  let interfaceDescription = $state('');

  let propertyName = $state('');
  let propertyDisplayName = $state('');
  let propertyDescription = $state('');
  let propertyType = $state('string');
  let propertyRequired = $state(false);
  let propertyUnique = $state(false);
  let propertyTimeDependent = $state(false);
  let propertyDefaultValueText = $state('null');
  let propertyValidationRulesText = $state('{}');

  const selectedInterface = $derived(interfaces.find((item) => item.id === selectedInterfaceId) ?? null);
  const filteredInterfaces = $derived.by(() => {
    const query = catalogQuery.trim().toLowerCase();
    if (!query) return interfaces;
    return interfaces.filter((item) =>
      `${item.name} ${item.display_name} ${item.description}`.toLowerCase().includes(query)
    );
  });
  const implementationRows = $derived.by(() => {
    if (!selectedInterfaceId) return [];
    return typeBindings.filter((row) => row.interfaces.some((iface) => iface.id === selectedInterfaceId));
  });
  const availableTypes = $derived.by(() =>
    typeBindings.filter((row) => !row.interfaces.some((iface) => iface.id === selectedInterfaceId))
  );
  const interfaceStats = $derived.by<InterfaceStats>(() => ({
    propertyCount: properties.length,
    implementationCount: implementationRows.length,
    requiredPropertyCount: properties.filter((property) => property.required).length,
    timeDependentCount: properties.filter((property) => property.time_dependent).length
  }));

  function prettyJson(value: unknown) {
    return JSON.stringify(value ?? null, null, 2);
  }

  function parseLooseJson(source: string, label: string): unknown {
    try {
      return JSON.parse(source);
    } catch (error) {
      throw new Error(`${label}: ${error instanceof Error ? error.message : 'Invalid JSON'}`);
    }
  }

  function resetInterfaceDraft() {
    interfaceName = '';
    interfaceDisplayName = '';
    interfaceDescription = '';
    selectedInterfaceId = '';
    properties = [];
    saveError = '';
    saveSuccess = '';
    propertyError = '';
    propertySuccess = '';
  }

  function syncInterfaceDraft(iface: OntologyInterface | null) {
    if (!iface) {
      resetInterfaceDraft();
      return;
    }

    interfaceName = iface.name;
    interfaceDisplayName = iface.display_name;
    interfaceDescription = iface.description;
    saveError = '';
    saveSuccess = '';
  }

  function resetPropertyDraft() {
    propertyName = '';
    propertyDisplayName = '';
    propertyDescription = '';
    propertyType = 'string';
    propertyRequired = false;
    propertyUnique = false;
    propertyTimeDependent = false;
    propertyDefaultValueText = 'null';
    propertyValidationRulesText = '{}';
  }

  async function loadBindingMatrix() {
    bindingLoading = true;
    try {
      const rows = await Promise.all(
        objectTypes.map(async (objectType) => ({
          objectType,
          interfaces: await listTypeInterfaces(objectType.id).catch(() => [])
        }))
      );
      typeBindings = rows;
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to load interface implementations';
      typeBindings = [];
    } finally {
      bindingLoading = false;
    }
  }

  async function loadInterfaceContext(interfaceId: string) {
    if (!interfaceId) {
      properties = [];
      return;
    }

    contextLoading = true;
    pageError = '';

    try {
      const propertyResponse = await listInterfaceProperties(interfaceId);
      properties = propertyResponse;
      syncInterfaceDraft(interfaces.find((item) => item.id === interfaceId) ?? null);
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to load interface definition';
      properties = [];
    } finally {
      contextLoading = false;
    }
  }

  async function loadPage() {
    loading = true;
    pageError = '';

    try {
      const [interfaceResponse, typeResponse] = await Promise.all([
        listInterfaces({ page: 1, per_page: 200 }),
        listObjectTypes({ per_page: 200 })
      ]);

      interfaces = interfaceResponse.data;
      objectTypes = typeResponse.data;
      selectedInterfaceId = selectedInterfaceId || interfaces[0]?.id || '';

      await Promise.all([loadInterfaceContext(selectedInterfaceId), loadBindingMatrix()]);
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to load Interfaces';
    } finally {
      loading = false;
    }
  }

  async function selectInterface(interfaceId: string) {
    selectedInterfaceId = interfaceId;
    await loadInterfaceContext(interfaceId);
  }

  async function saveInterface() {
    saveError = '';
    saveSuccess = '';
    savingInterface = true;

    try {
      if (selectedInterfaceId) {
        await updateInterface(selectedInterfaceId, {
          display_name: interfaceDisplayName.trim() || undefined,
          description: interfaceDescription.trim() || undefined
        });
        saveSuccess = 'Interface updated.';
      } else {
        if (!interfaceName.trim()) {
          throw new Error('Interface name is required.');
        }
        await createInterface({
          name: interfaceName.trim(),
          display_name: interfaceDisplayName.trim() || undefined,
          description: interfaceDescription.trim() || undefined
        });
        saveSuccess = 'Interface created.';
      }

      const response = await listInterfaces({ page: 1, per_page: 200 });
      interfaces = response.data;
      const matched =
        interfaces.find((item) => item.name === interfaceName.trim()) ??
        interfaces.find((item) => item.id === selectedInterfaceId) ??
        interfaces[0] ??
        null;
      selectedInterfaceId = matched?.id ?? '';
      await loadInterfaceContext(selectedInterfaceId);
      await loadBindingMatrix();
    } catch (error) {
      saveError = error instanceof Error ? error.message : 'Failed to save interface';
    } finally {
      savingInterface = false;
    }
  }

  async function removeInterface() {
    if (!selectedInterfaceId) return;
    if (typeof window !== 'undefined' && !window.confirm('Delete this interface?')) return;

    deletingInterface = true;
    saveError = '';
    saveSuccess = '';

    try {
      await deleteInterface(selectedInterfaceId);
      saveSuccess = 'Interface deleted.';
      const response = await listInterfaces({ page: 1, per_page: 200 });
      interfaces = response.data;
      selectedInterfaceId = interfaces[0]?.id ?? '';
      await loadInterfaceContext(selectedInterfaceId);
      await loadBindingMatrix();
    } catch (error) {
      saveError = error instanceof Error ? error.message : 'Failed to delete interface';
    } finally {
      deletingInterface = false;
    }
  }

  async function createPropertyRecord() {
    if (!selectedInterfaceId) return;

    propertyError = '';
    propertySuccess = '';
    savingProperty = true;

    try {
      if (!propertyName.trim()) {
        throw new Error('Property name is required.');
      }

      await createInterfaceProperty(selectedInterfaceId, {
        name: propertyName.trim(),
        display_name: propertyDisplayName.trim() || undefined,
        description: propertyDescription.trim() || undefined,
        property_type: propertyType,
        required: propertyRequired,
        unique_constraint: propertyUnique,
        time_dependent: propertyTimeDependent,
        default_value: parseLooseJson(propertyDefaultValueText, 'Default value JSON'),
        validation_rules: parseLooseJson(propertyValidationRulesText, 'Validation rules JSON')
      });

      propertySuccess = 'Interface property created.';
      resetPropertyDraft();
      await loadInterfaceContext(selectedInterfaceId);
    } catch (error) {
      propertyError = error instanceof Error ? error.message : 'Failed to create interface property';
    } finally {
      savingProperty = false;
    }
  }

  async function toggleRequired(property: InterfaceProperty) {
    if (!selectedInterfaceId) return;
    try {
      await updateInterfaceProperty(selectedInterfaceId, property.id, {
        required: !property.required
      });
      await loadInterfaceContext(selectedInterfaceId);
    } catch (error) {
      propertyError = error instanceof Error ? error.message : 'Failed to update interface property';
    }
  }

  async function toggleTimeDependent(property: InterfaceProperty) {
    if (!selectedInterfaceId) return;
    try {
      await updateInterfaceProperty(selectedInterfaceId, property.id, {
        time_dependent: !property.time_dependent
      });
      await loadInterfaceContext(selectedInterfaceId);
    } catch (error) {
      propertyError = error instanceof Error ? error.message : 'Failed to update interface property';
    }
  }

  async function removeProperty(propertyId: string) {
    if (!selectedInterfaceId) return;

    try {
      await deleteInterfaceProperty(selectedInterfaceId, propertyId);
      propertySuccess = 'Interface property deleted.';
      await loadInterfaceContext(selectedInterfaceId);
    } catch (error) {
      propertyError = error instanceof Error ? error.message : 'Failed to delete interface property';
    }
  }

  async function attachToType(typeId: string) {
    if (!selectedInterfaceId) return;
    bindingTypeId = typeId;

    try {
      await attachInterfaceToType(typeId, selectedInterfaceId);
      await loadBindingMatrix();
      propertySuccess = 'Interface attached to object type.';
    } catch (error) {
      propertyError = error instanceof Error ? error.message : 'Failed to attach interface';
    } finally {
      bindingTypeId = '';
    }
  }

  async function detachFromType(typeId: string) {
    if (!selectedInterfaceId) return;
    bindingTypeId = typeId;

    try {
      await detachInterfaceFromType(typeId, selectedInterfaceId);
      await loadBindingMatrix();
      propertySuccess = 'Interface detached from object type.';
    } catch (error) {
      propertyError = error instanceof Error ? error.message : 'Failed to detach interface';
    } finally {
      bindingTypeId = '';
    }
  }

  onMount(() => {
    void loadPage();
  });
</script>

<svelte:head>
  <title>OpenFoundry - Interfaces</title>
</svelte:head>

<div class="mx-auto flex max-w-7xl flex-col gap-6 px-4 py-6">
  <section class="overflow-hidden rounded-[2rem] border border-slate-200 bg-[radial-gradient(circle_at_top_left,_rgba(34,197,94,0.18),_transparent_35%),linear-gradient(135deg,_#f8fafc_0%,_#eef8f0_45%,_#f8fafc_100%)] p-6 shadow-sm">
    <div class="grid gap-6 lg:grid-cols-[1.45fr_1fr]">
      <div class="space-y-4">
        <div class="inline-flex items-center gap-2 rounded-full border border-emerald-200 bg-white/80 px-3 py-1 text-xs font-semibold uppercase tracking-[0.24em] text-emerald-700">
          <Glyph name="link" size={14} />
          Define Ontologies / Interfaces
        </div>
        <div class="space-y-3">
          <h1 class="text-3xl font-semibold tracking-tight text-slate-950">Interfaces</h1>
          <p class="max-w-3xl text-sm leading-6 text-slate-600">
            Build reusable ontology contracts with interface definitions, canonical properties, and implementation bindings across object types from one dedicated product surface.
          </p>
        </div>
        <div class="flex flex-wrap gap-3 text-xs text-slate-500">
          <a href="/ontology-manager" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-emerald-300 hover:text-emerald-700">Ontology Manager</a>
          <a href="/object-views" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-emerald-300 hover:text-emerald-700">Object Views</a>
          <a href="/action-types" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-emerald-300 hover:text-emerald-700">Action Types</a>
        </div>
      </div>

      <div class="grid gap-3 sm:grid-cols-2">
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Interfaces</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{interfaces.length}</p>
          <p class="mt-1 text-sm text-slate-500">Reusable schema contracts in the ontology.</p>
        </div>
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Object types</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{objectTypes.length}</p>
          <p class="mt-1 text-sm text-slate-500">Candidate implementations across your ontology.</p>
        </div>
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Selected properties</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{interfaceStats.propertyCount}</p>
          <p class="mt-1 text-sm text-slate-500">Interface-level fields and validation requirements.</p>
        </div>
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Implementations</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{interfaceStats.implementationCount}</p>
          <p class="mt-1 text-sm text-slate-500">Object types currently bound to the selected interface.</p>
        </div>
      </div>
    </div>
  </section>

  {#if pageError}
    <div class="rounded-3xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{pageError}</div>
  {/if}

  {#if loading}
    <div class="rounded-3xl border border-slate-200 bg-white px-5 py-10 text-center text-sm text-slate-500">
      Loading interfaces...
    </div>
  {:else}
    <div class="grid gap-6 xl:grid-cols-[330px_minmax(0,1fr)]">
      <aside class="space-y-4">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="flex items-center justify-between gap-3">
            <div>
              <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Library</p>
              <h2 class="mt-1 text-lg font-semibold text-slate-950">Interface catalog</h2>
            </div>
            <button
              class="inline-flex items-center gap-2 rounded-full bg-emerald-600 px-4 py-2 text-sm font-medium text-white hover:bg-emerald-500"
              onclick={resetInterfaceDraft}
            >
              <Glyph name="plus" size={16} />
              New
            </button>
          </div>

          <div class="mt-4 space-y-3">
            <label for="interfaces-search" class="block text-xs font-semibold uppercase tracking-[0.18em] text-slate-400">
              Search
            </label>
            <input
              id="interfaces-search"
              class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
              type="text"
              bind:value={catalogQuery}
              placeholder="Search interface name, display name, or description"
            />
          </div>

          <div class="mt-4 space-y-2">
            {#if filteredInterfaces.length === 0}
              <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-6 text-center text-sm text-slate-500">
                No interfaces match this filter.
              </div>
            {:else}
              {#each filteredInterfaces as iface}
                <button
                  class={`w-full rounded-2xl border px-4 py-3 text-left transition ${
                    selectedInterfaceId === iface.id
                      ? 'border-emerald-400 bg-emerald-50'
                      : 'border-slate-200 bg-white hover:border-slate-300'
                  }`}
                  onclick={() => void selectInterface(iface.id)}
                >
                  <p class="text-sm font-semibold text-slate-950">{iface.display_name}</p>
                  <p class="mt-1 text-xs font-mono text-slate-500">{iface.name}</p>
                  <p class="mt-2 line-clamp-2 text-sm text-slate-600">{iface.description || 'No description provided yet.'}</p>
                </button>
              {/each}
            {/if}
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Coverage</p>
          <div class="mt-4 space-y-3">
            <div class="rounded-2xl border border-slate-200 px-4 py-3">
              <p class="text-sm font-semibold text-slate-900">Required properties</p>
              <p class="mt-2 text-2xl font-semibold text-slate-950">{interfaceStats.requiredPropertyCount}</p>
            </div>
            <div class="rounded-2xl border border-slate-200 px-4 py-3">
              <p class="text-sm font-semibold text-slate-900">Time-dependent fields</p>
              <p class="mt-2 text-2xl font-semibold text-slate-950">{interfaceStats.timeDependentCount}</p>
            </div>
          </div>
        </section>
      </aside>

      <main class="space-y-4">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-4 shadow-sm">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div>
              <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Workbench</p>
              <h2 class="mt-1 text-xl font-semibold text-slate-950">
                {selectedInterface ? selectedInterface.display_name : 'New interface'}
              </h2>
            </div>
            <div class="flex flex-wrap gap-2">
              {#each workbenchTabs as tab}
                <button
                  class={`inline-flex items-center gap-2 rounded-full px-4 py-2 text-sm font-medium transition ${
                    activeTab === tab.id
                      ? 'bg-slate-950 text-white'
                      : 'border border-slate-200 bg-white text-slate-600 hover:border-slate-300'
                  }`}
                  onclick={() => activeTab = tab.id}
                >
                  <Glyph name={tab.glyph} size={16} />
                  {tab.label}
                </button>
              {/each}
            </div>
          </div>
        </section>

        {#if saveError}
          <div class="rounded-3xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{saveError}</div>
        {/if}
        {#if saveSuccess}
          <div class="rounded-3xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700">{saveSuccess}</div>
        {/if}
        {#if propertyError}
          <div class="rounded-3xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{propertyError}</div>
        {/if}
        {#if propertySuccess}
          <div class="rounded-3xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700">{propertySuccess}</div>
        {/if}

        {#if activeTab === 'library'}
          <div class="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_340px]">
            <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
              <div class="grid gap-4 md:grid-cols-2">
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Interface name</span>
                  <input
                    class="w-full rounded-2xl border border-slate-300 px-4 py-3 font-mono text-sm outline-none transition focus:border-emerald-500 disabled:bg-slate-100"
                    type="text"
                    bind:value={interfaceName}
                    disabled={Boolean(selectedInterfaceId)}
                    placeholder="case_contract"
                  />
                </label>
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Display name</span>
                  <input
                    class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
                    type="text"
                    bind:value={interfaceDisplayName}
                    placeholder="Case contract"
                  />
                </label>
              </div>

              <label class="mt-4 block space-y-2 text-sm text-slate-700">
                <span class="font-medium">Description</span>
                <textarea
                  rows="4"
                  class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
                  bind:value={interfaceDescription}
                  placeholder="Describe the shared semantic contract this interface should enforce across object types."
                ></textarea>
              </label>

              <div class="mt-4 flex flex-wrap gap-3">
                <button
                  class="rounded-full bg-emerald-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-emerald-500 disabled:cursor-not-allowed disabled:bg-emerald-300"
                  onclick={() => void saveInterface()}
                  disabled={savingInterface}
                >
                  {savingInterface ? 'Saving...' : selectedInterfaceId ? 'Save changes' : 'Create interface'}
                </button>
                {#if selectedInterfaceId}
                  <button
                    class="rounded-full border border-rose-200 bg-rose-50 px-5 py-2.5 text-sm font-medium text-rose-700 hover:border-rose-300 disabled:cursor-not-allowed disabled:opacity-60"
                    onclick={() => void removeInterface()}
                    disabled={deletingInterface}
                  >
                    {deletingInterface ? 'Deleting...' : 'Delete interface'}
                  </button>
                {/if}
              </div>
            </section>

            <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
              <p class="text-sm font-semibold text-slate-900">Overview</p>
              <div class="mt-4 space-y-3 text-sm leading-6 text-slate-600">
                <p>Interfaces define reusable contracts that several object types can implement without duplicating the conceptual schema.</p>
                <p>Use the definition tab to curate the canonical property set, then attach the interface to object types from the implementation tab.</p>
                <p>The backend stores interface resources independently from object types, so implementations can evolve while keeping the shared contract visible.</p>
              </div>
            </section>
          </div>
        {:else if activeTab === 'definition'}
          <div class="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_380px]">
            <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
              {#if contextLoading}
                <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-8 text-center text-sm text-slate-500">
                  Loading interface definition...
                </div>
              {:else if !selectedInterfaceId}
                <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-8 text-center text-sm text-slate-500">
                  Create or select an interface first.
                </div>
              {:else}
                <div class="flex items-start justify-between gap-4">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">Interface properties</p>
                    <p class="mt-1 text-sm text-slate-500">Canonical fields that implementations should expose consistently.</p>
                  </div>
                  <span class="rounded-full bg-slate-100 px-3 py-1 text-xs font-medium text-slate-600">
                    {properties.length} properties
                  </span>
                </div>

                <div class="mt-4 space-y-3">
                  {#if properties.length === 0}
                    <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-8 text-center text-sm text-slate-500">
                      No properties defined yet for this interface.
                    </div>
                  {:else}
                    {#each properties as property}
                      <div class="rounded-2xl border border-slate-200 p-4">
                        <div class="flex flex-wrap items-start justify-between gap-3">
                          <div>
                            <p class="text-sm font-semibold text-slate-900">{property.display_name}</p>
                            <p class="mt-1 text-xs font-mono text-slate-500">{property.name}</p>
                          </div>
                          <div class="flex flex-wrap gap-2 text-[11px] text-slate-500">
                            <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1">{property.property_type}</span>
                            {#if property.required}
                              <span class="rounded-full border border-amber-200 bg-amber-50 px-2 py-1 text-amber-700">Required</span>
                            {/if}
                            {#if property.time_dependent}
                              <span class="rounded-full border border-sky-200 bg-sky-50 px-2 py-1 text-sky-700">Time dependent</span>
                            {/if}
                          </div>
                        </div>
                        {#if property.description}
                          <p class="mt-3 text-sm text-slate-500">{property.description}</p>
                        {/if}
                        <div class="mt-4 flex flex-wrap gap-2">
                          <button
                            class="rounded-full border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:border-emerald-400 hover:text-emerald-700"
                            onclick={() => void toggleRequired(property)}
                          >
                            {property.required ? 'Mark optional' : 'Mark required'}
                          </button>
                          <button
                            class="rounded-full border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:border-emerald-400 hover:text-emerald-700"
                            onclick={() => void toggleTimeDependent(property)}
                          >
                            {property.time_dependent ? 'Disable time dependence' : 'Enable time dependence'}
                          </button>
                          <button
                            class="rounded-full border border-rose-200 bg-rose-50 px-3 py-1.5 text-xs font-medium text-rose-700 hover:border-rose-300"
                            onclick={() => void removeProperty(property.id)}
                          >
                            Delete
                          </button>
                        </div>
                      </div>
                    {/each}
                  {/if}
                </div>
              {/if}
            </section>

            <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
              <p class="text-sm font-semibold text-slate-900">Create interface property</p>
              <div class="mt-4 space-y-4">
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Property name</span>
                  <input
                    class="w-full rounded-2xl border border-slate-300 px-4 py-3 font-mono text-sm outline-none transition focus:border-emerald-500"
                    type="text"
                    bind:value={propertyName}
                    placeholder="status"
                  />
                </label>
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Display name</span>
                  <input
                    class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
                    type="text"
                    bind:value={propertyDisplayName}
                    placeholder="Status"
                  />
                </label>
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Description</span>
                  <input
                    class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
                    type="text"
                    bind:value={propertyDescription}
                    placeholder="Canonical state field shared across all implementations"
                  />
                </label>
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Property type</span>
                  <select
                    class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
                    bind:value={propertyType}
                  >
                    {#each propertyTypeOptions as option}
                      <option value={option}>{option}</option>
                    {/each}
                  </select>
                </label>

                <div class="grid gap-3 sm:grid-cols-3">
                  <label class="flex items-center gap-2 rounded-2xl border border-slate-200 px-4 py-3 text-sm text-slate-700">
                    <input type="checkbox" checked={propertyRequired} onchange={(event) => propertyRequired = (event.currentTarget as HTMLInputElement).checked} />
                    Required
                  </label>
                  <label class="flex items-center gap-2 rounded-2xl border border-slate-200 px-4 py-3 text-sm text-slate-700">
                    <input type="checkbox" checked={propertyUnique} onchange={(event) => propertyUnique = (event.currentTarget as HTMLInputElement).checked} />
                    Unique
                  </label>
                  <label class="flex items-center gap-2 rounded-2xl border border-slate-200 px-4 py-3 text-sm text-slate-700">
                    <input type="checkbox" checked={propertyTimeDependent} onchange={(event) => propertyTimeDependent = (event.currentTarget as HTMLInputElement).checked} />
                    Time dependent
                  </label>
                </div>

                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Default value JSON</span>
                  <textarea
                    rows="4"
                    class="w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 outline-none transition focus:border-emerald-500"
                    bind:value={propertyDefaultValueText}
                    spellcheck="false"
                  ></textarea>
                </label>
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Validation rules JSON</span>
                  <textarea
                    rows="4"
                    class="w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 outline-none transition focus:border-emerald-500"
                    bind:value={propertyValidationRulesText}
                    spellcheck="false"
                  ></textarea>
                </label>
                <button
                  class="rounded-full bg-emerald-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-emerald-500 disabled:cursor-not-allowed disabled:bg-emerald-300"
                  onclick={() => void createPropertyRecord()}
                  disabled={!selectedInterfaceId || savingProperty}
                >
                  {savingProperty ? 'Creating...' : 'Create property'}
                </button>
              </div>
            </section>
          </div>
        {:else if activeTab === 'implementation'}
          <div class="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_380px]">
            <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
              <div class="flex items-center justify-between gap-3">
                <div>
                  <p class="text-sm font-semibold text-slate-900">Implementing object types</p>
                  <p class="mt-1 text-sm text-slate-500">Bind the selected interface to object types that should expose this contract.</p>
                </div>
                {#if bindingLoading}
                  <span class="text-xs text-slate-500">Refreshing...</span>
                {/if}
              </div>

              <div class="mt-4 space-y-3">
                {#if !selectedInterfaceId}
                  <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-8 text-center text-sm text-slate-500">
                    Select an interface to manage implementations.
                  </div>
                {:else if implementationRows.length === 0}
                  <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-8 text-center text-sm text-slate-500">
                    No object types implement this interface yet.
                  </div>
                {:else}
                  {#each implementationRows as row}
                    <div class="rounded-2xl border border-slate-200 p-4">
                      <div class="flex items-start justify-between gap-3">
                        <div>
                          <p class="text-sm font-semibold text-slate-900">{row.objectType.display_name}</p>
                          <p class="mt-1 text-xs font-mono text-slate-500">{row.objectType.name}</p>
                        </div>
                        <button
                          class="rounded-full border border-rose-200 bg-rose-50 px-3 py-1.5 text-xs font-medium text-rose-700 hover:border-rose-300 disabled:cursor-not-allowed disabled:opacity-60"
                          onclick={() => void detachFromType(row.objectType.id)}
                          disabled={bindingTypeId === row.objectType.id}
                        >
                          {bindingTypeId === row.objectType.id ? 'Updating...' : 'Detach'}
                        </button>
                      </div>
                      <p class="mt-3 text-sm text-slate-500">{row.objectType.description || 'No object type description provided.'}</p>
                    </div>
                  {/each}
                {/if}
              </div>
            </section>

            <section class="space-y-4">
              <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                <p class="text-sm font-semibold text-slate-900">Available object types</p>
                <div class="mt-4 space-y-3">
                  {#if availableTypes.length === 0}
                    <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-8 text-center text-sm text-slate-500">
                      Every loaded object type already implements this interface.
                    </div>
                  {:else}
                    {#each availableTypes as row}
                      <div class="rounded-2xl border border-slate-200 p-4">
                        <div class="flex items-start justify-between gap-3">
                          <div>
                            <p class="text-sm font-semibold text-slate-900">{row.objectType.display_name}</p>
                            <p class="mt-1 text-xs font-mono text-slate-500">{row.objectType.name}</p>
                          </div>
                          <button
                            class="rounded-full border border-emerald-300 bg-emerald-50 px-3 py-1.5 text-xs font-medium text-emerald-700 hover:border-emerald-400 disabled:cursor-not-allowed disabled:opacity-60"
                            onclick={() => void attachToType(row.objectType.id)}
                            disabled={!selectedInterfaceId || bindingTypeId === row.objectType.id}
                          >
                            {bindingTypeId === row.objectType.id ? 'Updating...' : 'Attach'}
                          </button>
                        </div>
                        <p class="mt-3 text-sm text-slate-500">{row.objectType.description || 'No object type description provided.'}</p>
                      </div>
                    {/each}
                  {/if}
                </div>
              </div>
            </section>
          </div>
        {:else}
          <div class="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_360px]">
            <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
              <p class="text-sm font-semibold text-slate-900">Metadata reference</p>
              <div class="mt-4 grid gap-3 md:grid-cols-2">
                <div class="rounded-2xl border border-slate-200 p-4">
                  <p class="text-sm font-semibold text-slate-900">Interface resource</p>
                  <p class="mt-2 text-sm text-slate-500">Each interface has a stable `name`, a user-facing `display_name`, descriptive metadata, owner, and timestamps.</p>
                </div>
                <div class="rounded-2xl border border-slate-200 p-4">
                  <p class="text-sm font-semibold text-slate-900">Interface properties</p>
                  <p class="mt-2 text-sm text-slate-500">Properties track `property_type`, requiredness, uniqueness, time dependence, default values, and validation rules.</p>
                </div>
                <div class="rounded-2xl border border-slate-200 p-4">
                  <p class="text-sm font-semibold text-slate-900">Implementation binding</p>
                  <p class="mt-2 text-sm text-slate-500">Bindings connect an object type to an interface so the contract can be reused across schema definitions.</p>
                </div>
                <div class="rounded-2xl border border-slate-200 p-4">
                  <p class="text-sm font-semibold text-slate-900">Edit model</p>
                  <p class="mt-2 text-sm text-slate-500">Definition and implementation changes are governed independently, which keeps shared contracts reusable without duplicating object-type schemas.</p>
                </div>
              </div>
            </section>

            <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
              <p class="text-sm font-semibold text-slate-900">Notes</p>
              <div class="mt-4 space-y-3 text-sm leading-6 text-slate-600">
                <p>Use interfaces when several object types should expose a shared semantic shape and implementation contract.</p>
                <p>Keep high-signal fields required here, then attach the interface to every object type that should participate in the contract.</p>
                <p>Follow with Object Views, Action Types, and Functions once the interface is stable enough to power reusable application behavior.</p>
              </div>
            </section>
          </div>
        {/if}
      </main>
    </div>
  {/if}
</div>
