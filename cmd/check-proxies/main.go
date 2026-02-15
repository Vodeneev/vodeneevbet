// check-proxies loads proxy_list from config and tests each proxy (connectivity + auth).
// Use to verify proxies work and payment has not expired.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	pkgconfig "github.com/Vodeneev/vodeneevbet/internal/pkg/config"
)

const (
	checkURL   = "https://api.ipify.org"
	timeoutSec = 15
)

func main() {
	configPath := flag.String("config", "configs/production.yaml", "Path to YAML config with proxy_list")
	flag.Parse()

	cfg, err := pkgconfig.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	// Collect unique proxies from all parser sections that have proxy_list
	seen := make(map[string]struct{})
	var list []string
	for _, s := range cfg.Parser.Pinnacle.ProxyList {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			list = append(list, s)
		}
	}
	if len(list) == 0 {
		fmt.Println("No proxy_list found in config (parser.pinnacle.proxy_list).")
		os.Exit(0)
	}

	fmt.Printf("Checking %d proxies (timeout %ds, test URL %s)...\n\n", len(list), timeoutSec, checkURL)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var wg sync.WaitGroup
	type result struct {
		idx  int
		addr string
		ok   bool
		ip   string
		err  string
	}
	results := make([]result, len(list))
	for i, proxyURL := range list {
		wg.Add(1)
		go func(i int, proxyURL string) {
			defer wg.Done()
			ip, err := checkProxy(ctx, proxyURL)
			results[i] = result{idx: i, addr: maskProxy(proxyURL), ok: err == nil, ip: ip}
			if err != nil {
				results[i].err = err.Error()
			}
		}(i, proxyURL)
	}
	wg.Wait()

	okCount := 0
	for _, r := range results {
		if r.ok {
			okCount++
			fmt.Printf("[OK] %s -> IP: %s\n", r.addr, r.ip)
		} else {
			fmt.Printf("[FAIL] %s -> %s\n", r.addr, r.err)
		}
	}

	fmt.Printf("\n--- Summary: %d OK, %d FAIL (total %d)\n", okCount, len(list)-okCount, len(list))
	if okCount == 0 {
		fmt.Println("All proxies failed. Possible causes: expired payment, wrong credentials, or network issues.")
		os.Exit(1)
	}
}

func checkProxy(ctx context.Context, proxyURL string) (ip string, err error) {
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	client := &http.Client{
		Timeout: timeoutSec * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(parsed),
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	buf := make([]byte, 64)
	n, _ := resp.Body.Read(buf)
	ip = strings.TrimSpace(string(buf[:n]))
	if ip == "" {
		return "", fmt.Errorf("empty body")
	}
	return ip, nil
}

func maskProxy(proxyURL string) string {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return proxyURL
	}
	if u.User != nil {
		u.User = url.User(u.User.Username())
	}
	return u.String()
}
