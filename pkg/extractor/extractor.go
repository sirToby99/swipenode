// pkg/extractor/extractor.go
package extractor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// maxResponseBytes is the maximum HTTP response body size (50 MB).
const maxResponseBytes = 50 << 20

// maxPruneDepth is the maximum recursion depth for JSON pruning.
const maxPruneDepth = 100

// collapseWS matches two or more consecutive whitespace characters (including newlines).
var collapseWS = regexp.MustCompile(`\s{2,}`)

// ExtractData fetches the given URL and attempts to extract structured data
// using a fallback cascade:
//
//  1. Next.js  — <script id="__NEXT_DATA__" type="application/json">
//  2. Nuxt.js  — any <script> containing window.__NUXT__
//  3. Fallback — cleaned visible body text (boilerplate tags removed)

func ExtractData(url string, browser string) (string, error) {
	return extractData(url, browser, false)
}

func extractData(url string, browser string, allowPrivateTestTarget bool) (string, error) {
	doc, err := fetchDocument(url, browser, allowPrivateTestTarget)
	if err != nil {
		return "", err
	}

	return parseStructuredData(doc)
}

// ExtractDataFromFile reads a local HTML file and attempts to extract structured data
// using the same cascade as ExtractData.
func ExtractDataFromFile(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()

	doc, err := goquery.NewDocumentFromReader(file)
	if err != nil {
		return "", fmt.Errorf("parsing HTML: %w", err)
	}

	return parseStructuredData(doc)
}

// validateURL checks that the target URL uses http(s) and does not resolve to
// a private/internal IP address (SSRF protection). It returns the first valid
// resolved IP so callers can pin the connection to the validated address,
// preventing DNS rebinding attacks.
func validateURL(rawURL string, allowPrivateTestTarget bool) (string, error) {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q: only http and https are allowed", parsed.Scheme)
	}

	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("URL has no host")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("URL credentials are not allowed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return "", fmt.Errorf("DNS lookup failed for %s: %w", host, err)
	}

	var pinnedIP string
	for _, ip := range ips {
		ip = ip.Unmap()
		if !allowPrivateTestTarget && !isPublicAddress(ip) {
			return "", fmt.Errorf("URL resolves to private/internal address %s: request blocked", ip)
		}
		// Block cloud metadata endpoints (169.254.169.254) — even in test mode.
		if ip == netip.MustParseAddr("169.254.169.254") {
			return "", fmt.Errorf("URL resolves to cloud metadata address: request blocked")
		}
		if pinnedIP == "" {
			pinnedIP = ip.String()
		}
	}

	if pinnedIP == "" {
		return "", fmt.Errorf("DNS lookup returned no usable addresses for %s", host)
	}

	return pinnedIP, nil
}

func isPublicAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsUnspecified() || addr.IsMulticast() {
		return false
	}
	if addr.Is4() {
		for _, prefix := range []netip.Prefix{
			netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
			netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
			netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
			netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
		} {
			if prefix.Contains(addr) {
				return false
			}
		}
		return true
	}
	return !netip.MustParsePrefix("100::/64").Contains(addr) && !netip.MustParsePrefix("2001:db8::/32").Contains(addr) && !netip.MustParsePrefix("fc00::/7").Contains(addr) && !netip.MustParsePrefix("fe80::/10").Contains(addr)
}

// fetchDocument performs an HTTP GET with the standard library. The legacy
// browser argument selects deterministic compatibility headers only; SwipeNode
// does not impersonate browser TLS fingerprints or attempt to evade access
// controls. The connection is pinned to the address validated by validateURL
// while the original URL remains intact for Host routing and TLS SNI.
func fetchDocument(rawURL string, browser string, allowPrivateTestTarget bool) (*goquery.Document, error) {
	pinnedIP, err := validateURL(rawURL, allowPrivateTestTarget)
	if err != nil {
		return nil, err
	}

	var userAgent string
	switch strings.ToLower(browser) {
	case "safari":
		userAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.0 Mobile/15E148 Safari/604.1"
	case "firefox":
		userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:120.0) Gecko/20100101 Firefox/120.0"
	case "chrome":
		userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	default:
		return nil, fmt.Errorf("unsupported browser %q: use chrome, safari, or firefox", browser)
	}

	parsed, _ := url.Parse(rawURL) // already validated
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(pinnedIP, port))
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequest(http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, rawURL)
	}

	doc, err := goquery.NewDocumentFromReader(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}

	return doc, nil
}

// tryNextJS looks for the Next.js hydration payload.
func tryNextJS(doc *goquery.Document) (string, bool) {
	sel := doc.Find(`script#__NEXT_DATA__[type="application/json"]`)
	if sel.Length() == 0 {
		return "", false
	}
	data := strings.TrimSpace(sel.First().Text())
	if data == "" {
		return "", false
	}
	return data, true
}

// tryNuxtJS scans all <script> tags for one containing "window.__NUXT__".
func tryNuxtJS(doc *goquery.Document) (string, bool) {
	var result string
	doc.Find("script").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		text := s.Text()
		if strings.Contains(text, "window.__NUXT__") {
			result = strings.TrimSpace(text)
			return false // stop iterating
		}
		return true
	})
	if result == "" {
		return "", false
	}
	return result, true
}

// fallbackCleanText strips boilerplate elements and returns the remaining
// visible text with normalised whitespace.
func fallbackCleanText(doc *goquery.Document) string {
	// Remove elements that carry no useful content.
	doc.Find("script, style, noscript, header, footer, nav").Remove()

	raw := doc.Find("body").Text()
	clean := collapseWS.ReplaceAllString(raw, "\n")
	return strings.TrimSpace(clean)
}

// junkKeySubstrings are case-insensitive substrings that mark a key for removal.
var junkKeySubstrings = []string{"tracking", "analytics", "pixel", "telemetry"}

// pruneJSON strips tracking/analytics keys, huge base64-like strings, and
// resulting empty containers from a JSON payload to save LLM tokens.
func pruneJSON(rawJSON string) string {
	var data interface{}
	if err := json.Unmarshal([]byte(rawJSON), &data); err != nil {
		return rawJSON // unparseable → return as-is
	}

	pruned := pruneValue(data, "", 0)
	if pruned == nil {
		return rawJSON
	}

	out, err := json.Marshal(pruned)
	if err != nil {
		return rawJSON
	}
	return string(out)
}

// pruneValue recursively walks a decoded JSON value and applies pruning rules.
// parentKey is the map key that led to this value (empty at the root).
// depth tracks recursion depth to prevent stack overflow on deeply nested JSON.
func pruneValue(v interface{}, parentKey string, depth int) interface{} {
	if depth > maxPruneDepth {
		return v // stop recursing, return as-is
	}

	switch val := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, child := range val {
			if isJunkKey(k) {
				continue
			}
			pruned := pruneValue(child, k, depth+1)
			if pruned != nil {
				out[k] = pruned
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out

	case []interface{}:
		out := make([]interface{}, 0, len(val))
		for _, child := range val {
			pruned := pruneValue(child, "", depth+1)
			if pruned != nil {
				out = append(out, pruned)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out

	case string:
		if len(val) > 500 && !strings.Contains(val, " ") {
			return nil
		}
		return val

	default:
		return v
	}
}

// isJunkKey returns true if the key contains any junk substring (case-insensitive).
func isJunkKey(key string) bool {
	lower := strings.ToLower(key)
	for _, sub := range junkKeySubstrings {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}

// parseStructuredData extracts structured data from modern web frameworks
// (Next.js, Nuxt, Remix, Gatsby) and JSON-LD markup.
func parseStructuredData(doc *goquery.Document) (string, error) {
	result := make(map[string]interface{})

	// 1. Next.js (NEXT_DATA)
	nextData := doc.Find("script#__NEXT_DATA__").Text()
	if nextData != "" {
		var jsonMap map[string]interface{}
		if err := json.Unmarshal([]byte(nextData), &jsonMap); err == nil {
			result["nextjs"] = jsonMap
		}
	}

	// 2. JSON-LD (SEO and structured data)
	var jsonLdData []interface{}
	doc.Find("script[type='application/ld+json']").Each(func(i int, s *goquery.Selection) {
		var ldMap interface{}
		if err := json.Unmarshal([]byte(s.Text()), &ldMap); err == nil {
			jsonLdData = append(jsonLdData, ldMap)
		}
	})
	if len(jsonLdData) > 0 {
		result["json_ld"] = jsonLdData
	}

	// 3. Nuxt.js, Gatsby & Remix (Raw inline scripts)
	doc.Find("script").Each(func(i int, s *goquery.Selection) {
		text := s.Text()
		if strings.Contains(text, "window.__NUXT__") {
			result["nuxtjs_raw"] = text
		}
		if strings.Contains(text, "window.___gatsby") || strings.Contains(text, "pageData") {
			result["gatsby_raw"] = text
		}
		if strings.Contains(text, "window.__remixContext") {
			result["remix_raw"] = text
		}
	})

	// No structured data found — fall back to cleaned visible text.
	if len(result) == 0 {
		return fallbackCleanText(doc), nil
	}

	// Marshal to JSON, then prune tracking/base64/telemetry noise.
	jsonBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}

	return pruneJSON(string(jsonBytes)), nil
}
