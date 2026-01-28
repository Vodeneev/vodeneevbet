package calculator

import (
	"context"
	"fmt"
	"log"
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
	httpClient   *HTTPMatchesClient
	cfg          *config.ValueCalculatorConfig
	diffStorage  storage.DiffBetStorage
	notifier     *TelegramNotifier
	asyncTicker  *time.Ticker
	asyncMu      sync.RWMutex
	asyncStopped bool
	asyncCtx     context.Context
	asyncCancel  context.CancelFunc
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
