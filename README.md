# DLQ (Download Queue)

Minimal headless download-queue daemon + CLI inspired by JDownloader, designed for Docker and terminal use.

## Quick start (Docker)

```
docker build -t dlq:local .

docker run -d --name dlq \
  -v /mnt/user/downloads:/data \
  -v /mnt/user/appdata/dlq:/state \
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

To access the HTTP API from the host, add `-p 8099:8099` and set `-e DLQ_HTTP_ADDR=0.0.0.0:8099` (or set `DLQ_HTTP_HOST`/`DLQ_HTTP_PORT` in `.env` when using compose/dev scripts).

## CLI

- `dlq add <url> [<url2> ...] --out /data/downloads [--name optional] [--site mega|webshare|http|https]`
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
- `dlq settings` (show current settings)
- `dlq settings --concurrency <1-10>` (update concurrency)
- `dlq help`

## UI (SvelteKit)

The UI is optional and lives under `ui/`. It proxies the DLQ HTTP API server-side.

```
cd ui
npm install
DLQ_API=http://127.0.0.1:8099 npm run dev
```

Presets for out_dir are served from `GET /meta` and derived from `DATA_*` volume mappings (container paths).

## How it works

- `dlqd` daemon stores jobs in SQLite, resolves URLs, and starts downloads in aria2 via JSON-RPC.
- Resolvers default to anonymous mode and surface blocking reasons as error codes:
  `login_required`, `quota_exceeded`, `captcha_needed`, `temporarily_unavailable`.
- `--site` forces a resolver; unknown values return `unknown_site`.
- Queue is persistent across restarts (`/state/dlq.db`).

## Environment variables

- `DLQ_STATE_DIR` (default `/state`)
- `DLQ_DB` (default `/state/dlq.db`)
- `DLQ_HTTP_ADDR` (default `0.0.0.0:8099`)
- `DLQ_HTTP_HOST` / `DLQ_HTTP_PORT` (used by compose/dev scripts to derive `DLQ_HTTP_ADDR`)
- `PUID` / `PGID` (optional; if set, dlqd + aria2 run as that user)
- `ARIA2_RPC` (default `http://127.0.0.1:6800/jsonrpc`)
- `ARIA2_RPC_LISTEN_PORT` (default parsed from `ARIA2_RPC`, fallback `6800`)
- `ARIA2_SECRET` (optional; recommended)
- `ARIA2_DISABLE=1` (disable built-in aria2c process in the container)
- `ARIA2_DIR` (default first `DATA_*` container path, fallback `/data`)
- `ARIA2_EXTRA_OPTS` (optional extra aria2c flags)
- `ARIA2_MAX_CONNECTION_PER_SERVER` (default `4`)
- `ARIA2_SUMMARY_INTERVAL` (default `0`, set >0 to enable aria2 summary output)
- `ARIA2_CONSOLE_LOG_LEVEL` (default `warn`)
- `ARIA2_SHOW_CONSOLE_READOUT` (default `false`)

Concurrency is stored in `settings.json` under `DLQ_STATE_DIR` and can be updated via `dlq settings --concurrency` or the UI. The file is created with the default value on first start.
UI out_dir presets are derived from `DATA_*` env values (container paths); make sure they are passed into the container.
All job `out_dir` values must live under one of the `DATA_*` container paths.

## Notes

- Webshare resolver uses the public API in anonymous mode when possible and forces single-connection downloads for reliability.
- MEGA resolver is a stub; plug in MEGAcmd or a SDK-based resolver later.
- Credentials should be provided via env vars or secrets; never log them.
- If aria2 restarts, `dlq resume <id>` will re-queue the job and re-resolve the URL.
- If you set `PUID`/`PGID`, ensure `/data` and `/state` are writable by that user on the host.
- If you see `attempt to write a readonly database`, fix permissions on the host (e.g., `chown -R 99:100 /mnt/user/appdata/dlq`).

## Docker Compose

```bash
# Copy .env.example to .env and edit it
cp .env.example .env

# Start the service
docker-compose up -d
```

## Deploy to Unraid

```bash
# Copy .env.example to .env and edit it
cp .env.example .env

# Build and transfer image only
scripts/deploy-unraid.sh

# Build, transfer, and deploy container
scripts/deploy-unraid.sh --deploy
```

The `.env` file is your single source of truth for deployment configuration:
- `REMOTE_HOST` - SSH alias for your Unraid server
- `DATA_*` - Volume mappings (e.g., `DATA_TVSHOWS=/mnt/user/tvshows:/data/tvshows`)
- `STATE_MOUNT` - State/config volume mapping
- `PUID`/`PGID`/`TZ` - Runtime user and timezone settings

The deploy script automatically discovers all `DATA_*` variables and passes them through so presets can be derived in the app.

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
cp .env.example .env.dev
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
