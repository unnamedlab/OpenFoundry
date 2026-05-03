import api from './client';

export interface Dataset {
  id: string;
  name: string;
  description: string;
  format: string;
  storage_path: string;
  size_bytes: number;
  row_count: number;
  owner_id: string;
  tags: string[];
  current_version: number;
  active_branch: string;
  created_at: string;
  updated_at: string;
}

export interface DatasetListResponse {
  data: Dataset[];
  page: number;
  per_page: number;
  total: number;
  total_pages: number;
}

export interface CreateDatasetParams {
  name: string;
  description?: string;
  format?: string;
  tags?: string[];
}

export interface UpdateDatasetParams {
  name?: string;
  description?: string;
  owner_id?: string;
  tags?: string[];
}

export interface DatasetVersion {
  id: string;
  dataset_id: string;
  version: number;
  message: string;
  size_bytes: number;
  row_count: number;
  storage_path: string;
  transaction_id?: string | null;
  created_at: string;
}

export interface DatasetPreviewResponse {
  dataset_id: string;
  version?: number;
  size_bytes?: number;
  format?: string;
  branch?: string | null;
  storage_path?: string;
  limit?: number;
  offset?: number;
  row_count?: number;
  rows?: Array<Record<string, unknown>>;
  columns?: Array<{
    name: string;
    field_type?: string;
    data_type?: string;
    nullable?: boolean;
  }>;
  total_rows?: number;
  warnings?: string[];
  errors?: string[];
  message?: string;
}

export interface DatasetSchema {
  id: string;
  dataset_id: string;
  fields: unknown;
  created_at: string;
}

// ─── T6.x — view-scoped Foundry schema ───────────────────────────────────────
// Mirrors `services/dataset-versioning-service/src/models/schema.rs`. The
// `type` discriminator on `DatasetField.field_type` matches the JSON layout
// produced by the Rust serde model.

export type DatasetFileFormat = 'PARQUET' | 'AVRO' | 'TEXT';

export type DatasetFieldType =
  | { type: 'BOOLEAN' }
  | { type: 'BYTE' }
  | { type: 'SHORT' }
  | { type: 'INTEGER' }
  | { type: 'LONG' }
  | { type: 'FLOAT' }
  | { type: 'DOUBLE' }
  | { type: 'STRING' }
  | { type: 'BINARY' }
  | { type: 'DATE' }
  | { type: 'TIMESTAMP' }
  | { type: 'DECIMAL'; precision?: number; scale?: number }
  | { type: 'ARRAY'; arraySubType?: DatasetField }
  | { type: 'MAP'; mapKeyType?: DatasetField; mapValueType?: DatasetField }
  | { type: 'STRUCT'; subSchemas?: DatasetField[] };

export interface DatasetField {
  name: string;
  type: DatasetFieldType['type'];
  nullable?: boolean;
  description?: string;
  // Decimal
  precision?: number;
  scale?: number;
  // Array
  arraySubType?: DatasetField;
  // Map
  mapKeyType?: DatasetField;
  mapValueType?: DatasetField;
  // Struct
  subSchemas?: DatasetField[];
}

export interface DatasetCsvOptions {
  delimiter: string;
  quote: string;
  escape: string;
  header: boolean;
  null_value: string;
  date_format?: string;
  timestamp_format?: string;
  charset: string;
}

export interface DatasetCustomMetadata {
  csv?: DatasetCsvOptions;
}

export interface DatasetSchemaPayload {
  fields: DatasetField[];
  file_format: DatasetFileFormat;
  custom_metadata?: DatasetCustomMetadata | null;
}

export interface DatasetSchemaResponse {
  view_id: string;
  dataset_id: string;
  branch?: string | null;
  schema: DatasetSchemaPayload;
  content_hash: string;
  created_at: string;
  unchanged?: boolean;
}

export interface DatasetBranch {
  id: string;
  dataset_id: string;
  name: string;
  version: number;
  base_version?: number;
  description: string;
  is_default: boolean;
  created_at: string;
  updated_at: string;
  // P1 — Foundry-style branch model. Optional so the type stays
  // backwards-compatible with the legacy payload that still surfaces
  // through `handlers::branches`.
  rid?: string;
  dataset_rid?: string;
  parent_branch_id?: string | null;
  head_transaction_id?: string | null;
  created_from_transaction_id?: string | null;
  last_activity_at?: string;
  fallback_chain?: string[];
  labels?: Record<string, string>;
  has_open_transaction?: boolean;
  // P4 — retention + archival columns. Optional so legacy payloads
  // that pre-date the `20260504000030_branch_retention` migration
  // still parse cleanly.
  retention_policy?: 'INHERITED' | 'FOREVER' | 'TTL_DAYS';
  retention_ttl_days?: number | null;
  archived_at?: string | null;
  archive_grace_until?: string | null;
}

/// P3 — wire shape used by the BranchGraph + dashboard.
export interface DatasetBranchAncestor {
  rid: string;
  name: string;
  is_root: boolean;
}

export interface DatasetBranchPreviewDelete {
  branch: string;
  branch_rid: string;
  current_parent: string | null;
  current_parent_rid: string | null;
  children_to_reparent: Array<{
    branch: string;
    branch_rid: string;
    new_parent: string | null;
    new_parent_rid: string | null;
  }>;
  transactions_preserved: boolean;
  head_transaction: { id: string; rid: string } | null;
}

export interface DatasetBranchDeleteResponse {
  branch: string;
  branch_rid: string;
  reparented: Array<{
    child_branch: string;
    child_branch_rid: string;
    new_parent: string | null;
    new_parent_rid: string | null;
  }>;
}

export interface BranchSourceFromBranch { from_branch: string; }
export interface BranchSourceFromTransaction { from_transaction_rid: string; }
export interface BranchSourceAsRoot { as_root: true; }
export type BranchSource =
  | BranchSourceFromBranch
  | BranchSourceFromTransaction
  | BranchSourceAsRoot;

export interface CreateBranchV2Params {
  name: string;
  source: BranchSource;
  fallback_chain?: string[];
  labels?: Record<string, string>;
  description?: string;
}

export interface ReparentBranchParams {
  new_parent_branch?: string | null;
}

export interface DatasetJobSpecStatus {
  has_master_jobspec: boolean;
  branches_with_jobspec: string[];
}

export interface DatasetJobSpecRow {
  id: string;
  rid: string;
  pipeline_rid: string;
  branch_name: string;
  output_dataset_rid: string;
  output_branch: string;
  job_spec_json: unknown;
  inputs: unknown;
  content_hash: string;
  version: number;
  published_by: string;
  published_at: string;
}

export interface DatasetView {
  id: string;
  dataset_id: string;
  name: string;
  description: string;
  sql_text: string;
  source_branch?: string | null;
  source_version?: number | null;
  materialized: boolean;
  refresh_on_source_update: boolean;
  format: string;
  current_version: number;
  storage_path?: string | null;
  row_count: number;
  schema_fields: unknown;
  last_refreshed_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface CreateDatasetViewParams {
  name: string;
  description?: string;
  sql: string;
  source_branch?: string;
  source_version?: number;
  materialized?: boolean;
  refresh_on_source_update?: boolean;
}

export interface DatasetFilesystemEntry {
  entry_type: 'directory' | 'file';
  name: string;
  path: string;
  storage_path?: string;
  size_bytes?: number;
  last_modified?: string;
  content_type?: string | null;
  metadata: Record<string, unknown>;
}

export interface DatasetFilesystemResponse {
  dataset_id: string;
  requested_path: string;
  root: string;
  current_version: number;
  active_branch: string;
  entries: DatasetFilesystemEntry[];
  items: DatasetFilesystemEntry[];
  breadcrumbs: Array<{ name: string; path: string }>;
  sections: {
    versions: number;
    branches: number;
    views: number;
  };
}

export interface DatasetTransaction {
  id: string;
  dataset_id: string;
  view_id?: string | null;
  operation: string;
  branch_name?: string | null;
  status: string;
  summary: string;
  metadata: Record<string, unknown>;
  created_at: string;
  committed_at?: string | null;
}

export interface CreateDatasetBranchParams {
  name: string;
  source_version?: number;
  description?: string;
}

export interface CatalogTagFacet {
  value: string;
  count: number;
}

export interface CatalogOwnerFacet {
  owner_id: string;
  count: number;
}

export interface DatasetCatalogFacets {
  tags: CatalogTagFacet[];
  owners: CatalogOwnerFacet[];
}

export interface DatasetValueCount {
  value: string;
  count: number;
}

export interface DatasetColumnProfile {
  name: string;
  field_type: string;
  nullable: boolean;
  null_count: number;
  null_rate: number;
  distinct_count: number;
  uniqueness_rate: number;
  sample_values: DatasetValueCount[];
  min_value: string | null;
  max_value: string | null;
  average_value: number | null;
}

export interface DatasetRuleResult {
  rule_id: string;
  name: string;
  rule_type: string;
  severity: string;
  passed: boolean;
  measured_value: string | null;
  message: string;
}

export interface DatasetQualityProfile {
  row_count: number;
  column_count: number;
  duplicate_rows: number;
  completeness_ratio: number;
  uniqueness_ratio: number;
  generated_at: string;
  columns: DatasetColumnProfile[];
  rule_results: DatasetRuleResult[];
}

export interface DatasetQualityRule {
  id: string;
  dataset_id: string;
  name: string;
  rule_type: string;
  severity: string;
  config: Record<string, unknown>;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface DatasetQualityHistoryEntry {
  id: string;
  dataset_id: string;
  score: number;
  passed_rules: number;
  failed_rules: number;
  alerts_count: number;
  created_at: string;
}

export interface DatasetQualityAlert {
  id: string;
  dataset_id: string;
  level: string;
  kind: string;
  message: string;
  status: string;
  details: Record<string, unknown>;
  created_at: string;
  resolved_at: string | null;
}

export interface DatasetQualityResponse {
  profile: DatasetQualityProfile | null;
  score: number | null;
  history: DatasetQualityHistoryEntry[];
  alerts: DatasetQualityAlert[];
  rules: DatasetQualityRule[];
  profiled_at: string | null;
}

export interface DatasetLintSummary {
  resource_posture: string;
  total_findings: number;
  high_severity: number;
  medium_severity: number;
  low_severity: number;
  tracked_versions: number;
  branch_count: number;
  stale_branch_count: number;
  materialized_view_count: number;
  auto_refresh_view_count: number;
  transaction_count: number;
  failed_transaction_count: number;
  pending_transaction_count: number;
  enabled_rule_count: number;
  active_alert_count: number;
  object_count: number;
  small_file_count: number;
  largest_object_bytes: number;
  average_object_size_bytes: number;
  quality_score: number | null;
}

export interface DatasetLintFinding {
  code: string;
  title: string;
  severity: string;
  category: string;
  description: string;
  evidence: string[];
  impact: string;
  recommendation: string;
}

export interface DatasetLintRecommendation {
  code: string;
  priority: string;
  title: string;
  rationale: string;
  actions: string[];
}

export interface DatasetLintResponse {
  dataset_id: string;
  dataset_name: string;
  analyzed_at: string;
  summary: DatasetLintSummary;
  findings: DatasetLintFinding[];
  recommendations: DatasetLintRecommendation[];
}

export interface CreateDatasetQualityRuleParams {
  name: string;
  rule_type: string;
  severity?: string;
  enabled?: boolean;
  config: Record<string, unknown>;
}

export interface UpdateDatasetQualityRuleParams {
  name?: string;
  severity?: string;
  enabled?: boolean;
  config?: Record<string, unknown>;
}

export function listDatasets(params?: { page?: number; per_page?: number; search?: string; tag?: string; owner_id?: string }) {
  const query = new URLSearchParams();
  if (params?.page) query.set('page', String(params.page));
  if (params?.per_page) query.set('per_page', String(params.per_page));
  if (params?.search) query.set('search', params.search);
  if (params?.tag) query.set('tag', params.tag);
  if (params?.owner_id) query.set('owner_id', params.owner_id);
  const qs = query.toString();
  return api.get<DatasetListResponse>(`/datasets${qs ? `?${qs}` : ''}`);
}

export function getCatalogFacets() {
  return api.get<DatasetCatalogFacets>('/datasets/catalog/facets');
}

export function getDataset(id: string) {
  return api.get<Dataset>(`/datasets/${id}`);
}

export function previewDataset(datasetId: string, params?: { limit?: number; offset?: number; version?: number; branch?: string }) {
  const query = new URLSearchParams();
  if (params?.limit) query.set('limit', String(params.limit));
  if (params?.offset) query.set('offset', String(params.offset));
  if (params?.version !== undefined) query.set('version', String(params.version));
  if (params?.branch) query.set('branch', params.branch);
  const qs = query.toString();
  return api.get<DatasetPreviewResponse>(`/datasets/${datasetId}/preview${qs ? `?${qs}` : ''}`);
}

export function getDatasetSchema(datasetId: string) {
  return api.get<DatasetSchema>(`/datasets/${datasetId}/schema`);
}

export function getViewSchema(datasetId: string, viewId: string) {
  return api.get<DatasetSchemaResponse>(
    `/datasets/${datasetId}/views/${viewId}/schema`,
  );
}

// ─── P2 — view-scoped Foundry preview ─────────────────────────────────────
// Mirrors `services/dataset-versioning-service/src/storage/preview.rs::PreviewPage`.

export interface ViewPreviewCsvOptions {
  delimiter: string;
  quote: string;
  escape: string;
  header: boolean;
  null_value: string;
  date_format?: string | null;
  timestamp_format?: string | null;
  charset: string;
}

export interface ViewPreviewColumn {
  name: string;
  field_type: string;
  nullable: boolean;
}

export interface ViewPreviewResponse {
  view_id: string;
  dataset_id: string;
  branch?: string | null;
  file_format: string;
  text_sub_format?: string | null;
  limit: number;
  offset: number;
  row_count: number;
  total_rows: number;
  columns: ViewPreviewColumn[];
  rows: Array<Record<string, unknown>>;
  schema_inferred: boolean;
  csv_options?: ViewPreviewCsvOptions | null;
  warnings: string[];
  errors: string[];
}

export interface ViewPreviewParams {
  limit?: number;
  offset?: number;
  format?: 'auto' | 'parquet' | 'avro' | 'text';
  csv_delimiter?: string;
  csv_quote?: string;
  csv_escape?: string;
  csv_header?: boolean;
  csv_null_value?: string;
  csv_charset?: string;
  csv_date_format?: string;
  csv_timestamp_format?: string;
  csv?: boolean;
}

export function previewView(
  datasetId: string,
  viewId: string,
  params: ViewPreviewParams = {},
) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null) continue;
    query.set(key, String(value));
  }
  const qs = query.toString();
  return api.get<ViewPreviewResponse>(
    `/datasets/${datasetId}/views/${viewId}/data${qs ? `?${qs}` : ''}`,
  );
}

// ─── P3 — Foundry "Backing filesystem" Files tab ──────────────────────────
// Mirrors `services/dataset-versioning-service/src/handlers/files.rs`.

export type DatasetBackingFileStatus = 'active' | 'deleted';

export interface DatasetBackingFile {
  id: string;
  dataset_id: string;
  transaction_id: string;
  logical_path: string;
  physical_uri: string;
  size_bytes: number;
  sha256?: string | null;
  created_at: string;
  modified_at: string;
  status: DatasetBackingFileStatus;
}

export interface DatasetFilesResponse {
  view_id?: string | null;
  branch: string;
  total: number;
  files: DatasetBackingFile[];
}

export interface ListDatasetFilesParams {
  branch?: string;
  view_id?: string;
  prefix?: string;
}

export function listDatasetFiles(datasetId: string, params: ListDatasetFilesParams = {}) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === '') continue;
    query.set(key, String(value));
  }
  const qs = query.toString();
  return api.get<DatasetFilesResponse>(
    `/datasets/${datasetId}/files${qs ? `?${qs}` : ''}`,
  );
}

/** Return the absolute URL the browser should follow to download a
 *  file. The DVS endpoint returns a 302 to a presigned URL; the browser
 *  follows it transparently when this URL is used as the `href` of an
 *  `<a>` or assigned to `window.location`. */
export function datasetFileDownloadUrl(datasetId: string, fileId: string): string {
  return `/api/v1/datasets/${datasetId}/files/${fileId}/download`;
}

export interface DatasetStorageDetails {
  fs_id: string;
  driver: 'local' | 's3' | 'hdfs';
  base_directory: string;
  presign_ttl_seconds: number;
  total_active_bytes: number;
  total_active_files: number;
  total_deleted_bytes: number;
  total_deleted_files: number;
}

export function getDatasetStorageDetails(datasetId: string) {
  return api.get<DatasetStorageDetails>(`/datasets/${datasetId}/storage-details`);
}

export function putViewSchema(
  datasetId: string,
  viewId: string,
  schema: DatasetSchemaPayload,
) {
  return api.post<DatasetSchemaResponse>(
    `/datasets/${datasetId}/views/${viewId}/schema`,
    { schema },
  );
}

export function createDataset(params: CreateDatasetParams) {
  return api.post<Dataset>('/datasets', params);
}

export function updateDataset(id: string, params: UpdateDatasetParams) {
  return api.patch<Dataset>(`/datasets/${id}`, params);
}

export function deleteDataset(id: string) {
  return api.delete(`/datasets/${id}`);
}

export function getVersions(datasetId: string) {
  return api.get<DatasetVersion[]>(`/datasets/${datasetId}/versions`);
}

export function listDatasetTransactions(datasetId: string) {
  return api.get<DatasetTransaction[]>(`/datasets/${datasetId}/transactions`);
}

export function listBranches(datasetId: string) {
  return api.get<DatasetBranch[]>(`/datasets/${datasetId}/branches`);
}

export function createDatasetBranch(datasetId: string, params: CreateDatasetBranchParams) {
  return api.post<DatasetBranch>(`/datasets/${datasetId}/branches`, params);
}

export function checkoutDatasetBranch(datasetId: string, branchName: string) {
  return api.post<Dataset>(`/datasets/${datasetId}/branches/${encodeURIComponent(branchName)}/checkout`, {});
}

export function listDatasetViews(datasetId: string) {
  return api.get<DatasetView[]>(`/datasets/${datasetId}/views`);
}

export function getDatasetView(datasetId: string, viewId: string) {
  return api.get<DatasetView>(`/datasets/${datasetId}/views/${viewId}`);
}

export function createDatasetView(datasetId: string, params: CreateDatasetViewParams) {
  return api.post<DatasetView>(`/datasets/${datasetId}/views`, params);
}

export function refreshDatasetView(datasetId: string, viewId: string) {
  return api.post<DatasetView>(`/datasets/${datasetId}/views/${viewId}/refresh`, {});
}

export function previewDatasetView(datasetId: string, viewId: string, params?: { limit?: number; offset?: number }) {
  const query = new URLSearchParams();
  if (params?.limit) query.set('limit', String(params.limit));
  if (params?.offset) query.set('offset', String(params.offset));
  const qs = query.toString();
  return api.get<Record<string, unknown>>(`/datasets/${datasetId}/views/${viewId}/preview${qs ? `?${qs}` : ''}`);
}

export function listDatasetFilesystem(datasetId: string, params?: { path?: string }) {
  const query = new URLSearchParams();
  if (params?.path) query.set('path', params.path);
  const qs = query.toString();
  return api.get<DatasetFilesystemResponse>(`/datasets/${datasetId}/files${qs ? `?${qs}` : ''}`);
}

export function getDatasetQuality(datasetId: string) {
  return api.get<DatasetQualityResponse>(`/datasets/${datasetId}/quality`);
}

export function getDatasetLint(datasetId: string) {
  return api.get<DatasetLintResponse>(`/datasets/${datasetId}/lint`);
}

export function refreshDatasetQualityProfile(datasetId: string) {
  return api.post<DatasetQualityResponse>(`/datasets/${datasetId}/quality/profile`, {});
}

export function createDatasetQualityRule(datasetId: string, params: CreateDatasetQualityRuleParams) {
  return api.post<DatasetQualityResponse>(`/datasets/${datasetId}/quality/rules`, params);
}

export function updateDatasetQualityRule(datasetId: string, ruleId: string, params: UpdateDatasetQualityRuleParams) {
  return api.patch<DatasetQualityResponse>(`/datasets/${datasetId}/quality/rules/${ruleId}`, params);
}

export function deleteDatasetQualityRule(datasetId: string, ruleId: string) {
  return api.delete<DatasetQualityResponse>(`/datasets/${datasetId}/quality/rules/${ruleId}`);
}

// ─── P4 — Foundry "View retention policies for a dataset [Beta]" ──────────
// Mirrors `services/retention-policy-service/src/handlers/retention.rs`.

export interface RetentionSelector {
  dataset_rid?: string | null;
  project_id?: string | null;
  marking_id?: string | null;
  all_datasets?: boolean;
}

export interface RetentionCriteria {
  transaction_age_seconds?: number | null;
  transaction_state?: string | null;
  view_age_seconds?: number | null;
  last_accessed_seconds?: number | null;
}

export interface RetentionPolicy {
  id: string;
  name: string;
  scope: string;
  target_kind: 'dataset' | 'transaction' | string;
  retention_days: number;
  legal_hold: boolean;
  purge_mode: string;
  rules: string[];
  is_system: boolean;
  selector: RetentionSelector;
  criteria: RetentionCriteria;
  grace_period_minutes: number;
  last_applied_at?: string | null;
  next_run_at?: string | null;
  created_at: string;
  updated_at: string;
  active: boolean;
}

export interface ApplicablePoliciesResponse {
  dataset_rid: string;
  context: {
    project_id?: string | null;
    marking_id?: string | null;
    space_id?: string | null;
    org_id?: string | null;
  };
  inherited: {
    org: RetentionPolicy[];
    space: RetentionPolicy[];
    project: RetentionPolicy[];
  };
  explicit: RetentionPolicy[];
  effective: RetentionPolicy | null;
  conflicts: Array<{ winner_id: string; loser_id: string; reason: string }>;
}

export interface RetentionPreviewTransaction {
  id: string;
  tx_type: string;
  status: string;
  started_at: string;
  committed_at?: string | null;
  would_delete: boolean;
  policy_id?: string | null;
  policy_name?: string | null;
  reason?: string | null;
}

export interface RetentionPreviewFile {
  id: string;
  transaction_id: string;
  logical_path: string;
  physical_uri: string;
  size_bytes: number;
  policy_id: string;
  policy_name: string;
  reason: string;
}

export interface RetentionPreviewResponse {
  dataset_rid: string;
  as_of_days: number;
  as_of: string;
  effective_policy: RetentionPolicy | null;
  transactions: RetentionPreviewTransaction[];
  files: RetentionPreviewFile[];
  summary: {
    transactions_total: number;
    transactions_would_delete: number;
    files_total: number;
    bytes_total: number;
  };
  warnings: string[];
}

export interface ApplicablePoliciesParams {
  project_id?: string;
  marking_id?: string;
  space_id?: string;
  org_id?: string;
}

export function getApplicablePolicies(
  datasetRid: string,
  params: ApplicablePoliciesParams = {},
) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value) query.set(key, value);
  }
  const qs = query.toString();
  return api.get<ApplicablePoliciesResponse>(
    `/datasets/${datasetRid}/applicable-policies${qs ? `?${qs}` : ''}`,
  );
}

export function getRetentionPreview(
  datasetRid: string,
  asOfDays: number,
  params: ApplicablePoliciesParams = {},
) {
  const query = new URLSearchParams();
  query.set('as_of_days', String(asOfDays));
  for (const [key, value] of Object.entries(params)) {
    if (value) query.set(key, value);
  }
  return api.get<RetentionPreviewResponse>(
    `/datasets/${datasetRid}/retention-preview?${query.toString()}`,
  );
}

export interface CreateRetentionPolicyParams {
  name: string;
  target_kind: 'dataset' | 'transaction';
  retention_days: number;
  purge_mode: string;
  selector: RetentionSelector;
  criteria?: RetentionCriteria;
  grace_period_minutes?: number;
  legal_hold?: boolean;
  active?: boolean;
  scope?: string;
  rules?: string[];
  updated_by: string;
}

export function createRetentionPolicy(params: CreateRetentionPolicyParams) {
  // Routed via the gateway's `/api/v1/retention` prefix so it lands on
  // retention-policy-service (the bare `/policies` namespace is owned
  // by authorization-policy-service for RBAC).
  return api.post<RetentionPolicy>(`/retention/policies`, params);
}

export function updateRetentionPolicy(id: string, params: Partial<CreateRetentionPolicyParams>) {
  return api.put<RetentionPolicy>(`/retention/policies/${id}`, params);
}

export function deleteRetentionPolicy(id: string) {
  return api.delete(`/retention/policies/${id}`);
}

// ─── P6 — Dataset health (Foundry "Data Health" + "Health checks") ────────
// Mirrors `services/dataset-quality-service/src/handlers/health.rs`.

export interface DatasetHealthResponse {
  dataset_rid: string;
  dataset_id?: string | null;
  row_count: number;
  col_count: number;
  null_pct_by_column: Record<string, number>;
  freshness_seconds: number;
  last_commit_at?: string | null;
  txn_failure_rate_24h: number;
  last_build_status: 'success' | 'failed' | 'stale' | 'unknown';
  schema_drift_flag: boolean;
  extras: Record<string, unknown>;
  last_computed_at: string;
}

export function getDatasetHealth(datasetRid: string) {
  return api.get<DatasetHealthResponse>(`/datasets/${datasetRid}/health`);
}

// ─── P5 — Marketplace dataset product (publish + install) ─────────────────
// Mirrors `services/marketplace-service/src/handlers/dataset_product.rs`.

export interface DatasetProductManifest {
  entity: 'dataset';
  version: string;
  schema?: unknown;
  retention?: unknown[];
  branching_policy?: unknown;
  schedules?: string[];
  bootstrap: { mode: 'schema-only' | 'with-snapshot' };
}

export interface DatasetProduct {
  id: string;
  name: string;
  source_dataset_rid: string;
  entity_type: 'dataset';
  version: string;
  project_id?: string | null;
  published_by?: string | null;
  export_includes_data: boolean;
  include_schema: boolean;
  include_branches: boolean;
  include_retention: boolean;
  include_schedules: boolean;
  manifest: DatasetProductManifest;
  bootstrap_mode: 'schema-only' | 'with-snapshot';
  published_at: string;
  created_at: string;
}

export interface PublishDatasetProductRequest {
  name: string;
  version?: string;
  project_id?: string;
  export_includes_data?: boolean;
  include_schema?: boolean;
  include_branches?: boolean;
  include_retention?: boolean;
  include_schedules?: boolean;
  bootstrap_mode?: 'schema-only' | 'with-snapshot';
  schema?: unknown;
  retention?: unknown[];
  branching_policy?: unknown;
  schedules?: string[];
}

export function publishDatasetProduct(
  datasetRid: string,
  payload: PublishDatasetProductRequest,
) {
  return api.post<DatasetProduct>(
    `/marketplace/products/from-dataset/${datasetRid}`,
    payload,
  );
}

export interface DatasetProductInstall {
  id: string;
  product_id: string;
  target_project_id: string;
  target_dataset_rid: string;
  bootstrap_mode: string;
  status: string;
  details: unknown;
  installed_by?: string | null;
  created_at: string;
  completed_at?: string | null;
}

export function installDatasetProduct(
  productId: string,
  payload: {
    target_project_id: string;
    target_dataset_rid: string;
    bootstrap_mode?: 'schema-only' | 'with-snapshot';
  },
) {
  return api.post<DatasetProductInstall>(
    `/marketplace/products/${productId}/install`,
    payload,
  );
}

export async function uploadData(datasetId: string, file: File) {
  const formData = new FormData();
  formData.append('file', file);
  const headers: Record<string, string> = {};
  const authHeader = api.authorizationHeaders().Authorization;
  if (authHeader) {
    headers.Authorization = authHeader;
  }
  const response = await fetch(`/api/v1/datasets/${datasetId}/upload`, {
    method: 'POST',
    headers,
    body: formData,
  });
  if (!response.ok) throw new Error('Upload failed');
  return response.json();
}

// ────────────────────────────────────────────────────────────────────────────
// P3 — Foundry-style branch + JobSpec endpoints.
// Wired against the routes defined in `services/dataset-versioning-service/src/lib.rs`
// and `services/pipeline-authoring-service/src/main.rs`.
// ────────────────────────────────────────────────────────────────────────────

/// `POST /datasets/{rid}/branches` (P1 v2 shape).
export function createBranchV2(datasetRid: string, params: CreateBranchV2Params) {
  return api.post<DatasetBranch>(
    `/datasets/${encodeURIComponent(datasetRid)}/branches`,
    params,
  );
}

/// `DELETE /datasets/{rid}/branches/{branch}` (P1 reparent-preview).
export function deleteDatasetBranch(datasetRid: string, branchName: string) {
  return api.delete<DatasetBranchDeleteResponse>(
    `/datasets/${encodeURIComponent(datasetRid)}/branches/${encodeURIComponent(branchName)}`,
  );
}

/// `GET /datasets/{rid}/branches/{branch}/preview-delete`.
export function previewDeleteBranch(datasetRid: string, branchName: string) {
  return api.get<DatasetBranchPreviewDelete>(
    `/datasets/${encodeURIComponent(datasetRid)}/branches/${encodeURIComponent(branchName)}/preview-delete`,
  );
}

/// `POST /datasets/{rid}/branches/{branch}:reparent`.
export function reparentBranch(
  datasetRid: string,
  branchName: string,
  params: ReparentBranchParams,
) {
  return api.post<DatasetBranch>(
    `/datasets/${encodeURIComponent(datasetRid)}/branches/${encodeURIComponent(branchName)}:reparent`,
    params,
  );
}

/// `GET /datasets/{rid}/branches/{branch}/ancestry` — child→root walk.
export function listBranchAncestry(datasetRid: string, branchName: string) {
  return api.get<DatasetBranchAncestor[]>(
    `/datasets/${encodeURIComponent(datasetRid)}/branches/${encodeURIComponent(branchName)}/ancestry`,
  );
}

/// `POST /datasets/{rid}/branches/{branch}/transactions/{txn}:commit`.
export function commitTransaction(
  datasetRid: string,
  branchName: string,
  transactionId: string,
) {
  return api.post<DatasetTransaction>(
    `/datasets/${encodeURIComponent(datasetRid)}/branches/${encodeURIComponent(
      branchName,
    )}/transactions/${encodeURIComponent(transactionId)}:commit`,
    {},
  );
}

/// `POST /datasets/{rid}/branches/{branch}/transactions/{txn}:abort`.
export function abortTransaction(
  datasetRid: string,
  branchName: string,
  transactionId: string,
) {
  return api.post<DatasetTransaction>(
    `/datasets/${encodeURIComponent(datasetRid)}/branches/${encodeURIComponent(
      branchName,
    )}/transactions/${encodeURIComponent(transactionId)}:abort`,
    {},
  );
}

/// `GET /datasets/{rid}/job-specs?on_branch=<branch>` — queries the
/// `pipeline-authoring-service` index. The dataset-versioning surface
/// proxies this under the same `/datasets/...` namespace, so the UI
/// keeps using `api` (which is rooted at `/api/v1`).
export function listDatasetJobSpecs(
  datasetRid: string,
  params?: { on_branch?: string },
) {
  const query = new URLSearchParams();
  if (params?.on_branch) query.set('on_branch', params.on_branch);
  const qs = query.toString();
  return api.get<DatasetJobSpecRow[]>(
    `/datasets/${encodeURIComponent(datasetRid)}/job-specs${qs ? `?${qs}` : ''}`,
  );
}

/// Roll-up "is there a JobSpec on master?" used by the catalog card
/// + lineage colouring. Falls back to `false` when the listing is
/// empty or the call fails (we don't want a transient 404 to flip
/// every dataset to grey).
export async function loadJobSpecStatus(
  datasetRid: string,
): Promise<DatasetJobSpecStatus> {
  try {
    const rows = await listDatasetJobSpecs(datasetRid);
    const branches = Array.from(new Set(rows.map((r) => r.branch_name)));
    return {
      has_master_jobspec: branches.includes('master'),
      branches_with_jobspec: branches.sort(),
    };
  } catch {
    return { has_master_jobspec: false, branches_with_jobspec: [] };
  }
}

// ────────────────────────────────────────────────────────────────────────────
// P4 — branch retention + markings inheritance.
// ────────────────────────────────────────────────────────────────────────────

export interface BranchMarkingsView {
  effective: string[];
  explicit: string[];
  inherited_from_parent: string[];
}

export interface UpdateRetentionParams {
  policy: 'INHERITED' | 'FOREVER' | 'TTL_DAYS';
  ttl_days?: number | null;
}

export function updateBranchRetention(
  datasetRid: string,
  branchName: string,
  params: UpdateRetentionParams,
) {
  return api.patch<{ branch: string; policy: string; ttl_days: number | null }>(
    `/datasets/${encodeURIComponent(datasetRid)}/branches/${encodeURIComponent(branchName)}/retention`,
    params,
  );
}

export function restoreBranch(datasetRid: string, branchName: string) {
  return api.post<{ branch: string; restored_at: string; previously_archived_at: string }>(
    `/datasets/${encodeURIComponent(datasetRid)}/branches/${encodeURIComponent(branchName)}:restore`,
    {},
  );
}

export function getBranchMarkings(datasetRid: string, branchName: string) {
  return api.get<BranchMarkingsView>(
    `/datasets/${encodeURIComponent(datasetRid)}/branches/${encodeURIComponent(branchName)}/markings`,
  );
}

// ────────────────────────────────────────────────────────────────────────────
// P5 — Branch comparison + lifecycle timeline data.
// ────────────────────────────────────────────────────────────────────────────

export interface CompareTransactionSummary {
  transaction_rid: string;
  transaction_id: string;
  branch: string;
  tx_type: string;
  status: string;
  committed_at: string | null;
  files_changed: number;
}

export interface CompareConflictingFile {
  logical_path: string;
  a_transaction_rid: string;
  b_transaction_rid: string;
  content_hash_a: string | null;
  content_hash_b: string | null;
}

export interface BranchCompareResponse {
  base_branch: string;
  compare_branch: string;
  lca_branch_rid: string | null;
  a_only_transactions: CompareTransactionSummary[];
  b_only_transactions: CompareTransactionSummary[];
  conflicting_files: CompareConflictingFile[];
}

export function compareBranches(datasetRid: string, base: string, compare: string) {
  const query = new URLSearchParams({ base, compare });
  return api.get<BranchCompareResponse>(
    `/datasets/${encodeURIComponent(datasetRid)}/branches/compare?${query.toString()}`,
  );
}
