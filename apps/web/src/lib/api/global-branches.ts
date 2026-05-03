/**
 * P4 — global-branch-service client.
 *
 * Mirrors the Rust surface in
 * `services/global-branch-service/src/global/handlers.rs`. The
 * service runs behind the same gateway as everything else, so we
 * route through the shared `/api/v1` client.
 */

import api from './client';

export interface GlobalBranch {
  id: string;
  rid: string;
  name: string;
  parent_global_branch: string | null;
  description: string;
  created_by: string;
  created_at: string;
  archived_at: string | null;
}

export interface GlobalBranchSummary extends GlobalBranch {
  link_count: number;
  drifted_count: number;
  archived_count: number;
}

export interface GlobalBranchLink {
  global_branch_id: string;
  resource_type: string;
  resource_rid: string;
  branch_rid: string;
  status: 'in_sync' | 'drifted' | 'archived';
  last_synced_at: string;
}

export interface CreateGlobalBranchParams {
  name: string;
  description?: string;
  parent_global_branch?: string | null;
}

export interface CreateGlobalBranchLinkParams {
  resource_type: string;
  resource_rid: string;
  branch_rid: string;
}

export interface PromoteResponse {
  event_id: string;
  global_branch_id: string;
  topic: string;
}

export function listGlobalBranches() {
  return api.get<GlobalBranch[]>(`/global-branches`);
}

export function createGlobalBranch(params: CreateGlobalBranchParams) {
  return api.post<GlobalBranch>(`/global-branches`, params);
}

export function getGlobalBranch(id: string) {
  return api.get<GlobalBranchSummary>(`/global-branches/${encodeURIComponent(id)}`);
}

export function listGlobalBranchResources(id: string) {
  return api.get<GlobalBranchLink[]>(`/global-branches/${encodeURIComponent(id)}/resources`);
}

export function addGlobalBranchLink(id: string, params: CreateGlobalBranchLinkParams) {
  return api.post<GlobalBranchLink>(
    `/global-branches/${encodeURIComponent(id)}/links`,
    params,
  );
}

export function promoteGlobalBranch(id: string) {
  return api.post<PromoteResponse>(
    `/global-branches/${encodeURIComponent(id)}/promote`,
    {},
  );
}
