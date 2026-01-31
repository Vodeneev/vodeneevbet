#!/usr/bin/env bash
set -euo pipefail

# Deploy bookmaker services (fonbet, pinnacle, pinnacle888) to vm-bookmaker-services.
# Default VM: 158.160.159.73
#
# After deploy, configure parser orchestrator with:
#   parser.bookmaker_services:
#     fonbet: "http://158.160.159.73:8081"
#     pinnacle: "http://158.160.159.73:8082"
#     pinnacle888: "http://158.160.159.73:8083"
#
# Requirements on VM: docker + docker compose, GHCR_TOKEN if images are private.

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

if [[ -z "${IMAGE_OWNER}" ]]; then
  echo "IMAGE_OWNER is not set. Example: IMAGE_OWNER=vodeneev" >&2
  exit 1
fi

echo "🚀 Deploying bookmaker services (fonbet, pinnacle, pinnacle888) to ${VM_HOST}"

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
printf \"IMAGE_OWNER=%s\nIMAGE_TAG=%s\n\" \"${IMAGE_OWNER}\" \"${IMAGE_TAG}\" > .env
# Optional: pass API keys via .env (create manually or set COPY_KEYS and add to deploy)
if [ -n \"${GHCR_TOKEN}\" ]; then
  echo \"${GHCR_TOKEN}\" | sudo docker login ghcr.io -u \"${GHCR_USERNAME}\" --password-stdin
fi
export COMPOSE_PROJECT_NAME=vodeneevbet_bookmaker
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
# Check containers
test \"\$(sudo docker ps -q -f name=vodeneevbet-fonbet -f status=running | wc -l)\" -ge 1
test \"\$(sudo docker ps -q -f name=vodeneevbet-pinnacle -f status=running | wc -l)\" -ge 1
test \"\$(sudo docker ps -q -f name=vodeneevbet-pinnacle888 -f status=running | wc -l)\" -ge 1
'"

echo "✅ Bookmaker services deployed on ${VM_HOST}"
echo ""
echo "Ports: fonbet :8081, pinnacle :8082, pinnacle888 :8083"
echo "Orchestrator config (parser.bookmaker_services):"
echo "  fonbet: \"http://${VM_HOST}:8081\""
echo "  pinnacle: \"http://${VM_HOST}:8082\""
echo "  pinnacle888: \"http://${VM_HOST}:8083\""
echo ""
echo "Logs: ssh ${VM_USER}@${VM_HOST} 'sudo docker logs -f vodeneevbet-fonbet'"
