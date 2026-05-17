// SG.5: group-administration client. Mirrors the identity-federation
// admin surface — search/list, manage admins, nested edges, member
// expirations, and the inspect view.

import api from './client';

export type GroupKind = 'internal' | 'external' | 'rule_based';
export type GroupStatus = 'active' | 'archived';
export type GroupAdminScope = 'manage' | 'manage_members';

export interface AdminGroup {
  id: string;
  name: string;
  display_name: string;
  description: string | null;
  kind: GroupKind;
  realm: string;
  organization_id: string | null;
  attributes: Record<string, unknown>;
  rule_query?: Record<string, unknown> | null;
  status: GroupStatus;
  created_at: string;
  updated_at: string;
}

export interface GroupAdmin {
  group_id: string;
  user_id: string;
  scope: GroupAdminScope;
  granted_by?: string | null;
  created_at: string;
}

export interface GroupMember {
  group_id: string;
  user_id: string;
  added_at: string;
  added_by?: string | null;
  expires_at?: string | null;
}

export interface GroupBrief {
  id: string;
  name: string;
}

export interface GroupInspection {
  group: AdminGroup;
  direct_member_count: number;
  expiring_member_count: number;
  admins: GroupAdmin[];
  parents: GroupBrief[];
  children: GroupBrief[];
  project_access_hint: string;
}

export interface SearchGroupsFilter {
  q?: string;
  kind?: GroupKind;
  realm?: string;
  organization_id?: string;
  status?: GroupStatus;
  limit?: number;
  offset?: number;
}

export interface SearchGroupsResponse {
  items: AdminGroup[];
  total: number;
}

function toQuery(filter: SearchGroupsFilter): string {
  const params = new URLSearchParams();
  if (filter.q) params.set('q', filter.q);
  if (filter.kind) params.set('kind', filter.kind);
  if (filter.realm) params.set('realm', filter.realm);
  if (filter.organization_id) params.set('organization_id', filter.organization_id);
  if (filter.status) params.set('status', filter.status);
  if (filter.limit !== undefined) params.set('limit', String(filter.limit));
  if (filter.offset !== undefined) params.set('offset', String(filter.offset));
  const q = params.toString();
  return q ? `?${q}` : '';
}

export function searchGroups(filter: SearchGroupsFilter = {}): Promise<SearchGroupsResponse> {
  return api.get<SearchGroupsResponse>(`/groups/search${toQuery(filter)}`);
}

export function inspectGroup(groupId: string): Promise<GroupInspection> {
  return api.get<GroupInspection>(`/groups/${groupId}/inspect`);
}

export function listGroupMembers(groupId: string): Promise<GroupMember[]> {
  return api.get<GroupMember[]>(`/groups/${groupId}/members`);
}

export function addGroupMember(
  groupId: string,
  userId: string,
  expiresAt?: string,
): Promise<void> {
  return api.fetch<void>(`/groups/${groupId}/members/${userId}`, {
    method: 'PUT',
    body: expiresAt ? { expires_at: expiresAt } : {},
  });
}

export function removeGroupMember(groupId: string, userId: string): Promise<void> {
  return api.delete<void>(`/groups/${groupId}/members/${userId}`);
}

export function createGroup(body: {
  name: string;
  display_name?: string;
  description?: string;
  kind?: GroupKind;
  realm?: string;
  organization_id?: string;
  attributes?: Record<string, unknown>;
  rule_query?: Record<string, unknown>;
  status?: GroupStatus;
}): Promise<AdminGroup> {
  return api.post<AdminGroup>('/groups', body);
}

export function patchGroup(
  groupId: string,
  patch: Partial<{
    name: string;
    display_name: string;
    description: string | null;
    kind: GroupKind;
    realm: string;
    organization_id: string | null;
    attributes: Record<string, unknown>;
    rule_query: Record<string, unknown>;
    status: GroupStatus;
  }>,
): Promise<AdminGroup> {
  return api.fetch<AdminGroup>(`/groups/${groupId}`, { method: 'PATCH', body: patch });
}

export function deleteGroup(groupId: string): Promise<void> {
  return api.delete<void>(`/groups/${groupId}`);
}

export function listGroupAdmins(groupId: string): Promise<GroupAdmin[]> {
  return api.get<GroupAdmin[]>(`/groups/${groupId}/admins`);
}

export function addGroupAdmin(
  groupId: string,
  body: { user_id: string; scope?: GroupAdminScope; granted_by?: string },
): Promise<GroupAdmin> {
  return api.post<GroupAdmin>(`/groups/${groupId}/admins`, body);
}

export function removeGroupAdmin(
  groupId: string,
  userId: string,
  scope: GroupAdminScope = 'manage',
): Promise<void> {
  return api.delete<void>(
    `/groups/${groupId}/admins/${userId}?scope=${encodeURIComponent(scope)}`,
  );
}

export function addNestedGroup(parentId: string, memberId: string): Promise<void> {
  return api.fetch<void>(`/groups/${parentId}/nested/${memberId}`, { method: 'PUT' });
}

export function removeNestedGroup(parentId: string, memberId: string): Promise<void> {
  return api.delete<void>(`/groups/${parentId}/nested/${memberId}`);
}
