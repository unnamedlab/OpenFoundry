// Client for tenancy-organizations-service (foundation slice + SG.2
// administrators / guests / spaces / membership).
//
// All endpoints live under /api/v1 on the tenancy service. The web app
// proxies through the edge gateway so the relative API_BASE in
// ./client is sufficient.

import api from './client';

export interface Organization {
  id: string;
  slug: string;
  display_name: string;
  description: string;
  contact_email: string | null;
  organization_type: string;
  default_workspace: string | null;
  tenant_tier: string | null;
  status: string;
  metadata: Record<string, unknown>;
  settings: Record<string, unknown>;
  quotas: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface OrganizationAdmin {
  organization_id: string;
  user_id: string;
  scope: string;
  granted_by: string | null;
  created_at: string;
}

export interface OrganizationGuest {
  organization_id: string;
  user_id: string;
  primary_organization_id: string;
  status: string;
  invited_by: string | null;
  expires_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface TenancySpace {
  id: string;
  organization_id: string;
  slug: string;
  display_name: string;
  description: string;
  settings: Record<string, unknown>;
  quotas: Record<string, unknown>;
  status: string;
  created_at: string;
  updated_at: string;
}

export interface MembershipCheck {
  organization_id: string;
  user_id: string;
  is_member: boolean;
  is_admin: boolean;
}

interface ListEnvelope<T> {
  items: T[];
}

export async function listOrganizations(): Promise<Organization[]> {
  const res = await api.fetch<ListEnvelope<Organization>>('/organizations');
  return res.items;
}

export async function getOrganization(id: string): Promise<Organization> {
  return api.fetch<Organization>(`/organizations/${id}`);
}

export async function updateOrganization(
  id: string,
  patch: Partial<{
    display_name: string;
    description: string;
    contact_email: string | null;
    organization_type: string;
    default_workspace: string | null;
    tenant_tier: string | null;
    status: string;
    metadata: Record<string, unknown>;
    settings: Record<string, unknown>;
    quotas: Record<string, unknown>;
  }>,
): Promise<Organization> {
  return api.fetch<Organization>(`/organizations/${id}`, {
    method: 'PATCH',
    body: patch,
  });
}

export async function listOrganizationAdmins(orgId: string): Promise<OrganizationAdmin[]> {
  const res = await api.fetch<ListEnvelope<OrganizationAdmin>>(
    `/organizations/${orgId}/admins`,
  );
  return res.items;
}

export async function createOrganizationAdmin(
  orgId: string,
  body: { user_id: string; scope?: string; granted_by?: string },
): Promise<OrganizationAdmin> {
  return api.fetch<OrganizationAdmin>(`/organizations/${orgId}/admins`, {
    method: 'POST',
    body,
  });
}

export async function deleteOrganizationAdmin(
  orgId: string,
  userId: string,
  scope = 'enrollment_admin',
): Promise<void> {
  await api.fetch<void>(
    `/organizations/${orgId}/admins/${userId}?scope=${encodeURIComponent(scope)}`,
    { method: 'DELETE' },
  );
}

export async function listOrganizationGuests(orgId: string): Promise<OrganizationGuest[]> {
  const res = await api.fetch<ListEnvelope<OrganizationGuest>>(
    `/organizations/${orgId}/guests`,
  );
  return res.items;
}

export async function createOrganizationGuest(
  orgId: string,
  body: {
    user_id: string;
    primary_organization_id: string;
    status?: string;
    invited_by?: string;
    expires_at?: string;
  },
): Promise<OrganizationGuest> {
  return api.fetch<OrganizationGuest>(`/organizations/${orgId}/guests`, {
    method: 'POST',
    body,
  });
}

export async function deleteOrganizationGuest(
  orgId: string,
  userId: string,
): Promise<void> {
  await api.fetch<void>(`/organizations/${orgId}/guests/${userId}`, { method: 'DELETE' });
}

export async function listTenancySpaces(orgId: string): Promise<TenancySpace[]> {
  const res = await api.fetch<ListEnvelope<TenancySpace>>(
    `/organizations/${orgId}/spaces`,
  );
  return res.items;
}

export async function createTenancySpace(
  orgId: string,
  body: {
    slug: string;
    display_name: string;
    description?: string;
    settings?: Record<string, unknown>;
    quotas?: Record<string, unknown>;
    status?: string;
  },
): Promise<TenancySpace> {
  return api.fetch<TenancySpace>(`/organizations/${orgId}/spaces`, {
    method: 'POST',
    body,
  });
}

export async function updateTenancySpace(
  spaceId: string,
  patch: Partial<{
    display_name: string;
    description: string;
    settings: Record<string, unknown>;
    quotas: Record<string, unknown>;
    status: string;
  }>,
): Promise<TenancySpace> {
  return api.fetch<TenancySpace>(`/tenancy-spaces/${spaceId}`, {
    method: 'PATCH',
    body: patch,
  });
}

export async function deleteTenancySpace(spaceId: string): Promise<void> {
  await api.fetch<void>(`/tenancy-spaces/${spaceId}`, { method: 'DELETE' });
}

export async function checkOrganizationMembership(
  orgId: string,
  userId?: string,
): Promise<MembershipCheck> {
  const qs = userId ? `?user_id=${encodeURIComponent(userId)}` : '';
  return api.fetch<MembershipCheck>(`/organizations/${orgId}/membership${qs}`);
}

// ─── SG.6: project security boundary ──────────────────────────────────

export type ProjectRole = 'discoverer' | 'viewer' | 'editor' | 'owner';
export type ProjectAccessRequestStatus =
  | 'pending'
  | 'approved'
  | 'denied'
  | 'cancelled'
  | 'changes_requested'
  | 'action_required'
  | 'completed';
export type ProjectAccessRequestType = 'project_access' | 'additional_project_access';
export type ProjectAccessRequestTaskType =
  | 'group_membership'
  | 'project_role'
  | 'marking_access'
  | 'external_group_handoff';
export type ProjectAccessRequestTaskStatus =
  | 'review'
  | 'approved'
  | 'rejected'
  | 'action_required'
  | 'completed';
export type ProjectAccessGroupKind = 'internal' | 'external' | 'rule_based';

export interface ProjectReference {
  kind: string;
  id: string;
  label?: string;
}

export interface OntologyProject {
  id: string;
  slug: string;
  display_name: string;
  description: string;
  workspace_slug: string | null;
  owner_id: string;
  default_role: ProjectRole;
  point_of_contact_user_id?: string | null;
  point_of_contact_email?: string | null;
  references: ProjectReference[];
  created_at: string;
  updated_at: string;
}

export interface ProjectGroupMembership {
  project_id: string;
  group_id: string;
  role: ProjectRole;
  granted_by?: string | null;
  created_at: string;
  updated_at: string;
}

export interface ProjectAccessRequest {
  id: string;
  project_id: string;
  requested_by: string;
  request_type: ProjectAccessRequestType;
  requested_for_user_ids: string[];
  requested_role: ProjectRole;
  reason: string;
  scope_resource_kind?: string | null;
  scope_resource_id?: string | null;
  status: ProjectAccessRequestStatus;
  decided_by?: string | null;
  decision_reason?: string | null;
  created_at: string;
  decided_at?: string | null;
  completed_at?: string | null;
  tasks?: ProjectAccessRequestTask[];
}

export interface ProjectAccessRequestTask {
  id: string;
  request_id: string;
  project_id: string;
  task_type: ProjectAccessRequestTaskType;
  target_user_id: string;
  requested_role?: ProjectRole | null;
  group_id?: string | null;
  marking_id?: string | null;
  marking_name?: string | null;
  reason: string;
  status: ProjectAccessRequestTaskStatus;
  reviewer_user_ids: string[];
  external_request_message?: string | null;
  external_request_url?: string | null;
  decided_by?: string | null;
  decision_reason?: string | null;
  created_at: string;
  decided_at?: string | null;
  invoked_at?: string | null;
}

export interface ProjectAccessRequestGroupSetting {
  project_id: string;
  group_id: string;
  group_display_name?: string | null;
  group_kind: ProjectAccessGroupKind;
  request_role?: ProjectRole | null;
  reviewer_user_ids: string[];
  custom_form: Record<string, unknown>;
  external_request_message?: string | null;
  external_request_url?: string | null;
  excluded_from_request_forms: boolean;
  updated_by?: string | null;
  created_at: string;
  updated_at: string;
}

export interface ProjectRequiredMarking {
  project_id: string;
  marking_id: string;
  marking_name: string;
  reason_prompt?: string | null;
  reviewer_user_ids: string[];
  created_at: string;
  updated_at: string;
}

export interface ProjectAccessRequestFormGroup {
  group_id: string;
  role: ProjectRole;
  group_display_name?: string | null;
  group_kind: ProjectAccessGroupKind;
  reviewer_user_ids: string[];
  custom_form: Record<string, unknown>;
  external_request_message?: string | null;
  external_request_url?: string | null;
}

export interface ProjectAccessRequestForm {
  project_id: string;
  requester_id: string;
  project_owner_id: string;
  default_role: ProjectRole;
  groups: ProjectAccessRequestFormGroup[];
  required_markings: ProjectRequiredMarking[];
  direct_role_reviewers: string[];
}

interface DataEnvelope<T> {
  data: T[];
}

export async function listProjects(): Promise<OntologyProject[]> {
  const res = await api.fetch<{ data: OntologyProject[] }>(`/projects?per_page=100`);
  return res.data;
}

export async function getProject(projectId: string): Promise<OntologyProject> {
  return api.fetch<OntologyProject>(`/projects/${projectId}`);
}

export async function updateProject(
  projectId: string,
  patch: Partial<{
    display_name: string;
    description: string;
    workspace_slug: string | null;
    default_role: ProjectRole;
    point_of_contact_user_id: string | null;
    point_of_contact_email: string | null;
    references: ProjectReference[];
  }>,
): Promise<OntologyProject> {
  return api.fetch<OntologyProject>(`/projects/${projectId}`, {
    method: 'PATCH',
    body: patch,
  });
}

export async function listProjectGroupMemberships(
  projectId: string,
): Promise<ProjectGroupMembership[]> {
  const res = await api.fetch<DataEnvelope<ProjectGroupMembership>>(
    `/projects/${projectId}/group-memberships`,
  );
  return res.data;
}

export async function upsertProjectGroupMembership(
  projectId: string,
  groupId: string,
  role: ProjectRole,
): Promise<ProjectGroupMembership> {
  return api.fetch<ProjectGroupMembership>(`/projects/${projectId}/group-memberships`, {
    method: 'PUT',
    body: { group_id: groupId, role },
  });
}

export async function deleteProjectGroupMembership(
  projectId: string,
  groupId: string,
): Promise<void> {
  await api.fetch<void>(`/projects/${projectId}/group-memberships/${groupId}`, {
    method: 'DELETE',
  });
}

export async function bootstrapProjectAccessGroups(
  projectId: string,
  body: {
    viewer_group_id?: string;
    editor_group_id?: string;
    owner_group_id?: string;
  },
): Promise<{
  viewer: ProjectGroupMembership | null;
  editor: ProjectGroupMembership | null;
  owner: ProjectGroupMembership | null;
}> {
  return api.fetch<{
    viewer: ProjectGroupMembership | null;
    editor: ProjectGroupMembership | null;
    owner: ProjectGroupMembership | null;
  }>(`/projects/${projectId}/access-groups:bootstrap`, {
    method: 'POST',
    body,
  });
}

export async function listProjectAccessRequests(
  projectId: string,
  status?: ProjectAccessRequestStatus,
): Promise<ProjectAccessRequest[]> {
  const qs = status ? `?status=${encodeURIComponent(status)}` : '';
  const res = await api.fetch<DataEnvelope<ProjectAccessRequest>>(
    `/projects/${projectId}/access-requests${qs}`,
  );
  return res.data;
}

export async function listAccessRequestInbox(
  status?: ProjectAccessRequestStatus,
): Promise<ProjectAccessRequest[]> {
  const qs = status ? `?status=${encodeURIComponent(status)}` : '';
  const res = await api.fetch<DataEnvelope<ProjectAccessRequest>>(
    `/access-requests/inbox${qs}`,
  );
  return res.data;
}

export async function getProjectAccessRequestForm(
  projectId: string,
): Promise<ProjectAccessRequestForm> {
  return api.fetch<ProjectAccessRequestForm>(`/projects/${projectId}/access-request-form`);
}

export async function upsertProjectAccessRequestGroupSetting(
  projectId: string,
  groupId: string,
  body: Partial<{
    group_display_name: string;
    group_kind: ProjectAccessGroupKind;
    request_role: ProjectRole;
    reviewer_user_ids: string[];
    custom_form: Record<string, unknown>;
    external_request_message: string;
    external_request_url: string;
    excluded_from_request_forms: boolean;
  }>,
): Promise<ProjectAccessRequestGroupSetting> {
  return api.fetch<ProjectAccessRequestGroupSetting>(
    `/projects/${projectId}/access-request-groups/${groupId}`,
    { method: 'PUT', body },
  );
}

export async function deleteProjectAccessRequestGroupSetting(
  projectId: string,
  groupId: string,
): Promise<void> {
  await api.fetch<void>(`/projects/${projectId}/access-request-groups/${groupId}`, {
    method: 'DELETE',
  });
}

export async function upsertProjectRequiredMarking(
  projectId: string,
  markingId: string,
  body: {
    marking_name: string;
    reason_prompt?: string;
    reviewer_user_ids?: string[];
  },
): Promise<ProjectRequiredMarking> {
  return api.fetch<ProjectRequiredMarking>(
    `/projects/${projectId}/access-request-markings/${markingId}`,
    { method: 'PUT', body },
  );
}

export async function deleteProjectRequiredMarking(
  projectId: string,
  markingId: string,
): Promise<void> {
  await api.fetch<void>(`/projects/${projectId}/access-request-markings/${markingId}`, {
    method: 'DELETE',
  });
}

export async function createProjectAccessRequest(
  projectId: string,
  body: {
    request_type?: ProjectAccessRequestType;
    requested_for_user_ids?: string[];
    requested_role: ProjectRole;
    reason: string;
    scope_resource_kind?: string;
    scope_resource_id?: string;
    group_membership_requests?: Array<{
      group_id: string;
      target_user_id?: string;
      role?: ProjectRole;
    }>;
    project_role_requests?: Array<{
      target_user_id?: string;
      role: ProjectRole;
    }>;
    marking_access_requests?: Array<{
      marking_id: string;
      marking_name?: string;
      target_user_id?: string;
      reason?: string;
      reviewer_user_ids?: string[];
    }>;
  },
): Promise<ProjectAccessRequest> {
  return api.fetch<ProjectAccessRequest>(`/projects/${projectId}/access-requests`, {
    method: 'POST',
    body,
  });
}

export async function decideProjectAccessRequest(
  projectId: string,
  requestId: string,
  decision: 'approved' | 'denied',
  reason?: string,
): Promise<ProjectAccessRequest> {
  return api.fetch<ProjectAccessRequest>(
    `/projects/${projectId}/access-requests/${requestId}/decision`,
    { method: 'POST', body: { decision, reason } },
  );
}

export async function cancelProjectAccessRequest(
  projectId: string,
  requestId: string,
): Promise<void> {
  await api.fetch<void>(
    `/projects/${projectId}/access-requests/${requestId}:cancel`,
    { method: 'POST' },
  );
}

// ─── SG.8: role inheritance & direct grants ────────────────────────

export type GrantScopeKind = 'project' | 'folder';
export type GrantPrincipalKind = 'user' | 'group';

export interface ProjectResourceGrant {
  id: string;
  project_id: string;
  scope_kind: GrantScopeKind;
  scope_id?: string | null;
  principal_kind: GrantPrincipalKind;
  principal_id: string;
  role: ProjectRole;
  granted_by?: string | null;
  created_at: string;
  updated_at: string;
}

export type EffectiveAccessSourceKind =
  | 'project_owner'
  | 'project_default_role'
  | 'project_user_membership'
  | 'project_group_membership'
  | 'direct_user_grant'
  | 'direct_group_grant'
  | 'folder_user_grant'
  | 'folder_group_grant'
  | 'admin_role'
  | 'workspace_match';

export interface EffectiveAccessSource {
  kind: EffectiveAccessSourceKind;
  role: ProjectRole;
  grant_id?: string;
  principal_id?: string;
  group_id?: string;
  folder_id?: string;
  detail?: string;
}

export interface EffectiveAccessResponse {
  user_id: string;
  project_id: string;
  scope_kind: GrantScopeKind;
  scope_id?: string | null;
  resolved_role?: ProjectRole | null;
  sources: EffectiveAccessSource[];
  checked_at: string;
}

export async function listProjectResourceGrants(
  projectId: string,
  filter: {
    scope_kind?: GrantScopeKind;
    scope_id?: string;
    principal_kind?: GrantPrincipalKind;
    principal_id?: string;
  } = {},
): Promise<ProjectResourceGrant[]> {
  const params = new URLSearchParams();
  if (filter.scope_kind) params.set('scope_kind', filter.scope_kind);
  if (filter.scope_id) params.set('scope_id', filter.scope_id);
  if (filter.principal_kind) params.set('principal_kind', filter.principal_kind);
  if (filter.principal_id) params.set('principal_id', filter.principal_id);
  const qs = params.toString();
  const res = await api.fetch<{ data: ProjectResourceGrant[] }>(
    `/projects/${projectId}/resource-grants${qs ? `?${qs}` : ''}`,
  );
  return res.data;
}

export async function createProjectResourceGrant(
  projectId: string,
  body: {
    scope_kind: GrantScopeKind;
    scope_id?: string;
    principal_kind: GrantPrincipalKind;
    principal_id: string;
    role: ProjectRole;
  },
): Promise<ProjectResourceGrant> {
  return api.fetch<ProjectResourceGrant>(`/projects/${projectId}/resource-grants`, {
    method: 'POST',
    body,
  });
}

export async function deleteProjectResourceGrant(
  projectId: string,
  grantId: string,
): Promise<void> {
  await api.fetch<void>(`/projects/${projectId}/resource-grants/${grantId}`, {
    method: 'DELETE',
  });
}

export async function checkEffectiveAccess(
  projectId: string,
  args: {
    user_id: string;
    scope_kind?: GrantScopeKind;
    scope_id?: string;
    group_ids?: string[];
  },
): Promise<EffectiveAccessResponse> {
  const params = new URLSearchParams();
  params.set('user_id', args.user_id);
  if (args.scope_kind) params.set('scope_kind', args.scope_kind);
  if (args.scope_id) params.set('scope_id', args.scope_id);
  if (args.group_ids && args.group_ids.length > 0) {
    params.set('group_ids', args.group_ids.join(','));
  }
  return api.fetch<EffectiveAccessResponse>(
    `/projects/${projectId}/effective-access?${params.toString()}`,
  );
}
