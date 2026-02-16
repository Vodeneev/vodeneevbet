package olimp

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://www.olimp.bet/api/v4/0/line"
const defaultReferer = "https://www.olimp.bet/line/futbol-1/"

type Client struct {
	baseURL  string
	sportID  int
	referer  string
	client   *http.Client
}

func NewClient(baseURL string, sportID int, timeout time.Duration, referer string) *Client {
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
	return &Client{
		baseURL: baseURL,
		sportID: sportID,
		referer: referer,
		client:  &http.Client{Timeout: timeout},
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
	req.Header.Set("Referer", referer)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
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
