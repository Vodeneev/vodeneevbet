package fonbet

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/enums"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/enums/fonbet"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
)

// EventFetcher handles fetching events from Fonbet API
type EventFetcher struct {
	client  *http.Client
	config  *config.Config
	baseURL string
}

// NewEventFetcher creates a new event fetcher with connection pooling
func NewEventFetcher(config *config.Config) interfaces.EventFetcher {
	// Create HTTP client with connection pooling for better performance
	transport := &http.Transport{
		MaxIdleConns:        100,              // Максимум idle соединений
		MaxIdleConnsPerHost: 10,               // Максимум idle соединений на хост
		IdleConnTimeout:     90 * time.Second, // Таймаут для idle соединений
		DisableKeepAlives:   false,            // Включить keep-alive для переиспользования соединений
	}
	
	return &EventFetcher{
		client: &http.Client{
			Timeout:   config.Parser.Timeout,
			Transport: transport,
		},
		config:  config,
		baseURL: config.Parser.Fonbet.BaseURL,
	}
}

// FetchEvents fetches events for a specific sport with retry logic
func (f *EventFetcher) FetchEvents(sport string) ([]byte, error) {
	var lastErr error
	maxRetries := 3
	
	for attempt := 1; attempt <= maxRetries; attempt++ {
		fmt.Printf("🔄 HTTP fetch attempt %d/%d for sport: %s\n", attempt, maxRetries, sport)
		
		req, err := http.NewRequest("GET", f.baseURL, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to create request: %w", err)
			continue
		}

		q := req.URL.Query()
		q.Set("lang", f.config.Parser.Fonbet.Lang)
		q.Set("version", f.config.Parser.Fonbet.Version)
		
		// Convert sport string to enum and get scope market
		if sportEnum, valid := enums.ParseSport(sport); valid {
			scopeMarket := fonbet.GetScopeMarket(sportEnum)
			q.Set("scopeMarket", scopeMarket.String())
		}
		req.URL.RawQuery = q.Encode()

		req.Header.Set("User-Agent", f.config.Parser.UserAgent)
		for key, value := range f.config.Parser.Headers {
			req.Header.Set(key, value)
		}

		resp, err := f.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to make request (attempt %d): %w", attempt, err)
			if attempt < maxRetries {
				fmt.Printf("⏳ Retrying in 2 seconds...\n")
				time.Sleep(2 * time.Second)
			}
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("unexpected status code: %d (attempt %d)", resp.StatusCode, attempt)
			if attempt < maxRetries {
				fmt.Printf("⏳ Retrying in 2 seconds...\n")
				time.Sleep(2 * time.Second)
			}
			continue
		}

		// Success!
		fmt.Printf("✅ HTTP fetch successful on attempt %d\n", attempt)
		return f.readResponseBody(resp)
	}
	
	return nil, fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

// FetchEventFactors fetches factors for a specific event
func (f *EventFetcher) FetchEventFactors(eventID int64) ([]byte, error) {
	eventURL := "https://line52w.bk6bba-resources.com/events/event"
	req, err := http.NewRequest("GET", eventURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := req.URL.Query()
	q.Set("lang", f.config.Parser.Fonbet.Lang)
	q.Set("version", "0")
	q.Set("eventId", fmt.Sprintf("%d", eventID))
	q.Set("scopeMarket", "1600") // Football scope market
	req.URL.RawQuery = q.Encode()

	req.Header.Set("User-Agent", f.config.Parser.UserAgent)
	for key, value := range f.config.Parser.Headers {
		req.Header.Set(key, value)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return f.readResponseBody(resp)
}

// readResponseBody reads response body with gzip support
func (f *EventFetcher) readResponseBody(resp *http.Response) ([]byte, error) {
	var body []byte
	var err error

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
