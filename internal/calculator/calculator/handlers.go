package calculator

import "net/http"

// RegisterHTTP registers calculator endpoints onto mux.
func (c *ValueCalculator) RegisterHTTP(mux *http.ServeMux) {
	mux.HandleFunc("/diffs/top", c.handleTopDiffs)
	mux.HandleFunc("/value-bets/top", c.handleTopValueBets)
	mux.HandleFunc("/diffs/status", c.handleStatus)
	mux.HandleFunc("/async/stop", c.handleStopAsync)
	mux.HandleFunc("/async/start", c.handleStartAsync)
}
