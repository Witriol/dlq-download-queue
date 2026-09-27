import { json } from '@sveltejs/kit';
import { forward } from '$lib/server/dlq';

export async function POST({ fetch, request }: { fetch: typeof globalThis.fetch; request: Request }) {
  try {
    const body = await request.text();
    return await forward(fetch, '/api/settings/telegram/test', { method: 'POST', body });
  } catch (err) {
    return json({ error: err instanceof Error ? err.message : 'dlq_unreachable' }, { status: 502 });
  }
}
