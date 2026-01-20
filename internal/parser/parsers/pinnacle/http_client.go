package pinnacle

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type Client struct {
	baseURL    string
	apiKey     string
	deviceUUID string
	httpClient *http.Client
}

func NewClient(baseURL, apiKey, deviceUUID string, timeout time.Duration) *Client {
	// Allow env overrides to avoid committing secrets into configs.
	if apiKey == "" {
		apiKey = os.Getenv("PINNACLE_API_KEY")
	}
	if deviceUUID == "" {
		deviceUUID = os.Getenv("PINNACLE_DEVICE_UUID")
	}

	return &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		deviceUUID: deviceUUID,
		httpClient: &http.Client{Timeout: timeout},
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

func (c *Client) getJSON(path string, out any) error {
	if c.baseURL == "" {
		c.baseURL = "https://guest.api.arcadia.pinnacle.com"
	}
	url := c.baseURL + path

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", "vodeneevbet/1.0")

	// Pinnacle guest API expects these headers (captured from browser).
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	if c.deviceUUID != "" {
		req.Header.Set("X-Device-UUID", c.deviceUUID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(b))
	}

	body, err := readBodyMaybeGzip(resp)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	return nil
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

