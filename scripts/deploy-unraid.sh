#!/usr/bin/env bash
set -euo pipefail

# Build the image locally and copy it to the Unraid box (SSH host alias expected: HOMENAS).
# Usage: scripts/deploy-unraid.sh [--deploy]
#
# Options:
#   --deploy    Also (re)create the container on Unraid after loading the image

REMOTE_HOST="${REMOTE_HOST:-HOMENAS}"
VERSION_FILE="${VERSION_FILE:-VERSION}"
IMAGE_REPO="${IMAGE_REPO:-dlq}"
REMOTE_TAR="${REMOTE_TAR:-/tmp/dlq.tar.gz}"
CONTAINER_NAME="${CONTAINER_NAME:-dlq}"
DEPLOY=false

TEMPLATE_TPL="${TEMPLATE_TPL:-templates/unraid-dlq.tpl.xml}"
TEMPLATE_LOCAL="${TEMPLATE_LOCAL:-templates/unraid-dlq.xml}"
REMOTE_TEMPLATE="${REMOTE_TEMPLATE:-/boot/config/plugins/dockerMan/templates-user/my-downloader-queue.xml}"
REMOTE_CONTAINER_TEMPLATE="${REMOTE_CONTAINER_TEMPLATE:-/boot/config/plugins/dockerMan/templates-user/my-dlq.xml}"

TV_SHOWS_PATH="${TV_SHOWS_PATH:-/mnt/user/tvshows}"
MOVIES_PATH="${MOVIES_PATH:-/mnt/user/movies}"
STATE_PATH="${STATE_PATH:-/mnt/user/appdata/dlq}"
HTTP_ADDR="${HTTP_ADDR:-127.0.0.1:8080}"
DLQ_CONCURRENCY="${DLQ_CONCURRENCY:-2}"
DLQ_OUT_DIR_PRESETS="${DLQ_OUT_DIR_PRESETS:-/data/tvshows,/data/movies}"
PUID="${PUID:-99}"
PGID="${PGID:-100}"
TZ="${TZ:-UTC}"

# Parse arguments
while [[ $# -gt 0 ]]; do
  case "$1" in
    --deploy)
      DEPLOY=true
      shift
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"

if [[ ! -f "${repo_root}/${VERSION_FILE}" ]]; then
  echo "version file not found: ${repo_root}/${VERSION_FILE}" >&2
  exit 1
fi

VERSION="$(cat "${repo_root}/${VERSION_FILE}")"
if [[ -z "${VERSION}" ]]; then
  echo "ERROR: VERSION is empty" >&2
  exit 1
fi

IMAGE_TAG_VERSIONED="${IMAGE_REPO}:${VERSION}"
IMAGE_TAG_LATEST="${IMAGE_REPO}:latest"

if [[ ! -f "${repo_root}/${TEMPLATE_TPL}" ]]; then
  echo "template not found: ${repo_root}/${TEMPLATE_TPL}" >&2
  exit 1
fi

escape_sed() {
  printf '%s' "$1" | sed -e 's/[&|\\]/\\&/g'
}

printf "==> Building version %s\n" "${VERSION}"
printf "    Platform: linux/amd64\n"

docker buildx build --platform linux/amd64 --load \
  --build-arg VERSION="${VERSION}" \
  -t "${IMAGE_TAG_VERSIONED}" \
  -t "${IMAGE_TAG_LATEST}" \
  "${repo_root}"

printf "==> Saving image to tar.gz\n"
local_tar="$(mktemp -t dlq.XXXXXX).tar.gz"
docker save "${IMAGE_TAG_LATEST}" "${IMAGE_TAG_VERSIONED}" | gzip > "${local_tar}"

printf "==> Generating Unraid template\n"
TV_SHOWS_ESC="$(escape_sed "${TV_SHOWS_PATH}")"
MOVIES_ESC="$(escape_sed "${MOVIES_PATH}")"
STATE_ESC="$(escape_sed "${STATE_PATH}")"
IMAGE_ESC="$(escape_sed "${IMAGE_TAG_VERSIONED}")"
TEMPLATE_OUT="${repo_root}/${TEMPLATE_LOCAL}"

sed \
  -e "s|@IMAGE@|${IMAGE_ESC}|g" \
  -e "s|@TV_SHOWS_PATH@|${TV_SHOWS_ESC}|g" \
  -e "s|@MOVIES_PATH@|${MOVIES_ESC}|g" \
  -e "s|@STATE_PATH@|${STATE_ESC}|g" \
  -e "s|@HTTP_ADDR@|${HTTP_ADDR}|g" \
  -e "s|@DLQ_CONCURRENCY@|${DLQ_CONCURRENCY}|g" \
  -e "s|@DLQ_OUT_DIR_PRESETS@|$(escape_sed "${DLQ_OUT_DIR_PRESETS}")|g" \
  -e "s|@PUID@|${PUID}|g" \
  -e "s|@PGID@|${PGID}|g" \
  -e "s|@TZ@|${TZ}|g" \
  "${repo_root}/${TEMPLATE_TPL}" > "${TEMPLATE_OUT}"

printf "==> Copying image to %s:%s\n" "${REMOTE_HOST}" "${REMOTE_TAR}"
scp "${local_tar}" "${REMOTE_HOST}:${REMOTE_TAR}"

printf "==> Loading image on %s\n" "${REMOTE_HOST}"
ssh "${REMOTE_HOST}" "if command -v gunzip >/dev/null 2>&1; then gunzip -c ${REMOTE_TAR} | docker load; else gzip -dc ${REMOTE_TAR} | docker load; fi"

printf "==> Cleaning up remote tarball\n"
ssh "${REMOTE_HOST}" "rm -f ${REMOTE_TAR}"

printf "==> Updating Unraid templates on %s\n" "${REMOTE_HOST}"
scp "${TEMPLATE_OUT}" "${REMOTE_HOST}:${REMOTE_TEMPLATE}"
scp "${TEMPLATE_OUT}" "${REMOTE_HOST}:${REMOTE_CONTAINER_TEMPLATE}"

if [[ "${DEPLOY}" == "true" ]]; then
  printf "==> Deploying container on %s\n" "${REMOTE_HOST}"
  echo "    Ensuring data directories exist..."
  ssh "${REMOTE_HOST}" "mkdir -p '${TV_SHOWS_PATH}' '${MOVIES_PATH}' '${STATE_PATH}' && chmod 777 '${STATE_PATH}'"
  ssh "${REMOTE_HOST}" "if docker ps -a --format '{{.Names}}' | grep -qx '${CONTAINER_NAME}'; then docker stop '${CONTAINER_NAME}' && docker rm '${CONTAINER_NAME}'; fi"

  ssh "${REMOTE_HOST}" "docker run -d \
    --name ${CONTAINER_NAME} \
    --restart unless-stopped \
    -v ${TV_SHOWS_PATH}:/data/tvshows \
    -v ${MOVIES_PATH}:/data/movies \
    -v ${STATE_PATH}:/state \
    -e DLQ_HTTP_ADDR=${HTTP_ADDR} \
    -e DLQ_CONCURRENCY=${DLQ_CONCURRENCY} \
    -e DLQ_OUT_DIR_PRESETS=${DLQ_OUT_DIR_PRESETS} \
    -e PUID=${PUID} \
    -e PGID=${PGID} \
    -e TZ=${TZ} \
    ${IMAGE_TAG_VERSIONED}"
fi

rm -f "${local_tar}"

cat <<EOF

Done!
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Version:      ${VERSION}
  Image tags:   ${IMAGE_TAG_VERSIONED}
                ${IMAGE_TAG_LATEST}
  Remote host:  ${REMOTE_HOST}
  Container:    ${CONTAINER_NAME}
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

EOF

if [[ "${DEPLOY}" == "true" ]]; then
  echo "✓ Image loaded and container deployed"
else
  echo "Image loaded on ${REMOTE_HOST}"
  echo ""
  echo "Next steps:"
  echo "  1. Create container in Unraid UI using 'My Downloader Queue' template"
  echo "  2. Or re-run with --deploy to auto-create the container"
fi
