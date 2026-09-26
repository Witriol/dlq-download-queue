import { forwardOrError } from '$lib/server/dlq';

export async function GET({ fetch }: { fetch: typeof globalThis.fetch }) {
  return forwardOrError(fetch, '/series/refresh');
}

export async function POST({ request, fetch }: { request: Request; fetch: typeof globalThis.fetch }) {
  const body = await request.text();
  return forwardOrError(fetch, '/series/refresh', {
    method: 'POST',
    headers: { 'content-type': request.headers.get('content-type') || 'application/json' },
    body: body || '{}'
  });
}
