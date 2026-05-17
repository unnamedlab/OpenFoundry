import {
  runBuildV1,
  type BuildState,
  type CreateBuildRequest,
  type CreateBuildResponse,
} from './buildsV1';
import {
  generateHealthReportSnapshot,
  recordHealthReportSnapshot,
  type HealthReportSignal,
} from './health-reports';
import type { PipelineNode } from './pipelines';

export type DataExpectationScope = 'input' | 'output';
export type DataExpectationKind =
  | 'not_null'
  | 'row_count_min'
  | 'schema_has_columns'
  | 'unique'
  | 'no_parse_errors'
  | 'custom_sql';
export type DataExpectationFailureMode = 'ABORT_BUILD' | 'WARN';
export type DataExpectationReviewStatus = 'NEEDS_REVIEW' | 'APPROVED';
export type DataExpectationResultStatus = 'passed' | 'failed' | 'warning' | 'unknown';

export interface DataExpectationDefinition {
  id: string;
  pipeline_id: string;
  node_id: string;
  node_label: string;
  branch_name: string;
  scope: DataExpectationScope;
  subject_dataset_rid: string;
  name: string;
  kind: DataExpectationKind;
  column: string;
  expected_value: string;
  failure_mode: DataExpectationFailureMode;
  enabled: boolean;
  protected_branches: string[];
  review_status: DataExpectationReviewStatus;
  reviewed_by: string;
  reviewed_at: string | null;
  created_at: string;
  updated_at: string;
  last_result?: DataExpectationResult;
}

export interface DataExpectationDraft {
  scope: DataExpectationScope;
  name: string;
  kind: DataExpectationKind;
  column: string;
  expected_value: string;
  failure_mode: DataExpectationFailureMode;
  enabled: boolean;
}

export interface DataExpectationResult {
  id: string;
  expectation_id: string;
  pipeline_id: string;
  node_id: string;
  node_label: string;
  branch_name: string;
  scope: DataExpectationScope;
  subject_dataset_rid: string;
  expectation_name: string;
  kind: DataExpectationKind;
  failure_mode: DataExpectationFailureMode;
  status: DataExpectationResultStatus;
  message: string;
  observed_at: string;
  build_id: string | null;
  details: Record<string, unknown>;
}

export interface DataExpectationPreviewInput {
  columns?: string[];
  rows?: Array<Record<string, unknown>>;
  error?: { message?: string; kind?: string } | null;
}

export interface ExpectationGateResult {
  results: DataExpectationResult[];
  abortingResults: DataExpectationResult[];
  warningResults: DataExpectationResult[];
  reviewBlocked: DataExpectationDefinition[];
  shouldAbort: boolean;
  requiresReview: boolean;
}

const DEFINITIONS_KEY = 'openfoundry:data-expectations:v1';
const RESULTS_KEY = 'openfoundry:data-expectation-results:v1';
const MAX_RESULTS = 500;
const DEFAULT_PROTECTED_BRANCHES = ['main', 'master', 'production', 'prod', 'release'];

export const DATA_EXPECTATION_KINDS: Array<{ value: DataExpectationKind; label: string; description: string }> = [
  { value: 'not_null', label: 'Column not null', description: 'Fails when the selected column contains null or empty values.' },
  { value: 'row_count_min', label: 'Minimum row count', description: 'Fails when the evaluated dataset has fewer rows than the threshold.' },
  { value: 'schema_has_columns', label: 'Required columns', description: 'Fails when one or more required columns are missing.' },
  { value: 'unique', label: 'Column unique', description: 'Fails when duplicate values are observed in the selected column.' },
  { value: 'no_parse_errors', label: 'No parse errors', description: 'Fails when the preview/build input reports parser errors.' },
  { value: 'custom_sql', label: 'Custom SQL expectation', description: 'Registers a custom runtime expectation evaluated by build workers.' },
];

export function defaultDataExpectationDraft(scope: DataExpectationScope = 'output'): DataExpectationDraft {
  return {
    scope,
    name: scope === 'input' ? 'input pre-condition' : 'output post-condition',
    kind: 'not_null',
    column: '',
    expected_value: '',
    failure_mode: 'ABORT_BUILD',
    enabled: true,
  };
}

export function dataExpectationKindLabel(kind: DataExpectationKind) {
  return DATA_EXPECTATION_KINDS.find((entry) => entry.value === kind)?.label ?? kind;
}

export function listDataExpectations(filters: {
  pipelineId?: string;
  nodeId?: string;
  subjectDatasetRid?: string;
} = {}) {
  return readStorageArray<DataExpectationDefinition>(DEFINITIONS_KEY)
    .filter(isDataExpectationDefinition)
    .filter((definition) => !filters.pipelineId || definition.pipeline_id === filters.pipelineId)
    .filter((definition) => !filters.nodeId || definition.node_id === filters.nodeId)
    .filter((definition) => !filters.subjectDatasetRid || definition.subject_dataset_rid === filters.subjectDatasetRid)
    .sort((left, right) => left.name.localeCompare(right.name));
}

export function listDataExpectationResults(filters: {
  buildId?: string;
  pipelineId?: string;
  nodeId?: string;
  subjectDatasetRid?: string;
} = {}) {
  return readStorageArray<DataExpectationResult>(RESULTS_KEY)
    .filter(isDataExpectationResult)
    .filter((result) => !filters.buildId || result.build_id === filters.buildId)
    .filter((result) => !filters.pipelineId || result.pipeline_id === filters.pipelineId)
    .filter((result) => !filters.nodeId || result.node_id === filters.nodeId)
    .filter((result) => !filters.subjectDatasetRid || result.subject_dataset_rid === filters.subjectDatasetRid);
}

export function materializeDataExpectation(input: {
  draft: DataExpectationDraft;
  pipelineId: string;
  node: PipelineNode;
  branchName: string;
  existing?: DataExpectationDefinition | null;
}) {
  const now = new Date().toISOString();
  const subjectDatasetRid = subjectDatasetForExpectation(input.node, input.draft.scope);
  const protectedBranch = isProtectedBranch(input.branchName, input.existing?.protected_branches);
  return {
    id: input.existing?.id ?? createId('data-expectation'),
    pipeline_id: input.pipelineId,
    node_id: input.node.id,
    node_label: input.node.label || input.node.id,
    branch_name: input.branchName || 'main',
    scope: input.draft.scope,
    subject_dataset_rid: subjectDatasetRid,
    name: input.draft.name.trim() || `${input.draft.scope} ${dataExpectationKindLabel(input.draft.kind)}`,
    kind: input.draft.kind,
    column: input.draft.column.trim(),
    expected_value: input.draft.expected_value.trim(),
    failure_mode: input.draft.failure_mode,
    enabled: input.draft.enabled,
    protected_branches: input.existing?.protected_branches ?? DEFAULT_PROTECTED_BRANCHES,
    review_status: protectedBranch ? 'NEEDS_REVIEW' : input.existing?.review_status ?? 'APPROVED',
    reviewed_by: protectedBranch ? '' : input.existing?.reviewed_by ?? 'local-authoring',
    reviewed_at: protectedBranch ? null : input.existing?.reviewed_at ?? now,
    created_at: input.existing?.created_at ?? now,
    updated_at: now,
    last_result: input.existing?.last_result,
  } satisfies DataExpectationDefinition;
}

export function upsertDataExpectation(definition: DataExpectationDefinition) {
  const definitions = listDataExpectations();
  const next = [definition, ...definitions.filter((entry) => entry.id !== definition.id)];
  writeStorage(DEFINITIONS_KEY, next);
  return definition;
}

export function deleteDataExpectation(id: string) {
  writeStorage(DEFINITIONS_KEY, listDataExpectations().filter((definition) => definition.id !== id));
}

export function approveDataExpectation(id: string, reviewer = 'local-review') {
  const now = new Date().toISOString();
  writeStorage(DEFINITIONS_KEY, listDataExpectations().map((definition) => (
    definition.id === id
      ? { ...definition, review_status: 'APPROVED', reviewed_by: reviewer, reviewed_at: now, updated_at: now }
      : definition
  )));
}

export function nodeDataExpectations(node: PipelineNode | null | undefined): DataExpectationDefinition[] {
  const raw = node?.config?._expectations;
  if (!Array.isArray(raw)) return [];
  return raw.filter(isDataExpectationDefinition);
}

export function syncNodeExpectationsToStore(pipelineId: string, branchName: string, node: PipelineNode) {
  const existingById = new Map(listDataExpectations({ pipelineId, nodeId: node.id }).map((definition) => [definition.id, definition]));
  const fromNode = nodeDataExpectations(node).map((definition) => {
    const existing = existingById.get(definition.id);
    return {
      ...definition,
      pipeline_id: pipelineId,
      node_id: node.id,
      node_label: node.label || node.id,
      branch_name: definition.branch_name || branchName || 'main',
      review_status: existing?.review_status ?? definition.review_status,
      reviewed_by: existing?.reviewed_by ?? definition.reviewed_by,
      reviewed_at: existing?.reviewed_at ?? definition.reviewed_at,
      last_result: existing?.last_result ?? definition.last_result,
    };
  });
  for (const definition of fromNode) upsertDataExpectation(definition);
  return fromNode;
}

export function withNodeDataExpectations(node: PipelineNode, definitions: DataExpectationDefinition[]) {
  return {
    ...node,
    config: {
      ...(node.config ?? {}),
      _expectations: definitions.filter((definition) => definition.node_id === node.id),
    },
  };
}

export function evaluateDataExpectation(
  definition: DataExpectationDefinition,
  preview?: DataExpectationPreviewInput | null,
  buildId: string | null = null,
) {
  const observedAt = new Date().toISOString();
  const result = evaluateDefinition(definition, preview);
  return {
    id: createId('data-expectation-result'),
    expectation_id: definition.id,
    pipeline_id: definition.pipeline_id,
    node_id: definition.node_id,
    node_label: definition.node_label,
    branch_name: definition.branch_name,
    scope: definition.scope,
    subject_dataset_rid: definition.subject_dataset_rid,
    expectation_name: definition.name,
    kind: definition.kind,
    failure_mode: definition.failure_mode,
    status: result.status,
    message: result.message,
    observed_at: observedAt,
    build_id: buildId,
    details: result.details,
  } satisfies DataExpectationResult;
}

export function evaluateDataExpectationsForPreview(
  definitions: DataExpectationDefinition[],
  preview?: DataExpectationPreviewInput | null,
  buildId: string | null = null,
) {
  return definitions
    .filter((definition) => definition.enabled)
    .map((definition) => evaluateDataExpectation(definition, preview, buildId));
}

export function recordDataExpectationResults(results: DataExpectationResult[]) {
  if (results.length === 0) return [];
  const existing = listDataExpectationResults();
  const next = [...results, ...existing].slice(0, MAX_RESULTS);
  writeStorage(RESULTS_KEY, next);
  const latestByExpectation = new Map(results.map((result) => [result.expectation_id, result]));
  writeStorage(DEFINITIONS_KEY, listDataExpectations().map((definition) => {
    const result = latestByExpectation.get(definition.id);
    return result ? { ...definition, last_result: result, updated_at: result.observed_at } : definition;
  }));
  return results;
}

export function publishExpectationResultsToDataHealth(results: DataExpectationResult[]) {
  const signals = results.map(expectationResultToHealthSignal);
  if (signals.length === 0) return null;
  return recordHealthReportSnapshot(generateHealthReportSnapshot(signals));
}

export function evaluateBuildExpectationGates(input: {
  pipelineId: string;
  branchName: string;
  outputDatasetRids?: string[];
  definitions?: DataExpectationDefinition[];
}) {
  const outputSet = new Set(input.outputDatasetRids ?? []);
  const definitions = (input.definitions ?? listDataExpectations())
    .filter((definition) => definition.enabled)
    .filter((definition) => (
      definition.pipeline_id === input.pipelineId
      || outputSet.has(definition.subject_dataset_rid)
    ));
  const results = definitions.map((definition) => (
    definition.last_result
      ? { ...definition.last_result, build_id: null, observed_at: new Date().toISOString() }
      : evaluateDataExpectation(definition)
  ));
  const reviewBlocked = definitions.filter((definition) => (
    isProtectedBranch(input.branchName || definition.branch_name, definition.protected_branches)
    && definition.review_status !== 'APPROVED'
  ));
  const abortingResults = results.filter((result) => (
    result.status === 'failed' && result.failure_mode === 'ABORT_BUILD'
  ));
  const warningResults = results.filter((result) => (
    result.status === 'failed' && result.failure_mode === 'WARN'
  ));
  return {
    results,
    abortingResults,
    warningResults,
    reviewBlocked,
    shouldAbort: abortingResults.length > 0,
    requiresReview: reviewBlocked.length > 0,
  } satisfies ExpectationGateResult;
}

export async function runBuildWithExpectationGates(body: CreateBuildRequest): Promise<CreateBuildResponse> {
  const gate = evaluateBuildExpectationGates({
    pipelineId: body.pipeline_rid,
    branchName: body.build_branch,
    outputDatasetRids: body.output_dataset_rids,
  });
  if (gate.requiresReview) {
    throw new Error(reviewErrorMessage(gate.reviewBlocked, body.build_branch));
  }
  if (gate.shouldAbort) {
    const buildId = createId('expectation-aborted-build');
    const results = withBuildId(gate.results, buildId);
    recordDataExpectationResults(results);
    publishExpectationResultsToDataHealth(results);
    return {
      build_id: buildId,
      state: 'BUILD_ABORTED' as BuildState,
      queued_reason: `Data expectation gate failed: ${gate.abortingResults.map((result) => result.expectation_name).join(', ')}`,
      job_count: 0,
      output_transactions: [],
    };
  }
  const response = await runBuildV1(body);
  const results = withBuildId(gate.results, response.build_id);
  recordDataExpectationResults(results);
  publishExpectationResultsToDataHealth(results);
  return response;
}

export function guardPipelineRunWithExpectationGates(input: {
  pipelineId: string;
  branchName: string;
  nodes: PipelineNode[];
}) {
  const uniqueDefinitions = collectPipelineDefinitions(input.pipelineId, input.branchName, input.nodes);
  const gate = evaluateBuildExpectationGates({
    pipelineId: input.pipelineId,
    branchName: input.branchName,
    definitions: uniqueDefinitions,
  });
  if (gate.requiresReview) throw new Error(reviewErrorMessage(gate.reviewBlocked, input.branchName));
  if (gate.shouldAbort) {
    const buildId = createId('expectation-aborted-run');
    const results = withBuildId(gate.results, buildId);
    recordDataExpectationResults(results);
    publishExpectationResultsToDataHealth(results);
    throw new Error(`Build aborted by data expectations: ${gate.abortingResults.map((result) => result.expectation_name).join(', ')}`);
  }
  recordDataExpectationResults(gate.results);
  publishExpectationResultsToDataHealth(gate.results);
  return gate;
}

export function ensureExpectationChangesReviewed(input: {
  pipelineId: string;
  branchName: string;
  nodes: PipelineNode[];
}) {
  const definitions = collectPipelineDefinitions(input.pipelineId, input.branchName, input.nodes);
  const reviewBlocked = definitions.filter((definition) => (
    definition.enabled
    && isProtectedBranch(input.branchName || definition.branch_name, definition.protected_branches)
    && definition.review_status !== 'APPROVED'
  ));
  if (reviewBlocked.length > 0) throw new Error(reviewErrorMessage(reviewBlocked, input.branchName));
  return definitions;
}

function collectPipelineDefinitions(pipelineId: string, branchName: string, nodes: PipelineNode[]) {
  const definitions = nodes.flatMap((node) => [
    ...syncNodeExpectationsToStore(pipelineId, branchName, node),
    ...listDataExpectations({ pipelineId, nodeId: node.id }),
  ]);
  return Array.from(new Map(definitions.map((definition) => [definition.id, definition])).values());
}

function evaluateDefinition(
  definition: DataExpectationDefinition,
  preview?: DataExpectationPreviewInput | null,
): { status: DataExpectationResultStatus; message: string; details: Record<string, unknown> } {
  if (!definition.enabled) {
    return { status: 'unknown', message: 'Expectation is disabled.', details: {} };
  }
  if (definition.kind === 'no_parse_errors') {
    if (preview?.error) {
      return { status: 'failed', message: `Preview parse error: ${preview.error.message ?? preview.error.kind ?? 'unknown error'}`, details: { error: preview.error } };
    }
    if (!preview) return { status: 'unknown', message: 'No preview or build error payload was available.', details: {} };
    return { status: 'passed', message: 'No parse errors were observed.', details: {} };
  }
  if (definition.kind === 'schema_has_columns') {
    const required = parseList(definition.expected_value || definition.column);
    if (required.length === 0) return { status: 'unknown', message: 'No required columns configured.', details: {} };
    const columns = preview?.columns ?? [];
    if (columns.length === 0) return { status: 'unknown', message: 'No preview schema was available.', details: { required } };
    const missing = required.filter((column) => !columns.includes(column));
    if (missing.length > 0) return { status: 'failed', message: `Missing required column(s): ${missing.join(', ')}`, details: { required, missing } };
    return { status: 'passed', message: `Required column(s) are present: ${required.join(', ')}`, details: { required } };
  }
  if (definition.kind === 'row_count_min') {
    const threshold = Number(definition.expected_value || definition.column || 1);
    if (!preview?.rows) return { status: 'unknown', message: 'No preview rows were available.', details: { threshold } };
    if (preview.rows.length < threshold) return { status: 'failed', message: `Expected at least ${threshold} row(s), observed ${preview.rows.length}.`, details: { threshold, observed: preview.rows.length } };
    return { status: 'passed', message: `Observed ${preview.rows.length} row(s), meeting the ${threshold} minimum.`, details: { threshold, observed: preview.rows.length } };
  }
  if (definition.kind === 'not_null') {
    if (!definition.column) return { status: 'unknown', message: 'No column configured.', details: {} };
    if (!preview?.rows) return { status: 'unknown', message: 'No preview rows were available.', details: { column: definition.column } };
    const failures = preview.rows.filter((row) => row[definition.column] === null || row[definition.column] === undefined || row[definition.column] === '');
    if (failures.length > 0) return { status: 'failed', message: `${failures.length} row(s) have null or empty ${definition.column}.`, details: { column: definition.column, failures: failures.length } };
    return { status: 'passed', message: `${definition.column} is populated in the preview sample.`, details: { column: definition.column } };
  }
  if (definition.kind === 'unique') {
    if (!definition.column) return { status: 'unknown', message: 'No column configured.', details: {} };
    if (!preview?.rows) return { status: 'unknown', message: 'No preview rows were available.', details: { column: definition.column } };
    const values = preview.rows.map((row) => String(row[definition.column] ?? ''));
    const duplicates = values.length - new Set(values).size;
    if (duplicates > 0) return { status: 'failed', message: `${duplicates} duplicate value(s) observed in ${definition.column}.`, details: { column: definition.column, duplicates } };
    return { status: 'passed', message: `${definition.column} is unique in the preview sample.`, details: { column: definition.column } };
  }
  return {
    status: 'unknown',
    message: 'Custom SQL expectation is registered for runtime evaluation.',
    details: { expression: definition.expected_value },
  };
}

function expectationResultToHealthSignal(result: DataExpectationResult): HealthReportSignal {
  return {
    resource_rid: result.subject_dataset_rid || `${result.pipeline_id}:${result.node_id}`,
    resource_name: result.node_label,
    resource_type: result.scope === 'output' ? 'dataset' : 'pipeline_node',
    source_surface: 'pipeline_builder',
    kind: result.kind === 'schema_has_columns' ? 'schema' : result.kind === 'row_count_min' ? 'size' : 'content',
    status: result.status === 'passed'
      ? 'healthy'
      : result.status === 'failed' && result.failure_mode === 'ABORT_BUILD'
        ? 'critical'
        : result.status === 'failed' || result.status === 'warning'
          ? 'warning'
          : 'unknown',
    message: `${result.expectation_name}: ${result.message}`,
    observed_at: result.observed_at,
    group: 'Data Expectations',
    monitoring_view: result.branch_name,
  };
}

function subjectDatasetForExpectation(node: PipelineNode, scope: DataExpectationScope) {
  if (scope === 'output') {
    return readOutputDatasetRid(node) || node.output_dataset_id || `${node.id}:output`;
  }
  return node.input_dataset_ids[0] || readString(node.config, 'dataset_rid') || readString(node.config, 'dataset_id') || `${node.id}:input`;
}

function readOutputDatasetRid(node: PipelineNode) {
  const output = node.config?._output;
  if (output && typeof output === 'object' && !Array.isArray(output)) {
    const rid = (output as Record<string, unknown>).dataset_rid;
    if (typeof rid === 'string' && rid.trim()) return rid.trim();
  }
  return readString(node.config, 'dataset_rid');
}

function readString(record: Record<string, unknown> | null | undefined, key: string) {
  const value = record?.[key];
  return typeof value === 'string' ? value.trim() : '';
}

function isProtectedBranch(branchName: string, protectedBranches = DEFAULT_PROTECTED_BRANCHES) {
  const value = branchName.trim().toLowerCase();
  if (!value) return false;
  return protectedBranches.map((branch) => branch.toLowerCase()).includes(value);
}

function reviewErrorMessage(definitions: DataExpectationDefinition[], branchName: string) {
  return `Data expectation changes on protected branch "${branchName}" require review: ${definitions.map((definition) => definition.name).join(', ')}`;
}

function withBuildId(results: DataExpectationResult[], buildId: string) {
  const observedAt = new Date().toISOString();
  return results.map((result) => ({ ...result, id: createId('data-expectation-result'), build_id: buildId, observed_at: observedAt }));
}

function parseList(value: string) {
  return value.split(/[,\n]/).map((entry) => entry.trim()).filter(Boolean);
}

function createId(prefix: string) {
  const randomId = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  return `${prefix}-${randomId}`;
}

function readStorageArray<T>(key: string): T[] {
  if (typeof localStorage === 'undefined') return [];
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as unknown;
    return Array.isArray(parsed) ? parsed as T[] : [];
  } catch {
    return [];
  }
}

function writeStorage(key: string, value: unknown) {
  if (typeof localStorage === 'undefined') return;
  localStorage.setItem(key, JSON.stringify(value));
}

function isDataExpectationDefinition(value: unknown): value is DataExpectationDefinition {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const candidate = value as Partial<DataExpectationDefinition>;
  return typeof candidate.id === 'string'
    && typeof candidate.pipeline_id === 'string'
    && typeof candidate.node_id === 'string'
    && typeof candidate.name === 'string';
}

function isDataExpectationResult(value: unknown): value is DataExpectationResult {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const candidate = value as Partial<DataExpectationResult>;
  return typeof candidate.id === 'string'
    && typeof candidate.expectation_id === 'string'
    && typeof candidate.expectation_name === 'string';
}
