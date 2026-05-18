import type { CompassSearchResult } from '@/lib/api/workspace';
import type { GlyphName } from '@/lib/components/ui/Glyph';

export type CompassResourceAction = 'move' | 'rename' | 'trash' | 'restore' | 'share';

export interface OpenWithTarget {
  id: string;
  label: string;
  icon: GlyphName;
  urlTemplate: string;
}

export interface ResolvedOpenWithTarget extends OpenWithTarget {
  href: string;
}

export interface ReferenceTarget {
  relationship: string;
  targetType: string;
  required?: boolean;
}

export interface ResourceOpenContext {
  rid?: string | null;
  id?: string | null;
  type?: string | null;
  kind?: string | null;
  project_rid?: string | null;
  project_id?: string | null;
  open_url?: string | null;
}

interface NormalizedResourceOpenContext {
  rid: string;
  id: string;
  type: string;
  kind: string;
  project_rid: string;
  project_id: string;
  open_url: string;
}

export interface CompassResourceTypeDefinition {
  id: string;
  type: string;
  displayName: string;
  owningService: string;
  defaultIcon: GlyphName;
  supportedActions: CompassResourceAction[];
  openAppURLTemplate: string;
  referenceTargets?: ReferenceTarget[];
  openWith: OpenWithTarget[];
}

const COMMON_ACTIONS: CompassResourceAction[] = ['move', 'rename', 'trash', 'restore', 'share'];
const READ_ONLY_ACTIONS: CompassResourceAction[] = ['share'];

export const COMPASS_RESOURCE_TYPE_REGISTRY: CompassResourceTypeDefinition[] = [
  {
    id: 'COMPASS_PROJECT',
    type: 'project',
    displayName: 'Project',
    owningService: 'ontology-definition-service',
    defaultIcon: 'project',
    supportedActions: COMMON_ACTIONS,
    openAppURLTemplate: '/projects/{rid}',
    referenceTargets: [
      { relationship: 'contains', targetType: 'folder' },
      { relationship: 'contains', targetType: 'dataset' },
      { relationship: 'contains', targetType: 'pipeline' },
      { relationship: 'references', targetType: 'project' },
    ],
    openWith: [
      { id: 'project', label: 'Project', icon: 'project', urlTemplate: '/projects/{rid}' },
      { id: 'files', label: 'Files', icon: 'folder-open', urlTemplate: '/projects/{rid}' },
    ],
  },
  {
    id: 'COMPASS_FOLDER',
    type: 'folder',
    displayName: 'Folder',
    owningService: 'ontology-definition-service',
    defaultIcon: 'folder',
    supportedActions: COMMON_ACTIONS,
    openAppURLTemplate: '/projects/{project_rid}/folders/{rid}',
    openWith: [
      { id: 'folder', label: 'Folder', icon: 'folder-open', urlTemplate: '/projects/{project_rid}/folders/{rid}' },
      { id: 'project', label: 'Project', icon: 'project', urlTemplate: '/projects/{project_rid}' },
    ],
  },
  {
    id: 'FOUNDRY_DATASET',
    type: 'dataset',
    displayName: 'Dataset',
    owningService: 'dataset-versioning-service',
    defaultIcon: 'database',
    supportedActions: COMMON_ACTIONS,
    openAppURLTemplate: '/datasets/{rid}',
    openWith: [
      { id: 'dataset-preview', label: 'Dataset Preview', icon: 'database', urlTemplate: '/datasets/{rid}' },
      { id: 'catalog', label: 'Data Catalog', icon: 'badge-check', urlTemplate: '/projects?catalog=1&q={rid}' },
      { id: 'pipeline-builder', label: 'Pipeline Builder', icon: 'graph', urlTemplate: '/pipelines/new?dataset_rid={rid}' },
      { id: 'code-workbook', label: 'Code Workbook', icon: 'code', urlTemplate: '/notebooks/new?dataset_rid={rid}' },
      { id: 'quiver', label: 'Quiver', icon: 'query', urlTemplate: '/quiver?dataset_rid={rid}' },
    ],
  },
  {
    id: 'FOUNDRY_PIPELINE',
    type: 'pipeline',
    displayName: 'Pipeline',
    owningService: 'pipeline-build-service',
    defaultIcon: 'graph',
    supportedActions: COMMON_ACTIONS,
    openAppURLTemplate: '/pipelines/{rid}',
    referenceTargets: [
      { relationship: 'reads', targetType: 'dataset' },
      { relationship: 'writes', targetType: 'dataset' },
    ],
    openWith: [
      { id: 'pipeline', label: 'Pipeline', icon: 'graph', urlTemplate: '/pipelines/{rid}' },
      { id: 'lineage', label: 'Lineage', icon: 'link', urlTemplate: '/lineage?rid={rid}' },
    ],
  },
  {
    id: 'FOUNDRY_BUILD',
    type: 'build',
    displayName: 'Build',
    owningService: 'pipeline-build-service',
    defaultIcon: 'run',
    supportedActions: READ_ONLY_ACTIONS,
    openAppURLTemplate: '/builds/{rid}',
    openWith: [{ id: 'build', label: 'Build', icon: 'run', urlTemplate: '/builds/{rid}' }],
  },
  {
    id: 'FOUNDRY_JOB',
    type: 'job',
    displayName: 'Job',
    owningService: 'pipeline-build-service',
    defaultIcon: 'list',
    supportedActions: READ_ONLY_ACTIONS,
    openAppURLTemplate: '/builds/jobs/{rid}',
    openWith: [{ id: 'job', label: 'Job', icon: 'list', urlTemplate: '/builds/jobs/{rid}' }],
  },
  {
    id: 'FOUNDRY_SCHEDULE',
    type: 'schedule',
    displayName: 'Schedule',
    owningService: 'pipeline-build-service',
    defaultIcon: 'history',
    supportedActions: ['rename', 'trash', 'restore', 'share'],
    openAppURLTemplate: '/schedules/{rid}',
    referenceTargets: [
      { relationship: 'runs', targetType: 'pipeline' },
      { relationship: 'runs', targetType: 'workflow' },
    ],
    openWith: [{ id: 'schedule', label: 'Schedule', icon: 'history', urlTemplate: '/schedules/{rid}' }],
  },
  {
    id: 'FOUNDRY_QUERY',
    type: 'query',
    displayName: 'Query',
    owningService: 'query-engine-service',
    defaultIcon: 'query',
    supportedActions: COMMON_ACTIONS,
    openAppURLTemplate: '/queries/{rid}',
    referenceTargets: [
      { relationship: 'reads', targetType: 'dataset' },
      { relationship: 'reads', targetType: 'virtual-table' },
    ],
    openWith: [
      { id: 'query', label: 'Query', icon: 'query', urlTemplate: '/queries/{rid}' },
      { id: 'quiver', label: 'Quiver', icon: 'query', urlTemplate: '/quiver?query_rid={rid}' },
    ],
  },
  {
    id: 'FOUNDRY_SOURCE',
    type: 'source',
    displayName: 'Source',
    owningService: 'connector-management-service',
    defaultIcon: 'link',
    supportedActions: COMMON_ACTIONS,
    openAppURLTemplate: '/data-connection/sources/{rid}',
    openWith: [{ id: 'source', label: 'Source', icon: 'link', urlTemplate: '/data-connection/sources/{rid}' }],
  },
  {
    id: 'FOUNDRY_VIRTUAL_TABLE',
    type: 'virtual-table',
    displayName: 'Virtual table',
    owningService: 'connector-management-service',
    defaultIcon: 'spreadsheet',
    supportedActions: COMMON_ACTIONS,
    openAppURLTemplate: '/virtual-tables/{rid}',
    openWith: [{ id: 'virtual-table', label: 'Virtual table', icon: 'spreadsheet', urlTemplate: '/virtual-tables/{rid}' }],
  },
  {
    id: 'STREAMS_STREAM',
    type: 'stream',
    displayName: 'Stream',
    owningService: 'ingestion-replication-service',
    defaultIcon: 'graph',
    supportedActions: ['rename', 'trash', 'restore', 'share'],
    openAppURLTemplate: '/streaming/{rid}',
    openWith: [{ id: 'stream', label: 'Stream', icon: 'graph', urlTemplate: '/streaming/{rid}' }],
  },
  {
    id: 'ONTOLOGY_OBJECT_TYPE',
    type: 'object-type',
    displayName: 'Object type',
    owningService: 'ontology-definition-service',
    defaultIcon: 'cube',
    supportedActions: COMMON_ACTIONS,
    openAppURLTemplate: '/ontology/{rid}',
    openWith: [
      { id: 'ontology', label: 'Ontology', icon: 'ontology', urlTemplate: '/ontology/{rid}' },
      { id: 'object-explorer', label: 'Object Explorer', icon: 'search', urlTemplate: '/object-explorer?rid={rid}' },
    ],
  },
  {
    id: 'ONTOLOGY_ACTION_TYPE',
    type: 'action-type',
    displayName: 'Action type',
    owningService: 'ontology-actions-service',
    defaultIcon: 'run',
    supportedActions: COMMON_ACTIONS,
    openAppURLTemplate: '/action-types/{rid}',
    openWith: [{ id: 'action-type', label: 'Action type', icon: 'run', urlTemplate: '/action-types/{rid}' }],
  },
  {
    id: 'WORKSHOP_APP',
    type: 'app',
    displayName: 'Application',
    owningService: 'application-composition-service',
    defaultIcon: 'app',
    supportedActions: COMMON_ACTIONS,
    openAppURLTemplate: '/apps/{rid}',
    referenceTargets: [
      { relationship: 'embeds', targetType: 'dataset' },
      { relationship: 'embeds', targetType: 'object-type' },
      { relationship: 'invokes', targetType: 'action-type' },
      { relationship: 'embeds', targetType: 'report' },
      { relationship: 'embeds', targetType: 'dashboard' },
    ],
    openWith: [{ id: 'app', label: 'Application', icon: 'app', urlTemplate: '/apps/{rid}' }],
  },
  {
    id: 'REPORT_REPORT',
    type: 'report',
    displayName: 'Report',
    owningService: 'report-service',
    defaultIcon: 'document',
    supportedActions: COMMON_ACTIONS,
    openAppURLTemplate: '/reports/{rid}',
    referenceTargets: [
      { relationship: 'reads', targetType: 'query' },
      { relationship: 'reads', targetType: 'dataset' },
    ],
    openWith: [{ id: 'report', label: 'Report', icon: 'document', urlTemplate: '/reports/{rid}' }],
  },
  {
    id: 'NOTEPAD_NOTEPAD',
    type: 'notepad',
    displayName: 'Notepad',
    owningService: 'application-composition-service',
    defaultIcon: 'document',
    supportedActions: COMMON_ACTIONS,
    openAppURLTemplate: '/notepad/{rid}',
    openWith: [{ id: 'notepad', label: 'Notepad', icon: 'document', urlTemplate: '/notepad/{rid}' }],
  },
  {
    id: 'NOTEBOOK_NOTEBOOK',
    type: 'notebook',
    displayName: 'Notebook',
    owningService: 'notebook-runtime-service',
    defaultIcon: 'code',
    supportedActions: COMMON_ACTIONS,
    openAppURLTemplate: '/notebooks/{rid}',
    referenceTargets: [
      { relationship: 'reads', targetType: 'dataset' },
      { relationship: 'writes', targetType: 'dataset' },
    ],
    openWith: [{ id: 'notebook', label: 'Notebook', icon: 'code', urlTemplate: '/notebooks/{rid}' }],
  },
  {
    id: 'WORKSHOP_DASHBOARD',
    type: 'dashboard',
    displayName: 'Dashboard',
    owningService: 'application-composition-service',
    defaultIcon: 'graph',
    supportedActions: COMMON_ACTIONS,
    openAppURLTemplate: '/dashboards/{rid}',
    referenceTargets: [
      { relationship: 'reads', targetType: 'query' },
      { relationship: 'reads', targetType: 'dataset' },
    ],
    openWith: [
      { id: 'dashboard', label: 'Dashboard', icon: 'graph', urlTemplate: '/dashboards/{rid}' },
      { id: 'workshop', label: 'Workshop', icon: 'app', urlTemplate: '/apps/{rid}' },
    ],
  },
  {
    id: 'MODELS_MODEL',
    type: 'model',
    displayName: 'Model',
    owningService: 'model-catalog-service',
    defaultIcon: 'cube',
    supportedActions: COMMON_ACTIONS,
    openAppURLTemplate: '/ml?model={rid}',
    referenceTargets: [{ relationship: 'trained_from', targetType: 'dataset' }],
    openWith: [{ id: 'model', label: 'Model catalog', icon: 'cube', urlTemplate: '/ml?model={rid}' }],
  },
  {
    id: 'FOUNDRY_WORKFLOW',
    type: 'workflow',
    displayName: 'Workflow',
    owningService: 'workflow-orchestration-service',
    defaultIcon: 'list',
    supportedActions: COMMON_ACTIONS,
    openAppURLTemplate: '/workflows/{rid}',
    referenceTargets: [
      { relationship: 'runs', targetType: 'pipeline' },
      { relationship: 'reads', targetType: 'dataset' },
    ],
    openWith: [
      { id: 'workflow', label: 'Workflow', icon: 'list', urlTemplate: '/workflows/{rid}' },
      { id: 'workflow-lineage', label: 'Workflow Lineage', icon: 'graph', urlTemplate: '/lineage?rid={rid}' },
    ],
  },
];

const REGISTRY_BY_TYPE = new Map(COMPASS_RESOURCE_TYPE_REGISTRY.map((entry) => [entry.type, entry]));
const RESOURCE_KIND_TO_TYPE: Record<string, string> = {
  ontology_project: 'project',
  ontology_folder: 'folder',
  ontology_resource_binding: 'unknown',
  project: 'project',
  folder: 'folder',
};
const RID_NAMESPACE_BY_TYPE: Record<string, { service: string; resourceType: string }> = {
  project: { service: 'compass', resourceType: 'project' },
  folder: { service: 'compass', resourceType: 'folder' },
  dataset: { service: 'foundry', resourceType: 'dataset' },
  pipeline: { service: 'foundry', resourceType: 'pipeline' },
  build: { service: 'foundry', resourceType: 'build' },
  job: { service: 'foundry', resourceType: 'job' },
  schedule: { service: 'foundry', resourceType: 'schedule' },
  query: { service: 'foundry', resourceType: 'query' },
  source: { service: 'foundry', resourceType: 'source' },
  'virtual-table': { service: 'foundry', resourceType: 'virtual-table' },
  stream: { service: 'streams', resourceType: 'stream' },
  'object-type': { service: 'ontology', resourceType: 'object-type' },
  'action-type': { service: 'ontology', resourceType: 'action-type' },
  app: { service: 'foundry', resourceType: 'app' },
  report: { service: 'foundry', resourceType: 'report' },
  notepad: { service: 'foundry', resourceType: 'notepad' },
  notebook: { service: 'foundry', resourceType: 'notebook' },
  dashboard: { service: 'foundry', resourceType: 'dashboard' },
  model: { service: 'foundry', resourceType: 'model' },
  workflow: { service: 'foundry', resourceType: 'workflow' },
};

export const UNKNOWN_RESOURCE_TYPE: CompassResourceTypeDefinition = {
  id: 'UNKNOWN_RESOURCE_TYPE',
  type: 'unknown',
  displayName: 'Resource',
  owningService: 'unknown',
  defaultIcon: 'object',
  supportedActions: [],
  openAppURLTemplate: '/search?q={rid}',
  openWith: [{ id: 'search', label: 'Search', icon: 'search', urlTemplate: '/search?q={rid}' }],
};

export function getResourceTypeDefinition(type: string): CompassResourceTypeDefinition {
  const normalizedType = resourceTypeFromKind(type);
  return REGISTRY_BY_TYPE.get(normalizedType) ?? {
    ...UNKNOWN_RESOURCE_TYPE,
    type: normalizedType || UNKNOWN_RESOURCE_TYPE.type,
  };
}

export function openURLForCompassResource(result: CompassSearchResult): string {
  return openURLForResource(result.type, compassResultToOpenContext(result));
}

export function openWithTargetsForCompassResource(result: CompassSearchResult): OpenWithTarget[] {
  return openWithTargetsForResource(result.type);
}

export function openURLForResource(kind: string, context: ResourceOpenContext = {}): string {
  const normalizedContext = normalizeResourceContext(kind, context);
  const definition = getResourceTypeDefinition(normalizedContext.type || kind);
  const registryURL = expandResourceURL(definition.openAppURLTemplate, normalizedContext);
  if (registryURL) return registryURL;
  return normalizedContext.open_url || expandResourceURL(UNKNOWN_RESOURCE_TYPE.openAppURLTemplate, normalizedContext);
}

export function openWithTargetsForResource(kind: string): OpenWithTarget[] {
  const definition = getResourceTypeDefinition(kind);
  return definition.openWith.length > 0 ? definition.openWith : UNKNOWN_RESOURCE_TYPE.openWith;
}

export function resolveOpenWithTargetsForResource(
  kind: string,
  context: ResourceOpenContext = {},
): ResolvedOpenWithTarget[] {
  const normalizedContext = normalizeResourceContext(kind, context);
  const fallbackHref = openURLForResource(kind, normalizedContext);
  return openWithTargetsForResource(normalizedContext.type || kind).map((target) => ({
    ...target,
    href: expandResourceURL(target.urlTemplate, normalizedContext) || fallbackHref,
  }));
}

export function resourceTypeFromKind(kind: string | null | undefined): string {
  if (!kind) return UNKNOWN_RESOURCE_TYPE.type;
  return RESOURCE_KIND_TO_TYPE[kind] ?? kind;
}

export function resourceRIDForKind(kind: string, id: string | null | undefined): string {
  if (!id) return '';
  if (id.startsWith('ri.')) return id;
  const type = resourceTypeFromKind(kind);
  const namespace = RID_NAMESPACE_BY_TYPE[type];
  if (namespace) return `ri.${namespace.service}.main.${namespace.resourceType}.${id}`;
  return id;
}

export function expandResourceURL(template: string, resource: CompassSearchResult | ResourceOpenContext): string {
  const resourceKind = 'kind' in resource ? resource.kind : undefined;
  const context = normalizeResourceContext(resource.type ?? resourceKind ?? UNKNOWN_RESOURCE_TYPE.type, resource);
  const parsed = parseRID(context.rid);
  const replacements: Record<string, string> = {
    rid: context.rid,
    id: context.id,
    resource_id: context.id,
    project_rid: context.project_rid ?? '',
    project_id: context.project_id ?? '',
    service: parsed.service,
    instance: parsed.instance,
    type: parsed.type || context.type,
    locator: parsed.locator,
  };

  const url = template.replace(/\{([a-z_]+)\}/g, (_match, key: string) => {
    const value = replacements[key] ?? '';
    return encodeURIComponent(value);
  });
  if (url.includes('//') || url.endsWith('/')) return url.replace(/([^:])\/{2,}/g, '$1/').replace(/\/$/, '');
  return url;
}

function compassResultToOpenContext(result: CompassSearchResult): ResourceOpenContext {
  return {
    rid: result.rid,
    id: parseRID(result.rid).locator || result.rid,
    type: result.type,
    project_rid: result.owning_project_rid,
    project_id: result.owning_project_id,
    open_url: result.open_url,
  };
}

function normalizeResourceContext(kind: string, context: CompassSearchResult | ResourceOpenContext): NormalizedResourceOpenContext {
  const contextKind = 'kind' in context ? context.kind : undefined;
  const contextId = 'id' in context ? context.id : undefined;
  const contextProjectRID = 'project_rid' in context ? context.project_rid : undefined;
  const contextProjectID = 'project_id' in context ? context.project_id : undefined;
  const type = resourceTypeFromKind(context.type ?? contextKind ?? kind);
  const sourceId = contextId ?? ('rid' in context ? context.rid : null) ?? '';
  const rid = context.rid ?? resourceRIDForKind(type, sourceId) ?? sourceId;
  const parsed = parseRID(rid);
  const id = contextId ?? parsed.locator ?? rid;
  return {
    rid,
    id,
    type,
    kind: contextKind ?? kind,
    project_rid: contextProjectRID ?? ('owning_project_rid' in context ? context.owning_project_rid : null) ?? '',
    project_id: contextProjectID ?? ('owning_project_id' in context ? context.owning_project_id : null) ?? '',
    open_url: context.open_url ?? '',
  };
}

function parseRID(value: string) {
  const parts = value.split('.');
  if (parts.length >= 5 && parts[0] === 'ri') {
    return {
      service: parts[1] ?? '',
      instance: parts[2] ?? '',
      type: parts[3] ?? '',
      locator: parts.slice(4).join('.'),
    };
  }
  return { service: '', instance: '', type: '', locator: value };
}
