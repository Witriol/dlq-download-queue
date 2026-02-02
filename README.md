# DLQ (Download Queue)

Minimal headless download-queue daemon + CLI inspired by JDownloader, designed for Docker and terminal use.

## Quick start (Docker)

```
docker build -t dlq:local .

docker run -d --name dlq \
  -v /mnt/user/downloads:/data \
  -v /mnt/user/appdata/dlq:/state \
  -e DLQ_CONCURRENCY=2 \
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

To access the HTTP API from the host, add `-p 8080:8080` and set `-e DLQ_HTTP_ADDR=0.0.0.0:8080`.

## CLI

- `dlq add <url> [<url2> ...] --out /data/downloads [--name optional] [--site mega|webshare]`
- `dlq add --file urls.txt --out /data/downloads`
- `dlq add --stdin --out /data/downloads`
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

## UI (SvelteKit)

The UI is optional and lives under `ui/`. It proxies the DLQ HTTP API server-side.

```
cd ui
npm install
DLQ_API=http://127.0.0.1:8080 npm run dev
```

Presets for out_dir are served from `GET /meta` and configured via `DLQ_OUT_DIR_PRESETS`.

## How it works

- `dlqd` daemon stores jobs in SQLite, resolves URLs, and starts downloads in aria2 via JSON-RPC.
- Resolvers default to anonymous mode and surface blocking reasons as error codes:
  `login_required`, `quota_exceeded`, `captcha_needed`, `temporarily_unavailable`.
- Queue is persistent across restarts (`/state/dlq.db`).

## Environment variables

- `DLQ_STATE_DIR` (default `/state`)
- `DLQ_DB` (default `/state/dlq.db`)
- `DLQ_HTTP_ADDR` (default `127.0.0.1:8080`)
- `DLQ_CONCURRENCY` (default `2`)
- `DLQ_OUT_DIR_PRESETS` (default `/data/tvshows,/data/movies`) - comma-separated list for UI presets via `/meta`
- `PUID` / `PGID` (optional; if set, dlqd + aria2 run as that user)
- `ARIA2_RPC` (default `http://127.0.0.1:6800/jsonrpc`)
- `ARIA2_SECRET` (optional; recommended)
- `ARIA2_DISABLE=1` (disable built-in aria2c process in the container)
- `ARIA2_EXTRA_OPTS` (optional extra aria2c flags)
- `ARIA2_SUMMARY_INTERVAL` (default `0`, set >0 to enable aria2 summary output)
- `ARIA2_CONSOLE_LOG_LEVEL` (default `warn`)
- aria2 console readout is disabled by default; set `ARIA2_EXTRA_OPTS=--show-console-readout=true` to re-enable.

## Notes

- Webshare resolver uses the public API in anonymous mode when possible and forces single-connection downloads for reliability.
- MEGA resolver is a stub; plug in MEGAcmd or a SDK-based resolver later.
- Credentials should be provided via env vars or secrets; never log them.
- If aria2 restarts, `dlq resume <id>` will re-queue the job and re-resolve the URL.
- If you set `PUID`/`PGID`, ensure `/data` and `/state` are writable by that user on the host.
- If you see `attempt to write a readonly database`, fix permissions on the host (e.g., `chown -R 99:100 /mnt/user/appdata/dlq`).

## Unraid

Template: `templates/unraid-dlq.xml`

## Deploy to Unraid

```
scripts/deploy-unraid.sh
```

Environment overrides (optional):

- `REMOTE_HOST` (default `HOMENAS`)
- `VERSION_FILE` (default `VERSION`)
- `IMAGE_REPO` (default `dlq`)
- `REMOTE_TAR` (default `/tmp/dlq.tar.gz`)
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

## Testing

```
go test ./internal/queue
```

## Development

```
scripts/run-dev.sh
```

## License

MIT. See `LICENSE`. Third-party notices in `THIRD_PARTY_NOTICES.md`.

## Remote CLI shortcut

Add this to your `~/.zshrc` (or `~/.bashrc`) to run the CLI on Unraid via SSH:

```
dlq() {
  if [ -t 0 ]; then
    ssh -t HOMENAS "docker exec -it dlq dlq $(printf '%q ' "$@")"
  else
    ssh HOMENAS "docker exec -i dlq dlq $(printf '%q ' "$@")"
  fi
}
```

Then:

```
dlq status
dlq add "https://example.com/file?x=1&y=2" --out /data/movies
```
