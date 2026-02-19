package marathonbet

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
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
	baseURL           string
	userAgent         string
	timeout           time.Duration
	client            *http.Client
	proxyList         []string
	currentProxyIndex int
	proxyMu           sync.Mutex
}

// NewClient creates a Marathonbet HTTP client.
func NewClient(baseURL, userAgent string, timeout time.Duration, proxyList []string) *Client {
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

	insecureTLS := os.Getenv("MARATHONBET_INSECURE_TLS") == "1"

	// Create default transport (without proxy - we'll use proxy per request)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	if insecureTLS {
		transport.TLSClientConfig.InsecureSkipVerify = true
	}
	transport.Proxy = http.ProxyFromEnvironment

	return &Client{
		baseURL:           baseURL,
		userAgent:         userAgent,
		timeout:           timeout,
		client:            &http.Client{Timeout: timeout, Transport: transport},
		proxyList:         proxyList,
		currentProxyIndex: 0,
	}
}

// Get fetches a path (e.g. /su/all-events/11) and returns the response body.
// Includes global rate limiting (500ms minimum delay) and handles 429 with forced backoff.
// If proxyList is configured, tries proxies in order before falling back to direct connection.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	// Try proxies in order if available, fallback to direct connection
	if len(c.proxyList) > 0 {
		return c.getWithProxyRetry(ctx, path)
	}

	return c.getDirect(ctx, path)
}

// getDirect performs a direct HTTP request without proxy
func (c *Client) getDirect(ctx context.Context, path string) ([]byte, error) {
	// Rate limit: wait if last request was too recent
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

	requestURL := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

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

	return c.handleResponse(resp, body, path)
}

// getWithProxyRetry tries each proxy in the list until one works
func (c *Client) getWithProxyRetry(ctx context.Context, path string) ([]byte, error) {
	requestURL := c.baseURL + path

	// Rate limit: wait if last request was too recent
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

	// Try each proxy in the list
	c.proxyMu.Lock()
	startIndex := c.currentProxyIndex
	c.proxyMu.Unlock()

	for attempt := 0; attempt < len(c.proxyList); attempt++ {
		c.proxyMu.Lock()
		proxyIndex := (startIndex + attempt) % len(c.proxyList)
		proxyURLStr := c.proxyList[proxyIndex]
		c.proxyMu.Unlock()

		proxyURL, err := url.Parse(proxyURLStr)
		if err != nil {
			continue
		}

		// Create transport with this proxy
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		}
		if os.Getenv("MARATHONBET_INSECURE_TLS") == "1" {
			transport.TLSClientConfig.InsecureSkipVerify = true
		}
		transport.Proxy = http.ProxyURL(proxyURL)

		client := &http.Client{
			Timeout:   c.timeout,
			Transport: transport,
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			continue
		}

		c.setHeaders(req)

		resp, err := client.Do(req)
		if err != nil {
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}

		// Check if response is valid HTML (not blocking page)
		// For Marathonbet, we expect HTML content, not JSON
		isHTML := len(body) > 0 && body[0] == '<'
		isBlocked := strings.Contains(string(body), "TEMPLATE_NAME") && strings.Contains(string(body), "denied")

		if resp.StatusCode == http.StatusOK && isHTML && !isBlocked {
			// Success! Update current proxy index
			c.proxyMu.Lock()
			c.currentProxyIndex = proxyIndex
			c.proxyMu.Unlock()
			slog.Info("Marathonbet: Using working proxy", "proxy", maskProxyURL(proxyURLStr))

			// Update last request time
			marathonReqMu.Lock()
			marathonLastReq = time.Now()
			marathonReqMu.Unlock()

			return body, nil
		}

		// Not valid HTML or blocked - try next proxy
	}

	// All proxies failed, try direct connection as last resort
	slog.Warn("Marathonbet: All proxies failed, trying direct connection")
	return c.getDirect(ctx, path)
}

// setHeaders sets HTTP headers for requests
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en;q=0.8")
}

// handleResponse processes HTTP response and returns body or error
func (c *Client) handleResponse(resp *http.Response, body []byte, path string) ([]byte, error) {
	if resp.StatusCode == http.StatusOK {
		return body, nil
	}

	// On 429, force 3s backoff before next request
	if resp.StatusCode == http.StatusTooManyRequests {
		marathonReqMu.Lock()
		marathonLastReq = time.Now().Add(3 * time.Second) // force 3s pause before next request
		marathonReqMu.Unlock()
		slog.Warn("Marathonbet: rate limited (429), backing off 3s", "path", path)
		return nil, fmt.Errorf("marathonbet: GET %s: status %d", path, resp.StatusCode)
	}

	// Log response body for non-OK status codes to help debug (especially 403 Cloudflare blocks)
	bodyStr := string(body)
	if len(bodyStr) > 500 {
		bodyStr = bodyStr[:500] + "..."
	}
	slog.Warn("Marathonbet: HTTP error response", 
		"path", path, 
		"status", resp.StatusCode,
		"body_preview", bodyStr)

	return nil, fmt.Errorf("marathonbet: GET %s: status %d", path, resp.StatusCode)
}

// maskProxyURL masks password in proxy URL for logging
func maskProxyURL(proxyURL string) string {
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return "***"
	}
	if parsed.User != nil {
		if password, ok := parsed.User.Password(); ok {
			masked := strings.Repeat("*", len(password))
			parsed.User = url.UserPassword(parsed.User.Username(), masked)
		}
	}
	return parsed.String()
}
