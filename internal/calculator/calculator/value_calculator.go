package calculator

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"time"

	"vodeneevbet/internal/pkg/config"
	"vodeneevbet/internal/pkg/models"
	"vodeneevbet/internal/pkg/storage"
)

// ValueCalculator калькулятор для поиска валуйных ставок
type ValueCalculator struct {
	ydbClient *storage.YDBWorkingClient
	config    *config.ValueCalculatorConfig
}

// NewValueCalculator создает новый калькулятор Value Bet
func NewValueCalculator(ydbClient *storage.YDBWorkingClient, config *config.ValueCalculatorConfig) *ValueCalculator {
	return &ValueCalculator{
		ydbClient: ydbClient,
		config:    config,
	}
}

// Start запускает калькулятор
func (vc *ValueCalculator) Start(ctx context.Context) error {
	log.Printf("Starting Value Bet Calculator with interval: %s", vc.config.CheckInterval)
	
	// Парсим интервал
	interval, err := time.ParseDuration(vc.config.CheckInterval)
	if err != nil {
		// Для тестирования используем более частый интервал
		interval, err = time.ParseDuration(vc.config.TestInterval)
		if err != nil {
			interval = 30 * time.Second
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("Stopping Value Bet Calculator")
			return nil
		case <-ticker.C:
			if err := vc.findValueBets(ctx); err != nil {
				log.Printf("Error finding value bets: %v", err)
			}
		}
	}
}

// findValueBets ищет валуйные ставки
func (vc *ValueCalculator) findValueBets(ctx context.Context) error {
	log.Println("Searching for value bets...")
	
	// Получаем все матчи
	matches, err := vc.ydbClient.GetAllMatches(ctx)
	if err != nil {
		return fmt.Errorf("failed to get matches: %v", err)
	}

	if len(matches) == 0 {
		log.Println("No matches found")
		return nil
	}

	log.Printf("Found %d matches to analyze", len(matches))

	// Анализируем каждый матч
	for _, matchID := range matches {
		if err := vc.analyzeMatch(ctx, matchID); err != nil {
			log.Printf("Error analyzing match %s: %v", matchID, err)
		}
	}

	return nil
}

// analyzeMatch анализирует матч на предмет value bet
func (vc *ValueCalculator) analyzeMatch(ctx context.Context, matchID string) error {
	// Получаем все коэффициенты для матча
	odds, err := vc.ydbClient.GetOddsByMatch(ctx, matchID)
	if err != nil {
		return fmt.Errorf("failed to get odds for match %s: %v", matchID, err)
	}

	if len(odds) == 0 {
		return nil
	}

	// Группируем коэффициенты по рынкам
	oddsByMarket := vc.groupOddsByMarket(odds)

	// Анализируем каждый рынок
	for market, marketOdds := range oddsByMarket {
		if err := vc.analyzeMarket(ctx, matchID, market, marketOdds); err != nil {
			log.Printf("Error analyzing market %s for match %s: %v", market, matchID, err)
		}
	}

	return nil
}

// groupOddsByMarket группирует коэффициенты по рынкам
func (vc *ValueCalculator) groupOddsByMarket(odds []*models.Odd) map[string][]*models.Odd {
	grouped := make(map[string][]*models.Odd)
	
	for _, odd := range odds {
		grouped[odd.Market] = append(grouped[odd.Market], odd)
	}
	
	return grouped
}

// analyzeMarket анализирует рынок на предмет value bet
func (vc *ValueCalculator) analyzeMarket(ctx context.Context, matchID, market string, odds []*models.Odd) error {
	if len(odds) < 2 {
		return nil // Нужно минимум 2 БК для сравнения
	}

	// Создаем референсные данные
	referenceData, err := vc.calculateReferenceData(odds)
	if err != nil {
		return fmt.Errorf("failed to calculate reference data: %v", err)
	}

	// Ищем value bet для каждого исхода
	for outcome, referenceOdd := range referenceData.ReferenceOdds {
		valueBets := vc.findValueBetsForOutcome(odds, outcome, referenceOdd)
		
		// Сохраняем найденные value bet
		for _, valueBet := range valueBets {
			if err := vc.saveValueBet(ctx, valueBet); err != nil {
				log.Printf("Failed to save value bet: %v", err)
			}
		}
	}

	return nil
}

// ReferenceData представляет референсные данные
type ReferenceData struct {
	ReferenceOdds map[string]float64 `json:"reference_odds"` // исход -> коэффициент
	Source        string              `json:"source"`
}

// calculateReferenceData вычисляет референсные коэффициенты
func (vc *ValueCalculator) calculateReferenceData(odds []*models.Odd) (*ReferenceData, error) {
	// Собираем все исходы и их коэффициенты
	allOutcomes := make(map[string][]float64) // исход -> []коэффициенты
	
	for _, odd := range odds {
		for outcome, coefficient := range odd.Outcomes {
			allOutcomes[outcome] = append(allOutcomes[outcome], coefficient)
		}
	}

	// Вычисляем референсные коэффициенты
	referenceOdds := make(map[string]float64)
	
	for outcome, coefficients := range allOutcomes {
		if len(coefficients) == 0 {
			continue
		}
		
		// Сортируем коэффициенты
		sort.Float64s(coefficients)
		
		// Вычисляем среднее по топ-5 (или меньше, если недостаточно данных)
		topCount := int(math.Min(5, float64(len(coefficients))))
		topCoefficients := coefficients[:topCount]
		
		// Среднее арифметическое
		sum := 0.0
		for _, coef := range topCoefficients {
			sum += coef
		}
		referenceOdds[outcome] = sum / float64(len(topCoefficients))
	}

	return &ReferenceData{
		ReferenceOdds: referenceOdds,
		Source:        "average_top5",
	}, nil
}

// findValueBetsForOutcome ищет value bet для конкретного исхода
func (vc *ValueCalculator) findValueBetsForOutcome(odds []*models.Odd, outcome string, referenceOdd float64) []*models.ValueBet {
	var valueBets []*models.ValueBet

	for _, odd := range odds {
		bookmakerOdd, exists := odd.Outcomes[outcome]
		if !exists {
			continue
		}

		// Вычисляем value
		valuePercent := vc.calculateValuePercent(bookmakerOdd, referenceOdd)
		
		// Проверяем критерии
		if valuePercent >= vc.config.MinValuePercent {
			valueBet := &models.ValueBet{
				ID:              fmt.Sprintf("%s_%s_%s_%s", odd.MatchID, odd.Market, outcome, odd.Bookmaker),
				MatchID:         odd.MatchID,
				MatchName:       odd.MatchName,
				MatchTime:       odd.MatchTime,
				Sport:           odd.Sport,
				Market:          odd.Market,
				Outcome:         outcome,
				BookmakerOdd:    bookmakerOdd,
				ReferenceOdd:    referenceOdd,
				ValuePercent:    valuePercent,
				Bookmaker:       odd.Bookmaker,
				ReferenceSource: "average_top5",
				Stake:           float64(vc.config.MinStake),
				PotentialWin:    float64(vc.config.MinStake) * bookmakerOdd,
				FoundAt:         time.Now(),
				ExpiresAt:       time.Now().Add(30 * time.Minute), // 30 минут на ставку
			}
			
			valueBets = append(valueBets, valueBet)
		}
	}

	return valueBets
}

// calculateValuePercent вычисляет процент value
func (vc *ValueCalculator) calculateValuePercent(bookmakerOdd, referenceOdd float64) float64 {
	if referenceOdd <= 0 {
		return 0
	}
	return ((bookmakerOdd / referenceOdd) - 1) * 100
}

// saveValueBet сохраняет найденную value bet
func (vc *ValueCalculator) saveValueBet(ctx context.Context, valueBet *models.ValueBet) error {
	// В реальной реализации здесь будет сохранение в PostgreSQL
	// Пока что просто логируем
	log.Printf("🎯 VALUE BET FOUND!")
	log.Printf("   Match: %s", valueBet.MatchName)
	log.Printf("   Market: %s, Outcome: %s", valueBet.Market, valueBet.Outcome)
	log.Printf("   Bookmaker: %s (%.2f) vs Reference (%.2f)", 
		valueBet.Bookmaker, valueBet.BookmakerOdd, valueBet.ReferenceOdd)
	log.Printf("   Value: %.2f%%", valueBet.ValuePercent)
	log.Printf("   Stake: %.0f, Potential Win: %.0f", valueBet.Stake, valueBet.PotentialWin)
	log.Printf("   Expires: %s", valueBet.ExpiresAt.Format("15:04:05"))
	log.Printf("   ---")
	
	return nil
}
