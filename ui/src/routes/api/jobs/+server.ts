import { forwardOrError } from '$lib/server/dlq';

export async function GET({ url, fetch }: { url: URL; fetch: typeof globalThis.fetch }) {
  const params = new URLSearchParams();
  const status = url.searchParams.get('status');
  const includeDeleted = url.searchParams.get('include_deleted');
  if (status) params.set('status', status);
  if (includeDeleted) params.set('include_deleted', includeDeleted);
  const qs = params.toString();
  const path = qs ? `/jobs?${qs}` : '/jobs';
  return forwardOrError(fetch, path);
}

export async function POST({ request, fetch }: { request: Request; fetch: typeof globalThis.fetch }) {
  const body = await request.text();
  return forwardOrError(fetch, '/jobs', {
    method: 'POST',
    headers: { 'content-type': request.headers.get('content-type') || 'application/json' },
    body
  });
}
