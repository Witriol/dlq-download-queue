import { forwardOrError } from '$lib/server/dlq';

type Params = { params: { id: string; action: string } };

export async function POST({ params, request, fetch }: Params & { request: Request; fetch: typeof globalThis.fetch }) {
  const body = await request.text();
  return forwardOrError(fetch, `/series/${encodeURIComponent(params.id)}/${encodeURIComponent(params.action)}`, {
    method: 'POST',
    headers: { 'content-type': request.headers.get('content-type') || 'application/json' },
    body: body || '{}'
  });
}
