package calculator

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

// ValueCalculator reads odds from HTTP endpoint and calculates top diffs between bookmakers.
// Data is fetched on-demand from parser on each request.
// Can also run asynchronously to process matches periodically and send alerts.
type ValueCalculator struct {
	httpClient         *HTTPMatchesClient
	cfg                *config.ValueCalculatorConfig
	diffStorage        storage.DiffBetStorage
	oddsSnapshotStorage storage.OddsSnapshotStorage
	notifier           *TelegramNotifier
	asyncTicker              *time.Ticker
	testAlertTicker          *time.Ticker
	asyncMu                  sync.RWMutex
	asyncStopped             bool
	alertsValueEnabled       bool // алерты по валуям
	alertsLineMovementEnabled bool // алерты по прогрузам
	asyncCtx                 context.Context
	asyncCancel              context.CancelFunc
}

func NewValueCalculator(cfg *config.ValueCalculatorConfig, diffStorage storage.DiffBetStorage, oddsSnapshotStorage storage.OddsSnapshotStorage) *ValueCalculator {
	var httpClient *HTTPMatchesClient
	if cfg != nil && cfg.ParserURL != "" {
		httpClient = NewHTTPMatchesClient(cfg.ParserURL)
	}

	var notifier *TelegramNotifier
	if cfg != nil && cfg.AsyncEnabled && cfg.TelegramBotToken != "" && cfg.TelegramChatID != 0 {
		notifier = NewTelegramNotifier(cfg.TelegramBotToken, cfg.TelegramChatID)
	}

	return &ValueCalculator{
		httpClient:          httpClient,
		cfg:                  cfg,
		diffStorage:         diffStorage,
		oddsSnapshotStorage: oddsSnapshotStorage,
		notifier:            notifier,
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
		slog.Info("Async processing disabled, running in on-demand mode")
	}

	// Wait for context cancellation
	<-ctx.Done()

	c.StopAsync(true) // true = shutdown, stop notifier too

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
		slog.Info("Async processing is already running")
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
		slog.Warn("Invalid async_interval, using default 30s")
	}

	// Reset stopped flag and alert flags, create new ticker
	c.asyncStopped = false
	c.alertsValueEnabled = true
	c.alertsLineMovementEnabled = true
	if c.asyncTicker != nil {
		c.asyncTicker.Stop()
	}
	c.asyncTicker = time.NewTicker(interval)

	// Test alert ticker disabled - was used for diagnostics
	// if c.notifier != nil {
	// 	if c.testAlertTicker != nil {
	// 		c.testAlertTicker.Stop()
	// 	}
	// 	c.testAlertTicker = time.NewTicker(5 * time.Minute)
	// 	go c.runTestAlerts(c.asyncCtx)
	// 	slog.Info("Started test alert ticker", "interval", 5*time.Minute)
	// }

	slog.Info("Starting async processing", "interval", interval)
	go c.runAsyncProcessing(c.asyncCtx)

	return nil
}

// runAsyncProcessing runs the async processing loop.
// Value/diff processing and line movement (прогрузы) run in parallel on each tick.
func (c *ValueCalculator) runAsyncProcessing(ctx context.Context) {
	// Run immediately on start
	c.runAsyncIteration(ctx)

	for {
		c.asyncMu.RLock()
		stopped := c.asyncStopped
		c.asyncMu.RUnlock()

		if stopped {
			slog.Info("Async processing stopped by user")
			return
		}

		select {
		case <-ctx.Done():
			slog.Info("Stopping async processing")
			return
		case <-c.asyncTicker.C:
			c.asyncMu.RLock()
			stopped = c.asyncStopped
			c.asyncMu.RUnlock()
			if stopped {
				slog.Info("Async processing stopped by user")
				return
			}
			c.runAsyncIteration(ctx)
		}
	}
}

// runAsyncIteration runs value/diff processing and line movement in parallel
func (c *ValueCalculator) runAsyncIteration(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.processMatchesAsync(ctx)
	}()
	if c.cfg != nil && c.cfg.LineMovementEnabled && c.oddsSnapshotStorage != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.processLineMovementsAsync(ctx)
		}()
	}
	wg.Wait()
}

// runTestAlerts sends test alerts every 5 minutes to verify notification system
func (c *ValueCalculator) runTestAlerts(ctx context.Context) {
	// Send first test alert immediately
	testMsg := fmt.Sprintf("Test alert #1 - System check at %s", time.Now().UTC().Format("15:04:05 UTC"))
	if err := c.notifier.SendTestAlert(ctx, testMsg); err != nil {
		slog.Error("Failed to send initial test alert", "error", err)
	}

	alertCounter := 1
	for {
		select {
		case <-ctx.Done():
			slog.Info("Test alert ticker stopped")
			return
		case <-c.testAlertTicker.C:
			c.asyncMu.RLock()
			stopped := c.asyncStopped
			c.asyncMu.RUnlock()
			
			if stopped {
				slog.Info("Test alert ticker stopped by user")
				return
			}

			alertCounter++
			testMsg := fmt.Sprintf("Test alert #%d - System check at %s", alertCounter, time.Now().UTC().Format("15:04:05 UTC"))
			slog.Info("Sending test alert", "counter", alertCounter, "time", time.Now().UTC())
			if err := c.notifier.SendTestAlert(ctx, testMsg); err != nil {
				slog.Error("Failed to send test alert", "error", err, "counter", alertCounter)
			}
		}
	}
}

// processMatchesAsync processes matches asynchronously and sends alerts for new high-value diffs
func (c *ValueCalculator) processMatchesAsync(ctx context.Context) {
	if c.httpClient == nil {
		slog.Debug("Parser URL not configured, skipping async processing")
		return
	}

	if c.diffStorage == nil {
		slog.Debug("Diff storage not configured, skipping async processing")
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

	iterationStartedAt := time.Now()
	slog.Info("Async value iteration started", "started_at", iterationStartedAt.UTC().Format(time.RFC3339))

	slog.Debug("Fetching matches for async processing...")

	// Create context with timeout for the request
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	matches, err := c.httpClient.GetMatchesAll(reqCtx)
	if err != nil {
		slog.Error("Failed to fetch matches for async processing", "error", err.Error())
		return
	}

	// Log merged match counts by sport (football vs esports)
	matchesBySport := make(map[string]int)
	for _, m := range matches {
		s := m.Sport
		if s == "" {
			s = "unknown"
		}
		matchesBySport[s]++
	}
	slog.Info("Merged matches by sport", "total", len(matches), "by_sport", matchesBySport)

	// Calculate all diffs
	diffs := computeTopDiffs(matches, 1000) // Get more diffs for async processing

	// Log how many diffs came from esports (dota2, cs)
	diffsBySport := make(map[string]int)
	for _, d := range diffs {
		s := d.Sport
		if s == "" {
			s = "unknown"
		}
		diffsBySport[s]++
	}
	slog.Info("Diffs by sport", "total", len(diffs), "by_sport", diffsBySport)

	logStatisticalEventsSummary(matches)

	slog.Debug("Calculated diffs, storing and checking for alerts", "diff_count", len(diffs))

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

	maxOdds := 0.0
	if c.cfg != nil && c.cfg.MaxOdds > 0 {
		maxOdds = c.cfg.MaxOdds
	}

	for _, diff := range diffs {
		// Skip high-odds diffs: variance is higher, value is less reliable
		if maxOdds > 0 && diff.MaxOdd > maxOdds {
			_, _ = c.diffStorage.StoreDiffBet(ctx, &diff)
			continue
		}

		// Check if we should send an alert for this diff
		shouldSendAlert := false
		if alertThreshold > 0 && diff.DiffPercent > alertThreshold && c.notifier != nil {
			// Get the last diff for this match+bet combination (excluding current one)
			lastDiffPercent, lastCalculatedAt, err := c.diffStorage.GetLastDiffBet(ctx, diff.MatchGroupKey, diff.BetKey, diff.CalculatedAt)
			if err != nil {
				slog.Warn("Failed to get last diff", "error", err.Error())
				// Continue anyway - better to send duplicate than miss an alert
				shouldSendAlert = true
			} else if lastDiffPercent == 0 || lastCalculatedAt.IsZero() {
				// No previous diff found - this is a new diff, send alert
				shouldSendAlert = true
			} else if lastDiffPercent < alertThreshold {
				// Previous diff was below threshold, so no alert was sent
				// This is the first time diff exceeds threshold, send alert
				shouldSendAlert = true
				slog.Info("Diff crossed threshold, sending alert", "match", diff.MatchName, "from", lastDiffPercent, "to", diff.DiffPercent)
			} else {
				// Previous diff was also above threshold - check if alert was sent recently
				timeSinceLastAlert := time.Since(lastCalculatedAt)
				if timeSinceLastAlert > time.Duration(alertCooldownMinutes)*time.Minute {
					// Last alert was sent more than cooldown minutes ago, send alert
					shouldSendAlert = true
					slog.Info("Cooldown expired, sending alert", "match", diff.MatchName, "diff_percent", diff.DiffPercent)
				} else {
					// Last alert was sent recently - check if diff increased significantly
					diffIncrease := diff.DiffPercent - lastDiffPercent
					if diffIncrease >= alertMinIncrease {
						// Diff increased significantly, send alert again
						shouldSendAlert = true
						slog.Info("Diff increased significantly, sending alert", "match", diff.MatchName, "from", lastDiffPercent, "to", diff.DiffPercent, "increase", diffIncrease)
					} else {
						// Diff didn't increase significantly, skip
						slog.Debug("Skipping duplicate alert", "match", diff.MatchName, "from", lastDiffPercent, "to", diff.DiffPercent, "increase", diffIncrease, "minutes_since_last", timeSinceLastAlert.Minutes(), "min_increase", alertMinIncrease)
					}
				}
			}
		}

		// Store the diff (pass as interface{} to match interface)
		// We store all diffs, not just ones we alert on
		_, err := c.diffStorage.StoreDiffBet(ctx, &diff)
		if err != nil {
			slog.Error("Failed to store diff", "error", err.Error(), "match", diff.MatchGroupKey, "bet_key", diff.BetKey)
			// Continue even if storage fails
		}

		// Send Telegram alert if needed (and value alerts are enabled)
		c.asyncMu.RLock()
		valueAlertsOn := c.alertsValueEnabled
		c.asyncMu.RUnlock()
		if shouldSendAlert && valueAlertsOn {
			thresholdInt := int(math.Round(alertThreshold))
			queuedAt := time.Now()
			if err := c.notifier.SendDiffAlert(ctx, &diff, thresholdInt); err != nil {
				slog.Error("Failed to queue value alert", "match", diff.MatchName, "threshold", alertThreshold, "error", err.Error())
			} else {
				alertCount++
				delaySinceCalc := queuedAt.Sub(diff.CalculatedAt)
				slog.Info("Value alert queued",
					"match", diff.MatchName,
					"diff_percent", diff.DiffPercent,
					"threshold", alertThreshold,
					"calculated_at", diff.CalculatedAt.UTC().Format(time.RFC3339),
					"queued_at", queuedAt.UTC().Format(time.RFC3339),
					"delay_since_calculation_sec", delaySinceCalc.Seconds(),
					"queue_length", c.notifier.QueueLen())
			}
		}
	}

	iterationDuration := time.Since(iterationStartedAt)
	slog.Info("Async value iteration complete", "alerts_queued", alertCount, "threshold", alertThreshold, "duration_sec", iterationDuration.Seconds())
}

// processLineMovementsAsync tracks odds drops (прогрузы) in the same bookmaker, stores snapshots,
// cleans data for started matches, and sends Telegram alerts for strong drops.
func (c *ValueCalculator) processLineMovementsAsync(ctx context.Context) {
	if c.httpClient == nil || c.oddsSnapshotStorage == nil {
		return
	}
	threshold := 0.0
	if c.cfg != nil && c.cfg.LineMovementAlertThreshold > 0 {
		threshold = c.cfg.LineMovementAlertThreshold
	}

	// Clean snapshots for matches that already started so DB doesn't grow
	if err := c.oddsSnapshotStorage.CleanSnapshotsForStartedMatches(ctx); err != nil {
		slog.Warn("CleanSnapshotsForStartedMatches failed", "error", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	matches, err := c.httpClient.GetMatchesAll(reqCtx)
	if err != nil {
		slog.Error("Failed to fetch matches for line movement", "error", err)
		return
	}

	lmIterationStartedAt := time.Now()
	slog.Info("Line movement iteration started", "started_at", lmIterationStartedAt.UTC().Format(time.RFC3339), "matches_count", len(matches))

	movements, err := computeAndStoreLineMovements(ctx, matches, c.oddsSnapshotStorage, threshold)
	if err != nil {
		slog.Error("computeAndStoreLineMovements failed", "error", err)
		return
	}

	now := time.Now()
	alertCount := 0
	// Only send line movement alerts to Telegram if enabled in config and not disabled by user
	c.asyncMu.RLock()
	lineMovementAlertsOn := c.alertsLineMovementEnabled
	c.asyncMu.RUnlock()
	sendLineMovementToTelegram := c.cfg != nil && c.cfg.LineMovementTelegramAlerts && lineMovementAlertsOn
	// Note: No delay needed here - messages are queued asynchronously and rate-limited in the background worker
	for i := range movements {
		lm := &movements[i]
		// Reset extremes first so we don't re-detect after restart and send a late duplicate (e.g. 105 min later).
		_ = c.oddsSnapshotStorage.ResetExtremesAfterAlert(ctx, lm.MatchGroupKey, lm.BetKey, lm.Bookmaker)
		if sendLineMovementToTelegram && c.notifier != nil {
			history, _ := c.oddsSnapshotStorage.GetOddsHistory(ctx, lm.MatchGroupKey, lm.BetKey, lm.Bookmaker, 30)
			queuedAt := time.Now()
			if err := c.notifier.SendLineMovementAlert(ctx, lm, threshold, now, history); err != nil {
				slog.Error("Failed to queue line movement alert", "match", lm.MatchName, "error", err)
			} else {
				alertCount++
				delaySinceDetect := queuedAt.Sub(lm.RecordedAt)
				slog.Info("Line movement alert queued",
					"match", lm.MatchName,
					"bookmaker", lm.Bookmaker,
					"change_percent", lm.ChangePercent,
					"detected_at", lm.RecordedAt.UTC().Format(time.RFC3339),
					"queued_at", queuedAt.UTC().Format(time.RFC3339),
					"delay_since_detection_sec", delaySinceDetect.Seconds(),
					"queue_length", c.notifier.QueueLen())
			}
		}
	}
	lmDuration := time.Since(lmIterationStartedAt)
	slog.Info("Line movement iteration complete", "movements_detected", len(movements), "alerts_queued", alertCount, "duration_sec", lmDuration.Seconds())
}

// StopAsync stops the asynchronous processing.
// shutdown: if true, also stops the Telegram notifier (use on app exit);
// if false, only stops the ticker so /start can resume alerts.
func (c *ValueCalculator) StopAsync(shutdown bool) {
	c.asyncMu.Lock()
	defer c.asyncMu.Unlock()

	if !c.asyncStopped && c.asyncTicker != nil {
		c.asyncStopped = true
		c.alertsValueEnabled = false
		c.alertsLineMovementEnabled = false
		c.asyncTicker.Stop()
		if c.testAlertTicker != nil {
			c.testAlertTicker.Stop()
		}
		if c.asyncCancel != nil {
			c.asyncCancel()
		}
		slog.Info("Async processing stopped")
	}

	if shutdown && c.notifier != nil {
		c.notifier.Stop()
	}
}

// IsAsyncRunning returns true if async processing is currently running
func (c *ValueCalculator) IsAsyncRunning() bool {
	c.asyncMu.RLock()
	defer c.asyncMu.RUnlock()
	return c.asyncTicker != nil && !c.asyncStopped
}
