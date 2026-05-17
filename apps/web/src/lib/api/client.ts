const API_BASE = import.meta.env.VITE_API_BASE_URL ?? '/api/v1';

interface RequestOptions {
  method?: string;
  body?: unknown;
  headers?: Record<string, string>;
}

class ApiClient {
  private token: string | null = null;

  setToken(token: string | null) {
    this.token = token;
  }

  authorizationHeaders(): Record<string, string> {
    return this.token ? { Authorization: `Bearer ${this.token}` } : {};
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

  private async request(path: string, options: RequestOptions = {}): Promise<Response> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      ...options.headers,
    };

    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`;
    }

    let response: Response;
    try {
      response = await fetch(`${API_BASE}${path}`, {
        method: options.method ?? 'GET',
        headers,
        body: options.body ? JSON.stringify(options.body) : undefined,
      });
    } catch (cause) {
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
      const raw = error?.error ?? error?.message;
      let message: string;
      if (typeof raw === 'string') {
        message = raw;
      } else if (raw && typeof raw === 'object' && typeof raw.message === 'string') {
        message = raw.message;
      } else {
        message = response.statusText || 'Unknown error';
      }
      throw new ApiError(response.status, message);
    }

    return response;
  }

  get<T>(path: string) {
    return this.fetch<T>(path);
  }

  post<T>(path: string, body: unknown) {
    return this.fetch<T>(path, { method: 'POST', body });
  }

  put<T>(path: string, body: unknown) {
    return this.fetch<T>(path, { method: 'PUT', body });
  }

  patch<T>(path: string, body: unknown) {
    return this.fetch<T>(path, { method: 'PATCH', body });
  }

  delete<T>(path: string) {
    return this.fetch<T>(path, { method: 'DELETE' });
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

function extractService(path: string): string {
  const trimmed = path.replace(/^\/+/, '');
  const stripped = trimmed.startsWith('api/v1/') ? trimmed.slice('api/v1/'.length) : trimmed;
  const segment = stripped.split(/[/?#]/, 1)[0];
  return segment || 'unknown';
}

export const api = new ApiClient();
export default api;
