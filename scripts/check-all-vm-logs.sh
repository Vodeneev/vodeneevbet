#!/usr/bin/env bash
# Проверка логов со всех ВМ в Yandex Cloud Logging
# Использование: ./scripts/check-all-vm-logs.sh

set -euo pipefail

FOLDER_ID="${YC_FOLDER_ID:-b1g7tng74uda3ahpg6oi}"
LIMIT="${LIMIT:-10}"
SINCE="${SINCE:-5m}"  # Последние 5 минут

echo "=== Проверка логов со всех ВМ ==="
echo "Каталог: ${FOLDER_ID}"
echo "Лимит записей: ${LIMIT}"
echo "Период: последние ${SINCE}"
echo ""

# Проверка наличия yc CLI
if ! command -v yc &> /dev/null; then
    echo "❌ yc CLI не установлен. Установите: https://cloud.yandex.ru/docs/cli/quickstart" >&2
    exit 1
fi

# Проверка авторизации
if ! yc logging group list --folder-id="${FOLDER_ID}" &>/dev/null; then
    echo "❌ Ошибка авторизации или доступа к каталогу ${FOLDER_ID}" >&2
    echo "Проверьте: yc logging group list --folder-id=${FOLDER_ID}" >&2
    exit 1
fi

echo "📊 Проверка логов по сервисам:"
echo ""

# Функция для проверки логов сервиса
check_service_logs() {
    local service_label=$1
    local service_name=$2
    local vm_info=$3
    
    echo "--- ${service_name} (${vm_info}) ---"
    local count=$(yc logging read \
        --folder-id="${FOLDER_ID}" \
        --filter="service_label=\"${service_label}\" AND timestamp>=\"$(date -u -d "${SINCE} ago" +%Y-%m-%dT%H:%M:%SZ)\"" \
        --limit=1 \
        --format=json 2>/dev/null | jq -r 'length' 2>/dev/null || echo "0")
    
    if [ "${count}" -gt 0 ]; then
        echo "✅ Логи идут (найдено записей за последние ${SINCE})"
        # Показываем последние записи
        yc logging read \
            --folder-id="${FOLDER_ID}" \
            --filter="service_label=\"${service_label}\"" \
            --limit="${LIMIT}" \
            --format=json 2>/dev/null | jq -r '.[] | "  [\(.timestamp)] \(.message // .text // "no message")"' 2>/dev/null | head -3 || true
    else
        echo "⚠️  Логи не найдены за последние ${SINCE}"
    fi
    echo ""
}

# Проверка всех сервисов
check_service_logs "parser" "Parser" "vm-parsers (158.160.168.187)"
check_service_logs "calculator" "Calculator" "vm-core (158.160.222.217)"
check_service_logs "telegram-bot" "Telegram Bot" "vm-core (158.160.222.217)"
check_service_logs "bookmaker-fonbet" "Bookmaker Fonbet" "vm-bookmaker-services (158.160.159.73)"
check_service_logs "bookmaker-pinnacle" "Bookmaker Pinnacle" "vm-bookmaker-services (158.160.159.73)"
check_service_logs "bookmaker-pinnacle888" "Bookmaker Pinnacle888" "vm-bookmaker-services (158.160.159.73)"

echo "=== Общая статистика ==="
echo "Все логи проекта vodeneevbet за последние ${SINCE}:"
yc logging read \
    --folder-id="${FOLDER_ID}" \
    --filter="project_label=\"vodeneevbet\" AND timestamp>=\"$(date -u -d "${SINCE} ago" +%Y-%m-%dT%H:%M:%SZ)\"" \
    --limit="${LIMIT}" \
    --format=json 2>/dev/null | jq -r 'group_by(.service_label) | .[] | "\(.[0].service_label // "unknown"): \(length) записей"' 2>/dev/null || echo "Не удалось получить статистику"

echo ""
echo "✅ Проверка завершена"
echo ""
echo "Для детального просмотра используйте:"
echo "  yc logging read --folder-id=${FOLDER_ID} --filter='service_label=\"<service>\"' --limit=50"
