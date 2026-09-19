import { forwardOrError } from '$lib/server/dlq';

type Params = { params: { id: string } };

export async function GET({ params, fetch }: Params & { fetch: typeof globalThis.fetch }) {
  return forwardOrError(fetch, `/series/${encodeURIComponent(params.id)}/attention`);
}
