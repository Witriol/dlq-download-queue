import { env } from '$env/dynamic/private';
import { json } from '@sveltejs/kit';

const DEFAULT_BASE = 'http://127.0.0.1:8099';

function apiBase(): string {
  const raw = env.DLQ_API_BASE || env.DLQ_API;
  if (raw) {
    return raw.replace(/\/+$/, '');
  }
  return DEFAULT_BASE;
}

export function dlqURL(path: string): string {
  if (!path.startsWith('/')) {
    return `${apiBase()}/${path}`;
  }
  return `${apiBase()}${path}`;
}

export async function forward(fetchFn: typeof fetch, path: string, init?: RequestInit): Promise<Response> {
  const url = dlqURL(path);
  const headers = new Headers(init?.headers);
  if (init?.body && !headers.has('content-type')) {
    headers.set('content-type', 'application/json');
  }
  const resp = await fetchFn(url, { ...init, headers });
  const body = await resp.text();
  const outHeaders = new Headers();
  outHeaders.set('content-type', resp.headers.get('content-type') || 'application/json');
  return new Response(body, { status: resp.status, headers: outHeaders });
}

export async function forwardOrError(fetchFn: typeof fetch, path: string, init?: RequestInit): Promise<Response> {
  try {
    return await forward(fetchFn, path, init);
  } catch (err) {
    return json({ error: err instanceof Error ? err.message : 'dlq_unreachable' }, { status: 502 });
  }
}
