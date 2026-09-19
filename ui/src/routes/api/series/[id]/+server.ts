import { forwardOrError } from '$lib/server/dlq';

type Params = { params: { id: string } };

export async function PATCH({ params, request, fetch }: Params & { request: Request; fetch: typeof globalThis.fetch }) {
  const body = await request.text();
  return forwardOrError(fetch, `/series/${encodeURIComponent(params.id)}`, {
    method: 'PATCH',
    headers: { 'content-type': request.headers.get('content-type') || 'application/json' },
    body
  });
}

export async function DELETE({ params, fetch }: Params & { fetch: typeof globalThis.fetch }) {
  return forwardOrError(fetch, `/series/${encodeURIComponent(params.id)}`, { method: 'DELETE' });
}
