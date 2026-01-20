package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
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

	// Healthcheck
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	// Simple ping endpoint (for external monitors)
	http.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong\n"))
	})

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

	// Get real data from YDB
	matches, err := s.ydbClient.GetAllMatches(context.Background())
	if err != nil {
		http.Error(w, "Failed to get matches from database", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(matches)
}

func (s *APIServer) handleMatches(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Get real data from YDB
	matches, err := s.ydbClient.GetAllMatches(context.Background())
	if err != nil {
		http.Error(w, "Failed to get matches from database", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(matches)
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "../../configs/local.yaml", "Path to config file")
	flag.Parse()

	// Load config
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Create YDB client
	ydbClient, err := storage.NewYDBClient(&cfg.YDB)
	if err != nil {
		log.Fatalf("Failed to connect to YDB: %v", err)
	}
	defer ydbClient.Close()

	// Create and start API server
	server := NewAPIServer(ydbClient, cfg)
	log.Fatal(server.Start())
}
