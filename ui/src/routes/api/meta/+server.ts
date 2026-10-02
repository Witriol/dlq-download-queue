import { forwardOrError } from '$lib/server/dlq';

export async function GET({ fetch }: { fetch: typeof globalThis.fetch }) {
  return forwardOrError(fetch, '/meta');
}
