import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiClient, ApiError, ApiUnavailableError } from './client';

describe('ApiClient lifecycle hooks', () => {
  let client: ApiClient;

  beforeEach(() => {
    client = new ApiClient();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('runs the pre-request hook before each request', async () => {
    const hook = vi.fn(async () => {});
    client.setPreRequestHook(hook);
    const fetchMock = mockFetchOnce(200, { ok: true });
    vi.stubGlobal('fetch', fetchMock);

    await client.get('/foo');

    expect(hook).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('proceeds with the request even when the pre-request hook throws', async () => {
    client.setPreRequestHook(async () => {
      throw new Error('refresh boom');
    });
    const fetchMock = mockFetchOnce(200, { ok: true });
    vi.stubGlobal('fetch', fetchMock);

    await expect(client.get('/foo')).resolves.toEqual({ ok: true });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('retries once after a 401 with code=token_expired using the refreshed token', async () => {
    client.setToken('stale');
    const refresh = vi.fn(async () => {
      client.setToken('fresh');
      return 'fresh';
    });
    client.setRefreshHandler(refresh);

    const fetchMock = mockFetchSequence([
      makeResponse(401, { error: { code: 'token_expired', message: 'expired' } }),
      makeResponse(200, { ok: true }),
    ]);
    vi.stubGlobal('fetch', fetchMock);

    const result = await client.get<{ ok: true }>('/foo');

    expect(result).toEqual({ ok: true });
    expect(refresh).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(authHeaderOf(fetchMock.mock.calls[0])).toBe('Bearer stale');
    expect(authHeaderOf(fetchMock.mock.calls[1])).toBe('Bearer fresh');
  });

  it('accepts the token_expired code at the top-level of the error envelope', async () => {
    const refresh = vi.fn(async () => 'fresh');
    client.setRefreshHandler(refresh);

    const fetchMock = mockFetchSequence([
      makeResponse(401, { code: 'token_expired' }),
      makeResponse(200, { ok: true }),
    ]);
    vi.stubGlobal('fetch', fetchMock);

    await expect(client.get('/foo')).resolves.toEqual({ ok: true });
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it('does not retry a 401 without a token_expired code', async () => {
    const refresh = vi.fn();
    client.setRefreshHandler(refresh);
    const fetchMock = mockFetchOnce(401, { error: { message: 'forbidden' } });
    vi.stubGlobal('fetch', fetchMock);

    await expect(client.get('/foo')).rejects.toBeInstanceOf(ApiError);
    expect(refresh).not.toHaveBeenCalled();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('invokes the logout handler when the 401 retry refresh returns null', async () => {
    const refresh = vi.fn(async () => null);
    const onLogout = vi.fn();
    client.setRefreshHandler(refresh);
    client.setLogoutHandler(onLogout);

    const fetchMock = mockFetchOnce(401, { code: 'token_expired' });
    vi.stubGlobal('fetch', fetchMock);

    await expect(client.get('/foo')).rejects.toBeInstanceOf(ApiError);
    expect(refresh).toHaveBeenCalledTimes(1);
    expect(onLogout).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('skipAuthHooks bypasses both the pre-request hook and the 401 retry', async () => {
    const hook = vi.fn();
    const refresh = vi.fn();
    client.setPreRequestHook(hook);
    client.setRefreshHandler(refresh);

    const fetchMock = mockFetchOnce(401, { code: 'token_expired' });
    vi.stubGlobal('fetch', fetchMock);

    await expect(client.fetch('/foo', { skipAuthHooks: true })).rejects.toBeInstanceOf(ApiError);
    expect(hook).not.toHaveBeenCalled();
    expect(refresh).not.toHaveBeenCalled();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('throws ApiUnavailableError on network failures', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new TypeError('network');
      }),
    );

    await expect(client.get('/foo')).rejects.toBeInstanceOf(ApiUnavailableError);
  });

  it('propagates an AbortSignal to fetch and surfaces aborts as AbortError', async () => {
    const controller = new AbortController();
    const fetchMock = vi.fn(async (_url: unknown, init: RequestInit | undefined) => {
      expect(init?.signal).toBe(controller.signal);
      controller.abort();
      throw new DOMException('aborted', 'AbortError');
    });
    vi.stubGlobal('fetch', fetchMock);

    await expect(client.get('/foo', { signal: controller.signal })).rejects.toMatchObject({
      name: 'AbortError',
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('runs the pre-request hook before issuing the underlying fetch', async () => {
    const order: string[] = [];
    client.setPreRequestHook(async () => {
      order.push('pre-request');
    });
    const fetchMock = vi.fn(async () => {
      order.push('fetch');
      return new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      });
    });
    vi.stubGlobal('fetch', fetchMock);

    await client.get('/foo');

    expect(order).toEqual(['pre-request', 'fetch']);
  });
});

function makeResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });
}

function mockFetchOnce(status: number, body: unknown) {
  return vi.fn(async () => makeResponse(status, body));
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

function authHeaderOf(call: unknown[]): string | undefined {
  const init = call[1] as RequestInit | undefined;
  const headers = init?.headers as Record<string, string> | undefined;
  return headers?.Authorization;
}
