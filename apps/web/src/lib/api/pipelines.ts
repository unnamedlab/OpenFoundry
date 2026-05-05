import api from './client';

export interface PipelineScheduleConfig {
  enabled: boolean;
  cron: string | null;
}

export interface PipelineRetryPolicy {
  max_attempts: number;
  retry_on_failure: boolean;
  allow_partial_reexecution: boolean;
}

export interface PipelineColumnMapping {
  source_dataset_id: string | null;
  source_column: string;
  target_column: string;
}

export interface PipelineNode {
  id: string;
  label: string;
  transform_type: string;
  config: Record<string, unknown>;
  depends_on: string[];
  input_dataset_ids: string[];
  output_dataset_id: string | null;
  incremental_input?: boolean;
  preview_status?: string;
  validation_status?: string;
  validation_errors?: string[];
}

export type PipelineType =
  | 'BATCH'
  | 'FASTER'
  | 'INCREMENTAL'
  | 'STREAMING'
  | 'EXTERNAL';

export type PipelineLifecycle =
  | 'DRAFT'
  | 'VALIDATED'
  | 'DEPLOYED'
  | 'ARCHIVED';

export interface ExternalConfig {
  source_system: string;
  source_id?: string | null;
  compute_profile_id?: string | null;
}

export interface IncrementalConfig {
  replay_on_deploy: boolean;
  watermark_columns: string[];
  allowed_transaction_types: string;
}

export interface StreamingConfig {
  input_stream_id?: string | null;
  output_stream_id?: string | null;
  streaming_profile_id?: string | null;
  parallelism: number;
}

export interface Pipeline {
  id: string;
  name: string;
  description: string;
  owner_id: string;
  dag: PipelineNode[];
  status: string;
  schedule_config: PipelineScheduleConfig;
  retry_policy: PipelineRetryPolicy;
  next_run_at: string | null;
  created_at: string;
  updated_at: string;
  pipeline_type?: string;
  lifecycle?: string;
  external_config?: ExternalConfig | null;
  incremental_config?: IncrementalConfig | null;
  streaming_config?: StreamingConfig | null;
  compute_profile_id?: string | null;
  project_id?: string | null;
}

export interface PipelineNodeResult {
  node_id: string;
  label: string;
  transform_type: string;
  status: string;
  rows_affected: number | null;
  attempts: number;
  output: Record<string, unknown> | null;
  error: string | null;
}

export interface PipelineRun {
  id: string;
  pipeline_id: string;
  status: string;
  trigger_type: string;
  started_by: string | null;
  attempt_number: number;
  started_from_node_id: string | null;
  retry_of_run_id: string | null;
  execution_context: Record<string, unknown>;
  node_results: PipelineNodeResult[] | null;
  error_message: string | null;
  started_at: string;
  finished_at: string | null;
}

export interface LineageNode {
  id: string;
  kind: string;
  label: string;
  marking: string;
  metadata: Record<string, unknown>;
}

export interface LineageEdge {
  id: string;
  source: string;
  source_kind: string;
  target: string;
  target_kind: string;
  relation_kind: string;
  pipeline_id: string | null;
  workflow_id: string | null;
  node_id: string | null;
  step_id: string | null;
  effective_marking: string;
  metadata: Record<string, unknown>;
}

export interface LineageGraph {
  nodes: LineageNode[];
  edges: LineageEdge[];
}

export interface LineagePathHop {
  source_id: string;
  source_kind: string;
  target_id: string;
  target_kind: string;
  relation_kind: string;
  effective_marking: string;
}

export interface LineageImpactItem {
  id: string;
  kind: string;
  label: string;
  distance: number;
  marking: string;
  effective_marking: string;
  requires_acknowledgement: boolean;
  metadata: Record<string, unknown>;
  path: LineagePathHop[];
}

export interface LineageBuildCandidate {
  id: string;
  kind: string;
  label: string;
  status: string | null;
  distance: number;
  triggerable: boolean;
  marking: string;
  effective_marking: string;
  requires_acknowledgement: boolean;
  blocked_reason: string | null;
  metadata: Record<string, unknown>;
}

export interface LineageImpactAnalysis {
  root: LineageNode;
  propagated_marking: string;
  upstream: LineageImpactItem[];
  downstream: LineageImpactItem[];
  build_candidates: LineageBuildCandidate[];
}

export interface LineageBuildTriggerResult {
  id: string;
  kind: string;
  label: string;
  run_id: string | null;
  status: string;
  message: string | null;
}

export interface LineageBuildResult {
  root: LineageNode;
  dry_run: boolean;
  acknowledged_sensitive_lineage: boolean;
  propagated_marking: string;
  candidates: LineageBuildCandidate[];
  triggered: LineageBuildTriggerResult[];
  skipped: LineageBuildTriggerResult[];
}

export interface ColumnLineageEdge {
  id: string;
  source_dataset_id: string;
  source_column: string;
  target_dataset_id: string;
  target_column: string;
  pipeline_id: string | null;
  node_id: string | null;
  created_at: string;
}

// Pipeline CRUD
export function listPipelines(params?: { page?: number; per_page?: number; search?: string; status?: string }) {
  const qs = new URLSearchParams();
  if (params?.page) qs.set('page', String(params.page));
  if (params?.per_page) qs.set('per_page', String(params.per_page));
  if (params?.search) qs.set('search', params.search);
  if (params?.status) qs.set('status', params.status);
  return api.get<{ data: Pipeline[]; total: number; page: number; per_page: number }>(
    `/pipelines?${qs}`,
  );
}

export function getPipeline(id: string) {
  return api.get<Pipeline>(`/pipelines/${id}`);
}

export function createPipeline(body: {
  name: string;
  description?: string;
  status?: string;
  nodes: PipelineNode[];
  schedule_config?: PipelineScheduleConfig;
  retry_policy?: PipelineRetryPolicy;
  pipeline_type?: PipelineType;
  external?: ExternalConfig;
  incremental?: IncrementalConfig;
  streaming?: StreamingConfig;
  compute_profile_id?: string;
  project_id?: string;
}) {
  return api.post<Pipeline>('/pipelines', body);
}

export function updatePipeline(id: string, body: {
  name?: string;
  description?: string;
  status?: string;
  nodes?: PipelineNode[];
  schedule_config?: PipelineScheduleConfig;
  retry_policy?: PipelineRetryPolicy;
  pipeline_type?: PipelineType;
  lifecycle?: PipelineLifecycle;
  external?: ExternalConfig;
  incremental?: IncrementalConfig;
  streaming?: StreamingConfig;
  compute_profile_id?: string;
  project_id?: string;
}) {
  return api.put<Pipeline>(`/pipelines/${id}`, body);
}

export function deletePipeline(id: string) {
  return api.delete(`/pipelines/${id}`);
}

// Validation / compilation (Foundry: "Validate" and "Preview" buttons in
// Pipeline Builder before Deploy). These accept the in-flight DAG from the
// canvas — they do NOT require a persisted pipeline row.
export interface PipelineValidationIssue {
  level?: string;
  message: string;
  node_id?: string;
}

export interface PipelineGraphSummary {
  node_count: number;
  edge_count: number;
  root_node_ids: string[];
  leaf_node_ids: string[];
}

export interface PipelineValidationResponse {
  valid: boolean;
  errors: string[];
  warnings: string[];
  next_run_at: string | null;
  summary: PipelineGraphSummary;
}

export interface ExecutablePlan {
  topological_order: string[];
  stages: string[][];
  summary: PipelineGraphSummary;
}

export interface CompilePipelineResponse {
  validation: PipelineValidationResponse;
  plan: ExecutablePlan;
}

export interface PrunePipelineResponse {
  validation: PipelineValidationResponse;
  pruned_nodes: PipelineNode[];
  removed_node_ids: string[];
}

export interface ValidatePipelineRequest {
  status: string;
  schedule_config: PipelineScheduleConfig;
  nodes: PipelineNode[];
}

export interface CompilePipelineRequest extends ValidatePipelineRequest {
  start_from_node?: string | null;
}

export function validatePipeline(body: ValidatePipelineRequest) {
  return api.post<PipelineValidationResponse>('/pipelines/_validate', body);
}

// FASE 3 — id-scoped, type-safe validator. The canvas calls this on
// every config change (debounced ~250 ms) to render the squiggle
// overlay and the per-node ✓/⚠/✗ icons.
export interface NodeValidationError {
  node_id: string;
  column: string | null;
  message: string;
}

export interface NodeValidationReport {
  node_id: string;
  status: 'VALID' | 'INVALID' | 'PENDING';
  errors: NodeValidationError[];
}

export interface PipelineValidationByIdResponse {
  pipeline_id: string;
  all_valid: boolean;
  nodes: NodeValidationReport[];
}

export function validatePipelineById(pipelineId: string) {
  return api.post<PipelineValidationByIdResponse>(
    `/pipelines/${pipelineId}/validate`,
    {},
  );
}

// FASE 4 — node-level preview. The canvas's lower preview panel hits
// this endpoint whenever the operator selects a node. The backend
// walks the chain back to leaf inputs, applies each transform in
// memory and returns a deterministic sample window.
export interface PipelinePreviewOutput {
  pipeline_id: string;
  node_id: string;
  columns: string[];
  rows: Array<Record<string, unknown>>;
  sample_size: number;
  generated_at: string;
  seed: number;
  source_chain: string[];
  fresh: boolean;
}

export function previewPipelineNode(
  pipelineId: string,
  nodeId: string,
  params?: { sample_size?: number },
) {
  const qs = new URLSearchParams();
  if (params?.sample_size) qs.set('sample_size', String(params.sample_size));
  const suffix = qs.toString() ? `?${qs}` : '';
  return api.post<PipelinePreviewOutput>(
    `/pipelines/${pipelineId}/nodes/${encodeURIComponent(nodeId)}/preview${suffix}`,
    {},
  );
}

export function compilePipeline(body: CompilePipelineRequest) {
  return api.post<CompilePipelineResponse>('/pipelines/_compile', body);
}

export function prunePipeline(body: CompilePipelineRequest) {
  return api.post<PrunePipelineResponse>('/pipelines/_prune', body);
}

// Execution (Foundry: "Build dataset" / "Build downstream" / "Run").
export function triggerRun(pipelineId: string, body?: { from_node_id?: string; context?: Record<string, unknown>; skip_unchanged?: boolean }) {
  return api.post<PipelineRun>(`/pipelines/${pipelineId}/runs`, body ?? {});
}

export function listRuns(pipelineId: string, params?: { page?: number; per_page?: number }) {
  const qs = new URLSearchParams();
  if (params?.page) qs.set('page', String(params.page));
  if (params?.per_page) qs.set('per_page', String(params.per_page));
  return api.get<{ data: PipelineRun[] }>(`/pipelines/${pipelineId}/runs?${qs}`);
}

export function getRun(pipelineId: string, runId: string) {
  return api.get<PipelineRun>(`/pipelines/${pipelineId}/runs/${runId}`);
}

export function retryPipelineRun(pipelineId: string, runId: string, body?: { from_node_id?: string; skip_unchanged?: boolean }) {
  return api.post<PipelineRun>(`/pipelines/${pipelineId}/runs/${runId}/retry`, body ?? {});
}

// Scheduler (Foundry: "Schedules" tab and ops dispatch). Forces immediate
// dispatch of any pipeline whose next_run_at <= now.
export function runDuePipelines() {
  return api.post<{ triggered_runs: number }>('/pipelines/_scheduler/run-due', {});
}

// Builds queue (Foundry: "Builds" application). Cross-pipeline visibility
// of every run, abort path, and 24h status summary.
export interface BuildsQueueQuery {
  status?: 'running' | 'completed' | 'failed' | 'aborted';
  trigger_type?: 'manual' | 'scheduled' | 'event' | 'retry';
  pipeline_id?: string;
  page?: number;
  per_page?: number;
}

export function listBuilds(params: BuildsQueueQuery = {}) {
  const qs = new URLSearchParams();
  if (params.status) qs.set('status', params.status);
  if (params.trigger_type) qs.set('trigger_type', params.trigger_type);
  if (params.pipeline_id) qs.set('pipeline_id', params.pipeline_id);
  if (params.page) qs.set('page', String(params.page));
  if (params.per_page) qs.set('per_page', String(params.per_page));
  return api.get<{ data: PipelineRun[]; page: number; per_page: number }>(`/builds?${qs}`);
}

export function getBuildsSummary() {
  return api.get<{ last_24h: Record<string, number> }>('/builds/_summary');
}

export function abortBuild(runId: string) {
  return api.post<PipelineRun>(`/builds/${runId}/abort`, {});
}

export interface DueRunRecord {
  target_kind: 'pipeline' | 'workflow';
  target_id: string;
  name: string;
  due_at: string;
  schedule_expression: string;
  trigger_type: string;
}

export interface ScheduleWindow {
  scheduled_for: string;
  window_start: string;
  window_end: string;
}

export function listDueScheduleRuns(params?: { kind?: 'pipeline' | 'workflow'; limit?: number }) {
  const qs = new URLSearchParams();
  if (params?.kind) qs.set('kind', params.kind);
  if (params?.limit) qs.set('limit', String(params.limit));
  return api.get<{ data: DueRunRecord[]; total: number }>(`/schedules/due?${qs}`);
}

export function previewScheduleWindows(body: {
  target_kind: 'pipeline' | 'workflow';
  target_id: string;
  start_at: string;
  end_at: string;
  limit?: number;
}) {
  return api.post<{ target_kind: string; target_id: string; data: ScheduleWindow[] }>(
    '/schedules/preview',
    body,
  );
}

export function backfillSchedule(body: {
  target_kind: 'pipeline' | 'workflow';
  target_id: string;
  start_at: string;
  end_at: string;
  limit?: number;
  dry_run?: boolean;
  context?: Record<string, unknown>;
  skip_unchanged?: boolean;
}) {
  return api.post('/schedules/backfill', body);
}

// Lineage
export function getDatasetLineage(datasetId: string) {
  return api.get<LineageGraph>(`/lineage/datasets/${datasetId}`);
}

export function getDatasetColumnLineage(datasetId: string) {
  return api.get<ColumnLineageEdge[]>(`/lineage/datasets/${datasetId}/columns`);
}

export function getDatasetLineageImpact(datasetId: string) {
  return api.get<LineageImpactAnalysis>(`/lineage/datasets/${datasetId}/impact`);
}

export function triggerLineageBuilds(datasetId: string, body?: {
  include_workflows?: boolean;
  dry_run?: boolean;
  acknowledge_sensitive_lineage?: boolean;
  max_depth?: number;
  context?: Record<string, unknown>;
}) {
  return api.post<LineageBuildResult>(`/lineage/datasets/${datasetId}/builds`, body ?? {});
}

export function getFullLineage() {
  return api.get<LineageGraph>('/lineage');
}
