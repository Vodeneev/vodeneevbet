package zenit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultFrontVersion = "3.80.0"
	defaultSportID      = 1
	pageLength          = 50
)

type Client struct {
	baseURL      string
	imprintHash  string
	frontVersion string
	sportID      int
	httpClient   *http.Client
	proxyList    []string
	proxyIndex   int
	proxyMu      sync.Mutex
}

func NewClient(baseURL, imprintHash, frontVersion string, sportID int, timeout time.Duration, proxyList []string) *Client {
	if baseURL == "" {
		baseURL = "https://zenitnow549.top"
	}
	if frontVersion == "" {
		frontVersion = defaultFrontVersion
	}
	if sportID == 0 {
		sportID = defaultSportID
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	client := &Client{
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		imprintHash:  imprintHash,
		frontVersion: frontVersion,
		sportID:      sportID,
		httpClient:   &http.Client{Timeout: timeout, Transport: transport},
		proxyList:    proxyList,
	}
	return client
}

// GetLinePage fetches a page of the line (all matches, paginated).
// Use tournament=, league=, games= empty and offset to paginate.
func (c *Client) GetLinePage(ctx context.Context, offset int) (*LineResponse, error) {
	u := c.baseURL + "/ajax/line/printer/react"
	params := url.Values{
		"all":               {"0"},
		"onlyview":          {"0"},
		"timeline":          {"0"},
		"tournaments_mode":  {"1"},
		"sport":             {strconv.Itoa(c.sportID)},
		"tournament":        {""},
		"tournament_region": {""},
		"tournament_info":   {""},
		"league":            {""},
		"games":             {""},
		"ross":              {"0"},
		"lang_id":           {"1"},
		"timezone":          {"3"},
		"offset":            {strconv.Itoa(offset)},
		"show_from_main":    {"0"},
		"client_v":         {""},
		"length":            {strconv.Itoa(pageLength)},
		"sort_mode":         {"2"},
		"b_id":              {""},
		"popular":           {"1"},
	}
	rawURL := u + "?" + params.Encode()
	body, err := c.doRequest(ctx, rawURL, c.baseURL+"/line/football")
	if err != nil {
		return nil, err
	}
	var resp LineResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal line response: %w", err)
	}
	return &resp, nil
}

// GetMatch fetches full line for one match (includes t_b for corners, fouls, cards, etc.).
func (c *Client) GetMatch(ctx context.Context, tournamentRegion, tournament, league int, gameID int) (*LineResponse, error) {
	u := c.baseURL + "/ajax/line/printer/react"
	params := url.Values{
		"all":               {"0"},
		"onlyview":          {"0"},
		"timeline":          {"0"},
		"tournaments_mode":  {"1"},
		"sport":             {strconv.Itoa(c.sportID)},
		"tournament":        {strconv.Itoa(tournament)},
		"tournament_region": {strconv.Itoa(tournamentRegion)},
		"tournament_info":   {""},
		"league":            {strconv.Itoa(league)},
		"games":             {strconv.Itoa(gameID)},
		"ross":              {"1"},
		"lang_id":            {"1"},
		"timezone":          {"3"},
		"offset":            {"0"},
		"show_from_main":    {"0"},
		"client_v":         {""},
		"length":            {strconv.Itoa(pageLength)},
		"sort_mode":         {"2"},
		"b_id":              {""},
		"popular":           {"1"},
	}
	rawURL := u + "?" + params.Encode()
	body, err := c.doRequest(ctx, rawURL, c.baseURL+"/line/football")
	if err != nil {
		return nil, err
	}
	var resp LineResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal match response: %w", err)
	}
	return &resp, nil
}

func (c *Client) doRequest(ctx context.Context, rawURL, referer string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req, referer)

	if len(c.proxyList) > 0 {
		return c.doRequestWithProxies(ctx, req, referer)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) doRequestWithProxies(ctx context.Context, req *http.Request, referer string) ([]byte, error) {
	for i := 0; i < len(c.proxyList); i++ {
		c.proxyMu.Lock()
		idx := (c.proxyIndex + i) % len(c.proxyList)
		proxyURLStr := c.proxyList[idx]
		c.proxyMu.Unlock()

		proxyURL, err := url.Parse(proxyURLStr)
		if err != nil {
			continue
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = http.ProxyURL(proxyURL)
		client := &http.Client{Timeout: c.httpClient.Timeout, Transport: transport}

		r2, _ := http.NewRequestWithContext(ctx, req.Method, req.URL.String(), nil)
		c.setHeaders(r2, referer)

		resp, err := client.Do(r2)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}
		if resp.StatusCode == http.StatusOK {
			c.proxyMu.Lock()
			c.proxyIndex = idx
			c.proxyMu.Unlock()
			return body, nil
		}
	}
	return c.doRequestDirect(ctx, req, referer)
}

func (c *Client) doRequestDirect(ctx context.Context, req *http.Request, referer string) ([]byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (c *Client) setHeaders(req *http.Request, referer string) {
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
	req.Header.Set("Referer", referer)
	req.Header.Set("imprinthash", c.imprintHash)
	req.Header.Set("frontversion", c.frontVersion)
}
