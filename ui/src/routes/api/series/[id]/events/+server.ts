import { forwardOrError } from '$lib/server/dlq';

export async function GET({ params, url, fetch }: { params: { id: string }; url: URL; fetch: typeof globalThis.fetch }) {
  const limit = url.searchParams.get('limit');
  const qs = limit ? `?limit=${encodeURIComponent(limit)}` : '';
  return forwardOrError(fetch, `/series/${encodeURIComponent(params.id)}/events${qs}`);
}
