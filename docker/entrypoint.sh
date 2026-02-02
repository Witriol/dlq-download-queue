#!/bin/sh
set -e

SUMMARY_INTERVAL="${ARIA2_SUMMARY_INTERVAL:-0}"
CONSOLE_LOG_LEVEL="${ARIA2_CONSOLE_LOG_LEVEL:-warn}"
ARIA2_OPTS="--enable-rpc --rpc-listen-all=false --rpc-listen-port=6800 --dir=/data --max-concurrent-downloads=${DLQ_CONCURRENCY:-2} --max-connection-per-server=4 --summary-interval=${SUMMARY_INTERVAL} --console-log-level=${CONSOLE_LOG_LEVEL} --show-console-readout=false --continue=true --check-integrity=true --disable-ipv6=true"

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
