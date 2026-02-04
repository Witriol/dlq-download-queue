#!/usr/bin/env bash
set -e

PUID="${PUID:-$(id -u)}"
PGID="${PGID:-$(id -g)}"
RUN_UI="${RUN_UI:-1}"

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
if [ ! -f "${repo_root}/.env.dev" ]; then
  echo "ERROR: .env.dev not found. Copy .env.example to .env.dev and edit it." >&2
  exit 1
fi
set -a
# shellcheck source=/dev/null
. "${repo_root}/.env.dev"
set +a

DATA_VOLUMES=()
DATA_ENVS=()
DATA_KEYS=()
while IFS= read -r key; do
  [ -z "${key}" ] && continue
  DATA_KEYS+=("${key}")
done < <(awk -F= 'match($0,/^[[:space:]]*(export[[:space:]]+)?DATA_[A-Za-z0-9_]*=/){sub(/^[[:space:]]*export[[:space:]]+/,""); print $1}' "${repo_root}/.env.dev")

for var in "${DATA_KEYS[@]}"; do
  mount="${!var}"
  [ -z "${mount}" ] && continue
  DATA_VOLUMES+=("-v" "${mount}")
  DATA_ENVS+=("-e" "${var}=${mount}")
done

if [ "${#DATA_VOLUMES[@]}" -eq 0 ]; then
  DATA_DEFAULT="/tmp/dlq-downloads:/data"
  DATA_VOLUMES+=("-v" "${DATA_DEFAULT}")
  DATA_ENVS+=("-e" "DATA_DOWNLOADS=${DATA_DEFAULT}")
fi

STATE_MOUNT="${STATE_MOUNT:-/tmp/dlq-state:/state}"
# Keep in sync with scripts/deploy-unraid.sh
DLQ_HTTP_HOST="${DLQ_HTTP_HOST:-0.0.0.0}"
DLQ_HTTP_PORT="${DLQ_HTTP_PORT:-}"
if [[ -n "${DLQ_HTTP_ADDR:-}" ]]; then
  case "${DLQ_HTTP_ADDR}" in
    *:*) ;;
    *)
      echo "ERROR: DLQ_HTTP_ADDR must include a port (e.g., 0.0.0.0:8099)" >&2
      exit 1
      ;;
  esac
  DLQ_HTTP_ADDR_PORT="${DLQ_HTTP_ADDR##*:}"
  if [[ -n "${DLQ_HTTP_PORT}" && "${DLQ_HTTP_PORT}" != "${DLQ_HTTP_ADDR_PORT}" ]]; then
    echo "ERROR: DLQ_HTTP_PORT (${DLQ_HTTP_PORT}) must match DLQ_HTTP_ADDR (${DLQ_HTTP_ADDR})" >&2
    exit 1
  fi
  DLQ_HTTP_PORT="${DLQ_HTTP_ADDR_PORT}"
else
  DLQ_HTTP_PORT="${DLQ_HTTP_PORT:-8099}"
  DLQ_HTTP_ADDR="${DLQ_HTTP_HOST}:${DLQ_HTTP_PORT}"
fi

LOG_DOCKER_PID=""
LOG_UI_PID=""

cleanup() {
  if [ -n "${LOG_DOCKER_PID}" ]; then
    kill "${LOG_DOCKER_PID}" >/dev/null 2>&1 || true
  fi
  if [ -n "${LOG_UI_PID}" ]; then
    kill "${LOG_UI_PID}" >/dev/null 2>&1 || true
  fi
  docker rm -f dlq >/dev/null 2>&1 || true
}

trap cleanup INT TERM EXIT

docker build -t dlq:local .
docker rm -f dlq >/dev/null 2>&1 || true
docker run -d --name dlq \
  "${DATA_VOLUMES[@]}" \
  -v "${STATE_MOUNT}" \
  "${DATA_ENVS[@]}" \
  -e DLQ_HTTP_ADDR="${DLQ_HTTP_ADDR}" \
  -e DLQ_HTTP_PORT="${DLQ_HTTP_PORT}" \
  -e PUID="${PUID}" \
  -e PGID="${PGID}" \
  -p "${DLQ_HTTP_PORT}:${DLQ_HTTP_PORT}" \
  dlq:local

if [ "${RUN_UI}" != "1" ]; then
  echo "DLQ API running at http://127.0.0.1:${DLQ_HTTP_PORT}"
  exit 0
fi

if ! command -v npm >/dev/null 2>&1; then
  echo "npm is required to run the UI. Set RUN_UI=0 to skip."
  exit 1
fi

if [ -d "ui" ]; then
  if [ ! -d "ui/node_modules" ]; then
    (cd ui && npm install)
  fi
  echo "DLQ API running at http://127.0.0.1:${DLQ_HTTP_PORT}"
  echo "Starting UI dev server at http://127.0.0.1:5173"
  docker logs -f dlq 2>&1 | awk '{print "[dlq] " $0; fflush();}' &
  LOG_DOCKER_PID=$!
  (cd ui && DLQ_API=http://127.0.0.1:${DLQ_HTTP_PORT} npm run dev 2>&1 | awk '{print "[ui] " $0; fflush();}') &
  LOG_UI_PID=$!
  wait "${LOG_UI_PID}"
else
  echo "UI directory not found. Set RUN_UI=0 to skip."
  exit 1
fi
