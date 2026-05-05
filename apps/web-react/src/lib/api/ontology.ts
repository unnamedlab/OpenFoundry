import api from './client';

// ────────────────────────────────────────────────────────────────
// Ontology API — slices grow as routes are migrated. Today: object
// types + properties listing (used by /queries). Full ontology
// surface (links, indexing, design) gets added with /ontology* routes.
// ────────────────────────────────────────────────────────────────

export interface ObjectType {
  id: string;
  name: string;
  display_name: string;
  description: string;
  primary_key_property: string | null;
  icon: string | null;
  color: string | null;
  owner_id: string;
  created_at: string;
  updated_at: string;
}

export interface Property {
  id: string;
  object_type_id: string;
  name: string;
  display_name: string;
  description: string;
  property_type: string;
  required: boolean;
  unique_constraint: boolean;
  time_dependent: boolean;
  default_value: unknown;
  validation_rules: unknown;
  created_at: string;
  updated_at: string;
}

export function listObjectTypes(params?: { page?: number; per_page?: number; search?: string }) {
  const qs = new URLSearchParams();
  if (params?.page) qs.set('page', String(params.page));
  if (params?.per_page) qs.set('per_page', String(params.per_page));
  if (params?.search) qs.set('search', params.search);
  return api.get<{ data: ObjectType[]; total: number; page: number; per_page: number }>(
    `/ontology/types${qs.toString() ? `?${qs}` : ''}`,
  );
}

export function listProperties(typeId: string) {
  return api
    .get<{ data: Property[] }>(`/ontology/types/${typeId}/properties`)
    .then((response) => response.data);
}

// ────────────────────────────────────────────────────────────────
// Object instances + ontology graph + Quiver visual functions
// (used by /quiver). Other ontology surfaces ship with their routes.
// ────────────────────────────────────────────────────────────────

export interface ObjectInstance {
  id: string;
  object_type_id: string;
  properties: Record<string, unknown>;
  created_by: string;
  organization_id?: string | null;
  marking?: string;
  created_at: string;
  updated_at: string;
}

export interface GraphNode {
  id: string;
  kind: string;
  label: string;
  secondary_label: string | null;
  color: string | null;
  route: string | null;
  metadata: Record<string, unknown>;
}

export interface GraphEdge {
  id: string;
  kind: string;
  source: string;
  target: string;
  label: string;
  metadata: Record<string, unknown>;
}

export interface GraphSummary {
  scope: string;
  node_kinds: Record<string, number>;
  edge_kinds: Record<string, number>;
  object_types: Record<string, number>;
  markings: Record<string, number>;
  root_neighbor_count: number;
  max_hops_reached: number;
  boundary_crossings: number;
  sensitive_objects: number;
  sensitive_markings: string[];
}

export interface GraphResponse {
  mode: string;
  root_object_id: string | null;
  root_type_id: string | null;
  depth: number;
  total_nodes: number;
  total_edges: number;
  summary: GraphSummary;
  nodes: GraphNode[];
  edges: GraphEdge[];
}

export type QuiverChartKind = 'line' | 'area' | 'bar' | 'point';

export interface QuiverVisualFunction {
  id: string;
  name: string;
  description: string;
  primary_type_id: string;
  secondary_type_id: string | null;
  join_field: string;
  secondary_join_field: string;
  date_field: string;
  metric_field: string;
  group_field: string;
  selected_group: string | null;
  chart_kind: QuiverChartKind;
  shared: boolean;
  vega_spec: Record<string, unknown>;
  owner_id: string;
  created_at: string;
  updated_at: string;
}

export function listObjects(typeId: string, params?: { page?: number; per_page?: number }) {
  const qs = new URLSearchParams();
  if (params?.page) qs.set('page', String(params.page));
  if (params?.per_page) qs.set('per_page', String(params.per_page));
  return api.get<{ data: ObjectInstance[]; total: number; page?: number; per_page?: number }>(
    `/ontology/types/${typeId}/objects${qs.toString() ? `?${qs}` : ''}`,
  );
}

export function getOntologyGraph(params?: {
  root_object_id?: string;
  root_type_id?: string;
  depth?: number;
  limit?: number;
}) {
  const qs = new URLSearchParams();
  if (params?.root_object_id) qs.set('root_object_id', params.root_object_id);
  if (params?.root_type_id) qs.set('root_type_id', params.root_type_id);
  if (params?.depth) qs.set('depth', String(params.depth));
  if (params?.limit) qs.set('limit', String(params.limit));
  return api.get<GraphResponse>(`/ontology/graph${qs.toString() ? `?${qs}` : ''}`);
}

export function listQuiverVisualFunctions(params?: {
  page?: number;
  per_page?: number;
  search?: string;
  include_shared?: boolean;
}) {
  const qs = new URLSearchParams();
  if (params?.page) qs.set('page', String(params.page));
  if (params?.per_page) qs.set('per_page', String(params.per_page));
  if (params?.search) qs.set('search', params.search);
  if (typeof params?.include_shared === 'boolean') {
    qs.set('include_shared', params.include_shared ? 'true' : 'false');
  }
  return api.get<{ data: QuiverVisualFunction[]; total: number; page: number; per_page: number }>(
    `/ontology/quiver/visual-functions${qs.toString() ? `?${qs}` : ''}`,
  );
}

export function createQuiverVisualFunction(body: {
  name: string;
  description?: string;
  primary_type_id: string;
  secondary_type_id?: string | null;
  join_field: string;
  secondary_join_field?: string;
  date_field: string;
  metric_field: string;
  group_field: string;
  selected_group?: string | null;
  chart_kind?: QuiverChartKind;
  shared?: boolean;
}) {
  return api.post<QuiverVisualFunction>('/ontology/quiver/visual-functions', body);
}

export function updateQuiverVisualFunction(
  id: string,
  body: Partial<{
    name: string;
    description: string;
    primary_type_id: string;
    secondary_type_id: string | null;
    join_field: string;
    secondary_join_field: string;
    date_field: string;
    metric_field: string;
    group_field: string;
    selected_group: string | null;
    chart_kind: QuiverChartKind;
    shared: boolean;
  }>,
) {
  return api.patch<QuiverVisualFunction>(`/ontology/quiver/visual-functions/${id}`, body);
}

export function deleteQuiverVisualFunction(id: string) {
  return api.delete(`/ontology/quiver/visual-functions/${id}`);
}

// ────────────────────────────────────────────────────────────────
// Vertex slice — search, neighbors, scenario simulation. Used by
// /vertex; future ontology routes can extend the same module.
// ────────────────────────────────────────────────────────────────

export interface NeighborLink {
  direction: 'inbound' | 'outbound';
  link_id: string;
  link_type_id: string;
  link_name: string;
  object: ObjectInstance;
}

export interface SearchResult {
  kind: string;
  id: string;
  object_type_id: string | null;
  title: string;
  subtitle: string | null;
  snippet: string;
  score: number;
  route: string;
  metadata: Record<string, unknown>;
}

export interface ScenarioSimulationOperation {
  label?: string | null;
  target_object_id?: string | null;
  action_id?: string | null;
  action_parameters?: Record<string, unknown>;
  properties_patch?: Record<string, unknown>;
}

export interface ScenarioSimulationCandidate {
  name: string;
  description?: string | null;
  operations: ScenarioSimulationOperation[];
}

export interface ScenarioSummary {
  impacted_object_count: number;
  changed_object_count: number;
  deleted_object_count: number;
  automatic_rule_matches: number;
  automatic_rule_applications: number;
  advisory_rule_matches: number;
  schedule_count: number;
  impacted_types: string[];
  changed_properties: string[];
  boundary_crossings: number;
  sensitive_objects: number;
  failed_constraints: number;
  achieved_goals: number;
  total_goals: number;
  goal_score: number;
}

export interface ScenarioSimulationResult {
  scenario_id: string;
  name: string;
  description: string | null;
  graph: GraphResponse;
  summary: ScenarioSummary;
}

export interface ObjectScenarioSimulationResponse {
  root_object_id: string;
  root_type_id: string;
  compared_at: string;
  baseline: ScenarioSimulationResult | null;
  scenarios: ScenarioSimulationResult[];
}

export function searchOntology(body: {
  query: string;
  kind?: string;
  object_type_id?: string;
  limit?: number;
  semantic?: boolean;
}) {
  return api.post<{ query: string; total: number; data: SearchResult[] }>('/ontology/search', body);
}

export function listNeighbors(typeId: string, objectId: string) {
  return api
    .get<{ data: NeighborLink[] }>(`/ontology/types/${typeId}/objects/${objectId}/neighbors`)
    .then((response) => response.data);
}

export function simulateObjectScenarios(
  typeId: string,
  objectId: string,
  body: {
    scenarios: ScenarioSimulationCandidate[];
    depth?: number;
    include_baseline?: boolean;
  },
) {
  return api.post<ObjectScenarioSimulationResponse>(
    `/ontology/types/${typeId}/objects/${objectId}/scenarios/simulate`,
    body,
  );
}
