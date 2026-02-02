#!/bin/sh
set -e

SUMMARY_INTERVAL="${ARIA2_SUMMARY_INTERVAL:-0}"
CONSOLE_LOG_LEVEL="${ARIA2_CONSOLE_LOG_LEVEL:-warn}"
ARIA2_OPTS="--enable-rpc --rpc-listen-all=false --rpc-listen-port=6800 --dir=/data --max-concurrent-downloads=${DLQ_CONCURRENCY:-2} --max-connection-per-server=4 --summary-interval=${SUMMARY_INTERVAL} --console-log-level=${CONSOLE_LOG_LEVEL} --continue=true --check-integrity=true --disable-ipv6=true"

if [ -n "${ARIA2_SECRET}" ]; then
  ARIA2_OPTS="$ARIA2_OPTS --rpc-secret=${ARIA2_SECRET}"
fi

if [ -n "${ARIA2_EXTRA_OPTS}" ]; then
  ARIA2_OPTS="$ARIA2_OPTS ${ARIA2_EXTRA_OPTS}"
fi

if [ "${ARIA2_DISABLE:-0}" != "1" ]; then
  aria2c $ARIA2_OPTS &
fi

exec /usr/local/bin/dlqd
