import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import api from '../api/client';
import { auth } from './auth';

describe('auth cookie lifecycle', () => {
  beforeEach(() => {
    // localStorage is mocked but expected to remain empty for auth state —
    // the httpOnly of_session cookie owns the access token now and the JS
    // store must never persist it.
    mockLocalStorage();
    mockSessionStorage();
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-05-17T12:00:00Z'));
  });

  afterEach(() => {
    api.setToken(null);
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('does not persist any auth token in localStorage after login', async () => {
    const fetchMock = mockFetchSequence([
      jsonResponse(200, {
        status: 'authenticated',
        access_token: 'ignored-by-spa',
        refresh_token: 'ignored-by-spa',
        token_type: 'Bearer',
        expires_in: 3600,
      }),
      jsonResponse(200, sampleProfile()),
    ]);
    vi.stubGlobal('fetch', fetchMock);

    await auth.login('user@example.com', 'hunter2');

    expect(localStorage.getItem('of_access_token')).toBeNull();
    expect(localStorage.getItem('of_refresh_token')).toBeNull();
    expect(localStorage.getItem('of_access_token_expires_at')).toBeNull();
    // The browser-side cookie is invisible to JS, so success is observed
    // through getMe() returning a profile.
    expect(auth.getSnapshot().user?.email).toBe('user@example.com');
  });

  it('sends credentials:include on every authed fetch', async () => {
    const fetchMock = mockFetchSequence([jsonResponse(200, { items: [] })]);
    vi.stubGlobal('fetch', fetchMock);

    await api.get('/some/resource');

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const call = fetchMock.mock.calls[0] as unknown[];
    const init = (call[1] ?? {}) as RequestInit;
    expect(init.credentials).toBe('include');
    expect((init.headers as Record<string, string>)['Authorization']).toBeUndefined();
  });

  it('refreshes via /auth/token/refresh on 401 token_expired and retries the original request', async () => {
    const fetchMock = mockFetchByUrl({
      '/auth/token/refresh': () => jsonResponse(200, {}),
      '/some/resource': (() => {
        let firstCall = true;
        return () => {
          if (firstCall) {
            firstCall = false;
            return jsonResponse(401, { error: { code: 'token_expired', message: 'expired' } });
          }
          return jsonResponse(200, { ok: true });
        };
      })(),
    });
    vi.stubGlobal('fetch', fetchMock);

    const out = await api.get('/some/resource');

    expect(out).toEqual({ ok: true });
    const urls = fetchMock.mock.calls.map((c) => urlOf(c));
    expect(urls.some((u) => u.includes('/auth/token/refresh'))).toBe(true);
    expect(urls.filter((u) => u.includes('/some/resource'))).toHaveLength(2);
  });

  it('redirects to /auth/login when refresh fails', async () => {
    const fetchMock = mockFetchSequence([
      jsonResponse(401, { error: { code: 'token_expired', message: 'expired' } }),
      jsonResponse(401, { error: 'invalid_grant' }),
    ]);
    vi.stubGlobal('fetch', fetchMock);

    const assignMock = vi.fn();
    vi.stubGlobal('location', { assign: assignMock, href: 'http://localhost/' });

    await expect(api.get('/some/resource')).rejects.toBeDefined();
    expect(assignMock).toHaveBeenCalledWith('/auth/login');
  });

  it('logout POSTs to /auth/logout and clears the in-memory user', async () => {
    const fetchMock = mockFetchSequence([new Response(null, { status: 204 })]);
    vi.stubGlobal('fetch', fetchMock);

    // Seed a fake authenticated user.
    auth.updateCurrentUserProfile(sampleProfile());
    expect(auth.getSnapshot().user).not.toBeNull();

    await auth.logout();

    expect(urlOf(fetchMock.mock.calls[0])).toContain('/auth/logout');
    expect(auth.getSnapshot().user).toBeNull();
  });
});

function mockLocalStorage() {
  const store = new Map<string, string>();
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => {
      store.set(key, value);
    },
    removeItem: (key: string) => {
      store.delete(key);
    },
    clear: () => {
      store.clear();
    },
    key: (i: number) => Array.from(store.keys())[i] ?? null,
    get length() {
      return store.size;
    },
  });
}

function mockSessionStorage() {
  const store = new Map<string, string>();
  vi.stubGlobal('sessionStorage', {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => {
      store.set(key, value);
    },
    removeItem: (key: string) => {
      store.delete(key);
    },
    clear: () => {
      store.clear();
    },
    key: (i: number) => Array.from(store.keys())[i] ?? null,
    get length() {
      return store.size;
    },
  });
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(typeof body === 'string' ? body : JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });
}

function mockFetchSequence(responses: Response[]) {
  let i = 0;
  return vi.fn(async () => {
    if (i >= responses.length) {
      throw new Error(`fetch invoked ${i + 1} times; only ${responses.length} mocked`);
    }
    return responses[i++];
  });
}

function mockFetchByUrl(handlers: Record<string, () => Response>) {
  return vi.fn(async (input: unknown) => {
    const url = String(input);
    for (const [suffix, factory] of Object.entries(handlers)) {
      if (url.includes(suffix)) return factory();
    }
    throw new Error(`no mock handler for ${url}`);
  });
}

function urlOf(call: unknown[]): string {
  return String(call[0]);
}

function sampleProfile() {
  return {
    id: 'u-1',
    email: 'user@example.com',
    name: 'User One',
    is_active: true,
    roles: [],
    groups: [],
    permissions: [],
    organization_id: null,
    attributes: {},
    mfa_enabled: false,
    mfa_enforced: false,
    auth_source: 'local',
    created_at: '2026-05-01T00:00:00Z',
  };
}
