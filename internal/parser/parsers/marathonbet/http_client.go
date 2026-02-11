package marathonbet

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"strings"
	"time"
)

const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// Global rate limiting (similar to Pinnacle888 oddsRateLimit)
var (
	marathonReqMu   sync.Mutex
	marathonLastReq time.Time
)

// marathonMinDelay enforces minimum delay between requests to avoid 429 rate limiting.
const marathonMinDelay = 500 * time.Millisecond

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
// Includes global rate limiting (500ms minimum delay) and handles 429 with forced backoff.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	// Rate limit: wait if last request was too recent (similar to Pinnacle888)
	marathonReqMu.Lock()
	sinceLastReq := time.Since(marathonLastReq)
	if sinceLastReq < marathonMinDelay {
		wait := marathonMinDelay - sinceLastReq
		marathonReqMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		marathonReqMu.Lock()
	}
	marathonReqMu.Unlock()

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
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Update last request time
	marathonReqMu.Lock()
	marathonLastReq = time.Now()
	marathonReqMu.Unlock()

	if resp.StatusCode == http.StatusOK {
		return body, nil
	}

	// On 429, force 3s backoff before next request (similar to Pinnacle888)
	if resp.StatusCode == http.StatusTooManyRequests {
		marathonReqMu.Lock()
		marathonLastReq = time.Now().Add(3 * time.Second) // force 3s pause before next request
		marathonReqMu.Unlock()
		slog.Warn("Marathonbet: rate limited (429), backing off 3s", "path", path)
		return nil, fmt.Errorf("marathonbet: GET %s: status %d", path, resp.StatusCode)
	}

	return nil, fmt.Errorf("marathonbet: GET %s: status %d", path, resp.StatusCode)
}
