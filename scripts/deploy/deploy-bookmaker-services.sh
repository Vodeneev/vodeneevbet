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

# Read enabled bookmaker services from config
CONFIG_FILE="${CONFIG_FILE:-configs/production.yaml}"
if [[ ! -f "${CONFIG_FILE}" ]]; then
  echo "Error: Config file not found: ${CONFIG_FILE}" >&2
  exit 1
fi

# Extract enabled services from bookmaker_services section
# Parse YAML: find parser.bookmaker_services section and extract non-commented service names
ENABLED_SERVICES=()
in_parser=false
in_bookmaker_services=false
while IFS= read -r line; do
  # Detect start of parser section
  if [[ "$line" =~ ^[[:space:]]*parser:[[:space:]]*$ ]]; then
    in_parser=true
    continue
  fi
  # Detect start of bookmaker_services section (must be inside parser)
  if [[ "$in_parser" == true ]] && [[ "$line" =~ ^[[:space:]]{2}bookmaker_services:[[:space:]]*$ ]]; then
    in_bookmaker_services=true
    continue
  fi
  # Stop at next top-level key (0 spaces at start, not comment)
  if [[ "$line" =~ ^[^[:space:]#] ]]; then
    in_parser=false
    in_bookmaker_services=false
    continue
  fi
  # Stop bookmaker_services section at next key at same or higher level (2 spaces or less)
  if [[ "$in_bookmaker_services" == true ]] && [[ "$line" =~ ^[[:space:]]{0,2}[^[:space:]#] ]]; then
    if [[ ! "$line" =~ ^[[:space:]]{4} ]]; then
      in_bookmaker_services=false
      continue
    fi
  fi
  # Extract service names from non-commented lines in bookmaker_services section
  if [[ "$in_bookmaker_services" == true ]]; then
    # Skip commented lines
    [[ "$line" =~ ^[[:space:]]*# ]] && continue
    # Match lines like "    fonbet: "http://..." (4 spaces + service name + colon)
    if [[ "$line" =~ ^[[:space:]]{4}([a-zA-Z0-9_]+):[[:space:]]+ ]]; then
      service_name="${BASH_REMATCH[1]}"
      ENABLED_SERVICES+=("${service_name}")
    fi
  fi
done < "${CONFIG_FILE}"

if [[ ${#ENABLED_SERVICES[@]} -eq 0 ]]; then
  echo "Warning: No enabled bookmaker services found in ${CONFIG_FILE}" >&2
  echo "Please check parser.bookmaker_services section in the config" >&2
fi

echo "🚀 Deploying bookmaker services to ${VM_HOST}"
echo "📋 Enabled services from config: ${ENABLED_SERVICES[*]}"

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

# Build docker-compose services list as space-separated string
COMPOSE_SERVICES="${ENABLED_SERVICES[*]}"

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
# Stop and remove disabled services
ALL_SERVICES=\"fonbet pinnacle pinnacle888 marathonbet xbet1\"
ENABLED_SERVICES_LIST=\"${COMPOSE_SERVICES}\"
for svc in \$ALL_SERVICES; do
  if ! echo \"\$ENABLED_SERVICES_LIST\" | grep -qw \"\$svc\"; then
    echo \"Stopping disabled service: \$svc\"
    sudo docker stop vodeneevbet-\$svc 2>/dev/null || true
    sudo docker rm vodeneevbet-\$svc 2>/dev/null || true
  fi
done
# Build docker-compose command with profiles
if [ -z \"\$ENABLED_SERVICES_LIST\" ]; then
  echo \"Error: No enabled services found\" >&2
  exit 1
fi
# Build profile list for docker-compose (comma-separated)
COMPOSE_PROFILES=\"\"
for svc in \$ENABLED_SERVICES_LIST; do
  if [ -z \"\$COMPOSE_PROFILES\" ]; then
    COMPOSE_PROFILES=\"\$svc\"
  else
    COMPOSE_PROFILES=\"\$COMPOSE_PROFILES,\$svc\"
  fi
done
export COMPOSE_PROFILES=\"\$COMPOSE_PROFILES\"
if docker compose version >/dev/null 2>&1; then
  # docker compose v2 supports profiles via COMPOSE_PROFILES env var
  sudo docker compose down --remove-orphans || true
  sudo docker compose pull
  sudo docker compose up -d --remove-orphans --force-recreate
elif command -v docker-compose >/dev/null 2>&1; then
  # docker-compose v1 doesn't support profiles, use service names instead
  COMPOSE_CMD_ARGS=\"\"
  for svc in \$ENABLED_SERVICES_LIST; do
    COMPOSE_CMD_ARGS=\"\$COMPOSE_CMD_ARGS \$svc\"
  done
  sudo docker-compose down --remove-orphans || true
  sudo docker-compose pull\$COMPOSE_CMD_ARGS
  sudo docker-compose up -d --remove-orphans --force-recreate\$COMPOSE_CMD_ARGS
else
  echo \"Docker Compose is not installed\" >&2
  exit 1
fi
# Final cleanup: remove dangling images left after recreate
sudo docker image prune -f || true
# Verify only enabled services are running
for svc in \$ENABLED_SERVICES_LIST; do
  test \"\$(sudo docker ps -q -f name=vodeneevbet-\$svc -f status=running | wc -l)\" -ge 1 || (echo \"Service \$svc failed to start\" >&2 && exit 1)
done
'"

echo "✅ Bookmaker services deployed on ${VM_HOST}"
echo ""
echo "Enabled services: ${ENABLED_SERVICES[*]}"
echo ""
echo "Port mapping:"
declare -A PORT_MAP=(
  ["fonbet"]="8081"
  ["pinnacle"]="8082"
  ["pinnacle888"]="8083"
  ["marathonbet"]="8084"
  ["xbet1"]="8085"
)
for svc in "${ENABLED_SERVICES[@]}"; do
  port="${PORT_MAP[$svc]:-unknown}"
  echo "  ${svc}: http://${VM_HOST}:${port}"
done
echo ""
echo "Orchestrator config (parser.bookmaker_services):"
for svc in "${ENABLED_SERVICES[@]}"; do
  port="${PORT_MAP[$svc]:-unknown}"
  echo "  ${svc}: \"http://${VM_HOST}:${port}\""
done
echo ""
if [[ ${#ENABLED_SERVICES[@]} -gt 0 ]]; then
  first_svc="${ENABLED_SERVICES[0]}"
  echo "Logs: ssh ${VM_USER}@${VM_HOST} 'sudo docker logs -f vodeneevbet-${first_svc}'"
fi
