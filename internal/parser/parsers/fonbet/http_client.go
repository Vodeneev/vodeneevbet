package fonbet

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/enums"
)

type HTTPClient struct {
	client  *http.Client
	config  *config.Config
	baseURL string
}

func NewHTTPClient(config *config.Config) *HTTPClient {
	return &HTTPClient{
		client: &http.Client{
			Timeout: config.Parser.Timeout,
		},
		config:  config,
		baseURL: config.Parser.Fonbet.BaseURL,
	}
}

func (c *HTTPClient) GetEvents(sport enums.Sport) ([]byte, error) {
	req, err := http.NewRequest("GET", c.baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := req.URL.Query()
	q.Set("lang", c.config.Parser.Fonbet.Lang)
	q.Set("version", c.config.Parser.Fonbet.Version)
	
	scopeMarket := enums.GetScopeMarket(sport)
	q.Set("scopeMarket", scopeMarket.String())
	req.URL.RawQuery = q.Encode()

	req.Header.Set("User-Agent", c.config.Parser.UserAgent)
	for key, value := range c.config.Parser.Headers {
		req.Header.Set(key, value)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var body []byte
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzReader.Close()
		
		body, err = io.ReadAll(gzReader)
		if err != nil {
			return nil, fmt.Errorf("failed to read gzipped body: %w", err)
		}
	} else {
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read body: %w", err)
		}
	}

	return body, nil
}
