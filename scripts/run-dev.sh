#!/usr/bin/env sh
set -e

PUID="${PUID:-$(id -u)}"
PGID="${PGID:-$(id -g)}"

docker build -t dlq:local .
docker rm -f dlq
docker run -d --name dlq \
  -v /tmp/dlq-downloads:/data \
  -v /tmp/dlq-state:/state \
  -e DLQ_CONCURRENCY=2 \
  -e DLQ_HTTP_ADDR=0.0.0.0:8080 \
  -e PUID="${PUID}" \
  -e PGID="${PGID}" \
  -p 8080:8080 \
  dlq:local
