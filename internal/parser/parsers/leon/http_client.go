package leon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://leon.ru"
const defaultCtag = "ru-RU"
const eventsFlags = "reg,urlv2,orn2,mm2,rrc,nodup"
const eventFlags = "reg,urlv2,orn2,mm2,rrc,nodup,smgv2,outv2,wd3"

type Client struct {
	baseURL    string
	ctag      string
	client    *http.Client
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL: baseURL,
		ctag:   defaultCtag,
		client: &http.Client{Timeout: timeout},
	}
}

// GetSports возвращает все виды спорта с регионами и лигами.
// GET /api-2/betline/sports?ctag=ru-RU&flags=urlv2
func (c *Client) GetSports(ctx context.Context) ([]SportItem, error) {
	u := fmt.Sprintf("%s/api-2/betline/sports?ctag=%s&flags=urlv2", c.baseURL, c.ctag)
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	var list []SportItem
	if err := json.NewDecoder(body).Decode(&list); err != nil {
		return nil, fmt.Errorf("decode sports: %w", err)
	}
	return list, nil
}

// GetLeagueEvents возвращает матчи по лиге.
// GET /api-2/betline/events/all?ctag=ru-RU&league_id=...&hideClosed=true&flags=...
func (c *Client) GetLeagueEvents(ctx context.Context, leagueID int64) (*EventsResponse, error) {
	u := fmt.Sprintf("%s/api-2/betline/events/all?ctag=%s&league_id=%d&hideClosed=true&flags=%s",
		c.baseURL, c.ctag, leagueID, eventsFlags)
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	var out EventsResponse
	if err := json.NewDecoder(body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode events: %w", err)
	}
	return &out, nil
}

// GetEvent возвращает один матч со всеми рынками (полная линия).
// GET /api-2/betline/event/all?ctag=ru-RU&eventId=...&flags=...
func (c *Client) GetEvent(ctx context.Context, eventID int64) (*LeonEvent, error) {
	u := fmt.Sprintf("%s/api-2/betline/event/all?ctag=%s&eventId=%s&flags=%s",
		c.baseURL, c.ctag, strconv.FormatInt(eventID, 10), eventFlags)
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	var ev LeonEvent
	if err := json.NewDecoder(body).Decode(&ev); err != nil {
		return nil, fmt.Errorf("decode event: %w", err)
	}
	return &ev, nil
}

func (c *Client) get(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ValueBetBot/1.0 (https://github.com/Vodeneev/vodeneevbet)")
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en;q=0.8")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return resp.Body, nil
}
