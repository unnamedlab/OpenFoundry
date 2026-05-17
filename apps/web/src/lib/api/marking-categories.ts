// SG.11: marking category administration. The gateway routes these
// /api/v1 paths to authorization-policy-service.

import api from './client';

export type MarkingCategoryVisibility = 'visible' | 'hidden';
export type MarkingCategoryPrincipalKind = 'user' | 'group';
export type MarkingCategoryPermissionName = 'administrator' | 'viewer';
export type MarkingPermissionName = 'administrator' | 'remover' | 'applier' | 'member';

export interface MarkingCategoryPrincipal {
  principal_kind: MarkingCategoryPrincipalKind;
  principal_id: string;
}

export interface MarkingCategoryPermission {
  category_id: string;
  principal_kind: MarkingCategoryPrincipalKind;
  principal_id: string;
  permission: MarkingCategoryPermissionName;
  granted_by: string;
  created_at: string;
}

export interface MarkingCategory {
  id: string;
  tenant_id?: string | null;
  slug: string;
  display_name: string;
  description: string;
  visibility: MarkingCategoryVisibility;
  organization_id?: string | null;
  metadata: Record<string, unknown>;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface MarkingCategoryResponse extends MarkingCategory {
  permissions: MarkingCategoryPermission[];
}

export interface MarkingCategoryAuditEvent {
  id: string;
  tenant_id?: string | null;
  category_id?: string | null;
  actor_id: string;
  action: string;
  principal_kind?: MarkingCategoryPrincipalKind | null;
  principal_id?: string | null;
  permission?: MarkingCategoryPermissionName | null;
  before_state: Record<string, unknown>;
  after_state: Record<string, unknown>;
  metadata: Record<string, unknown>;
  created_at: string;
}

export interface MarkingPrincipal {
  principal_kind: MarkingCategoryPrincipalKind;
  principal_id: string;
}

export interface MarkingPermission {
  marking_id: string;
  principal_kind: MarkingCategoryPrincipalKind;
  principal_id: string;
  permission: MarkingPermissionName;
  granted_by: string;
  created_at: string;
}

export interface Marking {
  id: string;
  tenant_id?: string | null;
  category_id: string;
  slug: string;
  display_name: string;
  description: string;
  metadata: Record<string, unknown>;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface MarkingResponse extends Marking {
  permissions: MarkingPermission[];
  metadata_redacted?: boolean;
}

export interface MarkingAuditEvent {
  id: string;
  tenant_id?: string | null;
  category_id?: string | null;
  marking_id?: string | null;
  actor_id: string;
  action: string;
  principal_kind?: MarkingCategoryPrincipalKind | null;
  principal_id?: string | null;
  permission?: MarkingPermissionName | null;
  before_state: Record<string, unknown>;
  after_state: Record<string, unknown>;
  metadata: Record<string, unknown>;
  created_at: string;
}

export interface MarkingPermissionCheckRequest {
  principal_id?: string;
  group_ids?: string[];
  resource_update_markings_allowed?: boolean;
  expand_access_allowed?: boolean;
}

export interface MarkingPermissionCheckResponse {
  marking_id: string;
  principal_id: string;
  can_manage: boolean;
  can_apply: boolean;
  can_remove: boolean;
  is_member: boolean;
  can_access_marked_data: boolean;
  resource_update_markings_allowed: boolean;
  expand_access_allowed: boolean;
  can_apply_to_resource: boolean;
  can_remove_from_resource: boolean;
  reasons: string[];
}

export interface ResourceMarking {
  id: string;
  tenant_id?: string | null;
  resource_kind: string;
  resource_id: string;
  marking_id: string;
  source_kind: 'direct';
  metadata: Record<string, unknown>;
  applied_by: string;
  applied_at: string;
}

export interface ResourceMarkingMutationResponse {
  allowed: boolean;
  resource_marking?: ResourceMarking;
  permission_check: MarkingPermissionCheckResponse;
}

export type ResourceMarkingRelationKind = 'hierarchy' | 'lineage';
export type EffectiveResourceMarkingSourceKind = 'direct' | 'hierarchy' | 'lineage' | 'mixed';
export type ResourceMarkingRequiredFor = 'resource_access' | 'data_access';

export interface ResourceMarkingEdge {
  id: string;
  tenant_id?: string | null;
  source_resource_kind: string;
  source_resource_id: string;
  target_resource_kind: string;
  target_resource_id: string;
  relation_kind: ResourceMarkingRelationKind;
  metadata: Record<string, unknown>;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface ResourceMarkingPathHop {
  resource_kind: string;
  resource_id: string;
  relation_kind?: ResourceMarkingRelationKind;
}

export interface EffectiveResourceMarkingSource {
  source_kind: EffectiveResourceMarkingSourceKind;
  required_for: ResourceMarkingRequiredFor;
  source_resource_kind: string;
  source_resource_id: string;
  direct_resource_marking_id: string;
  relation_kinds?: ResourceMarkingRelationKind[];
  path: ResourceMarkingPathHop[];
  metadata: Record<string, unknown>;
}

export interface EffectiveResourceMarking {
  marking_id: string;
  marking_name: string;
  required_for: ResourceMarkingRequiredFor[];
  sources: EffectiveResourceMarkingSource[];
}

export interface EffectiveResourceMarkingsResponse {
  resource_kind: string;
  resource_id: string;
  items: EffectiveResourceMarking[];
  checked_at: string;
}

export interface ResourceAccessRequirementResult {
  kind: string;
  label: string;
  status: 'passed' | 'failed' | 'not_applicable';
  satisfied: boolean;
  required?: string[];
  present?: string[];
  missing?: string[];
  detail: string;
  sources?: string[];
}

export interface ResourceAccessMarkingResult {
  marking_id: string;
  marking_name: string;
  required_for: ResourceMarkingRequiredFor[];
  satisfied: boolean;
  missing_for?: ResourceMarkingRequiredFor[];
  sources: EffectiveResourceMarkingSource[];
}

export interface ResourceAccessCheckResponse {
  principal_id: string;
  resource_kind: string;
  resource_id: string;
  resource_access_allowed: boolean;
  data_access_allowed: boolean;
  access_requirements: ResourceAccessRequirementResult[];
  additional_data_requirements: ResourceAccessRequirementResult[];
  effective_markings: EffectiveResourceMarking[];
  marking_results: ResourceAccessMarkingResult[];
  checked_at: string;
}

export interface MarkingBuildResourceRef {
  resource_kind: string;
  resource_id: string;
}

export interface MarkingDiffEntry {
  marking_id: string;
  marking_name: string;
  required_for: ResourceMarkingRequiredFor[];
}

export interface MarkingBuildBlockedRemoval {
  output_resource: MarkingBuildResourceRef;
  marking_id: string;
  marking_name: string;
  required_for: ResourceMarkingRequiredFor[];
  permission: MarkingPermissionCheckResponse;
}

export interface MarkingBuildOutputDiff {
  output_resource: MarkingBuildResourceRef;
  before: EffectiveResourceMarking[];
  after: EffectiveResourceMarking[];
  added: MarkingDiffEntry[];
  removed: MarkingDiffEntry[];
  unchanged: MarkingDiffEntry[];
}

export interface PublishMarkingBuildResponse {
  allowed: boolean;
  applied: boolean;
  dry_run: boolean;
  build_id?: string;
  transaction_id?: string;
  output_diffs: MarkingBuildOutputDiff[];
  blocked_removals?: MarkingBuildBlockedRemoval[];
  checked_at: string;
}

export interface MarkingBuildEvent {
  id: string;
  tenant_id?: string | null;
  build_id?: string;
  transaction_id?: string;
  output_resource_kind: string;
  output_resource_id: string;
  actor_id: string;
  status: 'applied' | 'blocked' | 'dry_run';
  reason?: string;
  input_resources: MarkingBuildResourceRef[];
  before_state: Record<string, unknown>;
  after_state: Record<string, unknown>;
  diff: Record<string, unknown>;
  metadata: Record<string, unknown>;
  created_at: string;
}

export interface CreateMarkingCategoryBody {
  slug: string;
  display_name: string;
  description?: string;
  visibility?: MarkingCategoryVisibility;
  organization_id?: string;
  metadata?: Record<string, unknown>;
  administrators?: MarkingCategoryPrincipal[];
  viewers?: MarkingCategoryPrincipal[];
}

export interface CreateMarkingBody {
  id?: string;
  slug: string;
  display_name: string;
  description?: string;
  metadata?: Record<string, unknown>;
  administrators?: MarkingPrincipal[];
  removers?: MarkingPrincipal[];
  appliers?: MarkingPrincipal[];
  members?: MarkingPrincipal[];
}

export interface UpdateMarkingBody {
  display_name?: string;
  description?: string;
  metadata?: Record<string, unknown>;
}

export interface UpdateMarkingCategoryBody {
  display_name?: string;
  description?: string;
  visibility?: MarkingCategoryVisibility;
  organization_id?: string;
  metadata?: Record<string, unknown>;
}

export function listMarkingCategories(includeHidden = true): Promise<{ items: MarkingCategoryResponse[] }> {
  const qs = includeHidden ? '?include_hidden=true' : '';
  return api.get<{ items: MarkingCategoryResponse[] }>(`/marking-categories${qs}`);
}

export function createMarkingCategory(body: CreateMarkingCategoryBody): Promise<MarkingCategoryResponse> {
  return api.post<MarkingCategoryResponse>('/marking-categories', body);
}

export function updateMarkingCategory(id: string, body: UpdateMarkingCategoryBody): Promise<MarkingCategoryResponse> {
  return api.patch<MarkingCategoryResponse>(`/marking-categories/${id}`, body);
}

export function blockDeleteMarkingCategory(id: string): Promise<void> {
  return api.delete<void>(`/marking-categories/${id}`);
}

export function upsertMarkingCategoryPermission(
  categoryID: string,
  body: {
    principal_kind: MarkingCategoryPrincipalKind;
    principal_id: string;
    permission: MarkingCategoryPermissionName;
  },
): Promise<MarkingCategoryPermission> {
  return api.put<MarkingCategoryPermission>(`/marking-categories/${categoryID}/permissions`, body);
}

export function deleteMarkingCategoryPermission(
  categoryID: string,
  principalKind: MarkingCategoryPrincipalKind,
  principalID: string,
  permission: MarkingCategoryPermissionName,
): Promise<void> {
  return api.delete<void>(
    `/marking-categories/${categoryID}/permissions/${principalKind}/${principalID}/${permission}`,
  );
}

export function listMarkingCategoryAuditEvents(
  categoryID: string,
): Promise<{ items: MarkingCategoryAuditEvent[] }> {
  return api.get<{ items: MarkingCategoryAuditEvent[] }>(
    `/marking-categories/${categoryID}/audit-events`,
  );
}

export function listMarkingsForCategory(
  categoryID: string,
  includeHidden = true,
): Promise<{ items: MarkingResponse[] }> {
  const qs = includeHidden ? '?include_hidden=true' : '';
  return api.get<{ items: MarkingResponse[] }>(
    `/marking-categories/${categoryID}/markings${qs}`,
  );
}

export function createMarking(categoryID: string, body: CreateMarkingBody): Promise<MarkingResponse> {
  return api.post<MarkingResponse>(`/marking-categories/${categoryID}/markings`, body);
}

export function updateMarking(id: string, body: UpdateMarkingBody): Promise<MarkingResponse> {
  return api.patch<MarkingResponse>(`/markings/${id}`, body);
}

export function blockDeleteMarking(id: string): Promise<void> {
  return api.delete<void>(`/markings/${id}`);
}

export function blockMoveMarkingCategory(id: string, targetCategoryID: string): Promise<void> {
  return api.put<void>(`/markings/${id}/category`, { target_category_id: targetCategoryID });
}

export function upsertMarkingPermission(
  markingID: string,
  body: {
    principal_kind: MarkingCategoryPrincipalKind;
    principal_id: string;
    permission: MarkingPermissionName;
  },
): Promise<MarkingPermission> {
  return api.put<MarkingPermission>(`/markings/${markingID}/permissions`, body);
}

export function deleteMarkingPermission(
  markingID: string,
  principalKind: MarkingCategoryPrincipalKind,
  principalID: string,
  permission: MarkingPermissionName,
): Promise<void> {
  return api.delete<void>(
    `/markings/${markingID}/permissions/${principalKind}/${principalID}/${permission}`,
  );
}

export function listMarkingAuditEvents(
  markingID: string,
): Promise<{ items: MarkingAuditEvent[] }> {
  return api.get<{ items: MarkingAuditEvent[] }>(
    `/markings/${markingID}/audit-events`,
  );
}

export function checkMarkingPermission(
  markingID: string,
  body: MarkingPermissionCheckRequest,
): Promise<MarkingPermissionCheckResponse> {
  return api.post<MarkingPermissionCheckResponse>(
    `/markings/${markingID}/permission-check`,
    body,
  );
}

export function listResourceMarkings(
  resourceKind: string,
  resourceID: string,
): Promise<{ items: ResourceMarking[] }> {
  const qs = new URLSearchParams({ resource_kind: resourceKind, resource_id: resourceID });
  return api.get<{ items: ResourceMarking[] }>(`/resource-markings?${qs.toString()}`);
}

export function applyResourceMarking(body: {
  resource_kind: string;
  resource_id: string;
  marking_id: string;
  resource_update_markings_allowed: boolean;
  metadata?: Record<string, unknown>;
}): Promise<ResourceMarkingMutationResponse> {
  return api.post<ResourceMarkingMutationResponse>('/resource-markings', body);
}

export function removeResourceMarking(body: {
  resource_kind: string;
  resource_id: string;
  marking_id: string;
  resource_update_markings_allowed: boolean;
  expand_access_allowed?: boolean;
  reason?: string;
}): Promise<ResourceMarkingMutationResponse> {
  return api.post<ResourceMarkingMutationResponse>('/resource-markings/remove', body);
}

export function listEffectiveResourceMarkings(
  resourceKind: string,
  resourceID: string,
): Promise<EffectiveResourceMarkingsResponse> {
  const qs = new URLSearchParams({ resource_kind: resourceKind, resource_id: resourceID });
  return api.get<EffectiveResourceMarkingsResponse>(`/resource-markings/effective?${qs.toString()}`);
}

export function listResourceMarkingEdges(
  resourceKind: string,
  resourceID: string,
  direction: 'all' | 'upstream' | 'downstream' = 'all',
): Promise<{ items: ResourceMarkingEdge[] }> {
  const qs = new URLSearchParams({ resource_kind: resourceKind, resource_id: resourceID, direction });
  return api.get<{ items: ResourceMarkingEdge[] }>(`/resource-marking-edges?${qs.toString()}`);
}

export function upsertResourceMarkingEdge(body: {
  source_resource_kind: string;
  source_resource_id: string;
  target_resource_kind: string;
  target_resource_id: string;
  relation_kind: ResourceMarkingRelationKind;
  metadata?: Record<string, unknown>;
}): Promise<ResourceMarkingEdge> {
  return api.put<ResourceMarkingEdge>('/resource-marking-edges', body);
}

export function deleteResourceMarkingEdge(body: {
  source_resource_kind: string;
  source_resource_id: string;
  target_resource_kind: string;
  target_resource_id: string;
  relation_kind: ResourceMarkingRelationKind;
}): Promise<void> {
  return api.fetch<void>('/resource-marking-edges', { method: 'DELETE', body });
}

export function checkResourceAccess(body: {
  principal_id?: string;
  group_ids?: string[];
  resource_kind: string;
  resource_id: string;
  required_organization_id?: string;
  user_organization_ids?: string[];
  role_satisfied?: boolean;
  role_label?: string;
  role_detail?: string;
}): Promise<ResourceAccessCheckResponse> {
  return api.post<ResourceAccessCheckResponse>('/resource-access:check', body);
}

export function publishMarkingBuild(body: {
  build_id?: string;
  transaction_id?: string;
  input_resources: MarkingBuildResourceRef[];
  output_resources: MarkingBuildResourceRef[];
  replace_existing_lineage_to_output: boolean;
  dry_run?: boolean;
  group_ids?: string[];
  resource_update_markings_allowed: boolean;
  expand_access_allowed?: boolean;
  reason?: string;
  metadata?: Record<string, unknown>;
}): Promise<PublishMarkingBuildResponse> {
  return api.post<PublishMarkingBuildResponse>('/resource-marking-builds:publish', body);
}

export function listMarkingBuildEvents(params: {
  build_id?: string;
  transaction_id?: string;
  resource_kind?: string;
  resource_id?: string;
}): Promise<{ items: MarkingBuildEvent[] }> {
  const qs = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value) qs.set(key, value);
  });
  const suffix = qs.toString() ? `?${qs.toString()}` : '';
  return api.get<{ items: MarkingBuildEvent[] }>(`/resource-marking-build-events${suffix}`);
}
