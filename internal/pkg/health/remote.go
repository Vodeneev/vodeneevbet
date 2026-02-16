package health

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/health/handlers"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// RemoteParser calls a bookmaker service's /parse endpoint (implements interfaces.Parser for orchestrator).
type RemoteParser struct {
	name       string
	baseURL    string
	httpClient *http.Client
}

// NewRemoteParser creates a parser that triggers parsing via HTTP GET baseURL/parse.
func NewRemoteParser(name, baseURL string, timeout time.Duration) *RemoteParser {
	baseURL = strings.TrimSuffix(baseURL, "/")
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &RemoteParser{
		name:    name,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Start is a no-op for remote parser (orchestrator only calls ParseOnce periodically).
func (p *RemoteParser) Start(ctx context.Context) error {
	return nil
}

// Stop is a no-op.
func (p *RemoteParser) Stop() error {
	return nil
}

// GetName returns the bookmaker name.
func (p *RemoteParser) GetName() string {
	return p.name
}

// ParseOnce triggers GET baseURL/parse on the bookmaker service.
func (p *RemoteParser) ParseOnce(ctx context.Context) error {
	u, err := url.Parse(p.baseURL + "/parse")
	if err != nil {
		return fmt.Errorf("invalid base URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s/parse: %w", p.baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s/parse returned %d: %s", p.baseURL, resp.StatusCode, string(body))
	}
	return nil
}

// matchesResponse is the JSON response from /matches endpoint.
type matchesResponse struct {
	Matches []models.Match `json:"matches"`
	Meta    struct {
		Count    int    `json:"count"`
		Duration string `json:"duration"`
		Source   string `json:"source"`
	} `json:"meta"`
}

// AggregateMatches fetches /matches from each bookmaker service in parallel and merges results.
// services: bookmaker name -> base URL (e.g. "fonbet" -> "http://fonbet:8080").
func AggregateMatches(ctx context.Context, services map[string]string, timeout time.Duration) []models.Match {
	if len(services) == 0 {
		return nil
	}
	client := &http.Client{Timeout: timeout}
	var mu sync.Mutex
	var lists [][]models.Match
	var wg sync.WaitGroup
	for name, baseURL := range services {
		name, baseURL := name, strings.TrimSuffix(baseURL, "/")
		wg.Add(1)
		go func() {
			defer wg.Done()
			matches, err := fetchMatches(ctx, client, baseURL)
			if err != nil {
				slog.Warn("Failed to fetch matches from bookmaker service", "name", name, "url", baseURL, "error", err)
				return
			}
			mu.Lock()
			lists = append(lists, matches)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return MergeMatchLists(lists)
}

func fetchMatches(ctx context.Context, client *http.Client, baseURL string) ([]models.Match, error) {
	u, err := url.Parse(baseURL + "/matches")
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	var mr matchesResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, err
	}
	return mr.Matches, nil
}

// RemoteParsers builds a slice of interfaces.Parser for orchestrator from bookmaker_services config.
func RemoteParsers(services map[string]string, timeout time.Duration) []interfaces.Parser {
	out := make([]interfaces.Parser, 0, len(services))
	for name, baseURL := range services {
		if name == "" || baseURL == "" {
			continue
		}
		out = append(out, NewRemoteParser(name, baseURL, timeout))
	}
	return out
}

// SetMatchesAggregator sets GetMatchesFunc to fetch from bookmaker services and merge (orchestrator mode).
func SetMatchesAggregator(services map[string]string, timeout time.Duration) {
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	handlers.SetGetMatchesFunc(func() []models.Match {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		return AggregateMatches(ctx, services, timeout)
	})
	handlers.SetGetEsportsMatchesFunc(func() []models.EsportsMatch {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		return AggregateEsportsMatches(ctx, services, timeout)
	})
}

// esportsMatchesResponse is the JSON response from /esports/matches endpoint
type esportsMatchesResponse struct {
	Matches []models.EsportsMatch `json:"matches"`
	Meta    struct {
		Count    int    `json:"count"`
		Duration string `json:"duration"`
		Source   string `json:"source"`
	} `json:"meta"`
}

// AggregateEsportsMatches fetches /esports/matches from each bookmaker service in parallel and merges.
func AggregateEsportsMatches(ctx context.Context, services map[string]string, timeout time.Duration) []models.EsportsMatch {
	if len(services) == 0 {
		return nil
	}
	client := &http.Client{Timeout: timeout}
	var mu sync.Mutex
	// name -> matches, to log per-service counts and merge
	byService := make(map[string][]models.EsportsMatch)
	var wg sync.WaitGroup
	for name, baseURL := range services {
		name, baseURL := name, strings.TrimSuffix(baseURL, "/")
		wg.Add(1)
		go func() {
			defer wg.Done()
			matches, err := fetchEsportsMatches(ctx, client, baseURL)
			if err != nil {
				slog.Warn("Failed to fetch esports matches from bookmaker service", "name", name, "url", baseURL, "error", err)
				mu.Lock()
				byService[name] = nil
				mu.Unlock()
				return
			}
			mu.Lock()
			byService[name] = matches
			mu.Unlock()
		}()
	}
	wg.Wait()
	var lists [][]models.EsportsMatch
	for name, matches := range byService {
		if len(matches) > 0 {
			lists = append(lists, matches)
			slog.Info("Esports from bookmaker service", "name", name, "count", len(matches))
		} else if matches != nil {
			slog.Info("Esports from bookmaker service (empty)", "name", name)
		}
	}
	return MergeEsportsMatchLists(lists)
}

func fetchEsportsMatches(ctx context.Context, client *http.Client, baseURL string) ([]models.EsportsMatch, error) {
	u, err := url.Parse(baseURL + "/esports/matches")
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	var mr esportsMatchesResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, err
	}
	return mr.Matches, nil
}
