import { json } from '@sveltejs/kit';
import { forward } from '$lib/server/dlq';

export async function POST({ fetch }: { fetch: typeof globalThis.fetch }) {
  try {
    return await forward(fetch, '/jobs/clear', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: '{}'
    });
  } catch (err) {
    return json({ error: err instanceof Error ? err.message : 'dlq_unreachable' }, { status: 502 });
  }
}
