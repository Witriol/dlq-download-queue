# DLQ (Download Queue)

Minimal headless download-queue daemon + CLI inspired by JDownloader, designed for Docker and terminal use.

## Quick start (Docker)

```
docker build -t dlq:local .

docker run -d --name dlq \
  -v /mnt/user/downloads:/data \
  -v /mnt/user/appdata/dlq:/state \
  -e DLQ_CONCURRENCY=2 \
  -p 8080:8080 \
  dlq:local
```

Add a job:

```
docker exec -it dlq dlq add <url> --out /data/downloads
```

Check status:

```
docker exec -it dlq dlq status
```

## CLI

- `dlq add <url> --out /data/downloads [--name optional] [--site mega|webshare]`
- `dlq status` (summary + table)
- `dlq status --watch [--interval 1] [--status queued|resolving|downloading|paused|completed|failed|deleted]`
- `dlq files` (shows all jobs in DB, including soft-deleted)
- `dlq logs <job_id> [--tail 50]`
- `dlq retry <job_id>`
- `dlq pause <job_id>`
- `dlq resume <job_id>`
- `dlq remove <job_id>` (soft delete)
- `dlq clear` (hard delete + reset IDs)
- `dlq help`

## How it works

- `dlqd` daemon stores jobs in SQLite, resolves URLs, and starts downloads in aria2 via JSON-RPC.
- Resolvers default to anonymous mode and surface blocking reasons as error codes:
  `login_required`, `quota_exceeded`, `captcha_needed`, `temporarily_unavailable`.
- Queue is persistent across restarts (`/state/dlq.db`).

## Environment variables

- `DLQ_STATE_DIR` (default `/state`)
- `DLQ_DB` (default `/state/dlq.db`)
- `DLQ_HTTP_ADDR` (default `0.0.0.0:8080`)
- `DLQ_CONCURRENCY` (default `2`)
- `ARIA2_RPC` (default `http://127.0.0.1:6800/jsonrpc`)
- `ARIA2_SECRET` (optional; recommended)
- `ARIA2_DISABLE=1` (disable built-in aria2c process in the container)
- `ARIA2_EXTRA_OPTS` (optional extra aria2c flags)
- `ARIA2_SUMMARY_INTERVAL` (default `0`, set >0 to enable aria2 summary output)
- `ARIA2_CONSOLE_LOG_LEVEL` (default `warn`)

## Notes

- Webshare resolver uses the public API in anonymous mode when possible.
- MEGA resolver is a stub; plug in MEGAcmd or a SDK-based resolver later.
- Credentials should be provided via env vars or secrets; never log them.

## Unraid

Template: `templates/unraid-dlq.xml`

## Deploy to Unraid

```
scripts/deploy-unraid.sh
```

Environment overrides (optional):

- `SSH_HOST` (default `HOMENAS`)
- `VERSION_FILE` (default `VERSION`)
- `IMAGE_REPO` (default `dlq`)
- `IMAGE_TAG` (default value from `VERSION`)
- `IMAGE_NAME` (override full image name, e.g. `dlq:0.1.0`)
- `LATEST_TAG` (default `latest`)
- `PLATFORM` (default `linux/amd64`)
- `CONTAINER_NAME` (default `dlq`)
- `TEMPLATE_TPL` (default `templates/unraid-dlq.tpl.xml`)
- `REMOTE_TEMPLATE` (default `/boot/config/plugins/dockerMan/templates-user/my-downloader-queue.xml`)
- `REMOTE_CONTAINER_TEMPLATE` (default `/boot/config/plugins/dockerMan/templates-user/my-dlq.xml`)
- `TV_SHOWS_PATH` (default `/mnt/user/tvshows`)
- `MOVIES_PATH` (default `/mnt/user/movies`)
- `STATE_PATH` (default `/mnt/user/appdata/dlq`)
- `HTTP_ADDR` (default `127.0.0.1:8080`)
- `DLQ_CONCURRENCY` (default `2`)
- `PUID` (default `99`)
- `PGID` (default `100`)
- `TZ` (default `UTC`)

## Version

```
dlq --version
```

## Remote CLI shortcut

Add this to your `~/.zshrc` (or `~/.bashrc`) to run the CLI on Unraid via SSH:

```
dlq() { ssh -t HOMENAS "docker exec -it dlq dlq $(printf '%q ' "$@")"; }
```

Then:

```
dlq status
dlq add "https://example.com/file?x=1&y=2" --out /data/movies
```
