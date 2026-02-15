package calculator

import (
	"context"
	"encoding/json"
	"errors"
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

// GetMatches fetches all matches from the parser's /matches endpoint.
// Retries up to 3 times on transient errors (EOF, connection reset) with 2s backoff.
func (c *HTTPMatchesClient) GetMatches(ctx context.Context) ([]models.Match, error) {
	const maxAttempts = 3
	const backoff = 2 * time.Second
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		matches, err := c.fetchMatches(ctx)
		if err == nil {
			return matches, nil
		}
		lastErr = err
		if !isRetriableFetchError(err) || attempt == maxAttempts {
			return nil, err
		}
		if attempt < maxAttempts {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
	}
	return nil, lastErr
}

// isRetriableFetchError returns true for transient network errors (EOF, connection reset).
func isRetriableFetchError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "EOF") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "connection refused")
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
