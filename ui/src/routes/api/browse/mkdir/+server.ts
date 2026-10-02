import { json } from '@sveltejs/kit';
import { forwardOrError } from '$lib/server/dlq';

export async function POST({ fetch, request }: { fetch: typeof globalThis.fetch; request: Request }) {
  let body: string;
  try {
    body = await request.text();
  } catch (err) {
    return json({ error: err instanceof Error ? err.message : 'dlq_unreachable' }, { status: 502 });
  }
  return forwardOrError(fetch, '/api/browse/mkdir', { method: 'POST', body });
}
