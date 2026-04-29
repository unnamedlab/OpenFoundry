<script lang="ts">
  import { browser } from '$app/environment';
  import { onMount } from 'svelte';
  import Glyph from '$components/ui/Glyph.svelte';
  import { getMe, type UserProfile } from '$lib/api/auth';
  import { listDatasets, previewDataset, type Dataset, type DatasetPreviewResponse } from '$lib/api/datasets';
  import {
    attachInterfaceToType,
    attachSharedPropertyType,
    bindProjectResource,
    createInterface,
    createInterfaceProperty,
    createLinkType,
    createOntologyFunnelSource,
    createObjectType,
    createProject,
    createProperty,
    createSharedPropertyType,
    deleteInterfaceProperty,
    deleteProjectMembership,
    detachInterfaceFromType,
    detachSharedPropertyType,
    getObjectType,
    listInterfaceProperties,
    listInterfaces,
    listLinkTypes,
    listObjectSets,
    listObjects,
    listObjectTypes,
    listProjectMemberships,
    listProjectResources,
    listProjects,
    listProperties,
    listSharedPropertyTypes,
    listTypeInterfaces,
    listTypeSharedPropertyTypes,
    getProjectWorkingState,
    replaceProjectWorkingState,
    type InterfaceProperty,
    type LinkType,
    type ObjectSetDefinition,
    type ObjectType,
    type OntologyInterface,
    type OntologyProject,
    type OntologyProjectMembership,
    type OntologyProjectResourceBinding,
    type OntologyProjectRole,
    type Property,
    type SharedPropertyType,
    unbindProjectResource,
    updateInterface,
    updateInterfaceProperty,
    updateObjectType,
    updateProject,
    updateProperty,
    upsertProjectMembership
  } from '$lib/api/ontology';

  type ManagerSection = 'discover' | 'types' | 'interfaces' | 'shared' | 'links' | 'projects' | 'changes' | 'advanced';
  type ReviewFilter = 'all' | 'errors' | 'warnings';
  type ChangeKind =
    | 'object_type'
    | 'property'
    | 'shared_property_type'
    | 'type_shared_property'
    | 'interface'
    | 'interface_property'
    | 'type_interface'
    | 'link_type'
    | 'project'
    | 'project_membership'
    | 'project_resource';
  type ChangeAction = 'create' | 'update' | 'attach' | 'detach' | 'upsert';
  type ResourceRefKind = 'object_type' | 'interface' | 'shared_property_type' | 'project';

  interface ResourceRef {
    kind: ResourceRefKind;
    id?: string;
    name?: string;
    slug?: string;
  }

  interface StagedChange {
    id: string;
    kind: ChangeKind;
    action: ChangeAction;
    label: string;
    description: string;
    targetId?: string;
    parentRef?: ResourceRef;
    payload: Record<string, unknown>;
    warnings: string[];
    errors: string[];
    source: 'manual' | 'import';
    createdAt: string;
  }

  interface RecentItem {
    key: string;
    label: string;
    section: Exclude<ManagerSection, 'discover' | 'changes' | 'advanced'>;
    seenAt: string;
  }

  interface TypeDraft {
    display_name: string;
    description: string;
    icon: string;
    color: string;
  }

  interface CreateTypeDraft extends TypeDraft {
    name: string;
  }

  interface PropertyDraft {
    id?: string;
    name: string;
    display_name: string;
    description: string;
    property_type: string;
    required: boolean;
    unique_constraint: boolean;
    time_dependent: boolean;
  }

  interface InterfaceDraft {
    display_name: string;
    description: string;
  }

  interface CreateInterfaceDraft extends InterfaceDraft {
    name: string;
  }

  interface SharedPropertyDraft {
    name: string;
    display_name: string;
    description: string;
    property_type: string;
    required: boolean;
    unique_constraint: boolean;
    time_dependent: boolean;
  }

  interface LinkDraft {
    name: string;
    display_name: string;
    description: string;
    source_type_id: string;
    target_type_id: string;
    cardinality: string;
  }

  interface ProjectDraft {
    display_name: string;
    description: string;
    workspace_slug: string;
  }

  interface CreateProjectDraft extends ProjectDraft {
    slug: string;
  }

  interface ExportedObjectType {
    id: string;
    name: string;
    display_name: string;
    description: string;
    icon: string | null;
    color: string | null;
    properties: Property[];
    shared_properties: Array<{ id: string; name: string; display_name: string }>;
    interfaces: Array<{ id: string; name: string; display_name: string }>;
  }

  interface ExportedOntologySnapshot {
    metadata: {
      version: string;
      exported_at: string;
      branch: string;
      staged_change_count: number;
    };
    object_types: ExportedObjectType[];
    shared_property_types: SharedPropertyType[];
    interfaces: Array<OntologyInterface & { properties: InterfaceProperty[] }>;
    link_types: LinkType[];
    projects: Array<OntologyProject & {
      memberships: OntologyProjectMembership[];
      resources: OntologyProjectResourceBinding[];
    }>;
    working_changes: StagedChange[];
  }

  const sectionOptions: Array<{ id: ManagerSection; label: string; glyph: 'home' | 'cube' | 'artifact' | 'bookmark' | 'link' | 'folder' | 'history' | 'settings' }> = [
    { id: 'discover', label: 'Discover', glyph: 'home' },
    { id: 'types', label: 'Object types', glyph: 'cube' },
    { id: 'interfaces', label: 'Interfaces', glyph: 'artifact' },
    { id: 'shared', label: 'Shared props', glyph: 'bookmark' },
    { id: 'links', label: 'Link types', glyph: 'link' },
    { id: 'projects', label: 'Projects', glyph: 'folder' },
    { id: 'changes', label: 'Review edits', glyph: 'history' },
    { id: 'advanced', label: 'Advanced', glyph: 'settings' }
  ];

  const branchOptions = ['main', 'working-copy', 'release-candidate'];
  const propertyTypeOptions = ['string', 'integer', 'float', 'boolean', 'date', 'datetime', 'json', 'embedding'];
  const projectRoleOptions: OntologyProjectRole[] = ['owner', 'editor', 'viewer'];

  let loading = $state(true);
  let loadError = $state('');
  let saveError = $state('');
  let saveSuccess = $state('');
  let saving = $state(false);
  let refreshing = $state(false);

  let activeSection = $state<ManagerSection>('discover');
  let activeBranch = $state('main');
  let searchQuery = $state('');
  let reviewFilter = $state<ReviewFilter>('all');

  let objectTypes = $state<ObjectType[]>([]);
  let interfaces = $state<OntologyInterface[]>([]);
  let sharedPropertyTypes = $state<SharedPropertyType[]>([]);
  let linkTypes = $state<LinkType[]>([]);
  let projects = $state<OntologyProject[]>([]);
  let objectSets = $state<ObjectSetDefinition[]>([]);

  let selectedTypeId = $state('');
  let selectedInterfaceId = $state('');
  let selectedSharedPropertyId = $state('');
  let selectedLinkId = $state('');
  let selectedProjectId = $state('');

  let selectedTypeProperties = $state<Property[]>([]);
  let selectedTypeSharedProperties = $state<SharedPropertyType[]>([]);
  let selectedTypeInterfaces = $state<OntologyInterface[]>([]);
  let selectedTypeObjectCount = $state(0);

  let selectedInterfaceProperties = $state<InterfaceProperty[]>([]);

  let selectedProjectMemberships = $state<OntologyProjectMembership[]>([]);
  let selectedProjectResources = $state<OntologyProjectResourceBinding[]>([]);

  let changeQueue = $state<StagedChange[]>([]);
  let recentItems = $state<RecentItem[]>([]);
  let favoriteKeys = $state<string[]>([]);
  let currentUser = $state<UserProfile | null>(null);
  let datasets = $state<Dataset[]>([]);
  let datasetPreview = $state<DatasetPreviewResponse | null>(null);
  let wizardStep = $state(1);
  let wizardBusy = $state(false);
  let wizardError = $state('');
  let wizardSuccess = $state('');
  let wizardDraft = $state({
    dataset_id: '',
    object_name: '',
    display_name: '',
    description: '',
    primary_key_column: '',
    title_column: '',
    indexing_enabled: true
  });

  let typeDraft = $state<TypeDraft>({ display_name: '', description: '', icon: '', color: '' });
  let createTypeDraft = $state<CreateTypeDraft>({ name: '', display_name: '', description: '', icon: '', color: '' });
  let propertyDraft = $state<PropertyDraft>({
    name: '',
    display_name: '',
    description: '',
    property_type: 'string',
    required: false,
    unique_constraint: false,
    time_dependent: false
  });
  let interfaceDraft = $state<InterfaceDraft>({ display_name: '', description: '' });
  let createInterfaceDraft = $state<CreateInterfaceDraft>({ name: '', display_name: '', description: '' });
  let interfacePropertyDraft = $state<PropertyDraft>({
    name: '',
    display_name: '',
    description: '',
    property_type: 'string',
    required: false,
    unique_constraint: false,
    time_dependent: false
  });
  let sharedPropertyDraft = $state<SharedPropertyDraft>({
    name: '',
    display_name: '',
    description: '',
    property_type: 'string',
    required: false,
    unique_constraint: false,
    time_dependent: false
  });
  let linkDraft = $state<LinkDraft>({
    name: '',
    display_name: '',
    description: '',
    source_type_id: '',
    target_type_id: '',
    cardinality: 'many_to_many'
  });
  let projectDraft = $state<ProjectDraft>({ display_name: '', description: '', workspace_slug: '' });
  let createProjectDraft = $state<CreateProjectDraft>({
    slug: '',
    display_name: '',
    description: '',
    workspace_slug: ''
  });
  let membershipUserId = $state('');
  let membershipRole = $state<OntologyProjectRole>('editor');
  let resourceBindingKind = $state<'object_type' | 'link_type' | 'interface' | 'shared_property_type'>('object_type');
  let resourceBindingId = $state('');

  let importText = $state('');
  let importError = $state('');
  let importSummary = $state('');
  let exportBusy = $state(false);

  const objectTypeMap = $derived.by(() => {
    const map = new Map<string, ObjectType>();
    for (const item of objectTypes) map.set(item.id, item);
    return map;
  });

  const interfaceMap = $derived.by(() => {
    const map = new Map<string, OntologyInterface>();
    for (const item of interfaces) map.set(item.id, item);
    return map;
  });

  const sharedPropertyMap = $derived.by(() => {
    const map = new Map<string, SharedPropertyType>();
    for (const item of sharedPropertyTypes) map.set(item.id, item);
    return map;
  });

  const projectMap = $derived.by(() => {
    const map = new Map<string, OntologyProject>();
    for (const item of projects) map.set(item.id, item);
    return map;
  });

  const selectedType = $derived(objectTypes.find((item) => item.id === selectedTypeId) ?? null);
  const selectedInterface = $derived(interfaces.find((item) => item.id === selectedInterfaceId) ?? null);
  const selectedSharedProperty = $derived(sharedPropertyTypes.find((item) => item.id === selectedSharedPropertyId) ?? null);
  const selectedLink = $derived(linkTypes.find((item) => item.id === selectedLinkId) ?? null);
  const selectedProject = $derived(projects.find((item) => item.id === selectedProjectId) ?? null);
  const selectedProjectRole = $derived.by(() => {
    const current = currentUser;
    if (!current || !selectedProject) return null;
    if (selectedProject.owner_id === current.id) return 'owner' as const;
    return selectedProjectMemberships.find((item) => item.user_id === current.id)?.role ?? 'viewer';
  });
  const canEditSelectedProject = $derived(selectedProjectRole === 'owner' || selectedProjectRole === 'editor');

  const filteredObjectTypes = $derived.by(() => filterBySearch(objectTypes, searchQuery, (item) => `${item.name} ${item.display_name} ${item.description}`));
  const filteredInterfaces = $derived.by(() => filterBySearch(interfaces, searchQuery, (item) => `${item.name} ${item.display_name} ${item.description}`));
  const filteredSharedProperties = $derived.by(() => filterBySearch(sharedPropertyTypes, searchQuery, (item) => `${item.name} ${item.display_name} ${item.description}`));
  const filteredLinks = $derived.by(() =>
    filterBySearch(linkTypes, searchQuery, (item) => `${item.name} ${item.display_name} ${item.description} ${objectTypeMap.get(item.source_type_id)?.display_name ?? ''} ${objectTypeMap.get(item.target_type_id)?.display_name ?? ''}`)
  );
  const filteredProjects = $derived.by(() => filterBySearch(projects, searchQuery, (item) => `${item.slug} ${item.display_name} ${item.description}`));

  const prominentTypes = $derived.by(() => {
    return [...objectTypes]
      .sort((left, right) => right.updated_at.localeCompare(left.updated_at))
      .slice(0, 6);
  });

  const favoriteResources = $derived.by(() => {
    const resolved: RecentItem[] = [];
    for (const key of favoriteKeys) {
      const item = resolveFavorite(key);
      if (item) resolved.push(item);
      if (resolved.length >= 8) break;
    }
    return resolved;
  });

  const staleTypeCandidates = $derived.by(() => {
    return objectTypes
      .map((typeItem) => {
        const hasDescription = Boolean(typeItem.description?.trim());
        const recentlyTouched = typeItem.updated_at.slice(0, 10);
        const linked = linkTypes.filter((item) => item.source_type_id === typeItem.id || item.target_type_id === typeItem.id).length;
        return {
          id: typeItem.id,
          label: typeItem.display_name,
          issues: [
            !hasDescription ? 'missing description' : '',
            linked === 0 ? 'no link surface yet' : '',
            !favoriteKeys.includes(`types:${typeItem.id}`) ? 'not favorited by current curator' : ''
          ].filter(Boolean)
        };
      })
      .filter((item) => item.issues.length > 0)
      .slice(0, 6);
  });

  const pendingChanges = $derived(changeQueue.length);
  const reviewWarnings = $derived(changeQueue.reduce((total, change) => total + change.warnings.length, 0));
  const reviewErrors = $derived(changeQueue.reduce((total, change) => total + change.errors.length, 0));

  const visibleChanges = $derived.by(() => {
    if (reviewFilter === 'all') return changeQueue;
    if (reviewFilter === 'errors') return changeQueue.filter((change) => change.errors.length > 0);
    return changeQueue.filter((change) => change.warnings.length > 0);
  });

  const discoverSections = $derived.by(() => [
    {
      title: 'Favorites',
      description: 'Pinned resource types for repeat ontology curation.',
      items: favoriteResources
    },
    {
      title: 'Recently viewed',
      description: 'Resume object type and project changes where you left off.',
      items: recentItems.slice(0, 6)
    },
    {
      title: 'Recently modified object types',
      description: 'Updated object types that usually need save review and downstream checks.',
      items: prominentTypes.map((item) => ({
        key: `types:${item.id}`,
        label: item.display_name,
        section: 'types' as const,
        seenAt: item.updated_at
      }))
    }
  ]);

  onMount(() => {
    loadPreferences();
    void loadCatalog();
  });

  function createId() {
    if (browser && globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID();
    return `change-${Math.random().toString(36).slice(2, 10)}`;
  }

  function filterBySearch<T>(items: T[], query: string, accessor: (item: T) => string) {
    const value = query.trim().toLowerCase();
    if (!value) return items;
    return items.filter((item) => accessor(item).toLowerCase().includes(value));
  }

  function normalizeName(value: string) {
    return value
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '_')
      .replace(/^_+|_+$/g, '');
  }

  function storageKey(name: string) {
    return `of.ontologyManager.${name}`;
  }

  function loadPreferences() {
    if (!browser) return;
    try {
      favoriteKeys = JSON.parse(window.localStorage.getItem(storageKey('favorites')) ?? '[]');
      recentItems = JSON.parse(window.localStorage.getItem(storageKey('recent')) ?? '[]');
    } catch {
      favoriteKeys = [];
      recentItems = [];
    }
  }

  function persistPreferences() {
    if (!browser) return;
    window.localStorage.setItem(storageKey('favorites'), JSON.stringify(favoriteKeys));
    window.localStorage.setItem(storageKey('recent'), JSON.stringify(recentItems));
  }

  async function loadWorkingState(projectId = selectedProjectId) {
    if (!projectId) {
      changeQueue = [];
      return;
    }
    try {
      const workingState = await getProjectWorkingState(projectId);
      changeQueue = workingState.changes as StagedChange[];
    } catch {
      changeQueue = [];
    }
  }

  async function persistWorkingState(projectId = selectedProjectId) {
    if (!projectId || !canEditSelectedProject) return;
    await replaceProjectWorkingState(projectId, changeQueue);
  }

  function recentSectionKey(section: Exclude<ManagerSection, 'discover' | 'changes' | 'advanced'>, id: string) {
    return `${section}:${id}`;
  }

  function markRecent(section: Exclude<ManagerSection, 'discover' | 'changes' | 'advanced'>, id: string, label: string) {
    const key = recentSectionKey(section, id);
    recentItems = [{ key, label, section, seenAt: new Date().toISOString() }, ...recentItems.filter((item) => item.key !== key)].slice(0, 10);
    persistPreferences();
  }

  function resolveFavorite(key: string): RecentItem | null {
    const [section, id] = key.split(':');
    if (section === 'types') {
      const item = objectTypeMap.get(id);
      return item ? { key, label: item.display_name, section: 'types' as const, seenAt: item.updated_at } : null;
    }
    if (section === 'interfaces') {
      const item = interfaceMap.get(id);
      return item ? { key, label: item.display_name, section: 'interfaces' as const, seenAt: item.updated_at } : null;
    }
    if (section === 'projects') {
      const item = projectMap.get(id);
      return item ? { key, label: item.display_name, section: 'projects' as const, seenAt: item.updated_at } : null;
    }
    if (section === 'shared') {
      const item = sharedPropertyMap.get(id);
      return item ? { key, label: item.display_name, section: 'shared' as const, seenAt: item.updated_at } : null;
    }
    return null;
  }

  function toggleFavorite(section: Exclude<ManagerSection, 'discover' | 'changes' | 'advanced'>, id: string) {
    const key = recentSectionKey(section, id);
    favoriteKeys = favoriteKeys.includes(key) ? favoriteKeys.filter((item) => item !== key) : [...favoriteKeys, key];
    persistPreferences();
  }

  function isFavorite(section: Exclude<ManagerSection, 'discover' | 'changes' | 'advanced'>, id: string) {
    return favoriteKeys.includes(recentSectionKey(section, id));
  }

  function queueChange(change: StagedChange) {
    changeQueue = [change, ...changeQueue];
    saveSuccess = '';
    saveError = '';
    void persistWorkingState();
  }

  function removeChange(id: string) {
    changeQueue = changeQueue.filter((change) => change.id !== id);
    void persistWorkingState();
  }

  function clearChanges() {
    changeQueue = [];
    void persistWorkingState();
  }

  function buildChange(params: Omit<StagedChange, 'id' | 'createdAt'>): StagedChange {
    return {
      ...params,
      id: createId(),
      createdAt: new Date().toISOString()
    };
  }

  function validatePropertyDraft(draft: PropertyDraft) {
    const warnings: string[] = [];
    const errors: string[] = [];

    if (!normalizeName(draft.name)) errors.push('Property API name is required.');
    if (!draft.display_name.trim()) warnings.push('Property display name is empty.');
    if (!draft.description.trim()) warnings.push('Property description is empty.');
    if (draft.unique_constraint && !draft.required) warnings.push('Unique properties usually need required enabled.');

    return { warnings, errors };
  }

  function loadTypeDraft(typeItem: ObjectType) {
    typeDraft = {
      display_name: typeItem.display_name ?? '',
      description: typeItem.description ?? '',
      icon: typeItem.icon ?? '',
      color: typeItem.color ?? ''
    };
  }

  function loadInterfaceDraft(interfaceItem: OntologyInterface) {
    interfaceDraft = {
      display_name: interfaceItem.display_name ?? '',
      description: interfaceItem.description ?? ''
    };
  }

  function loadProjectDraft(projectItem: OntologyProject) {
    projectDraft = {
      display_name: projectItem.display_name ?? '',
      description: projectItem.description ?? '',
      workspace_slug: projectItem.workspace_slug ?? ''
    };
  }

  function loadPropertyDraft(property: Property | InterfaceProperty) {
    return {
      id: property.id,
      name: property.name,
      display_name: property.display_name ?? '',
      description: property.description ?? '',
      property_type: property.property_type,
      required: property.required,
      unique_constraint: property.unique_constraint,
      time_dependent: property.time_dependent
    };
  }

  async function loadCatalog() {
    loading = true;
    loadError = '';
    try {
      const [me, datasetResponse, typesResponse, interfacesResponse, sharedResponse, linksResponse, projectsResponse, objectSetsResponse] = await Promise.all([
        getMe(),
        listDatasets({ page: 1, per_page: 200 }),
        listObjectTypes({ page: 1, per_page: 200 }),
        listInterfaces({ page: 1, per_page: 200 }),
        listSharedPropertyTypes({ page: 1, per_page: 200 }),
        listLinkTypes({ page: 1, per_page: 200 }),
        listProjects({ page: 1, per_page: 200 }),
        listObjectSets().catch(() => ({ data: [] }))
      ]);

      currentUser = me;
      datasets = datasetResponse.data;
      objectTypes = typesResponse.data;
      interfaces = interfacesResponse.data;
      sharedPropertyTypes = sharedResponse.data;
      linkTypes = linksResponse.data;
      projects = projectsResponse.data;
      objectSets = objectSetsResponse.data;

      if (!selectedTypeId && objectTypes[0]) selectedTypeId = objectTypes[0].id;
      if (!selectedInterfaceId && interfaces[0]) selectedInterfaceId = interfaces[0].id;
      if (!selectedSharedPropertyId && sharedPropertyTypes[0]) selectedSharedPropertyId = sharedPropertyTypes[0].id;
      if (!selectedLinkId && linkTypes[0]) selectedLinkId = linkTypes[0].id;
      if (!selectedProjectId && projects[0]) selectedProjectId = projects[0].id;
      if (!wizardDraft.dataset_id && datasets[0]) wizardDraft.dataset_id = datasets[0].id;

      if (selectedTypeId) await loadSelectedType(selectedTypeId);
      if (selectedInterfaceId) await loadSelectedInterface(selectedInterfaceId);
      if (selectedProjectId) {
        await Promise.all([loadSelectedProject(selectedProjectId), loadWorkingState(selectedProjectId)]);
      }
      if (wizardDraft.dataset_id) {
        datasetPreview = await previewDataset(wizardDraft.dataset_id, { limit: 20 }).catch(() => null);
      }

      if (!linkDraft.source_type_id && objectTypes[0]) linkDraft.source_type_id = objectTypes[0].id;
      if (!linkDraft.target_type_id && objectTypes[1]) linkDraft.target_type_id = objectTypes[1].id;
      else if (!linkDraft.target_type_id && objectTypes[0]) linkDraft.target_type_id = objectTypes[0].id;
      if (!resourceBindingId && objectTypes[0]) resourceBindingId = objectTypes[0].id;
    } catch (cause) {
      loadError = cause instanceof Error ? cause.message : 'Failed to load Ontology Manager';
    } finally {
      loading = false;
    }
  }

  async function loadSelectedType(id: string) {
    if (!id) return;
    const typeItem = objectTypeMap.get(id);
    if (typeItem) loadTypeDraft(typeItem);
    try {
      const [detail, propertiesResponse, sharedProps, interfacesResponse, objectsResponse] = await Promise.all([
        getObjectType(id),
        listProperties(id),
        listTypeSharedPropertyTypes(id),
        listTypeInterfaces(id),
        listObjects(id, { page: 1, per_page: 1 }).catch(() => ({ data: [], total: 0 }))
      ]);
      selectedTypeProperties = propertiesResponse;
      selectedTypeSharedProperties = sharedProps;
      selectedTypeInterfaces = interfacesResponse;
      selectedTypeObjectCount = objectsResponse.total;
      propertyDraft = {
        name: '',
        display_name: '',
        description: '',
        property_type: 'string',
        required: false,
        unique_constraint: false,
        time_dependent: false
      };
      if (typeItem) markRecent('types', typeItem.id, typeItem.display_name);
    } catch (cause) {
      saveError = cause instanceof Error ? cause.message : 'Failed to load object type detail';
    }
  }

  async function loadSelectedInterface(id: string) {
    if (!id) return;
    const interfaceItem = interfaceMap.get(id);
    if (interfaceItem) loadInterfaceDraft(interfaceItem);
    try {
      selectedInterfaceProperties = await listInterfaceProperties(id);
      interfacePropertyDraft = {
        name: '',
        display_name: '',
        description: '',
        property_type: 'string',
        required: false,
        unique_constraint: false,
        time_dependent: false
      };
      if (interfaceItem) markRecent('interfaces', interfaceItem.id, interfaceItem.display_name);
    } catch (cause) {
      saveError = cause instanceof Error ? cause.message : 'Failed to load interface detail';
    }
  }

  async function loadSelectedProject(id: string) {
    if (!id) return;
    const projectItem = projectMap.get(id);
    if (projectItem) loadProjectDraft(projectItem);
    try {
      const [memberships, resources] = await Promise.all([
        listProjectMemberships(id),
        listProjectResources(id)
      ]);
      selectedProjectMemberships = memberships;
      selectedProjectResources = resources;
      if (projectItem) markRecent('projects', projectItem.id, projectItem.display_name);
    } catch (cause) {
      saveError = cause instanceof Error ? cause.message : 'Failed to load project detail';
    }
  }

  function selectType(id: string) {
    selectedTypeId = id;
    activeSection = 'types';
    void loadSelectedType(id);
  }

  function selectInterface(id: string) {
    selectedInterfaceId = id;
    activeSection = 'interfaces';
    void loadSelectedInterface(id);
  }

  function selectSharedProperty(id: string) {
    selectedSharedPropertyId = id;
    activeSection = 'shared';
    const item = sharedPropertyMap.get(id);
    if (item) markRecent('shared', item.id, item.display_name);
  }

  function selectLink(id: string) {
    selectedLinkId = id;
    activeSection = 'links';
  }

  function selectProject(id: string) {
    selectedProjectId = id;
    activeSection = 'projects';
    void Promise.all([loadSelectedProject(id), loadWorkingState(id)]);
  }

  function stageTypeUpdate() {
    if (!selectedType) return;
    const warnings: string[] = [];
    const errors: string[] = [];

    if (!typeDraft.display_name.trim()) warnings.push('Display name is empty.');
    if (!typeDraft.description.trim()) warnings.push('Description is empty.');
    if (typeDraft.color && !typeDraft.color.startsWith('#')) warnings.push('Color should usually be a hex value.');

    queueChange(
      buildChange({
        kind: 'object_type',
        action: 'update',
        label: selectedType.display_name,
        description: 'Update object type metadata',
        targetId: selectedType.id,
        payload: { ...typeDraft },
        warnings,
        errors,
        source: 'manual'
      })
    );
  }

  function stageTypeCreate() {
    const name = normalizeName(createTypeDraft.name);
    const warnings: string[] = [];
    const errors: string[] = [];

    if (!name) errors.push('Object type API name is required.');
    if (!createTypeDraft.display_name.trim()) warnings.push('Display name is empty.');
    if (!createTypeDraft.description.trim()) warnings.push('Description is empty.');

    queueChange(
      buildChange({
        kind: 'object_type',
        action: 'create',
        label: createTypeDraft.display_name || name,
        description: 'Create object type',
        payload: {
          name,
          display_name: createTypeDraft.display_name || titleCase(name),
          description: createTypeDraft.description,
          icon: createTypeDraft.icon || undefined,
          color: createTypeDraft.color || undefined
        },
        warnings,
        errors,
        source: 'manual'
      })
    );

    createTypeDraft = { name: '', display_name: '', description: '', icon: '', color: '' };
  }

  function stagePropertyCreate() {
    if (!selectedType) return;
    const draft = { ...propertyDraft, name: normalizeName(propertyDraft.name) };
    const { warnings, errors } = validatePropertyDraft(draft);
    queueChange(
      buildChange({
        kind: 'property',
        action: 'create',
        label: draft.display_name || draft.name,
        description: `Create property on ${selectedType.display_name}`,
        parentRef: { kind: 'object_type', id: selectedType.id, name: selectedType.name },
        payload: { ...draft },
        warnings,
        errors,
        source: 'manual'
      })
    );
    propertyDraft = { name: '', display_name: '', description: '', property_type: 'string', required: false, unique_constraint: false, time_dependent: false };
  }

  function stagePropertyUpdate(property: Property) {
    const draft = loadPropertyDraft(property);
    const warnings: string[] = [];
    const errors: string[] = [];

    if (!draft.display_name.trim()) warnings.push('Display name is empty.');
    if (!draft.description.trim()) warnings.push('Description is empty.');
    warnings.push('Changing properties can affect object views, searches, and writeback flows.');

    queueChange(
      buildChange({
        kind: 'property',
        action: 'update',
        label: draft.display_name || draft.name,
        description: `Refresh property metadata on ${selectedType?.display_name ?? 'object type'}`,
        targetId: property.id,
        parentRef: { kind: 'object_type', id: selectedType?.id, name: selectedType?.name },
        payload: {
          display_name: draft.display_name,
          description: draft.description,
          required: draft.required,
          unique_constraint: draft.unique_constraint,
          time_dependent: draft.time_dependent
        },
        warnings,
        errors,
        source: 'manual'
      })
    );
  }

  function stageSharedPropertyCreate() {
    const name = normalizeName(sharedPropertyDraft.name);
    const warnings: string[] = [];
    const errors: string[] = [];

    if (!name) errors.push('Shared property API name is required.');
    if (!sharedPropertyDraft.display_name.trim()) warnings.push('Display name is empty.');

    queueChange(
      buildChange({
        kind: 'shared_property_type',
        action: 'create',
        label: sharedPropertyDraft.display_name || name,
        description: 'Create shared property type',
        payload: {
          name,
          display_name: sharedPropertyDraft.display_name || titleCase(name),
          description: sharedPropertyDraft.description,
          property_type: sharedPropertyDraft.property_type,
          required: sharedPropertyDraft.required,
          unique_constraint: sharedPropertyDraft.unique_constraint,
          time_dependent: sharedPropertyDraft.time_dependent
        },
        warnings,
        errors,
        source: 'manual'
      })
    );

    sharedPropertyDraft = {
      name: '',
      display_name: '',
      description: '',
      property_type: 'string',
      required: false,
      unique_constraint: false,
      time_dependent: false
    };
  }

  function stageSharedPropertyAttach(sharedProperty: SharedPropertyType) {
    if (!selectedType) return;
    const alreadyAttached = selectedTypeSharedProperties.some((item) => item.id === sharedProperty.id);
    queueChange(
      buildChange({
        kind: 'type_shared_property',
        action: alreadyAttached ? 'detach' : 'attach',
        label: `${selectedType.display_name} -> ${sharedProperty.display_name}`,
        description: `${alreadyAttached ? 'Detach' : 'Attach'} shared property`,
        parentRef: { kind: 'object_type', id: selectedType.id, name: selectedType.name },
        payload: { shared_property_type_id: sharedProperty.id, shared_property_type_name: sharedProperty.name },
        warnings: [],
        errors: [],
        source: 'manual'
      })
    );
  }

  function stageInterfaceCreate() {
    const name = normalizeName(createInterfaceDraft.name);
    const warnings: string[] = [];
    const errors: string[] = [];
    if (!name) errors.push('Interface API name is required.');
    if (!createInterfaceDraft.display_name.trim()) warnings.push('Display name is empty.');

    queueChange(
      buildChange({
        kind: 'interface',
        action: 'create',
        label: createInterfaceDraft.display_name || name,
        description: 'Create interface',
        payload: {
          name,
          display_name: createInterfaceDraft.display_name || titleCase(name),
          description: createInterfaceDraft.description
        },
        warnings,
        errors,
        source: 'manual'
      })
    );

    createInterfaceDraft = { name: '', display_name: '', description: '' };
  }

  function stageInterfaceUpdate() {
    if (!selectedInterface) return;
    const warnings: string[] = [];
    const errors: string[] = [];
    if (!interfaceDraft.display_name.trim()) warnings.push('Display name is empty.');
    queueChange(
      buildChange({
        kind: 'interface',
        action: 'update',
        label: selectedInterface.display_name,
        description: 'Update interface metadata',
        targetId: selectedInterface.id,
        payload: { ...interfaceDraft },
        warnings,
        errors,
        source: 'manual'
      })
    );
  }

  function stageInterfacePropertyCreate() {
    if (!selectedInterface) return;
    const draft = { ...interfacePropertyDraft, name: normalizeName(interfacePropertyDraft.name) };
    const { warnings, errors } = validatePropertyDraft(draft);
    queueChange(
      buildChange({
        kind: 'interface_property',
        action: 'create',
        label: draft.display_name || draft.name,
        description: `Create interface property on ${selectedInterface.display_name}`,
        parentRef: { kind: 'interface', id: selectedInterface.id, name: selectedInterface.name },
        payload: { ...draft },
        warnings,
        errors,
        source: 'manual'
      })
    );
    interfacePropertyDraft = { name: '', display_name: '', description: '', property_type: 'string', required: false, unique_constraint: false, time_dependent: false };
  }

  function stageInterfacePropertyUpdate(property: InterfaceProperty) {
    const draft = loadPropertyDraft(property);
    queueChange(
      buildChange({
        kind: 'interface_property',
        action: 'update',
        label: draft.display_name || draft.name,
        description: `Update interface property on ${selectedInterface?.display_name ?? 'interface'}`,
        targetId: property.id,
        parentRef: { kind: 'interface', id: selectedInterface?.id, name: selectedInterface?.name },
        payload: {
          display_name: draft.display_name,
          description: draft.description,
          required: draft.required,
          unique_constraint: draft.unique_constraint,
          time_dependent: draft.time_dependent
        },
        warnings: draft.description.trim() ? [] : ['Property description is empty.'],
        errors: [],
        source: 'manual'
      })
    );
  }

  function stageTypeInterfaceToggle(interfaceItem: OntologyInterface) {
    if (!selectedType) return;
    const alreadyAttached = selectedTypeInterfaces.some((item) => item.id === interfaceItem.id);
    queueChange(
      buildChange({
        kind: 'type_interface',
        action: alreadyAttached ? 'detach' : 'attach',
        label: `${selectedType.display_name} -> ${interfaceItem.display_name}`,
        description: `${alreadyAttached ? 'Detach' : 'Attach'} interface`,
        parentRef: { kind: 'object_type', id: selectedType.id, name: selectedType.name },
        payload: { interface_id: interfaceItem.id, interface_name: interfaceItem.name },
        warnings: [],
        errors: [],
        source: 'manual'
      })
    );
  }

  function stageLinkCreate() {
    const name = normalizeName(linkDraft.name);
    const warnings: string[] = [];
    const errors: string[] = [];
    if (!name) errors.push('Link type API name is required.');
    if (!linkDraft.source_type_id || !linkDraft.target_type_id) errors.push('Source and target object types are required.');
    if (!linkDraft.display_name.trim()) warnings.push('Display name is empty.');

    queueChange(
      buildChange({
        kind: 'link_type',
        action: 'create',
        label: linkDraft.display_name || name,
        description: 'Create link type',
        payload: {
          name,
          display_name: linkDraft.display_name || titleCase(name),
          description: linkDraft.description,
          source_type_id: linkDraft.source_type_id,
          target_type_id: linkDraft.target_type_id,
          cardinality: linkDraft.cardinality
        },
        warnings,
        errors,
        source: 'manual'
      })
    );

    linkDraft = {
      name: '',
      display_name: '',
      description: '',
      source_type_id: objectTypes[0]?.id ?? '',
      target_type_id: objectTypes[1]?.id ?? objectTypes[0]?.id ?? '',
      cardinality: 'many_to_many'
    };
  }

  function stageProjectCreate() {
    const slug = normalizeName(createProjectDraft.slug);
    const warnings: string[] = [];
    const errors: string[] = [];
    if (!slug) errors.push('Project slug is required.');
    if (!createProjectDraft.display_name.trim()) warnings.push('Display name is empty.');

    queueChange(
      buildChange({
        kind: 'project',
        action: 'create',
        label: createProjectDraft.display_name || slug,
        description: 'Create ontology project',
        payload: {
          slug,
          display_name: createProjectDraft.display_name || titleCase(slug),
          description: createProjectDraft.description,
          workspace_slug: createProjectDraft.workspace_slug || undefined
        },
        warnings,
        errors,
        source: 'manual'
      })
    );
    createProjectDraft = { slug: '', display_name: '', description: '', workspace_slug: '' };
  }

  function stageProjectUpdate() {
    if (!selectedProject) return;
    queueChange(
      buildChange({
        kind: 'project',
        action: 'update',
        label: selectedProject.display_name,
        description: 'Update project metadata',
        targetId: selectedProject.id,
        payload: {
          display_name: projectDraft.display_name,
          description: projectDraft.description,
          workspace_slug: projectDraft.workspace_slug || null
        },
        warnings: projectDraft.description.trim() ? [] : ['Description is empty.'],
        errors: [],
        source: 'manual'
      })
    );
  }

  function stageProjectMembership() {
    if (!selectedProject) return;
    const userId = membershipUserId.trim();
    const warnings: string[] = [];
    const errors: string[] = [];
    if (!userId) errors.push('User id is required.');
    if (membershipRole === 'owner') warnings.push('Owners can mutate the project and all bound resources.');

    queueChange(
      buildChange({
        kind: 'project_membership',
        action: 'upsert',
        label: `${selectedProject.display_name} -> ${userId}`,
        description: 'Grant or update project membership',
        parentRef: { kind: 'project', id: selectedProject.id, slug: selectedProject.slug },
        payload: { user_id: userId, role: membershipRole },
        warnings,
        errors,
        source: 'manual'
      })
    );
    membershipUserId = '';
    membershipRole = 'editor';
  }

  function stageProjectResource() {
    if (!selectedProject) return;
    const warnings: string[] = [];
    const errors: string[] = [];
    if (!resourceBindingId) errors.push('Select a resource to bind.');

    queueChange(
      buildChange({
        kind: 'project_resource',
        action: 'attach',
        label: `${selectedProject.display_name} -> ${resourceBindingKind}`,
        description: 'Bind project-scoped ontology resource',
        parentRef: { kind: 'project', id: selectedProject.id, slug: selectedProject.slug },
        payload: {
          resource_kind: resourceBindingKind,
          resource_id: resourceBindingId
        },
        warnings,
        errors,
        source: 'manual'
      })
    );
  }

  function stageImportedSnapshot(snapshot: ExportedOntologySnapshot) {
    importError = '';
    importSummary = '';

    const importedChanges: StagedChange[] = [];

    for (const sharedProperty of snapshot.shared_property_types ?? []) {
      const existing = sharedPropertyTypes.find((item) => item.id === sharedProperty.id || item.name === sharedProperty.name);
      if (!existing) {
        importedChanges.push(
          buildChange({
            kind: 'shared_property_type',
            action: 'create',
            label: sharedProperty.display_name,
            description: 'Imported shared property type',
            payload: {
              name: sharedProperty.name,
              display_name: sharedProperty.display_name,
              description: sharedProperty.description,
              property_type: sharedProperty.property_type,
              required: sharedProperty.required,
              unique_constraint: sharedProperty.unique_constraint,
              time_dependent: sharedProperty.time_dependent
            },
            warnings: [],
            errors: [],
            source: 'import'
          })
        );
      }
    }

    for (const ontologyInterface of snapshot.interfaces ?? []) {
      const existing = interfaces.find((item) => item.id === ontologyInterface.id || item.name === ontologyInterface.name);
      importedChanges.push(
        buildChange({
          kind: 'interface',
          action: existing ? 'update' : 'create',
          label: ontologyInterface.display_name,
          description: 'Imported interface metadata',
          targetId: existing?.id,
          payload: existing
            ? {
                display_name: ontologyInterface.display_name,
                description: ontologyInterface.description
              }
            : {
                name: ontologyInterface.name,
                display_name: ontologyInterface.display_name,
                description: ontologyInterface.description
              },
          warnings: [],
          errors: [],
          source: 'import'
        })
      );

      for (const property of ontologyInterface.properties ?? []) {
        const existingProperty = existing ? selectedInterfaceProperties.find((item) => item.name === property.name) : null;
        importedChanges.push(
          buildChange({
            kind: 'interface_property',
            action: existingProperty ? 'update' : 'create',
            label: property.display_name,
            description: 'Imported interface property',
            targetId: existingProperty?.id,
            parentRef: { kind: 'interface', id: existing?.id, name: ontologyInterface.name },
            payload: existingProperty
              ? {
                  display_name: property.display_name,
                  description: property.description,
                  required: property.required,
                  unique_constraint: property.unique_constraint,
                  time_dependent: property.time_dependent
                }
              : {
                  name: property.name,
                  display_name: property.display_name,
                  description: property.description,
                  property_type: property.property_type,
                  required: property.required,
                  unique_constraint: property.unique_constraint,
                  time_dependent: property.time_dependent
                },
            warnings: [],
            errors: [],
            source: 'import'
          })
        );
      }
    }

    for (const objectType of snapshot.object_types ?? []) {
      const existing = objectTypes.find((item) => item.id === objectType.id || item.name === objectType.name);
      importedChanges.push(
        buildChange({
          kind: 'object_type',
          action: existing ? 'update' : 'create',
          label: objectType.display_name,
          description: 'Imported object type metadata',
          targetId: existing?.id,
          payload: existing
            ? {
                display_name: objectType.display_name,
                description: objectType.description,
                icon: objectType.icon ?? '',
                color: objectType.color ?? ''
              }
            : {
                name: objectType.name,
                display_name: objectType.display_name,
                description: objectType.description,
                icon: objectType.icon ?? undefined,
                color: objectType.color ?? undefined
              },
          warnings: [],
          errors: [],
          source: 'import'
        })
      );

      for (const property of objectType.properties ?? []) {
        importedChanges.push(
          buildChange({
            kind: 'property',
            action: 'create',
            label: property.display_name,
            description: 'Imported object property',
            parentRef: { kind: 'object_type', id: existing?.id, name: objectType.name },
            payload: {
              name: property.name,
              display_name: property.display_name,
              description: property.description,
              property_type: property.property_type,
              required: property.required,
              unique_constraint: property.unique_constraint,
              time_dependent: property.time_dependent
            },
            warnings: [],
            errors: [],
            source: 'import'
          })
        );
      }

      for (const sharedProperty of objectType.shared_properties ?? []) {
        importedChanges.push(
          buildChange({
            kind: 'type_shared_property',
            action: 'attach',
            label: `${objectType.display_name} -> ${sharedProperty.display_name}`,
            description: 'Imported shared property attachment',
            parentRef: { kind: 'object_type', id: existing?.id, name: objectType.name },
            payload: {
              shared_property_type_id: sharedProperty.id,
              shared_property_type_name: sharedProperty.name
            },
            warnings: [],
            errors: [],
            source: 'import'
          })
        );
      }

      for (const ontologyInterface of objectType.interfaces ?? []) {
        importedChanges.push(
          buildChange({
            kind: 'type_interface',
            action: 'attach',
            label: `${objectType.display_name} -> ${ontologyInterface.display_name}`,
            description: 'Imported interface attachment',
            parentRef: { kind: 'object_type', id: existing?.id, name: objectType.name },
            payload: {
              interface_id: ontologyInterface.id,
              interface_name: ontologyInterface.name
            },
            warnings: [],
            errors: [],
            source: 'import'
          })
        );
      }
    }

    for (const linkType of snapshot.link_types ?? []) {
      const existing = linkTypes.find((item) => item.id === linkType.id || item.name === linkType.name);
      if (!existing) {
        const source = objectTypes.find((item) => item.id === linkType.source_type_id)?.name ?? snapshot.object_types.find((item) => item.id === linkType.source_type_id)?.name;
        const target = objectTypes.find((item) => item.id === linkType.target_type_id)?.name ?? snapshot.object_types.find((item) => item.id === linkType.target_type_id)?.name;
        importedChanges.push(
          buildChange({
            kind: 'link_type',
            action: 'create',
            label: linkType.display_name,
            description: 'Imported link type',
            payload: {
              name: linkType.name,
              display_name: linkType.display_name,
              description: linkType.description,
              source_type_id: linkType.source_type_id,
              target_type_id: linkType.target_type_id,
              source_type_name: source,
              target_type_name: target,
              cardinality: linkType.cardinality
            },
            warnings: [],
            errors: [],
            source: 'import'
          })
        );
      }
    }

    for (const project of snapshot.projects ?? []) {
      const existing = projects.find((item) => item.id === project.id || item.slug === project.slug);
      importedChanges.push(
        buildChange({
          kind: 'project',
          action: existing ? 'update' : 'create',
          label: project.display_name,
          description: 'Imported ontology project',
          targetId: existing?.id,
          payload: existing
            ? {
                display_name: project.display_name,
                description: project.description,
                workspace_slug: project.workspace_slug
              }
            : {
                slug: project.slug,
                display_name: project.display_name,
                description: project.description,
                workspace_slug: project.workspace_slug ?? undefined
              },
          warnings: [],
          errors: [],
          source: 'import'
        })
      );

      for (const membership of project.memberships ?? []) {
        importedChanges.push(
          buildChange({
            kind: 'project_membership',
            action: 'upsert',
            label: `${project.display_name} -> ${membership.user_id}`,
            description: 'Imported project membership',
            parentRef: { kind: 'project', id: existing?.id, slug: project.slug },
            payload: {
              user_id: membership.user_id,
              role: membership.role
            },
            warnings: [],
            errors: [],
            source: 'import'
          })
        );
      }

      for (const resource of project.resources ?? []) {
        importedChanges.push(
          buildChange({
            kind: 'project_resource',
            action: 'attach',
            label: `${project.display_name} -> ${resource.resource_kind}`,
            description: 'Imported project-scoped binding',
            parentRef: { kind: 'project', id: existing?.id, slug: project.slug },
            payload: {
              resource_kind: resource.resource_kind,
              resource_id: resource.resource_id
            },
            warnings: [],
            errors: [],
            source: 'import'
          })
        );
      }
    }

    changeQueue = [...importedChanges, ...changeQueue];
    void persistWorkingState();
    importSummary = `Imported ${importedChanges.length} working-state edits from JSON.`;
    activeSection = 'changes';
  }

  async function handleImportFile(event: Event) {
    const target = event.currentTarget as HTMLInputElement | null;
    const file = target?.files?.[0];
    if (!file) return;
    importError = '';
    importSummary = '';

    try {
      importText = await file.text();
      handleImportText();
    } catch (cause) {
      importError = cause instanceof Error ? cause.message : 'Failed to read ontology file';
    } finally {
      if (target) target.value = '';
    }
  }

  function handleImportText() {
    importError = '';
    importSummary = '';
    try {
      const parsed = JSON.parse(importText) as ExportedOntologySnapshot;
      if (!parsed.metadata || !Array.isArray(parsed.object_types)) {
        importError = 'This JSON file does not look like an OpenFoundry ontology export.';
        return;
      }
      stageImportedSnapshot(parsed);
    } catch (cause) {
      importError = cause instanceof Error ? cause.message : 'Failed to parse import JSON';
    }
  }

  async function buildExportSnapshot(): Promise<ExportedOntologySnapshot> {
    const objectTypeDetails = await Promise.all(
      objectTypes.map(async (typeItem) => {
        const [properties, sharedProps, interfacesForType] = await Promise.all([
          listProperties(typeItem.id),
          listTypeSharedPropertyTypes(typeItem.id),
          listTypeInterfaces(typeItem.id)
        ]);
        return {
          id: typeItem.id,
          name: typeItem.name,
          display_name: typeItem.display_name,
          description: typeItem.description,
          icon: typeItem.icon,
          color: typeItem.color,
          properties,
          shared_properties: sharedProps.map((item) => ({ id: item.id, name: item.name, display_name: item.display_name })),
          interfaces: interfacesForType.map((item) => ({ id: item.id, name: item.name, display_name: item.display_name }))
        };
      })
    );

    const interfaceDetails = await Promise.all(
      interfaces.map(async (ontologyInterface) => ({
        ...ontologyInterface,
        properties: await listInterfaceProperties(ontologyInterface.id)
      }))
    );

    const projectDetails = await Promise.all(
      projects.map(async (project) => ({
        ...project,
        memberships: await listProjectMemberships(project.id),
        resources: await listProjectResources(project.id)
      }))
    );

    return {
      metadata: {
        version: 'openfoundry-ontology-manager.v1',
        exported_at: new Date().toISOString(),
        branch: activeBranch,
        staged_change_count: changeQueue.length
      },
      object_types: objectTypeDetails,
      shared_property_types: sharedPropertyTypes,
      interfaces: interfaceDetails,
      link_types: linkTypes,
      projects: projectDetails,
      working_changes: changeQueue
    };
  }

  async function exportOntology() {
    if (!browser) return;
    exportBusy = true;
    importError = '';
    try {
      const snapshot = await buildExportSnapshot();
      const blob = new Blob([JSON.stringify(snapshot, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement('a');
      anchor.href = url;
      anchor.download = `openfoundry-ontology-${new Date().toISOString().slice(0, 10)}.json`;
      anchor.click();
      URL.revokeObjectURL(url);
      importSummary = 'Ontology working state exported to JSON.';
    } catch (cause) {
      importError = cause instanceof Error ? cause.message : 'Failed to export ontology';
    } finally {
      exportBusy = false;
    }
  }

  function resolveParentRef(ref: ResourceRef | undefined, typeIdsByName: Map<string, string>, interfaceIdsByName: Map<string, string>, projectIdsBySlug: Map<string, string>) {
    if (!ref) return null;
    if (ref.kind === 'object_type') {
      if (ref.id && objectTypeMap.has(ref.id)) return ref.id;
      if (ref.name && typeIdsByName.has(ref.name)) return typeIdsByName.get(ref.name) ?? null;
      return null;
    }
    if (ref.kind === 'interface') {
      if (ref.id && interfaceMap.has(ref.id)) return ref.id;
      if (ref.name && interfaceIdsByName.has(ref.name)) return interfaceIdsByName.get(ref.name) ?? null;
      return null;
    }
    if (ref.kind === 'project') {
      if (ref.id && projectMap.has(ref.id)) return ref.id;
      if (ref.slug && projectIdsBySlug.has(ref.slug)) return projectIdsBySlug.get(ref.slug) ?? null;
      return null;
    }
    return null;
  }

  async function applyChanges() {
    saving = true;
    saveError = '';
    saveSuccess = '';

    const blockingErrors = changeQueue.filter((change) => change.errors.length > 0);
    if (blockingErrors.length > 0) {
      saving = false;
      saveError = 'Resolve Review edits errors before saving the working state.';
      return;
    }

    try {
      const typeIdsByName = new Map<string, string>(objectTypes.map((item) => [item.name, item.id]));
      const interfaceIdsByName = new Map<string, string>(interfaces.map((item) => [item.name, item.id]));
      const sharedPropertyIdsByName = new Map<string, string>(sharedPropertyTypes.map((item) => [item.name, item.id]));
      const projectIdsBySlug = new Map<string, string>(projects.map((item) => [item.slug, item.id]));

      const priority: Record<ChangeKind, number> = {
        object_type: 1,
        shared_property_type: 2,
        interface: 3,
        property: 4,
        interface_property: 5,
        type_shared_property: 6,
        type_interface: 7,
        link_type: 8,
        project: 9,
        project_membership: 10,
        project_resource: 11
      };

      const orderedChanges = [...changeQueue].sort((left, right) => priority[left.kind] - priority[right.kind]);

      for (const change of orderedChanges) {
        if (change.kind === 'object_type') {
          if (change.action === 'create') {
            const created = await createObjectType(change.payload as {
              name: string;
              display_name?: string;
              description?: string;
              icon?: string;
              color?: string;
            });
            if (typeof change.payload.name === 'string') typeIdsByName.set(change.payload.name, created.id);
          } else if (change.action === 'update' && change.targetId) {
            await updateObjectType(change.targetId, change.payload as {
              display_name?: string;
              description?: string;
              icon?: string;
              color?: string;
            });
          }
          continue;
        }

        if (change.kind === 'shared_property_type' && change.action === 'create') {
          const created = await createSharedPropertyType(change.payload as {
            name: string;
            display_name?: string;
            description?: string;
            property_type: string;
            required?: boolean;
            unique_constraint?: boolean;
            time_dependent?: boolean;
          });
          if (typeof change.payload.name === 'string') sharedPropertyIdsByName.set(change.payload.name, created.id);
          continue;
        }

        if (change.kind === 'interface') {
          if (change.action === 'create') {
            const created = await createInterface(change.payload as {
              name: string;
              display_name?: string;
              description?: string;
            });
            if (typeof change.payload.name === 'string') interfaceIdsByName.set(change.payload.name, created.id);
          } else if (change.action === 'update' && change.targetId) {
            await updateInterface(change.targetId, change.payload as { display_name?: string; description?: string });
          }
          continue;
        }

        if (change.kind === 'property') {
          const typeId = resolveParentRef(change.parentRef, typeIdsByName, interfaceIdsByName, projectIdsBySlug);
          if (!typeId) throw new Error(`Missing object type target for ${change.label}`);
          if (change.action === 'create') {
            await createProperty(typeId, change.payload as {
              name: string;
              display_name?: string;
              description?: string;
              property_type: string;
              required?: boolean;
              unique_constraint?: boolean;
              time_dependent?: boolean;
            });
          } else if (change.action === 'update' && change.targetId) {
            await updateProperty(typeId, change.targetId, change.payload as {
              display_name?: string;
              description?: string;
              required?: boolean;
              unique_constraint?: boolean;
              time_dependent?: boolean;
            });
          }
          continue;
        }

        if (change.kind === 'interface_property') {
          const interfaceId = resolveParentRef(change.parentRef, typeIdsByName, interfaceIdsByName, projectIdsBySlug);
          if (!interfaceId) throw new Error(`Missing interface target for ${change.label}`);
          if (change.action === 'create') {
            await createInterfaceProperty(interfaceId, change.payload as {
              name: string;
              display_name?: string;
              description?: string;
              property_type: string;
              required?: boolean;
              unique_constraint?: boolean;
              time_dependent?: boolean;
            });
          } else if (change.action === 'update' && change.targetId) {
            await updateInterfaceProperty(interfaceId, change.targetId, change.payload as {
              display_name?: string;
              description?: string;
              required?: boolean;
              unique_constraint?: boolean;
              time_dependent?: boolean;
            });
          }
          continue;
        }

        if (change.kind === 'type_shared_property') {
          const typeId = resolveParentRef(change.parentRef, typeIdsByName, interfaceIdsByName, projectIdsBySlug);
          if (!typeId) throw new Error(`Missing object type target for ${change.label}`);
          const sharedId =
            (typeof change.payload.shared_property_type_id === 'string' && sharedPropertyMap.has(change.payload.shared_property_type_id) && change.payload.shared_property_type_id) ||
            (typeof change.payload.shared_property_type_name === 'string' ? sharedPropertyIdsByName.get(change.payload.shared_property_type_name) : null);
          if (!sharedId) throw new Error(`Missing shared property target for ${change.label}`);
          if (change.action === 'attach') await attachSharedPropertyType(typeId, sharedId);
          if (change.action === 'detach') await detachSharedPropertyType(typeId, sharedId);
          continue;
        }

        if (change.kind === 'type_interface') {
          const typeId = resolveParentRef(change.parentRef, typeIdsByName, interfaceIdsByName, projectIdsBySlug);
          if (!typeId) throw new Error(`Missing object type target for ${change.label}`);
          const interfaceId =
            (typeof change.payload.interface_id === 'string' && interfaceMap.has(change.payload.interface_id) && change.payload.interface_id) ||
            (typeof change.payload.interface_name === 'string' ? interfaceIdsByName.get(change.payload.interface_name) : null);
          if (!interfaceId) throw new Error(`Missing interface target for ${change.label}`);
          if (change.action === 'attach') await attachInterfaceToType(typeId, interfaceId);
          if (change.action === 'detach') await detachInterfaceFromType(typeId, interfaceId);
          continue;
        }

        if (change.kind === 'link_type' && change.action === 'create') {
          const sourceTypeId =
            (typeof change.payload.source_type_id === 'string' && objectTypeMap.has(change.payload.source_type_id) && change.payload.source_type_id) ||
            (typeof change.payload.source_type_name === 'string' ? typeIdsByName.get(change.payload.source_type_name) : null);
          const targetTypeId =
            (typeof change.payload.target_type_id === 'string' && objectTypeMap.has(change.payload.target_type_id) && change.payload.target_type_id) ||
            (typeof change.payload.target_type_name === 'string' ? typeIdsByName.get(change.payload.target_type_name) : null);
          if (!sourceTypeId || !targetTypeId) throw new Error(`Missing link endpoints for ${change.label}`);
          await createLinkType({
            name: String(change.payload.name ?? ''),
            display_name: String(change.payload.display_name ?? ''),
            description: String(change.payload.description ?? ''),
            source_type_id: sourceTypeId,
            target_type_id: targetTypeId,
            cardinality: String(change.payload.cardinality ?? 'many_to_many')
          });
          continue;
        }

        if (change.kind === 'project') {
          if (change.action === 'create') {
            const created = await createProject(change.payload as {
              slug: string;
              display_name?: string;
              description?: string;
              workspace_slug?: string;
            });
            if (typeof change.payload.slug === 'string') projectIdsBySlug.set(change.payload.slug, created.id);
          } else if (change.action === 'update' && change.targetId) {
            await updateProject(change.targetId, change.payload as {
              display_name?: string;
              description?: string;
              workspace_slug?: string | null;
            });
          }
          continue;
        }

        if (change.kind === 'project_membership' && change.action === 'upsert') {
          const projectId = resolveParentRef(change.parentRef, typeIdsByName, interfaceIdsByName, projectIdsBySlug);
          if (!projectId) throw new Error(`Missing project target for ${change.label}`);
          await upsertProjectMembership(projectId, change.payload as { user_id: string; role: OntologyProjectRole });
          continue;
        }

        if (change.kind === 'project_resource') {
          const projectId = resolveParentRef(change.parentRef, typeIdsByName, interfaceIdsByName, projectIdsBySlug);
          if (!projectId) throw new Error(`Missing project target for ${change.label}`);
          if (change.action === 'attach') {
            await bindProjectResource(projectId, change.payload as { resource_kind: string; resource_id: string });
          }
        }
      }

      clearChanges();
      saveSuccess = 'Ontology working state saved successfully.';
      await loadCatalog();
    } catch (cause) {
      saveError = cause instanceof Error ? cause.message : 'Failed to save ontology working state';
    } finally {
      saving = false;
    }
  }

  async function removeMembership(userId: string) {
    if (!selectedProject) return;
    try {
      await deleteProjectMembership(selectedProject.id, userId);
      await loadSelectedProject(selectedProject.id);
    } catch (cause) {
      saveError = cause instanceof Error ? cause.message : 'Failed to remove membership';
    }
  }

  async function removeResourceBinding(resource: OntologyProjectResourceBinding) {
    if (!selectedProject) return;
    try {
      await unbindProjectResource(selectedProject.id, resource.resource_kind, resource.resource_id);
      await loadSelectedProject(selectedProject.id);
    } catch (cause) {
      saveError = cause instanceof Error ? cause.message : 'Failed to remove resource binding';
    }
  }

  async function detachInterfacePropertyNow(property: InterfaceProperty) {
    if (!selectedInterface) return;
    try {
      await deleteInterfaceProperty(selectedInterface.id, property.id);
      await loadSelectedInterface(selectedInterface.id);
    } catch (cause) {
      saveError = cause instanceof Error ? cause.message : 'Failed to remove interface property';
    }
  }

  async function refreshManager() {
    refreshing = true;
    await loadCatalog();
    refreshing = false;
  }

  async function loadWizardDatasetPreview(datasetId: string) {
    wizardDraft.dataset_id = datasetId;
    wizardError = '';
    wizardSuccess = '';
    datasetPreview = datasetId ? await previewDataset(datasetId, { limit: 20 }).catch(() => null) : null;
    if (!wizardDraft.object_name) {
      const dataset = datasets.find((item) => item.id === datasetId);
      wizardDraft.object_name = normalizeName(dataset?.name ?? '');
      wizardDraft.display_name = dataset?.name ?? '';
      wizardDraft.description = dataset?.description ?? '';
    }
    if (!wizardDraft.primary_key_column && datasetPreview?.columns?.[0]?.name) {
      wizardDraft.primary_key_column = datasetPreview.columns[0].name;
    }
    if (!wizardDraft.title_column && datasetPreview?.columns?.[1]?.name) {
      wizardDraft.title_column = datasetPreview.columns[1].name;
    }
  }

  function inferPropertyType(column?: { field_type?: string; data_type?: string; nullable?: boolean }) {
    const value = (column?.field_type ?? column?.data_type ?? 'string').toLowerCase();
    if (value.includes('int')) return 'integer';
    if (value.includes('float') || value.includes('double') || value.includes('decimal')) return 'float';
    if (value.includes('bool')) return 'boolean';
    if (value.includes('time')) return 'datetime';
    if (value.includes('date')) return 'date';
    if (value.includes('json')) return 'json';
    return 'string';
  }

  async function createObjectFromDatasource() {
    if (!selectedProjectId || !canEditSelectedProject) {
      wizardError = 'Select an ontology where you have edit permissions before creating an object.';
      return;
    }
    if (!wizardDraft.dataset_id) {
      wizardError = 'Select a datasource.';
      return;
    }
    if (!wizardDraft.object_name.trim()) {
      wizardError = 'Object API name is required.';
      return;
    }
    if (!wizardDraft.primary_key_column) {
      wizardError = 'Choose a primary key column.';
      return;
    }

    wizardBusy = true;
    wizardError = '';
    wizardSuccess = '';

    try {
      const createdType = await createObjectType({
        name: normalizeName(wizardDraft.object_name),
        display_name: wizardDraft.display_name.trim() || titleCase(normalizeName(wizardDraft.object_name)),
        description: wizardDraft.description.trim() || undefined,
        primary_key_property: normalizeName(wizardDraft.primary_key_column)
      });

      await bindProjectResource(selectedProjectId, { resource_kind: 'object_type', resource_id: createdType.id });

      const columns = datasetPreview?.columns ?? [];
      for (const column of columns) {
        const propertyName = normalizeName(column.name);
        await createProperty(createdType.id, {
          name: propertyName,
          display_name: titleCase(propertyName),
          description:
            column.name === wizardDraft.primary_key_column
              ? 'Primary key selected in the datasource wizard.'
              : column.name === wizardDraft.title_column
                ? 'Suggested title field from datasource wizard.'
                : undefined,
          property_type: inferPropertyType(column),
          required: propertyName === normalizeName(wizardDraft.primary_key_column) ? true : !(column.nullable ?? true),
          unique_constraint: propertyName === normalizeName(wizardDraft.primary_key_column),
          time_dependent: false
        });
      }

      if (wizardDraft.indexing_enabled) {
        await createOntologyFunnelSource({
          name: `${normalizeName(wizardDraft.object_name)}-source`,
          description: `Datasource-backed indexing flow for ${wizardDraft.display_name || wizardDraft.object_name}.`,
          object_type_id: createdType.id,
          dataset_id: wizardDraft.dataset_id,
          dataset_branch: 'main',
          preview_limit: 500,
          property_mappings: columns.map((column) => ({
            source_field: column.name,
            target_property: normalizeName(column.name)
          }))
        });
      }

      wizardSuccess = 'Object type created from datasource and saved to ontology.';
      wizardStep = 1;
      await loadCatalog();
    } catch (cause) {
      wizardError = cause instanceof Error ? cause.message : 'Failed to create object from datasource';
    } finally {
      wizardBusy = false;
    }
  }

  function titleCase(value: string) {
    return value
      .split('_')
      .filter(Boolean)
      .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
      .join(' ');
  }

  function formatDate(value: string | null | undefined) {
    if (!value) return '—';
    return new Date(value).toLocaleString();
  }

  function objectTypeOptionLabel(id: string) {
    return objectTypeMap.get(id)?.display_name ?? id;
  }

  function scopedResourceOptions() {
    if (resourceBindingKind === 'object_type') return objectTypes.map((item) => ({ id: item.id, label: item.display_name }));
    if (resourceBindingKind === 'link_type') return linkTypes.map((item) => ({ id: item.id, label: item.display_name }));
    if (resourceBindingKind === 'interface') return interfaces.map((item) => ({ id: item.id, label: item.display_name }));
    return sharedPropertyTypes.map((item) => ({ id: item.id, label: item.display_name }));
  }
</script>

<svelte:head>
  <title>Ontology Manager | OpenFoundry</title>
</svelte:head>

<div class="mx-auto flex max-w-[1600px] flex-col gap-6 px-4 pb-10 pt-4 text-slate-900">
  <section class="overflow-hidden rounded-[28px] border border-[#d6dfef] bg-[linear-gradient(135deg,#f5f7fb_0%,#e8effb_48%,#fdf6ec_100%)] shadow-[0_24px_80px_rgba(21,55,110,0.10)]">
    <div class="flex flex-col gap-6 px-6 py-6 lg:flex-row lg:items-end lg:justify-between lg:px-8">
      <div class="max-w-3xl space-y-3">
        <div class="inline-flex items-center gap-2 rounded-full border border-[#bed0ec] bg-white/75 px-3 py-1 text-xs font-semibold uppercase tracking-[0.24em] text-[#315ea8]">
          <Glyph name="ontology" size={16} />
          Ontology Manager
        </div>
        <div>
          <h1 class="text-3xl font-semibold tracking-tight text-[#12233f] md:text-4xl">Build, review, import, and govern the ontology from one working state.</h1>
          <p class="mt-2 max-w-2xl text-sm leading-6 text-slate-600">
            This dedicated app mirrors the product split from the reference suite: discover resources, curate object types and interfaces, stage edits locally,
            review warnings before save, export working state JSON, and manage project-scoped permissions.
          </p>
        </div>
      </div>

      <div class="grid min-w-[320px] gap-3 rounded-[24px] border border-white/70 bg-white/80 p-4 shadow-sm backdrop-blur">
        <div class="flex flex-wrap items-center gap-2">
          <label class="text-xs font-semibold uppercase tracking-[0.2em] text-slate-500" for="ontology-search">Search</label>
          <input
            id="ontology-search"
            bind:value={searchQuery}
            class="min-w-[220px] flex-1 rounded-xl border border-[#d2dcec] bg-white px-3 py-2 text-sm outline-none ring-0 transition focus:border-[#6186c5]"
            placeholder="Search resources, groups, or editors"
          />
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <span class="text-xs font-semibold uppercase tracking-[0.2em] text-slate-500">Access</span>
          <div class="rounded-xl border border-[#d2dcec] bg-white px-3 py-2 text-sm text-slate-700">
            {selectedProjectRole ?? 'viewer'}
          </div>
          <button
            class="rounded-xl border border-[#d2dcec] bg-white px-3 py-2 text-sm font-medium text-slate-700 transition hover:border-[#86a6dc]"
            onclick={refreshManager}
            disabled={refreshing || loading}
          >
            {refreshing ? 'Refreshing…' : 'Refresh'}
          </button>
          <a href="/ontology" class="rounded-xl bg-[#1d4f91] px-3 py-2 text-sm font-medium text-white transition hover:bg-[#163f72]">Ontology hub</a>
        </div>
        <div class="grid grid-cols-3 gap-2">
          <div class="rounded-2xl border border-[#dce5f2] bg-[#f8fbff] px-3 py-3">
            <div class="text-xs uppercase tracking-[0.18em] text-slate-500">Working edits</div>
            <div class="mt-1 text-2xl font-semibold text-[#173d71]">{pendingChanges}</div>
          </div>
          <div class="rounded-2xl border border-[#efe3c8] bg-[#fffaf0] px-3 py-3">
            <div class="text-xs uppercase tracking-[0.18em] text-slate-500">Warnings</div>
            <div class="mt-1 text-2xl font-semibold text-[#9b6612]">{reviewWarnings}</div>
          </div>
          <div class="rounded-2xl border border-[#f0d7d7] bg-[#fff6f6] px-3 py-3">
            <div class="text-xs uppercase tracking-[0.18em] text-slate-500">Errors</div>
            <div class="mt-1 text-2xl font-semibold text-[#b74141]">{reviewErrors}</div>
          </div>
        </div>
      </div>
    </div>
  </section>

  {#if loadError}
    <div class="rounded-2xl border border-[#efc5c5] bg-[#fff5f5] px-4 py-3 text-sm text-[#9b2c2c]">{loadError}</div>
  {/if}
  {#if saveError}
    <div class="rounded-2xl border border-[#efc5c5] bg-[#fff5f5] px-4 py-3 text-sm text-[#9b2c2c]">{saveError}</div>
  {/if}
  {#if saveSuccess}
    <div class="rounded-2xl border border-[#cce3cb] bg-[#f3fbf2] px-4 py-3 text-sm text-[#2e6b33]">{saveSuccess}</div>
  {/if}

  <div class="grid gap-6 lg:grid-cols-[280px_minmax(0,1fr)]">
    <aside class="space-y-4">
      <div class="rounded-[26px] border border-[#dbe4f1] bg-white p-4 shadow-sm">
        <div class="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">Navigation</div>
        <div class="mt-3 space-y-2">
          {#each sectionOptions as section}
            <button
              class={`flex w-full items-center justify-between rounded-2xl px-3 py-3 text-left text-sm transition ${
                activeSection === section.id ? 'bg-[#183d70] text-white shadow-sm' : 'bg-[#f6f8fc] text-slate-700 hover:bg-[#edf2fb]'
              }`}
              onclick={() => (activeSection = section.id)}
            >
              <span class="flex items-center gap-2">
                <Glyph name={section.glyph} size={16} />
                {section.label}
              </span>
              {#if section.id === 'changes' && pendingChanges > 0}
                <span class="rounded-full bg-white/15 px-2 py-0.5 text-xs font-semibold">{pendingChanges}</span>
              {/if}
            </button>
          {/each}
        </div>
      </div>

      {#if activeSection === 'types'}
        <div class="rounded-[26px] border border-[#dbe4f1] bg-white p-4 shadow-sm">
          <div class="flex items-center justify-between">
            <div>
              <div class="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">Object types</div>
              <div class="mt-1 text-sm text-slate-600">{filteredObjectTypes.length} visible</div>
            </div>
          </div>
          <div class="mt-3 space-y-2">
            {#each filteredObjectTypes as typeItem}
              <div
                class={`rounded-2xl border px-3 py-3 transition ${
                  selectedTypeId === typeItem.id ? 'border-[#8eabd8] bg-[#eef4ff]' : 'border-[#e7edf6] bg-[#fbfcfe]'
                }`}
              >
                <div class="flex items-center justify-between gap-2">
                  <button class="min-w-0 flex-1 text-left" onclick={() => selectType(typeItem.id)}>
                    <div class="font-medium text-slate-800">{typeItem.display_name}</div>
                    <div class="mt-1 text-xs text-slate-500">{typeItem.name}</div>
                  </button>
                  <button
                    class={`rounded-full px-2 py-1 text-[11px] font-semibold ${isFavorite('types', typeItem.id) ? 'bg-[#183d70] text-white' : 'bg-[#edf2fb] text-[#315ea8]'}`}
                    onclick={() => toggleFavorite('types', typeItem.id)}
                  >
                    {isFavorite('types', typeItem.id) ? 'Pinned' : 'Pin'}
                  </button>
                </div>
              </div>
            {/each}
          </div>
        </div>
      {/if}

      {#if activeSection === 'interfaces'}
        <div class="rounded-[26px] border border-[#dbe4f1] bg-white p-4 shadow-sm">
          <div class="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">Interfaces</div>
          <div class="mt-3 space-y-2">
            {#each filteredInterfaces as interfaceItem}
              <button
                class={`w-full rounded-2xl border px-3 py-3 text-left transition ${
                  selectedInterfaceId === interfaceItem.id ? 'border-[#8eabd8] bg-[#eef4ff]' : 'border-[#e7edf6] bg-[#fbfcfe] hover:border-[#bfd1ef]'
                }`}
                onclick={() => selectInterface(interfaceItem.id)}
              >
                <div class="font-medium text-slate-800">{interfaceItem.display_name}</div>
                <div class="mt-1 text-xs text-slate-500">{interfaceItem.name}</div>
              </button>
            {/each}
          </div>
        </div>
      {/if}

      {#if activeSection === 'shared'}
        <div class="rounded-[26px] border border-[#dbe4f1] bg-white p-4 shadow-sm">
          <div class="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">Shared properties</div>
          <div class="mt-3 space-y-2">
            {#each filteredSharedProperties as sharedProperty}
              <button
                class={`w-full rounded-2xl border px-3 py-3 text-left transition ${
                  selectedSharedPropertyId === sharedProperty.id ? 'border-[#8eabd8] bg-[#eef4ff]' : 'border-[#e7edf6] bg-[#fbfcfe] hover:border-[#bfd1ef]'
                }`}
                onclick={() => selectSharedProperty(sharedProperty.id)}
              >
                <div class="font-medium text-slate-800">{sharedProperty.display_name}</div>
                <div class="mt-1 text-xs text-slate-500">{sharedProperty.property_type}</div>
              </button>
            {/each}
          </div>
        </div>
      {/if}

      {#if activeSection === 'links'}
        <div class="rounded-[26px] border border-[#dbe4f1] bg-white p-4 shadow-sm">
          <div class="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">Link types</div>
          <div class="mt-3 space-y-2">
            {#each filteredLinks as linkType}
              <button
                class={`w-full rounded-2xl border px-3 py-3 text-left transition ${
                  selectedLinkId === linkType.id ? 'border-[#8eabd8] bg-[#eef4ff]' : 'border-[#e7edf6] bg-[#fbfcfe] hover:border-[#bfd1ef]'
                }`}
                onclick={() => selectLink(linkType.id)}
              >
                <div class="font-medium text-slate-800">{linkType.display_name}</div>
                <div class="mt-1 text-xs text-slate-500">{objectTypeOptionLabel(linkType.source_type_id)} → {objectTypeOptionLabel(linkType.target_type_id)}</div>
              </button>
            {/each}
          </div>
        </div>
      {/if}

      {#if activeSection === 'projects'}
        <div class="rounded-[26px] border border-[#dbe4f1] bg-white p-4 shadow-sm">
          <div class="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">Projects</div>
          <div class="mt-3 space-y-2">
            {#each filteredProjects as project}
              <button
                class={`w-full rounded-2xl border px-3 py-3 text-left transition ${
                  selectedProjectId === project.id ? 'border-[#8eabd8] bg-[#eef4ff]' : 'border-[#e7edf6] bg-[#fbfcfe] hover:border-[#bfd1ef]'
                }`}
                onclick={() => selectProject(project.id)}
              >
                <div class="font-medium text-slate-800">{project.display_name}</div>
                <div class="mt-1 text-xs text-slate-500">{project.slug}</div>
              </button>
            {/each}
          </div>
        </div>
      {/if}
    </aside>

    <main class="space-y-6">
      {#if loading}
        <div class="rounded-[28px] border border-[#dbe4f1] bg-white px-6 py-8 text-sm text-slate-600 shadow-sm">Loading Ontology Manager…</div>
      {:else if activeSection === 'discover'}
        <section class="space-y-6">
          <div class="grid gap-4 md:grid-cols-4">
            <div class="rounded-[24px] border border-[#dce5f2] bg-white p-5 shadow-sm">
              <div class="text-xs uppercase tracking-[0.18em] text-slate-500">Object types</div>
              <div class="mt-2 text-3xl font-semibold text-[#173d71]">{objectTypes.length}</div>
              <p class="mt-2 text-sm text-slate-600">Modeled entities available for views, actions, and search.</p>
            </div>
            <div class="rounded-[24px] border border-[#dce5f2] bg-white p-5 shadow-sm">
              <div class="text-xs uppercase tracking-[0.18em] text-slate-500">Interfaces</div>
              <div class="mt-2 text-3xl font-semibold text-[#173d71]">{interfaces.length}</div>
              <p class="mt-2 text-sm text-slate-600">Reusable schema blocks bound across multiple object types.</p>
            </div>
            <div class="rounded-[24px] border border-[#dce5f2] bg-white p-5 shadow-sm">
              <div class="text-xs uppercase tracking-[0.18em] text-slate-500">Projects</div>
              <div class="mt-2 text-3xl font-semibold text-[#173d71]">{projects.length}</div>
              <p class="mt-2 text-sm text-slate-600">Project-scoped permissions and resource bindings.</p>
            </div>
            <div class="rounded-[24px] border border-[#dce5f2] bg-white p-5 shadow-sm">
              <div class="text-xs uppercase tracking-[0.18em] text-slate-500">Saved lists</div>
              <div class="mt-2 text-3xl font-semibold text-[#173d71]">{objectSets.length}</div>
              <p class="mt-2 text-sm text-slate-600">Existing object sets that can be impacted by schema changes.</p>
            </div>
          </div>

          <div class="grid gap-6 xl:grid-cols-[1.25fr_0.95fr]">
            <div class="space-y-6">
              {#each discoverSections as section}
                <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
                  <div class="flex items-end justify-between gap-4">
                    <div>
                      <h2 class="text-xl font-semibold text-slate-900">{section.title}</h2>
                      <p class="mt-1 text-sm text-slate-600">{section.description}</p>
                    </div>
                  </div>
                  <div class="mt-4 grid gap-3 md:grid-cols-2">
                    {#each section.items as item}
                      <button
                        class="rounded-2xl border border-[#e4ebf5] bg-[#fbfcff] px-4 py-4 text-left transition hover:border-[#bfd1ef]"
                        onclick={() => {
                          activeSection = item.section;
                          if (item.section === 'types') selectType(item.key.split(':')[1]);
                          if (item.section === 'interfaces') selectInterface(item.key.split(':')[1]);
                          if (item.section === 'shared') selectSharedProperty(item.key.split(':')[1]);
                          if (item.section === 'projects') selectProject(item.key.split(':')[1]);
                        }}
                      >
                        <div class="font-medium text-slate-800">{item.label}</div>
                        <div class="mt-1 text-xs uppercase tracking-[0.16em] text-slate-500">{item.section}</div>
                        <div class="mt-3 text-xs text-slate-500">{formatDate(item.seenAt)}</div>
                      </button>
                    {/each}
                  </div>
                </div>
              {/each}
            </div>

            <div class="space-y-6">
              <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
                <h2 class="text-xl font-semibold text-slate-900">Working state</h2>
                <p class="mt-1 text-sm text-slate-600">Save applies queued ontology edits. Discard clears the backend working state for the selected ontology.</p>
                <div class="mt-4 flex flex-wrap gap-3">
                  <button class="rounded-2xl bg-[#183d70] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#14345e] disabled:bg-[#8fa7c7]" onclick={applyChanges} disabled={saving || pendingChanges === 0 || !canEditSelectedProject}>
                    {saving ? 'Saving…' : 'Save changes'}
                  </button>
                  <button class="rounded-2xl border border-[#d2dcec] bg-white px-4 py-3 text-sm font-semibold text-slate-700 transition hover:border-[#9fb8df] disabled:opacity-50" onclick={clearChanges} disabled={pendingChanges === 0 || !canEditSelectedProject}>
                    Discard all
                  </button>
                  <button class="rounded-2xl border border-[#d2dcec] bg-white px-4 py-3 text-sm font-semibold text-slate-700 transition hover:border-[#9fb8df]" onclick={() => (activeSection = 'changes')}>
                    Review edits
                  </button>
                </div>
                {#if !canEditSelectedProject}
                  <p class="mt-3 text-xs text-amber-700">You currently have viewer access. Switch ontology or create/use an editable branch before saving.</p>
                {/if}
              </div>

              <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
                <h2 class="text-xl font-semibold text-slate-900">Create object from datasource</h2>
                <p class="mt-1 text-sm text-slate-600">Use one guided flow to select a datasource, map a primary key, and save a new object type plus indexing source through real backend APIs.</p>
                {#if wizardError}
                  <div class="mt-4 rounded-2xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{wizardError}</div>
                {/if}
                {#if wizardSuccess}
                  <div class="mt-4 rounded-2xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700">{wizardSuccess}</div>
                {/if}
                <div class="mt-4 grid gap-4">
                  <label class="space-y-2 text-sm text-slate-700">
                    <span class="font-medium">Step 1 · Datasource</span>
                    <select class="w-full rounded-2xl border border-[#d2dcec] bg-white px-3 py-2.5" bind:value={wizardDraft.dataset_id} onchange={(event) => void loadWizardDatasetPreview((event.currentTarget as HTMLSelectElement).value)}>
                      <option value="">Select datasource</option>
                      {#each datasets as dataset}
                        <option value={dataset.id}>{dataset.name}</option>
                      {/each}
                    </select>
                  </label>
                  <div class="grid gap-4 md:grid-cols-2">
                    <label class="space-y-2 text-sm text-slate-700">
                      <span class="font-medium">Step 2 · Object API name</span>
                      <input bind:value={wizardDraft.object_name} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5" placeholder="orders" />
                    </label>
                    <label class="space-y-2 text-sm text-slate-700">
                      <span class="font-medium">Display name</span>
                      <input bind:value={wizardDraft.display_name} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5" placeholder="Orders" />
                    </label>
                  </div>
                  <label class="space-y-2 text-sm text-slate-700">
                    <span class="font-medium">Description</span>
                    <textarea bind:value={wizardDraft.description} rows="3" class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5"></textarea>
                  </label>
                  <div class="grid gap-4 md:grid-cols-2">
                    <label class="space-y-2 text-sm text-slate-700">
                      <span class="font-medium">Step 3 · Primary key column</span>
                      <select class="w-full rounded-2xl border border-[#d2dcec] bg-white px-3 py-2.5" bind:value={wizardDraft.primary_key_column}>
                        <option value="">Select primary key</option>
                        {#each datasetPreview?.columns ?? [] as column}
                          <option value={column.name}>{column.name}</option>
                        {/each}
                      </select>
                    </label>
                    <label class="space-y-2 text-sm text-slate-700">
                      <span class="font-medium">Title column</span>
                      <select class="w-full rounded-2xl border border-[#d2dcec] bg-white px-3 py-2.5" bind:value={wizardDraft.title_column}>
                        <option value="">Select title column</option>
                        {#each datasetPreview?.columns ?? [] as column}
                          <option value={column.name}>{column.name}</option>
                        {/each}
                      </select>
                    </label>
                  </div>
                  <label class="flex items-center gap-2 rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm text-slate-700">
                    <input type="checkbox" checked={wizardDraft.indexing_enabled} onchange={(event) => wizardDraft.indexing_enabled = (event.currentTarget as HTMLInputElement).checked} />
                    Enable indexing source immediately
                  </label>
                  {#if datasetPreview?.columns?.length}
                    <div class="rounded-2xl border border-[#dbe4f1] bg-[#fbfcff] p-4">
                      <div class="text-sm font-semibold text-slate-900">Preview columns</div>
                      <div class="mt-3 flex flex-wrap gap-2">
                        {#each datasetPreview.columns as column}
                          <span class="rounded-full bg-[#eef4ff] px-2 py-1 text-xs text-[#315ea8]">{column.name}</span>
                        {/each}
                      </div>
                    </div>
                  {/if}
                  <div class="flex flex-wrap gap-3">
                    <button class="rounded-2xl bg-[#183d70] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#14345e] disabled:bg-[#8fa7c7]" onclick={createObjectFromDatasource} disabled={wizardBusy || !canEditSelectedProject}>
                      {wizardBusy ? 'Creating…' : 'Save to ontology'}
                    </button>
                    <button class="rounded-2xl border border-[#d2dcec] bg-white px-4 py-3 text-sm font-semibold text-slate-700 transition hover:border-[#9fb8df]" onclick={() => (activeSection = 'projects')}>
                      Review ontology permissions
                    </button>
                  </div>
                </div>
              </div>

              <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
                <h2 class="text-xl font-semibold text-slate-900">Cleanup recommendations</h2>
                <p class="mt-1 text-sm text-slate-600">Spot schema areas that may need metadata, grouping, or downstream linking.</p>
                <div class="mt-4 space-y-3">
                  {#each staleTypeCandidates as candidate}
                    <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4">
                      <div class="font-medium text-slate-800">{candidate.label}</div>
                      <div class="mt-2 flex flex-wrap gap-2">
                        {#each candidate.issues as issue}
                          <span class="rounded-full bg-[#eef4ff] px-2 py-1 text-xs font-semibold text-[#315ea8]">{issue}</span>
                        {/each}
                      </div>
                    </div>
                  {/each}
                </div>
              </div>
            </div>
          </div>
        </section>
      {:else if activeSection === 'types' && selectedType}
        <section class="space-y-6">
          <div class="grid gap-6 xl:grid-cols-[1.2fr_0.85fr]">
            <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
              <div class="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <div class="text-xs font-semibold uppercase tracking-[0.2em] text-slate-500">Object type overview</div>
                  <h2 class="mt-2 text-2xl font-semibold text-slate-900">{selectedType.display_name}</h2>
                  <p class="mt-1 text-sm text-slate-600">{selectedType.name}</p>
                </div>
                <button
                  class={`rounded-full px-3 py-2 text-sm font-semibold ${isFavorite('types', selectedType.id) ? 'bg-[#183d70] text-white' : 'bg-[#eef4ff] text-[#315ea8]'}`}
                  onclick={() => toggleFavorite('types', selectedType.id)}
                >
                  {isFavorite('types', selectedType.id) ? 'Pinned in Discover' : 'Pin in Discover'}
                </button>
              </div>

              <div class="mt-6 grid gap-4 md:grid-cols-2">
                <label class="space-y-2 text-sm">
                  <span class="font-medium text-slate-700">Display name</span>
                  <input bind:value={typeDraft.display_name} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5" />
                </label>
                <label class="space-y-2 text-sm">
                  <span class="font-medium text-slate-700">Icon</span>
                  <input bind:value={typeDraft.icon} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5" placeholder="plane, facility, alert" />
                </label>
                <label class="space-y-2 text-sm">
                  <span class="font-medium text-slate-700">Color</span>
                  <input bind:value={typeDraft.color} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5" placeholder="#2458b8" />
                </label>
                <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-3 text-sm text-slate-600">
                  <div class="font-medium text-slate-800">Usage and safety</div>
                  <div class="mt-2">Objects: {selectedTypeObjectCount}</div>
                  <div>Properties: {selectedTypeProperties.length}</div>
                  <div>Shared properties: {selectedTypeSharedProperties.length}</div>
                  <div>Interfaces: {selectedTypeInterfaces.length}</div>
                </div>
                <label class="space-y-2 text-sm md:col-span-2">
                  <span class="font-medium text-slate-700">Description</span>
                  <textarea bind:value={typeDraft.description} class="min-h-[110px] w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5"></textarea>
                </label>
              </div>

              <div class="mt-4 flex flex-wrap gap-3">
                <button class="rounded-2xl bg-[#183d70] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#14345e]" onclick={stageTypeUpdate}>Stage metadata edit</button>
                <a href={`/ontology/${selectedType.id}`} class="rounded-2xl border border-[#d2dcec] bg-white px-4 py-3 text-sm font-semibold text-slate-700 transition hover:border-[#9fb8df]">Open object workbench</a>
                <a href="/object-views" class="rounded-2xl border border-[#d2dcec] bg-white px-4 py-3 text-sm font-semibold text-slate-700 transition hover:border-[#9fb8df]">Configured views</a>
              </div>
            </div>

            <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
              <h2 class="text-xl font-semibold text-slate-900">Create object type</h2>
              <p class="mt-1 text-sm text-slate-600">Queue a new object type in the local working state before publishing.</p>
              <div class="mt-4 space-y-3">
                <input bind:value={createTypeDraft.name} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="API name" />
                <input bind:value={createTypeDraft.display_name} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Display name" />
                <input bind:value={createTypeDraft.icon} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Icon" />
                <input bind:value={createTypeDraft.color} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Color" />
                <textarea bind:value={createTypeDraft.description} class="min-h-[90px] w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Description"></textarea>
                <button class="w-full rounded-2xl bg-[#2458b8] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#1d4f91]" onclick={stageTypeCreate}>Stage new object type</button>
              </div>
            </div>
          </div>

          <div class="grid gap-6 xl:grid-cols-[1.1fr_0.95fr]">
            <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
              <div class="flex items-end justify-between gap-4">
                <div>
                  <h2 class="text-xl font-semibold text-slate-900">Properties</h2>
                  <p class="mt-1 text-sm text-slate-600">Manage the canonical schema, then send edits to review.</p>
                </div>
                <div class="text-sm text-slate-500">{selectedTypeProperties.length} total</div>
              </div>

              <div class="mt-4 space-y-3">
                {#each selectedTypeProperties as property}
                  <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4">
                    <div class="flex flex-wrap items-start justify-between gap-3">
                      <div>
                        <div class="font-medium text-slate-800">{property.display_name}</div>
                        <div class="mt-1 text-xs text-slate-500">{property.name} · {property.property_type}</div>
                      </div>
                      <button class="rounded-xl border border-[#d2dcec] bg-white px-3 py-2 text-xs font-semibold text-slate-700 transition hover:border-[#9fb8df]" onclick={() => stagePropertyUpdate(property)}>
                        Stage metadata refresh
                      </button>
                    </div>
                    <div class="mt-3 flex flex-wrap gap-2">
                      {#if property.required}<span class="rounded-full bg-[#eef5e8] px-2 py-1 text-xs font-semibold text-[#356b3c]">required</span>{/if}
                      {#if property.unique_constraint}<span class="rounded-full bg-[#fff1df] px-2 py-1 text-xs font-semibold text-[#a35a11]">unique</span>{/if}
                      {#if property.time_dependent}<span class="rounded-full bg-[#edf4ff] px-2 py-1 text-xs font-semibold text-[#2458b8]">time-dependent</span>{/if}
                    </div>
                  </div>
                {/each}
              </div>

              <div class="mt-6 grid gap-3 rounded-[24px] border border-dashed border-[#bfd1ef] bg-[#f8fbff] p-4 md:grid-cols-2">
                <input bind:value={propertyDraft.name} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Property API name" />
                <input bind:value={propertyDraft.display_name} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Display name" />
                <select bind:value={propertyDraft.property_type} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm">
                  {#each propertyTypeOptions as option}
                    <option value={option}>{option}</option>
                  {/each}
                </select>
                <div class="flex items-center gap-4 rounded-2xl border border-[#d2dcec] bg-white px-3 py-2.5 text-sm">
                  <label class="flex items-center gap-2"><input type="checkbox" bind:checked={propertyDraft.required} /> required</label>
                  <label class="flex items-center gap-2"><input type="checkbox" bind:checked={propertyDraft.unique_constraint} /> unique</label>
                </div>
                <label class="md:col-span-2">
                  <textarea bind:value={propertyDraft.description} class="min-h-[90px] w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Property description"></textarea>
                </label>
                <button class="md:col-span-2 rounded-2xl bg-[#2458b8] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#1d4f91]" onclick={stagePropertyCreate}>Stage new property</button>
              </div>
            </div>

            <div class="space-y-6">
              <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
                <h2 class="text-xl font-semibold text-slate-900">Shared properties</h2>
                <p class="mt-1 text-sm text-slate-600">Attach or detach reusable property definitions from the type.</p>
                <div class="mt-4 space-y-3">
                  {#each sharedPropertyTypes as sharedProperty}
                    <button
                      class={`flex w-full items-center justify-between rounded-2xl border px-4 py-3 text-left transition ${
                        selectedTypeSharedProperties.some((item) => item.id === sharedProperty.id) ? 'border-[#9bc1a2] bg-[#f4fbf3]' : 'border-[#e7edf6] bg-[#fbfcff] hover:border-[#bfd1ef]'
                      }`}
                      onclick={() => stageSharedPropertyAttach(sharedProperty)}
                    >
                      <span>
                        <span class="block font-medium text-slate-800">{sharedProperty.display_name}</span>
                        <span class="mt-1 block text-xs text-slate-500">{sharedProperty.property_type}</span>
                      </span>
                      <span class="rounded-full px-2 py-1 text-xs font-semibold text-slate-700">
                        {selectedTypeSharedProperties.some((item) => item.id === sharedProperty.id) ? 'Detach' : 'Attach'}
                      </span>
                    </button>
                  {/each}
                </div>
              </div>

              <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
                <h2 class="text-xl font-semibold text-slate-900">Interfaces</h2>
                <p class="mt-1 text-sm text-slate-600">Bind standardized interface contracts onto this object type.</p>
                <div class="mt-4 space-y-3">
                  {#each interfaces as interfaceItem}
                    <button
                      class={`flex w-full items-center justify-between rounded-2xl border px-4 py-3 text-left transition ${
                        selectedTypeInterfaces.some((item) => item.id === interfaceItem.id) ? 'border-[#9bc1a2] bg-[#f4fbf3]' : 'border-[#e7edf6] bg-[#fbfcff] hover:border-[#bfd1ef]'
                      }`}
                      onclick={() => stageTypeInterfaceToggle(interfaceItem)}
                    >
                      <span>
                        <span class="block font-medium text-slate-800">{interfaceItem.display_name}</span>
                        <span class="mt-1 block text-xs text-slate-500">{interfaceItem.name}</span>
                      </span>
                      <span class="rounded-full px-2 py-1 text-xs font-semibold text-slate-700">
                        {selectedTypeInterfaces.some((item) => item.id === interfaceItem.id) ? 'Detach' : 'Attach'}
                      </span>
                    </button>
                  {/each}
                </div>
              </div>
            </div>
          </div>
        </section>
      {:else if activeSection === 'interfaces' && selectedInterface}
        <section class="grid gap-6 xl:grid-cols-[1.15fr_0.9fr]">
          <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
            <div class="text-xs font-semibold uppercase tracking-[0.2em] text-slate-500">Interface editor</div>
            <h2 class="mt-2 text-2xl font-semibold text-slate-900">{selectedInterface.display_name}</h2>
            <div class="mt-4 grid gap-4 md:grid-cols-2">
              <label class="space-y-2 text-sm md:col-span-2">
                <span class="font-medium text-slate-700">Display name</span>
                <input bind:value={interfaceDraft.display_name} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5" />
              </label>
              <label class="space-y-2 text-sm md:col-span-2">
                <span class="font-medium text-slate-700">Description</span>
                <textarea bind:value={interfaceDraft.description} class="min-h-[110px] w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5"></textarea>
              </label>
            </div>
            <div class="mt-4 flex gap-3">
              <button class="rounded-2xl bg-[#183d70] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#14345e]" onclick={stageInterfaceUpdate}>Stage interface edit</button>
              <button class="rounded-2xl border border-[#d2dcec] bg-white px-4 py-3 text-sm font-semibold text-slate-700 transition hover:border-[#9fb8df]" onclick={() => toggleFavorite('interfaces', selectedInterface.id)}>
                {isFavorite('interfaces', selectedInterface.id) ? 'Unpin' : 'Pin'}
              </button>
            </div>

            <div class="mt-6">
              <div class="flex items-end justify-between gap-4">
                <div>
                  <h3 class="text-lg font-semibold text-slate-900">Interface properties</h3>
                  <p class="mt-1 text-sm text-slate-600">Review and evolve the shared schema block.</p>
                </div>
                <div class="text-sm text-slate-500">{selectedInterfaceProperties.length} total</div>
              </div>

              <div class="mt-4 space-y-3">
                {#each selectedInterfaceProperties as property}
                  <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4">
                    <div class="flex flex-wrap items-start justify-between gap-3">
                      <div>
                        <div class="font-medium text-slate-800">{property.display_name}</div>
                        <div class="mt-1 text-xs text-slate-500">{property.name} · {property.property_type}</div>
                      </div>
                      <div class="flex gap-2">
                        <button class="rounded-xl border border-[#d2dcec] bg-white px-3 py-2 text-xs font-semibold text-slate-700 transition hover:border-[#9fb8df]" onclick={() => stageInterfacePropertyUpdate(property)}>
                          Stage edit
                        </button>
                        <button class="rounded-xl border border-[#efc5c5] bg-white px-3 py-2 text-xs font-semibold text-[#9b2c2c] transition hover:border-[#d99]" onclick={() => detachInterfacePropertyNow(property)}>
                          Delete now
                        </button>
                      </div>
                    </div>
                  </div>
                {/each}
              </div>
            </div>
          </div>

          <div class="space-y-6">
            <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
              <h2 class="text-xl font-semibold text-slate-900">Create interface</h2>
              <div class="mt-4 space-y-3">
                <input bind:value={createInterfaceDraft.name} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Interface API name" />
                <input bind:value={createInterfaceDraft.display_name} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Display name" />
                <textarea bind:value={createInterfaceDraft.description} class="min-h-[90px] w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Description"></textarea>
                <button class="w-full rounded-2xl bg-[#2458b8] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#1d4f91]" onclick={stageInterfaceCreate}>Stage new interface</button>
              </div>
            </div>

            <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
              <h2 class="text-xl font-semibold text-slate-900">Create interface property</h2>
              <div class="mt-4 grid gap-3">
                <input bind:value={interfacePropertyDraft.name} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Property API name" />
                <input bind:value={interfacePropertyDraft.display_name} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Display name" />
                <select bind:value={interfacePropertyDraft.property_type} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm">
                  {#each propertyTypeOptions as option}
                    <option value={option}>{option}</option>
                  {/each}
                </select>
                <label class="flex items-center gap-2 text-sm text-slate-700"><input type="checkbox" bind:checked={interfacePropertyDraft.required} /> required</label>
                <textarea bind:value={interfacePropertyDraft.description} class="min-h-[90px] w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Description"></textarea>
                <button class="rounded-2xl bg-[#2458b8] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#1d4f91]" onclick={stageInterfacePropertyCreate}>Stage interface property</button>
              </div>
            </div>
          </div>
        </section>
      {:else if activeSection === 'shared'}
        <section class="grid gap-6 xl:grid-cols-[1.1fr_0.92fr]">
          <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
            <div class="text-xs font-semibold uppercase tracking-[0.2em] text-slate-500">Shared property catalog</div>
            <h2 class="mt-2 text-2xl font-semibold text-slate-900">{selectedSharedProperty?.display_name ?? 'Shared properties'}</h2>
            {#if selectedSharedProperty}
              <div class="mt-4 grid gap-4 md:grid-cols-2">
                <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4">
                  <div class="text-xs uppercase tracking-[0.16em] text-slate-500">API name</div>
                  <div class="mt-2 font-medium text-slate-800">{selectedSharedProperty.name}</div>
                </div>
                <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4">
                  <div class="text-xs uppercase tracking-[0.16em] text-slate-500">Property type</div>
                  <div class="mt-2 font-medium text-slate-800">{selectedSharedProperty.property_type}</div>
                </div>
                <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4 md:col-span-2">
                  <div class="text-xs uppercase tracking-[0.16em] text-slate-500">Description</div>
                  <div class="mt-2 text-sm text-slate-700">{selectedSharedProperty.description || 'No description yet.'}</div>
                </div>
              </div>
            {/if}

            <div class="mt-6">
              <h3 class="text-lg font-semibold text-slate-900">Attach coverage</h3>
              <p class="mt-1 text-sm text-slate-600">Use object type view to stage attach and detach operations from the working state.</p>
              <div class="mt-4 grid gap-3 md:grid-cols-2">
                {#each objectTypes.filter((typeItem) => selectedSharedProperty ? typeItem.id === selectedTypeId || selectedTypeSharedProperties.some((item) => item.id === selectedSharedProperty.id) : true).slice(0, 6) as typeItem}
                  <a href="/ontology-manager" class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4 text-left transition hover:border-[#bfd1ef]" onclick={() => selectType(typeItem.id)}>
                    <div class="font-medium text-slate-800">{typeItem.display_name}</div>
                    <div class="mt-1 text-xs text-slate-500">{typeItem.name}</div>
                  </a>
                {/each}
              </div>
            </div>
          </div>

          <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
            <h2 class="text-xl font-semibold text-slate-900">Create shared property type</h2>
            <p class="mt-1 text-sm text-slate-600">Add reusable schema blocks to the ontology working state.</p>
            <div class="mt-4 grid gap-3">
              <input bind:value={sharedPropertyDraft.name} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="API name" />
              <input bind:value={sharedPropertyDraft.display_name} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Display name" />
              <select bind:value={sharedPropertyDraft.property_type} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm">
                {#each propertyTypeOptions as option}
                  <option value={option}>{option}</option>
                {/each}
              </select>
              <label class="flex items-center gap-2 text-sm text-slate-700"><input type="checkbox" bind:checked={sharedPropertyDraft.required} /> required</label>
              <label class="flex items-center gap-2 text-sm text-slate-700"><input type="checkbox" bind:checked={sharedPropertyDraft.unique_constraint} /> unique</label>
              <textarea bind:value={sharedPropertyDraft.description} class="min-h-[90px] w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Description"></textarea>
              <button class="rounded-2xl bg-[#2458b8] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#1d4f91]" onclick={stageSharedPropertyCreate}>Stage shared property</button>
            </div>
          </div>
        </section>
      {:else if activeSection === 'links'}
        <section class="grid gap-6 xl:grid-cols-[1fr_0.96fr]">
          <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
            <div class="text-xs font-semibold uppercase tracking-[0.2em] text-slate-500">Link type view</div>
            <h2 class="mt-2 text-2xl font-semibold text-slate-900">{selectedLink?.display_name ?? 'Link types'}</h2>
            {#if selectedLink}
              <div class="mt-4 grid gap-4 md:grid-cols-2">
                <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4">
                  <div class="text-xs uppercase tracking-[0.16em] text-slate-500">Source</div>
                  <div class="mt-2 font-medium text-slate-800">{objectTypeOptionLabel(selectedLink.source_type_id)}</div>
                </div>
                <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4">
                  <div class="text-xs uppercase tracking-[0.16em] text-slate-500">Target</div>
                  <div class="mt-2 font-medium text-slate-800">{objectTypeOptionLabel(selectedLink.target_type_id)}</div>
                </div>
                <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4">
                  <div class="text-xs uppercase tracking-[0.16em] text-slate-500">Cardinality</div>
                  <div class="mt-2 font-medium text-slate-800">{selectedLink.cardinality}</div>
                </div>
                <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4">
                  <div class="text-xs uppercase tracking-[0.16em] text-slate-500">Updated</div>
                  <div class="mt-2 font-medium text-slate-800">{formatDate(selectedLink.updated_at)}</div>
                </div>
              </div>
              <p class="mt-4 text-sm text-slate-600">{selectedLink.description || 'No description yet.'}</p>
            {/if}
          </div>

          <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
            <h2 class="text-xl font-semibold text-slate-900">Create link type</h2>
            <p class="mt-1 text-sm text-slate-600">Stage new relationships between object types and publish them through review.</p>
            <div class="mt-4 grid gap-3">
              <input bind:value={linkDraft.name} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Link API name" />
              <input bind:value={linkDraft.display_name} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Display name" />
              <select bind:value={linkDraft.source_type_id} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm">
                {#each objectTypes as typeItem}
                  <option value={typeItem.id}>{typeItem.display_name}</option>
                {/each}
              </select>
              <select bind:value={linkDraft.target_type_id} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm">
                {#each objectTypes as typeItem}
                  <option value={typeItem.id}>{typeItem.display_name}</option>
                {/each}
              </select>
              <select bind:value={linkDraft.cardinality} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm">
                <option value="one_to_one">one_to_one</option>
                <option value="one_to_many">one_to_many</option>
                <option value="many_to_one">many_to_one</option>
                <option value="many_to_many">many_to_many</option>
              </select>
              <textarea bind:value={linkDraft.description} class="min-h-[90px] w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Description"></textarea>
              <button class="rounded-2xl bg-[#2458b8] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#1d4f91]" onclick={stageLinkCreate}>Stage link type</button>
            </div>
          </div>
        </section>
      {:else if activeSection === 'projects' && selectedProject}
        <section class="space-y-6">
          <div class="grid gap-6 xl:grid-cols-[1.12fr_0.92fr]">
            <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
              <div class="text-xs font-semibold uppercase tracking-[0.2em] text-slate-500">Project-based permissions</div>
              <h2 class="mt-2 text-2xl font-semibold text-slate-900">{selectedProject.display_name}</h2>
              <div class="mt-4 grid gap-4 md:grid-cols-2">
                <label class="space-y-2 text-sm">
                  <span class="font-medium text-slate-700">Display name</span>
                  <input bind:value={projectDraft.display_name} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5" />
                </label>
                <label class="space-y-2 text-sm">
                  <span class="font-medium text-slate-700">Workspace slug</span>
                  <input bind:value={projectDraft.workspace_slug} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5" />
                </label>
                <label class="space-y-2 text-sm md:col-span-2">
                  <span class="font-medium text-slate-700">Description</span>
                  <textarea bind:value={projectDraft.description} class="min-h-[110px] w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5"></textarea>
                </label>
              </div>
              <div class="mt-4 flex flex-wrap gap-3">
                <button class="rounded-2xl bg-[#183d70] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#14345e]" onclick={stageProjectUpdate}>Stage project update</button>
                <button class="rounded-2xl border border-[#d2dcec] bg-white px-4 py-3 text-sm font-semibold text-slate-700 transition hover:border-[#9fb8df]" onclick={() => toggleFavorite('projects', selectedProject.id)}>
                  {isFavorite('projects', selectedProject.id) ? 'Unpin' : 'Pin'}
                </button>
              </div>
            </div>

            <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
              <h2 class="text-xl font-semibold text-slate-900">Create project</h2>
              <div class="mt-4 grid gap-3">
                <input bind:value={createProjectDraft.slug} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Project slug" />
                <input bind:value={createProjectDraft.display_name} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Display name" />
                <input bind:value={createProjectDraft.workspace_slug} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Workspace slug" />
                <textarea bind:value={createProjectDraft.description} class="min-h-[90px] w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Description"></textarea>
                <button class="rounded-2xl bg-[#2458b8] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#1d4f91]" onclick={stageProjectCreate}>Stage project</button>
              </div>
            </div>
          </div>

          <div class="grid gap-6 xl:grid-cols-[1fr_1fr]">
            <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
              <div class="flex items-end justify-between gap-4">
                <div>
                  <h2 class="text-xl font-semibold text-slate-900">Memberships</h2>
                  <p class="mt-1 text-sm text-slate-600">Migrate ownership and editor access into project-scoped roles.</p>
                </div>
                <div class="text-sm text-slate-500">{selectedProjectMemberships.length} active</div>
              </div>

              <div class="mt-4 space-y-3">
                {#each selectedProjectMemberships as membership}
                  <div class="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4">
                    <div>
                      <div class="font-medium text-slate-800">{membership.user_id}</div>
                      <div class="mt-1 text-xs uppercase tracking-[0.16em] text-slate-500">{membership.role}</div>
                    </div>
                    <button class="rounded-xl border border-[#efc5c5] bg-white px-3 py-2 text-xs font-semibold text-[#9b2c2c] transition hover:border-[#d99]" onclick={() => removeMembership(membership.user_id)}>
                      Remove now
                    </button>
                  </div>
                {/each}
              </div>

              <div class="mt-4 grid gap-3 rounded-[24px] border border-dashed border-[#bfd1ef] bg-[#f8fbff] p-4">
                <input bind:value={membershipUserId} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="User id" />
                <select bind:value={membershipRole} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm">
                  {#each projectRoleOptions as role}
                    <option value={role}>{role}</option>
                  {/each}
                </select>
                <button class="rounded-2xl bg-[#2458b8] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#1d4f91]" onclick={stageProjectMembership}>Stage membership change</button>
              </div>
            </div>

            <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
              <div class="flex items-end justify-between gap-4">
                <div>
                  <h2 class="text-xl font-semibold text-slate-900">Bound resources</h2>
                  <p class="mt-1 text-sm text-slate-600">Scope object types, links, interfaces, and shared properties to this project.</p>
                </div>
                <div class="text-sm text-slate-500">{selectedProjectResources.length} bound</div>
              </div>

              <div class="mt-4 space-y-3">
                {#each selectedProjectResources as resource}
                  <div class="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4">
                    <div>
                      <div class="font-medium text-slate-800">{resource.resource_kind}</div>
                      <div class="mt-1 text-xs text-slate-500">{resource.resource_id}</div>
                    </div>
                    <button class="rounded-xl border border-[#efc5c5] bg-white px-3 py-2 text-xs font-semibold text-[#9b2c2c] transition hover:border-[#d99]" onclick={() => removeResourceBinding(resource)}>
                      Unbind now
                    </button>
                  </div>
                {/each}
              </div>

              <div class="mt-4 grid gap-3 rounded-[24px] border border-dashed border-[#bfd1ef] bg-[#f8fbff] p-4">
                <select
                  bind:value={resourceBindingKind}
                  class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm"
                  onchange={() => {
                    resourceBindingId = scopedResourceOptions()[0]?.id ?? '';
                  }}
                >
                  <option value="object_type">object_type</option>
                  <option value="link_type">link_type</option>
                  <option value="interface">interface</option>
                  <option value="shared_property_type">shared_property_type</option>
                </select>
                <select bind:value={resourceBindingId} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm">
                  {#each scopedResourceOptions() as option}
                    <option value={option.id}>{option.label}</option>
                  {/each}
                </select>
                <button class="rounded-2xl bg-[#2458b8] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#1d4f91]" onclick={stageProjectResource}>Stage resource binding</button>
              </div>
            </div>
          </div>
        </section>
      {:else if activeSection === 'changes'}
        <section class="space-y-6">
          <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
            <div class="flex flex-wrap items-end justify-between gap-4">
              <div>
                <div class="text-xs font-semibold uppercase tracking-[0.2em] text-slate-500">Review edits</div>
                <h2 class="mt-2 text-2xl font-semibold text-slate-900">Save changes to the ontology</h2>
                <p class="mt-1 text-sm text-slate-600">Review local working-state edits, inspect warnings and errors, then publish them to the shared ontology.</p>
              </div>
              <div class="flex flex-wrap gap-2">
                <button class={`rounded-full px-3 py-2 text-sm font-semibold ${reviewFilter === 'all' ? 'bg-[#183d70] text-white' : 'bg-[#eef4ff] text-[#315ea8]'}`} onclick={() => (reviewFilter = 'all')}>All</button>
                <button class={`rounded-full px-3 py-2 text-sm font-semibold ${reviewFilter === 'warnings' ? 'bg-[#9b6612] text-white' : 'bg-[#fff5e5] text-[#9b6612]'}`} onclick={() => (reviewFilter = 'warnings')}>Warnings</button>
                <button class={`rounded-full px-3 py-2 text-sm font-semibold ${reviewFilter === 'errors' ? 'bg-[#b74141] text-white' : 'bg-[#fff0f0] text-[#b74141]'}`} onclick={() => (reviewFilter = 'errors')}>Errors</button>
              </div>
            </div>

            <div class="mt-4 flex flex-wrap gap-3">
              <button class="rounded-2xl bg-[#183d70] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#14345e]" onclick={applyChanges} disabled={saving || pendingChanges === 0}>
                {saving ? 'Saving…' : 'Save'}
              </button>
              <button class="rounded-2xl border border-[#d2dcec] bg-white px-4 py-3 text-sm font-semibold text-slate-700 transition hover:border-[#9fb8df]" onclick={clearChanges} disabled={pendingChanges === 0}>
                Discard all
              </button>
            </div>
          </div>

          <div class="space-y-4">
            {#if visibleChanges.length === 0}
              <div class="rounded-[28px] border border-[#dbe4f1] bg-white px-6 py-10 text-sm text-slate-600 shadow-sm">No edits match this filter.</div>
            {/if}
            {#each visibleChanges as change}
              <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
                <div class="flex flex-wrap items-start justify-between gap-4">
                  <div>
                    <div class="text-xs font-semibold uppercase tracking-[0.2em] text-slate-500">{change.kind.replaceAll('_', ' ')} · {change.action}</div>
                    <h3 class="mt-2 text-lg font-semibold text-slate-900">{change.label}</h3>
                    <p class="mt-1 text-sm text-slate-600">{change.description}</p>
                  </div>
                  <button class="rounded-xl border border-[#d2dcec] bg-white px-3 py-2 text-sm font-semibold text-slate-700 transition hover:border-[#9fb8df]" onclick={() => removeChange(change.id)}>
                    Discard
                  </button>
                </div>

                <div class="mt-4 grid gap-4 xl:grid-cols-[1fr_0.9fr]">
                  <pre class="overflow-x-auto rounded-2xl border border-[#e7edf6] bg-[#fbfcff] p-4 text-xs leading-5 text-slate-700">{JSON.stringify(change.payload, null, 2)}</pre>
                  <div class="space-y-3">
                    <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] p-4">
                      <div class="text-xs uppercase tracking-[0.16em] text-slate-500">Warnings</div>
                      <div class="mt-2 space-y-2 text-sm text-[#9b6612]">
                        {#if change.warnings.length === 0}
                          <div class="text-slate-500">No warnings.</div>
                        {/if}
                        {#each change.warnings as warning}
                          <div class="rounded-xl bg-[#fff7e8] px-3 py-2">{warning}</div>
                        {/each}
                      </div>
                    </div>
                    <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] p-4">
                      <div class="text-xs uppercase tracking-[0.16em] text-slate-500">Errors</div>
                      <div class="mt-2 space-y-2 text-sm text-[#b74141]">
                        {#if change.errors.length === 0}
                          <div class="text-slate-500">No blocking errors.</div>
                        {/if}
                        {#each change.errors as error}
                          <div class="rounded-xl bg-[#fff0f0] px-3 py-2">{error}</div>
                        {/each}
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            {/each}
          </div>
        </section>
      {:else if activeSection === 'advanced'}
        <section class="grid gap-6 xl:grid-cols-[1.05fr_0.95fr]">
          <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
            <div class="text-xs font-semibold uppercase tracking-[0.2em] text-slate-500">Export, edit, and import</div>
            <h2 class="mt-2 text-2xl font-semibold text-slate-900">Advanced ontology workflows</h2>
            <p class="mt-1 text-sm text-slate-600">
              Export the current working state to JSON, edit it offline, and import it back into the local review queue before publishing.
            </p>

            <div class="mt-6 grid gap-4">
              <button class="rounded-2xl bg-[#183d70] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#14345e]" onclick={exportOntology} disabled={exportBusy}>
                {exportBusy ? 'Exporting…' : 'Export ontology JSON'}
              </button>
              <label class="rounded-2xl border border-dashed border-[#bfd1ef] bg-[#f8fbff] px-4 py-5 text-sm text-slate-600">
                <span class="block font-medium text-slate-800">Import from file</span>
                <span class="mt-1 block">Choose a previously exported ontology snapshot and stage it into the working state.</span>
                <input type="file" accept="application/json" class="mt-3 block text-sm" onchange={handleImportFile} />
              </label>
              <textarea bind:value={importText} class="min-h-[260px] w-full rounded-[24px] border border-[#d2dcec] px-4 py-3 text-sm" placeholder="Paste ontology JSON here to import it into Review edits"></textarea>
              <button class="rounded-2xl border border-[#d2dcec] bg-white px-4 py-3 text-sm font-semibold text-slate-700 transition hover:border-[#9fb8df]" onclick={handleImportText}>
                Stage pasted JSON
              </button>
              {#if importError}
                <div class="rounded-2xl border border-[#efc5c5] bg-[#fff5f5] px-4 py-3 text-sm text-[#9b2c2c]">{importError}</div>
              {/if}
              {#if importSummary}
                <div class="rounded-2xl border border-[#cce3cb] bg-[#f3fbf2] px-4 py-3 text-sm text-[#2e6b33]">{importSummary}</div>
              {/if}
            </div>
          </div>

          <div class="space-y-6">
            <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
              <h2 class="text-xl font-semibold text-slate-900">Change management notes</h2>
              <div class="mt-4 space-y-3 text-sm text-slate-600">
                <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4">
                  <div class="font-medium text-slate-800">Working state first</div>
                  <p class="mt-1">All edits stay local until you save them from Review edits.</p>
                </div>
                <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4">
                  <div class="font-medium text-slate-800">Warnings do not block save</div>
                  <p class="mt-1">Metadata gaps and risky schema changes appear as warnings, but only validation errors stop publishing.</p>
                </div>
                <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4">
                  <div class="font-medium text-slate-800">Project migration support</div>
                  <p class="mt-1">Projects can hold memberships and resource bindings, matching the project-scoped governance flow.</p>
                </div>
              </div>
            </div>

            <div class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
              <h2 class="text-xl font-semibold text-slate-900">Downstream checks</h2>
              <div class="mt-4 grid gap-3">
                <a href="/object-explorer" class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4 transition hover:border-[#bfd1ef]">
                  <div class="font-medium text-slate-800">Object Explorer</div>
                  <div class="mt-1 text-sm text-slate-600">Search the ontology after schema or link changes.</div>
                </a>
                <a href="/object-views" class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4 transition hover:border-[#bfd1ef]">
                  <div class="font-medium text-slate-800">Object Views</div>
                  <div class="mt-1 text-sm text-slate-600">Validate configured full and panel layouts against updated properties.</div>
                </a>
                <a href="/foundry-rules" class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4 transition hover:border-[#bfd1ef]">
                  <div class="font-medium text-slate-800">Foundry Rules</div>
                  <div class="mt-1 text-sm text-slate-600">Review workflows, schedules, and function-backed logic after ontology changes.</div>
                </a>
              </div>
            </div>
          </div>
        </section>
      {/if}
    </main>
  </div>
</div>
