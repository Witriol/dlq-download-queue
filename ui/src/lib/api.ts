import type { BatchResult, JobView, Meta } from './types';

async function extractError(res: Response): Promise<string> {
  const text = await res.text();
  if (!text) {
    return res.statusText || `HTTP ${res.status}`;
  }
  try {
    const parsed = JSON.parse(text);
    if (parsed && typeof parsed.error === 'string') {
      return parsed.error;
    }
  } catch {
    // ignore parse errors
  }
  return text;
}

async function requestJson<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init);
  if (!res.ok) {
    throw new Error(await extractError(res));
  }
  return res.json() as Promise<T>;
}

export async function listJobs(status?: string, includeDeleted?: boolean): Promise<JobView[]> {
  const params = new URLSearchParams();
  if (status) params.set('status', status);
  if (includeDeleted) params.set('include_deleted', '1');
  const qs = params.toString();
  const url = qs ? `/api/jobs?${qs}` : '/api/jobs';
  return requestJson<JobView[]>(url);
}

export async function getEvents(id: string | number, limit = 50): Promise<string[]> {
  return requestJson<string[]>(`/api/jobs/${id}/events?limit=${limit}`);
}

export async function addJob(payload: {
  url: string;
  out_dir: string;
  name?: string;
  site?: string;
  max_attempts?: number;
}): Promise<{ id: number }> {
  return requestJson<{ id: number }>('/api/jobs', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(payload)
  });
}

export async function addJobsBatch(
  payload: {
  urls: string[];
  out_dir: string;
  name?: string;
  site?: string;
  max_attempts?: number;
},
  siteResolver?: (url: string) => string | undefined
): Promise<BatchResult[]> {
  const results: BatchResult[] = [];
  for (const url of payload.urls) {
    const resolvedSite = payload.site ?? (siteResolver ? siteResolver(url) : undefined);
    try {
      const resp = await addJob({
        url,
        out_dir: payload.out_dir,
        name: payload.name,
        site: resolvedSite,
        max_attempts: payload.max_attempts
      });
      results.push({ url, ok: true, id: resp.id });
    } catch (err) {
      results.push({ url, ok: false, error: err instanceof Error ? err.message : String(err) });
    }
  }
  return results;
}

export async function postAction(id: string | number, action: 'retry' | 'remove' | 'pause' | 'resume'):
  Promise<{ status: string }> {
  return requestJson<{ status: string }>(`/api/jobs/${id}/${action}`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: '{}'
  });
}

export async function clearJobs(): Promise<{ status: string }> {
  return requestJson<{ status: string }>('/api/jobs/clear', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: '{}'
  });
}

export async function getMeta(): Promise<Meta> {
  return requestJson<Meta>('/api/meta');
}

export async function getSettings(): Promise<{ concurrency: number; max_attempts: number }> {
  return requestJson<{ concurrency: number; max_attempts: number }>('/api/settings');
}

export async function updateSettings(
  updates: { concurrency?: number; max_attempts?: number }
): Promise<{ concurrency: number; max_attempts: number }> {
  return requestJson<{ concurrency: number; max_attempts: number }>('/api/settings', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(updates)
  });
}

export interface BrowseResponse {
  path: string;
  parent: string;
  dirs: string[];
  is_root: boolean;
}

export async function browse(path?: string): Promise<BrowseResponse> {
  const url = path ? `/api/browse?path=${encodeURIComponent(path)}` : '/api/browse';
  return requestJson<BrowseResponse>(url);
}

export async function mkdir(path: string): Promise<{ ok: boolean; path: string }> {
  return requestJson<{ ok: boolean; path: string }>('/api/browse/mkdir', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ path })
  });
}
