// pkg/extractor/extractor.go
package extractor

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// collapseWS matches two or more consecutive whitespace characters (including newlines).
var collapseWS = regexp.MustCompile(`\s{2,}`)

// ExtractData fetches the given URL and attempts to extract structured data
// using a fallback cascade:
//
//  1. Next.js  — <script id="__NEXT_DATA__" type="application/json">
//  2. Nuxt.js  — any <script> containing window.__NUXT__
//  3. Fallback — cleaned visible body text (boilerplate tags removed)
func ExtractData(url string) (string, error) {
	doc, err := fetchDocument(url)
	if err != nil {
		return "", err
	}

	// Attempt 1: Next.js __NEXT_DATA__
	if data, ok := tryNextJS(doc); ok {
		return data, nil
	}

	// Attempt 2: Nuxt.js window.__NUXT__
	if data, ok := tryNuxtJS(doc); ok {
		return data, nil
	}

	// Attempt 3: Clean text fallback
	return fallbackCleanText(doc), nil
}

// fetchDocument performs an HTTP GET with realistic headers and returns a
// parsed goquery document.
func fetchDocument(url string) (*goquery.Document, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, url)
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
