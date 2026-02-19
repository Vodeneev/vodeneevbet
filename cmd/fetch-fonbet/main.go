package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/enums"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/enums/fonbet"
)

func main() {
	configPath := flag.String("config", "configs/production.yaml", "Path to config file")
	sportStr := flag.String("sport", "football", "Sport to fetch (football, dota2, cs)")
	outputFile := flag.String("output", "fonbet_response.json", "Output JSON file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	sport, valid := enums.ParseSport(*sportStr)
	if !valid {
		fmt.Fprintf(os.Stderr, "Invalid sport: %s\n", *sportStr)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", cfg.Parser.Fonbet.BaseURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create request: %v\n", err)
		os.Exit(1)
	}

	q := req.URL.Query()
	q.Set("lang", cfg.Parser.Fonbet.Lang)
	q.Set("version", cfg.Parser.Fonbet.Version)
	scopeMarket := fonbet.GetScopeMarket(sport)
	q.Set("scopeMarket", scopeMarket.String())
	req.URL.RawQuery = q.Encode()

	req.Header.Set("User-Agent", cfg.Parser.UserAgent)
	for key, value := range cfg.Parser.Headers {
		req.Header.Set(key, value)
	}

	fmt.Printf("Fetching Fonbet API...\n")
	client := &http.Client{Timeout: cfg.Parser.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to make request: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Unexpected status code: %d\nResponse: %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	var body []byte
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create gzip reader: %v\n", err)
			os.Exit(1)
		}
		defer gzReader.Close()
		body, err = io.ReadAll(gzReader)
	} else {
		body, err = io.ReadAll(resp.Body)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read body: %v\n", err)
		os.Exit(1)
	}

	var jsonData interface{}
	if err := json.Unmarshal(body, &jsonData); err != nil {
		fmt.Fprintf(os.Stderr, "Response is not valid JSON: %v\n", err)
		os.Exit(1)
	}

	prettyJSON, err := json.MarshalIndent(jsonData, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal JSON: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*outputFile, prettyJSON, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Success! Response saved to %s (%d bytes)\n", *outputFile, len(body))
}
