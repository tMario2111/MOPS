export class ApiError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

export function getApiBaseUrl(): string {
  const configured: unknown = import.meta.env.VITE_API_BASE_URL;
  return typeof configured === 'string' && configured.length > 0
    ? configured
    : 'http://localhost:8080';
}

/**
 * Calls the API with the session cookie attached and returns the parsed JSON
 * body (`undefined` for 204). Non-2xx responses throw an `ApiError` carrying
 * the server's `error` message; network failures throw a plain `Error`.
 */
export async function apiRequest<T = undefined>(
  path: string,
  options: { method?: 'GET' | 'POST'; body?: Record<string, unknown> } = {}
): Promise<T> {
  const { method = 'POST', body } = options;

  let response: Response;
  try {
    response = await fetch(`${getApiBaseUrl()}${path}`, {
      method,
      credentials: 'include',
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch (cause) {
    throw new Error('Could not reach the server. Check your connection and try again.', { cause });
  }

  if (!response.ok) {
    throw new ApiError(await readApiError(response), response.status);
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

async function readApiError(response: Response): Promise<string> {
  try {
    const body = (await response.json()) as { error?: unknown };
    if (typeof body.error === 'string' && body.error !== '') {
      return body.error;
    }
  } catch {
    // fall through to the generic message
  }
  return `Request failed (${response.status}).`;
}

export function getFormValue(formData: FormData, name: string): string {
  const value = formData.get(name);
  return typeof value === 'string' ? value : '';
}
