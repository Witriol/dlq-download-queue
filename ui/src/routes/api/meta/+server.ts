import { json } from '@sveltejs/kit';
import { forward } from '$lib/server/dlq';

export async function GET({ fetch }: { fetch: typeof globalThis.fetch }) {
  try {
    return await forward(fetch, '/meta');
  } catch (err) {
    return json({ error: err instanceof Error ? err.message : 'dlq_unreachable' }, { status: 502 });
  }
}
