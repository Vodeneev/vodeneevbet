package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

func main() {
	mirrorURL := "https://bit.ly/pinnacle_mirror"
	if len(os.Args) > 1 {
		mirrorURL = os.Args[1]
	}

	fmt.Printf("=== Testing Mirror Resolution ===\n")
	fmt.Printf("Mirror URL: %s\n\n", mirrorURL)

	timeout := 30 * time.Second

	// Test HTTP redirect resolution first
	fmt.Println("1. Testing HTTP redirect resolution...")
	testHTTPRedirect(mirrorURL, timeout)

	// Test JavaScript resolution
	fmt.Println("\n2. Testing JavaScript resolution...")
	testJavaScriptResolution(mirrorURL, timeout)

	// Test using the actual resolveMirror function (if we can access it)
	// Since resolveMirror is not exported, we'll test the logic manually
	fmt.Println("\n3. Testing full resolution flow...")
	testFullResolution(mirrorURL, timeout)
}

func testHTTPRedirect(mirrorURL string, timeout time.Duration) {
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return nil
		},
	}

	req, err := http.NewRequest("HEAD", mirrorURL, nil)
	if err != nil {
		fmt.Printf("  Error creating request: %v\n", err)
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("  Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	finalURL := resp.Request.URL.String()
	fmt.Printf("  Final URL after HTTP redirect: %s\n", finalURL)

	parsed, err := url.Parse(finalURL)
	if err == nil {
		domain := parsed.Host
		if idx := strings.Index(domain, ":"); idx != -1 {
			domain = domain[:idx]
		}
		fmt.Printf("  Extracted domain: %s\n", domain)
		fmt.Printf("  Is IP address: %v\n", isIPAddress(domain))
	}
}

func testJavaScriptResolution(mirrorURL string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36"),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(ctx, opts...)
	defer cancel()

	ctx, cancel = chromedp.NewContext(allocCtx)
	defer cancel()

	var finalURL string

	fmt.Printf("  Navigating to %s...\n", mirrorURL)
	err := chromedp.Run(ctx,
		chromedp.Navigate(mirrorURL),
		chromedp.Sleep(5*time.Second),
		chromedp.Location(&finalURL),
	)

	if err != nil {
		fmt.Printf("  Error: %v\n", err)
		return
	}

	fmt.Printf("  Final URL after JavaScript: %s\n", finalURL)

	parsed, err := url.Parse(finalURL)
	if err == nil {
		domain := parsed.Host
		if idx := strings.Index(domain, ":"); idx != -1 {
			domain = domain[:idx]
		}
		fmt.Printf("  Extracted domain: %s\n", domain)
		fmt.Printf("  Is IP address: %v\n", isIPAddress(domain))
		fmt.Printf("  Path: %s\n", parsed.Path)
	}
}

func testFullResolution(mirrorURL string, timeout time.Duration) {
	// This simulates what NewClient does
	fmt.Printf("  Simulating NewClient resolution flow...\n")

	// First try HTTP
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return nil
		},
	}

	req, err := http.NewRequest("HEAD", mirrorURL, nil)
	if err == nil {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			finalURL := resp.Request.URL.String()

			parsed, err := url.Parse(finalURL)
			if err == nil {
				domain := parsed.Host
				if idx := strings.Index(domain, ":"); idx != -1 {
					domain = domain[:idx]
				}

				if isIPAddress(domain) {
					fmt.Printf("  HTTP redirect leads to IP %s, using JavaScript resolution...\n", domain)
					testJavaScriptResolution(mirrorURL, timeout)
				} else {
					fmt.Printf("  HTTP redirect leads to domain: %s\n", domain)
					fmt.Printf("  Using domain: %s\n", domain)
				}
			}
		}
	}
}

func isIPAddress(s string) bool {
	return net.ParseIP(s) != nil
}
