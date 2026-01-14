#!/usr/bin/env bash
set -euo pipefail

# Docker compose deploy script for vm-parsers (parser service only).
#
# Requirements on VM:
# - docker + docker compose plugin (or docker-compose)
# - access to GHCR if images are private (GHCR_TOKEN)
#
# Legacy systemd deploy is available at: scripts/deploy/legacy/deploy-parsers.systemd.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
cd "$PROJECT_ROOT"

VM_HOST="${VM_HOST:-158.160.197.172}"
VM_USER="${VM_USER:-vodeneevm}"
REMOTE_DIR="${REMOTE_DIR:-/opt/vodeneevbet/parsers}"

IMAGE_OWNER="${IMAGE_OWNER:-}"
IMAGE_TAG="${IMAGE_TAG:-main}"
GHCR_USERNAME="${GHCR_USERNAME:-${IMAGE_OWNER}}"
GHCR_TOKEN="${GHCR_TOKEN:-}"
COPY_KEYS="${COPY_KEYS:-0}" # set to 1 if you want to upload ./keys to VM

if [[ -z "${IMAGE_OWNER}" ]]; then
  echo "IMAGE_OWNER is not set. Example: IMAGE_OWNER=vodeneev" >&2
  exit 1
fi

echo "🚀 Deploying Parser (docker compose) to ${VM_HOST}"

echo "📡 Checking SSH connection..."
ssh -o ConnectTimeout=5 -o StrictHostKeyChecking=no "${VM_USER}@${VM_HOST}" "echo 'Connection OK'" >/dev/null

echo "📁 Preparing remote directory..."
ssh "${VM_USER}@${VM_HOST}" "sudo mkdir -p '${REMOTE_DIR}' '${REMOTE_DIR}/configs' '${REMOTE_DIR}/keys' && sudo chown -R '${VM_USER}:${VM_USER}' '${REMOTE_DIR}'"

echo "📦 Uploading docker-compose.yml..."
scp "deploy/vm-parsers/docker-compose.yml" "${VM_USER}@${VM_HOST}:${REMOTE_DIR}/docker-compose.yml"

echo "📦 Syncing configs..."
rsync -avz --delete "./configs/" "${VM_USER}@${VM_HOST}:${REMOTE_DIR}/configs/"

if [[ "${COPY_KEYS}" == "1" ]]; then
  echo "🔐 Syncing keys (COPY_KEYS=1)..."
  rsync -avz --delete "./keys/" "${VM_USER}@${VM_HOST}:${REMOTE_DIR}/keys/"
else
  echo "🔐 Skipping keys upload (set COPY_KEYS=1 to sync ./keys)"
fi

echo "🚦 Stopping legacy systemd service if exists..."
ssh "${VM_USER}@${VM_HOST}" "sudo systemctl stop vodeneevbet-parser >/dev/null 2>&1 || true"

echo "🐳 Pull & up..."
ssh "${VM_USER}@${VM_HOST}" "bash -lc 'set -euo pipefail
cd \"${REMOTE_DIR}\"
printf \"IMAGE_OWNER=%s\nIMAGE_TAG=%s\n\" \"${IMAGE_OWNER}\" \"${IMAGE_TAG}\" > .env
if [ -n \"${GHCR_TOKEN}\" ]; then
  echo \"${GHCR_TOKEN}\" | sudo docker login ghcr.io -u \"${GHCR_USERNAME}\" --password-stdin
fi
export COMPOSE_PROJECT_NAME=vodeneevbet_parsers
if docker compose version >/dev/null 2>&1; then
  sudo docker compose down --remove-orphans || true
  sudo docker compose pull
  sudo docker compose up -d --remove-orphans --force-recreate
elif command -v docker-compose >/dev/null 2>&1; then
  sudo docker-compose down --remove-orphans || true
  sudo docker-compose pull
  sudo docker-compose up -d --remove-orphans --force-recreate
else
  echo \"Docker Compose is not installed (need docker compose plugin or docker-compose)\" >&2
  exit 1
fi
test \"\$(sudo docker ps -q -f name=vodeneevbet-parser -f status=running | wc -l)\" -ge 1
'"

echo "✅ Parser deployed successfully!"
echo "📊 Logs: ssh ${VM_USER}@${VM_HOST} 'sudo docker logs -f vodeneevbet-parser'"
