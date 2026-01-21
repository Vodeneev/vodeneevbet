package calculator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// HTTPMatchesClient fetches matches from parser's /matches endpoint
type HTTPMatchesClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewHTTPMatchesClient creates a new HTTP client for fetching matches
func NewHTTPMatchesClient(baseURL string) *HTTPMatchesClient {
	if baseURL == "" {
		return nil
	}

	// Ensure baseURL doesn't end with /
	baseURL = strings.TrimSuffix(baseURL, "/")

	return &HTTPMatchesClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// matchesResponse represents the response from /matches endpoint
type matchesResponse struct {
	Matches []models.Match `json:"matches"`
	Meta    struct {
		Count    int    `json:"count"`
		Duration string `json:"duration"`
		Source   string `json:"source"`
	} `json:"meta"`
}

// GetMatches fetches all matches from the parser's /matches endpoint
func (c *HTTPMatchesClient) GetMatches(ctx context.Context) ([]models.Match, error) {
	return c.fetchMatches(ctx)
}

// fetchMatches fetches all matches from the parser's /matches endpoint
func (c *HTTPMatchesClient) fetchMatches(ctx context.Context) ([]models.Match, error) {
	if c == nil {
		return nil, fmt.Errorf("HTTP client is not configured")
	}

	// Build URL without limit parameter
	u, err := url.Parse(c.baseURL + "/matches")
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch matches: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var matchesResp matchesResponse
	if err := json.NewDecoder(resp.Body).Decode(&matchesResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return matchesResp.Matches, nil
}
