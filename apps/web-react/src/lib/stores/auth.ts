import { useSyncExternalStore } from 'react';

import api from '../api/client';
import { getMe, refreshToken, type UserProfile } from '../api/auth';
import { applyUserLocalePreference } from '../i18n/store';

const ACCESS_TOKEN_KEY = 'of_access_token';
const REFRESH_TOKEN_KEY = 'of_refresh_token';

interface AuthSnapshot {
  token: string | null;
  user: UserProfile | null;
  loading: boolean;
}

const initialSnapshot: AuthSnapshot = {
  token: null,
  user: null,
  loading: false,
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

function persistTokens(access: string, refresh: string) {
  if (typeof localStorage === 'undefined') return;
  localStorage.setItem(ACCESS_TOKEN_KEY, access);
  localStorage.setItem(REFRESH_TOKEN_KEY, refresh);
}

function clearTokens() {
  if (typeof localStorage === 'undefined') return;
  localStorage.removeItem(ACCESS_TOKEN_KEY);
  localStorage.removeItem(REFRESH_TOKEN_KEY);
}

function updateCurrentUserProfile(profile: UserProfile) {
  setSnapshot({ user: profile });
  applyUserLocalePreference(profile.attributes);
}

function logout() {
  api.setToken(null);
  setSnapshot({ token: null, user: null });
  clearTokens();
}

async function restore() {
  if (restorePromise) return restorePromise;

  restorePromise = (async () => {
    setSnapshot({ loading: true });
    try {
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
          api.setToken(refreshed.access_token);
          persistTokens(refreshed.access_token, refreshed.refresh_token);
          setSnapshot({ token: refreshed.access_token });
          updateCurrentUserProfile(await getMe());
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

export const auth = {
  subscribe,
  getSnapshot,
  restore,
  logout,
  updateCurrentUserProfile,
};

export function useAuth() {
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}

export function useCurrentUser() {
  return useAuth().user;
}

export function useIsAuthenticated() {
  return Boolean(useAuth().token) || Boolean(useAuth().user);
}
