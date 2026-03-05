import { json } from '@sveltejs/kit';
import { forward } from '$lib/server/dlq';

const allowed = new Set(['remove', 'retry-decrypt']);

export async function POST(
  { params, fetch }: { params: { groupId: string; action: string }; fetch: typeof globalThis.fetch }
) {
  if (!allowed.has(params.action)) {
    return json({ error: 'unsupported_action' }, { status: 400 });
  }
  try {
    return await forward(fetch, `/jobs/groups/${encodeURIComponent(params.groupId)}/${params.action}`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: '{}'
    });
  } catch (err) {
    return json({ error: err instanceof Error ? err.message : 'dlq_unreachable' }, { status: 502 });
  }
}
