package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

type APIServer struct {
	parserURL  string
	httpClient *http.Client
	config     *config.Config
}

func NewAPIServer(parserURL string, config *config.Config) *APIServer {
	return &APIServer{
		parserURL: parserURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		config: config,
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

	matches, err := s.fetchMatchesFromParser(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get matches from parser: %v", err), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(matches)
}

func (s *APIServer) handleMatches(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	matches, err := s.fetchMatchesFromParser(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get matches from parser: %v", err), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(matches)
}

func (s *APIServer) fetchMatchesFromParser(ctx context.Context) ([]models.Match, error) {
	if s.parserURL == "" {
		return nil, fmt.Errorf("parser URL is not configured")
	}

	baseURL := strings.TrimSuffix(s.parserURL, "/")
	u, err := url.Parse(baseURL + "/matches")
	if err != nil {
		return nil, fmt.Errorf("invalid parser URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch matches: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var matchesResp struct {
		Matches []models.Match `json:"matches"`
		Meta    struct {
			Count    int    `json:"count"`
			Duration string `json:"duration"`
			Source   string `json:"source"`
		} `json:"meta"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&matchesResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return matchesResp.Matches, nil
}

func main() {
	var configPath string
	var parserURL string
	flag.StringVar(&configPath, "config", "../../configs/production.yaml", "Path to config file")
	flag.StringVar(&parserURL, "parser-url", "", "URL to parser service (e.g. http://localhost:8080)")
	flag.Parse()

	// Load config
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Use parser URL from flag, config, or default
	if parserURL == "" {
		parserURL = cfg.ValueCalculator.ParserURL
	}
	if parserURL == "" {
		parserURL = "http://localhost:8080"
		log.Printf("Using default parser URL: %s", parserURL)
	}

	// Create and start API server
	server := NewAPIServer(parserURL, cfg)
	log.Fatal(server.Start())
}
