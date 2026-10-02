import { forwardOrError } from '$lib/server/dlq';

export async function POST({ fetch }: { fetch: typeof globalThis.fetch }) {
  return forwardOrError(fetch, '/jobs/clear', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: '{}'
  });
}
