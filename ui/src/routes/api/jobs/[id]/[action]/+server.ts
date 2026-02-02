import { json } from '@sveltejs/kit';
import { forward } from '$lib/server/dlq';

const allowed = new Set(['retry', 'remove', 'pause', 'resume']);

export async function POST({ params, fetch }: { params: { id: string; action: string }; fetch: typeof globalThis.fetch }) {
  if (!allowed.has(params.action)) {
    return json({ error: 'unsupported_action' }, { status: 400 });
  }
  try {
    return await forward(fetch, `/jobs/${params.id}/${params.action}`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: '{}'
    });
  } catch (err) {
    return json({ error: err instanceof Error ? err.message : 'dlq_unreachable' }, { status: 502 });
  }
}
