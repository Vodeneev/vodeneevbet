package olimp

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultBaseURL = "https://www.olimp.bet/api/v4/0/line"
const defaultReferer = "https://www.olimp.bet/line/futbol-1/"

type Client struct {
	baseURL           string
	sportID           int
	referer           string
	client            *http.Client
	proxyList         []string
	currentProxyIndex int
	proxyMu           sync.Mutex
}

func NewClient(baseURL string, sportID int, timeout time.Duration, referer string, proxyList []string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	if sportID <= 0 {
		sportID = 1
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if referer == "" {
		referer = defaultReferer
	}

	insecureTLS := os.Getenv("OLIMP_INSECURE_TLS") == "1"

	// Create default transport (without proxy - we'll use proxy per request)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	if insecureTLS {
		transport.TLSClientConfig.InsecureSkipVerify = true
	}
	transport.Proxy = http.ProxyFromEnvironment

	// Use proxy list from config
	if len(proxyList) > 0 {
		slog.Debug("Olimp: Using proxy list from config", "proxy_count", len(proxyList))
	}

	return &Client{
		baseURL:           baseURL,
		sportID:           sportID,
		referer:           referer,
		client:            &http.Client{Timeout: timeout, Transport: transport},
		proxyList:         proxyList,
		currentProxyIndex: 0,
	}
}

// GetSportsWithCompetitions fetches leagues for sport (vids=1 for football).
func (c *Client) GetSportsWithCompetitions(ctx context.Context) (SportsWithCompetitionsResponse, error) {
	u := c.baseURL + "/sports-with-categories-with-competitions?vids=" + strconv.Itoa(c.sportID)
	body, err := c.do(ctx, u, c.referer)
	if err != nil {
		return nil, err
	}
	var resp SportsWithCompetitionsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse sports: %w", err)
	}
	return resp, nil
}

// GetCompetitionsWithEvents fetches events (matches) for one competition. vids[]=competitionId:
func (c *Client) GetCompetitionsWithEvents(ctx context.Context, competitionID string) (CompetitionsWithEventsResponse, error) {
	u := c.baseURL + "/competitions-with-events?" + url.Values{"vids[]": {competitionID + ":"}}.Encode()
	body, err := c.do(ctx, u, c.referer)
	if err != nil {
		return nil, err
	}
	var resp CompetitionsWithEventsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse competitions-with-events: %w", err)
	}
	return resp, nil
}

// GetEventLine fetches full line for one event (step 3). vids[]=eventId:&main=false
func (c *Client) GetEventLine(ctx context.Context, eventID string) (*OlimpEvent, error) {
	u := c.baseURL + "/events?" + url.Values{"vids[]": {eventID + ":"}, "main": {"false"}}.Encode()
	body, err := c.do(ctx, u, c.referer)
	if err != nil {
		return nil, err
	}
	var resp EventLineResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse events: %w", err)
	}
	if len(resp) == 0 || resp[0].Payload == nil {
		return nil, fmt.Errorf("empty event line for %s", eventID)
	}
	return resp[0].Payload, nil
}

func (c *Client) do(ctx context.Context, rawURL, referer string) ([]byte, error) {
	// Try proxies in order if available, fallback to direct connection
	if len(c.proxyList) > 0 {
		slog.Debug("Olimp: Using proxy list", "proxy_count", len(c.proxyList), "url", rawURL)
		return c.doWithProxyRetry(ctx, rawURL, referer)
	}

	slog.Debug("Olimp: No proxy list configured, using direct connection", "url", rawURL)
	return c.doDirect(ctx, rawURL, referer)
}

func (c *Client) doDirect(ctx context.Context, rawURL, referer string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req, referer)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return c.handleResponse(resp)
}

func (c *Client) doWithProxyRetry(ctx context.Context, rawURL, referer string) ([]byte, error) {
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
		if os.Getenv("OLIMP_INSECURE_TLS") == "1" {
			transport.TLSClientConfig.InsecureSkipVerify = true
		}
		transport.Proxy = http.ProxyURL(proxyURL)

		client := &http.Client{
			Timeout:   c.client.Timeout,
			Transport: transport,
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			continue
		}

		c.setHeaders(req, referer)

		resp, err := client.Do(req)
		if err != nil {
			continue
		}

		// Check if response is valid JSON (not HTML blocking page)
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
			resp.Body = io.NopCloser(bodyReader)

			// Update current proxy index
			c.proxyMu.Lock()
			c.currentProxyIndex = proxyIndex
			c.proxyMu.Unlock()
			slog.Info("Olimp: Using working proxy", "proxy_index", proxyIndex+1, "proxy", maskProxyURL(proxyURLStr), "url", rawURL)

			body, err := c.handleResponse(resp)
			resp.Body.Close()
			return body, err
		}

		// Not JSON - read and close body
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	// All proxies failed, try direct connection as last resort
	slog.Warn("Olimp: All proxies failed, trying direct connection", "url", rawURL, "total_proxies_tried", len(c.proxyList))
	return c.doDirect(ctx, rawURL, referer)
}

func (c *Client) setHeaders(req *http.Request, referer string) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
	req.Header.Set("Referer", referer)
}

func (c *Client) handleResponse(resp *http.Response) ([]byte, error) {
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	var r io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		r = gz
	}
	return io.ReadAll(r)
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
