import api from './client';

export interface UserProfile {
  id: string;
  email: string;
  name: string;
  is_active: boolean;
  roles: string[];
  groups: string[];
  permissions: string[];
  organization_id: string | null;
  attributes: Record<string, unknown>;
  mfa_enabled: boolean;
  mfa_enforced: boolean;
  auth_source: string;
  created_at: string;
}

export interface TokenResponse {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
}

export function getMe() {
  return api.get<UserProfile>('/users/me');
}

export function refreshToken(refresh_token: string) {
  return api.post<TokenResponse>('/auth/refresh', { refresh_token });
}

export function updateUser(
  userId: string,
  data: Partial<Pick<UserProfile, 'name' | 'organization_id' | 'attributes' | 'mfa_enforced' | 'is_active'>>,
) {
  return api.patch<UserProfile>(`/users/${userId}`, data);
}
