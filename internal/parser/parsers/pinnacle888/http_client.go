package pinnacle888

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
)

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
	resolvedMu        sync.RWMutex
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
		fmt.Printf("Pinnacle888: Resolved mirror %s -> %s (HTTP redirect)\n", mirrorURL, finalURL)
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
				fmt.Printf("Pinnacle888: Detected JavaScript redirect, using headless browser...\n")
				return resolveMirrorWithJS(mirrorURL, timeout)
			}
		}
	}

	finalURL = resp.Request.URL.String()
	if finalURL != mirrorURL {
		fmt.Printf("Pinnacle888: Resolved mirror %s -> %s (HTTP redirect)\n", mirrorURL, finalURL)
		return finalURL, nil
	}

	// If still same URL, try JavaScript resolution
	fmt.Printf("Pinnacle888: HTTP redirect didn't work, trying JavaScript resolution...\n")
	return resolveMirrorWithJS(mirrorURL, timeout)
}

// resolveMirrorWithJS uses headless browser to execute JavaScript and get final URL
func resolveMirrorWithJS(mirrorURL string, timeout time.Duration) (string, error) {
	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Create chromedp context
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36"),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(ctx, opts...)
	defer cancel()

	// Create chrome instance
	ctx, cancel = chromedp.NewContext(allocCtx, chromedp.WithLogf(func(format string, v ...interface{}) {
		// Suppress chromedp logs unless debugging
		if os.Getenv("PINNACLE888_DEBUG") == "1" {
			fmt.Printf("chromedp: "+format, v...)
		}
	}))
	defer cancel()

	var finalURL string

	// Navigate and wait for page to load (including JavaScript redirects)
	err := chromedp.Run(ctx,
		chromedp.Navigate(mirrorURL),
		// Wait a bit for JavaScript to execute
		chromedp.Sleep(2*time.Second),
		// Get the current URL after JavaScript execution
		chromedp.Location(&finalURL),
	)

	if err != nil {
		return "", fmt.Errorf("chromedp navigation: %w", err)
	}

	if finalURL == "" || finalURL == mirrorURL {
		// Try waiting longer and checking again
		err = chromedp.Run(ctx,
			chromedp.Sleep(3*time.Second),
			chromedp.Location(&finalURL),
		)
		if err != nil {
			return "", fmt.Errorf("chromedp wait: %w", err)
		}
	}

	if finalURL != "" && finalURL != mirrorURL {
		fmt.Printf("Pinnacle888: Resolved mirror %s -> %s (JavaScript redirect)\n", mirrorURL, finalURL)
		return finalURL, nil
	}

	// If still same URL, return it (maybe no redirect needed)
	if finalURL != "" {
		return finalURL, nil
	}

	return "", fmt.Errorf("failed to resolve mirror URL: %s", mirrorURL)
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
		fmt.Printf("Pinnacle888: Using proxy list from config (%d proxies)\n", len(proxyList))
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
	}

	// Resolve mirror URL if provided
	if mirrorURL != "" {
		resolved, err := resolveMirror(mirrorURL, timeout)
		if err != nil {
			fmt.Printf("Pinnacle888: Warning: failed to resolve mirror %s: %v, using baseURL %s\n", mirrorURL, err, baseURL)
		} else {
			client.resolvedMu.Lock()
			client.resolvedURL = resolved
			client.resolvedMu.Unlock()
			// Use resolved URL as baseURL
			client.baseURL = resolved
		}
	}

	return client
}

// getResolvedBaseURL returns the resolved base URL (from mirror or direct)
func (c *Client) getResolvedBaseURL() string {
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

// GetLiveEvents gets live events in compact format from the events endpoint
func (c *Client) GetLiveEvents(eventsURL string, sportID int64) ([]byte, error) {
	if eventsURL == "" {
		return nil, fmt.Errorf("events_url not configured")
	}

	// For compact events API, always use URL from config as-is
	// Don't use mirror URL resolution as it may return HTML instead of JSON
	var finalURL string
	finalURL = eventsURL

	// Build URL with parameters
	u, err := url.Parse(finalURL)
	if err != nil {
		return nil, fmt.Errorf("parse final events_url: %w", err)
	}

	// Set query parameters for live events (matching the actual API request)
	queryParams := u.Query()
	queryParams.Set("btg", "1")
	queryParams.Set("c", "")
	queryParams.Set("cl", "3")
	queryParams.Set("d", "")
	queryParams.Set("ec", "")
	queryParams.Set("ev", "")
	queryParams.Set("g", "")
	queryParams.Set("hle", "false")
	queryParams.Set("ic", "false")
	queryParams.Set("ice", "false")
	queryParams.Set("inl", "false")
	queryParams.Set("l", "3")
	queryParams.Set("lang", "")
	queryParams.Set("lg", "")
	queryParams.Set("lv", "")
	queryParams.Set("me", "0") // Matches In Progress = false (for live, this should be 0 to get all)
	queryParams.Set("mk", "2") // Market type
	queryParams.Set("more", "false")
	queryParams.Set("o", "1")
	queryParams.Set("ot", "1") // Order type = 1 for live (vs 2 for line)
	queryParams.Set("pa", "0")
	queryParams.Set("pimo", "0,1,8,39,2,3,6,7,4,5") // Market IDs
	queryParams.Set("pn", "-1")                     // Page Number = -1 (all)
	queryParams.Set("pv", "1")
	queryParams.Set("sp", fmt.Sprintf("%d", sportID))
	queryParams.Set("tm", "0") // Time = 0 (all, including live)
	queryParams.Set("v", "0")
	// Use English locale to match Fonbet team names for proper match merging
	queryParams.Set("locale", "en_US")
	queryParams.Set("_", fmt.Sprintf("%d", time.Now().UnixMilli())) // Timestamp
	queryParams.Set("withCredentials", "true")

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u.Scheme+"://"+u.Host+u.Path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for live events: %w", err)
	}
	req.URL.RawQuery = queryParams.Encode()

	// Set headers for live events API
	// Use English language to match Fonbet team names for proper match merging
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en,en-US;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 YaBrowser/25.12.0.0 Safari/537.36")
	req.Header.Set("Referer", u.Scheme+"://"+u.Host+"/ru/compact/sports/soccer")
	req.Header.Set("Sec-CH-UA", `"Chromium";v="142", "YaBrowser";v="25.12", "Not_A Brand";v="99", "Yowser";v="2.5"`)
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", `"macOS"`)
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Priority", "u=1, i")

	// Set custom headers from the user's request
	req.Header.Set("X-App-Data", "directusToken=TwEdnphtyxsfMpXoJkCkWaPsL2KJJ3lo;lang=en_US;dpVXz=ZDfaFZUP9")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		previewLen := 500
		if len(b) < previewLen {
			previewLen = len(b)
		}
		fmt.Printf("Pinnacle888: Live events API returned status %d, body preview: %s\n", resp.StatusCode, string(b[:previewLen]))
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(b))
	}

	body, err := readBodyMaybeGzip(resp)
	if err != nil {
		return nil, err
	}

	// Log response preview for debugging
	if len(body) > 0 {
		previewLen := 200
		if len(body) < previewLen {
			previewLen = len(body)
		}
		fmt.Printf("Pinnacle888: Live events API response (%d bytes), preview: %s\n", len(body), string(body[:previewLen]))
	}

	return body, nil
}

// GetLineEvents gets pre-match/line events in compact format from the events endpoint
func (c *Client) GetLineEvents(eventsURL string, sportID int64) ([]byte, error) {
	if eventsURL == "" {
		return nil, fmt.Errorf("events_url not configured")
	}

	// For compact events API, always use URL from config as-is
	// Don't use mirror URL resolution as it may return HTML instead of JSON
	var finalURL string
	finalURL = eventsURL

	// Build URL with parameters
	u, err := url.Parse(finalURL)
	if err != nil {
		return nil, fmt.Errorf("parse final events_url: %w", err)
	}

	// Set query parameters based on the user's request
	queryParams := u.Query()
	queryParams.Set("btg", "1")
	queryParams.Set("c", "")
	queryParams.Set("cl", "3")
	queryParams.Set("d", "")
	queryParams.Set("ec", "")
	queryParams.Set("ev", "")
	queryParams.Set("g", "QQ==")
	queryParams.Set("hle", "true")
	queryParams.Set("ic", "false")
	queryParams.Set("ice", "false")
	queryParams.Set("inl", "false")
	queryParams.Set("l", "3")
	queryParams.Set("lang", "")
	queryParams.Set("lg", "")
	queryParams.Set("lv", "")
	queryParams.Set("me", "0") // Matches In Progress = false
	queryParams.Set("mk", "0")
	queryParams.Set("more", "false")
	queryParams.Set("o", "1")
	queryParams.Set("ot", "2")
	queryParams.Set("pa", "0")
	queryParams.Set("pimo", "0,1,8,39,2,3,6,7,4,5") // Market IDs
	queryParams.Set("pn", "-1")                     // Page Number = -1 (all)
	queryParams.Set("pv", "1")
	queryParams.Set("sp", fmt.Sprintf("%d", sportID))
	queryParams.Set("tm", "0") // Time = 0 (all, including future)
	queryParams.Set("v", "0")
	// Use English locale to match Fonbet team names for proper match merging
	queryParams.Set("locale", "en_US")
	queryParams.Set("_", fmt.Sprintf("%d", time.Now().UnixMilli())) // Timestamp
	queryParams.Set("withCredentials", "true")

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u.Scheme+"://"+u.Host+u.Path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for line events: %w", err)
	}
	req.URL.RawQuery = queryParams.Encode()

	// Set headers matching the user's request
	// Use English language to match Fonbet team names for proper match merging
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en,en-US;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 YaBrowser/25.12.0.0 Safari/537.36")
	req.Header.Set("Referer", u.Scheme+"://"+u.Host+"/en/compact/sports/soccer")
	req.Header.Set("Sec-CH-UA", `"Chromium";v="142", "YaBrowser";v="25.12", "Not_A Brand";v="99", "Yowser";v="2.5"`)
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", `"macOS"`)
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Priority", "u=1, i")

	// Set custom headers from the user's request
	// Use English language to match Fonbet team names for proper match merging
	req.Header.Set("X-App-Data", "dpVXz=ZDfaFZUP9;pctag=4e95f421-8ffd-447b-ae41-0c5e9334131c;directusToken=TwEdnphtyxsfMpXoJkCkWaPsL2KJJ3lo;BrowserSessionId=be6e0f48-80cb-433d-bbe5-979963d02332;PCTR=1927013371756;_og=QQ%3D%3D;_ulp=TDV6YVJod0xqREFDUVBXc05McXBSb2k0dDRVbXdvcm04aUxHOFlUcEZ6ZEN5M2xRWUtFeHo2TW5LUHR5MDNPZWVqa041Nk1ySFlyWG1DaXVXRjY5REE9PXxmNjY4NjBmODNiZTJmMzYyMmYzMjEzYzhhNGMxMWQyYg%3D%3D;custid=id%3DVODENEEVM%26login%3D202601240529%26roundTrip%3D202601240529%26hash%3D6F4625A408B4E0F6D47D091593F63240;_userDefaultView=COMPACT;__prefs=W251bGwsMiwxLDAsMSxudWxsLGZhbHNlLDAuMDAwMCxmYWxzZSx0cnVlLCJfM0xJTkVTIiwwLG51bGwsdHJ1ZSx0cnVlLGZhbHNlLGZhbHNlLG51bGwsbnVsbCx0cnVlXQ%3D%3D;lang=en_US")
	req.Header.Set("X-Browser-Session-Id", "be6e0f48-80cb-433d-bbe5-979963d02332")
	req.Header.Set("X-Custid", "id=VODENEEVM&login=202601240529&roundTrip=202601240529&hash=6F4625A408B4E0F6D47D091593F63240")
	req.Header.Set("X-Lcu", "AAAABAAAAAADp14LAAABm--NRf1Voab-5stXVfI97CwfIEPwzgOEc2vB4liSJu0qgueg9A==")
	req.Header.Set("X-Slid", "-331989785")
	req.Header.Set("X-U", "AAAABAAAAAADp14LAAABm--NRf1Voab-5stXVfI97CwfIEPwzgOEc2vB4liSJu0qgueg9A==")

	// Set cookie header
	// Use English language to match Fonbet team names for proper match merging
	req.Header.Set("Cookie", "dpVXz=ZDfaFZUP9; _sig=Wcy1Nemd5TkRrM05HVTBOak5tTVRnM1pROnNCaXZyN0VkNzBJMUV3cHByZXpjd3hVT3c6LTU0MzQwMzEwMTo3NjkyNDY4MDg6Mi4xMS4wOllwVUxCcGlpQ3c%3D; _apt=YpULBpiiCw; pctag=4e95f421-8ffd-447b-ae41-0c5e9334131c; skin=pa; PCTR=1927013371756; u=AAAABAAAAAADp14LAAABm--NRf1Voab-5stXVfI97CwfIEPwzgOEc2vB4liSJu0qgueg9A==; lcu=AAAABAAAAAADp14LAAABm--NRf1Voab-5stXVfI97CwfIEPwzgOEc2vB4liSJu0qgueg9A==; custid=id=VODENEEVM&login=202601240529&roundTrip=202601240529&hash=6F4625A408B4E0F6D47D091593F63240; BrowserSessionId=be6e0f48-80cb-433d-bbe5-979963d02332; _og=QQ==; _ulp=TDV6YVJod0xqREFDUVBXc05McXBSb2k0dDRVbXdvcm04aUxHOFlUcEZ6ZEN5M2xRWUtFeHo2TW5LUHR5MDNPZWVqa041Nk1ySFlyWG1DaXVXRjY5REE9PXxmNjY4NjBmODNiZTJmMzYyMmYzMjEzYzhhNGMxMWQyYg==; uoc=450f4a9c12b96e79968ebfa6b0fe147e; _userDefaultView=COMPACT; SLID=-331989785; auth=true; __prefs=W251bGwsMiwxLDAsMSxudWxsLGZhbHNlLDAuMDAwMCxmYWxzZSx0cnVlLCJfM0xJTkVTIiwwLG51bGwsdHJ1ZSx0cnVlLGZhbHNlLGZhbHNlLG51bGwsbnVsbCx0cnVlXQ==; _ga=GA1.1.914965420.1769246973; _lastView=eyJ2b2RlbmVldm0iOiJDT01QQUNUIn0%3D; displayMessPopUp=true; _ga_5PLZ6DPTZ0=GS2.1.s1769246972$o1$g1$t1769247050$j60$l0$h0; lang=en_US")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		previewLen := 500
		if len(b) < previewLen {
			previewLen = len(b)
		}
		fmt.Printf("Pinnacle888: Line events API returned status %d, body preview: %s\n", resp.StatusCode, string(b[:previewLen]))
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(b))
	}

	body, err := readBodyMaybeGzip(resp)
	if err != nil {
		return nil, err
	}

	// Log response preview for debugging
	if len(body) > 0 {
		previewLen := 200
		if len(body) < previewLen {
			previewLen = len(body)
		}
		fmt.Printf("Pinnacle888: Line events API response (%d bytes), preview: %s\n", len(body), string(body[:previewLen]))
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
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

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
			fmt.Printf("Pinnacle888: Invalid proxy URL %s: %v\n", maskProxyURL(proxyURLStr), err)
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
			fmt.Printf("Pinnacle888: Failed to create request: %v, trying next proxy...\n", err)
			continue
		}

		c.setHeaders(req)

		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("Pinnacle888: Proxy %s failed: %v, trying next...\n", maskProxyURL(proxyURLStr), err)
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
			fmt.Printf("Pinnacle888: Using working proxy %s\n", maskProxyURL(proxyURLStr))

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
		fmt.Printf("Pinnacle888: Proxy %s returned status=%d, content-type=%s, cf-ray=%s, body_preview=%s (blocked/invalid), trying next...\n",
			maskProxyURL(proxyURLStr), resp.StatusCode, contentType, cfRay, preview)
	}

	// All proxies failed, try direct connection as last resort
	fmt.Printf("Pinnacle888: All proxies failed, trying direct connection...\n")
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
		fmt.Printf("Pinnacle888 API response: status=%d, content-type=%s, cf-ray=%s\n",
			resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Cf-Ray"))
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
			fmt.Printf("Pinnacle888 DEBUG: %s - markets: %d total, periods: %v, statuses: %v, types: %v\n",
				resp.Request.URL.Path, len(marketsArray), periodCounts, statusCounts, typeCounts)

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
					fmt.Printf("Pinnacle888 DEBUG: Market[%d] sample: %s\n", i, preview)
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
				fmt.Printf("Pinnacle888 DEBUG: %s - response object: %s\n", resp.Request.URL.Path, preview)
			} else {
				// Raw body if can't parse
				preview := string(body)
				if len(preview) > 1000 {
					preview = preview[:1000] + "..."
				}
				fmt.Printf("Pinnacle888 DEBUG: %s - raw response (%d bytes): %s\n", resp.Request.URL.Path, len(body), preview)
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
