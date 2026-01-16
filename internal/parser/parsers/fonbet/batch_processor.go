package fonbet

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
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
) interfaces.EventProcessor {
	return &BatchProcessor{
		storage:      storage,
		eventFetcher: eventFetcher,
		oddsParser:   oddsParser,
		matchBuilder: matchBuilder,
		batchSize:    50,  // Начальный размер батча
		workers:      5,   // 5 параллельных воркеров
		// Динамические параметры
		avgBatchTime:   0,
		targetBatchTime: 2 * time.Second, // Целевое время батча: 2 секунды
		minBatchSize:   10,  // Минимальный размер батча
		maxBatchSize:   200, // Максимальный размер батча
	}
}

// ProcessSportEvents processes events for a specific sport using batch operations
func (p *BatchProcessor) ProcessSportEvents(sport string) error {
	startTime := time.Now()
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

	// Group events by match (Level 1 events are main matches)
	groupStart := time.Now()
	eventsByMatch := p.groupEventsByMatchFromAPI(apiResponse.Events)
	groupDuration := time.Since(groupStart)
	fmt.Printf("⏱️  Event grouping took: %v\n", groupDuration)
	
	fmt.Printf("🏆 Found %d main matches\n", len(eventsByMatch))

	// Process matches in batches with parallel workers
	processStart := time.Now()
	processedCount := p.processMatchesInBatches(eventsByMatch, apiResponse.CustomFactors)
	processDuration := time.Since(processStart)
	
	totalDuration := time.Since(startTime)
	fmt.Printf("✅ Successfully processed %d matches for sport: %s\n", processedCount, sport)
	fmt.Printf("⏱️  Total timing: fetch=%v, parse=%v, group=%v, process=%v, total=%v\n", 
		fetchDuration, parseDuration, groupDuration, processDuration, totalDuration)
	
	return nil
}

// processMatchesInBatches processes matches in batches with parallel workers
func (p *BatchProcessor) processMatchesInBatches(
	eventsByMatch map[string][]FonbetAPIEvent, 
	customFactors []FonbetFactorGroup,
) int {
	// Convert to slice for batch processing with filtering
	matches := make([]MatchData, 0, len(eventsByMatch))
	filteredCount := 0
	
	for matchID, matchEvents := range eventsByMatch {
		if len(matchEvents) == 0 {
			continue
		}
		
		mainEvent := matchEvents[0]
		
		// Применяем фильтры к матчу
		if !p.isValidMatch(mainEvent) {
			filteredCount++
			continue
		}
		
		statisticalEvents := matchEvents[1:]
		matchFactors := p.getFactorsForMatch(matchID, customFactors)
		
		matches = append(matches, MatchData{
			ID:                matchID,
			MainEvent:         mainEvent,
			StatisticalEvents: statisticalEvents,
			Factors:           matchFactors,
		})
	}
	
	fmt.Printf("🔍 Filtered out %d matches with empty teams\n", filteredCount)
	
	fmt.Printf("🔄 Processing %d matches in batches of %d with %d workers\n", 
		len(matches), p.batchSize, p.workers)
	
	// Process in batches with dynamic sizing
	processedCount := 0
	for i := 0; i < len(matches); i += p.batchSize {
		end := i + p.batchSize
		if end > len(matches) {
			end = len(matches)
		}
		
		batch := matches[i:end]
		batchStart := time.Now()
		
		// Process batch with parallel workers
		count := p.processBatch(batch)
		processedCount += count
		
		batchDuration := time.Since(batchStart)
		fmt.Printf("⏱️  Batch %d-%d: %d matches in %v (batch_size=%d)\n", 
			i+1, end, count, batchDuration, p.batchSize)
		
		// Динамически корректируем размер батча
		p.adjustBatchSize(batchDuration)
	}
	
	return processedCount
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
	
	// Фильтр 5: Пропускаем матчи без названия
	if event.Name == "" {
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
	
	// Фильтр 7: Пропускаем матчи с очень короткими названиями (менее 5 символов)
	if len(event.Name) < 5 {
		return false
	}
	
	return true
}

// processBatch processes a batch of matches with parallel workers
func (p *BatchProcessor) processBatch(matches []MatchData) int {
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
	for result := range resultsChan {
		if result.Success {
			successCount++
		} else {
			fmt.Printf("❌ Failed to process match %s: %v\n", result.MatchID, result.Error)
		}
	}
	
	return successCount
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
	
	for match := range matchesChan {
		startTime := time.Now()
		
		// Process the match
		err := p.processMatchWithEventsAndFactors(
			match.MainEvent, 
			match.StatisticalEvents, 
			match.Factors,
		)
		
		duration := time.Since(startTime)
		
		resultsChan <- ProcessResult{
			MatchID: match.ID,
			Success: err == nil,
			Error:   err,
			Duration: duration,
		}
		
		if duration > 1*time.Second {
			fmt.Printf("⏱️  Worker: Match %s took %v\n", match.ID, duration)
		}
	}
}

// MatchData represents a match with its events and factors
type MatchData struct {
	ID                string
	MainEvent         FonbetAPIEvent
	StatisticalEvents []FonbetAPIEvent
	Factors           []FonbetFactor
}

// ProcessResult represents the result of processing a match
type ProcessResult struct {
	MatchID  string
	Success  bool
	Error    error
	Duration time.Duration
}

// groupEventsByMatchFromAPI groups events by their parent match ID from API response
func (p *BatchProcessor) groupEventsByMatchFromAPI(events []FonbetAPIEvent) map[string][]FonbetAPIEvent {
	groups := make(map[string][]FonbetAPIEvent)
	
	// First, find all main matches (Level 1)
	mainMatches := make(map[string]FonbetAPIEvent)
	for _, event := range events {
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

// getFactorsForMatch gets factors for a specific match from the main API response
func (p *BatchProcessor) getFactorsForMatch(matchID string, customFactors []FonbetFactorGroup) []FonbetFactor {
	matchIDInt, err := strconv.ParseInt(matchID, 10, 64)
	if err != nil {
		return []FonbetFactor{}
	}

	// Find factors for this specific match
	for _, factorGroup := range customFactors {
		if factorGroup.EventID == matchIDInt {
			return factorGroup.Factors
		}
	}

	return []FonbetFactor{}
}

// processMatchWithEventsAndFactors processes a main match with all its statistical events and factors
func (p *BatchProcessor) processMatchWithEventsAndFactors(
	mainEvent FonbetAPIEvent, 
	statisticalEvents []FonbetAPIEvent, 
	factors []FonbetFactor,
) error {
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
	factorsInterface := make([]interface{}, len(factors))
	for i, factor := range factors {
		factorsInterface[i] = factor
	}

	// Use match builder to create the match
	matchBuilder := NewMatchBuilder("Fonbet")
	match, err := matchBuilder.BuildMatch(mainFonbetEvent, statEvents, factorsInterface)
	if err != nil {
		return fmt.Errorf("failed to build match: %w", err)
	}

	if matchModel, ok := (*match).(*models.Match); ok {
		// Storage is optional: if YDB client failed to initialize, we still want
		// the parser to keep running (e.g. for debugging / dry-run parsing).
		if p.storage == nil {
			return nil
		}
		// Use batch storage for better performance
		if batchStorage, ok := p.storage.(*storage.BatchYDBClient); ok {
			if err := batchStorage.StoreMatchBatch(context.Background(), matchModel); err != nil {
				return fmt.Errorf("failed to store match batch: %w", err)
			}
		} else {
			// Fallback to regular storage
			if err := p.storage.StoreMatch(context.Background(), matchModel); err != nil {
				return fmt.Errorf("failed to store match: %w", err)
			}
		}

		return nil
	}

	return fmt.Errorf("failed to convert match")
}

// ProcessEvent - legacy method for compatibility
func (p *BatchProcessor) ProcessEvent(event interface{}) error {
	return fmt.Errorf("ProcessEvent not supported in batch processor")
}

// ProcessEvents - legacy method for compatibility  
func (p *BatchProcessor) ProcessEvents(events []interface{}) error {
	return fmt.Errorf("ProcessEvents not supported in batch processor")
}


