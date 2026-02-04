#!/usr/bin/env bash
set -euo pipefail

# Build and deploy DLQ to Unraid
# Usage: scripts/deploy-unraid.sh [--deploy]
# Config: Copy .env.example to .env and edit it

DEPLOY=false
[[ "${1:-}" == "--deploy" ]] && DEPLOY=true

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"

# Source .env
if [[ ! -f "${repo_root}/.env" ]]; then
  echo "ERROR: .env not found. Copy .env.example to .env and edit it." >&2
  exit 1
fi
set -a
# shellcheck source=/dev/null
. "${repo_root}/.env"
set +a

# Read version
VERSION="$(cat "${repo_root}/VERSION")"
[[ -z "${VERSION}" ]] && { echo "ERROR: VERSION is empty" >&2; exit 1; }

# Constants
IMAGE_REPO="dlq"
CONTAINER_NAME="dlq"
REMOTE_TAR="/tmp/dlq.tar.gz"
IMAGE_TAG_VERSIONED="${IMAGE_REPO}:${VERSION}"
IMAGE_TAG_LATEST="${IMAGE_REPO}:latest"

# Parse DATA_* variables to build volume flags and presets
VOLUME_FLAGS=()
PRESETS=()
HOST_PATHS=()
for var in $(compgen -v | grep '^DATA_'); do
  mount="${!var}"
  [[ -z "${mount}" ]] && continue
  IFS=':' read -r host_path container_path <<< "${mount}"
  VOLUME_FLAGS+=("-v" "${mount}")
  PRESETS+=("${container_path}")
  HOST_PATHS+=("${host_path}")
done
DLQ_OUT_DIR_PRESETS="$(IFS=','; echo "${PRESETS[*]}")"

if [[ ${#VOLUME_FLAGS[@]} -eq 0 ]]; then
  echo "ERROR: No DATA_* variables found in .env" >&2
  exit 1
fi

printf "==> Building %s (version %s)\n" "${IMAGE_TAG_VERSIONED}" "${VERSION}"
docker buildx build --platform linux/amd64 --load \
  --build-arg VERSION="${VERSION}" \
  -t "${IMAGE_TAG_VERSIONED}" \
  -t "${IMAGE_TAG_LATEST}" \
  "${repo_root}"

printf "==> Transferring image to %s\n" "${REMOTE_HOST}"
docker save "${IMAGE_TAG_LATEST}" "${IMAGE_TAG_VERSIONED}" | gzip | \
  ssh "${REMOTE_HOST}" "cat > ${REMOTE_TAR} && gunzip -c ${REMOTE_TAR} | docker load && rm -f ${REMOTE_TAR}"

if [[ "${DEPLOY}" == "true" ]]; then
  printf "==> Deploying container on %s\n" "${REMOTE_HOST}"

  # Build volume args string for docker run
  VOLUME_ARGS=""
  for vflag in "${VOLUME_FLAGS[@]}"; do
    VOLUME_ARGS="${VOLUME_ARGS} ${vflag}"
  done

  # Build mkdir args
  MKDIR_ARGS=""
  for hpath in "${HOST_PATHS[@]}"; do
    MKDIR_ARGS="${MKDIR_ARGS} '${hpath}'"
  done

  ssh "${REMOTE_HOST}" "
    mkdir -p ${MKDIR_ARGS} '${STATE_MOUNT%%:*}' && \
    chmod 777 '${STATE_MOUNT%%:*}' && \
    docker stop ${CONTAINER_NAME} 2>/dev/null || true && \
    docker rm ${CONTAINER_NAME} 2>/dev/null || true && \
    docker run -d --name ${CONTAINER_NAME} --restart unless-stopped \
      ${VOLUME_ARGS} \
      -v '${STATE_MOUNT}' \
      -e DLQ_HTTP_ADDR=127.0.0.1:8080 \
      -e DLQ_CONCURRENCY=2 \
      -e DLQ_OUT_DIR_PRESETS=${DLQ_OUT_DIR_PRESETS} \
      -e PUID=${PUID} \
      -e PGID=${PGID} \
      -e TZ=${TZ} \
      ${IMAGE_TAG_VERSIONED}
  "
  echo "✓ Container deployed"
else
  echo "Image loaded on ${REMOTE_HOST}"
  echo "Run with --deploy to create/update the container"
fi
