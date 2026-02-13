package xbet1

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/chromedp/chromedp"
	"github.com/klauspost/compress/zstd"
)

// chromeMu serializes all Chrome usage so only one instance runs at a time
var chromeMu sync.Mutex

// fallbackBaseURL is used when mirror resolution fails
const fallbackBaseURL = "https://1xlite-6173396.bar"

type Client struct {
	baseURL        string
	mirrorURL      string // Mirror URL to resolve actual baseURL
	httpClient     *http.Client
	proxyList      []string
	currentProxyIndex int
	proxyMu        sync.Mutex
	resolvedURL    string // Cached resolved URL
	resolvedMu     sync.RWMutex
	resolveTimeout time.Duration
	lastResolveTime time.Time
	resolveInterval time.Duration
	resolveMu      sync.Mutex
	resolveCond    *sync.Cond
	resolving      bool
}

// resolveMirror resolves the actual URL from mirror link
// First tries HTTP redirects, then falls back to JavaScript execution via headless browser
func resolveMirror(mirrorURL string, timeout time.Duration) (string, error) {
	// First, try simple HTTP redirect
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	if os.Getenv("1XBET_INSECURE_TLS") == "1" {
		transport.TLSClientConfig.InsecureSkipVerify = true
	}
	// Increase dial timeout for VM network issues (default is 30s, use 60s)
	transport.DialContext = (&net.Dialer{
		Timeout: 60 * time.Second,
	}).DialContext
	transport.TLSHandshakeTimeout = 30 * time.Second

	// Use longer timeout for mirror resolution (intermediate redirects may be slow)
	resolveTimeout := timeout
	if resolveTimeout < 180*time.Second {
		resolveTimeout = 180 * time.Second
	}

	client := &http.Client{
		Timeout:   resolveTimeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return nil
		},
	}

	// Use HEAD request first
	req, err := http.NewRequest(http.MethodHead, mirrorURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		slog.Info("HTTP HEAD request failed, trying GET", "error", err)
		// Continue to GET request
	} else {
		defer resp.Body.Close()
		finalURL := resp.Request.URL.String()
		if finalURL != mirrorURL {
			// Accept HTTP redirect result (even if it's an IP) - simpler and works on VM without Chrome
			slog.Info("Resolved mirror", "from", mirrorURL, "to", finalURL, "method", "HTTP redirect")
			return finalURL, nil
		}
	}
	// If HEAD didn't redirect or failed, try GET
	req, err = http.NewRequest(http.MethodGet, mirrorURL, nil)
	if err != nil {
		return resolveMirrorWithJS(mirrorURL, timeout)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")

	resp, err = client.Do(req)
	if err != nil {
		slog.Info("HTTP GET request failed, falling back to JavaScript resolution", "error", err)
		return resolveMirrorWithJS(mirrorURL, timeout)
	}
	defer resp.Body.Close()

	finalURL := resp.Request.URL.String()
	if finalURL != mirrorURL {
		// Accept HTTP redirect result (even if it's an IP) - simpler and works on VM without Chrome
		slog.Info("Resolved mirror", "from", mirrorURL, "to", finalURL, "method", "HTTP redirect")
		return finalURL, nil
	}

	// Check if we got HTML (might need JavaScript execution)
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") {
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			bodyStr := string(body)
			if strings.Contains(bodyStr, "<script") || strings.Contains(bodyStr, "window.location") ||
				strings.Contains(bodyStr, "location.href") || strings.Contains(bodyStr, "document.location") {
				slog.Debug("Detected JavaScript redirect, using headless browser")
				return resolveMirrorWithJS(mirrorURL, timeout)
			}
		}
	}

	slog.Debug("1xbet: HTTP redirect didn't work, trying JavaScript resolution...")
	return resolveMirrorWithJS(mirrorURL, timeout)
}

// resolveMirrorWithJS uses headless browser to execute JavaScript and get final URL
func resolveMirrorWithJS(mirrorURL string, timeout time.Duration) (string, error) {
	chromeMu.Lock()
	defer chromeMu.Unlock()

	chromeDir, err := os.MkdirTemp("", "xbet1_chrome_")
	if err != nil {
		return "", fmt.Errorf("create chrome temp dir: %w", err)
	}
	defer os.RemoveAll(chromeDir)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.UserDataDir(chromeDir),
		chromedp.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36"),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(ctx, opts...)
	defer cancel()

	ctx, cancel = chromedp.NewContext(allocCtx, chromedp.WithLogf(func(format string, v ...interface{}) {
		if os.Getenv("1XBET_DEBUG") == "1" {
			slog.Debug("chromedp", "message", fmt.Sprintf(format, v...))
		}
	}))
	defer cancel()

	var finalURL string

	err = chromedp.Run(ctx,
		chromedp.Navigate(mirrorURL),
		chromedp.Sleep(3*time.Second),
		chromedp.Location(&finalURL),
	)

	if err != nil {
		return "", fmt.Errorf("chromedp navigation: %w", err)
	}

	if finalURL != "" && finalURL != mirrorURL {
		var checkURL string
		err = chromedp.Run(ctx,
			chromedp.Sleep(2*time.Second),
			chromedp.Location(&checkURL),
		)
		if err == nil && checkURL != "" && checkURL != finalURL {
			finalURL = checkURL
		}

		slog.Debug("Resolved mirror", "from", mirrorURL, "to", finalURL, "method", "JavaScript redirect")
		return finalURL, nil
	}

	if finalURL == "" || finalURL == mirrorURL {
		err = chromedp.Run(ctx,
			chromedp.Sleep(5*time.Second),
			chromedp.Location(&finalURL),
		)
		if err != nil {
			return "", fmt.Errorf("chromedp wait: %w", err)
		}
	}

	if finalURL != "" && finalURL != mirrorURL {
		slog.Debug("1xbet: Resolved mirror (JavaScript redirect after wait)", "from", mirrorURL, "to", finalURL)
		return finalURL, nil
	}

	if finalURL != "" {
		slog.Debug("1xbet: Mirror URL did not redirect", "url", finalURL)
		return finalURL, nil
	}

	// Fallback to known working base URL if resolution fails
	slog.Warn("1xbet: mirror resolution failed, using fallback base URL", "mirror_url", mirrorURL, "fallback", fallbackBaseURL)
	return fallbackBaseURL, nil
}

// isIPAddress checks if a string is an IP address (IPv4 or IPv6)
func isIPAddress(s string) bool {
	return net.ParseIP(s) != nil
}

// normalizeResolvedBaseURL returns scheme://host from a full redirect URL (no path/query, no default port).
// e.g. https://1xlite-6173396.bar:443/ru/registration?tag=... -> https://1xlite-6173396.bar
func normalizeResolvedBaseURL(resolved string) string {
	u, err := url.Parse(resolved)
	if err != nil {
		return resolved
	}
	host := u.Hostname()
	port := u.Port()
	if port != "" && port != "80" && port != "443" {
		host = net.JoinHostPort(u.Hostname(), port)
	}
	return u.Scheme + "://" + host
}

// ResolveMirrorToBaseURL resolves mirror URL to the actual 1xbet base URL (scheme://host).
// Can be used by scripts/cron to get a fixed base_url for XBET1_BASE_URL env.
func ResolveMirrorToBaseURL(mirrorURL string, timeout time.Duration) (baseURL string, err error) {
	resolved, err := resolveMirror(mirrorURL, timeout)
	if err != nil {
		return "", err
	}
	return normalizeResolvedBaseURL(resolved), nil
}

func NewClient(baseURL, mirrorURL string, timeout time.Duration, proxyList []string) *Client {
	insecureTLS := os.Getenv("1XBET_INSECURE_TLS") == "1"

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableCompression = true // we send Accept-Encoding and decode in readBodyDecode
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	if insecureTLS {
		transport.TLSClientConfig.InsecureSkipVerify = true
	}

	transport.Proxy = http.ProxyFromEnvironment

	client := &Client{
		baseURL:           baseURL,
		mirrorURL:         mirrorURL,
		httpClient:        &http.Client{Timeout: timeout, Transport: transport},
		proxyList:         proxyList,
		currentProxyIndex: 0,
		resolveTimeout:    timeout,
		resolveInterval:   2 * time.Hour,
	}
	
	client.resolveCond = sync.NewCond(&client.resolveMu)

	return client
}

// ensureResolved ensures that mirror URL is resolved and cached
func (c *Client) ensureResolved() error {
	if c.mirrorURL == "" {
		return nil
	}

	c.resolveMu.Lock()
	for c.resolving {
		c.resolveCond.Wait()
	}
	c.resolvedMu.RLock()
	hasResolved := c.resolvedURL != ""
	lastResolve := c.lastResolveTime
	resolvedURL := c.resolvedURL
	c.resolvedMu.RUnlock()

	if hasResolved && time.Since(lastResolve) < c.resolveInterval {
		c.resolveMu.Unlock()
		return nil
	}
	if hasResolved {
		c.resolveMu.Unlock()
		if c.checkURLHealth(resolvedURL) {
			c.resolvedMu.Lock()
			c.lastResolveTime = time.Now()
			c.resolvedMu.Unlock()
			return nil
		}
		c.resolveMu.Lock()
		slog.Debug("1xbet: Cached URL is not responding, re-resolving mirror", "cached_url", resolvedURL)
	}

	c.resolving = true
	c.resolveMu.Unlock()

	resolved, err := resolveMirror(c.mirrorURL, c.resolveTimeout)

	c.resolveMu.Lock()
	c.resolving = false
	defer func() {
		c.resolveCond.Broadcast()
		c.resolveMu.Unlock()
	}()

	if err != nil {
		if hasResolved {
			slog.Warn("1xbet: mirror re-resolve failed, keeping cached URL", "mirror_url", c.mirrorURL, "error", err, "cached_url", resolvedURL)
			return nil
		}
		// Use fallback base URL if resolution fails and we don't have cached URL
		slog.Warn("1xbet: mirror resolve failed, using fallback base URL", "mirror_url", c.mirrorURL, "error", err, "fallback", fallbackBaseURL)
		base := fallbackBaseURL
		c.resolvedMu.Lock()
		c.resolvedURL = base
		c.lastResolveTime = time.Now()
		c.baseURL = base
		c.resolvedMu.Unlock()
		slog.Info("1xbet: using fallback base URL", "fallback_base", base)
		return nil
	}

	base := normalizeResolvedBaseURL(resolved)
	c.resolvedMu.Lock()
	c.resolvedURL = base
	c.lastResolveTime = time.Now()
	c.baseURL = base
	c.resolvedMu.Unlock()

	slog.Info("1xbet: mirror resolved", "mirror_url", c.mirrorURL, "resolved_base", base)
	return nil
}

// checkURLHealth checks if a URL is accessible
func (c *Client) checkURLHealth(urlStr string) bool {
	req, err := http.NewRequest(http.MethodHead, urlStr, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// clearResolvedURL clears the cached resolved URL to force re-resolution
func (c *Client) clearResolvedURL() {
	c.resolvedMu.Lock()
	defer c.resolvedMu.Unlock()
	if c.resolvedURL != "" {
		slog.Debug("1xbet: Clearing cached URL to force re-resolution", "url", c.resolvedURL)
		c.resolvedURL = ""
	}
}

// getResolvedBaseURL returns the resolved base URL (from mirror or direct)
func (c *Client) getResolvedBaseURL() string {
	if err := c.ensureResolved(); err != nil {
		slog.Debug("1xbet: Warning: failed to ensure resolved URL", "error", err)
	}

	c.resolvedMu.RLock()
	defer c.resolvedMu.RUnlock()
	if c.resolvedURL != "" {
		return c.resolvedURL
	}
	if c.baseURL != "" {
		return c.baseURL
	}
	// Last resort: return fallback base URL
	slog.Warn("1xbet: no resolved or base URL available, using fallback", "fallback", fallbackBaseURL)
	return fallbackBaseURL
}

// shouldReResolve checks if an error indicates that we should re-resolve the mirror URL
func (c *Client) shouldReResolve(err error, statusCode int) bool {
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "connection refused") ||
			strings.Contains(errStr, "no such host") ||
			strings.Contains(errStr, "timeout") ||
			strings.Contains(errStr, "network is unreachable") {
			return true
		}
	}
	if statusCode == 502 || statusCode == 503 {
		return true
	}
	return false
}

// GetChamps fetches championships/leagues
func (c *Client) GetChamps(sportID, countryID int, virtualSports bool) ([]ChampItem, error) {
	baseURL := c.getResolvedBaseURL()
	if baseURL == "" {
		return nil, fmt.Errorf("base URL not resolved")
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	u.Path = "/service-api/LineFeed/GetChampsZip"
	// Query order matters for 1xbet (sport first = 200, other order can yield 406)
	u.RawQuery = fmt.Sprintf("sport=%d&country=%d&virtualSports=%t&groupChamps=true", sportID, countryID, virtualSports)

	body, err := c.doRequest(u.String())
	if err != nil {
		if c.shouldReResolve(err, 0) {
			c.clearResolvedURL()
		}
		return nil, err
	}

	var resp ChampsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal champs response: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("API error: %s (code: %d)", resp.Error, resp.ErrorCode)
	}

	// Flatten grouped championships (countries with sub-leagues)
	var flattened []ChampItem
	for _, champ := range resp.Value {
		if len(champ.SC) > 0 {
			// This is a grouped championship (country), add sub-leagues
			flattened = append(flattened, champ.SC...)
		} else {
			// Regular championship
			flattened = append(flattened, champ)
		}
	}

	return flattened, nil
}

// GetMatches fetches matches for a specific league
func (c *Client) GetMatches(sportID int, champID int64, count int, mode int, countryID int, virtualSports bool) ([]Match, error) {
	baseURL := c.getResolvedBaseURL()
	if baseURL == "" {
		return nil, fmt.Errorf("base URL not resolved")
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	u.Path = "/service-api/LineFeed/Get1x2_VZip"
	// Query order matters for 1xbet (sports first to avoid 406)
	u.RawQuery = fmt.Sprintf("sports=%d&champs=%d&count=%d&mode=%d&country=%d&getEmpty=true&virtualSports=%t", sportID, champID, count, mode, countryID, virtualSports)

	body, err := c.doRequest(u.String())
	if err != nil {
		if c.shouldReResolve(err, 0) {
			c.clearResolvedURL()
		}
		return nil, err
	}

	var resp MatchesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal matches response: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("API error: %s (code: %d)", resp.Error, resp.ErrorCode)
	}

	return resp.Value, nil
}

// GetGame fetches detailed game information
func (c *Client) GetGame(gameID int64, isSubGames, groupEvents bool, countEvents, grMode int, topGroups string, countryID, marketType int, isNewBuilder bool) (*GameDetails, error) {
	baseURL := c.getResolvedBaseURL()
	if baseURL == "" {
		return nil, fmt.Errorf("base URL not resolved")
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	u.Path = "/service-api/LineFeed/GetGameZip"
	// Query order fixed to avoid 406 (id/country first)
	u.RawQuery = fmt.Sprintf("id=%d&isSubGames=%t&GroupEvents=%t&countevents=%d&grMode=%d&topGroups=%s&country=%d&marketType=%d&isNewBuilder=%t", gameID, isSubGames, groupEvents, countEvents, grMode, url.QueryEscape(topGroups), countryID, marketType, isNewBuilder)

	body, err := c.doRequest(u.String())
	if err != nil {
		if c.shouldReResolve(err, 0) {
			c.clearResolvedURL()
		}
		return nil, err
	}

	var resp GameResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal game response: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("API error: %s (code: %d)", resp.Error, resp.ErrorCode)
	}

	return &resp.Value, nil
}

// GetSubGame fetches sub-game data (for statistical events like corners, fouls, etc.)
func (c *Client) GetSubGame(subGameID int64) (*GameDetails, error) {
	return c.GetGame(subGameID, true, true, 250, 4, "", 1, 1, true)
}

// doRequest performs HTTP GET request with proper headers
func (c *Client) doRequest(urlStr string) ([]byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	baseURL := c.getResolvedBaseURL()
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "ru,en;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 YaBrowser/25.12.0.0 Safari/537.36")
	if baseURL != "" {
		req.Header.Set("Referer", baseURL+"/ru/line")
		req.Header.Set("Origin", baseURL)
	}
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("is-srv", "false")
	req.Header.Set("priority", "u=1, i")
	req.Header.Set("sec-ch-ua", `"Chromium";v="142", "YaBrowser";v="25.12", "Not_A Brand";v="99", "Yowser";v="2.5"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"macOS"`)
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-origin")
	// 1xbet specific headers
	req.Header.Set("x-app-n", "__BETTING_APP__")
	req.Header.Set("x-svc-source", "__BETTING_APP__")
	req.Header.Set("x-requested-with", "XMLHttpRequest")
	req.Header.Set("x-mobile-project-id", "0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		preview := string(b)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		slog.Warn("1xbet: API request failed", "url", urlStr, "status", resp.StatusCode, "body_preview", preview)
		if c.shouldReResolve(nil, resp.StatusCode) {
			c.clearResolvedURL()
		}
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(b))
	}

	return readBodyDecode(resp)
}

// readBodyDecode reads response body and decompresses it based on Content-Encoding (gzip, br, zstd).
func readBodyDecode(resp *http.Response) ([]byte, error) {
	enc := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	switch {
	case enc == "br" || strings.Contains(enc, "br"):
		r := brotli.NewReader(resp.Body)
		return io.ReadAll(r)
	case enc == "zstd" || strings.Contains(enc, "zstd"):
		r, err := zstd.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("zstd reader: %w", err)
		}
		defer r.Close()
		return io.ReadAll(r)
	case enc == "gzip" || strings.Contains(enc, "gzip"):
		r, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("gzip reader: %w", err)
		}
		defer r.Close()
		b, err := io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("read gzip body: %w", err)
		}
		return b, nil
	default:
		return io.ReadAll(resp.Body)
	}
}
