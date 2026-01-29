package pinnacle

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"encoding/json"
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

type Client struct {
	baseURL           string
	apiKey            string
	deviceUUID        string
	httpClient        *http.Client
	proxyList         []string
	currentProxyIndex int
	proxyMu           sync.Mutex
}

func NewClient(baseURL, apiKey, deviceUUID string, timeout time.Duration, proxyList []string) *Client {
	// Allow env overrides to avoid committing secrets into configs.
	if apiKey == "" {
		apiKey = os.Getenv("PINNACLE_API_KEY")
	}
	if deviceUUID == "" {
		deviceUUID = os.Getenv("PINNACLE_DEVICE_UUID")
	}

	insecureTLS := os.Getenv("PINNACLE_INSECURE_TLS") == "1"

	// Use proxy list from config
	if len(proxyList) > 0 {
		slog.Debug("Using proxy list from config", "proxy_count", len(proxyList))
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

	return &Client{
		baseURL:           baseURL,
		apiKey:            apiKey,
		deviceUUID:        deviceUUID,
		httpClient:        &http.Client{Timeout: timeout, Transport: transport},
		proxyList:         proxyList,
		currentProxyIndex: 0,
	}
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

func (c *Client) getJSON(path string, out any) error {
	// Try proxies in order if available, fallback to direct connection
	if len(c.proxyList) > 0 {
		return c.getJSONWithProxyRetry(path, out)
	}

	return c.getJSONDirect(path, out)
}

func (c *Client) getJSONDirect(path string, out any) error {
	if c.baseURL == "" {
		c.baseURL = "https://guest.api.arcadia.pinnacle.com"
	}
	url := c.baseURL + path

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
	if c.baseURL == "" {
		c.baseURL = "https://guest.api.arcadia.pinnacle.com"
	}
	requestURL := c.baseURL + path

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
			slog.Debug("Invalid proxy URL", "proxy", maskProxyURL(proxyURLStr), "error", err)
			continue
		}

		// Create transport with this proxy
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		}
		if os.Getenv("PINNACLE_INSECURE_TLS") == "1" {
			transport.TLSClientConfig.InsecureSkipVerify = true
		}
		transport.Proxy = http.ProxyURL(proxyURL)

		client := &http.Client{
			Timeout:   c.httpClient.Timeout,
			Transport: transport,
		}

		req, err := http.NewRequest(http.MethodGet, requestURL, nil)
		if err != nil {
			slog.Debug("Failed to create request, trying next proxy", "error", err)
			continue
		}

		c.setHeaders(req)

		resp, err := client.Do(req)
		if err != nil {
			slog.Debug("Proxy failed, trying next", "proxy", maskProxyURL(proxyURLStr), "error", err)
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
			slog.Debug("Using working proxy", "proxy", maskProxyURL(proxyURLStr))

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
		slog.Debug("Proxy returned blocked/invalid response, trying next", "proxy", maskProxyURL(proxyURLStr), "status", resp.StatusCode, "content_type", contentType, "cf_ray", cfRay, "body_preview", preview)
	}

	// All proxies failed, try direct connection as last resort
	slog.Debug("All proxies failed, trying direct connection")
	return c.getJSONDirect(path, out)
}

func (c *Client) setHeaders(req *http.Request) {
	// Set headers in the same order as browser (may help bypass Cloudflare detection)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "ru,en;q=0.9")
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
		slog.Debug("Pinnacle API response", "status", resp.StatusCode, "content_type", resp.Header.Get("Content-Type"), "cf_ray", resp.Header.Get("Cf-Ray"))
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
			slog.Debug("Pinnacle markets response", "path", resp.Request.URL.Path, "total", len(marketsArray), "periods", periodCounts, "statuses", statusCounts, "types", typeCounts)

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
					slog.Debug("Pinnacle market sample", "index", i, "preview", preview)
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
				slog.Debug("Pinnacle response object", "path", resp.Request.URL.Path, "preview", preview)
			} else {
				// Raw body if can't parse
				preview := string(body)
				if len(preview) > 1000 {
					preview = preview[:1000] + "..."
				}
				slog.Debug("Pinnacle raw response", "path", resp.Request.URL.Path, "bytes", len(body), "preview", preview)
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
