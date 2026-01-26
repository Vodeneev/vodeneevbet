#!/bin/bash
# Скрипт для проверки производительности парсеров после оптимизаций

FONBET_URL="http://158.160.197.172/parser/fonbet/metrics"
PINNACLE_URL="http://158.160.197.172/parser/pinnacle/metrics"

check_parser() {
    local PARSER_NAME=$1
    local METRICS_URL=$2
    
    echo "📊 Проверка производительности парсера $PARSER_NAME"
    echo "=============================================="
    echo ""

    # Получаем метрики
    METRICS=$(curl -sS --max-time 10 "$METRICS_URL" 2>/dev/null)
    
    if [ -z "$METRICS" ]; then
        echo "  ⚠️  Парсер $PARSER_NAME недоступен или метрики пусты"
        echo ""
        return 1
    fi

    # Извлекаем ключевые метрики
    SUCCESS_RATE=$(echo "$METRICS" | jq -r '.per_match.success_rate')
    AVG_STORE_TIME=$(echo "$METRICS" | jq -r '.per_match.avg_store_time')
    TOTAL_MATCHES=$(echo "$METRICS" | jq -r '.overall.total_matches')
    TOTAL_RUNS=$(echo "$METRICS" | jq -r '.overall.total_runs')
    
    echo "📈 Общая статистика:"
    echo "  Запусков: $TOTAL_RUNS"
    echo "  Обработано матчей: $TOTAL_MATCHES"
    echo ""
    
    echo "⏱️  Производительность:"
    echo "  Success Rate: ${SUCCESS_RATE}%"
    echo "  Avg Store Time: $AVG_STORE_TIME"
    echo ""

    # Проверка проблем
    echo "🔍 Анализ:"
    echo ""
    
    ISSUES=0
    
    # Проверка success rate
    if (( $(echo "$SUCCESS_RATE < 95" | bc -l 2>/dev/null || echo "0") )); then
        echo "  ⚠️  WARNING: Success rate ниже 95% (${SUCCESS_RATE}%)"
        ISSUES=$((ISSUES + 1))
    else
        echo "  ✅ Success rate в норме (${SUCCESS_RATE}%)"
    fi
    
    # Проверка avg store time (нужно парсить строку типа "1.971628948s")
    STORE_TIME_SEC=$(echo "$AVG_STORE_TIME" | sed 's/[^0-9.]//g' | head -c 10)
    if (( $(echo "$STORE_TIME_SEC > 0.5" | bc -l 2>/dev/null || echo "0") )); then
        echo "  ⚠️  WARNING: Среднее время записи больше 500ms (${AVG_STORE_TIME})"
        echo "     → Проверьте, используются ли bulk операции в логах"
        ISSUES=$((ISSUES + 1))
    else
        echo "  ✅ Среднее время записи в норме (${AVG_STORE_TIME})"
    fi
    
    
    echo ""
    if [ $ISSUES -eq 0 ]; then
        echo "✅ Все метрики в норме!"
    else
        echo "⚠️  Обнаружено проблем: $ISSUES"
        echo ""
        echo "Рекомендации:"
        PARSER_LOWER=$(echo "$PARSER_NAME" | tr '[:upper:]' '[:lower:]')
        echo "1. Проверьте логи: docker logs vodeneevbet-parser-${PARSER_LOWER} --tail 100"
        echo "2. Ищите ошибки: grep -E '⚠️|❌' в логах"
        echo "3. Проверьте производительность парсера и доступность API"
    fi
    
    echo ""
    echo "📊 Полные метрики для $PARSER_NAME:"
    echo "$METRICS" | jq '.'
    echo ""
    echo ""
}

# Проверяем оба парсера
check_parser "Fonbet" "$FONBET_URL"
check_parser "Pinnacle" "$PINNACLE_URL"
