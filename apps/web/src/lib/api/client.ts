const API_BASE = import.meta.env.VITE_API_BASE_URL ?? '/api/v1';

interface RequestOptions {
  method?: string;
  body?: unknown;
  headers?: Record<string, string>;
  skipAuthHooks?: boolean;
  signal?: AbortSignal;
}

export interface CallOptions {
  signal?: AbortSignal;
}

type PreRequestHook = () => void | Promise<void>;
type RefreshHandler = () => Promise<boolean>;
type LogoutHandler = () => void;

// Authentication is now driven by the httpOnly of_session cookie set
// by identity-federation-service. The client no longer holds an access
// token in JS — every request opts into cookie propagation via
// `credentials: 'include'`. setToken is kept as a no-op so call sites
// that still invoke it during the migration window do not break; it
// will be deleted once all callers have been updated.
export class ApiClient {
  private preRequestHook: PreRequestHook | null = null;
  private refreshHandler: RefreshHandler | null = null;
  private logoutHandler: LogoutHandler | null = null;

  setToken(_token: string | null) {
    // no-op: tokens live in the httpOnly cookie.
  }

  setPreRequestHook(hook: PreRequestHook | null) {
    this.preRequestHook = hook;
  }

  setRefreshHandler(handler: RefreshHandler | null) {
    this.refreshHandler = handler;
  }

  setLogoutHandler(handler: LogoutHandler | null) {
    this.logoutHandler = handler;
  }

  authorizationHeaders(): Record<string, string> {
    return {};
  }

  async fetch<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const response = await this.request(path, options);
    if (response.status === 204) return undefined as T;
    return response.json();
  }

  async text(path: string, options: RequestOptions = {}): Promise<string> {
    const response = await this.request(path, options);
    return response.text();
  }

  private async runPreRequestHook() {
    if (!this.preRequestHook) return;
    try {
      await this.preRequestHook();
    } catch {
      // Pre-request hook failures must not block the request; the request will
      // either succeed with the existing token or take the 401 refresh path.
    }
  }

  private async request(
    path: string,
    options: RequestOptions = {},
    isRetry = false,
  ): Promise<Response> {
    if (!options.skipAuthHooks && !isRetry) {
      await this.runPreRequestHook();
    }

    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      ...options.headers,
    };

    let response: Response;
    try {
      response = await fetch(`${API_BASE}${path}`, {
        method: options.method ?? 'GET',
        headers,
        body: options.body ? JSON.stringify(options.body) : undefined,
        // The auth JWT travels as an httpOnly cookie now; every fetch
        // must opt into cookie propagation or the request lands at the
        // backend unauthenticated even though the browser has the
        // cookie.
        credentials: 'include',
        signal: options.signal,
      });
    } catch (cause) {
      if (cause instanceof DOMException && cause.name === 'AbortError') {
        throw cause;
      }
      throw new ApiUnavailableError(0, extractService(path), 'Network error', { cause });
    }

    if (response.status === 502 || response.status === 503 || response.status === 504) {
      throw new ApiUnavailableError(
        response.status,
        extractService(path),
        response.statusText || 'Service unavailable',
      );
    }

    if (!response.ok) {
      const error = await response.json().catch(() => ({ error: response.statusText }));

      if (
        response.status === 401 &&
        !options.skipAuthHooks &&
        !isRetry &&
        this.refreshHandler &&
        isTokenExpired(error)
      ) {
        const refreshed = await this.refreshHandler();
        if (!refreshed) {
          this.logoutHandler?.();
          throw new ApiError(response.status, extractMessage(error, response));
        }
        // Server rotated the of_session cookie; just retry — the new
        // cookie will be attached to the next fetch by the browser.
        return this.request(path, options, true);
      }

      throw new ApiError(response.status, extractMessage(error, response));
    }

    return response;
  }

  get<T>(path: string, options?: CallOptions) {
    return this.fetch<T>(path, { signal: options?.signal });
  }

  post<T>(path: string, body: unknown, options?: CallOptions) {
    return this.fetch<T>(path, { method: 'POST', body, signal: options?.signal });
  }

  put<T>(path: string, body: unknown, options?: CallOptions) {
    return this.fetch<T>(path, { method: 'PUT', body, signal: options?.signal });
  }

  patch<T>(path: string, body: unknown, options?: CallOptions) {
    return this.fetch<T>(path, { method: 'PATCH', body, signal: options?.signal });
  }

  delete<T>(path: string, options?: CallOptions) {
    return this.fetch<T>(path, { method: 'DELETE', signal: options?.signal });
  }
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

export class ApiUnavailableError extends ApiError {
  constructor(
    status: number,
    public service: string,
    message: string,
    options?: { cause?: unknown },
  ) {
    super(status, message);
    this.name = 'ApiUnavailableError';
    if (options?.cause !== undefined) {
      (this as { cause?: unknown }).cause = options.cause;
    }
  }
}

function isTokenExpired(error: unknown): boolean {
  if (!error || typeof error !== 'object') return false;
  const envelope = error as { code?: unknown; error?: unknown };
  if (envelope.code === 'token_expired') return true;
  if (envelope.error && typeof envelope.error === 'object') {
    const nested = envelope.error as { code?: unknown };
    if (nested.code === 'token_expired') return true;
  }
  return false;
}

function extractMessage(error: unknown, response: Response): string {
  if (!error || typeof error !== 'object') return response.statusText || 'Unknown error';
  const envelope = error as { error?: unknown; message?: unknown };
  const raw = envelope.error ?? envelope.message;
  if (typeof raw === 'string') return raw;
  if (raw && typeof raw === 'object' && typeof (raw as { message?: unknown }).message === 'string') {
    return (raw as { message: string }).message;
  }
  return response.statusText || 'Unknown error';
}

function extractService(path: string): string {
  const trimmed = path.replace(/^\/+/, '');
  const stripped = trimmed.startsWith('api/v1/') ? trimmed.slice('api/v1/'.length) : trimmed;
  const segment = stripped.split(/[/?#]/, 1)[0];
  return segment || 'unknown';
}

export const api = new ApiClient();
export default api;
