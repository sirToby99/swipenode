// Package extractor retrieves and parses public HTML without executing JavaScript.
package extractor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const maxResponseBytes = 10 << 20

var (
	ErrSSRFBlocked       = errors.New("target address is not publicly routable")
	ErrUnsupportedScheme = errors.New("only http and https URLs are supported")
)

// Metrics describes the size of the retrieved and returned content.
type Metrics struct {
	RawBytes     int `json:"raw_bytes"`
	CleanedBytes int `json:"cleaned_bytes"`
}

// ExtractResult is the stable response contract used by HTTP and MCP.
type ExtractResult struct {
	URL            string   `json:"url"`
	Title          string   `json:"title"`
	Content        string   `json:"content"`
	StructuredData any      `json:"structured_data"`
	Framework      string   `json:"framework"`
	ContentType    string   `json:"content_type"`
	FetchedAt      string   `json:"fetched_at"`
	ETag           string   `json:"etag,omitempty"`
	LastModified   string   `json:"last_modified,omitempty"`
	NotModified    bool     `json:"not_modified,omitempty"`
	Metrics        Metrics  `json:"metrics"`
	Warnings       []string `json:"warnings"`
}

// RequestOptions carries safe HTTP validators for a deterministic source
// check. It never changes URL validation, DNS pinning, or redirect policy.
type RequestOptions struct {
	IfNoneMatch     string
	IfModifiedSince string
}

// DownloadResult is a bounded binary response retrieved through the same DNS,
// address-pinning, redirect, timeout, and credential controls as Extract.
type DownloadResult struct {
	URL          string
	ContentType  string
	FetchedAt    string
	ETag         string
	LastModified string
	Body         []byte
}

// Extract retrieves one public URL. Redirects are intentionally disabled.
func Extract(ctx context.Context, targetURL string) (*ExtractResult, error) {
	return ExtractWithOptions(ctx, targetURL, RequestOptions{})
}

// ExtractWithOptions performs the same guarded retrieval as Extract and may
// return a metadata-only result when the authoritative server answers 304.
func ExtractWithOptions(ctx context.Context, targetURL string, options RequestOptions) (*ExtractResult, error) {
	parsed, client, req, err := guardedRequest(ctx, targetURL, options, "text/html,application/xhtml+xml")
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("retrieve target: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return &ExtractResult{
			URL: parsed.String(), FetchedAt: time.Now().UTC().Format(time.RFC3339),
			ETag: cleanHTTPValidator(resp.Header.Get("ETag")), LastModified: cleanHTTPValidator(resp.Header.Get("Last-Modified")),
			NotModified: true, Warnings: []string{},
		}, nil
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, HTTPStatusError{StatusCode: resp.StatusCode}
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if len(raw) > maxResponseBytes {
		return nil, fmt.Errorf("target response exceeds size limit")
	}

	contentType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "text/html"
	}
	title, content, err := parseHTMLDocument(bytes.NewReader(raw))
	if err != nil {
		return nil, ParseError{Err: err}
	}
	return &ExtractResult{
		URL:          parsed.String(),
		Title:        title,
		Content:      strings.TrimSpace(content),
		Framework:    "text_fallback",
		ContentType:  contentType,
		FetchedAt:    time.Now().UTC().Format(time.RFC3339),
		ETag:         cleanHTTPValidator(resp.Header.Get("ETag")),
		LastModified: cleanHTTPValidator(resp.Header.Get("Last-Modified")),
		Metrics:      Metrics{RawBytes: len(raw), CleanedBytes: len(content)},
		Warnings:     []string{"Structured hydration extraction is not enabled in this release candidate."},
	}, nil
}

// Download retrieves a public binary artifact without parsing it. The caller
// supplies a strict size bound no larger than the extractor's global limit.
func Download(ctx context.Context, targetURL string, limit int64) (*DownloadResult, error) {
	if limit <= 0 || limit > maxResponseBytes {
		return nil, fmt.Errorf("download size limit is invalid")
	}
	parsed, client, req, err := guardedRequest(ctx, targetURL, RequestOptions{}, PackageAcceptHeader)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("retrieve target: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, HTTPStatusError{StatusCode: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("target response exceeds size limit")
	}
	return &DownloadResult{URL: parsed.String(), ContentType: resp.Header.Get("Content-Type"), FetchedAt: time.Now().UTC().Format(time.RFC3339), ETag: cleanHTTPValidator(resp.Header.Get("ETag")), LastModified: cleanHTTPValidator(resp.Header.Get("Last-Modified")), Body: body}, nil
}

// PackageAcceptHeader is exported so distribution clients and tests use one
// stable media-type negotiation value without importing distribution.
const PackageAcceptHeader = "application/vnd.swipenode.pack-release.v1+tar,application/json"

func guardedRequest(ctx context.Context, targetURL string, options RequestOptions, accept string) (*url.URL, *http.Client, *http.Request, error) {
	parsed, pinnedIP, err := validateURL(ctx, targetURL)
	if err != nil {
		return nil, nil, nil, err
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(pinnedIP.String(), port))
	}}
	client := &http.Client{Timeout: 10 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create request: %w", err)
	}
	req.Host = parsed.Host
	req.Header.Set("User-Agent", "SwipeNode/0.0 (zero-render retrieval)")
	req.Header.Set("Accept", accept)
	if validator := cleanHTTPValidator(options.IfNoneMatch); validator != "" {
		req.Header.Set("If-None-Match", validator)
	}
	if validator := cleanHTTPValidator(options.IfModifiedSince); validator != "" {
		req.Header.Set("If-Modified-Since", validator)
	}
	return parsed, client, req, nil
}

// HTTPStatusError keeps response classification available to refresh without
// exposing response bodies or weakening extractor safety.
type HTTPStatusError struct{ StatusCode int }

func (err HTTPStatusError) Error() string {
	return fmt.Sprintf("target returned status %d", err.StatusCode)
}

// ParseError identifies a document parser failure without exposing source
// content in logs or conflating it with a network outage.
type ParseError struct{ Err error }

func (err ParseError) Error() string {
	if err.Err == nil {
		return "parse technical document"
	}
	return "parse technical document: " + err.Err.Error()
}
func (err ParseError) Unwrap() error { return err.Err }

func cleanHTTPValidator(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 1024 || strings.ContainsAny(value, "\r\n\x00") {
		return ""
	}
	return value
}

func validateURL(ctx context.Context, rawURL string) (*url.URL, netip.Addr, error) {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return nil, netip.Addr{}, fmt.Errorf("invalid target URL")
	}
	if parsed.User != nil {
		return nil, netip.Addr{}, fmt.Errorf("target URL must not contain credentials")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, netip.Addr{}, ErrUnsupportedScheme
	}

	host := parsed.Hostname()
	if addr, err := netip.ParseAddr(host); err == nil {
		if !isPublicAddr(addr) {
			return nil, netip.Addr{}, ErrSSRFBlocked
		}
		return parsed, addr.Unmap(), nil
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, netip.Addr{}, fmt.Errorf("resolve target: %w", err)
	}
	if len(ips) == 0 {
		return nil, netip.Addr{}, fmt.Errorf("target has no usable address")
	}
	for _, addr := range ips {
		if !isPublicAddr(addr) {
			return nil, netip.Addr{}, ErrSSRFBlocked
		}
	}
	return parsed, ips[0].Unmap(), nil
}

func isPublicAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsUnspecified() || addr.IsMulticast() {
		return false
	}
	if addr.Is4() {
		// Special-use, carrier-grade NAT, benchmarking, documentation, and
		// reserved ranges are never public retrieval targets.
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

func parseHTML(r io.Reader) (string, string) {
	title, content, _ := parseHTMLDocument(r)
	return title, content
}

func parseHTMLDocument(r io.Reader) (string, string, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", "", err
	}
	var title string
	var contentBuilder strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			tag := strings.ToLower(n.Data)
			if tag == "script" || tag == "style" || tag == "noscript" || tag == "iframe" || tag == "svg" || tag == "nav" || tag == "header" || tag == "footer" {
				return
			}
			if tag == "title" && title == "" {
				title = extractText(n)
			}
			if tag == "h1" && title == "" {
				title = extractText(n)
			}
			if tag == "pre" {
				text := extractPreformattedText(n)
				if text != "" {
					contentBuilder.WriteString(text)
					contentBuilder.WriteString("\n\n")
				}
				return
			}
			if tag == "p" || tag == "h1" || tag == "h2" || tag == "h3" || tag == "h4" || tag == "h5" || tag == "h6" || tag == "li" {
				text := extractText(n)
				if text != "" {
					if tag == "li" {
						contentBuilder.WriteString("- ")
					}
					contentBuilder.WriteString(text)
					contentBuilder.WriteString("\n\n")
				}
				return
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return strings.TrimSpace(title), strings.TrimSpace(contentBuilder.String()), nil
}

func extractText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

// extractPreformattedText retains paragraph boundaries while reflowing lines
// that technical documents use only for fixed-width presentation. This keeps
// RFC-style sentences intact for downstream fact extraction without executing
// or interpreting the document.
func extractPreformattedText(n *html.Node) string {
	var raw strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			raw.WriteString(node.Data)
			return
		}
		if node.Type == html.ElementNode && strings.EqualFold(node.Data, "br") {
			raw.WriteByte('\n')
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)

	var paragraphs []string
	var lines []string
	flush := func() {
		if len(lines) == 0 {
			return
		}
		paragraphs = append(paragraphs, strings.Join(lines, " "))
		lines = nil
	}
	value := strings.ReplaceAll(raw.String(), "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	for _, line := range strings.Split(value, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			flush()
			continue
		}
		lines = append(lines, line)
	}
	flush()
	return strings.Join(paragraphs, "\n\n")
}
