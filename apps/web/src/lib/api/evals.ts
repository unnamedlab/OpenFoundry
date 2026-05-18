import api from './client';

export type EvaluationSuiteSourceSurface =
  | 'logic_preview'
  | 'evals_sidebar'
  | 'aip_evals_app'
  | 'code_function_published'
  | 'api';

export type EvaluationTargetFunctionKind = 'logic' | 'agent_like' | 'code_function';

export interface EvaluationSignatureParameter {
  id?: string;
  apiName: string;
  name?: string;
  type?: string;
  outputType?: string;
  [key: string]: unknown;
}

export interface EvaluationTargetFunction {
  id: string;
  kind: EvaluationTargetFunctionKind;
  function_rid?: string;
  functionRid?: string;
  agent_id?: string;
  agentId?: string;
  version: 'published' | 'last_saved' | 'last_saved_or_preview' | 'draft' | 'current' | 'latest' | 'specific' | string;
  version_id?: string;
  versionId?: string;
  signature: {
    inputs: EvaluationSignatureParameter[];
    outputs: EvaluationSignatureParameter[];
  };
  [key: string]: unknown;
}

export interface EvaluationSuiteColumn {
  id: string;
  name: string;
  apiName: string;
  type: string;
  role: 'input' | 'expected_output' | 'intermediate_parameter' | 'metadata' | string;
  [key: string]: unknown;
}

export interface EvaluationTestCase {
  id: string;
  name: string;
  values: Record<string, unknown>;
  metadata?: Record<string, unknown>;
  generated_name_hint?: string;
  generatedNameHint?: string;
  source?: 'manual' | 'logic_preview' | 'generated' | 'object_set' | string;
  object_set_backing_id?: string;
  objectSetBackingId?: string;
  [key: string]: unknown;
}

export interface EvaluationEvaluator {
  id: string;
  kind: 'built_in' | 'custom_function' | 'marketplace_function' | string;
  evaluator: string;
  function_rid?: string;
  functionRid?: string;
  function_kind?: 'typescript' | 'python' | 'logic' | string;
  functionKind?: 'typescript' | 'python' | 'logic' | string;
  version?: 'published' | string;
  marketplace_product_slug?: string;
  marketplaceProductSlug?: string;
  marketplace_listing_id?: string;
  marketplaceListingId?: string;
  marketplace_install_status?: 'installed' | 'setup_required' | 'missing' | string;
  marketplaceInstallStatus?: 'installed' | 'setup_required' | 'missing' | string;
  marketplace_dependency_plan?: Array<Record<string, unknown>>;
  marketplaceDependencyPlan?: Array<Record<string, unknown>>;
  return_signature?: {
    outputs?: EvaluationSignatureParameter[];
    [key: string]: unknown;
  };
  returnSignature?: {
    outputs?: EvaluationSignatureParameter[];
    [key: string]: unknown;
  };
  mappings?: Record<string, string>;
  target_id?: string;
  targetId?: string;
  target_mappings?: Record<string, Record<string, string>>;
  targetMappings?: Record<string, Record<string, string>>;
  metric_objectives?: Record<string, Record<string, unknown>>;
  metricObjectives?: Record<string, Record<string, unknown>>;
  config?: Record<string, unknown>;
  objective?: Record<string, unknown>;
  [key: string]: unknown;
}

export interface EvaluationSuite {
  id: string;
  name: string;
  description?: string | null;
  project_id: string;
  folder_id: string;
  owner_id: string;
  target_functions: EvaluationTargetFunction[];
  test_case_columns: EvaluationSuiteColumn[];
  test_cases: EvaluationTestCase[];
  evaluators: EvaluationEvaluator[];
  run_history: Array<Record<string, unknown>>;
  results_dataset_rid?: string | null;
  permissions: Record<string, string[]>;
  source_surface: EvaluationSuiteSourceSurface | string;
  source_resource_id?: string | null;
  archived_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface CreateEvaluationSuiteRequest {
  name: string;
  description?: string | null;
  project_id: string;
  folder_id: string;
  target_functions?: EvaluationTargetFunction[];
  test_case_columns?: EvaluationSuiteColumn[];
  test_cases?: EvaluationTestCase[];
  evaluators?: EvaluationEvaluator[];
  run_history?: Array<Record<string, unknown>>;
  results_dataset_rid?: string | null;
  permissions?: Record<string, string[]>;
  source_surface?: EvaluationSuiteSourceSurface;
  source_resource_id?: string | null;
}

export type UpdateEvaluationSuiteRequest = Partial<Pick<
  CreateEvaluationSuiteRequest,
  'name' | 'description' | 'target_functions' | 'test_case_columns' | 'test_cases' | 'evaluators' | 'run_history' | 'results_dataset_rid' | 'permissions'
>>;

export function listEvaluationSuites(params?: {
  project_id?: string;
  folder_id?: string;
  include_archived?: boolean;
}) {
  const qs = new URLSearchParams();
  if (params?.project_id) qs.set('project_id', params.project_id);
  if (params?.folder_id) qs.set('folder_id', params.folder_id);
  if (params?.include_archived) qs.set('include_archived', 'true');
  const tail = qs.toString();
  return api.get<EvaluationSuite[]>(`/agent-runtime/eval-suites${tail ? `?${tail}` : ''}`);
}

export function createEvaluationSuite(body: CreateEvaluationSuiteRequest) {
  return api.post<EvaluationSuite>('/agent-runtime/eval-suites', body);
}

export function getEvaluationSuite(id: string, params?: { include_archived?: boolean }) {
  const qs = new URLSearchParams();
  if (params?.include_archived) qs.set('include_archived', 'true');
  const tail = qs.toString();
  return api.get<EvaluationSuite>(`/agent-runtime/eval-suites/${encodeURIComponent(id)}${tail ? `?${tail}` : ''}`);
}

export function updateEvaluationSuite(id: string, body: UpdateEvaluationSuiteRequest) {
  return api.patch<EvaluationSuite>(`/agent-runtime/eval-suites/${encodeURIComponent(id)}`, body);
}

export function moveEvaluationSuite(id: string, body: { project_id: string; folder_id: string }) {
  return api.post<EvaluationSuite>(`/agent-runtime/eval-suites/${encodeURIComponent(id)}/move`, body);
}

export function duplicateEvaluationSuite(id: string, body: {
  name?: string;
  description?: string | null;
  project_id?: string;
  folder_id?: string;
} = {}) {
  return api.post<EvaluationSuite>(`/agent-runtime/eval-suites/${encodeURIComponent(id)}/duplicate`, body);
}

export function archiveEvaluationSuite(id: string) {
  return api.delete<EvaluationSuite>(`/agent-runtime/eval-suites/${encodeURIComponent(id)}`);
}

export function restoreEvaluationSuite(id: string) {
  return api.post<EvaluationSuite>(`/agent-runtime/eval-suites/${encodeURIComponent(id)}/restore`, {});
}

export interface RunEvaluationSuiteRequest {
  target_ids?: string[];
  test_case_ids?: string[];
  target_versions?: Record<string, string>;
  iterations?: number;
  parallelization?: number;
  execution_mode?: 'user_scoped' | 'project_scoped';
  input_mappings?: Record<string, Record<string, string>>;
  target_models?: Record<string, string>;
  metadata?: Record<string, unknown>;
  results_dataset_rid?: string | null;
}

export interface EvaluationRunResponse {
  id: string;
  suite_id: string;
  status: 'queued' | 'running' | 'completed' | 'error' | string;
  started_at?: string;
  completed_at?: string;
  result_dataset_rid?: string | null;
  summary?: Record<string, unknown>;
  [key: string]: unknown;
}

export interface EvaluationResultsDatasetRequest {
  dataset_rid: string;
  write_mode?: 'append' | 'overwrite' | string;
  include_debug_outputs?: boolean;
  include_intermediate_parameters?: boolean;
}

export interface CreateEvaluationExperimentRequest {
  dimensions: Array<Record<string, unknown>>;
  base_config?: RunEvaluationSuiteRequest;
  max_runs?: number;
  budget_policy?: Record<string, unknown>;
}

export interface EvaluationAnalyzerJobRequest {
  run_id: string;
  model?: string;
  max_categories?: number;
  max_failing_test_cases?: number;
}

export function runEvaluationSuite(id: string, body: RunEvaluationSuiteRequest = {}) {
  return api.post<EvaluationRunResponse>(`/agent-runtime/eval-suites/${encodeURIComponent(id)}/runs`, body);
}

export function getEvaluationRun(suiteId: string, runId: string) {
  return api.get<EvaluationRunResponse>(`/agent-runtime/eval-suites/${encodeURIComponent(suiteId)}/runs/${encodeURIComponent(runId)}`);
}

export function getEvaluationRunResults(suiteId: string, runId: string) {
  return api.get<Record<string, unknown>>(`/agent-runtime/eval-suites/${encodeURIComponent(suiteId)}/runs/${encodeURIComponent(runId)}/results`);
}

export function configureEvaluationResultsDataset(id: string, body: EvaluationResultsDatasetRequest) {
  return api.post<EvaluationSuite>(`/agent-runtime/eval-suites/${encodeURIComponent(id)}/results-dataset`, body);
}

export function createEvaluationExperiment(id: string, body: CreateEvaluationExperimentRequest) {
  return api.post<Record<string, unknown>>(`/agent-runtime/eval-suites/${encodeURIComponent(id)}/experiments`, body);
}

export function runEvaluationExperimentApi(id: string, experimentId: string) {
  return api.post<Record<string, unknown>>(`/agent-runtime/eval-suites/${encodeURIComponent(id)}/experiments/${encodeURIComponent(experimentId)}/runs`, {});
}

export function createEvaluationAnalyzerJob(id: string, body: EvaluationAnalyzerJobRequest) {
  return api.post<Record<string, unknown>>(`/agent-runtime/eval-suites/${encodeURIComponent(id)}/analyzer-jobs`, body);
}
