// SG.7: role-sets, operations catalog, and the delegation-rank
// checker. The client targets the authorization-policy-service
// surface; the gateway routes /api/v1/role-sets and /api/v1/operations
// to APS.

import api from './client';

export type RoleSetContext =
  | 'project'
  | 'ontology'
  | 'restricted_view'
  | 'platform_admin';

export interface RoleSet {
  id: string;
  tenant_id?: string | null;
  slug: string;
  name: string;
  context: RoleSetContext;
  description?: string | null;
  created_at: string;
  updated_at: string;
}

export interface RoleSetRole {
  role_set_id: string;
  role_id: string;
  role_name: string;
  rank: number;
  created_at: string;
}

export interface RoleSetResponse extends RoleSet {
  roles: RoleSetRole[];
}

export interface OperationCatalogEntry {
  id: string;
  resource: string;
  action: string;
  description?: string | null;
}

export interface CheckDelegationResponse {
  allowed: boolean;
  grantor_role_id?: string | null;
  grantor_rank?: number | null;
  target_role_id: string;
  target_rank: number;
  reason?: string;
}

export function listRoleSets(context?: RoleSetContext): Promise<RoleSetResponse[]> {
  const qs = context ? `?context=${encodeURIComponent(context)}` : '';
  return api.get<RoleSetResponse[]>(`/role-sets${qs}`);
}

export function getRoleSet(id: string): Promise<RoleSetResponse> {
  return api.get<RoleSetResponse>(`/role-sets/${id}`);
}

export function createRoleSet(body: {
  slug: string;
  name: string;
  context: RoleSetContext;
  description?: string;
}): Promise<RoleSetResponse> {
  return api.post<RoleSetResponse>('/role-sets', body);
}

export function updateRoleSet(
  id: string,
  patch: { name?: string; description?: string },
): Promise<RoleSetResponse> {
  return api.fetch<RoleSetResponse>(`/role-sets/${id}`, {
    method: 'PATCH',
    body: patch,
  });
}

export function deleteRoleSet(id: string): Promise<void> {
  return api.delete<void>(`/role-sets/${id}`);
}

export function addRoleToRoleSet(
  roleSetID: string,
  body: { role_id: string; rank: number },
): Promise<RoleSetRole> {
  return api.post<RoleSetRole>(`/role-sets/${roleSetID}/roles`, body);
}

export function removeRoleFromRoleSet(
  roleSetID: string,
  roleID: string,
): Promise<void> {
  return api.delete<void>(`/role-sets/${roleSetID}/roles/${roleID}`);
}

export function checkRoleSetDelegation(
  roleSetID: string,
  body: { target_role_id: string; grantor_id?: string },
): Promise<CheckDelegationResponse> {
  return api.post<CheckDelegationResponse>(
    `/role-sets/${roleSetID}/delegation:check`,
    body,
  );
}

export function listOperations(): Promise<{ items: OperationCatalogEntry[] }> {
  return api.get<{ items: OperationCatalogEntry[] }>('/operations');
}
