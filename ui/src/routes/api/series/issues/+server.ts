import { forwardOrError } from '#lib/server/dlq.js';

export async function GET({ fetch }: { fetch: typeof globalThis.fetch }) {
  return forwardOrError(fetch, '/series/issues');
}
