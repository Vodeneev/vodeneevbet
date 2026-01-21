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
    YDB_WRITE_PERCENT=$(echo "$METRICS" | jq -r '.timing.ydb_write_percent')
    EVENTS_BATCH_SUCCESS=$(echo "$METRICS" | jq -r '.ydb_operations.events_batch.success_rate // "N/A"')
    MATCH_SUCCESS=$(echo "$METRICS" | jq -r '.ydb_operations.match.success_rate // "N/A"')
    EVENTS_BATCH_AVG_TIME=$(echo "$METRICS" | jq -r '.ydb_operations.events_batch.avg_time // "N/A"')
    TOTAL_MATCHES=$(echo "$METRICS" | jq -r '.overall.total_matches')
    TOTAL_RUNS=$(echo "$METRICS" | jq -r '.overall.total_runs')
    
    echo "📈 Общая статистика:"
    echo "  Запусков: $TOTAL_RUNS"
    echo "  Обработано матчей: $TOTAL_MATCHES"
    echo ""
    
    echo "⏱️  Производительность:"
    echo "  Success Rate: ${SUCCESS_RATE}%"
    echo "  Avg Store Time: $AVG_STORE_TIME"
    echo "  YDB Write %: ${YDB_WRITE_PERCENT}%"
    if [ "$EVENTS_BATCH_AVG_TIME" != "N/A" ]; then
        echo "  Events Batch Avg Time: $EVENTS_BATCH_AVG_TIME"
    fi
    echo ""
    
    if [ "$EVENTS_BATCH_SUCCESS" != "N/A" ] || [ "$MATCH_SUCCESS" != "N/A" ]; then
        echo "✅ Успешность операций:"
        if [ "$EVENTS_BATCH_SUCCESS" != "N/A" ]; then
            echo "  Events Batch Success: ${EVENTS_BATCH_SUCCESS}%"
        fi
        if [ "$MATCH_SUCCESS" != "N/A" ]; then
            echo "  Match Success: ${MATCH_SUCCESS}%"
        fi
        echo ""
    fi

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
    
    # Проверка YDB write percent
    if (( $(echo "$YDB_WRITE_PERCENT > 80" | bc -l 2>/dev/null || echo "0") )); then
        echo "  ⚠️  WARNING: YDB write занимает больше 80% времени (${YDB_WRITE_PERCENT}%)"
        echo "     → Проверьте логи на ResourceExhausted ошибки"
        ISSUES=$((ISSUES + 1))
    else
        echo "  ✅ YDB write процент в норме (${YDB_WRITE_PERCENT}%)"
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
    
    # Проверка events batch success (только если доступно)
    if [ "$EVENTS_BATCH_SUCCESS" != "N/A" ]; then
        if (( $(echo "$EVENTS_BATCH_SUCCESS < 95" | bc -l 2>/dev/null || echo "0") )); then
            echo "  ⚠️  WARNING: Events batch success rate ниже 95% (${EVENTS_BATCH_SUCCESS}%)"
            echo "     → Проверьте логи на ошибки bulk операций"
            ISSUES=$((ISSUES + 1))
        else
            echo "  ✅ Events batch success в норме (${EVENTS_BATCH_SUCCESS}%)"
        fi
    fi
    
    # Проверка match success (только если доступно)
    if [ "$MATCH_SUCCESS" != "N/A" ]; then
        if (( $(echo "$MATCH_SUCCESS < 95" | bc -l 2>/dev/null || echo "0") )); then
            echo "  ⚠️  WARNING: Match success rate ниже 95% (${MATCH_SUCCESS}%)"
            echo "     → Много матчей не сохраняются успешно, проверьте логи"
            ISSUES=$((ISSUES + 1))
        else
            echo "  ✅ Match success в норме (${MATCH_SUCCESS}%)"
        fi
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
        echo "2. Ищите ошибки: grep -E 'ResourceExhausted|⚠️|❌' в логах"
        echo "3. Проверьте использование bulk операций: grep 'Bulk insert' в логах"
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
