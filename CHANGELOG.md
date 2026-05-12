# Changelog

All notable changes to this project will be documented in this file.

## 0.2.7 - 2026-05-12

- Switched archive extraction to a single `7z` command path using Debian `7zip-rar` from `non-free` for RAR/RAR5 support.
- Removed the `unar` fallback and added two-pass `7z` extraction for tar-compressed archives so `.tar.gz`, `.tar.bz2`, and `.tar.xz` contents are unpacked directly.
- Updated the Docker image package sources to use bookworm backports/non-free for the RAR-capable 7-Zip runtime.

## 0.2.6 - 2026-05-11

- Added Add Jobs folder favorites: Browse now stars folders into a persisted favorites row so users can pick common output folders directly from Add Jobs.
- Improved MEGA link handling for duplicate-pasted and padded links, plus clearer token/key error messaging.
- Detected RAR5 header-encrypted archives earlier, surfaced decrypt failures in grouped UI rows, and added coverage for the failure path.
- Corrected RAR5 encryption detection to verify the RAR5 signature first and check the encryption block type.
- Updated Docker/Unraid build flow to use the native arm64 build stage.

## 0.2.5 - 2026-03-05

- Added multipart archive grouping across API/UI, including job group metadata and group actions (`retry decrypt`, `remove`) via dedicated group endpoints.
- Added strict group-route parsing and typed group errors with explicit HTTP mapping (`400` invalid group id, `409` blocked/no decrypt failures).
- Improved multipart processing: latest-per-part selection (ignores stale jobs), blocking sibling waits before extraction, and support for both `.partNN.rar` and legacy `.rar/.r00`.
- Added URL basename fallback for archive grouping/path inference when `name`/`filename` is not resolved yet.
- `GET /jobs/{id}` now returns archive grouping metadata aligned with `GET /jobs` list responses.
- Added a UI setting for default `max attempts`; manual retry now resets attempts to a fresh retry budget.
- Updated grouped UI behavior: group controls stay visible in filtered views, per-file retry remains, and grouped children hide per-file remove.
- Moved transient queue fetch failures to a header red badge beside title/version and kept `status=509` mapped as `quota_exceeded` with delayed retry.
- Expanded tests for multipart wait/state handling, group API/status behavior, filtered grouping, and URL/r-style grouping.

## 0.2.4 - 2026-02-26

- Added multipart-aware postprocess retry flow (wait for sibling parts and decrypt from first archive volume).
- Classified transient download `status=509` failures as `quota_exceeded` with delayed retry.
- Refined Web UI for desktop/mobile: clearer dialogs, inline error rows, and always-visible desktop filters.

## 0.2.3 - 2026-02-17

- Added MEGA post-download payload decryption in the runner (`mega decrypt started/completed/failed` events).
- Added integrity verification for MEGA payload decryption using MEGA content MAC checks.
- Refactored post-download pipeline to run MEGA payload decryption before optional archive extraction.
- Added dedicated `mega_decrypt_failed` job error code while preserving existing archive decrypt failure handling.
- Added RAR signature pre-check so `.rar` files with non-RAR content are skipped as non-archives instead of failing decrypt.
- Redacted URL fragments in add logs/events so MEGA keys are not written to logs.

## 0.2.2 - 2026-02-16

- Reworked Webshare controls to `stop + retry` semantics; Webshare resume now requeues with a fresh link instead of unpausing old transfer state.
- Added Webshare detection fallback from URL when `site` is missing, so CLI/API jobs without explicit `--site` still use Webshare-specific behavior.
- Hardened Webshare resolver output for aria2 (`continue=false`, `always-resume=false`, overwrite/no-rename) and added browser-like headers (`Referer`, `User-Agent`).
- Added safer fresh-start preparation that removes stale `.aria2` state only when no other active job targets the same output path.
- Updated status/action wording for Webshare paused jobs to user-facing `stopped` across UI, CLI, and daemon action logs.
- Refined UI table/dialog ergonomics: wider layout, sticky actions column, icon-based actions, larger logs dialog, and consistent `X` close buttons.

## 0.2.1 - 2026-02-15

- Added MEGA resolver support for public file links (`mega.nz/file/...`), including filename decryption and API error mapping.
- Added optional automatic archive decrypt/extract after download completion via `7zz`.
- Added archive password support across API, CLI (`dlq add --archive-password`), and Web UI add modal (one password per add batch).
- Removed global archive password fallback (`DLQ_ARCHIVE_PASSWORD`); archive decrypt now uses request/batch password only.
- Moved archive auto-decrypt toggle from environment to runtime settings (`settings.json`: `auto_decrypt`) editable via CLI/UI.
- Added DB support for `archive_password` with masked password logging in job events, plus cleanup after decrypt processing.
- Moved archive decrypt/extract work to a dedicated worker path so download polling is not blocked by extraction.
- Added explicit post-download states `decrypting` and `decrypt_failed`, with download/decrypt lifecycle events visible in CLI/UI logs.
- Mirrored job lifecycle events to daemon stdout logs (`job_event ...`) so terminal/docker logs show download/decrypt progress and failures.
- Auto-decrypt now also attempts extraction for archive files without `archive_password` (works for unencrypted archives).
- Added RAR extraction fallback to `unar` when `7zz` cannot open/supported-method errors.
- Removed `DLQ_ARCHIVE_TOOL`; extractor selection is now automatic (`7zz` first, `unar` fallback).
- Updated Docker image to include `7zip` (`7zz`) and `unar`.

## 0.2.0 - 2026-02-02

- New SvelteKit UI app under `ui/` with queue dashboard, batch add, and log viewer.
- UI proxies DLQ API and supports auto-detected site per URL.
- Added `/meta` endpoint for UI out_dir presets derived from volume mappings.

## 0.1.0 - 2026-02-02

- Initial Dockerized headless download queue (dlqd + dlq).
- SQLite-backed job queue with retries, resume, pause, and soft delete.
- Aria2-backed downloader with progress, speed, and ETA reporting.
- Webshare resolver (anonymous mode) and HTTP passthrough resolver, with safer single-connection defaults.
- Unraid template + deploy script for amd64 servers.
- CLI supports multi-URL add, file/stdin input, watch status, and job logs.
- Non-root runtime (PUID/PGID) and improved batch URL handling.
