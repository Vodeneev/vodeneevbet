package fonbet

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/health"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/performance"
)

// BatchProcessor handles processing events with batch operations and parallel processing
type BatchProcessor struct {
	storage      interfaces.Storage
	eventFetcher interfaces.EventFetcher
	oddsParser   interfaces.OddsParser
	matchBuilder interfaces.MatchBuilder
	batchSize    int
	workers      int
	// Динамические параметры
	avgBatchTime    time.Duration
	targetBatchTime time.Duration
	minBatchSize    int
	maxBatchSize    int
	// lastProcessedCount — количество матчей, обработанных в последнем вызове ProcessSportEvents
	lastProcessedCount atomic.Int64
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(
	storage interfaces.Storage,
	eventFetcher interfaces.EventFetcher,
	oddsParser interfaces.OddsParser,
	matchBuilder interfaces.MatchBuilder,
) interfaces.EventProcessor {
	return &BatchProcessor{
		storage:      storage,
		eventFetcher: eventFetcher,
		oddsParser:   oddsParser,
		matchBuilder: matchBuilder,
		batchSize:    100, // Увеличен начальный размер батча для лучшей производительности
		workers:      5,   // Увеличено количество воркеров (bulk операции более эффективны)
		// Динамические параметры
		avgBatchTime:    0,
		targetBatchTime: 3 * time.Second, // Увеличено целевое время батча (bulk операции быстрее)
		minBatchSize:    20,              // Увеличено минимальный размер батча
		maxBatchSize:    300,             // Увеличено максимальный размер батча
	}
}

func splitTeamsFromName(name string) (string, string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", false
	}

	// Common separators in match names.
	separators := []string{" vs ", " - ", " — ", " – "}
	for _, sep := range separators {
		parts := strings.Split(name, sep)
		if len(parts) != 2 {
			continue
		}
		home := strings.TrimSpace(parts[0])
		away := strings.TrimSpace(parts[1])
		if home == "" || away == "" {
			return "", "", false
		}
		return home, away, true
	}

	return "", "", false
}

func (p *BatchProcessor) normalizeMainEventTeams(event *FonbetAPIEvent) {
	if event == nil {
		return
	}
	if event.Team1 != "" && event.Team2 != "" {
		return
	}

	home, away, ok := splitTeamsFromName(event.Name)
	if !ok {
		return
	}
	if event.Team1 == "" {
		event.Team1 = home
	}
	if event.Team2 == "" {
		event.Team2 = away
	}
}

// ProcessSportEvents processes events for a specific sport using batch operations
func (p *BatchProcessor) ProcessSportEvents(sport string) error {
	startTime := time.Now()
	tracker := performance.GetTracker()

	slog.Info(fmt.Sprintf("Fonbet: Processing sport %s", sport))

	// Fetch events for the sport (single HTTP request)
	fetchStart := time.Now()
	eventsData, err := p.eventFetcher.FetchEvents(sport)
	if err != nil {
		return fmt.Errorf("failed to fetch events for sport %s: %w", sport, err)
	}
	fetchDuration := time.Since(fetchStart)
	slog.Debug("HTTP fetch completed", "duration", fetchDuration)

	// Parse the complete API response
	parseStart := time.Now()
	var apiResponse FonbetAPIResponse
	if err := json.Unmarshal(eventsData, &apiResponse); err != nil {
		return fmt.Errorf("failed to unmarshal API response: %w", err)
	}
	parseDuration := time.Since(parseStart)
	slog.Debug("JSON parsing completed", "duration", parseDuration)

	slog.Debug("Found events and factor groups", "events", len(apiResponse.Events), "factor_groups", len(apiResponse.CustomFactors))

	// Index custom factors by event id for fast lookup.
	factorsByEventID := make(map[int64]FonbetFactorGroup, len(apiResponse.CustomFactors))
	for _, g := range apiResponse.CustomFactors {
		factorsByEventID[g.EventID] = g
	}

	// Build allowed sport IDs for requested sport (Fonbet uses hierarchical sport IDs;
	// events often reference a "segment" id that belongs to a top-level sport category).
	allowedSportIDs := p.getAllowedSportIDs(apiResponse.Sports, sport)

	// Group events by match (Level 1 events are main matches)
	groupStart := time.Now()

	eventsByMatch := p.groupEventsByMatchFromAPI(apiResponse.Events, allowedSportIDs)
	groupDuration := time.Since(groupStart)
	slog.Debug("Event grouping completed", "duration", groupDuration)

	slog.Info(fmt.Sprintf("Fonbet: Found main matches %d", len(eventsByMatch)))

	// Process matches in batches with parallel workers
	processStart := time.Now()
	processedCount, totalEvents, totalOutcomes, ydbWriteTime := p.processMatchesInBatches(eventsByMatch, factorsByEventID, sport)
	processDuration := time.Since(processStart)

	totalDuration := time.Since(startTime)

	// Record metrics
	tracker.RecordRun(
		fetchDuration,
		parseDuration,
		groupDuration,
		processDuration,
		ydbWriteTime,
		totalDuration,
		processedCount,
		totalEvents,
		totalOutcomes,
	)

	p.lastProcessedCount.Store(int64(processedCount))
	slog.Info(fmt.Sprintf("Fonbet: Successfully processed matches %d (%s)", processedCount, sport))
	slog.Debug("Total timing", "fetch", fetchDuration, "parse", parseDuration, "group", groupDuration, "process", processDuration, "total", totalDuration)
	slog.Debug("Stats", "events", totalEvents, "outcomes", totalOutcomes)

	return nil
}

// LastProcessedCount возвращает количество матчей, обработанных в последнем вызове ProcessSportEvents.
func (p *BatchProcessor) LastProcessedCount() int {
	return int(p.lastProcessedCount.Load())
}

// processMatchesInBatches processes matches in batches with parallel workers
// Returns: processedCount, totalEvents, totalOutcomes, ydbWriteTime
func (p *BatchProcessor) processMatchesInBatches(
	eventsByMatch map[string][]FonbetAPIEvent,
	factorsByEventID map[int64]FonbetFactorGroup,
	sport string,
) (int, int, int, time.Duration) {
	// Convert to slice for batch processing with filtering
	matches := make([]MatchData, 0, len(eventsByMatch))
	filteredCount := 0

	for matchID, matchEvents := range eventsByMatch {
		if len(matchEvents) == 0 {
			continue
		}

		mainEvent := matchEvents[0]
		p.normalizeMainEventTeams(&mainEvent)

		// Применяем фильтры к матчу
		if !p.isValidMatch(mainEvent) {
			filteredCount++
			continue
		}

		statisticalEvents := matchEvents[1:]
		// Ensure stat events have teams too (often missing in API list response).
		for i := range statisticalEvents {
			if statisticalEvents[i].Team1 == "" {
				statisticalEvents[i].Team1 = mainEvent.Team1
			}
			if statisticalEvents[i].Team2 == "" {
				statisticalEvents[i].Team2 = mainEvent.Team2
			}
		}

		// Collect factor groups for main + statistical events (if available).
		factorGroups := make([]FonbetFactorGroup, 0, 1+len(statisticalEvents))
		if g, ok := factorsByEventID[mainEvent.ID]; ok {
			factorGroups = append(factorGroups, g)
		}
		for _, se := range statisticalEvents {
			if g, ok := factorsByEventID[se.ID]; ok {
				factorGroups = append(factorGroups, g)
			}
		}

		matches = append(matches, MatchData{
			ID:                matchID,
			MainEvent:         mainEvent,
			StatisticalEvents: statisticalEvents,
			FactorGroups:      factorGroups,
			Sport:             sport,
		})
	}

	slog.Debug("Filtered out matches", "count", filteredCount, "reason", "invalid teams/name")

	slog.Debug("Processing matches in batches", "total", len(matches), "batch_size", p.batchSize, "workers", p.workers)

	// Process in batches with dynamic sizing
	processedCount := 0
	totalEvents := 0
	totalOutcomes := 0
	var totalYDBWriteTime time.Duration

	for i := 0; i < len(matches); i += p.batchSize {
		end := i + p.batchSize
		if end > len(matches) {
			end = len(matches)
		}

		batch := matches[i:end]
		batchStart := time.Now()

		// Process batch with parallel workers
		count, events, outcomes, ydbTime := p.processBatch(batch)
		processedCount += count
		totalEvents += events
		totalOutcomes += outcomes
		totalYDBWriteTime += ydbTime

		batchDuration := time.Since(batchStart)
		slog.Debug("Batch processed", "start", i+1, "end", end, "matches", count, "events", events, "outcomes", outcomes, "duration", batchDuration, "batch_size", p.batchSize)

		// Динамически корректируем размер батча
		p.adjustBatchSize(batchDuration)
	}

	return processedCount, totalEvents, totalOutcomes, totalYDBWriteTime
}

// isValidMatch проверяет, является ли матч валидным для обработки
func (p *BatchProcessor) isValidMatch(event FonbetAPIEvent) bool {
	// Фильтр 1: Пропускаем матчи с пустыми командами
	if event.Team1 == "" || event.Team2 == "" {
		return false
	}

	// Фильтр 2: Пропускаем матчи с командами "vs" или пустыми названиями
	if event.Team1 == "vs" || event.Team2 == "vs" {
		return false
	}

	// Фильтр 3: Пропускаем матчи с очень короткими названиями команд (менее 2 символов)
	if len(event.Team1) < 2 || len(event.Team2) < 2 {
		return false
	}

	// Фильтр 4: Пропускаем матчи с одинаковыми командами
	if event.Team1 == event.Team2 {
		return false
	}

	// Фильтр 6: Пропускаем матчи с общими названиями команд
	genericTeams := []string{
		"Хозяева", "Гости", "Home", "Away", "Team 1", "Team 2", "TBD", "vs",
	}

	for _, genericTeam := range genericTeams {
		if event.Team1 == genericTeam || event.Team2 == genericTeam {
			return false
		}
	}

	// Фильтр 7: если имя есть — отбрасываем совсем короткие; пустое имя допускаем
	// (у Fonbet в некоторых ответах `name` бывает пустым при наличии team1/team2).
	if event.Name != "" && len(event.Name) < 5 {
		return false
	}

	return true
}

// processBatch processes a batch of matches with parallel workers
// Returns: successCount, totalEvents, totalOutcomes, totalYDBWriteTime
func (p *BatchProcessor) processBatch(matches []MatchData) (int, int, int, time.Duration) {
	// Create channels for parallel processing
	matchesChan := make(chan MatchData, len(matches))
	resultsChan := make(chan ProcessResult, len(matches))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < p.workers; i++ {
		wg.Add(1)
		go p.worker(matchesChan, resultsChan, &wg)
	}

	// Send matches to workers
	for _, match := range matches {
		matchesChan <- match
	}
	close(matchesChan)

	// Wait for all workers to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results
	successCount := 0
	totalEvents := 0
	totalOutcomes := 0
	var totalYDBWriteTime time.Duration

	for result := range resultsChan {
		if result.Success {
			successCount++
			totalEvents += result.EventsCount
			totalOutcomes += result.OutcomesCount
			totalYDBWriteTime += result.YDBWriteTime
		}
		// Silently skip failed matches (e.g., live matches that are filtered)
	}

	return successCount, totalEvents, totalOutcomes, totalYDBWriteTime
}

// adjustBatchSize dynamically adjusts batch size based on performance
func (p *BatchProcessor) adjustBatchSize(batchDuration time.Duration) {
	// Обновляем среднее время батча
	if p.avgBatchTime == 0 {
		p.avgBatchTime = batchDuration
	} else {
		// Экспоненциальное скользящее среднее
		p.avgBatchTime = time.Duration(0.7*float64(p.avgBatchTime) + 0.3*float64(batchDuration))
	}

	// Корректируем размер батча
	if batchDuration > time.Duration(float64(p.targetBatchTime)*1.5) {
		// Батч слишком медленный - уменьшаем размер
		newSize := int(float64(p.batchSize) * 0.8)
		if newSize < p.minBatchSize {
			newSize = p.minBatchSize
		}
		if newSize != p.batchSize {
			slog.Debug("Reducing batch size", "from", p.batchSize, "to", newSize, "batch_duration", batchDuration, "target", p.targetBatchTime)
			p.batchSize = newSize
		}
	} else if batchDuration < time.Duration(float64(p.targetBatchTime)*0.5) {
		// Батч слишком быстрый - увеличиваем размер
		newSize := int(float64(p.batchSize) * 1.2)
		if newSize > p.maxBatchSize {
			newSize = p.maxBatchSize
		}
		if newSize != p.batchSize {
			slog.Debug("Increasing batch size", "from", p.batchSize, "to", newSize, "batch_duration", batchDuration, "target", p.targetBatchTime)
			p.batchSize = newSize
		}
	}
}

// worker processes matches from the channel
func (p *BatchProcessor) worker(
	matchesChan <-chan MatchData,
	resultsChan chan<- ProcessResult,
	wg *sync.WaitGroup,
) {
	defer wg.Done()
	tracker := performance.GetTracker()

	for match := range matchesChan {
		startTime := time.Now()
		buildStart := time.Now()

		// Strictly exclude live matches (matches that have already started)
		matchStartTime := time.Unix(match.MainEvent.StartTime, 0).UTC()
		now := time.Now().UTC()
		if !matchStartTime.After(now) {
			// Match has already started, skip it
			slog.Debug("Fonbet: filtered live match", "match_id", match.ID, "start", matchStartTime.Format(time.RFC3339), "now", now.Format(time.RFC3339))
			resultsChan <- ProcessResult{
				MatchID:  match.ID,
				Success:  false,
				Error:     fmt.Errorf("live match filtered"),
				Duration:  time.Since(startTime),
				EventsCount: 0,
				OutcomesCount: 0,
				YDBWriteTime: 0,
			}
			continue
		}

		buildTime := time.Since(buildStart)
		var eventsCount, outcomesCount int
		var success bool
		var err error

		// Киберспорт (dota2, cs) → отдельная модель EsportsMatch, не футбольная
		if match.Sport == "dota2" || match.Sport == "cs" {
			var mainFactors []FonbetFactor
			for _, g := range match.FactorGroups {
				if g.EventID == match.MainEvent.ID {
					mainFactors = g.Factors
					break
				}
			}
			lineMatch := BuildEsportsLineMatch(match.MainEvent, mainFactors, match.Sport, "Unknown Tournament", "fonbet")
			if lineMatch != nil {
				em := lineMatch.ToEsportsMatch()
				if em != nil {
					health.AddEsportsMatch(em)
					eventsCount = len(em.Markets)
					for _, mk := range em.Markets {
						outcomesCount += len(mk.Outcomes)
					}
					success = true
					tracker.RecordMatch(match.ID, eventsCount, outcomesCount, buildTime, 0, time.Since(startTime), true)
				}
			}
		} else {
			// Футбол: текущий путь
			var matchModel *models.Match
			matchModel, err = p.buildMatchWithEventsAndFactors(
				match.MainEvent,
				match.StatisticalEvents,
				match.FactorGroups,
			)
			if err == nil && matchModel != nil {
				eventsCount = len(matchModel.Events)
				for _, event := range matchModel.Events {
					outcomesCount += len(event.Outcomes)
				}
				success = true
				health.AddMatch(matchModel)
				tracker.RecordMatch(match.ID, eventsCount, outcomesCount, buildTime, 0, time.Since(startTime), true)
			}
		}

		duration := time.Since(startTime)

		resultsChan <- ProcessResult{
			MatchID:       match.ID,
			Success:       success,
			Error:         err,
			Duration:      duration,
			EventsCount:   eventsCount,
			OutcomesCount: outcomesCount,
			YDBWriteTime:  0,
		}

		if duration > 1*time.Second {
			slog.Debug("Worker match processing", "match_id", match.ID, "duration", duration, "build_time", buildTime, "events", eventsCount, "outcomes", outcomesCount)
		}
	}
}

// MatchData represents a match with its events and factors
type MatchData struct {
	ID                string
	MainEvent         FonbetAPIEvent
	StatisticalEvents []FonbetAPIEvent
	FactorGroups      []FonbetFactorGroup
	Sport             string // football, dota2, cs — для ветки esports
}

// ProcessResult represents the result of processing a match
type ProcessResult struct {
	MatchID       string
	Success       bool
	Error         error
	Duration      time.Duration
	EventsCount   int
	OutcomesCount int
	YDBWriteTime  time.Duration
}

// fonbetEsportCategoryID returns Fonbet sportCategoryId for esports (19=Dota2, 20=CS).
// В API Fonbet нет топ-уровня с alias "dota2"/"cs", сегменты приходят с sportCategoryId 19/20.
func fonbetEsportCategoryID(sportAlias string) int {
	switch sportAlias {
	case "dota2":
		return 19
	case "cs":
		return 20
	default:
		return 0
	}
}

func (p *BatchProcessor) getAllowedSportIDs(sports []FonbetSport, sportAlias string) map[int64]struct{} {
	// Find top-level sport category id by alias (football, hockey, etc.)
	sportCategoryID := 0
	for _, s := range sports {
		if s.Kind == "sport" && s.Alias == sportAlias {
			sportCategoryID = s.ID
			break
		}
	}
	// Киберспорт: в Fonbet сегменты Dota2/CS имеют sportCategoryId 19/20, топ-уровня с alias "dota2"/"cs" нет
	if sportCategoryID == 0 {
		sportCategoryID = fonbetEsportCategoryID(sportAlias)
	}
	if sportCategoryID == 0 {
		return nil
	}

	allowed := make(map[int64]struct{}, len(sports))
	esportCategory := sportCategoryID == 19 || sportCategoryID == 20 // Dota2, CS — не подмешивать сегменты с null category
	for _, s := range sports {
		// "segment" entries carry sportCategoryId which points to the top-level sport id.
		if s.SportCategoryID == sportCategoryID {
			allowed[int64(s.ID)] = struct{}{}
		}
		// Для футбола и др.: сегменты с пустым sportCategoryId допускаем (могут относиться к спорту).
		// Для киберспорта (19/20) НЕ добавляем сегменты с null — иначе в dota2/cs попадают футбольные матчи.
		if !esportCategory && s.Kind == "segment" && s.SportCategoryID == 0 && sportCategoryID > 0 {
			allowed[int64(s.ID)] = struct{}{}
		}
	}

	// Include the category id itself as a safety net (some responses may use it directly).
	allowed[int64(sportCategoryID)] = struct{}{}
	return allowed
}

// groupEventsByMatchFromAPI groups events by their parent match ID from API response
func (p *BatchProcessor) groupEventsByMatchFromAPI(events []FonbetAPIEvent, allowedSportIDs map[int64]struct{}) map[string][]FonbetAPIEvent {
	groups := make(map[string][]FonbetAPIEvent)

	// First, find all main matches (Level 1)
	mainMatches := make(map[string]FonbetAPIEvent)
	for _, event := range events {
		if len(allowedSportIDs) > 0 {
			if _, ok := allowedSportIDs[event.SportID]; !ok {
				continue
			}
		}
		if event.Level == 1 {
			matchID := fmt.Sprintf("%d", event.ID)
			mainMatches[matchID] = event
		}
	}

	// Then, for each main match, find all related events
	for matchID, mainMatch := range mainMatches {
		// Add the main match itself
		groups[matchID] = append(groups[matchID], mainMatch)

		// Find all statistical events for this match
		for _, event := range events {
			if len(allowedSportIDs) > 0 {
				if _, ok := allowedSportIDs[event.SportID]; !ok {
					continue
				}
			}
			if event.Level > 1 && event.ParentID > 0 {
				parentID := fmt.Sprintf("%d", event.ParentID)
				if parentID == matchID {
					groups[matchID] = append(groups[matchID], event)
				}
			}
		}
	}

	return groups
}

// buildMatchWithEventsAndFactors builds a match model from events and factors
// Returns the match model or error
func (p *BatchProcessor) buildMatchWithEventsAndFactors(
	mainEvent FonbetAPIEvent,
	statisticalEvents []FonbetAPIEvent,
	factorGroups []FonbetFactorGroup,
) (*models.Match, error) {
	// Convert main event to FonbetEvent
	mainFonbetEvent := FonbetEvent{
		ID:         fmt.Sprintf("%d", mainEvent.ID),
		Name:       mainEvent.Name,
		HomeTeam:   mainEvent.Team1,
		AwayTeam:   mainEvent.Team2,
		StartTime:  time.Unix(mainEvent.StartTime, 0),
		Category:   "football",
		Tournament: "Unknown Tournament",
		Kind:       mainEvent.Kind,
		RootKind:   mainEvent.RootKind,
		Level:      mainEvent.Level,
		ParentID:   mainEvent.ParentID,
	}

	// Convert statistical events to interface{}
	statEvents := make([]interface{}, len(statisticalEvents))
	for i, event := range statisticalEvents {
		fonbetEvent := FonbetEvent{
			ID:         fmt.Sprintf("%d", event.ID),
			Name:       event.Name,
			HomeTeam:   event.Team1,
			AwayTeam:   event.Team2,
			StartTime:  time.Unix(event.StartTime, 0),
			Category:   "football",
			Tournament: "Unknown Tournament",
			Kind:       event.Kind,
			RootKind:   event.RootKind,
			Level:      event.Level,
			ParentID:   event.ParentID,
		}
		statEvents[i] = fonbetEvent
	}

	// Convert factors to interface{}
	factorsInterface := make([]interface{}, len(factorGroups))
	for i, g := range factorGroups {
		factorsInterface[i] = g
	}

	// Use match builder to create the match
	matchBuilder := NewMatchBuilder("fonbet")
	match, err := matchBuilder.BuildMatch(mainFonbetEvent, statEvents, factorsInterface)
	if err != nil {
		return nil, fmt.Errorf("failed to build match: %w", err)
	}

	if matchModel, ok := (*match).(*models.Match); ok {
		return matchModel, nil
	}

	return nil, fmt.Errorf("failed to convert match")
}

func (p *BatchProcessor) ProcessEvent(event interface{}) error {
	return fmt.Errorf("ProcessEvent not supported in batch processor")
}

func (p *BatchProcessor) ProcessEvents(events []interface{}) error {
	return fmt.Errorf("ProcessEvents not supported in batch processor")
}
