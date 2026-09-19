import type { BatchResult, JobView, Meta, SeriesAttentionEpisode, SeriesEpisode, SeriesPreview, SeriesWatch } from './types';

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
  archive_password?: string;
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
  archive_password?: string;
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
        archive_password: payload.archive_password,
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

export async function postGroupAction(groupId: string, action: 'retry-decrypt' | 'remove'):
  Promise<{ status: string }> {
  return requestJson<{ status: string }>(`/api/jobs/groups/${encodeURIComponent(groupId)}/${action}`, {
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

export async function getSettings(): Promise<{ concurrency: number; max_attempts: number; auto_decrypt: boolean }> {
  return requestJson<{ concurrency: number; max_attempts: number; auto_decrypt: boolean }>('/api/settings');
}

export async function updateSettings(
  updates: { concurrency?: number; max_attempts?: number; auto_decrypt?: boolean }
): Promise<{ concurrency: number; max_attempts: number; auto_decrypt: boolean }> {
  return requestJson<{ concurrency: number; max_attempts: number; auto_decrypt: boolean }>('/api/settings', {
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

function unwrapSeries<T>(value: unknown, key: string): T {
  if (value && typeof value === 'object' && key in value) {
    return (value as Record<string, unknown>)[key] as T;
  }
  return value as T;
}

export async function listSeries(): Promise<SeriesWatch[]> {
  const response = await requestJson<unknown>('/api/series');
  const value = unwrapSeries<unknown>(response, 'series');
  return Array.isArray(value) ? value as SeriesWatch[] : [];
}

export type SeriesDraft = {
  reference_url?: string;
  reference_webshare_url?: string;
  reference_webshare_ident?: string;
  reference_filename?: string;
  out_dir: string;
  series_folder?: string;
  organize_by_season?: boolean;
  tvmaze_id?: number;
  display_name?: string;
  search_title?: string;
  initial_mode: string;
  /** Backend name for initial_mode; UI sends both for forward compatibility. */
  start_mode?: string;
  initial_season?: number;
  initial_episode?: number;
  fallback_policy: string;
  release_delay_seconds: number;
  preferred_wait_seconds: number;
  quality_profile?: Record<string, unknown>;
  preview_episode?: number | string;
};

export async function previewSeries(payload: Partial<SeriesDraft>): Promise<SeriesPreview> {
  return requestJson<SeriesPreview>('/api/series/preview', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(payload)
  });
}

export async function createSeries(payload: SeriesDraft): Promise<SeriesWatch> {
  return requestJson<SeriesWatch>('/api/series', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(payload)
  });
}

export async function updateSeries(id: string | number, payload: Partial<SeriesDraft>): Promise<SeriesWatch> {
  return requestJson<SeriesWatch>(`/api/series/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(payload)
  });
}

export async function seriesAction(id: string | number, action: 'check-now' | 'pause' | 'resume' | 'remove'):
  Promise<SeriesWatch | { status: string }> {
  return requestJson<SeriesWatch | { status: string }>(`/api/series/${encodeURIComponent(id)}/${action}`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: '{}'
  });
}

export async function listSeriesAttention(id: string | number): Promise<SeriesAttentionEpisode[]> {
  const response = await requestJson<unknown>(`/api/series/${encodeURIComponent(id)}/attention`);
  const value = unwrapSeries<unknown>(response, 'episodes');
  return Array.isArray(value) ? value as SeriesAttentionEpisode[] : [];
}

export async function selectSeriesCandidate(
  watchId: string | number,
  episodeId: string | number,
  ident: string
): Promise<SeriesEpisode> {
  return requestJson<SeriesEpisode>(
    `/api/series/${encodeURIComponent(watchId)}/episodes/${encodeURIComponent(episodeId)}/select`,
    {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ ident })
    }
  );
}

export type TVMazeShow = {
  id: number;
  name: string;
  premiered?: string;
  ended?: string | null;
  status?: string;
  network?: { name?: string } | null;
  image?: { medium?: string; original?: string } | null;
  summary?: string | null;
  [key: string]: unknown;
};

export async function searchTVMaze(query: string): Promise<TVMazeShow[]> {
  const response = await requestJson<unknown>(`/api/tvmaze/search?q=${encodeURIComponent(query)}`);
  const value = unwrapSeries<unknown>(response, 'results');
  const rows = Array.isArray(value) ? value : [];
  return rows.map((item) => {
    if (item && typeof item === 'object' && 'show' in item) return (item as { show: TVMazeShow }).show;
    return item as TVMazeShow;
  }).filter((show) => show && Number.isFinite(Number(show.id)));
}
