#!/bin/bash
# Скрипт для проверки производительности endpoint /matches

BASE_URL="http://158.160.197.172/parser/fonbet"
LIMIT=${1:-100}

echo "📊 Проверка производительности endpoint /matches"
echo "=============================================="
echo ""

echo "🔗 URL: ${BASE_URL}/matches?limit=${LIMIT}"
echo ""

# Измеряем время выполнения
START_TIME=$(date +%s.%N)

# Получаем данные
RESPONSE=$(curl -sS --max-time 30 \
  -w "\n%{http_code}\n%{time_total}" \
  "${BASE_URL}/matches?limit=${LIMIT}")

END_TIME=$(date +%s.%N)

# Извлекаем HTTP код и время из curl
HTTP_CODE=$(echo "$RESPONSE" | tail -2 | head -1)
CURL_TIME=$(echo "$RESPONSE" | tail -1)
RESPONSE_BODY=$(echo "$RESPONSE" | head -n -2)

if [ "$HTTP_CODE" != "200" ]; then
    echo "❌ Ошибка: HTTP $HTTP_CODE"
    echo "$RESPONSE_BODY" | jq '.' 2>/dev/null || echo "$RESPONSE_BODY"
    exit 1
fi

# Парсим JSON ответ
MATCHES_JSON=$(echo "$RESPONSE_BODY" | jq '.' 2>/dev/null)

if [ -z "$MATCHES_JSON" ]; then
    echo "❌ Не удалось распарсить JSON ответ"
    echo "$RESPONSE_BODY" | head -20
    exit 1
fi

# Извлекаем метаданные
COUNT=$(echo "$MATCHES_JSON" | jq -r '.meta.count // 0')
DURATION=$(echo "$MATCHES_JSON" | jq -r '.meta.duration // "unknown"')
LIMIT_USED=$(echo "$MATCHES_JSON" | jq -r '.meta.limit // 0')

# Подсчитываем статистику
TOTAL_EVENTS=$(echo "$MATCHES_JSON" | jq '[.matches[].events | length] | add // 0')
TOTAL_OUTCOMES=$(echo "$MATCHES_JSON" | jq '[.matches[].events[].outcomes | length] | add // 0')
BOOKMAKERS=$(echo "$MATCHES_JSON" | jq '[.matches[].events[].bookmaker] | unique | length')

echo "📈 Результаты:"
echo "  HTTP Code: $HTTP_CODE"
echo "  Matches: $COUNT"
echo "  Duration (from API): $DURATION"
echo "  Duration (curl): ${CURL_TIME}s"
echo "  Limit: $LIMIT_USED"
echo ""

echo "📊 Статистика данных:"
echo "  Total Events: $TOTAL_EVENTS"
echo "  Total Outcomes: $TOTAL_OUTCOMES"
echo "  Unique Bookmakers: $BOOKMAKERS"
echo ""

# Проверка производительности
echo "⏱️  Анализ производительности:"
CURL_TIME_NUM=$(echo "$CURL_TIME" | awk '{print $1}')

if (( $(echo "$CURL_TIME_NUM > 2.0" | bc -l 2>/dev/null || echo "0") )); then
    echo "  ⚠️  WARNING: Запрос занял больше 2 секунд (${CURL_TIME}s)"
    echo "     → Рассмотрите использование меньшего limit"
elif (( $(echo "$CURL_TIME_NUM > 1.0" | bc -l 2>/dev/null || echo "0") )); then
    echo "  ⚠️  WARNING: Запрос занял больше 1 секунды (${CURL_TIME}s)"
    echo "     → Проверьте производительность парсера и доступность API"
else
    echo "  ✅ Время выполнения в норме (${CURL_TIME}s)"
fi

# Проверка количества данных
if [ "$COUNT" -eq 0 ]; then
    echo "  ⚠️  WARNING: Не найдено матчей"
    echo "     → Проверьте, что парсеры работают и записывают данные"
elif [ "$COUNT" -lt "$LIMIT_USED" ]; then
    echo "  ℹ️  INFO: Возвращено меньше матчей чем запрошено ($COUNT < $LIMIT_USED)"
    echo "     → В базе меньше матчей чем запрошено"
else
    echo "  ✅ Количество матчей соответствует запросу"
fi

echo ""
echo "📋 Пример первого матча:"
echo "$MATCHES_JSON" | jq '.matches[0] | {
  id: .id,
  name: .name,
  events_count: (.events | length),
  bookmakers: [.events[].bookmaker] | unique,
  sample_event: .events[0] | {
    type: .event_type,
    bookmaker: .bookmaker,
    outcomes_count: (.outcomes | length),
    sample_outcomes: .outcomes[0:3]
  }
}' 2>/dev/null || echo "Не удалось извлечь пример"

echo ""
echo "💾 Полный ответ сохранен в matches_response.json"
echo "$MATCHES_JSON" > matches_response.json
