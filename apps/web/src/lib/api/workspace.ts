// Workspace API client: thin wrapper over the Phase 1 backend surface
// exposed by `tenancy-organizations-service` under `/api/v1/workspace/*`.
// Used by the new project navigation/detail pages and components.

import api from './client';

export type ResourceKind =
  | 'ontology_project'
  | 'ontology_folder'
  | 'ontology_resource_binding'
  | 'dataset'
  | 'pipeline'
  | 'query'
  | 'notebook'
  | 'app'
  | 'dashboard'
  | 'report'
  | 'model'
  | 'workflow'
  | 'other';

export type AccessLevel = 'viewer' | 'editor' | 'owner';

// ---------------------------------------------------------------------------
// Favorites
// ---------------------------------------------------------------------------

export interface UserFavorite {
  user_id: string;
  resource_kind: ResourceKind;
  resource_id: string;
  group_id: string | null;
  display_order: number;
  created_at: string;
  updated_at: string;
}

export interface FavoriteGroup {
  id: string;
  user_id: string;
  name: string;
  display_order: number;
  created_at: string;
  updated_at: string;
}

export interface ListFavoritesEnvelope {
  data: UserFavorite[];
  groups: FavoriteGroup[];
}

export function listFavorites(params?: { kind?: ResourceKind; limit?: number }) {
  return listFavoritesWithGroups(params).then((response) => response.data);
}

export function listFavoritesWithGroups(params?: { kind?: ResourceKind; limit?: number }) {
  const qs = new URLSearchParams();
  if (params?.kind) qs.set('kind', params.kind);
  if (params?.limit) qs.set('limit', String(params.limit));
  const query = qs.toString();
  return api
    .get<ListFavoritesEnvelope>(
      `/workspace/favorites${query ? `?${query}` : ''}`,
    );
}

export function createFavorite(body: {
  resource_kind: ResourceKind;
  resource_id: string;
  group_id?: string | null;
  display_order?: number;
}) {
  return api.post<UserFavorite>('/workspace/favorites', body);
}

export function deleteFavorite(kind: ResourceKind, id: string) {
  return api.delete(`/workspace/favorites/${kind}/${id}`);
}

export function listFavoriteGroups() {
  return api
    .get<{ data: FavoriteGroup[] }>('/workspace/favorites/groups')
    .then((response) => response.data);
}

export function createFavoriteGroup(body: { name: string; display_order?: number }) {
  return api.post<FavoriteGroup>('/workspace/favorites/groups', body);
}

export function updateFavoriteOrder(items: Array<{
  resource_kind: ResourceKind;
  resource_id: string;
  group_id?: string | null;
  display_order: number;
}>) {
  return api.put<void>('/workspace/favorites/order', { items });
}

export function updateFavoriteGroupsOrder(groups: Array<{ id: string; display_order: number }>) {
  return api.put<void>('/workspace/favorites/groups/order', { groups });
}

// ---------------------------------------------------------------------------
// Recents
// ---------------------------------------------------------------------------

export interface RecentEntry {
  resource_kind: ResourceKind;
  resource_id: string;
  last_accessed_at: string;
}

export function recordAccess(body: { resource_kind: ResourceKind; resource_id: string }) {
  return api.post<{ ok: true }>('/workspace/recents', body);
}

export function listRecents(params?: { kind?: ResourceKind; limit?: number }) {
  const qs = new URLSearchParams();
  if (params?.kind) qs.set('kind', params.kind);
  if (params?.limit) qs.set('limit', String(params.limit));
  const query = qs.toString();
  return api
    .get<{ data: RecentEntry[] }>(
      `/workspace/recents${query ? `?${query}` : ''}`,
    )
    .then((response) => response.data);
}

// ---------------------------------------------------------------------------
// Resource references
// ---------------------------------------------------------------------------

export interface ResourceReferenceNode {
  resource_kind: ResourceKind | string;
  resource_id: string;
  resource_rid: string;
  display_name: string;
  description?: string | null;
  project_id?: string | null;
  project_rid?: string | null;
}

export interface ResourceReferenceEdge {
  source: ResourceReferenceNode;
  target: ResourceReferenceNode;
  relationship: string;
  created_at: string;
  updated_at: string;
  derived: boolean;
}

export interface ResourceReferenceGraph {
  resource_kind: ResourceKind | string;
  resource_id: string;
  resource_rid: string;
  depends_on: ResourceReferenceEdge[];
  used_by: ResourceReferenceEdge[];
}

export interface ReplaceResourceReferencesBody {
  depends_on: Array<{
    resource_kind: ResourceKind | string;
    resource_id: string;
    relationship?: string;
  }>;
}

export function listResourceReferences(kind: ResourceKind | string, id: string) {
  return api.get<ResourceReferenceGraph>(`/workspace/resources/${kind}/${id}/references`);
}

export function replaceResourceReferences(
  kind: ResourceKind | string,
  id: string,
  body: ReplaceResourceReferencesBody,
) {
  return api.put<ResourceReferenceGraph>(`/workspace/resources/${kind}/${id}/references`, body);
}

// ---------------------------------------------------------------------------
// Compass search
// ---------------------------------------------------------------------------

export interface CompassSearchResult {
  rid: string;
  type: string;
  display_name: string;
  owning_project_id?: string | null;
  owning_project_rid?: string | null;
  organization_rids: string[];
  marking_rids: string[];
  last_modified_at: string;
  owner_id?: string | null;
  tags: string[];
  summary: string;
  open_url: string;
  is_deleted: boolean;
  score?: number;
}

export interface CompassSearchResponse {
  data: CompassSearchResult[];
  next_cursor?: string | null;
  limit: number;
}

export interface CompassSearchParams {
  q?: string;
  type?: string;
  project?: string;
  owner?: string;
  marking?: string[];
  limit?: number;
  cursor?: string;
}

export function searchCompass(params: CompassSearchParams) {
  const qs = new URLSearchParams();
  if (params.q) qs.set('q', params.q);
  if (params.type) qs.set('type', params.type);
  if (params.project) qs.set('project', params.project);
  if (params.owner) qs.set('owner', params.owner);
  for (const marking of params.marking ?? []) {
    if (marking.trim()) qs.append('marking', marking.trim());
  }
  if (params.limit) qs.set('limit', String(params.limit));
  if (params.cursor) qs.set('cursor', params.cursor);
  const query = qs.toString();
  return api.get<CompassSearchResponse>(`/compass/search${query ? `?${query}` : ''}`);
}

// ---------------------------------------------------------------------------
// Trash
// ---------------------------------------------------------------------------

export interface TrashEntry {
  resource_kind: ResourceKind;
  resource_id: string;
  project_id: string | null;
  display_name: string;
  deleted_at: string;
  deleted_by: string | null;
  retention_days: number;
  purge_after: string | null;
  original_project_id: string | null;
  original_parent_folder_id: string | null;
  restore_target_status: 'original_path' | 'project_root' | string;
}

export interface RestoreResourceResponse {
  restored: boolean;
  restored_to_original_path: boolean;
  restored_to_project_id?: string | null;
  restored_to_folder_id?: string | null;
  banner?: string | null;
}

export function listTrash(params?: { kind?: ResourceKind; limit?: number }) {
  const qs = new URLSearchParams();
  if (params?.kind) qs.set('kind', params.kind);
  if (params?.limit) qs.set('limit', String(params.limit));
  const query = qs.toString();
  return api
    .get<{ data: TrashEntry[] }>(
      `/workspace/trash${query ? `?${query}` : ''}`,
    )
    .then((response) => response.data);
}

export function restoreResource(kind: ResourceKind, id: string) {
  return api.post<RestoreResourceResponse>(`/workspace/resources/${kind}/${id}/restore`, {});
}

export function purgeResource(kind: ResourceKind, id: string) {
  return api.delete(`/workspace/resources/${kind}/${id}/purge`);
}

// ---------------------------------------------------------------------------
// Sharing
// ---------------------------------------------------------------------------

export interface ResourceShare {
  id: string;
  resource_kind: ResourceKind;
  resource_id: string;
  shared_with_user_id: string | null;
  shared_with_group_id: string | null;
  sharer_id: string;
  access_level: AccessLevel;
  note: string;
  expires_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface CreateShareBody {
  shared_with_user_id?: string;
  shared_with_group_id?: string;
  access_level: AccessLevel;
  note?: string;
  expires_at?: string | null;
}

export function createShare(kind: ResourceKind, id: string, body: CreateShareBody) {
  return api.post<ResourceShare>(`/workspace/resources/${kind}/${id}/share`, body);
}

export function listResourceShares(kind: ResourceKind, id: string) {
  return api
    .get<{ data: ResourceShare[] }>(`/workspace/resources/${kind}/${id}/shares`)
    .then((response) => response.data);
}

export function revokeShare(shareId: string) {
  return api.delete(`/workspace/shares/${shareId}`);
}

export function listSharedWithMe(params?: { kind?: ResourceKind; limit?: number }) {
  const qs = new URLSearchParams();
  if (params?.kind) qs.set('kind', params.kind);
  if (params?.limit) qs.set('limit', String(params.limit));
  const query = qs.toString();
  return api
    .get<{ data: ResourceShare[] }>(
      `/workspace/shared-with-me${query ? `?${query}` : ''}`,
    )
    .then((response) => response.data);
}

export function listSharedByMe(params?: { kind?: ResourceKind; limit?: number }) {
  const qs = new URLSearchParams();
  if (params?.kind) qs.set('kind', params.kind);
  if (params?.limit) qs.set('limit', String(params.limit));
  const query = qs.toString();
  return api
    .get<{ data: ResourceShare[] }>(
      `/workspace/shared-by-me${query ? `?${query}` : ''}`,
    )
    .then((response) => response.data);
}

// ---------------------------------------------------------------------------
// Resource operations (move / rename / duplicate / soft-delete / batch)
// ---------------------------------------------------------------------------

export interface MoveBody {
  target_folder_id?: string | null;
  target_folder_rid?: string | null;
  target_project_id?: string | null;
  target_project_rid?: string | null;
  confirm_access_policy_change?: boolean;
  confirm_marking_change?: boolean;
}

export function moveResource(kind: ResourceKind, id: string, body: MoveBody) {
  return api.post<{ ok: true }>(`/workspace/resources/${kind}/${id}/move`, body);
}

export function renameResource(kind: ResourceKind, id: string, body: { name: string }) {
  return api.post<{ ok: true }>(`/workspace/resources/${kind}/${id}/rename`, body);
}

export function duplicateResource(
  kind: ResourceKind,
  id: string,
  body?: { target_folder_id?: string | null; suffix?: string },
) {
  return api.post<{ id: string }>(
    `/workspace/resources/${kind}/${id}/duplicate`,
    body ?? {},
  );
}

export function softDeleteResource(kind: ResourceKind, id: string, params?: { retention_days?: number }) {
  const qs = new URLSearchParams();
  if (params?.retention_days) qs.set('retention_days', String(params.retention_days));
  const query = qs.toString();
  return api.delete(`/workspace/resources/${kind}/${id}${query ? `?${query}` : ''}`);
}

export interface BatchAction {
  op: 'move' | 'delete' | 'restore' | 'purge' | 'rename' | 'duplicate' | 'soft_delete';
  resource_kind: ResourceKind;
  resource_id: string;
  target_folder_id?: string | null;
  target_project_id?: string | null;
  target_folder_rid?: string | null;
  target_project_rid?: string | null;
  confirm_access_policy_change?: boolean;
  confirm_marking_change?: boolean;
  retention_days?: number;
  name?: string;
}

export interface BatchResultEntry {
  op?: string;
  resource_kind: ResourceKind;
  resource_id: string;
  ok: boolean;
  error: string | null;
}

export function batchApply(actions: BatchAction[]) {
  return api.post<{ results: BatchResultEntry[] }>('/workspace/resources/batch', { actions });
}

// ---------------------------------------------------------------------------
// Cross-resource label resolver (POST /workspace/resources/resolve).
// Returns canonical labels for ontology projects/folders today; other
// kinds fall back to `resolved: false` and the caller keeps its
// placeholder. The frontend `resource-labels.ts` cache batches calls.
// ---------------------------------------------------------------------------

export interface ResolvedLabel {
  resource_kind: ResourceKind;
  resource_id: string;
  resolved: boolean;
  label: string | null;
  description: string | null;
}

export function resolveResourceLabels(
  items: Array<{ resource_kind: ResourceKind; resource_id: string }>,
) {
  return api.post<{ data: ResolvedLabel[] }>('/workspace/resources/resolve', { items });
}
