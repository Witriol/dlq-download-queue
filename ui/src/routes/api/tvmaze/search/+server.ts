import { forwardOrError } from '$lib/server/dlq';

export async function GET({ url, fetch }: { url: URL; fetch: typeof globalThis.fetch }) {
  const qs = url.searchParams.toString();
  return forwardOrError(fetch, qs ? `/tvmaze/search?${qs}` : '/tvmaze/search');
}
