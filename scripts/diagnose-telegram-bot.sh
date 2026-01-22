#!/bin/bash
# Diagnostic script for telegram bot issues

set -euo pipefail

REMOTE_DIR="${REMOTE_DIR:-/opt/vodeneevbet/core}"
CONTAINER_NAME="vodeneevbet-telegram-bot"

echo "=== Telegram Bot Diagnostic ==="
echo ""

echo "1. Checking container status..."
sudo docker ps -a --filter "name=${CONTAINER_NAME}" || echo "Container not found"
echo ""

echo "2. Checking if container is running..."
if sudo docker ps -q -f "name=${CONTAINER_NAME}" -f "status=running" | grep -q .; then
    echo "✓ Container is running"
else
    echo "✗ Container is NOT running"
    echo ""
    echo "Container status:"
    sudo docker ps -a --filter "name=${CONTAINER_NAME}"
fi
echo ""

echo "3. Recent logs (last 50 lines)..."
sudo docker logs --tail=50 "${CONTAINER_NAME}" 2>&1 || echo "Failed to get logs"
echo ""

echo "4. Checking environment variables..."
sudo docker inspect "${CONTAINER_NAME}" 2>/dev/null | grep -A 10 "Env" || echo "Failed to inspect container"
echo ""

echo "5. Checking if calculator service is accessible from bot container..."
if sudo docker exec "${CONTAINER_NAME}" wget -q --spider --timeout=5 http://calculator:8080/health 2>/dev/null; then
    echo "✓ Calculator service is accessible"
else
    echo "✗ Calculator service is NOT accessible"
    echo "Trying to ping calculator container..."
    sudo docker exec "${CONTAINER_NAME}" ping -c 2 calculator 2>&1 || echo "Ping failed"
fi
echo ""

echo "6. Checking calculator container..."
CALC_CONTAINER="vodeneevbet-calculator"
if sudo docker ps -q -f "name=${CALC_CONTAINER}" -f "status=running" | grep -q .; then
    echo "✓ Calculator container is running"
    echo "Calculator health check:"
    sudo docker exec "${CALC_CONTAINER}" wget -q -O- http://localhost:8080/health 2>&1 || echo "Health check failed"
else
    echo "✗ Calculator container is NOT running"
fi
echo ""

echo "7. Checking network connectivity..."
sudo docker network ls | grep vodeneevbet || echo "No vodeneevbet network found"
echo ""

echo "8. Full container logs (last 100 lines)..."
echo "---"
sudo docker logs --tail=100 "${CONTAINER_NAME}" 2>&1
echo "---"
echo ""

echo "9. Container resource usage..."
sudo docker stats --no-stream "${CONTAINER_NAME}" 2>&1 || echo "Stats unavailable"
echo ""

echo "=== Diagnostic complete ==="
