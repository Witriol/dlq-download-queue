#!/usr/bin/env bash
set -euo pipefail

# Build and deploy DLQ to Unraid
# Usage: scripts/deploy-unraid.sh <cli|webui|all>
# Config: Copy .env.example to .env and edit it

usage() {
  cat <<'EOF'
Usage: scripts/deploy-unraid.sh <cli|webui|all>

  cli   build + deploy dlq container only
  webui build + deploy dlq-webui container only
  all   build + deploy both containers
EOF
}

MODE="${1:-}"
case "${MODE}" in
  "")
    usage
    exit 1
    ;;
  -h|--help)
    usage
    exit 0
    ;;
  cli|webui|all) ;;
  *)
    usage
    exit 1
    ;;
esac

DEPLOY_CLI=false
DEPLOY_WEBUI=false
case "${MODE}" in
  cli) DEPLOY_CLI=true ;;
  webui) DEPLOY_WEBUI=true ;;
  all) DEPLOY_CLI=true; DEPLOY_WEBUI=true ;;
esac

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

# Keep in sync with scripts/run-dev.sh
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
DLQ_WEBUI_PORT="${DLQ_WEBUI_PORT:-8098}"

# Constants
IMAGE_REPO="dlq"
CONTAINER_NAME="dlq"
WEBUI_IMAGE_REPO="dlq-webui"
WEBUI_CONTAINER_NAME="dlq-webui"
NETWORK_NAME="dlq-net"
REMOTE_TAR="/tmp/dlq.tar.gz"
IMAGE_TAG_VERSIONED="${IMAGE_REPO}:${VERSION}"
IMAGE_TAG_LATEST="${IMAGE_REPO}:latest"
WEBUI_IMAGE_TAG_VERSIONED="${WEBUI_IMAGE_REPO}:${VERSION}"
WEBUI_IMAGE_TAG_LATEST="${WEBUI_IMAGE_REPO}:latest"

# Parse DATA_* variables to build volume flags and env passthrough
VOLUME_FLAGS=()
ENV_FLAGS=()
HOST_PATHS=()
for var in $(compgen -v | grep '^DATA_'); do
  mount="${!var}"
  [[ -z "${mount}" ]] && continue
  IFS=':' read -r host_path container_path <<< "${mount}"
  VOLUME_FLAGS+=("-v" "${mount}")
  ENV_FLAGS+=("-e" "${var}=${mount}")
  HOST_PATHS+=("${host_path}")
done

if [[ ${#VOLUME_FLAGS[@]} -eq 0 ]]; then
  echo "ERROR: No DATA_* variables found in .env" >&2
  exit 1
fi

IMAGES_TO_SAVE=()
if [[ "${DEPLOY_CLI}" == "true" ]]; then
  printf "==> Building %s (version %s)\n" "${IMAGE_TAG_VERSIONED}" "${VERSION}"
  docker buildx build --platform linux/amd64 --load \
    --build-arg VERSION="${VERSION}" \
    -t "${IMAGE_TAG_VERSIONED}" \
    -t "${IMAGE_TAG_LATEST}" \
    "${repo_root}"
  IMAGES_TO_SAVE+=("${IMAGE_TAG_LATEST}")
fi

if [[ "${DEPLOY_WEBUI}" == "true" ]]; then
  printf "==> Building %s (version %s)\n" "${WEBUI_IMAGE_TAG_VERSIONED}" "${VERSION}"
  docker buildx build --platform linux/amd64 --load \
    -f "${repo_root}/Dockerfile.webui" \
    -t "${WEBUI_IMAGE_TAG_VERSIONED}" \
    -t "${WEBUI_IMAGE_TAG_LATEST}" \
    "${repo_root}"
  IMAGES_TO_SAVE+=("${WEBUI_IMAGE_TAG_LATEST}")
fi

printf "==> Transferring image to %s\n" "${REMOTE_HOST}"
docker save "${IMAGES_TO_SAVE[@]}" | gzip | \
  ssh "${REMOTE_HOST}" "cat > ${REMOTE_TAR} && gunzip -c ${REMOTE_TAR} | docker load && rm -f ${REMOTE_TAR}"

printf "==> Deploying containers on %s\n" "${REMOTE_HOST}"

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

REMOTE_CMD="docker network inspect ${NETWORK_NAME} >/dev/null 2>&1 || docker network create ${NETWORK_NAME} && \
mkdir -p ${MKDIR_ARGS} '${STATE_MOUNT%%:*}' && \
chmod 777 '${STATE_MOUNT%%:*}'"

if [[ "${DEPLOY_CLI}" == "true" ]]; then
  REMOTE_CMD="${REMOTE_CMD} && \
docker stop ${CONTAINER_NAME} 2>/dev/null || true && \
docker rm ${CONTAINER_NAME} 2>/dev/null || true && \
docker run -d --name ${CONTAINER_NAME} --restart unless-stopped \
  --network ${NETWORK_NAME} \
  ${VOLUME_ARGS} \
  -p ${DLQ_HTTP_PORT}:${DLQ_HTTP_PORT} \
  -v '${STATE_MOUNT}' \
  -e DLQ_HTTP_ADDR=${DLQ_HTTP_ADDR} \
  -e DLQ_HTTP_PORT=${DLQ_HTTP_PORT} \
  ${ENV_FLAGS[*]} \
  -e PUID=${PUID} \
  -e PGID=${PGID} \
  -e TZ=${TZ} \
  ${IMAGE_TAG_LATEST} && \
for img in \$(docker images --format '{{.Repository}}:{{.Tag}}' ${IMAGE_REPO} | awk '\$0 !~ /:latest$/'); do docker rmi \"\${img}\" >/dev/null 2>&1 || true; done"
fi

if [[ "${DEPLOY_WEBUI}" == "true" ]]; then
  REMOTE_CMD="${REMOTE_CMD} && \
docker stop ${WEBUI_CONTAINER_NAME} 2>/dev/null || true && \
docker rm ${WEBUI_CONTAINER_NAME} 2>/dev/null || true && \
docker run -d --name ${WEBUI_CONTAINER_NAME} --restart unless-stopped \
  --network ${NETWORK_NAME} \
  -p ${DLQ_WEBUI_PORT}:${DLQ_WEBUI_PORT} \
  -e HOST=0.0.0.0 \
  -e PORT=${DLQ_WEBUI_PORT} \
  -e DLQ_API=http://dlq:${DLQ_HTTP_PORT} \
  ${WEBUI_IMAGE_TAG_LATEST} && \
for img in \$(docker images --format '{{.Repository}}:{{.Tag}}' ${WEBUI_IMAGE_REPO} | awk '\$0 !~ /:latest$/'); do docker rmi \"\${img}\" >/dev/null 2>&1 || true; done"
fi

ssh "${REMOTE_HOST}" "${REMOTE_CMD}"
echo "✓ Containers deployed"
