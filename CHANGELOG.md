# Changelog

All notable changes to this project will be documented in this file.

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
