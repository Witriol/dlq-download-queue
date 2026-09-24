export type JobStatus =
  | 'queued'
  | 'resolving'
  | 'downloading'
  | 'paused'
  | 'decrypting'
  | 'decrypt_failed'
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
  next_retry_at?: string;
  archive_group_id?: string;
  archive_group_label?: string;
  archive_part_number?: number;
  archive_is_multipart?: boolean;
  created_at: string;
  updated_at: string;
  status_changed_at?: string;
};

export type BatchResult = {
  url: string;
  ok: boolean;
  id?: number;
  error?: string;
};

export type Meta = {
  out_dir_presets: string[];
  version?: string;
};

/** The long-lived series watcher returned by the optional series API. */
export type SeriesWatch = {
  id: number | string;
  enabled?: boolean;
  tvmaze_id?: number;
  display_name: string;
  search_title?: string;
  reference_webshare_ident?: string;
  reference_filename?: string;
  out_dir: string;
  series_folder?: string;
  organize_by_season?: boolean;
  quality_profile?: Record<string, unknown>;
  quality_profile_json?: string | Record<string, unknown>;
  fallback_policy?: 'strict' | 'balanced' | 'manual' | string;
  preferred_wait_seconds?: number;
  next_check_at?: string;
  last_checked_at?: string;
  last_error?: string;
  status?: string;
  attention_count?: number;
  next_episode?: SeriesEpisode | null;
  last_episode?: SeriesEpisode | null;
  next_episode_at?: string;
  created_at?: string;
  updated_at?: string;
  /** TVmaze show status as returned: Running, Ended, To Be Determined, In Development. Empty when not fetched yet. */
  show_status?: string;
};

export type SeriesEpisode = {
  id?: number | string;
  tvmaze_episode_id?: number;
  season?: number;
  episode?: number;
  episode_name?: string;
  name?: string;
  air_timestamp?: string;
  state?: string;
  chosen_filename?: string;
  search_attempts?: number;
  job_id?: number;
  /** Episode row updated_at; for completed episodes this is the download-finished time. */
  updated_at?: string;
};

/** An episode waiting for a manual fallback decision and its persisted alternatives. */
export type SeriesAttentionEpisode = SeriesEpisode & {
  candidates: SeriesPreviewCandidate[];
};

export type SeriesProfileField = {
  value?: unknown;
  normalized?: unknown;
  confidence?: number;
  mode?: 'required' | 'preferred' | 'ignored' | string;
  token?: string;
};

export type SeriesPreviewCandidate = {
  ident?: string;
  webshare_ident?: string;
  filename?: string;
  name?: string;
  size_bytes?: number;
  score?: number;
  accepted?: boolean;
  reasons?: string[];
  reject_reasons?: string[];
  [key: string]: unknown;
};

export type SeriesPreview = {
  profile?: Record<string, unknown>;
  quality_profile?: Record<string, unknown>;
  reference_filename?: string;
  search_title?: string;
  /** The reference episode (season/episode parsed from the reference filename). */
  episode?: SeriesEpisode | null;
  /** First episode that will be tracked per start_mode; only set when tvmaze_id > 0. */
  next_episode?: SeriesEpisode | null;
  candidates?: SeriesPreviewCandidate[];
  total_candidates?: number;
  error?: string;
  [key: string]: unknown;
};
