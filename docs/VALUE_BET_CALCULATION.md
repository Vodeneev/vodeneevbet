# Расчет валуя (Value Bet) через средневзвешенный подход

## Концепция валуя

**Валуй (Value Bet)** - это ставка, где коэффициент конторы выше справедливой вероятности события.

**Формула:**
```
Value = (BookmakerOdd / FairOdd - 1) * 100%
```

Если Value > 0, это валуй!

## Средневзвешенный подход

### Шаг 1: Расчет справедливой вероятности

1. Берем коэффициенты от референсных контор (Pinnacle, Betfair, etc.)
2. Конвертируем в вероятности: `probability = 1 / odd`
3. Считаем средневзвешенную вероятность:
   - **Простое среднее:** `avg_prob = (prob1 + prob2 + ... + probN) / N`
   - **Взвешенное среднее:** `avg_prob = (prob1*w1 + prob2*w2 + ...) / (w1 + w2 + ...)`
     где `w` - вес конторы (надежность)
4. Конвертируем обратно в справедливый коэффициент: `fair_odd = 1 / avg_prob`

### Шаг 2: Поиск валуя

Для каждой конторы (не референсной):
```
value_percent = (bookmaker_odd / fair_odd - 1) * 100
if value_percent > threshold:
    это валуй!
```

## Пример расчета

**Исходные данные:**
- Pinnacle: 2.0 (вероятность 50%)
- Betfair: 2.1 (вероятность 47.6%)
- Bet365: 1.9 (вероятность 52.6%)

**Расчет справедливой вероятности:**
```
avg_prob = (50% + 47.6% + 52.6%) / 3 = 50.07%
fair_odd = 1 / 0.5007 = 1.997 ≈ 2.0
```

**Поиск валуя:**
- Контора X предлагает 2.5
- Value = (2.5 / 2.0 - 1) * 100 = **25%** ✅ Валуй!
- Контора Y предлагает 1.8
- Value = (1.8 / 2.0 - 1) * 100 = **-10%** ❌ Не валуй

## Варианты взвешивания

### 1. Простое среднее (равные веса)
```go
probabilities := []float64{1/2.0, 1/2.1, 1/1.9}
avgProb := average(probabilities)
fairOdd := 1 / avgProb
```

### 2. Взвешенное среднее (по надежности)
```go
weights := map[string]float64{
    "pinnacle": 1.0,  // максимальная надежность
    "betfair":  0.9,
    "bet365":   0.8,
}
avgProb := weightedAverage(probabilities, weights)
fairOdd := 1 / avgProb
```

### 3. Медиана (устойчивость к выбросам)
```go
probabilities := []float64{1/2.0, 1/2.1, 1/1.9}
medianProb := median(probabilities)
fairOdd := 1 / medianProb
```

### 4. Гармоническое среднее (для коэффициентов)
```go
odds := []float64{2.0, 2.1, 1.9}
harmonicMean := len(odds) / sum(1/odds)  // для odds
fairOdd := harmonicMean
```

## Рекомендуемый подход: Взвешенное среднее

**Преимущества:**
- ✅ Учитывает надежность контор
- ✅ Более точная оценка справедливой вероятности
- ✅ Устойчивость к выбросам (если одна контора сильно отличается)

**Формула:**
```
fair_prob = Σ(prob_i * weight_i) / Σ(weight_i)
fair_odd = 1 / fair_prob
```

**Веса контор:**
```yaml
bookmaker_weights:
  pinnacle: 1.0    # эталон
  betfair: 0.95
  sbobet: 0.9
  bet365: 0.85
  williamhill: 0.8
  pinnacle888: 0.7  # новая контора, меньше вес
```

## Структура данных

```go
type ValueBet struct {
    MatchGroupKey string    `json:"match_group_key"`
    MatchName     string    `json:"match_name"`
    StartTime     time.Time `json:"start_time"`
    Sport         string    `json:"sport"`
    
    EventType    string `json:"event_type"`
    OutcomeType  string `json:"outcome_type"`
    Parameter    string `json:"parameter"`
    BetKey       string `json:"bet_key"`
    
    // Референсные данные
    ReferenceBookmakers []string `json:"reference_bookmakers"` // какие конторы использовались
    ReferenceOdds       []float64 `json:"reference_odds"`      // их коэффициенты
    FairOdd             float64  `json:"fair_odd"`            // справедливый коэффициент
    FairProbability     float64  `json:"fair_probability"`     // справедливая вероятность
    
    // Валуй
    Bookmaker           string  `json:"bookmaker"`             // контора с валуем
    BookmakerOdd        float64 `json:"bookmaker_odd"`         // её коэффициент
    ValuePercent        float64 `json:"value_percent"`          // процент валуя
    ExpectedValue       float64 `json:"expected_value"`         // математическое ожидание
    
    CalculatedAt        time.Time `json:"calculated_at"`
}
```

## Математическое ожидание

Для полной картины можно считать Expected Value (EV):

```
EV = (bookmaker_odd * fair_probability) - 1
```

Если EV > 0, ставка выгодна в долгосрочной перспективе.

**Пример:**
- Fair probability: 50%
- Bookmaker odd: 2.5
- EV = (2.5 * 0.5) - 1 = 1.25 - 1 = **0.25** (25% прибыль в среднем)

## Минимальные требования

Для расчета справедливой вероятности нужно минимум:
- **2 референсные конторы** (лучше 3+)
- Если меньше 2, используем fallback (например, просто лучший коэффициент)

## Фильтрация результатов

1. **Минимальный валуй:** показывать только если `value_percent >= 5%`
2. **Минимальное количество референсных контор:** минимум 2
3. **Максимальный валуй:** ограничить сверху (например, 100%), чтобы отфильтровать ошибки

## План реализации

1. **Этап 1:** Реализовать расчет справедливой вероятности через простое среднее
2. **Этап 2:** Добавить взвешенное среднее с конфигурацией весов
3. **Этап 3:** Добавить расчет Expected Value
4. **Этап 4:** Оптимизация и кэширование

## Конфигурация

```yaml
value_calculator:
  # Референсные конторы для расчета справедливой вероятности
  reference_bookmakers: ["pinnacle", "betfair", "sbobet", "bet365", "williamhill"]
  
  # Веса контор (опционально, по умолчанию все равны 1.0)
  # Более надежные конторы имеют больший вес
  bookmaker_weights:
    pinnacle: 1.0      # эталон
    betfair: 0.95
    sbobet: 0.9
    bet365: 0.85
    williamhill: 0.8
    pinnacle888: 0.7   # новая контора, меньше вес
  
  # Минимальный валуй для показа (в процентах)
  min_value_percent: 5.0
  
  # Остальные настройки
  parser_url: "http://localhost:8080/matches"
  async_enabled: true
  async_interval: 30s
  alert_threshold: 10.0  # Отправлять алерты для валуя >= 10%
```

## API Endpoints

### GET /value-bets/top

Возвращает топ валуев.

**Параметры:**
- `limit` (optional): количество результатов (по умолчанию 5, максимум 50)
- `status` (optional): фильтр по статусу матча (`live`, `upcoming`, или пусто для всех)

**Пример запроса:**
```bash
curl "http://localhost:8081/value-bets/top?limit=10&status=upcoming"
```

**Пример ответа:**
```json
[
  {
    "match_group_key": "football|manchester united|liverpool|2026-01-25T15:00:00Z",
    "match_name": "Manchester United vs Liverpool",
    "start_time": "2026-01-25T15:00:00Z",
    "sport": "football",
    "event_type": "main_match",
    "outcome_type": "home_win",
    "parameter": "",
    "bet_key": "main_match|home_win|",
    "reference_bookmakers": ["pinnacle", "betfair", "bet365"],
    "reference_odds": [2.0, 2.1, 1.9],
    "fair_odd": 2.0,
    "fair_probability": 0.5007,
    "bookmaker": "pinnacle888",
    "bookmaker_odd": 2.5,
    "value_percent": 25.0,
    "expected_value": 0.25,
    "calculated_at": "2026-01-24T10:00:00Z"
  }
]
```

## Как это работает

1. **Сбор данных:** Собираются все коэффициенты от всех контор для каждой ставки
2. **Расчет справедливой вероятности:**
   - Берем коэффициенты от референсных контор
   - Конвертируем в вероятности: `prob = 1 / odd`
   - Считаем средневзвешенную: `avg_prob = Σ(prob_i * weight_i) / Σ(weight_i)`
   - Справедливый коэффициент: `fair_odd = 1 / avg_prob`
3. **Поиск валуя:**
   - Для каждой не-референсной конторы: `value = (bookmaker_odd / fair_odd - 1) * 100`
   - Если `value >= min_value_percent`, это валуй!
4. **Сортировка:** Результаты сортируются по `value_percent` (по убыванию)
