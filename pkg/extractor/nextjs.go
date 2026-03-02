// pkg/extractor/nextjs.go
package extractor

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// ExtractNextData fetches the given URL and returns the raw JSON payload
// from the <script id="__NEXT_DATA__"> tag embedded by Next.js.
func ExtractNextData(url string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d for %s", resp.StatusCode, url)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("parsing HTML: %w", err)
	}

	sel := doc.Find(`script#__NEXT_DATA__[type="application/json"]`)
	if sel.Length() == 0 {
		return "", fmt.Errorf("no __NEXT_DATA__ script found on %s", url)
	}

	data := strings.TrimSpace(sel.First().Text())
	if data == "" {
		return "", fmt.Errorf("__NEXT_DATA__ script is empty on %s", url)
	}

	return data, nil
}
