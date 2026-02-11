#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
cd "$PROJECT_ROOT"

VM_HOST="${VM_HOST:-158.160.159.73}"
VM_USER="${VM_USER:-vodeneevm}"
REMOTE_DIR="${REMOTE_DIR:-/opt/vodeneevbet/bookmaker-services}"

IMAGE_OWNER="${IMAGE_OWNER:-}"
IMAGE_TAG="${IMAGE_TAG:-main}"
GHCR_USERNAME="${GHCR_USERNAME:-${IMAGE_OWNER}}"
GHCR_TOKEN="${GHCR_TOKEN:-}"
COPY_KEYS="${COPY_KEYS:-0}"
YC_SERVICE_ACCOUNT_KEY_JSON_B64="${YC_SERVICE_ACCOUNT_KEY_JSON_B64:-}"
YC_FOLDER_ID="${YC_FOLDER_ID:-}"
YC_LOG_GROUP_ID="${YC_LOG_GROUP_ID:-}"
YC_LOG_GROUP_NAME="${YC_LOG_GROUP_NAME:-default}"

if [[ -z "${IMAGE_OWNER}" ]]; then
  echo "IMAGE_OWNER is not set. Example: IMAGE_OWNER=vodeneev" >&2
  exit 1
fi

echo "🚀 Deploying bookmaker services (fonbet, pinnacle, pinnacle888, marathonbet) to ${VM_HOST}"

echo "📡 Checking SSH connection..."
ssh -o ConnectTimeout=5 -o StrictHostKeyChecking=no "${VM_USER}@${VM_HOST}" "echo 'Connection OK'" >/dev/null

echo "📁 Preparing remote directory..."
ssh "${VM_USER}@${VM_HOST}" "sudo mkdir -p '${REMOTE_DIR}' '${REMOTE_DIR}/configs' '${REMOTE_DIR}/keys' && sudo chown -R '${VM_USER}:${VM_USER}' '${REMOTE_DIR}'"

echo "📦 Uploading docker-compose.yml..."
scp "deploy/vm-bookmaker-services/docker-compose.yml" "${VM_USER}@${VM_HOST}:${REMOTE_DIR}/docker-compose.yml"

echo "📦 Syncing configs..."
rsync -avz --delete "./configs/" "${VM_USER}@${VM_HOST}:${REMOTE_DIR}/configs/"

if [[ "${COPY_KEYS}" == "1" ]]; then
  echo "🔐 Syncing keys (COPY_KEYS=1)..."
  rsync -avz --delete "./keys/" "${VM_USER}@${VM_HOST}:${REMOTE_DIR}/keys/"
else
  echo "🔐 Skipping keys upload (set COPY_KEYS=1 to sync ./keys)"
fi

echo "🐳 Pull & up..."
ssh "${VM_USER}@${VM_HOST}" "bash -lc 'set -euo pipefail
cd \"${REMOTE_DIR}\"
if [ -n \"${YC_SERVICE_ACCOUNT_KEY_JSON_B64}\" ]; then
  printf \"IMAGE_OWNER=%s\nIMAGE_TAG=%s\nYC_SERVICE_ACCOUNT_KEY_JSON_B64=%s\nYC_FOLDER_ID=%s\nYC_LOG_GROUP_ID=%s\nYC_LOG_GROUP_NAME=%s\n\" \
    \"${IMAGE_OWNER}\" \"${IMAGE_TAG}\" \"${YC_SERVICE_ACCOUNT_KEY_JSON_B64}\" \"${YC_FOLDER_ID}\" \"${YC_LOG_GROUP_ID}\" \"${YC_LOG_GROUP_NAME}\" > .env
else
  printf \"IMAGE_OWNER=%s\nIMAGE_TAG=%s\nYC_FOLDER_ID=%s\nYC_LOG_GROUP_ID=%s\nYC_LOG_GROUP_NAME=%s\n\" \
    \"${IMAGE_OWNER}\" \"${IMAGE_TAG}\" \"${YC_FOLDER_ID}\" \"${YC_LOG_GROUP_ID}\" \"${YC_LOG_GROUP_NAME}\" > .env
fi
if [ -n \"${GHCR_TOKEN}\" ]; then
  echo \"${GHCR_TOKEN}\" | sudo docker login ghcr.io -u \"${GHCR_USERNAME}\" --password-stdin
fi
export COMPOSE_PROJECT_NAME=vodeneevbet_bookmaker
# Clean up old images/containers/build cache to prevent disk from filling up
sudo docker image prune -af --filter \"until=2h\" || true
sudo docker builder prune -af || true
if docker compose version >/dev/null 2>&1; then
  sudo docker compose down --remove-orphans || true
  sudo docker compose pull
  sudo docker compose up -d --remove-orphans --force-recreate
elif command -v docker-compose >/dev/null 2>&1; then
  sudo docker-compose down --remove-orphans || true
  sudo docker-compose pull
  sudo docker-compose up -d --remove-orphans --force-recreate
else
  echo \"Docker Compose is not installed\" >&2
  exit 1
fi
# Final cleanup: remove dangling images left after recreate
sudo docker image prune -f || true
test \"\$(sudo docker ps -q -f name=vodeneevbet-fonbet -f status=running | wc -l)\" -ge 1
test \"\$(sudo docker ps -q -f name=vodeneevbet-pinnacle -f status=running | wc -l)\" -ge 1
test \"\$(sudo docker ps -q -f name=vodeneevbet-pinnacle888 -f status=running | wc -l)\" -ge 1
test \"\$(sudo docker ps -q -f name=vodeneevbet-marathonbet -f status=running | wc -l)\" -ge 1
'"

echo "✅ Bookmaker services deployed on ${VM_HOST}"
echo ""
echo "Ports: fonbet :8081, pinnacle :8082, pinnacle888 :8083, marathonbet :8084"
echo "Orchestrator config (parser.bookmaker_services):"
echo "  fonbet: \"http://${VM_HOST}:8081\""
echo "  pinnacle: \"http://${VM_HOST}:8082\""
echo "  pinnacle888: \"http://${VM_HOST}:8083\""
echo "  marathonbet: \"http://${VM_HOST}:8084\""
echo ""
echo "Logs: ssh ${VM_USER}@${VM_HOST} 'sudo docker logs -f vodeneevbet-fonbet'"
