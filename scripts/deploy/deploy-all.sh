#!/bin/bash
set -e

# Главный скрипт для деплоя всех сервисов на обе VM
# Использование: ./deploy-all.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

cd "$PROJECT_ROOT"

echo "🚀 Deploying all services"
echo "========================"
echo ""

# Деплой парсера
echo "📡 Deploying Parser Service..."
bash "$SCRIPT_DIR/deploy-parsers.sh"

echo ""
echo "---"
echo ""

# Деплой core сервисов
echo "📡 Deploying Core Services..."
bash "$SCRIPT_DIR/deploy-core-services.sh"

echo ""
echo "✅ All services deployed successfully!"
echo ""
echo "📊 Quick commands:"
echo "  - Parser logs:    ssh vm-parsers 'sudo journalctl -u vodeneevbet-parser -f'"
echo "  - Calculator logs: ssh vm-core-services 'sudo journalctl -u vodeneevbet-calculator -f'"
echo "  - API logs:        ssh vm-core-services 'sudo journalctl -u vodeneevbet-api -f'"
