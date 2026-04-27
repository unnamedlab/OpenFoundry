<script lang="ts">
  import { browser } from '$app/environment';
  import { onMount } from 'svelte';
  import EChartView from '$components/analytics/EChartView.svelte';
  import Glyph from '$components/ui/Glyph.svelte';
  import {
    getOntologyGraph,
    listNeighbors,
    listObjectTypes,
    listObjects,
    listQuiverVisualFunctions,
    searchOntology,
    simulateObjectScenarios,
    type GraphEdge,
    type GraphNode,
    type GraphResponse,
    type NeighborLink,
    type ObjectInstance,
    type ObjectScenarioSimulationResponse,
    type ObjectType,
    type QuiverVisualFunction,
    type ScenarioSimulationCandidate,
    type SearchResult,
  } from '$lib/api/ontology';

  type LayoutMode = 'cose' | 'breadthfirst' | 'grid' | 'circle' | 'concentric';
  type NodeDisplayMode = 'compact' | 'card';
  type SidebarTab = 'selection' | 'events' | 'series' | 'layers' | 'media' | 'scenarios';

  interface VertexTemplate {
    id: string;
    name: string;
    description: string;
    rootTypeId: string;
    rootObjectId: string;
    depth: number;
    layout: LayoutMode;
    nodeDisplayMode: NodeDisplayMode;
    subtitleField: string;
    extendedLabelField: string;
    colorByField: string;
    timeField: string;
    eventStartField: string;
    eventEndField: string;
    mediaField: string;
    annotationField: string;
    sharedLensId: string;
    createdAt: string;
    updatedAt: string;
  }

  interface VertexAnnotation {
    id: string;
    label: string;
    x: number;
    y: number;
    width: number;
    height: number;
    color: string;
    note: string;
  }

  interface TemporalEventRow {
    nodeId: string;
    label: string;
    start: string;
    end: string;
    typeLabel: string;
    active: boolean;
  }

  interface ScenarioDraft {
    name: string;
    description: string;
    propertyName: string;
    propertyValue: string;
  }

  const layoutOptions: Array<{ id: LayoutMode; label: string }> = [
    { id: 'cose', label: 'Auto' },
    { id: 'breadthfirst', label: 'Hierarchy' },
    { id: 'grid', label: 'Grid' },
    { id: 'circle', label: 'Circular' },
    { id: 'concentric', label: 'Cluster' },
  ];

  const sidebarTabs: Array<{ id: SidebarTab; label: string; glyph: 'object' | 'bell' | 'artifact' | 'settings' | 'graph' | 'run' }> = [
    { id: 'selection', label: 'Selection', glyph: 'object' },
    { id: 'events', label: 'Events', glyph: 'bell' },
    { id: 'series', label: 'Series', glyph: 'artifact' },
    { id: 'layers', label: 'Layers', glyph: 'settings' },
    { id: 'media', label: 'Media', glyph: 'graph' },
    { id: 'scenarios', label: 'Scenarios', glyph: 'run' },
  ];

  let container = $state<HTMLDivElement | null>(null);
  let cytoscapeModule = $state<((options: Record<string, unknown>) => import('cytoscape').Core) | null>(null);
  let graphInstance = $state<import('cytoscape').Core | null>(null);

  let objectTypes = $state<ObjectType[]>([]);
  let typeMap = $state(new Map<string, ObjectType>());
  let visualFunctions = $state<QuiverVisualFunction[]>([]);
  let graph = $state<GraphResponse | null>(null);
  let loading = $state(true);
  let graphLoading = $state(false);
  let searchLoading = $state(false);
  let scenarioLoading = $state(false);
  let loadError = $state('');
  let notice = $state('');

  let rootTypeId = $state('');
  let rootObjectId = $state('');
  let depth = $state(2);
  let layoutMode = $state<LayoutMode>('cose');
  let nodeDisplayMode = $state<NodeDisplayMode>('compact');
  let activeTab = $state<SidebarTab>('selection');

  let selectedNodeId = $state('');
  let selectedNode = $state<GraphNode | null>(null);
  let selectedNodeProperties = $state<Record<string, unknown>>({});

  let subtitleField = $state('');
  let extendedLabelField = $state('');
  let colorByField = $state('');
  let timeField = $state('');
  let eventStartField = $state('');
  let eventEndField = $state('');
  let mediaField = $state('');
  let annotationField = $state('');
  let selectedLensId = $state('');

  let templates = $state<VertexTemplate[]>([]);
  let selectedTemplateId = $state('');
  let templateName = $state('');
  let templateDescription = $state('');

  let neighborResults = $state<NeighborLink[]>([]);
  let neighborLoading = $state(false);
  let searchAroundFilter = $state('');
  let globalSearchQuery = $state('');
  let globalSearchResults = $state<SearchResult[]>([]);

  let cachedTypeRows = $state<Record<string, ObjectInstance[]>>({});
  let currentTimeIndex = $state(0);

  let customAnnotations = $state<Record<string, VertexAnnotation[]>>({});
  let annotationLabel = $state('');
  let annotationColor = $state('#ef4444');
  let annotationNote = $state('');
  let annotationX = $state(18);
  let annotationY = $state(18);
  let annotationWidth = $state(24);
  let annotationHeight = $state(16);

  let scenarioDrafts = $state<ScenarioDraft[]>([
    { name: 'Optimistic case', description: 'Improve one modeled input to understand downstream impact.', propertyName: '', propertyValue: '' },
    { name: 'Stress case', description: 'Apply a second override to compare a more constrained state.', propertyName: '', propertyValue: '' },
  ]);
  let scenarioResponse = $state<ObjectScenarioSimulationResponse | null>(null);

  function createId() {
    if (browser && globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID();
    return Math.random().toString(36).slice(2, 10);
  }

  function storageKey(name: string) {
    return `of.vertex.${name}`;
  }

  function parseObjectId(node: GraphNode | null) {
    if (!node) return '';
    if (!node.id.startsWith('object:')) return '';
    return node.id.slice('object:'.length);
  }

  function selectedTypeIdFromNode(node: GraphNode | null) {
    const value = node?.metadata?.['object_type_id'];
    return typeof value === 'string' ? value : '';
  }

  function parseRecord(value: unknown) {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return {};
    return value as Record<string, unknown>;
  }

  function stringifyValue(value: unknown) {
    if (value === null || value === undefined) return '—';
    if (Array.isArray(value)) return value.length ? value.join(', ') : '—';
    if (typeof value === 'object') return JSON.stringify(value);
    return String(value);
  }

  function numericValue(value: unknown) {
    if (typeof value === 'number' && Number.isFinite(value)) return value;
    if (typeof value === 'string') {
      const parsed = Number(value.replace(/,/g, ''));
      return Number.isFinite(parsed) ? parsed : null;
    }
    return null;
  }

  function objectLabelFromProperties(properties: Record<string, unknown>) {
    const preferredKeys = ['name', 'title', 'display_name', 'label', 'code', 'identifier', 'id'];
    for (const key of preferredKeys) {
      const value = properties[key];
      if (typeof value === 'string' && value.trim()) return value;
    }
    return 'Object';
  }

  function looksLikeIso(value: unknown) {
    return typeof value === 'string' && /\d{4}-\d{2}-\d{2}/.test(value);
  }

  function detectDateField(properties: Record<string, unknown>) {
    const keys = Object.keys(properties);
    return (
      keys.find((key) => /date|time|timestamp|day|week|month/i.test(key) && looksLikeIso(properties[key])) ??
      keys.find((key) => looksLikeIso(properties[key])) ??
      ''
    );
  }

  function detectMetricField(properties: Record<string, unknown>) {
    const keys = Object.keys(properties);
    return (
      keys.find((key) => /score|value|count|duration|delay|cost|risk|load|temperature|pressure/i.test(key) && numericValue(properties[key]) !== null) ??
      keys.find((key) => numericValue(properties[key]) !== null) ??
      ''
    );
  }

  function detectTemporalFields(properties: Record<string, unknown>) {
    const keys = Object.keys(properties);
    const start =
      keys.find((key) => /start|opened|begin|from|scheduled_start/i.test(key) && looksLikeIso(properties[key])) ??
      '';
    const end =
      keys.find((key) => /end|closed|finish|to|scheduled_end/i.test(key) && looksLikeIso(properties[key])) ??
      '';
    return { start, end };
  }

  function detectMediaField(properties: Record<string, unknown>) {
    const keys = Object.keys(properties);
    return (
      keys.find((key) => /image|media|photo|thumbnail|diagram|url/i.test(key) && typeof properties[key] === 'string') ??
      ''
    );
  }

  function detectAnnotationField(properties: Record<string, unknown>) {
    return Object.keys(properties).find((key) => /coordinate|bbox|bound|annotation|polygon|box/i.test(key)) ?? '';
  }

  function deriveDefaultsFromNode(node: GraphNode | null) {
    if (!node) return;
    const properties = parseRecord(node.metadata?.properties);
    if (!subtitleField) subtitleField = detectDateField(properties) || Object.keys(properties)[0] || '';
    if (!extendedLabelField) extendedLabelField = detectMetricField(properties);
    if (!colorByField) colorByField = detectMetricField(properties);
    if (!timeField) timeField = detectDateField(properties);
    const temporal = detectTemporalFields(properties);
    if (!eventStartField) eventStartField = temporal.start;
    if (!eventEndField) eventEndField = temporal.end;
    if (!mediaField) mediaField = detectMediaField(properties);
    if (!annotationField) annotationField = detectAnnotationField(properties);
    if (!scenarioDrafts[0].propertyName) {
      scenarioDrafts = scenarioDrafts.map((draft) => ({ ...draft, propertyName: detectMetricField(properties) || Object.keys(properties)[0] || '' }));
    }
  }

  function loadTemplates() {
    if (!browser) return;
    try {
      templates = JSON.parse(window.localStorage.getItem(storageKey('templates')) ?? '[]');
      customAnnotations = JSON.parse(window.localStorage.getItem(storageKey('annotations')) ?? '{}');
    } catch {
      templates = [];
      customAnnotations = {};
    }
  }

  function persistTemplates() {
    if (!browser) return;
    window.localStorage.setItem(storageKey('templates'), JSON.stringify(templates));
  }

  function persistAnnotations() {
    if (!browser) return;
    window.localStorage.setItem(storageKey('annotations'), JSON.stringify(customAnnotations));
  }

  function applyTemplate(template: VertexTemplate) {
    selectedTemplateId = template.id;
    templateName = template.name;
    templateDescription = template.description;
    rootTypeId = template.rootTypeId;
    rootObjectId = template.rootObjectId;
    depth = template.depth;
    layoutMode = template.layout;
    nodeDisplayMode = template.nodeDisplayMode;
    subtitleField = template.subtitleField;
    extendedLabelField = template.extendedLabelField;
    colorByField = template.colorByField;
    timeField = template.timeField;
    eventStartField = template.eventStartField;
    eventEndField = template.eventEndField;
    mediaField = template.mediaField;
    annotationField = template.annotationField;
    selectedLensId = template.sharedLensId;
    void loadGraph();
  }

  function saveTemplate() {
    const next: VertexTemplate = {
      id: selectedTemplateId || createId(),
      name: templateName.trim() || `${typeMap.get(rootTypeId)?.display_name ?? 'Vertex'} template`,
      description: templateDescription.trim(),
      rootTypeId,
      rootObjectId,
      depth,
      layout: layoutMode,
      nodeDisplayMode,
      subtitleField,
      extendedLabelField,
      colorByField,
      timeField,
      eventStartField,
      eventEndField,
      mediaField,
      annotationField,
      sharedLensId: selectedLensId,
      createdAt: selectedTemplateId ? templates.find((item) => item.id === selectedTemplateId)?.createdAt ?? new Date().toISOString() : new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    };
    selectedTemplateId = next.id;
    templates = [next, ...templates.filter((item) => item.id !== next.id)];
    persistTemplates();
    notice = `Saved Vertex template "${next.name}".`;
  }

  function deleteTemplate(id: string) {
    templates = templates.filter((item) => item.id !== id);
    if (selectedTemplateId === id) {
      selectedTemplateId = '';
      templateName = '';
      templateDescription = '';
    }
    persistTemplates();
  }

  async function loadCatalog() {
    loading = true;
    loadError = '';
    try {
      const [typesResponse, lensResponse] = await Promise.all([
        listObjectTypes({ per_page: 200 }),
        listQuiverVisualFunctions({ per_page: 100, include_shared: true }).catch(() => ({ data: [], total: 0, page: 1, per_page: 100 })),
      ]);
      objectTypes = typesResponse.data;
      typeMap = new Map(objectTypes.map((item) => [item.id, item]));
      visualFunctions = lensResponse.data;
      if (!rootTypeId && objectTypes[0]) rootTypeId = objectTypes[0].id;
      if (!selectedLensId) {
        const matching = visualFunctions.find((lens) => lens.primary_type_id === rootTypeId);
        selectedLensId = matching?.id ?? '';
      }
      await loadGraph();
    } catch (cause) {
      loadError = cause instanceof Error ? cause.message : 'Failed to load Vertex';
    } finally {
      loading = false;
    }
  }

  async function loadTypeRows(typeId: string) {
    if (!typeId) return [];
    if (cachedTypeRows[typeId]) return cachedTypeRows[typeId];
    const rows: ObjectInstance[] = [];
    let page = 1;
    let total = 0;
    do {
      const response = await listObjects(typeId, { page, per_page: 100 });
      rows.push(...response.data);
      total = response.total;
      page += 1;
    } while (rows.length < total);
    cachedTypeRows = { ...cachedTypeRows, [typeId]: rows };
    return rows;
  }

  async function loadGraph() {
    graphLoading = true;
    loadError = '';
    scenarioResponse = null;
    try {
      graph = await getOntologyGraph({
        root_object_id: rootObjectId || undefined,
        root_type_id: rootObjectId ? undefined : rootTypeId || undefined,
        depth,
        limit: 120,
      });
      selectedNodeId = graph.nodes[0]?.id ?? '';
      selectNodeById(selectedNodeId);
      await renderGraph();
    } catch (cause) {
      loadError = cause instanceof Error ? cause.message : 'Failed to load Vertex graph';
      graph = null;
    } finally {
      graphLoading = false;
    }
  }

  function selectedObjectRows() {
    const typeId = selectedTypeIdFromNode(selectedNode);
    return typeId ? cachedTypeRows[typeId] ?? [] : [];
  }

  function selectedSeriesRows() {
    const rows = selectedObjectRows();
    if (!rows.length || !timeField || !extendedLabelField) return [];
    const buckets: Record<string, number> = {};
    for (const row of rows) {
      const bucket = String(row.properties[timeField] ?? '').slice(0, 10);
      if (!bucket) continue;
      const value = numericValue(row.properties[extendedLabelField]);
      if (value === null) continue;
      buckets[bucket] = (buckets[bucket] ?? 0) + value;
    }
    return Object.entries(buckets)
      .map(([date, value]) => ({ date, value: Number(value.toFixed(2)) }))
      .sort((left, right) => left.date.localeCompare(right.date));
  }

  function selectedGroupedRows() {
    const rows = selectedObjectRows();
    if (!rows.length) return [];
    const key = subtitleField || Object.keys(rows[0].properties)[0] || '';
    const metric = extendedLabelField;
    const buckets: Record<string, number> = {};
    for (const row of rows) {
      const group = String(row.properties[key] ?? 'Unknown');
      const value = numericValue(row.properties[metric]);
      buckets[group] = (buckets[group] ?? 0) + (value ?? 1);
    }
    return Object.entries(buckets)
      .map(([group, value]) => ({ group, value: Number(value.toFixed(2)) }))
      .sort((left, right) => right.value - left.value)
      .slice(0, 12);
  }

  function eventRows() {
    if (!graph || !selectedNode) return [] as TemporalEventRow[];
    const focus = selectedNode;
    const selectedEdgeIds = graph.edges
      .filter((edge) => edge.source === focus.id || edge.target === focus.id)
      .map((edge) => (edge.source === focus.id ? edge.target : edge.source));
    const timeRows = selectedSeriesRows();
    const currentTime = timeRows[currentTimeIndex]?.date;
    return graph.nodes
      .filter((node) => selectedEdgeIds.includes(node.id))
      .map((node) => {
        const properties = parseRecord(node.metadata?.properties);
        const temporal = detectTemporalFields(properties);
        return {
          nodeId: node.id,
          label: node.label,
          start: String(properties[eventStartField || temporal.start] ?? ''),
          end: String(properties[eventEndField || temporal.end] ?? ''),
          typeLabel: node.secondary_label ?? node.kind,
          active: currentTime
            ? String(properties[eventStartField || temporal.start] ?? '').slice(0, 10) <= currentTime &&
              currentTime <= String(properties[eventEndField || temporal.end] ?? '').slice(0, 10)
            : false,
        };
      })
      .filter((row) => row.start && row.end);
  }

  function relatedEventCount(nodeId: string) {
    if (!graph) return 0;
    const adjacent = graph.edges
      .filter((edge) => edge.source === nodeId || edge.target === nodeId)
      .map((edge) => (edge.source === nodeId ? edge.target : edge.source));
    return graph.nodes.filter((node) => adjacent.includes(node.id) && eventRows().some((row) => row.nodeId === node.id)).length;
  }

  function selectedLens() {
    return visualFunctions.find((item) => item.id === selectedLensId) ?? null;
  }

  function selectionProperties() {
    return Object.entries(selectedNodeProperties).slice(0, 12);
  }

  function nodeLabel(node: GraphNode) {
    const properties = parseRecord(node.metadata?.properties);
    const parts = [node.label];
    if (subtitleField && properties[subtitleField] != null) parts.push(String(properties[subtitleField]));
    if (extendedLabelField && properties[extendedLabelField] != null) parts.push(String(properties[extendedLabelField]));
    const badges = relatedEventCount(node.id);
    if (badges > 0) parts.push(`${badges} event${badges === 1 ? '' : 's'}`);
    return parts.join('\n');
  }

  function nodeColor(node: GraphNode) {
    const properties = parseRecord(node.metadata?.properties);
    if (node.metadata?.scenario_changed === true) return '#f97316';
    if (node.metadata?.scenario_deleted === true) return '#ef4444';
    const eventMatch = eventRows().find((row) => row.nodeId === node.id);
    if (eventMatch?.active) return '#dc2626';
    if (colorByField) {
      const value = numericValue(properties[colorByField]);
      if (value !== null) {
        if (value >= 90) return '#991b1b';
        if (value >= 60) return '#ea580c';
        if (value >= 30) return '#0f766e';
      }
    }
    return node.color || '#2458b8';
  }

  function edgeWidth(edge: GraphEdge) {
    if (edge.metadata?.simulated === true) return 3.6;
    if (edge.metadata?.crosses_organization_boundary === true) return 2.8;
    return 1.8;
  }

  function edgeStyle(edge: GraphEdge) {
    if (edge.metadata?.simulated === true) return 'dashed';
    return 'solid';
  }

  async function renderGraph() {
    if (!browser || !container || !graph) return;
    if (!cytoscapeModule) {
      cytoscapeModule = (await import('cytoscape')).default;
    }

    graphInstance?.destroy();

    const elements = [
      ...graph.nodes.map((node) => ({
        data: {
          id: node.id,
          label: nodeLabel(node),
          route: node.route,
          color: nodeColor(node),
          kind: node.kind,
        },
      })),
      ...graph.edges.map((edge) => ({
        data: {
          id: edge.id,
          source: edge.source,
          target: edge.target,
          label: edge.label,
          width: edgeWidth(edge),
          lineStyle: edgeStyle(edge),
        },
      })),
    ];

    const instance = cytoscapeModule({
      container,
      elements,
      style: [
        {
          selector: 'node',
          style: {
            'background-color': 'data(color)',
            label: 'data(label)',
            color: '#10233f',
            'font-size': nodeDisplayMode === 'card' ? 11 : 10,
            'font-family': 'Georgia, "Times New Roman", serif',
            'text-wrap': 'wrap',
            'text-max-width': nodeDisplayMode === 'card' ? '150px' : '110px',
            'text-valign': 'center',
            'text-halign': 'center',
            width: nodeDisplayMode === 'card' ? 60 : 26,
            height: nodeDisplayMode === 'card' ? 60 : 26,
            shape: 'round-rectangle',
            padding: nodeDisplayMode === 'card' ? '18px' : '12px',
            'border-width': 1.4,
            'border-color': '#d6dfef',
          },
        },
        {
          selector: 'node:selected',
          style: {
            'border-color': '#0f172a',
            'border-width': 3,
          },
        },
        {
          selector: 'edge',
          style: {
            label: 'data(label)',
            width: 'data(width)',
            'line-color': '#94a3b8',
            'target-arrow-color': '#94a3b8',
            'target-arrow-shape': 'triangle',
            'curve-style': 'bezier',
            'line-style': 'data(lineStyle)',
            'font-size': 9,
            color: '#64748b',
            'text-rotation': 'autorotate',
          },
        },
      ],
      layout: {
        name: layoutMode,
        animate: true,
        padding: 36,
      },
    });

    instance.on('tap', 'node', (event) => {
      selectNodeById(String(event.target.id()));
    });
    graphInstance = instance;
  }

  function selectNodeById(nodeId: string) {
    selectedNodeId = nodeId;
    selectedNode = graph?.nodes.find((node) => node.id === nodeId) ?? null;
    selectedNodeProperties = selectedNode ? parseRecord(selectedNode.metadata?.properties) : {};
    deriveDefaultsFromNode(selectedNode);
    const typeId = selectedTypeIdFromNode(selectedNode);
    if (typeId) {
      void loadTypeRows(typeId);
      const matchingLens = visualFunctions.find((lens) => lens.primary_type_id === typeId);
      if (matchingLens && !selectedLensId) selectedLensId = matchingLens.id;
    }
    neighborResults = [];
    globalSearchResults = [];
    currentTimeIndex = 0;
  }

  async function runGlobalSearch() {
    const query = globalSearchQuery.trim();
    if (!query) {
      globalSearchResults = [];
      return;
    }
    searchLoading = true;
    try {
      const response = await searchOntology({
        query,
        object_type_id: rootTypeId || undefined,
        semantic: true,
        limit: 10,
      });
      globalSearchResults = response.data;
    } catch (cause) {
      loadError = cause instanceof Error ? cause.message : 'Failed to search graph resources';
    } finally {
      searchLoading = false;
    }
  }

  async function loadNeighborsForSelection() {
    if (!selectedNode) return;
    const objectId = parseObjectId(selectedNode);
    const typeId = selectedTypeIdFromNode(selectedNode);
    if (!objectId || !typeId) return;
    neighborLoading = true;
    try {
      neighborResults = await listNeighbors(typeId, objectId);
    } catch (cause) {
      loadError = cause instanceof Error ? cause.message : 'Failed to search around the selected node';
    } finally {
      neighborLoading = false;
    }
  }

  function filteredNeighbors() {
    const query = searchAroundFilter.trim().toLowerCase();
    if (!query) return neighborResults;
    return neighborResults.filter((neighbor) => {
      return `${neighbor.link_name} ${objectLabelFromProperties(neighbor.object.properties)}`
        .toLowerCase()
        .includes(query);
    });
  }

  function addNeighborToGraph(neighbor: NeighborLink) {
    if (!graph || !selectedNode) return;
    const typeItem = typeMap.get(neighbor.object.object_type_id);
    const nodeId = `object:${neighbor.object.id}`;
    if (!graph.nodes.some((node) => node.id === nodeId)) {
      const newNode: GraphNode = {
        id: nodeId,
        kind: 'object_instance',
        label: objectLabelFromProperties(neighbor.object.properties),
        secondary_label: typeItem?.display_name ?? neighbor.object.object_type_id,
        color: typeItem?.color ?? null,
        route: `/ontology/${neighbor.object.object_type_id}#object-${neighbor.object.id}`,
        metadata: {
          object_type_id: neighbor.object.object_type_id,
          distance_from_root: 1,
          role: 'search_around',
          organization_id: neighbor.object.organization_id ?? null,
          marking: neighbor.object.marking ?? null,
          properties: neighbor.object.properties,
        },
      };
      graph = { ...graph, nodes: [...graph.nodes, newNode], total_nodes: graph.total_nodes + 1 };
    }
    const edgeId = `neighbor:${neighbor.link_id}:${selectedNode.id}:${neighbor.object.id}`;
    if (!graph.edges.some((edge) => edge.id === edgeId)) {
      const nextEdge: GraphEdge = {
        id: edgeId,
        kind: 'link_instance',
        source: neighbor.direction === 'outbound' ? selectedNode.id : nodeId,
        target: neighbor.direction === 'outbound' ? nodeId : selectedNode.id,
        label: neighbor.link_name,
        metadata: { link_type_id: neighbor.link_type_id, search_around: true },
      };
      graph = { ...graph, edges: [...graph.edges, nextEdge], total_edges: graph.total_edges + 1 };
    }
    void renderGraph();
  }

  function addSearchResultToGraph(result: SearchResult) {
    if (!result.object_type_id) return;
    rootTypeId = result.object_type_id;
    if (result.kind === 'object_instance') {
      rootObjectId = result.id;
    }
    void loadGraph();
  }

  function scenarioCandidates(): ScenarioSimulationCandidate[] {
    return scenarioDrafts
      .filter((draft) => draft.name.trim() && draft.propertyName.trim())
      .map((draft) => {
        const original = selectedNodeProperties[draft.propertyName];
        const override = coerceScenarioValue(draft.propertyValue, original);
        return {
          name: draft.name.trim(),
          description: draft.description.trim(),
          operations: [
            {
              properties_patch: {
                [draft.propertyName]: override,
              },
            },
          ],
        };
      });
  }

  function coerceScenarioValue(raw: string, original: unknown) {
    if (typeof original === 'number') {
      const parsed = Number(raw);
      return Number.isFinite(parsed) ? parsed : original;
    }
    if (typeof original === 'boolean') {
      return raw.trim().toLowerCase() === 'true';
    }
    if (typeof original === 'object' && raw.trim().startsWith('{')) {
      try {
        return JSON.parse(raw);
      } catch {
        return original;
      }
    }
    return raw;
  }

  async function runScenarios() {
    if (!selectedNode) return;
    const objectId = parseObjectId(selectedNode);
    const typeId = selectedTypeIdFromNode(selectedNode);
    if (!objectId || !typeId) return;
    scenarioLoading = true;
    try {
      scenarioResponse = await simulateObjectScenarios(typeId, objectId, {
        scenarios: scenarioCandidates(),
        include_baseline: true,
      });
      notice = `Simulated ${scenarioResponse.scenarios.length} Vertex scenario${scenarioResponse.scenarios.length === 1 ? '' : 's'}.`;
    } catch (cause) {
      loadError = cause instanceof Error ? cause.message : 'Failed to simulate scenarios';
    } finally {
      scenarioLoading = false;
    }
  }

  function activeMediaUrl() {
    if (selectedNodeProperties[mediaField] && typeof selectedNodeProperties[mediaField] === 'string') {
      return String(selectedNodeProperties[mediaField]);
    }
    const typeLabel = typeMap.get(selectedTypeIdFromNode(selectedNode))?.display_name ?? 'system';
    const svg = encodeURIComponent(`
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 900 520">
        <defs>
          <linearGradient id="bg" x1="0%" y1="0%" x2="100%" y2="100%">
            <stop offset="0%" stop-color="#0f172a" />
            <stop offset="100%" stop-color="#1e3a8a" />
          </linearGradient>
        </defs>
        <rect width="900" height="520" fill="url(#bg)" />
        <g stroke="#7dd3fc" stroke-width="1.2" fill="none" opacity="0.55">
          <rect x="100" y="90" width="700" height="320" rx="18" />
          <rect x="155" y="135" width="190" height="90" rx="8" />
          <rect x="390" y="135" width="320" height="90" rx="8" />
          <rect x="155" y="265" width="265" height="100" rx="8" />
          <rect x="455" y="265" width="255" height="100" rx="8" />
          <path d="M250 90v320M550 90v320M100 250h700" />
        </g>
        <text x="64" y="56" fill="#e2e8f0" font-size="28" font-family="Georgia, serif">Vertex media layer</text>
        <text x="64" y="92" fill="#93c5fd" font-size="18" font-family="Georgia, serif">${typeLabel}</text>
      </svg>
    `);
    return `data:image/svg+xml;charset=UTF-8,${svg}`;
  }

  function parseCoordinates(value: unknown) {
    if (Array.isArray(value) && value.length >= 4) {
      const [x, y, width, height] = value.map((item) => Number(item));
      if ([x, y, width, height].every((item) => Number.isFinite(item))) {
        return normalizeAnnotationFrame(x, y, width, height);
      }
    }

    if (typeof value === 'string' && value.trim()) {
      try {
        return parseCoordinates(JSON.parse(value));
      } catch {
        const parts = value.split(/[,\s]+/).map((item) => Number(item));
        if (parts.length >= 4 && parts.every((item) => Number.isFinite(item))) {
          return normalizeAnnotationFrame(parts[0], parts[1], parts[2], parts[3]);
        }
      }
    }

    if (value && typeof value === 'object') {
      const record = value as Record<string, unknown>;
      const x = Number(record.x ?? record.left ?? 0);
      const y = Number(record.y ?? record.top ?? 0);
      const width = Number(record.width ?? record.w ?? 0);
      const height = Number(record.height ?? record.h ?? 0);
      if ([x, y, width, height].every((item) => Number.isFinite(item))) {
        return normalizeAnnotationFrame(x, y, width, height);
      }
    }

    return null;
  }

  function normalizeAnnotationFrame(x: number, y: number, width: number, height: number) {
    const scale = Math.max(x, y, width, height) > 100 ? 10 : 1;
    return {
      x: Math.max(0, Math.min(90, x / scale)),
      y: Math.max(0, Math.min(90, y / scale)),
      width: Math.max(4, Math.min(70, width / scale)),
      height: Math.max(4, Math.min(70, height / scale)),
    };
  }

  function graphAnnotations() {
    const annotations: VertexAnnotation[] = [];
    const selectedId = parseObjectId(selectedNode);
    const saved = selectedId ? customAnnotations[selectedId] ?? [] : [];
    annotations.push(...saved);

    if (!graph || !annotationField) return annotations;
    for (const node of graph.nodes) {
      const properties = parseRecord(node.metadata?.properties);
      const parsed = parseCoordinates(properties[annotationField]);
      if (!parsed) continue;
      annotations.push({
        id: `graph-${node.id}`,
        label: node.label,
        x: parsed.x,
        y: parsed.y,
        width: parsed.width,
        height: parsed.height,
        color: '#38bdf8',
        note: node.secondary_label ?? '',
      });
    }
    return annotations;
  }

  function addAnnotation() {
    const selectedId = parseObjectId(selectedNode);
    if (!selectedId || !annotationLabel.trim()) return;
    const next: VertexAnnotation = {
      id: createId(),
      label: annotationLabel.trim(),
      x: annotationX,
      y: annotationY,
      width: annotationWidth,
      height: annotationHeight,
      color: annotationColor,
      note: annotationNote.trim(),
    };
    customAnnotations = {
      ...customAnnotations,
      [selectedId]: [...(customAnnotations[selectedId] ?? []), next],
    };
    persistAnnotations();
    annotationLabel = '';
    annotationNote = '';
  }

  function removeAnnotation(id: string) {
    const selectedId = parseObjectId(selectedNode);
    if (!selectedId) return;
    customAnnotations = {
      ...customAnnotations,
      [selectedId]: (customAnnotations[selectedId] ?? []).filter((item) => item.id !== id),
    };
    persistAnnotations();
  }

  function currentTimeLabel() {
    return selectedSeriesRows()[currentTimeIndex]?.date ?? 'No timeline';
  }

  onMount(async () => {
    loadTemplates();
    await loadCatalog();
  });

  $effect(() => {
    if (selectedNode) {
      const typeId = selectedTypeIdFromNode(selectedNode);
      if (typeId) void loadTypeRows(typeId);
    }
  });

  $effect(() => {
    if (graph) {
      void renderGraph();
    }
  });
</script>

<svelte:head>
  <title>Vertex | OpenFoundry</title>
</svelte:head>

<div class="mx-auto flex max-w-[1680px] flex-col gap-6 px-4 pb-10 pt-4 text-slate-900">
  <section class="overflow-hidden rounded-[30px] border border-[#d7dfef] bg-[linear-gradient(135deg,#081428_0%,#10284f_52%,#1d4f91_100%)] shadow-[0_28px_80px_rgba(8,20,40,0.28)]">
    <div class="flex flex-col gap-6 px-6 py-6 text-white lg:flex-row lg:items-end lg:justify-between lg:px-8">
      <div class="max-w-3xl">
        <div class="inline-flex items-center gap-2 rounded-full border border-white/15 bg-white/10 px-3 py-1 text-xs font-semibold uppercase tracking-[0.24em] text-sky-100">
          <Glyph name="graph" size={16} />
          Vertex
        </div>
        <h1 class="mt-4 text-3xl font-semibold tracking-tight md:text-4xl">Visualize, simulate, and annotate your digital twin as a dedicated graph product.</h1>
        <p class="mt-3 max-w-2xl text-sm leading-6 text-sky-50/85">
          This dedicated `Vertex` surface now packages graph exploration, graph templates, event badges, time-series sidecars, media layers, and `what-if`
          scenario simulation into one product instead of scattering them between graph explorer and Quiver.
        </p>
      </div>

      <div class="grid min-w-[340px] gap-3 rounded-[24px] border border-white/12 bg-white/10 p-4 backdrop-blur">
        <div class="grid grid-cols-3 gap-2">
          <div class="rounded-2xl border border-white/10 bg-white/8 px-3 py-3">
            <div class="text-[11px] uppercase tracking-[0.18em] text-sky-100/75">Templates</div>
            <div class="mt-1 text-2xl font-semibold">{templates.length}</div>
          </div>
          <div class="rounded-2xl border border-white/10 bg-white/8 px-3 py-3">
            <div class="text-[11px] uppercase tracking-[0.18em] text-sky-100/75">Graph nodes</div>
            <div class="mt-1 text-2xl font-semibold">{graph?.total_nodes ?? 0}</div>
          </div>
          <div class="rounded-2xl border border-white/10 bg-white/8 px-3 py-3">
            <div class="text-[11px] uppercase tracking-[0.18em] text-sky-100/75">Scenarios</div>
            <div class="mt-1 text-2xl font-semibold">{scenarioResponse?.scenarios.length ?? 0}</div>
          </div>
        </div>
        <div class="flex flex-wrap gap-2">
          <a href="/ontology/graph" class="rounded-xl border border-white/15 bg-white/10 px-3 py-2 text-sm font-semibold text-white transition hover:bg-white/16">Raw graph</a>
          <a href="/quiver" class="rounded-xl border border-white/15 bg-white/10 px-3 py-2 text-sm font-semibold text-white transition hover:bg-white/16">Quiver</a>
          <a href="/object-explorer" class="rounded-xl border border-white/15 bg-white/10 px-3 py-2 text-sm font-semibold text-white transition hover:bg-white/16">Object Explorer</a>
        </div>
      </div>
    </div>
  </section>

  {#if loadError}
    <div class="rounded-2xl border border-[#efc5c5] bg-[#fff5f5] px-4 py-3 text-sm text-[#9b2c2c]">{loadError}</div>
  {/if}
  {#if notice}
    <div class="rounded-2xl border border-[#cce3cb] bg-[#f3fbf2] px-4 py-3 text-sm text-[#2e6b33]">{notice}</div>
  {/if}

  <section class="rounded-[28px] border border-[#dbe4f1] bg-white p-5 shadow-sm">
    <div class="grid gap-4 xl:grid-cols-[1.3fr_0.9fr_0.9fr]">
      <div class="grid gap-3 md:grid-cols-3">
        <label class="space-y-2 text-sm">
          <span class="font-medium text-slate-700">Object type</span>
          <select bind:value={rootTypeId} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5">
            {#each objectTypes as typeItem}
              <option value={typeItem.id}>{typeItem.display_name}</option>
            {/each}
          </select>
        </label>
        <label class="space-y-2 text-sm">
          <span class="font-medium text-slate-700">Root object id</span>
          <input bind:value={rootObjectId} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5" placeholder="Optional object UUID" />
        </label>
        <label class="space-y-2 text-sm">
          <span class="font-medium text-slate-700">Depth</span>
          <input type="number" bind:value={depth} min="1" max="4" class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5" />
        </label>
      </div>

      <div class="grid gap-3 md:grid-cols-2">
        <label class="space-y-2 text-sm">
          <span class="font-medium text-slate-700">Layout</span>
          <select bind:value={layoutMode} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5">
            {#each layoutOptions as option}
              <option value={option.id}>{option.label}</option>
            {/each}
          </select>
        </label>
        <label class="space-y-2 text-sm">
          <span class="font-medium text-slate-700">Node mode</span>
          <select bind:value={nodeDisplayMode} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5">
            <option value="compact">Compact</option>
            <option value="card">Object card</option>
          </select>
        </label>
        <label class="space-y-2 text-sm md:col-span-2">
          <span class="font-medium text-slate-700">Quiver lens</span>
          <select bind:value={selectedLensId} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5">
            <option value="">No shared lens</option>
            {#each visualFunctions as lens}
              <option value={lens.id}>{lens.name}</option>
            {/each}
          </select>
        </label>
      </div>

      <div class="grid gap-3">
        <label class="space-y-2 text-sm">
          <span class="font-medium text-slate-700">Global graph search</span>
          <div class="flex gap-2">
            <input bind:value={globalSearchQuery} class="flex-1 rounded-2xl border border-[#d2dcec] px-3 py-2.5" placeholder="Find objects or types" />
            <button class="rounded-2xl bg-[#183d70] px-4 py-2.5 text-sm font-semibold text-white transition hover:bg-[#14345e]" onclick={runGlobalSearch} disabled={searchLoading}>
              {searchLoading ? '…' : 'Search'}
            </button>
          </div>
        </label>
        <div class="flex flex-wrap gap-2">
          <button class="rounded-2xl bg-[#2458b8] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#1d4f91]" onclick={loadGraph} disabled={graphLoading}>
            {graphLoading ? 'Loading…' : 'Load graph'}
          </button>
          <button class="rounded-2xl border border-[#d2dcec] bg-white px-4 py-3 text-sm font-semibold text-slate-700 transition hover:border-[#9fb8df]" onclick={saveTemplate}>
            Save as template
          </button>
        </div>
      </div>
    </div>

    {#if globalSearchResults.length > 0}
      <div class="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {#each globalSearchResults as result}
          <button class="rounded-2xl border border-[#e4ebf5] bg-[#fbfcff] px-4 py-4 text-left transition hover:border-[#bfd1ef]" onclick={() => addSearchResultToGraph(result)}>
            <div class="text-xs font-semibold uppercase tracking-[0.18em] text-slate-500">{result.kind.replaceAll('_', ' ')}</div>
            <div class="mt-2 font-medium text-slate-900">{result.title}</div>
            <div class="mt-1 text-sm text-slate-600">{result.subtitle ?? result.snippet}</div>
          </button>
        {/each}
      </div>
    {/if}
  </section>

  <div class="grid gap-6 xl:grid-cols-[minmax(0,1.42fr)_380px]">
    <section class="space-y-6">
      <div class="rounded-[28px] border border-[#dbe4f1] bg-white shadow-sm">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-[#e6edf6] px-5 py-4">
          <div>
            <div class="text-[11px] font-semibold uppercase tracking-[0.24em] text-slate-500">Graph canvas</div>
            <h2 class="mt-1 text-xl font-semibold text-slate-900">Editable system graph</h2>
          </div>
          <div class="flex flex-wrap gap-2">
            <span class="rounded-full bg-[#eef4ff] px-3 py-1 text-xs font-semibold text-[#2458b8]">Nodes {graph?.total_nodes ?? 0}</span>
            <span class="rounded-full bg-[#eef5e8] px-3 py-1 text-xs font-semibold text-[#356b3c]">Edges {graph?.total_edges ?? 0}</span>
            <span class="rounded-full bg-[#fff1df] px-3 py-1 text-xs font-semibold text-[#a35a11]">Timeline {currentTimeLabel()}</span>
          </div>
        </div>
        <div class="grid gap-0 xl:grid-cols-[minmax(0,1fr)_270px]">
          <div class="relative min-h-[640px] border-r border-[#e6edf6]">
            <div bind:this={container} class="h-[640px] w-full"></div>
            {#if graphLoading}
              <div class="absolute inset-0 flex items-center justify-center bg-white/70 text-sm font-medium text-slate-600">Loading Vertex graph…</div>
            {/if}
          </div>
          <div class="space-y-4 px-4 py-4">
            <div class="rounded-2xl border border-[#e4ebf5] bg-[#fbfcff] p-4">
              <div class="text-xs font-semibold uppercase tracking-[0.18em] text-slate-500">Graph template</div>
              <input bind:value={templateName} class="mt-3 w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Template name" />
              <textarea bind:value={templateDescription} class="mt-3 min-h-[82px] w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Describe when to reuse this template"></textarea>
              <div class="mt-3 space-y-2">
                {#each templates.slice(0, 6) as template}
                  <div class="rounded-2xl border border-[#dbe4f1] bg-white p-3">
                    <button class="w-full text-left" onclick={() => applyTemplate(template)}>
                      <div class="font-medium text-slate-900">{template.name}</div>
                      <div class="mt-1 text-xs text-slate-500">{template.updatedAt.slice(0, 10)} · {layoutOptions.find((item) => item.id === template.layout)?.label}</div>
                    </button>
                    <button class="mt-3 rounded-xl border border-[#efc5c5] px-3 py-1.5 text-xs font-semibold text-[#9b2c2c]" onclick={() => deleteTemplate(template.id)}>
                      Delete
                    </button>
                  </div>
                {/each}
              </div>
            </div>

            <div class="rounded-2xl border border-[#e4ebf5] bg-[#fbfcff] p-4">
              <div class="text-xs font-semibold uppercase tracking-[0.18em] text-slate-500">Search around</div>
              <button class="mt-3 w-full rounded-2xl bg-[#183d70] px-4 py-2.5 text-sm font-semibold text-white transition hover:bg-[#14345e]" onclick={loadNeighborsForSelection} disabled={neighborLoading || !selectedNode}>
                {neighborLoading ? 'Loading…' : 'Load related objects'}
              </button>
              <input bind:value={searchAroundFilter} class="mt-3 w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="Filter neighbors" />
              <div class="mt-3 max-h-[240px] space-y-2 overflow-y-auto">
                {#each filteredNeighbors() as neighbor}
                  <button class="w-full rounded-2xl border border-[#dbe4f1] bg-white px-3 py-3 text-left transition hover:border-[#bfd1ef]" onclick={() => addNeighborToGraph(neighbor)}>
                    <div class="font-medium text-slate-900">{objectLabelFromProperties(neighbor.object.properties)}</div>
                    <div class="mt-1 text-xs text-slate-500">{neighbor.direction} via {neighbor.link_name}</div>
                  </button>
                {/each}
                {#if !neighborLoading && neighborResults.length === 0}
                  <div class="rounded-2xl border border-dashed border-[#dbe4f1] px-3 py-4 text-sm text-slate-500">Load neighbors from the current selection to expand the graph.</div>
                {/if}
              </div>
            </div>
          </div>
        </div>
      </div>

      <div class="grid gap-6 xl:grid-cols-[1fr_1fr]">
        <section class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
          <div class="flex items-start justify-between gap-3">
            <div>
              <div class="text-[11px] font-semibold uppercase tracking-[0.24em] text-slate-500">Time series</div>
              <h2 class="mt-1 text-xl font-semibold text-slate-900">Series view</h2>
            </div>
            {#if selectedLens()}
              <div class="rounded-full bg-[#eef4ff] px-3 py-1 text-xs font-semibold text-[#2458b8]">{selectedLens()?.name}</div>
            {/if}
          </div>
          <div class="mt-4 h-[320px]">
            <EChartView
              rows={selectedSeriesRows().map((row) => ({ date: row.date, value: row.value }))}
              categoryKey="date"
              valueKeys={['value']}
              mode="line"
              emptyLabel="Select an object-backed node with time and numeric properties to open the series view."
              onCategoryClick={(value) => {
                const index = selectedSeriesRows().findIndex((row) => row.date === value);
                currentTimeIndex = index >= 0 ? index : 0;
              }}
            />
          </div>
          {#if selectedSeriesRows().length > 1}
            <input
              class="mt-4 w-full accent-[#2458b8]"
              type="range"
              min="0"
              max={Math.max(0, selectedSeriesRows().length - 1)}
              bind:value={currentTimeIndex}
            />
          {/if}
        </section>

        <section class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
          <div class="flex items-start justify-between gap-3">
            <div>
              <div class="text-[11px] font-semibold uppercase tracking-[0.24em] text-slate-500">Graph readouts</div>
              <h2 class="mt-1 text-xl font-semibold text-slate-900">Histogram and extended labels</h2>
            </div>
            <div class="text-xs text-slate-500">{subtitleField || 'No group field yet'}</div>
          </div>
          <div class="mt-4 h-[320px]">
            <EChartView
              rows={selectedGroupedRows().map((row) => ({ group: row.group, value: row.value }))}
              categoryKey="group"
              valueKeys={['value']}
              mode="bar"
              emptyLabel="Select a node to derive grouped readouts from its object type."
            />
          </div>
        </section>
      </div>
    </section>

    <aside class="space-y-6">
      <section class="rounded-[28px] border border-[#dbe4f1] bg-white p-4 shadow-sm">
        <div class="flex flex-wrap gap-2">
          {#each sidebarTabs as tab}
            <button
              class={`flex items-center gap-2 rounded-2xl px-3 py-2 text-sm font-semibold transition ${
                activeTab === tab.id ? 'bg-[#183d70] text-white' : 'bg-[#eef4ff] text-[#2458b8]'
              }`}
              onclick={() => activeTab = tab.id}
            >
              <Glyph name={tab.glyph} size={15} />
              {tab.label}
            </button>
          {/each}
        </div>
      </section>

      {#if activeTab === 'selection'}
        <section class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
          <div class="text-[11px] font-semibold uppercase tracking-[0.24em] text-slate-500">Selection</div>
          <h2 class="mt-1 text-xl font-semibold text-slate-900">{selectedNode?.label ?? 'Choose a node'}</h2>
          <div class="mt-1 text-sm text-slate-600">{selectedNode?.secondary_label ?? 'No object selected.'}</div>
          <div class="mt-4 grid gap-3">
            {#each selectionProperties() as [key, value]}
              <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-3">
                <div class="text-xs uppercase tracking-[0.16em] text-slate-500">{key}</div>
                <div class="mt-2 text-sm text-slate-800">{stringifyValue(value)}</div>
              </div>
            {/each}
          </div>
          {#if selectedNode?.route}
            <a href={selectedNode.route} class="mt-4 inline-flex rounded-2xl border border-[#d2dcec] px-4 py-3 text-sm font-semibold text-slate-700 transition hover:border-[#9fb8df]">
              Open source object
            </a>
          {/if}
        </section>
      {/if}

      {#if activeTab === 'events'}
        <section class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
          <div class="text-[11px] font-semibold uppercase tracking-[0.24em] text-slate-500">Associated events</div>
          <h2 class="mt-1 text-xl font-semibold text-slate-900">Timeline-aware event badges</h2>
          <p class="mt-2 text-sm text-slate-600">Neighbor objects with start and end timestamps are surfaced as Vertex-style events around the current selection.</p>
          <div class="mt-4 space-y-3">
            {#each eventRows() as eventRow}
              <button class={`w-full rounded-2xl border px-4 py-4 text-left ${eventRow.active ? 'border-[#fecaca] bg-[#fff5f5]' : 'border-[#e7edf6] bg-[#fbfcff]'}`} onclick={() => selectNodeById(eventRow.nodeId)}>
                <div class="flex items-center justify-between gap-3">
                  <div class="font-medium text-slate-900">{eventRow.label}</div>
                  <span class={`rounded-full px-2 py-1 text-xs font-semibold ${eventRow.active ? 'bg-[#dc2626] text-white' : 'bg-[#eef4ff] text-[#2458b8]'}`}>
                    {eventRow.active ? 'Active' : 'Scheduled'}
                  </span>
                </div>
                <div class="mt-2 text-xs text-slate-500">{eventRow.typeLabel} · {eventRow.start.slice(0, 10)} → {eventRow.end.slice(0, 10)}</div>
              </button>
            {/each}
            {#if eventRows().length === 0}
              <div class="rounded-2xl border border-dashed border-[#dbe4f1] px-4 py-5 text-sm text-slate-500">No temporal neighbors were detected for the current selection.</div>
            {/if}
          </div>
        </section>
      {/if}

      {#if activeTab === 'series'}
        <section class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
          <div class="text-[11px] font-semibold uppercase tracking-[0.24em] text-slate-500">Series handoff</div>
          <h2 class="mt-1 text-xl font-semibold text-slate-900">Time-series actions</h2>
          <div class="mt-4 space-y-3">
            <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4">
              <div class="font-medium text-slate-900">Current timeline point</div>
              <div class="mt-2 text-sm text-slate-600">{currentTimeLabel()}</div>
            </div>
            <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-4 py-4">
              <div class="font-medium text-slate-900">Series field</div>
              <div class="mt-2 text-sm text-slate-600">{timeField || 'Set a time field in Layers'}</div>
            </div>
            <a href="/quiver" class="rounded-2xl border border-[#d2dcec] px-4 py-3 text-sm font-semibold text-slate-700 transition hover:border-[#9fb8df]">Open in Quiver</a>
            <a href="/object-explorer" class="rounded-2xl border border-[#d2dcec] px-4 py-3 text-sm font-semibold text-slate-700 transition hover:border-[#9fb8df]">Open in Object Explorer</a>
          </div>
        </section>
      {/if}

      {#if activeTab === 'layers'}
        <section class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
          <div class="text-[11px] font-semibold uppercase tracking-[0.24em] text-slate-500">Layer styling</div>
          <h2 class="mt-1 text-xl font-semibold text-slate-900">Object and edge display options</h2>
          <div class="mt-4 grid gap-3">
            <label class="space-y-2 text-sm">
              <span class="font-medium text-slate-700">Subtitle field</span>
              <input bind:value={subtitleField} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5" placeholder="property name" />
            </label>
            <label class="space-y-2 text-sm">
              <span class="font-medium text-slate-700">Extended label field</span>
              <input bind:value={extendedLabelField} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5" placeholder="numeric or status property" />
            </label>
            <label class="space-y-2 text-sm">
              <span class="font-medium text-slate-700">Color by field</span>
              <input bind:value={colorByField} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5" placeholder="threshold metric" />
            </label>
            <label class="space-y-2 text-sm">
              <span class="font-medium text-slate-700">Time field</span>
              <input bind:value={timeField} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5" placeholder="date or timestamp property" />
            </label>
            <label class="space-y-2 text-sm">
              <span class="font-medium text-slate-700">Event start / end</span>
              <div class="grid grid-cols-2 gap-2">
                <input bind:value={eventStartField} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5" placeholder="start field" />
                <input bind:value={eventEndField} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5" placeholder="end field" />
              </div>
            </label>
            <button class="rounded-2xl bg-[#183d70] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#14345e]" onclick={renderGraph}>
              Apply styling
            </button>
          </div>
        </section>
      {/if}

      {#if activeTab === 'media'}
        <section class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
          <div class="text-[11px] font-semibold uppercase tracking-[0.24em] text-slate-500">Media layers</div>
          <h2 class="mt-1 text-xl font-semibold text-slate-900">Image annotations and overlays</h2>
          <div class="mt-4 overflow-hidden rounded-[22px] border border-[#dbe4f1] bg-slate-900">
            <div class="relative aspect-[16/10] w-full overflow-hidden">
              <img src={activeMediaUrl()} alt="Vertex media layer" class="h-full w-full object-cover" />
              {#each graphAnnotations() as annotation}
                <button
                  class="absolute rounded-lg border-2 text-left shadow-sm"
                  style={`left:${annotation.x}%; top:${annotation.y}%; width:${annotation.width}%; height:${annotation.height}%; border-color:${annotation.color}; background:${annotation.color}22;`}
                >
                  <span class="absolute left-1 top-1 rounded bg-black/60 px-1.5 py-0.5 text-[10px] font-semibold text-white">{annotation.label}</span>
                </button>
              {/each}
            </div>
          </div>

          <div class="mt-4 grid gap-3">
            <div class="grid grid-cols-2 gap-2">
              <input bind:value={mediaField} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="media property" />
              <input bind:value={annotationField} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="annotation property" />
            </div>
            <input bind:value={annotationLabel} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="annotation label" />
            <textarea bind:value={annotationNote} class="min-h-[72px] rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="note"></textarea>
            <div class="grid grid-cols-2 gap-2">
              <input type="number" bind:value={annotationX} min="0" max="90" class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="x %" />
              <input type="number" bind:value={annotationY} min="0" max="90" class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="y %" />
              <input type="number" bind:value={annotationWidth} min="4" max="80" class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="width %" />
              <input type="number" bind:value={annotationHeight} min="4" max="80" class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="height %" />
            </div>
            <input type="color" bind:value={annotationColor} class="h-11 rounded-2xl border border-[#d2dcec] px-3 py-2.5" />
            <button class="rounded-2xl bg-[#183d70] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#14345e]" onclick={addAnnotation}>
              Create annotation
            </button>
            <div class="space-y-2">
              {#each graphAnnotations().filter((item) => item.id && !item.id.startsWith('graph-')) as annotation}
                <div class="flex items-center justify-between rounded-2xl border border-[#e7edf6] bg-[#fbfcff] px-3 py-3">
                  <div>
                    <div class="font-medium text-slate-900">{annotation.label}</div>
                    <div class="mt-1 text-xs text-slate-500">{annotation.note || 'No note'}</div>
                  </div>
                  <button class="rounded-xl border border-[#efc5c5] px-3 py-2 text-xs font-semibold text-[#9b2c2c]" onclick={() => removeAnnotation(annotation.id)}>
                    Remove
                  </button>
                </div>
              {/each}
            </div>
          </div>
        </section>
      {/if}

      {#if activeTab === 'scenarios'}
        <section class="rounded-[28px] border border-[#dbe4f1] bg-white p-6 shadow-sm">
          <div class="text-[11px] font-semibold uppercase tracking-[0.24em] text-slate-500">Scenarios</div>
          <h2 class="mt-1 text-xl font-semibold text-slate-900">What-if simulation</h2>
          <p class="mt-2 text-sm text-slate-600">Run multi-case overrides on the selected root object and inspect graph deltas, impacted objects, and rule outcomes.</p>
          <div class="mt-4 space-y-4">
            {#each scenarioDrafts as draft, index}
              <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] p-4">
                <input bind:value={draft.name} class="w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" />
                <textarea bind:value={draft.description} class="mt-3 min-h-[70px] w-full rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm"></textarea>
                <div class="mt-3 grid grid-cols-2 gap-2">
                  <input bind:value={draft.propertyName} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="property to patch" />
                  <input bind:value={draft.propertyValue} class="rounded-2xl border border-[#d2dcec] px-3 py-2.5 text-sm" placeholder="override value" />
                </div>
              </div>
            {/each}
            <button class="w-full rounded-2xl bg-[#183d70] px-4 py-3 text-sm font-semibold text-white transition hover:bg-[#14345e]" onclick={runScenarios} disabled={scenarioLoading || !selectedNode}>
              {scenarioLoading ? 'Simulating…' : 'Run scenarios'}
            </button>
          </div>

          {#if scenarioResponse}
            <div class="mt-5 space-y-3">
              {#each scenarioResponse.scenarios as result}
                <div class="rounded-2xl border border-[#e7edf6] bg-[#fbfcff] p-4">
                  <div class="flex items-start justify-between gap-3">
                    <div>
                      <div class="font-semibold text-slate-900">{result.name}</div>
                      <div class="mt-1 text-sm text-slate-600">{result.description ?? 'Scenario result'}</div>
                    </div>
                    <span class="rounded-full bg-[#eef4ff] px-3 py-1 text-xs font-semibold text-[#2458b8]">Goal {result.summary.goal_score}</span>
                  </div>
                  <div class="mt-3 grid grid-cols-2 gap-2 text-sm text-slate-700">
                    <div class="rounded-xl border border-[#dbe4f1] bg-white px-3 py-2">Changed objects {result.summary.changed_object_count}</div>
                    <div class="rounded-xl border border-[#dbe4f1] bg-white px-3 py-2">Schedules {result.summary.schedule_count}</div>
                    <div class="rounded-xl border border-[#dbe4f1] bg-white px-3 py-2">Advisory matches {result.summary.advisory_rule_matches}</div>
                    <div class="rounded-xl border border-[#dbe4f1] bg-white px-3 py-2">Boundary crossings {result.summary.boundary_crossings}</div>
                  </div>
                </div>
              {/each}
            </div>
          {/if}
        </section>
      {/if}
    </aside>
  </div>
</div>
