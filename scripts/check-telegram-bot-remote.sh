#!/bin/bash
# Quick check script - run this on the VM via SSH
# Usage: ssh user@vm-host "bash -s" < check-telegram-bot-remote.sh

set -euo pipefail

CONTAINER="vodeneevbet-telegram-bot"

echo "=== Quick Telegram Bot Check ==="
echo ""

echo "1. Container status:"
sudo docker ps -a --filter "name=${CONTAINER}" --format "table {{.Names}}\t{{.Status}}\t{{.State}}"
echo ""

echo "2. Last 30 lines of logs:"
sudo docker logs --tail=30 "${CONTAINER}" 2>&1 | tail -30
echo ""

echo "3. Checking for errors in logs:"
sudo docker logs "${CONTAINER}" 2>&1 | grep -i "error\|panic\|fatal\|failed" | tail -10 || echo "No errors found in logs"
echo ""

echo "4. Environment check:"
sudo docker exec "${CONTAINER}" env | grep -E "TELEGRAM_BOT_TOKEN|CALCULATOR_URL" || echo "Cannot check env (container might be stopped)"
echo ""

echo "5. Calculator connectivity test:"
sudo docker exec "${CONTAINER}" wget -q --spider --timeout=3 http://calculator:8080/health 2>&1 && echo "✓ Calculator reachable" || echo "✗ Calculator NOT reachable"
echo ""

echo "=== Done ==="
