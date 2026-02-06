import { fileName, folderPath } from './format';
import type { JobStatus, JobView } from './types';

export type JobSortKey = 'id' | 'status' | 'name' | 'progress' | 'speed' | 'eta' | 'path' | 'url';
export type SortDir = 'asc' | 'desc';

export function parseUrls(text: string): string[] {
  const lines = text.split(/\r?\n/);
  const out: string[] = [];
  for (const rawLine of lines) {
    const line = rawLine.trim();
    if (!line || line.startsWith('#')) continue;
    const tokens = line.split(/[\s,]+/).map((t) => t.trim()).filter(Boolean);
    out.push(...tokens);
  }
  return out;
}

export function detectSite(url: string): string {
  if (!url) return '';
  try {
    const host = new URL(url).hostname.toLowerCase();
    if (host.includes('mega.nz') || host.includes('mega.co.nz')) return 'mega';
    if (host.includes('webshare.cz')) return 'webshare';
    return '';
  } catch {
    const lower = url.toLowerCase();
    if (lower.includes('mega.nz') || lower.includes('mega.co.nz')) return 'mega';
    if (lower.includes('webshare.cz')) return 'webshare';
    return '';
  }
}

export function countsFor(list: JobView[]): Record<JobStatus, number> {
  const counts: Record<JobStatus, number> = {
    queued: 0,
    resolving: 0,
    downloading: 0,
    paused: 0,
    completed: 0,
    failed: 0,
    deleted: 0
  };
  for (const job of list) {
    if (job.status in counts) {
      counts[job.status] += 1;
    }
  }
  return counts;
}

export function sortJobs(list: JobView[], sortKey: JobSortKey, sortDir: SortDir): JobView[] {
  const dir = sortDir === 'asc' ? 1 : -1;
  return [...list].sort((a, b) => {
    const av = getSortValue(a, sortKey);
    const bv = getSortValue(b, sortKey);
    if (typeof av === 'number' && typeof bv === 'number') {
      return (av - bv) * dir;
    }
    return String(av).localeCompare(String(bv)) * dir;
  });
}

function getSortValue(job: JobView, key: JobSortKey): number | string {
  switch (key) {
    case 'id':
      return job.id;
    case 'status':
      return job.status;
    case 'name':
      return fileName(job);
    case 'progress': {
      const total = job.size_bytes ?? 0;
      const done = job.bytes_done ?? 0;
      if (total <= 0) return done;
      return done / total;
    }
    case 'speed':
      return job.download_speed ?? 0;
    case 'eta':
      return job.eta_seconds ?? 0;
    case 'path':
      return folderPath(job);
    case 'url':
      return job.url || '';
    default:
      return job.id;
  }
}
