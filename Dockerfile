FROM --platform=$BUILDPLATFORM golang:1.22-bullseye AS build
WORKDIR /src
COPY go.mod ./
COPY . ./
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=mod -ldflags "-X main.version=${VERSION}" -o /out/dlq ./cmd/dlq
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=mod -ldflags "-X main.version=${VERSION}" -o /out/dlqd ./cmd/dlqd

FROM debian:bookworm-slim
RUN printf '%s\n' \
      'deb http://deb.debian.org/debian bookworm main contrib non-free non-free-firmware' \
      'deb http://deb.debian.org/debian-security bookworm-security main contrib non-free non-free-firmware' \
      'deb http://deb.debian.org/debian bookworm-updates main contrib non-free non-free-firmware' \
      'deb http://deb.debian.org/debian bookworm-backports main contrib non-free non-free-firmware' \
      > /etc/apt/sources.list \
    && rm -f /etc/apt/sources.list.d/debian.sources \
    && apt-get update \
    && apt-get install -y --no-install-recommends -t bookworm-backports 7zip 7zip-rar aria2 ca-certificates gosu passwd \
    && update-ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/dlq /usr/local/bin/dlq
COPY --from=build /out/dlqd /usr/local/bin/dlqd
COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
VOLUME ["/data", "/state"]
ENV DLQ_STATE_DIR=/state
ENV DLQ_HTTP_PORT=8099
ENV ARIA2_RPC=http://127.0.0.1:6800/jsonrpc
ENTRYPOINT ["/entrypoint.sh"]
