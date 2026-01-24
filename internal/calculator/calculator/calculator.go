package calculator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

// ValueCalculator reads odds from HTTP endpoint and calculates top diffs between bookmakers.
// Data is fetched on-demand from parser on each request.
// Can also run asynchronously to process matches periodically and send alerts.
type ValueCalculator struct {
	httpClient    *HTTPMatchesClient
	cfg           *config.ValueCalculatorConfig
	diffStorage   storage.DiffBetStorage
	notifier      *TelegramNotifier
	asyncTicker   *time.Ticker
	asyncMu       sync.RWMutex
	asyncStopped  bool
	asyncCtx      context.Context
	asyncCancel   context.CancelFunc
}

func NewValueCalculator(cfg *config.ValueCalculatorConfig, diffStorage storage.DiffBetStorage) *ValueCalculator {
	var httpClient *HTTPMatchesClient
	if cfg != nil && cfg.ParserURL != "" {
		httpClient = NewHTTPMatchesClient(cfg.ParserURL)
	}

	var notifier *TelegramNotifier
	if cfg != nil && cfg.AsyncEnabled && cfg.TelegramBotToken != "" && cfg.TelegramChatID != 0 {
		notifier = NewTelegramNotifier(cfg.TelegramBotToken, cfg.TelegramChatID)
	}

	return &ValueCalculator{
		httpClient:  httpClient,
		cfg:         cfg,
		diffStorage: diffStorage,
		notifier:    notifier,
	}
}

func (c *ValueCalculator) Start(ctx context.Context) error {
	// Start async processing if enabled
	if c.cfg != nil && c.cfg.AsyncEnabled {
		c.asyncMu.Lock()
		c.asyncCtx, c.asyncCancel = context.WithCancel(ctx)
		c.asyncMu.Unlock()
		
		c.StartAsync()
	} else {
		log.Println("calculator: async processing disabled, running in on-demand mode")
	}

	// Wait for context cancellation
	<-ctx.Done()
	
	c.StopAsync()
	
	return nil
}

// StartAsync starts or restarts the asynchronous processing
func (c *ValueCalculator) StartAsync() error {
	c.asyncMu.Lock()
	defer c.asyncMu.Unlock()
	
	if c.cfg == nil || !c.cfg.AsyncEnabled {
		return fmt.Errorf("async processing is not enabled in config")
	}
	
	// If already running, don't restart
	if c.asyncTicker != nil && !c.asyncStopped {
		log.Println("calculator: async processing is already running")
		return nil
	}
	
	// Cancel old context if exists
	if c.asyncCancel != nil {
		c.asyncCancel()
	}
	
	// Create new context for restart
	c.asyncCtx, c.asyncCancel = context.WithCancel(context.Background())
	
	interval, err := time.ParseDuration(c.cfg.AsyncInterval)
	if err != nil {
		interval = 30 * time.Second // Default to 30 seconds
		log.Printf("calculator: invalid async_interval, using default 30s")
	}
	
	// Reset stopped flag and create new ticker
	c.asyncStopped = false
	if c.asyncTicker != nil {
		c.asyncTicker.Stop()
	}
	c.asyncTicker = time.NewTicker(interval)
	
	log.Printf("calculator: starting async processing with interval %v", interval)
	go c.runAsyncProcessing(c.asyncCtx)
	
	return nil
}

// runAsyncProcessing runs the async processing loop
func (c *ValueCalculator) runAsyncProcessing(ctx context.Context) {
	// Run immediately on start
	c.processMatchesAsync(ctx)
	
	for {
		// Check if async processing was stopped
		c.asyncMu.RLock()
		stopped := c.asyncStopped
		c.asyncMu.RUnlock()
		
		if stopped {
			log.Println("calculator: async processing stopped by user")
			return
		}
		
		select {
		case <-ctx.Done():
			log.Println("calculator: stopping async processing")
			return
		case <-c.asyncTicker.C:
			// Check again before processing
			c.asyncMu.RLock()
			stopped = c.asyncStopped
			c.asyncMu.RUnlock()
			if stopped {
				log.Println("calculator: async processing stopped by user")
				return
			}
			c.processMatchesAsync(ctx)
		}
	}
}

// processMatchesAsync processes matches asynchronously and sends alerts for new high-value diffs
func (c *ValueCalculator) processMatchesAsync(ctx context.Context) {
	if c.httpClient == nil {
		log.Printf("calculator: async: parser URL not configured, skipping")
		return
	}

	if c.diffStorage == nil {
		log.Printf("calculator: async: diff storage not configured, skipping")
		return
	}

	alertThreshold := 0.0
	if c.cfg != nil {
		// Preferred single threshold
		if c.cfg.AlertThreshold > 0 {
			alertThreshold = c.cfg.AlertThreshold
		} else if c.cfg.AlertThreshold20 > 0 {
			// Backward compatibility
			alertThreshold = c.cfg.AlertThreshold20
		} else if c.cfg.AlertThreshold10 > 0 {
			// Backward compatibility
			alertThreshold = c.cfg.AlertThreshold10
		}
	}

	log.Println("calculator: async: fetching matches...")
	
	// Create context with timeout for the request
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	matches, err := c.httpClient.GetMatches(reqCtx)
	if err != nil {
		log.Printf("calculator: async: failed to fetch matches: %v", err)
		return
	}

	log.Printf("calculator: async: fetched %d matches, calculating diffs...", len(matches))

	// Calculate all diffs
	diffs := computeTopDiffs(matches, 1000) // Get more diffs for async processing

	log.Printf("calculator: async: calculated %d diffs, storing and checking for alerts...", len(diffs))

	// Store diffs and check for new high-value ones
	alertCount := 0
	// Time window to prevent duplicate alerts
	// This prevents sending the same alert repeatedly for unchanged diffs
	alertCooldownMinutes := 60 // Default: 60 minutes
	if c.cfg != nil && c.cfg.AlertCooldownMinutes > 0 {
		alertCooldownMinutes = c.cfg.AlertCooldownMinutes
	}
	// Minimum increase in diff_percent to send alert again even if already sent recently
	alertMinIncrease := 5.0 // Default: 5% increase
	if c.cfg != nil && c.cfg.AlertMinIncrease > 0 {
		alertMinIncrease = c.cfg.AlertMinIncrease
	}
	
	for _, diff := range diffs {
		// Check if we should send an alert for this diff
		shouldSendAlert := false
		if alertThreshold > 0 && diff.DiffPercent > alertThreshold && c.notifier != nil {
			// Get the last diff for this match+bet combination (excluding current one)
			lastDiffPercent, lastCalculatedAt, err := c.diffStorage.GetLastDiffBet(ctx, diff.MatchGroupKey, diff.BetKey, diff.CalculatedAt)
			if err != nil {
				log.Printf("calculator: async: failed to get last diff: %v", err)
				// Continue anyway - better to send duplicate than miss an alert
				shouldSendAlert = true
			} else if lastDiffPercent == 0 || lastCalculatedAt.IsZero() {
				// No previous diff found - this is a new diff, send alert
				shouldSendAlert = true
			} else if lastDiffPercent < alertThreshold {
				// Previous diff was below threshold, so no alert was sent
				// This is the first time diff exceeds threshold, send alert
				shouldSendAlert = true
				log.Printf("calculator: async: diff crossed threshold for %s (%.2f%% -> %.2f%%), sending alert", 
					diff.MatchName, lastDiffPercent, diff.DiffPercent)
			} else {
				// Previous diff was also above threshold - check if alert was sent recently
				timeSinceLastAlert := time.Since(lastCalculatedAt)
				if timeSinceLastAlert > time.Duration(alertCooldownMinutes)*time.Minute {
					// Last alert was sent more than cooldown minutes ago, send alert
					shouldSendAlert = true
					log.Printf("calculator: async: cooldown expired for %s (%.2f%%), sending alert", diff.MatchName, diff.DiffPercent)
				} else {
					// Last alert was sent recently - check if diff increased significantly
					diffIncrease := diff.DiffPercent - lastDiffPercent
					if diffIncrease >= alertMinIncrease {
						// Diff increased significantly, send alert again
						shouldSendAlert = true
						log.Printf("calculator: async: diff increased significantly for %s (%.2f%% -> %.2f%%, +%.2f%%), sending alert", 
							diff.MatchName, lastDiffPercent, diff.DiffPercent, diffIncrease)
					} else {
						// Diff didn't increase significantly, skip
						log.Printf("calculator: async: skipping duplicate alert for %s (%.2f%% -> %.2f%%, +%.2f%%) - already sent %.0f minutes ago, increase %.2f%% < %.2f%%", 
							diff.MatchName, lastDiffPercent, diff.DiffPercent, diffIncrease, 
							timeSinceLastAlert.Minutes(), diffIncrease, alertMinIncrease)
					}
				}
			}
		}

		// Store the diff (pass as interface{} to match interface)
		// We store all diffs, not just ones we alert on
		_, err := c.diffStorage.StoreDiffBet(ctx, &diff)
		if err != nil {
			log.Printf("calculator: async: failed to store diff: %v", err)
			// Continue even if storage fails
		}

		// Send Telegram alert if needed
		if shouldSendAlert {
			thresholdInt := int(math.Round(alertThreshold))
			if err := c.notifier.SendDiffAlert(ctx, &diff, thresholdInt); err != nil {
				log.Printf("calculator: async: failed to send %.0f%% alert: %v", alertThreshold, err)
			} else {
				alertCount++
				log.Printf("calculator: async: sent %.0f%% alert for %s (%.2f%%)", alertThreshold, diff.MatchName, diff.DiffPercent)
			}
		}
	}

	log.Printf("calculator: async: processing complete. Sent %d alerts (>%.0f%% threshold)", alertCount, alertThreshold)
}

// StopAsync stops the asynchronous processing
func (c *ValueCalculator) StopAsync() {
	c.asyncMu.Lock()
	defer c.asyncMu.Unlock()
	
	if !c.asyncStopped && c.asyncTicker != nil {
		c.asyncStopped = true
		c.asyncTicker.Stop()
		if c.asyncCancel != nil {
			c.asyncCancel()
		}
		log.Println("calculator: async processing stopped")
	}
}

// IsAsyncRunning returns true if async processing is currently running
func (c *ValueCalculator) IsAsyncRunning() bool {
	c.asyncMu.RLock()
	defer c.asyncMu.RUnlock()
	return c.asyncTicker != nil && !c.asyncStopped
}

// RegisterHTTP registers calculator endpoints onto mux.
func (c *ValueCalculator) RegisterHTTP(mux *http.ServeMux) {
	mux.HandleFunc("/diffs/top", c.handleTopDiffs)
	mux.HandleFunc("/diffs/status", c.handleStatus)
	mux.HandleFunc("/async/stop", c.handleStopAsync)
	mux.HandleFunc("/async/start", c.handleStartAsync)
}

func (c *ValueCalculator) handleTopDiffs(w http.ResponseWriter, r *http.Request) {
	limit := 5
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 50 {
				n = 50
			}
			limit = n
		}
	}

	// Filter by match status: "live" (started), "upcoming" (not started), or empty (all)
	statusFilter := r.URL.Query().Get("status")

	// Fetch fresh data from parser on each request
	var diffs []DiffBet
	if c.httpClient == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "parser URL is not configured"})
		return
	}

	// Create context with timeout for the request
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	matches, err := c.httpClient.GetMatches(ctx)
	if err != nil {
		log.Printf("calculator: failed to load matches in handleTopDiffs: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to fetch matches from parser", "details": err.Error()})
		return
	}

	// Calculate diffs from fresh data
	diffs = computeTopDiffs(matches, 100)

	// Filter by status if specified
	// Use UTC for comparison to handle timezones correctly (StartTime is stored in UTC)
	now := time.Now().UTC()
	// Matches typically last up to 2-3 hours, so exclude matches that started more than 3 hours ago
	maxLiveAge := 3 * time.Hour
	if statusFilter != "" {
		filtered := make([]DiffBet, 0, len(diffs))
		for _, diff := range diffs {
			// Match is live if it has started (StartTime is in the past) but not too long ago
			// StartTime is stored in UTC, so we compare with UTC time
			// Use Before with equal check to handle edge cases
			hasStarted := !diff.StartTime.IsZero() && (diff.StartTime.Before(now) || diff.StartTime.Equal(now))
			notTooOld := !diff.StartTime.IsZero() && now.Sub(diff.StartTime) <= maxLiveAge
			isLive := hasStarted && notTooOld
			switch statusFilter {
			case "live":
				if isLive {
					filtered = append(filtered, diff)
				}
			case "upcoming":
				// Upcoming means match hasn't started yet (StartTime is in the future)
				if !hasStarted {
					filtered = append(filtered, diff)
				}
			default:
				// Unknown status filter, return all
				filtered = append(filtered, diff)
			}
		}
		diffs = filtered
	}

	// Re-sort after filtering (computeTopDiffs already sorts, but we filter after)
	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].DiffPercent > diffs[j].DiffPercent
	})

	if limit > len(diffs) {
		limit = len(diffs)
	}

	w.Header().Set("Content-Type", "application/json")
	if len(diffs) > 0 {
		_ = json.NewEncoder(w).Encode(diffs[:limit])
	} else {
		_ = json.NewEncoder(w).Encode([]DiffBet{})
	}
}

func (c *ValueCalculator) handleStatus(w http.ResponseWriter, r *http.Request) {
	// Status endpoint - data is fetched on-demand, no caching
	status := map[string]any{
		"status":            "ok",
		"parser_configured": c.httpClient != nil,
		"mode":              "on-demand",
		"async_running":     c.IsAsyncRunning(),
	}
	if c.httpClient == nil {
		status["error"] = "parser URL is not configured"
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (c *ValueCalculator) handleStopAsync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed, use POST"})
		return
	}

	c.asyncMu.RLock()
	wasRunning := c.asyncTicker != nil && !c.asyncStopped
	c.asyncMu.RUnlock()

	if !wasRunning {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "already_stopped",
			"message": "Async processing is not running",
		})
		return
	}

	c.StopAsync()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "stopped",
		"message": "Async processing stopped successfully",
	})
}

func (c *ValueCalculator) handleStartAsync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed, use POST"})
		return
	}

	c.asyncMu.RLock()
	wasRunning := c.asyncTicker != nil && !c.asyncStopped
	c.asyncMu.RUnlock()

	if wasRunning {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "already_running",
			"message": "Async processing is already running",
		})
		return
	}

	if err := c.StartAsync(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "failed to start async processing",
			"message": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "started",
		"message": "Async processing started successfully",
	})
}

func computeTopDiffs(matches []models.Match, keepTop int) []DiffBet {
	if keepTop <= 0 {
		keepTop = 100
	}
	now := time.Now()

	// matchGroupKey -> betKey -> bookmaker -> odd
	type betMap map[string]map[string]float64
	groups := map[string]betMap{}

	// Some metadata for group: choose "best" human-readable match fields (first seen is fine).
	type groupMeta struct {
		name      string
		startTime time.Time
		sport     string
	}
	meta := map[string]groupMeta{}

	for i := range matches {
		m := matches[i]
		gk := matchGroupKey(m)
		if gk == "" {
			continue
		}
		if _, ok := meta[gk]; !ok {
			meta[gk] = groupMeta{
				name:      strings.TrimSpace(m.HomeTeam) + " vs " + strings.TrimSpace(m.AwayTeam),
				startTime: m.StartTime,
				sport:     m.Sport,
			}
		}
		if _, ok := groups[gk]; !ok {
			groups[gk] = betMap{}
		}

		for _, ev := range m.Events {
			for _, out := range ev.Outcomes {
				bk := strings.TrimSpace(out.Bookmaker)
				if bk == "" {
					bk = strings.TrimSpace(ev.Bookmaker)
				}
				if bk == "" {
					bk = strings.TrimSpace(m.Bookmaker)
				}
				if bk == "" {
					continue
				}

				odd := out.Odds
				if !isFinitePositiveOdd(odd) {
					continue
				}

				eventType := strings.TrimSpace(ev.EventType)
				outcomeType := strings.TrimSpace(out.OutcomeType)
				param := strings.TrimSpace(out.Parameter)
				if eventType == "" || outcomeType == "" {
					continue
				}

				betKey := eventType + "|" + outcomeType + "|" + param
				if _, ok := groups[gk][betKey]; !ok {
					groups[gk][betKey] = map[string]float64{}
				}

				// Keep latest/maximum? For diffs we just keep the best (max) seen per bookmaker+bet.
				if prev, ok := groups[gk][betKey][bk]; !ok || odd > prev {
					groups[gk][betKey][bk] = odd
				}
			}
		}
	}

	var diffs []DiffBet
	for gk, bets := range groups {
		gm := meta[gk]
		for betKey, byBook := range bets {
			if len(byBook) < 2 {
				continue
			}
			minOdd := math.MaxFloat64
			maxOdd := -math.MaxFloat64
			minBk, maxBk := "", ""
			for bk, odd := range byBook {
				if odd < minOdd {
					minOdd = odd
					minBk = bk
				}
				if odd > maxOdd {
					maxOdd = odd
					maxBk = bk
				}
			}
			if minOdd <= 0 || maxOdd <= 0 || maxOdd <= minOdd {
				continue
			}

			diffAbs := maxOdd - minOdd
			diffPct := (maxOdd/minOdd - 1.0) * 100.0

			parts := strings.SplitN(betKey, "|", 3)
			evType, outType, param := "", "", ""
			if len(parts) >= 1 {
				evType = parts[0]
			}
			if len(parts) >= 2 {
				outType = parts[1]
			}
			if len(parts) >= 3 {
				param = parts[2]
			}

			diffs = append(diffs, DiffBet{
				MatchGroupKey: gk,
				MatchName:     gm.name,
				StartTime:     gm.startTime,
				Sport:         gm.sport,
				EventType:     evType,
				OutcomeType:   outType,
				Parameter:     param,
				BetKey:        betKey,
				Bookmakers:    len(byBook),
				MinBookmaker:  minBk,
				MinOdd:        minOdd,
				MaxBookmaker:  maxBk,
				MaxOdd:        maxOdd,
				DiffAbs:       diffAbs,
				DiffPercent:   diffPct,
				CalculatedAt:  now,
			})
		}
	}

	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].DiffPercent > diffs[j].DiffPercent
	})

	if len(diffs) > keepTop {
		diffs = diffs[:keepTop]
	}
	return diffs
}

func matchGroupKey(m models.Match) string {
	home := normalizeTeam(m.HomeTeam)
	away := normalizeTeam(m.AwayTeam)
	if home == "" || away == "" {
		// fallback to name parsing if teams are missing
		n := strings.TrimSpace(m.Name)
		if n != "" {
			if h, a, ok := splitTeamsFromName(n); ok {
				home = normalizeTeam(h)
				away = normalizeTeam(a)
			}
		}
	}
	if home == "" || away == "" {
		return ""
	}
	sport := strings.ToLower(strings.TrimSpace(m.Sport))
	if sport == "" {
		sport = "unknown"
	}

	// Time rounding to tolerate small differences between APIs.
	t := m.StartTime.UTC().Truncate(30 * time.Minute)
	if t.IsZero() {
		// If no start time, group only by teams.
		return sport + "|" + home + "|" + away
	}
	return sport + "|" + home + "|" + away + "|" + t.Format(time.RFC3339)
}

func normalizeTeam(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	// collapse whitespace
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func splitTeamsFromName(name string) (string, string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", false
	}
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

func isFinitePositiveOdd(v float64) bool {
	return v > 1.000001 && !math.IsInf(v, 0) && !math.IsNaN(v)
}
