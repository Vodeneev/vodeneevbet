package calculator

import (
	"encoding/json"
	"net/http"
)

// handleStopAsync stops asynchronous processing
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

	c.StopAsync(false) // false = user /stop, keep notifier for resume on /start

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "stopped",
		"message": "Async processing stopped successfully",
	})
}

// handleStopAsyncValues disables only value (валуй) alerts; async keeps running.
func (c *ValueCalculator) handleStopAsyncValues(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed, use POST"})
		return
	}

	c.asyncMu.Lock()
	c.alertsValueEnabled = false
	c.asyncMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Алерты по валуям отключены. Прогрузы продолжают отправляться.",
	})
}

// handleStopAsyncLineMovements disables only line movement (прогрузы) alerts; async keeps running.
func (c *ValueCalculator) handleStopAsyncLineMovements(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed, use POST"})
		return
	}

	c.asyncMu.Lock()
	c.alertsLineMovementEnabled = false
	c.asyncMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Алерты по прогрузам отключены. Валуи продолжают отправляться.",
	})
}

// handleStartAsync starts asynchronous processing
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

// handleClearNotificationQueue drains the Telegram notification queue (pending alerts are dropped).
func (c *ValueCalculator) handleClearNotificationQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed, use POST"})
		return
	}

	if c.notifier == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"cleared": 0,
			"message": "Telegram notifier is not configured",
		})
		return
	}

	dropped := c.notifier.ClearQueue()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"cleared": dropped,
		"message": "Notification queue cleared",
	})
}
