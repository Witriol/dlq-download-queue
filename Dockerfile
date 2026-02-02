FROM golang:1.22-bullseye AS build
WORKDIR /src
COPY go.mod ./
COPY . ./
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -mod=mod -ldflags "-X main.version=${VERSION}" -o /out/dlq ./cmd/dlq
RUN CGO_ENABLED=0 go build -mod=mod -ldflags "-X main.version=${VERSION}" -o /out/dlqd ./cmd/dlqd

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends aria2 ca-certificates && update-ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/dlq /usr/local/bin/dlq
COPY --from=build /out/dlqd /usr/local/bin/dlqd
COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
VOLUME ["/data", "/state"]
ENV DLQ_STATE_DIR=/state
ENV DLQ_HTTP_ADDR=0.0.0.0:8080
ENV ARIA2_RPC=http://127.0.0.1:6800/jsonrpc
EXPOSE 8080
ENTRYPOINT ["/entrypoint.sh"]
