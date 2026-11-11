import { vi } from 'vitest';

export interface Call {
  method: string;
  path: string;
  headers: Record<string, string>;
  body: unknown;
}

type Handler = (call: Call) => unknown | Response | Promise<unknown | Response>;

/**
 * Replaces fetch with a router over "METHOD /path" keys, so components run
 * against the real API client (headers, status handling, error bodies)
 * without a server. A handler returns a Response, or any value to be sent
 * as 200 JSON. Unmatched requests fail the test loudly with a 599.
 */
export function fakeApi(routes: Record<string, Handler>) {
  const calls: Call[] = [];
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const url = new URL(String(input), 'http://localhost');
    const method = init.method ?? 'GET';
    const headers = Object.fromEntries(Object.entries((init.headers as Record<string, string>) ?? {}));
    let body: unknown = init.body;
    if (typeof body === 'string' && headers['Content-Type'] === 'application/json') body = JSON.parse(body);
    const call: Call = { method, path: url.pathname, headers, body };
    calls.push(call);
    const handler = routes[`${method} ${url.pathname}`];
    if (!handler) return json({ error: `unexpected ${method} ${url.pathname}`, code: 'test' }, 599);
    const out = await handler(call);
    return out instanceof Response ? out : json(out);
  });
  vi.stubGlobal('fetch', fetchMock);
  return { calls, fetch: fetchMock, find: (method: string, path: string) => calls.filter((c) => c.method === method && c.path === path) };
}

export function json(body: unknown, status = 200): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });
}

export function apiError(status: number, error: string, code: string, details?: unknown): Response {
  return json(details === undefined ? { error, code } : { error, code, details }, status);
}
