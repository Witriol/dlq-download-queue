import type { JobView } from './types';

const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];

export function humanBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) {
    return '0.0B';
  }
  let val = n;
  let idx = 0;
  while (val >= 1024 && idx < units.length - 1) {
    val /= 1024;
    idx += 1;
  }
  return `${val.toFixed(1)}${units[idx]}`;
}

export function humanDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return '-';
  }
  const s = Math.floor(seconds);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (h > 0) {
    return `${h}h${String(m).padStart(2, '0')}m`;
  }
  if (m > 0) {
    return `${m}m${String(sec).padStart(2, '0')}s`;
  }
  return `${sec}s`;
}

export function filePath(job: JobView): string {
  const name = job.filename || job.name || shortURL(job.url);
  if (!name) {
    return shortURL(job.url);
  }
  return `${job.out_dir.replace(/\/$/, '')}/${name}`;
}

export function fileName(job: JobView): string {
  const name = job.filename || job.name || shortURL(job.url);
  if (!name) {
    return '-';
  }
  return name;
}

export function folderPath(job: JobView): string {
  const outDir = job.out_dir || '';
  if (!outDir || outDir === '/') {
    return outDir || '';
  }
  if (outDir.endsWith('/')) {
    return outDir;
  }
  return `${outDir}/`;
}

export function formatProgress(job: JobView): string {
  const total = job.size_bytes ?? 0;
  let done = job.bytes_done ?? 0;
  if (done === 0 && (job.status === 'completed' || job.status === 'decrypting' || job.status === 'decrypt_failed') && total > 0) {
    done = total;
  }
  if (total <= 0) {
    return humanBytes(done);
  }
  const pct = Math.min(100, (done / total) * 100);
  if (pct >= 100) {
    return humanBytes(total);
  }
  return `${humanBytes(done)} / ${humanBytes(total)} (${pct.toFixed(1)}%)`;
}

export function formatSpeed(job: JobView): string {
  if (job.status !== 'downloading' || !job.download_speed || job.download_speed <= 0) {
    return '-';
  }
  return `${humanBytes(job.download_speed)}/s`;
}

export function formatETA(job: JobView): string {
  if (job.status !== 'downloading' || !job.eta_seconds || job.eta_seconds <= 0) {
    return '-';
  }
  return humanDuration(job.eta_seconds);
}

export function shortURL(url: string): string {
  if (!url) return '';
  if (url.length <= 64) return url;
  return `${url.slice(0, 61)}...`;
}
