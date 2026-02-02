export type JobStatus =
  | 'queued'
  | 'resolving'
  | 'downloading'
  | 'paused'
  | 'completed'
  | 'failed'
  | 'deleted';

export type JobView = {
  id: number;
  url: string;
  site: string;
  out_dir: string;
  name: string;
  status: JobStatus;
  filename?: string;
  size_bytes?: number;
  bytes_done: number;
  download_speed: number;
  eta_seconds: number;
  error?: string;
  error_code?: string;
  created_at: string;
  updated_at: string;
};

export type BatchResult = {
  url: string;
  ok: boolean;
  id?: number;
  error?: string;
};

export type Meta = {
  out_dir_presets: string[];
};
