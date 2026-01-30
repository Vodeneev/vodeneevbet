package pinnacle888

import (
	"bytes"
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

	"github.com/chromedp/chromedp"
)

// Single Chrome user data dir to avoid accumulating temp dirs (each ~20MB+), which can fill disk on small VMs.
const pinnacle888ChromeUserDataDir = "/tmp/pinnacle888_chrome"

type Client struct {
	baseURL           string
	mirrorURL         string // Mirror URL to resolve actual baseURL
	apiKey            string
	deviceUUID        string
	httpClient        *http.Client
	proxyList         []string
	currentProxyIndex int
	proxyMu           sync.Mutex
	resolvedURL       string // Cached resolved URL
	oddsDomain        string // Cached odds domain (resolved from mirror)
	resolvedMu        sync.RWMutex
	resolveTimeout    time.Duration // Timeout for mirror resolution
	lastResolveTime   time.Time     // When we last resolved the mirror
	resolveInterval   time.Duration // How often to check if resolution is needed
}

// resolveMirror resolves the actual URL from mirror link
// First tries HTTP redirects, then falls back to JavaScript execution via headless browser
func resolveMirror(mirrorURL string, timeout time.Duration) (string, error) {
	// First, try simple HTTP redirect
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	if os.Getenv("PINNACLE_INSECURE_TLS") == "1" {
		transport.TLSClientConfig.InsecureSkipVerify = true
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Follow redirects automatically
			return nil
		},
	}

	// Use HEAD request first to avoid downloading body
	req, err := http.NewRequest(http.MethodHead, mirrorURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		// If HEAD fails, try JavaScript resolution
		return resolveMirrorWithJS(mirrorURL, timeout)
	}
	defer resp.Body.Close()

	// Get final URL after redirects
	finalURL := resp.Request.URL.String()
	if finalURL != mirrorURL {
		// Check if the final URL is an IP address - if so, we need JavaScript resolution
		parsed, err := url.Parse(finalURL)
		if err == nil {
			domain := parsed.Host
			if idx := strings.Index(domain, ":"); idx != -1 {
				domain = domain[:idx]
			}
			if isIPAddress(domain) {
				slog.Debug("HTTP redirect leads to IP address, using JavaScript resolution", "domain", domain)
				return resolveMirrorWithJS(mirrorURL, timeout)
			}
		}
		slog.Debug("Resolved mirror", "from", mirrorURL, "to", finalURL, "method", "HTTP redirect")
		return finalURL, nil
	}

	// If HEAD didn't redirect, try GET
	req, err = http.NewRequest(http.MethodGet, mirrorURL, nil)
	if err != nil {
		return resolveMirrorWithJS(mirrorURL, timeout)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")

	resp, err = client.Do(req)
	if err != nil {
		return resolveMirrorWithJS(mirrorURL, timeout)
	}
	defer resp.Body.Close()

	// Get final URL after GET redirects
	finalURL = resp.Request.URL.String()
	if finalURL != mirrorURL {
		// Check if the final URL is an IP address - if so, we need JavaScript resolution
		parsed, err := url.Parse(finalURL)
		if err == nil {
			domain := parsed.Host
			if idx := strings.Index(domain, ":"); idx != -1 {
				domain = domain[:idx]
			}
			if isIPAddress(domain) {
				slog.Debug("HTTP redirect leads to IP address, using JavaScript resolution", "domain", domain)
				return resolveMirrorWithJS(mirrorURL, timeout)
			}
		}
		slog.Debug("Resolved mirror", "from", mirrorURL, "to", finalURL, "method", "HTTP redirect")
		return finalURL, nil
	}

	// Check if we got HTML (might need JavaScript execution)
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") {
		// Read body to check if it contains JavaScript redirect
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			bodyStr := string(body)
			// Check if body contains JavaScript that might do redirect
			if strings.Contains(bodyStr, "<script") || strings.Contains(bodyStr, "window.location") ||
				strings.Contains(bodyStr, "location.href") || strings.Contains(bodyStr, "document.location") {
				slog.Debug("Detected JavaScript redirect, using headless browser")
				return resolveMirrorWithJS(mirrorURL, timeout)
			}
		}
	}

	// If still same URL, try JavaScript resolution
	slog.Debug("Pinnacle888: HTTP redirect didn't work, trying JavaScript resolution...\n")
	return resolveMirrorWithJS(mirrorURL, timeout)
}

// resolveMirrorWithJS uses headless browser to execute JavaScript and get final URL
func resolveMirrorWithJS(mirrorURL string, timeout time.Duration) (string, error) {
	// Reuse single dir and clean before use to avoid filling disk with Chrome temp dirs
	_ = os.RemoveAll(pinnacle888ChromeUserDataDir)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Create chromedp context (fixed UserDataDir to avoid new temp dir per run)
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.UserDataDir(pinnacle888ChromeUserDataDir),
		chromedp.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36"),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(ctx, opts...)
	defer cancel()

	// Create chrome instance
	ctx, cancel = chromedp.NewContext(allocCtx, chromedp.WithLogf(func(format string, v ...interface{}) {
		// Suppress chromedp logs unless debugging
		if os.Getenv("PINNACLE888_DEBUG") == "1" {
			slog.Debug("chromedp", "message", fmt.Sprintf(format, v...))
		}
	}))
	defer cancel()

	var finalURL string

	// Navigate and wait for page to load (including JavaScript redirects)
	// Use longer wait times to ensure JavaScript redirects complete
	err := chromedp.Run(ctx,
		chromedp.Navigate(mirrorURL),
		chromedp.Sleep(3*time.Second), // Wait for initial page load
		chromedp.Location(&finalURL),
	)

	if err != nil {
		return "", fmt.Errorf("chromedp navigation: %w", err)
	}

	// Check if URL changed
	if finalURL != "" && finalURL != mirrorURL {
		// Wait a bit more and check again to ensure we got the final redirect
		var checkURL string
		err = chromedp.Run(ctx,
			chromedp.Sleep(2*time.Second),
			chromedp.Location(&checkURL),
		)
		if err == nil && checkURL != "" && checkURL != finalURL {
			// URL changed again, use the new one
			finalURL = checkURL
		}

		slog.Debug("Resolved mirror", "from", mirrorURL, "to", finalURL, "method", "JavaScript redirect")
		return finalURL, nil
	}

	// If URL didn't change, try waiting longer
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
		slog.Debug("Pinnacle888: Resolved mirror %s -> %s (JavaScript redirect after wait)\n", mirrorURL, finalURL)
		return finalURL, nil
	}

	// If still same URL, return it (maybe no redirect needed)
	if finalURL != "" {
		slog.Debug("Pinnacle888: Mirror URL did not redirect: %s\n", finalURL)
		return finalURL, nil
	}

	return "", fmt.Errorf("failed to resolve mirror URL: %s", mirrorURL)
}

// getFinalDomainFromResolved tries to get the final domain after JavaScript redirects
// This is used to find the actual odds domain from the resolved mirror URL
func getFinalDomainFromResolved(resolvedURL string, timeout time.Duration) (string, error) {
	// First, check if resolvedURL is already a domain (not an IP address)
	parsed, err := url.Parse(resolvedURL)
	if err == nil {
		domain := parsed.Host
		if idx := strings.Index(domain, ":"); idx != -1 {
			domain = domain[:idx]
		}
		// If it's already a domain (not an IP), return it directly
		if !isIPAddress(domain) {
			slog.Debug("Pinnacle888: Resolved URL already contains domain: %s\n", domain)
			return domain, nil
		}
	}

	// Reuse single dir and clean before use to avoid filling disk with Chrome temp dirs
	_ = os.RemoveAll(pinnacle888ChromeUserDataDir)

	// If it's an IP address, try JavaScript resolution to get final URL after all redirects
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.UserDataDir(pinnacle888ChromeUserDataDir),
		chromedp.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36"),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(ctx, opts...)
	defer cancel()

	ctx, cancel = chromedp.NewContext(allocCtx, chromedp.WithLogf(func(format string, v ...interface{}) {
		// Suppress chromedp logs unless debugging
		if os.Getenv("PINNACLE888_DEBUG") == "1" {
			slog.Debug("chromedp", "message", fmt.Sprintf(format, v...))
		}
	}))
	defer cancel()

	var finalURL string
	var pageHTML string

	// Navigate and wait for page to load (including JavaScript redirects)
	err = chromedp.Run(ctx,
		chromedp.Navigate(resolvedURL),
		chromedp.Sleep(5*time.Second), // Wait longer for JavaScript redirects
		chromedp.Location(&finalURL),
		chromedp.OuterHTML("html", &pageHTML),
	)

	if err != nil {
		return "", fmt.Errorf("chromedp navigation: %w", err)
	}

	// Check if URL changed - wait a bit more to ensure final redirect
	if finalURL != "" && finalURL != resolvedURL {
		var checkURL string
		err = chromedp.Run(ctx,
			chromedp.Sleep(2*time.Second),
			chromedp.Location(&checkURL),
		)
		if err == nil && checkURL != "" && checkURL != finalURL {
			// URL changed again, use the new one
			finalURL = checkURL
		}

		// Parse final URL to extract domain
		parsed, err := url.Parse(finalURL)
		if err != nil {
			return "", fmt.Errorf("parse final URL: %w", err)
		}
		domain := parsed.Host
		// Remove port if present
		if idx := strings.Index(domain, ":"); idx != -1 {
			domain = domain[:idx]
		}
		// Only return if it's a domain (not an IP address)
		if !isIPAddress(domain) {
			slog.Debug("Pinnacle888: Extracted domain from final URL: %s\n", domain)
			return domain, nil
		}
	}

	// If URL didn't change, try to extract domain from the original resolvedURL
	if finalURL == "" || finalURL == resolvedURL {
		parsed, err := url.Parse(resolvedURL)
		if err == nil {
			domain := parsed.Host
			if idx := strings.Index(domain, ":"); idx != -1 {
				domain = domain[:idx]
			}
			if !isIPAddress(domain) {
				return domain, nil
			}
		}
	}

	// Try to extract domain from JavaScript code in the page
	// Look for common patterns like window.location, document.location, etc.
	if strings.Contains(pageHTML, "window.location") || strings.Contains(pageHTML, "document.location") {
		// Use simple string search for domain patterns
		// Look for domains in the HTML
		lines := strings.Split(pageHTML, "\n")
		for _, line := range lines {
			if strings.Contains(line, "https://") || strings.Contains(line, "http://") {
				// Try to extract domain from this line
				startIdx := strings.Index(line, "https://")
				if startIdx == -1 {
					startIdx = strings.Index(line, "http://")
				}
				if startIdx != -1 {
					// Extract URL from this position
					urlPart := line[startIdx:]
					// Find end of URL (space, quote, etc.)
					endIdx := len(urlPart)
					for i, char := range urlPart {
						if char == ' ' || char == '"' || char == '\'' || char == ';' || char == ')' || char == '}' {
							endIdx = i
							break
						}
					}
					urlStr := urlPart[:endIdx]
					parsed, err := url.Parse(urlStr)
					if err == nil {
						domain := parsed.Host
						if idx := strings.Index(domain, ":"); idx != -1 {
							domain = domain[:idx]
						}
						if domain != "" && !isIPAddress(domain) {
							slog.Debug("Pinnacle888: Extracted domain from JavaScript: %s\n", domain)
							return domain, nil
						}
					}
				}
			}
		}
	}

	return "", fmt.Errorf("no domain found in JavaScript redirects")
}

// isIPAddress checks if a string is an IP address (IPv4 or IPv6)
func isIPAddress(s string) bool {
	return net.ParseIP(s) != nil
}

func NewClient(baseURL, mirrorURL, apiKey, deviceUUID string, timeout time.Duration, proxyList []string) *Client {
	// Allow env overrides to avoid committing secrets into configs.
	if apiKey == "" {
		apiKey = os.Getenv("PINNACLE888_API_KEY")
	}
	if deviceUUID == "" {
		deviceUUID = os.Getenv("PINNACLE888_DEVICE_UUID")
	}

	insecureTLS := os.Getenv("PINNACLE888_INSECURE_TLS") == "1"

	// Use proxy list from config
	if len(proxyList) > 0 {
		slog.Debug("Pinnacle888: Using proxy list from config (%d proxies)\n", len(proxyList))
	}

	// Create default transport (without proxy - we'll use proxy per request)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	if insecureTLS {
		// Some networks intercept TLS and present self-signed / invalid certs.
		// Allow opting out of verification for guest API scraping.
		transport.TLSClientConfig.InsecureSkipVerify = true
	}

	// Use default proxy from environment (HTTP_PROXY, HTTPS_PROXY) for non-Pinnacle requests
	transport.Proxy = http.ProxyFromEnvironment

	client := &Client{
		baseURL:           baseURL,
		mirrorURL:         mirrorURL,
		apiKey:            apiKey,
		deviceUUID:        deviceUUID,
		httpClient:        &http.Client{Timeout: timeout, Transport: transport},
		proxyList:         proxyList,
		currentProxyIndex: 0,
		resolveTimeout:    timeout,
		resolveInterval:   5 * time.Minute, // Check every 5 minutes if resolution is needed
	}

	// Don't resolve immediately - do lazy resolution when needed
	// This avoids blocking startup and allows re-resolution when URL stops working

	return client
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

	// Consider 2xx and 3xx as healthy
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// ensureResolved ensures that mirror URL is resolved and cached
// It only resolves if:
// 1. Not resolved yet, OR
// 2. Last resolution was more than resolveInterval ago AND URL is not healthy
func (c *Client) ensureResolved() error {
	if c.mirrorURL == "" {
		return nil
	}

	c.resolvedMu.RLock()
	hasResolved := c.resolvedURL != ""
	lastResolve := c.lastResolveTime
	resolvedURL := c.resolvedURL
	c.resolvedMu.RUnlock()

	// If we have a resolved URL, check if it's still healthy
	if hasResolved {
		// Check if enough time has passed since last resolution
		if time.Since(lastResolve) < c.resolveInterval {
			// Too soon to check again, use cached URL
			return nil
		}

		// Check if current URL is still healthy
		if c.checkURLHealth(resolvedURL) {
			// URL is still working, update last check time
			c.resolvedMu.Lock()
			c.lastResolveTime = time.Now()
			c.resolvedMu.Unlock()
			return nil
		}

		// URL is not healthy, need to re-resolve
		slog.Debug("Pinnacle888: Cached URL %s is not responding, re-resolving mirror...\n", resolvedURL)
	}

	// Resolve mirror URL
	resolved, err := resolveMirror(c.mirrorURL, c.resolveTimeout)
	if err != nil {
		if hasResolved {
			// If we had a cached URL but re-resolution failed, log warning but keep using cached URL
			slog.Warn("Pinnacle888: mirror re-resolve failed, keeping cached URL", "mirror_url", c.mirrorURL, "error", err, "error_msg", err.Error(), "cached_url", resolvedURL)
			return nil
		}
		slog.Error("Pinnacle888: mirror resolve failed", "mirror_url", c.mirrorURL, "error", err, "error_msg", err.Error())
		return fmt.Errorf("failed to resolve mirror: %w", err)
	}

	// Update cached resolved URL
	c.resolvedMu.Lock()
	c.resolvedURL = resolved
	c.lastResolveTime = time.Now()
	c.baseURL = resolved
	c.resolvedMu.Unlock()

	slog.Debug("Pinnacle888: Resolved mirror URL: %s\n", resolved)

	// Extract domain from resolved URL for odds endpoint
	parsed, err := url.Parse(resolved)
	if err == nil {
		domain := parsed.Host
		// Remove port if present
		if idx := strings.Index(domain, ":"); idx != -1 {
			domain = domain[:idx]
		}

		// Check if it's an IP address
		if isIPAddress(domain) {
			// If it's an IP address, try to get domain from JavaScript redirects
			slog.Debug("Pinnacle888: Resolved URL is IP address %s, attempting to resolve domain via JavaScript...\n", domain)
			finalDomain, err := getFinalDomainFromResolved(resolved, c.resolveTimeout)
			if err != nil {
				slog.Debug("Pinnacle888: Failed to resolve domain from IP via JavaScript: %v, using IP address directly\n", err)
				c.resolvedMu.Lock()
				c.oddsDomain = domain
				c.resolvedMu.Unlock()
				slog.Debug("Pinnacle888: Using IP address %s for odds endpoint\n", domain)
			} else if finalDomain != "" {
				c.resolvedMu.Lock()
				c.oddsDomain = finalDomain
				c.resolvedMu.Unlock()
				slog.Debug("Pinnacle888: Resolved odds domain from JavaScript: %s\n", finalDomain)
			} else {
				c.resolvedMu.Lock()
				c.oddsDomain = domain
				c.resolvedMu.Unlock()
				slog.Debug("Pinnacle888: Using IP address %s for odds endpoint (fallback)\n", domain)
			}
		} else {
			// It's already a domain, use it directly
			c.resolvedMu.Lock()
			c.oddsDomain = domain
			c.resolvedMu.Unlock()
			slog.Debug("Pinnacle888: Using resolved domain %s for odds endpoint\n", domain)
		}
	}

	// Log resolved URLs at INFO so production logs show what mirror resolved to
	c.resolvedMu.RLock()
	oddsDomain := c.oddsDomain
	c.resolvedMu.RUnlock()
	if oddsDomain == "" {
		oddsDomain = "(empty)"
	}
	slog.Info("Pinnacle888: mirror resolved", "mirror_url", c.mirrorURL, "resolved_base_url", resolved, "odds_domain", oddsDomain)

	return nil
}

// shouldReResolve checks if an error indicates that we should re-resolve the mirror URL
func (c *Client) shouldReResolve(err error, statusCode int) bool {
	if err != nil {
		errStr := err.Error()
		// Check for connection errors that might indicate URL is down
		if strings.Contains(errStr, "connection refused") ||
			strings.Contains(errStr, "no such host") ||
			strings.Contains(errStr, "timeout") ||
			strings.Contains(errStr, "network is unreachable") {
			return true
		}
	}
	// Check for HTTP errors that might indicate URL changed
	if statusCode >= 400 && statusCode < 500 {
		// 4xx errors might indicate URL changed or resource not found
		return true
	}
	return false
}

// clearResolvedURL clears the cached resolved URL to force re-resolution
func (c *Client) clearResolvedURL() {
	c.resolvedMu.Lock()
	defer c.resolvedMu.Unlock()
	if c.resolvedURL != "" {
		slog.Debug("Pinnacle888: Clearing cached URL %s to force re-resolution\n", c.resolvedURL)
		c.resolvedURL = ""
		c.oddsDomain = ""
	}
}

// getResolvedBaseURL returns the resolved base URL (from mirror or direct)
// It ensures the URL is resolved before returning
func (c *Client) getResolvedBaseURL() string {
	// Ensure mirror is resolved (lazy resolution)
	if err := c.ensureResolved(); err != nil {
		slog.Debug("Pinnacle888: Warning: failed to ensure resolved URL: %v\n", err)
	}

	c.resolvedMu.RLock()
	defer c.resolvedMu.RUnlock()
	if c.resolvedURL != "" {
		return c.resolvedURL
	}
	return c.baseURL
}

func (c *Client) GetRelatedMatchups(matchupID int64) ([]RelatedMatchup, error) {
	var out []RelatedMatchup
	if err := c.getJSON(fmt.Sprintf("/0.1/matchups/%d/related", matchupID), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetRelatedStraightMarkets(matchupID int64) ([]Market, error) {
	var out []Market
	if err := c.getJSON(fmt.Sprintf("/0.1/matchups/%d/markets/related/straight", matchupID), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetSports() ([]Sport, error) {
	var out []Sport
	if err := c.getJSON("/0.1/sports", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetSportMatchups(sportID int64) ([]RelatedMatchup, error) {
	var out []RelatedMatchup
	if err := c.getJSON(fmt.Sprintf("/0.1/sports/%d/matchups", sportID), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetSportStraightMarkets(sportID int64) ([]Market, error) {
	var out []Market
	if err := c.getJSON(fmt.Sprintf("/0.1/sports/%d/markets/straight", sportID), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// oddsBasePath returns the base path for sports-service euro from odds URL.
// e.g. "/sports-service/sv/euro/odds" -> "/sports-service/sv/euro"
func oddsBasePath(oddsPath string) string {
	s := strings.TrimSuffix(strings.TrimSuffix(oddsPath, "/"), "/odds")
	if !strings.HasPrefix(s, "/") {
		return "/sports-service/sv/euro"
	}
	return s
}

// buildOddsRequestURL builds URL for odds-domain requests (leagues, league odds, event odds).
func (c *Client) buildOddsRequestURL(oddsPath string, pathSuffix string, query url.Values) (*url.URL, error) {
	if oddsPath == "" {
		return nil, fmt.Errorf("odds_url not configured")
	}
	pathForBase := oddsPath
	if strings.HasPrefix(oddsPath, "http://") || strings.HasPrefix(oddsPath, "https://") {
		parsed, err := url.Parse(oddsPath)
		if err != nil {
			return nil, fmt.Errorf("parse odds_url: %w", err)
		}
		pathForBase = parsed.Path
	}
	base := oddsBasePath(pathForBase)
	pathStr := base + pathSuffix
	if !strings.HasPrefix(pathStr, "/") {
		pathStr = "/" + pathStr
	}

	var u *url.URL
	if strings.HasPrefix(oddsPath, "http://") || strings.HasPrefix(oddsPath, "https://") {
		parsed, _ := url.Parse(oddsPath)
		u = &url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: pathStr, RawQuery: query.Encode()}
	} else {
		if err := c.ensureResolved(); err != nil {
			slog.Debug("Pinnacle888: Warning: failed to ensure resolved URL: %v\n", err)
		}
		c.resolvedMu.RLock()
		oddsDomain := c.oddsDomain
		c.resolvedMu.RUnlock()
		if oddsDomain == "" {
			oddsDomain = "www.gentleflame47.xyz"
		}
		u = &url.URL{Scheme: "https", Host: oddsDomain, Path: pathStr, RawQuery: query.Encode()}
	}
	return u, nil
}

// doOddsRequest performs GET with common headers for odds domain.
func (c *Client) doOddsRequest(u *url.URL) ([]byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en,en-US;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
	req.Header.Set("Referer", u.Scheme+"://"+u.Host+"/")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if c.shouldReResolve(err, 0) {
			c.clearResolvedURL()
		}
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		if c.shouldReResolve(nil, resp.StatusCode) {
			c.clearResolvedURL()
		}
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(b))
	}
	return readBodyMaybeGzip(resp)
}

// GetLeagues fetches leagues for a sport from /sports-service/sv/euro/leagues
func (c *Client) GetLeagues(oddsPath string, sportID int64) ([]LeagueListItem, error) {
	q := url.Values{}
	q.Set("sportId", fmt.Sprintf("%d", sportID))
	q.Set("locale", "en_US")
	q.Set("_", fmt.Sprintf("%d", time.Now().UnixMilli()))

	u, err := c.buildOddsRequestURL(oddsPath, "/leagues", q)
	if err != nil {
		return nil, err
	}
	body, err := c.doOddsRequest(u)
	if err != nil {
		return nil, err
	}
	var list []LeagueListItem
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("unmarshal leagues: %w", err)
	}
	return list, nil
}

// GetLeagueOdds fetches odds for one league from /sports-service/sv/euro/odds/league
func (c *Client) GetLeagueOdds(oddsPath string, leagueCode string, sportID int64, isLive bool) ([]byte, error) {
	q := url.Values{}
	q.Set("sportId", fmt.Sprintf("%d", sportID))
	q.Set("oddsType", "1")
	q.Set("version", "0")
	q.Set("timeStamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	q.Set("periodNum", "-1")
	q.Set("eSportCode", "")
	q.Set("locale", "en_US")
	q.Set("leagueCode", leagueCode)
	q.Set("isHlE", "true")
	if isLive {
		q.Set("isLive", "true")
	} else {
		q.Set("isLive", "false")
	}
	q.Set("eventType", "0")
	q.Set("_", fmt.Sprintf("%d", time.Now().UnixMilli()))

	u, err := c.buildOddsRequestURL(oddsPath, "/odds/league", q)
	if err != nil {
		return nil, err
	}
	return c.doOddsRequest(u)
}

// GetEventOdds fetches full odds for one event from /sports-service/sv/euro/odds/event
func (c *Client) GetEventOdds(oddsPath string, eventID int64) ([]byte, error) {
	q := url.Values{}
	q.Set("eventId", fmt.Sprintf("%d", eventID))
	q.Set("oddsType", "1")
	q.Set("version", "0")
	q.Set("specialVersion", "0")
	q.Set("locale", "en_US")
	q.Set("_", fmt.Sprintf("%d", time.Now().UnixMilli()))

	u, err := c.buildOddsRequestURL(oddsPath, "/odds/event", q)
	if err != nil {
		return nil, err
	}
	return c.doOddsRequest(u)
}

// GetOddsEvents gets events from the odds endpoint (sports-service/sv/euro/odds)
// This endpoint returns structured JSON with clear home/away team information
// oddsPath can be either a full URL or a relative path (e.g., "/sports-service/sv/euro/odds")
func (c *Client) GetOddsEvents(oddsPath string, sportID int64, isLive bool) ([]byte, error) {
	if oddsPath == "" {
		return nil, fmt.Errorf("odds_url not configured")
	}

	// Determine path and domain: if oddsPath is a full URL, use it as-is
	// Otherwise, use default odds domain (odds endpoint is on different domain than API)
	var u *url.URL
	if strings.HasPrefix(oddsPath, "http://") || strings.HasPrefix(oddsPath, "https://") {
		// Full URL provided - use as-is for backward compatibility
		var err error
		u, err = url.Parse(oddsPath)
		if err != nil {
			return nil, fmt.Errorf("parse odds_url: %w", err)
		}
	} else {
		// Relative path provided - try to use resolved odds domain, fallback to default
		oddsPathStr := oddsPath
		if !strings.HasPrefix(oddsPathStr, "/") {
			oddsPathStr = "/" + oddsPathStr
		}

		// Ensure mirror is resolved to get odds domain
		if err := c.ensureResolved(); err != nil {
			slog.Debug("Pinnacle888: Warning: failed to ensure resolved URL: %v\n", err)
		}

		// Try to get resolved odds domain from mirror
		c.resolvedMu.RLock()
		oddsDomain := c.oddsDomain
		c.resolvedMu.RUnlock()

		if oddsDomain == "" {
			// Fallback to default domain
			oddsDomain = "www.gentleflame47.xyz"
		}

		u = &url.URL{
			Scheme: "https",
			Host:   oddsDomain,
			Path:   oddsPathStr,
		}
	}

	// Log the URL construction for debugging
	slog.Debug("Pinnacle888: Using odds endpoint: %s%s\n", u.Scheme+"://"+u.Host, u.Path)

	// Set query parameters
	queryParams := u.Query()
	queryParams.Set("sportId", fmt.Sprintf("%d", sportID))
	if isLive {
		queryParams.Set("isLive", "true")
	} else {
		queryParams.Set("isLive", "false")
	}
	queryParams.Set("isHlE", "true")
	queryParams.Set("oddsType", "2")
	queryParams.Set("version", "0")
	queryParams.Set("timeStamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	queryParams.Set("language", "en_US")
	queryParams.Set("locale", "en_US")
	queryParams.Set("periodNum", "0,8,39,3,4,5,6,7")
	queryParams.Set("_", fmt.Sprintf("%d", time.Now().UnixMilli()))

	// Build full URL
	fullURL := u.String()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for odds events: %w", err)
	}
	req.URL.RawQuery = queryParams.Encode()

	// Set headers
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en,en-US;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
	req.Header.Set("Referer", u.Scheme+"://"+u.Host+"/")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// If request failed, check if we should re-resolve mirror
		if c.shouldReResolve(err, 0) {
			slog.Debug("Pinnacle888: Request to odds endpoint failed: %v, clearing cached URL for re-resolution\n", err)
			c.clearResolvedURL()
		}
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		previewLen := 500
		if len(b) < previewLen {
			previewLen = len(b)
		}
		slog.Debug("Pinnacle888: Odds events API returned status %d, body preview: %s\n", resp.StatusCode, string(b[:previewLen]))

		// If we got error that might indicate URL changed, clear cached URL
		if c.shouldReResolve(nil, resp.StatusCode) {
			slog.Debug("Pinnacle888: HTTP error %d, clearing cached URL to force re-resolution on next request\n", resp.StatusCode)
			c.clearResolvedURL()
		}

		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(b))
	}

	body, err := readBodyMaybeGzip(resp)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func (c *Client) getJSON(path string, out any) error {
	// Try proxies in order if available, fallback to direct connection
	if len(c.proxyList) > 0 {
		return c.getJSONWithProxyRetry(path, out)
	}

	return c.getJSONDirect(path, out)
}

func (c *Client) getJSONDirect(path string, out any) error {
	baseURL := c.getResolvedBaseURL()
	if baseURL == "" {
		baseURL = "https://guest.api.arcadia.pinnacle.com"
	}
	url := baseURL + path

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Check if error indicates URL might be down
		if c.shouldReResolve(err, 0) {
			c.clearResolvedURL()
		}
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status before handling
	if resp.StatusCode >= 400 {
		// Read body for error details
		b, _ := io.ReadAll(resp.Body)
		if c.shouldReResolve(nil, resp.StatusCode) {
			c.clearResolvedURL()
		}
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(b))
	}

	return c.handleResponse(resp, out)
}

func (c *Client) getJSONWithProxyRetry(path string, out any) error {
	baseURL := c.getResolvedBaseURL()
	if baseURL == "" {
		baseURL = "https://guest.api.arcadia.pinnacle.com"
	}
	requestURL := baseURL + path

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
			slog.Debug("Pinnacle888: Invalid proxy URL %s: %v\n", maskProxyURL(proxyURLStr), err)
			continue
		}

		// Create transport with this proxy
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		}
		if os.Getenv("PINNACLE888_INSECURE_TLS") == "1" {
			transport.TLSClientConfig.InsecureSkipVerify = true
		}
		transport.Proxy = http.ProxyURL(proxyURL)

		client := &http.Client{
			Timeout:   c.httpClient.Timeout,
			Transport: transport,
		}

		req, err := http.NewRequest(http.MethodGet, requestURL, nil)
		if err != nil {
			slog.Debug("Pinnacle888: Failed to create request: %v, trying next proxy...\n", err)
			continue
		}

		c.setHeaders(req)

		resp, err := client.Do(req)
		if err != nil {
			slog.Debug("Pinnacle888: Proxy %s failed: %v, trying next...\n", maskProxyURL(proxyURLStr), err)
			continue
		}

		// Check if response is valid JSON (not HTML blocking page)
		// Peek at body to check if it's JSON or HTML
		contentType := resp.Header.Get("Content-Type")
		bodyPeek := make([]byte, 100)
		n, _ := resp.Body.Read(bodyPeek)

		// Check if it's JSON by looking at first character
		isJSON := n > 0 && (bodyPeek[0] == '[' || bodyPeek[0] == '{')
		isHTML := n > 0 && bodyPeek[0] == '<'

		if resp.StatusCode == http.StatusOK && (strings.Contains(contentType, "application/json") || isJSON) && !isHTML {
			// Success! Create a new reader that includes the peeked bytes
			bodyReader := io.MultiReader(bytes.NewReader(bodyPeek[:n]), resp.Body)

			// Create a new response with the combined body
			// We need to wrap the body reader
			resp.Body = io.NopCloser(bodyReader)

			// Update current proxy index
			c.proxyMu.Lock()
			c.currentProxyIndex = proxyIndex
			c.proxyMu.Unlock()
			slog.Debug("Pinnacle888: Using working proxy %s\n", maskProxyURL(proxyURLStr))

			err := c.handleResponse(resp, out)
			resp.Body.Close()
			return err
		}

		// Not JSON - read body to check what we got before closing
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		preview := string(body)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		cfRay := resp.Header.Get("Cf-Ray")
		slog.Debug("Pinnacle888: Proxy %s returned status=%d, content-type=%s, cf-ray=%s, body_preview=%s (blocked/invalid), trying next...\n",
			maskProxyURL(proxyURLStr), resp.StatusCode, contentType, cfRay, preview)
	}

	// All proxies failed, try direct connection as last resort
	slog.Debug("Pinnacle888: All proxies failed, trying direct connection...\n")
	return c.getJSONDirect(path, out)
}

func (c *Client) setHeaders(req *http.Request) {
	// Set headers in the same order as browser (may help bypass Cloudflare detection)
	// Use English language to match Fonbet team names for proper match merging
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en,en-US;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("Content-Type", "application/json")
	// Use realistic browser User-Agent to match browser requests
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 YaBrowser/25.12.0.0 Safari/537.36")
	// Add Referer header - it helps bypass blocking (as seen in working browser requests)
	req.Header.Set("Referer", "https://www.pinnacle.com/")
	// Add Sec-CH-UA headers to mimic browser
	req.Header.Set("Sec-CH-UA", `"Chromium";v="142", "YaBrowser";v="25.12", "Not_A Brand";v="99", "Yowser";v="2.5"`)
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", `"macOS"`)
	// Note: Origin header may cause 401 errors, so we don't send it

	// Pinnacle guest API expects these headers (captured from browser).
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	if c.deviceUUID != "" {
		req.Header.Set("X-Device-UUID", c.deviceUUID)
	}
}

func (c *Client) handleResponse(resp *http.Response, out any) error {
	// Log response headers for debugging (especially for blocked requests)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/json" {
		slog.Debug("Pinnacle888 API response", "status", resp.StatusCode, "content_type", resp.Header.Get("Content-Type"), "cf_ray", resp.Header.Get("Cf-Ray"))
	}

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		// Log first 500 chars to help debug
		preview := string(b)
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		// Log headers for debugging
		headers := ""
		for k, v := range resp.Header {
			if len(v) > 0 {
				headers += fmt.Sprintf("%s: %s; ", k, v[0])
			}
		}
		return fmt.Errorf("unexpected status %d (headers: %s): %s", resp.StatusCode, headers, preview)
	}

	body, err := readBodyMaybeGzip(resp)
	if err != nil {
		return err
	}

	// DEBUG: Log full response body for markets endpoints to understand what Pinnacle returns
	if strings.Contains(resp.Request.URL.Path, "/markets/") {
		// Try to parse as JSON array to log structure
		var marketsArray []map[string]interface{}
		if err := json.Unmarshal(body, &marketsArray); err == nil {
			// Log summary of markets response
			periodCounts := make(map[interface{}]int)
			statusCounts := make(map[string]int)
			typeCounts := make(map[string]int)
			for _, m := range marketsArray {
				if period, ok := m["period"]; ok {
					periodCounts[period]++
				}
				if status, ok := m["status"].(string); ok {
					statusCounts[status]++
				}
				if mtype, ok := m["type"].(string); ok {
					typeCounts[mtype]++
				}
			}
			slog.Debug("Pinnacle888 markets response", "path", resp.Request.URL.Path, "total", len(marketsArray), "periods", periodCounts, "statuses", statusCounts, "types", typeCounts)

			// Log first few markets in detail
			if len(marketsArray) > 0 {
				firstFew := len(marketsArray)
				if firstFew > 3 {
					firstFew = 3
				}
				for i := 0; i < firstFew; i++ {
					marketJSON, _ := json.Marshal(marketsArray[i])
					preview := string(marketJSON)
					if len(preview) > 300 {
						preview = preview[:300] + "..."
					}
					slog.Debug("Pinnacle888 market sample", "index", i, "preview", preview)
				}
			}
		} else {
			// If not array, log as object
			var marketObj map[string]interface{}
			if err := json.Unmarshal(body, &marketObj); err == nil {
				marketJSON, _ := json.Marshal(marketObj)
				preview := string(marketJSON)
				if len(preview) > 500 {
					preview = preview[:500] + "..."
				}
				slog.Debug("Pinnacle888 response object", "path", resp.Request.URL.Path, "preview", preview)
			} else {
				// Raw body if can't parse
				preview := string(body)
				if len(preview) > 1000 {
					preview = preview[:1000] + "..."
				}
				slog.Debug("Pinnacle888 raw response", "path", resp.Request.URL.Path, "bytes", len(body), "preview", preview)
			}
		}
	}

	if err := json.Unmarshal(body, out); err != nil {
		// If unmarshal fails, log the body to help debug (might be HTML error page)
		preview := string(body)
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		// Check if it's HTML (common error response)
		if len(body) > 0 && (body[0] == '<' || strings.Contains(strings.ToLower(preview), "<html")) {
			return fmt.Errorf("unmarshal: received HTML instead of JSON (status %d): %s", resp.StatusCode, preview)
		}
		return fmt.Errorf("unmarshal: %w (body preview: %s)", err, preview)
	}
	return nil
}

func maskProxyURL(proxyURL string) string {
	// Mask password in proxy URL for logging
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return "***"
	}
	if parsed.User != nil {
		password, _ := parsed.User.Password()
		if password != "" {
			parsed.User = url.UserPassword(parsed.User.Username(), "***")
		}
	}
	return parsed.String()
}

func readBodyMaybeGzip(resp *http.Response) ([]byte, error) {
	if resp.Header.Get("Content-Encoding") == "gzip" {
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
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return b, nil
}
