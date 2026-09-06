package verification

import (
	"errors"
	"net/netip"
	"net/url"
	"strings"
)

var ErrUnsafeURL = errors.New("unsafe evidence URL")

var sensitiveQueryNames = map[string]struct{}{
	"access_token": {}, "api_key": {}, "apikey": {}, "auth": {}, "authorization": {},
	"code": {}, "credential": {}, "credentials": {}, "jwt": {}, "key": {},
	"password": {}, "passwd": {}, "secret": {}, "session": {}, "sig": {},
	"signature": {}, "token": {},
}

// sourceAuthorityPolicy groups reviewed documentation domains by the owner
// responsible for their content. The structure is intentionally data-shaped so
// future Knowledge Packs can supply additional reviewed owner/domain rules
// without weakening hostname matching.
type sourceAuthorityPolicy struct {
	Owner   string
	Domains []sourceAuthorityDomain
}

type sourceAuthorityDomain struct {
	Domain            string
	IncludeSubdomains bool
}

var builtInSourceAuthorityPolicies = []sourceAuthorityPolicy{
	{Owner: "Alby", Domains: []sourceAuthorityDomain{{Domain: "docs.albylabs.com"}}},
	{Owner: "Cloudflare", Domains: []sourceAuthorityDomain{{Domain: "developers.cloudflare.com"}}},
	{Owner: "Entire", Domains: []sourceAuthorityDomain{{Domain: "docs.entire.io"}}},
	{Owner: "GitHub", Domains: []sourceAuthorityDomain{{Domain: "docs.github.com"}}},
	{Owner: "IETF", Domains: []sourceAuthorityDomain{{Domain: "datatracker.ietf.org"}}},
	{Owner: "IEC", Domains: []sourceAuthorityDomain{{Domain: "www.iec.ch"}}},
	{Owner: "ISO", Domains: []sourceAuthorityDomain{{Domain: "www.iso.org"}}},
	{Owner: "Mozilla", Domains: []sourceAuthorityDomain{{Domain: "developer.mozilla.org"}}},
	{Owner: "NVIDIA", Domains: []sourceAuthorityDomain{{Domain: "nvidia.com"}, {Domain: "docs.nvidia.com"}}},
	{Owner: "OpenAI", Domains: []sourceAuthorityDomain{{Domain: "platform.openai.com"}}},
	{Owner: "RFC Editor", Domains: []sourceAuthorityDomain{{Domain: "www.rfc-editor.org"}}},
	{Owner: "Robotiq", Domains: []sourceAuthorityDomain{{Domain: "robotiq.com"}}},
	{Owner: "SCHUNK", Domains: []sourceAuthorityDomain{{Domain: "schunk.com"}}},
	{Owner: "Universal Robots", Domains: []sourceAuthorityDomain{{Domain: "www.universal-robots.com"}}},
}

// SanitizeFetchURL validates a repository-derived URL without resolving DNS.
// DNS and address pinning are enforced by the extractor immediately before IO.
func SanitizeFetchURL(raw string) (string, error) {
	if fragment := strings.IndexByte(raw, '#'); fragment >= 0 {
		raw = raw[:fragment]
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Hostname() == "" {
		return "", ErrUnsafeURL
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ErrUnsafeURL
	}
	if parsed.User != nil {
		return "", ErrUnsafeURL
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if blockedHostname(host) {
		return "", ErrUnsafeURL
	}
	if addr, err := netip.ParseAddr(host); err == nil && !isPublicEvidenceAddr(addr) {
		return "", ErrUnsafeURL
	}
	query := parsed.Query()
	for name := range query {
		if isSensitiveQueryName(name) {
			query.Del(name)
		}
	}
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String(), nil
}

func blockedHostname(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return true
	}
	switch host {
	case "metadata.google.internal", "metadata.goog", "instance-data.ec2.internal", "metadata.azure.internal", "metadata.azure.com":
		return true
	}
	return strings.Contains(host, "metadata.internal")
}

func isPublicEvidenceAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsUnspecified() || addr.IsMulticast() {
		return false
	}
	blocked := []netip.Prefix{
		netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("fc00::/7"), netip.MustParsePrefix("fe80::/10"),
	}
	for _, prefix := range blocked {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func isSensitiveQueryName(name string) bool {
	name = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), "-", "_"))
	if _, ok := sensitiveQueryNames[name]; ok {
		return true
	}
	return strings.Contains(name, "token") || strings.Contains(name, "secret") || strings.Contains(name, "password") || strings.Contains(name, "credential") || strings.Contains(name, "signature") || strings.Contains(name, "accesskey") || strings.Contains(name, "access_key") || strings.Contains(name, "apikey") || strings.HasSuffix(name, "key")
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	// Lookup errors can contain a URL supplied by the repository. Never echo it.
	return "source unavailable"
}

func authorityForURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") {
		return "unknown"
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	for _, ownerPolicy := range builtInSourceAuthorityPolicies {
		for _, domainPolicy := range ownerPolicy.Domains {
			if sourceHostMatchesDomain(host, domainPolicy) {
				return "authoritative"
			}
		}
	}
	return "unknown"
}

func sourceHostMatchesDomain(host string, policy sourceAuthorityDomain) bool {
	domain := strings.ToLower(strings.Trim(strings.TrimSpace(policy.Domain), "."))
	if domain == "" || host == "" {
		return false
	}
	if host == domain {
		return true
	}
	return policy.IncludeSubdomains && strings.HasSuffix(host, "."+domain)
}

func sourceTypeForURL(raw string) string {
	if authorityForURL(raw) == "authoritative" {
		return "official_documentation"
	}
	return "public_html"
}
