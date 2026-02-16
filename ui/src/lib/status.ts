import { detectSite } from './job-utils';
import type { JobView } from './types';

type SiteURL = Pick<JobView, 'site' | 'url'>;
type StatusSiteURL = Pick<JobView, 'status' | 'site' | 'url'>;

export function isWebshareJob(job: SiteURL): boolean {
  const site = (job.site || '').trim().toLowerCase();
  if (site === 'webshare') {
    return true;
  }
  return detectSite(job.url || '') === 'webshare';
}

export function displayStatus(job: StatusSiteURL): string {
  if (job.status === 'paused' && isWebshareJob(job)) {
    return 'stopped';
  }
  return job.status;
}

export function displayStatusFilter(status: string): string {
  if (!status) return 'all statuses';
  if (status === 'paused') return 'paused/stopped';
  return status;
}
