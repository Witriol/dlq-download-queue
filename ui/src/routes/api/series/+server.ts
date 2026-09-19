import { forwardOrError } from '$lib/server/dlq';

export async function GET({ url, fetch }: { url: URL; fetch: typeof globalThis.fetch }) {
  const qs = url.searchParams.toString();
  return forwardOrError(fetch, qs ? `/series?${qs}` : '/series');
}

export async function POST({ request, fetch }: { request: Request; fetch: typeof globalThis.fetch }) {
  const body = await request.text();
  return forwardOrError(fetch, '/series', {
    method: 'POST',
    headers: { 'content-type': request.headers.get('content-type') || 'application/json' },
    body
  });
}
