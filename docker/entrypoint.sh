#!/bin/sh
set -e

SUMMARY_INTERVAL="${ARIA2_SUMMARY_INTERVAL:-0}"
CONSOLE_LOG_LEVEL="${ARIA2_CONSOLE_LOG_LEVEL:-warn}"
STATE_DIR="${DLQ_STATE_DIR:-/state}"
SETTINGS_FILE="${STATE_DIR}/settings.json"
DEFAULT_CONCURRENCY=2
DEFAULT_MAX_ATTEMPTS=5
ARIA2_MAX_CONN_PER_SERVER="${ARIA2_MAX_CONNECTION_PER_SERVER:-4}"
ARIA2_SHOW_CONSOLE_READOUT="${ARIA2_SHOW_CONSOLE_READOUT:-false}"

first_data_path() {
  for entry in $(env | awk -F= '/^DATA_/{print}'); do
    mount="${entry#*=}"
    [ -z "${mount}" ] && continue
    case "${mount}" in
      *:*)
        last="${mount##*:}"
        if [ -n "${last}" ] && printf '%s' "${last}" | grep -q '/'; then
          printf '%s' "${last}"
          return 0
        fi
        tmp="${mount%:*}"
        printf '%s' "${tmp##*:}"
        return 0
        ;;
      *)
        printf '%s' "${mount}"
        return 0
        ;;
    esac
  done
  return 1
}

ARIA2_DIR="${ARIA2_DIR:-$(first_data_path || true)}"
ARIA2_DIR="${ARIA2_DIR:-/data}"

RPC_PORT="${ARIA2_RPC_LISTEN_PORT:-}"
if [ -z "${RPC_PORT}" ] && [ -n "${ARIA2_RPC:-}" ]; then
  RPC_PORT="$(printf '%s' "${ARIA2_RPC}" | sed -n 's#.*://[^:/]*:\\([0-9][0-9]*\\)/.*#\\1#p' | head -n 1)"
fi
RPC_PORT="${RPC_PORT:-6800}"

if [ ! -f "${SETTINGS_FILE}" ]; then
  mkdir -p "$(dirname "${SETTINGS_FILE}")"
  printf '{\n  "concurrency": %s,\n  "max_attempts": %s\n}\n' "${DEFAULT_CONCURRENCY}" "${DEFAULT_MAX_ATTEMPTS}" > "${SETTINGS_FILE}"
fi

CONCURRENCY="$(sed -n 's/.*"concurrency"[[:space:]]*:[[:space:]]*\\([0-9][0-9]*\\).*/\\1/p' "${SETTINGS_FILE}" | head -n 1)"
if [ -z "${CONCURRENCY}" ]; then
  CONCURRENCY="${DEFAULT_CONCURRENCY}"
fi

ARIA2_OPTS="--enable-rpc --rpc-listen-all=false --rpc-listen-port=${RPC_PORT} --dir=${ARIA2_DIR} --max-concurrent-downloads=${CONCURRENCY} --max-connection-per-server=${ARIA2_MAX_CONN_PER_SERVER} --summary-interval=${SUMMARY_INTERVAL} --console-log-level=${CONSOLE_LOG_LEVEL} --show-console-readout=${ARIA2_SHOW_CONSOLE_READOUT} --continue=true --check-integrity=true --disable-ipv6=true"

RUN_AS=""
if [ -n "${PUID:-}" ] || [ -n "${PGID:-}" ]; then
  PUID="${PUID:-1000}"
  PGID="${PGID:-1000}"
  if ! getent group "${PGID}" >/dev/null 2>&1; then
    groupadd -g "${PGID}" dlq
  fi
  if ! getent passwd "${PUID}" >/dev/null 2>&1; then
    useradd -u "${PUID}" -g "${PGID}" -s /bin/sh -M dlq
  fi
  chown -R "${PUID}:${PGID}" /state || true
  chown "${PUID}:${PGID}" /data || true
  RUN_AS="$(getent passwd "${PUID}" | cut -d: -f1)"
fi

if [ -n "${ARIA2_SECRET}" ]; then
  ARIA2_OPTS="$ARIA2_OPTS --rpc-secret=${ARIA2_SECRET}"
fi

if [ -n "${ARIA2_EXTRA_OPTS}" ]; then
  ARIA2_OPTS="$ARIA2_OPTS ${ARIA2_EXTRA_OPTS}"
fi

if [ "${ARIA2_DISABLE:-0}" != "1" ]; then
  if [ -n "${RUN_AS}" ]; then
    gosu "${RUN_AS}" aria2c $ARIA2_OPTS &
  else
    aria2c $ARIA2_OPTS &
  fi
fi

if [ -n "${RUN_AS}" ]; then
  exec gosu "${RUN_AS}" /usr/local/bin/dlqd
fi
exec /usr/local/bin/dlqd
