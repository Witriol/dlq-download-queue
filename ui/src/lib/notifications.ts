import type { JobStatus, JobView } from './types';

const storageKey = 'dlq.browserNotifications';
const failedStatuses: JobStatus[] = ['failed', 'decrypt_failed'];

let previous: Map<number, JobStatus> | null = null;

/** Notification needs a secure context: https or localhost, not a plain-http LAN IP. */
export function notificationsUnavailableReason(): string {
  if (typeof window === 'undefined' || !('Notification' in window)) {
    return 'This browser does not support notifications.';
  }
  if (!window.isSecureContext) {
    return 'Needs https or localhost; this page is served over plain http.';
  }
  return '';
}

export function notificationsEnabled(): boolean {
  if (notificationsUnavailableReason()) {
    return false;
  }
  try {
    return localStorage.getItem(storageKey) === '1' && Notification.permission === 'granted';
  } catch {
    return false;
  }
}

/** Must be called from a click handler so the permission prompt is allowed. */
export async function setNotificationsEnabled(enabled: boolean): Promise<boolean> {
  let on = false;
  if (enabled && !notificationsUnavailableReason()) {
    on = (await Notification.requestPermission()) === 'granted';
  }
  try {
    localStorage.setItem(storageKey, on ? '1' : '0');
  } catch {
    // Preference is per-browser only; losing it is harmless.
  }
  return on;
}

/** Compares against the previous poll and fires one notification for all jobs that just finished. */
export function notifyFinishedJobs(jobs: JobView[]) {
  const last = previous;
  previous = new Map(jobs.map((job) => [job.id, job.status]));
  // First poll: jobs already final must not notify.
  if (!last || !notificationsEnabled()) {
    return;
  }

  let completed = 0;
  let failed = 0;
  for (const job of jobs) {
    const was = last.get(job.id);
    if (!was || was === job.status) {
      continue;
    }
    if (job.status === 'completed' && !failedStatuses.includes(was)) {
      completed++;
    } else if (failedStatuses.includes(job.status)) {
      failed++;
    }
  }
  if (completed + failed === 0) {
    return;
  }

  const parts = [];
  if (completed > 0) {
    parts.push(`${completed} completed`);
  }
  if (failed > 0) {
    parts.push(`${failed} failed`);
  }
  try {
    new Notification('DLQ', { body: parts.join(', '), tag: 'dlq-jobs' });
  } catch {
    // Some mobile browsers only allow notifications via a service worker.
  }
}
