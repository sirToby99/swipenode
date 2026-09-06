// Package knowledge defines SwipeNode's local, declarative source policies.
package knowledge

import (
	"fmt"
	"net/netip"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sirToby99/swipenode/internal/verification"
	"golang.org/x/net/publicsuffix"
)

const SchemaVersion = "swipenode.knowledge-pack.v1"

type Origin string

const (
	OriginBuiltin Origin = "builtin"
	OriginManaged Origin = "managed"
	OriginProject Origin = "project"
)

type TechnicalIdentity struct {
	Kind  string `yaml:"kind" json:"kind"`
	Owner string `yaml:"owner,omitempty" json:"owner,omitempty"`
	Name  string `yaml:"name" json:"name"`
}

type Scope struct {
	Topics     []string `yaml:"topics" json:"topics"`
	Components []string `yaml:"components,omitempty" json:"components,omitempty"`
	Versions   []string `yaml:"versions,omitempty" json:"versions,omitempty"`
}

type Source struct {
	ID                string   `yaml:"id" json:"id"`
	CanonicalURL      string   `yaml:"canonical_url" json:"canonical_url"`
	AuthoritativeHost string   `yaml:"authoritative_host" json:"authoritative_host"`
	SourceType        string   `yaml:"source_type" json:"source_type"`
	Strategy          string   `yaml:"strategy" json:"strategy"`
	Endpoints         []string `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
	PathPattern       string   `yaml:"path_pattern,omitempty" json:"path_pattern,omitempty"`
	HTTPSRequired     bool     `yaml:"https_required" json:"https_required"`
	Authoritative     bool     `yaml:"authoritative" json:"authoritative"`
	AllowSubdomains   bool     `yaml:"allow_subdomains,omitempty" json:"allow_subdomains,omitempty"`
	Revision          string   `yaml:"revision,omitempty" json:"revision,omitempty"`
}

type TrustPolicy struct {
	RequireProvenance bool `yaml:"require_provenance" json:"require_provenance"`
}

type FreshnessPolicy struct {
	CheckStrategy     string `yaml:"check_strategy" json:"check_strategy"`
	SuggestedInterval string `yaml:"suggested_interval" json:"suggested_interval"`
	RevisionStrategy  string `yaml:"revision_strategy" json:"revision_strategy"`
}

type Provenance struct {
	ReviewedAt string `yaml:"reviewed_at" json:"reviewed_at"`
	ReviewedBy string `yaml:"reviewed_by" json:"reviewed_by"`
	Basis      string `yaml:"basis" json:"basis"`
}

type Pack struct {
	SchemaVersion string              `yaml:"schema_version" json:"schema_version"`
	ID            string              `yaml:"id" json:"id"`
	Owner         string              `yaml:"owner" json:"owner"`
	Project       string              `yaml:"project,omitempty" json:"project,omitempty"`
	Description   string              `yaml:"description" json:"description"`
	Identities    []TechnicalIdentity `yaml:"identities" json:"identities"`
	Scope         Scope               `yaml:"scope" json:"scope"`
	Sources       []Source            `yaml:"sources" json:"sources"`
	Trust         TrustPolicy         `yaml:"trust" json:"trust"`
	Freshness     FreshnessPolicy     `yaml:"freshness" json:"freshness"`
	Provenance    Provenance          `yaml:"provenance" json:"provenance"`
	Origin        Origin              `yaml:"-" json:"origin"`
}

type ResolvedSource struct {
	PackID   string `json:"pack_id"`
	SourceID string `json:"source_id"`
	URL      string `json:"url"`
	Owner    string `json:"owner"`
	Type     string `json:"source_type"`
	Revision string `json:"revision,omitempty"`
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
var secretPattern = regexp.MustCompile(`(?i)\b(token|secret|password|passwd|api[_-]?key|authorization)\s*[:=]\s*[^\s]+`)

var allowedIdentityKinds = map[string]bool{
	"vendor": true, "project": true, "product": true, "product_family": true,
	"library": true, "protocol": true, "standard": true, "domain": true,
}

var allowedSourceTypes = map[string]bool{
	"official_documentation": true, "api_reference": true, "compatibility_matrix": true,
	"standards_specification": true, "vendor_support": true,
}

func (p Pack) Validate() error {
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version must be %q", SchemaVersion)
	}
	if !idPattern.MatchString(p.ID) {
		return fmt.Errorf("invalid pack id %q", p.ID)
	}
	if strings.TrimSpace(p.Owner) == "" || strings.TrimSpace(p.Description) == "" {
		return fmt.Errorf("owner and description are required")
	}
	if len(p.Identities) == 0 || len(p.Sources) == 0 {
		return fmt.Errorf("at least one identity and source are required")
	}
	if p.Trust.RequireProvenance && (p.Provenance.ReviewedAt == "" || p.Provenance.ReviewedBy == "" || p.Provenance.Basis == "") {
		return fmt.Errorf("review provenance is required")
	}
	if p.Provenance.ReviewedAt != "" {
		if _, err := time.Parse("2006-01-02", p.Provenance.ReviewedAt); err != nil {
			return fmt.Errorf("invalid provenance reviewed_at: %w", err)
		}
	}
	if p.Freshness.SuggestedInterval != "" {
		interval, err := time.ParseDuration(p.Freshness.SuggestedInterval)
		if err != nil {
			return fmt.Errorf("invalid freshness suggested_interval: %w", err)
		}
		if interval <= 0 {
			return fmt.Errorf("freshness suggested_interval must be positive")
		}
	}
	seen := map[string]bool{}
	for _, identity := range p.Identities {
		if strings.TrimSpace(identity.Kind) == "" || strings.TrimSpace(identity.Name) == "" {
			return fmt.Errorf("identity kind and name are required")
		}
		if !allowedIdentityKinds[identity.Kind] {
			return fmt.Errorf("unsupported identity kind %q", identity.Kind)
		}
	}
	for _, source := range p.Sources {
		if err := source.validate(); err != nil {
			return fmt.Errorf("source %q: %w", source.ID, err)
		}
		if seen[source.ID] {
			return fmt.Errorf("duplicate source id %q", source.ID)
		}
		if source.Authoritative && !p.Trust.RequireProvenance {
			return fmt.Errorf("authoritative source %q requires trust.require_provenance", source.ID)
		}
		seen[source.ID] = true
	}
	switch p.Freshness.CheckStrategy {
	case "manual", "content_hash", "http_metadata_and_content_hash":
	default:
		return fmt.Errorf("unsupported freshness check_strategy %q", p.Freshness.CheckStrategy)
	}
	switch p.Freshness.RevisionStrategy {
	case "source_derived", "content_hash", "explicit_source_revision":
	default:
		return fmt.Errorf("unsupported freshness revision_strategy %q", p.Freshness.RevisionStrategy)
	}
	return nil
}

func (s Source) validate() error {
	if !idPattern.MatchString(s.ID) || strings.TrimSpace(s.SourceType) == "" || strings.TrimSpace(s.AuthoritativeHost) == "" {
		return fmt.Errorf("valid id, source_type, and authoritative_host are required")
	}
	if !allowedSourceTypes[s.SourceType] {
		return fmt.Errorf("unsupported source_type %q", s.SourceType)
	}
	u, err := url.ParseRequestURI(s.CanonicalURL)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("invalid canonical_url")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("canonical_url must not contain credentials, query parameters, or fragments")
	}
	if _, err := verification.SanitizeFetchURL(s.CanonicalURL); err != nil {
		return fmt.Errorf("canonical_url violates global network safety policy")
	}
	boundary := strings.ToLower(strings.Trim(strings.TrimSpace(s.AuthoritativeHost), "."))
	if _, err := netip.ParseAddr(boundary); err == nil {
		return fmt.Errorf("authoritative_host must be a DNS hostname, not an IP address")
	}
	if _, err := netip.ParseAddr(u.Hostname()); err == nil {
		return fmt.Errorf("canonical_url source identity must use a DNS hostname")
	}
	if suffix, _ := publicsuffix.PublicSuffix(boundary); boundary == suffix {
		return fmt.Errorf("authoritative_host must not be a public suffix")
	}
	if s.Authoritative && !s.HTTPSRequired {
		return fmt.Errorf("authoritative sources must require HTTPS")
	}
	if s.HTTPSRequired && !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("canonical_url must use HTTPS")
	}
	if !hostMatches(u.Hostname(), s.AuthoritativeHost, s.AllowSubdomains) {
		return fmt.Errorf("canonical_url is outside authoritative_host boundary")
	}
	switch s.Strategy {
	case "explicit_url", "known_documentation_index", "sitemap_endpoint":
	case "base_url_endpoints":
		if len(s.Endpoints) == 0 {
			return fmt.Errorf("base_url_endpoints requires endpoints")
		}
	case "path_pattern":
		if s.PathPattern == "" {
			return fmt.Errorf("path_pattern strategy requires path_pattern")
		}
	default:
		return fmt.Errorf("unsupported strategy %q", s.Strategy)
	}
	for _, endpoint := range s.Endpoints {
		if endpoint == "" || strings.Contains(endpoint, "\\") {
			return fmt.Errorf("invalid endpoint %q", endpoint)
		}
		e, err := url.Parse(endpoint)
		if err != nil || e.IsAbs() || e.Host != "" || e.User != nil || e.RawQuery != "" || e.Fragment != "" {
			return fmt.Errorf("endpoint %q must be a relative path without credentials, query, or fragment", endpoint)
		}
	}
	return nil
}

func (p Pack) ResolveSources() ([]ResolvedSource, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	var resolved []ResolvedSource
	for _, source := range p.Sources {
		// A path pattern can validate an explicit claim URL, but it is not by
		// itself a deterministic document endpoint to fetch.
		if source.Strategy == "path_pattern" {
			continue
		}
		urls := []string{source.CanonicalURL}
		if source.Strategy == "base_url_endpoints" {
			urls = urls[:0]
			base, _ := url.Parse(source.CanonicalURL)
			for _, endpoint := range source.Endpoints {
				u, _ := base.Parse(endpoint)
				urls = append(urls, u.String())
			}
		}
		for _, resolvedURL := range urls {
			resolved = append(resolved, ResolvedSource{PackID: p.ID, SourceID: source.ID, URL: resolvedURL, Owner: p.Owner, Type: source.SourceType, Revision: source.Revision})
		}
	}
	sort.Slice(resolved, func(i, j int) bool {
		if resolved[i].SourceID == resolved[j].SourceID {
			return resolved[i].URL < resolved[j].URL
		}
		return resolved[i].SourceID < resolved[j].SourceID
	})
	return resolved, nil
}

func (p Pack) AllowsURL(rawURL string) bool {
	_, ok := p.SourceForURL(rawURL)
	return ok
}

// SourceForURL returns the reviewed source rule that permits rawURL. Matching
// is constrained by both hostname boundary and resolution strategy.
func (p Pack) SourceForURL(rawURL string) (Source, bool) {
	u, err := url.ParseRequestURI(rawURL)
	if err != nil || u.User != nil || u.Hostname() == "" {
		return Source{}, false
	}
	for _, source := range p.Sources {
		if !source.Authoritative || (source.HTTPSRequired && !strings.EqualFold(u.Scheme, "https")) || !hostMatches(u.Hostname(), source.AuthoritativeHost, source.AllowSubdomains) {
			continue
		}
		canonical, _ := url.Parse(source.CanonicalURL)
		switch source.Strategy {
		case "explicit_url", "known_documentation_index", "sitemap_endpoint":
			if equalEndpoint(u, canonical) {
				return source, true
			}
		case "base_url_endpoints":
			for _, endpoint := range source.Endpoints {
				allowed, _ := canonical.Parse(endpoint)
				if equalEndpoint(u, allowed) {
					return source, true
				}
			}
		case "path_pattern":
			matched, _ := path.Match(source.PathPattern, u.EscapedPath())
			if matched {
				return source, true
			}
		}
	}
	return Source{}, false
}

func equalEndpoint(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host) && strings.TrimSuffix(a.EscapedPath(), "/") == strings.TrimSuffix(b.EscapedPath(), "/") && a.RawQuery == "" && a.Fragment == ""
}

func hostMatches(host, boundary string, allowSubdomains bool) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	boundary = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(boundary)), ".")
	if host == boundary {
		return true
	}
	return allowSubdomains && strings.HasSuffix(host, "."+boundary)
}
