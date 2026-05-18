// Authentication is driven by the httpOnly `of_session` cookie set by
// identity-federation-service. The store no longer holds an access or
// refresh token — both live exclusively in cookies the JS cannot read,
// which removes the localStorage XSS vector. Session validity is
// inferred from a successful GET /users/me; expiry is observable only
// as a 401 from the server.
//
// SSO and scoped-session flows still return TokenResponse JSON for
// non-cookie consumers; the SPA now ignores those bodies because the
// backend rotates the cookie alongside them.
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
  logout as apiLogout,
  selectScopedSession,
  startSsoLogin as apiStartSsoLogin,
  type LoginResponse,
  type MfaRequiredResponse,
  type UserProfile,
} from '../api/auth';
import {
  buildAuthReturnToPath,
  clearStoredAuthReturnTo,
  rememberAuthReturnTo,
  withAuthReturnTo,
} from '../auth/redirects';
import { applyUserLocalePreference } from '../i18n/store';

const PENDING_MFA_KEY = 'of_pending_mfa';

type AuthFlowResult = { status: 'authenticated' } | MfaRequiredResponse;
type PendingMfaChallenge = MfaRequiredResponse & { received_at: number };
type CompleteMfaInput =
  | string
  | {
      code?: string;
      recoveryCode?: string;
    };

interface AuthSnapshot {
  user: UserProfile | null;
  loading: boolean;
  pendingChallenge: PendingMfaChallenge | null;
}

const initialSnapshot: AuthSnapshot = {
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

let pendingRefresh: Promise<boolean> | null = null;

function forceLogoutRedirect() {
  setSnapshot({ user: null });
  clearPendingChallenge();
  const target = (typeof globalThis !== 'undefined' ? globalThis.location : undefined) as
    | { assign?: (url: string) => void }
    | undefined;
  if (target && typeof target.assign === 'function') {
    target.assign('/auth/login');
  }
}

// Singleton-style refresh: concurrent callers share the same in-flight
// promise so we never fire two POST /auth/refresh in parallel. The
// server reads the refresh token from the of_refresh cookie, so the
// request body is empty; success is signalled by 200 + a rotated
// of_session cookie that the browser stores automatically.
export function refreshAccessToken(): Promise<boolean> {
  if (pendingRefresh) return pendingRefresh;
  pendingRefresh = (async () => {
    try {
      try {
        await api.fetch<unknown>('/auth/token/refresh', {
          method: 'POST',
          body: {},
          skipAuthHooks: true,
        });
        return true;
      } catch {
        forceLogoutRedirect();
        return false;
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

async function finalizeSession() {
  updateCurrentUserProfile(await getMe());
  clearPendingChallenge();
}

async function handleLoginResponse(resp: LoginResponse): Promise<AuthFlowResult> {
  if (resp.status === 'mfa_required') {
    persistChallenge(resp);
    return resp;
  }
  await finalizeSession();
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
    await completeMfaLogin({ ...payload, challenge_token: challenge.challenge_token });
    await finalizeSession();
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

async function completeTokenCallback(): Promise<{ status: 'authenticated' }> {
  setSnapshot({ loading: true });
  try {
    await finalizeSession();
    return { status: 'authenticated' };
  } finally {
    setSnapshot({ loading: false });
  }
}

async function switchScopedSession(presetId: string | null): Promise<void> {
  setSnapshot({ loading: true });
  try {
    await selectScopedSession(presetId);
    await finalizeSession();
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

function clearLocalAuthState() {
  setSnapshot({ user: null });
  clearPendingChallenge();
  clearStoredAuthReturnTo();
}

async function logout() {
  try {
    await apiLogout();
  } catch {
    // Server-side cookie clear is best-effort; the local state still
    // drops so the UI bounces back to login regardless.
  }
  clearLocalAuthState();
}

async function restore() {
  if (restorePromise) return restorePromise;

  restorePromise = (async () => {
    setSnapshot({ loading: true });
    try {
      hydratePendingChallenge();
      try {
        updateCurrentUserProfile(await getMe());
      } catch {
        // No session cookie (or expired) — stay unauthenticated; the
        // route guard will redirect to /auth/login when needed.
      }
    } finally {
      setSnapshot({ loading: false });
      restorePromise = null;
    }
  })();

  return restorePromise;
}

api.setRefreshHandler(() => refreshAccessToken());
api.setLogoutHandler(() => {
  clearLocalAuthState();
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
  return Boolean(useAuth().user);
}

export function usePendingMfaChallenge() {
  return useAuth().pendingChallenge;
}

export function useRequireAuth(redirectTo: string = '/auth/login') {
  const { loading, user } = useAuth();
  const location = useLocation();
  const navigate = useNavigate();
  const authenticated = Boolean(user);

  useEffect(() => {
    if (!loading && !authenticated) {
      const returnTo = buildAuthReturnToPath(location);
      rememberAuthReturnTo(returnTo);
      navigate(withAuthReturnTo(redirectTo, returnTo), { replace: true });
    }
  }, [loading, authenticated, location, navigate, redirectTo]);

  return { loading, authenticated };
}
