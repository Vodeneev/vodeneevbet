package fonbet

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/performance"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

// BatchProcessor handles processing events with batch operations and parallel processing
type BatchProcessor struct {
	storage      interfaces.Storage
	eventFetcher interfaces.EventFetcher
	oddsParser   interfaces.OddsParser
	matchBuilder interfaces.MatchBuilder
	batchSize    int
	workers      int
	testLimit    int
	// Динамические параметры
	avgBatchTime time.Duration
	targetBatchTime time.Duration
	minBatchSize int
	maxBatchSize int
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(
	storage interfaces.Storage,
	eventFetcher interfaces.EventFetcher,
	oddsParser interfaces.OddsParser,
	matchBuilder interfaces.MatchBuilder,
	testLimit int,
) interfaces.EventProcessor {
	return &BatchProcessor{
		storage:      storage,
		eventFetcher: eventFetcher,
		oddsParser:   oddsParser,
		matchBuilder: matchBuilder,
		batchSize:    100, // Увеличен начальный размер батча для лучшей производительности
		workers:      5,   // Увеличено количество воркеров (bulk операции более эффективны)
		testLimit:    testLimit,
		// Динамические параметры
		avgBatchTime:   0,
		targetBatchTime: 3 * time.Second, // Увеличено целевое время батча (bulk операции быстрее)
		minBatchSize:   20,  // Увеличено минимальный размер батча
		maxBatchSize:   300, // Увеличено максимальный размер батча
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
	
	fmt.Printf("🚀 Starting batch processing for sport: %s\n", sport)
	
	// Fetch events for the sport (single HTTP request)
	fetchStart := time.Now()
	eventsData, err := p.eventFetcher.FetchEvents(sport)
	if err != nil {
		return fmt.Errorf("failed to fetch events for sport %s: %w", sport, err)
	}
	fetchDuration := time.Since(fetchStart)
	fmt.Printf("⏱️  HTTP fetch took: %v\n", fetchDuration)

	// Parse the complete API response
	parseStart := time.Now()
	var apiResponse FonbetAPIResponse
	if err := json.Unmarshal(eventsData, &apiResponse); err != nil {
		return fmt.Errorf("failed to unmarshal API response: %w", err)
	}
	parseDuration := time.Since(parseStart)
	fmt.Printf("⏱️  JSON parsing took: %v\n", parseDuration)

	fmt.Printf("📊 Found %d events and %d factor groups in API response\n", 
		len(apiResponse.Events), len(apiResponse.CustomFactors))

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
	fmt.Printf("⏱️  Event grouping took: %v\n", groupDuration)
	
	fmt.Printf("🏆 Found %d main matches\n", len(eventsByMatch))

	// Process matches in batches with parallel workers
	processStart := time.Now()
	processedCount, totalEvents, totalOutcomes, ydbWriteTime := p.processMatchesInBatches(eventsByMatch, factorsByEventID)
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
	
	fmt.Printf("✅ Successfully processed %d matches for sport: %s\n", processedCount, sport)
	fmt.Printf("⏱️  Total timing: fetch=%v, parse=%v, group=%v, process=%v, ydb_write=%v, total=%v\n", 
		fetchDuration, parseDuration, groupDuration, processDuration, ydbWriteTime, totalDuration)
	fmt.Printf("📈 Stats: %d events, %d outcomes processed\n", totalEvents, totalOutcomes)
	
	return nil
}

// processMatchesInBatches processes matches in batches with parallel workers
// Returns: processedCount, totalEvents, totalOutcomes, ydbWriteTime
func (p *BatchProcessor) processMatchesInBatches(
	eventsByMatch map[string][]FonbetAPIEvent, 
	factorsByEventID map[int64]FonbetFactorGroup,
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
		})
	}
	
	fmt.Printf("🔍 Filtered out %d matches (invalid teams/name)\n", filteredCount)

	if p.testLimit > 0 && len(matches) > p.testLimit {
		fmt.Printf("🧪 Test limit enabled: processing first %d matches (out of %d)\n", p.testLimit, len(matches))
		matches = matches[:p.testLimit]
	}
	
	fmt.Printf("🔄 Processing %d matches in batches of %d with %d workers\n", 
		len(matches), p.batchSize, p.workers)
	
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
		fmt.Printf("⏱️  Batch %d-%d: %d matches, %d events, %d outcomes in %v (ydb_write=%v, batch_size=%d)\n", 
			i+1, end, count, events, outcomes, batchDuration, ydbTime, p.batchSize)
		
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
		} else {
			fmt.Printf("❌ Failed to process match %s: %v\n", result.MatchID, result.Error)
		}
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
			fmt.Printf("📉 Reducing batch size: %d -> %d (batch took %v, target: %v)\n", 
				p.batchSize, newSize, batchDuration, p.targetBatchTime)
			p.batchSize = newSize
		}
	} else if batchDuration < time.Duration(float64(p.targetBatchTime)*0.5) {
		// Батч слишком быстрый - увеличиваем размер
		newSize := int(float64(p.batchSize) * 1.2)
		if newSize > p.maxBatchSize {
			newSize = p.maxBatchSize
		}
		if newSize != p.batchSize {
			fmt.Printf("📈 Increasing batch size: %d -> %d (batch took %v, target: %v)\n", 
				p.batchSize, newSize, batchDuration, p.targetBatchTime)
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
		
		// Process the match
		matchModel, err := p.buildMatchWithEventsAndFactors(
			match.MainEvent, 
			match.StatisticalEvents, 
			match.FactorGroups,
		)
		
		buildTime := time.Since(buildStart)
		var storeTime time.Duration
		var eventsCount, outcomesCount int
		
		if err == nil && matchModel != nil {
			eventsCount = len(matchModel.Events)
			for _, event := range matchModel.Events {
				outcomesCount += len(event.Outcomes)
			}
			
			// Store the match
			storeStart := time.Now()
			if p.storage != nil {
				if batchStorage, ok := p.storage.(*storage.BatchYDBClient); ok {
					err = batchStorage.StoreMatchBatch(context.Background(), matchModel)
				} else {
					err = p.storage.StoreMatch(context.Background(), matchModel)
				}
			}
			storeTime = time.Since(storeStart)
			
			// Record match timing
			tracker.RecordMatch(match.ID, eventsCount, outcomesCount, buildTime, storeTime, time.Since(startTime), err == nil)
		}
		
		duration := time.Since(startTime)
		
		resultsChan <- ProcessResult{
			MatchID:       match.ID,
			Success:       err == nil,
			Error:         err,
			Duration:      duration,
			EventsCount:   eventsCount,
			OutcomesCount: outcomesCount,
			YDBWriteTime:  storeTime,
		}
		
		if duration > 1*time.Second {
			fmt.Printf("⏱️  Worker: Match %s took %v (build=%v, store=%v, events=%d, outcomes=%d)\n", 
				match.ID, duration, buildTime, storeTime, eventsCount, outcomesCount)
		}
	}
}

// MatchData represents a match with its events and factors
type MatchData struct {
	ID                string
	MainEvent         FonbetAPIEvent
	StatisticalEvents []FonbetAPIEvent
	FactorGroups      []FonbetFactorGroup
}

// ProcessResult represents the result of processing a match
type ProcessResult struct {
	MatchID      string
	Success      bool
	Error        error
	Duration     time.Duration
	EventsCount  int
	OutcomesCount int
	YDBWriteTime time.Duration
}

func (p *BatchProcessor) getAllowedSportIDs(sports []FonbetSport, sportAlias string) map[int64]struct{} {
	// Find top-level sport category id.
	sportCategoryID := 0
	for _, s := range sports {
		if s.Kind == "sport" && s.Alias == sportAlias {
			sportCategoryID = s.ID
			break
		}
	}
	if sportCategoryID == 0 {
		return nil
	}

	allowed := make(map[int64]struct{}, len(sports))
	for _, s := range sports {
		// "segment" entries carry sportCategoryId which points to the top-level sport id.
		if s.SportCategoryID == sportCategoryID {
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
	matchBuilder := NewMatchBuilder("Fonbet")
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


