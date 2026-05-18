// TODO(security): access/refresh tokens are persisted in localStorage, which
// exposes them to any script with XSS reach. Migrate to httpOnly cookies set
// by identity-federation-service (on /auth/login, /auth/refresh) and forwarded
// by the edge gateway. That requires coordinated backend changes, so it is
// tracked separately from this auth-lifecycle pass.
import { useEffect } from 'react';
import { useSyncExternalStore } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';

import api from '../api/client';
import {
  buildSsoStartUrl,
  type CompleteMfaLoginRequest,
  completeMfaLogin,
  completeSsoLogin,
  getMe,
  login as apiLogin,
  refreshToken,
  selectScopedSession,
  startSsoLogin as apiStartSsoLogin,
  type LoginResponse,
  type MfaRequiredResponse,
  type TokenResponse,
  type UserProfile,
} from '../api/auth';
import {
  buildAuthReturnToPath,
  clearStoredAuthReturnTo,
  rememberAuthReturnTo,
  withAuthReturnTo,
} from '../auth/redirects';
import { applyUserLocalePreference } from '../i18n/store';

const ACCESS_TOKEN_KEY = 'of_access_token';
const REFRESH_TOKEN_KEY = 'of_refresh_token';
const EXPIRES_AT_KEY = 'of_access_token_expires_at';
const PENDING_MFA_KEY = 'of_pending_mfa';

// Subtracted from the response's expires_in when computing the persisted
// expiry timestamp so we never treat the token as valid up to the last second
// of its real lifetime.
const TOKEN_SKEW_MS = 30_000;

// If the cached access token will expire inside this window, trigger a
// pro-active refresh before the next outbound request.
const TOKEN_REFRESH_THRESHOLD_MS = 60_000;

type AuthFlowResult = { status: 'authenticated' } | MfaRequiredResponse;
type PendingMfaChallenge = MfaRequiredResponse & { received_at: number };
type CompleteMfaInput =
  | string
  | {
      code?: string;
      recoveryCode?: string;
    };

interface AuthSnapshot {
  token: string | null;
  user: UserProfile | null;
  loading: boolean;
  pendingChallenge: PendingMfaChallenge | null;
}

const initialSnapshot: AuthSnapshot = {
  token: null,
  user: null,
  loading: false,
  pendingChallenge: null,
};

let snapshot: AuthSnapshot = initialSnapshot;
const listeners = new Set<() => void>();
let restorePromise: Promise<void> | null = null;

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function getSnapshot() {
  return snapshot;
}

function setSnapshot(next: Partial<AuthSnapshot>) {
  snapshot = { ...snapshot, ...next };
  listeners.forEach((l) => l());
}

function persistTokens(access: string, refresh: string, expiresInSec: number) {
  if (typeof localStorage === 'undefined') return;
  localStorage.setItem(ACCESS_TOKEN_KEY, access);
  localStorage.setItem(REFRESH_TOKEN_KEY, refresh);
  const expiresAt = Date.now() + expiresInSec * 1000 - TOKEN_SKEW_MS;
  localStorage.setItem(EXPIRES_AT_KEY, String(expiresAt));
}

function clearTokens() {
  if (typeof localStorage === 'undefined') return;
  localStorage.removeItem(ACCESS_TOKEN_KEY);
  localStorage.removeItem(REFRESH_TOKEN_KEY);
  localStorage.removeItem(EXPIRES_AT_KEY);
}

function getStoredRefreshToken(): string | null {
  if (typeof localStorage === 'undefined') return null;
  return localStorage.getItem(REFRESH_TOKEN_KEY);
}

function getStoredExpiresAt(): number | null {
  if (typeof localStorage === 'undefined') return null;
  const raw = localStorage.getItem(EXPIRES_AT_KEY);
  if (!raw) return null;
  const value = Number(raw);
  return Number.isFinite(value) ? value : null;
}

export function isAccessTokenExpiringSoon(): boolean {
  const expiresAt = getStoredExpiresAt();
  if (expiresAt === null) return false;
  return expiresAt - Date.now() < TOKEN_REFRESH_THRESHOLD_MS;
}

function isPendingChallengeExpired(challenge: PendingMfaChallenge) {
  if (!challenge.expires_in) return false;
  return Date.now() >= challenge.received_at + challenge.expires_in * 1000;
}

function persistChallenge(challenge: MfaRequiredResponse) {
  const pendingChallenge: PendingMfaChallenge = {
    ...challenge,
    received_at: Date.now(),
  };
  setSnapshot({ pendingChallenge });
  if (typeof sessionStorage !== 'undefined') {
    sessionStorage.setItem(PENDING_MFA_KEY, JSON.stringify(pendingChallenge));
  }
}

function clearPendingChallenge() {
  setSnapshot({ pendingChallenge: null });
  if (typeof sessionStorage !== 'undefined') {
    sessionStorage.removeItem(PENDING_MFA_KEY);
  }
}

function hydratePendingChallenge() {
  if (typeof sessionStorage === 'undefined') return;
  const raw = sessionStorage.getItem(PENDING_MFA_KEY);
  if (!raw) return;
  try {
    const parsed = JSON.parse(raw) as Partial<PendingMfaChallenge>;
    if (parsed.status !== 'mfa_required' || !parsed.challenge_token) {
      clearPendingChallenge();
      return;
    }
    const challenge: PendingMfaChallenge = {
      status: 'mfa_required',
      challenge_token: parsed.challenge_token,
      expires_in: Number(parsed.expires_in ?? 0),
      methods: Array.isArray(parsed.methods) ? parsed.methods : undefined,
      received_at: typeof parsed.received_at === 'number' ? parsed.received_at : Date.now(),
    };
    if (isPendingChallengeExpired(challenge)) {
      clearPendingChallenge();
      return;
    }
    setSnapshot({ pendingChallenge: challenge });
  } catch {
    clearPendingChallenge();
  }
}

function setSession(resp: TokenResponse) {
  api.setToken(resp.access_token);
  setSnapshot({ token: resp.access_token });
  persistTokens(resp.access_token, resp.refresh_token, resp.expires_in);
}

let pendingRefresh: Promise<string | null> | null = null;

function forceLogoutRedirect() {
  api.setToken(null);
  setSnapshot({ token: null, user: null });
  clearTokens();
  clearPendingChallenge();
  const target = (typeof globalThis !== 'undefined' ? globalThis.location : undefined) as
    | { assign?: (url: string) => void }
    | undefined;
  if (target && typeof target.assign === 'function') {
    target.assign('/auth/login');
  }
}

// Singleton-style proactive refresh: concurrent callers share the same
// in-flight promise so we never fire two POST /auth/refresh in parallel.
export function refreshAccessToken(): Promise<string | null> {
  if (pendingRefresh) return pendingRefresh;
  pendingRefresh = (async () => {
    try {
      const refresh = getStoredRefreshToken();
      if (!refresh) {
        forceLogoutRedirect();
        return null;
      }
      try {
        // skipAuthHooks avoids re-entering the pre-request hook from inside
        // the refresh call itself.
        const resp = await api.fetch<TokenResponse>('/auth/refresh', {
          method: 'POST',
          body: { refresh_token: refresh },
          skipAuthHooks: true,
        });
        setSession(resp);
        return resp.access_token;
      } catch {
        forceLogoutRedirect();
        return null;
      }
    } finally {
      pendingRefresh = null;
    }
  })();
  return pendingRefresh;
}

function updateCurrentUserProfile(profile: UserProfile) {
  setSnapshot({ user: profile });
  applyUserLocalePreference(profile.attributes);
}

async function finalizeSession(resp: TokenResponse) {
  setSession(resp);
  updateCurrentUserProfile(await getMe());
  clearPendingChallenge();
}

async function handleLoginResponse(resp: LoginResponse): Promise<AuthFlowResult> {
  if (resp.status === 'mfa_required') {
    persistChallenge(resp);
    return resp;
  }
  await finalizeSession(resp);
  return { status: 'authenticated' };
}

async function login(email: string, password: string): Promise<AuthFlowResult> {
  setSnapshot({ loading: true });
  try {
    const resp = await apiLogin({ email, password });
    return handleLoginResponse(resp);
  } finally {
    setSnapshot({ loading: false });
  }
}

function normalizeCompleteMfaInput(input: CompleteMfaInput): CompleteMfaLoginRequest {
  if (typeof input === 'string') {
    return { challenge_token: '', code: input.trim() };
  }
  return {
    challenge_token: '',
    code: input.code?.trim() || undefined,
    recovery_code: input.recoveryCode?.trim() || undefined,
  };
}

async function completeMfa(input: CompleteMfaInput): Promise<{ status: 'authenticated' }> {
  const challenge = snapshot.pendingChallenge;
  if (!challenge) {
    throw new Error('MFA challenge missing or expired');
  }
  if (isPendingChallengeExpired(challenge)) {
    clearPendingChallenge();
    throw new Error('MFA challenge missing or expired');
  }
  const payload = normalizeCompleteMfaInput(input);
  if (!payload.code && !payload.recovery_code) {
    throw new Error('MFA verification code is required');
  }
  setSnapshot({ loading: true });
  try {
    const resp = await completeMfaLogin({ ...payload, challenge_token: challenge.challenge_token });
    await finalizeSession(resp);
    return { status: 'authenticated' };
  } finally {
    setSnapshot({ loading: false });
  }
}

async function startSsoLogin(slug: string, returnTo?: string | null) {
  setSnapshot({ loading: true });
  try {
    const rememberedReturnTo = rememberAuthReturnTo(returnTo);
    const callbackTarget = withAuthReturnTo('/auth/callback', rememberedReturnTo);
    if (typeof globalThis.location !== 'undefined') {
      globalThis.location.assign(buildSsoStartUrl(slug, callbackTarget));
      return;
    }
    await apiStartSsoLogin(slug, callbackTarget);
  } finally {
    setSnapshot({ loading: false });
  }
}

async function completeTokenCallback(resp: TokenResponse): Promise<{ status: 'authenticated' }> {
  setSnapshot({ loading: true });
  try {
    await finalizeSession(resp);
    return { status: 'authenticated' };
  } finally {
    setSnapshot({ loading: false });
  }
}

async function switchScopedSession(presetId: string | null): Promise<void> {
  setSnapshot({ loading: true });
  try {
    const resp = await selectScopedSession(presetId);
    await finalizeSession(resp);
    if (typeof window !== 'undefined') {
      window.location.reload();
    }
  } finally {
    setSnapshot({ loading: false });
  }
}

async function handleSsoCallback(payload: {
  code?: string;
  state?: string;
  saml_response?: string;
  relay_state?: string;
}): Promise<AuthFlowResult> {
  setSnapshot({ loading: true });
  try {
    const resp = await completeSsoLogin(payload);
    return handleLoginResponse(resp);
  } finally {
    setSnapshot({ loading: false });
  }
}

function logout() {
  api.setToken(null);
  setSnapshot({ token: null, user: null });
  clearTokens();
  clearPendingChallenge();
  clearStoredAuthReturnTo();
}

async function restore() {
  if (restorePromise) return restorePromise;

  restorePromise = (async () => {
    setSnapshot({ loading: true });
    try {
      hydratePendingChallenge();

      if (typeof localStorage === 'undefined') return;

      const savedAccess = localStorage.getItem(ACCESS_TOKEN_KEY);
      const savedRefresh = localStorage.getItem(REFRESH_TOKEN_KEY);

      if (savedAccess) {
        api.setToken(savedAccess);
        setSnapshot({ token: savedAccess });
        try {
          updateCurrentUserProfile(await getMe());
          return;
        } catch {
          api.setToken(null);
          setSnapshot({ token: null });
        }
      }

      if (savedRefresh) {
        try {
          const refreshed = await refreshToken(savedRefresh);
          await finalizeSession(refreshed);
        } catch {
          logout();
        }
      } else {
        // Fallback: dev-auth shim does not require a token. If getMe() succeeds
        // anonymously we still treat the user as authenticated for local dev.
        try {
          updateCurrentUserProfile(await getMe());
        } catch {
          // Stay unauthenticated; the page can prompt the user to sign in.
        }
      }
    } finally {
      setSnapshot({ loading: false });
      restorePromise = null;
    }
  })();

  return restorePromise;
}

api.setPreRequestHook(async () => {
  if (isAccessTokenExpiringSoon() && getStoredRefreshToken()) {
    await refreshAccessToken();
  }
});
api.setRefreshHandler(() => refreshAccessToken());
api.setLogoutHandler(() => {
  api.setToken(null);
  setSnapshot({ token: null, user: null });
  clearTokens();
});

export const auth = {
  subscribe,
  getSnapshot,
  restore,
  login,
  completeMfa,
  startSsoLogin,
  handleSsoCallback,
  completeTokenCallback,
  switchScopedSession,
  logout,
  clearPendingChallenge,
  updateCurrentUserProfile,
};

export function useAuth() {
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}

export function useCurrentUser() {
  return useAuth().user;
}

export function useIsAuthenticated() {
  const { token, user } = useAuth();
  return Boolean(token) || Boolean(user);
}

export function usePendingMfaChallenge() {
  return useAuth().pendingChallenge;
}

export function useRequireAuth(redirectTo: string = '/auth/login') {
  const { loading, token, user } = useAuth();
  const location = useLocation();
  const navigate = useNavigate();
  const authenticated = Boolean(token) || Boolean(user);

  useEffect(() => {
    if (!loading && !authenticated) {
      const returnTo = buildAuthReturnToPath(location);
      rememberAuthReturnTo(returnTo);
      navigate(withAuthReturnTo(redirectTo, returnTo), { replace: true });
    }
  }, [loading, authenticated, location, navigate, redirectTo]);

  return { loading, authenticated };
}
