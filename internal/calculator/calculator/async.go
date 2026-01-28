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

	c.StopAsync()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "stopped",
		"message": "Async processing stopped successfully",
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
