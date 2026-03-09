// pkg/extractor/extractor.go
package extractor

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	fhttp "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/PuerkitoBio/goquery"
)

// collapseWS matches two or more consecutive whitespace characters (including newlines).
var collapseWS = regexp.MustCompile(`\s{2,}`)

// ExtractData fetches the given URL and attempts to extract structured data
// using a fallback cascade:
//
//  1. Next.js  — <script id="__NEXT_DATA__" type="application/json">
//  2. Nuxt.js  — any <script> containing window.__NUXT__
//  3. Fallback — cleaned visible body text (boilerplate tags removed)
func ExtractData(url string, browser string) (string, error) {
	doc, err := fetchDocument(url, browser)
	if err != nil {
		return "", err
	}

	// Attempt 1: Next.js __NEXT_DATA__
	if data, ok := tryNextJS(doc); ok {
		return pruneJSON(data), nil
	}

	// Attempt 2: Nuxt.js window.__NUXT__
	if data, ok := tryNuxtJS(doc); ok {
		return pruneJSON(data), nil
	}

	// Attempt 3: Clean text fallback
	return fallbackCleanText(doc), nil
}

// validateURL checks that the target URL uses http(s) and does not resolve to
// a private/internal IP address (SSRF protection).
func validateURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported scheme %q: only http and https are allowed", parsed.Scheme)
	}

	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no host")
	}

	ips, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("DNS lookup failed for %s: %w", host, err)
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("URL resolves to private/internal address %s: request blocked", ipStr)
		}
		// Block cloud metadata endpoints (169.254.169.254)
		if ip.Equal(net.ParseIP("169.254.169.254")) {
			return fmt.Errorf("URL resolves to cloud metadata address: request blocked")
		}
	}

	return nil
}

// fetchDocument performs an HTTP GET using a TLS-spoofed client that mimics
// the given browser's fingerprint and returns a parsed goquery document.
func fetchDocument(rawURL string, browser string) (*goquery.Document, error) {
	if err := validateURL(rawURL); err != nil {
		return nil, err
	}

	var selectedProfile profiles.ClientProfile
	switch strings.ToLower(browser) {
	case "safari":
		selectedProfile = profiles.Safari_IOS_16_0
	case "firefox":
		selectedProfile = profiles.Firefox_120
	case "chrome":
		fallthrough
	default:
		selectedProfile = profiles.Chrome_120
	}

	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(15),
		tls_client.WithClientProfile(selectedProfile),
		tls_client.WithNotFollowRedirects(),
	}

	client, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
	if err != nil {
		return nil, fmt.Errorf("creating tls client: %w", err)
	}

	req, err := fhttp.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fhttp.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, rawURL)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
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

	pruned := pruneValue(data, "")
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
func pruneValue(v interface{}, parentKey string) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, child := range val {
			if isJunkKey(k) {
				continue
			}
			pruned := pruneValue(child, k)
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
			pruned := pruneValue(child, "")
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
