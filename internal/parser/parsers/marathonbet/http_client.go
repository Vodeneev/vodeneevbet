package marathonbet

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// Client fetches Marathonbet HTML pages.
type Client struct {
	baseURL   string
	userAgent string
	timeout   time.Duration
	client    *http.Client
}

// NewClient creates a Marathonbet HTTP client.
func NewClient(baseURL, userAgent string, timeout time.Duration) *Client {
	if baseURL == "" {
		baseURL = "https://www.marathonbet.ru"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL:   baseURL,
		userAgent: userAgent,
		timeout:   timeout,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     30 * time.Second,
				DisableCompression:  false,
				DisableKeepAlives:   false,
			},
		},
	}
}

// Get fetches a path (e.g. /su/all-events/11) and returns the response body.
// Retries on 429 (Too Many Requests) with exponential backoff.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	const maxRetries = 3
	const initialDelay = 2 * time.Second
	
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 2s, 4s, 8s
			delay := initialDelay * time.Duration(1<<uint(attempt-1))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		
		url := c.baseURL + path
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.userAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en;q=0.8")

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		
		if resp.StatusCode == http.StatusOK {
			return body, readErr
		}
		
		// Retry on 429, fail immediately on other errors
		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxRetries {
			lastErr = fmt.Errorf("marathonbet: GET %s: status %d (retrying)", path, resp.StatusCode)
			continue
		}
		
		if readErr != nil {
			return nil, readErr
		}
		return nil, fmt.Errorf("marathonbet: GET %s: status %d", path, resp.StatusCode)
	}
	return nil, lastErr
}
