import { json } from '@sveltejs/kit';
import { forward } from '$lib/server/dlq';

export async function GET({ fetch, url }: { fetch: typeof globalThis.fetch; url: URL }) {
  try {
    const path = url.searchParams.get('path');
    const endpoint = path ? `/browse?path=${encodeURIComponent(path)}` : '/browse';
    return await forward(fetch, endpoint);
  } catch (err) {
    return json({ error: err instanceof Error ? err.message : 'dlq_unreachable' }, { status: 502 });
  }
}
