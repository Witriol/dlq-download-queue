import { json } from '@sveltejs/kit';
import { forward } from '$lib/server/dlq';

export async function GET({ params, url, fetch }: { params: { id: string }; url: URL; fetch: typeof globalThis.fetch }) {
  const limit = url.searchParams.get('limit');
  const qs = limit ? `?limit=${encodeURIComponent(limit)}` : '';
  try {
    return await forward(fetch, `/jobs/${params.id}/events${qs}`);
  } catch (err) {
    return json({ error: err instanceof Error ? err.message : 'dlq_unreachable' }, { status: 502 });
  }
}
