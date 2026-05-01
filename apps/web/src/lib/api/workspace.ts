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
  created_at: string;
}

export function listFavorites(params?: { kind?: ResourceKind; limit?: number }) {
  const qs = new URLSearchParams();
  if (params?.kind) qs.set('kind', params.kind);
  if (params?.limit) qs.set('limit', String(params.limit));
  const query = qs.toString();
  return api
    .get<{ data: UserFavorite[] }>(
      `/workspace/favorites${query ? `?${query}` : ''}`,
    )
    .then((response) => response.data);
}

export function createFavorite(body: { resource_kind: ResourceKind; resource_id: string }) {
  return api.post<UserFavorite>('/workspace/favorites', body);
}

export function deleteFavorite(kind: ResourceKind, id: string) {
  return api.delete(`/workspace/favorites/${kind}/${id}`);
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
// Trash
// ---------------------------------------------------------------------------

export interface TrashEntry {
  resource_kind: ResourceKind;
  resource_id: string;
  project_id: string | null;
  display_name: string;
  deleted_at: string;
  deleted_by: string;
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
  return api.post<{ ok: true }>(`/workspace/resources/${kind}/${id}/restore`, {});
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
  target_project_id?: string | null;
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

export function softDeleteResource(kind: ResourceKind, id: string) {
  return api.delete(`/workspace/resources/${kind}/${id}`);
}

export interface BatchAction {
  op: 'move' | 'rename' | 'duplicate' | 'soft_delete';
  resource_kind: ResourceKind;
  resource_id: string;
  target_folder_id?: string | null;
  name?: string;
}

export interface BatchResultEntry {
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
