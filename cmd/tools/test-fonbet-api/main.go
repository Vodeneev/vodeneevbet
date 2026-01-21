package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

func main() {
	// API parameters from config
	baseURL := "https://line55w.bk6bba-resources.com/events/list"
	lang := "en"
	version := "60312723953"
	scopeMarket := "1600" // Football

	// Build request
	req, err := http.NewRequest("GET", baseURL, nil)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		return
	}

	// Add query parameters
	q := req.URL.Query()
	q.Set("lang", lang)
	q.Set("version", version)
	q.Set("scopeMarket", scopeMarket)
	req.URL.RawQuery = q.Encode()

	// Add headers
	req.Header.Set("User-Agent", "ValueBetBot/1.0 (https://github.com/Vodeneev/vodeneevbet)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Connection", "keep-alive")

	// Make request
	client := &http.Client{
		Timeout: 120 * time.Second,
	}

	fmt.Printf("Making request to: %s\n", req.URL.String())
	fmt.Println()

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error making request: %v\n", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("Status Code: %d\n", resp.StatusCode)
	fmt.Printf("Content-Encoding: %s\n", resp.Header.Get("Content-Encoding"))
	fmt.Println()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Unexpected status code: %d\n", resp.StatusCode)
		return
	}

	// Read response body
	var body []byte
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			fmt.Printf("Error creating gzip reader: %v\n", err)
			return
		}
		defer gzReader.Close()
		body, err = io.ReadAll(gzReader)
		if err != nil {
			fmt.Printf("Error reading gzipped body: %v\n", err)
			return
		}
	} else {
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("Error reading body: %v\n", err)
			return
		}
	}

	fmt.Printf("Response size: %d bytes\n", len(body))
	fmt.Println()

	// Parse JSON to count events
	var apiResponse struct {
		Events []interface{} `json:"events"`
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		fmt.Printf("Error parsing JSON: %v\n", err)
		fmt.Printf("First 500 chars of response:\n%s\n", string(body[:min(500, len(body))]))
		return
	}

	fmt.Printf("Total events returned: %d\n", len(apiResponse.Events))

	// Try to parse more details
	var detailedResponse struct {
		Events []struct {
			ID       int64  `json:"id"`
			Name     string `json:"name"`
			Level    int    `json:"level"`
			ParentID int64  `json:"parentId"`
			Kind     int    `json:"kind"`
		} `json:"events"`
	}

	if err := json.Unmarshal(body, &detailedResponse); err == nil {
		// Count main events (level 1, kind 1 = main match)
		mainMatches := 0
		for _, event := range detailedResponse.Events {
			if event.Level == 1 && event.Kind == 1 {
				mainMatches++
			}
		}
		fmt.Printf("Main matches (level=1, kind=1): %d\n", mainMatches)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
