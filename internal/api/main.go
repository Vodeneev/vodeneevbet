package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

type APIServer struct {
	ydbClient *storage.YDBClient
	config    *config.Config
}

func NewAPIServer(ydbClient *storage.YDBClient, config *config.Config) *APIServer {
	return &APIServer{
		ydbClient: ydbClient,
		config:    config,
	}
}

func (s *APIServer) Start() error {
	// Serve static files
	http.Handle("/", http.FileServer(http.Dir("./static/")))
	
	// API endpoints
	http.HandleFunc("/api/odds", s.handleOdds)
	http.HandleFunc("/api/matches", s.handleMatches)
	
	fmt.Println("Starting API server on :8081")
	fmt.Println("Open http://localhost:8081 in your browser")
	return http.ListenAndServe(":8081", nil)
}

func (s *APIServer) handleOdds(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	// Mock data for demonstration
	odds := []models.Odd{
		{
			MatchID:   "match_1",
			Bookmaker: "Fonbet",
			Market:    "1x2",
			Outcomes: map[string]float64{
				"home": 1.85,
				"draw": 3.20,
				"away": 4.10,
			},
			UpdatedAt: time.Now(),
			MatchName: "Real Madrid vs Barcelona",
			MatchTime: time.Now().Add(2 * time.Hour),
			Sport:     "football",
		},
		{
			MatchID:   "match_2",
			Bookmaker: "Fonbet",
			Market:    "Corners",
			Outcomes: map[string]float64{
				"total_+5.5": 1.06,
				"total_-5.5": 10.0,
				"alt_total_+4.5": 1.5,
				"alt_total_-4.5": 2.6,
			},
			UpdatedAt: time.Now(),
			MatchName: "Manchester United vs Liverpool",
			MatchTime: time.Now().Add(4 * time.Hour),
			Sport:     "football",
		},
	}
	
	json.NewEncoder(w).Encode(odds)
}

func (s *APIServer) handleMatches(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	// Mock data for demonstration
	matches := []string{
		"Real Madrid vs Barcelona",
		"Manchester United vs Liverpool", 
		"Bayern Munich vs Borussia Dortmund",
	}
	
	json.NewEncoder(w).Encode(matches)
}

func main() {
	// Load config
	cfg, err := config.Load("../../configs/local.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	
	// Create YDB client (mock for now)
	ydbClient, err := storage.NewYDBClient(&cfg.YDB)
	if err != nil {
		log.Fatalf("Failed to connect to YDB: %v", err)
	}
	defer ydbClient.Close()
	
	// Create and start API server
	server := NewAPIServer(ydbClient, cfg)
	log.Fatal(server.Start())
}