<script lang="ts">
  import { browser } from '$app/environment';
  import { onMount } from 'svelte';
  import Glyph from '$components/ui/Glyph.svelte';
  import {
    getObjectView,
    listActionTypes,
    listObjectTypes,
    listObjects,
    listProperties,
    type ActionType,
    type ObjectInstance,
    type ObjectType,
    type ObjectViewResponse,
    type Property
  } from '$lib/api/ontology';

  type ViewMode = 'standard' | 'configured';
  type FormFactor = 'full' | 'panel';
  type EditorTab = 'editor' | 'versions' | 'publish';
  type SectionKind = 'summary' | 'properties' | 'links' | 'timeline' | 'actions' | 'graph' | 'comments' | 'apps';

  interface ObjectViewSection {
    id: string;
    title: string;
    kind: SectionKind;
    description: string;
  }

  interface ObjectViewSidebarLink {
    id: string;
    label: string;
    href: string;
    glyph: 'artifact' | 'graph' | 'search' | 'run' | 'bookmark' | 'link';
  }

  interface ConfiguredObjectView {
    mode: 'configured';
    form_factor: FormFactor;
    title_template: string;
    subtitle_property: string;
    prominent_properties: string[];
    panel_properties: string[];
    sections: ObjectViewSection[];
    sidebar_links: ObjectViewSidebarLink[];
    comments_enabled: boolean;
    branch_label: string;
    auto_publish: boolean;
  }

  interface ObjectViewVersion {
    id: string;
    object_type_id: string;
    form_factor: FormFactor;
    description: string;
    created_at: string;
    created_by: string;
    published: boolean;
    branch_label: string;
    config: ConfiguredObjectView;
  }

  type StoredViewState = {
    full: ObjectViewVersion[];
    panel: ObjectViewVersion[];
  };

  const editorTabs: { id: EditorTab; label: string }[] = [
    { id: 'editor', label: 'Editor' },
    { id: 'versions', label: 'Versions' },
    { id: 'publish', label: 'Publish' }
  ];

  const sectionKinds: { id: SectionKind; label: string; description: string }[] = [
    { id: 'summary', label: 'Summary', description: 'Hero metrics and prominent object properties.' },
    { id: 'properties', label: 'Properties', description: 'Object schema and canonical fields.' },
    { id: 'links', label: 'Linked objects', description: 'Traverse related entities and previews.' },
    { id: 'timeline', label: 'Timeline', description: 'Recent activity, comments, and runtime events.' },
    { id: 'actions', label: 'Actions', description: 'Applicable actions and remediation paths.' },
    { id: 'graph', label: 'Graph', description: 'Neighborhood and graph context.' },
    { id: 'comments', label: 'Comments', description: 'Notes, handoff, and collaboration slot.' },
    { id: 'apps', label: 'Applications', description: 'Related Quiver, Map, Rules, or workflow links.' }
  ];

  const sidebarPresets: ObjectViewSidebarLink[] = [
    { id: 'quiver', label: 'Quiver', href: '/quiver', glyph: 'artifact' },
    { id: 'graph', label: 'Graph', href: '/ontology/graph', glyph: 'graph' },
    { id: 'explorer', label: 'Object Explorer', href: '/object-explorer', glyph: 'search' },
    { id: 'rules', label: 'Foundry Rules', href: '/foundry-rules', glyph: 'run' },
    { id: 'set', label: 'Saved lists', href: '/ontology/object-sets', glyph: 'bookmark' }
  ];

  let objectTypes = $state<ObjectType[]>([]);
  let properties = $state<Property[]>([]);
  let actions = $state<ActionType[]>([]);
  let objects = $state<ObjectInstance[]>([]);
  let loading = $state(true);
  let previewLoading = $state(false);
  let error = $state('');

  let selectedTypeId = $state('');
  let selectedObjectId = $state('');
  let activeMode = $state<ViewMode>('configured');
  let activeFormFactor = $state<FormFactor>('full');
  let activeEditorTab = $state<EditorTab>('editor');
  let versionDescription = $state('');

  let preview = $state<ObjectViewResponse | null>(null);
  let config = $state<ConfiguredObjectView>(createDefaultConfiguredView('full'));
  let versions = $state<StoredViewState>({ full: [], panel: [] });

  const selectedType = $derived(objectTypes.find((item) => item.id === selectedTypeId) ?? null);
  const selectedObject = $derived(objects.find((item) => item.id === selectedObjectId) ?? null);

  const availableVersions = $derived(activeFormFactor === 'full' ? versions.full : versions.panel);
  const publishedVersion = $derived(availableVersions.find((item) => item.published) ?? null);

  const selectedSummaryEntries = $derived.by(() => {
    if (!preview) return [];
    return Object.entries(preview.summary)
      .filter(([key]) =>
        activeMode === 'standard'
          ? true
          : (activeFormFactor === 'full' ? config.prominent_properties : config.panel_properties).includes(key)
      )
      .slice(0, activeFormFactor === 'full' ? 8 : 4);
  });

  const resolvedSections = $derived.by(() => {
    if (activeMode === 'standard') {
      return activeFormFactor === 'full'
        ? ['summary', 'properties', 'links', 'timeline', 'actions', 'graph']
        : ['summary', 'properties', 'links'];
    }
    return config.sections.map((section) => section.kind);
  });

  const generatedFullUrl = $derived.by(() =>
    selectedTypeId && selectedObjectId ? `/object-views?type=${selectedTypeId}&object=${selectedObjectId}&mode=configured&factor=full` : ''
  );

  const generatedPanelUrl = $derived.by(() =>
    selectedTypeId && selectedObjectId ? `/object-views?type=${selectedTypeId}&object=${selectedObjectId}&mode=configured&factor=panel` : ''
  );

  function createDefaultConfiguredView(formFactor: FormFactor): ConfiguredObjectView {
    return {
      mode: 'configured',
      form_factor: formFactor,
      title_template: '{{name}}',
      subtitle_property: '',
      prominent_properties: [],
      panel_properties: [],
      sections:
        formFactor === 'full'
          ? [
              { id: crypto.randomUUID(), title: 'Overview', kind: 'summary', description: 'Core identity and metrics.' },
              { id: crypto.randomUUID(), title: 'Properties', kind: 'properties', description: 'Canonical schema fields.' },
              { id: crypto.randomUUID(), title: 'Linked Objects', kind: 'links', description: 'Traverse the object neighborhood.' },
              { id: crypto.randomUUID(), title: 'Activity', kind: 'timeline', description: 'Recent events and discussions.' },
              { id: crypto.randomUUID(), title: 'Actions', kind: 'actions', description: 'Applicable actions and controls.' }
            ]
          : [
              { id: crypto.randomUUID(), title: 'Panel Summary', kind: 'summary', description: 'Critical data for in-context panel workflows.' },
              { id: crypto.randomUUID(), title: 'Linked Objects', kind: 'links', description: 'Compact related-entity preview.' }
            ],
      sidebar_links: [...sidebarPresets.slice(0, formFactor === 'full' ? 4 : 2)],
      comments_enabled: true,
      branch_label: 'main',
      auto_publish: true
    };
  }

  function storageKey(typeId: string) {
    return `of.objectViews.${typeId}`;
  }

  function loadStoredState(typeId: string): StoredViewState {
    if (!browser) return { full: [], panel: [] };
    try {
      const parsed = JSON.parse(window.localStorage.getItem(storageKey(typeId)) ?? '{"full":[],"panel":[]}');
      return {
        full: Array.isArray(parsed.full) ? parsed.full : [],
        panel: Array.isArray(parsed.panel) ? parsed.panel : []
      };
    } catch {
      return { full: [], panel: [] };
    }
  }

  function persistStoredState(typeId: string, state: StoredViewState) {
    if (!browser) return;
    window.localStorage.setItem(storageKey(typeId), JSON.stringify(state));
  }

  function normalizeValue(value: unknown) {
    if (value === null || value === undefined) return '—';
    if (Array.isArray(value)) return value.length ? value.join(', ') : '—';
    if (typeof value === 'object') return JSON.stringify(value);
    return String(value);
  }

  function objectLabel(instance: ObjectInstance) {
    const props = instance.properties ?? {};
    const candidates = ['name', 'title', 'display_name', 'label', 'code', 'identifier'];
    for (const key of candidates) {
      const value = props[key];
      if (typeof value === 'string' && value.trim()) return value;
    }
    return instance.id;
  }

  function resolveSubtitle() {
    if (!selectedObject) return selectedType?.display_name ?? '';
    if (!config.subtitle_property) return selectedType?.display_name ?? '';
    return normalizeValue(selectedObject.properties[config.subtitle_property]);
  }

  function resolveTitle() {
    if (!selectedObject) return selectedType?.display_name ?? 'Object';
    return config.title_template.replaceAll('{{name}}', objectLabel(selectedObject)).replaceAll('{{id}}', selectedObject.id);
  }

  async function loadCatalog() {
    loading = true;
    error = '';
    try {
      const typeResponse = await listObjectTypes({ per_page: 200 });
      objectTypes = typeResponse.data;
      if (!selectedTypeId && objectTypes[0]) {
        selectedTypeId = objectTypes[0].id;
      }
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to load Object Views';
    } finally {
      loading = false;
    }
  }

  async function loadTypeContext(typeId: string) {
    try {
      const [propertyResponse, objectResponse, actionResponse] = await Promise.all([
        listProperties(typeId),
        listObjects(typeId, { per_page: 50 }),
        listActionTypes({ object_type_id: typeId, per_page: 100 })
      ]);

      properties = propertyResponse;
      objects = objectResponse.data;
      actions = actionResponse.data;

      if (!selectedObjectId && objects[0]) {
        selectedObjectId = objects[0].id;
      } else if (!objects.some((item) => item.id === selectedObjectId)) {
        selectedObjectId = objects[0]?.id ?? '';
      }

      versions = loadStoredState(typeId);
      const currentVersions = activeFormFactor === 'full' ? versions.full : versions.panel;
      const currentPublished = currentVersions.find((item) => item.published);
      config = currentPublished?.config ?? createAutoConfiguredView(typeId, activeFormFactor);
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to load object type context';
      properties = [];
      objects = [];
      actions = [];
    }
  }

  function createAutoConfiguredView(typeId: string, formFactor: FormFactor) {
    const base = createDefaultConfiguredView(formFactor);
    const prominent = properties.slice(0, formFactor === 'full' ? 6 : 3).map((item) => item.name);
    return {
      ...base,
      prominent_properties: prominent,
      panel_properties: properties.slice(0, 3).map((item) => item.name),
      subtitle_property: properties[1]?.name ?? properties[0]?.name ?? '',
      branch_label: `main:${typeId.slice(0, 6)}`
    };
  }

  async function loadPreview() {
    if (!selectedTypeId || !selectedObjectId) {
      preview = null;
      return;
    }

    previewLoading = true;
    try {
      preview = await getObjectView(selectedTypeId, selectedObjectId);
    } catch (cause) {
      preview = null;
      error = cause instanceof Error ? cause.message : 'Failed to load Object View preview';
    } finally {
      previewLoading = false;
    }
  }

  function toggleProperty(target: 'prominent_properties' | 'panel_properties', name: string) {
    const current = config[target];
    config = {
      ...config,
      [target]: current.includes(name) ? current.filter((item) => item !== name) : [...current, name]
    };
  }

  function addSection(kind: SectionKind) {
    const template = sectionKinds.find((item) => item.id === kind);
    config = {
      ...config,
      sections: [
        ...config.sections,
        {
          id: crypto.randomUUID(),
          title: template?.label ?? kind,
          kind,
          description: template?.description ?? ''
        }
      ]
    };
  }

  function removeSection(id: string) {
    config = {
      ...config,
      sections: config.sections.filter((section) => section.id !== id)
    };
  }

  function addSidebarPreset(preset: ObjectViewSidebarLink) {
    if (config.sidebar_links.some((item) => item.id === preset.id)) return;
    config = {
      ...config,
      sidebar_links: [...config.sidebar_links, preset]
    };
  }

  function removeSidebarLink(id: string) {
    config = {
      ...config,
      sidebar_links: config.sidebar_links.filter((item) => item.id !== id)
    };
  }

  function saveVersion(publish = false) {
    if (!selectedTypeId) return;
    const nextVersion: ObjectViewVersion = {
      id: crypto.randomUUID(),
      object_type_id: selectedTypeId,
      form_factor: activeFormFactor,
      description: versionDescription.trim() || `Configured ${activeFormFactor} Object View`,
      created_at: new Date().toISOString(),
      created_by: 'OpenFoundry Builder',
      published: publish || config.auto_publish,
      branch_label: config.branch_label,
      config: { ...config, form_factor: activeFormFactor }
    };

    const nextState = structuredClone(versions);
    const bucket = activeFormFactor === 'full' ? nextState.full : nextState.panel;
    if (nextVersion.published) {
      for (const version of bucket) version.published = false;
    }
    bucket.unshift(nextVersion);
    versions = nextState;
    persistStoredState(selectedTypeId, nextState);
    versionDescription = '';
  }

  function publishVersion(versionId: string) {
    if (!selectedTypeId) return;
    const nextState = structuredClone(versions);
    const bucket = activeFormFactor === 'full' ? nextState.full : nextState.panel;
    for (const version of bucket) {
      version.published = version.id === versionId;
    }
    versions = nextState;
    persistStoredState(selectedTypeId, nextState);
    const published = bucket.find((item) => item.id === versionId);
    if (published) {
      config = published.config;
      activeMode = 'configured';
    }
  }

  function previewVersion(versionId: string) {
    const version = availableVersions.find((item) => item.id === versionId);
    if (!version) return;
    config = version.config;
    activeMode = 'configured';
  }

  async function copyUrl(url: string) {
    if (!browser || !url) return;
    await navigator.clipboard.writeText(url);
  }

  function hydrateFromUrl() {
    if (!browser) return;
    const params = new URLSearchParams(window.location.search);
    selectedTypeId = params.get('type') ?? selectedTypeId;
    selectedObjectId = params.get('object') ?? selectedObjectId;
    const mode = params.get('mode');
    const factor = params.get('factor');
    if (mode === 'standard' || mode === 'configured') activeMode = mode;
    if (factor === 'full' || factor === 'panel') activeFormFactor = factor;
  }

  function syncUrl() {
    if (!browser) return;
    const url = new URL(window.location.href);
    if (selectedTypeId) url.searchParams.set('type', selectedTypeId);
    if (selectedObjectId) url.searchParams.set('object', selectedObjectId);
    url.searchParams.set('mode', activeMode);
    url.searchParams.set('factor', activeFormFactor);
    window.history.replaceState({}, '', url);
  }

  $effect(() => {
    syncUrl();
  });

  $effect(() => {
    if (selectedTypeId) {
      void loadTypeContext(selectedTypeId);
    }
  });

  $effect(() => {
    if (selectedTypeId && selectedObjectId) {
      void loadPreview();
    }
  });

  $effect(() => {
    if (selectedTypeId) {
      const currentVersions = activeFormFactor === 'full' ? versions.full : versions.panel;
      const currentPublished = currentVersions.find((item) => item.published);
      config = currentPublished?.config ?? createAutoConfiguredView(selectedTypeId, activeFormFactor);
    }
  });

  onMount(async () => {
    hydrateFromUrl();
    await loadCatalog();
  });
</script>

<svelte:head>
  <title>OpenFoundry - Object Views</title>
</svelte:head>

<div class="space-y-5">
  <section class="overflow-hidden rounded-[30px] border border-[var(--border-default)] bg-[linear-gradient(135deg,#fbfcff_0%,#eef5ff_42%,#f3f8ed_100%)] shadow-[var(--shadow-panel)]">
    <div class="grid gap-6 px-6 py-6 lg:grid-cols-[minmax(0,1.35fr)_340px] lg:px-8">
      <div>
        <div class="of-eyebrow">Object Views</div>
        <h1 class="mt-3 max-w-4xl text-[34px] font-semibold tracking-[-0.03em] text-[var(--text-strong)]">
          Configure reusable full and panel Object Views, version them safely, and preview them on live objects.
        </h1>
        <p class="mt-4 max-w-3xl text-[15px] leading-7 text-[var(--text-muted)]">
          This dedicated Object Views surface separates `standard` and `configured` views, exposes both
          `full` and `panel` form factors, and adds version-aware editing so object representation
          becomes a first-class product rather than a side effect of the object inspector.
        </p>

        <div class="mt-6 flex flex-wrap gap-3">
          <a href="/object-explorer" class="of-btn">
            <Glyph name="search" size={16} />
            <span>Open Object Explorer</span>
          </a>
          <a href="/ontology" class="of-btn">
            <Glyph name="ontology" size={16} />
            <span>Ontology hub</span>
          </a>
          {#if selectedTypeId && selectedObjectId}
            <a href={`/ontology/${selectedTypeId}`} class="of-btn">
              <Glyph name="object" size={16} />
              <span>Open type workbench</span>
            </a>
          {/if}
        </div>
      </div>

      <aside class="rounded-[22px] border border-white/80 bg-white/82 p-5 backdrop-blur">
        <div class="of-heading-sm">View posture</div>
        <div class="mt-4 grid gap-3">
          <article class="rounded-[18px] border border-[var(--border-subtle)] bg-white px-4 py-3">
            <div class="text-[11px] font-semibold uppercase tracking-[0.18em] text-[var(--text-soft)]">Form factor</div>
            <div class="mt-1 text-2xl font-semibold capitalize text-[var(--text-strong)]">{activeFormFactor}</div>
          </article>
          <article class="rounded-[18px] border border-[var(--border-subtle)] bg-white px-4 py-3">
            <div class="text-[11px] font-semibold uppercase tracking-[0.18em] text-[var(--text-soft)]">Mode</div>
            <div class="mt-1 text-2xl font-semibold capitalize text-[var(--text-strong)]">{activeMode}</div>
          </article>
          <article class="rounded-[18px] border border-[var(--border-subtle)] bg-white px-4 py-3">
            <div class="text-[11px] font-semibold uppercase tracking-[0.18em] text-[var(--text-soft)]">Saved versions</div>
            <div class="mt-1 text-2xl font-semibold text-[var(--text-strong)]">{availableVersions.length}</div>
          </article>
        </div>
      </aside>
    </div>
  </section>

  {#if error}
    <div class="of-inline-note">{error}</div>
  {/if}

  <section class="grid gap-5 xl:grid-cols-[320px_minmax(0,1fr)_420px]">
    <aside class="of-panel overflow-hidden">
      <div class="border-b border-[var(--border-subtle)] px-5 py-4">
        <div class="of-heading-sm">Type and object</div>
        <div class="mt-1 text-sm text-[var(--text-muted)]">Choose the object type and preview object used by the editor.</div>
      </div>

      <div class="space-y-4 px-5 py-5">
        <label class="block text-sm">
          <span class="mb-1 block font-medium text-[var(--text-default)]">Object type</span>
          <select bind:value={selectedTypeId} class="of-select">
            <option value="">Choose object type</option>
            {#each objectTypes as item (item.id)}
              <option value={item.id}>{item.display_name}</option>
            {/each}
          </select>
        </label>

        <label class="block text-sm">
          <span class="mb-1 block font-medium text-[var(--text-default)]">Preview object</span>
          <select bind:value={selectedObjectId} class="of-select">
            <option value="">Choose object</option>
            {#each objects as item (item.id)}
              <option value={item.id}>{objectLabel(item)}</option>
            {/each}
          </select>
        </label>

        <div class="block text-sm">
          <span class="mb-2 block font-medium text-[var(--text-default)]">Object View mode</span>
          <div class="of-pill-toggle">
            <button type="button" data-active={activeMode === 'standard'} onclick={() => activeMode = 'standard'}>Standard</button>
            <button type="button" data-active={activeMode === 'configured'} onclick={() => activeMode = 'configured'}>Configured</button>
          </div>
        </div>

        <div class="block text-sm">
          <span class="mb-2 block font-medium text-[var(--text-default)]">Form factor</span>
          <div class="of-pill-toggle">
            <button type="button" data-active={activeFormFactor === 'full'} onclick={() => activeFormFactor = 'full'}>Full</button>
            <button type="button" data-active={activeFormFactor === 'panel'} onclick={() => activeFormFactor = 'panel'}>Panel</button>
          </div>
        </div>

        <div class="rounded-[18px] border border-[var(--border-subtle)] bg-[#fbfcfe] px-4 py-4 text-sm text-[var(--text-muted)]">
          Standard views stay auto-generated from the object schema. Configured views add curated sections,
          custom tab composition, related applications, and publishable versions.
        </div>
      </div>
    </aside>

    <div class="space-y-5">
      <section class="of-panel overflow-hidden">
        <div class="flex items-center justify-between border-b border-[var(--border-subtle)] px-5 py-4">
          <div>
            <div class="of-heading-sm">Configured Object View editor</div>
            <div class="mt-1 text-sm text-[var(--text-muted)]">
              Build the default configured representation for the current object type and form factor.
            </div>
          </div>
          <div class="flex gap-2">
            {#each editorTabs as tab}
              <button
                type="button"
                class={`rounded-full px-3 py-1.5 text-xs font-medium ${
                  activeEditorTab === tab.id
                    ? 'bg-[#e5eefb] text-[#2458b8]'
                    : 'border border-[var(--border-default)] text-[var(--text-muted)]'
                }`}
                onclick={() => activeEditorTab = tab.id}
              >
                {tab.label}
              </button>
            {/each}
          </div>
        </div>

        {#if activeEditorTab === 'editor'}
          <div class="space-y-5 px-5 py-5">
            <div class="grid gap-4 lg:grid-cols-2">
              <label class="block text-sm">
                <span class="mb-1 block font-medium text-[var(--text-default)]">Title template</span>
                <input bind:value={config.title_template} class="of-input" placeholder="&#123;&#123;name&#125;&#125;" />
              </label>
              <label class="block text-sm">
                <span class="mb-1 block font-medium text-[var(--text-default)]">Subtitle property</span>
                <select bind:value={config.subtitle_property} class="of-select">
                  <option value="">Use object type label</option>
                  {#each properties as property (property.id)}
                    <option value={property.name}>{property.display_name}</option>
                  {/each}
                </select>
              </label>
            </div>

            <div class="grid gap-5 xl:grid-cols-2">
              <div>
                <div class="of-heading-sm">Prominent properties</div>
                <div class="mt-3 flex flex-wrap gap-2">
                  {#each properties as property (property.id)}
                    <button
                      type="button"
                      class={`rounded-full border px-3 py-1.5 text-xs ${
                        config.prominent_properties.includes(property.name)
                          ? 'border-[#99b6e7] bg-[#edf4ff] text-[#2458b8]'
                          : 'border-[var(--border-default)] text-[var(--text-muted)]'
                      }`}
                      onclick={() => toggleProperty('prominent_properties', property.name)}
                    >
                      {property.display_name}
                    </button>
                  {/each}
                </div>
              </div>

              <div>
                <div class="of-heading-sm">Panel properties</div>
                <div class="mt-3 flex flex-wrap gap-2">
                  {#each properties as property (property.id)}
                    <button
                      type="button"
                      class={`rounded-full border px-3 py-1.5 text-xs ${
                        config.panel_properties.includes(property.name)
                          ? 'border-[#99b6e7] bg-[#edf4ff] text-[#2458b8]'
                          : 'border-[var(--border-default)] text-[var(--text-muted)]'
                      }`}
                      onclick={() => toggleProperty('panel_properties', property.name)}
                    >
                      {property.display_name}
                    </button>
                  {/each}
                </div>
              </div>
            </div>

            <div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_320px]">
              <div>
                <div class="flex items-center justify-between">
                  <div class="of-heading-sm">Sections and tabs</div>
                  <div class="flex flex-wrap gap-2">
                    {#each sectionKinds as kind}
                      <button type="button" class="of-chip" onclick={() => addSection(kind.id)}>+ {kind.label}</button>
                    {/each}
                  </div>
                </div>

                <div class="mt-4 space-y-3">
                  {#each config.sections as section (section.id)}
                    <article class="rounded-[18px] border border-[var(--border-subtle)] p-4">
                      <div class="flex items-start justify-between gap-3">
                        <div class="min-w-0 flex-1">
                          <input bind:value={section.title} class="of-input text-sm" />
                          <textarea bind:value={section.description} rows={2} class="of-input mt-2 min-h-[84px] text-sm"></textarea>
                        </div>
                        <div class="space-y-2">
                          <select bind:value={section.kind} class="of-select text-sm">
                            {#each sectionKinds as kind}
                              <option value={kind.id}>{kind.label}</option>
                            {/each}
                          </select>
                          <button type="button" class="of-btn px-3 py-1.5 text-xs" onclick={() => removeSection(section.id)}>
                            Remove
                          </button>
                        </div>
                      </div>
                    </article>
                  {/each}
                </div>
              </div>

              <div>
                <div class="of-heading-sm">Applications sidebar</div>
                <div class="mt-3 space-y-3">
                  {#each config.sidebar_links as link (link.id)}
                    <div class="flex items-center justify-between rounded-[16px] border border-[var(--border-subtle)] px-3 py-2 text-sm">
                      <span>{link.label}</span>
                      <button type="button" class="text-xs font-medium text-rose-600 hover:text-rose-700" onclick={() => removeSidebarLink(link.id)}>
                        Remove
                      </button>
                    </div>
                  {/each}
                </div>

                <div class="mt-4 flex flex-wrap gap-2">
                  {#each sidebarPresets as preset}
                    <button type="button" class="of-chip" onclick={() => addSidebarPreset(preset)}>+ {preset.label}</button>
                  {/each}
                </div>

                <div class="mt-5 rounded-[18px] border border-[var(--border-subtle)] bg-[#fbfcfe] p-4 text-sm">
                  <label class="flex items-center gap-3">
                    <input bind:checked={config.comments_enabled} type="checkbox" />
                    <span>Enable comments section for object collaboration.</span>
                  </label>
                </div>
              </div>
            </div>
          </div>
        {:else if activeEditorTab === 'versions'}
          <div class="space-y-4 px-5 py-5">
            <div class="grid gap-4 lg:grid-cols-[minmax(0,1fr)_220px_auto]">
              <label class="block text-sm">
                <span class="mb-1 block font-medium text-[var(--text-default)]">Version description</span>
                <input bind:value={versionDescription} class="of-input" placeholder="Added panel summary and linked-object previews" />
              </label>
              <label class="block text-sm">
                <span class="mb-1 block font-medium text-[var(--text-default)]">Branch label</span>
                <input bind:value={config.branch_label} class="of-input" />
              </label>
              <div class="flex items-end gap-2">
                <button class="of-btn" type="button" onclick={() => saveVersion(false)}>
                  <Glyph name="bookmark" size={15} />
                  <span>Save version</span>
                </button>
                <button class="of-btn of-btn-primary" type="button" onclick={() => saveVersion(true)}>
                  <Glyph name="run" size={15} />
                  <span>Save and publish</span>
                </button>
              </div>
            </div>

            <label class="flex items-center gap-3 rounded-[16px] border border-[var(--border-subtle)] px-4 py-3 text-sm">
              <input bind:checked={config.auto_publish} type="checkbox" />
              <span>Automatically publish new versions when saving.</span>
            </label>

            <div class="space-y-3">
              {#if availableVersions.length === 0}
                <div class="rounded-[18px] border border-dashed border-[var(--border-default)] px-4 py-10 text-center text-sm text-[var(--text-muted)]">
                  No saved versions yet for this form factor.
                </div>
              {:else}
                {#each availableVersions as version (version.id)}
                  <article class="rounded-[18px] border border-[var(--border-subtle)] p-4">
                    <div class="flex items-start justify-between gap-3">
                      <div>
                        <div class="text-sm font-medium text-[var(--text-strong)]">{version.description}</div>
                        <div class="mt-1 text-xs text-[var(--text-muted)]">
                          {version.created_by} · {new Date(version.created_at).toLocaleString()} · {version.branch_label}
                        </div>
                      </div>
                      {#if version.published}
                        <span class="rounded-full bg-emerald-50 px-2 py-1 text-[11px] text-emerald-700">Published</span>
                      {/if}
                    </div>
                    <div class="mt-3 flex gap-2">
                      <button type="button" class="of-btn px-3 py-1.5 text-xs" onclick={() => previewVersion(version.id)}>
                        Preview
                      </button>
                      <button type="button" class="of-btn px-3 py-1.5 text-xs" onclick={() => publishVersion(version.id)}>
                        Publish
                      </button>
                    </div>
                  </article>
                {/each}
              {/if}
            </div>
          </div>
        {:else}
          <div class="space-y-4 px-5 py-5">
            <div class="grid gap-4 lg:grid-cols-2">
              <article class="rounded-[18px] border border-[var(--border-subtle)] bg-[#fbfcfe] p-4">
                <div class="of-heading-sm">Full Object View URL</div>
                <div class="mt-2 break-all text-sm text-[var(--text-muted)]">{generatedFullUrl || 'Choose a type and object to generate a URL.'}</div>
                <button class="of-btn mt-3 px-3 py-1.5 text-xs" type="button" onclick={() => copyUrl(generatedFullUrl)} disabled={!generatedFullUrl}>
                  Copy full URL
                </button>
              </article>
              <article class="rounded-[18px] border border-[var(--border-subtle)] bg-[#fbfcfe] p-4">
                <div class="of-heading-sm">Panel Object View URL</div>
                <div class="mt-2 break-all text-sm text-[var(--text-muted)]">{generatedPanelUrl || 'Choose a type and object to generate a URL.'}</div>
                <button class="of-btn mt-3 px-3 py-1.5 text-xs" type="button" onclick={() => copyUrl(generatedPanelUrl)} disabled={!generatedPanelUrl}>
                  Copy panel URL
                </button>
              </article>
            </div>

            <div class="rounded-[18px] border border-[var(--border-subtle)] p-4">
              <div class="of-heading-sm">Publishing summary</div>
              <div class="mt-2 text-sm text-[var(--text-muted)]">
                Published configured views become the default representation for the current object type and form factor,
                while standard views remain available as a first-class fallback.
              </div>
            </div>
          </div>
        {/if}
      </section>
    </div>

    <aside class="space-y-5">
      <section class="of-panel overflow-hidden">
        <div class="border-b border-[var(--border-subtle)] px-5 py-4">
          <div class="flex items-center justify-between">
            <div>
              <div class="of-heading-sm">Live preview</div>
              <div class="mt-1 text-sm text-[var(--text-muted)]">
                {activeMode} · {activeFormFactor} · {selectedType?.display_name ?? 'No type selected'}
              </div>
            </div>
            {#if publishedVersion}
              <span class="rounded-full bg-emerald-50 px-2 py-1 text-[11px] text-emerald-700">Published configured view</span>
            {/if}
          </div>
        </div>

        {#if previewLoading}
          <div class="px-5 py-14 text-center text-sm text-[var(--text-muted)]">Loading Object View preview...</div>
        {:else if !preview}
          <div class="px-5 py-14 text-center text-sm text-[var(--text-muted)]">Choose a type and preview object to render the Object View.</div>
        {:else if activeFormFactor === 'full'}
          <div class="space-y-5 px-5 py-5">
            <div class="rounded-[20px] border border-[var(--border-subtle)] bg-[linear-gradient(135deg,#f8fbff_0%,#eef5ff_100%)] p-5">
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                  <div class="truncate text-[20px] font-semibold text-[var(--text-strong)]">
                    {activeMode === 'configured' ? resolveTitle() : objectLabel(preview.object)}
                  </div>
                  <div class="mt-1 text-sm text-[var(--text-muted)]">
                    {activeMode === 'configured' ? resolveSubtitle() : selectedType?.display_name}
                  </div>
                </div>
                <a href={`/ontology/${selectedTypeId}`} class="of-btn px-3 py-1.5 text-xs">
                  <Glyph name="object" size={14} />
                  <span>Open type</span>
                </a>
              </div>

              <div class="mt-4 grid gap-3 sm:grid-cols-2">
                {#each selectedSummaryEntries as [key, value]}
                  <div class="rounded-[16px] border border-white/80 bg-white/90 px-4 py-3">
                    <div class="text-[11px] font-semibold uppercase tracking-[0.18em] text-[var(--text-soft)]">{key}</div>
                    <div class="mt-1 text-sm text-[var(--text-default)]">{normalizeValue(value)}</div>
                  </div>
                {/each}
              </div>
            </div>

            {#if resolvedSections.includes('properties')}
              <section>
                <div class="mb-2 of-heading-sm">Properties</div>
                <div class="space-y-2">
                  {#each Object.entries(preview.object.properties).slice(0, activeMode === 'standard' ? 12 : 8) as [key, value]}
                    <div class="flex items-start justify-between gap-3 rounded-[14px] border border-[var(--border-subtle)] px-3 py-2 text-sm">
                      <span class="text-[var(--text-muted)]">{key}</span>
                      <span class="max-w-[60%] text-right text-[var(--text-default)]">{normalizeValue(value)}</span>
                    </div>
                  {/each}
                </div>
              </section>
            {/if}

            {#if resolvedSections.includes('links')}
              <section>
                <div class="mb-2 of-heading-sm">Linked objects</div>
                <div class="space-y-2">
                  {#each preview.neighbors.slice(0, 6) as neighbor (neighbor.link_id)}
                    <a href={`/object-views?type=${neighbor.object.object_type_id}&object=${neighbor.object.id}&mode=${activeMode}&factor=${activeFormFactor}`} class="flex items-center justify-between rounded-[14px] border border-[var(--border-subtle)] px-3 py-2 text-sm hover:bg-[var(--bg-hover)]">
                      <span>{objectLabel(neighbor.object)}</span>
                      <span class="text-[var(--text-muted)]">{neighbor.link_name}</span>
                    </a>
                  {/each}
                </div>
              </section>
            {/if}

            {#if resolvedSections.includes('timeline')}
              <section>
                <div class="mb-2 of-heading-sm">Timeline</div>
                <div class="space-y-2">
                  {#each preview.timeline.slice(0, 5) as item, index (index)}
                    <div class="rounded-[14px] border border-[var(--border-subtle)] px-3 py-2 text-sm">
                      <div class="text-[var(--text-default)]">{normalizeValue(item.title ?? item.event ?? item.kind ?? `Event ${index + 1}`)}</div>
                      <div class="mt-1 text-xs text-[var(--text-muted)]">{normalizeValue(item.timestamp ?? item.at ?? item.created_at)}</div>
                    </div>
                  {/each}
                </div>
              </section>
            {/if}

            {#if resolvedSections.includes('actions')}
              <section>
                <div class="mb-2 of-heading-sm">Actions</div>
                <div class="flex flex-wrap gap-2">
                  {#each preview.applicable_actions.slice(0, 5) as action (action.id)}
                    <span class="rounded-full bg-[#edf4ff] px-3 py-1.5 text-xs text-[#2458b8]">{action.display_name}</span>
                  {/each}
                </div>
              </section>
            {/if}

            {#if resolvedSections.includes('graph')}
              <section class="rounded-[18px] border border-[var(--border-subtle)] bg-[#fbfcfe] p-4 text-sm text-[var(--text-muted)]">
                Graph context: {preview.graph.total_nodes} nodes · {preview.graph.total_edges} edges · max hops {preview.graph.summary.max_hops_reached}
              </section>
            {/if}

            {#if activeMode === 'configured' && config.sidebar_links.length > 0}
              <section>
                <div class="mb-2 of-heading-sm">Applications sidebar</div>
                <div class="space-y-2">
                  {#each config.sidebar_links as link (link.id)}
                    <a href={link.href} class="flex items-center justify-between rounded-[14px] border border-[var(--border-subtle)] px-3 py-2 text-sm hover:bg-[var(--bg-hover)]">
                      <span>{link.label}</span>
                      <Glyph name={link.glyph} size={14} />
                    </a>
                  {/each}
                </div>
              </section>
            {/if}

            {#if activeMode === 'configured' && config.comments_enabled}
              <section class="rounded-[18px] border border-[var(--border-subtle)] p-4">
                <div class="of-heading-sm">Comments</div>
                <div class="mt-2 text-sm text-[var(--text-muted)]">Configured Object Views can reserve collaboration space for comments and workflow handoff.</div>
              </section>
            {/if}
          </div>
        {:else}
          <div class="px-5 py-5">
            <div class="rounded-[24px] border border-[var(--border-subtle)] bg-white shadow-sm">
              <div class="border-b border-[var(--border-subtle)] px-4 py-3">
                <div class="flex items-start justify-between gap-3">
                  <div class="min-w-0">
                    <div class="truncate text-[16px] font-semibold text-[var(--text-strong)]">
                      {activeMode === 'configured' ? resolveTitle() : objectLabel(preview.object)}
                    </div>
                    <div class="mt-1 text-sm text-[var(--text-muted)]">
                      {activeMode === 'configured' ? resolveSubtitle() : selectedType?.display_name}
                    </div>
                  </div>
                  <button class="of-btn px-3 py-1.5 text-xs" type="button" onclick={() => activeFormFactor = 'full'}>
                    Open full
                  </button>
                </div>
              </div>

              <div class="space-y-4 px-4 py-4">
                <div class="grid gap-3">
                  {#each selectedSummaryEntries as [key, value]}
                    <div class="rounded-[14px] border border-[var(--border-subtle)] bg-[#fbfcfe] px-3 py-2 text-sm">
                      <div class="text-[11px] font-semibold uppercase tracking-[0.18em] text-[var(--text-soft)]">{key}</div>
                      <div class="mt-1 text-[var(--text-default)]">{normalizeValue(value)}</div>
                    </div>
                  {/each}
                </div>

                {#if resolvedSections.includes('links')}
                  <div>
                    <div class="mb-2 of-heading-sm">Linked objects</div>
                    <div class="space-y-2">
                      {#each preview.neighbors.slice(0, 3) as neighbor (neighbor.link_id)}
                        <a href={`/object-views?type=${neighbor.object.object_type_id}&object=${neighbor.object.id}&mode=${activeMode}&factor=panel`} class="flex items-center justify-between rounded-[14px] border border-[var(--border-subtle)] px-3 py-2 text-sm hover:bg-[var(--bg-hover)]">
                          <span>{objectLabel(neighbor.object)}</span>
                          <span class="text-[var(--text-muted)]">{neighbor.link_name}</span>
                        </a>
                      {/each}
                    </div>
                  </div>
                {/if}

                {#if activeMode === 'configured' && config.sidebar_links.length > 0}
                  <div>
                    <div class="mb-2 of-heading-sm">Sidebar shortcuts</div>
                    <div class="flex flex-wrap gap-2">
                      {#each config.sidebar_links.slice(0, 3) as link (link.id)}
                        <a href={link.href} class="of-chip">{link.label}</a>
                      {/each}
                    </div>
                  </div>
                {/if}
              </div>
            </div>
          </div>
        {/if}
      </section>
    </aside>
  </section>
</div>
