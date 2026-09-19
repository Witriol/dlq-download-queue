import { forwardOrError } from '$lib/server/dlq';

export async function POST({ request, fetch }: { request: Request; fetch: typeof globalThis.fetch }) {
  const body = await request.text();
  return forwardOrError(fetch, '/series/preview', {
    method: 'POST',
    headers: { 'content-type': request.headers.get('content-type') || 'application/json' },
    body
  });
}
