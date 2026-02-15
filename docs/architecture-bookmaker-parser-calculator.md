# Схема: букмекер-сервис → парсер → калькулятор

## 1. Текущая схема (футбол)

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│  BOOKMAKER-SERVICE (по одному инстансу на контору)                                │
│  cmd/bookmaker-service -parser=fonbet | -parser=xbet1 | ...                       │
├─────────────────────────────────────────────────────────────────────────────────┤
│  • Один парсер в процессе (Fonbet или xbet1 и т.д.)                               │
│  • Парсер дергает API конторы → собирает models.Match (футбол)                    │
│  • health.AddMatch(match) → InMemoryMatchStore (map[matchID]*models.Match)        │
│  • HTTP: GET /matches → JSON { "matches": []models.Match, "meta": {...} }         │
│  • HTTP: GET /parse   → запуск цикла парсинга (incremental или runOnce)           │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                    GET /matches (агрегатор тянет со всех контор)
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│  PARSER (оркестратор или локальный)                                              │
│  cmd/parser                                                                       │
├─────────────────────────────────────────────────────────────────────────────────┤
│  Режим A — оркестратор (bookmaker_services задан в конфиге):                      │
│    • Локальных парсеров нет                                                        │
│    • GetMatchesFunc = AggregateMatches: параллельно GET {url}/matches по каждому  │
│      bookmaker_services (fonbet→http://..., xbet1→http://...), затем MergeMatchLists│
│    • GET /parse проксируется на GET {service}/parse по имени парсера              │
│  Режим B — локальный (bookmaker_services пустой):                                 │
│    • В процессе крутятся включённые парсеры (enabled_parsers)                     │
│    • Каждый парсер сам кладёт матчи в health.AddMatch                             │
│    • GetMatchesFunc = health.GetMatches (читает общий InMemoryMatchStore)          │
├─────────────────────────────────────────────────────────────────────────────────┤
│  Ответ GET /matches: { "matches": []models.Match, "meta": { count, duration } }   │
│  Модель: Match { ID, HomeTeam, AwayTeam, StartTime, Sport, Tournament, Events[] }│
│          Event { EventType, MarketName, Outcomes[] }                               │
│          Outcome { OutcomeType, Parameter, Odds, Bookmaker }                      │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                        value_calculator.parser_url → GET /matches
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│  CALCULATOR                                                                       │
│  cmd/calculator                                                                   │
├─────────────────────────────────────────────────────────────────────────────────┤
│  • HTTPMatchesClient.GetMatches(ctx) → GET parser_url/matches                     │
│  • Получает []models.Match                                                        │
│  • computeTopDiffs(matches) — сравнение коэффициентов контор, поиск value/diffs  │
│  • Match.Sport участвует в группировке (matcher: sport|team1|team2|time)           │
│  • Line movement: StoreOddsSnapshot(..., sport, ...), алерты по просадке линии    │
│  • Алерты в Telegram, сохранение диффов в Postgres (если async_enabled)           │
└─────────────────────────────────────────────────────────────────────────────────┘
```

Итого по футболу:
- **Букмекер-сервис**: парсер → `models.Match` → `health.AddMatch` → **/matches** отдаёт футбольные матчи.
- **Парсер**: либо агрегирует /matches с нескольких букмекер-сервисов, либо сам парсит и хранит в памяти.
- **Калькулятор**: забирает /matches по `parser_url`, считает value/diffs по `models.Match`, использует `Match.Sport` в ключах и снимках.

---

## 2. Схема при подключении киберспорта (новая модель)

Киберспорт выносим в отдельную модель **models.EsportsMatch**, чтобы не смешивать с футбольной **models.Match**. Унифицированная линия **line.Match** используется только как промежуточный слой при маппинге API конторы → esports.

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│  BOOKMAKER-SERVICE (тот же бинарь, тот же парсер по имени)                        │
│  Для киберспорта: те же fonbet/xbet1, но в конфиге добавлены спорты/ sport_ids    │
├─────────────────────────────────────────────────────────────────────────────────┤
│  В парсере (после доработки):                                                     │
│    • По sport alias (Fonbet: dota2/cs) или sport_id (xbet: 40) определяется        │
│      «это киберспорт».                                                            │
│    • Вместо сборки models.Match парсер собирает line.Match (HomeTeam, AwayTeam,  │
│      Sport=dota2|cs, Markets[]).                                                  │
│    • line.Match → ToEsportsMatch() → models.EsportsMatch                          │
│    • health.AddEsportsMatch(em) → отдельное хранилище (см. ниже)                  │
│  Футбол (без изменений):                                                          │
│    • Как сейчас: API → models.Match → health.AddMatch(match)                       │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
          GET /matches        (футбол, как сейчас)
          GET /esports/matches (новый endpoint — киберспорт)
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│  PARSER                                                                           │
├─────────────────────────────────────────────────────────────────────────────────┤
│  Хранилище:                                                                       │
│    • InMemoryMatchStore       — map[string]*models.Match (футбол)                 │
│    • InMemoryEsportsStore     — map[string]*models.EsportsMatch (киберспорт) [NEW]│
│  Агрегатор (оркестратор):                                                         │
│    • GET /matches         → как сейчас (только футбол или merge из контор)        │
│    • GET /esports/matches → запрос к каждому bookmaker-service /esports/matches, │
│                             merge по match ID [NEW]                               │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
              parser_url/matches        parser_url/esports/matches (опционально)
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│  CALCULATOR                                                                       │
├─────────────────────────────────────────────────────────────────────────────────┤
│  Футбол (без изменений):                                                          │
│    • GetMatches() → []models.Match → computeTopDiffs, line movement, алерты       │
│  Киберспорт (при подключении) [NEW]:                                              │
│    • Конфиг: parser_esports_url или тот же parser_url + второй запрос             │
│    • GetEsportsMatches() → []models.EsportsMatch                                  │
│    • Отдельный пайплайн: computeEsportsDiffs(esportsMatches), свои алерты/снапшоты│
│    • Или общий пайплайн с веткой по типу (Match vs EsportsMatch) и общим хранилищем│
└─────────────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Что нужно добавить, чтобы «подключить» киберспорт

| Место | Изменение |
|-------|-----------|
| **Парсер (Fonbet/xbet1)** | При обработке спорта dota2/cs (Fonbet) или sport_id=40 (xbet): собирать **line.Match**, вызывать **ToEsportsMatch()**, отдавать результат в новое хранилище; футбол по-прежнему → **models.Match** и **AddMatch**. |
| **health (store)** | Ввести **InMemoryEsportsStore**, **AddEsportsMatch**, **GetEsportsMatches**, **MergeEsportsMatchLists** (по аналогии с Match). |
| **health (handlers)** | Добавить **GET /esports/matches** → JSON `{ "matches": []EsportsMatch }`. |
| **health (remote)** | В режиме оркестратора: **AggregateEsportsMatches** — GET `{url}/esports/matches` по каждому сервису, merge. |
| **calculator** | Опционально: **parser_esports_url** или флаг «киберспорт включён»; **HTTPMatchesClient.GetEsportsMatches**; отдельный/расширенный compute и алерты для **EsportsMatch** (по Markets/Outcomes). |
| **Конфиг** | При необходимости: отдельный URL для esports или один parser_url с двумя типами запросов (/matches и /esports/matches). |

---

## 4. Поток данных в одном предложении

- **Сейчас (футбол):** букмекер-сервис парсит → **models.Match** → in-memory store → парсер отдаёт **/matches** → калькулятор тянет **/matches** и считает value/diffs по **Match**.
- **После подключения киберспорта:** тот же букмекер-сервис для dota2/cs парсит → **line.Match** → **ToEsportsMatch()** → **models.EsportsMatch** → отдельный store → **/esports/matches** → калькулятор при включённом киберспорте тянет **/esports/matches** и считает по **EsportsMatch** в отдельном (или расширенном) пайплайне; футбол продолжает идти по старой схеме без изменений.
