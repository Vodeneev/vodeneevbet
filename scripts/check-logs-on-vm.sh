#!/usr/bin/env bash
# Проверка логов на VM bookmaker-services
# Использование: ./scripts/check-logs-on-vm.sh [user@host]

set -euo pipefail

VM="${1:-vodeneevm@158.160.159.73}"

echo "=== Проверка логов на VM bookmaker-services ==="
echo "VM: ${VM}"
echo ""

echo "1. Проверка статуса контейнеров:"
ssh "${VM}" "sudo docker ps --filter name=vodeneevbet --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'"
echo ""

echo "2. Проверка переменных окружения YC_* в контейнере fonbet:"
ssh "${VM}" "sudo docker exec vodeneevbet-fonbet env | grep YC_ || echo 'Переменные YC_ не найдены'"
echo ""

echo "3. Последние 20 строк логов fonbet:"
ssh "${VM}" "sudo docker logs vodeneevbet-fonbet --tail 20 2>&1 | tail -20"
echo ""

echo "4. Поиск сообщений об инициализации логирования:"
ssh "${VM}" "sudo docker logs vodeneevbet-fonbet 2>&1 | grep -i 'logging\|yandex\|yc-logging' | tail -5 || echo 'Сообщений о логировании не найдено'"
echo ""

echo "5. Проверка .env файла:"
ssh "${VM}" "cat /opt/vodeneevbet/bookmaker-services/.env 2>/dev/null | grep -E 'YC_|IMAGE' || echo '.env файл не найден или не содержит YC_ переменных'"
echo ""

echo "6. Генерация тестового сообщения:"
TEST_MSG="TEST_LOG_CHECK_$(date +%Y%m%d_%H%M%S)"
ssh "${VM}" "sudo docker exec vodeneevbet-fonbet sh -c 'echo \"${TEST_MSG}\"'"
echo ""
echo "Тестовое сообщение отправлено: ${TEST_MSG}"
echo "Проверь в Yandex Cloud Logging через 5-10 секунд по фильтру: message:\"${TEST_MSG}\""
