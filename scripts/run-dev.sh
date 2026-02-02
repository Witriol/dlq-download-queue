docker build -t dlq:local .
docker rm -f dlq
docker run -d --name dlq \
  -v /tmp/dlq-downloads:/data \
  -v /tmp/dlq-state:/state \
  -e DLQ_CONCURRENCY=2 \
  -p 8080:8080 \
  dlq:local