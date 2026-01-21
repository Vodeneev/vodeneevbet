package performance

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Tracker tracks performance metrics for parser operations
type Tracker struct {
	mu sync.RWMutex
	
	// Overall metrics
	TotalRuns        int
	TotalMatches     int
	TotalEvents      int
	TotalOutcomes    int
	
	// Timing metrics
	TotalDuration       time.Duration
	HTTPFetchDuration   time.Duration
	JSONParseDuration   time.Duration
	GroupingDuration    time.Duration
	ProcessingDuration  time.Duration
	YDBWriteDuration    time.Duration
	
	// Per-match metrics
	MatchTimings []MatchTiming
	
	// YDB operation metrics
	YDBOperations []YDBOperation
}

// MatchTiming tracks timing for a single match
type MatchTiming struct {
	MatchID      string
	EventsCount  int
	OutcomesCount int
	BuildTime    time.Duration
	StoreTime    time.Duration
	TotalTime    time.Duration
	Success      bool
}

// YDBOperation tracks a single YDB operation
type YDBOperation struct {
	Operation   string // "match", "event", "outcome"
	MatchID     string
	EventID     string
	Duration    time.Duration
	Success     bool
	Error       string
	Timestamp   time.Time
}

var globalTracker = &Tracker{
	MatchTimings: make([]MatchTiming, 0, 1000),
	YDBOperations: make([]YDBOperation, 0, 10000),
}

// GetTracker returns the global performance tracker
func GetTracker() *Tracker {
	return globalTracker
}

// Reset resets all metrics
func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	t.TotalRuns = 0
	t.TotalMatches = 0
	t.TotalEvents = 0
	t.TotalOutcomes = 0
	t.TotalDuration = 0
	t.HTTPFetchDuration = 0
	t.JSONParseDuration = 0
	t.GroupingDuration = 0
	t.ProcessingDuration = 0
	t.YDBWriteDuration = 0
	t.MatchTimings = t.MatchTimings[:0]
	t.YDBOperations = t.YDBOperations[:0]
}

// RecordRun records a complete parser run
func (t *Tracker) RecordRun(
	httpFetch, jsonParse, grouping, processing, ydbWrite, total time.Duration,
	matches, events, outcomes int,
) {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	t.TotalRuns++
	t.TotalMatches += matches
	t.TotalEvents += events
	t.TotalOutcomes += outcomes
	t.TotalDuration += total
	t.HTTPFetchDuration += httpFetch
	t.JSONParseDuration += jsonParse
	t.GroupingDuration += grouping
	t.ProcessingDuration += processing
	t.YDBWriteDuration += ydbWrite
}

// RecordMatch records timing for a single match
func (t *Tracker) RecordMatch(matchID string, eventsCount, outcomesCount int, buildTime, storeTime, totalTime time.Duration, success bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	t.MatchTimings = append(t.MatchTimings, MatchTiming{
		MatchID:      matchID,
		EventsCount:  eventsCount,
		OutcomesCount: outcomesCount,
		BuildTime:    buildTime,
		StoreTime:    storeTime,
		TotalTime:    totalTime,
		Success:      success,
	})
}

// RecordYDBOperation records a single YDB operation
func (t *Tracker) RecordYDBOperation(operation, matchID, eventID string, duration time.Duration, success bool, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	
	t.YDBOperations = append(t.YDBOperations, YDBOperation{
		Operation: operation,
		MatchID:   matchID,
		EventID:   eventID,
		Duration:  duration,
		Success:   success,
		Error:     errStr,
		Timestamp: time.Now(),
	})
}

// PrintSummary prints a detailed performance summary
func (t *Tracker) PrintSummary() {
	t.mu.RLock()
	defer t.mu.RUnlock()
	
	if t.TotalRuns == 0 {
		fmt.Println("📊 No performance data collected yet")
		return
	}
	
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("📊 PERFORMANCE SUMMARY")
	fmt.Println(strings.Repeat("=", 80))
	
	avgRuns := float64(t.TotalRuns)
	
	fmt.Printf("\n🔢 Overall Statistics:\n")
	fmt.Printf("  Total Runs:        %d\n", t.TotalRuns)
	fmt.Printf("  Total Matches:     %d (avg: %.1f per run)\n", t.TotalMatches, float64(t.TotalMatches)/avgRuns)
	fmt.Printf("  Total Events:      %d (avg: %.1f per match)\n", t.TotalEvents, float64(t.TotalEvents)/float64(t.TotalMatches))
	fmt.Printf("  Total Outcomes:    %d (avg: %.1f per event)\n", t.TotalOutcomes, float64(t.TotalOutcomes)/float64(t.TotalEvents))
	
	fmt.Printf("\n⏱️  Timing Breakdown (average per run):\n")
	fmt.Printf("  HTTP Fetch:        %v (%.1f%%)\n", 
		t.HTTPFetchDuration/time.Duration(t.TotalRuns),
		float64(t.HTTPFetchDuration)/float64(t.TotalDuration)*100)
	fmt.Printf("  JSON Parse:        %v (%.1f%%)\n",
		t.JSONParseDuration/time.Duration(t.TotalRuns),
		float64(t.JSONParseDuration)/float64(t.TotalDuration)*100)
	fmt.Printf("  Event Grouping:    %v (%.1f%%)\n",
		t.GroupingDuration/time.Duration(t.TotalRuns),
		float64(t.GroupingDuration)/float64(t.TotalDuration)*100)
	fmt.Printf("  Processing:        %v (%.1f%%)\n",
		t.ProcessingDuration/time.Duration(t.TotalRuns),
		float64(t.ProcessingDuration)/float64(t.TotalDuration)*100)
	fmt.Printf("  YDB Write:         %v (%.1f%%)\n",
		t.YDBWriteDuration/time.Duration(t.TotalRuns),
		float64(t.YDBWriteDuration)/float64(t.TotalDuration)*100)
	fmt.Printf("  Total:             %v\n",
		t.TotalDuration/time.Duration(t.TotalRuns))
	
	// Per-match statistics
	if len(t.MatchTimings) > 0 {
		var totalMatchTime, totalBuildTime, totalStoreTime time.Duration
		successCount := 0
		totalEvents, totalOutcomes := 0, 0
		
		for _, mt := range t.MatchTimings {
			totalMatchTime += mt.TotalTime
			totalBuildTime += mt.BuildTime
			totalStoreTime += mt.StoreTime
			totalEvents += mt.EventsCount
			totalOutcomes += mt.OutcomesCount
			if mt.Success {
				successCount++
			}
		}
		
		avgMatches := float64(len(t.MatchTimings))
		
		fmt.Printf("\n🏆 Per-Match Statistics:\n")
		fmt.Printf("  Processed Matches: %d\n", len(t.MatchTimings))
		fmt.Printf("  Success Rate:      %.1f%% (%d/%d)\n", 
			float64(successCount)/avgMatches*100, successCount, len(t.MatchTimings))
		fmt.Printf("  Avg Events/Match:   %.1f\n", float64(totalEvents)/avgMatches)
		fmt.Printf("  Avg Outcomes/Match: %.1f\n", float64(totalOutcomes)/avgMatches)
		fmt.Printf("  Avg Build Time:     %v\n", totalBuildTime/time.Duration(len(t.MatchTimings)))
		fmt.Printf("  Avg Store Time:     %v\n", totalStoreTime/time.Duration(len(t.MatchTimings)))
		fmt.Printf("  Avg Total Time:     %v\n", totalMatchTime/time.Duration(len(t.MatchTimings)))
	}
	
	// YDB operation statistics
	if len(t.YDBOperations) > 0 {
		opsByType := make(map[string]struct {
			count   int
			total   time.Duration
			success int
		})
		
		for _, op := range t.YDBOperations {
			stat := opsByType[op.Operation]
			stat.count++
			stat.total += op.Duration
			if op.Success {
				stat.success++
			}
			opsByType[op.Operation] = stat
		}
		
		fmt.Printf("\n💾 YDB Operations:\n")
		for opType, stat := range opsByType {
			avgTime := stat.total / time.Duration(stat.count)
			successRate := float64(stat.success) / float64(stat.count) * 100
			fmt.Printf("  %s: %d ops, avg: %v, success: %.1f%%\n", 
				opType, stat.count, avgTime, successRate)
		}
		
		// Find slowest operations
		if len(t.YDBOperations) > 0 {
			fmt.Printf("\n🐌 Slowest YDB Operations:\n")
			// Sort by duration (simplified - show first 5 slowest)
			slowest := make([]YDBOperation, 0, 5)
			for _, op := range t.YDBOperations {
				if len(slowest) < 5 || op.Duration > slowest[len(slowest)-1].Duration {
					slowest = append(slowest, op)
					if len(slowest) > 5 {
						// Simple sort (keep top 5)
						for i := len(slowest) - 1; i > 0 && slowest[i].Duration > slowest[i-1].Duration; i-- {
							slowest[i], slowest[i-1] = slowest[i-1], slowest[i]
						}
						slowest = slowest[:5]
					}
				}
			}
			for _, op := range slowest {
				fmt.Printf("  %s (match=%s, event=%s): %v\n", 
					op.Operation, op.MatchID[:min(8, len(op.MatchID))], 
					op.EventID[:min(8, len(op.EventID))], op.Duration)
			}
		}
	}
	
	fmt.Println(strings.Repeat("=", 80) + "\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// MetricsResponse represents the JSON response structure for /metrics endpoint
type MetricsResponse struct {
	Overall struct {
		TotalRuns     int     `json:"total_runs"`
		TotalMatches  int     `json:"total_matches"`
		TotalEvents   int     `json:"total_events"`
		TotalOutcomes int     `json:"total_outcomes"`
	} `json:"overall"`
	
	Timing struct {
		TotalDuration      string  `json:"total_duration"`
		HTTPFetchDuration  string  `json:"http_fetch_duration"`
		JSONParseDuration  string  `json:"json_parse_duration"`
		GroupingDuration   string  `json:"grouping_duration"`
		ProcessingDuration string  `json:"processing_duration"`
		YDBWriteDuration   string  `json:"ydb_write_duration"`
		
		HTTPFetchPercent  float64 `json:"http_fetch_percent"`
		JSONParsePercent  float64 `json:"json_parse_percent"`
		GroupingPercent   float64 `json:"grouping_percent"`
		ProcessingPercent float64 `json:"processing_percent"`
		YDBWritePercent   float64 `json:"ydb_write_percent"`
	} `json:"timing"`
	
	PerMatch struct {
		ProcessedMatches int     `json:"processed_matches"`
		SuccessRate      float64 `json:"success_rate"`
		AvgEventsPerMatch float64 `json:"avg_events_per_match"`
		AvgOutcomesPerMatch float64 `json:"avg_outcomes_per_match"`
		AvgBuildTime     string  `json:"avg_build_time"`
		AvgStoreTime     string  `json:"avg_store_time"`
		AvgTotalTime     string  `json:"avg_total_time"`
	} `json:"per_match"`
	
	YDBOperations map[string]struct {
		Count      int     `json:"count"`
		AvgTime    string  `json:"avg_time"`
		SuccessRate float64 `json:"success_rate"`
	} `json:"ydb_operations"`
	
	SlowestOperations []struct {
		Operation string `json:"operation"`
		MatchID    string `json:"match_id"`
		EventID    string `json:"event_id"`
		Duration   string `json:"duration"`
	} `json:"slowest_operations"`
}

// GetMetrics returns structured metrics for JSON API
func (t *Tracker) GetMetrics() MetricsResponse {
	t.mu.RLock()
	defer t.mu.RUnlock()
	
	var resp MetricsResponse
	
	// Overall statistics
	resp.Overall.TotalRuns = t.TotalRuns
	resp.Overall.TotalMatches = t.TotalMatches
	resp.Overall.TotalEvents = t.TotalEvents
	resp.Overall.TotalOutcomes = t.TotalOutcomes
	
	// Timing statistics
	if t.TotalRuns > 0 {
		avgTotal := t.TotalDuration / time.Duration(t.TotalRuns)
		avgHTTPFetch := t.HTTPFetchDuration / time.Duration(t.TotalRuns)
		avgJSONParse := t.JSONParseDuration / time.Duration(t.TotalRuns)
		avgGrouping := t.GroupingDuration / time.Duration(t.TotalRuns)
		avgProcessing := t.ProcessingDuration / time.Duration(t.TotalRuns)
		avgYDBWrite := t.YDBWriteDuration / time.Duration(t.TotalRuns)
		
		resp.Timing.TotalDuration = avgTotal.String()
		resp.Timing.HTTPFetchDuration = avgHTTPFetch.String()
		resp.Timing.JSONParseDuration = avgJSONParse.String()
		resp.Timing.GroupingDuration = avgGrouping.String()
		resp.Timing.ProcessingDuration = avgProcessing.String()
		resp.Timing.YDBWriteDuration = avgYDBWrite.String()
		
		if t.TotalDuration > 0 {
			resp.Timing.HTTPFetchPercent = float64(t.HTTPFetchDuration) / float64(t.TotalDuration) * 100
			resp.Timing.JSONParsePercent = float64(t.JSONParseDuration) / float64(t.TotalDuration) * 100
			resp.Timing.GroupingPercent = float64(t.GroupingDuration) / float64(t.TotalDuration) * 100
			resp.Timing.ProcessingPercent = float64(t.ProcessingDuration) / float64(t.TotalDuration) * 100
			resp.Timing.YDBWritePercent = float64(t.YDBWriteDuration) / float64(t.TotalDuration) * 100
		}
	}
	
	// Per-match statistics
	if len(t.MatchTimings) > 0 {
		var totalMatchTime, totalBuildTime, totalStoreTime time.Duration
		successCount := 0
		totalEvents, totalOutcomes := 0, 0
		
		for _, mt := range t.MatchTimings {
			totalMatchTime += mt.TotalTime
			totalBuildTime += mt.BuildTime
			totalStoreTime += mt.StoreTime
			totalEvents += mt.EventsCount
			totalOutcomes += mt.OutcomesCount
			if mt.Success {
				successCount++
			}
		}
		
		avgMatches := float64(len(t.MatchTimings))
		resp.PerMatch.ProcessedMatches = len(t.MatchTimings)
		resp.PerMatch.SuccessRate = float64(successCount) / avgMatches * 100
		resp.PerMatch.AvgEventsPerMatch = float64(totalEvents) / avgMatches
		resp.PerMatch.AvgOutcomesPerMatch = float64(totalOutcomes) / avgMatches
		resp.PerMatch.AvgBuildTime = (totalBuildTime / time.Duration(len(t.MatchTimings))).String()
		resp.PerMatch.AvgStoreTime = (totalStoreTime / time.Duration(len(t.MatchTimings))).String()
		resp.PerMatch.AvgTotalTime = (totalMatchTime / time.Duration(len(t.MatchTimings))).String()
	}
	
	// YDB operations statistics
	resp.YDBOperations = make(map[string]struct {
		Count      int     `json:"count"`
		AvgTime    string  `json:"avg_time"`
		SuccessRate float64 `json:"success_rate"`
	})
	
	if len(t.YDBOperations) > 0 {
		opsByType := make(map[string]struct {
			count   int
			total   time.Duration
			success int
		})
		
		for _, op := range t.YDBOperations {
			stat := opsByType[op.Operation]
			stat.count++
			stat.total += op.Duration
			if op.Success {
				stat.success++
			}
			opsByType[op.Operation] = stat
		}
		
		for opType, stat := range opsByType {
			avgTime := stat.total / time.Duration(stat.count)
			successRate := float64(stat.success) / float64(stat.count) * 100
			resp.YDBOperations[opType] = struct {
				Count      int     `json:"count"`
				AvgTime    string  `json:"avg_time"`
				SuccessRate float64 `json:"success_rate"`
			}{
				Count:       stat.count,
				AvgTime:     avgTime.String(),
				SuccessRate: successRate,
			}
		}
		
		// Find slowest operations (top 5)
		slowest := make([]YDBOperation, 0, 5)
		for _, op := range t.YDBOperations {
			if len(slowest) < 5 || op.Duration > slowest[len(slowest)-1].Duration {
				slowest = append(slowest, op)
				if len(slowest) > 5 {
					// Simple sort (keep top 5)
					for i := len(slowest) - 1; i > 0 && slowest[i].Duration > slowest[i-1].Duration; i-- {
						slowest[i], slowest[i-1] = slowest[i-1], slowest[i]
					}
					slowest = slowest[:5]
				}
			}
		}
		
		resp.SlowestOperations = make([]struct {
			Operation string `json:"operation"`
			MatchID    string `json:"match_id"`
			EventID    string `json:"event_id"`
			Duration   string `json:"duration"`
		}, 0, len(slowest))
		
		for _, op := range slowest {
			matchID := op.MatchID
			if len(matchID) > 16 {
				matchID = matchID[:16]
			}
			eventID := op.EventID
			if len(eventID) > 16 {
				eventID = eventID[:16]
			}
			resp.SlowestOperations = append(resp.SlowestOperations, struct {
				Operation string `json:"operation"`
				MatchID    string `json:"match_id"`
				EventID    string `json:"event_id"`
				Duration   string `json:"duration"`
			}{
				Operation: op.Operation,
				MatchID:   matchID,
				EventID:   eventID,
				Duration:  op.Duration.String(),
			})
		}
	}
	
	return resp
}
