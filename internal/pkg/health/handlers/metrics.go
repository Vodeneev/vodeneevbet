package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/performance"
)

// HandleMetrics handles /metrics endpoint
func HandleMetrics(w http.ResponseWriter, r *http.Request) {
	tracker := performance.GetTracker()
	metrics := tracker.GetMetrics()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode metrics: %v", err), http.StatusInternalServerError)
		return
	}
}
