import { forwardOrError } from '$lib/server/dlq';

export async function GET({ fetch, url }: { fetch: typeof globalThis.fetch; url: URL }) {
  const path = url.searchParams.get('path');
  const endpoint = path ? `/api/browse?path=${encodeURIComponent(path)}` : '/api/browse';
  return forwardOrError(fetch, endpoint);
}
