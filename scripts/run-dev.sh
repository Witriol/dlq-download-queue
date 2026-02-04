#!/usr/bin/env sh
set -e

PUID="${PUID:-$(id -u)}"
PGID="${PGID:-$(id -g)}"
RUN_UI="${RUN_UI:-1}"

cleanup() {
  docker rm -f dlq >/dev/null 2>&1 || true
}

trap cleanup INT TERM EXIT

docker build -t dlq:local .
docker rm -f dlq >/dev/null 2>&1 || true
docker run -d --name dlq \
  -v /tmp/dlq-downloads:/data \
  -v /tmp/dlq-state:/state \
  -e DLQ_CONCURRENCY=2 \
  -e DLQ_HTTP_ADDR=0.0.0.0:8080 \
  -e PUID="${PUID}" \
  -e PGID="${PGID}" \
  -p 8080:8080 \
  dlq:local

if [ "${RUN_UI}" != "1" ]; then
  echo "DLQ API running at http://127.0.0.1:8080"
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
  echo "DLQ API running at http://127.0.0.1:8080"
  echo "Starting UI dev server at http://127.0.0.1:5173"
  (cd ui && DLQ_API=http://127.0.0.1:8080 npm run dev)
else
  echo "UI directory not found. Set RUN_UI=0 to skip."
  exit 1
fi
