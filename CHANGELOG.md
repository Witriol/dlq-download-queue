# Changelog

All notable changes to this project will be documented in this file.

## 0.1.0 - 2026-02-02

- Initial Dockerized headless download queue (dlqd + dlq).
- SQLite-backed job queue with retries, resume, pause, and soft delete.
- Aria2-backed downloader with progress, speed, and ETA reporting.
- Webshare resolver (anonymous mode) and HTTP passthrough resolver, with safer single-connection defaults.
- Unraid template + deploy script for amd64 servers.
- CLI supports multi-URL add, file/stdin input, watch status, and job logs.
- Non-root runtime (PUID/PGID) and improved batch URL handling.
