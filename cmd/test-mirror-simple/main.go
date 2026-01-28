package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	mirrorURL := "https://bit.ly/pinnacle_mirror"
	if len(os.Args) > 1 {
		mirrorURL = os.Args[1]
	}

	fmt.Printf("=== Simple Mirror Resolution Test ===\n")
	fmt.Printf("Mirror URL: %s\n\n", mirrorURL)

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			fmt.Printf("  Redirect %d: %s\n", len(via), req.URL.String())
			return nil
		},
	}

	req, err := http.NewRequest("GET", mirrorURL, nil)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

	fmt.Println("Following redirects...")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	finalURL := resp.Request.URL.String()
	fmt.Printf("\nFinal URL: %s\n", finalURL)

	parsed, err := url.Parse(finalURL)
	if err == nil {
		domain := parsed.Host
		if idx := strings.Index(domain, ":"); idx != -1 {
			domain = domain[:idx]
		}
		fmt.Printf("Domain: %s\n", domain)
		fmt.Printf("Path: %s\n", parsed.Path)
		fmt.Printf("Is IP: %v\n", isIPAddress(domain))

		if isIPAddress(domain) {
			fmt.Printf("\n⚠️  WARNING: Final URL is an IP address, JavaScript resolution needed!\n")
			fmt.Printf("   Expected domain: www.crimsonhaven46.xyz\n")
		} else {
			fmt.Printf("\n✅ Successfully resolved to domain: %s\n", domain)
		}
	}
}

func isIPAddress(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		if len(part) == 0 || len(part) > 3 {
			return false
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}
